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
