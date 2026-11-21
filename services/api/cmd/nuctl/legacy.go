package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nu-digital/nu_app/services/api/internal/legacyimport"
)

// nuctl import-legacy: 既存サイトの記事67本をCMSへ移行する。運用者がサーバー上で手動で実行する（workflowから呼ばない）。
//
//	nuctl import-legacy --content-dir DIR --baseline-dir DIR --as <OwnerのログインID> [--public-dir DIR] [--apply] [--allow-source-changes]
//
// 既定はdry-run（同じ処理を最後まで実行してROLLBACK）。入力に違反が1本でもあれば何も書かない（終了コード3）。
// 再実行しても重複せず、既存の記事の公開状態・投稿者には触れない。本番で実行する前に、docs/OPERATIONS.mdの手順を読むこと。
func runImportLegacy(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("import-legacy", flag.ContinueOnError)
	fs.SetOutput(stderr)
	contentDir := fs.String("content-dir", "", "content/{blog,news,project}のある親ディレクトリ（apps/web/content）")
	baselineDir := fs.String("baseline-dir", "", "inventory.jsonとtakedown.jsonのあるディレクトリ（docs/legacy-baseline）")
	publicDir := fs.String("public-dir", "", "任意。静的画像のディレクトリ（apps/web/public）。指定すると画像の実在を確認する")
	as := fs.String("as", "", "記事の作成者・公開者として記録する、有効なOwnerのログインID")
	apply := fs.Bool("apply", false, "実際に書き込む（指定しなければdry-run）")
	allow := fs.Bool("allow-source-changes", false, "移行元の.mdがinventory.jsonのsha256と違っていても続ける（意図して記事を変えた場合だけ）")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	dsn := getenv("DATABASE_URL")
	if dsn == "" || *contentDir == "" || *baselineDir == "" || *as == "" {
		fmt.Fprintln(stderr, "DATABASE_URL、--content-dir、--baseline-dir、--as が必要です")
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
	rep, err := legacyimport.Run(ctx, pool, legacyimport.Options{
		ContentDir: *contentDir, BaselineDir: *baselineDir, PublicDir: *publicDir, AsLoginID: *as, OSUser: osUser,
		Apply: *apply, AllowSourceChanges: *allow,
	})
	if err != nil {
		fmt.Fprintln(stderr, "移行に失敗しました（何も書き込んでいません）: "+err.Error())
		return 1
	}
	if len(rep.Problems) > 0 {
		fmt.Fprintf(stderr, "入力に問題があります（何も書き込んでいません）: %d件\n", len(rep.Problems))
		for _, p := range rep.Problems {
			fmt.Fprintln(stderr, "  "+p)
		}
		return 3
	}
	mode := "dry-run（何も書き込んでいません。--applyで書き込みます）"
	if rep.Applied {
		mode = "適用しました"
	}
	fmt.Fprintf(stdout, "%s\n新規 %d 件・移行済みでスキップ %d 件・差分あり（上書きしません）%d 件・取り下げ一覧で除外 %d 件。投稿者（移行元）: %d → %d 件（公開可否はすべて未確認から始まります）\n",
		mode, len(rep.Created), len(rep.Skipped), len(rep.Drift), len(rep.Takedown), rep.AuthorsBefore, rep.AuthorsAfter)
	for _, d := range rep.Drift {
		fmt.Fprintln(stdout, "  差分: "+d)
	}
	return 0
}
