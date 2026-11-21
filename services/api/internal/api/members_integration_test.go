//go:build integration

package api_test

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

type memberOut struct {
	ID, DisplayName string
	Bio             *string
	Faculty         *string
	Department      *string
	SchoolYear      *int
	Links           []struct{ ID, Label, URL string }
	Devices         []struct{ ID, Category, Name, Spec string }
}

type membersResp struct {
	Items      []memberOut
	NextCursor *string
}

// 公開設定の異なる3人と、一覧に出ない停止・招待中のユーザーを用意する。
func memberFixture(t *testing.T) (*env, actor) {
	e := newEnv(t, opts{})
	e.addUser("viewer.example", "active", goodPass)
	e.addUser("open.example", "active", goodPass)
	e.addUser("closed.example", "active", goodPass)
	e.addUser("gone.example", "active", goodPass)
	e.addUser("invited.example", "invited", "")
	viewer, open, closed, gone := e.signIn("viewer.example"), e.signIn("open.example"), e.signIn("closed.example"), e.signIn("gone.example")
	e.patchProfile(viewer, `{"displayName":"閲覧者"}`) // 既定の表示名にはログインIDが入っているため、検査の邪魔にならない名前にする
	// open: 全部メンバー内公開。closed: 全部入力するが非公開（既定）
	for _, c := range []struct {
		a      actor
		body   string
		public bool
	}{{open, `{"displayName":"公開さん","bio":"公開の自己紹介","faculty":"理工学部","department":"情報科学科","schoolYear":2,"legalName":"秘密 太郎","studentNumber":"11A-111","universityEmail":"open@example.invalid","nearestStation":"秘密駅"}`, true},
		{closed, `{"displayName":"非公開さん","bio":"非公開の自己紹介","faculty":"秘密学部","department":"秘密学科","schoolYear":4,"legalName":"非公 花子","studentNumber":"22B-222","universityEmail":"closed@example.invalid","nearestStation":"非公駅"}`, false},
		{gone, `{"displayName":"退会さん","bio":"退会者の自己紹介"}`, true}} {
		if rec := e.patchProfile(c.a, c.body); rec.Code != 200 {
			t.Fatalf("準備: %d %s", rec.Code, rec.Body)
		}
		if c.public {
			e.as(c.a, "PUT", "/me/profile/visibility", `{"bio":"members","faculty":"members","department":"members","schoolYear":"members"}`)
		}
	}
	e.as(open, "POST", "/me/profile/links", `{"label":"公開リンク","url":"https://example.invalid/open","visibility":"members"}`)
	e.as(open, "POST", "/me/profile/links", `{"label":"非公開リンク","url":"https://example.invalid/secret"}`)
	e.as(open, "POST", "/me/devices", `{"category":"PC","name":"公開機材","spec":"高性能","visibility":"members"}`)
	e.as(open, "POST", "/me/devices", `{"category":"PC","name":"非公開機材","spec":"秘密"}`)
	e.as(closed, "PUT", "/me/profile/public-credit-consent", `{"granted":true}`)
	// goneは退会（停止）
	if _, err := e.pool.Exec(ctxBG(), `UPDATE users SET status='disabled', disabled_at=now() WHERE login_id='gone.example'`); err != nil {
		t.Fatal(err)
	}
	return e, viewer
}

func TestMembersListShowsOnlyActiveAndOnlyPublicFields(t *testing.T) {
	e, viewer := memberFixture(t)
	if rec := e.do("GET", "/members", ""); rec.Code != 401 {
		t.Fatalf("匿名: %d", rec.Code)
	}
	rec := e.as(viewer, "GET", "/members", "")
	if rec.Code != 200 {
		t.Fatalf("一覧: %d", rec.Code)
	}
	body := rec.Body.String()
	var lr membersResp
	decode(t, rec, &lr)
	names := []string{}
	byName := map[string]memberOut{}
	for _, m := range lr.Items {
		names = append(names, m.DisplayName)
		byName[m.DisplayName] = m
	}
	// 有効な3人だけ（退会・招待中は現役一覧に出ない）
	if len(lr.Items) != 3 || byName["退会さん"].ID != "" {
		t.Fatalf("有効なメンバーだけのはず: %v", names)
	}
	for _, m := range lr.Items {
		if strings.Contains(m.DisplayName, "invited") {
			t.Fatal("招待中のユーザーが一覧に出る")
		}
	}
	// 公開した項目は見え、非公開の項目はnull
	o, c := byName["公開さん"], byName["非公開さん"]
	if o.Bio == nil || *o.Bio != "公開の自己紹介" || o.Faculty == nil || *o.Faculty != "理工学部" || o.SchoolYear == nil || *o.SchoolYear != 2 {
		t.Fatalf("公開した項目が見えない: %+v", o)
	}
	if c.Bio != nil || c.Faculty != nil || c.Department != nil || c.SchoolYear != nil {
		t.Fatalf("非公開の項目が見える: %+v", c)
	}
	// 個人情報・ログインID・同意状態・非公開の値は、どこにも出ない
	for _, secret := range []string{"秘密 太郎", "11A-111", "open@example.invalid", "秘密駅", "非公 花子", "22B-222", "closed@example.invalid", "非公駅", "非公開の自己紹介", "秘密学部", "秘密学科",
		"alice", "open.example", "closed.example", "viewer.example", "loginId", "legalName", "studentNumber", "universityEmail", "nearestStation", "publicCreditConsent", "consent", "退会者の自己紹介"} {
		if strings.Contains(body, secret) {
			t.Fatalf("一覧に %q が含まれる: %s", secret, body)
		}
	}
	var raw struct{ Items []map[string]any }
	_ = json.Unmarshal([]byte(body), &raw)
	allowed := map[string]bool{"id": true, "displayName": true, "bio": true, "faculty": true, "department": true, "schoolYear": true, "avatarMediaId": true}
	for k := range raw.Items[0] {
		if !allowed[k] {
			t.Fatalf("想定外の項目 %q", k)
		}
	}
}

func TestMembersSearchByDisplayNameAndPagination(t *testing.T) {
	e, viewer := memberFixture(t)
	get := func(q string) membersResp {
		var lr membersResp
		rec := e.as(viewer, "GET", "/members"+q, "")
		if rec.Code != 200 {
			t.Fatalf("%s: %d", q, rec.Code)
		}
		decode(t, rec, &lr)
		return lr
	}
	if lr := get("?q=" + url.QueryEscape("公開")); len(lr.Items) != 2 { // 「公開さん」と「非公開さん」
		t.Fatalf("表示名で部分一致: %d", len(lr.Items))
	}
	// ログインIDでは検索できない（ログインIDはメンバーに見せない）
	if lr := get("?q=" + url.QueryEscape("closed.example")); len(lr.Items) != 0 {
		t.Fatal("ログインIDで検索できてしまう")
	}
	seen := map[string]bool{}
	cursor := ""
	for pages := 0; pages < 5; pages++ {
		q := "?limit=2"
		if cursor != "" {
			q += "&cursor=" + url.QueryEscape(cursor)
		}
		lr := get(q)
		for _, m := range lr.Items {
			if seen[m.ID] {
				t.Fatal("重複")
			}
			seen[m.ID] = true
		}
		if lr.NextCursor == nil {
			break
		}
		cursor = *lr.NextCursor
	}
	if len(seen) != 3 {
		t.Fatalf("ページングで全件（3人）が1回ずつ出るはず: %d", len(seen))
	}
	for _, bad := range []string{"?limit=0", "?cursor=!!!", "?q=" + strings.Repeat("a", 101)} {
		if rec := e.as(viewer, "GET", "/members"+bad, ""); rec.Code != 400 {
			t.Errorf("%s = %d", bad, rec.Code)
		}
	}
}

func TestMemberDetailShowsOnlyPublicLinksDevicesAndHidesInactive(t *testing.T) {
	e, viewer := memberFixture(t)
	idOf := func(login string) string { return e.userID(login).String() }
	rec := e.as(viewer, "GET", "/members/"+idOf("open.example"), "")
	var m memberOut
	decode(t, rec, &m)
	if rec.Code != 200 || m.DisplayName != "公開さん" || len(m.Links) != 1 || m.Links[0].Label != "公開リンク" || len(m.Devices) != 1 || m.Devices[0].Name != "公開機材" || m.Devices[0].Spec != "高性能" {
		t.Fatalf("詳細: %d %+v", rec.Code, m)
	}
	if strings.Contains(rec.Body.String(), "非公開リンク") || strings.Contains(rec.Body.String(), "非公開機材") || strings.Contains(rec.Body.String(), "secret") || strings.Contains(rec.Body.String(), "秘密") {
		t.Fatal("非公開のリンク・機材・個人情報が含まれる")
	}
	// 非公開さんの詳細は表示名だけ
	rec = e.as(viewer, "GET", "/members/"+idOf("closed.example"), "")
	decode(t, rec, &m)
	if m.Bio != nil || len(m.Links) != 0 || len(m.Devices) != 0 {
		t.Fatalf("非公開さん: %+v", m)
	}
	// 退会（停止）・招待中・存在しない・不正なIDは同じ404
	for name, id := range map[string]string{"退会": idOf("gone.example"), "招待中": idOf("invited.example"), "存在しない": "00000000-0000-0000-0000-000000000000", "不正": "nope"} {
		if rec := e.as(viewer, "GET", "/members/"+id, ""); rec.Code != 404 {
			t.Errorf("%s: %d（404のはず）", name, rec.Code)
		}
	}
}

func TestAdminPrivateProfileViewAuditedAndAuthorized(t *testing.T) {
	e, viewer := memberFixture(t)
	e.addUser("admin.example", "active", goodPass, "admin")
	admin := e.signIn("admin.example")
	id := e.userID("open.example").String()
	for name, a := range map[string]actor{"member": viewer} {
		if rec := e.as(a, "GET", "/admin/users/"+id+"/private-profile", ""); rec.Code != 403 {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	if rec := e.do("GET", "/admin/users/"+id+"/private-profile", ""); rec.Code != 401 {
		t.Errorf("匿名: %d", rec.Code)
	}
	rec := e.as(admin, "GET", "/admin/users/"+id+"/private-profile", "")
	var pp struct {
		LoginID string
		Private struct{ LegalName, StudentNumber, UniversityEmail, NearestStation *string }
	}
	decode(t, rec, &pp)
	if rec.Code != 200 || pp.LoginID != "open.example" || *pp.Private.LegalName != "秘密 太郎" || *pp.Private.UniversityEmail != "open@example.invalid" {
		t.Fatalf("Adminの閲覧: %d %+v", rec.Code, pp)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("no-storeが無い")
	}
	if n := e.count(`SELECT count(*) FROM audit_logs WHERE action='admin.private_profile_viewed' AND actor_user_id IS NOT NULL AND target_id=$1 AND strpos(summary_json::text,'秘密')=0 AND strpos(summary_json::text,'11A')=0`, id); n != 1 {
		t.Fatalf("閲覧の監査（値なし）が1件のはず: %d", n)
	}
	if rec := e.as(admin, "GET", "/admin/users/00000000-0000-0000-0000-000000000000/private-profile", ""); rec.Code != 404 {
		t.Errorf("存在しない: %d", rec.Code)
	}
}

func TestAdminDeletesPrivateProfileOnlyForDisabledUsersWithReason(t *testing.T) {
	e, _ := memberFixture(t)
	e.addUser("admin.example", "active", goodPass, "admin")
	admin := e.signIn("admin.example")
	closed := e.userID("closed.example").String()
	del := func(id, body string) (int, string) {
		rec := e.as(admin, "DELETE", "/admin/users/"+id+"/private-profile", body)
		return rec.Code, rec.Body.String()
	}
	// 有効なメンバーは削除できない（誤削除防止）
	if c, _ := del(closed, `{"reason":"退会のため"}`); c != 409 {
		t.Fatalf("有効なユーザーの個人情報削除 = %d（409のはず）", c)
	}
	if n := e.count(`SELECT count(*) FROM personal_profiles WHERE legal_name IS NOT NULL`); n != 2 {
		t.Fatalf("拒否したのに削除された: %d", n)
	}
	// 停止してから、理由つきで削除
	if rec := e.as(admin, "PATCH", "/admin/users/"+closed+"/status", `{"status":"disabled"}`); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	for _, bad := range []string{`{}`, `{"reason":"  "}`, `{"reason":"` + strings.Repeat("あ", 201) + `"}`, `{"reason":"x","extra":1}`} {
		if c, _ := del(closed, bad); c != 422 && c != 400 {
			t.Errorf("不正な入力 %s = %d", bad, c)
		}
	}
	if n := e.count(`SELECT count(*) FROM personal_profiles WHERE user_id=$1 AND legal_name IS NOT NULL`, e.userID("closed.example")); n != 1 {
		t.Fatal("不正な入力で削除された")
	}
	c, body := del(closed, `{"reason":"卒業のため本人から依頼","anonymizeDisplayName":true}`)
	if c != 200 {
		t.Fatalf("削除: %d %s", c, body)
	}
	uid := e.userID("closed.example")
	if n := e.count(`SELECT count(*) FROM personal_profiles WHERE user_id=$1 AND legal_name IS NULL AND student_number IS NULL AND university_email IS NULL AND nearest_station IS NULL`, uid); n != 1 {
		t.Fatal("個人情報が残っている")
	}
	if n := e.count(`SELECT count(*) FROM profiles WHERE user_id=$1 AND bio IS NULL AND faculty IS NULL AND department IS NULL AND school_year IS NULL AND display_name='退会メンバー'`, uid); n != 1 {
		t.Fatal("プロフィールの項目が残っている/表示名が匿名化されていない")
	}
	if n := e.count(`SELECT (SELECT count(*) FROM profile_links WHERE user_id=$1)+(SELECT count(*) FROM personal_devices WHERE user_id=$1)+(SELECT count(*) FROM profile_field_visibility WHERE user_id=$1)`, uid); n != 0 {
		t.Fatal("リンク・機材・公開設定が残っている")
	}
	// D08: 公開クレジット同意は撤回される
	if n := e.count(`SELECT count(*) FROM public_credit_consents WHERE user_id=$1 AND withdrawn_at IS NOT NULL`, uid); n != 1 {
		t.Fatal("公開クレジット同意が撤回されていない")
	}
	// ユーザー行と履歴（監査）の参照は維持される。監査に削除した値は残らない
	if n := e.count(`SELECT count(*) FROM users WHERE id=$1 AND status='disabled'`, uid); n != 1 {
		t.Fatal("ユーザー行が失われた")
	}
	if n := e.count(`SELECT count(*) FROM audit_logs WHERE target_id=$1`, uid.String()); n < 3 {
		t.Fatalf("監査の履歴が維持されていない: %d", n)
	}
	for _, v := range []string{"非公 花子", "22B-222", "closed@example.invalid", "非公駅", "非公開の自己紹介", "秘密学部"} {
		if n := e.count(`SELECT count(*) FROM audit_logs WHERE strpos(summary_json::text, $1) > 0`, v); n != 0 {
			t.Fatalf("監査に削除した値 %q が残っている", v)
		}
	}
	if n := e.count(`SELECT count(*) FROM audit_logs WHERE action='admin.private_profile_deleted' AND summary_json->>'reason' LIKE '卒業%'`); n != 1 {
		t.Fatal("削除の監査（理由）が無い")
	}
	// 冪等: もう一度実行しても成功し、壊れない
	if c, _ := del(closed, `{"reason":"再実行"}`); c != 200 {
		t.Fatalf("再実行: %d", c)
	}
	// 存在しない・権限なし
	if c, _ := del("00000000-0000-0000-0000-000000000000", `{"reason":"x"}`); c != 404 {
		t.Errorf("存在しない: %d", c)
	}
}
