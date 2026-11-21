-- +goose Up
-- P2-03a: 物品の訂正（D09）。元の貸出・返却は更新・削除せず、訂正イベントを追加して「有効数量」へ反映する。
-- 有効数量 = 元の数量 + adjustの差分の合計（voidされたら0）。残数・貸出中の導出は有効数量を使う（訂正が無ければ元と同じ）。

CREATE TABLE inventory_correction_events (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    item_id             uuid NOT NULL REFERENCES inventory_items (id),
    target_type         text NOT NULL,
    target_id           uuid NOT NULL,
    kind                text NOT NULL,
    delta_quantity      integer,
    reason              text NOT NULL,
    -- 訂正した人（元の貸出・返却の操作者は元の行に残り、変更されない）
    actor_user_id       uuid NOT NULL REFERENCES users (id),
    -- 同じ訂正の重複適用を防ぐ（クライアントが再送しても1回だけ反映される）
    idempotency_key     text NOT NULL,
    request_fingerprint text NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT inventory_corrections_target_type_valid CHECK (target_type IN ('loan', 'return')),
    CONSTRAINT inventory_corrections_kind_valid CHECK (kind IN ('adjust', 'void')),
    CONSTRAINT inventory_corrections_delta_matches_kind CHECK ((kind = 'adjust' AND delta_quantity IS NOT NULL AND delta_quantity <> 0 AND abs(delta_quantity) <= 100000) OR (kind = 'void' AND delta_quantity IS NULL)),
    CONSTRAINT inventory_corrections_reason_valid CHECK (btrim(reason) <> '' AND char_length(reason) <= 200),
    CONSTRAINT inventory_corrections_key_valid CHECK (char_length(idempotency_key) BETWEEN 8 AND 100),
    CONSTRAINT inventory_corrections_key_unique UNIQUE (idempotency_key)
);
-- 取消(void)は対象ごとに1回だけ
CREATE UNIQUE INDEX inventory_corrections_one_void_idx ON inventory_correction_events (target_type, target_id) WHERE kind = 'void';
CREATE INDEX inventory_corrections_target_idx ON inventory_correction_events (target_type, target_id);
CREATE INDEX inventory_corrections_item_idx ON inventory_correction_events (item_id, created_at DESC);

CREATE TRIGGER inventory_corrections_append_only BEFORE UPDATE OR DELETE ON inventory_correction_events
    FOR EACH ROW EXECUTE FUNCTION inventory_forbid_modification();

CREATE VIEW inventory_loans_eff AS
SELECT l.id, l.item_id, l.unit_id, l.borrower_user_id, l.borrowed_at, l.quantity AS original_quantity,
       (CASE WHEN EXISTS (SELECT 1 FROM inventory_correction_events c WHERE c.target_type = 'loan' AND c.target_id = l.id AND c.kind = 'void') THEN 0
             ELSE l.quantity + COALESCE((SELECT SUM(c.delta_quantity) FROM inventory_correction_events c WHERE c.target_type = 'loan' AND c.target_id = l.id AND c.kind = 'adjust'), 0)
        END)::integer AS quantity
FROM inventory_loans l;

CREATE VIEW inventory_returns_eff AS
SELECT r.id, r.loan_id, r.actor_user_id, r.returned_at, r.quantity AS original_quantity,
       (CASE WHEN EXISTS (SELECT 1 FROM inventory_correction_events c WHERE c.target_type = 'return' AND c.target_id = r.id AND c.kind = 'void') THEN 0
             ELSE r.quantity + COALESCE((SELECT SUM(c.delta_quantity) FROM inventory_correction_events c WHERE c.target_type = 'return' AND c.target_id = r.id AND c.kind = 'adjust'), 0)
        END)::integer AS quantity
FROM inventory_returns r;

-- +goose Down
DROP VIEW inventory_returns_eff;
DROP VIEW inventory_loans_eff;
DROP TRIGGER inventory_corrections_append_only ON inventory_correction_events;
DROP TABLE inventory_correction_events;
