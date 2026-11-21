-- +goose Up
-- P3-01a: CMS（記事・作品）。版は不変、公開ポインタと作業版ポインタは同じコンテンツの版だけを指す（docs/design/p3-cms.md §2）。

CREATE TABLE contents (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    type                  text NOT NULL,
    section               text NOT NULL,
    -- 作成後に変えない（公開URLの安定）。大文字・ドットを保つ（既存slugは [\w.-]+）
    slug                  text NOT NULL,
    creator_user_id       uuid NOT NULL REFERENCES users (id),
    working_revision_id   uuid NOT NULL,
    published_revision_id uuid,
    -- P3-01では未使用（アーカイブのAPIを作らない）
    archived_at           timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT contents_type_valid CHECK (type IN ('article', 'project')),
    CONSTRAINT contents_section_valid CHECK ((type = 'article' AND section IN ('blog', 'news')) OR (type = 'project' AND section = 'project')),
    CONSTRAINT contents_slug_valid CHECK (slug ~ '^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$'),
    CONSTRAINT contents_section_slug_unique UNIQUE (section, slug)
);
CREATE INDEX contents_updated_idx ON contents (updated_at DESC, id);
CREATE INDEX contents_creator_idx ON contents (creator_user_id);

CREATE TABLE content_revisions (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    content_id             uuid NOT NULL REFERENCES contents (id) DEFERRABLE INITIALLY IMMEDIATE,
    revision_no            integer NOT NULL,
    title                  text NOT NULL,
    summary                text NOT NULL DEFAULT '',
    markdown               text NOT NULL,
    -- P3-02でmediaを作るときFKを追加する（それまでは常にNULL）
    cover_media_id         uuid,
    metadata_json          jsonb NOT NULL DEFAULT '{}',
    -- 日付（D12）: 表示は常に原文(date_label)。sort_dateは並び替え用で、補った日は開催日として表示しない
    date_label             text NOT NULL DEFAULT '',
    date_kind              text NOT NULL DEFAULT 'unknown',
    sort_date              date,
    sort_date_is_estimated boolean NOT NULL DEFAULT false,
    edited_by              uuid NOT NULL REFERENCES users (id),
    created_at             timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT content_revisions_no_unique UNIQUE (content_id, revision_no),
    -- contentsの複合FK（同じコンテンツの版だけを指す）の参照先
    CONSTRAINT content_revisions_content_id_id_unique UNIQUE (content_id, id),
    CONSTRAINT content_revisions_no_valid CHECK (revision_no >= 1),
    CONSTRAINT content_revisions_title_valid CHECK (btrim(title) <> '' AND char_length(title) <= 200 AND title !~ '[\n\r]'),
    CONSTRAINT content_revisions_summary_valid CHECK (char_length(summary) <= 500),
    CONSTRAINT content_revisions_markdown_size CHECK (octet_length(markdown) <= 262144),
    CONSTRAINT content_revisions_metadata_object CHECK (jsonb_typeof(metadata_json) = 'object'),
    CONSTRAINT content_revisions_date_label_valid CHECK (char_length(date_label) <= 100),
    CONSTRAINT content_revisions_date_kind_valid CHECK (date_kind IN ('day', 'month', 'range', 'unknown')),
    CONSTRAINT content_revisions_date_consistent CHECK (
        (date_kind = 'day' AND sort_date IS NOT NULL AND NOT sort_date_is_estimated)
        OR (date_kind IN ('month', 'range') AND sort_date IS NOT NULL AND sort_date_is_estimated)
        OR (date_kind = 'unknown' AND (sort_date IS NULL OR sort_date_is_estimated))
    )
);

-- 作業版・公開版ポインタは「同じコンテンツの版」だけを指せる（複合FK）。公開前は published_revision_id IS NULL。
-- 作成時だけ、循環する参照を SET CONSTRAINTS ... DEFERRED で後から満たす。
ALTER TABLE contents
    ADD CONSTRAINT contents_working_revision_fk FOREIGN KEY (id, working_revision_id) REFERENCES content_revisions (content_id, id) DEFERRABLE INITIALLY IMMEDIATE,
    ADD CONSTRAINT contents_published_revision_fk FOREIGN KEY (id, published_revision_id) REFERENCES content_revisions (content_id, id) DEFERRABLE INITIALLY IMMEDIATE;

CREATE TABLE content_collaborators (
    content_id uuid NOT NULL REFERENCES contents (id),
    user_id    uuid NOT NULL REFERENCES users (id),
    added_by   uuid NOT NULL REFERENCES users (id),
    added_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (content_id, user_id)
);
CREATE INDEX content_collaborators_user_idx ON content_collaborators (user_id);

-- 制作メンバー（作品）。**版ごと**に持つ: 公開後の変更は新しい版になり、公開版の制作メンバーは公開操作まで変わらない。
-- 公開クレジットの同意は別（public_credit_consents）。名前は版に焼き込まず配信時に解決する（D08）。
CREATE TABLE work_contributors (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    content_id  uuid NOT NULL,
    revision_id uuid NOT NULL,
    user_id     uuid NOT NULL REFERENCES users (id),
    role_label  text NOT NULL,
    sort_order  integer NOT NULL,
    added_by    uuid NOT NULL REFERENCES users (id),
    CONSTRAINT work_contributors_revision_fk FOREIGN KEY (content_id, revision_id) REFERENCES content_revisions (content_id, id),
    CONSTRAINT work_contributors_unique UNIQUE (revision_id, user_id, role_label),
    CONSTRAINT work_contributors_role_valid CHECK (btrim(role_label) <> '' AND char_length(role_label) <= 50 AND role_label !~ '[\n\r\t@<>]' AND role_label !~* '(https?:|www\.)')
);
CREATE INDEX work_contributors_user_idx ON work_contributors (user_id);

CREATE TABLE content_publication_events (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    content_id    uuid NOT NULL,
    revision_id   uuid NOT NULL,
    actor_user_id uuid NOT NULL REFERENCES users (id),
    event_type    text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT content_publication_events_revision_fk FOREIGN KEY (content_id, revision_id) REFERENCES content_revisions (content_id, id),
    CONSTRAINT content_publication_events_type_valid CHECK (event_type IN ('publish', 'unpublish'))
);
CREATE INDEX content_publication_events_content_idx ON content_publication_events (content_id, created_at);

-- 制作メンバーは作品(project)の版だけ
-- +goose StatementBegin
CREATE FUNCTION work_contributors_only_projects() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF (SELECT type FROM contents WHERE id = NEW.content_id) <> 'project' THEN
        RAISE EXCEPTION '制作メンバーは作品にだけ登録できます' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- 版・制作メンバー・公開履歴は追記専用（更新・削除を拒否する）
CREATE FUNCTION cms_forbid_modification() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION '% は追記専用です', TG_TABLE_NAME USING ERRCODE = 'restrict_violation';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER work_contributors_projects_only BEFORE INSERT ON work_contributors
    FOR EACH ROW EXECUTE FUNCTION work_contributors_only_projects();
CREATE TRIGGER content_revisions_append_only BEFORE UPDATE OR DELETE ON content_revisions
    FOR EACH ROW EXECUTE FUNCTION cms_forbid_modification();
CREATE TRIGGER work_contributors_append_only BEFORE UPDATE OR DELETE ON work_contributors
    FOR EACH ROW EXECUTE FUNCTION cms_forbid_modification();
CREATE TRIGGER content_publication_events_append_only BEFORE UPDATE OR DELETE ON content_publication_events
    FOR EACH ROW EXECUTE FUNCTION cms_forbid_modification();

-- +goose Down
DROP TRIGGER content_publication_events_append_only ON content_publication_events;
DROP TRIGGER work_contributors_append_only ON work_contributors;
DROP TRIGGER content_revisions_append_only ON content_revisions;
DROP TRIGGER work_contributors_projects_only ON work_contributors;
DROP FUNCTION cms_forbid_modification();
DROP FUNCTION work_contributors_only_projects();
DROP TABLE content_publication_events;
DROP TABLE work_contributors;
DROP TABLE content_collaborators;
ALTER TABLE contents DROP CONSTRAINT contents_published_revision_fk, DROP CONSTRAINT contents_working_revision_fk;
DROP TABLE content_revisions;
DROP TABLE contents;
