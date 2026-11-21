-- name: CreateInventoryItem :one
INSERT INTO inventory_items (name, description, category, location, mode, total_quantity)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetInventoryItemForUpdate :one
SELECT * FROM inventory_items WHERE id = $1 FOR UPDATE;

-- name: GetInventoryItem :one
SELECT * FROM inventory_items WHERE id = $1;

-- name: UpdateInventoryItem :one
UPDATE inventory_items
SET name = $2, description = $3, category = $4, location = $5, total_quantity = $6, archived_at = $7, mode = $8, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ItemOutstanding :one
-- 物品の貸出中の数量（貸出数の合計 − 返却数の合計）。呼び出し側が物品行をロックしてから使う。
SELECT (COALESCE((SELECT SUM(l.quantity) FROM inventory_loans l WHERE l.item_id = $1), 0)
      - COALESCE((SELECT SUM(r.quantity) FROM inventory_returns r JOIN inventory_loans l ON l.id = r.loan_id WHERE l.item_id = $1), 0))::bigint;

-- name: ItemLoanCount :one
SELECT count(*) FROM inventory_loans WHERE item_id = $1;

-- name: InsertInventoryAdjustment :exec
INSERT INTO inventory_adjustments (item_id, actor_user_id, before_quantity, after_quantity, reason)
VALUES ($1, $2, $3, $4, $5);

-- name: CreateInventoryUnit :one
INSERT INTO inventory_units (item_id, asset_tag, serial_number)
VALUES ($1, $2, $3)
RETURNING *;

-- name: CountItemUnits :one
SELECT count(*) FROM inventory_units WHERE item_id = $1 AND state = 'active';

-- name: ListItemUnits :many
SELECT u.id, u.asset_tag, u.serial_number, u.state, u.current_loan_id,
       l.borrowed_at AS loaned_at, COALESCE(p.display_name, '') AS borrower_display_name
FROM inventory_units u
LEFT JOIN inventory_loans l ON l.id = u.current_loan_id
LEFT JOIN profiles p ON p.user_id = l.borrower_user_id
WHERE u.item_id = $1
ORDER BY u.asset_tag, u.id;

-- name: ListItemLoansOutstanding :many
-- 数量管理の物品の貸出中の一覧（貸出ごとの未返却数）。
SELECT l.id, l.quantity, l.borrowed_at, l.borrower_user_id, COALESCE(p.display_name, '') AS borrower_display_name,
       (l.quantity - COALESCE((SELECT SUM(r.quantity) FROM inventory_returns r WHERE r.loan_id = l.id), 0))::int AS outstanding
FROM inventory_loans l
LEFT JOIN profiles p ON p.user_id = l.borrower_user_id
WHERE l.item_id = $1 AND l.unit_id IS NULL
  AND l.quantity > COALESCE((SELECT SUM(r.quantity) FROM inventory_returns r WHERE r.loan_id = l.id), 0)
ORDER BY l.borrowed_at, l.id;

-- name: ListInventoryItems :many
-- 一覧。残数(数量管理)・利用可能な個体数(個体管理)を導出して返す。廃止した物品は含めない。
WITH base AS (
  SELECT i.*,
    (COALESCE((SELECT SUM(l.quantity) FROM inventory_loans l WHERE l.item_id = i.id), 0)
     - COALESCE((SELECT SUM(r.quantity) FROM inventory_returns r JOIN inventory_loans l ON l.id = r.loan_id WHERE l.item_id = i.id), 0))::bigint AS outstanding,
    (SELECT count(*) FROM inventory_units u WHERE u.item_id = i.id AND u.state = 'active')::bigint AS unit_count,
    (SELECT count(*) FROM inventory_units u WHERE u.item_id = i.id AND u.state = 'active' AND u.current_loan_id IS NULL)::bigint AS units_available
  FROM inventory_items i
  WHERE i.archived_at IS NULL
)
SELECT *,
  (CASE WHEN mode = 'quantity' THEN total_quantity - outstanding ELSE units_available END)::bigint AS available
FROM base
WHERE ($1::text = '' OR name ILIKE '%' || $1 || '%' ESCAPE '\' OR category ILIKE '%' || $1 || '%' ESCAPE '\' OR location ILIKE '%' || $1 || '%' ESCAPE '\' OR description ILIKE '%' || $1 || '%' ESCAPE '\')
  AND ($2::text = '' OR category = $2)
  AND (NOT $3::bool OR (CASE WHEN mode = 'quantity' THEN total_quantity - outstanding ELSE units_available END) > 0)
  AND (NOT $4::bool OR (name, id) > ($5::text, $6::uuid))
ORDER BY name, id
LIMIT $7;

-- name: ListInventoryCategories :many
SELECT DISTINCT category FROM inventory_items WHERE archived_at IS NULL AND category <> '' ORDER BY category;
