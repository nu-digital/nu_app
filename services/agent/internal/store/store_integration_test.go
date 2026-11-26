//go:build integration

package store_test

import (
	"context"
	"encoding/json"
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
