package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/nu-digital/nu_app/services/agent/internal/control"
	"github.com/nu-digital/nu_app/services/agent/internal/store"
)

// ---- 偽物 ----

type fakeStore struct {
	mu       sync.Mutex
	msgs     map[int64]store.Message
	progress map[int64]store.Progress
	failID   int64
}

func newFakeStore() *fakeStore {
	return &fakeStore{msgs: map[int64]store.Message{}, progress: map[int64]store.Progress{}}
}
func (f *fakeStore) UpsertMessage(_ context.Context, m store.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs[m.ID] = m
	return nil
}
func (f *fakeStore) InsertImported(_ context.Context, m store.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m.ID == f.failID {
		return errors.New("db")
	}
	if _, ok := f.msgs[m.ID]; !ok {
		f.msgs[m.ID] = m
	}
	return nil
}
func (f *fakeStore) UpdateMessage(_ context.Context, id int64, content string, att []store.Attachment, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m, ok := f.msgs[id]; ok {
		m.Content, m.Attachments, m.EditedAt = content, att, &at
		f.msgs[id] = m
	}
	return nil
}
func (f *fakeStore) DeleteMessages(_ context.Context, ids ...int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for _, id := range ids {
		if _, ok := f.msgs[id]; ok {
			delete(f.msgs, id)
			n++
		}
	}
	return n, nil
}
func (f *fakeStore) GetProgress(_ context.Context, ch int64) (store.Progress, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.progress[ch]
	return p, ok, nil
}
func (f *fakeStore) SaveProgress(_ context.Context, p store.Progress) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.progress[p.ChannelID] = p
	return nil
}

type fakeHistory struct {
	channels []Channel
	msgs     map[int64][]Msg // チャンネルごと・新しい順
	failCh   int64
	calls    int
}

func (h *fakeHistory) Channels(context.Context, int64) ([]Channel, error) { return h.channels, nil }
func (h *fakeHistory) MessagesBefore(_ context.Context, ch Channel, before int64, limit int) ([]Msg, error) {
	h.calls++
	if ch.ID == h.failCh {
		return nil, errors.New("403")
	}
	var out []Msg
	for _, m := range h.msgs[ch.ID] {
		if before == 0 || m.ID < before {
			out = append(out, m)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

func history(chID int64, n int) []Msg { // ID chID*1000+n .. chID*1000+1（新しい順。チャンネルごとにIDを分ける）
	var out []Msg
	for i := n; i >= 1; i-- {
		out = append(out, Msg{ID: chID*1000 + int64(i), ChannelID: chID, GuildID: 1, AuthorID: 9, Content: "本文", CreatedAt: time.Unix(int64(i), 0)})
	}
	return out
}

func quiet() (*slog.Logger, *bytes.Buffer) {
	var b bytes.Buffer
	return slog.New(slog.NewJSONHandler(&b, nil)), &b
}

// ---- 記録 ----

func TestRecorderCreateUpdateDelete(t *testing.T) {
	st := newFakeStore()
	log, buf := quiet()
	r := NewRecorder(st, log)
	ctx := context.Background()
	r.OnCreate(ctx, Msg{ID: 5, ChannelID: 2, GuildID: 1, AuthorID: 9, IsBot: true, Content: "秘密っぽい本文", CreatedAt: time.Now(), Attachments: []Attachment{{ID: "a", Filename: "x.png", ContentType: "image/png", Size: 3}}})
	if m := st.msgs[5]; m.Kind != "guild" || !m.IsBot || len(m.Attachments) != 1 {
		t.Errorf("作成: %+v", m)
	}
	r.OnCreate(ctx, Msg{ID: 6, ChannelID: 3, GuildID: 0, AuthorID: 9, Content: "DM", CreatedAt: time.Now()})
	if st.msgs[6].Kind != "dm" {
		t.Error("GuildID=0はDM")
	}
	at := time.Now()
	r.OnUpdate(ctx, Msg{ID: 5, Content: "編集後", EditedAt: &at, CreatedAt: time.Now()})
	if st.msgs[5].Content != "編集後" || st.msgs[5].EditedAt == nil {
		t.Errorf("編集: %+v", st.msgs[5])
	}
	r.OnUpdate(ctx, Msg{ID: 5, Content: "", CreatedAt: time.Now()}) // 本文なしの更新（埋め込み展開等）は無視
	if st.msgs[5].Content != "編集後" {
		t.Error("本文なしの更新で消えた")
	}
	r.OnDelete(ctx, 5, 6, 999)
	if len(st.msgs) != 0 {
		t.Errorf("削除されていない: %v", st.msgs)
	}
	for _, s := range []string{"秘密っぽい本文", "編集後", "DM"} {
		if strings.Contains(buf.String(), s) {
			t.Errorf("ログに本文が出ている: %s", s)
		}
	}
}

// ---- 過去ログの取り込み ----

func TestImporterImportsAllPagesAndIsIdempotent(t *testing.T) {
	st := newFakeStore()
	h := &fakeHistory{channels: []Channel{{ID: 10, Name: "a"}, {ID: 20, Name: "b"}}, msgs: map[int64][]Msg{10: history(10, 250), 20: history(20, 3)}}
	log, _ := quiet()
	im := &Importer{Hist: h, St: st, Log: log, PageSize: 100}
	if err := im.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if len(st.msgs) != 253 {
		t.Errorf("取り込み件数: %d", len(st.msgs))
	}
	if p := st.progress[10]; !p.Done || p.Imported != 250 || *p.OldestMessageID != 10001 {
		t.Errorf("進捗: %+v", p)
	}
	calls := h.calls
	if err := im.Run(context.Background(), 1); err != nil || h.calls != calls {
		t.Errorf("完了済みを再取得した: calls %d→%d %v", calls, h.calls, err)
	}
}

func TestImporterDoesNotOverwriteLiveMessages(t *testing.T) {
	st := newFakeStore()
	st.msgs[10003] = store.Message{ID: 10003, Content: "リアルタイムの最新"}
	h := &fakeHistory{channels: []Channel{{ID: 10}}, msgs: map[int64][]Msg{10: history(10, 5)}}
	log, _ := quiet()
	if err := (&Importer{Hist: h, St: st, Log: log}).Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if st.msgs[10003].Content != "リアルタイムの最新" {
		t.Error("リアルタイムの記録を上書きした")
	}
}

func TestImporterResumesAfterInterruption(t *testing.T) {
	st := newFakeStore()
	h := &fakeHistory{channels: []Channel{{ID: 10}}, msgs: map[int64][]Msg{10: history(10, 250)}}
	log, _ := quiet()
	pages := 0
	im := &Importer{Hist: h, St: st, Log: log, PageSize: 100, Allow: func() bool { pages++; return pages <= 1 }} // 1ページだけ進んで中断
	if err := im.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if p := st.progress[10]; p.Done || p.Imported != 100 || *p.OldestMessageID != 10151 {
		t.Fatalf("中断時の進捗: %+v", p)
	}
	// 再起動: 続きから（最初から取り直さない）
	h.calls = 0
	im2 := &Importer{Hist: h, St: st, Log: log, PageSize: 100}
	if err := im2.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if len(st.msgs) != 250 || h.calls != 2 {
		t.Errorf("再開: 件数%d・呼び出し%d（残り150件=2ページ）", len(st.msgs), h.calls)
	}
}

func TestImporterContinuesPastAFailingChannel(t *testing.T) {
	st := newFakeStore()
	h := &fakeHistory{channels: []Channel{{ID: 10}, {ID: 20}}, msgs: map[int64][]Msg{10: history(10, 3), 20: history(20, 4)}, failCh: 10}
	log, _ := quiet()
	if err := (&Importer{Hist: h, St: st, Log: log}).Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if len(st.msgs) != 4 || st.progress[10].Done || !st.progress[20].Done {
		t.Errorf("失敗したチャンネルを飛ばして続けるはず: %d %+v", len(st.msgs), st.progress)
	}
}

func TestImporterStopsOnContextCancel(t *testing.T) {
	st := newFakeStore()
	h := &fakeHistory{channels: []Channel{{ID: 10}}, msgs: map[int64][]Msg{10: history(10, 500)}}
	log, _ := quiet()
	ctx, cancel := context.WithCancel(context.Background())
	im := &Importer{Hist: h, St: st, Log: log, PageSize: 100, PageDelay: time.Hour}
	done := make(chan error, 1)
	go func() { done <- im.Run(ctx, 1) }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ctxを取り消しても止まらない")
	}
	ids := make([]int, 0)
	for id := range st.msgs {
		ids = append(ids, int(id))
	}
	sort.Ints(ids)
	if len(ids) != 100 {
		t.Errorf("1ページだけ取り込んで止まるはず: %d", len(ids))
	}
}

// ---- /nul コマンド ----

type json_ = []byte

type fakeSettings struct {
	m       map[string]json_
	failSet bool
}

func (f *fakeSettings) GetSetting(_ context.Context, k string) (json.RawMessage, bool, error) {
	v, ok := f.m[k]
	return v, ok, nil
}
func (f *fakeSettings) SetSetting(_ context.Context, k string, v any) error {
	if f.failSet {
		return errors.New("db")
	}
	b, _ := json.Marshal(v)
	f.m[k] = b
	return nil
}

func TestCommanderPermissionsAndModes(t *testing.T) {
	ctx := context.Background()
	set := &fakeSettings{m: map[string]json_{}}
	ctl, _ := control.New(ctx, set)
	c := &Commander{Ctl: ctl, AdminRole: "333", Status: func(context.Context) (string, error) { return FormatStatus(ctl.Mode(), 10, 3, 1, 7), nil }}
	if r := c.Handle(ctx, "stop", []string{"1"}); !strings.Contains(r, "Adminロール") || ctl.Mode() != control.Running {
		t.Errorf("Adminでない人が停止できた: %s %s", r, ctl.Mode())
	}
	if r := c.Handle(ctx, "stop", []string{"333"}); !strings.Contains(r, "全停止") || ctl.Mode() != control.Stopped {
		t.Errorf("stop: %s", r)
	}
	if c.Handle(ctx, "quiet", []string{"333"}); ctl.Mode() != control.Quiet {
		t.Error("quiet")
	}
	if c.Handle(ctx, "resume", []string{"333"}); ctl.Mode() != control.Running {
		t.Error("resume")
	}
	if r := c.Handle(ctx, "status", []string{"333"}); !strings.Contains(r, "10 件") || !strings.Contains(r, "1/3") {
		t.Errorf("status: %s", r)
	}
	if r := c.Handle(ctx, "unknown", []string{"333"}); !strings.Contains(r, "不明") {
		t.Error("不明なコマンド")
	}
	// 保存できないときは変更しない
	set.failSet = true
	if r := c.Handle(ctx, "stop", []string{"333"}); !strings.Contains(r, "変更していません") || ctl.Mode() != control.Running {
		t.Errorf("保存失敗: %s %s", r, ctl.Mode())
	}
	// AdminロールIDが未設定なら、誰も使えない
	c2 := &Commander{Ctl: ctl}
	if r := c2.Handle(ctx, "stop", []string{"333", ""}); !strings.Contains(r, "Adminロール") {
		t.Error("ロール未設定で使えた")
	}
}

// ---- discordgoのメッセージの変換 ----

func TestConvertMessage(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	edited := ts.Add(time.Minute)
	m := &discordgo.Message{ID: "100", ChannelID: "55", GuildID: "1", Content: "こんにちは", Timestamp: ts, EditedTimestamp: &edited,
		Author:      &discordgo.User{ID: "9", Username: "user9", GlobalName: "表示名", Bot: false},
		Member:      &discordgo.Member{Nick: "ニックネーム"},
		Attachments: []*discordgo.MessageAttachment{{ID: "a1", Filename: "x.png", ContentType: "image/png", Size: 42, URL: "https://cdn.example/secret?sig=1"}}}
	got := ConvertMessage(m, "", "")
	if got.ID != 100 || got.ChannelID != 55 || got.GuildID != 1 || got.AuthorID != 9 || got.AuthorName != "ニックネーム" || got.ThreadID != nil || got.EditedAt == nil || len(got.Attachments) != 1 {
		t.Errorf("%+v", got)
	}
	// スレッド: ChannelIDは親、ThreadIDがスレッド
	th := ConvertMessage(m, "7", "55")
	if th.ChannelID != 7 || th.ThreadID == nil || *th.ThreadID != 55 {
		t.Errorf("スレッド: %+v", th)
	}
	// DM（GuildIDなし）・名前の優先順位
	dm := &discordgo.Message{ID: "1", ChannelID: "2", Timestamp: ts, Author: &discordgo.User{ID: "3", Username: "u", GlobalName: "G"}}
	if d := ConvertMessage(dm, "", ""); d.GuildID != 0 || d.AuthorName != "G" {
		t.Errorf("DM: %+v", d)
	}
	dm.Author.GlobalName = ""
	if d := ConvertMessage(dm, "", ""); d.AuthorName != "u" {
		t.Errorf("ユーザー名: %+v", d)
	}
	// 添付のURL（署名つきで期限切れになる）は保存用の型に含まれない
	if strings.Contains(strings.ToLower(strings.Join([]string{got.Attachments[0].ID, got.Attachments[0].Filename, got.Attachments[0].ContentType}, "")), "cdn.example") {
		t.Error("URLが含まれる")
	}
}
