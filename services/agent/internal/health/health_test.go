package health

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestHealthzReflectsStatus(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	l.Close()
	s := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Serve(ctx, addr) }()
	var err error
	for i := 0; i < 50; i++ {
		if err = Check(addr); err == nil {
			break
		}
		time.Sleep(40 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("起動直後は200のはず: %v", err)
	}
	s.SetHealthy(false)
	if Check(addr) == nil {
		t.Error("異常のときは200以外のはず")
	}
	s.SetHealthy(true)
	if Check(addr) != nil {
		t.Error("復帰したら200のはず")
	}
}
