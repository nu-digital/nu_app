// Package responder は「いつ・どこで・だれに応えるか」の判断と、応答の実行。
// 仕様 §3: timesは自由。ほかのチャンネルは「メンション＋メッセージ」か、ぬるの返信へのリプライのときだけ。DMは受ける。
// Botには応えない（ループ防止）。停止中（stopped）は応えない。許可リストの送信（Sender）だけを使う。ログにメッセージ本文を出さない。
package responder

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math/rand/v2"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nu-digital/nu_app/services/agent/internal/brain"
	"github.com/nu-digital/nu_app/services/agent/internal/control"
	"github.com/nu-digital/nu_app/services/agent/internal/discord"
	"github.com/nu-digital/nu_app/services/agent/internal/imageprep"
	"github.com/nu-digital/nu_app/services/agent/internal/store"
)

// Sender は、ぬるが使えるDiscordへの送信の**許可リスト**。削除・BAN・キック・ロール／チャンネル／設定の変更は無い。
type Sender interface {
	// Reply は呼ばれたメッセージへの返信（呼ばれたチャンネル・スレッド・DMにだけ送る。誰にも通知（メンション）を飛ばさない）。
	Reply(ctx context.Context, channelID, replyToID int64, text string) error
	// React は絵文字リアクション。
	React(ctx context.Context, channelID, messageID int64, emoji string) error
	// Typing は入力中の表示。
	Typing(ctx context.Context, channelID int64) error
}

// Replier は応答を作る（brain.Brain が満たす。試験では偽物）。
type Replier interface {
	Reply(ctx context.Context, in brain.Input) (brain.Output, error)
}

// History は直近の会話（store.Store が満たす）。
type History interface {
	RecentMessages(ctx context.Context, channelID int64, threadID *int64, beforeID int64, limit int) ([]store.Message, error)
}

// Memory は気分・親密度の更新（store.Store が満たす）。
type Memory interface {
	GetState(ctx context.Context) (store.State, error)
	SaveState(ctx context.Context, st store.State) error
	BumpCloseness(ctx context.Context, userID int64) error
}

// Reactions はリアクションの枠と設定（store.Store と設定が満たす）。
type Reactions interface {
	TakeReaction(ctx context.Context, day time.Time, cap int) (bool, error)
	GetSetting(ctx context.Context, key string) (json.RawMessage, bool, error)
}

// Images は添付画像の取得（縮小済みのJPEGを返す）。
type Images interface {
	Fetch(ctx context.Context, a discord.Attachment) ([]byte, error)
}

type Responder struct {
	Ctl          *control.Controller
	Brain        Replier
	Hist         History
	Mem          Memory
	Send         Sender
	Img          Images
	Reacts       Reactions // nilならリアクションしない
	Rng          *rand.Rand
	BotID        func() int64 // ぬる自身のユーザーID（接続後に分かる）
	TimesChannel int64
	Log          *slog.Logger
	Cooldown     time.Duration // 同じチャンネルでの最短の返答間隔（連投対策。既定3秒）
	Now          func() time.Time

	mu   sync.Mutex
	last map[int64]time.Time
}

const (
	historyLimit = 10
	maxImages    = 3
)

// 絵文字（言葉を使わない応答。APIを呼ばない）
const (
	emojiAsleep  = "💤"
	emojiLimit   = "⏳"
	emojiBusy    = "⌛"
	emojiNoSpeak = "🤐"
	emojiSeen    = "👀"
)

// Decide は、このメッセージに応えるか（応えるなら true）。副作用なし。
func (r *Responder) botID() int64 {
	if r.BotID == nil {
		return 0
	}
	return r.BotID()
}

func (r *Responder) Decide(in discord.Incoming) bool {
	m := in.Msg
	if m.AuthorID == r.botID() || m.IsBot || in.IsBotAuthored {
		return false // 自分・ほかのBotには応えない（ループ防止）
	}
	if !r.Ctl.MayRespond() {
		return false // 全停止
	}
	if m.GuildID == 0 {
		return true // DM
	}
	if m.ChannelID == r.TimesChannel && (m.ThreadID == nil) {
		return true // timesは自由（来た人と会話する）
	}
	return in.MentionsBot || in.RepliesToBot // ほかは、呼ばれたときだけ
}

// Handle はメッセージを受けて、応えるべきなら応える。エラーは外へ出さずログに（本文を出さない）。
func (r *Responder) Handle(ctx context.Context, in discord.Incoming) {
	if !r.Decide(in) {
		r.maybeReact(ctx, in)
		return
	}
	m := in.Msg
	if !r.cooldownOK(m.ChannelID) {
		return // 連投には応えない（静かに）
	}
	_ = r.Send.Typing(ctx, target(m))
	hist := r.history(ctx, m)
	var imgs [][]byte
	for _, a := range m.Attachments {
		if len(imgs) >= maxImages || !strings.HasPrefix(a.ContentType, "image/") {
			continue
		}
		if b, err := r.Img.Fetch(ctx, a); err == nil {
			imgs = append(imgs, b)
		} else {
			r.Log.Info("画像を読み込めませんでした", "reason", errKind(err))
		}
	}
	text := stripMention(m.Content, r.botID())
	if text == "" && len(imgs) == 0 {
		_ = r.Send.React(ctx, target(m), m.ID, emojiSeen) // 呼ばれただけ（本文も画像も無い）
		return
	}
	out, err := r.Brain.Reply(ctx, brain.Input{UserID: m.AuthorID, ChannelID: m.ChannelID, AuthorName: m.AuthorName, Where: r.where(m),
		History: hist, Text: text, Images: imgs, IsDM: m.GuildID == 0, MessageID: m.ID})
	switch {
	case errors.Is(err, brain.ErrAsleep):
		_ = r.Send.React(ctx, target(m), m.ID, emojiAsleep)
		return
	case errors.Is(err, brain.ErrUserLimit):
		_ = r.Send.React(ctx, target(m), m.ID, emojiLimit)
		return
	case errors.Is(err, brain.ErrBusy):
		_ = r.Send.React(ctx, target(m), m.ID, emojiBusy)
		return
	case err != nil:
		r.Log.Error("応答を作れませんでした", "reason", errKind(err))
		_ = r.Send.React(ctx, target(m), m.ID, emojiNoSpeak)
		return
	}
	// 生成中に停止された場合は送らない
	if !r.Ctl.MayRespond() {
		return
	}
	if err := r.Send.Reply(ctx, target(m), m.ID, out.Text); err != nil {
		r.Log.Error("返信を送れませんでした")
		return
	}
	r.afterReply(ctx, m)
}

func (r *Responder) history(ctx context.Context, m discord.Msg) []brain.Turn {
	rows, err := r.Hist.RecentMessages(ctx, m.ChannelID, m.ThreadID, m.ID, historyLimit)
	if err != nil {
		return nil // 履歴を読めなくても答える
	}
	out := make([]brain.Turn, 0, len(rows))
	for _, x := range rows {
		name := x.AuthorName
		if x.IsBot && x.AuthorID == r.botID() {
			name = "ぬる"
		}
		out = append(out, brain.Turn{AuthorName: name, Text: x.Content})
	}
	return out
}

func (r *Responder) where(m discord.Msg) string {
	switch {
	case m.GuildID == 0:
		return "DM（この人との個人的な会話。ここで聞いた話は、ほかの場所で話さない）"
	case m.ChannelID == r.TimesChannel:
		return "ぬるのtimes（自分の個人チャンネル）"
	}
	return "サーバーのほかのチャンネル（呼ばれたので来た）"
}

var (
	praiseRe = regexp.MustCompile(`ありがとう|ありがと|助かった|すごい|天才|さすが|神`)
	roughRe  = regexp.MustCompile(`うざ|ウザ|ばか|バカ|黙れ|使えない|役立たず`)
)

// afterReply は、話したことを親密度に数え、気分を簡単な規則で動かす。
func (r *Responder) afterReply(ctx context.Context, m discord.Msg) {
	if err := r.Mem.BumpCloseness(ctx, m.AuthorID); err != nil {
		r.Log.Error("親密度を更新できません")
	}
	praised, rough := praiseRe.MatchString(m.Content), roughRe.MatchString(m.Content)
	if praised == rough {
		return
	}
	st, err := r.Mem.GetState(ctx)
	if err != nil {
		return
	}
	bs := brain.State{Mood: st.Mood, Energy: st.Energy, Hobby: st.Hobby}
	if praised {
		bs = bs.Praised()
	} else {
		bs = bs.Roughed()
	}
	_ = r.Mem.SaveState(ctx, store.State{Mood: bs.Mood, Energy: bs.Energy, Hobby: bs.Hobby})
}

func (r *Responder) cooldownOK(ch int64) bool {
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}
	cd := r.Cooldown
	if cd == 0 {
		cd = 3 * time.Second
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.last == nil {
		r.last = map[int64]time.Time{}
	}
	if t, ok := r.last[ch]; ok && now.Sub(t) < cd {
		return false
	}
	r.last[ch] = now
	return true
}

var mentionRe = regexp.MustCompile(`<@!?(\d+)>`)

// stripMention は、ぬるへのメンション（<@id>）を本文から外す（ほかの人へのメンションは残す）。
func stripMention(s string, botID int64) string {
	out := mentionRe.ReplaceAllStringFunc(s, func(x string) string {
		sub := mentionRe.FindStringSubmatch(x)
		if sub[1] == itoa(botID) {
			return ""
		}
		return x
	})
	return strings.TrimSpace(out)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// errKind はエラーの種類だけを返す（本文・秘密を含めない）。
func errKind(err error) string {
	switch {
	case errors.Is(err, imageprep.ErrTooLarge):
		return "too_large"
	case errors.Is(err, imageprep.ErrUnsupported):
		return "unsupported"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	}
	return "error"
}

// リアクション: 呼ばれていない発言に、たまに絵文字だけ付ける。APIは呼ばない（言葉の規則だけ）。
// 条件: 通常モード（静か・停止では付けない）・設定でオン・1日の上限・Bot以外・自分以外・確率。
const (
	keyReactOn  = "reactions.enabled"   // false でオフ（Admin）。無ければオン
	keyReactCap = "reactions.daily_cap" // 1日の上限。無ければ10
	reactProb   = 0.08
)

var reactRules = []struct {
	re    *regexp.Regexp
	emoji string
}{
	{regexp.MustCompile(`ありがとう|感謝|助かった`), "🙏"},
	{regexp.MustCompile(`お疲れ|おつかれ|がんばった|頑張った`), "👏"},
	{regexp.MustCompile(`笑|www|草|ウケる`), "😂"},
	{regexp.MustCompile(`完成|できた|リリース|公開した|合格`), "🎉"},
	{regexp.MustCompile(`眠い|ねむい|徹夜|寝れない`), "😴"},
}

func (r *Responder) maybeReact(ctx context.Context, in discord.Incoming) {
	m := in.Msg
	if r.Reacts == nil || m.AuthorID == r.botID() || m.IsBot || in.IsBotAuthored || m.GuildID == 0 || !r.Ctl.MayPostSpontaneously() {
		return
	}
	emoji := ""
	for _, rule := range reactRules {
		if rule.re.MatchString(m.Content) {
			emoji = rule.emoji
			break
		}
	}
	if emoji == "" {
		return
	}
	rng := r.Rng
	if rng == nil {
		rng = rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 7))
	}
	if rng.Float64() >= reactProb {
		return
	}
	if raw, ok, err := r.Reacts.GetSetting(ctx, keyReactOn); err != nil || (ok && string(raw) == "false") {
		return
	}
	cap := 10
	if raw, ok, err := r.Reacts.GetSetting(ctx, keyReactCap); err == nil && ok {
		var n int
		if json.Unmarshal(raw, &n) == nil && n >= 0 {
			cap = n
		}
	}
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}
	if ok, err := r.Reacts.TakeReaction(ctx, now.In(time.FixedZone("JST", 9*3600)), cap); err != nil || !ok {
		return
	}
	if err := r.Send.React(ctx, target(m), m.ID, emoji); err != nil {
		r.Log.Error("リアクションを付けられません")
	}
}

// target は送信先のチャンネル。スレッドの中のメッセージには、そのスレッド（Msg.ChannelID は親）へ送る。
func target(m discord.Msg) int64 {
	if m.ThreadID != nil {
		return *m.ThreadID
	}
	return m.ChannelID
}
