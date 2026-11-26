-- DBのダンプ（pg_dump）には、クラスター全体のロールが含まれない。復元先に先に作っておく（pg_restoreのGRANTが失敗するため）。
-- ロールを追加するmigrationを入れたら、ここにも追加する。パスワードは含めない（運用者が復旧後に設定: docs/RUNBOOK.md §16.2）。
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'nu_agent') THEN
        CREATE ROLE nu_agent NOLOGIN;
    END IF;
END
$$;
ALTER ROLE nu_agent SET search_path = agent;
