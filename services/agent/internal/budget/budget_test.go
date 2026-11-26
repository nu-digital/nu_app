package budget

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nu-digital/nu_app/services/agent/internal/llm"
	"github.com/nu-digital/nu_app/services/agent/internal/store"
)

type ledger struct {
	usage    []store.UsageRecord
	times    []time.Time
	settings map[string]json.RawMessage
	daily    map[string]int
	spentErr error
	incErr   error
}

func newLedger() *ledger {
	return &ledger{settings: map[string]json.RawMessage{}, daily: map[string]int{}}
}
func (l *ledger) RecordUsage(_ context.Context, u store.UsageRecord) error {
	l.usage = append(l.usage, u)
	l.times = append(l.times, clock)
	return nil
}
func (l *ledger) SpentMicroSince(_ context.Context, from time.Time) (int64, error) {
	if l.spentErr != nil {
		return 0, l.spentErr
	}
	var n int64
	for i, u := range l.usage {
		if !l.times[i].Before(from) {
			n += u.CostMicroUSD
		}
	}
	return n, nil
}
func (l *ledger) IncDaily(_ context.Context, day time.Time, user int64, kind string) (int, error) {
	if l.incErr != nil {
		return 0, l.incErr
	}
	k := day.Format("2006-01-02") + "/" + kind + "/" + string(rune(user))
	l.daily[k]++
	return l.daily[k], nil
}
func (l *ledger) GetSetting(_ context.Context, k string) (json.RawMessage, bool, error) {
	v, ok := l.settings[k]
	return v, ok, nil
}
func (l *ledger) SetSetting(_ context.Context, k string, v any) error {
	b, _ := json.Marshal(v)
	l.settings[k] = b
	return nil
}

var clock = time.Date(2026, 11, 15, 12, 0, 0, 0, time.FixedZone("JST", 9*3600))

func tracker(l *ledger, cfg Config) *Tracker { return New(l, cfg, func() time.Time { return clock }) }

func TestCostPerModelAndCache(t *testing.T) {
	// Sonnet: 入力$2・出力$10 /MTok。1,000,000入力+100,000出力 = $2 + $1 = $3
	if c := Cost(llm.Sonnet, llm.Usage{InputTokens: 1_000_000, OutputTokens: 100_000}); c != 3_000_000 {
		t.Errorf("Sonnet: %d", c)
	}
	// キャッシュ: 読み取りは入力の0.1倍・書き込みは1.25倍。Opus: 1Mの読み取り=$0.40、1Mの書き込み=$5.00
	if c := Cost(llm.Opus, llm.Usage{CacheReadTokens: 1_000_000}); c != 400_000 {
		t.Errorf("Opus cache read: %d", c)
	}
	if c := Cost(llm.Opus, llm.Usage{CacheWriteTokens: 1_000_000}); c != 5_000_000 {
		t.Errorf("Opus cache write: %d", c)
	}
	// Haiku: $0.10/$0.50。1M入力+1M出力=$0.60
	if c := Cost(llm.Haiku, llm.Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}); c != 600_000 {
		t.Errorf("Haiku: %d", c)
	}
	// 少なく数えない（切り上げ）。1トークンでも0にならない
	if c := Cost(llm.Haiku, llm.Usage{CacheReadTokens: 1}); c < 1 {
		t.Errorf("切り上げ: %d", c)
	}
	// 未知のモデルは最も高い単価（Opus）で数える
	if Cost("claude-unknown", llm.Usage{InputTokens: 1_000_000}) != Cost(llm.Opus, llm.Usage{InputTokens: 1_000_000}) {
		t.Error("未知のモデルは安全側")
	}
	// Sonnetの標準的な1回（入力3000・出力300）は約$0.009
	if c := Cost(llm.Sonnet, llm.Usage{InputTokens: 3000, OutputTokens: 300}); c != 9000 {
		t.Errorf("標準的な返答: %d", c)
	}
}

func TestTiersAtThresholds(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		spent int64
		tier  Tier
		opus  bool
		api   bool
		spon  float64
	}{
		{0, Normal, true, true, 1},
		{41_999_999, Normal, true, true, 1},
		{42_000_000, Reduced, true, true, 0.5}, // 70%
		{53_999_999, Reduced, true, true, 0.5},
		{54_000_000, NoOpus, false, true, 0}, // 90%
		{59_999_999, NoOpus, false, true, 0},
		{60_000_000, Stopped, false, false, 0}, // 100%
		{99_000_000, Stopped, false, false, 0},
	} {
		l := newLedger()
		l.usage = []store.UsageRecord{{CostMicroUSD: tc.spent}}
		l.times = []time.Time{clock}
		tr := tracker(l, Config{})
		st := tr.State(ctx)
		if st.Tier != tc.tier || tr.AllowOpus(ctx) != tc.opus || tr.AllowAPI(ctx) != tc.api || tr.SpontaneousFactor(ctx) != tc.spon {
			t.Errorf("$%.2f: tier=%v opus=%v api=%v spon=%v", float64(tc.spent)/1e6, st.Tier, tr.AllowOpus(ctx), tr.AllowAPI(ctx), tr.SpontaneousFactor(ctx))
		}
	}
}

func TestPeriodFollowsTheAnchorDay(t *testing.T) {
	l := newLedger()
	tr := tracker(l, Config{AnchorDay: 10})
	s, n := tr.Period(time.Date(2026, 11, 15, 0, 0, 0, 0, tr.cfg.Location))
	if s.Day() != 10 || s.Month() != 11 || n.Month() != 12 || n.Day() != 10 {
		t.Errorf("11/15 → %v〜%v", s, n)
	}
	s, n = tr.Period(time.Date(2026, 11, 5, 0, 0, 0, 0, tr.cfg.Location))
	if s.Month() != 10 || s.Day() != 10 || n.Month() != 11 {
		t.Errorf("11/5 → %v〜%v", s, n)
	}
	// 期間をまたぐと、使用額は新しい期間から数え直す
	l.usage, l.times = []store.UsageRecord{{CostMicroUSD: 70_000_000}}, []time.Time{time.Date(2026, 10, 20, 0, 0, 0, 0, tr.cfg.Location)}
	if st := tr.State(context.Background()); st.Tier != Normal || st.Spent != 0 {
		t.Errorf("前の期間の使用額を数えている: %+v", st)
	}
	// 範囲外の付与日は1日にする
	if New(l, Config{AnchorDay: 31}, nil).cfg.AnchorDay != 1 {
		t.Error("付与日の範囲")
	}
}

func TestRecordStoresCostAndNoContent(t *testing.T) {
	l := newLedger()
	tr := tracker(l, Config{})
	ch, us := int64(5), int64(9)
	cost, err := tr.Record(context.Background(), llm.Sonnet, "chat", llm.Usage{InputTokens: 1000, OutputTokens: 100, CacheReadTokens: 500}, &ch, &us)
	if err != nil || cost != Cost(llm.Sonnet, llm.Usage{InputTokens: 1000, OutputTokens: 100, CacheReadTokens: 500}) {
		t.Fatalf("%d %v", cost, err)
	}
	if u := l.usage[0]; u.Model != llm.Sonnet || u.Purpose != "chat" || u.Input != 1000 || u.CacheRead != 500 || *u.UserID != 9 {
		t.Errorf("%+v", u)
	}
}

func TestPerUserDailyLimits(t *testing.T) {
	ctx := context.Background()
	l := newLedger()
	tr := tracker(l, Config{ReplyPerUserDay: 3, OpusPerUserDay: 1})
	for i := 1; i <= 5; i++ {
		ok, err := tr.AllowUser(ctx, 7, "reply")
		if err != nil || ok != (i <= 3) {
			t.Errorf("%d回目: %v %v", i, ok, err)
		}
	}
	if ok, _ := tr.AllowUser(ctx, 8, "reply"); !ok {
		t.Error("別の人は別に数える")
	}
	if ok, _ := tr.AllowUser(ctx, 7, "opus"); !ok {
		t.Error("Opusは別枠")
	}
	if ok, _ := tr.AllowUser(ctx, 7, "opus"); ok {
		t.Error("Opusの上限（1回）を超えた")
	}
	// 翌日は数え直す
	clock2 := clock.AddDate(0, 0, 1)
	tr2 := New(l, Config{ReplyPerUserDay: 3}, func() time.Time { return clock2 })
	if ok, _ := tr2.AllowUser(ctx, 7, "reply"); !ok {
		t.Error("翌日は数え直す")
	}
	// 数えられないときは断る（安全側）
	l.incErr = errors.New("db")
	if ok, err := tr.AllowUser(ctx, 7, "reply"); ok || err == nil {
		t.Error("DBエラーのときは断る")
	}
}

func TestCreditExhaustionSleepsUntilNextPeriodAndFailsSafe(t *testing.T) {
	ctx := context.Background()
	l := newLedger()
	tr := tracker(l, Config{AnchorDay: 1})
	if err := tr.MarkCreditExhausted(ctx); err != nil {
		t.Fatal(err)
	}
	st := tr.State(ctx)
	if !st.Exhausted || st.Tier != Stopped || tr.AllowAPI(ctx) {
		t.Errorf("クレジット切れで止まらない: %+v", st)
	}
	// 次の期間（12/1）になったら再開
	after := time.Date(2026, 12, 1, 0, 0, 1, 0, tr.cfg.Location)
	tr3 := New(l, Config{AnchorDay: 1}, func() time.Time { return after })
	if st := tr3.State(ctx); st.Exhausted || st.Tier != Normal {
		t.Errorf("次の期間で再開しない: %+v", st)
	}
	// 使用額を読めないときは停止（安全側）
	l2 := newLedger()
	l2.spentErr = errors.New("db")
	if tracker(l2, Config{}).AllowAPI(ctx) {
		t.Error("読めないときはAPIを呼ばない")
	}
}
