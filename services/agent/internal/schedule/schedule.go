// Package schedule はぬるの「自分から動く」部分: 毎日の気分の更新・timesへの気まぐれ投稿・予算停止のお知らせ。
// 1分ごとに Tick を呼ぶ。全停止・静かモード・予算の段階を守る。ログにメッセージ本文を出さない。
package schedule

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
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

type Speaker interface {
	Spontaneous(ctx context.Context, in brain.SpontInput) (string, error)
	Report(ctx context.Context, in brain.ReportInput) (string, error)
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
}

type Scheduler struct {
	Ctl      *control.Controller
	Budget   *budget.Tracker
	Brain    Speaker
	Post     Poster
	Mem      Memory
	Set      Settings
	TimesCh  int64
	BotID    func() int64
	Log      *slog.Logger
	Now      func() time.Time
	Rng      *rand.Rand
	MaxDaily int // 1日の気まぐれ投稿の上限（既定3。仕様は最大5）
}

var jst = time.FixedZone("JST", 9*3600)

const (
	keyDrift = "mood.drift_day"
	keySpont = "spont.state"
	keyStop  = "budget.stop_announced"
	keyWeek  = "report.weekly"
	keyMonth = "report.monthly"
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
	text, err := s.Brain.Spontaneous(ctx, brain.SpontInput{ChannelID: s.TimesCh, Recent: recent})
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
