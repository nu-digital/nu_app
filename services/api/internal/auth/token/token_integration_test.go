//go:build integration

package token_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nu-digital/nu_app/services/api/internal/auth/token"
	"github.com/nu-digital/nu_app/services/api/internal/store"
	"github.com/nu-digital/nu_app/services/api/internal/testutil/pgtest"
)

func newUser(t *testing.T, pool *pgxpool.Pool, id string) pgtype.UUID {
	t.Helper()
	u, err := store.New(pool).CreateUser(context.Background(), store.CreateUserParams{LoginID: id, Status: "invited"})
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func issue(t *testing.T, pool *pgxpool.Pool, uid pgtype.UUID, purpose string, ttl time.Duration, now time.Time) string {
	t.Helper()
	raw, _, err := token.Issue(context.Background(), pool, uid, purpose, ttl, pgtype.UUID{}, now)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestIssueAndConsumeOnce(t *testing.T) {
	pool, _ := pgtest.New(t)
	ctx := context.Background()
	uid := newUser(t, pool, "alice.example")
	now := time.Now()
	raw := issue(t, pool, uid, token.PurposeInvite, time.Hour, now)
	got, err := token.Consume(ctx, pool, raw, token.PurposeInvite, now)
	if err != nil || got != uid {
		t.Fatalf("使用に失敗: %v", err)
	}
	if _, err := token.Consume(ctx, pool, raw, token.PurposeInvite, now); !errors.Is(err, token.ErrInvalid) {
		t.Fatalf("使用済みのtokenが再利用できた: %v", err)
	}
}

func TestInvalidTokensAreAllRejectedTheSameWay(t *testing.T) {
	pool, _ := pgtest.New(t)
	ctx := context.Background()
	uid := newUser(t, pool, "alice.example")
	now := time.Now()
	raw := issue(t, pool, uid, token.PurposeInvite, time.Hour, now)

	cases := map[string]func() error{
		"期限切れ":      func() error { _, e := token.Consume(ctx, pool, raw, token.PurposeInvite, now.Add(time.Hour)); return e },
		"purpose違い": func() error { _, e := token.Consume(ctx, pool, raw, token.PurposePasswordReset, now); return e },
		"形式不正":      func() error { _, e := token.Consume(ctx, pool, "not-a-token", token.PurposeInvite, now); return e },
		"存在しない": func() error {
			other, _, _ := token.New()
			_, e := token.Consume(ctx, pool, other, token.PurposeInvite, now)
			return e
		},
	}
	for name, f := range cases {
		if err := f(); !errors.Is(err, token.ErrInvalid) {
			t.Errorf("%s: ErrInvalidにならない: %v", name, err)
		}
	}
	// 上の拒否はいずれもトークンを消費していない
	if _, err := token.Consume(ctx, pool, raw, token.PurposeInvite, now); err != nil {
		t.Fatalf("拒否した試行がトークンを消費した: %v", err)
	}
}

func TestReissueRevokesPreviousToken(t *testing.T) {
	pool, _ := pgtest.New(t)
	ctx := context.Background()
	uid := newUser(t, pool, "alice.example")
	now := time.Now()
	first := issue(t, pool, uid, token.PurposeInvite, time.Hour, now)
	second := issue(t, pool, uid, token.PurposeInvite, time.Hour, now)
	if _, err := token.Consume(ctx, pool, first, token.PurposeInvite, now); !errors.Is(err, token.ErrInvalid) {
		t.Fatalf("再発行後の旧tokenが使えた: %v", err)
	}
	if _, err := token.Consume(ctx, pool, second, token.PurposeInvite, now); err != nil {
		t.Fatalf("新しいtokenが使えない: %v", err)
	}
	// 別purposeのtokenは影響を受けない
	reset := issue(t, pool, uid, token.PurposePasswordReset, time.Hour, now)
	issue(t, pool, uid, token.PurposeInvite, time.Hour, now)
	if _, err := token.Consume(ctx, pool, reset, token.PurposePasswordReset, now); err != nil {
		t.Fatalf("別purposeのtokenまで失効した: %v", err)
	}
}

func TestConcurrentConsumeSucceedsExactlyOnce(t *testing.T) {
	pool, _ := pgtest.New(t)
	uid := newUser(t, pool, "alice.example")
	now := time.Now()
	raw := issue(t, pool, uid, token.PurposePasswordReset, time.Hour, now)
	const workers = 16
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, results[i] = token.Consume(context.Background(), pool, raw, token.PurposePasswordReset, now)
		}(i)
	}
	close(start)
	wg.Wait()
	ok := 0
	for _, e := range results {
		if e == nil {
			ok++
		} else if !errors.Is(e, token.ErrInvalid) {
			t.Fatalf("想定外のエラー: %v", e)
		}
	}
	if ok != 1 {
		t.Fatalf("並行使用の成功が%d件（1件のはず）", ok)
	}
}

func TestRollbackDoesNotConsumeToken(t *testing.T) {
	pool, _ := pgtest.New(t)
	ctx := context.Background()
	uid := newUser(t, pool, "alice.example")
	now := time.Now()
	raw := issue(t, pool, uid, token.PurposeInvite, time.Hour, now)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := token.Consume(ctx, tx, raw, token.PurposeInvite, now); err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback(ctx) // 後続処理(パスワード保存など)が失敗した想定
	if _, err := token.Consume(ctx, pool, raw, token.PurposeInvite, now); err != nil {
		t.Fatalf("巻き戻したのにtokenが消費されている: %v", err)
	}
}

func TestConcurrentReissueConflictIsReportedNotSilentlyDuplicated(t *testing.T) {
	pool, _ := pgtest.New(t)
	uid := newUser(t, pool, "alice.example")
	now := time.Now()
	const workers = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, workers)
	raws := make([]string, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			raws[i], _, errs[i] = token.Issue(context.Background(), pool, uid, token.PurposeInvite, time.Hour, pgtype.UUID{}, now)
		}(i)
	}
	close(start)
	wg.Wait()
	for _, e := range errs {
		if e != nil && !errors.Is(e, token.ErrConflict) {
			t.Fatalf("想定外のエラー: %v", e)
		}
	}
	var active int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM one_time_tokens WHERE user_id = $1 AND purpose = 'invite' AND used_at IS NULL AND revoked_at IS NULL`, uid).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatalf("有効なtokenが%d件（1件のはず）", active)
	}
}
