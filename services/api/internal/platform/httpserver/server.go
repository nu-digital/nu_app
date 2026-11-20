// Package httpserver はAPIのHTTPルーティングと共通ヘッダーを提供する。
// P0では /healthz（liveness）と /readyz（DBを含むreadiness）だけを持つ。業務APIは含まない。
package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Pinger はDB等の依存先への疎通確認。
type Pinger interface {
	Ping(ctx context.Context) error
}

func New(db Pinger, readyTimeout time.Duration) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		write(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			// 接続文字列やエラー詳細は返さない（秘密値・内部情報の露出防止）。
			write(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
		write(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	return secure(mux)
}

func secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func write(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
