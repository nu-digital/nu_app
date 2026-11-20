package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nu-digital/nu_app/services/api/internal/recovery"
)

// runRecover は Owner の緊急復旧。運用責任者がサーバー上で、本人確認と承認の記録とともに実行する。
// 公開Web・GitHub Actionsのデプロイからは呼ばない。実行内容は emergency_recovery_events と監査ログに残る。
func runRecover(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("recover-owner", flag.ContinueOnError)
	fs.SetOutput(stderr)
	loginID := fs.String("login-id", "", "復旧するOwnerのログインID")
	confirm := fs.String("confirm-login-id", "", "確認のためログインIDをもう一度入力")
	note := fs.String("approver-note", "", "運用責任者の本人確認と承認の記録（必須。誰が・どう確認し・誰が承認したか）")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	dsn := getenv("DATABASE_URL")
	if dsn == "" {
		fmt.Fprintln(stderr, "DATABASE_URL が未設定です")
		return 2
	}
	if *loginID == "" || *confirm == "" || *note == "" {
		fmt.Fprintln(stderr, "--login-id・--confirm-login-id・--approver-note はすべて必須です")
		return 2
	}
	osUser := getenv("NUCTL_OS_USER")
	if osUser == "" {
		if cu, err := user.Current(); err == nil {
			osUser = cu.Username
		}
	}
	if osUser == "" {
		osUser = fmt.Sprintf("uid-%d", os.Getuid())
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintln(stderr, "DB設定が不正です")
		return 1
	}
	defer pool.Close()

	res, err := recovery.RecoverOwner(ctx, pool, recovery.Params{LoginID: *loginID, ConfirmLoginID: *confirm, ApproverNote: *note, OSUser: osUser})
	switch {
	case errors.Is(err, recovery.ErrInvalidInput):
		fmt.Fprintln(stderr, "入力が不正です。--login-id と --confirm-login-id が一致し、--approver-note が空でないか確認してください")
		return 2
	case errors.Is(err, recovery.ErrNotFound):
		fmt.Fprintln(stderr, err.Error())
		return 3
	case errors.Is(err, recovery.ErrNotAllowed):
		fmt.Fprintln(stderr, err.Error())
		return 3
	case err != nil:
		fmt.Fprintln(stderr, "復旧に失敗しました。DBに接続できるか確認してください。")
		return 1
	}
	fmt.Fprintf(stdout, "復旧しました（ログインID: %s）。既存のセッションと未使用のトークンは失効しました。\n", *loginID)
	if res.Designated {
		fmt.Fprintln(stdout, "activeなOwnerが不在だったため、このユーザーをOwnerに指名しました。")
	}
	fmt.Fprintf(stdout, "復旧トークン（一度だけ表示。有効期限: %s。用途: %s）:\n%s\n", res.ExpiresAt.Format(time.RFC3339), res.Purpose, res.Token)
	fmt.Fprintln(stdout, "このトークンでパスワードを設定します。安全な方法で本人に渡し、記録しないでください。この実行は監査ログに記録されました。")
	return 0
}
