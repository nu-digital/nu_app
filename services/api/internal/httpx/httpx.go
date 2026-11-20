// Package httpx はAPI共通のHTTP部品（JSONエラー、リクエストID、クライアントIP、Origin検証）。
package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

type ctxKey int

const requestIDKey ctxKey = 1

// RequestID はリクエストごとの不透明なID（エラー応答とログで使う）。
func RequestID(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

// WithRequestID はリクエストIDを発行してコンテキストと応答ヘッダーへ入れる。受け取ったヘッダーは信用しない。
func WithRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b [8]byte
		_, _ = rand.Read(b[:])
		id := hex.EncodeToString(b[:])
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

// ErrorBody はAPI.mdのJSONエラー形式。
type ErrorBody struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"requestId"`
	} `json:"error"`
}

// Error はAPI.mdの形式でエラーを返す。messageは利用者に見せてよい日本語の短い文言（内部情報は入れない）。
func Error(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	var b ErrorBody
	b.Error.Code, b.Error.Message, b.Error.RequestID = code, message, RequestID(r.Context())
	JSON(w, status, b)
}

// JSON はJSONを返す。認証・個人情報を含み得るため常に no-store。
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ClientIP はクライアントのIPを返す。
//   - 直接の接続元が信頼プロキシに含まれるときだけ、X-Forwarded-For の**右端**（信頼プロキシ自身が付けた値）を採用する。
//   - それ以外は RemoteAddr を使い、X-Forwarded-For は無視する（ヘッダー偽装でレート制限を回避させない）。
func ClientIP(r *http.Request, trusted []netip.Prefix) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	remote, perr := netip.ParseAddr(host)
	if perr != nil {
		return host
	}
	remote = remote.Unmap()
	for _, p := range trusted {
		if p.Contains(remote) {
			parts := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
			if last := strings.TrimSpace(parts[len(parts)-1]); last != "" {
				if a, err := netip.ParseAddr(last); err == nil {
					return a.Unmap().String()
				}
			}
			break
		}
	}
	return remote.String()
}

// OriginAllowed は状態変更リクエストのOrigin検証。
//   - Origin が許可オリジンと一致すれば許可。"null"・不一致は拒否。
//   - Origin が無いときは Referer のオリジンで判定。どちらも無い場合は拒否。
//   - Sec-Fetch-Site が cross-site なら拒否（ブラウザが付ける追加の手掛かり）。
func OriginAllowed(r *http.Request, allowed string) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	if o := r.Header.Get("Origin"); o != "" {
		return o == allowed
	}
	if ref := r.Header.Get("Referer"); ref != "" {
		u, err := url.Parse(ref)
		return err == nil && u.Scheme+"://"+u.Host == allowed
	}
	return false
}

// IsUnsafe は状態を変え得るメソッド。
func IsUnsafe(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}
