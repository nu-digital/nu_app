package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nu-digital/nu_app/services/api/internal/media"
	"github.com/nu-digital/nu_app/services/api/internal/store"
)

// 画像（メディア）の運用コマンド。運用者がサーバー上で手動で実行する（GitHub Actionsのworkflowから呼んではならない）。
// 削除APIは公開していないため、孤児の掃除（media-gc）と、本人情報を含む画像の無効化（media-redact）はここで行う。
//
//	nuctl media-gc [--older-than 24h] [--apply]       参照が無く24時間以上前の画像を消す（既定はdry-run）
//	nuctl media-verify                                DBの行とファイルの食い違いを報告する（消さない）
//	nuctl media-redact --id <uuid> --reason "<理由>"   画像のファイルを消し、以後の配信・参照・公開を止める（行と参照は残す）
func runMedia(ctx context.Context, cmd string, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var older *time.Duration
	var apply *bool
	var id, reason *string
	switch cmd {
	case "media-gc":
		older = fs.Duration("older-than", 24*time.Hour, "この時間より前にアップロードされた孤児だけを対象にする")
		apply = fs.Bool("apply", false, "実際に削除する（指定しなければdry-run）")
	case "media-redact":
		id = fs.String("id", "", "無効化する画像のid")
		reason = fs.String("reason", "", "理由（必須。個人情報は書かない）")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	dsn, dir := getenv("DATABASE_URL"), getenv("MEDIA_DIR")
	if dsn == "" || dir == "" {
		fmt.Fprintln(stderr, "DATABASE_URL と MEDIA_DIR が必要です")
		return 2
	}
	if cmd == "media-gc" && *older < time.Hour {
		fmt.Fprintln(stderr, "--older-than は1時間以上にしてください（アップロード直後で参照を付ける前の画像を消さないため）")
		return 2
	}
	var target pgtype.UUID
	if cmd == "media-redact" {
		if err := target.Scan(*id); err != nil || !target.Valid || strings.TrimSpace(*reason) == "" || len(*reason) > 200 {
			fmt.Fprintln(stderr, "--id（uuid）と --reason（200文字以内）は必須です")
			return 2
		}
	}
	st, err := media.OpenStore(dir)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	defer st.Close()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintln(stderr, "DB設定が不正です")
		return 1
	}
	defer pool.Close()
	q := store.New(pool)
	switch cmd {
	case "media-gc":
		return mediaGC(ctx, q, st, time.Now().Add(-*older), *apply, stdout, stderr)
	case "media-verify":
		return mediaVerify(ctx, q, st, stdout, stderr)
	default:
		return mediaRedact(ctx, pool, q, st, target, strings.TrimSpace(*reason), getenv, stdout, stderr)
	}
}

func mediaGC(ctx context.Context, q *store.Queries, st *media.Store, before time.Time, apply bool, stdout, stderr io.Writer) int {
	ts := pgtype.Timestamptz{Time: before, Valid: true}
	if !apply {
		rows, err := q.ListOrphanMedia(ctx, ts)
		if err != nil {
			fmt.Fprintln(stderr, "孤児の一覧を取得できません")
			return 1
		}
		var bytes int64
		for _, r := range rows {
			bytes += r.ByteSize
		}
		fmt.Fprintf(stdout, "dry-run: 孤児（参照が無く、%s より前）は %d 件・%d バイト。削除するには --apply を付けてください。\n", before.Format(time.RFC3339), len(rows), bytes)
		return 0
	}
	n := 0
	for {
		// 確認と削除を1文で行う（掃除中に新しい参照が付いたものは消えない）。ファイルはコミットの後に消す
		row, err := q.DeleteOneOrphanMedia(ctx, ts)
		if err != nil {
			break // 該当なし（pgx.ErrNoRows）
		}
		if err := st.Remove(row.StorageKey); err != nil {
			fmt.Fprintf(stderr, "ファイルを消せませんでした（行は削除済み。media-verifyで確認してください）: id=%s\n", row.ID.String())
		}
		n++
	}
	tmp, _ := st.CleanTmp(24 * time.Hour)
	fmt.Fprintf(stdout, "削除しました: 孤児 %d 件、古いtmpファイル %d 件。\n", n, tmp)
	return 0
}

func mediaVerify(ctx context.Context, q *store.Queries, st *media.Store, stdout, stderr io.Writer) int {
	rows, err := q.ListAllMediaKeys(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "画像の一覧を取得できません")
		return 1
	}
	keys, strays, err := st.Keys()
	if err != nil {
		fmt.Fprintln(stderr, "保存先を読めません")
		return 1
	}
	onDisk := map[string]bool{}
	for _, k := range keys {
		onDisk[k] = true
	}
	inDB := map[string]bool{}
	problems := 0
	for _, r := range rows {
		inDB[r.StorageKey] = true
		switch {
		case !r.RedactedAt.Valid && !onDisk[r.StorageKey]:
			fmt.Fprintf(stdout, "ファイルが無い: id=%s\n", r.ID.String())
			problems++
		case r.RedactedAt.Valid && onDisk[r.StorageKey]:
			fmt.Fprintf(stdout, "無効化済みなのにファイルが残っている: id=%s\n", r.ID.String())
			problems++
		}
	}
	for _, k := range keys {
		if !inDB[k] {
			fmt.Fprintf(stdout, "行が無いファイル: %s\n", k)
			problems++
		}
	}
	for _, s := range strays {
		fmt.Fprintf(stdout, "想定外のファイル（形式に合わない名前、または通常のファイルではない）: %s\n", s)
		problems++
	}
	t, err := q.MediaTotals(ctx)
	if err == nil {
		fmt.Fprintf(stdout, "有効な画像: %d 件・%d バイト\n", t.N, t.Bytes)
	}
	if free, err := st.FreeBytes(); err == nil {
		fmt.Fprintf(stdout, "保存先の空き: %d バイト\n", free)
	}
	if problems > 0 {
		fmt.Fprintf(stdout, "食い違い: %d 件（何も削除していません）\n", problems)
		return 1
	}
	fmt.Fprintln(stdout, "食い違いはありません。")
	return 0
}

func mediaRedact(ctx context.Context, pool *pgxpool.Pool, q *store.Queries, st *media.Store, id pgtype.UUID, reason string, getenv func(string) string, stdout, stderr io.Writer) int {
	osUser := getenv("NUCTL_OS_USER")
	if osUser == "" {
		if cu, err := user.Current(); err == nil {
			osUser = cu.Username
		}
	}
	if osUser == "" {
		osUser = fmt.Sprintf("uid-%d", os.Getuid())
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "DBに接続できません")
		return 1
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tq := store.New(tx)
	key, err := tq.RedactMedia(ctx, store.RedactMediaParams{ID: id, RedactedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}})
	if err != nil {
		fmt.Fprintln(stderr, "該当する画像がありません（または既に無効化済みです）")
		return 3
	}
	summary, _ := json.Marshal(map[string]any{"reason": reason, "executedByOsUser": osUser})
	if err := tq.InsertAuditLog(ctx, store.InsertAuditLogParams{Action: "media.redacted", TargetType: pgtype.Text{String: "media", Valid: true},
		TargetID: pgtype.Text{String: id.String(), Valid: true}, SummaryJson: summary}); err != nil {
		fmt.Fprintln(stderr, "監査ログを記録できませんでした。何も変更していません。")
		return 1
	}
	if err := tx.Commit(ctx); err != nil {
		fmt.Fprintln(stderr, "記録を確定できませんでした")
		return 1
	}
	// 配信は、DBのredacted_atで先に止まっている。ファイルは確定の後に消す
	if err := st.Remove(key); err != nil {
		fmt.Fprintln(stderr, "ファイルを消せませんでした（配信は止まっています。media-verifyで確認してください）")
		return 1
	}
	fmt.Fprintf(stdout, "画像を無効化しました（id=%s）。ファイルを削除し、配信・新規参照・公開を止めました。行と参照は監査のため残しています。\n", id.String())
	return 0
}
