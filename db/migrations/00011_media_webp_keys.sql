-- +goose Up
-- 新規の画像はWebPで保存する（D06追記）。既存のjpg/pngの行・ファイルはそのまま扱えるよう、storage_keyの拡張子にwebpを加える。
ALTER TABLE media_assets DROP CONSTRAINT media_assets_storage_key_valid;
ALTER TABLE media_assets ADD CONSTRAINT media_assets_storage_key_valid
    CHECK (storage_key ~ '^[0-9a-f]{2}/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.(jpg|png|webp)$');

-- +goose Down
-- webpの行があるとjpg|pngの制約を付けられない。ロールバックの前にwebpの行を無くすこと（その場合は失敗して止まる）
ALTER TABLE media_assets DROP CONSTRAINT media_assets_storage_key_valid;
ALTER TABLE media_assets ADD CONSTRAINT media_assets_storage_key_valid
    CHECK (storage_key ~ '^[0-9a-f]{2}/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.(jpg|png)$');
