package main

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/nu-digital/nu_app/services/agent/internal/config"
	"github.com/nu-digital/nu_app/services/agent/internal/health"
)

func freeAddr(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

// 秘密が未設定なら無効モードで起動し、healthzが応答する。ログに秘密・値が出ない。
func TestDisabledModeServesHealthzAndLogsNoSecrets(t *testing.T) {
	addr := freeAddr(t)
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	cfg := config.Config{HealthAddr: addr, DiscordToken: "tok-secret-1"} // 一部だけ設定（残りは未設定）
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, log, cfg) }()
	var err error
	for i := 0; i < 50; i++ {
		if err = health.Check(addr); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("healthzが応答しない: %v", err)
	}
	cancel()
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	out := buf.String()
	if !strings.Contains(out, "無効モード") || !strings.Contains(out, "ANTHROPIC_API_KEY") {
		t.Errorf("無効モードのログが無い: %s", out)
	}
	if strings.Contains(out, "tok-secret-1") {
		t.Error("ログに秘密が出ている")
	}
}
