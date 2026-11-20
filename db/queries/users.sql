-- name: CreateUser :one
INSERT INTO users (login_id, status)
VALUES ($1, $2)
RETURNING *;

-- name: GetUserByLoginID :one
SELECT * FROM users WHERE login_id = $1;

-- name: InsertAuditLog :exec
INSERT INTO audit_logs (actor_user_id, action, target_type, target_id, summary_json)
VALUES ($1, $2, $3, $4, $5);
