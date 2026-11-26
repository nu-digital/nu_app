-- +goose Up
-- ぬるの記憶（P2-1）: ノートとログ検索（docs/design/nul-agent.md §5）。
-- 日本語の部分一致検索のため pg_trgm（contrib。postgres公式イメージに含まれる）を agent スキーマに入れる。
-- 索引は検索を速くするだけで、ILIKEの結果は索引の有無で変わらない（trigramが取れない短い語は全件走査）。
CREATE EXTENSION IF NOT EXISTS pg_trgm SCHEMA agent;

CREATE INDEX agent_messages_content_trgm_idx ON agent.messages USING gin (content agent.gin_trgm_ops) WHERE kind = 'guild';

-- ぬるのノート。ぬる自身のメモ（人・出来事・感想）。元メッセージへのリンク付き。
-- subject_user_id: 話者本人についてのメモのとき、その人のID（NULLは一般のメモ）。ほかの人のIDでは作らない（コードで強制）。
-- private: DMで作ったメモ。その相手とのDMでだけ使う。
-- source_message_id: 元のメッセージ。Discordで消されたら、このノートも消す（コード側。FKにしない: 取り込み前後で順序が不定）。
CREATE TABLE agent.notes (
    id                bigserial PRIMARY KEY,
    subject_user_id   bigint,
    content           text NOT NULL CHECK (char_length(content) BETWEEN 1 AND 500),
    private           boolean NOT NULL DEFAULT false,
    source_message_id bigint,
    source_channel_id bigint,
    created_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX agent_notes_subject_idx ON agent.notes (subject_user_id);
CREATE INDEX agent_notes_source_idx ON agent.notes (source_message_id) WHERE source_message_id IS NOT NULL;
CREATE INDEX agent_notes_content_trgm_idx ON agent.notes USING gin (content agent.gin_trgm_ops);

GRANT SELECT, INSERT, UPDATE, DELETE ON agent.notes TO nu_agent;
GRANT USAGE, SELECT ON SEQUENCE agent.notes_id_seq TO nu_agent;
GRANT USAGE ON SCHEMA agent TO nu_agent;
-- 拡張の関数・演算子（agentスキーマ内）はUSAGE権限で使える。EXECUTEはPUBLICに既定で付く。

-- +goose Down
DROP TABLE agent.notes;
DROP EXTENSION pg_trgm CASCADE; -- 拡張に依存する索引（messages・notes）も消える
