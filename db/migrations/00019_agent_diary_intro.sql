-- +goose Up
-- 内省（P2-4）の「明日の話題」を日誌（diary）に入れる。kind に intro を加える。
ALTER TABLE agent.diary DROP CONSTRAINT diary_kind_check;
ALTER TABLE agent.diary ADD CONSTRAINT diary_kind_check CHECK (kind IN ('dig', 'web', 'intro'));

-- +goose Down
DELETE FROM agent.diary WHERE kind = 'intro';
ALTER TABLE agent.diary DROP CONSTRAINT diary_kind_check;
ALTER TABLE agent.diary ADD CONSTRAINT diary_kind_check CHECK (kind IN ('dig', 'web'));
