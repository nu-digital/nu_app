-- +goose Up
-- 一人あたりの1日の上限に、リマインダーの作成（remind）を加える（第3フェーズ P0。乱用・大量作成の対策）。
ALTER TABLE agent.daily_calls DROP CONSTRAINT daily_calls_kind_check;
ALTER TABLE agent.daily_calls ADD CONSTRAINT daily_calls_kind_check CHECK (kind IN ('reply', 'opus', 'web', 'remind'));

-- +goose Down
DELETE FROM agent.daily_calls WHERE kind = 'remind';
ALTER TABLE agent.daily_calls DROP CONSTRAINT daily_calls_kind_check;
ALTER TABLE agent.daily_calls ADD CONSTRAINT daily_calls_kind_check CHECK (kind IN ('reply', 'opus', 'web'));
