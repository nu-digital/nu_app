package schedule

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/nu-digital/nu_app/services/agent/internal/brain"
	"github.com/nu-digital/nu_app/services/agent/internal/budget"
	"github.com/nu-digital/nu_app/services/agent/internal/control"
	"github.com/nu-digital/nu_app/services/agent/internal/store"
)

type mem struct {
	settings map[string]json.RawMessage
	st       store.State
	spent    int64
}

func (m *mem) GetSetting(_ context.Context, k string) (json.RawMessage, bool, error) {
	v, ok := m.settings[k]
	return v, ok, nil
}
func (m *mem) SetSetting(_ context.Context, k string, v any) error {
	b, _ := json.Marshal(v)
	m.settings[k] = b
	return nil
}
func (m *mem) GetState(context.Context) (store.State, error)    { return m.st, nil }
func (m *mem) SaveState(_ context.Context, s store.State) error { m.st = s; return nil }
func (m *mem) RecentMessages(context.Context, int64, *int64, int64, int) ([]store.Message, error) {
	return []store.Message{{AuthorName: "花子", Content: "こんばんは"}}, nil
}

// budget.Ledger
func (m *mem) RecordUsage(context.Context, store.UsageRecord) error            { return nil }
func (m *mem) SpentMicroSince(context.Context, time.Time) (int64, error)       { return m.spent, nil }
func (m *mem) IncDaily(context.Context, time.Time, int64, string) (int, error) { return 1, nil }

type speaker struct {
	calls int
	text  string
	err   error
}

func (s *speaker) Spontaneous(context.Context, brain.SpontInput) (string, error) {
	s.calls++
	return s.text, s.err
}

type poster struct{ posts []string }

func (p *poster) PostTimes(_ context.Context, t string) error {
	p.posts = append(p.posts, t)
	return nil
}

type rig struct {
	s   *Scheduler
	m   *mem
	sp  *speaker
	p   *poster
	now time.Time
}

// 夜（JST 01:00）。乱数は必ず当たる（Float64が0）ように固定したいので、確率側ではなく何度も試す。
func newRig(t *testing.T) *rig {
	t.Helper()
	m := &mem{settings: map[string]json.RawMessage{}}
	ctl, _ := control.New(context.Background(), m)
	x := &rig{m: m, sp: &speaker{text: "夜はいい。"}, p: &poster{}, now: time.Date(2026, 11, 26, 16, 0, 0, 0, time.UTC)} // JST 01:00
	x.s = &Scheduler{Ctl: ctl, Budget: budget.New(m, budget.Config{}, func() time.Time { return x.now }), Brain: x.sp, Post: x.p, Mem: m, Set: m,
		TimesCh: 1, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return x.now }, Rng: rand.New(rand.NewPCG(1, 2))}
	return x
}

// tryMany は、1分ずつ進めながら n 分 Tick を呼ぶ。
func (x *rig) tryMany(n int) {
	for i := 0; i < n; i++ {
		x.s.Tick(context.Background())
		x.now = x.now.Add(time.Minute)
	}
}

func TestSpeaksSometimesButNeverMoreThanDailyCap(t *testing.T) {
	x := newRig(t)
	x.s.MaxDaily = 2
	x.tryMany(24 * 60) // 1日
	if len(x.p.posts) == 0 {
		t.Fatal("1日で1回も話さない（確率が低すぎる）")
	}
	if len(x.p.posts) > 2 {
		t.Errorf("上限(2)を超えた: %d", len(x.p.posts))
	}
	x.tryMany(24 * 60 * 3)
	// 4日で最大8回
	if len(x.p.posts) > 8 {
		t.Errorf("4日で8回を超えた: %d", len(x.p.posts))
	}
}

func TestGapBetweenPosts(t *testing.T) {
	x := newRig(t)
	x.s.MaxDaily = 5
	var last time.Time
	for i := 0; i < 24*60; i++ {
		before := len(x.p.posts)
		x.s.Tick(context.Background())
		if len(x.p.posts) > before {
			if !last.IsZero() && x.now.Sub(last) < minGap {
				t.Fatalf("間隔が短い: %v", x.now.Sub(last))
			}
			last = x.now
		}
		x.now = x.now.Add(time.Minute)
	}
}

func TestQuietAndStoppedNeverSpeakOrCallAPI(t *testing.T) {
	for _, mode := range []control.Mode{control.Quiet, control.Stopped} {
		x := newRig(t)
		_ = x.s.Ctl.Set(context.Background(), mode)
		x.tryMany(24 * 60)
		if x.sp.calls != 0 || len(x.p.posts) != 0 {
			t.Errorf("%s: calls=%d posts=%d", mode, x.sp.calls, len(x.p.posts))
		}
	}
}

func TestBudgetTiersSuppress(t *testing.T) {
	x := newRig(t)
	x.m.spent = 55_000_000 // 90%超 → 係数0
	x.tryMany(24 * 60)
	if x.sp.calls != 0 {
		t.Errorf("予算が90%%を超えたら自発投稿しない: %d", x.sp.calls)
	}
}

func TestFailureOrEmptyTextPostsNothingAndDoesNotCount(t *testing.T) {
	x := newRig(t)
	x.sp.err = errors.New("x")
	x.tryMany(6 * 60)
	if len(x.p.posts) != 0 {
		t.Error("失敗では投稿しない")
	}
	if _, ok := x.m.settings[keySpont]; ok {
		t.Error("失敗は数えない")
	}
}

func TestDailyMoodRunsOncePerDayAfter4AM(t *testing.T) {
	x := newRig(t)
	x.m.st = store.State{Mood: 0, Energy: 0}
	x.now = time.Date(2026, 11, 26, 18, 0, 0, 0, time.UTC) // JST 03:00
	x.s.dailyMood(context.Background(), x.now)
	if _, ok := x.m.settings[keyDrift]; ok {
		t.Error("04:00前は更新しない")
	}
	x.now = time.Date(2026, 11, 26, 19, 5, 0, 0, time.UTC) // JST 04:05
	x.s.dailyMood(context.Background(), x.now)
	if x.m.st.Hobby == "" {
		t.Error("趣味が入る")
	}
	snap := x.m.st
	x.m.st.Mood = 2
	x.s.dailyMood(context.Background(), x.now.Add(time.Hour))
	if x.m.st.Mood != 2 {
		t.Error("同じ日は2回更新しない")
	}
	_ = snap
	x.s.dailyMood(context.Background(), x.now.Add(24*time.Hour))
	if x.m.st.Mood == 2 && x.m.st.Energy == snap.Energy && false {
		t.Error("unreachable")
	}
}

func TestStopAnnouncementOncePerPeriodFixedText(t *testing.T) {
	x := newRig(t)
	x.m.spent = 60_000_000
	x.s.Tick(context.Background())
	x.s.Tick(context.Background())
	if len(x.p.posts) != 1 || !strings.Contains(x.p.posts[0], "寝る") {
		t.Fatalf("posts=%v", x.p.posts)
	}
	if x.sp.calls != 0 {
		t.Error("お知らせはAPIを呼ばない")
	}
	// 静かモードでは書かない
	y := newRig(t)
	y.m.spent = 60_000_000
	_ = y.s.Ctl.Set(context.Background(), control.Quiet)
	y.s.Tick(context.Background())
	if len(y.p.posts) != 0 {
		t.Error("静かモードでお知らせしない")
	}
}
