// Command bot はDiscord通知workerの骨組み。P0では起動と停止だけを行い、Discordへは接続しない（P2で実装）。
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	run(ctx, log)
}

// run は ctx が終了するまで待つ。P2でoutboxの取得とDiscord送信をここへ追加する。
func run(ctx context.Context, log *slog.Logger) {
	log.Info("Botを起動しました（P0では何もしません）")
	<-ctx.Done()
	log.Info("Botを停止しました")
}
