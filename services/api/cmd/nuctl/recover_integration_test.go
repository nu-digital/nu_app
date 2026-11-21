//go:build integration

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nu-digital/nu_app/services/api/internal/testutil/pgtest"
)

func TestRecoverOwnerCommand(t *testing.T) {
	pool, dsn := pgtest.New(t)
	rows := func(sql string) int { return rows2(t, pool, sql) }
	if _, err := pool.Exec(context.Background(), `WITH u AS (INSERT INTO users (login_id, status, disabled_at) VALUES ('owner.example','disabled', now()) RETURNING id), p AS (INSERT INTO password_credentials (user_id, password_hash) SELECT id, '$argon2id$v=19$m=64,t=1,p=1$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5a2V5' FROM u) INSERT INTO user_roles (user_id, role) SELECT id, 'owner' FROM u`); err != nil {
		t.Fatal(err)
	}
	// 引数不足は2、確認ID不一致は2。何も変わらない。
	if code, _, _ := runCLI(t, dsn, "recover-owner", "--login-id", "owner.example"); code != 2 {
		t.Errorf("引数不足: %d", code)
	}
	if code, _, _ := runCLI(t, dsn, "recover-owner", "--login-id", "owner.example", "--confirm-login-id", "other.example", "--approver-note", "x"); code != 2 {
		t.Errorf("確認ID不一致: %d", code)
	}
	if rows(`SELECT count(*) FROM emergency_recovery_events`) != 0 {
		t.Fatal("拒否した実行が記録を残した")
	}
	if code, _, e := runCLI(t, dsn, "recover-owner", "--login-id", "ghost.example", "--confirm-login-id", "ghost.example", "--approver-note", "x"); code != 3 || e == "" {
		t.Errorf("存在しないユーザー: %d", code)
	}
	code, out, errOut := runCLI(t, dsn, "recover-owner", "--login-id", "owner.example", "--confirm-login-id", "owner.example", "--approver-note", "運用責任者(架空)が本人確認し承認")
	if code != 0 || errOut != "" || !strings.Contains(out, "復旧トークン") || !strings.Contains(out, "監査ログに記録") {
		t.Fatalf("復旧: code=%d out=%q err=%q", code, out, errOut)
	}
	if rows(`SELECT count(*) FROM emergency_recovery_events WHERE executed_by_os_user <> ''`) != 1 {
		t.Fatal("実行記録がない")
	}
	if rows(`SELECT count(*) FROM users WHERE login_id='owner.example' AND status='active'`) != 1 {
		t.Fatal("復旧されていない")
	}
}

func rows2(t *testing.T, pool *pgxpool.Pool, sql string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
