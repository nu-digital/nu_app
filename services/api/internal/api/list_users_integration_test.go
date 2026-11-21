//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"testing"
)

type listResp struct {
	Items []struct {
		ID, LoginID, DisplayName, Status string
		Roles                            []string
	} `json:"items"`
	NextCursor *string `json:"nextCursor"`
}

func (e *env) list(a actor, query string) (int, listResp, string) {
	rec := e.as(a, "GET", "/admin/users"+query, "")
	var lr listResp
	_ = json.Unmarshal(rec.Body.Bytes(), &lr)
	return rec.Code, lr, rec.Body.String()
}

func TestListUsersAuthorizationAndNoSensitiveFields(t *testing.T) {
	e := newEnv(t, opts{})
	admin, owner := e.adminSetup()
	e.addUser("member.example", "active", goodPass)
	e.addUser("editor.example", "active", goodPass, "editor")
	member, editor := e.signIn("member.example"), e.signIn("editor.example")
	// 個人情報を持つユーザーがいても、一覧には出ない
	if _, err := e.pool.Exec(ctxBG(), `INSERT INTO personal_profiles (user_id, legal_name, student_number, university_email, nearest_station) SELECT id, '架空 太郎', '99X9999', 'taro@example.invalid', '架空駅' FROM users WHERE login_id='member.example'`); err != nil {
		t.Fatal(err)
	}
	if rec := e.do("GET", "/admin/users", ""); rec.Code != 401 {
		t.Errorf("匿名 = %d", rec.Code)
	}
	for name, a := range map[string]actor{"member": member, "editor": editor} {
		if c, _, _ := e.list(a, ""); c != 403 {
			t.Errorf("%s = %d（403のはず）", name, c)
		}
	}
	for name, a := range map[string]actor{"admin": admin, "owner": owner} {
		c, lr, body := e.list(a, "")
		if c != 200 || len(lr.Items) != 4 {
			t.Fatalf("%s: %d 件数=%d", name, c, len(lr.Items))
		}
		for _, secret := range []string{"架空 太郎", "99X9999", "taro@example.invalid", "架空駅", "password", "argon2", "token_hash", "legal_name", "student", "email"} {
			if strings.Contains(strings.ToLower(body), strings.ToLower(secret)) {
				t.Fatalf("%s: 一覧に %q が含まれる: %s", name, secret, body)
			}
		}
		// 項目は許可リストのみ
		var raw struct{ Items []map[string]any }
		_ = json.Unmarshal([]byte(body), &raw)
		allowed := map[string]bool{"id": true, "loginId": true, "displayName": true, "status": true, "roles": true, "createdAt": true, "activatedAt": true}
		for k := range raw.Items[0] {
			if !allowed[k] {
				t.Fatalf("想定外の項目 %q が一覧に含まれる", k)
			}
		}
	}
	_, lr, _ := e.list(owner, "")
	roles := map[string]string{}
	for _, it := range lr.Items {
		roles[it.LoginID] = strings.Join(it.Roles, ",")
	}
	if roles["owner.example"] != "owner" || roles["admin.example"] != "admin" || roles["editor.example"] != "editor" || roles["member.example"] != "" {
		t.Fatalf("ロールの表示: %v", roles)
	}
}

func TestListUsersSearchAndEscaping(t *testing.T) {
	e := newEnv(t, opts{})
	_, owner := e.adminSetup()
	for _, id := range []string{"alice.example", "bob_smith.example", "carol.example"} {
		e.addUser(id, "active", goodPass)
	}
	if _, err := e.pool.Exec(ctxBG(), `UPDATE profiles SET display_name='100%達成者' WHERE user_id=(SELECT id FROM users WHERE login_id='carol.example')`); err != nil {
		t.Fatal(err)
	}
	count := func(q string) int {
		_, lr, _ := e.list(owner, "?q="+url.QueryEscape(q))
		return len(lr.Items)
	}
	if n := count("ALICE"); n != 1 {
		t.Errorf("大文字小文字を区別せず部分一致: %d", n)
	}
	if n := count("表示名(alice"); n != 1 {
		t.Errorf("表示名で検索: %d", n)
	}
	// LIKEのワイルドカードは文字として扱う: "_" は bob_smith だけ、"%" は100%達成者だけ
	if n := count("_"); n != 1 {
		t.Errorf("'_'がワイルドカードとして働いた: %d", n)
	}
	if n := count("%"); n != 1 {
		t.Errorf("'%%'がワイルドカードとして働いた: %d", n)
	}
	if n := count(`\`); n != 0 {
		t.Errorf("バックスラッシュ: %d", n)
	}
	if n := count("該当なし"); n != 0 {
		t.Errorf("該当なし: %d", n)
	}
	if c, _, _ := e.list(owner, "?q="+strings.Repeat("a", 101)); c != 400 {
		t.Errorf("長すぎる検索語: %d", c)
	}
}

func TestListUsersPagination(t *testing.T) {
	e := newEnv(t, opts{})
	_, owner := e.adminSetup() // owner + admin
	for i := 0; i < 7; i++ {
		e.addUser(fmt.Sprintf("user%02d.example", i), "invited", "")
	}
	var seen []string
	cursor := ""
	pages := 0
	for {
		q := "?limit=4"
		if cursor != "" {
			q += "&cursor=" + url.QueryEscape(cursor)
		}
		c, lr, body := e.list(owner, q)
		if c != 200 {
			t.Fatalf("ページ取得: %d %s", c, body)
		}
		pages++
		for _, it := range lr.Items {
			seen = append(seen, it.LoginID)
		}
		if lr.NextCursor == nil {
			break
		}
		cursor = *lr.NextCursor
		if pages > 10 {
			t.Fatal("ページングが終わらない")
		}
	}
	if len(seen) != 9 || pages != 3 {
		t.Fatalf("件数=%d ページ=%d（9件・3ページのはず）", len(seen), pages)
	}
	sorted := append([]string(nil), seen...)
	sort.Strings(sorted)
	for i := 1; i < len(sorted); i++ {
		if sorted[i] == sorted[i-1] {
			t.Fatalf("重複: %s", sorted[i])
		}
	}
	for _, bad := range []string{"?limit=0", "?limit=101", "?limit=abc", "?cursor=!!!", "?cursor=" + url.QueryEscape("bm90LWEtY3Vyc29y")} {
		if c, _, _ := e.list(owner, bad); c != 400 {
			t.Errorf("不正なクエリ %s = %d", bad, c)
		}
	}
}

func ctxBG() context.Context { return context.Background() }
