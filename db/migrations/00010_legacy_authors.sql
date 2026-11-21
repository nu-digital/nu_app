-- +goose Up
-- P3-04a（D14）: 既存記事の投稿者（移行元の情報）。アプリのユーザーとは別に管理する。
-- 名前は版（content_revisions.metadata_json）やJSONに保存しない（版は不変で消せず、個人情報削除と両立しないため）。
-- 公開APIは配信のたびにここを見て、許可済みなら名前、それ以外は「nu_digital」を返す。

CREATE TABLE legacy_authors (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- 移行元の名前。個人情報削除では匿名の記号へ置換する（erased_at）
    source_name       text NOT NULL,
    -- 公開可否（インターネット上の投稿者名の公開）。**紐付けとは独立**。既定は未確認（=公開しない）
    publication_state text NOT NULL DEFAULT 'unconfirmed',
    -- 管理者が本人の公開許可を確認・記録した者と日時（Adminであること・過去の公開・メンバー内の公開設定は根拠にしない）
    confirmed_by      uuid REFERENCES users (id),
    confirmed_at      timestamptz,
    confirm_note      text,
    withdrawn_at      timestamptz,
    -- アカウント作成後に、管理者が本人を確認して**明示的に**紐付ける（表示名の一致では紐付けない）。公開可否は変えない
    linked_user_id    uuid REFERENCES users (id),
    linked_by         uuid REFERENCES users (id),
    linked_at         timestamptz,
    erased_at         timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT legacy_authors_name_unique UNIQUE (source_name),
    CONSTRAINT legacy_authors_name_valid CHECK (btrim(source_name) <> '' AND char_length(source_name) <= 64),
    CONSTRAINT legacy_authors_state_valid CHECK (publication_state IN ('unconfirmed', 'permitted', 'withdrawn')),
    -- 公開を許可した記録（誰が・いつ）が無いまま「許可」にできない
    CONSTRAINT legacy_authors_permitted_confirmed CHECK (publication_state <> 'permitted' OR (confirmed_by IS NOT NULL AND confirmed_at IS NOT NULL)),
    CONSTRAINT legacy_authors_withdrawn_at CHECK ((publication_state = 'withdrawn') = (withdrawn_at IS NOT NULL)),
    -- 個人情報を削除した投稿者は、公開できない状態でのみ存在する
    CONSTRAINT legacy_authors_erased_withdrawn CHECK (erased_at IS NULL OR publication_state = 'withdrawn'),
    CONSTRAINT legacy_authors_link_consistent CHECK ((linked_user_id IS NULL) = (linked_at IS NULL))
);
CREATE INDEX legacy_authors_linked_user_idx ON legacy_authors (linked_user_id) WHERE linked_user_id IS NOT NULL;

ALTER TABLE contents ADD COLUMN legacy_author_id uuid REFERENCES legacy_authors (id);
CREATE INDEX contents_legacy_author_idx ON contents (legacy_author_id) WHERE legacy_author_id IS NOT NULL;

-- +goose Down
DROP INDEX contents_legacy_author_idx;
ALTER TABLE contents DROP COLUMN legacy_author_id;
DROP TABLE legacy_authors;
