//go:build integration

package store_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nu-digital/nu_app/services/api/internal/testutil/pgtest"
)

// agentスキーマ（ぬる）: 専用ロール nu_agent は agent の表だけ使え、nu_app本体のテーブル（public）は読めない。
func TestAgentRoleIsConfinedToAgentSchema(t *testing.T) {
	pool, dsn := pgtest.New(t)
	ctx := context.Background()
	for _, tbl := range []string{"settings", "state", "relationships", "messages", "import_progress", "api_usage", "daily_calls", "reactions_daily"} {
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_schema='agent' AND table_name=$1", tbl).Scan(&n); err != nil || n != 1 {
			t.Fatalf("agent.%s がない: %v", tbl, err)
		}
	}
	var rows int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM agent.state").Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("agent.stateの初期行: %d %v", rows, err)
	}
	// ロールは NOLOGIN で作られる（秘密をmigrationに書かない）。試験では一時的にログインを許可して接続する
	var login bool
	if err := pool.QueryRow(ctx, "SELECT rolcanlogin FROM pg_roles WHERE rolname='nu_agent'").Scan(&login); err != nil {
		t.Fatal(err)
	}
	if login {
		t.Fatal("nu_agentがログイン可能になっている（migrationはNOLOGINで作る）")
	}
	if _, err := pool.Exec(ctx, "ALTER ROLE nu_agent LOGIN PASSWORD 'itest-agent-only'"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "ALTER ROLE nu_agent NOLOGIN PASSWORD NULL") })
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.User, cfg.ConnConfig.Password = "nu_agent", "itest-agent-only"
	ap, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer ap.Close()
	// agentスキーマは読み書きできる
	if _, err := ap.Exec(ctx, "INSERT INTO agent.messages (message_id, channel_id, kind, author_id, content, created_at) VALUES (1, 2, 'guild', 3, 'テスト', now())"); err != nil {
		t.Fatalf("agent.messagesへ書けない: %v", err)
	}
	if _, err := ap.Exec(ctx, "DELETE FROM agent.messages WHERE message_id = 1"); err != nil {
		t.Fatalf("agent.messagesの削除: %v", err)
	}
	// nu_app本体のテーブルは読めない・書けない
	for _, sql := range []string{"SELECT 1 FROM public.users LIMIT 1", "SELECT 1 FROM public.profiles LIMIT 1", "SELECT 1 FROM public.contents LIMIT 1", "SELECT 1 FROM public.audit_logs LIMIT 1", "INSERT INTO public.audit_logs (action) VALUES ('x')", "CREATE TABLE public.nu_agent_probe (id int)"} {
		if _, err := ap.Exec(ctx, sql); err == nil {
			t.Errorf("nu_agentがpublicを操作できてしまう: %s", sql)
		}
	}
	// DB全体の別の権限も持たない（ロール作成・スーパーユーザー）
	var super, createrole, createdb bool
	if err := pool.QueryRow(ctx, "SELECT rolsuper, rolcreaterole, rolcreatedb FROM pg_roles WHERE rolname='nu_agent'").Scan(&super, &createrole, &createdb); err != nil || super || createrole || createdb {
		t.Errorf("nu_agentに余計な権限: %v %v %v %v", super, createrole, createdb, err)
	}
	_ = pgx.ErrNoRows
}
