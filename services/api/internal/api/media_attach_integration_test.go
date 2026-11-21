//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	xwebp "golang.org/x/image/webp"
)

func (e *env) uploadPurpose(a actor, purpose string, seed int) (int, mediaOut) {
	e.t.Helper()
	rec := e.upload(a, purpose, "image/png", pngImg(e.t, 40, 30, seed))
	var m mediaOut
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	return rec.Code, m
}

func (e *env) mediaStatus(a actor, id string) int { return e.mediaGet(a, "/media/"+id).Code }

// アバター: 自分がアップロードしたavatar用の画像だけ設定でき、ログイン中のメンバーに配信される。匿名・公開・他の用途には出ない。
func TestAvatarAttachAndDelivery(t *testing.T) {
	e, as, _ := mediaSetup(t)
	code, m1 := e.uploadPurpose(as.author, "avatar", 1)
	if code != 201 {
		t.Fatalf("アップロード: %d", code)
	}
	attach := func(a actor, id string) int {
		return e.as(a, "PUT", "/me/profile/avatar", fmt.Sprintf(`{"mediaId":%q}`, id)).Code
	}
	// 設定前: 他のメンバーには見えない（アップロード者だけ）
	if e.mediaStatus(as.other, m1.ID) != 404 || e.mediaStatus(as.author, m1.ID) != 200 {
		t.Error("設定前の配信")
	}
	// 他人のアップロードした画像・cms用・存在しない画像は設定できない（同じ422）
	_, cms := e.uploadPurpose(as.author, "cms", 2)
	for name, c := range map[string]int{
		"他人の画像": attach(as.other, m1.ID),
		"cms用":  attach(as.author, cms.ID),
		"存在しない": attach(as.author, "00000000-0000-0000-0000-000000000000"),
		"不正なID": attach(as.author, "x"),
	} {
		if c != 422 {
			t.Errorf("%s: %d", name, c)
		}
	}
	if code := e.as(as.author, "PUT", "/me/profile/avatar", `{}`).Code; code != 422 {
		t.Errorf("mediaIdなし: %d", code)
	}
	if attach(as.author, m1.ID) != 204 {
		t.Fatal("設定できない")
	}
	// 配信: 他のメンバー・Editor・Adminは200（WebP・private・no-store・nosniff）。匿名は401。公開・by-publicは別（404）
	for name, a := range map[string]actor{"他のメンバー": as.other, "Editor": as.editor, "Admin": as.admin} {
		rec := e.mediaGet(a, "/media/"+m1.ID)
		if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/webp" || rec.Header().Get("Cache-Control") != "private, no-store" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: %d %v", name, rec.Code, rec.Header())
			continue
		}
		if _, err := xwebp.Decode(bytes.NewReader(rec.Body.Bytes())); err != nil {
			t.Errorf("%s: 画像でない", name)
		}
	}
	if rec := e.do("GET", "/media/"+m1.ID, ""); rec.Code != 401 {
		t.Errorf("匿名: %d", rec.Code)
	}
	var m1pub string
	_ = e.pool.QueryRow(context.Background(), `SELECT public_id FROM media_assets WHERE id=$1::uuid`, m1.ID).Scan(&m1pub)
	if rec := e.do("GET", "/public/media/"+m1pub, ""); rec.Code != 404 {
		t.Errorf("公開経路にアバターが出る: %d", rec.Code)
	}
	if e.mediaGet(as.other, "/media/by-public/"+m1pub).Code != 404 {
		t.Error("by-publicにアバターが出る")
	}
	// プロフィール・メンバー一覧に avatarMediaId が出る
	var me struct{ AvatarMediaID *string }
	_ = json.Unmarshal(e.as(as.author, "GET", "/me/profile", "").Body.Bytes(), &me)
	if me.AvatarMediaID == nil || *me.AvatarMediaID != m1.ID {
		t.Errorf("me/profile: %v", me.AvatarMediaID)
	}
	var list struct {
		Items []struct {
			ID            string
			DisplayName   string
			AvatarMediaID *string
		}
	}
	_ = json.Unmarshal(e.as(as.other, "GET", "/members?limit=100", "").Body.Bytes(), &list)
	found := false
	for _, it := range list.Items {
		if it.DisplayName == "作者A" {
			found = it.AvatarMediaID != nil && *it.AvatarMediaID == m1.ID
		} else if it.AvatarMediaID != nil {
			t.Errorf("アバターの無い人に付いている: %s", it.DisplayName)
		}
	}
	if !found {
		t.Error("メンバー一覧にアバターが無い")
	}
	// 差し替え: 古い画像は他のメンバーには見えなくなる（参照が外れる）。新しい画像は見える
	_, m2 := e.uploadPurpose(as.author, "avatar", 3)
	if attach(as.author, m2.ID) != 204 {
		t.Fatal("差し替え")
	}
	if e.mediaStatus(as.other, m1.ID) != 404 || e.mediaStatus(as.other, m2.ID) != 200 || e.mediaStatus(as.author, m1.ID) != 200 {
		t.Error("差し替え後の配信")
	}
	if e.count(`SELECT count(*)::int FROM media_references WHERE owner_type='profile'`) != 1 {
		t.Error("参照が1つではない")
	}
	// 無効なユーザー（停止）のアバターは見えない。再有効化で戻る
	e.exec(`UPDATE users SET status='disabled', disabled_at=now() WHERE login_id='author.example'`)
	if e.mediaStatus(as.other, m2.ID) != 404 {
		t.Error("停止中のユーザーのアバターが見える")
	}
	e.exec(`UPDATE users SET status='active', disabled_at=NULL WHERE login_id='author.example'`)
	// 解除: 見えなくなる。冪等
	if code := e.as(as.author, "DELETE", "/me/profile/avatar", "").Code; code != 204 {
		t.Errorf("解除: %d", code)
	}
	if code := e.as(as.author, "DELETE", "/me/profile/avatar", "").Code; code != 204 {
		t.Errorf("解除の再実行: %d", code)
	}
	if e.mediaStatus(as.other, m2.ID) != 404 || e.count(`SELECT count(*)::int FROM media_references WHERE owner_type='profile'`) != 0 {
		t.Error("解除後")
	}
	// 解除・差し替えで古くなった画像は、参照が無いので media-gc の対象になる（行は残るが配信されない）
	if e.count(`SELECT count(*)::int FROM media_assets WHERE purpose='avatar'`) != 2 {
		t.Error("画像の行")
	}
}

// 個人情報の削除（D08）でアバターが外れ、他のメンバーに配信されなくなる。
func TestAvatarRemovedOnPrivateProfileDeletion(t *testing.T) {
	e, as, _ := mediaSetup(t)
	code, m := e.uploadPurpose(as.collab, "avatar", 4)
	if code != 201 {
		t.Fatal(code)
	}
	e.as(as.collab, "PUT", "/me/profile/avatar", fmt.Sprintf(`{"mediaId":%q}`, m.ID))
	if e.mediaStatus(as.other, m.ID) != 200 {
		t.Fatal("設定できていない")
	}
	e.exec(`UPDATE users SET status='disabled', disabled_at=now() WHERE login_id='collab.example'`)
	rec := e.as(as.admin, "DELETE", "/admin/users/"+e.userID("collab.example").String()+"/private-profile", `{"reason":"本人の依頼"}`)
	if rec.Code != 200 {
		t.Fatalf("削除: %d %s", rec.Code, rec.Body.String())
	}
	if e.count(`SELECT count(*)::int FROM profiles WHERE avatar_media_id IS NOT NULL AND user_id=(SELECT id FROM users WHERE login_id='collab.example')`) != 0 ||
		e.count(`SELECT count(*)::int FROM media_references WHERE owner_type='profile'`) != 0 {
		t.Error("アバターの参照が残っている")
	}
	e.exec(`UPDATE users SET status='active', disabled_at=NULL WHERE login_id='collab.example'`)
	if e.mediaStatus(as.other, m.ID) != 404 {
		t.Error("削除後も他のメンバーに配信される")
	}
}

// 物品の写真: アップロードも設定も管理者だけ。メンバーには配信され、アーカイブすると見えなくなる。
func TestInventoryPhoto(t *testing.T) {
	e, as, _ := mediaSetup(t)
	if code, _ := e.uploadPurpose(as.author, "inventory", 1); code != 403 {
		t.Errorf("メンバーのアップロード: %d", code)
	}
	if code, _ := e.uploadPurpose(as.editor, "inventory", 1); code != 403 {
		t.Errorf("Editorのアップロード: %d", code)
	}
	code, m := e.uploadPurpose(as.admin, "inventory", 2)
	if code != 201 {
		t.Fatalf("管理者のアップロード: %d", code)
	}
	_, item := e.newItem(as.admin, `{"name":"HDMIケーブル","mode":"quantity","totalQuantity":3}`)
	path := "/inventory/items/" + item.ID + "/photo"
	body := fmt.Sprintf(`{"mediaId":%q}`, m.ID)
	for name, a := range map[string]actor{"メンバー": as.author, "Editor": as.editor} {
		if c := e.as(a, "PUT", path, body).Code; c != 403 {
			t.Errorf("%sの設定: %d", name, c)
		}
		if c := e.as(a, "DELETE", path, "").Code; c != 403 {
			t.Errorf("%sの解除: %d", name, c)
		}
	}
	if c := e.do("PUT", path, body); c.Code != 401 {
		t.Errorf("匿名: %d", c.Code)
	}
	if c := e.as(as.admin, "PUT", "/inventory/items/00000000-0000-0000-0000-000000000000/photo", body).Code; c != 404 {
		t.Errorf("存在しない物品: %d", c)
	}
	_, cms := e.uploadPurpose(as.admin, "cms", 3)
	if c := e.as(as.admin, "PUT", path, fmt.Sprintf(`{"mediaId":%q}`, cms.ID)).Code; c != 422 {
		t.Errorf("用途が違う画像: %d", c)
	}
	if e.mediaStatus(as.other, m.ID) != 404 {
		t.Error("設定前にメンバーへ配信される")
	}
	if c := e.as(as.admin, "PUT", path, body).Code; c != 204 {
		t.Fatalf("設定: %d", c)
	}
	if e.mediaStatus(as.other, m.ID) != 200 {
		t.Error("メンバーに配信されない")
	}
	var one struct{ PhotoMediaID *string }
	_ = json.Unmarshal(e.as(as.other, "GET", "/inventory/items/"+item.ID, "").Body.Bytes(), &one)
	if one.PhotoMediaID == nil || *one.PhotoMediaID != m.ID {
		t.Errorf("詳細: %v", one.PhotoMediaID)
	}
	var list struct {
		Items []struct {
			ID           string
			PhotoMediaID *string
		}
	}
	_ = json.Unmarshal(e.as(as.other, "GET", "/inventory/items", "").Body.Bytes(), &list)
	if len(list.Items) != 1 || list.Items[0].PhotoMediaID == nil {
		t.Errorf("一覧: %+v", list.Items)
	}
	e.exec(`UPDATE inventory_items SET archived_at=now() WHERE id=$1::uuid`, item.ID)
	if e.mediaStatus(as.other, m.ID) != 404 {
		t.Error("アーカイブした物品の写真が見える")
	}
	e.exec(`UPDATE inventory_items SET archived_at=NULL WHERE id=$1::uuid`, item.ID)
	if c := e.as(as.admin, "DELETE", path, "").Code; c != 204 {
		t.Errorf("解除: %d", c)
	}
	if e.mediaStatus(as.other, m.ID) != 404 || e.count(`SELECT count(*)::int FROM media_references WHERE owner_type='inventory_item'`) != 0 {
		t.Error("解除後")
	}
}
