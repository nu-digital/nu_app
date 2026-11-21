-- +goose Up
-- P2-04a: 通知アウトボックスと通知設定。業務のDB更新と同じトランザクションでoutboxへ記録し、
-- Discordへの送信は別のworkerが行う（Discord障害で業務操作をロールバックしない）。

CREATE TABLE notification_settings (
    event_type         text PRIMARY KEY,
    enabled            boolean NOT NULL DEFAULT true,
    -- 送信先のDiscordチャンネルID（snowflake。数字のみ）。未設定の間は送信せず、outboxに待機させる
    discord_channel_id text,
    updated_by         uuid REFERENCES users (id),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT notification_settings_event_valid CHECK (event_type IN ('inventory.loan_created', 'inventory.loan_returned', 'inventory.correction_created')),
    CONSTRAINT notification_settings_channel_valid CHECK (discord_channel_id IS NULL OR discord_channel_id ~ '^[0-9]{5,25}$')
);
INSERT INTO notification_settings (event_type) VALUES ('inventory.loan_created'), ('inventory.loan_returned'), ('inventory.correction_created');

CREATE TABLE notification_outbox (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type      text NOT NULL REFERENCES notification_settings (event_type),
    -- 1つの業務イベントにつき1行（貸出ID・返却ID・訂正IDなど）。再実行でも二重に積まない
    dedupe_key      text NOT NULL,
    -- 通知本文の材料。機密（学生証番号・大学メール・氏名・最寄り駅・トークン）を入れない
    payload_json    jsonb NOT NULL,
    status          text NOT NULL DEFAULT 'pending',
    attempts        integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    -- 送信失敗の要約（Discordの応答本文やtokenは保存しない）
    last_error      text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    sent_at         timestamptz,
    CONSTRAINT notification_outbox_status_valid CHECK (status IN ('pending', 'sent', 'dead')),
    CONSTRAINT notification_outbox_attempts_valid CHECK (attempts >= 0),
    CONSTRAINT notification_outbox_sent_matches CHECK ((status = 'sent') = (sent_at IS NOT NULL)),
    CONSTRAINT notification_outbox_dedupe_unique UNIQUE (dedupe_key)
);
CREATE INDEX notification_outbox_due_idx ON notification_outbox (next_attempt_at) WHERE status = 'pending';
CREATE INDEX notification_outbox_status_idx ON notification_outbox (status, created_at DESC);

-- +goose Down
DROP TABLE notification_outbox;
DROP TABLE notification_settings;
