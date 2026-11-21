-- +goose Up
-- P3-02a: メディア（画像）。設計: docs/design/p3-media.md §2。
-- 公開かどうかは列で持たず、「公開中の版が参照しているか」で毎回導く（公開停止・再公開・複数の版の参照で戻し忘れが起きない）。

CREATE TABLE media_assets (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- CMS本文の公開URLに使う、idから導けない128bitの乱数（base64url 22文字）
    public_id   text NOT NULL,
    -- 保存先は相対キーだけ（絶対パスはAPIに出さない）。ファイルは再エンコード後のもの
    storage_key text NOT NULL,
    mime_type   text NOT NULL,
    byte_size   bigint NOT NULL,
    -- 向き補正後の寸法
    width       integer NOT NULL,
    height      integer NOT NULL,
    sha256      text NOT NULL,
    purpose     text NOT NULL,
    uploaded_by uuid NOT NULL REFERENCES users (id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    -- 本人情報を含む画像の誤アップロード・個人情報削除の依頼で、ファイルを消した印。行と参照は残し、以後は配信・新規参照・公開をしない
    redacted_at timestamptz,
    CONSTRAINT media_assets_public_id_unique UNIQUE (public_id),
    CONSTRAINT media_assets_storage_key_unique UNIQUE (storage_key),
    CONSTRAINT media_assets_public_id_valid CHECK (public_id ~ '^[A-Za-z0-9_-]{22}$'),
    CONSTRAINT media_assets_storage_key_valid CHECK (storage_key ~ '^[0-9a-f]{2}/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.(jpg|png)$'),
    CONSTRAINT media_assets_mime_valid CHECK (mime_type IN ('image/jpeg', 'image/png', 'image/webp')),
    CONSTRAINT media_assets_size_valid CHECK (byte_size > 0 AND byte_size <= 33554432),
    CONSTRAINT media_assets_dims_valid CHECK (width BETWEEN 1 AND 10000 AND height BETWEEN 1 AND 10000 AND width::bigint * height <= 20000000),
    CONSTRAINT media_assets_sha256_valid CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT media_assets_purpose_valid CHECK (purpose IN ('avatar', 'inventory', 'cms'))
);
CREATE INDEX media_assets_uploader_idx ON media_assets (uploaded_by, created_at);

CREATE TABLE media_references (
    media_id   uuid NOT NULL REFERENCES media_assets (id) ON DELETE RESTRICT,
    owner_type text NOT NULL,
    owner_id   uuid NOT NULL,
    purpose    text NOT NULL,
    PRIMARY KEY (media_id, owner_type, owner_id, purpose),
    CONSTRAINT media_references_owner_valid CHECK (owner_type IN ('content_revision')),
    CONSTRAINT media_references_purpose_valid CHECK (purpose IN ('cover', 'inline'))
);
CREATE INDEX media_references_owner_idx ON media_references (owner_type, owner_id);

-- 版は不変なので、版への参照は追記だけ（P3-02bでアバター等の差し替えを扱う種別は対象外）
CREATE TRIGGER media_references_revision_append_only BEFORE UPDATE OR DELETE ON media_references
    FOR EACH ROW WHEN (OLD.owner_type = 'content_revision') EXECUTE FUNCTION cms_forbid_modification();

ALTER TABLE content_revisions
    ADD CONSTRAINT content_revisions_cover_media_fk FOREIGN KEY (cover_media_id) REFERENCES media_assets (id) ON DELETE RESTRICT;

-- +goose Down
ALTER TABLE content_revisions DROP CONSTRAINT content_revisions_cover_media_fk;
DROP TRIGGER media_references_revision_append_only ON media_references;
DROP TABLE media_references;
DROP TABLE media_assets;
