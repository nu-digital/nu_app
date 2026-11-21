// Package pgtest は統合テスト用に、実PostgreSQL上へ使い捨てのDBを作り、本物のmigrationを適用する。
// 環境変数 TEST_DATABASE_URL（CREATE DATABASE権限を持つ接続先。例: postgres://postgres:...@127.0.0.1:5432/postgres）が必須。
// 未設定ならテストは失敗する（skipして成功扱いにしない）。
package pgtest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// MigrationsDir は実際のmigration(db/migrations)の場所。
func MigrationsDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "..", "db", "migrations")
}

func adminURL(t testing.TB) string {
	t.Helper()
	u := os.Getenv("TEST_DATABASE_URL")
	if u == "" {
		t.Fatal("TEST_DATABASE_URL が未設定です。統合テストは実PostgreSQLが必須です（scripts/test-integration.sh）")
	}
	return u
}

// WithDatabase は dsn のDB名だけを差し替えたURLを返す。
func WithDatabase(t testing.TB, dsn, name string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("DSNを解釈できません: %v", err)
	}
	u.Path = "/" + name
	return u.String()
}

// New は使い捨てDBを作り、migrationをすべて適用して接続プールを返す。テスト終了時にDBを削除する。
func New(t testing.TB) (*pgxpool.Pool, string) {
	t.Helper()
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, adminURL(t))
	if err != nil {
		t.Fatalf("管理接続に失敗: %v", err)
	}
	var b [6]byte
	_, _ = rand.Read(b[:])
	name := "nu_test_" + hex.EncodeToString(b[:])
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		admin.Close()
		t.Fatalf("テスト用DBを作成できません: %v", err)
	}
	dsn := WithDatabase(t, adminURL(t), name)
	Migrate(t, dsn)
	// 並行テストで接続プールの枯渇が競合を隠さないよう（接続待ちで要求が直列化されると、ロックの欠陥を見逃す）、十分大きくする。
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("DSNを解釈できません: %v", err)
	}
	cfg.MaxConns = 64
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("接続に失敗: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		// テスト専用DB(nu_test_*)だけを削除する。
		_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		admin.Close()
	})
	return pool, dsn
}

// Migrate は本物のmigrationを最新まで適用する（適用済みなら何もしない）。
func Migrate(t testing.TB, dsn string) {
	t.Helper()
	db := openSQL(t, dsn)
	defer db.Close()
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	goose.SetLogger(goose.NopLogger())
	if err := goose.Up(db, MigrationsDir()); err != nil {
		t.Fatalf("migrate up に失敗: %v", err)
	}
}

// MigrateDownTo はversionまで戻す（0で全て戻す）。
func MigrateDownTo(t testing.TB, dsn string, version int64) {
	t.Helper()
	db := openSQL(t, dsn)
	defer db.Close()
	goose.SetLogger(goose.NopLogger())
	if err := goose.DownTo(db, MigrationsDir(), version); err != nil {
		t.Fatalf("migrate down に失敗: %v", err)
	}
}

func openSQL(t testing.TB, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetConnMaxLifetime(time.Minute)
	return db
}
