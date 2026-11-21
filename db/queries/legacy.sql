-- 既存記事の移行（nuctl import-legacy）と投稿者（legacy_authors。D14）

-- 同じ名前の行を取得、無ければ未確認で作る。**公開可否・紐付けには触れない**（再実行しても状態が変わらない）
-- name: UpsertLegacyAuthor :one
INSERT INTO legacy_authors (source_name) VALUES ($1)
ON CONFLICT (source_name) DO UPDATE SET source_name = EXCLUDED.source_name
RETURNING *;

-- 移行の再実行で、既存の記事と作業版・公開版を比べるための取得
-- name: GetContentBySectionSlug :one
SELECT * FROM contents WHERE section = $1 AND slug = $2;

-- 移行の入口の検査: 有効なOwnerか（ログインIDで引き、Tx内で共有ロックして読み直す）
-- name: GetActiveUserByLoginIDForShare :one
SELECT id, status FROM users WHERE login_id = $1 FOR SHARE;

-- name: CountContentImportedAudit :one
SELECT count(*)::bigint FROM audit_logs WHERE action = 'content.imported';
