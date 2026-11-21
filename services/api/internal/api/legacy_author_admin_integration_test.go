//go:build integration

package api_test

import (
	"context"
	"strings"
	"testing"
)

func (e *env) authorID(name string) string {
	e.t.Helper()
	var id string
	if err := e.pool.QueryRow(context.Background(), `SELECT id::text FROM legacy_authors WHERE source_name=$1`, name).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *env) authorState(id string) (state string, linked bool, erased bool) {
	e.t.Helper()
	if err := e.pool.QueryRow(context.Background(), `SELECT publication_state, linked_user_id IS NOT NULL, erased_at IS NOT NULL FROM legacy_authors WHERE id=$1`, id).Scan(&state, &linked, &erased); err != nil {
		e.t.Fatal(err)
	}
	return
}

// 権限: 一覧・許可・撤回・紐付けはAdmin/Ownerだけ。Editor・メンバー・未ログインは拒否される。
func TestLegacyAuthorAdminAuthz(t *testing.T) {
	e, as := legacySetup(t)
	id := e.authorID("iwa")
	calls := [][3]string{
		{"GET", "/admin/legacy-authors", ""},
		{"POST", "/admin/legacy-authors/" + id + "/permit", `{"note":"確認済み"}`},
		{"POST", "/admin/legacy-authors/" + id + "/withdraw", ""},
		{"PUT", "/admin/legacy-authors/" + id + "/link", `{"userId":"` + e.userID("author.example").String() + `"}`},
		{"DELETE", "/admin/legacy-authors/" + id + "/link", ""},
	}
	for _, c := range calls {
		for name, a := range map[string]actor{"member": as.author, "editor": as.editor} {
			if rec := e.as(a, c[0], c[1], c[2]); rec.Code != 403 {
				t.Errorf("%s %s (%s): %d", c[0], c[1], name, rec.Code)
			}
		}
		if rec := e.do(c[0], c[1], c[2]); rec.Code != 401 {
			t.Errorf("%s %s 未ログイン: %d", c[0], c[1], rec.Code)
		}
	}
	if s, l, _ := e.authorState(id); s != "unconfirmed" || l {
		t.Errorf("拒否された操作で状態が変わった: %s %v", s, l)
	}
}

// 許可の確認は理由が必須で、確認者と日時が記録される。公開APIに名前が出る。撤回で止まり、再許可できる。監査に名前・理由が入らない。
func TestLegacyAuthorPermitWithdrawFlow(t *testing.T) {
	e, as := legacySetup(t)
	id := e.authorID("iwa")
	sec, slug := articleBy(t, "iwa")
	path := "/admin/legacy-authors/" + id
	if rec := e.as(as.admin, "POST", path+"/permit", `{"note":"  "}`); rec.Code != 422 {
		t.Errorf("理由なし: %d", rec.Code)
	}
	if rec := e.as(as.admin, "POST", "/admin/legacy-authors/00000000-0000-0000-0000-000000000000/permit", `{"note":"x"}`); rec.Code != 404 {
		t.Errorf("存在しない: %d", rec.Code)
	}
	if e.authorOf(sec, slug) != "nu_digital" {
		t.Fatal("初期は未確認")
	}
	if rec := e.as(as.admin, "POST", path+"/permit", `{"note":"本人にDiscordで確認した"}`); rec.Code != 204 {
		t.Fatalf("許可: %d %s", rec.Code, rec.Body.String())
	}
	if e.authorOf(sec, slug) != "iwa" {
		t.Error("許可後に名前が出ない")
	}
	if e.count(`SELECT count(*)::int FROM legacy_authors WHERE id=$1::uuid AND confirmed_by=(SELECT id FROM users WHERE login_id='admin.example') AND confirmed_at IS NOT NULL`, id) != 1 {
		t.Error("確認者・日時が記録されていない")
	}
	rec := e.as(as.admin, "GET", "/admin/legacy-authors", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"name":"iwa"`) {
		t.Errorf("一覧: %d", rec.Code)
	}
	if rec := e.as(as.admin, "POST", path+"/withdraw", ""); rec.Code != 204 {
		t.Fatalf("撤回: %d", rec.Code)
	}
	if rec := e.as(as.admin, "POST", path+"/withdraw", ""); rec.Code != 204 { // 冪等
		t.Errorf("撤回の再実行: %d", rec.Code)
	}
	_, d, body := e.pubDetail(sec, slug)
	if d.Author != "nu_digital" || strings.Contains(body, "iwa") {
		t.Errorf("撤回後: %q", d.Author)
	}
	if rec := e.as(as.admin, "POST", path+"/permit", `{"note":"再確認"}`); rec.Code != 204 || e.authorOf(sec, slug) != "iwa" {
		t.Error("再許可")
	}
	// 監査ログに名前・確認の理由が入らない
	if e.count(`SELECT count(*)::int FROM audit_logs WHERE target_type='legacy_author' AND (summary_json::text LIKE '%iwa%' OR summary_json::text LIKE '%Discord%')`) != 0 {
		t.Error("監査に名前・理由が入っている")
	}
	if e.count(`SELECT count(*)::int FROM audit_logs WHERE target_type='legacy_author' AND action IN ('legacy_author.permitted','legacy_author.withdrawn')`) != 4 {
		t.Error("監査の件数")
	}
}

// 紐付けはユーザーIDだけ。公開可否は変わらず、同じ表示名の別ユーザーには紐付かない。存在しないユーザーは拒否。
func TestLegacyAuthorLinkByIDOnly(t *testing.T) {
	e, as := legacySetup(t)
	e.exec(`UPDATE profiles SET display_name='yurarara' WHERE user_id IN ($1,$2)`, e.userID("author.example"), e.userID("other.example"))
	id := e.authorID("yurarara")
	sec, slug := articleBy(t, "yurarara")
	path := "/admin/legacy-authors/" + id + "/link"
	if rec := e.as(as.admin, "PUT", path, `{"userId":"yurarara"}`); rec.Code != 422 {
		t.Errorf("表示名では紐付けられない: %d", rec.Code)
	}
	if rec := e.as(as.admin, "PUT", path, `{"userId":"00000000-0000-0000-0000-000000000000"}`); rec.Code != 422 {
		t.Errorf("存在しないユーザー: %d", rec.Code)
	}
	if rec := e.as(as.admin, "PUT", path, `{"userId":"`+e.userID("other.example").String()+`"}`); rec.Code != 204 {
		t.Fatalf("紐付け: %d", rec.Code)
	}
	if e.count(`SELECT count(*)::int FROM legacy_authors WHERE linked_user_id=(SELECT id FROM users WHERE login_id='other.example')`) != 1 ||
		e.count(`SELECT count(*)::int FROM legacy_authors WHERE linked_user_id=(SELECT id FROM users WHERE login_id='author.example')`) != 0 {
		t.Error("紐付け先が違う")
	}
	if st, _, _ := e.authorState(id); st != "unconfirmed" || e.authorOf(sec, slug) != "nu_digital" {
		t.Error("紐付けで公開された")
	}
	e.as(as.admin, "POST", "/admin/legacy-authors/"+id+"/permit", `{"note":"確認"}`)
	if rec := e.as(as.admin, "DELETE", path, ""); rec.Code != 204 {
		t.Fatalf("解除: %d", rec.Code)
	}
	if st, l, _ := e.authorState(id); st != "permitted" || l || e.authorOf(sec, slug) != "yurarara" {
		t.Error("解除で公開可否が変わった")
	}
	// 停止・停止解除でも公開可否は変わらない（紐付けた上で）
	e.as(as.admin, "PUT", path, `{"userId":"`+e.userID("collab.example").String()+`"}`)
	e.exec(`UPDATE users SET status='disabled', disabled_at=now() WHERE login_id='collab.example'`)
	if st, _, _ := e.authorState(id); st != "permitted" {
		t.Error("停止で変わった")
	}
}

// 同意の撤回で、紐付く投稿者の公開が止まる。同意を再度与えても自動では再公開されない。紐付かない投稿者は影響を受けない。
func TestLegacyAuthorFollowsConsentWithdrawal(t *testing.T) {
	e, as := legacySetup(t)
	idY, idI := e.authorID("yurarara"), e.authorID("iwa")
	secY, slugY := articleBy(t, "yurarara")
	secI, slugI := articleBy(t, "iwa")
	for _, id := range []string{idY, idI} {
		e.as(as.admin, "POST", "/admin/legacy-authors/"+id+"/permit", `{"note":"確認"}`)
	}
	e.as(as.admin, "PUT", "/admin/legacy-authors/"+idY+"/link", `{"userId":"`+e.userID("collab.example").String()+`"}`)
	if rec := e.as(as.collab, "PUT", "/me/profile/public-credit-consent", `{"granted":true}`); rec.Code != 200 && rec.Code != 204 {
		t.Fatal(rec.Code)
	}
	if e.authorOf(secY, slugY) != "yurarara" {
		t.Fatal("許可済み")
	}
	if rec := e.as(as.collab, "PUT", "/me/profile/public-credit-consent", `{"granted":false}`); rec.Code != 200 && rec.Code != 204 {
		t.Fatal(rec.Code)
	}
	if e.authorOf(secY, slugY) != "nu_digital" {
		t.Error("同意の撤回後も名前が出る")
	}
	if e.authorOf(secI, slugI) != "iwa" {
		t.Error("紐付かない投稿者が巻き込まれた")
	}
	e.as(as.collab, "PUT", "/me/profile/public-credit-consent", `{"granted":true}`)
	if e.authorOf(secY, slugY) != "nu_digital" {
		t.Error("同意の再付与で自動的に再公開された")
	}
	if e.count(`SELECT count(*)::int FROM audit_logs WHERE action='legacy_author.withdrawn_by_consent'`) != 1 {
		t.Error("連動の監査")
	}
}

// 個人情報の削除で、紐付く投稿者の名前は匿名の記号に置換され、公開APIからも一覧からも取得できず、再許可もできない。
func TestLegacyAuthorErasedWithPersonalData(t *testing.T) {
	e, as := legacySetup(t)
	e.addUser("gone.example", "disabled", goodPass)
	id := e.authorID("iwa")
	sec, slug := articleBy(t, "iwa")
	e.as(as.admin, "POST", "/admin/legacy-authors/"+id+"/permit", `{"note":"確認"}`)
	e.as(as.admin, "PUT", "/admin/legacy-authors/"+id+"/link", `{"userId":"`+e.userID("gone.example").String()+`"}`)
	if e.authorOf(sec, slug) != "iwa" {
		t.Fatal("許可済み")
	}
	if rec := e.as(as.admin, "DELETE", "/admin/users/"+e.userID("gone.example").String()+"/private-profile", `{"reason":"本人の依頼"}`); rec.Code != 200 {
		t.Fatalf("削除: %d %s", rec.Code, rec.Body.String())
	}
	_, d, body := e.pubDetail(sec, slug)
	list := e.pub("?limit=100").Body.String()
	if d.Author != "nu_digital" || strings.Contains(body, "iwa") || strings.Contains(list, `"iwa"`) {
		t.Error("削除後も公開経路から名前が取得できる")
	}
	if e.count(`SELECT count(*)::int FROM legacy_authors WHERE source_name='iwa'`) != 0 {
		t.Error("名前がDBに残っている")
	}
	if rec := e.as(as.admin, "GET", "/admin/legacy-authors", ""); strings.Contains(rec.Body.String(), `"name":"iwa"`) || !strings.Contains(rec.Body.String(), `"erased":true`) {
		t.Error("管理者の一覧にも名前が残っている／削除済みの表示がない")
	}
	if rec := e.as(as.admin, "POST", "/admin/legacy-authors/"+id+"/permit", `{"note":"再度"}`); rec.Code != 409 {
		t.Errorf("削除済みの再許可: %d", rec.Code)
	}
	if rec := e.as(as.admin, "PUT", "/admin/legacy-authors/"+id+"/link", `{"userId":"`+e.userID("author.example").String()+`"}`); rec.Code != 409 {
		t.Errorf("削除済みへの紐付け: %d", rec.Code)
	}
	if e.count(`SELECT count(*)::int FROM audit_logs WHERE action='legacy_author.erased'`) != 1 {
		t.Error("削除の監査")
	}
}
