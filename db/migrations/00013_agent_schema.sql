-- +goose Up
-- AIメンバー「ぬる」（services/agent）の専用スキーマ（docs/design/nul-agent-p1.md §3）。
-- nu_appの本体のテーブル（public）とは分け、専用のDBロール nu_agent は agent スキーマだけを使える（publicのテーブルは読めない）。
-- nu_agent はここでは NOLOGIN で作る（秘密をmigrationに書かない）。運用者が初回に ALTER ROLE nu_agent LOGIN PASSWORD … を実行する（docs/RUNBOOK.md）。
CREATE SCHEMA agent;

-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'nu_agent') THEN
        CREATE ROLE nu_agent NOLOGIN;
    END IF;
END
$$;
-- +goose StatementEnd

-- 設定・状態（停止状態・times・予算の付与日・気分など）
CREATE TABLE agent.settings (
    key        text PRIMARY KEY,
    value      jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT agent_settings_key_valid CHECK (key ~ '^[a-z][a-z0-9_.]{0,63}$')
);

-- ぬるの状態（1行）。機嫌・テンション・今ハマっていること
CREATE TABLE agent.state (
    id         boolean PRIMARY KEY DEFAULT true CHECK (id),
    mood       smallint NOT NULL DEFAULT 0 CHECK (mood BETWEEN -2 AND 2),
    energy     smallint NOT NULL DEFAULT 0 CHECK (energy BETWEEN -2 AND 2),
    hobby      text NOT NULL DEFAULT '' CHECK (char_length(hobby) <= 200),
    updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO agent.state (id) VALUES (true);

-- 人ごとの親密度（P1は最小。P2で拡張）
CREATE TABLE agent.relationships (
    discord_user_id bigint PRIMARY KEY,
    closeness       integer NOT NULL DEFAULT 0 CHECK (closeness >= 0),
    nickname        text NOT NULL DEFAULT '' CHECK (char_length(nickname) <= 50),
    last_seen       timestamptz NOT NULL DEFAULT now()
);

-- ログの記録。削除はDiscordでの削除に合わせて物理削除。DM（kind='dm'）の内容は他の場所の話題・検索に使わない（P2のクエリで必ずkindを条件にする）
CREATE TABLE agent.messages (
    message_id  bigint PRIMARY KEY,
    channel_id  bigint NOT NULL,
    thread_id   bigint,
    kind        text NOT NULL CHECK (kind IN ('guild', 'dm')),
    author_id   bigint NOT NULL,
    author_name text NOT NULL DEFAULT '',
    is_bot      boolean NOT NULL DEFAULT false,
    content     text NOT NULL DEFAULT '',
    attachments jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at  timestamptz NOT NULL,
    edited_at   timestamptz
);
CREATE INDEX agent_messages_channel_idx ON agent.messages (channel_id, message_id DESC);
CREATE INDEX agent_messages_author_idx ON agent.messages (author_id, message_id DESC);

-- 過去ログの取り込みの進捗（再開可能）
CREATE TABLE agent.import_progress (
    channel_id        bigint PRIMARY KEY,
    oldest_message_id bigint,
    done              boolean NOT NULL DEFAULT false,
    counted           bigint,
    imported          bigint NOT NULL DEFAULT 0,
    updated_at        timestamptz NOT NULL DEFAULT now()
);

-- API使用量の記録（予算の根拠）。本文は入れない
CREATE TABLE agent.api_usage (
    id             bigserial PRIMARY KEY,
    ts             timestamptz NOT NULL DEFAULT now(),
    model          text NOT NULL,
    purpose        text NOT NULL,
    input_tokens   integer NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens  integer NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    cache_read     integer NOT NULL DEFAULT 0 CHECK (cache_read >= 0),
    cache_write    integer NOT NULL DEFAULT 0 CHECK (cache_write >= 0),
    cost_micro_usd bigint NOT NULL CHECK (cost_micro_usd >= 0),
    channel_id     bigint,
    user_id        bigint
);
CREATE INDEX agent_api_usage_ts_idx ON agent.api_usage (ts);

-- 一人あたりの1日の上限
CREATE TABLE agent.daily_calls (
    day             date NOT NULL,
    discord_user_id bigint NOT NULL,
    kind            text NOT NULL CHECK (kind IN ('reply', 'opus')),
    n               integer NOT NULL DEFAULT 0 CHECK (n >= 0),
    PRIMARY KEY (day, discord_user_id, kind)
);

-- リアクションの1日の上限
CREATE TABLE agent.reactions_daily (
    day date PRIMARY KEY,
    n   integer NOT NULL DEFAULT 0 CHECK (n >= 0)
);

GRANT USAGE ON SCHEMA agent TO nu_agent;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA agent TO nu_agent;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA agent TO nu_agent;
-- 注意: ALTER DEFAULT PRIVILEGES は使わない（ダンプに「FOR ROLE <所有者>」が出て、別のDBへの復元で所有者のロールが無いと失敗する）。
-- agentスキーマにテーブルを足すmigrationは、そのテーブルへのGRANT（nu_agent）を自分で書くこと。
ALTER ROLE nu_agent SET search_path = agent;

-- +goose Down
DROP SCHEMA agent CASCADE;
-- ロール nu_agent はクラスター全体のため残す（再度のupで再利用。不要なら運用者が DROP ROLE）
