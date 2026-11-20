//go:build integration

package session_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nu-digital/nu_app/services/api/internal/auth/session"
	"github.com/nu-digital/nu_app/services/api/internal/authz"
	"github.com/nu-digital/nu_app/services/api/internal/testutil/pgtest"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func setup(t *testing.T) (*pgxpool.Pool, *session.Service, *clock, pgtype.UUID) {
	t.Helper()
	pool, _ := pgtest.New(t)
	c := &clock{t: time.Now()}
	var uid pgtype.UUID
	if err := pool.QueryRow(context.Background(), `INSERT INTO users (login_id, status, activated_at) VALUES ('alice.example', 'active', now()) RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	return pool, session.New(pool, session.DefaultConfig).WithClock(c.now), c, uid
}

func TestCreateAndValidate(t *testing.T) {
	pool, svc, _, uid := setup(t)
	ctx := context.Background()
	tok, _, err := svc.Create(ctx, pool, uid)
	if err != nil {
		t.Fatal(err)
	}
	id, err := svc.Validate(ctx, tok)
	if err != nil || id.UserID != uid {
		t.Fatalf("検証に失敗: %v", err)
	}
	// 平文のtokenはDBに無く、SHA-256(32バイト)のみ
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE octet_length(token_hash) = 32 AND strpos(encode(token_hash, 'escape'), $1) = 0`, tok).Scan(&n); err != nil || n != 1 {
		t.Fatalf("hashのみが保存されているはず: n=%d err=%v", n, err)
	}
	for _, bad := range []string{"", "x", "!!!", tok + "A"} {
		if _, err := svc.Validate(ctx, bad); !errors.Is(err, session.ErrInvalid) {
			t.Errorf("不正なtoken %q が受理された: %v", bad, err)
		}
	}
	// 毎回新しいセッションが発行される（固定されない）
	tok2, _, _ := svc.Create(ctx, pool, uid)
	if tok2 == tok {
		t.Fatal("セッションtokenが再利用された")
	}
}

func TestExpiryAbsoluteAndIdle(t *testing.T) {
	pool, svc, c, uid := setup(t)
	ctx := context.Background()
	tok, _, _ := svc.Create(ctx, pool, uid)
	// アイドル: 6日おきに使えば絶対期限(14日)まで有効、7日放置で無効
	for i := 0; i < 2; i++ {
		c.advance(6 * 24 * time.Hour)
		if _, err := svc.Validate(ctx, tok); err != nil {
			t.Fatalf("%d回目: アイドル期限内なのに無効: %v", i, err)
		}
	}
	c.advance(3 * 24 * time.Hour) // 作成から15日: 絶対期限切れ
	if _, err := svc.Validate(ctx, tok); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("絶対期限(14日)を過ぎても有効: %v", err)
	}
	tok2, _, _ := svc.Create(ctx, pool, uid)
	c.advance(7*24*time.Hour + time.Second)
	if _, err := svc.Validate(ctx, tok2); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("アイドル期限(7日)を過ぎても有効: %v", err)
	}
}

func TestLastSeenIsThrottled(t *testing.T) {
	pool, svc, c, uid := setup(t)
	ctx := context.Background()
	tok, _, _ := svc.Create(ctx, pool, uid)
	var before time.Time
	pool.QueryRow(ctx, `SELECT last_seen_at FROM sessions`).Scan(&before)
	c.advance(10 * time.Second)
	svc.Validate(ctx, tok)
	var after time.Time
	pool.QueryRow(ctx, `SELECT last_seen_at FROM sessions`).Scan(&after)
	if !after.Equal(before) {
		t.Fatal("間隔(1分)未満ではlast_seenを更新しないはず")
	}
	c.advance(2 * time.Minute)
	svc.Validate(ctx, tok)
	pool.QueryRow(ctx, `SELECT last_seen_at FROM sessions`).Scan(&after)
	if !after.After(before) {
		t.Fatal("間隔を空ければlast_seenを更新するはず")
	}
}

func TestRevokeAndDisabledUsers(t *testing.T) {
	pool, svc, _, uid := setup(t)
	ctx := context.Background()
	a, _, _ := svc.Create(ctx, pool, uid)
	b, _, _ := svc.Create(ctx, pool, uid)
	if err := svc.Revoke(ctx, a); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Validate(ctx, a); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("ログアウト後のセッションが使えた")
	}
	if _, err := svc.Validate(ctx, b); err != nil {
		t.Fatal("他のセッションまで失効した")
	}
	// 停止するとセッションが失効していなくても使えない（毎回statusを確認）
	if _, err := pool.Exec(ctx, `UPDATE users SET status = 'disabled', disabled_at = now() WHERE id = $1`, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Validate(ctx, b); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("停止ユーザーのセッションが使えた")
	}
	if err := svc.Revoke(ctx, "unknown"); err != nil {
		t.Fatalf("存在しないtokenの失効はエラーにしない: %v", err)
	}
}

func TestRevokeAllAndRoles(t *testing.T) {
	pool, svc, _, uid := setup(t)
	ctx := context.Background()
	a, _, _ := svc.Create(ctx, pool, uid)
	b, _, _ := svc.Create(ctx, pool, uid)
	n, err := session.RevokeAll(ctx, pool, uid)
	if err != nil || n != 2 {
		t.Fatalf("全失効: n=%d err=%v", n, err)
	}
	for _, tok := range []string{a, b} {
		if _, err := svc.Validate(ctx, tok); !errors.Is(err, session.ErrInvalid) {
			t.Fatal("全失効後にセッションが使えた")
		}
	}
	// ロールは毎回DBから読む: 付与・剥奪が次のリクエストに反映される
	c, _, _ := svc.Create(ctx, pool, uid)
	id, _ := svc.Validate(ctx, c)
	if len(id.Roles) != 0 {
		t.Fatalf("Memberはロール行を持たない: %v", id.Roles)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_roles (user_id, role) VALUES ($1, 'admin')`, uid); err != nil {
		t.Fatal(err)
	}
	id, _ = svc.Validate(ctx, c)
	if len(id.Roles) != 1 || id.Roles[0] != authz.RoleAdmin || !authz.Decide(id.Subject(), "admin.users.create", authz.Resource{}).Allowed {
		t.Fatalf("付与が反映されない: %v", id.Roles)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM user_roles WHERE user_id = $1`, uid); err != nil {
		t.Fatal(err)
	}
	id, _ = svc.Validate(ctx, c)
	if authz.Decide(id.Subject(), "admin.users.create", authz.Resource{}).Allowed {
		t.Fatal("剥奪が次のリクエストで反映されない")
	}
}

func TestCSRFToken(t *testing.T) {
	a, b := "session-token-a", "session-token-b"
	ca := session.CSRFToken(a)
	if ca == session.CSRFToken(b) || ca == a || len(ca) != 64 {
		t.Fatalf("CSRFトークンがセッションに紐づいていない: %s", ca)
	}
	if !session.VerifyCSRF(a, ca) || session.VerifyCSRF(a, session.CSRFToken(b)) || session.VerifyCSRF(a, "") || session.VerifyCSRF(a, a) {
		t.Fatal("CSRF検証が想定と異なる")
	}
}
