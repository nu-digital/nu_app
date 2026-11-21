-- name: NewUUID :one
SELECT gen_random_uuid();

-- name: CreateContent :one
INSERT INTO contents (id, type, section, slug, creator_user_id, working_revision_id, legacy_author_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: InsertContentRevision :one
INSERT INTO content_revisions (id, content_id, revision_no, title, summary, markdown, metadata_json, date_label, date_kind, sort_date, sort_date_is_estimated, edited_by, cover_media_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
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

-- 公開・公開停止。書き込みのTx内でEditor以上かを読み直す（降格との競合を避けるため行を共有ロックする）。
-- name: ActorRolesForShare :many
SELECT ur.role FROM user_roles ur JOIN users u ON u.id = ur.user_id
WHERE ur.user_id = $1 AND u.status = 'active'
FOR SHARE OF ur, u;

-- name: SetPublishedRevision :exec
UPDATE contents SET published_revision_id = $2, updated_at = $3 WHERE id = $1;

-- name: InsertPublicationEvent :one
INSERT INTO content_publication_events (content_id, revision_id, actor_user_id, event_type)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- 公開API。公開ポインタの版だけを返す（下書き・未公開・アーカイブ・存在しないものは同じ「行なし」）。
-- name: GetPublicContent :one
SELECT c.type, c.section, c.slug, r.id AS revision_id, r.title, r.summary, r.markdown, r.metadata_json,
       r.date_label, r.date_kind, r.sort_date, r.sort_date_is_estimated,
       COALESCE(cm.public_id, '')::text AS cover_public_id,
       -- 投稿者名は、公開が許可済み（かつ削除されていない）ときだけ。それ以外は空（=サイトは「nu_digital」）。版には依存しない
       COALESCE(CASE WHEN la.publication_state = 'permitted' AND la.erased_at IS NULL THEN la.source_name END, '')::text AS author_name
FROM contents c JOIN content_revisions r ON r.id = c.published_revision_id
LEFT JOIN media_assets cm ON cm.id = r.cover_media_id AND cm.redacted_at IS NULL
LEFT JOIN legacy_authors la ON la.id = c.legacy_author_id
WHERE c.section = $1 AND c.slug = $2 AND c.archived_at IS NULL;

-- 並び順は日付の新しい順（日付不明は最後）。カーソルは (並び用の日付, id)。
-- name: ListPublicContents :many
SELECT c.id, c.type, c.section, c.slug, r.title, r.summary, r.metadata_json,
       r.date_label, r.date_kind, r.sort_date, r.sort_date_is_estimated,
       COALESCE(r.sort_date, DATE '0001-01-01')::date AS order_date,
       COALESCE(cm.public_id, '')::text AS cover_public_id,
       COALESCE(CASE WHEN la.publication_state = 'permitted' AND la.erased_at IS NULL THEN la.source_name END, '')::text AS author_name
FROM contents c JOIN content_revisions r ON r.id = c.published_revision_id
LEFT JOIN media_assets cm ON cm.id = r.cover_media_id AND cm.redacted_at IS NULL
LEFT JOIN legacy_authors la ON la.id = c.legacy_author_id
WHERE c.archived_at IS NULL
  AND (sqlc.narg('section')::text IS NULL OR c.section = sqlc.narg('section')::text)
  AND (sqlc.narg('before_date')::date IS NULL OR (COALESCE(r.sort_date, DATE '0001-01-01'), c.id) < (sqlc.narg('before_date')::date, sqlc.narg('before_id')::uuid))
ORDER BY COALESCE(r.sort_date, DATE '0001-01-01') DESC, c.id DESC
LIMIT @lim;

-- 公開版の制作メンバー。**表示名は、公開クレジットに同意中のメンバーだけ**返す（それ以外は空文字＝匿名）。
-- 名前は版に焼き込まず、配信のたびにここで解決する（D08）。表示名はプロフィールの表示名（氏名ではない）。
-- name: ListPublicContributors :many
SELECT w.role_label,
       COALESCE(CASE WHEN pc.user_id IS NOT NULL THEN p.display_name END, '')::text AS display_name
FROM work_contributors w
LEFT JOIN public_credit_consents pc ON pc.user_id = w.user_id AND pc.withdrawn_at IS NULL
LEFT JOIN profiles p ON p.user_id = w.user_id
WHERE w.revision_id = $1
ORDER BY w.sort_order, w.id;

-- プロフィールの制作実績（公開版が本人をクレジットしている作品）
-- name: ListPublishedWorksOfUser :many
SELECT c.section, c.slug, r.title, array_agg(w.role_label ORDER BY w.sort_order, w.id)::text[] AS roles
FROM contents c
JOIN content_revisions r ON r.id = c.published_revision_id
JOIN work_contributors w ON w.revision_id = c.published_revision_id AND w.user_id = $1
WHERE c.type = 'project' AND c.archived_at IS NULL
GROUP BY c.id, c.section, c.slug, r.title, r.sort_date
ORDER BY r.sort_date DESC NULLS LAST, c.id;

-- 公開中の版（title・summary・本文・制作メンバーの役割名）に語が含まれるコンテンツを探す（管理者の確認用。D08）。
-- 語は大文字小文字を区別せず、部分一致。LIKEの記号は使わず position() で探す。
-- name: SearchPublishedContents :many
SELECT c.section::text AS section, c.slug::text AS slug, r.title::text AS title,
       (position(lower(@q::text) in lower(r.title)) > 0)::boolean AS in_title,
       (position(lower(@q::text) in lower(r.summary)) > 0)::boolean AS in_summary,
       (position(lower(@q::text) in lower(r.markdown)) > 0)::boolean AS in_markdown,
       (EXISTS (SELECT 1 FROM work_contributors w WHERE w.revision_id = r.id AND position(lower(@q::text) in lower(w.role_label)) > 0))::boolean AS in_role,
       CASE WHEN position(lower(@q::text) in lower(r.markdown)) > 0
            THEN substr(r.markdown, greatest(position(lower(@q::text) in lower(r.markdown)) - 20, 1), 60) ELSE '' END::text AS snippet
FROM contents c JOIN content_revisions r ON r.id = c.published_revision_id
WHERE position(lower(@q::text) in lower(r.title)) > 0
   OR position(lower(@q::text) in lower(r.summary)) > 0
   OR position(lower(@q::text) in lower(r.markdown)) > 0
   OR EXISTS (SELECT 1 FROM work_contributors w WHERE w.revision_id = r.id AND position(lower(@q::text) in lower(w.role_label)) > 0)
ORDER BY c.section, c.slug
LIMIT 100;
