//go:build integration

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nu-digital/nu_app/services/api/internal/bootstrap"
	"github.com/nu-digital/nu_app/services/api/internal/testutil/pgtest"
)

func runCLI(t *testing.T, dsn string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	env := map[string]string{"DATABASE_URL": dsn}
	code := run(context.Background(), args, func(k string) string { return env[k] }, &out, &errb)
	return code, out.String(), errb.String()
}

func TestBootstrapOwnerCommand(t *testing.T) {
	pool, dsn := pgtest.New(t)
	code, out, errOut := runCLI(t, dsn, "bootstrap-owner", "--login-id", "owner.example", "--display-name", "初期オーナー(架空)")
	if code != 0 || errOut != "" {
		t.Fatalf("成功しない: code=%d stderr=%s", code, errOut)
	}
	// 出力の最後から2行目付近のtokenがDBのhashと一致する
	var token string
	for _, line := range strings.Split(out, "\n") {
		if _, ok := bootstrap.HashToken(strings.TrimSpace(line)); ok {
			token = strings.TrimSpace(line)
		}
	}
	if token == "" {
		t.Fatalf("出力にtokenが無い: %s", out)
	}
	hash, _ := bootstrap.HashToken(token)
	if n := rows(t, pool, `SELECT count(*) FROM one_time_tokens WHERE token_hash = $1`, hash); n != 1 {
		t.Fatalf("表示したtokenに対応するhashが保存されていない: %d", n)
	}

	// 2回目は別のIDでも拒否され、tokenを表示しない
	code, out2, errOut2 := runCLI(t, dsn, "bootstrap-owner", "--login-id", "second.example", "--display-name", "二人目(架空)")
	if code != 3 || out2 != "" || !strings.Contains(errOut2, "再実行はできません") {
		t.Fatalf("2回目の挙動が想定と異なる: code=%d out=%q err=%q", code, out2, errOut2)
	}
	if n := rows(t, pool, `SELECT count(*) FROM users`); n != 1 {
		t.Fatalf("ユーザーが増えている: %d", n)
	}
}

func TestBootstrapOwnerCommandArguments(t *testing.T) {
	_, dsn := pgtest.New(t)
	if code, _, _ := runCLI(t, dsn); code != 2 {
		t.Errorf("引数なし: code=%d", code)
	}
	if code, _, _ := runCLI(t, dsn, "bootstrap-owner", "--login-id", "owner.example"); code != 2 {
		t.Errorf("表示名なし: code=%d", code)
	}
	if code, _, e := runCLI(t, dsn, "bootstrap-owner", "--login-id", "Bad ID", "--display-name", "x"); code != 2 || e == "" {
		t.Errorf("不正なログインID: code=%d", code)
	}
	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{"bootstrap-owner", "--login-id", "a.example", "--display-name", "x"}, func(string) string { return "" }, &out, &errb); code != 2 {
		t.Errorf("DATABASE_URLなし: code=%d", code)
	}
	if code, _, _ := runCLI(t, dsn, "unknown-command"); code != 2 {
		t.Errorf("未知のコマンド: code=%d", code)
	}
}

func TestBootstrapOwnerCommandDoesNotLeakConnectionString(t *testing.T) {
	var out, errb bytes.Buffer
	dsn := "postgres://leaky-user:leaky-secret-pass@127.0.0.1:1/none?sslmode=disable&connect_timeout=1"
	code := run(context.Background(), []string{"bootstrap-owner", "--login-id", "owner.example", "--display-name", "x"}, func(string) string { return dsn }, &out, &errb)
	if code != 1 {
		t.Fatalf("DB不達でcode=1にならない: %d", code)
	}
	if all := out.String() + errb.String(); strings.Contains(all, "leaky-secret-pass") || strings.Contains(all, "leaky-user") {
		t.Fatalf("接続情報が出力に漏れた: %s", all)
	}
}

func rows(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
