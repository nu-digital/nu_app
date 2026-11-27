-- +goose Up
-- ぬるのリマインダー（P5-1）。配信したら行を消す。sending は配信中（落ちて残っても10分で pending に戻して再送する）。
CREATE TABLE agent.reminders (
    id          bigserial PRIMARY KEY,
    user_id     bigint NOT NULL,
    channel_id  bigint NOT NULL, -- 送り先（チャンネル・スレッド・DMのID）
    message     text NOT NULL CHECK (char_length(message) BETWEEN 1 AND 200),
    remind_at   timestamptz NOT NULL,
    status      text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sending', 'failed')),
    attempts    integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    claimed_at  timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX agent_reminders_due_idx ON agent.reminders (remind_at) WHERE status IN ('pending', 'sending');
CREATE INDEX agent_reminders_user_idx ON agent.reminders (user_id);
GRANT SELECT, INSERT, UPDATE, DELETE ON agent.reminders TO nu_agent;
GRANT USAGE, SELECT ON SEQUENCE agent.reminders_id_seq TO nu_agent;

-- +goose Down
DROP TABLE agent.reminders;
