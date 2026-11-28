-- +goose Up
-- 会計機能 P1（docs/design/phase3-accounting.md）: 年度・予算・支出申請・履歴・規則・作品/イベント。前進のみ（追加だけ）。
-- 金額はすべて整数円（bigint）。AIには計算させず、アプリが計算して保存する。会計の記録は削除できない（トリガー）。

CREATE TABLE fiscal_years (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    label        text NOT NULL,                 -- 例: 2026年度
    starts_on    date NOT NULL,
    ends_on      date NOT NULL,
    budget_yen   bigint NOT NULL DEFAULT 0,     -- 使用可能額（予算総額）
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT fiscal_years_label_unique UNIQUE (label),
    CONSTRAINT fiscal_years_period_valid CHECK (ends_on > starts_on),
    CONSTRAINT fiscal_years_budget_valid CHECK (budget_yen >= 0)
);

CREATE TABLE expense_categories (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text NOT NULL,
    sort_order integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT expense_categories_name_valid CHECK (btrim(name) <> '' AND char_length(name) <= 30),
    CONSTRAINT expense_categories_name_unique UNIQUE (name)
);
-- 既存スプレッドシートのカテゴリ（物品・印刷・通信費・交通費）。通信費は大学規則で不可だが、予算枠・過去データのため残す。
INSERT INTO expense_categories (name, sort_order) VALUES ('物品', 10), ('印刷', 20), ('通信費', 30), ('交通費', 40), ('アセット・ソフトウェア', 50), ('その他', 60);

CREATE TABLE budget_allocations (
    fiscal_year_id uuid NOT NULL REFERENCES fiscal_years (id),
    category_id    uuid NOT NULL REFERENCES expense_categories (id),
    amount_yen     bigint NOT NULL,
    PRIMARY KEY (fiscal_year_id, category_id),
    CONSTRAINT budget_allocations_amount_valid CHECK (amount_yen >= 0)
);

-- 作品・イベント（制作中・未公開・開催予定を含む軽量な枠。公開コンテンツとは別。contents への参照は任意）
CREATE TABLE projects (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL,
    kind        text NOT NULL,
    status      text NOT NULL DEFAULT 'active',
    content_id  uuid REFERENCES contents (id),
    starts_on   date,
    ends_on     date,
    note        text NOT NULL DEFAULT '',
    created_by  uuid NOT NULL REFERENCES users (id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT projects_name_valid CHECK (btrim(name) <> '' AND char_length(name) <= 80),
    CONSTRAINT projects_kind_valid CHECK (kind IN ('work', 'event', 'other')),
    CONSTRAINT projects_status_valid CHECK (status IN ('planning', 'active', 'done')),
    CONSTRAINT projects_note_valid CHECK (char_length(note) <= 500)
);

-- 大学・団体の規則（管理者が追加・無効化できる。未提供の規則は推測の既定値を入れない）
CREATE TABLE expense_rules (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    kind        text NOT NULL,                  -- confirm: 申請者の確認項目 / ai_hint: AIの注意喚起 / info: 要確認の記録
    keywords    text[] NOT NULL DEFAULT '{}',   -- ai_hint用のキーワード
    active      boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT expense_rules_name_valid CHECK (btrim(name) <> '' AND char_length(name) <= 80),
    CONSTRAINT expense_rules_kind_valid CHECK (kind IN ('confirm', 'ai_hint', 'info'))
);
-- 現時点で把握している大学の規則（2026-11-29。運用責任者）。それ以外（保存期間・30万円超の見積書など）は未提供なので入れない。
INSERT INTO expense_rules (name, description, kind, keywords) VALUES
 ('領収書が発行されないもの', '領収書が発行されない支出は認められない。購入前に領収書が発行されることを確認する', 'confirm', '{}'),
 ('個人で備えるべきもの', '通常、個人で備えるべきものは認められない', 'ai_hint', '{}'),
 ('通信費', 'インターネット・携帯電話などの通信費は認められない', 'ai_hint', '{回線,プロバイダ,wi-fi,wifi,sim,携帯,モバイル,通信}'),
 ('飲食に関わる経費', '飲食に関わる経費は認められない', 'ai_hint', '{飲料,食品,弁当,お菓子,ジュース,コーヒー,ケータリング,飲み物}');

CREATE SEQUENCE expense_request_seq;

CREATE TABLE expense_requests (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    seq                bigint NOT NULL DEFAULT nextval('expense_request_seq'),
    fiscal_year_id     uuid NOT NULL REFERENCES fiscal_years (id),
    kind               text NOT NULL DEFAULT 'purchase',
    status             text NOT NULL DEFAULT 'draft',
    requester_id       uuid NOT NULL REFERENCES users (id),
    category_id        uuid REFERENCES expense_categories (id),
    purpose_kind       text NOT NULL DEFAULT 'work',
    project_id         uuid REFERENCES projects (id),
    title              text NOT NULL DEFAULT '',
    vendor             text NOT NULL DEFAULT '',
    url                text NOT NULL DEFAULT '',
    quantity           integer NOT NULL DEFAULT 1,
    unit_price_yen     bigint NOT NULL DEFAULT 0,
    shipping_yen       bigint NOT NULL DEFAULT 0,
    discount_yen       bigint NOT NULL DEFAULT 0,
    planned_total_yen  bigint NOT NULL DEFAULT 0,     -- アプリが計算（数量×単価＋送料−割引）。CHECKで再検証
    actual_total_yen   bigint,                        -- 購入報告で確定した実額
    reason             text NOT NULL DEFAULT '',
    receipt_confirmed  boolean NOT NULL DEFAULT false, -- 購入前に「領収書が発行される」ことを確認した
    rules_ack          jsonb NOT NULL DEFAULT '{}'::jsonb,
    reapproval_needed  boolean NOT NULL DEFAULT false, -- 実額が予定額を大きく超えた（再承認が必要）
    submitted_at       timestamptz,
    decided_at         timestamptz,
    decided_by         uuid REFERENCES users (id),
    purchased_on       date,
    settled_at         timestamptz,
    version            integer NOT NULL DEFAULT 1,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT expense_requests_seq_unique UNIQUE (seq),
    CONSTRAINT expense_requests_kind_valid CHECK (kind IN ('purchase', 'transport', 'other')),
    CONSTRAINT expense_requests_status_valid CHECK (status IN ('draft', 'submitted', 'approved', 'returned', 'rejected', 'purchased', 'receipt_submitted', 'receipt_verified', 'settled', 'cancelled')),
    CONSTRAINT expense_requests_purpose_valid CHECK (purpose_kind IN ('work', 'event', 'learning', 'operation', 'other')),
    CONSTRAINT expense_requests_amounts_valid CHECK (quantity BETWEEN 1 AND 10000 AND unit_price_yen BETWEEN 0 AND 100000000 AND shipping_yen BETWEEN 0 AND 100000000 AND discount_yen BETWEEN 0 AND 100000000 AND planned_total_yen >= 0 AND (actual_total_yen IS NULL OR actual_total_yen >= 0)),
    CONSTRAINT expense_requests_total_consistent CHECK (planned_total_yen = quantity::bigint * unit_price_yen + shipping_yen - discount_yen),
    CONSTRAINT expense_requests_text_valid CHECK (char_length(title) <= 200 AND char_length(vendor) <= 200 AND char_length(url) <= 2000 AND char_length(reason) <= 2000),
    -- 申請以降は、目的と理由が必要（下書きは空でよい）
    CONSTRAINT expense_requests_submitted_complete CHECK (status IN ('draft', 'cancelled') OR (btrim(title) <> '' AND btrim(reason) <> '' AND category_id IS NOT NULL AND receipt_confirmed))
);
CREATE INDEX expense_requests_year_status_idx ON expense_requests (fiscal_year_id, status);
CREATE INDEX expense_requests_requester_idx ON expense_requests (requester_id, created_at DESC);
CREATE INDEX expense_requests_project_idx ON expense_requests (project_id) WHERE project_id IS NOT NULL;

-- 変更・状態遷移の履歴（追記のみ。更新・削除はトリガーで拒否）
CREATE TABLE expense_history (
    id          bigserial PRIMARY KEY,
    request_id  uuid NOT NULL REFERENCES expense_requests (id),
    at          timestamptz NOT NULL DEFAULT now(),
    actor_id    uuid NOT NULL REFERENCES users (id),
    action      text NOT NULL,
    from_status text,
    to_status   text,
    diff        jsonb NOT NULL DEFAULT '{}'::jsonb,
    note        text NOT NULL DEFAULT '',
    CONSTRAINT expense_history_note_valid CHECK (char_length(note) <= 1000)
);
CREATE INDEX expense_history_request_idx ON expense_history (request_id, id);

-- +goose StatementBegin
CREATE FUNCTION expense_forbid_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION '会計の記録は変更・削除できません (%)', TG_TABLE_NAME USING ERRCODE = 'restrict_violation';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER expense_history_append_only BEFORE UPDATE OR DELETE ON expense_history FOR EACH ROW EXECUTE FUNCTION expense_forbid_change();
CREATE TRIGGER expense_requests_no_delete BEFORE DELETE ON expense_requests FOR EACH ROW EXECUTE FUNCTION expense_forbid_change();

-- +goose Down
DROP TRIGGER expense_requests_no_delete ON expense_requests;
DROP TRIGGER expense_history_append_only ON expense_history;
DROP FUNCTION expense_forbid_change();
DROP TABLE expense_history;
DROP TABLE expense_requests;
DROP SEQUENCE expense_request_seq;
DROP TABLE expense_rules;
DROP TABLE projects;
DROP TABLE budget_allocations;
DROP TABLE expense_categories;
DROP TABLE fiscal_years;
