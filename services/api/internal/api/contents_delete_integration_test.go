//go:build integration

package api_test

import (
	"fmt"
	"testing"
)

// 記事・作品の削除（論理削除）: 下書きは作者が削除できる。公開中はEditor以上だけ。共同編集者・他人は不可。
// 削除すると一覧・取得・公開API・画像の公開配信から消えるが、データ（版・画像）は残り、DBで戻せる。
func TestContentDelete(t *testing.T) {
	e, as, _ := mediaSetup(t)

	// 下書き: 作者が削除できる。共同編集者・他人は削除できない
	_, d := e.createContent(as.author, article("del-draft"))
	if rec := e.as(as.author, "PUT", "/contents/"+d.ID+"/collaborators", fmt.Sprintf(`{"userIds":[%q]}`, e.userID("collab.example").String())); rec.Code != 200 {
		t.Fatalf("共同編集者: %d", rec.Code)
	}
	if l := e.listContents(as.author, ""); len(l) != 1 || !l[0].CanDelete {
		t.Errorf("作者の下書きはcanDelete: %+v", l)
	}
	if l := e.listContents(as.collab, ""); len(l) != 1 || !l[0].CanEdit || l[0].CanDelete {
		t.Errorf("共同編集者は編集できるが削除はできない: %+v", l)
	}
	if rec := e.as(as.collab, "DELETE", "/contents/"+d.ID, ""); rec.Code != 403 {
		t.Errorf("共同編集者の削除: %d", rec.Code)
	}
	if rec := e.as(as.other, "DELETE", "/contents/"+d.ID, ""); rec.Code != 404 {
		t.Errorf("他人の削除は存在しない扱い: %d", rec.Code)
	}
	if rec := e.as(as.author, "DELETE", "/contents/"+d.ID, ""); rec.Code != 204 {
		t.Fatalf("作者の下書き削除: %d %s", rec.Code, rec.Body)
	}
	if code, _ := e.getContent(as.author, d.ID); code != 404 {
		t.Errorf("削除後の取得: %d", code)
	}
	if rec := e.as(as.author, "DELETE", "/contents/"+d.ID, ""); rec.Code != 404 {
		t.Errorf("二重の削除: %d", rec.Code)
	}
	if code, _ := e.saveRev(as.author, d.ID, 1, "削除後の保存", "x"); code != 404 {
		t.Errorf("削除後の保存: %d", code)
	}
	if n := e.revCount(d.ID); n != 1 {
		t.Errorf("版は残るはず: %d", n)
	}
	if got := e.listContents(as.author, ""); len(got) != 0 {
		t.Errorf("削除後も一覧に残っている: %+v", got)
	}

	// 公開中: 作者（Editor未満）は削除できない。Editorは削除でき、公開APIからも消える。画像の公開配信も止まる
	_, p := e.createContent(as.author, article("del-pub"))
	_, m := e.uploadPurpose(as.author, "cms", 41)
	if code := e.saveCoverDate(as.author, p.ID, 1, "公開する記事", m.ID, `{"label":"2025年12月3日","kind":"day","sortDate":"2025-12-03"}`); code != 201 {
		t.Fatalf("保存: %d", code)
	}
	if code, _ := e.publish(as.editor, p.ID, 2); code != 200 {
		t.Fatal("公開")
	}
	if l := e.listContents(as.author, ""); len(l) != 1 || l[0].CanDelete {
		t.Errorf("公開中の記事は作者でもcanDeleteがfalse: %+v", l)
	}
	if l := e.listContents(as.editor, ""); len(l) != 1 || !l[0].CanDelete {
		t.Errorf("Editorはcandelete: %+v", l)
	}
	if rec := e.as(as.author, "DELETE", "/contents/"+p.ID, ""); rec.Code != 403 {
		t.Errorf("作者が公開中の記事を削除: %d", rec.Code)
	}
	if code, _, _ := e.pubDetail("blog", "del-pub"); code != 200 {
		t.Fatalf("公開API（削除前）: %d", code)
	}
	if rec := e.pubMedia(m.PublicID); rec.Code != 200 {
		t.Fatalf("公開画像（削除前）: %d", rec.Code)
	}
	if rec := e.as(as.editor, "DELETE", "/contents/"+p.ID, ""); rec.Code != 204 {
		t.Fatalf("Editorの削除: %d %s", rec.Code, rec.Body)
	}
	if code, _, _ := e.pubDetail("blog", "del-pub"); code != 404 {
		t.Errorf("削除後の公開API: %d", code)
	}
	if rec := e.pub("?limit=100"); rec.Body.String() != "" && containsStr(rec.Body.String(), "del-pub") {
		t.Errorf("公開一覧に残っている: %s", rec.Body)
	}
	if rec := e.pubMedia(m.PublicID); rec.Code != 404 {
		t.Errorf("削除後の公開画像: %d", rec.Code)
	}
	if got := e.listContents(as.other, ""); len(got) != 0 {
		t.Errorf("他のメンバーの一覧に残っている: %+v", got)
	}
	if n := e.count(`SELECT count(*)::int FROM media_assets WHERE id=$1::uuid`, m.ID); n != 1 {
		t.Errorf("画像は残るはず")
	}
	if n := e.count(`SELECT count(*)::int FROM audit_logs WHERE action='content.deleted'`); n != 2 {
		t.Errorf("監査ログ: %d", n)
	}
	// slugは予約されたまま（URLの再利用を防ぐ）。DBで戻せば復旧できる
	if code, _ := e.createContent(as.author, article("del-pub")); code != 409 {
		t.Errorf("削除済みのslugの再利用: %d", code)
	}
	e.exec(`UPDATE contents SET archived_at=NULL WHERE id=$1::uuid`, p.ID)
	if code, _, _ := e.pubDetail("blog", "del-pub"); code != 200 {
		t.Errorf("復旧後の公開API: %d", code)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// 一覧は記事と作品が混在し、利用者に見せる日付の新しい順。type で絞り込め、指定なしで全件。
func TestContentListMixedByDate(t *testing.T) {
	e, as, _ := mediaSetup(t)
	mk := func(body string, date string, title string) string {
		_, c := e.createContent(as.author, body)
		if code := e.saveCoverDate(as.author, c.ID, 1, title, "", date); code != 201 {
			t.Fatalf("保存: %d", code)
		}
		return c.ID
	}
	day := func(d string) string { return fmt.Sprintf(`{"label":"%s","kind":"day","sortDate":"%s"}`, d, d) }
	mk(article("mix-a"), day("2025-01-10"), "古い記事")
	mk(project("mix-p"), day("2025-12-01"), "新しい作品")
	mk(article("mix-b"), day("2025-06-15"), "中くらいの記事")
	titles := func(q string) []string {
		var out []string
		for _, r := range e.listContents(as.author, q) {
			out = append(out, r.Title)
		}
		return out
	}
	if got := titles(""); fmt.Sprint(got) != "[新しい作品 中くらいの記事 古い記事]" {
		t.Errorf("混在・日付の新しい順: %v", got)
	}
	if got := titles("?type=article"); fmt.Sprint(got) != "[中くらいの記事 古い記事]" {
		t.Errorf("記事: %v", got)
	}
	if got := titles("?type=project"); fmt.Sprint(got) != "[新しい作品]" {
		t.Errorf("作品: %v", got)
	}
	if rec := e.as(as.author, "GET", "/contents?type=x", ""); rec.Code != 400 {
		t.Errorf("不正なtype: %d", rec.Code)
	}
}
