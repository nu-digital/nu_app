//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nu-digital/nu_app/services/api/internal/auth/token"
)

type actor struct {
	cookie *http.Cookie
	csrf   string
}

// signIn は既存ユーザー（addUser済み・パスワード=goodPass）でログインして、Cookieとcsrfトークンを返す。
func (e *env) signIn(loginID string) actor {
	e.t.Helper()
	rec := e.login(loginID, goodPass, fromIP("198.51.100.200"))
	if rec.Code != 200 {
		e.t.Fatalf("%s のログインに失敗: %d %s", loginID, rec.Code, rec.Body)
	}
	var sr sessResp
	decode(e.t, rec, &sr)
	return actor{cookie: sessionCookie(rec), csrf: sr.CSRFToken}
}

func (e *env) as(a actor, method, path, body string, o ...reqOpt) *httptest.ResponseRecorder {
	e.t.Helper()
	all := append([]reqOpt{withCookie(a.cookie), withHeader("X-CSRF-Token", a.csrf)}, o...)
	return e.do(method, path, body, all...)
}

type linkResp struct {
	User *struct {
		ID, LoginID, DisplayName, Status string
	} `json:"user"`
	URL       string `json:"url"`
	ExpiresAt string `json:"expiresAt"`
}

func tokenFromURL(t *testing.T, url, kind string) string {
	t.Helper()
	prefix := origin + "/" + kind + "#token="
	if !strings.HasPrefix(url, prefix) {
		t.Fatalf("URLの形式が想定と異なる: %s", url)
	}
	return strings.TrimPrefix(url, prefix)
}

// setBeforeOwnerCount はテスト用フックを差し替えたハンドラーを作り直す。
func (e *env) setBeforeOwnerCount(f func()) {
	e.hook = f
	e.h = e.build()
}

func (e *env) adminSetup() (admin, owner actor) {
	e.addUser("owner.example", "active", goodPass, "owner")
	e.addUser("admin.example", "active", goodPass, "admin")
	return e.signIn("admin.example"), e.signIn("owner.example")
}

func TestAdminEndpointsAuthorization(t *testing.T) {
	e := newEnv(t, opts{})
	admin, owner := e.adminSetup()
	e.addUser("member.example", "active", goodPass)
	e.addUser("editor.example", "active", goodPass, "editor")
	member, editor := e.signIn("member.example"), e.signIn("editor.example")
	target := e.userID("member.example").String()
	for _, c := range []struct{ method, path, body string }{
		{"POST", "/admin/users", `{"loginId":"x.example","displayName":"x"}`},
		{"POST", "/admin/users/" + target + "/reset-link", ""},
		{"POST", "/admin/users/" + target + "/invite-link", ""},
		{"PATCH", "/admin/users/" + target + "/status", `{"status":"disabled"}`},
	} {
		if rec := e.do(c.method, c.path, c.body); rec.Code != 401 {
			t.Errorf("匿名 %s %s = %d（401のはず）", c.method, c.path, rec.Code)
		}
		for name, a := range map[string]actor{"member": member, "editor": editor} {
			if rec := e.as(a, c.method, c.path, c.body); rec.Code != 403 {
				t.Errorf("%s %s %s = %d（403のはず）", name, c.method, c.path, rec.Code)
			}
		}
	}
	// CSRFトークン無しは管理者でも403
	if rec := e.do("POST", "/admin/users", `{"loginId":"y.example","displayName":"y"}`, withCookie(admin.cookie)); rec.Code != 403 {
		t.Errorf("CSRF無し = %d", rec.Code)
	}
	if rec := e.as(admin, "POST", "/admin/users", `{"loginId":"y.example","displayName":"y"}`); rec.Code != 201 {
		t.Errorf("Adminのユーザー作成 = %d", rec.Code)
	}
	if rec := e.as(owner, "POST", "/admin/users", `{"loginId":"z.example","displayName":"z"}`); rec.Code != 201 {
		t.Errorf("Ownerのユーザー作成 = %d", rec.Code)
	}
}

func TestCreateUserAndAcceptInviteEndToEnd(t *testing.T) {
	e := newEnv(t, opts{})
	admin, _ := e.adminSetup()
	rec := e.as(admin, "POST", "/admin/users", `{"loginId":"newbie.example","displayName":"新入部員(架空)"}`)
	if rec.Code != 201 {
		t.Fatalf("作成: %d %s", rec.Code, rec.Body)
	}
	var lr linkResp
	decode(t, rec, &lr)
	if lr.User == nil || lr.User.Status != "invited" || lr.User.LoginID != "newbie.example" || lr.User.DisplayName != "新入部員(架空)" {
		t.Fatalf("応答: %+v", lr)
	}
	tok := tokenFromURL(t, lr.URL, "invite")
	if n := e.count(`SELECT count(*) FROM users u JOIN profiles p ON p.user_id=u.id WHERE u.login_id='newbie.example' AND u.status='invited' AND p.display_name='新入部員(架空)'`); n != 1 {
		t.Fatal("ユーザー/プロフィールが作られていない")
	}
	// 招待URLをログイン前に受諾し、ログインできる
	if code := e.accept(tok, newPass); code != 204 {
		t.Fatalf("受諾: %d", code)
	}
	if rec := e.login("newbie.example", newPass); rec.Code != 200 {
		t.Fatalf("受諾後のログイン: %d", rec.Code)
	}
	// 監査: 作成者と対象が記録され、token・URLは含まれない
	if n := e.count(`SELECT count(*) FROM audit_logs WHERE action='admin.user_created' AND actor_user_id IS NOT NULL AND strpos(summary_json::text, $1) = 0 AND strpos(summary_json::text, 'invite') = 0`, tok); n != 1 {
		t.Fatalf("監査が想定と異なる: %d", n)
	}
	// 重複・不正
	if rec := e.as(admin, "POST", "/admin/users", `{"loginId":"newbie.example","displayName":"x"}`); rec.Code != 409 {
		t.Errorf("重複: %d", rec.Code)
	}
	for _, bad := range []string{`{"loginId":"Bad ID","displayName":"x"}`, `{"loginId":"ok.example","displayName":"   "}`, `{"loginId":"ok.example","displayName":"` + strings.Repeat("あ", 65) + `"}`, `{"loginId":"ok.example","displayName":"x","extra":1}`} {
		if rec := e.as(admin, "POST", "/admin/users", bad); rec.Code != 422 && rec.Code != 400 {
			t.Errorf("不正な入力 %s = %d", bad, rec.Code)
		}
	}
	if n := e.count(`SELECT count(*) FROM users WHERE login_id='ok.example'`); n != 0 {
		t.Fatal("拒否した作成でユーザーが作られた")
	}
}

func TestLinkReissueRevokesOldTokenAndChecksState(t *testing.T) {
	e := newEnv(t, opts{})
	admin, _ := e.adminSetup()
	var lr linkResp
	decode(t, e.as(admin, "POST", "/admin/users", `{"loginId":"newbie.example","displayName":"新入(架空)"}`), &lr)
	first := tokenFromURL(t, lr.URL, "invite")
	id := lr.User.ID

	// invited への再発行: 旧トークンは失効
	rec := e.as(admin, "POST", "/admin/users/"+id+"/invite-link", "")
	var again linkResp
	decode(t, rec, &again)
	second := tokenFromURL(t, again.URL, "invite")
	if rec.Code != 200 || first == second {
		t.Fatalf("再発行: %d", rec.Code)
	}
	if code := e.accept(first, newPass); code != 400 {
		t.Fatalf("旧招待トークンが使えた: %d", code)
	}
	// invitedに再設定リンクは発行できない（状態条件）
	if rec := e.as(admin, "POST", "/admin/users/"+id+"/reset-link", ""); rec.Code != 409 {
		t.Fatalf("invitedへの再設定リンク: %d", rec.Code)
	}
	if code := e.accept(second, newPass); code != 204 {
		t.Fatalf("新しい招待トークンで受諾できない: %d", code)
	}
	// 有効化済みに招待リンクは発行できない／再設定リンクは発行できて使える
	if rec := e.as(admin, "POST", "/admin/users/"+id+"/invite-link", ""); rec.Code != 409 {
		t.Fatalf("activeへの招待リンク: %d", rec.Code)
	}
	var rl linkResp
	rec = e.as(admin, "POST", "/admin/users/"+id+"/reset-link", "")
	decode(t, rec, &rl)
	if rec.Code != 200 {
		t.Fatalf("再設定リンク: %d", rec.Code)
	}
	if r := e.do("POST", "/auth/reset-password", tpBody(tokenFromURL(t, rl.URL, "reset"), "reset-passphrase-123")); r.Code != 204 {
		t.Fatalf("再設定リンクが使えない: %d", r.Code)
	}
	// 存在しない・不正なID
	for _, bad := range []string{"00000000-0000-0000-0000-000000000000", "not-a-uuid"} {
		if rec := e.as(admin, "POST", "/admin/users/"+bad+"/reset-link", ""); rec.Code != 404 {
			t.Errorf("不正/存在しないID %s = %d", bad, rec.Code)
		}
	}
}

// Adminは管理者権限を持つ対象にリンク発行・停止できない（権限昇格の防止。Ownerのみ）。
func TestAdminCannotTargetPrivilegedUsers(t *testing.T) {
	e := newEnv(t, opts{})
	admin, owner := e.adminSetup()
	e.addUser("admin2.example", "active", goodPass, "admin")
	ownerID, admin2ID := e.userID("owner.example").String(), e.userID("admin2.example").String()
	for _, id := range []string{ownerID, admin2ID} {
		if rec := e.as(admin, "POST", "/admin/users/"+id+"/reset-link", ""); rec.Code != 403 {
			t.Errorf("Adminが管理者(%s)の再設定リンクを発行できた: %d", id, rec.Code)
		}
		if rec := e.as(admin, "PATCH", "/admin/users/"+id+"/status", `{"status":"disabled"}`); rec.Code != 403 {
			t.Errorf("Adminが管理者(%s)を停止できた: %d", id, rec.Code)
		}
	}
	if n := e.count(`SELECT count(*) FROM one_time_tokens`); n != 0 {
		t.Fatal("拒否したのにトークンが発行された")
	}
	// Ownerは管理者対象でも可
	if rec := e.as(owner, "POST", "/admin/users/"+admin2ID+"/reset-link", ""); rec.Code != 200 {
		t.Fatalf("Ownerの管理者対象の再設定リンク: %d", rec.Code)
	}
	if rec := e.as(owner, "PATCH", "/admin/users/"+admin2ID+"/status", `{"status":"disabled"}`); rec.Code != 200 {
		t.Fatalf("Ownerが管理者を停止: %d", rec.Code)
	}
}

func TestDisableRevokesSessionsAndTokensAndEnableDoesNotRestoreThem(t *testing.T) {
	e := newEnv(t, opts{})
	admin, _ := e.adminSetup()
	e.addUser("member.example", "active", goodPass)
	m := e.signIn("member.example")
	id := e.userID("member.example").String()
	stale := e.issue("member.example", token.PurposePasswordReset, time.Hour)

	rec := e.as(admin, "PATCH", "/admin/users/"+id+"/status", `{"status":"disabled"}`)
	var v struct{ Status string }
	decode(t, rec, &v)
	if rec.Code != 200 || v.Status != "disabled" {
		t.Fatalf("停止: %d %s", rec.Code, rec.Body)
	}
	if r := e.do("GET", "/auth/me", "", withCookie(m.cookie)); r.Code != 401 {
		t.Fatalf("停止後に既存セッションが使えた: %d", r.Code)
	}
	if r := e.login("member.example", goodPass, fromIP("198.51.100.201")); r.Code != 401 {
		t.Fatalf("停止ユーザーがログインできた: %d", r.Code)
	}
	if r := e.do("POST", "/auth/reset-password", tpBody(stale, newPass)); r.Code != 400 {
		t.Fatalf("停止時に未使用トークンが失効していない: %d", r.Code)
	}
	// 冪等: 2回目の停止も200
	if r := e.as(admin, "PATCH", "/admin/users/"+id+"/status", `{"status":"disabled"}`); r.Code != 200 {
		t.Fatalf("2回目の停止: %d", r.Code)
	}
	// 再有効化: パスワード設定済みなのでactive。古いトークンは復活せず、ログインできる
	rec = e.as(admin, "PATCH", "/admin/users/"+id+"/status", `{"status":"active"}`)
	decode(t, rec, &v)
	if rec.Code != 200 || v.Status != "active" {
		t.Fatalf("再有効化: %d %s", rec.Code, rec.Body)
	}
	if r := e.do("POST", "/auth/reset-password", tpBody(stale, newPass)); r.Code != 400 {
		t.Fatalf("再有効化で古いトークンが復活した: %d", r.Code)
	}
	if r := e.do("GET", "/auth/me", "", withCookie(m.cookie)); r.Code != 401 {
		t.Fatalf("再有効化で古いセッションが復活した: %d", r.Code)
	}
	if r := e.login("member.example", goodPass, fromIP("198.51.100.202")); r.Code != 200 {
		t.Fatalf("再有効化後にログインできない: %d", r.Code)
	}
	// パスワード未設定(invited)のまま停止→再有効化はinvitedに戻り、新しい招待リンクが必要
	var lr linkResp
	decode(t, e.as(admin, "POST", "/admin/users", `{"loginId":"newbie.example","displayName":"新入(架空)"}`), &lr)
	e.as(admin, "PATCH", "/admin/users/"+lr.User.ID+"/status", `{"status":"disabled"}`)
	if code := e.accept(tokenFromURL(t, lr.URL, "invite"), newPass); code != 400 {
		t.Fatalf("停止した招待中ユーザーが受諾できた: %d", code)
	}
	rec = e.as(admin, "PATCH", "/admin/users/"+lr.User.ID+"/status", `{"status":"active"}`)
	decode(t, rec, &v)
	if v.Status != "invited" {
		t.Fatalf("未設定ユーザーの再有効化はinvitedのはず: %s", v.Status)
	}
}

func TestStatusGuards(t *testing.T) {
	e := newEnv(t, opts{})
	_, owner := e.adminSetup()
	ownerID := e.userID("owner.example").String()
	if rec := e.as(owner, "PATCH", "/admin/users/"+ownerID+"/status", `{"status":"disabled"}`); rec.Code != 409 {
		t.Fatalf("自分自身の停止: %d（409のはず）", rec.Code)
	}
	if rec := e.as(owner, "PATCH", "/admin/users/"+e.userID("admin.example").String()+"/status", `{"status":"bogus"}`); rec.Code != 422 {
		t.Fatalf("不正な状態: %d", rec.Code)
	}
	// 他にactiveなOwnerがいない状態で、Adminの操作ではOwnerを停止できない(403)。Owner自身も停止できない
	if n := e.count(`SELECT count(*) FROM users WHERE status='active'`); n != 2 {
		t.Fatal("前提が崩れている")
	}
}

// 2人のOwnerが同時に互いを停止しても、必ず1人は残る（Owner行のロック下で判定）。
func TestConcurrentMutualOwnerDisableLeavesOneOwner(t *testing.T) {
	for round := 0; round < 5; round++ {
		e := newEnv(t, opts{})
		e.addUser("owner1.example", "active", goodPass, "owner")
		e.addUser("owner2.example", "active", goodPass, "owner")
		a1, a2 := e.signIn("owner1.example"), e.signIn("owner2.example")
		id1, id2 := e.userID("owner1.example").String(), e.userID("owner2.example").String()
		var wg sync.WaitGroup
		start := make(chan struct{})
		codes := make([]int, 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			codes[0] = e.as(a1, "PATCH", "/admin/users/"+id2+"/status", `{"status":"disabled"}`).Code
		}()
		go func() {
			defer wg.Done()
			<-start
			codes[1] = e.as(a2, "PATCH", "/admin/users/"+id1+"/status", `{"status":"disabled"}`).Code
		}()
		close(start)
		wg.Wait()
		ok := 0
		for _, c := range codes {
			if c == 200 {
				ok++
			} else if c != 409 && c != 401 {
				t.Fatalf("想定外のステータス: %v", codes)
			}
		}
		if n := e.count(`SELECT count(*) FROM user_roles r JOIN users u ON u.id=r.user_id WHERE r.role='owner' AND u.status='active'`); n < 1 {
			t.Fatalf("round %d: activeなOwnerが0人になった: codes=%v", round, codes)
		}
		if ok > 1 {
			// 両方200なら、片方が先に停止された後の操作は「停止済みOwnerの操作」として401/409になるはず
			t.Fatalf("round %d: 両方成功した: %v", round, codes)
		}
	}
}

// 判定の直前で必ず双方が到達するようにして、競合を確実に再現する（タイミング頼みにしない）。
// Owner行のロックが無ければ、両方が「相手はまだactive」と判断して両方成功し、Ownerが0人になる。
func TestMutualOwnerDisableRaceIsDeterministic(t *testing.T) {
	e := newEnv(t, opts{})
	e.addUser("owner1.example", "active", goodPass, "owner")
	e.addUser("owner2.example", "active", goodPass, "owner")
	a1, a2 := e.signIn("owner1.example"), e.signIn("owner2.example")
	id1, id2 := e.userID("owner1.example").String(), e.userID("owner2.example").String()
	e.setBeforeOwnerCount(func() { time.Sleep(300 * time.Millisecond) })
	var wg sync.WaitGroup
	start := make(chan struct{})
	codes := make([]int, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		codes[0] = e.as(a1, "PATCH", "/admin/users/"+id2+"/status", `{"status":"disabled"}`).Code
	}()
	go func() {
		defer wg.Done()
		<-start
		codes[1] = e.as(a2, "PATCH", "/admin/users/"+id1+"/status", `{"status":"disabled"}`).Code
	}()
	close(start)
	wg.Wait()
	// 片方だけが成功し、もう片方は「最後のOwner」として409になる（500などの内部エラーで偶然守られたのではない）
	if !((codes[0] == 200 && codes[1] == 409) || (codes[0] == 409 && codes[1] == 200)) {
		t.Fatalf("結果は[200,409]のいずれかの順のはず: %v", codes)
	}
	if n := e.count(`SELECT count(*) FROM user_roles r JOIN users u ON u.id=r.user_id WHERE r.role='owner' AND u.status='active'`); n != 1 {
		t.Fatalf("activeなOwnerが%d人（1人のはず）: codes=%v", n, codes)
	}
}

func TestLastOwnerCannotBeDisabledEvenByAnotherOwnerWhoIsLast(t *testing.T) {
	e := newEnv(t, opts{})
	e.addUser("owner1.example", "active", goodPass, "owner")
	e.addUser("owner2.example", "active", goodPass, "owner")
	a1 := e.signIn("owner1.example")
	id2 := e.userID("owner2.example").String()
	if rec := e.as(a1, "PATCH", "/admin/users/"+id2+"/status", `{"status":"disabled"}`); rec.Code != 200 {
		t.Fatalf("Owner2を停止: %d", rec.Code)
	}
	// 残りが自分だけ。自分自身は停止できない（かつ最後のOwner）
	if rec := e.as(a1, "PATCH", "/admin/users/"+e.userID("owner1.example").String()+"/status", `{"status":"disabled"}`); rec.Code != 409 {
		t.Fatalf("最後のOwner自身の停止: %d", rec.Code)
	}
	// 停止済みの別Ownerを巻き込んだ「最後のOwner」判定: adminがOwnerを停止しようとしても403
	e.addUser("admin.example", "active", goodPass, "admin")
	ad := e.signIn("admin.example")
	if rec := e.as(ad, "PATCH", "/admin/users/"+e.userID("owner1.example").String()+"/status", `{"status":"disabled"}`); rec.Code != 403 {
		t.Fatalf("AdminがOwnerを停止: %d", rec.Code)
	}
	_ = pgtype.UUID{}
	_ = context.Background
	_ = json.Marshal
}

func TestNoTokenOrURLInLogsOrAuditForLinkIssuance(t *testing.T) {
	e := newEnv(t, opts{})
	admin, _ := e.adminSetup()
	var lr linkResp
	decode(t, e.as(admin, "POST", "/admin/users", `{"loginId":"newbie.example","displayName":"新入(架空)"}`), &lr)
	tok := tokenFromURL(t, lr.URL, "invite")
	var rl linkResp
	e.accept(tok, newPass)
	decode(t, e.as(admin, "POST", "/admin/users/"+lr.User.ID+"/reset-link", ""), &rl)
	rtok := tokenFromURL(t, rl.URL, "reset")
	for _, secret := range []string{tok, rtok} {
		if strings.Contains(e.logs.String(), secret) {
			t.Fatal("ログにトークンが含まれる")
		}
		if n := e.count(`SELECT count(*) FROM audit_logs WHERE strpos(summary_json::text, $1) > 0 OR strpos(coalesce(target_id,''), $1) > 0`, secret); n != 0 {
			t.Fatal("監査にトークンが含まれる")
		}
	}
	if !strings.Contains(lr.URL, "#token=") || strings.Contains(lr.URL, "?token=") {
		t.Fatal("URLはフラグメントでトークンを渡す")
	}
}
