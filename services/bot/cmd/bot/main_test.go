package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestRunStopsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { run(ctx, slog.New(slog.NewTextHandler(io.Discard, nil))); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ctx終了後もrunが戻らない")
	}
}
