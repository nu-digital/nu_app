-- +goose Up
-- UI/UX大改善（docs/design/ui-overhaul.md §5.1・§6）: 物品のカテゴリ管理と複数画像。前進のみ。
-- 旧コードは inventory_items.category（名前）と cover_media_id を読めるので、このmigration適用後も前の版で動く。

CREATE TABLE inventory_categories (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text NOT NULL,
    sort_order integer NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT inventory_categories_name_valid CHECK (btrim(name) <> '' AND char_length(name) <= 30),
    CONSTRAINT inventory_categories_name_unique UNIQUE (name)
);

-- 初期カテゴリ（運用責任者の指定）。
INSERT INTO inventory_categories (name, sort_order) VALUES ('機材', 10), ('書籍', 20), ('その他', 30);

-- 既存の物品に使われている文字列のカテゴリを、データを失わずにカテゴリへ移す（初期3種と同名なら再利用）。
INSERT INTO inventory_categories (name, sort_order)
SELECT category, 100 + (row_number() OVER (ORDER BY category))::int
FROM (SELECT DISTINCT btrim(category) AS category FROM inventory_items WHERE btrim(category) <> '') c
WHERE category NOT IN (SELECT name FROM inventory_categories);

ALTER TABLE inventory_items ADD COLUMN category_id uuid REFERENCES inventory_categories (id);
UPDATE inventory_items i SET category_id = c.id FROM inventory_categories c WHERE c.name = btrim(i.category);
-- カテゴリが空だった物品は「その他」に入れる（暫定。DECISIONS）。名前の列も同期する。
UPDATE inventory_items SET category_id = (SELECT id FROM inventory_categories WHERE name = 'その他'), category = 'その他' WHERE category_id IS NULL;
-- category_id は NULL可のまま: 前の版のコード（categoryの文字列だけを書く）に戻しても、物品の作成が失敗しないようにする。
-- 前後の空白があった名前は、整形したカテゴリ名にそろえる。
UPDATE inventory_items i SET category = c.name FROM inventory_categories c WHERE c.id = i.category_id AND i.category <> c.name;
CREATE INDEX inventory_items_category_idx ON inventory_items (category_id);

-- 物品の画像（複数枚。sort_order の小さい順、先頭がメイン）。cover_media_id は先頭の画像と同期する（旧コード・一覧用）。
CREATE TABLE inventory_item_images (
    item_id    uuid NOT NULL REFERENCES inventory_items (id),
    media_id   uuid NOT NULL REFERENCES media_assets (id) ON DELETE RESTRICT,
    sort_order integer NOT NULL,
    PRIMARY KEY (item_id, media_id)
);
CREATE INDEX inventory_item_images_order_idx ON inventory_item_images (item_id, sort_order);
INSERT INTO inventory_item_images (item_id, media_id, sort_order) SELECT id, cover_media_id, 0 FROM inventory_items WHERE cover_media_id IS NOT NULL;

-- media_references の「1所有者に1枚」は、物品だけ複数枚を許す（アバターは1枚のまま）。
DROP INDEX media_references_single_owner_idx;
CREATE UNIQUE INDEX media_references_single_owner_idx ON media_references (owner_type, owner_id) WHERE owner_type = 'profile';

-- +goose Down
DROP INDEX media_references_single_owner_idx;
CREATE UNIQUE INDEX media_references_single_owner_idx ON media_references (owner_type, owner_id) WHERE owner_type <> 'content_revision';
DROP TABLE inventory_item_images;
DROP INDEX inventory_items_category_idx;
ALTER TABLE inventory_items DROP COLUMN category_id;
DROP TABLE inventory_categories;
