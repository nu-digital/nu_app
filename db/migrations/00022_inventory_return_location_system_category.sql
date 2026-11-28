-- +goose Up
-- UI/UX第2フェーズ（docs/design/ui-overhaul-2.md §2.1）: 返却場所の記録と、システムカテゴリ（その他）。前進のみ。
-- 旧コードは新しい列を無視して動く（返却の記録は location が空になるだけ）。

-- 返却時に入力した場所。追記専用の表だが、列の追加だけなので既存の行は変わらない（既存は空）。
ALTER TABLE inventory_returns ADD COLUMN location text NOT NULL DEFAULT '';
ALTER TABLE inventory_returns ADD CONSTRAINT inventory_returns_location_len CHECK (char_length(location) <= 50);

-- 削除できないカテゴリ（物品の自動の移し先「その他」）。
ALTER TABLE inventory_categories ADD COLUMN is_system boolean NOT NULL DEFAULT false;
UPDATE inventory_categories SET is_system = true WHERE name = 'その他';

-- +goose Down
ALTER TABLE inventory_categories DROP COLUMN is_system;
ALTER TABLE inventory_returns DROP CONSTRAINT inventory_returns_location_len;
ALTER TABLE inventory_returns DROP COLUMN location;
