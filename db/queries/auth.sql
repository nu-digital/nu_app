-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: ListUserRoles :many
SELECT role FROM user_roles WHERE user_id = $1 ORDER BY role;

-- name: GetPasswordHash :one
SELECT password_hash FROM password_credentials WHERE user_id = $1;

-- name: UpsertPasswordHash :exec
INSERT INTO password_credentials (user_id, password_hash, updated_at)
VALUES ($1, $2, now())
ON CONFLICT (user_id) DO UPDATE SET password_hash = EXCLUDED.password_hash, updated_at = now();

-- name: CreateSession :one
INSERT INTO sessions (user_id, token_hash, expires_at)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetSessionByTokenHash :one
-- セッションと所有者の状態を一度に取得する。失効・期限・停止の判定は呼び出し側(session.Service)で行う。
SELECT s.id, s.user_id, s.created_at, s.last_seen_at, s.expires_at, s.revoked_at, u.status
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = $1;

-- name: TouchSession :exec
UPDATE sessions SET last_seen_at = $2 WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeSession :exec
UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeSessionByTokenHash :exec
UPDATE sessions SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL;

-- name: RevokeAllUserSessions :execrows
UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL;

-- name: RevokeActiveTokens :execrows
-- 使用も失効もされていない未使用トークンを失効する。purposeが空文字なら全purpose。
UPDATE one_time_tokens SET revoked_at = now()
WHERE user_id = $1 AND used_at IS NULL AND revoked_at IS NULL AND ($2::text = '' OR purpose = $2);

-- name: ConsumeToken :one
-- トークンの使用を1文でアトミックに確保する。条件を満たすのは同時に1人だけ。
UPDATE one_time_tokens SET used_at = $3
WHERE token_hash = $1 AND purpose = $2 AND used_at IS NULL AND revoked_at IS NULL AND expires_at > $3
RETURNING id, user_id;

-- name: ActivateInvitedUser :execrows
UPDATE users SET status = 'active', activated_at = $2 WHERE id = $1 AND status = 'invited';

-- name: GetDisplayName :one
SELECT display_name FROM profiles WHERE user_id = $1;
