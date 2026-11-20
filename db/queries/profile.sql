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
