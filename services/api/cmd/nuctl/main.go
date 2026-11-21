// Command nuctl はnu_appの管理CLI。公開Web/APIとは別経路で、運用者がサーバー上で実行する。
//
//	nuctl bootstrap-owner --login-id <id> --display-name <name> [--token-ttl 24h]
//	nuctl recover-owner --login-id <id> --confirm-login-id <id> --approver-note "<本人確認と承認の記録>"
//	nuctl media-gc | media-verify | media-redact（画像の運用。docs/OPERATIONS.md）
//
// 初期Ownerの作成は1回だけ。招待tokenは標準出力へ一度だけ表示する（ログに出さない・保存しない）。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nu-digital/nu_app/services/api/internal/bootstrap"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "recover-owner" {
		return runRecover(ctx, args[1:], getenv, stdout, stderr)
	}
	if len(args) > 0 && (args[0] == "media-gc" || args[0] == "media-verify" || args[0] == "media-redact") {
		return runMedia(ctx, args[0], args[1:], getenv, stdout, stderr)
	}
	if len(args) == 0 || args[0] != "bootstrap-owner" {
		fmt.Fprintln(stderr, "使い方:\n  nuctl bootstrap-owner --login-id <id> --display-name <name> [--token-ttl 24h]\n  nuctl recover-owner --login-id <id> --confirm-login-id <id> --approver-note \"<本人確認と承認の記録>\"")
		return 2
	}
	fs := flag.NewFlagSet("bootstrap-owner", flag.ContinueOnError)
	fs.SetOutput(stderr)
	loginID := fs.String("login-id", "", "初期OwnerのログインID（小文字英数字と . _ -）")
	displayName := fs.String("display-name", "", "初期Ownerの表示名")
	ttl := fs.Duration("token-ttl", bootstrap.DefaultTokenTTL, "招待tokenの有効期間")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	dsn := getenv("DATABASE_URL")
	if dsn == "" {
		fmt.Fprintln(stderr, "DATABASE_URL が未設定です")
		return 2
	}
	if *loginID == "" || *displayName == "" {
		fmt.Fprintln(stderr, "--login-id と --display-name は必須です")
		return 2
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintln(stderr, "DB設定が不正です")
		return 1
	}
	defer pool.Close()

	res, err := bootstrap.Owner(ctx, pool, bootstrap.Params{LoginID: *loginID, DisplayName: *displayName, TokenTTL: *ttl})
	switch {
	case errors.Is(err, bootstrap.ErrAlreadyBootstrapped), errors.Is(err, bootstrap.ErrOwnerExists):
		fmt.Fprintln(stderr, err.Error()+"。再実行はできません。緊急復旧は別の手順（運用文書）を使ってください。")
		return 3
	case errors.Is(err, bootstrap.ErrInvalidInput):
		fmt.Fprintln(stderr, err.Error())
		return 2
	case err != nil:
		// DSNなど内部情報を出さないため、詳細は表示しない。
		fmt.Fprintln(stderr, "作成に失敗しました。DBに接続できるか確認してください。")
		return 1
	}
	fmt.Fprintf(stdout, "初期Ownerを作成しました（ログインID: %s）。\n", *loginID)
	fmt.Fprintf(stdout, "招待token（一度だけ表示。有効期限: %s）:\n%s\n", res.ExpiresAt.Format(time.RFC3339), res.InviteToken)
	fmt.Fprintln(stdout, "このtokenでパスワードを設定します（ログイン機能はP1-02で提供）。安全な方法で本人に渡し、記録しないでください。")
	return 0
}
