//go:build integration

package api_test

import (
	"context"
	"testing"

	"github.com/nu-digital/nu_app/services/api/internal/recovery"
)

// 復旧したOwnerは、復旧トークンでパスワードを設定してログインできる。古いセッションは使えない。
func TestRecoveredOwnerCanResetPasswordAndLogin(t *testing.T) {
	e := newEnv(t, opts{})
	e.addUser("owner.example", "active", goodPass, "owner")
	old := e.signIn("owner.example")
	if _, err := e.pool.Exec(context.Background(), `UPDATE users SET status='disabled', disabled_at=now() WHERE login_id='owner.example'`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ { // ロックされた状態も想定
		e.login("owner.example", "wrong-password-1", fromIP("198.51.100.1"))
	}
	res, err := recovery.RecoverOwner(context.Background(), e.pool, recovery.Params{
		LoginID: "owner.example", ConfirmLoginID: "owner.example", ApproverNote: "運用責任者(架空)が本人確認し承認", OSUser: "operator",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec := e.do("GET", "/auth/me", "", withCookie(old.cookie)); rec.Code != 401 {
		t.Fatalf("復旧前のセッションが使える: %d", rec.Code)
	}
	if r := e.do("POST", "/auth/reset-password", tpBody(res.Token, newPass)); r.Code != 204 {
		t.Fatalf("復旧トークンでパスワードを設定できない: %d %s", r.Code, r.Body)
	}
	rec := e.login("owner.example", newPass, fromIP("198.51.100.2"))
	if rec.Code != 200 {
		t.Fatalf("復旧後のログイン: %d", rec.Code)
	}
	var sr sessResp
	decode(t, rec, &sr)
	if len(sr.User.Roles) != 1 || sr.User.Roles[0] != "owner" {
		t.Fatalf("Ownerのまま: %v", sr.User.Roles)
	}
}
