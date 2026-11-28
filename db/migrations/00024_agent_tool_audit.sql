-- +goose Up
-- ツール実行の監査ログ（第3フェーズ P0）。引数・本文は保存しない。保持90日（アプリが掃除）。会計の正式な監査記録とは別。
CREATE TABLE agent.tool_audit (
    id         bigserial PRIMARY KEY,
    at         timestamptz NOT NULL DEFAULT now(),
    user_id    bigint NOT NULL,
    channel_id bigint NOT NULL,
    tool       text NOT NULL CHECK (char_length(tool) <= 64),
    ok         boolean NOT NULL
);
CREATE INDEX agent_tool_audit_at_idx ON agent.tool_audit (at);
GRANT SELECT, INSERT, UPDATE, DELETE ON agent.tool_audit TO nu_agent;
GRANT USAGE, SELECT ON SEQUENCE agent.tool_audit_id_seq TO nu_agent;

-- +goose Down
DROP TABLE agent.tool_audit;
