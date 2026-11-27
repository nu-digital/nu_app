//go:build integration

package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nu-digital/nu_app/services/agent/internal/store"
	"github.com/nu-digital/nu_app/services/agent/internal/testutil/pgtest"
)

func newStore(t *testing.T) (*store.Store, *pgxpool.Pool) {
	t.Helper()
	_, dsn := pgtest.New(t)
	cfg, err := store.PoolConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return store.New(pool), pool
}

func msg(id int64, content string) store.Message {
	return store.Message{ID: id, ChannelID: 10, Kind: "guild", AuthorID: 7, AuthorName: "テスト太郎", Content: content, CreatedAt: time.Now()}
}

func TestSettingsRoundTrip(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	if _, ok, err := s.GetSetting(ctx, "control.mode"); ok || err != nil {
		t.Fatalf("初期: %v %v", ok, err)
	}
	if err := s.SetSetting(ctx, "control.mode", "quiet"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(ctx, "control.mode", "stopped"); err != nil { // 上書き
		t.Fatal(err)
	}
	v, ok, _ := s.GetSetting(ctx, "control.mode")
	var m string
	_ = json.Unmarshal(v, &m)
	if !ok || m != "stopped" {
		t.Errorf("%s", v)
	}
	if err := s.SetSetting(ctx, "Bad Key!", 1); err == nil {
		t.Error("不正なキーを受け付けた")
	}
}

func TestMessagesLifecycle(t *testing.T) {
	s, pool := newStore(t)
	ctx := context.Background()
	m := msg(100, "最初の本文")
	m.Attachments = []store.Attachment{{ID: "1", Filename: "a.png", ContentType: "image/png", Size: 10}}
	if err := s.UpsertMessage(ctx, m); err != nil {
		t.Fatal(err)
	}
	// 編集
	if err := s.UpdateMessage(ctx, 100, "編集後の本文", nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	var content string
	var edited *time.Time
	var att string
	_ = pool.QueryRow(ctx, "SELECT content, edited_at, attachments::text FROM messages WHERE message_id=100").Scan(&content, &edited, &att)
	if content != "編集後の本文" || edited == nil || att != "[]" {
		t.Errorf("編集: %q %v %s", content, edited, att)
	}
	// 記録に無いメッセージの編集は何もしない
	if err := s.UpdateMessage(ctx, 999, "x", nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountMessages(ctx, nil); n != 1 {
		t.Errorf("件数: %d", n)
	}
	// 取り込みは、リアルタイムで記録済みのものを上書きしない
	if err := s.InsertImported(ctx, msg(100, "取り込みの本文")); err != nil {
		t.Fatal(err)
	}
	_ = pool.QueryRow(ctx, "SELECT content FROM messages WHERE message_id=100").Scan(&content)
	if content != "編集後の本文" {
		t.Errorf("取り込みが上書きした: %q", content)
	}
	if err := s.InsertImported(ctx, msg(101, "古い本文")); err != nil {
		t.Fatal(err)
	}
	// 削除は物理削除（行そのものが無くなる。本文が残らない）
	n, err := s.DeleteMessages(ctx, 100, 101, 555)
	if err != nil || n != 2 {
		t.Fatalf("削除: %d %v", n, err)
	}
	var rows int
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM messages").Scan(&rows)
	if rows != 0 {
		t.Errorf("残っている: %d", rows)
	}
	if n, err := s.DeleteMessages(ctx); n != 0 || err != nil {
		t.Errorf("空の削除: %d %v", n, err)
	}
}

func TestProgressResumes(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	if _, ok, _ := s.GetProgress(ctx, 5); ok {
		t.Fatal("初期は無い")
	}
	oldest := int64(1234)
	if err := s.SaveProgress(ctx, store.Progress{ChannelID: 5, OldestMessageID: &oldest, Imported: 300}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveProgress(ctx, store.Progress{ChannelID: 5, OldestMessageID: &oldest, Imported: 450, Done: true}); err != nil {
		t.Fatal(err)
	}
	p, ok, err := s.GetProgress(ctx, 5)
	if err != nil || !ok || !p.Done || p.Imported != 450 || p.OldestMessageID == nil || *p.OldestMessageID != 1234 {
		t.Errorf("%+v %v %v", p, ok, err)
	}
	_ = s.SaveProgress(ctx, store.Progress{ChannelID: 6, Imported: 10})
	ch, done, imp, err := s.ImportSummary(ctx)
	if err != nil || ch != 2 || done != 1 || imp != 460 {
		t.Errorf("%d %d %d %v", ch, done, imp, err)
	}
}

func TestUsageAndDailyCounters(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	now := time.Now()
	ch, us := int64(5), int64(9)
	for _, c := range []int64{1500, 2500} {
		if err := s.RecordUsage(ctx, store.UsageRecord{Model: "claude-sonnet-5-5", Purpose: "chat", Input: 10, Output: 5, CostMicroUSD: c, ChannelID: &ch, UserID: &us}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.SpentMicroSince(ctx, now.Add(-time.Hour))
	if err != nil || n != 4000 {
		t.Errorf("使用額: %d %v", n, err)
	}
	if n, _ := s.SpentMicroSince(ctx, now.Add(time.Hour)); n != 0 {
		t.Errorf("未来: %d", n)
	}
	if err := s.RecordUsage(ctx, store.UsageRecord{Model: "m", Purpose: "p", CostMicroUSD: -1}); err == nil {
		t.Error("負の金額を受け付けた")
	}
	// 1日の回数は原子的に増え、日が変われば別に数える。種別（reply/opus）は別
	for want := 1; want <= 3; want++ {
		if got, err := s.IncDaily(ctx, now, 7, "reply"); err != nil || got != want {
			t.Errorf("%d回目: %d %v", want, got, err)
		}
	}
	if got, err := s.IncDaily(ctx, now, 7, "web"); err != nil || got != 1 {
		t.Errorf("web: %d %v", got, err)
	}
	if got, _ := s.IncDaily(ctx, now, 7, "opus"); got != 1 {
		t.Errorf("opus: %d", got)
	}
	if got, _ := s.IncDaily(ctx, now.AddDate(0, 0, 1), 7, "reply"); got != 1 {
		t.Errorf("翌日: %d", got)
	}
	if _, err := s.IncDaily(ctx, now, 7, "bad"); err == nil {
		t.Error("不正な種別を受け付けた")
	}
	// 並行して増やしても数え落とさない
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() { _, _ = s.IncDaily(ctx, now, 99, "reply"); done <- struct{}{} }()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
	if got, _ := s.IncDaily(ctx, now, 99, "reply"); got != 11 {
		t.Errorf("並行: %d", got)
	}
}

func TestStateAndCloseness(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	st, err := s.GetState(ctx)
	if err != nil || st.Mood != 0 || st.Hobby != "" {
		t.Fatalf("初期: %+v %v", st, err)
	}
	if err := s.SaveState(ctx, store.State{Mood: -2, Energy: 1, Hobby: "ネットの歴史"}); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.GetState(ctx); st.Mood != -2 || st.Energy != 1 || st.Hobby != "ネットの歴史" {
		t.Errorf("%+v", st)
	}
	if err := s.SaveState(ctx, store.State{Mood: 3}); err == nil {
		t.Error("範囲外の機嫌を受け付けた")
	}
	if n, err := s.GetCloseness(ctx, 7); n != 0 || err != nil {
		t.Errorf("初期の親密度: %d %v", n, err)
	}
	for i := 0; i < 3; i++ {
		if err := s.BumpCloseness(ctx, 7); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := s.GetCloseness(ctx, 7); n != 3 {
		t.Errorf("親密度: %d", n)
	}
}

func TestRecentMessagesScopesToTheConversation(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	th := int64(500)
	put := func(id, ch int64, thread *int64, kind, content string) {
		m := store.Message{ID: id, ChannelID: ch, ThreadID: thread, Kind: kind, AuthorID: 1, AuthorName: "a", Content: content, CreatedAt: time.Now()}
		if err := s.UpsertMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	for i := int64(1); i <= 5; i++ {
		put(i, 10, nil, "guild", "チャンネル10")
	}
	put(6, 10, &th, "guild", "スレッド500")
	put(7, 20, nil, "guild", "別のチャンネル")
	put(8, 30, nil, "dm", "DMの内容")
	got, err := s.RecentMessages(ctx, 10, nil, 0, 3)
	if err != nil || len(got) != 3 || got[0].ID != 3 || got[2].ID != 5 {
		t.Fatalf("チャンネル（スレッドを除く）の直近3件・古い順: %+v %v", got, err)
	}
	if got, _ := s.RecentMessages(ctx, 10, nil, 4, 10); len(got) != 3 || got[2].ID != 3 {
		t.Errorf("beforeID: %+v", got)
	}
	if got, _ := s.RecentMessages(ctx, 10, &th, 0, 10); len(got) != 1 || got[0].Content != "スレッド500" {
		t.Errorf("スレッド: %+v", got)
	}
	if got, _ := s.RecentMessages(ctx, 30, nil, 0, 10); len(got) != 1 || got[0].Kind != "dm" {
		t.Errorf("DM: %+v", got)
	}
	if got, _ := s.RecentMessages(ctx, 99, nil, 0, 10); len(got) != 0 {
		t.Errorf("該当なし: %+v", got)
	}
}

func TestTakeReactionRespectsDailyCap(t *testing.T) {
	ctx := context.Background()
	s, _ := newStore(t)
	day := time.Date(2026, 11, 27, 3, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		if ok, err := s.TakeReaction(ctx, day, 2); err != nil || !ok {
			t.Fatalf("%d: ok=%v err=%v", i, ok, err)
		}
	}
	if ok, err := s.TakeReaction(ctx, day, 2); err != nil || ok {
		t.Errorf("上限を超えて使えた: ok=%v err=%v", ok, err)
	}
	if ok, _ := s.TakeReaction(ctx, day.AddDate(0, 0, 1), 2); !ok {
		t.Error("翌日は使える")
	}
	if ok, _ := s.TakeReaction(ctx, day.AddDate(0, 0, 2), 0); ok {
		t.Error("上限0は使えない")
	}
}

func TestSearchMessagesNeverReturnsDMsOrBots(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	mk := func(id int64, kind, content string, bot bool) store.Message {
		m := msg(id, content)
		m.Kind, m.IsBot = kind, bot
		return m
	}
	for _, m := range []store.Message{mk(1, "guild", "ハッカソンの日程は50%で決まった", false), mk(2, "dm", "ハッカソンのひみつ", false), mk(3, "guild", "ハッカソンの宣伝", true), mk(4, "guild", "別の話", false)} {
		if err := s.UpsertMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	hits, err := s.SearchMessages(ctx, "ハッカソン", 10)
	if err != nil || len(hits) != 1 || hits[0].MessageID != 1 {
		t.Fatalf("DM・Botを返した: %+v %v", hits, err)
	}
	if h, _ := s.SearchMessages(ctx, "50%", 10); len(h) != 1 {
		t.Error("%は文字として探す")
	}
	if h, _ := s.SearchMessages(ctx, "%", 10); len(h) != 1 {
		t.Errorf("%%だけの検索は全件にならない: %d", len(h))
	}
	if h, _ := s.SearchMessages(ctx, "", 10); len(h) != 0 {
		t.Error("空の検索は何も返さない")
	}
}

func TestNotesPrivacyAuthorizationLimitsAndDeletionLink(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	alice, bob := int64(11), int64(22)
	pub, _ := s.AddNote(ctx, store.Note{SubjectUserID: &alice, Content: "猫が好き"})
	priv, _ := s.AddNote(ctx, store.Note{SubjectUserID: &alice, Content: "就活で悩んでいる", Private: true})
	gen, _ := s.AddNote(ctx, store.Note{Content: "来月ハッカソンがある"})
	src := int64(900)
	linked, _ := s.AddNote(ctx, store.Note{Content: "元メッセージ付き", SourceMessageID: &src})
	ids := func(ns []store.Note) map[int64]bool {
		m := map[int64]bool{}
		for _, n := range ns {
			m[n.ID] = true
		}
		return m
	}
	// private は、本人とのDMの中だけ
	got, _ := s.SearchNotes(ctx, "", alice, false, 50)
	if m := ids(got); m[priv] || !m[pub] || !m[gen] {
		t.Errorf("DM外でprivateが見えた/公開が見えない: %v", m)
	}
	if m := ids(mustNotes(s.SearchNotes(ctx, "", alice, true, 50))); !m[priv] {
		t.Error("本人とのDMではprivateが見える")
	}
	if m := ids(mustNotes(s.SearchNotes(ctx, "", bob, true, 50))); m[priv] {
		t.Error("別の人とのDMでprivateが見えた")
	}
	if m := ids(mustNotes(s.SearchNotes(ctx, "ハッカソン", bob, false, 50))); len(m) != 1 || !m[gen] {
		t.Errorf("検索: %v", m)
	}
	// 他人のノートは消せない
	if ok, _ := s.ForgetNote(ctx, pub, bob); ok {
		t.Error("他人のノートを消せた")
	}
	if ok, _ := s.ForgetNote(ctx, gen, alice); ok {
		t.Error("一般のメモを一人で消せた")
	}
	if ok, _ := s.ForgetNote(ctx, pub, alice); !ok {
		t.Error("本人のノートが消せない")
	}
	// 元メッセージの削除で、ノートも消える
	if _, err := s.DeleteMessages(ctx, src); err != nil {
		t.Fatal(err)
	}
	if m := ids(mustNotes(s.SearchNotes(ctx, "", bob, false, 50))); m[linked] {
		t.Error("元メッセージが消えてもノートが残った")
	}
	// 「忘れて」: 本人のノートと親密度が全部消える。他の人は残る
	_ = s.BumpCloseness(ctx, alice)
	bobNote, _ := s.AddNote(ctx, store.Note{SubjectUserID: &bob, Content: "ボブのメモ"})
	n, err := s.ForgetPerson(ctx, alice)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if c, _ := s.GetCloseness(ctx, alice); c != 0 {
		t.Error("親密度が残った")
	}
	if m := ids(mustNotes(s.SearchNotes(ctx, "", bob, true, 50))); !m[bobNote] || m[priv] {
		t.Errorf("忘れての範囲: %v", m)
	}
	// 上限
	for i := 0; i < store.MaxNotesPerUser; i++ {
		if _, err := s.AddNote(ctx, store.Note{SubjectUserID: &bob, Content: "x"}); err != nil && i < store.MaxNotesPerUser-1 {
			t.Fatal(err)
		}
	}
	if _, err := s.AddNote(ctx, store.Note{SubjectUserID: &bob, Content: "あふれる"}); !errors.Is(err, store.ErrNoteLimit) {
		t.Errorf("上限: %v", err)
	}
	if _, err := s.AddNote(ctx, store.Note{Content: strings.Repeat("あ", 501)}); err == nil {
		t.Error("501文字を受け付けた")
	}
}

func mustNotes(n []store.Note, err error) []store.Note {
	if err != nil {
		panic(err)
	}
	return n
}

func TestSummariesCandidatesSaveAndDeletionLink(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	jst := time.FixedZone("JST", 9*3600)
	d1 := time.Date(2026, 11, 20, 23, 30, 0, 0, jst) // JSTの20日（UTCでも20日）
	d2 := time.Date(2026, 11, 21, 0, 30, 0, 0, jst)  // JSTの21日（UTCでは20日15:30）
	var id int64
	add := func(ch int64, at time.Time, kind string, bot bool) int64 {
		id++
		m := store.Message{ID: id, ChannelID: ch, Kind: kind, AuthorID: 7, AuthorName: "太郎", Content: "発言", CreatedAt: at, IsBot: bot}
		if err := s.UpsertMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
		return id
	}
	var firstOfDay1 int64
	for i := 0; i < 5; i++ {
		x := add(1, d1.Add(time.Duration(i)*time.Minute), "guild", false)
		if i == 0 {
			firstOfDay1 = x
		}
	}
	for i := 0; i < 5; i++ {
		add(1, d2.Add(time.Duration(i)*time.Minute), "guild", false) // 翌日（JST）
	}
	for i := 0; i < 9; i++ {
		add(2, d1, "dm", false)   // DMは対象外
		add(3, d1, "guild", true) // Botは対象外
	}
	before := time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC)
	gs, err := s.SummaryCandidates(ctx, before, nil, 5, 100)
	if err != nil || len(gs) != 2 {
		t.Fatalf("JSTで日付が分かれ、DM・Botは除く: %+v %v", gs, err)
	}
	if !gs[0].Day.Equal(time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)) || gs[0].Count != 5 {
		t.Errorf("新しい日から: %+v", gs)
	}
	if gs2, _ := s.SummaryCandidates(ctx, time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC), nil, 5, 100); len(gs2) != 1 {
		t.Error("before（含まない）")
	}
	since := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
	if gs3, _ := s.SummaryCandidates(ctx, before, &since, 5, 100); len(gs3) != 1 {
		t.Error("since")
	}
	if gs4, _ := s.SummaryCandidates(ctx, before, nil, 6, 100); len(gs4) != 0 {
		t.Error("最小件数")
	}
	msgs, _ := s.DayMessages(ctx, 1, gs[1].Day, 100)
	if len(msgs) != 5 {
		t.Errorf("その日の発言: %d", len(msgs))
	}
	if err := s.SaveSummary(ctx, store.Summary{ChannelID: 1, Day: gs[1].Day, Content: "- ハッカソンの話", MsgCount: 5}); err != nil {
		t.Fatal(err)
	}
	if gs5, _ := s.SummaryCandidates(ctx, before, nil, 5, 100); len(gs5) != 1 {
		t.Error("要約済みは候補から外れる")
	}
	if got, _ := s.SearchSummaries(ctx, "ハッカソン", 5); len(got) != 1 {
		t.Error("要約の検索")
	}
	if got, _ := s.SearchSummaries(ctx, "", 5); len(got) != 1 {
		t.Error("空なら最近のもの")
	}
	if got, _ := s.SummariesBetween(ctx, since.AddDate(0, 0, -1), since.AddDate(0, 0, 1), 10); len(got) != 1 {
		t.Errorf("期間（to含まず）: %d", len(got))
	}
	if got, _ := s.SummariesBetween(ctx, since, since.AddDate(0, 0, 5), 10); len(got) != 0 {
		t.Errorf("範囲外: %d", len(got))
	}
	// メッセージが消されたら、その日の要約も消えて、また候補になる
	if _, err := s.DeleteMessages(ctx, firstOfDay1); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.SearchSummaries(ctx, "", 5); len(got) != 0 {
		t.Error("消されたメッセージの日の要約が残った")
	}
}
