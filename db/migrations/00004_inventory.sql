-- +goose Up
-- P2-02a: 物品・貸出。docs/DATABASE.md §3、D09（元記録は更新・削除しない。有効数量は元記録+訂正の導出値）。
-- 貸出期限・承認は持たない（要件）。経過日数は表示時に計算する。

CREATE TABLE inventory_items (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name           text NOT NULL,
    description    text NOT NULL DEFAULT '',
    category       text NOT NULL DEFAULT '',
    location       text NOT NULL DEFAULT '',
    -- 写真はメディア機能(P3)で扱う。
    cover_media_id uuid,
    mode           text NOT NULL,
    -- 数量管理のときの総数。個体管理のときは常に0（個体数は inventory_units から数える）。
    total_quantity integer NOT NULL DEFAULT 0,
    archived_at    timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT inventory_items_name_valid CHECK (btrim(name) <> '' AND char_length(name) <= 100),
    CONSTRAINT inventory_items_description_len CHECK (char_length(description) <= 1000),
    CONSTRAINT inventory_items_category_len CHECK (char_length(category) <= 30),
    CONSTRAINT inventory_items_location_len CHECK (char_length(location) <= 50),
    CONSTRAINT inventory_items_mode_valid CHECK (mode IN ('quantity', 'individual')),
    CONSTRAINT inventory_items_total_valid CHECK (total_quantity >= 0 AND total_quantity <= 100000 AND (mode = 'quantity' OR total_quantity = 0))
);
CREATE INDEX inventory_items_active_idx ON inventory_items (name, id) WHERE archived_at IS NULL;

-- 個体管理の物品の1個体。current_loan_id は貸出中の貸出ID（UNIQUE＝同じ貸出が複数個体を占有せず、個体は同時に1貸出まで）。
CREATE TABLE inventory_units (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    item_id         uuid NOT NULL REFERENCES inventory_items (id),
    asset_tag       text NOT NULL,
    serial_number   text,
    state           text NOT NULL DEFAULT 'active',
    current_loan_id uuid,
    archived_at     timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT inventory_units_tag_valid CHECK (btrim(asset_tag) <> '' AND char_length(asset_tag) <= 50),
    CONSTRAINT inventory_units_serial_len CHECK (serial_number IS NULL OR char_length(serial_number) <= 100),
    CONSTRAINT inventory_units_state_valid CHECK (state IN ('active', 'archived')),
    CONSTRAINT inventory_units_archived_matches_state CHECK ((state = 'archived') = (archived_at IS NOT NULL)),
    -- 貸出中の個体は廃止できない
    CONSTRAINT inventory_units_loaned_not_archived CHECK (current_loan_id IS NULL OR state = 'active'),
    CONSTRAINT inventory_units_tag_unique UNIQUE (item_id, asset_tag),
    CONSTRAINT inventory_units_current_loan_unique UNIQUE (current_loan_id)
);

CREATE TABLE inventory_loans (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    item_id          uuid NOT NULL REFERENCES inventory_items (id),
    -- 数量管理のときNULL。個体管理のとき対象の個体（数量は1）。
    unit_id          uuid REFERENCES inventory_units (id),
    -- 借りた本人（クライアント入力を信用せず、セッションのユーザー）。履歴の参照のためユーザー行は削除しない。
    borrower_user_id uuid NOT NULL REFERENCES users (id),
    quantity         integer NOT NULL,
    borrowed_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT inventory_loans_quantity_valid CHECK (quantity >= 1 AND quantity <= 100000),
    CONSTRAINT inventory_loans_unit_quantity CHECK (unit_id IS NULL OR quantity = 1)
);
CREATE INDEX inventory_loans_item_idx ON inventory_loans (item_id, borrowed_at DESC, id);
CREATE INDEX inventory_loans_borrower_idx ON inventory_loans (borrower_user_id, borrowed_at DESC);

ALTER TABLE inventory_units
    ADD CONSTRAINT inventory_units_current_loan_fk FOREIGN KEY (current_loan_id) REFERENCES inventory_loans (id);

-- 返却。返却者は借りた人と異なってよい（代理返却）。部分返却可。
CREATE TABLE inventory_returns (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    loan_id       uuid NOT NULL REFERENCES inventory_loans (id),
    actor_user_id uuid NOT NULL REFERENCES users (id),
    quantity      integer NOT NULL,
    returned_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT inventory_returns_quantity_valid CHECK (quantity >= 1 AND quantity <= 100000)
);
CREATE INDEX inventory_returns_loan_idx ON inventory_returns (loan_id);

-- 管理数量(総数)の調整履歴。
CREATE TABLE inventory_adjustments (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    item_id         uuid NOT NULL REFERENCES inventory_items (id),
    actor_user_id   uuid NOT NULL REFERENCES users (id),
    before_quantity integer NOT NULL,
    after_quantity  integer NOT NULL,
    reason          text NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT inventory_adjustments_reason_valid CHECK (btrim(reason) <> '' AND char_length(reason) <= 200),
    CONSTRAINT inventory_adjustments_changed CHECK (before_quantity <> after_quantity)
);

-- 元記録は更新・削除しない（D09）。貸出・返却・調整は追記専用。
-- +goose StatementBegin
CREATE FUNCTION inventory_forbid_modification() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION '% は追記専用です', TG_TABLE_NAME USING ERRCODE = 'restrict_violation';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER inventory_loans_append_only BEFORE UPDATE OR DELETE ON inventory_loans
    FOR EACH ROW EXECUTE FUNCTION inventory_forbid_modification();
CREATE TRIGGER inventory_returns_append_only BEFORE UPDATE OR DELETE ON inventory_returns
    FOR EACH ROW EXECUTE FUNCTION inventory_forbid_modification();
CREATE TRIGGER inventory_adjustments_append_only BEFORE UPDATE OR DELETE ON inventory_adjustments
    FOR EACH ROW EXECUTE FUNCTION inventory_forbid_modification();

-- +goose Down
DROP TRIGGER inventory_adjustments_append_only ON inventory_adjustments;
DROP TRIGGER inventory_returns_append_only ON inventory_returns;
DROP TRIGGER inventory_loans_append_only ON inventory_loans;
DROP FUNCTION inventory_forbid_modification();
DROP TABLE inventory_adjustments;
DROP TABLE inventory_returns;
ALTER TABLE inventory_units DROP CONSTRAINT inventory_units_current_loan_fk;
DROP TABLE inventory_loans;
DROP TABLE inventory_units;
DROP TABLE inventory_items;
