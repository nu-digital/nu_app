//go:build integration

package bootstrap_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nu-digital/nu_app/services/api/internal/bootstrap"
	"github.com/nu-digital/nu_app/services/api/internal/testutil/pgtest"
)

func count(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestBootstrapCreatesOwnerWithHashedInviteToken(t *testing.T) {
	pool, _ := pgtest.New(t)
	ctx := context.Background()
	before := time.Now()
	res, err := bootstrap.Owner(ctx, pool, bootstrap.Params{LoginID: "owner.example", DisplayName: "初期オーナー(架空)", TokenTTL: 2 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	var status, name string
	if err := pool.QueryRow(ctx, `SELECT u.status, p.display_name FROM users u JOIN profiles p ON p.user_id = u.id WHERE u.login_id = 'owner.example'`).Scan(&status, &name); err != nil {
		t.Fatal(err)
	}
	if status != "invited" || name != "初期オーナー(架空)" {
		t.Fatalf("ユーザー/プロフィールが想定と異なる: %s %s", status, name)
	}
	if n := count(t, pool, `SELECT count(*) FROM user_roles WHERE user_id = $1 AND role = 'owner' AND assigned_by IS NULL`, res.UserID); n != 1 {
		t.Fatalf("owner権限が1件でない: %d", n)
	}
	// パスワードはまだ無い（招待受諾で本人が設定する）
	if n := count(t, pool, `SELECT count(*) FROM password_credentials`); n != 0 {
		t.Fatalf("パスワードが作られている: %d", n)
	}
	// tokenはSHA-256のみ保存。受け取った平文のhashと一致する
	hash, ok := bootstrap.HashToken(res.InviteToken)
	if !ok {
		t.Fatal("返されたtokenの形式が不正")
	}
	var purpose string
	var expires time.Time
	if err := pool.QueryRow(ctx, `SELECT purpose, expires_at FROM one_time_tokens WHERE token_hash = $1 AND used_at IS NULL AND revoked_at IS NULL AND created_by IS NULL`, hash).Scan(&purpose, &expires); err != nil {
		t.Fatalf("保存されたhashで招待tokenが見つからない: %v", err)
	}
	if purpose != "invite" || expires.Before(before.Add(2*time.Hour-time.Minute)) || expires.After(time.Now().Add(2*time.Hour+time.Minute)) {
		t.Fatalf("purpose/期限が想定と異なる: %s %v", purpose, expires)
	}
	// 平文tokenが監査ログに残っていない。監査は1件で、秘密値を含まない
	if n := count(t, pool, `SELECT count(*) FROM audit_logs WHERE action = 'bootstrap.owner_created'`); n != 1 {
		t.Fatalf("監査ログが1件でない: %d", n)
	}
	if n := count(t, pool, `SELECT count(*) FROM audit_logs WHERE strpos(summary_json::text, $1) > 0 OR strpos(coalesce(target_id,''), $1) > 0`, res.InviteToken); n != 0 {
		t.Fatal("監査ログに平文tokenが含まれている")
	}
}

func TestSecondBootstrapIsRefusedEvenWithAnotherLoginID(t *testing.T) {
	pool, _ := pgtest.New(t)
	ctx := context.Background()
	if _, err := bootstrap.Owner(ctx, pool, bootstrap.Params{LoginID: "owner.example", DisplayName: "初期オーナー(架空)"}); err != nil {
		t.Fatal(err)
	}
	_, err := bootstrap.Owner(ctx, pool, bootstrap.Params{LoginID: "second.example", DisplayName: "二人目(架空)"})
	if !errors.Is(err, bootstrap.ErrAlreadyBootstrapped) {
		t.Fatalf("2回目が拒否されない: %v", err)
	}
	if n := count(t, pool, `SELECT count(*) FROM users`); n != 1 {
		t.Fatalf("ユーザーが増えている: %d", n)
	}
}

func TestRefusedWhenOwnerAlreadyExistsWithoutBootstrapRecord(t *testing.T) {
	pool, _ := pgtest.New(t)
	ctx := context.Background()
	// bootstrap_state が空でも、ownerが居れば作らない（想定外の状態での二重保護）
	if _, err := pool.Exec(ctx, `WITH u AS (INSERT INTO users (login_id, status) VALUES ('existing.example', 'active') RETURNING id) INSERT INTO user_roles (user_id, role) SELECT id, 'owner' FROM u`); err != nil {
		t.Fatal(err)
	}
	_, err := bootstrap.Owner(ctx, pool, bootstrap.Params{LoginID: "new.example", DisplayName: "新規(架空)"})
	if !errors.Is(err, bootstrap.ErrOwnerExists) {
		t.Fatalf("ownerが居るのに作成できてしまう: %v", err)
	}
	if n := count(t, pool, `SELECT count(*) FROM bootstrap_state`); n != 0 {
		t.Fatalf("拒否した試行がbootstrapを消費した: %d", n)
	}
	if n := count(t, pool, `SELECT count(*) FROM users`); n != 1 {
		t.Fatalf("ユーザーが増えている: %d", n)
	}
}

func TestFailedAttemptDoesNotConsumeBootstrap(t *testing.T) {
	pool, _ := pgtest.New(t)
	ctx := context.Background()
	for _, p := range []bootstrap.Params{
		{LoginID: "Bad ID", DisplayName: "表示名(架空)"},
		{LoginID: "", DisplayName: "表示名(架空)"},
		{LoginID: "ok.example", DisplayName: "   "},
		{LoginID: "ok.example", DisplayName: strings.Repeat("あ", 65)},
	} {
		if _, err := bootstrap.Owner(ctx, pool, p); !errors.Is(err, bootstrap.ErrInvalidInput) {
			t.Fatalf("%+v: 入力エラーにならない: %v", p, err)
		}
	}
	if n := count(t, pool, `SELECT (SELECT count(*) FROM bootstrap_state) + (SELECT count(*) FROM users) + (SELECT count(*) FROM one_time_tokens) + (SELECT count(*) FROM audit_logs)`); n != 0 {
		t.Fatalf("失敗した試行が何かを残した: %d", n)
	}
	if _, err := bootstrap.Owner(ctx, pool, bootstrap.Params{LoginID: "ok.example", DisplayName: "表示名(架空)"}); err != nil {
		t.Fatalf("修正後の再実行ができない: %v", err)
	}
}

func TestConcurrentBootstrapCreatesExactlyOneOwner(t *testing.T) {
	pool, _ := pgtest.New(t)
	ctx := context.Background()
	const workers = 12
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = bootstrap.Owner(ctx, pool, bootstrap.Params{LoginID: "owner" + string(rune('a'+i)) + ".example", DisplayName: "並行(架空)"})
		}(i)
	}
	close(start)
	wg.Wait()
	ok, refused := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, bootstrap.ErrAlreadyBootstrapped):
			refused++
		default:
			t.Fatalf("想定外のエラー: %v", err)
		}
	}
	if ok != 1 || refused != workers-1 {
		t.Fatalf("成功=%d 拒否=%d（成功は1件のはず）", ok, refused)
	}
	for table, want := range map[string]int{"users": 1, "profiles": 1, "user_roles": 1, "one_time_tokens": 1, "bootstrap_state": 1, "audit_logs": 1} {
		if n := count(t, pool, `SELECT count(*) FROM `+table); n != want {
			t.Errorf("%s の件数 = %d, 期待 %d", table, n, want)
		}
	}
}
