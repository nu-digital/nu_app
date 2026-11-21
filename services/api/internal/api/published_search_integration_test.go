//go:build integration

package api_test

import (
	"encoding/json"
	"fmt"
	"net/url"
	"testing"
)

type searchOut struct {
	Items []struct {
		Section, Slug, Title string
		Found                []string
		Snippet              string
	}
	Truncated bool
}

func (e *env) searchPub(a actor, q string) (int, searchOut) {
	e.t.Helper()
	rec := e.as(a, "GET", "/admin/contents/published-search?q="+url.QueryEscape(q), "")
	var out searchOut
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// 公開中の版だけが対象。題名・本文・役割名のどこに語があるかを返す。権限・入力の検査・語を監査に残さない。
func TestPublishedSearch(t *testing.T) {
	e, as := cmsSetup(t)
	mk := func(slug, title, md string) string {
		_, v := e.createContent(as.editor, fmt.Sprintf(`{"type":"article","section":"blog","slug":%q,"title":%q,"markdown":%q}`, slug, title, md))
		return v.ID
	}
	a := mk("has-name", "活動報告", "山田花子さんが登壇しました。")
	b := mk("no-name", "別の記事", "関係のない本文")
	c := mk("draft-only", "下書きだけ", "山田花子の下書き") // 公開しない
	d := mk("title-name", "山田花子のインタビュー", "本文")
	for _, id := range []string{a, b, d} {
		if code, _ := e.publish(as.editor, id, 1); code != 200 {
			t.Fatalf("公開できない: %d", code)
		}
	}
	_ = c
	// 作品の役割名
	_, w := e.createContent(as.author, `{"type":"project","section":"project","slug":"work-1","title":"作品","markdown":"本文"}`)
	e.as(as.author, "PUT", "/contents/"+w.ID+"/contributors", fmt.Sprintf(`{"baseRevision":1,"contributors":[{"userId":%q,"roleLabel":"山田花子（音楽）"}]}`, e.userID("author.example").String()))
	if code, _ := e.publish(as.editor, w.ID, 2); code != 200 {
		t.Fatal("作品を公開できない")
	}

	// 権限: Admin/Ownerだけ
	for name, act := range map[string]actor{"メンバー": as.author, "Editor": as.editor} {
		if code, _ := e.searchPub(act, "山田花子"); code != 403 {
			t.Errorf("%s: %d", name, code)
		}
	}
	if rec := e.do("GET", "/admin/contents/published-search?q=abc", ""); rec.Code != 401 {
		t.Errorf("匿名: %d", rec.Code)
	}
	for _, q := range []string{"", "山", " ", "あ"} {
		if code, _ := e.searchPub(as.admin, q); code != 422 {
			t.Errorf("短い語 %q: %d", q, code)
		}
	}
	code, out := e.searchPub(as.admin, "山田花子")
	if code != 200 {
		t.Fatalf("検索: %d", code)
	}
	got := map[string][]string{}
	for _, it := range out.Items {
		got[it.Slug] = it.Found
	}
	if len(got) != 3 || len(got["has-name"]) != 1 || got["has-name"][0] != "markdown" || len(got["title-name"]) != 1 || got["title-name"][0] != "title" || len(got["work-1"]) != 1 || got["work-1"][0] != "roleLabel" {
		t.Errorf("結果: %+v", got)
	}
	if _, ok := got["draft-only"]; ok {
		t.Error("公開していない記事が出ている")
	}
	if _, ok := got["no-name"]; ok {
		t.Error("語を含まない記事が出ている")
	}
	for _, it := range out.Items {
		if it.Slug == "has-name" && it.Snippet == "" {
			t.Error("抜粋が無い")
		}
	}
	// 公開後に編集して語を消し、再公開すると、もう出ない（公開中の版が対象）
	if code, _ := e.saveRev(as.editor, a, 1, "活動報告", "ある登壇者が登壇しました。"); code != 201 {
		t.Fatal("保存できない")
	}
	if _, o2 := e.searchPub(as.admin, "山田花子"); len(o2.Items) != 3 {
		t.Error("公開前の編集が検索に影響した（公開中の版だけが対象のはず）")
	}
	if code, _ := e.publish(as.editor, a, 2); code != 200 {
		t.Fatal("再公開")
	}
	if _, o3 := e.searchPub(as.admin, "山田花子"); len(o3.Items) != 2 {
		t.Errorf("再公開後: %d件", len(o3.Items))
	}
	// 大文字小文字を区別しない（SQLのlower()）
	asc := mk("ascii", "Hello World", "contains Alice Smith here")
	if code, _ := e.publish(as.editor, asc, 1); code != 200 {
		t.Fatal("公開できない")
	}
	if _, o4 := e.searchPub(as.admin, "aLiCe sMiTh"); len(o4.Items) != 1 || o4.Items[0].Slug != "ascii" {
		t.Errorf("大文字小文字を区別せずに探せない: %+v", o4.Items)
	}
	// 検索語は監査に残らない。検索したことは残る
	if e.count(`SELECT count(*)::int FROM audit_logs WHERE summary_json::text LIKE '%山田%'`) != 0 {
		t.Error("監査に検索語が残っている")
	}
	if e.count(`SELECT count(*)::int FROM audit_logs WHERE action='admin.published_search'`) < 1 {
		t.Error("検索の監査が無い")
	}
}
