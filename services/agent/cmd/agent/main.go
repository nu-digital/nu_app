// Command agent はAIメンバー「ぬる」（Discord）。docs/design/nul-agent.md。
// 必要な設定（Discordのtoken・Claude APIキー・DBのパスワード・ID類）が揃っていないときは「無効モード」で起動し、
// Discordにも接続せずClaude APIも呼ばない（healthzだけ応答する）。secretを置く前にデプロイしても、スタックは壊れない。
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/nu-digital/nu_app/services/agent/internal/config"
	"github.com/nu-digital/nu_app/services/agent/internal/health"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "コンテナのヘルスチェック（/healthzを確認して終了）")
	flag.Parse()
	cfg, err := config.Load(os.Getenv)
	if *healthcheck {
		if err := health.Check(cfg.HealthAddr); err != nil {
			fmt.Fprintln(os.Stderr, "unhealthy")
			os.Exit(1)
		}
		return
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err != nil {
		log.Error("設定が不正です", "error", err.Error())
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, log, cfg); err != nil {
		log.Error("ぬるが異常終了しました", "error", err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger, cfg config.Config) error {
	st := health.New()
	if m := cfg.Missing(); len(m) > 0 {
		// 値は出さない。足りない設定の名前だけ
		log.Info("無効モードで起動します（Discordに接続せず、Claude APIも呼びません）", "missing", m)
		st.SetMode("disabled")
	} else {
		// P1-2以降: Discordへの接続・応答をここへ追加する
		log.Info("設定は揃っています（P1-1ではまだDiscordに接続しません）", "config", cfg.String())
		st.SetMode("ready")
	}
	return st.Serve(ctx, cfg.HealthAddr)
}
