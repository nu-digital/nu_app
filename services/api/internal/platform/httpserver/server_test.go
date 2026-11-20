package httpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func get(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestHealthzDoesNotTouchDatabase(t *testing.T) {
	h := New(fakePinger{err: errors.New("down")}, time.Second)
	rec := get(t, h, http.MethodGet, "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz = %d", rec.Code)
	}
}

func TestReadyz(t *testing.T) {
	if rec := get(t, New(fakePinger{}, time.Second), http.MethodGet, "/readyz"); rec.Code != http.StatusOK {
		t.Fatalf("DB到達時のreadyz = %d", rec.Code)
	}
	rec := get(t, New(fakePinger{err: errors.New("dial tcp postgres://user:secret@db: refused")}, time.Second), http.MethodGet, "/readyz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("DB不達時のreadyz = %d", rec.Code)
	}
	for _, leak := range []string{"secret", "postgres://", "refused"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Fatalf("レスポンスに内部情報 %q が含まれる: %s", leak, rec.Body.String())
		}
	}
}

func TestReadyzTimesOut(t *testing.T) {
	slow := pingerFunc(func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() })
	start := time.Now()
	rec := get(t, New(slow, 50*time.Millisecond), http.MethodGet, "/readyz")
	if rec.Code != http.StatusServiceUnavailable || time.Since(start) > time.Second {
		t.Fatalf("タイムアウトしない: %d %v", rec.Code, time.Since(start))
	}
}

type pingerFunc func(context.Context) error

func (f pingerFunc) Ping(ctx context.Context) error { return f(ctx) }

func TestHeadersAndMethods(t *testing.T) {
	h := New(fakePinger{}, time.Second)
	rec := get(t, h, http.MethodGet, "/healthz")
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("共通ヘッダーが不足: %v", rec.Header())
	}
	if rec := get(t, h, http.MethodPost, "/healthz"); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /healthz = %d", rec.Code)
	}
	if rec := get(t, h, http.MethodGet, "/nope"); rec.Code != http.StatusNotFound {
		t.Fatalf("未定義パス = %d", rec.Code)
	}
}
