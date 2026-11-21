//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nu-digital/nu_app/services/api/internal/auth/token"
)

const newPass = "a-brand-new-passphrase-9"

func (e *env) userID(loginID string) pgtype.UUID {
	e.t.Helper()
	var id pgtype.UUID
	if err := e.pool.QueryRow(context.Background(), `SELECT id FROM users WHERE login_id = $1`, loginID).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *env) issue(loginID, purpose string, ttl time.Duration) string {
	e.t.Helper()
	raw, _, err := token.Issue(context.Background(), e.pool, e.userID(loginID), purpose, ttl, pgtype.UUID{}, *e.clock)
	if err != nil {
		e.t.Fatal(err)
	}
	return raw
}

func tpBody(tok, pass string) string {
	b, _ := json.Marshal(map[string]string{"token": tok, "password": pass})
	return string(b)
}

func (e *env) accept(tok, pass string, o ...reqOpt) int {
	return e.do(http.MethodPost, "/auth/accept-invite", tpBody(tok, pass), o...).Code
}

func (e *env) count(sql string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func TestAcceptInviteActivatesAndAllowsLogin(t *testing.T) {
	e := newEnv(t, opts{})
	e.addUser("newbie.example", "invited", "")
	tok := e.issue("newbie.example", token.PurposeInvite, time.Hour)

	// 受諾前はログインできない
	if rec := e.login("newbie.example", newPass); rec.Code != 401 {
		t.Fatalf("受諾前のログイン = %d", rec.Code)
	}
	rec := e.do(http.MethodPost, "/auth/accept-invite", tpBody(tok, newPass))
	if rec.Code != 204 || sessionCookie(rec) != nil {
		t.Fatalf("受諾: %d（204でCookieなし=自動ログインしない）", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("応答本文にtoken等を含めない: %s", rec.Body)
	}
	if n := e.count(`SELECT count(*) FROM users WHERE login_id='newbie.example' AND status='active' AND activated_at IS NOT NULL`); n != 1 {
		t.Fatal("有効化されていない")
	}
	var hash string
	e.pool.QueryRow(context.Background(), `SELECT password_hash FROM password_credentials WHERE user_id = $1`, e.userID("newbie.example")).Scan(&hash)
	if !strings.HasPrefix(hash, "$argon2id$") || strings.Contains(hash, newPass) {
		t.Fatalf("保存形式が不正: %s", hash)
	}
	if rec := e.login("newbie.example", newPass); rec.Code != 200 {
		t.Fatalf("受諾後のログイン = %d", rec.Code)
	}
	if n := e.count(`SELECT count(*) FROM audit_logs WHERE action='auth.invite_accepted' AND strpos(summary_json::text, $1) = 0`, tok); n != 1 {
		t.Fatalf("監査が1件でない/tokenを含む: %d", n)
	}
	// 2回目は同じ応答で拒否
	if code := e.accept(tok, "another-passphrase-77"); code != 400 {
		t.Fatalf("使用済みtokenの再利用 = %d", code)
	}
	if rec := e.login("newbie.example", "another-passphrase-77"); rec.Code != 401 {
		t.Fatal("再利用でパスワードが変わった")
	}
}

func TestInvalidInviteLinksAreAllTheSame(t *testing.T) {
	e := newEnv(t, opts{})
	e.addUser("newbie.example", "invited", "")
	good := e.issue("newbie.example", token.PurposeInvite, time.Hour)
	expired := e.issue("newbie.example", token.PurposeInvite, time.Hour) // 再発行でgoodが失効
	*e.clock = e.clock.Add(2 * time.Hour)
	other, _, _ := token.New()
	var ref string
	for name, tok := range map[string]string{"失効(再発行で旧)": good, "期限切れ": expired, "存在しない": other, "形式不正": "not-a-token", "空": ""} {
		rec := e.do(http.MethodPost, "/auth/accept-invite", tpBody(tok, newPass))
		if rec.Code != 400 {
			t.Fatalf("%s: %d", name, rec.Code)
		}
		var eb struct {
			Error struct{ Code, Message string }
		}
		decode(t, rec, &eb)
		got := eb.Error.Code + "|" + eb.Error.Message
		if ref == "" {
			ref = got
		} else if got != ref {
			t.Fatalf("%s: 応答が他と異なる: %s vs %s", name, got, ref)
		}
	}
	if !strings.Contains(ref, "INVALID_LINK") {
		t.Fatal(ref)
	}
}

// パスワード方針違反ではトークンを消費しない。直して再試行できる。
func TestWeakPasswordDoesNotConsumeToken(t *testing.T) {
	e := newEnv(t, opts{})
	e.addUser("newbie.example", "invited", "")
	tok := e.issue("newbie.example", token.PurposeInvite, time.Hour)
	for name, pw := range map[string]string{"短い": "short", "ログインIDと同じ": "newbie.example", "繰り返し": strings.Repeat("a", 12), "一般的": "Password123", "長すぎる": strings.Repeat("x", 2000)} {
		rec := e.do(http.MethodPost, "/auth/accept-invite", tpBody(tok, pw))
		if rec.Code != 422 {
			t.Fatalf("%s: %d（422のはず）", name, rec.Code)
		}
	}
	if n := e.count(`SELECT count(*) FROM one_time_tokens WHERE used_at IS NULL AND revoked_at IS NULL`); n != 1 {
		t.Fatal("方針違反でトークンが消費された")
	}
	if n := e.count(`SELECT count(*) FROM password_credentials`); n != 0 {
		t.Fatal("方針違反でパスワードが保存された")
	}
	if code := e.accept(tok, newPass); code != 204 {
		t.Fatalf("直して再試行できない: %d", code)
	}
}

func TestConcurrentAcceptSucceedsOnce(t *testing.T) {
	e := newEnv(t, opts{})
	e.addUser("newbie.example", "invited", "")
	tok := e.issue("newbie.example", token.PurposeInvite, time.Hour)
	const workers = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	codes := make([]int, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			codes[i] = e.accept(tok, newPass+string(rune('a'+i)))
		}(i)
	}
	close(start)
	wg.Wait()
	ok, bad := 0, 0
	for _, c := range codes {
		switch c {
		case 204:
			ok++
		case 400:
			bad++
		default:
			t.Fatalf("想定外のステータス: %d", c)
		}
	}
	if ok != 1 || bad != workers-1 {
		t.Fatalf("成功=%d 拒否=%d（成功は1件のはず）", ok, bad)
	}
	if n := e.count(`SELECT count(*) FROM password_credentials`); n != 1 {
		t.Fatalf("資格情報が%d件", n)
	}
}

// 停止中・有効化済みのユーザーの招待トークンでは状態を変えられない（再有効化できない）。
func TestInviteCannotReactivateDisabledOrChangeActiveUsers(t *testing.T) {
	e := newEnv(t, opts{})
	e.addUser("suspended.example", "invited", "")
	tok := e.issue("suspended.example", token.PurposeInvite, time.Hour)
	if _, err := e.pool.Exec(context.Background(), `UPDATE users SET status='disabled', disabled_at=now() WHERE login_id='suspended.example'`); err != nil {
		t.Fatal(err)
	}
	if code := e.accept(tok, newPass); code != 400 {
		t.Fatalf("停止中のユーザーが招待で受諾できた: %d", code)
	}
	if n := e.count(`SELECT count(*) FROM users WHERE login_id='suspended.example' AND status='disabled'`); n != 1 {
		t.Fatal("停止中のユーザーの状態が変わった")
	}
	if n := e.count(`SELECT count(*) FROM password_credentials`); n != 0 {
		t.Fatal("停止中のユーザーにパスワードが保存された")
	}
	// 有効化済みのユーザーが古い招待トークンでパスワードを上書きできない
	e.addUser("member.example", "active", goodPass)
	tok2 := e.issue("member.example", token.PurposeInvite, time.Hour)
	if code := e.accept(tok2, newPass); code != 400 {
		t.Fatalf("有効なユーザーのパスワードが招待で変更できた: %d", code)
	}
	if rec := e.login("member.example", goodPass); rec.Code != 200 {
		t.Fatal("元のパスワードでログインできなくなった")
	}
}

func TestInviteDoesNotWorkAsResetAndViceVersa(t *testing.T) {
	e := newEnv(t, opts{})
	e.addUser("newbie.example", "invited", "")
	e.addUser("member.example", "active", goodPass)
	invite := e.issue("newbie.example", token.PurposeInvite, time.Hour)
	reset := e.issue("member.example", token.PurposePasswordReset, time.Hour)
	if rec := e.do(http.MethodPost, "/auth/reset-password", tpBody(invite, newPass)); rec.Code != 400 {
		t.Fatalf("招待tokenで再設定できた: %d", rec.Code)
	}
	if code := e.accept(reset, newPass); code != 400 {
		t.Fatalf("再設定tokenで受諾できた: %d", code)
	}
	if n := e.count(`SELECT count(*) FROM one_time_tokens WHERE used_at IS NOT NULL`); n != 0 {
		t.Fatal("purpose違いの試行がトークンを消費した")
	}
}

func TestResetPasswordRevokesSessionsAndOtherTokens(t *testing.T) {
	e := newEnv(t, opts{})
	e.addUser("member.example", "active", goodPass)
	s1 := sessionCookie(e.login("member.example", goodPass))
	s2 := sessionCookie(e.login("member.example", goodPass))
	// 再設定とは別の未使用トークン（古い招待）も残っているとする
	stale := e.issue("member.example", token.PurposeInvite, time.Hour)
	// ロックされていても再設定で解除される
	for i := 0; i < 5; i++ {
		e.login("member.example", "wrong-password-1", fromIP("198.51.100.1"))
	}
	reset := e.issue("member.example", token.PurposePasswordReset, time.Hour)

	rec := e.do(http.MethodPost, "/auth/reset-password", tpBody(reset, newPass))
	if rec.Code != 204 {
		t.Fatalf("再設定: %d %s", rec.Code, rec.Body)
	}
	for i, c := range []*http.Cookie{s1, s2} {
		if r := e.do(http.MethodGet, "/auth/me", "", withCookie(c)); r.Code != 401 {
			t.Fatalf("再設定後に既存セッション%dが使えた: %d", i+1, r.Code)
		}
	}
	if rec := e.login("member.example", goodPass, fromIP("198.51.100.2")); rec.Code != 401 {
		t.Fatalf("旧パスワードが使えた: %d", rec.Code)
	}
	if rec := e.login("member.example", newPass, fromIP("198.51.100.1")); rec.Code != 200 {
		t.Fatalf("新パスワードでログインできない（失敗カウントも消えるはず）: %d", rec.Code)
	}
	if code := e.accept(stale, "yet-another-pass-55"); code != 400 {
		t.Fatalf("他の未使用トークンが失効していない: %d", code)
	}
	// 再設定tokenの再利用は不可
	if r := e.do(http.MethodPost, "/auth/reset-password", tpBody(reset, "yet-another-pass-55")); r.Code != 400 {
		t.Fatalf("再設定tokenの再利用: %d", r.Code)
	}
}

func TestResetIsRefusedForDisabledUsersAndKeepsOriginCheck(t *testing.T) {
	e := newEnv(t, opts{})
	e.addUser("member.example", "active", goodPass)
	reset := e.issue("member.example", token.PurposePasswordReset, time.Hour)
	if _, err := e.pool.Exec(context.Background(), `UPDATE users SET status='disabled', disabled_at=now() WHERE login_id='member.example'`); err != nil {
		t.Fatal(err)
	}
	if r := e.do(http.MethodPost, "/auth/reset-password", tpBody(reset, newPass)); r.Code != 400 {
		t.Fatalf("停止中のユーザーの再設定: %d", r.Code)
	}
	// Origin検証は公開エンドポイントにも適用される
	e.addUser("other.example", "active", goodPass)
	reset2 := e.issue("other.example", token.PurposePasswordReset, time.Hour)
	if r := e.do(http.MethodPost, "/auth/reset-password", tpBody(reset2, newPass), withoutHeader("Origin")); r.Code != 403 {
		t.Fatalf("Origin無しの再設定: %d", r.Code)
	}
	if n := e.count(`SELECT count(*) FROM one_time_tokens WHERE used_at IS NOT NULL`); n != 0 {
		t.Fatal("拒否した試行がトークンを消費した")
	}
}

func TestNoTokensOrPasswordsInLogsOrAudit(t *testing.T) {
	e := newEnv(t, opts{})
	e.addUser("newbie.example", "invited", "")
	tok := e.issue("newbie.example", token.PurposeInvite, time.Hour)
	e.accept(tok, "short") // 422
	e.accept(tok, newPass) // 204
	e.accept(tok, newPass) // 400
	all := e.logs.String()
	for _, secret := range []string{tok, newPass, "short"} {
		if strings.Contains(all, secret) {
			t.Fatalf("ログに秘密値が含まれる: %q", secret)
		}
	}
	if n := e.count(`SELECT count(*) FROM audit_logs WHERE strpos(summary_json::text, $1) > 0 OR strpos(coalesce(target_id,''), $1) > 0`, tok); n != 0 {
		t.Fatal("監査にトークンが含まれる")
	}
}
