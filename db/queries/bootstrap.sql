-- name: ClaimBootstrap :one
-- 初期Owner作成の権利を1回だけ確保する。既に確保済みなら行を返さない(pgx.ErrNoRows)。
INSERT INTO bootstrap_state DEFAULT VALUES
ON CONFLICT DO NOTHING
RETURNING completed_at;

-- name: CountUsersWithRole :one
SELECT count(*) FROM user_roles WHERE role = $1;

-- name: CreateProfile :exec
INSERT INTO profiles (user_id, display_name) VALUES ($1, $2);

-- name: GrantRole :exec
INSERT INTO user_roles (user_id, role, assigned_by) VALUES ($1, $2, $3);

-- name: CreateOneTimeToken :exec
INSERT INTO one_time_tokens (user_id, purpose, token_hash, expires_at, created_by)
VALUES ($1, $2, $3, $4, $5);
