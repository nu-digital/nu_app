package responder

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nu-digital/nu_app/services/agent/internal/brain"
	"github.com/nu-digital/nu_app/services/agent/internal/control"
	"github.com/nu-digital/nu_app/services/agent/internal/discord"
	"github.com/nu-digital/nu_app/services/agent/internal/store"
)

const (
	botID   = 900
	timesCh = 100
	otherCh = 200
)

type memSettings struct{ m map[string]json.RawMessage }

func (s *memSettings) GetSetting(_ context.Context, k string) (json.RawMessage, bool, error) {
	v, ok := s.m[k]
	return v, ok, nil
}
func (s *memSettings) SetSetting(_ context.Context, k string, v any) error {
	b, _ := json.Marshal(v)
	s.m[k] = b
	return nil
}

type fakeSender struct {
	mu        sync.Mutex
	replies   []string
	reactions []string
	typing    int
}

func (f *fakeSender) Reply(_ context.Context, _, _ int64, t string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies = append(f.replies, t)
	return nil
}
func (f *fakeSender) React(_ context.Context, _, _ int64, e string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reactions = append(f.reactions, e)
	return nil
}
func (f *fakeSender) Typing(context.Context, int64) error { f.typing++; return nil }

type fakeBrain struct {
	err   error
	calls []brain.Input
}

func (b *fakeBrain) Reply(_ context.Context, in brain.Input) (brain.Output, error) {
	b.calls = append(b.calls, in)
	if b.err != nil {
		return brain.Output{}, b.err
	}
	return brain.Output{Text: "なによ"}, nil
}

type fakeMem struct {
	st     store.State
	bumped []int64
}

func (m *fakeMem) GetState(context.Context) (store.State, error)    { return m.st, nil }
func (m *fakeMem) SaveState(_ context.Context, s store.State) error { m.st = s; return nil }
func (m *fakeMem) BumpCloseness(_ context.Context, u int64) error {
	m.bumped = append(m.bumped, u)
	return nil
}

type fakeHist struct{ rows []store.Message }

func (h *fakeHist) RecentMessages(context.Context, int64, *int64, int64, int) ([]store.Message, error) {
	return h.rows, nil
}

type fakeImg struct{ err error }

func (i fakeImg) Fetch(context.Context, discord.Attachment) ([]byte, error) {
	if i.err != nil {
		return nil, i.err
	}
	return []byte{1}, nil
}

type rig struct {
	r *Responder
	s *fakeSender
	b *fakeBrain
	m *fakeMem
	c *control.Controller
}

func newRig(t *testing.T) *rig {
	t.Helper()
	c, err := control.New(context.Background(), &memSettings{m: map[string]json.RawMessage{}})
	if err != nil {
		t.Fatal(err)
	}
	s, b, m := &fakeSender{}, &fakeBrain{}, &fakeMem{}
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	r := &Responder{Ctl: c, Brain: b, Hist: &fakeHist{}, Mem: m, Send: s, Img: fakeImg{}, BotID: func() int64 { return botID }, TimesChannel: timesCh,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { now = now.Add(time.Minute); return now }}
	return &rig{r: r, s: s, b: b, m: m, c: c}
}

func msg(ch int64, guild int64, content string) discord.Msg {
	return discord.Msg{ID: 1, ChannelID: ch, GuildID: guild, AuthorID: 5, AuthorName: "たろう", Content: content}
}

func TestDecide(t *testing.T) {
	x := newRig(t)
	thread := int64(7)
	tm := msg(timesCh, 1, "やあ")
	tmThread := tm
	tmThread.ThreadID = &thread
	selfMsg := msg(timesCh, 1, "x")
	selfMsg.AuthorID = botID
	botMsg := msg(timesCh, 1, "x")
	botMsg.IsBot = true
	cases := []struct {
		name string
		in   discord.Incoming
		want bool
	}{
		{"timesは自由", discord.Incoming{Msg: tm}, true},
		{"timesのスレッドはメンションが要る", discord.Incoming{Msg: tmThread}, false},
		{"ほかのチャンネルは呼ばれないと応えない", discord.Incoming{Msg: msg(otherCh, 1, "やあ")}, false},
		{"メンションで応える", discord.Incoming{Msg: msg(otherCh, 1, "やあ"), MentionsBot: true}, true},
		{"返信で応える", discord.Incoming{Msg: msg(otherCh, 1, "やあ"), RepliesToBot: true}, true},
		{"DMは応える", discord.Incoming{Msg: msg(0, 0, "やあ")}, true},
		{"自分には応えない", discord.Incoming{Msg: selfMsg, MentionsBot: true}, false},
		{"Botには応えない", discord.Incoming{Msg: botMsg, MentionsBot: true}, false},
		{"Bot発言フラグ", discord.Incoming{Msg: tm, IsBotAuthored: true}, false},
	}
	for _, c := range cases {
		if got := x.r.Decide(c.in); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
	if err := x.c.Set(context.Background(), control.Stopped); err != nil {
		t.Fatal(err)
	}
	if x.r.Decide(discord.Incoming{Msg: tm}) || x.r.Decide(discord.Incoming{Msg: msg(0, 0, "x")}) {
		t.Error("停止中は応えない")
	}
}

func TestHandleRepliesAndRecords(t *testing.T) {
	x := newRig(t)
	x.r.Handle(context.Background(), discord.Incoming{Msg: msg(otherCh, 1, "<@900> ありがとう"), MentionsBot: true})
	if len(x.s.replies) != 1 || x.s.replies[0] != "なによ" {
		t.Fatalf("replies=%v", x.s.replies)
	}
	if strings.Contains(x.b.calls[0].Text, "900") || x.b.calls[0].Text != "ありがとう" {
		t.Errorf("メンションを外す: %q", x.b.calls[0].Text)
	}
	if len(x.m.bumped) != 1 || x.m.bumped[0] != 5 {
		t.Errorf("親密度: %v", x.m.bumped)
	}
	if x.m.st.Mood != 1 {
		t.Errorf("褒められて気分が上がる: %+v", x.m.st)
	}
}

func TestHandleBrainErrorsBecomeReactions(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{{brain.ErrAsleep, emojiAsleep}, {brain.ErrUserLimit, emojiLimit}, {brain.ErrBusy, emojiBusy}, {errors.New("boom"), emojiNoSpeak}} {
		x := newRig(t)
		x.b.err = c.err
		x.r.Handle(context.Background(), discord.Incoming{Msg: msg(timesCh, 1, "やあ")})
		if len(x.s.replies) != 0 || len(x.s.reactions) != 1 || x.s.reactions[0] != c.want {
			t.Errorf("%v: replies=%v reactions=%v", c.err, x.s.replies, x.s.reactions)
		}
		if len(x.m.bumped) != 0 {
			t.Errorf("失敗では親密度を上げない")
		}
	}
}

func TestHandleStoppedDoesNothing(t *testing.T) {
	x := newRig(t)
	_ = x.c.Set(context.Background(), control.Stopped)
	x.r.Handle(context.Background(), discord.Incoming{Msg: msg(timesCh, 1, "やあ")})
	if len(x.b.calls) != 0 || len(x.s.replies)+len(x.s.reactions)+x.s.typing != 0 {
		t.Error("停止中は何もしない（APIも呼ばない）")
	}
}

// 生成中に停止されたら、できた返答は送らない。
type stopDuring struct {
	c *control.Controller
	fakeBrain
}

func (b *stopDuring) Reply(ctx context.Context, in brain.Input) (brain.Output, error) {
	_ = b.c.Set(ctx, control.Stopped)
	return b.fakeBrain.Reply(ctx, in)
}

func TestStopDuringGenerationDropsReply(t *testing.T) {
	x := newRig(t)
	x.r.Brain = &stopDuring{c: x.c}
	x.r.Handle(context.Background(), discord.Incoming{Msg: msg(timesCh, 1, "やあ")})
	if len(x.s.replies) != 0 {
		t.Error("停止後は送らない")
	}
}

func TestEmptyMentionOnlyGetsEyes(t *testing.T) {
	x := newRig(t)
	x.r.Handle(context.Background(), discord.Incoming{Msg: msg(otherCh, 1, "<@!900>"), MentionsBot: true})
	if len(x.b.calls) != 0 || len(x.s.reactions) != 1 || x.s.reactions[0] != emojiSeen {
		t.Errorf("calls=%d reactions=%v", len(x.b.calls), x.s.reactions)
	}
}

func TestCooldownSkipsRapidMessages(t *testing.T) {
	x := newRig(t)
	fixed := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	x.r.Now = func() time.Time { return fixed }
	x.r.Handle(context.Background(), discord.Incoming{Msg: msg(timesCh, 1, "1")})
	x.r.Handle(context.Background(), discord.Incoming{Msg: msg(timesCh, 1, "2")})
	if len(x.b.calls) != 1 {
		t.Errorf("連投は1回だけ: %d", len(x.b.calls))
	}
	x.r.Handle(context.Background(), discord.Incoming{Msg: msg(otherCh, 1, "3"), MentionsBot: true})
	if len(x.b.calls) != 2 {
		t.Error("別チャンネルは別枠")
	}
}

func TestImagesLimitedAndFailuresIgnored(t *testing.T) {
	x := newRig(t)
	m := msg(timesCh, 1, "見て")
	for i := 0; i < 5; i++ {
		m.Attachments = append(m.Attachments, discord.Attachment{ContentType: "image/png"})
	}
	m.Attachments = append(m.Attachments, discord.Attachment{ContentType: "application/pdf"})
	x.r.Handle(context.Background(), discord.Incoming{Msg: m})
	if len(x.b.calls[0].Images) != maxImages {
		t.Errorf("画像は最大%d枚: %d", maxImages, len(x.b.calls[0].Images))
	}
	y := newRig(t)
	y.r.Img = fakeImg{err: errors.New("x")}
	m.Content = ""
	y.r.Handle(context.Background(), discord.Incoming{Msg: m})
	if len(y.s.reactions) != 1 || y.s.reactions[0] != emojiSeen {
		t.Errorf("画像だけで全部失敗したら👀: %v", y.s.reactions)
	}
}

func TestHistoryNamesBotAsNul(t *testing.T) {
	x := newRig(t)
	x.r.Hist = &fakeHist{rows: []store.Message{{AuthorID: botID, IsBot: true, AuthorName: "nul-bot", Content: "a"}, {AuthorID: 6, AuthorName: "花子", Content: "b"}}}
	x.r.Handle(context.Background(), discord.Incoming{Msg: msg(timesCh, 1, "やあ")})
	h := x.b.calls[0].History
	if len(h) != 2 || h[0].AuthorName != "ぬる" || h[1].AuthorName != "花子" {
		t.Errorf("%+v", h)
	}
}

func TestStripMention(t *testing.T) {
	if got := stripMention("<@900> hi <@901>", 900); got != "hi <@901>" {
		t.Errorf("%q", got)
	}
}

type fakeReacts struct {
	settings map[string]json.RawMessage
	used     int
	cap      int
}

func (f *fakeReacts) GetSetting(_ context.Context, k string) (json.RawMessage, bool, error) {
	v, ok := f.settings[k]
	return v, ok, nil
}
func (f *fakeReacts) TakeReaction(_ context.Context, _ time.Time, cap int) (bool, error) {
	f.cap = cap
	if f.used >= cap {
		return false, nil
	}
	f.used++
	return true, nil
}

func reactRig(t *testing.T) (*rig, *fakeReacts) {
	x := newRig(t)
	fr := &fakeReacts{settings: map[string]json.RawMessage{}}
	x.r.Reacts = fr
	x.r.Rng = rand.New(rand.NewPCG(1, 1))
	return x, fr
}

func TestReactionsAreRareRuleBasedAndCapped(t *testing.T) {
	x, fr := reactRig(t)
	for i := 0; i < 400; i++ {
		x.r.Handle(context.Background(), discord.Incoming{Msg: msg(otherCh, 1, "助かった、ありがとう")})
	}
	if len(x.b.calls) != 0 {
		t.Error("リアクションでClaudeを呼ばない")
	}
	if len(x.s.reactions) == 0 || len(x.s.reactions) > 10 || fr.used != len(x.s.reactions) {
		t.Errorf("既定の上限10・確率つき: %d", len(x.s.reactions))
	}
	if x.s.reactions[0] != "🙏" {
		t.Errorf("%v", x.s.reactions[0])
	}
}

func TestReactionsOffByAdminSettingOrQuietOrNoKeyword(t *testing.T) {
	run := func(mod func(*rig, *fakeReacts), content string) int {
		x, fr := reactRig(t)
		mod(x, fr)
		for i := 0; i < 400; i++ {
			x.r.Handle(context.Background(), discord.Incoming{Msg: msg(otherCh, 1, content)})
		}
		return len(x.s.reactions)
	}
	if run(func(_ *rig, f *fakeReacts) { f.settings[keyReactOn] = json.RawMessage("false") }, "ありがとう") != 0 {
		t.Error("オフ設定でも付けた")
	}
	if run(func(x *rig, _ *fakeReacts) { _ = x.c.Set(context.Background(), control.Quiet) }, "ありがとう") != 0 {
		t.Error("静かモードで付けた")
	}
	if run(func(*rig, *fakeReacts) {}, "ふつうの発言") != 0 {
		t.Error("該当なしで付けた")
	}
	if run(func(_ *rig, f *fakeReacts) { f.settings[keyReactCap] = json.RawMessage("0") }, "ありがとう") != 0 {
		t.Error("上限0で付けた")
	}
}

func TestRepliesInThreadsGoToTheThread(t *testing.T) {
	x := newRig(t)
	th := int64(555)
	var gotCh int64
	x.r.Send = chanRecorder{fakeSender: x.s, ch: &gotCh}
	m := msg(otherCh, 1, "<@900> やあ")
	m.ThreadID = &th
	x.r.Handle(context.Background(), discord.Incoming{Msg: m, MentionsBot: true})
	if gotCh != th {
		t.Errorf("スレッドへ送る: %d", gotCh)
	}
}

type chanRecorder struct {
	*fakeSender
	ch *int64
}

func (c chanRecorder) Reply(ctx context.Context, ch, id int64, t string) error {
	*c.ch = ch
	return c.fakeSender.Reply(ctx, ch, id, t)
}

type forgetBrain struct{}

func (forgetBrain) Reply(context.Context, brain.Input) (brain.Output, error) {
	return brain.Output{Text: "消した", Forgot: true}, nil
}

func TestForgetReplyDoesNotRecreateCloseness(t *testing.T) {
	x := newRig(t)
	x.r.Brain = forgetBrain{}
	x.r.Handle(context.Background(), discord.Incoming{Msg: msg(timesCh, 1, "私のことを全部忘れて")})
	if len(x.s.replies) != 1 || len(x.m.bumped) != 0 {
		t.Errorf("replies=%v bumped=%v", x.s.replies, x.m.bumped)
	}
}
