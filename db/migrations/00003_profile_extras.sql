-- +goose Up
-- P2-01a: 外部リンクと個人所有機材。docs/DATABASE.md §2、D02。
-- 公開範囲は項目ごと（private=本人と管理者のみ / members=ログイン済みメンバー）。初期値は非公開。

CREATE TABLE profile_links (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users (id),
    label      text NOT NULL,
    url        text NOT NULL,
    sort_order integer NOT NULL DEFAULT 0,
    visibility text NOT NULL DEFAULT 'private',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT profile_links_label_valid CHECK (btrim(label) <> '' AND char_length(label) <= 50),
    -- http(s)のみ。javascript:等のスキームはDBでも拒否する（アプリ側でも検証する）。
    CONSTRAINT profile_links_url_valid CHECK (url ~* '^https?://[^[:space:]]+$' AND char_length(url) <= 2048),
    CONSTRAINT profile_links_visibility_valid CHECK (visibility IN ('private', 'members'))
);
CREATE INDEX profile_links_user_idx ON profile_links (user_id, sort_order, id);

CREATE TABLE personal_devices (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users (id),
    category   text NOT NULL,
    name       text NOT NULL,
    specs_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    sort_order integer NOT NULL DEFAULT 0,
    visibility text NOT NULL DEFAULT 'private',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT personal_devices_category_valid CHECK (btrim(category) <> '' AND char_length(category) <= 30),
    CONSTRAINT personal_devices_name_valid CHECK (btrim(name) <> '' AND char_length(name) <= 100),
    CONSTRAINT personal_devices_specs_object CHECK (jsonb_typeof(specs_json) = 'object' AND pg_column_size(specs_json) <= 2048),
    CONSTRAINT personal_devices_visibility_valid CHECK (visibility IN ('private', 'members'))
);
CREATE INDEX personal_devices_user_idx ON personal_devices (user_id, sort_order, id);

-- +goose Down
DROP TABLE personal_devices;
DROP TABLE profile_links;
