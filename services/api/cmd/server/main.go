// Command server はnu_appのGo APIを起動する。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nu-digital/nu_app/services/api/internal/api"
	"github.com/nu-digital/nu_app/services/api/internal/auth/password"
	"github.com/nu-digital/nu_app/services/api/internal/auth/ratelimit"
	"github.com/nu-digital/nu_app/services/api/internal/auth/session"
	"github.com/nu-digital/nu_app/services/api/internal/platform/config"
	"github.com/nu-digital/nu_app/services/api/internal/platform/httpserver"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "自プロセスの/healthzを確認して終了する（コンテナのHEALTHCHECK用）")
	flag.Parse()
	if *healthcheck {
		os.Exit(runHealthcheck())
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("APIが異常終了しました", "error", err.Error())
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.FromOS()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// pgxpool.New は遅延接続のため、DBが未起動でも起動できる（/readyz が503を返す）。
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("DB設定が不正です")
	}
	defer pool.Close()

	// /healthz・/readyz（インフラ用）と /api/v1（認証付きAPI）を1つのサーバーで提供する。
	root := http.NewServeMux()
	root.Handle("/api/v1/", http.StripPrefix("/api/v1", api.New(api.Deps{
		Pool:     pool,
		Cfg:      cfg,
		Sessions: session.New(pool, session.DefaultConfig),
		// 同時にArgon2idを計算する数と、順番待ちの上限時間（超えたら503）。暫定値: DECISIONS P1-02-3。
		Hasher:  password.NewHasher(password.DefaultParams, 4, 3*time.Second),
		Limiter: ratelimit.New(ratelimit.DefaultConfig, nil),
		Log:     log,
	})))
	root.Handle("/", httpserver.New(pool, cfg.ReadyTimeout))

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           root,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("APIを起動しました", "addr", cfg.Addr)

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdown); err != nil {
			return err
		}
	}
	log.Info("APIを停止しました")
	return nil
}

func runHealthcheck() int {
	addr := os.Getenv("API_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 1
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
