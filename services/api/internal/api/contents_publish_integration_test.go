//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func (e *env) publish(a actor, id string, revNo int) (int, *httptest.ResponseRecorder) {
	e.t.Helper()
	rec := e.as(a, "POST", "/contents/"+id+"/publish", fmt.Sprintf(`{"revisionNo":%d}`, revNo))
	return rec.Code, rec
}

func (e *env) events(id, typ string) int {
	return e.count(`SELECT count(*)::int FROM content_publication_events WHERE content_id=$1 AND event_type=$2`, id, typ)
}

type pubItem struct {
	Type, Section, Slug, Title, Summary string
	Metadata                            map[string]string
	Date                                struct {
		Label               string
		Kind                string
		SortDate            *string
		SortDateIsEstimated bool
	}
}

type pubDetail struct {
	pubItem
	Markdown     string
	Contributors []struct{ DisplayName, RoleLabel string }
}

// 匿名で公開APIを呼ぶ（Cookieなし）。
func (e *env) pub(path string, o ...reqOpt) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.do("GET", "/public/contents"+path, "", o...)
}

func (e *env) pubDetail(section, slug string) (int, pubDetail, string) {
	rec := e.pub("/" + section + "/" + slug)
	var d pubDetail
	_ = json.Unmarshal(rec.Body.Bytes(), &d)
	return rec.Code, d, rec.Body.String()
}

func TestPublishAuthorization(t *testing.T) {
	e, as := cmsSetup(t)
	_, v := e.createContent(as.author, article("pub-authz"))
	e.as(as.author, "PUT", "/contents/"+v.ID+"/collaborators", fmt.Sprintf(`{"userIds":[%q]}`, e.userID("collab.example").String()))
	missing := "00000000-0000-0000-0000-000000000000"
	if rec := e.do("POST", "/contents/"+v.ID+"/publish", `{"revisionNo":1}`); rec.Code != 401 {
		t.Errorf("匿名: %d", rec.Code)
	}
	// 作者・共同編集者・無関係なメンバーは、存在するIDも存在しないIDも**同じ403**（公開はEditor以上のみ）
	for name, a := range map[string]actor{"作者": as.author, "共同編集者": as.collab, "他のメンバー": as.other} {
		for _, path := range []string{"/contents/" + v.ID + "/publish", "/contents/" + missing + "/publish", "/contents/" + v.ID + "/unpublish", "/contents/" + missing + "/unpublish"} {
			rec := e.as(a, "POST", path, `{"revisionNo":1}`)
			if rec.Code != 403 {
				t.Errorf("%s %s: %d", name, path, rec.Code)
			}
		}
	}
	if n := e.events(v.ID, "publish"); n != 0 {
		t.Errorf("拒否されたのに公開イベント: %d", n)
	}
	// revisionNoは必須
	for name, body := range map[string]string{"なし": `{}`, "0": `{"revisionNo":0}`, "未知のフィールド": `{"revisionNo":1,"x":1}`} {
		if rec := e.as(as.editor, "POST", "/contents/"+v.ID+"/publish", body); rec.Code != 422 && rec.Code != 400 {
			t.Errorf("revisionNo %s: %d", name, rec.Code)
		}
	}
	if rec := e.as(as.editor, "POST", "/contents/"+missing+"/publish", `{"revisionNo":1}`); rec.Code != 404 {
		t.Errorf("Editorが存在しないIDを公開: %d", rec.Code)
	}
	for name, a := range map[string]actor{"Editor": as.editor, "Admin": as.admin} {
		_, v2 := e.createContent(as.author, article("pub-by-"+strings.ToLower(name)))
		if code, rec := e.publish(a, v2.ID, 1); code != 200 {
			t.Errorf("%s: %d %s", name, code, rec.Body.String())
		}
	}
	// 公開した版の表示
	code, rec := e.publish(as.editor, v.ID, 1)
	var got cView
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if code != 200 || got.PublishedRevisionNo == nil || *got.PublishedRevisionNo != 1 || e.events(v.ID, "publish") != 1 {
		t.Fatalf("公開: %d %s", code, rec.Body.String())
	}
	// 同じ版の再公開は何もしない（イベント・通知を重ねない）
	before := e.count(`SELECT count(*)::int FROM notification_outbox WHERE event_type='content.published'`)
	if code, _ := e.publish(as.editor, v.ID, 1); code != 200 || e.events(v.ID, "publish") != 1 || e.count(`SELECT count(*)::int FROM notification_outbox WHERE event_type='content.published'`) != before {
		t.Errorf("同じ版の再公開で増えた")
	}
	// 公開した版の通知: タイトルと種別だけ
	var payload string
	_ = e.pool.QueryRow(context.Background(), `SELECT payload_json::text FROM notification_outbox WHERE event_type='content.published' ORDER BY created_at LIMIT 1`).Scan(&payload)
	if !strings.Contains(payload, "最初のタイトル") || strings.Contains(payload, "author.example") || strings.Contains(payload, "本文") {
		t.Errorf("通知の本文: %s", payload)
	}
	if n := e.count(`SELECT count(*)::int FROM audit_logs WHERE action='content.published'`); n < 3 {
		t.Errorf("監査ログ: %d", n)
	}
}

// publishは、Editorが確認した版（revisionNo）だけを公開する。確認後に作者が保存した版は公開しない。
func TestPublishOnlyTheReviewedRevision(t *testing.T) {
	e, as := cmsSetup(t)
	_, v := e.createContent(as.author, article("reviewed"))
	e.saveRev(as.author, v.ID, 1, "二版目（Editorは未確認）", "未確認の本文")
	// Editorは版1を確認して公開しようとするが、作業版は2になっている → 409。何も公開されない
	code, rec := e.publish(as.editor, v.ID, 1)
	var cf struct {
		Error   struct{ Code string }
		Current cRev
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &cf)
	if code != 409 || cf.Error.Code != "REVISION_CONFLICT" || cf.Current.RevisionNo != 2 {
		t.Fatalf("確認していない版: %d %s", code, rec.Body.String())
	}
	if c, _, _ := e.pubDetail("blog", "reviewed"); c != 404 {
		t.Errorf("公開されてしまった: %d", c)
	}
	if n := e.count(`SELECT count(*)::int FROM contents WHERE id=$1 AND published_revision_id IS NOT NULL`, v.ID); n != 0 {
		t.Errorf("公開ポインタが動いた")
	}
	if code, _ := e.publish(as.editor, v.ID, 2); code != 200 {
		t.Errorf("版2の公開: %d", code)
	}
}

// 公開と保存が同時に起きても、公開されるのは指定した版だけ（確認していない版が公開されない）。
func TestPublishVersusSaveRace(t *testing.T) {
	e, as := cmsSetup(t)
	for i := 0; i < 15; i++ {
		_, v := e.createContent(as.author, article(fmt.Sprintf("race-pub-%02d", i)))
		var wg sync.WaitGroup
		var pc, sc int
		start := make(chan struct{})
		wg.Add(2)
		go func() { defer wg.Done(); <-start; pc, _ = e.publish(as.editor, v.ID, 1) }()
		go func() {
			defer wg.Done()
			<-start
			sc, _ = e.saveRev(as.author, v.ID, 1, "同時の保存", "未確認")
		}()
		close(start)
		wg.Wait()
		if sc != 201 && sc != 429 {
			t.Fatalf("保存: %d", sc)
		}
		if pc != 200 && pc != 409 {
			t.Fatalf("公開: %d", pc)
		}
		if n := e.count(`SELECT COALESCE((SELECT r.revision_no FROM contents c JOIN content_revisions r ON r.id=c.published_revision_id WHERE c.id=$1),0)`, v.ID); n != 0 && n != 1 {
			t.Fatalf("確認していない版%dが公開された（公開=%d 保存=%d）", n, pc, sc)
		}
		if pc == 200 && e.count(`SELECT count(*)::int FROM contents WHERE id=$1 AND published_revision_id IS NOT NULL`, v.ID) != 1 {
			t.Fatalf("公開が成功したのにポインタが無い")
		}
	}
}

func TestPublishRechecksMarkdownAndUnpublish(t *testing.T) {
	e, as := cmsSetup(t)
	_, v := e.createContent(as.author, article("recheck"))
	// 検査をすり抜けた版が（DB直接の混入等で）あっても、公開の直前に再検査して拒否する
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO content_revisions (content_id, revision_no, title, markdown, edited_by) SELECT id, 2, 't', '<script>alert(1)</script>', creator_user_id FROM contents WHERE id=$1`, v.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(context.Background(), `UPDATE contents SET working_revision_id=(SELECT id FROM content_revisions WHERE content_id=$1 AND revision_no=2) WHERE id=$1`, v.ID); err != nil {
		t.Fatal(err)
	}
	if code, rec := e.publish(as.editor, v.ID, 2); code != 422 || strings.Contains(rec.Body.String(), "alert(") {
		t.Errorf("検査を通っていない版の公開: %d %s", code, rec.Body.String())
	}
	if c, _, _ := e.pubDetail("blog", "recheck"); c != 404 {
		t.Errorf("公開された: %d", c)
	}
	// 公開停止
	_, u := e.createContent(as.author, article("to-unpublish"))
	e.publish(as.editor, u.ID, 1)
	if c, _, _ := e.pubDetail("blog", "to-unpublish"); c != 200 {
		t.Fatalf("公開後: %d", c)
	}
	for name, a := range map[string]actor{"作者": as.author, "共同編集者候補": as.other} {
		if rec := e.as(a, "POST", "/contents/"+u.ID+"/unpublish", ""); rec.Code != 403 {
			t.Errorf("%sが公開停止: %d", name, rec.Code)
		}
	}
	if rec := e.as(as.editor, "POST", "/contents/"+u.ID+"/unpublish", ""); rec.Code != 200 {
		t.Fatalf("公開停止: %d", rec.Code)
	}
	if c, _, _ := e.pubDetail("blog", "to-unpublish"); c != 404 {
		t.Errorf("公開停止後も見える: %d", c)
	}
	if e.events(u.ID, "unpublish") != 1 {
		t.Errorf("公開停止のイベント")
	}
	// 公開中でなければ何もしない。作業版・履歴は残り、再公開できる
	if rec := e.as(as.editor, "POST", "/contents/"+u.ID+"/unpublish", ""); rec.Code != 200 || e.events(u.ID, "unpublish") != 1 {
		t.Errorf("二重の公開停止: %d", rec.Code)
	}
	if code, _ := e.publish(as.editor, u.ID, 1); code != 200 || e.events(u.ID, "publish") != 2 {
		t.Errorf("再公開: %d", code)
	}
}

// 公開APIは公開版だけを返し、内部の情報を出さない。公開後に編集しても再公開まで旧版が表示される（受け入れ3）。
func TestPublicAPIServesOnlyThePublishedRevision(t *testing.T) {
	e, as := cmsSetup(t)
	// 公開前は一覧にも詳細にも出ない。存在しないものと同じ404
	_, v := e.createContent(as.editor, `{"type":"article","section":"blog","slug":"My.Post_v1","title":"公開する記事","summary":"要約","markdown":"## 本文\n\n![x](/images/a.webp)","metadata":{"category":"Blog","author":"yurarara","sourceId":"blog/x","sourceUrl":"https://example.com/src"},"date":{"label":"2025年12月","kind":"month","sortDate":"2025-12-01"}}`)
	_, draft := e.createContent(as.author, article("only-draft"))
	if rec := e.pub(""); rec.Code != 200 || strings.Contains(rec.Body.String(), "公開する記事") {
		t.Errorf("公開前の一覧: %d %s", rec.Code, rec.Body.String())
	}
	c1, _, b1 := e.pubDetail("blog", "My.Post_v1")
	c2, _, b2 := e.pubDetail("blog", "does-not-exist")
	c3, _, b3 := e.pubDetail("blog", "only-draft")
	norm := func(s string) string { return s[:strings.Index(s, `"requestId"`)] }
	if c1 != 404 || c2 != 404 || c3 != 404 || norm(b1) != norm(b2) || norm(b1) != norm(b3) {
		t.Errorf("未公開と存在しないものが区別できる: %d %d %d", c1, c2, c3)
	}
	if code, _ := e.publish(as.editor, v.ID, 1); code != 200 {
		t.Fatal(code)
	}
	code, d, body := e.pubDetail("blog", "My.Post_v1") // 大文字・ドットを含むslugも、そのまま
	if code != 200 || d.Title != "公開する記事" || d.Slug != "My.Post_v1" || d.Metadata["category"] != "Blog" || d.Metadata["author"] != "yurarara" || d.Date.Label != "2025年12月" || d.Date.Kind != "month" || !d.Date.SortDateIsEstimated {
		t.Fatalf("公開版: %d %s", code, body)
	}
	// 内部の情報を出さない
	for _, leak := range []string{v.ID, draft.ID, "sourceId", "sourceUrl", "example.com/src", "revisionNo", "creator", "collaborators", "editedBy", "workingRevision", "editor.example", "userId"} {
		if strings.Contains(body, leak) {
			t.Errorf("公開APIに内部情報 %q が出ている: %s", leak, body)
		}
	}
	// 公開後に編集しても、再公開まで旧版が表示される
	if code, _ := e.saveRev(as.editor, v.ID, 1, "編集後の新タイトル（未公開）", "編集後の本文（未公開）"); code != 201 {
		t.Fatal(code)
	}
	_, d, body = e.pubDetail("blog", "My.Post_v1")
	if d.Title != "公開する記事" || strings.Contains(body, "未公開") {
		t.Errorf("再公開前に新しい版が見える: %s", body)
	}
	if rec := e.pub(""); strings.Contains(rec.Body.String(), "未公開") {
		t.Errorf("一覧に未公開の版が出た: %s", rec.Body.String())
	}
	if code, _ := e.publish(as.editor, v.ID, 2); code != 200 {
		t.Fatal(code)
	}
	if _, d, _ = e.pubDetail("blog", "My.Post_v1"); d.Title != "編集後の新タイトル（未公開）" {
		t.Errorf("再公開後: %s", d.Title)
	}
	// 認証情報で応答が変わらない・Cookieを発行しない
	anon := e.pub("/blog/My.Post_v1")
	withSession := e.pub("/blog/My.Post_v1", withCookie(as.editor.cookie))
	if anon.Body.String() != withSession.Body.String() || anon.Header().Get("ETag") != withSession.Header().Get("ETag") || len(withSession.Result().Cookies()) != 0 {
		t.Errorf("認証情報で応答が変わった／Cookieを発行した")
	}
	// ETagと304、Cache-Control
	etag := anon.Header().Get("ETag")
	if etag == "" || !strings.HasPrefix(etag, `"`) || strings.HasPrefix(etag, "W/") || anon.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("ETag/Cache-Control: %q %q", etag, anon.Header().Get("Cache-Control"))
	}
	if rec := e.pub("/blog/My.Post_v1", withHeader("If-None-Match", etag)); rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Errorf("304: %d", rec.Code)
	}
	if rec := e.pub("/blog/My.Post_v1", withHeader("If-None-Match", `"other"`)); rec.Code != 200 {
		t.Errorf("別のETag: %d", rec.Code)
	}
	// 入力の検査
	for _, p := range []string{"/other/x", "/blog/a%2Fb", "/blog/%E8%A8%98%E4%BA%8B"} {
		if rec := e.pub(p); rec.Code != 404 {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
	if rec := e.pub("/blog/.."); rec.Code == 200 || strings.Contains(rec.Body.String(), "公開する記事") { // 正規化されてリダイレクト(307)または404。内容は返らない
		t.Errorf("/blog/..: %d", rec.Code)
	}
	for _, q := range []string{"?limit=0", "?limit=101", "?section=x", "?cursor=zzz"} {
		if rec := e.pub(q); rec.Code != 400 {
			t.Errorf("%s: %d", q, rec.Code)
		}
	}
}

func TestPublicListOrderAndPaging(t *testing.T) {
	e, as := cmsSetup(t)
	mk := func(slug, section, typ, date string) {
		body := fmt.Sprintf(`{"type":%q,"section":%q,"slug":%q,"title":%q,"markdown":"x","date":%s}`, typ, section, slug, "題-"+slug, date)
		code, v := e.createContent(as.editor, body)
		if code != 201 {
			t.Fatalf("%s: %d", slug, code)
		}
		if c, _ := e.publish(as.editor, v.ID, 1); c != 200 {
			t.Fatal(c)
		}
	}
	mk("old", "blog", "article", `{"label":"2025年3月","kind":"month","sortDate":"2025-03-01"}`)
	mk("new", "blog", "article", `{"label":"2026年7月22日","kind":"day","sortDate":"2026-07-22"}`)
	mk("nodate", "news", "article", `{"label":"","kind":"unknown"}`)
	mk("work", "project", "project", `{"label":"2026年8月1〜2日","kind":"range","sortDate":"2026-08-01"}`)
	rec := e.pub("")
	var l struct {
		Items      []pubItem
		NextCursor *string
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &l)
	got := []string{}
	for _, it := range l.Items {
		got = append(got, it.Slug)
	}
	if strings.Join(got, ",") != "work,new,old,nodate" {
		t.Errorf("並び順（日付の新しい順・日付不明は最後）: %v", got)
	}
	rec = e.pub("?section=blog")
	if strings.Contains(rec.Body.String(), `"work"`) || strings.Contains(rec.Body.String(), "nodate") {
		t.Errorf("section絞り込み: %s", rec.Body.String())
	}
	// ページング
	var seen []string
	cursor := ""
	for i := 0; i < 5; i++ {
		path := "?limit=1"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		rec := e.pub(path)
		var pg struct {
			Items      []pubItem
			NextCursor *string
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &pg)
		if len(pg.Items) != 1 {
			t.Fatalf("ページ%d: %s", i, rec.Body.String())
		}
		seen = append(seen, pg.Items[0].Slug)
		if pg.NextCursor == nil {
			break
		}
		cursor = *pg.NextCursor
	}
	if strings.Join(seen, ",") != "work,new,old,nodate" {
		t.Errorf("ページングで通して読むと: %v", seen)
	}
}

// 公開クレジット（D08・受け入れ7・8）。同意中の人だけ名前が出る。撤回・削除は即座に匿名になる（再公開も古い版も不要）。
func TestPublicCreditsAreResolvedAtDeliveryTime(t *testing.T) {
	e, as := cmsSetup(t)
	e.addUser("p.consent", "active", goodPass)
	e.addUser("p.none", "active", goodPass)
	e.addUser("p.withdrawn", "active", goodPass)
	e.addUser("p.gone", "disabled", goodPass) // 退会（停止）したが同意は続いている
	names := map[string]string{"p.consent": "同意中の人", "p.none": "未同意の人", "p.withdrawn": "撤回した人", "p.gone": "退会した人"}
	acts := map[string]actor{}
	for id, name := range names {
		if id == "p.gone" {
			continue
		}
		a := e.signIn(id + "")
		acts[id] = a
		if rec := e.patchProfile(a, fmt.Sprintf(`{"displayName":%q,"legalName":"秘密の氏名-%s","studentNumber":"99X-%s"}`, name, id, id[2:3])); rec.Code != 200 {
			t.Fatalf("プロフィール: %d %s", rec.Code, rec.Body.String())
		}
	}
	// 退会者の表示名は直接設定（ログインできない）
	if _, err := e.pool.Exec(context.Background(), `UPDATE profiles SET display_name='退会した人' WHERE user_id=$1`, e.userID("p.gone")); err != nil {
		t.Fatal(err)
	}
	consent := func(id string, grant bool) {
		method := "PUT"
		body := `{"granted":true}`
		if !grant {
			body = `{"granted":false}`
		}
		if rec := e.as(acts[id], method, "/me/profile/public-credit-consent", body); rec.Code != 200 && rec.Code != 204 {
			t.Fatalf("同意 %s: %d %s", id, rec.Code, rec.Body.String())
		}
	}
	consent("p.consent", true)
	consent("p.withdrawn", true)
	consent("p.withdrawn", false)
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO public_credit_consents (user_id, granted_at) VALUES ($1, now() - interval '1 day')`, e.userID("p.gone")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO personal_profiles (user_id, legal_name) VALUES ($1, '秘密の氏名-gone') ON CONFLICT (user_id) DO UPDATE SET legal_name=EXCLUDED.legal_name`, e.userID("p.gone")); err != nil {
		t.Fatal(err)
	}
	_, w := e.createContent(as.author, project("credited-work"))
	e.as(as.author, "PUT", "/contents/"+w.ID+"/contributors", fmt.Sprintf(`{"baseRevision":1,"contributors":[{"userId":%q,"roleLabel":"企画"},{"userId":%q,"roleLabel":"3Dモデル"},{"userId":%q,"roleLabel":"音楽"},{"userId":%q,"roleLabel":"プログラム"}]}`,
		e.userID("p.consent"), e.userID("p.none"), e.userID("p.withdrawn"), e.userID("p.gone")))
	if code, rec := e.publish(as.editor, w.ID, 2); code != 200 {
		t.Fatalf("公開: %d %s", code, rec.Body.String())
	}
	check := func(label string, want map[string]string) {
		t.Helper()
		code, d, body := e.pubDetail("project", "credited-work")
		if code != 200 || len(d.Contributors) != len(want) {
			t.Fatalf("%s: %d %s", label, code, body)
		}
		for _, c := range d.Contributors {
			if want[c.RoleLabel] != c.DisplayName {
				t.Errorf("%s: 役割%q の表示 %q（%qのはず）", label, c.RoleLabel, c.DisplayName, want[c.RoleLabel])
			}
		}
		for _, leak := range []string{"秘密の氏名", "99X-", e.userID("p.none").String(), e.userID("p.consent").String(), "p.consent", "p.none"} {
			if strings.Contains(body, leak) {
				t.Errorf("%s: 公開APIに本人情報/IDが出ている: %q", label, leak)
			}
		}
		if label != "初期" { // 匿名の人の表示名は出さない
			for _, n := range []string{"未同意の人", "撤回した人"} {
				if strings.Contains(body, n) {
					t.Errorf("%s: 匿名のはずの名前 %s が出ている", label, n)
				}
			}
		}
	}
	base := map[string]string{"企画": "同意中の人", "3Dモデル": "匿名のメンバー", "音楽": "匿名のメンバー", "プログラム": "退会した人"}
	check("初期", base)
	etagBefore := e.pub("/project/credited-work").Header().Get("ETag")
	// 同意中の人が表示名を変えると、再公開なしで反映され、ETagも変わる
	if rec := e.patchProfile(acts["p.consent"], `{"displayName":"改名した人"}`); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	if _, d, _ := e.pubDetail("project", "credited-work"); d.Contributors[0].DisplayName != "改名した人" {
		t.Errorf("改名が反映されない: %+v", d.Contributors)
	}
	if e.pub("/project/credited-work").Header().Get("ETag") == etagBefore {
		t.Errorf("表示名の変更でETagが変わらない（304で古い名前が残る）")
	}
	// 撤回は即座に匿名化される（再公開も不要）
	consent("p.consent", false)
	check("撤回後", map[string]string{"企画": "匿名のメンバー", "3Dモデル": "匿名のメンバー", "音楽": "匿名のメンバー", "プログラム": "退会した人"})
	// 個人情報の削除（Adminの操作）でも同意が撤回され、匿名になる
	if rec := e.as(as.admin, "DELETE", "/admin/users/"+e.userID("p.gone").String()+"/private-profile", `{"reason":"本人の依頼"}`); rec.Code != 200 && rec.Code != 204 {
		t.Fatalf("個人情報の削除: %d %s", rec.Code, rec.Body.String())
	}
	check("削除後", map[string]string{"企画": "匿名のメンバー", "3Dモデル": "匿名のメンバー", "音楽": "匿名のメンバー", "プログラム": "匿名のメンバー"})
	// 版・制作メンバーの表には、名前を焼き込んでいない（古い版を何度配信しても結果は同じ）
	var dump string
	_ = e.pool.QueryRow(context.Background(), `SELECT COALESCE(string_agg(title||summary||markdown||metadata_json::text, ' '),'') FROM content_revisions`).Scan(&dump)
	var dump2 string
	_ = e.pool.QueryRow(context.Background(), `SELECT COALESCE(string_agg(role_label, ' '),'') FROM work_contributors`).Scan(&dump2)
	for _, n := range []string{"同意中の人", "改名した人", "未同意の人", "撤回した人", "退会した人"} {
		if strings.Contains(dump, n) || strings.Contains(dump2, n) {
			t.Errorf("版・制作メンバーに名前 %s が保存されている", n)
		}
	}
}

// プロフィールの制作実績（公開版が本人をクレジットしている作品）。同意が続く間だけ他のメンバーに見える。
func TestProfileWorks(t *testing.T) {
	e, as := cmsSetup(t)
	consentBody := `{"granted":true}`
	if rec := e.as(as.collab, "PUT", "/me/profile/public-credit-consent", consentBody); rec.Code != 200 && rec.Code != 204 {
		t.Fatal(rec.Code)
	}
	collabID := e.userID("collab.example").String()
	mk := func(slug, role string) string {
		_, w := e.createContent(as.author, project(slug))
		e.as(as.author, "PUT", "/contents/"+w.ID+"/contributors", fmt.Sprintf(`{"baseRevision":1,"contributors":[{"userId":%q,"roleLabel":%q},{"userId":%q,"roleLabel":"監督"}]}`, collabID, role, collabID))
		return w.ID
	}
	pubWork := mk("works-pub", "企画")
	mk("works-draft", "音楽")
	if code, _ := e.publish(as.editor, pubWork, 2); code != 200 {
		t.Fatal(code)
	}
	type works struct {
		Works []struct {
			Section, Slug, Title string
			Roles                []string
		}
	}
	get := func(a actor, path string) works {
		rec := e.as(a, "GET", path, "")
		var w works
		if rec.Code != 200 {
			t.Fatalf("%s: %d", path, rec.Code)
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &w)
		return w
	}
	// 本人: 公開済みの作品だけ（下書きの作品は出ない）。役割は並び順どおり
	me := get(as.collab, "/me/profile")
	if len(me.Works) != 1 || me.Works[0].Slug != "works-pub" || me.Works[0].Section != "project" || strings.Join(me.Works[0].Roles, ",") != "企画,監督" {
		t.Errorf("本人の制作実績: %+v", me.Works)
	}
	// 他のメンバー: 同意が続いている間だけ
	if w := get(as.other, "/members/"+collabID); len(w.Works) != 1 {
		t.Errorf("同意中のメンバーの制作実績: %+v", w.Works)
	}
	if rec := e.as(as.collab, "PUT", "/me/profile/public-credit-consent", `{"granted":false}`); rec.Code != 200 && rec.Code != 204 {
		t.Fatal(rec.Code)
	}
	if w := get(as.other, "/members/"+collabID); len(w.Works) != 0 {
		t.Errorf("撤回後も他のメンバーに制作実績が見える: %+v", w.Works)
	}
	if me := get(as.collab, "/me/profile"); len(me.Works) != 1 {
		t.Errorf("本人には見える: %+v", me.Works)
	}
	// 公開停止すると制作実績からも消える
	e.as(as.editor, "POST", "/contents/"+pubWork+"/unpublish", "")
	if me := get(as.collab, "/me/profile"); len(me.Works) != 0 {
		t.Errorf("公開停止後: %+v", me.Works)
	}
}
