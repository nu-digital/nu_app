-- name: CreateInventoryItem :one
INSERT INTO inventory_items (name, description, category, category_id, location, mode, total_quantity)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetInventoryItemForUpdate :one
SELECT * FROM inventory_items WHERE id = $1 FOR UPDATE;

-- name: GetInventoryItem :one
SELECT * FROM inventory_items WHERE id = $1;

-- name: UpdateInventoryItem :one
UPDATE inventory_items
SET name = $2, description = $3, category = $4, category_id = $9, location = $5, total_quantity = $6, archived_at = $7, mode = $8, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ItemOutstanding :one
-- 物品の貸出中の数量（貸出数の合計 − 返却数の合計）。呼び出し側が物品行をロックしてから使う。
SELECT (COALESCE((SELECT SUM(l.quantity) FROM inventory_loans_eff l WHERE l.item_id = $1), 0)
      - COALESCE((SELECT SUM(r.quantity) FROM inventory_returns_eff r JOIN inventory_loans_eff l ON l.id = r.loan_id WHERE l.item_id = $1), 0))::bigint;

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
SELECT u.id, u.asset_tag, u.serial_number, u.state, u.current_loan_id, l.borrower_user_id,
       l.borrowed_at AS loaned_at, COALESCE(p.display_name, '') AS borrower_display_name
FROM inventory_units u
LEFT JOIN inventory_loans l ON l.id = u.current_loan_id
LEFT JOIN profiles p ON p.user_id = l.borrower_user_id
WHERE u.item_id = $1
ORDER BY u.asset_tag, u.id;

-- name: ListItemLoansOutstanding :many
-- 数量管理の物品の貸出中の一覧（貸出ごとの未返却数）。
SELECT l.id, l.quantity, l.borrowed_at, l.borrower_user_id, COALESCE(p.display_name, '') AS borrower_display_name,
       (l.quantity - COALESCE((SELECT SUM(r.quantity) FROM inventory_returns_eff r WHERE r.loan_id = l.id), 0))::int AS outstanding
FROM inventory_loans_eff l
LEFT JOIN profiles p ON p.user_id = l.borrower_user_id
WHERE l.item_id = $1 AND l.unit_id IS NULL
  AND l.quantity > COALESCE((SELECT SUM(r.quantity) FROM inventory_returns_eff r WHERE r.loan_id = l.id), 0)
ORDER BY l.borrowed_at, l.id;

-- name: ListInventoryItems :many
-- 一覧。残数(数量管理)・利用可能な個体数(個体管理)を導出して返す。廃止した物品は含めない。
-- mine = 自分が借りている未返却数。並び替え: name（既定）・newest・available（借りられる数の多い順）。カーソルは件数のoffset。
WITH base AS (
  SELECT i.*,
    (COALESCE((SELECT SUM(l.quantity) FROM inventory_loans_eff l WHERE l.item_id = i.id), 0)
     - COALESCE((SELECT SUM(r.quantity) FROM inventory_returns_eff r JOIN inventory_loans_eff l ON l.id = r.loan_id WHERE l.item_id = i.id), 0))::bigint AS outstanding,
    (SELECT count(*) FROM inventory_units u WHERE u.item_id = i.id AND u.state = 'active')::bigint AS unit_count,
    (SELECT count(*) FROM inventory_units u WHERE u.item_id = i.id AND u.state = 'active' AND u.current_loan_id IS NULL)::bigint AS units_available,
    (SELECT COALESCE(SUM(l.quantity - COALESCE((SELECT SUM(r.quantity) FROM inventory_returns_eff r WHERE r.loan_id = l.id), 0)), 0)
       FROM inventory_loans_eff l WHERE l.item_id = i.id AND l.borrower_user_id = @user_id::uuid)::bigint AS mine
  FROM inventory_items i
  WHERE i.archived_at IS NULL
), withavail AS (
  SELECT *, (CASE WHEN mode = 'quantity' THEN total_quantity - outstanding ELSE units_available END)::bigint AS available FROM base
)
SELECT * FROM withavail
WHERE (@q::text = '' OR name ILIKE '%' || @q || '%' ESCAPE '\' OR category ILIKE '%' || @q || '%' ESCAPE '\' OR location ILIKE '%' || @q || '%' ESCAPE '\' OR description ILIKE '%' || @q || '%' ESCAPE '\')
  AND (@category::text = '' OR category = @category)
  AND (@category_id::uuid IS NULL OR category_id = @category_id::uuid)
  AND (NOT @only_available::bool OR available > 0)
  AND (NOT @only_mine::bool OR mine > 0)
  AND (NOT @only_loaned::bool OR outstanding > 0)
ORDER BY (CASE WHEN @sort::text = 'available' THEN available END) DESC NULLS LAST,
         (CASE WHEN @sort::text = 'newest' THEN created_at END) DESC NULLS LAST,
         name, id
OFFSET @row_offset::int LIMIT @row_limit::int;

-- name: ListInventoryCategories :many
SELECT DISTINCT category FROM inventory_items WHERE archived_at IS NULL AND category <> '' ORDER BY category;

-- カテゴリ管理（UI/UX大改善。00021）
-- name: ListCategories :many
SELECT c.id, c.name, c.sort_order, c.is_system,
       (SELECT count(*) FROM inventory_items i WHERE i.category_id = c.id AND i.archived_at IS NULL)::bigint AS item_count
FROM inventory_categories c ORDER BY c.sort_order, c.name;

-- name: GetCategory :one
SELECT * FROM inventory_categories WHERE id = $1;

-- name: GetCategoryByName :one
SELECT * FROM inventory_categories WHERE name = $1;

-- name: GetCategoryForUpdate :one
SELECT * FROM inventory_categories WHERE id = $1 FOR UPDATE;

-- name: LockCategories :exec
-- カテゴリの追加・並び替え・削除を直列化する（表全体を共有しない小さな表なので、全行をロックする）。
SELECT id FROM inventory_categories ORDER BY id FOR UPDATE;

-- name: CreateCategory :one
INSERT INTO inventory_categories (name, sort_order)
VALUES ($1, COALESCE((SELECT MAX(sort_order) FROM inventory_categories), 0) + 10)
RETURNING *;

-- name: RenameCategory :one
UPDATE inventory_categories SET name = $2 WHERE id = $1 RETURNING *;

-- name: SetCategoryOrder :exec
UPDATE inventory_categories SET sort_order = $2 WHERE id = $1;

-- 物品の（旧コード向けの）カテゴリ名の列を、カテゴリの名前に同期する
-- name: SyncItemCategoryName :exec
UPDATE inventory_items SET category = $2 WHERE category_id = $1;

-- name: CountItemsInCategory :one
-- 使用中の物品の数（廃止した物品は数えない）。
SELECT count(*)::bigint FROM inventory_items WHERE category_id = $1 AND archived_at IS NULL;

-- name: MoveItemsToCategory :exec
-- 廃止した物品も移す（外部キーのため。貸出の履歴は物品のIDで残るので影響しない）。
UPDATE inventory_items SET category_id = $2, category = $3, updated_at = now() WHERE category_id = $1;

-- name: CountAllItemsInCategory :one
-- 廃止した物品を含む（カテゴリを消す前に、外部キーで残る物品がないかを見る）。
SELECT count(*)::bigint FROM inventory_items WHERE category_id = $1;

-- name: GetSystemCategory :one
SELECT * FROM inventory_categories WHERE is_system ORDER BY sort_order LIMIT 1;

-- name: MarkCategorySystem :exec
UPDATE inventory_categories SET is_system = true WHERE id = $1;

-- name: UpdateItemLocation :exec
UPDATE inventory_items SET location = $2, updated_at = now() WHERE id = $1;

-- name: DeleteCategory :exec
DELETE FROM inventory_categories WHERE id = $1;

-- 物品の画像（複数枚）
-- name: ListItemImages :many
SELECT media_id FROM inventory_item_images WHERE item_id = $1 ORDER BY sort_order, media_id;

-- name: ListImagesOfItems :many
SELECT item_id, media_id FROM inventory_item_images WHERE item_id = ANY(@ids::uuid[]) ORDER BY item_id, sort_order, media_id;

-- name: DeleteItemImages :exec
DELETE FROM inventory_item_images WHERE item_id = $1;

-- name: InsertItemImage :exec
INSERT INTO inventory_item_images (item_id, media_id, sort_order) VALUES ($1, $2, $3);

-- name: GetInventoryLoan :one
SELECT * FROM inventory_loans WHERE id = $1;

-- name: LoanOutstanding :one
-- 貸出ごとの未返却数。
SELECT (l.quantity - COALESCE((SELECT SUM(r.quantity) FROM inventory_returns_eff r WHERE r.loan_id = l.id), 0))::int
FROM inventory_loans_eff l WHERE l.id = $1;

-- name: CreateInventoryLoan :one
INSERT INTO inventory_loans (item_id, unit_id, borrower_user_id, quantity, borrowed_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetInventoryUnitForUpdate :one
SELECT * FROM inventory_units WHERE id = $1 AND item_id = $2 FOR UPDATE;

-- name: SetUnitLoan :execrows
-- 貸出中でない有効な個体にだけ貸出IDを付ける（0行なら貸出できない）。
UPDATE inventory_units SET current_loan_id = $3 WHERE id = $1 AND item_id = $2 AND current_loan_id IS NULL AND state = 'active';

-- name: ClearUnitLoan :exec
UPDATE inventory_units SET current_loan_id = NULL WHERE current_loan_id = $1;

-- name: CreateInventoryReturn :one
INSERT INTO inventory_returns (loan_id, actor_user_id, quantity, returned_at, location)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListOutstandingLoans :many
-- 貸出中（未返却数>0）の一覧。古い順。
SELECT l.id, l.item_id, i.name AS item_name, l.unit_id, COALESCE(un.asset_tag, '') AS asset_tag,
       l.borrower_user_id, COALESCE(p.display_name, '') AS borrower_display_name, l.borrowed_at,
       (l.quantity - COALESCE((SELECT SUM(r.quantity) FROM inventory_returns_eff r WHERE r.loan_id = l.id), 0))::int AS outstanding
FROM inventory_loans_eff l
JOIN inventory_items i ON i.id = l.item_id
LEFT JOIN inventory_units un ON un.id = l.unit_id
LEFT JOIN profiles p ON p.user_id = l.borrower_user_id
WHERE l.quantity > COALESCE((SELECT SUM(r.quantity) FROM inventory_returns_eff r WHERE r.loan_id = l.id), 0)
  AND (NOT $1::bool OR l.borrower_user_id = $2::uuid)
  AND ($3::text = '' OR i.name ILIKE '%' || $3 || '%' ESCAPE '\' OR p.display_name ILIKE '%' || $3 || '%' ESCAPE '\')
  AND ($4::uuid IS NULL OR l.item_id = $4::uuid)
  AND (NOT $5::bool OR (l.borrowed_at, l.id) > ($6::timestamptz, $7::uuid))
ORDER BY l.borrowed_at, l.id
LIMIT $8;

-- name: ListInventoryHistory :many
-- 貸出と返却の履歴（新しい順）。借りた人(borrower)と返却した人(actor)を別に返す。
SELECT * FROM (
  SELECT l.id AS event_id, 'loan'::text AS kind, l.item_id, i.name AS item_name, COALESCE(un.asset_tag, '') AS asset_tag,
         COALESCE(pb.display_name, '') AS borrower_display_name, COALESCE(pb.display_name, '') AS actor_display_name,
         l.quantity, l.borrowed_at AS at, ''::text AS correction_kind, ''::text AS target_type, ''::text AS reason, ''::text AS location
  FROM inventory_loans l
  JOIN inventory_items i ON i.id = l.item_id
  LEFT JOIN inventory_units un ON un.id = l.unit_id
  LEFT JOIN profiles pb ON pb.user_id = l.borrower_user_id
  UNION ALL
  SELECT r.id, 'return'::text, l.item_id, i.name, COALESCE(un.asset_tag, ''),
         COALESCE(pb.display_name, ''), COALESCE(pa.display_name, ''), r.quantity, r.returned_at, ''::text, ''::text, ''::text, r.location
  FROM inventory_returns r
  JOIN inventory_loans l ON l.id = r.loan_id
  JOIN inventory_items i ON i.id = l.item_id
  LEFT JOIN inventory_units un ON un.id = l.unit_id
  LEFT JOIN profiles pb ON pb.user_id = l.borrower_user_id
  LEFT JOIN profiles pa ON pa.user_id = r.actor_user_id
  UNION ALL
  SELECT c.id, 'correction'::text, c.item_id, i.name, COALESCE(un.asset_tag, ''),
         COALESCE(pb.display_name, ''), COALESCE(pc.display_name, ''), COALESCE(c.delta_quantity, 0), c.created_at, c.kind, c.target_type, c.reason, ''::text
  FROM inventory_correction_events c
  JOIN inventory_items i ON i.id = c.item_id
  LEFT JOIN inventory_loans tl ON c.target_type = 'loan' AND tl.id = c.target_id
  LEFT JOIN inventory_returns tr ON c.target_type = 'return' AND tr.id = c.target_id
  LEFT JOIN inventory_loans tl2 ON tl2.id = tr.loan_id
  LEFT JOIN inventory_units un ON un.id = COALESCE(tl.unit_id, tl2.unit_id)
  LEFT JOIN profiles pb ON pb.user_id = COALESCE(tl.borrower_user_id, tl2.borrower_user_id)
  LEFT JOIN profiles pc ON pc.user_id = c.actor_user_id
) h
WHERE ($1::uuid IS NULL OR h.item_id = $1::uuid)
  AND (NOT $2::bool OR (h.at, h.event_id) < ($3::timestamptz, $4::uuid))
ORDER BY h.at DESC, h.event_id DESC
LIMIT $5;

-- name: GetLoanEff :one
SELECT * FROM inventory_loans_eff WHERE id = $1;

-- name: GetReturnEff :one
SELECT * FROM inventory_returns_eff WHERE id = $1;

-- name: GetRawReturn :one
SELECT * FROM inventory_returns WHERE id = $1;

-- name: GetCorrectionByKey :one
SELECT * FROM inventory_correction_events WHERE idempotency_key = $1;

-- name: InsertCorrection :one
INSERT INTO inventory_correction_events (item_id, target_type, target_id, kind, delta_quantity, reason, actor_user_id, idempotency_key, request_fingerprint, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;
