//go:build integration

package api_test

import (
	"net/http"
	"testing"
)

type roleResp struct {
	ID, LoginID string
	Roles       []string
}

func TestRoleEndpointsAuthorizationMatrix(t *testing.T) {
	e := newEnv(t, opts{})
	admin, owner := e.adminSetup()
	e.addUser("member.example", "active", goodPass)
	e.addUser("editor.example", "active", goodPass, "editor")
	member, editor := e.signIn("member.example"), e.signIn("editor.example")
	target := e.userID("member.example").String()
	editorPath, adminPath := "/admin/users/"+target+"/editor-role", "/owner/users/"+target+"/admin-role"
	body := `{"enabled":true}`

	for _, p := range []string{editorPath, adminPath} {
		if rec := e.do("PUT", p, body); rec.Code != 401 {
			t.Errorf("匿名 %s = %d", p, rec.Code)
		}
		for name, a := range map[string]actor{"member": member, "editor": editor} {
			if rec := e.as(a, "PUT", p, body); rec.Code != 403 {
				t.Errorf("%s %s = %d（403）", name, p, rec.Code)
			}
		}
	}
	// AdminはEditorのみ。Admin付与はOwnerだけ。
	if rec := e.as(admin, "PUT", editorPath, body); rec.Code != 200 {
		t.Errorf("AdminのEditor付与 = %d", rec.Code)
	}
	if rec := e.as(admin, "PUT", adminPath, body); rec.Code != 403 {
		t.Errorf("AdminのAdmin付与 = %d（403のはず）", rec.Code)
	}
	if rec := e.as(owner, "PUT", adminPath, body); rec.Code != 200 {
		t.Errorf("OwnerのAdmin付与 = %d", rec.Code)
	}
	if n := e.count(`SELECT count(*) FROM user_roles WHERE role='owner'`); n != 1 {
		t.Fatalf("Ownerの数が変わった: %d", n)
	}
	// CSRF無しは管理者でも403
	if rec := e.do("PUT", editorPath, body, withCookie(admin.cookie)); rec.Code != 403 {
		t.Errorf("CSRF無し = %d", rec.Code)
	}
}

func TestRoleGrantAndRevokeAreIdempotentAndAudited(t *testing.T) {
	e := newEnv(t, opts{})
	_, owner := e.adminSetup()
	e.addUser("member.example", "active", goodPass)
	path := "/admin/users/" + e.userID("member.example").String() + "/editor-role"
	var rr roleResp
	for i := 0; i < 2; i++ { // 2回付与しても1行
		rec := e.as(owner, "PUT", path, `{"enabled":true}`)
		decode(t, rec, &rr)
		if rec.Code != 200 || len(rr.Roles) != 1 || rr.Roles[0] != "editor" {
			t.Fatalf("付与%d回目: %d %+v", i+1, rec.Code, rr)
		}
	}
	if n := e.count(`SELECT count(*) FROM audit_logs WHERE action='admin.editor_role_granted'`); n != 1 {
		t.Fatalf("付与の監査は変更があった1回だけのはず: %d", n)
	}
	for i := 0; i < 2; i++ {
		rec := e.as(owner, "PUT", path, `{"enabled":false}`)
		decode(t, rec, &rr)
		if rec.Code != 200 || len(rr.Roles) != 0 {
			t.Fatalf("解除%d回目: %d %+v", i+1, rec.Code, rr)
		}
	}
	if n := e.count(`SELECT count(*) FROM audit_logs WHERE action='admin.editor_role_revoked'`); n != 1 {
		t.Fatalf("解除の監査: %d", n)
	}
	for _, bad := range []string{`{}`, `{"enabled":"yes"}`, `{"enabled":true,"x":1}`} {
		if rec := e.as(owner, "PUT", path, bad); rec.Code != 422 && rec.Code != 400 {
			t.Errorf("不正な入力 %s = %d", bad, rec.Code)
		}
	}
	for _, bad := range []string{"00000000-0000-0000-0000-000000000000", "nope"} {
		if rec := e.as(owner, "PUT", "/admin/users/"+bad+"/editor-role", `{"enabled":true}`); rec.Code != 404 {
			t.Errorf("存在しない/不正なID %s = %d", bad, rec.Code)
		}
	}
}

// 付与・剥奪は、対象の既存セッションに次のリクエストで反映される。
func TestRoleChangesApplyToExistingSessionsImmediately(t *testing.T) {
	e := newEnv(t, opts{})
	_, owner := e.adminSetup()
	e.addUser("member.example", "active", goodPass)
	m := e.signIn("member.example")
	id := e.userID("member.example").String()
	roles := func() []string {
		var mr struct{ User struct{ Roles []string } }
		decode(t, e.do("GET", "/auth/me", "", withCookie(m.cookie)), &mr)
		return mr.User.Roles
	}
	adminOnlyCall := func() int {
		return e.as(m, "POST", "/admin/users", `{"loginId":"new.example","displayName":"x"}`).Code
	}
	if len(roles()) != 0 || adminOnlyCall() != 403 {
		t.Fatal("前提: Memberは管理操作できない")
	}
	if rec := e.as(owner, "PUT", "/owner/users/"+id+"/admin-role", `{"enabled":true}`); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	if r := roles(); len(r) != 1 || r[0] != "admin" {
		t.Fatalf("付与がmeに反映されない: %v", r)
	}
	if c := adminOnlyCall(); c != 201 {
		t.Fatalf("付与直後に管理操作ができない: %d", c)
	}
	if rec := e.as(owner, "PUT", "/owner/users/"+id+"/admin-role", `{"enabled":false}`); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	if c := adminOnlyCall(); c != 403 {
		t.Fatalf("剥奪直後も管理操作できてしまう: %d", c)
	}
}

func TestAdminRoleDoesNotMakeAnOwnerAndDisabledUsersStayInert(t *testing.T) {
	e := newEnv(t, opts{})
	_, owner := e.adminSetup()
	e.addUser("member.example", "active", goodPass)
	m := e.signIn("member.example")
	id := e.userID("member.example").String()
	e.as(owner, "PUT", "/owner/users/"+id+"/admin-role", `{"enabled":true}`)
	// AdminはOwner専用操作ができない
	if rec := e.as(m, "PUT", "/owner/users/"+e.userID("owner.example").String()+"/admin-role", `{"enabled":false}`); rec.Code != 403 {
		t.Fatalf("Adminが Owner専用APIを実行できた: %d", rec.Code)
	}
	// 停止するとロールがあっても何もできない
	e.as(owner, "PATCH", "/admin/users/"+id+"/status", `{"status":"disabled"}`)
	if rec := e.do("GET", "/auth/me", "", withCookie(m.cookie)); rec.Code != 401 {
		t.Fatalf("停止後: %d", rec.Code)
	}
	if n := e.count(`SELECT count(*) FROM user_roles WHERE user_id=$1 AND role='owner'`, e.userID("member.example")); n != 0 {
		t.Fatal("Ownerになってしまった")
	}
}

var _ = http.StatusOK
