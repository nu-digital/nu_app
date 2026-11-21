-- +goose Up
-- アバター・物品写真（P3-02b）。画像の所有者（プロフィール・物品）への参照を media_references に持つ。
-- 版への参照（content_revision）は追記専用のまま。それ以外は1つの所有者に1枚（差し替えで古い参照を消す）。
ALTER TABLE media_references DROP CONSTRAINT media_references_owner_valid;
ALTER TABLE media_references ADD CONSTRAINT media_references_owner_valid CHECK (owner_type IN ('content_revision', 'profile', 'inventory_item'));
ALTER TABLE media_references DROP CONSTRAINT media_references_purpose_valid;
ALTER TABLE media_references ADD CONSTRAINT media_references_purpose_valid CHECK (purpose IN ('cover', 'inline', 'avatar', 'photo'));
ALTER TABLE media_references ADD CONSTRAINT media_references_owner_purpose_match CHECK (
    (owner_type = 'content_revision' AND purpose IN ('cover', 'inline')) OR
    (owner_type = 'profile' AND purpose = 'avatar') OR
    (owner_type = 'inventory_item' AND purpose = 'photo'));
CREATE UNIQUE INDEX media_references_single_owner_idx ON media_references (owner_type, owner_id) WHERE owner_type <> 'content_revision';

ALTER TABLE profiles ADD CONSTRAINT profiles_avatar_media_fk FOREIGN KEY (avatar_media_id) REFERENCES media_assets (id) ON DELETE RESTRICT;
ALTER TABLE inventory_items ADD CONSTRAINT inventory_items_cover_media_fk FOREIGN KEY (cover_media_id) REFERENCES media_assets (id) ON DELETE RESTRICT;

-- +goose Down
ALTER TABLE inventory_items DROP CONSTRAINT inventory_items_cover_media_fk;
ALTER TABLE profiles DROP CONSTRAINT profiles_avatar_media_fk;
DROP INDEX media_references_single_owner_idx;
ALTER TABLE media_references DROP CONSTRAINT media_references_owner_purpose_match;
ALTER TABLE media_references DROP CONSTRAINT media_references_purpose_valid;
ALTER TABLE media_references ADD CONSTRAINT media_references_purpose_valid CHECK (purpose IN ('cover', 'inline'));
ALTER TABLE media_references DROP CONSTRAINT media_references_owner_valid;
ALTER TABLE media_references ADD CONSTRAINT media_references_owner_valid CHECK (owner_type IN ('content_revision'));
