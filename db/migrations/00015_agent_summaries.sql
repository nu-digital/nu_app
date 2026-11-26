-- +goose Up
-- ぬるの日次要約（P2-2）: チャンネル×日（JST）。サーバーのチャンネルだけ（DMは要約しない）。
-- 元のメッセージがDiscordで消されたら、その日の要約も消して作り直す（コード側）。
CREATE TABLE agent.summaries (
    channel_id bigint NOT NULL,
    day        date NOT NULL,
    content    text NOT NULL CHECK (char_length(content) BETWEEN 1 AND 3000),
    msg_count  integer NOT NULL CHECK (msg_count >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (channel_id, day)
);
CREATE INDEX agent_summaries_day_idx ON agent.summaries (day DESC);
CREATE INDEX agent_summaries_content_trgm_idx ON agent.summaries USING gin (content agent.gin_trgm_ops);

GRANT SELECT, INSERT, UPDATE, DELETE ON agent.summaries TO nu_agent;

-- +goose Down
DROP TABLE agent.summaries;
