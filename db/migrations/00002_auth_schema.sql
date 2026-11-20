-- +goose Up
-- P1-01a: 認証・権限・プロフィール・監査。docs/DATABASE.md §1・§2・§5、D02・D05。
-- ユーザー行は削除しない（活動履歴の参照整合性のため）。停止は status=disabled で表す。

CREATE TABLE users (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- ログインID（管理者が発行。本人は変更不可）。小文字英数字と . _ - 、2〜63文字。
    login_id     text NOT NULL,
    status       text NOT NULL DEFAULT 'invited',
    created_at   timestamptz NOT NULL DEFAULT now(),
    activated_at timestamptz,
    disabled_at  timestamptz,
    CONSTRAINT users_login_id_format CHECK (login_id ~ '^[a-z0-9][a-z0-9._-]{1,62}$'),
    CONSTRAINT users_login_id_unique UNIQUE (login_id),
    CONSTRAINT users_status_valid CHECK (status IN ('invited', 'active', 'disabled')),
    CONSTRAINT users_disabled_at_matches_status CHECK ((status = 'disabled') = (disabled_at IS NOT NULL))
);

CREATE TABLE password_credentials (
    user_id       uuid PRIMARY KEY REFERENCES users (id),
    -- Argon2id等のエンコード済みハッシュ。平文・可逆暗号は保存しない。
    password_hash text NOT NULL CHECK (password_hash <> ''),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users (id),
    -- セッションIDそのものは保存せずSHA-256(32バイト)のみ保存する。
    token_hash   bytea NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    revoked_at   timestamptz,
    CONSTRAINT sessions_token_hash_unique UNIQUE (token_hash),
    CONSTRAINT sessions_token_hash_length CHECK (octet_length(token_hash) = 32),
    CONSTRAINT sessions_expires_after_created CHECK (expires_at > created_at)
);
CREATE INDEX sessions_user_id_active_idx ON sessions (user_id) WHERE revoked_at IS NULL;

CREATE TABLE one_time_tokens (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users (id),
    purpose    text NOT NULL,
    token_hash bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    revoked_at timestamptz,
    -- 発行者。初期Ownerの作成CLIではNULL。
    created_by uuid REFERENCES users (id),
    CONSTRAINT one_time_tokens_purpose_valid CHECK (purpose IN ('invite', 'password_reset')),
    CONSTRAINT one_time_tokens_hash_unique UNIQUE (token_hash),
    CONSTRAINT one_time_tokens_hash_length CHECK (octet_length(token_hash) = 32),
    CONSTRAINT one_time_tokens_expires_after_created CHECK (expires_at > created_at),
    CONSTRAINT one_time_tokens_not_used_and_revoked CHECK (used_at IS NULL OR revoked_at IS NULL)
);
-- 使用も失効もされていないtokenは(user,purpose)ごとに1件。再発行時は古いものを失効させてから発行する。
CREATE UNIQUE INDEX one_time_tokens_one_active_idx ON one_time_tokens (user_id, purpose)
    WHERE used_at IS NULL AND revoked_at IS NULL;

CREATE TABLE user_roles (
    user_id     uuid NOT NULL REFERENCES users (id),
    -- Memberはactiveなユーザーの基本権限のため行を持たない。
    role        text NOT NULL,
    assigned_by uuid REFERENCES users (id),
    assigned_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, role),
    CONSTRAINT user_roles_role_valid CHECK (role IN ('owner', 'admin', 'editor'))
);

CREATE TABLE owner_transfer_requests (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    from_user_id uuid NOT NULL REFERENCES users (id),
    to_user_id   uuid NOT NULL REFERENCES users (id),
    status       text NOT NULL DEFAULT 'pending',
    requested_at timestamptz NOT NULL DEFAULT now(),
    -- 有効期限は申請から24時間（D05）。アプリ側が requested_at + 24h を設定する。
    expires_at   timestamptz NOT NULL,
    resolved_at  timestamptz,
    CONSTRAINT owner_transfer_status_valid CHECK (status IN ('pending', 'accepted', 'cancelled', 'expired')),
    CONSTRAINT owner_transfer_not_self CHECK (from_user_id <> to_user_id),
    CONSTRAINT owner_transfer_expires_after_request CHECK (expires_at > requested_at),
    CONSTRAINT owner_transfer_resolved_matches_status CHECK ((status = 'pending') = (resolved_at IS NULL))
);
-- 同じOwnerが同時に出せるpending申請は1件。
CREATE UNIQUE INDEX owner_transfer_one_pending_per_owner_idx ON owner_transfer_requests (from_user_id)
    WHERE status = 'pending';

CREATE TABLE profiles (
    user_id         uuid PRIMARY KEY REFERENCES users (id),
    -- 本人入力で必須なのは表示名のみ（D02）。他の項目は任意。
    display_name    text NOT NULL,
    bio             text,
    avatar_media_id uuid,
    faculty         text,
    department      text,
    school_year     smallint,
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT profiles_display_name_not_blank CHECK (btrim(display_name) <> '' AND char_length(display_name) <= 64),
    CONSTRAINT profiles_school_year_range CHECK (school_year IS NULL OR school_year BETWEEN 1 AND 10)
);

-- 氏名・大学メール・学生証番号・最寄り駅。本人と権限あるAdmin/Ownerだけが読む。公開設定の対象にしない。
CREATE TABLE personal_profiles (
    user_id          uuid PRIMARY KEY REFERENCES users (id),
    legal_name       text,
    student_number   text,
    university_email text,
    nearest_station  text,
    updated_at       timestamptz NOT NULL DEFAULT now()
);

-- メンバー内公開の設定（D02）。行が無い項目は非公開。個人情報4項目は指定できない。
CREATE TABLE profile_field_visibility (
    user_id    uuid NOT NULL REFERENCES users (id),
    field_key  text NOT NULL,
    visibility text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, field_key),
    CONSTRAINT profile_field_key_valid CHECK (field_key IN ('bio', 'faculty', 'department', 'school_year', 'external_links', 'devices')),
    CONSTRAINT profile_field_visibility_valid CHECK (visibility IN ('private', 'members'))
);

-- インターネット公開（公開クレジット）の同意（D02・D08）。メンバー内公開とは別。
CREATE TABLE public_credit_consents (
    user_id      uuid PRIMARY KEY REFERENCES users (id),
    granted_at   timestamptz NOT NULL,
    withdrawn_at timestamptz,
    updated_by   uuid REFERENCES users (id),
    CONSTRAINT public_credit_withdrawn_after_granted CHECK (withdrawn_at IS NULL OR withdrawn_at >= granted_at)
);

CREATE TABLE audit_logs (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_user_id uuid REFERENCES users (id),
    action        text NOT NULL CHECK (action <> ''),
    target_type   text,
    target_id     text,
    -- トークン・パスワード・学生証番号などの値は入れない。
    summary_json  jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_logs_created_at_idx ON audit_logs (created_at);

-- 緊急復旧CLIの実行記録（D05）。個人情報・秘密値は入れない。
CREATE TABLE emergency_recovery_events (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    executed_by_os_user text NOT NULL CHECK (executed_by_os_user <> ''),
    target_user_id      uuid NOT NULL REFERENCES users (id),
    approver_note       text NOT NULL CHECK (btrim(approver_note) <> ''),
    created_at          timestamptz NOT NULL DEFAULT now()
);

-- 初期Ownerの作成は1回だけ。この表は最大1行で、2回目のINSERTは主キー衝突で拒否される。
CREATE TABLE bootstrap_state (
    id           boolean PRIMARY KEY DEFAULT true CHECK (id),
    completed_at timestamptz NOT NULL DEFAULT now()
);

-- 追記専用: audit_logs と emergency_recovery_events の更新・削除を拒否する。
-- +goose StatementBegin
CREATE FUNCTION forbid_modification() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION '% は追記専用です', TG_TABLE_NAME USING ERRCODE = 'restrict_violation';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER audit_logs_append_only BEFORE UPDATE OR DELETE ON audit_logs
    FOR EACH ROW EXECUTE FUNCTION forbid_modification();
CREATE TRIGGER emergency_recovery_events_append_only BEFORE UPDATE OR DELETE ON emergency_recovery_events
    FOR EACH ROW EXECUTE FUNCTION forbid_modification();

-- +goose Down
DROP TRIGGER emergency_recovery_events_append_only ON emergency_recovery_events;
DROP TRIGGER audit_logs_append_only ON audit_logs;
DROP FUNCTION forbid_modification();
DROP TABLE bootstrap_state;
DROP TABLE emergency_recovery_events;
DROP TABLE audit_logs;
DROP TABLE public_credit_consents;
DROP TABLE profile_field_visibility;
DROP TABLE personal_profiles;
DROP TABLE profiles;
DROP TABLE owner_transfer_requests;
DROP TABLE user_roles;
DROP TABLE one_time_tokens;
DROP TABLE sessions;
DROP TABLE password_credentials;
DROP TABLE users;
