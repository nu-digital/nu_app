-- +goose Up
-- ぬるの研究日誌（P3-2）。dig=古い日次要約からの発掘日誌、web=Webで調べたこと。1日×種別につき1件。
-- 出典URLは保存しない（投稿にもURLを載せない）。
CREATE TABLE agent.diary (
    id         bigserial PRIMARY KEY,
    day        date NOT NULL,
    kind       text NOT NULL CHECK (kind IN ('dig', 'web')),
    topic      text NOT NULL CHECK (char_length(topic) BETWEEN 1 AND 100),
    content    text NOT NULL CHECK (char_length(content) BETWEEN 1 AND 1500),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (day, kind)
);
GRANT SELECT, INSERT, UPDATE, DELETE ON agent.diary TO nu_agent;
GRANT USAGE, SELECT ON SEQUENCE agent.diary_id_seq TO nu_agent;

-- +goose Down
DROP TABLE agent.diary;
