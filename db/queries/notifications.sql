-- name: GetNotificationSetting :one
SELECT * FROM notification_settings WHERE event_type = $1;

-- name: ListNotificationSettings :many
SELECT * FROM notification_settings ORDER BY event_type;

-- name: UpdateNotificationSetting :one
UPDATE notification_settings
SET enabled = $2, discord_channel_id = $3, updated_by = $4, updated_at = now()
WHERE event_type = $1
RETURNING *;

-- 設定が有効なときだけ積む。同じdedupe_keyは積まない（再実行で二重にならない）。
-- name: EnqueueNotification :execrows
INSERT INTO notification_outbox (event_type, dedupe_key, payload_json)
SELECT s.event_type, @dedupe_key, @payload_json
FROM notification_settings s
WHERE s.event_type = @event_type AND s.enabled
ON CONFLICT (dedupe_key) DO NOTHING;

-- name: ListNotificationDeliveries :many
SELECT id, event_type, status, attempts, next_attempt_at, last_error, created_at, sent_at
FROM notification_outbox
WHERE (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
ORDER BY created_at DESC, id DESC
LIMIT $1;

-- 送信に失敗して止まったもの(dead)だけを再送待ちに戻す
-- name: RetryNotificationDelivery :execrows
UPDATE notification_outbox
SET status = 'pending', attempts = 0, next_attempt_at = now(), last_error = NULL
WHERE id = $1 AND status = 'dead';

-- 送信対象を1件だけ確保する（複数workerでも同じ行を同時に取らない）。
-- 設定が有効でチャンネルが設定されている種別だけ。確保した時点で試行回数を1増やし、次回試行をリース期限へ進める
-- （workerが送信の途中で落ちても、期限後に再び取り出される＝少なくとも1回送る。重複は通知IDで調べられる）。
-- name: ClaimDueNotification :one
UPDATE notification_outbox o
SET attempts = o.attempts + 1, next_attempt_at = @lease_until
FROM notification_settings s
WHERE s.event_type = o.event_type
  AND o.id = (
    SELECT o2.id FROM notification_outbox o2
    JOIN notification_settings s2 ON s2.event_type = o2.event_type AND s2.enabled AND s2.discord_channel_id IS NOT NULL
    WHERE o2.status = 'pending' AND o2.next_attempt_at <= @now
    ORDER BY o2.next_attempt_at, o2.created_at, o2.id
    LIMIT 1
    FOR UPDATE OF o2 SKIP LOCKED
  )
RETURNING o.id, o.event_type, o.payload_json, o.attempts, s.discord_channel_id::text AS channel_id;

-- name: MarkNotificationSent :execrows
UPDATE notification_outbox SET status = 'sent', sent_at = now(), last_error = NULL WHERE id = $1 AND status = 'pending';

-- 失敗して再試行を待つ。refund_attempt=trueなら試行回数を戻す（429は送信失敗ではなく待ちの指示のため）。
-- name: RescheduleNotification :execrows
UPDATE notification_outbox
SET next_attempt_at = @next_attempt_at, last_error = @last_error,
    attempts = attempts - CASE WHEN @refund_attempt::boolean THEN 1 ELSE 0 END
WHERE id = @id AND status = 'pending';

-- name: MarkNotificationDead :execrows
UPDATE notification_outbox SET status = 'dead', last_error = $2 WHERE id = $1 AND status = 'pending';
