package store

import (
	"context"
	"errors"
	"strings"
	"time"
)

type Reminder struct {
	ID        int64
	UserID    int64
	ChannelID int64
	Message   string
	RemindAt  time.Time
	Attempts  int
}

const (
	MaxRemindersPerUser = 10
	MaxReminderRunes    = 200
	maxReminderAttempts = 3
	reminderStuckAfter  = 10 * time.Minute
)

var ErrReminderLimit = errors.New("リマインダーが多すぎる")

// AddReminder はリマインダーを登録する（1人10件まで。過去・1年より先は呼び出し側で弾く）。
func (s *Store) AddReminder(ctx context.Context, r Reminder) (int64, error) {
	m := strings.TrimSpace(r.Message)
	if n := len([]rune(m)); n == 0 || n > MaxReminderRunes {
		return 0, errors.New("内容は1〜200文字")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	// 同じ人の同時登録で上限を超えないよう、人ごとに直列化する
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, r.UserID); err != nil {
		return 0, err
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM reminders WHERE user_id = $1 AND status <> 'failed'`, r.UserID).Scan(&n); err != nil {
		return 0, err
	}
	if n >= MaxRemindersPerUser {
		return 0, ErrReminderLimit
	}
	var id int64
	if err := tx.QueryRow(ctx, `INSERT INTO reminders (user_id, channel_id, message, remind_at) VALUES ($1,$2,$3,$4) RETURNING id`,
		r.UserID, r.ChannelID, m, r.RemindAt).Scan(&id); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

// ListReminders は、その人の未配信のリマインダー（時刻順）。
func (s *Store) ListReminders(ctx context.Context, userID int64) ([]Reminder, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, user_id, channel_id, message, remind_at, attempts FROM reminders WHERE user_id = $1 AND status <> 'failed' ORDER BY remind_at, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Reminder
	for rows.Next() {
		var r Reminder
		if err := rows.Scan(&r.ID, &r.UserID, &r.ChannelID, &r.Message, &r.RemindAt, &r.Attempts); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CancelReminder は、**本人の**リマインダーを取り消す。消せたら true。
func (s *Store) CancelReminder(ctx context.Context, id, userID int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM reminders WHERE id = $1 AND user_id = $2`, id, userID)
	return tag.RowsAffected() == 1, err
}

// ClaimDueReminders は、時刻が来たリマインダーを**原子的に確保**して返す（複数のプロセスでも二重に配信しない）。
// 配信中（sending）のまま10分たったものは、落ちたとみなして再び確保する。確保のたびに attempts が増える。
func (s *Store) ClaimDueReminders(ctx context.Context, now time.Time, limit int) ([]Reminder, error) {
	rows, err := s.pool.Query(ctx, `UPDATE reminders SET status = 'sending', claimed_at = $1, attempts = attempts + 1
	  WHERE id IN (SELECT id FROM reminders WHERE remind_at <= $1 AND (status = 'pending' OR (status = 'sending' AND claimed_at < $1 - $3::interval))
	               ORDER BY remind_at LIMIT $2 FOR UPDATE SKIP LOCKED)
	  RETURNING id, user_id, channel_id, message, remind_at, attempts`, now, limit, reminderStuckAfter.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Reminder
	for rows.Next() {
		var r Reminder
		if err := rows.Scan(&r.ID, &r.UserID, &r.ChannelID, &r.Message, &r.RemindAt, &r.Attempts); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// FinishReminder は配信の結果を反映する。成功したら消す。失敗は、回数が上限未満なら pending に戻して再送、上限なら failed にする。
func (s *Store) FinishReminder(ctx context.Context, id int64, sent bool) error {
	if sent {
		_, err := s.pool.Exec(ctx, `DELETE FROM reminders WHERE id = $1`, id)
		return err
	}
	_, err := s.pool.Exec(ctx, `UPDATE reminders SET status = CASE WHEN attempts >= $2 THEN 'failed' ELSE 'pending' END, claimed_at = NULL WHERE id = $1`, id, maxReminderAttempts)
	return err
}
