package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nu-digital/nu_app/services/api/internal/auth/password"
	"github.com/nu-digital/nu_app/services/api/internal/auth/session"
	"github.com/nu-digital/nu_app/services/api/internal/authz"
	"github.com/nu-digital/nu_app/services/api/internal/httpx"
	"github.com/nu-digital/nu_app/services/api/internal/store"
)

type loginRequest struct {
	LoginID  string `json:"loginId"`
	Password string `json:"password"`
}

type userView struct {
	ID          string   `json:"id"`
	LoginID     string   `json:"loginId"`
	DisplayName string   `json:"displayName"`
	Roles       []string `json:"roles"`
}

type sessionResponse struct {
	User      userView  `json:"user"`
	CSRFToken string    `json:"csrfToken"`
	ExpiresAt time.Time `json:"sessionExpiresAt"`
}

const (
	invalidCredentialsMessage = "ログインIDまたはパスワードが違います"
	// 利用者が入力する値の上限（巨大な入力でハッシュ計算を重くされるのを防ぐ）。
	maxLoginIDLen  = 64
	maxPasswordLen = 1024
)

// login は POST /auth/login。失敗は理由を区別せず常に同じ応答にし、ダミー検証で時間差も減らす。
func (s *server) login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req loginRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			httpx.Error(w, r, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "送信するデータが大きすぎます")
			return
		}
		httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", "入力内容を確認してください")
		return
	}
	loginID := strings.ToLower(strings.TrimSpace(req.LoginID))
	ip := httpx.ClientIP(r, s.Cfg.TrustedProxies)

	if allowed, retry := s.Limiter.Check(loginID, ip); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		httpx.Error(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "ログインの試行が多すぎます。しばらくしてからお試しください")
		return
	}

	user, found := s.activeUserWithHash(r, loginID, req)
	// 資格情報が無い（存在しない・招待中・停止・入力が不正）場合も、同じ計算量のダミー検証を行う。
	var ok, needsRehash bool
	var verr error
	if found.hash == "" {
		verr = s.Hasher.VerifyDummy(req.Password)
	} else {
		ok, needsRehash, verr = s.Hasher.Verify(found.hash, req.Password)
	}
	if verr != nil {
		if errors.Is(verr, password.ErrBusy) {
			w.Header().Set("Retry-After", "5")
			httpx.Error(w, r, http.StatusServiceUnavailable, "SERVICE_BUSY", "混み合っています。しばらくしてからお試しください")
			return
		}
		// 保存ハッシュが壊れている等。利用者には通常の失敗と同じに見せ、運用ログにだけ残す。
		s.Log.Error("パスワード検証に失敗", "requestId", httpx.RequestID(ctx))
		ok = false
	}
	if !ok {
		s.Limiter.Failure(loginID, ip)
		httpx.Error(w, r, http.StatusUnauthorized, "INVALID_CREDENTIALS", invalidCredentialsMessage)
		return
	}
	s.Limiter.Success(loginID, ip)

	// セッション固定対策: 常に新規発行する。送られてきた旧セッションCookieは失効させる。
	if c, err := r.Cookie(s.cookieName()); err == nil && c.Value != "" {
		_ = s.Sessions.Revoke(ctx, c.Value)
	}
	tok, expires, err := s.Sessions.Create(ctx, s.Pool, user.ID)
	if err != nil {
		s.Log.Error("セッション作成に失敗", "requestId", httpx.RequestID(ctx))
		httpx.Error(w, r, http.StatusInternalServerError, "INTERNAL", "サーバーでエラーが発生しました")
		return
	}
	if needsRehash {
		s.rehash(r, user.ID, req.Password)
	}
	s.setCookie(w, tok, expires)
	view, err := s.userView(r, user.ID, nil)
	if err != nil {
		httpx.Error(w, r, http.StatusInternalServerError, "INTERNAL", "サーバーでエラーが発生しました")
		return
	}
	httpx.JSON(w, http.StatusOK, sessionResponse{User: view, CSRFToken: session.CSRFToken(tok), ExpiresAt: expires})
}

type foundUser struct{ hash string }

// activeUserWithHash は、ログイン可能（active かつ資格情報あり）なときだけハッシュを返す。それ以外は hash が空。
func (s *server) activeUserWithHash(r *http.Request, loginID string, req loginRequest) (store.User, foundUser) {
	if loginID == "" || len(loginID) > maxLoginIDLen || req.Password == "" || len(req.Password) > maxPasswordLen {
		return store.User{}, foundUser{}
	}
	q := store.New(s.Pool)
	u, err := q.GetUserByLoginID(r.Context(), loginID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			s.Log.Error("ユーザー取得に失敗", "requestId", httpx.RequestID(r.Context()))
		}
		return store.User{}, foundUser{}
	}
	if u.Status != "active" {
		return store.User{}, foundUser{}
	}
	h, err := q.GetPasswordHash(r.Context(), u.ID)
	if err != nil {
		return store.User{}, foundUser{}
	}
	return u, foundUser{hash: h}
}

// rehash は保存ハッシュのパラメータが古い場合に、ログイン成功時に作り直す（失敗してもログインは成功のまま）。
func (s *server) rehash(r *http.Request, userID pgtype.UUID, pw string) {
	enc, err := s.Hasher.Hash(pw)
	if err != nil {
		return
	}
	_ = store.New(s.Pool).UpsertPasswordHash(r.Context(), store.UpsertPasswordHashParams{UserID: userID, PasswordHash: enc})
}

func (s *server) setCookie(w http.ResponseWriter, value string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: s.cookieName(), Value: value, Path: "/", Expires: expires, HttpOnly: true,
		Secure: s.Cfg.SecureCookies, SameSite: http.SameSiteLaxMode,
	})
}

func (s *server) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: s.cookieName(), Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: s.Cfg.SecureCookies, SameSite: http.SameSiteLaxMode,
	})
}

// userView は応答用の利用者情報。ロールが渡されなければDBから読む。
func (s *server) userView(r *http.Request, userID pgtype.UUID, roles []authz.Role) (userView, error) {
	q := store.New(s.Pool)
	u, err := q.GetUserByID(r.Context(), userID)
	if err != nil {
		return userView{}, err
	}
	name, err := q.GetDisplayName(r.Context(), userID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return userView{}, err
	}
	var rs []string
	if roles == nil {
		rs, err = q.ListUserRoles(r.Context(), userID)
		if err != nil {
			return userView{}, err
		}
	} else {
		for _, ro := range roles {
			rs = append(rs, string(ro))
		}
	}
	if rs == nil {
		rs = []string{}
	}
	return userView{ID: userID.String(), LoginID: u.LoginID, DisplayName: name, Roles: rs}, nil
}

// me は GET /auth/me。
func (s *server) me(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "auth.me", authz.Resource{})
	if !ok {
		return
	}
	view, err := s.userView(r, a.Identity.UserID, a.Identity.Roles)
	if err != nil {
		httpx.Error(w, r, http.StatusInternalServerError, "INTERNAL", "サーバーでエラーが発生しました")
		return
	}
	httpx.JSON(w, http.StatusOK, sessionResponse{User: view, CSRFToken: session.CSRFToken(a.Token), ExpiresAt: a.Identity.ExpiresAt})
}

// logout は POST /auth/logout。
func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "auth.logout", authz.Resource{})
	if !ok {
		return
	}
	if err := s.Sessions.Revoke(r.Context(), a.Token); err != nil {
		httpx.Error(w, r, http.StatusInternalServerError, "INTERNAL", "サーバーでエラーが発生しました")
		return
	}
	s.clearCookie(w)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}
