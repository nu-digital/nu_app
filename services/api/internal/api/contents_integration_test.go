//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type cRev struct {
	RevisionNo int
	Title      string
	Summary    string
	Markdown   string
	Metadata   map[string]string
	Date       struct {
		Label               string
		Kind                string
		SortDate            *string
		SortDateIsEstimated bool
	}
	Contributors []struct{ UserID, DisplayName, RoleLabel string }
}

type cView struct {
	ID, Type, Section, Slug string
	Creator                 struct{ UserID, DisplayName string }
	WorkingRevision         cRev
	PublishedRevisionNo     *int
	Collaborators           []struct{ UserID, DisplayName string }
	Revisions               []struct{ RevisionNo int }
	CanManageCollaborators  bool
	CanPublish              bool
}

type cActors struct {
	author, collab, other, editor, admin actor
}

// 作者・共同編集者候補・無関係なメンバー・Editor・Adminを用意する。表示名にログインIDを含めない。
func cmsSetup(t *testing.T) (*env, cActors) {
	e := newEnv(t, opts{})
	e.addUser("author.example", "active", goodPass)
	e.addUser("collab.example", "active", goodPass)
	e.addUser("other.example", "active", goodPass)
	e.addUser("editor.example", "active", goodPass, "editor")
	e.addUser("admin.example", "active", goodPass, "admin")
	as := cActors{e.signIn("author.example"), e.signIn("collab.example"), e.signIn("other.example"), e.signIn("editor.example"), e.signIn("admin.example")}
	for k, name := range map[string]actor{"作者A": as.author, "共同B": as.collab, "他人C": as.other, "編集者D": as.editor, "管理者E": as.admin} {
		if rec := e.patchProfile(name, fmt.Sprintf(`{"displayName":%q}`, k)); rec.Code != 200 {
			t.Fatalf("表示名: %d", rec.Code)
		}
	}
	return e, as
}

func (e *env) createContent(a actor, body string) (int, cView) {
	e.t.Helper()
	rec := e.as(a, "POST", "/contents", body)
	var v cView
	_ = json.Unmarshal(rec.Body.Bytes(), &v)
	return rec.Code, v
}

func article(slug string) string {
	return fmt.Sprintf(`{"type":"article","section":"blog","slug":%q,"title":"最初のタイトル","summary":"要約","markdown":"## 見出し\n\n本文","metadata":{"category":"Blog"}}`, slug)
}

func project(slug string) string {
	return fmt.Sprintf(`{"type":"project","section":"project","slug":%q,"title":"作品","markdown":"本文"}`, slug)
}

func (e *env) saveRev(a actor, id string, base int, title, md string) (int, *httptest.ResponseRecorder) {
	e.t.Helper()
	rec := e.as(a, "POST", "/contents/"+id+"/revisions", fmt.Sprintf(`{"baseRevision":%d,"title":%q,"summary":"要約","markdown":%q,"metadata":{"category":"Blog"}}`, base, title, md))
	return rec.Code, rec
}

func (e *env) getContent(a actor, id string) (int, cView) {
	e.t.Helper()
	rec := e.as(a, "GET", "/contents/"+id, "")
	var v cView
	_ = json.Unmarshal(rec.Body.Bytes(), &v)
	return rec.Code, v
}

func (e *env) revCount(id string) int {
	return e.count(`SELECT count(*)::int FROM content_revisions WHERE content_id=$1`, id)
}

func (e *env) publishDirect(id string, revNo int) {
	e.t.Helper() // 公開API（P3-01b）が無いため、公開済みの状態をSQLで作る（一覧の漏えい検査用）
	if _, err := e.pool.Exec(context.Background(), `UPDATE contents SET published_revision_id=(SELECT id FROM content_revisions WHERE content_id=$1 AND revision_no=$2) WHERE id=$1`, id, revNo); err != nil {
		e.t.Fatal(err)
	}
}

func TestContentCreate(t *testing.T) {
	e, as := cmsSetup(t)
	if rec := e.do("POST", "/contents", article("a")); rec.Code != 401 {
		t.Errorf("匿名: %d", rec.Code)
	}
	code, v := e.createContent(as.author, article("first-post"))
	if code != 201 || v.WorkingRevision.RevisionNo != 1 || v.Creator.DisplayName != "作者A" || v.PublishedRevisionNo != nil || v.WorkingRevision.Date.Kind != "unknown" || v.CanPublish {
		t.Fatalf("作成: %d %+v", code, v)
	}
	if !v.CanManageCollaborators {
		t.Error("作者は共同編集者を管理できる")
	}
	if n := e.count(`SELECT count(*)::int FROM audit_logs WHERE action='content.created'`); n != 1 {
		t.Errorf("監査ログ: %d", n)
	}
	// 入力の検査
	for name, body := range map[string]string{
		"slugが空":         `{"type":"article","section":"blog","slug":"","title":"t","markdown":"x"}`,
		"slugに日本語":       `{"type":"article","section":"blog","slug":"記事","title":"t","markdown":"x"}`,
		"slugがドット始まり":    `{"type":"article","section":"blog","slug":".x","title":"t","markdown":"x"}`,
		"slugにスラッシュ":     `{"type":"article","section":"blog","slug":"a/b","title":"t","markdown":"x"}`,
		"slugが長すぎる":      fmt.Sprintf(`{"type":"article","section":"blog","slug":%q,"title":"t","markdown":"x"}`, strings.Repeat("a", 101)),
		"記事なのにproject区分": `{"type":"article","section":"project","slug":"a","title":"t","markdown":"x"}`,
		"作品なのにblog区分":    `{"type":"project","section":"blog","slug":"a","title":"t","markdown":"x"}`,
		"未知の種別":          `{"type":"page","section":"blog","slug":"a","title":"t","markdown":"x"}`,
		"タイトルが空":         `{"type":"article","section":"blog","slug":"a","title":"  ","markdown":"x"}`,
		"タイトルに改行":        `{"type":"article","section":"blog","slug":"a","title":"a\nb","markdown":"x"}`,
		"タイトルが201文字":     fmt.Sprintf(`{"type":"article","section":"blog","slug":"a","title":%q,"markdown":"x"}`, strings.Repeat("あ", 201)),
		"要約が501文字":       fmt.Sprintf(`{"type":"article","section":"blog","slug":"a","title":"t","summary":%q,"markdown":"x"}`, strings.Repeat("あ", 501)),
		"未知のメタデータ":       `{"type":"article","section":"blog","slug":"a","title":"t","markdown":"x","metadata":{"evil":"1"}}`,
		"未知のフィールド":       `{"type":"article","section":"blog","slug":"a","title":"t","markdown":"x","creatorUserId":"x"}`,
		"dayなのに日付なし":     `{"type":"article","section":"blog","slug":"a","title":"t","markdown":"x","date":{"label":"2026年","kind":"day"}}`,
		"日付の形式":          `{"type":"article","section":"blog","slug":"a","title":"t","markdown":"x","date":{"label":"x","kind":"day","sortDate":"2026/11/21"}}`,
		"日付の種類":          `{"type":"article","section":"blog","slug":"a","title":"t","markdown":"x","date":{"label":"x","kind":"week","sortDate":"2026-11-21"}}`,
		"scriptを含む本文":    `{"type":"article","section":"blog","slug":"a","title":"t","markdown":"<script>alert(1)</script>"}`,
		"javascriptリンク":  `{"type":"article","section":"blog","slug":"a","title":"t","markdown":"[x](javascript:alert(1))"}`,
		"外部画像":           `{"type":"article","section":"blog","slug":"a","title":"t","markdown":"![x](https://tracker.example/p.png)"}`,
		"HTMLコメント":       `{"type":"article","section":"blog","slug":"a","title":"t","markdown":"<!-- 内部メモ -->"}`,
	} {
		rec := e.as(as.author, "POST", "/contents", body)
		if rec.Code != 422 && rec.Code != 400 {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "alert(") || strings.Contains(rec.Body.String(), "tracker.example") {
			t.Errorf("%s: 応答に入力の内容が出ている", name)
		}
	}
	if n := e.count(`SELECT count(*)::int FROM contents`); n != 1 {
		t.Errorf("拒否した入力でコンテンツができた: %d", n)
	}
	// slugの重複は409。大文字・ドットを含むslugは別物として作れる
	if code, _ := e.createContent(as.other, article("first-post")); code != 409 {
		t.Errorf("slug重複: %d", code)
	}
	for _, slug := range []string{"First-Post", "v1.2.3", "dg_2"} {
		if code, _ := e.createContent(as.author, article(slug)); code != 201 {
			t.Errorf("slug %s: %d", slug, code)
		}
	}
	// 同じsectionとslugでも、区分が違えば別（blogとnews）
	if code, _ := e.createContent(as.author, `{"type":"article","section":"news","slug":"first-post","title":"t","markdown":"x"}`); code != 201 {
		t.Errorf("別区分の同slug: %d", code)
	}
}

// 制限項目（image・sourceId・sourceUrl）はEditor以上だけが書ける。**authorはD14で許可キーから外れた**（名前は版に保存しない）。
func TestContentRestrictedMetadata(t *testing.T) {
	e, as := cmsSetup(t)
	withRestricted := func(slug string) string {
		return fmt.Sprintf(`{"type":"article","section":"blog","slug":%q,"title":"t","markdown":"x","metadata":{"sourceId":"blog/forged"}}`, slug)
	}
	if rec := e.as(as.author, "POST", "/contents", withRestricted("a1")); rec.Code != 403 {
		t.Errorf("Memberが制限項目を指定: %d", rec.Code)
	}
	code, v := e.createContent(as.editor, withRestricted("a2"))
	if code != 201 || v.WorkingRevision.Metadata["sourceId"] != "blog/forged" {
		t.Fatalf("Editorは制限項目を指定できる: %d %+v", code, v)
	}
	// 作者がEditorのコンテンツをMemberが保存する: 制限項目は引き継がれ、違う値は403、同じ値の再送は可
	if rec := e.as(as.editor, "PUT", "/contents/"+v.ID+"/collaborators", fmt.Sprintf(`{"userIds":[%q]}`, e.userID("author.example").String())); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	save := func(meta string) int {
		rec := e.as(as.author, "POST", "/contents/"+v.ID+"/revisions", fmt.Sprintf(`{"baseRevision":%d,"title":"t2","markdown":"x","metadata":%s}`, e.curRev(v.ID), meta))
		return rec.Code
	}
	if c := save(`{"category":"Blog"}`); c != 201 {
		t.Errorf("制限項目を送らない保存: %d", c)
	}
	_, got := e.getContent(as.author, v.ID)
	if got.WorkingRevision.Metadata["sourceId"] != "blog/forged" {
		t.Errorf("制限項目が引き継がれていない: %+v", got.WorkingRevision.Metadata)
	}
	if c := save(`{"sourceId":"blog/other"}`); c != 403 {
		t.Errorf("Memberが違う制限項目を指定: %d", c)
	}
	if c := save(`{"sourceId":null}`); c != 403 {
		t.Errorf("Memberが制限項目を消そうとした: %d", c)
	}
	if c := save(`{"sourceId":"blog/forged","category":"News"}`); c != 201 {
		t.Errorf("同じ値の再送は可: %d", c)
	}
	// Editorは変更・削除できる
	rec := e.as(as.editor, "POST", "/contents/"+v.ID+"/revisions", fmt.Sprintf(`{"baseRevision":%d,"title":"t3","markdown":"x","metadata":{"sourceId":null}}`, e.curRev(v.ID)))
	if rec.Code != 201 {
		t.Fatalf("Editorが制限項目を消す: %d", rec.Code)
	}
	if _, got := e.getContent(as.editor, v.ID); got.WorkingRevision.Metadata["sourceId"] != "" {
		t.Errorf("制限項目が消えていない")
	}
	// D14: 投稿者名（author）は版に保存しない。Editor以上でも、作成・保存のどちらでもauthorキーは受け付けない
	for _, a := range []actor{as.author, as.editor} {
		if rec := e.as(a, "POST", "/contents", `{"type":"article","section":"blog","slug":"no-author","title":"t","markdown":"x","metadata":{"author":"誰か"}}`); rec.Code != 422 {
			t.Errorf("authorを指定した作成: %d", rec.Code)
		}
	}
	if code := save(`{"author":"誰か"}`); code != 422 {
		t.Errorf("authorを指定した保存: %d", code)
	}
}

func (e *env) curRev(id string) int {
	return e.count(`SELECT r.revision_no FROM contents c JOIN content_revisions r ON r.id=c.working_revision_id WHERE c.id=$1`, id)
}

// 403と404の表（docs/design/p3-cms.md §3）。下書きの存在を、関係の無い人に漏らさない。
func TestContentAccessMatrix(t *testing.T) {
	e, as := cmsSetup(t)
	_, v := e.createContent(as.author, article("secret-draft"))
	if rec := e.as(as.author, "PUT", "/contents/"+v.ID+"/collaborators", fmt.Sprintf(`{"userIds":[%q]}`, e.userID("collab.example").String())); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	missing := "00000000-0000-0000-0000-000000000000"
	// 関係の無いメンバー: 存在するIDも存在しないIDも**同じ404・同じ本文**
	bodyOf := func(a actor, method, path, body string) (int, string) {
		rec := e.as(a, method, path, body)
		return rec.Code, rec.Body.String()
	}
	norm := func(s string) string { // requestIdだけ違う
		if i := strings.Index(s, `"requestId"`); i >= 0 {
			return s[:i]
		}
		return s
	}
	for _, c := range []struct{ m, suffix, body string }{
		{"GET", "", ""},
		{"POST", "/revisions", `{"baseRevision":1,"title":"x","markdown":"x"}`},
		{"PUT", "/collaborators", `{"userIds":[]}`},
		{"PUT", "/contributors", `{"baseRevision":1,"contributors":[]}`},
	} {
		c1, b1 := bodyOf(as.other, c.m, "/contents/"+v.ID+c.suffix, c.body)
		c2, b2 := bodyOf(as.other, c.m, "/contents/"+missing+c.suffix, c.body)
		if c1 != 404 || c2 != 404 || norm(b1) != norm(b2) {
			t.Errorf("%s %s: 存在する=%d 存在しない=%d\n%s\n%s", c.m, c.suffix, c1, c2, b1, b2)
		}
	}
	// 匿名は401
	if rec := e.do("GET", "/contents/"+v.ID, ""); rec.Code != 401 {
		t.Errorf("匿名: %d", rec.Code)
	}
	// 作者・共同編集者・Editor・Adminは閲覧できる
	for name, a := range map[string]actor{"作者": as.author, "共同編集者": as.collab, "Editor": as.editor, "Admin": as.admin} {
		if c, got := e.getContent(a, v.ID); c != 200 || got.ID != v.ID {
			t.Errorf("%s: %d", name, c)
		}
	}
	// 共同編集者は共同編集者を指定できない（閲覧できる人には403）。他は404
	if rec := e.as(as.collab, "PUT", "/contents/"+v.ID+"/collaborators", `{"userIds":[]}`); rec.Code != 403 {
		t.Errorf("共同編集者が共同編集者を指定: %d", rec.Code)
	}
	if c, got := e.getContent(as.collab, v.ID); c != 200 || got.CanManageCollaborators || got.CanPublish {
		t.Errorf("共同編集者の権限表示: %d %+v", c, got)
	}
	if c, got := e.getContent(as.editor, v.ID); c != 200 || !got.CanManageCollaborators || !got.CanPublish {
		t.Errorf("Editorの権限表示: %d %+v", c, got)
	}
	// 一覧にも出ない
	rec := e.as(as.other, "GET", "/contents", "")
	if strings.Contains(rec.Body.String(), "secret-draft") || strings.Contains(rec.Body.String(), "最初のタイトル") {
		t.Errorf("無関係なメンバーの一覧に下書きが出た: %s", rec.Body.String())
	}
	// 共同編集者の解除後は、閲覧も保存もできない（行ロック下で関係を読み直す）
	if rec := e.as(as.author, "PUT", "/contents/"+v.ID+"/collaborators", `{"userIds":[]}`); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	if c, _ := e.getContent(as.collab, v.ID); c != 404 {
		t.Errorf("解除後の閲覧: %d", c)
	}
	if code, _ := e.saveRev(as.collab, v.ID, 1, "解除後の保存", "x"); code != 404 {
		t.Errorf("解除後の保存: %d", code)
	}
	if n := e.revCount(v.ID); n != 1 {
		t.Errorf("解除後の保存で版が増えた: %d", n)
	}
}

// 公開中のコンテンツに未公開の書き換えがあっても、編集できない人の一覧には公開版の値だけが出る。
func TestListNeverShowsWorkingRevisionToOthers(t *testing.T) {
	e, as := cmsSetup(t)
	_, v := e.createContent(as.author, article("live-post"))
	e.publishDirect(v.ID, 1)
	if code, rec := e.saveRev(as.author, v.ID, 1, "公開前の秘密の新タイトル", "秘密の本文"); code != 201 {
		t.Fatalf("保存: %d %s", code, rec.Body.String())
	}
	type item struct {
		ID, Title, Summary    string
		RevisionNo            int
		CanEdit               bool
		IsPublished           bool
		HasUnpublishedChanges bool
	}
	list := func(a actor) []item {
		rec := e.as(a, "GET", "/contents", "")
		var out struct{ Items []item }
		if rec.Code != 200 {
			t.Fatalf("一覧: %d", rec.Code)
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if a.csrf != as.author.csrf && a.csrf != as.editor.csrf && a.csrf != as.admin.csrf && strings.Contains(rec.Body.String(), "秘密") {
			t.Errorf("他のメンバーの一覧に作業版の内容が出た: %s", rec.Body.String())
		}
		return out.Items
	}
	o := list(as.other)
	if len(o) != 1 || o[0].Title != "最初のタイトル" || o[0].RevisionNo != 1 || o[0].CanEdit || !o[0].IsPublished || o[0].HasUnpublishedChanges {
		t.Errorf("他のメンバーには公開版の値だけ: %+v", o)
	}
	for name, a := range map[string]actor{"作者": as.author, "Editor": as.editor} {
		l := list(a)
		if len(l) != 1 || l[0].Title != "公開前の秘密の新タイトル" || l[0].RevisionNo != 2 || !l[0].CanEdit || !l[0].HasUnpublishedChanges {
			t.Errorf("%s: %+v", name, l)
		}
	}
	// 未公開の下書きは他のメンバーに出ない。一覧のフィルタ・カーソル
	e.createContent(as.author, `{"type":"project","section":"project","slug":"draft-work","title":"下書きの作品","markdown":"x"}`)
	if l := list(as.other); len(l) != 1 {
		t.Errorf("他のメンバーに下書きが出た: %+v", l)
	}
	if l := list(as.author); len(l) != 2 {
		t.Errorf("作者の一覧: %+v", l)
	}
	rec := e.as(as.author, "GET", "/contents?section=project", "")
	if !strings.Contains(rec.Body.String(), "draft-work") || strings.Contains(rec.Body.String(), "live-post") {
		t.Errorf("sectionの絞り込み: %s", rec.Body.String())
	}
	rec = e.as(as.author, "GET", "/contents?limit=1", "")
	var pg struct {
		Items      []item
		NextCursor *string
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &pg)
	if len(pg.Items) != 1 || pg.NextCursor == nil {
		t.Fatalf("ページング: %s", rec.Body.String())
	}
	rec = e.as(as.author, "GET", "/contents?limit=1&cursor="+*pg.NextCursor, "")
	var pg2 struct{ Items []item }
	_ = json.Unmarshal(rec.Body.Bytes(), &pg2)
	if len(pg2.Items) != 1 || pg2.Items[0].ID == pg.Items[0].ID {
		t.Errorf("2ページ目: %s", rec.Body.String())
	}
	for _, q := range []string{"limit=0", "limit=101", "section=x", "cursor=not-a-cursor"} {
		if rec := e.as(as.author, "GET", "/contents?"+q, ""); rec.Code != 400 {
			t.Errorf("%s: %d", q, rec.Code)
		}
	}
	// mine=true: 共同編集者になった分も含む
	if rec := e.as(as.author, "PUT", "/contents/"+v.ID+"/collaborators", fmt.Sprintf(`{"userIds":[%q]}`, e.userID("collab.example").String())); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	rec = e.as(as.collab, "GET", "/contents?mine=true", "")
	if !strings.Contains(rec.Body.String(), "live-post") || strings.Contains(rec.Body.String(), "draft-work") {
		t.Errorf("mine: %s", rec.Body.String())
	}
}

func TestRevisionSaveAndConflict(t *testing.T) {
	e, as := cmsSetup(t)
	_, v := e.createContent(as.author, article("rev-post"))
	e.publishDirect(v.ID, 1)
	// 保存すると版が増え、作業版だけが進む。公開ポインタは変わらない（公開は再公開まで旧版）
	code, rec := e.saveRev(as.author, v.ID, 1, "二版目", "二版目の本文")
	var v2 cView
	_ = json.Unmarshal(rec.Body.Bytes(), &v2)
	if code != 201 || v2.WorkingRevision.RevisionNo != 2 || v2.WorkingRevision.Title != "二版目" || v2.PublishedRevisionNo == nil || *v2.PublishedRevisionNo != 1 {
		t.Fatalf("保存: %d %s", code, rec.Body.String())
	}
	if n := e.count(`SELECT revision_no FROM content_revisions r JOIN contents c ON c.published_revision_id=r.id WHERE c.id=$1`, v.ID); n != 1 {
		t.Errorf("公開ポインタが動いた: %d", n)
	}
	// 古いbaseからの保存は409で、現在の作業版が返る。版は増えない
	code, rec = e.saveRev(as.collab, v.ID, 1, "遅れた保存", "x")
	if code != 404 { // 共同編集者ではないので404（関係が無い）
		t.Errorf("関係の無い人: %d", code)
	}
	e.as(as.author, "PUT", "/contents/"+v.ID+"/collaborators", fmt.Sprintf(`{"userIds":[%q]}`, e.userID("collab.example").String()))
	code, rec = e.saveRev(as.collab, v.ID, 1, "遅れた保存", "x")
	var cf struct {
		Error   struct{ Code string }
		Current cRev
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &cf)
	if code != 409 || cf.Error.Code != "REVISION_CONFLICT" || cf.Current.RevisionNo != 2 || cf.Current.Title != "二版目" {
		t.Errorf("競合: %d %s", code, rec.Body.String())
	}
	if n := e.revCount(v.ID); n != 2 {
		t.Errorf("競合で版が増えた: %d", n)
	}
	// 古いbaseで内容が同じでも409（baseの比較が先）
	if code, _ := e.saveRev(as.collab, v.ID, 1, "二版目", "二版目の本文"); code != 409 {
		t.Errorf("古いbaseで同じ内容: %d", code)
	}
	// 同じ内容の保存は版を増やさず200
	if code, _ := e.saveRev(as.author, v.ID, 2, "二版目", "二版目の本文"); code != 200 {
		t.Errorf("同じ内容: %d", code)
	}
	if n := e.revCount(v.ID); n != 2 {
		t.Errorf("同じ内容で版が増えた: %d", n)
	}
	// 入力の検査
	for name, body := range map[string]string{
		"baseRevisionなし": `{"title":"x","markdown":"x"}`,
		"baseRevisionが0": `{"baseRevision":0,"title":"x","markdown":"x"}`,
		"scriptを含む":      `{"baseRevision":2,"title":"x","markdown":"<script>1</script>"}`,
		"本文が大きすぎる":       fmt.Sprintf(`{"baseRevision":2,"title":"x","markdown":%q}`, strings.Repeat("a", 256*1024+1)),
	} {
		if rec := e.as(as.author, "POST", "/contents/"+v.ID+"/revisions", body); rec.Code != 422 && rec.Code != 400 && rec.Code != 413 {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	// 過去の版の閲覧と履歴
	rec = e.as(as.author, "GET", "/contents/"+v.ID+"?revision=1", "")
	var old cView
	_ = json.Unmarshal(rec.Body.Bytes(), &old)
	if rec.Code != 200 || old.WorkingRevision.RevisionNo != 1 || old.WorkingRevision.Title != "最初のタイトル" || len(old.Revisions) != 2 {
		t.Errorf("過去の版: %d %s", rec.Code, rec.Body.String())
	}
	if rec := e.as(as.author, "GET", "/contents/"+v.ID+"?revision=99", ""); rec.Code != 404 {
		t.Errorf("存在しない版: %d", rec.Code)
	}
	if rec := e.as(as.other, "GET", "/contents/"+v.ID+"?revision=1", ""); rec.Code != 404 {
		t.Errorf("関係の無い人の過去の版: %d", rec.Code)
	}
}

// 同じbaseから同時に保存すると、成功は1件だけ（残りは409）。版番号が飛ばない。
func TestConcurrentSavesFromSameBase(t *testing.T) {
	e, as := cmsSetup(t)
	const n = 8
	e.cmsHook = barrierHook(n) // 確認を終えた時点で全員を揃える。行ロックが無ければ全員が同じ版を見て、必ず競合する
	e.h = e.build()
	_, v := e.createContent(as.author, article("race-post"))
	codes := make([]int, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			codes[i], _ = e.saveRev(as.author, v.ID, 1, fmt.Sprintf("並行%d", i), "本文")
		}(i)
	}
	close(start)
	wg.Wait()
	ok, conflict := 0, 0
	for _, c := range codes {
		switch c {
		case 201:
			ok++
		case 409:
			conflict++
		case 429:
		default:
			t.Errorf("想定外: %d", c)
		}
	}
	if ok != 1 || conflict != n-1 {
		t.Errorf("成功%d・競合%d（1と%dのはず）: %v", ok, conflict, n-1, codes)
	}
	if rev := e.revCount(v.ID); rev != 2 || e.curRev(v.ID) != 2 {
		t.Errorf("版の数 %d 作業版 %d", rev, e.curRev(v.ID))
	}
}

func TestCollaborators(t *testing.T) {
	e, as := cmsSetup(t)
	e.addUser("disabled.example", "disabled", goodPass)
	e.addUser("invited.example", "invited", "")
	_, v := e.createContent(as.author, article("collab-post"))
	put := func(a actor, ids ...string) int {
		q := make([]string, len(ids))
		for i, id := range ids {
			q[i] = fmt.Sprintf("%q", id)
		}
		return e.as(a, "PUT", "/contents/"+v.ID+"/collaborators", `{"userIds":[`+strings.Join(q, ",")+`]}`).Code
	}
	collab, other := e.userID("collab.example").String(), e.userID("other.example").String()
	if c := put(as.author, e.userID("disabled.example").String()); c != 422 {
		t.Errorf("停止ユーザーを追加: %d", c)
	}
	if c := put(as.author, e.userID("invited.example").String()); c != 422 {
		t.Errorf("招待中のユーザーを追加: %d", c)
	}
	if c := put(as.author, "not-a-uuid"); c != 422 {
		t.Errorf("不正なID: %d", c)
	}
	if c := put(as.author, "00000000-0000-0000-0000-000000000009"); c != 422 {
		t.Errorf("存在しないユーザー: %d", c)
	}
	// 作者自身・重複は無視される
	rec := e.as(as.author, "PUT", "/contents/"+v.ID+"/collaborators", fmt.Sprintf(`{"userIds":[%q,%q,%q,%q]}`, e.userID("author.example").String(), collab, collab, other))
	var out struct {
		Collaborators []struct{ UserID, DisplayName string }
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != 200 || len(out.Collaborators) != 2 {
		t.Fatalf("置き換え: %d %s", rec.Code, rec.Body.String())
	}
	// 共同編集者は編集できる。Editorは共同編集者を変えられる
	if code, _ := e.saveRev(as.collab, v.ID, 1, "共同編集", "x"); code != 201 {
		t.Errorf("共同編集者の保存: %d", code)
	}
	if c := put(as.editor, collab); c != 200 {
		t.Errorf("Editorによる変更: %d", c)
	}
	if c, _ := e.getContent(as.other, v.ID); c != 404 {
		t.Errorf("外された人の閲覧: %d", c)
	}
	// 停止した人は残せるが、停止中の人を新たに加えることはできない（既に居る人は維持される）
	if _, err := e.pool.Exec(context.Background(), `UPDATE users SET status='disabled', disabled_at=now() WHERE id=$1`, e.userID("collab.example")); err != nil {
		t.Fatal(err)
	}
	if c := put(as.author, collab); c != 200 {
		t.Errorf("既存の共同編集者は維持できる: %d", c)
	}
	// 上限20人
	ids := []string{}
	for i := 0; i < 21; i++ {
		id := fmt.Sprintf("u%02d.example", i)
		e.addUser(id, "active", goodPass)
		ids = append(ids, e.userID(id).String())
	}
	if c := put(as.author, ids...); c != 422 {
		t.Errorf("21人: %d", c)
	}
	if c := put(as.author, ids[:20]...); c != 200 {
		t.Errorf("20人: %d", c)
	}
	if n := e.count(`SELECT count(*)::int FROM audit_logs WHERE action='content.collaborators_updated'`); n < 3 {
		t.Errorf("監査ログ: %d", n)
	}
}

func TestContributors(t *testing.T) {
	e, as := cmsSetup(t)
	e.addUser("gone.example", "disabled", goodPass)
	_, w := e.createContent(as.author, project("my-work"))
	_, art := e.createContent(as.author, article("an-article"))
	put := func(a actor, id string, base int, body string) (int, cView) {
		rec := e.as(a, "PUT", "/contents/"+id+"/contributors", fmt.Sprintf(`{"baseRevision":%d,"contributors":%s}`, base, body))
		var v cView
		_ = json.Unmarshal(rec.Body.Bytes(), &v)
		return rec.Code, v
	}
	u := func(login string) string { return e.userID(login).String() }
	if c, _ := put(as.author, art.ID, 1, `[]`); c != 422 {
		t.Errorf("記事に制作メンバー: %d", c)
	}
	list := fmt.Sprintf(`[{"userId":%q,"roleLabel":"企画・プログラム"},{"userId":%q,"roleLabel":"3Dモデル"},{"userId":%q,"roleLabel":"3Dモデル"}]`, u("collab.example"), u("other.example"), u("gone.example"))
	code, v := put(as.author, w.ID, 1, list)
	if code != 201 || v.WorkingRevision.RevisionNo != 2 || len(v.WorkingRevision.Contributors) != 3 || v.WorkingRevision.Contributors[0].DisplayName != "共同B" || v.WorkingRevision.Contributors[0].RoleLabel != "企画・プログラム" {
		t.Fatalf("登録（停止ユーザーも可）: %d %+v", code, v)
	}
	// 変更なしなら新しい版を作らない。順序は保持される
	if code, _ := put(as.author, w.ID, 2, list); code != 200 || e.revCount(w.ID) != 2 {
		t.Errorf("変更なし: %d 版%d", code, e.revCount(w.ID))
	}
	// 普通の保存でも制作メンバーは引き継がれる（版ごとに持つ）
	code, rec := e.saveRev(as.author, w.ID, 2, "作品の新題", "新しい本文")
	var v3 cView
	_ = json.Unmarshal(rec.Body.Bytes(), &v3)
	if code != 201 || len(v3.WorkingRevision.Contributors) != 3 {
		t.Errorf("引き継ぎ: %d %s", code, rec.Body.String())
	}
	// 公開版の制作メンバーは、後の変更で動かない
	e.publishDirect(w.ID, 3)
	if code, _ := put(as.author, w.ID, 3, fmt.Sprintf(`[{"userId":%q,"roleLabel":"監督"}]`, u("other.example"))); code != 201 {
		t.Fatalf("公開後の変更: %d", code)
	}
	if n := e.count(`SELECT count(*)::int FROM work_contributors wc JOIN contents c ON c.published_revision_id=wc.revision_id WHERE c.id=$1`, w.ID); n != 3 {
		t.Errorf("公開版の制作メンバーが変わった: %d", n)
	}
	if n := e.count(`SELECT count(*)::int FROM work_contributors wc JOIN contents c ON c.working_revision_id=wc.revision_id WHERE c.id=$1`, w.ID); n != 1 {
		t.Errorf("作業版の制作メンバー: %d", n)
	}
	// 入力の検査・競合・権限
	cur := e.curRev(w.ID)
	for name, body := range map[string]string{
		"同じ人の同じ役割":  fmt.Sprintf(`[{"userId":%q,"roleLabel":"x"},{"userId":%q,"roleLabel":"x"}]`, u("collab.example"), u("collab.example")),
		"空の役割":      fmt.Sprintf(`[{"userId":%q,"roleLabel":" "}]`, u("collab.example")),
		"役割が51文字":   fmt.Sprintf(`[{"userId":%q,"roleLabel":%q}]`, u("collab.example"), strings.Repeat("あ", 51)),
		"役割にURL":    fmt.Sprintf(`[{"userId":%q,"roleLabel":"https://evil.example"}]`, u("collab.example")),
		"役割にwww":    fmt.Sprintf(`[{"userId":%q,"roleLabel":"www.evil.example"}]`, u("collab.example")),
		"役割に山括弧":    fmt.Sprintf(`[{"userId":%q,"roleLabel":"<b>x"}]`, u("collab.example")),
		"役割に@":      fmt.Sprintf(`[{"userId":%q,"roleLabel":"@everyone"}]`, u("collab.example")),
		"存在しないユーザー": `[{"userId":"00000000-0000-0000-0000-000000000009","roleLabel":"x"}]`,
		"不正なユーザーID": `[{"userId":"x","roleLabel":"x"}]`,
	} {
		if c, _ := put(as.author, w.ID, cur, body); c != 422 {
			t.Errorf("%s: %d", name, c)
		}
	}
	if c, _ := put(as.author, w.ID, cur-1, `[]`); c != 409 {
		t.Errorf("古いbase: %d", c)
	}
	if c, _ := put(as.other, w.ID, cur, `[]`); c != 404 {
		t.Errorf("関係の無い人: %d", c)
	}
	if rec := e.as(as.editor, "PUT", "/contents/"+w.ID+"/contributors", fmt.Sprintf(`{"baseRevision":%d,"contributors":[]}`, cur)); rec.Code != 201 {
		t.Errorf("Editor: %d", rec.Code)
	}
	// 公開クレジットの同意は、この操作では変わらない
	if n := e.count(`SELECT count(*)::int FROM public_credit_consents`); n != 0 {
		t.Errorf("同意が作られた: %d", n)
	}
}

// DBの制約: 複合FK・追記専用・作品だけの制作メンバー・日付・区分。
func TestContentDBConstraints(t *testing.T) {
	e, as := cmsSetup(t)
	_, a := e.createContent(as.author, article("db-a"))
	_, b := e.createContent(as.author, article("db-b"))
	_, w := e.createContent(as.author, project("db-w"))
	ctx := context.Background()
	exec := func(sql string, args ...any) error { _, err := e.pool.Exec(ctx, sql, args...); return err }
	// 別のコンテンツの版を公開・作業ポインタにできない（複合FK）
	if err := exec(`UPDATE contents SET published_revision_id=(SELECT working_revision_id FROM contents WHERE id=$2) WHERE id=$1`, a.ID, b.ID); err == nil {
		t.Error("別コンテンツの版を公開ポインタにできた")
	}
	if err := exec(`UPDATE contents SET working_revision_id=(SELECT working_revision_id FROM contents WHERE id=$2) WHERE id=$1`, a.ID, b.ID); err == nil {
		t.Error("別コンテンツの版を作業ポインタにできた")
	}
	// 版は更新・削除できない
	if err := exec(`UPDATE content_revisions SET title='改ざん' WHERE content_id=$1`, a.ID); err == nil {
		t.Error("版を更新できた")
	}
	if err := exec(`DELETE FROM content_revisions WHERE content_id=$1`, a.ID); err == nil {
		t.Error("版を削除できた")
	}
	// 記事に制作メンバーは登録できない（トリガー）
	if err := exec(`INSERT INTO work_contributors (content_id, revision_id, user_id, role_label, sort_order, added_by)
		SELECT $1, working_revision_id, $2, 'x', 0, $2 FROM contents WHERE id=$1`, a.ID, e.userID("author.example")); err == nil {
		t.Error("記事に制作メンバーを登録できた")
	}
	// 制作メンバーは版に紐づく: 別のコンテンツの版は指せない・更新削除できない
	if err := exec(`INSERT INTO work_contributors (content_id, revision_id, user_id, role_label, sort_order, added_by)
		SELECT $1, working_revision_id, $3, 'x', 0, $3 FROM contents WHERE id=$2`, w.ID, a.ID, e.userID("author.example")); err == nil {
		t.Error("別コンテンツの版に制作メンバーを登録できた")
	}
	if err := exec(`INSERT INTO work_contributors (content_id, revision_id, user_id, role_label, sort_order, added_by)
		SELECT $1, working_revision_id, $2, 'x', 0, $2 FROM contents WHERE id=$1`, w.ID, e.userID("author.example")); err != nil {
		t.Fatalf("作品への登録: %v", err)
	}
	if err := exec(`UPDATE work_contributors SET role_label='y'`); err == nil {
		t.Error("制作メンバーを更新できた")
	}
	if err := exec(`DELETE FROM work_contributors`); err == nil {
		t.Error("制作メンバーを削除できた")
	}
	// 公開履歴は追記専用で、別コンテンツの版は指せない
	if err := exec(`INSERT INTO content_publication_events (content_id, revision_id, actor_user_id, event_type)
		SELECT $1, working_revision_id, $3, 'publish' FROM contents WHERE id=$2`, a.ID, b.ID, e.userID("editor.example")); err == nil {
		t.Error("別コンテンツの版で公開履歴を作れた")
	}
	if err := exec(`INSERT INTO content_publication_events (content_id, revision_id, actor_user_id, event_type)
		SELECT $1, working_revision_id, $2, 'publish' FROM contents WHERE id=$1`, a.ID, e.userID("editor.example")); err != nil {
		t.Fatal(err)
	}
	if err := exec(`UPDATE content_publication_events SET event_type='unpublish'`); err == nil {
		t.Error("公開履歴を更新できた")
	}
	if err := exec(`DELETE FROM content_publication_events`); err == nil {
		t.Error("公開履歴を削除できた")
	}
	// 区分・slug・日付・サイズのCHECK
	for name, sql := range map[string]string{
		"type/sectionの不整合": `INSERT INTO contents (type, section, slug, creator_user_id, working_revision_id) VALUES ('article','project','x',(SELECT id FROM users LIMIT 1),gen_random_uuid())`,
		"slugの形式":          `INSERT INTO contents (type, section, slug, creator_user_id, working_revision_id) VALUES ('article','blog','.x',(SELECT id FROM users LIMIT 1),gen_random_uuid())`,
	} {
		if err := exec(sql); err == nil {
			t.Errorf("%s: 通った", name)
		}
	}
	for name, vals := range map[string]string{
		"dayで日付なし":        `'day', NULL, false`,
		"dayで補った日":        `'day', '2026-01-01', true`,
		"monthで補った日でない":   `'month', '2026-01-01', false`,
		"monthで日付なし":      `'month', NULL, false`,
		"unknownで補っていない日": `'unknown', '2026-01-01', false`,
		"未知の種類":           `'week', '2026-01-01', true`,
	} {
		err := exec(fmt.Sprintf(`INSERT INTO content_revisions (content_id, revision_no, title, markdown, edited_by, date_kind, sort_date, sort_date_is_estimated)
			SELECT id, 100, 't', 'm', creator_user_id, %s FROM contents WHERE id=$1`, vals), a.ID)
		if err == nil {
			t.Errorf("日付の制約 %s: 通った", name)
		}
	}
	for name, vals := range map[string]string{
		"タイトルが空":   `'  ', 'm'`,
		"タイトルに改行":  `E'a\nb', 'm'`,
		"本文が大きすぎる": `'t', repeat('a', 262145)`,
	} {
		if err := exec(fmt.Sprintf(`INSERT INTO content_revisions (content_id, revision_no, title, markdown, edited_by)
			SELECT id, 101, %s, creator_user_id FROM contents WHERE id=$1`, vals), a.ID); err == nil {
			t.Errorf("版の制約 %s: 通った", name)
		}
	}
}

func TestContentDates(t *testing.T) {
	e, as := cmsSetup(t)
	mk := func(slug, date string) (int, cView) {
		return e.createContent(as.author, fmt.Sprintf(`{"type":"article","section":"news","slug":%q,"title":"t","markdown":"x","date":%s}`, slug, date))
	}
	// 現行サイトの部分日付（D12）: 原文を保ち、補った並び替え用の日を開催日として扱わない
	for slug, c := range map[string]struct {
		date      string
		kind      string
		label     string
		estimated bool
	}{
		"d-day":     {`{"label":"2026年7月22日","kind":"day","sortDate":"2026-07-22"}`, "day", "2026年7月22日", false},
		"d-month":   {`{"label":"2025年12月","kind":"month","sortDate":"2025-12-01"}`, "month", "2025年12月", true},
		"d-range":   {`{"label":"2025年3月〜 6月","kind":"range","sortDate":"2025-03-01"}`, "range", "2025年3月〜 6月", true},
		"d-unknown": {`{"label":"","kind":"unknown"}`, "unknown", "", false},
		"d-unk2":    {`{"label":"時期不明","kind":"unknown","sortDate":"2025-01-01"}`, "unknown", "時期不明", true},
	} {
		code, v := mk(slug, c.date)
		d := v.WorkingRevision.Date
		if code != 201 || d.Kind != c.kind || d.Label != c.label || d.SortDateIsEstimated != c.estimated {
			t.Errorf("%s: %d %+v", slug, code, d)
		}
	}
	// 日付を送らない保存は前の日付を引き継ぐ
	_, v := mk("d-keep", `{"label":"2025年12月","kind":"month","sortDate":"2025-12-01"}`)
	if code, rec := e.saveRev(as.author, v.ID, 1, "題名を変える", "x"); code != 201 {
		t.Fatalf("%d %s", code, rec.Body.String())
	}
	if _, got := e.getContent(as.author, v.ID); got.WorkingRevision.Date.Label != "2025年12月" || got.WorkingRevision.Date.Kind != "month" || !got.WorkingRevision.Date.SortDateIsEstimated {
		t.Errorf("日付が引き継がれていない: %+v", got.WorkingRevision.Date)
	}
}

func TestSaveRateLimit(t *testing.T) {
	e, as := cmsSetup(t)
	bad := `{"type":"article","section":"blog","slug":"","title":"t","markdown":"x"}`
	var last int
	for i := 0; i < 61; i++ {
		last = e.as(as.author, "POST", "/contents", bad).Code
		if i < 60 && last != 422 {
			t.Fatalf("%d回目: %d", i+1, last)
		}
	}
	if last != 429 {
		t.Errorf("61回目: %d（429のはず）", last)
	}
	// 他の利用者は影響を受けない
	if c := e.as(as.other, "POST", "/contents", bad).Code; c != 422 {
		t.Errorf("他の利用者: %d", c)
	}
}
