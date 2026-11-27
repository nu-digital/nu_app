// Package schedule はぬるの「自分から動く」部分: 毎日の気分の更新・timesへの気まぐれ投稿・予算停止のお知らせ。
// 1分ごとに Tick を呼ぶ。全停止・静かモード・予算の段階を守る。ログにメッセージ本文を出さない。
package schedule

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/nu-digital/nu_app/services/agent/internal/brain"
	"github.com/nu-digital/nu_app/services/agent/internal/budget"
	"github.com/nu-digital/nu_app/services/agent/internal/control"
	"github.com/nu-digital/nu_app/services/agent/internal/store"
)

// Poster は「times（設定した1つのチャンネル）へ投稿する」だけの窓口。送信先は呼び出し側で変えられない。
type Poster interface {
	PostTimes(ctx context.Context, text string) error
}

// Notifier は、リマインダーを依頼された場所へ送る（依頼した本人にだけメンションする）。
type Notifier interface {
	Remind(ctx context.Context, channelID, userID int64, text string) error
}

// ReminderStore はリマインダーの確保と結果の反映（store.Store が満たす）。
type ReminderStore interface {
	ClaimDueReminders(ctx context.Context, now time.Time, limit int) ([]store.Reminder, error)
	FinishReminder(ctx context.Context, id int64, sent bool) error
}

type Speaker interface {
	Spontaneous(ctx context.Context, in brain.SpontInput) (string, error)
	Report(ctx context.Context, in brain.ReportInput) (string, error)
	Dig(ctx context.Context, sum store.Summary) (topic, content string, err error)
	Study(ctx context.Context, hobby string, recent []string) (topic, content string, err error)
}

type Settings interface {
	GetSetting(ctx context.Context, key string) (json.RawMessage, bool, error)
	SetSetting(ctx context.Context, key string, value any) error
}

type Memory interface {
	GetState(ctx context.Context) (store.State, error)
	SaveState(ctx context.Context, st store.State) error
	RecentMessages(ctx context.Context, channelID int64, threadID *int64, beforeID int64, limit int) ([]store.Message, error)
	SummariesBetween(ctx context.Context, from, to time.Time, limit int) ([]store.Summary, error)
	RandomOldSummary(ctx context.Context, before time.Time, minMsgs int) (store.Summary, bool, error)
	AddDiary(ctx context.Context, d store.Diary) (bool, error)
	RecentDiary(ctx context.Context, limit int) ([]store.Diary, error)
}

type Scheduler struct {
	Ctl      *control.Controller
	Budget   *budget.Tracker
	Brain    Speaker
	Post     Poster
	Notify   Notifier // nilならリマインダーを配信しない
	Rem      ReminderStore
	Mem      Memory
	Set      Settings
	TimesCh  int64
	BotID    func() int64
	Log      *slog.Logger
	Now      func() time.Time
	Rng      *rand.Rand
	joinMu   sync.Mutex
	joins    []time.Time
	MaxDaily int // 1日の気まぐれ投稿の上限（既定3。仕様は最大5）
}

var jst = time.FixedZone("JST", 9*3600)

const (
	keyDrift  = "mood.drift_day"
	keySpont  = "spont.state"
	keyStop   = "budget.stop_announced"
	keyDiary  = "diary.last"
	keyKeyExp = "apikey.expires" // 運用責任者が設定する（YYYY-MM-DD）
	keyKeyNag = "apikey.nag_day"
	keyWeek   = "report.weekly"
	keyMonth  = "report.monthly"
)

// 趣味の候補（P1。P3で「調べたこと」に置き換える）。数日ごとに入れ替える。
var Hobbies = []string{"ローファイな音楽", "古いネット文化", "星や宇宙の話", "ミニマルなUI", "謎のレトロゲーム", "コーヒーの淹れ方"}

func (s *Scheduler) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Tick は1分ごとに呼ぶ。どれかが失敗しても、ほかは続ける。
func (s *Scheduler) Tick(ctx context.Context) {
	now := s.now()
	s.dailyMood(ctx, now)
	s.announceStop(ctx, now)
	s.maybeReports(ctx, now)
	s.maybeDiary(ctx, now)
	s.nagKey(ctx, now)
	s.deliverReminders(ctx, now)
	s.maybeSpeak(ctx, now)
}

// dailyMood は毎日04:00（JST）以降の最初のTickで、機嫌を揺らす。3日ごとに趣味を入れ替える。
func (s *Scheduler) dailyMood(ctx context.Context, now time.Time) {
	n := now.In(jst)
	if n.Hour() < 4 {
		return
	}
	today := n.Format("2006-01-02")
	var last string
	if raw, ok, err := s.Set.GetSetting(ctx, keyDrift); err != nil {
		return
	} else if ok {
		_ = json.Unmarshal(raw, &last)
	}
	if last == today {
		return
	}
	st, err := s.Mem.GetState(ctx)
	if err != nil {
		return
	}
	next := brain.DailyDrift(brain.State{Mood: st.Mood, Energy: st.Energy, Hobby: st.Hobby}, s.rng())
	hobby := st.Hobby
	if hobby == "" || n.YearDay()%3 == 0 {
		hobby = Hobbies[s.rng().IntN(len(Hobbies))]
	}
	if err := s.Mem.SaveState(ctx, store.State{Mood: next.Mood, Energy: next.Energy, Hobby: hobby}); err != nil {
		s.Log.Error("気分を保存できません")
		return
	}
	if err := s.Set.SetSetting(ctx, keyDrift, today); err != nil {
		s.Log.Error("気分の更新日を保存できません")
	}
}

// announceStop は、予算が尽きて寝るとき、その期間に1回だけtimesへ固定の文言を書く（APIは呼ばない）。
func (s *Scheduler) announceStop(ctx context.Context, now time.Time) {
	st := s.Budget.State(ctx)
	if st.Tier != budget.Stopped || !s.Ctl.MayPostSpontaneously() {
		return
	}
	period := st.PeriodNext.Format(time.RFC3339)
	var done string
	if raw, ok, err := s.Set.GetSetting(ctx, keyStop); err != nil {
		return
	} else if ok {
		_ = json.Unmarshal(raw, &done)
	}
	if done == period {
		return
	}
	// 先に記録する（投稿に失敗しても、繰り返し書き込まない）
	if err := s.Set.SetSetting(ctx, keyStop, period); err != nil {
		return
	}
	text := "今月ぶんの頭の体力を使い切った。" + st.PeriodNext.In(jst).Format("1月2日") + "ごろまで、ちょっと寝る。"
	if err := s.Post.PostTimes(ctx, text); err != nil {
		s.Log.Error("寝るお知らせを投稿できません")
	}
}

type spontState struct {
	Day  string    `json:"day"`
	N    int       `json:"n"`
	Last time.Time `json:"last"`
}

// hourWeight は時間帯（JST）ごとの気まぐれ投稿のしやすさ。深夜に多め。
func hourWeight(h int) float64 {
	switch {
	case h >= 0 && h < 3:
		return 1.0
	case h >= 3 && h < 8:
		return 0.15
	case h >= 8 && h < 18:
		return 0.4
	}
	return 0.7
}

// weightSum は hourWeight の1日ぶんの合計（3×1.0 + 5×0.15 + 10×0.4 + 6×0.7）。
const weightSum = 11.95

const minGap = 90 * time.Minute

// maybeSpeak は、条件を満たし、乱数に当たったら、timesへ1つ書く。
func (s *Scheduler) maybeSpeak(ctx context.Context, now time.Time) {
	if !s.Ctl.MayPostSpontaneously() {
		return
	}
	factor := s.Budget.SpontaneousFactor(ctx)
	if factor <= 0 {
		return
	}
	max := s.MaxDaily
	if max <= 0 {
		max = 3
	}
	if max > 5 {
		max = 5
	}
	n := now.In(jst)
	sp := spontState{Day: n.Format("2006-01-02")}
	if raw, ok, err := s.Set.GetSetting(ctx, keySpont); err != nil {
		return
	} else if ok {
		var old spontState
		if json.Unmarshal(raw, &old) == nil {
			sp.Last = old.Last
			if old.Day == sp.Day {
				sp.N = old.N
			}
		}
	}
	if sp.N >= max || now.Sub(sp.Last) < minGap {
		return
	}
	// 1分あたりの確率: 1日に平均 max 回ほど、時間帯で重みをつける。節約の段階では減らす。
	p := float64(max) * 0.8 / (60 * weightSum) * hourWeight(n.Hour()) * factor
	if s.rng().Float64() >= p {
		return
	}
	var recent []brain.Turn
	if rows, err := s.Mem.RecentMessages(ctx, s.TimesCh, nil, 0, 8); err == nil {
		for _, m := range rows {
			name := m.AuthorName
			if m.IsBot && s.BotID != nil && m.AuthorID == s.BotID() {
				name = "ぬる"
			}
			recent = append(recent, brain.Turn{AuthorName: name, Text: m.Content})
		}
	}
	var diary []brain.Turn
	if ds, err := s.Mem.RecentDiary(ctx, 3); err == nil {
		for _, d := range ds {
			diary = append(diary, brain.Turn{AuthorName: d.Topic, Text: d.Content})
		}
	}
	text, err := s.Brain.Spontaneous(ctx, brain.SpontInput{ChannelID: s.TimesCh, Recent: recent, Diary: diary})
	if err != nil || text == "" {
		return // 失敗・寝ている・空のときは書かない（次のチャンスを待つ）
	}
	if !s.Ctl.MayPostSpontaneously() { // 生成中に止められたら書かない
		return
	}
	// 先に数える（投稿の失敗で連投しない）
	sp.N, sp.Last = sp.N+1, now
	if err := s.Set.SetSetting(ctx, keySpont, sp); err != nil {
		return
	}
	if err := s.Post.PostTimes(ctx, text); err != nil {
		s.Log.Error("timesへ投稿できません")
	}
}

func (s *Scheduler) rng() *rand.Rand {
	if s.Rng == nil {
		s.Rng = rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 1))
	}
	return s.Rng
}

// Run は1分ごとに Tick を呼ぶ（ctxが終わるまで）。
func (s *Scheduler) Run(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Tick(ctx)
		}
	}
}

// ---- 週報・月報 ----

type reportState struct {
	Key   string    `json:"key"`   // 期間の識別（週報は金曜の日付、月報は YYYY-MM）
	Done  bool      `json:"done"`  // 投稿した（または材料が無くて見送った）
	Tries int       `json:"tries"` // 失敗の回数（上限あり）
	Last  time.Time `json:"last"`  // 最後に試した時刻
}

const (
	maxReportTries = 3
	reportRetry    = 30 * time.Minute
	maxMaterial    = 14000 // 材料の文字数の上限
)

// maybeReports は、金曜21:00（JST）以降の週報、毎月1日12:00以降の月報を、その期間に1回だけtimesへ書く。
func (s *Scheduler) maybeReports(ctx context.Context, now time.Time) {
	if !s.Ctl.MayPostSpontaneously() {
		return
	}
	n := now.In(jst)
	day0 := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC) // 日付だけ（JSTのカレンダー日）
	if n.Weekday() == time.Friday && n.Hour() >= 21 {
		from, to := day0.AddDate(0, 0, -7), day0
		title := "週報 " + from.Format("1/2") + "〜" + to.AddDate(0, 0, -1).Format("1/2")
		s.report(ctx, now, keyWeek, day0.Format("2006-01-02"), "weekly", title, from, to, "")
	}
	if n.Day() == 1 && n.Hour() >= 12 {
		to := day0
		from := to.AddDate(0, -1, 0)
		title := "月報 " + from.Format("2006年1月")
		facts := ""
		if spent, start, err := s.Budget.PreviousPeriod(ctx); err == nil {
			facts = fmt.Sprintf("ぬるの予算（%s〜の期間）: 使った %s / 上限 %s（残り %s）。\n", start.In(jst).Format("1/2"), usd(spent), usd(s.Budget.Limit()), usd(s.Budget.Limit()-spent))
		}
		s.report(ctx, now, keyMonth, to.Format("2006-01"), "monthly", title, from, to, facts)
	}
}

func usd(micro int64) string { return fmt.Sprintf("$%.2f", float64(micro)/1e6) }

func (s *Scheduler) report(ctx context.Context, now time.Time, setKey, period, kind, title string, from, to time.Time, facts string) {
	var st reportState
	if raw, ok, err := s.Set.GetSetting(ctx, setKey); err != nil {
		return
	} else if ok {
		_ = json.Unmarshal(raw, &st)
	}
	if st.Key == period && (st.Done || st.Tries >= maxReportTries || now.Sub(st.Last) < reportRetry) {
		return
	}
	if st.Key != period {
		st = reportState{Key: period}
	}
	save := func() { _ = s.Set.SetSetting(ctx, setKey, st) }
	sums, err := s.Mem.SummariesBetween(ctx, from, to, 40)
	if err != nil {
		return
	}
	if len(sums) == 0 { // 材料が無い（要約がまだ・静かな期間）。書かずに見送る
		st.Done, st.Last = true, now
		save()
		return
	}
	var material []brain.Turn
	var total, msgs int
	for _, x := range sums {
		msgs += x.MsgCount
		t := x.Content
		if total+len([]rune(t)) > maxMaterial {
			continue
		}
		total += len([]rune(t))
		material = append(material, brain.Turn{AuthorName: fmt.Sprintf("<#%d> %s", x.ChannelID, x.Day.Format("1/2")), Text: t})
	}
	facts += fmt.Sprintf("対象期間: %s〜%s。要約のあるチャンネル×日: %d、発言数の合計: %d。", from.Format("1/2"), to.AddDate(0, 0, -1).Format("1/2"), len(sums), msgs)
	st.Tries++
	st.Last = now
	save() // 先に数える（生成が失敗しても、続けて何度も呼ばない）
	text, err := s.Brain.Report(ctx, brain.ReportInput{Kind: kind, Title: title, Facts: facts, Material: material, ChannelID: s.TimesCh})
	if err != nil || text == "" {
		return
	}
	if !s.Ctl.MayPostSpontaneously() { // 生成中に止められたら書かない（次の試行で）
		return
	}
	st.Done = true
	save()
	if err := s.Post.PostTimes(ctx, text); err != nil {
		s.Log.Error("報告をtimesへ投稿できません")
	}
}

// ---- 研究日誌 ----

type diaryState struct {
	Dig string `json:"dig"` // 最後に発掘日誌を書いた日（JST）
	Web string `json:"web"` // 最後に調べもの日誌を書いた日（JST）
}

const (
	digHour = 5 // JST。毎日、この時刻以降の最初のTickで試す
	webHour = 6
	// 発掘に使う要約は、これより前（日数）の、発言がこれ以上の日のものだけ
	digMinAgeDays = 30
	digMinMsgs    = 8
)

// maybeDiary は、毎日1回ずつ「発掘日誌」（古い要約から。Web検索なし）と「調べもの日誌」（Web検索あり。予算が通常の段階のときだけ）を書く。
// 失敗しても、その日は再試行しない（費用の暴走を避ける）。投稿はしない（気まぐれ投稿のネタになる）。
func (s *Scheduler) maybeDiary(ctx context.Context, now time.Time) {
	if !s.Ctl.MayPostSpontaneously() {
		return
	}
	n := now.In(jst)
	today := n.Format("2006-01-02")
	var st diaryState
	if raw, ok, err := s.Set.GetSetting(ctx, keyDiary); err != nil {
		return
	} else if ok {
		_ = json.Unmarshal(raw, &st)
	}
	day := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
	if n.Hour() >= digHour && st.Dig != today && s.Budget.SpontaneousFactor(ctx) > 0 {
		st.Dig = today
		_ = s.Set.SetSetting(ctx, keyDiary, st) // 先に記録する
		if sum, ok, err := s.Mem.RandomOldSummary(ctx, day.AddDate(0, 0, -digMinAgeDays), digMinMsgs); err == nil && ok {
			if topic, content, err := s.Brain.Dig(ctx, sum); err == nil && content != "" {
				if _, err := s.Mem.AddDiary(ctx, store.Diary{Day: day, Kind: "dig", Topic: topic, Content: content}); err != nil {
					s.Log.Error("発掘日誌を保存できません")
				}
			}
		}
	}
	if n.Hour() >= webHour && st.Web != today && s.Budget.Remaining(ctx) > 0 && s.Ctl.MayPostSpontaneously() {
		st.Web = today
		_ = s.Set.SetSetting(ctx, keyDiary, st)
		cur, err := s.Mem.GetState(ctx)
		if err != nil || cur.Hobby == "" {
			return
		}
		var recent []string
		if ds, err := s.Mem.RecentDiary(ctx, 6); err == nil {
			for _, d := range ds {
				if d.Kind == "web" {
					recent = append(recent, d.Topic)
				}
			}
		}
		topic, content, err := s.Brain.Study(ctx, cur.Hobby, recent)
		if err != nil || content == "" {
			return
		}
		if ok, err := s.Mem.AddDiary(ctx, store.Diary{Day: day, Kind: "web", Topic: topic, Content: content}); err != nil || !ok {
			return
		}
		// 調べたことが、次のハマりになる（P1の候補リストを置き換える。長すぎる題は切る）
		h := topic
		if r := []rune(h); len(r) > 40 {
			h = string(r[:40])
		}
		cur.Hobby = h
		if err := s.Mem.SaveState(ctx, cur); err != nil {
			s.Log.Error("ハマりを更新できません")
		}
	}
}

// ---- 新メンバーへの反応 ----

var greetings = []string{
	"新入り来たな……%sか。覚えておいてやる。",
	"%sが来た。ふーん、ようこそ。別に歓迎してないけど。",
	"……%s？ ここ、けっこう変な人多いから覚悟して。",
	"%s、いらっしゃい。私はぬる。ここの住人。以上。",
}

const (
	maxJoinsPerHour = 5 // 一度に大勢（荒らし・招待リンクの拡散）が来ても連投しない
	maxNameRunes    = 30
)

// sanitizeName は、表示名を一言に入れる前に整える（改行・メンション・コード記号を除き、短くする）。
func sanitizeName(n string) string {
	n = strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '@', '`', '<', '>', '*', '_', '~', '|', '\\':
			return -1
		}
		return r
	}, n)
	n = strings.TrimSpace(n)
	if r := []rune(n); len(r) > maxNameRunes {
		n = string(r[:maxNameRunes])
	}
	return n
}

// Greet は、新しいメンバーに、timesで一言（固定の文言から。APIは呼ばない）。静か・全停止では書かない。
func (s *Scheduler) Greet(ctx context.Context, name string) {
	if !s.Ctl.MayPostSpontaneously() {
		return
	}
	name = sanitizeName(name)
	if name == "" {
		return
	}
	now := s.now()
	s.joinMu.Lock()
	kept := s.joins[:0]
	for _, t := range s.joins {
		if now.Sub(t) < time.Hour {
			kept = append(kept, t)
		}
	}
	s.joins = kept
	if len(s.joins) >= maxJoinsPerHour {
		s.joinMu.Unlock()
		return
	}
	s.joins = append(s.joins, now)
	s.joinMu.Unlock()
	if err := s.Post.PostTimes(ctx, fmt.Sprintf(greetings[s.rng().IntN(len(greetings))], name)); err != nil {
		s.Log.Error("新メンバーへの一言を投稿できません")
	}
}

// ---- APIキーの期限の催促 ----

// nagKey は、設定の apikey.expires（YYYY-MM-DD）の14日前から、毎日1回（夜21時以降）timesで催促する。期限後も続ける。
func (s *Scheduler) nagKey(ctx context.Context, now time.Time) {
	if !s.Ctl.MayPostSpontaneously() {
		return
	}
	n := now.In(jst)
	if n.Hour() < 21 {
		return
	}
	raw, ok, err := s.Set.GetSetting(ctx, keyKeyExp)
	if err != nil || !ok {
		return
	}
	var exp string
	if json.Unmarshal(raw, &exp) != nil {
		return
	}
	expDay, err := time.Parse("2006-01-02", exp)
	if err != nil {
		return
	}
	today := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
	left := int(expDay.Sub(today).Hours() / 24)
	if left > 14 {
		return
	}
	var last string
	if raw, ok, _ := s.Set.GetSetting(ctx, keyKeyNag); ok {
		_ = json.Unmarshal(raw, &last)
	}
	if last == today.Format("2006-01-02") {
		return
	}
	if err := s.Set.SetSetting(ctx, keyKeyNag, today.Format("2006-01-02")); err != nil {
		return
	}
	var text string
	switch {
	case left < 0:
		text = "私の命（APIキー）、もう期限切れてるんだけど。誰か新しいの入れて。"
	case left == 0:
		text = "私の命、今日までなんだけど。運営の誰か、キーの更新お願い。"
	default:
		text = fmt.Sprintf("私の命、あと%d日なんだけど。運営の誰か、APIキーの更新お願い。", left)
	}
	if err := s.Post.PostTimes(ctx, text); err != nil {
		s.Log.Error("キー期限の催促を投稿できません")
	}
}

// ---- リマインダーの配信 ----

const lateAfter = 10 * time.Minute

// deliverReminders は、時刻が来たリマインダーを配信する。全停止中は配信しない（保留。再開後に届く）。
// 静かモードでは配信する（頼まれたものなので）。失敗は最大3回まで再送。
func (s *Scheduler) deliverReminders(ctx context.Context, now time.Time) {
	if s.Notify == nil || s.Rem == nil || !s.Ctl.MayRespond() {
		return
	}
	rs, err := s.Rem.ClaimDueReminders(ctx, now, 10)
	if err != nil {
		return
	}
	for _, r := range rs {
		text := "⏰ リマインド: " + r.Message
		if now.Sub(r.RemindAt) > lateAfter {
			text = "⏰ リマインド（遅れてごめん）: " + r.Message
		}
		sent := s.Notify.Remind(ctx, r.ChannelID, r.UserID, text) == nil
		if !sent {
			s.Log.Warn("リマインダーを送れませんでした", "attempts", r.Attempts)
		}
		if err := s.Rem.FinishReminder(ctx, r.ID, sent); err != nil {
			s.Log.Error("リマインダーの結果を保存できません")
		}
	}
}
