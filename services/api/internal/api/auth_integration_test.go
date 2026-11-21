//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nu-digital/nu_app/services/api/internal/api"
	"github.com/nu-digital/nu_app/services/api/internal/auth/password"
	"github.com/nu-digital/nu_app/services/api/internal/auth/ratelimit"
	"github.com/nu-digital/nu_app/services/api/internal/auth/session"
	"github.com/nu-digital/nu_app/services/api/internal/notify"
	"github.com/nu-digital/nu_app/services/api/internal/platform/config"
	"github.com/nu-digital/nu_app/services/api/internal/store"
	"github.com/nu-digital/nu_app/services/api/internal/testutil/pgtest"
)

// 値はすべて架空。
const (
	origin   = "https://app.example.invalid"
	goodPass = "correct-horse-battery-1"
)

var fastParams = password.Params{MemoryKiB: 64, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}

type env struct {
	hook    func()
	invHook func()
	cfg     config.Config
	t       *testing.T
	pool    *pgxpool.Pool
	h       http.Handler
	logs    *bytes.Buffer
	limiter *ratelimit.Limiter
	clock   *time.Time
	hasher  *password.Hasher
	// notifier は接続テスト用のDiscord送信（nilならBot未設定）。
	notifier notify.Sender
}

type opts struct {
	trusted []netip.Prefix
	secure  *bool
}

func newEnv(t *testing.T, o opts) *env {
	t.Helper()
	pool, _ := pgtest.New(t)
	now := time.Now()
	e := &env{t: t, pool: pool, logs: &bytes.Buffer{}, clock: &now}
	e.hasher = password.NewHasher(fastParams, 4, time.Second)
	e.limiter = ratelimit.New(ratelimit.DefaultConfig, func() time.Time { return *e.clock })
	secure := true
	if o.secure != nil {
		secure = *o.secure
	}
	e.cfg = config.Config{AppBaseURL: origin, AllowedOrigin: origin, SecureCookies: secure, TrustedProxies: o.trusted}
	e.h = e.build()
	return e
}

func (e *env) build() http.Handler {
	return api.New(api.Deps{
		Pool: e.pool, Cfg: e.cfg,
		Sessions: session.New(e.pool, session.DefaultConfig).WithClock(func() time.Time { return *e.clock }),
		Hasher:   e.hasher, Limiter: e.limiter,
		Log:              slog.New(slog.NewTextHandler(e.logs, nil)),
		Now:              func() time.Time { return *e.clock },
		Notifier:         e.notifier,
		BeforeOwnerCount: e.hook, BeforeInventoryCheck: e.invHook,
	})
}

func (e *env) addUser(loginID, status, pass string, roles ...string) {
	e.t.Helper()
	ctx := context.Background()
	q := store.New(e.pool)
	u, err := q.CreateUser(ctx, store.CreateUserParams{LoginID: loginID, Status: "invited"})
	if err != nil {
		e.t.Fatal(err)
	}
	if err := q.CreateProfile(ctx, store.CreateProfileParams{UserID: u.ID, DisplayName: "表示名(" + loginID + ")"}); err != nil {
		e.t.Fatal(err)
	}
	if pass != "" {
		enc, _ := e.hasher.Hash(pass)
		if err := q.UpsertPasswordHash(ctx, store.UpsertPasswordHashParams{UserID: u.ID, PasswordHash: enc}); err != nil {
			e.t.Fatal(err)
		}
	}
	switch status {
	case "active":
		if _, err := e.pool.Exec(ctx, `UPDATE users SET status='active', activated_at=now() WHERE id=$1`, u.ID); err != nil {
			e.t.Fatal(err)
		}
	case "disabled":
		if _, err := e.pool.Exec(ctx, `UPDATE users SET status='disabled', disabled_at=now() WHERE id=$1`, u.ID); err != nil {
			e.t.Fatal(err)
		}
	}
	for _, r := range roles {
		if _, err := e.pool.Exec(ctx, `INSERT INTO user_roles (user_id, role) VALUES ($1,$2)`, u.ID, r); err != nil {
			e.t.Fatal(err)
		}
	}
}

type reqOpt func(*http.Request)

func withHeader(k, v string) reqOpt { return func(r *http.Request) { r.Header.Set(k, v) } }
func withoutHeader(k string) reqOpt { return func(r *http.Request) { r.Header.Del(k) } }
func withCookie(c *http.Cookie) reqOpt {
	return func(r *http.Request) { r.AddCookie(c) }
}
func fromIP(ip string) reqOpt { return func(r *http.Request) { r.RemoteAddr = ip + ":40000" } }

func (e *env) do(method, path, body string, o ...reqOpt) *httptest.ResponseRecorder {
	e.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	r := httptest.NewRequest(method, path, rd)
	r.RemoteAddr = "203.0.113.10:40000"
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet {
		r.Header.Set("Origin", origin) // 既定は同一オリジン。個別テストで外す・変える
	}
	for _, f := range o {
		f(r)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, r)
	return rec
}

func loginBody(id, pass string) string {
	b, _ := json.Marshal(map[string]string{"loginId": id, "password": pass})
	return string(b)
}

func (e *env) login(id, pass string, o ...reqOpt) *httptest.ResponseRecorder {
	return e.do(http.MethodPost, "/auth/login", loginBody(id, pass), o...)
}

type sessResp struct {
	User struct {
		ID, LoginID, DisplayName string
		Roles                    []string
	} `json:"user"`
	CSRFToken string `json:"csrfToken"`
}

func sessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if strings.HasSuffix(c.Name, "nu_session") {
			return c
		}
	}
	return nil
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("JSONを解釈できない: %v: %s", err, rec.Body.String())
	}
}

func TestLoginSuccessSetsSecureCookieAndMeWorks(t *testing.T) {
	e := newEnv(t, opts{})
	e.addUser("alice.example", "active", goodPass, "admin")
	rec := e.login("Alice.Example", goodPass) // ログインIDは大文字小文字を区別しない
	if rec.Code != 200 {
		t.Fatalf("ログイン失敗: %d %s", rec.Code, rec.Body)
	}
	c := sessionCookie(rec)
	if c == nil || c.Name != "__Host-nu_session" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Domain != "" || c.Value == "" {
		t.Fatalf("Cookie属性が想定と異なる: %+v", c)
	}
	var sr sessResp
	decode(t, rec, &sr)
	if sr.CSRFToken == "" || sr.CSRFToken != session.CSRFToken(c.Value) || sr.User.LoginID != "alice.example" || sr.User.DisplayName != "表示名(alice.example)" || len(sr.User.Roles) != 1 || sr.User.Roles[0] != "admin" {
		t.Fatalf("応答が想定と異なる: %+v", sr)
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Request-Id") == "" {
		t.Fatalf("ヘッダー: %v", rec.Header())
	}
	if strings.Contains(rec.Body.String(), goodPass) || strings.Contains(rec.Body.String(), "password_hash") {
		t.Fatal("応答にパスワード関連の値が含まれる")
	}
	me := e.do(http.MethodGet, "/auth/me", "", withCookie(c))
	var mr sessResp
	decode(t, me, &mr)
	if me.Code != 200 || mr.User.LoginID != "alice.example" || mr.CSRFToken != sr.CSRFToken {
		t.Fatalf("me: %d %s", me.Code, me.Body)
	}
	if rec := e.do(http.MethodGet, "/auth/me", ""); rec.Code != 401 {
		t.Fatalf("未認証のme = %d", rec.Code)
	}
}

func TestLocalDevCookieWithoutSecure(t *testing.T) {
	insecure := false
	e := newEnv(t, opts{secure: &insecure})
	e.addUser("alice.example", "active", goodPass)
	c := sessionCookie(e.login("alice.example", goodPass))
	if c == nil || c.Name != "nu_session" || c.Secure || !c.HttpOnly {
		t.Fatalf("ローカル開発のCookie: %+v", c)
	}
}

// 失敗の理由（存在しない・誤り・招待中・停止・資格情報なし・入力不正）は区別できない。
func TestLoginFailuresAreIndistinguishable(t *testing.T) {
	e := newEnv(t, opts{})
	e.addUser("active.example", "active", goodPass)
	e.addUser("invited.example", "invited", goodPass)
	e.addUser("disabled.example", "disabled", goodPass)
	e.addUser("nopass.example", "active", "") // 資格情報なし
	cases := map[string]string{
		"誤パスワード":  loginBody("active.example", "wrong-password-1"),
		"存在しない":   loginBody("ghost.example", goodPass),
		"招待中":     loginBody("invited.example", goodPass),
		"停止":      loginBody("disabled.example", goodPass),
		"資格情報なし":  loginBody("nopass.example", goodPass),
		"空のパスワード": loginBody("active.example", ""),
		"空のID":    loginBody("", goodPass),
		"長すぎるID":  loginBody(strings.Repeat("a", 200), goodPass),
	}
	var ref string
	for name, body := range cases {
		e.limiter = ratelimit.New(ratelimit.DefaultConfig, func() time.Time { return *e.clock }) // 制限は別テストで確認
		rec := e.do(http.MethodPost, "/auth/login", body, fromIP("198.51.100.77"))
		if rec.Code != 401 {
			t.Fatalf("%s: ステータス %d", name, rec.Code)
		}
		var eb struct {
			Error struct{ Code, Message string }
		}
		decode(t, rec, &eb)
		got := eb.Error.Code + "|" + eb.Error.Message
		if ref == "" {
			ref = got
		} else if got != ref {
			t.Fatalf("%s: 応答が他と異なる（理由が推測できる）: %s vs %s", name, got, ref)
		}
		if sessionCookie(rec) != nil {
			t.Fatalf("%s: 失敗でCookieが発行された", name)
		}
	}
	if !strings.Contains(ref, "INVALID_CREDENTIALS") {
		t.Fatalf("エラーコード: %s", ref)
	}
}

func TestLoginRateLimit(t *testing.T) {
	e := newEnv(t, opts{})
	e.addUser("alice.example", "active", goodPass)
	for i := 0; i < 5; i++ {
		if rec := e.login("alice.example", "wrong-password-1", fromIP("198.51.100.1")); rec.Code != 401 {
			t.Fatalf("%d回目: %d", i+1, rec.Code)
		}
	}
	rec := e.login("alice.example", goodPass, fromIP("198.51.100.1")) // 正しいパスワードでもロック中は拒否
	if rec.Code != 429 || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("ロックされない: %d %v", rec.Code, rec.Header())
	}
	if sessionCookie(rec) != nil {
		t.Fatal("ロック中にCookieが発行された")
	}
	// 別IPからは(ID,IP)の組が別なのでログインできる（IDだけを狙ったロックアウトの緩和）
	if rec := e.login("alice.example", goodPass, fromIP("198.51.100.2")); rec.Code != 200 {
		t.Fatalf("別IPからのログイン: %d", rec.Code)
	}
	// 時間が経てば解除される
	*e.clock = e.clock.Add(16 * time.Minute)
	if rec := e.login("alice.example", goodPass, fromIP("198.51.100.1")); rec.Code != 200 {
		t.Fatalf("ロック解除後のログイン: %d", rec.Code)
	}
	// 存在しないIDも同じようにロックされる（429の有無で存在を推測できない）
	for i := 0; i < 5; i++ {
		e.login("ghost.example", "x-x-x-x-x-x-x-x", fromIP("198.51.100.50"))
	}
	if rec := e.login("ghost.example", "x-x-x-x-x-x-x-x", fromIP("198.51.100.50")); rec.Code != 429 {
		t.Fatalf("存在しないIDがロックされない: %d", rec.Code)
	}
}

func TestSpoofedForwardedForDoesNotBypassRateLimit(t *testing.T) {
	e := newEnv(t, opts{}) // 信頼プロキシなし
	e.addUser("alice.example", "active", goodPass)
	for i := 0; i < 5; i++ {
		e.login("alice.example", "wrong-password-1", fromIP("198.51.100.1"), withHeader("X-Forwarded-For", "10.9.9."+string(rune('1'+i))))
	}
	if rec := e.login("alice.example", goodPass, fromIP("198.51.100.1"), withHeader("X-Forwarded-For", "10.1.2.3")); rec.Code != 429 {
		t.Fatalf("X-Forwarded-Forの偽装でロックを回避できた: %d", rec.Code)
	}
}

func TestTrustedProxyUsesRightmostForwardedFor(t *testing.T) {
	e := newEnv(t, opts{trusted: []netip.Prefix{netip.MustParsePrefix("172.18.0.0/16")}})
	e.addUser("alice.example", "active", goodPass)
	// プロキシ(172.18.0.2)経由。左側は偽装値、右端が実クライアント。
	for i := 0; i < 5; i++ {
		e.login("alice.example", "wrong-password-1", fromIP("172.18.0.2"), withHeader("X-Forwarded-For", "6.6.6."+string(rune('1'+i))+", 198.51.100.9"))
	}
	if rec := e.login("alice.example", goodPass, fromIP("172.18.0.2"), withHeader("X-Forwarded-For", "7.7.7.7, 198.51.100.9")); rec.Code != 429 {
		t.Fatalf("右端のIPで数えられていない: %d", rec.Code)
	}
	if rec := e.login("alice.example", goodPass, fromIP("172.18.0.2"), withHeader("X-Forwarded-For", "198.51.100.10")); rec.Code != 200 {
		t.Fatalf("別のクライアントIPはロックされないはず: %d", rec.Code)
	}
}

func TestOriginAndCSRFProtection(t *testing.T) {
	e := newEnv(t, opts{})
	e.addUser("alice.example", "active", goodPass)
	login := e.login("alice.example", goodPass)
	c := sessionCookie(login)
	var sr sessResp
	decode(t, login, &sr)

	// ログイン自体もOriginを検証する（ログインCSRF対策）
	bad := map[string][]reqOpt{
		"Originもrefererも無い": {withoutHeader("Origin")},
		"Origin: null":      {withHeader("Origin", "null")},
		"別のOrigin":          {withHeader("Origin", "https://evil.example")},
		"cross-site":        {withHeader("Sec-Fetch-Site", "cross-site")},
	}
	for name, o := range bad {
		if rec := e.login("alice.example", goodPass, o...); rec.Code != 403 {
			t.Errorf("ログインで%s: %d（403のはず）", name, rec.Code)
		}
	}
	// 認証済みのログアウト: CSRFトークンの欠落・不一致は403、Origin不正も403
	csrfBad := map[string][]reqOpt{
		"トークンなし":         {withCookie(c)},
		"不一致のトークン":       {withCookie(c), withHeader("X-CSRF-Token", session.CSRFToken("other-session"))},
		"セッションtokenそのもの": {withCookie(c), withHeader("X-CSRF-Token", c.Value)},
		"Origin不正":       {withCookie(c), withHeader("X-CSRF-Token", sr.CSRFToken), withHeader("Origin", "https://evil.example")},
		"Originなし":       {withCookie(c), withHeader("X-CSRF-Token", sr.CSRFToken), withoutHeader("Origin")},
	}
	for name, o := range csrfBad {
		if rec := e.do(http.MethodPost, "/auth/logout", "", o...); rec.Code != 403 {
			t.Errorf("ログアウトで%s: %d（403のはず）", name, rec.Code)
		}
	}
	// まだセッションは生きている（失敗した試行が失効させていない）
	if rec := e.do(http.MethodGet, "/auth/me", "", withCookie(c)); rec.Code != 200 {
		t.Fatalf("拒否した試行でセッションが失効した: %d", rec.Code)
	}
	// 正しいOrigin+CSRFでログアウトでき、その後は使えず、Cookieは削除される
	rec := e.do(http.MethodPost, "/auth/logout", "", withCookie(c), withHeader("X-CSRF-Token", sr.CSRFToken))
	if rec.Code != 204 {
		t.Fatalf("ログアウト: %d %s", rec.Code, rec.Body)
	}
	if del := sessionCookie(rec); del == nil || del.MaxAge >= 0 || del.Value != "" {
		t.Fatalf("Cookieが削除されていない: %+v", del)
	}
	if rec := e.do(http.MethodGet, "/auth/me", "", withCookie(c)); rec.Code != 401 {
		t.Fatalf("ログアウト後のme = %d", rec.Code)
	}
	// 未認証のログアウトは401（認可表）
	if rec := e.do(http.MethodPost, "/auth/logout", ""); rec.Code != 401 {
		t.Fatalf("未認証のログアウト = %d", rec.Code)
	}
}

func TestLoginRotatesSessionAndRevokesPresentedCookie(t *testing.T) {
	e := newEnv(t, opts{})
	e.addUser("alice.example", "active", goodPass)
	first := sessionCookie(e.login("alice.example", goodPass))
	// 古いCookieを付けたままログインし直す
	second := sessionCookie(e.login("alice.example", goodPass, withCookie(first)))
	if second == nil || second.Value == first.Value {
		t.Fatal("ログインのたびに新しいセッションが発行されるはず")
	}
	if rec := e.do(http.MethodGet, "/auth/me", "", withCookie(first)); rec.Code != 401 {
		t.Fatalf("提示された旧セッションが失効していない: %d", rec.Code)
	}
	if rec := e.do(http.MethodGet, "/auth/me", "", withCookie(second)); rec.Code != 200 {
		t.Fatalf("新しいセッションが使えない: %d", rec.Code)
	}
}

func TestDisabledUserSessionStopsWorkingImmediately(t *testing.T) {
	e := newEnv(t, opts{})
	e.addUser("alice.example", "active", goodPass)
	c := sessionCookie(e.login("alice.example", goodPass))
	if _, err := e.pool.Exec(context.Background(), `UPDATE users SET status='disabled', disabled_at=now() WHERE login_id='alice.example'`); err != nil {
		t.Fatal(err)
	}
	if rec := e.do(http.MethodGet, "/auth/me", "", withCookie(c)); rec.Code != 401 {
		t.Fatalf("停止後のme = %d", rec.Code)
	}
}

func TestRequestShapeLimits(t *testing.T) {
	e := newEnv(t, opts{})
	e.addUser("alice.example", "active", goodPass)
	big := `{"loginId":"alice.example","password":"` + strings.Repeat("x", 2<<20) + `"}`
	if rec := e.do(http.MethodPost, "/auth/login", big); rec.Code != 413 {
		t.Errorf("1MiB超: %d（413のはず）", rec.Code)
	}
	if rec := e.do(http.MethodPost, "/auth/login", loginBody("alice.example", goodPass), withHeader("Content-Type", "text/plain")); rec.Code != 415 {
		t.Errorf("JSON以外: %d（415のはず）", rec.Code)
	}
	if rec := e.do(http.MethodPost, "/auth/login", `{"loginId":"alice.example","password":"x","extra":1}`); rec.Code != 400 {
		t.Errorf("未知のフィールド: %d（400のはず）", rec.Code)
	}
	if rec := e.do(http.MethodPost, "/auth/login", `not json`); rec.Code != 400 {
		t.Errorf("不正なJSON: %d", rec.Code)
	}
	if rec := e.do(http.MethodGet, "/auth/nope", ""); rec.Code != 404 {
		t.Errorf("未定義パス: %d", rec.Code)
	}
}

func TestNoSecretsInLogs(t *testing.T) {
	e := newEnv(t, opts{})
	e.addUser("alice.example", "active", goodPass)
	rec := e.login("alice.example", goodPass)
	e.login("alice.example", "wrong-password-secret-XYZ")
	e.login("ghost.example", "another-secret-ABC")
	c := sessionCookie(rec)
	all := e.logs.String()
	for _, secret := range []string{goodPass, "wrong-password-secret-XYZ", "another-secret-ABC", c.Value, "argon2id"} {
		if strings.Contains(all, secret) {
			t.Fatalf("ログに秘密値が含まれる: %q", secret)
		}
	}
}

func TestBusyHasherReturns503WithRetryAfter(t *testing.T) {
	e := newEnv(t, opts{})
	busy := password.NewHasher(fastParams, 1, 20*time.Millisecond)
	e.h = api.New(api.Deps{
		Pool: e.pool, Cfg: config.Config{AppBaseURL: origin, AllowedOrigin: origin, SecureCookies: true},
		Sessions: session.New(e.pool, session.DefaultConfig), Hasher: busy,
		Limiter: ratelimit.New(ratelimit.DefaultConfig, nil), Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	e.addUser("alice.example", "active", goodPass)
	release, err := busy.Hold() // 唯一の計算枠を占有して混雑を再現する
	if err != nil {
		t.Fatal(err)
	}
	rec := e.login("alice.example", goodPass)
	if rec.Code != 503 || rec.Header().Get("Retry-After") == "" || sessionCookie(rec) != nil {
		t.Fatalf("混雑時は503+Retry-After（Cookieなし）のはず: %d %v", rec.Code, rec.Header())
	}
	// 存在しないIDでも同じく503（枠の混雑状況からもIDの有無が分からない）
	if rec := e.login("ghost.example", goodPass); rec.Code != 503 {
		t.Fatalf("存在しないIDも503のはず: %d", rec.Code)
	}
	release()
	if rec := e.login("alice.example", goodPass); rec.Code != 200 {
		t.Fatalf("枠が空けば成功する: %d", rec.Code)
	}
}
