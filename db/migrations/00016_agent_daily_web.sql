-- +goose Up
-- 一人あたりの1日の上限に、Web検索（web）を加える（P3-1）。
ALTER TABLE agent.daily_calls DROP CONSTRAINT daily_calls_kind_check;
ALTER TABLE agent.daily_calls ADD CONSTRAINT daily_calls_kind_check CHECK (kind IN ('reply', 'opus', 'web'));

-- +goose Down
DELETE FROM agent.daily_calls WHERE kind = 'web';
ALTER TABLE agent.daily_calls DROP CONSTRAINT daily_calls_kind_check;
ALTER TABLE agent.daily_calls ADD CONSTRAINT daily_calls_kind_check CHECK (kind IN ('reply', 'opus'));
