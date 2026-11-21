//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nu-digital/nu_app/services/api/internal/legacyimport"
)

const (
	legacyContent  = "../../../../apps/web/content"
	legacyBaseline = "../../../../docs/legacy-baseline"
)

// 既存記事を移行した状態（投稿者はすべて未確認）を作る。Owner・Editor・メンバーも用意する。
func legacySetup(t *testing.T) (*env, cActors) {
	e, as := cmsSetup(t)
	e.addUser("owner.example", "active", goodPass, "owner")
	rep, err := legacyimport.Run(context.Background(), e.pool, legacyimport.Options{ContentDir: legacyContent, BaselineDir: legacyBaseline, AsLoginID: "owner.example", OSUser: "test", Apply: true})
	if err != nil || len(rep.Problems) != 0 || !rep.Applied || len(rep.Created) != 67 {
		t.Fatalf("移行: %v %+v", err, rep.Problems)
	}
	return e, as
}

// 投稿者名ごとの代表記事（区分・slug）。
func articleBy(t *testing.T, author string) (string, string) {
	arts, _ := legacyimport.Load(legacyContent)
	for _, a := range arts {
		if a.Author == author {
			return a.Collection, a.Slug
		}
	}
	t.Fatalf("投稿者 %s の記事が無い", author)
	return "", ""
}

func (e *env) authorOf(section, slug string) string {
	e.t.Helper()
	code, d, body := e.pubDetail(section, slug)
	if code != 200 {
		e.t.Fatalf("%s/%s: %d %s", section, slug, code, body)
	}
	return d.Author
}

func (e *env) setAuthor(name, state string) {
	e.t.Helper()
	switch state {
	case "permitted":
		e.exec(`UPDATE legacy_authors SET publication_state='permitted', confirmed_by=(SELECT id FROM users WHERE login_id='owner.example'), confirmed_at=now(), withdrawn_at=NULL WHERE source_name=$1`, name)
	case "withdrawn":
		e.exec(`UPDATE legacy_authors SET publication_state='withdrawn', withdrawn_at=now() WHERE source_name=$1`, name)
	case "unconfirmed":
		e.exec(`UPDATE legacy_authors SET publication_state='unconfirmed', confirmed_by=NULL, confirmed_at=NULL, withdrawn_at=NULL WHERE source_name=$1`, name)
	}
}

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), sql, args...); err != nil {
		e.t.Fatal(err)
	}
}

// (2) 未登録・公開許可が未確認なら、公開APIは「nu_digital」を返し、名前はどこにも出ない。
func TestUnconfirmedAuthorShowsNuDigital(t *testing.T) {
	e, _ := legacySetup(t)
	rec := e.pub("?limit=100")
	var l struct{ Items []pubItem }
	_ = json.Unmarshal(rec.Body.Bytes(), &l)
	if rec.Code != 200 || len(l.Items) != 67 {
		t.Fatalf("一覧: %d 件数%d", rec.Code, len(l.Items))
	}
	for _, it := range l.Items {
		if it.Author != "nu_digital" {
			t.Errorf("%s: 未確認なのに投稿者が %q", it.Slug, it.Author)
		}
	}
	for _, name := range []string{"yurarara", "iwa", "mizmelon"} {
		sec, slug := articleBy(t, name)
		code, d, body := e.pubDetail(sec, slug)
		if code != 200 || d.Author != "nu_digital" {
			t.Errorf("%s: %d author=%q", name, code, d.Author)
		}
		// 公開APIの出力のどこにも、名前が値として現れない（メタデータ・投稿者・その他）
		if strings.Contains(body, `"`+name+`"`) || strings.Contains(rec.Body.String(), `"`+name+`"`) {
			t.Errorf("%s: 公開APIの出力に未確認の投稿者名が出ている", name)
		}
		if strings.Count(body, `"author":`) != 1 {
			t.Errorf("%s: authorのキーが1つではない: %s", name, body)
		}
	}
	// 団体名の記事（投稿者なし）も同じ表示
	sec, slug := articleBy(t, "nu_digital")
	if e.authorOf(sec, slug) != "nu_digital" {
		t.Error("団体名の記事")
	}
}

// (1) 管理者が公開許可を確認・記録した投稿者名は表示できる（未登録＝アプリのユーザーに未紐付けでも）。他の投稿者には影響しない。
func TestPermittedAuthorIsShownWithoutAnyLinkedUser(t *testing.T) {
	e, _ := legacySetup(t)
	e.setAuthor("yurarara", "permitted")
	if e.count(`SELECT count(*)::int FROM legacy_authors WHERE linked_user_id IS NOT NULL`) != 0 {
		t.Fatal("紐付けは無いはず")
	}
	sec, slug := articleBy(t, "yurarara")
	if got := e.authorOf(sec, slug); got != "yurarara" {
		t.Errorf("許可済みの投稿者名: %q", got)
	}
	rec := e.pub("?limit=100")
	var l struct{ Items []pubItem }
	_ = json.Unmarshal(rec.Body.Bytes(), &l)
	named, anon := 0, 0
	for _, it := range l.Items {
		switch it.Author {
		case "yurarara":
			named++
		case "nu_digital":
			anon++
		default:
			t.Errorf("想定外の投稿者 %q", it.Author)
		}
	}
	if named != 58 || anon != 9 {
		t.Errorf("一覧の内訳 名前%d・nu_digital%d（58と9のはず。iwa・mizmelonは未確認のまま）", named, anon)
	}
	for _, other := range []string{"iwa", "mizmelon"} {
		s2, sl2 := articleBy(t, other)
		if e.authorOf(s2, sl2) != "nu_digital" {
			t.Errorf("%s: 他の投稿者の許可で表示された", other)
		}
	}
}

// (3) ユーザーへの紐付けで、公開可否が勝手に変わらない。(5) 同じ表示名の別ユーザーへ誤って紐付かない（紐付けはIDで行う）。
func TestLinkingNeverChangesPublicationState(t *testing.T) {
	e, _ := legacySetup(t)
	e.addUser("same1.example", "active", goodPass)
	e.addUser("same2.example", "active", goodPass)
	for _, id := range []string{"same1.example", "same2.example"} { // 表示名が同じ別ユーザー
		e.exec(`UPDATE profiles SET display_name='yurarara' WHERE user_id=$1`, e.userID(id))
	}
	sec, slug := articleBy(t, "yurarara")
	link := func(login string) {
		e.exec(`UPDATE legacy_authors SET linked_user_id=$1, linked_by=(SELECT id FROM users WHERE login_id='owner.example'), linked_at=now() WHERE source_name='yurarara'`, e.userID(login))
	}
	linked := func() string {
		var l string
		_ = e.pool.QueryRow(context.Background(), `SELECT u.login_id FROM legacy_authors a JOIN users u ON u.id=a.linked_user_id WHERE a.source_name='yurarara'`).Scan(&l)
		return l
	}
	// 未確認のまま紐付けても、公開されない
	link("same2.example")
	if linked() != "same2.example" || e.authorOf(sec, slug) != "nu_digital" {
		t.Errorf("未確認のまま紐付けたら公開された／紐付け先が違う: %s", linked())
	}
	// 許可済みで紐付けを変えても、許可は保たれる。解除しても変わらない
	e.setAuthor("yurarara", "permitted")
	link("same1.example")
	if linked() != "same1.example" || e.authorOf(sec, slug) != "yurarara" {
		t.Errorf("紐付けの変更で公開可否が変わった: %s", linked())
	}
	e.exec(`UPDATE legacy_authors SET linked_user_id=NULL, linked_by=NULL, linked_at=NULL WHERE source_name='yurarara'`)
	if e.authorOf(sec, slug) != "yurarara" {
		t.Error("紐付けの解除で公開可否が変わった")
	}
	// 同じ表示名のユーザーが複数いても、指定したIDにだけ紐付く（表示名では紐付かない）
	link("same2.example")
	if e.count(`SELECT count(*)::int FROM legacy_authors WHERE linked_user_id IS NOT NULL`) != 1 || linked() != "same2.example" {
		t.Errorf("紐付け先: %s", linked())
	}
	// ユーザーの停止・退会（status変更）でも、公開可否は変わらない（継続している公開同意を自動で取り消さない）
	e.exec(`UPDATE users SET status='disabled', disabled_at=now() WHERE id=$1`, e.userID("same2.example"))
	if e.authorOf(sec, slug) != "yurarara" {
		t.Error("紐付け先ユーザーの停止で公開可否が変わった")
	}
}

// (4) 撤回・個人情報の削除の後は、公開経路から名前が取得できない。許可の記録なしに「許可」にも「削除済みで許可」にもできない（DB）。
func TestWithdrawnOrErasedAuthorIsNotReachable(t *testing.T) {
	e, _ := legacySetup(t)
	sec, slug := articleBy(t, "iwa")
	e.setAuthor("iwa", "permitted")
	if e.authorOf(sec, slug) != "iwa" {
		t.Fatal("許可済みの投稿者名が出ない")
	}
	e.setAuthor("iwa", "withdrawn")
	rec := e.pub("?limit=100")
	_, d, body := e.pubDetail(sec, slug)
	if d.Author != "nu_digital" || strings.Contains(body, `"iwa"`) || strings.Contains(rec.Body.String(), `"iwa"`) {
		t.Errorf("撤回後も名前が出る: %q", d.Author)
	}
	// 削除（移行元の名前も匿名の記号へ置換）。許可に戻そうとしても、削除済みはDBが拒否する
	e.exec(`UPDATE legacy_authors SET source_name='erased-' || id::text, erased_at=now() WHERE source_name='iwa'`)
	if e.count(`SELECT count(*)::int FROM legacy_authors WHERE source_name='iwa'`) != 0 {
		t.Error("移行元の名前が残っている")
	}
	if _, err := e.pool.Exec(context.Background(), `UPDATE legacy_authors SET publication_state='permitted', confirmed_by=(SELECT id FROM users LIMIT 1), confirmed_at=now(), withdrawn_at=NULL WHERE erased_at IS NOT NULL`); err == nil {
		t.Error("削除済みの投稿者を許可にできた")
	}
	// 移行を再実行しても、削除した名前は復活しない（同じ名前の新しい行が作られ、未確認から始まる＝公開されない）
	rep, err := legacyimport.Run(context.Background(), e.pool, legacyimport.Options{ContentDir: legacyContent, BaselineDir: legacyBaseline, AsLoginID: "owner.example", Apply: true})
	if err != nil || len(rep.Problems) != 0 {
		t.Fatalf("再実行: %v %v", err, rep.Problems)
	}
	if e.authorOf(sec, slug) != "nu_digital" {
		t.Error("再実行で削除した投稿者名が復活した")
	}
}

// (7) 公開停止・再公開・新しい版の公開・過去の版への切り戻しで、非公開の名前が復活しない（名前は版に入っていない）。
func TestRepublishAndRollbackNeverBringBackAName(t *testing.T) {
	e, as := legacySetup(t)
	sec, slug := articleBy(t, "yurarara")
	var cid string
	_ = e.pool.QueryRow(context.Background(), `SELECT id::text FROM contents WHERE section=$1 AND slug=$2`, sec, slug).Scan(&cid)
	e.setAuthor("yurarara", "permitted")
	if e.authorOf(sec, slug) != "yurarara" {
		t.Fatal("許可済み")
	}
	e.setAuthor("yurarara", "withdrawn") // 撤回
	// 公開停止→再公開
	if rec := e.as(as.editor, "POST", "/contents/"+cid+"/unpublish", ""); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	if code, _ := e.publish(as.editor, cid, 1); code != 200 {
		t.Fatal("再公開できない")
	}
	if e.authorOf(sec, slug) != "nu_digital" {
		t.Error("再公開で名前が復活した")
	}
	// 新しい版を保存して公開
	if code, rec := e.saveRev(as.editor, cid, 1, "新しい版", "本文"); code != 201 {
		t.Fatalf("%d %s", code, rec.Body.String())
	}
	if code, _ := e.publish(as.editor, cid, 2); code != 200 {
		t.Fatal("公開できない")
	}
	if e.authorOf(sec, slug) != "nu_digital" {
		t.Error("新しい版の公開で名前が復活した")
	}
	// 過去の版（版1）への切り戻し（公開ポインタを直接戻す）
	e.exec(`UPDATE contents SET published_revision_id=(SELECT id FROM content_revisions WHERE content_id=$1 AND revision_no=1) WHERE id=$1`, cid)
	if e.authorOf(sec, slug) != "nu_digital" {
		t.Error("切り戻しで名前が復活した")
	}
	// どの版・公開イベント・監査ログにも名前が値として無い（版は不変で消せないので、そもそも入れていない）
	if e.count(`SELECT count(*)::int FROM content_revisions r, jsonb_each_text(r.metadata_json) kv WHERE kv.value='yurarara'`) != 0 {
		t.Error("版のメタデータに名前がある")
	}
	if e.count(`SELECT count(*)::int FROM audit_logs WHERE summary_json::text LIKE '%yurarara%'`) != 0 {
		t.Error("監査ログに名前がある")
	}
	// 許可に戻せば、再び表示される（状態は投稿者の行が持つ）
	e.setAuthor("yurarara", "permitted")
	if e.authorOf(sec, slug) != "yurarara" {
		t.Error("許可に戻しても表示されない")
	}
}

// (8) CMSの入力で投稿者名は決められない（Editorでも）。CMSで新規に作った記事の投稿者は既定のnu_digital。
func TestCMSCreatedContentUsesDefaultAuthor(t *testing.T) {
	e, as := legacySetup(t)
	_, v := e.createContent(as.editor, `{"type":"article","section":"blog","slug":"cms-new","title":"新しい記事","markdown":"本文"}`)
	if code, _ := e.publish(as.editor, v.ID, 1); code != 200 {
		t.Fatal("公開できない")
	}
	if e.authorOf("blog", "cms-new") != "nu_digital" {
		t.Error("CMS作成の記事の投稿者")
	}
	if code, rec := e.saveWith(as.editor, v.ID, 1, "t", "本文", `,"metadata":{"author":"yurarara"}`); code != 422 {
		t.Errorf("Editorがauthorを指定: %d %s", code, rec.Body.String())
	}
}
