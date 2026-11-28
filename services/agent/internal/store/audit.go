package store

import (
	"context"
	"time"
)

// 保持期間
const (
	ToolAuditRetention = 90 * 24 * time.Hour
)

// AuditTool はツール実行を監査ログに残す（誰が・どこで・どのツールを・成否）。**引数や本文は保存しない**。失敗しても呼び出し元を止めない。
func (s *Store) AuditTool(ctx context.Context, userID, channelID int64, tool string, ok bool) {
	_, _ = s.pool.Exec(ctx, `INSERT INTO tool_audit (user_id, channel_id, tool, ok) VALUES ($1,$2,$3,$4)`, userID, channelID, tool, ok)
}

// ToolDenials は、直近 since 以内に失敗（上限・拒否・エラー）したツール実行の件数（異常検知用）。
func (s *Store) ToolDenials(ctx context.Context, since time.Time) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM tool_audit WHERE NOT ok AND at >= $1`, since).Scan(&n)
	return n, err
}

// PurgeResult はDM由来データの削除件数。
type PurgeResult struct{ Messages, Notes, Reminders, Summaries, Progress int64 }

// PurgeDMData は、DMに由来するデータを削除する（冪等。DMの廃止後に残ったもの）。サーバーの通常の会話・ノート・リマインダーは消さない。
// 対象: kind='dm'のメッセージ／DMチャンネルを元にしたノート・private（DM由来）のノート／privateのリマインダー／DMチャンネルの要約・取り込み進捗。
func (s *Store) PurgeDMData(ctx context.Context) (PurgeResult, error) {
	var r PurgeResult
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return r, err
	}
	defer tx.Rollback(ctx)
	run := func(dst *int64, q string) error {
		tag, err := tx.Exec(ctx, q)
		*dst = tag.RowsAffected()
		return err
	}
	const dmCh = `(SELECT DISTINCT channel_id FROM messages WHERE kind = 'dm')`
	// 先に、DMチャンネルIDを使う削除（メッセージを消すとチャンネルIDが分からなくなる）
	if err := run(&r.Notes, `DELETE FROM notes WHERE private OR source_channel_id IN `+dmCh); err != nil {
		return r, err
	}
	if err := run(&r.Summaries, `DELETE FROM summaries WHERE channel_id IN `+dmCh); err != nil {
		return r, err
	}
	if err := run(&r.Progress, `DELETE FROM import_progress WHERE channel_id IN `+dmCh); err != nil {
		return r, err
	}
	if err := run(&r.Reminders, `DELETE FROM reminders WHERE private OR channel_id IN `+dmCh); err != nil {
		return r, err
	}
	if err := run(&r.Messages, `DELETE FROM messages WHERE kind = 'dm'`); err != nil {
		return r, err
	}
	return r, tx.Commit(ctx)
}

// Maintain は定期の掃除（監査ログの保持期間）。
func (s *Store) Maintain(ctx context.Context, now time.Time) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM tool_audit WHERE at < $1::timestamptz - $2::interval`, now, ToolAuditRetention.String())
	return err
}
