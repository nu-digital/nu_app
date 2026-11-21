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

-- name: ListLegacyAuthors :many
-- 管理者向けの確認一覧。名前は管理者にだけ返す（公開APIには出さない）。
SELECT a.id, a.source_name, a.publication_state, a.confirmed_at, a.confirm_note, a.withdrawn_at, a.erased_at, a.created_at,
       a.linked_user_id, a.linked_at,
       COALESCE((SELECT p.display_name FROM profiles p WHERE p.user_id = a.linked_user_id), '')::text AS linked_display_name,
       (SELECT count(*) FROM contents c WHERE c.legacy_author_id = a.id)::int AS content_count
FROM legacy_authors a
ORDER BY (a.publication_state = 'unconfirmed') DESC, a.created_at, a.id;

-- name: GetLegacyAuthorForUpdate :one
SELECT * FROM legacy_authors WHERE id = $1 FOR UPDATE;

-- name: PermitLegacyAuthor :execrows
-- 管理者が本人の公開許可を確認して記録する（削除済みは不可）。紐付けは変えない。
UPDATE legacy_authors
SET publication_state = 'permitted', confirmed_by = $2, confirmed_at = $3, confirm_note = $4, withdrawn_at = NULL
WHERE id = $1 AND erased_at IS NULL;

-- name: WithdrawLegacyAuthor :execrows
UPDATE legacy_authors SET publication_state = 'withdrawn', withdrawn_at = $2
WHERE id = $1 AND publication_state <> 'withdrawn';

-- name: LinkLegacyAuthor :execrows
-- ユーザーIDを明示して紐付ける。公開可否は変えない。
UPDATE legacy_authors SET linked_user_id = $2, linked_by = $3, linked_at = $4 WHERE id = $1;

-- name: UnlinkLegacyAuthor :execrows
UPDATE legacy_authors SET linked_user_id = NULL, linked_by = NULL, linked_at = NULL WHERE id = $1;

-- name: WithdrawLegacyAuthorsOfUser :many
-- 公開クレジット同意の撤回に連動して、そのユーザーに紐付く投稿者の公開を止める。
UPDATE legacy_authors SET publication_state = 'withdrawn', withdrawn_at = $2
WHERE linked_user_id = $1 AND publication_state <> 'withdrawn'
RETURNING id;

-- name: EraseLegacyAuthorsOfUser :many
-- 個人情報の削除に連動して、紐付く投稿者の名前を匿名の記号へ置換し公開不能にする（紐付けは残す）。
UPDATE legacy_authors
SET source_name = 'erased-' || id::text, erased_at = $2,
    publication_state = 'withdrawn', withdrawn_at = COALESCE(withdrawn_at, $2)
WHERE linked_user_id = $1 AND erased_at IS NULL
RETURNING id;
