// Package health はコンテナ内だけのヘルスチェック用HTTP（外には出さない）。
package health

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

// Status は現在の状態（mode: disabled|running）。
type Status struct {
	mode    atomic.Value
	healthy atomic.Bool
}

func New() *Status {
	s := &Status{}
	s.mode.Store("disabled")
	s.healthy.Store(true)
	return s
}

func (s *Status) SetMode(m string)  { s.mode.Store(m) }
func (s *Status) SetHealthy(h bool) { s.healthy.Store(h) }

func (s *Status) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if !s.healthy.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"healthy": s.healthy.Load(), "mode": s.mode.Load()})
	})
	return mux
}

// Serve はaddrで待ち受け、ctx終了で止まる。
func (s *Status) Serve(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(c)
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Check は -healthcheck 用。addr の /healthz が200を返せばnil。
func Check(addr string) error {
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://" + addr + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("healthzが200ではありません")
	}
	return nil
}
