-- name: GetProfile :one
SELECT * FROM profiles WHERE user_id = $1;

-- name: UpdateProfileFields :exec
UPDATE profiles SET display_name = $2, bio = $3, faculty = $4, department = $5, school_year = $6, updated_at = now()
WHERE user_id = $1;

-- name: GetPersonalProfile :one
SELECT * FROM personal_profiles WHERE user_id = $1;

-- name: UpsertPersonalProfile :exec
INSERT INTO personal_profiles (user_id, legal_name, student_number, university_email, nearest_station, updated_at)
VALUES ($1, $2, $3, $4, $5, now())
ON CONFLICT (user_id) DO UPDATE
SET legal_name = EXCLUDED.legal_name, student_number = EXCLUDED.student_number,
    university_email = EXCLUDED.university_email, nearest_station = EXCLUDED.nearest_station, updated_at = now();

-- name: ListFieldVisibility :many
SELECT field_key, visibility FROM profile_field_visibility WHERE user_id = $1;

-- name: UpsertFieldVisibility :exec
INSERT INTO profile_field_visibility (user_id, field_key, visibility, updated_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (user_id, field_key) DO UPDATE SET visibility = EXCLUDED.visibility, updated_at = now();

-- name: ListProfileLinks :many
SELECT * FROM profile_links WHERE user_id = $1 ORDER BY sort_order, id;

-- name: CountProfileLinks :one
SELECT count(*) FROM profile_links WHERE user_id = $1;

-- name: CreateProfileLink :one
INSERT INTO profile_links (user_id, label, url, sort_order, visibility)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetProfileLinkForUpdate :one
SELECT * FROM profile_links WHERE id = $1 AND user_id = $2 FOR UPDATE;

-- name: UpdateProfileLink :one
UPDATE profile_links SET label = $3, url = $4, sort_order = $5, visibility = $6, updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: DeleteProfileLink :execrows
DELETE FROM profile_links WHERE id = $1 AND user_id = $2;

-- name: ListPersonalDevices :many
SELECT * FROM personal_devices WHERE user_id = $1 ORDER BY sort_order, id;

-- name: CountPersonalDevices :one
SELECT count(*) FROM personal_devices WHERE user_id = $1;

-- name: CreatePersonalDevice :one
INSERT INTO personal_devices (user_id, category, name, specs_json, sort_order, visibility)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetPersonalDeviceForUpdate :one
SELECT * FROM personal_devices WHERE id = $1 AND user_id = $2 FOR UPDATE;

-- name: UpdatePersonalDevice :one
UPDATE personal_devices SET category = $3, name = $4, specs_json = $5, sort_order = $6, visibility = $7, updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: DeletePersonalDevice :execrows
DELETE FROM personal_devices WHERE id = $1 AND user_id = $2;

-- name: GetPublicCreditConsent :one
SELECT * FROM public_credit_consents WHERE user_id = $1;

-- name: GrantPublicCreditConsent :exec
INSERT INTO public_credit_consents (user_id, granted_at, withdrawn_at, updated_by)
VALUES ($1, $2, NULL, $1)
ON CONFLICT (user_id) DO UPDATE SET granted_at = EXCLUDED.granted_at, withdrawn_at = NULL, updated_by = EXCLUDED.updated_by;

-- name: WithdrawPublicCreditConsent :execrows
UPDATE public_credit_consents SET withdrawn_at = $2, updated_by = $1
WHERE user_id = $1 AND withdrawn_at IS NULL;

-- name: ListMembers :many
-- 有効(active)なユーザーだけ。メンバー内公開(members)にした項目だけを返す（それ以外はNULL）。ログインID・個人情報は返さない。
SELECT u.id, p.display_name,
       p.bio AS bio, (vb.field_key IS NOT NULL)::bool AS bio_public,
       p.faculty AS faculty, (vf.field_key IS NOT NULL)::bool AS faculty_public,
       p.department AS department, (vd.field_key IS NOT NULL)::bool AS department_public,
       p.school_year AS school_year, (vy.field_key IS NOT NULL)::bool AS school_year_public
FROM users u
JOIN profiles p ON p.user_id = u.id
LEFT JOIN profile_field_visibility vb ON vb.user_id = u.id AND vb.field_key = 'bio' AND vb.visibility = 'members'
LEFT JOIN profile_field_visibility vf ON vf.user_id = u.id AND vf.field_key = 'faculty' AND vf.visibility = 'members'
LEFT JOIN profile_field_visibility vd ON vd.user_id = u.id AND vd.field_key = 'department' AND vd.visibility = 'members'
LEFT JOIN profile_field_visibility vy ON vy.user_id = u.id AND vy.field_key = 'school_year' AND vy.visibility = 'members'
WHERE u.status = 'active'
  AND ($1::text = '' OR p.display_name ILIKE '%' || $1 || '%' ESCAPE '\')
  AND (NOT $5::bool OR (p.display_name, u.id) > ($2::text, $3::uuid))
ORDER BY p.display_name, u.id
LIMIT $4;

-- name: GetMember :one
SELECT u.id, p.display_name,
       p.bio AS bio, (vb.field_key IS NOT NULL)::bool AS bio_public,
       p.faculty AS faculty, (vf.field_key IS NOT NULL)::bool AS faculty_public,
       p.department AS department, (vd.field_key IS NOT NULL)::bool AS department_public,
       p.school_year AS school_year, (vy.field_key IS NOT NULL)::bool AS school_year_public
FROM users u
JOIN profiles p ON p.user_id = u.id
LEFT JOIN profile_field_visibility vb ON vb.user_id = u.id AND vb.field_key = 'bio' AND vb.visibility = 'members'
LEFT JOIN profile_field_visibility vf ON vf.user_id = u.id AND vf.field_key = 'faculty' AND vf.visibility = 'members'
LEFT JOIN profile_field_visibility vd ON vd.user_id = u.id AND vd.field_key = 'department' AND vd.visibility = 'members'
LEFT JOIN profile_field_visibility vy ON vy.user_id = u.id AND vy.field_key = 'school_year' AND vy.visibility = 'members'
WHERE u.id = $1 AND u.status = 'active';

-- name: ListMemberLinks :many
SELECT id, label, url, sort_order FROM profile_links WHERE user_id = $1 AND visibility = 'members' ORDER BY sort_order, id;

-- name: ListMemberDevices :many
SELECT id, category, name, specs_json, sort_order FROM personal_devices WHERE user_id = $1 AND visibility = 'members' ORDER BY sort_order, id;

-- name: ClearPersonalProfile :execrows
UPDATE personal_profiles SET legal_name = NULL, student_number = NULL, university_email = NULL, nearest_station = NULL, updated_at = now() WHERE user_id = $1;

-- name: ClearProfileExtras :exec
UPDATE profiles SET bio = NULL, faculty = NULL, department = NULL, school_year = NULL, updated_at = now() WHERE user_id = $1;

-- name: DeleteAllProfileLinks :execrows
DELETE FROM profile_links WHERE user_id = $1;

-- name: DeleteAllPersonalDevices :execrows
DELETE FROM personal_devices WHERE user_id = $1;

-- name: DeleteAllFieldVisibility :execrows
DELETE FROM profile_field_visibility WHERE user_id = $1;

-- name: SetDisplayName :exec
UPDATE profiles SET display_name = $2, updated_at = now() WHERE user_id = $1;

-- name: WithdrawPublicCreditConsentByAdmin :execrows
-- 管理者による個人情報削除に伴う撤回（対象と操作者が別）。
UPDATE public_credit_consents SET withdrawn_at = $3, updated_by = $2
WHERE user_id = $1 AND withdrawn_at IS NULL;
