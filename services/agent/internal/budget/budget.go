// Package budget はClaude APIの予算管理（追加課金をしない。Teamプラン付属のAPIクレジットだけ。docs/design/nul-agent.md §11）。
// 上限は**コードで**守る: 呼び出しごとに記録し、70%で自発投稿を減らし、90%でOpusを止め、100%で止まる。一人あたりの1日の上限もある。
package budget

import (
	"context"
	"encoding/json"
	"time"

	"github.com/nu-digital/nu_app/services/agent/internal/llm"
	"github.com/nu-digital/nu_app/services/agent/internal/store"
)

// Price は1トークンあたりの価格。単位はミリ・マイクロドル（= 1MTokあたりのドル価格 × 1000。例: $2/MTok = 2000）。
// 整数で正確に数えるため（Haikuのキャッシュ読み取り $0.01/MTok のような小さな単価を丸めない）。
type Price struct{ In, Out, CacheRead, CacheWrite int64 }

// Prices はモデルごとの単価（2026-11-27時点のAnthropicの公開価格。キャッシュ読み取りは入力の0.1倍・書き込みは1.25倍（5分））。
// Haiku 5.5は10万トークン以下のプロンプトの価格（それを超える場合の割増は、ぬるの使い方では起きない）。
var Prices = map[string]Price{
	llm.Sonnet: {In: 2000, Out: 10000, CacheRead: 200, CacheWrite: 2500},
	llm.Opus:   {In: 4000, Out: 20000, CacheRead: 400, CacheWrite: 5000},
	llm.Haiku:  {In: 100, Out: 500, CacheRead: 10, CacheWrite: 125},
}

// priceScale は Prices の単位（$/MTok × 1000）からマイクロドルへの換算。
const priceScale = 1000

// Cost は使用量の金額（マイクロドル）。未知のモデルは、安全側（最も高いOpus）の単価で数える。
func Cost(model string, u llm.Usage) int64 {
	p, ok := Prices[model]
	if !ok {
		p = Prices[llm.Opus]
	}
	milli := u.InputTokens*p.In + u.OutputTokens*p.Out + u.CacheReadTokens*p.CacheRead + u.CacheWriteTokens*p.CacheWrite
	return (milli + priceScale - 1) / priceScale // 切り上げ（少なく数えない）
}

type Tier int

const (
	Normal  Tier = iota // 通常
	Reduced             // 70%〜: 自発投稿を減らす
	NoOpus              // 90%〜: Opusを止める（Sonnetだけ）
	Stopped             // 100%〜、またはクレジット切れ: 止まる
)

func (t Tier) String() string {
	return [...]string{"通常", "節約（自発投稿を半減）", "Opus停止", "停止"}[t]
}

// Ledger は記録先（store.Store が満たす。試験では偽物）。
type Ledger interface {
	RecordUsage(ctx context.Context, u store.UsageRecord) error
	SpentMicroSince(ctx context.Context, from time.Time) (int64, error)
	IncDaily(ctx context.Context, day time.Time, userID int64, kind string) (int, error)
	GetSetting(ctx context.Context, key string) (json.RawMessage, bool, error)
	SetSetting(ctx context.Context, key string, value any) error
}

type Config struct {
	LimitMicro      int64          // 月の上限（既定 $60）
	AnchorDay       int            // 月の始まり（クレジットの付与日。1〜28。既定1）
	ReplyPerUserDay int            // 一人あたりの1日の返答の上限（既定20）
	OpusPerUserDay  int            // 一人あたりの1日のOpus依頼の上限（既定3）
	Location        *time.Location // 日付の基準（JST）
}

func (c Config) withDefaults() Config {
	if c.LimitMicro <= 0 {
		c.LimitMicro = 60_000_000
	}
	if c.AnchorDay < 1 || c.AnchorDay > 28 {
		c.AnchorDay = 1
	}
	if c.ReplyPerUserDay <= 0 {
		c.ReplyPerUserDay = 20
	}
	if c.OpusPerUserDay <= 0 {
		c.OpusPerUserDay = 3
	}
	if c.Location == nil {
		c.Location = time.FixedZone("JST", 9*3600)
	}
	return c
}

type Tracker struct {
	l   Ledger
	cfg Config
	now func() time.Time
}

func New(l Ledger, cfg Config, now func() time.Time) *Tracker {
	if now == nil {
		now = time.Now
	}
	return &Tracker{l: l, cfg: cfg.withDefaults(), now: now}
}

const exhaustedKey = "budget.exhausted_until"

// Period は、いまの予算期間（開始と、次の期間の開始）。
func (t *Tracker) Period(now time.Time) (start, next time.Time) {
	n := now.In(t.cfg.Location)
	start = time.Date(n.Year(), n.Month(), t.cfg.AnchorDay, 0, 0, 0, 0, t.cfg.Location)
	if n.Before(start) {
		start = start.AddDate(0, -1, 0)
	}
	return start, start.AddDate(0, 1, 0)
}

type State struct {
	Tier       Tier
	Spent      int64
	Limit      int64
	Exhausted  bool // APIクレジットが尽きて寝ている
	PeriodNext time.Time
}

// State は、いまの使用額と段階。DBが読めないときは**安全側（停止）**。
func (t *Tracker) State(ctx context.Context) State {
	now := t.now()
	start, next := t.Period(now)
	st := State{Limit: t.cfg.LimitMicro, PeriodNext: next}
	if raw, ok, err := t.l.GetSetting(ctx, exhaustedKey); err == nil && ok {
		var until time.Time
		if json.Unmarshal(raw, &until) == nil && now.Before(until) {
			st.Exhausted, st.Tier = true, Stopped
		}
	}
	spent, err := t.l.SpentMicroSince(ctx, start)
	if err != nil {
		st.Tier = Stopped
		return st
	}
	st.Spent = spent
	if st.Exhausted {
		return st
	}
	switch pct := spent * 100 / t.cfg.LimitMicro; {
	case pct >= 100:
		st.Tier = Stopped
	case pct >= 90:
		st.Tier = NoOpus
	case pct >= 70:
		st.Tier = Reduced
	}
	return st
}

// AllowAPI は、Claude APIを呼んでよいか（停止の段階でなければ）。
func (t *Tracker) AllowAPI(ctx context.Context) bool { return t.State(ctx).Tier != Stopped }

// AllowOpus は、Opusを使ってよいか。
func (t *Tracker) AllowOpus(ctx context.Context) bool { return t.State(ctx).Tier < NoOpus }

// SpontaneousFactor は自発投稿の頻度の倍率（通常1.0・節約0.5・それ以降0）。
func (t *Tracker) SpontaneousFactor(ctx context.Context) float64 {
	switch t.State(ctx).Tier {
	case Normal:
		return 1
	case Reduced:
		return 0.5
	}
	return 0
}

// Record は呼び出しの使用量と金額を記録し、金額（マイクロドル）を返す。
func (t *Tracker) Record(ctx context.Context, model, purpose string, u llm.Usage, channelID, userID *int64) (int64, error) {
	cost := Cost(model, u)
	err := t.l.RecordUsage(ctx, store.UsageRecord{Model: model, Purpose: purpose, Input: u.InputTokens, Output: u.OutputTokens,
		CacheRead: u.CacheReadTokens, CacheWrite: u.CacheWriteTokens, CostMicroUSD: cost, ChannelID: channelID, UserID: userID})
	return cost, err
}

// RecordBatch はBatch APIの使用量を記録する（半額。切り上げ）。
func (t *Tracker) RecordBatch(ctx context.Context, model, purpose string, u llm.Usage) (int64, error) {
	cost := (Cost(model, u) + 1) / 2
	err := t.l.RecordUsage(ctx, store.UsageRecord{Model: model, Purpose: purpose, Input: u.InputTokens, Output: u.OutputTokens,
		CacheRead: u.CacheReadTokens, CacheWrite: u.CacheWriteTokens, CostMicroUSD: cost})
	return cost, err
}

// EstimateBatch はBatch APIの見積もり（半額。切り上げ）。入力・出力のトークン数から。
func EstimateBatch(model string, inTokens, outTokens int64) int64 {
	return (Cost(model, llm.Usage{InputTokens: inTokens, OutputTokens: outTokens}) + 1) / 2
}

// Remaining は、予算の70%（節約の段階に入る前）までの残り。0以下なら、バッチなどの任意の仕事はしない。
func (t *Tracker) Remaining(ctx context.Context) int64 {
	st := t.State(ctx)
	if st.Tier != Normal {
		return 0
	}
	return st.Limit*70/100 - st.Spent
}

// PreviousPeriod は、ひとつ前の予算期間の使用額と、期間の始まり（月報用）。
func (t *Tracker) PreviousPeriod(ctx context.Context) (spent int64, start time.Time, err error) {
	cur, _ := t.Period(t.now())
	prev := cur.AddDate(0, -1, 0)
	a, err := t.l.SpentMicroSince(ctx, prev)
	if err != nil {
		return 0, prev, err
	}
	b, err := t.l.SpentMicroSince(ctx, cur)
	if err != nil {
		return 0, prev, err
	}
	return a - b, prev, nil
}

// Limit は月の上限（マイクロドル）。
func (t *Tracker) Limit() int64 { return t.cfg.LimitMicro }

// AllowUser は、一人あたりの1日の上限（連打対策）。kind は "reply" か "opus"。数えてから判定する（上限を超えた回は拒否）。
func (t *Tracker) AllowUser(ctx context.Context, userID int64, kind string) (bool, error) {
	limit := t.cfg.ReplyPerUserDay
	if kind == "opus" {
		limit = t.cfg.OpusPerUserDay
	}
	n, err := t.l.IncDaily(ctx, t.now().In(t.cfg.Location), userID, kind)
	if err != nil {
		return false, err // 数えられないときは断る（安全側）
	}
	return n <= limit, nil
}

// MarkCreditExhausted は、APIクレジットが尽きたとき、次の期間の開始まで寝ると記録する（リトライしない・追加課金しない）。
func (t *Tracker) MarkCreditExhausted(ctx context.Context) error {
	_, next := t.Period(t.now())
	return t.l.SetSetting(ctx, exhaustedKey, next)
}
