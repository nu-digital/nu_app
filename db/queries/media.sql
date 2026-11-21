-- name: InsertMediaAsset :one
INSERT INTO media_assets (id, public_id, storage_key, mime_type, byte_size, width, height, sha256, purpose, uploaded_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: GetMediaAsset :one
SELECT * FROM media_assets WHERE id = $1;

-- name: GetMediaAssetByPublicID :one
SELECT * FROM media_assets WHERE public_id = $1;

-- 利用者の直近のアップロード（1日の上限の判定）
-- name: UserMediaUsage :one
SELECT count(*)::bigint AS n, COALESCE(sum(byte_size), 0)::bigint AS bytes
FROM media_assets WHERE uploaded_by = $1 AND created_at >= $2;

-- 参照する画像の行を共有ロックして読む（孤児の掃除との競合を避ける）
-- name: LockMediaByPublicIDs :many
SELECT id, public_id, purpose, uploaded_by, redacted_at FROM media_assets WHERE public_id = ANY(@public_ids::text[]) FOR KEY SHARE;

-- name: LockMediaByIDs :many
SELECT id, public_id, purpose, uploaded_by, redacted_at FROM media_assets WHERE id = ANY(@ids::uuid[]) FOR KEY SHARE;

-- name: InsertMediaReference :exec
INSERT INTO media_references (media_id, owner_type, owner_id, purpose) VALUES ($1, 'content_revision', $2, $3);

-- 新しい版へ、前の版の画像参照を引き継ぐ
-- name: CopyMediaReferences :exec
INSERT INTO media_references (media_id, owner_type, owner_id, purpose)
SELECT src.media_id, 'content_revision', @to_revision_id::uuid, src.purpose
FROM media_references src WHERE src.owner_type = 'content_revision' AND src.owner_id = @from_revision_id::uuid;

-- name: ListRevisionMedia :many
SELECT m.id, m.public_id, m.redacted_at, r.purpose AS ref_purpose
FROM media_references r JOIN media_assets m ON m.id = r.media_id
WHERE r.owner_type = 'content_revision' AND r.owner_id = $1;

-- そのコンテンツのいずれかの版が、この画像を参照しているか
-- name: MediaReferencedByContent :one
SELECT EXISTS (
    SELECT 1 FROM media_references r JOIN content_revisions rv ON rv.id = r.owner_id
    WHERE r.owner_type = 'content_revision' AND r.media_id = $1 AND rv.content_id = $2
);

-- その画像を（いずれかの版が）参照しているコンテンツの、作者または共同編集者か（プレビューの閲覧権限）
-- name: MediaViewableViaContent :one
SELECT EXISTS (
    SELECT 1 FROM media_references r
    JOIN content_revisions rv ON rv.id = r.owner_id
    JOIN contents c ON c.id = rv.content_id
    WHERE r.owner_type = 'content_revision' AND r.media_id = @media_id
      AND (c.creator_user_id = @user_id OR EXISTS (SELECT 1 FROM content_collaborators cc WHERE cc.content_id = c.id AND cc.user_id = @user_id))
);

-- 公開配信: 公開中の版が参照している画像だけ（redacted・アーカイブは除く）
-- name: GetServablePublicMedia :one
SELECT m.* FROM media_assets m
WHERE m.public_id = $1 AND m.purpose = 'cms' AND m.redacted_at IS NULL
  AND EXISTS (
    SELECT 1 FROM media_references r JOIN contents c ON c.published_revision_id = r.owner_id
    WHERE r.media_id = m.id AND r.owner_type = 'content_revision' AND c.archived_at IS NULL
  );

-- 運用CLI ------------------------------------------------------------------------------------------
-- 孤児（参照が無く、指定時刻より前にアップロードされた画像）の一覧（dry-run用）
-- name: ListOrphanMedia :many
SELECT id, storage_key, byte_size, created_at FROM media_assets m
WHERE m.created_at < $1 AND NOT EXISTS (SELECT 1 FROM media_references r WHERE r.media_id = m.id)
ORDER BY m.created_at;

-- 孤児を1件だけ、条件つきで消す（確認と削除を1文で行い、掃除中に新しい参照が付いても整合する）。ファイルはコミットの後に消す
-- name: DeleteOneOrphanMedia :one
DELETE FROM media_assets
WHERE id = (
    SELECT m.id FROM media_assets m
    WHERE m.created_at < $1 AND NOT EXISTS (SELECT 1 FROM media_references r WHERE r.media_id = m.id)
    ORDER BY m.created_at LIMIT 1 FOR UPDATE SKIP LOCKED
)
RETURNING id, storage_key;

-- name: ListAllMediaKeys :many
SELECT id, storage_key, redacted_at, byte_size FROM media_assets ORDER BY created_at;

-- name: MediaTotals :one
SELECT count(*)::bigint AS n, COALESCE(sum(byte_size), 0)::bigint AS bytes FROM media_assets WHERE redacted_at IS NULL;

-- name: RedactMedia :one
UPDATE media_assets SET redacted_at = $2 WHERE id = $1 AND redacted_at IS NULL RETURNING storage_key;
