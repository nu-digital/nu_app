//go:build integration

package api_test

import (
	"fmt"
	"testing"
)

type listRow struct {
	ID, Title, Slug, DateLabel string
	SortDate, CoverURL         *string
	IsPublished, CanEdit       bool
	CanDelete                  bool
	Members                    []struct {
		UserID, DisplayName string
		AvatarMediaID       *string
	}
}

func (e *env) listContents(a actor, query string) []listRow {
	e.t.Helper()
	rec := e.as(a, "GET", "/contents"+query, "")
	if rec.Code != 200 {
		e.t.Fatalf("一覧%s: %d %s", query, rec.Code, rec.Body)
	}
	var r struct{ Items []listRow }
	decode(e.t, rec, &r)
	return r.Items
}

func (e *env) saveCoverDate(a actor, id string, base int, title, cover, date string) int {
	e.t.Helper()
	coverJSON := "null"
	if cover != "" {
		coverJSON = fmt.Sprintf("%q", cover)
	}
	body := fmt.Sprintf(`{"baseRevision":%d,"title":%q,"summary":"要約","markdown":"本文","metadata":{},"coverMediaId":%s,"date":%s}`, base, title, coverJSON, date)
	return e.as(a, "POST", "/contents/"+id+"/revisions", body).Code
}

// 一覧: サムネイル・日付・関わるメンバー。編集できる人には作業版（カバーは認証つきの配信URL・共同編集者も）、
// できない人には公開版だけ（カバーは公開URL・共同編集者は出ない・作業版のタイトルや画像は漏れない）。
func TestContentListThumbnailDateMembers(t *testing.T) {
	e, as, _ := mediaSetup(t)
	_, c := e.createContent(as.author, article("list-a"))
	if rec := e.as(as.author, "PUT", "/contents/"+c.ID+"/collaborators", fmt.Sprintf(`{"userIds":[%q]}`, e.userID("collab.example").String())); rec.Code != 200 {
		t.Fatalf("共同編集者: %d %s", rec.Code, rec.Body)
	}
	_, m1 := e.uploadPurpose(as.author, "cms", 31)
	_, m2 := e.uploadPurpose(as.author, "cms", 32)
	day := `{"label":"2025年12月3日","kind":"day","sortDate":"2025-12-03"}`
	if code := e.saveCoverDate(as.author, c.ID, 1, "公開するタイトル", m1.ID, day); code != 201 {
		t.Fatalf("保存2: %d", code)
	}
	if code, _ := e.publish(as.editor, c.ID, 2); code != 200 {
		t.Fatal("公開")
	}
	if code := e.saveCoverDate(as.author, c.ID, 2, "作業中の新しいタイトル", m2.ID, day); code != 201 {
		t.Fatalf("保存3: %d", code)
	}

	mine := e.listContents(as.author, "")
	if len(mine) != 1 || mine[0].Title != "作業中の新しいタイトル" || mine[0].CoverURL == nil || *mine[0].CoverURL != "/api/v1/media/"+m2.ID ||
		mine[0].DateLabel != "2025年12月3日" || mine[0].SortDate == nil || *mine[0].SortDate != "2025-12-03" {
		t.Fatalf("作者の一覧: %+v", mine)
	}
	if len(mine[0].Members) != 2 || mine[0].Members[0].DisplayName != "作者A" || mine[0].Members[1].DisplayName != "共同B" {
		t.Errorf("作者から見たメンバー: %+v", mine[0].Members)
	}
	other := e.listContents(as.other, "")
	if len(other) != 1 || other[0].Title != "公開するタイトル" || other[0].CanEdit {
		t.Fatalf("無関係なメンバーの一覧: %+v", other)
	}
	if other[0].CoverURL == nil || *other[0].CoverURL != "/api/v1/public/media/"+m1.PublicID {
		t.Errorf("公開版のカバー（公開URL）のはず: %v", other[0].CoverURL)
	}
	if len(other[0].Members) != 1 || other[0].Members[0].DisplayName != "作者A" {
		t.Errorf("編集できない人には共同編集者が出ない: %+v", other[0].Members)
	}
	// 作業版のカバー（未公開）は、公開URLでは匿名に配信されない
	if rec := e.do("GET", "/public/media/"+m2.PublicID, ""); rec.Code != 404 {
		t.Errorf("未公開の画像が匿名に配信される: %d", rec.Code)
	}
}

func TestContentListSearchAndStatus(t *testing.T) {
	e, as := cmsSetup(t)
	_, a := e.createContent(as.author, article("s-pub"))
	_, b := e.createContent(as.author, article("s-draft"))
	e.saveRev(as.author, a.ID, 1, "公開する活動報告", "x")
	if code, _ := e.publish(as.editor, a.ID, 2); code != 200 {
		t.Fatal("公開")
	}
	e.saveRev(as.author, b.ID, 1, "書きかけの案内", "x")
	if got := e.listContents(as.author, ""); len(got) != 2 {
		t.Fatalf("すべて: %d", len(got))
	}
	if got := e.listContents(as.author, "?status=published"); len(got) != 1 || got[0].ID != a.ID {
		t.Errorf("公開中: %+v", got)
	}
	if got := e.listContents(as.author, "?status=draft"); len(got) != 1 || got[0].ID != b.ID {
		t.Errorf("下書き: %+v", got)
	}
	if got := e.listContents(as.author, "?q=活動"); len(got) != 1 || got[0].ID != a.ID {
		t.Errorf("検索: %+v", got)
	}
	if got := e.listContents(as.author, "?q=%25"); len(got) != 0 { // %はワイルドカードとして扱わない
		t.Errorf("%%のエスケープ: %+v", got)
	}
	// 無関係なメンバーは、公開版のタイトルだけで検索される（下書きのタイトルでは見つからない）
	if got := e.listContents(as.other, "?q=書きかけ"); len(got) != 0 {
		t.Errorf("下書きのタイトルが検索で漏れる: %+v", got)
	}
	if got := e.listContents(as.other, "?status=draft"); len(got) != 0 {
		t.Errorf("無関係なメンバーに下書きが出る: %+v", got)
	}
	if rec := e.as(as.author, "GET", "/contents?status=bogus", ""); rec.Code != 400 {
		t.Errorf("不正なstatus: %d", rec.Code)
	}
}

// 既存記事（移行した記事）のカバーは metadata.image（公式サイトの /images/…）。一覧のカバーURLに出る。
func TestContentListLegacyCover(t *testing.T) {
	e, as := cmsSetup(t)
	body := `{"type":"article","section":"blog","slug":"legacy-a","title":"移行した記事","summary":"","markdown":"本文","metadata":{"image":"/images/blog/x.webp","category":"Blog"}}`
	if code, _ := e.createContent(as.editor, body); code != 201 {
		t.Fatalf("作成: %d", code)
	}
	got := e.listContents(as.editor, "")
	if len(got) != 1 || got[0].CoverURL == nil || *got[0].CoverURL != "/images/blog/x.webp" {
		t.Fatalf("既存記事のカバー: %+v", got)
	}
	// `..` を含む不正な値は出さない
	e.exec(`ALTER TABLE content_revisions DISABLE TRIGGER USER`)
	e.exec(`UPDATE content_revisions SET metadata_json = '{"image":"/images/../x.webp"}'::jsonb`)
	e.exec(`ALTER TABLE content_revisions ENABLE TRIGGER USER`)
	if got := e.listContents(as.editor, ""); len(got) != 1 || got[0].CoverURL != nil {
		t.Errorf("不正な画像パスが出る: %+v", got)
	}
}
