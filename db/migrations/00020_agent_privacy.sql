-- +goose Up
-- 運用前の見直し（P2〜P5の確認）で見つかった、プライバシーの穴の修正。
-- is_public: そのメッセージが、@everyoneが見られるチャンネルのものか。非公開チャンネルの内容は、検索・要約・日誌・週報・内省の材料にしない。
--            既存の行は、判定できていないので非公開（false）にする（安全側。公開として取り込み直すまで検索・要約に出ない）。
-- private（リマインダー）: DMで頼んだもの。公開の場の一覧には出さない。
ALTER TABLE agent.messages ADD COLUMN is_public boolean NOT NULL DEFAULT false;
ALTER TABLE agent.reminders ADD COLUMN private boolean NOT NULL DEFAULT false;
CREATE INDEX agent_messages_public_idx ON agent.messages (channel_id, message_id DESC) WHERE kind = 'guild' AND is_public;

-- +goose Down
DROP INDEX agent.agent_messages_public_idx;
ALTER TABLE agent.reminders DROP COLUMN private;
ALTER TABLE agent.messages DROP COLUMN is_public;
