//go:build integration

package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type linkOut struct {
	ID, Label, URL, Visibility string
	SortOrder                  int
}

type profOut struct {
	DisplayName string
	Bio         *string
	Faculty     *string
	Department  *string
	SchoolYear  *int
	Visibility  map[string]string
	Private     struct {
		LegalName, StudentNumber, UniversityEmail, NearestStation *string
	}
	Links               []linkOut
	Devices             []struct{ ID, Category, Name, Spec, Visibility string }
	PublicCreditConsent struct {
		Granted     bool
		WithdrawnAt *string
	}
}

func (e *env) getProfile(a actor) profOut {
	e.t.Helper()
	rec := e.as(a, "GET", "/me/profile", "")
	if rec.Code != 200 {
		e.t.Fatalf("プロフィール取得: %d %s", rec.Code, rec.Body)
	}
	var p profOut
	decode(e.t, rec, &p)
	return p
}

func (e *env) patchProfile(a actor, body string) *httptest.ResponseRecorder {
	return e.as(a, "PATCH", "/me/profile", body)
}

func newMember(t *testing.T) (*env, actor) {
	e := newEnv(t, opts{})
	e.addUser("alice.example", "active", goodPass)
	return e, e.signIn("alice.example")
}

func TestProfileRequiresLoginAndCSRF(t *testing.T) {
	e, a := newMember(t)
	for _, c := range []struct{ m, p, b string }{
		{"GET", "/me/profile", ""}, {"PATCH", "/me/profile", `{"bio":"x"}`}, {"PUT", "/me/profile/visibility", `{"bio":"members"}`},
		{"PUT", "/me/profile/public-credit-consent", `{"granted":true}`}, {"POST", "/me/profile/links", `{"label":"x","url":"https://example.invalid"}`},
		{"POST", "/me/devices", `{"category":"PC","name":"x"}`},
	} {
		if rec := e.do(c.m, c.p, c.b); rec.Code != 401 {
			t.Errorf("匿名 %s %s = %d", c.m, c.p, rec.Code)
		}
	}
	if rec := e.do("PATCH", "/me/profile", `{"bio":"x"}`, withCookie(a.cookie)); rec.Code != 403 {
		t.Errorf("CSRF無し = %d", rec.Code)
	}
	if rec := e.as(a, "GET", "/me/profile", ""); rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control: %s", rec.Header().Get("Cache-Control"))
	}
}

// 表示名だけで使える。他は任意で、既定は全て非公開・未同意。
func TestNewProfileHasOnlyDisplayNameAndEverythingPrivate(t *testing.T) {
	e, a := newMember(t)
	p := e.getProfile(a)
	if p.DisplayName != "表示名(alice.example)" || p.Bio != nil || p.Faculty != nil || p.SchoolYear != nil {
		t.Fatalf("初期状態: %+v", p)
	}
	for k, v := range p.Visibility {
		if v != "private" {
			t.Errorf("既定は非公開のはず: %s=%s", k, v)
		}
	}
	if p.Private.LegalName != nil || p.Private.StudentNumber != nil || p.Private.UniversityEmail != nil || p.Private.NearestStation != nil {
		t.Fatal("個人情報は未入力でよい")
	}
	if p.PublicCreditConsent.Granted || len(p.Links) != 0 || len(p.Devices) != 0 {
		t.Fatal("同意・リンク・機材は初期状態で空")
	}
}

func TestPatchProfileIsPartialAndCanClearOptionalFields(t *testing.T) {
	e, a := newMember(t)
	rec := e.patchProfile(a, `{"bio":"自己紹介です","faculty":"理工学部","department":"情報科学科","schoolYear":3,"legalName":"架空 花子","studentNumber":"99X-1234","universityEmail":"hanako@example.invalid","nearestStation":"架空駅"}`)
	if rec.Code != 200 {
		t.Fatalf("更新: %d %s", rec.Code, rec.Body)
	}
	var p profOut
	decode(t, rec, &p)
	if *p.Bio != "自己紹介です" || *p.SchoolYear != 3 || *p.Private.LegalName != "架空 花子" || *p.Private.UniversityEmail != "hanako@example.invalid" {
		t.Fatalf("反映されない: %+v", p)
	}
	// 一部だけ更新: 未指定は変更なし
	rec = e.patchProfile(a, `{"displayName":"新しい名前"}`)
	decode(t, rec, &p)
	if p.DisplayName != "新しい名前" || *p.Bio != "自己紹介です" || *p.Private.StudentNumber != "99X-1234" {
		t.Fatalf("未指定の項目が変わった: %+v", p)
	}
	// nullまたは空文字で任意項目を消去（個人情報も）
	rec = e.patchProfile(a, `{"bio":null,"faculty":"  ","schoolYear":null,"legalName":"","universityEmail":null}`)
	decode(t, rec, &p)
	if p.Bio != nil || p.Faculty != nil || p.SchoolYear != nil || p.Private.LegalName != nil || p.Private.UniversityEmail != nil || *p.Department != "情報科学科" || *p.Private.StudentNumber != "99X-1234" {
		t.Fatalf("消去/保持が想定と異なる: %+v", p)
	}
	// 表示名は消せない・長さ・形式の検証。拒否時は何も変わらない
	for name, body := range map[string]string{
		"表示名null":   `{"displayName":null}`,
		"表示名空":      `{"displayName":"   "}`,
		"表示名65文字":   `{"displayName":"` + strings.Repeat("あ", 65) + `"}`,
		"学年0":       `{"schoolYear":0}`,
		"学年11":      `{"schoolYear":11}`,
		"自己紹介1001字": `{"bio":"` + strings.Repeat("a", 1001) + `"}`,
		"学生証番号":     `{"studentNumber":"99 X"}`,
		"メール":       `{"universityEmail":"not-an-email"}`,
		"メール(ドット無)": `{"universityEmail":"a@localhost"}`,
		"未知の項目":     `{"nope":1}`,
		"型違い":       `{"bio":123}`,
	} {
		if rec := e.patchProfile(a, body); rec.Code != 422 && rec.Code != 400 {
			t.Errorf("%s: %d（拒否されるはず）", name, rec.Code)
		}
	}
	if got := e.getProfile(a); got.DisplayName != "新しい名前" || *got.Private.StudentNumber != "99X-1234" {
		t.Fatal("拒否した更新で内容が変わった")
	}
	// 監査には変更した項目名だけが残り、個人情報の値は残らない
	if n := e.count(`SELECT count(*) FROM audit_logs WHERE action='profile.updated' AND (strpos(summary_json::text,'架空 花子')>0 OR strpos(summary_json::text,'99X-1234')>0 OR strpos(summary_json::text,'hanako')>0 OR strpos(summary_json::text,'架空駅')>0 OR strpos(summary_json::text,'自己紹介です')>0)`); n != 0 {
		t.Fatal("監査に値が含まれる")
	}
	if n := e.count(`SELECT count(*) FROM audit_logs WHERE action='profile.updated'`); n != 3 {
		t.Fatalf("監査件数: %d", n)
	}
}

func TestVisibilityDefaultsPrivateAndPersonalFieldsCannotBePublished(t *testing.T) {
	e, a := newMember(t)
	rec := e.as(a, "PUT", "/me/profile/visibility", `{"bio":"members","schoolYear":"members"}`)
	var p profOut
	decode(t, rec, &p)
	if rec.Code != 200 || p.Visibility["bio"] != "members" || p.Visibility["schoolYear"] != "members" || p.Visibility["faculty"] != "private" {
		t.Fatalf("公開設定: %d %+v", rec.Code, p.Visibility)
	}
	rec = e.as(a, "PUT", "/me/profile/visibility", `{"bio":"private"}`)
	decode(t, rec, &p)
	if p.Visibility["bio"] != "private" {
		t.Fatal("非公開に戻せない")
	}
	for name, body := range map[string]string{
		"氏名":       `{"legalName":"members"}`,
		"メール":      `{"universityEmail":"members"}`,
		"学生証番号":    `{"studentNumber":"members"}`,
		"最寄り駅":     `{"nearestStation":"members"}`,
		"表示名":      `{"displayName":"members"}`,
		"値がpublic": `{"bio":"public"}`,
		"未知":       `{"x":"members"}`,
	} {
		if rec := e.as(a, "PUT", "/me/profile/visibility", body); rec.Code != 422 && rec.Code != 400 {
			t.Errorf("%s: %d（拒否されるはず）", name, rec.Code)
		}
	}
}

// インターネット公開の同意は、メンバー内公開とは別のAPIで付与・撤回する。
func TestPublicCreditConsentIsSeparateFromMemberVisibility(t *testing.T) {
	e, a := newMember(t)
	e.as(a, "PUT", "/me/profile/visibility", `{"bio":"members","faculty":"members","department":"members","schoolYear":"members"}`)
	if p := e.getProfile(a); p.PublicCreditConsent.Granted {
		t.Fatal("メンバー内公開を設定しただけでインターネット公開に同意したことになっている")
	}
	rec := e.as(a, "PUT", "/me/profile/public-credit-consent", `{"granted":true}`)
	var c struct {
		Granted     bool
		GrantedAt   *string
		WithdrawnAt *string
	}
	decode(t, rec, &c)
	if rec.Code != 200 || !c.Granted || c.GrantedAt == nil {
		t.Fatalf("付与: %d %+v", rec.Code, c)
	}
	// 同意を撤回してもメンバー内公開の設定は変わらない（別の同意）
	rec = e.as(a, "PUT", "/me/profile/public-credit-consent", `{"granted":false}`)
	decode(t, rec, &c)
	if c.Granted || c.WithdrawnAt == nil {
		t.Fatalf("撤回: %+v", c)
	}
	if p := e.getProfile(a); p.Visibility["bio"] != "members" || p.PublicCreditConsent.Granted {
		t.Fatalf("撤回がメンバー内公開に影響した、または状態が不正: %+v", p.Visibility)
	}
	// 冪等（監査は変化があったときだけ）と再付与
	e.as(a, "PUT", "/me/profile/public-credit-consent", `{"granted":false}`)
	e.as(a, "PUT", "/me/profile/public-credit-consent", `{"granted":true}`)
	rec = e.as(a, "GET", "/me/profile/public-credit-consent", "")
	decode(t, rec, &c)
	if !c.Granted || c.WithdrawnAt != nil {
		t.Fatalf("再付与: %+v", c)
	}
	if n := e.count(`SELECT count(*) FROM audit_logs WHERE action='profile.public_credit_consent_changed'`); n != 3 {
		t.Fatalf("監査は付与・撤回・再付与の3件のはず: %d", n)
	}
	if rec := e.as(a, "PUT", "/me/profile/public-credit-consent", `{}`); rec.Code != 422 {
		t.Errorf("granted未指定: %d", rec.Code)
	}
}

func TestLinksCRUDValidationAndLimits(t *testing.T) {
	e, a := newMember(t)
	rec := e.as(a, "POST", "/me/profile/links", `{"label":"GitHub","url":"https://github.com/example"}`)
	var l linkOut
	decode(t, rec, &l)
	if rec.Code != 201 || l.Visibility != "private" || l.URL != "https://github.com/example" {
		t.Fatalf("作成: %d %+v", rec.Code, l)
	}
	for name, u := range map[string]string{
		"javascript": "javascript:alert(1)", "data": "data:text/html,x", "ftp": "ftp://example.invalid/x", "相対": "/path", "ホスト無し": "https://",
		"認証情報つき": "https://user:pass@example.invalid/", "空白": "https://example.invalid/a b", "長すぎ": "https://example.invalid/" + strings.Repeat("a", 2100), "空": "",
	} {
		if rec := e.as(a, "POST", "/me/profile/links", `{"label":"x","url":`+jsonStr(u)+`}`); rec.Code != 422 && rec.Code != 400 {
			t.Errorf("URL %s: %d（拒否されるはず）", name, rec.Code)
		}
	}
	if rec := e.as(a, "POST", "/me/profile/links", `{"label":"","url":"https://example.invalid"}`); rec.Code != 422 {
		t.Errorf("名前が空: %d", rec.Code)
	}
	// 更新（部分）・公開範囲・並べ替え
	rec = e.as(a, "PATCH", "/me/profile/links/"+l.ID, `{"visibility":"members","sortOrder":5}`)
	decode(t, rec, &l)
	if rec.Code != 200 || l.Visibility != "members" || l.SortOrder != 5 || l.Label != "GitHub" {
		t.Fatalf("部分更新: %d %+v", rec.Code, l)
	}
	if rec := e.as(a, "PATCH", "/me/profile/links/"+l.ID, `{"url":"javascript:alert(1)"}`); rec.Code != 422 {
		t.Errorf("更新でjavascript: %d", rec.Code)
	}
	// 上限10件
	for i := 1; i < 10; i++ {
		if rec := e.as(a, "POST", "/me/profile/links", `{"label":"L","url":"https://example.invalid/`+itoaT(i)+`"}`); rec.Code != 201 {
			t.Fatalf("%d件目: %d", i+1, rec.Code)
		}
	}
	if rec := e.as(a, "POST", "/me/profile/links", `{"label":"L","url":"https://example.invalid/over"}`); rec.Code != 409 {
		t.Errorf("11件目: %d（409のはず）", rec.Code)
	}
	if rec := e.as(a, "DELETE", "/me/profile/links/"+l.ID, ""); rec.Code != 204 {
		t.Fatalf("削除: %d", rec.Code)
	}
	if rec := e.as(a, "DELETE", "/me/profile/links/"+l.ID, ""); rec.Code != 404 {
		t.Errorf("削除済みの再削除: %d", rec.Code)
	}
}

func TestOthersItemsAreNotAccessible(t *testing.T) {
	e, a := newMember(t)
	e.addUser("bob.example", "active", goodPass)
	b := e.signIn("bob.example")
	var l linkOut
	decode(t, e.as(a, "POST", "/me/profile/links", `{"label":"自分","url":"https://example.invalid/a"}`), &l)
	var d struct{ ID string }
	decode(t, e.as(a, "POST", "/me/devices", `{"category":"PC","name":"自作PC","spec":"RTX"}`), &d)
	for _, c := range []struct{ m, p, body string }{
		{"PATCH", "/me/profile/links/" + l.ID, `{"label":"乗っ取り"}`}, {"DELETE", "/me/profile/links/" + l.ID, ""},
		{"PATCH", "/me/devices/" + d.ID, `{"name":"乗っ取り"}`}, {"DELETE", "/me/devices/" + d.ID, ""},
	} {
		if rec := e.as(b, c.m, c.p, c.body); rec.Code != 404 {
			t.Errorf("他人の項目への %s %s = %d（404のはず）", c.m, c.p, rec.Code)
		}
	}
	if got := e.getProfile(a); len(got.Links) != 1 || got.Links[0].Label != "自分" || len(got.Devices) != 1 || got.Devices[0].Name != "自作PC" {
		t.Fatal("他人の操作で内容が変わった")
	}
	if got := e.getProfile(b); len(got.Links) != 0 || len(got.Devices) != 0 {
		t.Fatal("他人の項目が自分のプロフィールに出る")
	}
}

func TestDevicesCRUDAndLimits(t *testing.T) {
	e, a := newMember(t)
	rec := e.as(a, "POST", "/me/devices", `{"category":"PC","name":"自作PC","spec":"RTX 4070 / 32GB"}`)
	var d struct{ ID, Category, Name, Spec, Visibility string }
	decode(t, rec, &d)
	if rec.Code != 201 || d.Spec != "RTX 4070 / 32GB" || d.Visibility != "private" {
		t.Fatalf("作成: %d %+v", rec.Code, d)
	}
	for name, body := range map[string]string{
		"種類なし": `{"name":"x"}`, "名前なし": `{"category":"PC"}`, "名前101字": `{"category":"PC","name":"` + strings.Repeat("a", 101) + `"}`,
		"詳細501字": `{"category":"PC","name":"x","spec":"` + strings.Repeat("a", 501) + `"}`, "公開範囲": `{"category":"PC","name":"x","visibility":"public"}`,
	} {
		if rec := e.as(a, "POST", "/me/devices", body); rec.Code != 422 && rec.Code != 400 {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	rec = e.as(a, "PATCH", "/me/devices/"+d.ID, `{"visibility":"members","spec":""}`)
	decode(t, rec, &d)
	if rec.Code != 200 || d.Visibility != "members" || d.Spec != "" || d.Name != "自作PC" {
		t.Fatalf("部分更新: %d %+v", rec.Code, d)
	}
	for i := 1; i < 20; i++ {
		if rec := e.as(a, "POST", "/me/devices", `{"category":"PC","name":"d`+itoaT(i)+`"}`); rec.Code != 201 {
			t.Fatalf("%d件目: %d", i+1, rec.Code)
		}
	}
	if rec := e.as(a, "POST", "/me/devices", `{"category":"PC","name":"over"}`); rec.Code != 409 {
		t.Errorf("21件目: %d", rec.Code)
	}
	if rec := e.as(a, "DELETE", "/me/devices/"+d.ID, ""); rec.Code != 204 {
		t.Errorf("削除: %d", rec.Code)
	}
	if rec := e.as(a, "PATCH", "/me/devices/not-a-uuid", `{"name":"x"}`); rec.Code != 404 {
		t.Errorf("不正なID: %d", rec.Code)
	}
}

func jsonStr(s string) string { b, _ := json.Marshal(s); return string(b) }
func itoaT(n int) string      { b, _ := json.Marshal(n); return string(b) }

var _ = http.StatusOK
