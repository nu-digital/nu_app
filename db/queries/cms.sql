-- name: NewUUID :one
SELECT gen_random_uuid();

-- name: CreateContent :one
INSERT INTO contents (id, type, section, slug, creator_user_id, working_revision_id)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: InsertContentRevision :one
INSERT INTO content_revisions (id, content_id, revision_no, title, summary, markdown, metadata_json, date_label, date_kind, sort_date, sort_date_is_estimated, edited_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: GetContent :one
SELECT * FROM contents WHERE id = $1;

-- 書き込み系は先にこの行をロックしてから関係を読み直す（共同編集者の解除との競合を避ける）
-- name: GetContentForUpdate :one
SELECT * FROM contents WHERE id = $1 FOR UPDATE;

-- name: SetWorkingRevision :exec
UPDATE contents SET working_revision_id = $2, updated_at = $3 WHERE id = $1;

-- name: GetContentRevision :one
SELECT * FROM content_revisions WHERE id = $1;

-- name: GetContentRevisionByNo :one
SELECT * FROM content_revisions WHERE content_id = $1 AND revision_no = $2;

-- name: ListContentRevisionSummaries :many
SELECT r.revision_no, r.title, r.created_at, r.edited_by, COALESCE(p.display_name, '') AS edited_by_name
FROM content_revisions r LEFT JOIN profiles p ON p.user_id = r.edited_by
WHERE r.content_id = $1
ORDER BY r.revision_no DESC;

-- name: IsContentCollaborator :one
SELECT EXISTS (SELECT 1 FROM content_collaborators WHERE content_id = $1 AND user_id = $2);

-- name: ListContentCollaborators :many
SELECT c.user_id, COALESCE(p.display_name, '') AS display_name
FROM content_collaborators c LEFT JOIN profiles p ON p.user_id = c.user_id
WHERE c.content_id = $1
ORDER BY c.added_at, c.user_id;

-- name: DeleteContentCollaborators :exec
DELETE FROM content_collaborators WHERE content_id = $1;

-- name: InsertContentCollaborator :exec
INSERT INTO content_collaborators (content_id, user_id, added_by) VALUES ($1, $2, $3);

-- 有効（active）なユーザーのうち、指定したIDに含まれるもの
-- name: ActiveUserIDs :many
SELECT id FROM users WHERE id = ANY(@ids::uuid[]) AND status = 'active';

-- 存在するユーザー（停止・退会済みを含む）
-- name: ExistingUserIDs :many
SELECT id FROM users WHERE id = ANY(@ids::uuid[]);

-- name: InsertWorkContributor :exec
INSERT INTO work_contributors (content_id, revision_id, user_id, role_label, sort_order, added_by)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListWorkContributors :many
SELECT w.user_id, w.role_label, w.sort_order, COALESCE(p.display_name, '') AS display_name
FROM work_contributors w LEFT JOIN profiles p ON p.user_id = w.user_id
WHERE w.revision_id = $1
ORDER BY w.sort_order, w.id;

-- 新しい版へ、前の版の制作メンバーを引き継ぐ
-- name: CopyWorkContributors :exec
INSERT INTO work_contributors (content_id, revision_id, user_id, role_label, sort_order, added_by)
SELECT src.content_id, @to_revision_id::uuid, src.user_id, src.role_label, src.sort_order, src.added_by
FROM work_contributors src WHERE src.revision_id = @from_revision_id::uuid;

-- 一覧。編集できる人（Editor以上・作者・共同編集者）には作業版の値、それ以外には**公開版の値だけ**を返す
-- （公開中の記事の未公開の書き換えを、他のメンバーに見せない）。
-- name: ListContents :many
SELECT c.id, c.type, c.section, c.slug, c.updated_at, (c.published_revision_id IS NOT NULL)::boolean AS is_published,
       v.can_edit::boolean AS can_edit,
       (CASE WHEN v.can_edit THEN w.title ELSE p.title END)::text AS title,
       (CASE WHEN v.can_edit THEN w.summary ELSE p.summary END)::text AS summary,
       (CASE WHEN v.can_edit THEN w.revision_no ELSE p.revision_no END)::integer AS revision_no,
       (CASE WHEN v.can_edit AND c.published_revision_id IS NOT NULL THEN c.working_revision_id <> c.published_revision_id ELSE false END)::boolean AS has_unpublished_changes
FROM contents c
JOIN content_revisions w ON w.id = c.working_revision_id
LEFT JOIN content_revisions p ON p.id = c.published_revision_id
CROSS JOIN LATERAL (
    SELECT (@is_editor::boolean OR c.creator_user_id = @user_id
            OR EXISTS (SELECT 1 FROM content_collaborators cc WHERE cc.content_id = c.id AND cc.user_id = @user_id)) AS can_edit
) v
WHERE (v.can_edit OR c.published_revision_id IS NOT NULL)
  AND (sqlc.narg('section')::text IS NULL OR c.section = sqlc.narg('section')::text)
  AND (NOT @mine::boolean OR c.creator_user_id = @user_id
       OR EXISTS (SELECT 1 FROM content_collaborators cc WHERE cc.content_id = c.id AND cc.user_id = @user_id))
  AND (sqlc.narg('before_at')::timestamptz IS NULL OR (c.updated_at, c.id) < (sqlc.narg('before_at')::timestamptz, sqlc.narg('before_id')::uuid))
ORDER BY c.updated_at DESC, c.id DESC
LIMIT @lim;
