//go:build integration

package recovery_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nu-digital/nu_app/services/api/internal/auth/token"
	"github.com/nu-digital/nu_app/services/api/internal/recovery"
	"github.com/nu-digital/nu_app/services/api/internal/testutil/pgtest"
)

func addUser(t *testing.T, pool *pgxpool.Pool, login, status string, pw bool, roles ...string) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	var id pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users (login_id, status) VALUES ($1,'invited') RETURNING id`, login).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if status == "active" {
		pool.Exec(ctx, `UPDATE users SET status='active', activated_at=now() WHERE id=$1`, id)
	}
	if status == "disabled" {
		pool.Exec(ctx, `UPDATE users SET status='disabled', disabled_at=now() WHERE id=$1`, id)
	}
	if pw {
		pool.Exec(ctx, `INSERT INTO password_credentials (user_id, password_hash) VALUES ($1,'$argon2id$v=19$m=64,t=1,p=1$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5a2V5')`, id)
	}
	for _, r := range roles {
		pool.Exec(ctx, `INSERT INTO user_roles (user_id, role) VALUES ($1,$2)`, id, r)
	}
	return id
}

func count(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func params(login string) recovery.Params {
	return recovery.Params{LoginID: login, ConfirmLoginID: login, ApproverNote: "運用責任者(架空)が本人確認し承認 2026-11-21", OSUser: "operator"}
}

func TestRecoverExistingOwnerRevokesSessionsAndIssuesToken(t *testing.T) {
	pool, _ := pgtest.New(t)
	ctx := context.Background()
	id := addUser(t, pool, "owner.example", "disabled", true, "owner")
	addUser(t, pool, "member.example", "active", true) // 他のユーザーは影響を受けない
	hash := make([]byte, 32)
	hash[0] = 7
	if _, err := pool.Exec(ctx, `INSERT INTO sessions (user_id, token_hash, expires_at) VALUES ($1,$2, now()+interval '1 day')`, id, hash); err != nil {
		t.Fatal(err)
	}
	old := make([]byte, 32)
	old[0] = 9
	pool.Exec(ctx, `INSERT INTO one_time_tokens (user_id, purpose, token_hash, expires_at) VALUES ($1,'password_reset',$2, now()+interval '1 day')`, id, old)

	res, err := recovery.RecoverOwner(ctx, pool, params("Owner.Example"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Purpose != token.PurposePasswordReset || res.Designated || res.Token == "" {
		t.Fatalf("結果: %+v", res)
	}
	if n := count(t, pool, `SELECT count(*) FROM users WHERE id=$1 AND status='active' AND disabled_at IS NULL`, id); n != 1 {
		t.Fatal("停止中のOwnerが有効に戻っていない")
	}
	if n := count(t, pool, `SELECT count(*) FROM sessions WHERE user_id=$1 AND revoked_at IS NULL`, id); n != 0 {
		t.Fatal("既存セッションが失効していない")
	}
	if n := count(t, pool, `SELECT count(*) FROM one_time_tokens WHERE user_id=$1 AND revoked_at IS NOT NULL`, id); n != 1 {
		t.Fatal("古いトークンが失効していない")
	}
	h, _ := token.Hash(res.Token)
	if n := count(t, pool, `SELECT count(*) FROM one_time_tokens WHERE token_hash=$1 AND purpose='password_reset' AND used_at IS NULL AND revoked_at IS NULL`, h); n != 1 {
		t.Fatal("新しいトークン(hash)が保存されていない")
	}
	// イベントと監査: 承認メモ・実行者は記録、トークンは記録しない
	if n := count(t, pool, `SELECT count(*) FROM emergency_recovery_events WHERE target_user_id=$1 AND executed_by_os_user='operator' AND approver_note LIKE '運用責任者%'`, id); n != 1 {
		t.Fatal("復旧イベントが記録されていない")
	}
	if n := count(t, pool, `SELECT count(*) FROM audit_logs WHERE action='recovery.owner_recovered' AND strpos(summary_json::text, $1) = 0 AND strpos(summary_json::text, '本人確認') = 0`, res.Token); n != 1 {
		t.Fatal("監査が不正（トークンや承認メモを含む/未記録）")
	}
	if n := count(t, pool, `SELECT count(*) FROM users WHERE login_id='member.example' AND status='active'`); n != 1 {
		t.Fatal("他のユーザーに影響した")
	}
}

func TestRecoverOwnerWithoutPasswordGetsInviteToken(t *testing.T) {
	pool, _ := pgtest.New(t)
	addUser(t, pool, "owner.example", "invited", false, "owner")
	res, err := recovery.RecoverOwner(context.Background(), pool, params("owner.example"))
	if err != nil || res.Purpose != token.PurposeInvite {
		t.Fatalf("未設定のOwnerには招待トークン: %+v %v", res, err)
	}
}

// activeなOwnerが居るのに、別のユーザーをOwnerにすることはできない（誰でもOwnerになれる機能を作らない）。
func TestCannotCrownAnotherUserWhileAnActiveOwnerExists(t *testing.T) {
	pool, _ := pgtest.New(t)
	addUser(t, pool, "owner.example", "active", true, "owner")
	addUser(t, pool, "member.example", "active", true)
	_, err := recovery.RecoverOwner(context.Background(), pool, params("member.example"))
	if !errors.Is(err, recovery.ErrNotAllowed) {
		t.Fatalf("activeなOwnerが居るのにOwner化できてしまう: %v", err)
	}
	if n := count(t, pool, `SELECT count(*) FROM user_roles WHERE role='owner'`); n != 1 {
		t.Fatal("拒否したのにOwnerが増えた")
	}
	if n := count(t, pool, `SELECT (SELECT count(*) FROM emergency_recovery_events)+(SELECT count(*) FROM one_time_tokens)+(SELECT count(*) FROM audit_logs)`); n != 0 {
		t.Fatalf("拒否した試行が何かを残した: %d", n)
	}
}

func TestDesignateNewOwnerOnlyWhenNoActiveOwnerExists(t *testing.T) {
	pool, _ := pgtest.New(t)
	addUser(t, pool, "owner.example", "disabled", true, "owner") // 唯一のOwnerが停止中
	mid := addUser(t, pool, "member.example", "active", true)
	res, err := recovery.RecoverOwner(context.Background(), pool, params("member.example"))
	if err != nil || !res.Designated {
		t.Fatalf("指名できない: %+v %v", res, err)
	}
	if n := count(t, pool, `SELECT count(*) FROM user_roles WHERE user_id=$1 AND role='owner'`, mid); n != 1 {
		t.Fatal("Ownerに指名されていない")
	}
	if n := count(t, pool, `SELECT count(*) FROM users u JOIN user_roles r ON r.user_id=u.id WHERE r.role='owner' AND u.status='active'`); n != 1 {
		t.Fatal("activeなOwnerが1人になっていない")
	}
}

func TestRefusalsLeaveNothingBehind(t *testing.T) {
	pool, _ := pgtest.New(t)
	addUser(t, pool, "owner.example", "disabled", true, "owner")
	ctx := context.Background()
	bad := map[string]recovery.Params{
		"承認メモなし":   {LoginID: "owner.example", ConfirmLoginID: "owner.example", ApproverNote: "  ", OSUser: "op"},
		"確認IDが不一致": {LoginID: "owner.example", ConfirmLoginID: "other.example", ApproverNote: "x", OSUser: "op"},
		"確認IDなし":   {LoginID: "owner.example", ApproverNote: "x", OSUser: "op"},
		"実行者なし":    {LoginID: "owner.example", ConfirmLoginID: "owner.example", ApproverNote: "x"},
		"メモが長すぎる":  {LoginID: "owner.example", ConfirmLoginID: "owner.example", ApproverNote: strings.Repeat("あ", 501), OSUser: "op"},
		"IDなし":     {ConfirmLoginID: "", ApproverNote: "x", OSUser: "op"},
	}
	for name, p := range bad {
		if _, err := recovery.RecoverOwner(ctx, pool, p); !errors.Is(err, recovery.ErrInvalidInput) {
			t.Errorf("%s: 入力エラーにならない: %v", name, err)
		}
	}
	if _, err := recovery.RecoverOwner(ctx, pool, params("ghost.example")); !errors.Is(err, recovery.ErrNotFound) {
		t.Errorf("存在しないユーザー: %v", err)
	}
	if n := count(t, pool, `SELECT (SELECT count(*) FROM emergency_recovery_events)+(SELECT count(*) FROM one_time_tokens)+(SELECT count(*) FROM audit_logs)+(SELECT count(*) FROM users WHERE status='active')`); n != 0 {
		t.Fatalf("拒否した試行が何かを変えた: %d", n)
	}
}
