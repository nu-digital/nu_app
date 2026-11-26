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
