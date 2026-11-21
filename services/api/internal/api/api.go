// Package api はGo APIの認証・認可付きHTTPハンドラーを組み立てる。ルートは /api/v1 の下に置く（呼び出し側でStripPrefix）。
//
// 共通の防御（docs/design/p1-02-auth.md）:
//   - 本文は1MiBまで、JSONのみ（415）。リクエストIDを発行し、エラーはAPI.mdのJSON形式。
//   - 状態変更(POST/PUT/PATCH/DELETE)はOriginを検証し、認証済みはさらに X-CSRF-Token を検証する。
//   - セッションCookieを検証し、認可は authz.Decide に集約する。
package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nu-digital/nu_app/services/api/internal/auth/password"
	"github.com/nu-digital/nu_app/services/api/internal/auth/ratelimit"
	"github.com/nu-digital/nu_app/services/api/internal/auth/session"
	"github.com/nu-digital/nu_app/services/api/internal/authz"
	"github.com/nu-digital/nu_app/services/api/internal/httpx"
	"github.com/nu-digital/nu_app/services/api/internal/platform/config"
)

// maxBody は本文サイズの上限。
const maxBody = 1 << 20

type Deps struct {
	Pool     *pgxpool.Pool
	Cfg      config.Config
	Sessions *session.Service
	Hasher   *password.Hasher
	Limiter  *ratelimit.Limiter
	Log      *slog.Logger
	Now      func() time.Time

	// BeforeOwnerCount はテスト専用のフック（本番ではnil）。最後のOwnerの判定の直前に呼ばれ、競合を再現するために使う。
	BeforeOwnerCount func()
}

type server struct {
	Deps
}

// authCtx は検証済みセッション。
type authCtx struct {
	Identity session.Identity
	Token    string
}

type ctxKey int

const authKey ctxKey = 1

func authFrom(ctx context.Context) (authCtx, bool) {
	a, ok := ctx.Value(authKey).(authCtx)
	return a, ok
}

// cookieName は Secure 時に __Host- 接頭辞を付ける（Secure・Path=/・Domain無し が必須条件）。
func (s *server) cookieName() string {
	if s.Cfg.SecureCookies {
		return "__Host-nu_session"
	}
	return "nu_session"
}

// New は /api/v1 配下のハンドラーを返す。
func New(d Deps) http.Handler {
	if d.Now == nil {
		d.Now = time.Now
	}
	s := &server{Deps: d}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/login", s.login)
	mux.HandleFunc("POST /auth/logout", s.logout)
	mux.HandleFunc("GET /auth/me", s.me)
	mux.HandleFunc("POST /auth/accept-invite", s.acceptInvite)
	mux.HandleFunc("POST /auth/reset-password", s.resetPassword)
	mux.HandleFunc("POST /admin/users", s.createUser)
	mux.HandleFunc("POST /admin/users/{id}/invite-link", s.inviteLink)
	mux.HandleFunc("POST /admin/users/{id}/reset-link", s.resetLink)
	mux.HandleFunc("PATCH /admin/users/{id}/status", s.setStatus)
	// 未定義のパス・メソッドはJSONの404/405
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		httpx.Error(w, r, http.StatusNotFound, "NOT_FOUND", "見つかりません")
	})
	return httpx.WithRequestID(s.recoverer(s.bodyAndType(s.originCheck(s.authenticate(mux)))))
}

func (s *server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				// panicの内容にはリクエストの値が入り得るため、種別だけを記録する。
				s.Log.Error("ハンドラーでpanic", "requestId", httpx.RequestID(r.Context()))
				httpx.Error(w, r, http.StatusInternalServerError, "INTERNAL", "サーバーでエラーが発生しました")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// bodyAndType は本文サイズを制限し、本文を持つ状態変更リクエストはJSONのみ受け付ける。
func (s *server) bodyAndType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if httpx.IsUnsafe(r.Method) && r.ContentLength != 0 {
			if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(strings.ToLower(ct), "application/json") {
				httpx.Error(w, r, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "JSONで送信してください")
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		next.ServeHTTP(w, r)
	})
}

// originCheck は状態変更リクエストのOriginを検証する（未認証のログイン等も対象。ログインCSRF対策）。
func (s *server) originCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if httpx.IsUnsafe(r.Method) && !httpx.OriginAllowed(r, s.Cfg.AllowedOrigin) {
			httpx.Error(w, r, http.StatusForbidden, "ORIGIN_NOT_ALLOWED", "このサイトからの操作のみ受け付けます")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// authenticate はセッションCookieがあれば検証してコンテキストへ入れる。無効なCookieは匿名として扱う。
// 認証済みの状態変更リクエストは X-CSRF-Token も検証する。
func (s *server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(s.cookieName())
		if err != nil || c.Value == "" {
			next.ServeHTTP(w, r)
			return
		}
		id, err := s.Sessions.Validate(r.Context(), c.Value)
		if err != nil {
			if !errors.Is(err, session.ErrInvalid) {
				s.Log.Error("セッション検証に失敗", "requestId", httpx.RequestID(r.Context()))
				httpx.Error(w, r, http.StatusInternalServerError, "INTERNAL", "サーバーでエラーが発生しました")
				return
			}
			next.ServeHTTP(w, r) // 期限切れ・失効・停止は匿名として扱い、ハンドラー側で401にする
			return
		}
		if httpx.IsUnsafe(r.Method) && !isPublicUnsafe(r) && !session.VerifyCSRF(c.Value, r.Header.Get("X-CSRF-Token")) {
			httpx.Error(w, r, http.StatusForbidden, "CSRF_FAILED", "操作を確認できませんでした。ページを読み込み直してください")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authKey, authCtx{Identity: id, Token: c.Value})))
	})
}

// isPublicUnsafe は未認証でも使う状態変更エンドポイント（CSRFトークンは持てない。Origin検証のみ）。
func isPublicUnsafe(r *http.Request) bool {
	switch r.URL.Path {
	case "/auth/login", "/auth/accept-invite", "/auth/reset-password":
		return true
	}
	return false
}

// authorize は認可表で判断し、拒否ならレスポンスを書いて false を返す。未認証は401、権限不足は403。
func (s *server) authorize(w http.ResponseWriter, r *http.Request, action string, res authz.Resource) (authCtx, bool) {
	a, ok := authFrom(r.Context())
	subject := authz.Anonymous
	if ok {
		subject = a.Identity.Subject()
	}
	d := authz.Decide(subject, action, res)
	if d.Allowed {
		return a, true
	}
	if d.Reason == authz.ReasonUnauthed || d.Reason == authz.ReasonInactive {
		httpx.Error(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "ログインが必要です")
	} else {
		httpx.Error(w, r, http.StatusForbidden, "FORBIDDEN", "この操作を行う権限がありません")
	}
	return authCtx{}, false
}
