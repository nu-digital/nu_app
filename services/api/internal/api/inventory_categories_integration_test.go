//go:build integration

package api_test

import (
	"encoding/json"
	"fmt"
	"testing"
)

type catOut struct {
	ID, Name  string
	SortOrder int
	ItemCount int
}

func (e *env) categories(a actor) []catOut {
	e.t.Helper()
	rec := e.as(a, "GET", "/inventory/categories", "")
	if rec.Code != 200 {
		e.t.Fatalf("カテゴリ一覧: %d %s", rec.Code, rec.Body)
	}
	var r struct{ Items []catOut }
	decode(e.t, rec, &r)
	return r.Items
}

func catByName(cs []catOut, name string) (catOut, bool) {
	for _, c := range cs {
		if c.Name == name {
			return c, true
		}
	}
	return catOut{}, false
}

// 初期カテゴリ（機材・書籍・その他）がこの順で入っている。メンバーも閲覧できる。
func TestInventoryCategoriesDefault(t *testing.T) {
	e, _, member := invSetup(t)
	cs := e.categories(member)
	if len(cs) != 3 || cs[0].Name != "機材" || cs[1].Name != "書籍" || cs[2].Name != "その他" {
		t.Fatalf("初期カテゴリ: %+v", cs)
	}
}

// 追加・名称変更・並び替え・削除は管理者だけ。匿名は401、メンバーは403。
func TestInventoryCategoriesAuthorization(t *testing.T) {
	e, admin, member := invSetup(t)
	cs := e.categories(admin)
	id := cs[0].ID
	calls := []struct{ m, p, b string }{
		{"POST", "/inventory/categories", `{"name":"工具"}`},
		{"PATCH", "/inventory/categories/" + id, `{"name":"x"}`},
		{"PUT", "/inventory/categories/order", `{"ids":[]}`},
		{"DELETE", "/inventory/categories/" + id, ""},
	}
	for _, c := range calls {
		if rec := e.do(c.m, c.p, c.b); rec.Code != 401 {
			t.Errorf("匿名 %s %s = %d", c.m, c.p, rec.Code)
		}
		if rec := e.as(member, c.m, c.p, c.b); rec.Code != 403 {
			t.Errorf("Member %s %s = %d", c.m, c.p, rec.Code)
		}
	}
	if len(e.categories(admin)) != 3 {
		t.Error("拒否された操作でカテゴリが変わった")
	}
}

func TestInventoryCategoriesCRUD(t *testing.T) {
	e, admin, _ := invSetup(t)
	rec := e.as(admin, "POST", "/inventory/categories", `{"name":"  工具  "}`)
	if rec.Code != 201 {
		t.Fatalf("追加: %d %s", rec.Code, rec.Body)
	}
	var tool catOut
	decode(t, rec, &tool)
	if tool.Name != "工具" {
		t.Errorf("名前が整形されていない: %q", tool.Name)
	}
	for name, body := range map[string]string{"重複": `{"name":"機材"}`, "空": `{"name":" "}`, "31字": fmt.Sprintf(`{"name":%q}`, "あいうえおかきくけこさしすせそたちつてとなにぬねのはひふへほま")} {
		if rec := e.as(admin, "POST", "/inventory/categories", body); rec.Code != 409 && rec.Code != 422 {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	if rec := e.as(admin, "POST", "/inventory/categories", `{"name":"機材"}`); rec.Code != 409 {
		t.Errorf("重複は409のはず: %d", rec.Code)
	}
	// 名称変更は、その物品の（旧クライアント向けの）カテゴリ名にも反映される
	_, it := e.newItem(admin, fmt.Sprintf(`{"name":"ドライバー","mode":"quantity","totalQuantity":2,"categoryId":%q}`, tool.ID))
	if it.Category != "工具" {
		t.Fatalf("作成した物品のカテゴリ: %q", it.Category)
	}
	if rec := e.as(admin, "PATCH", "/inventory/categories/"+tool.ID, `{"name":"工具・備品"}`); rec.Code != 200 {
		t.Fatalf("名称変更: %d %s", rec.Code, rec.Body)
	}
	if got := e.item(admin, it.ID); got.Category != "工具・備品" {
		t.Errorf("名称変更が物品に反映されていない: %q", got.Category)
	}
	if rec := e.as(admin, "PATCH", "/inventory/categories/"+tool.ID, `{"name":"機材"}`); rec.Code != 409 {
		t.Errorf("既存名への変更: %d", rec.Code)
	}
	if rec := e.as(admin, "PATCH", "/inventory/categories/00000000-0000-0000-0000-000000000000", `{"name":"y"}`); rec.Code != 404 {
		t.Errorf("存在しない: %d", rec.Code)
	}
	// 並び替え: 全件を渡す。過不足・重複・不明なIDは拒否
	cs := e.categories(admin)
	ids := []string{cs[3].ID, cs[0].ID, cs[1].ID, cs[2].ID}
	for name, body := range map[string]string{
		"不足": fmt.Sprintf(`{"ids":[%q]}`, ids[0]), "重複": fmt.Sprintf(`{"ids":[%q,%q,%q,%q]}`, ids[0], ids[0], ids[1], ids[2]),
		"不明": fmt.Sprintf(`{"ids":[%q,%q,%q,"00000000-0000-0000-0000-000000000000"]}`, ids[0], ids[1], ids[2]),
	} {
		if rec := e.as(admin, "PUT", "/inventory/categories/order", body); rec.Code != 422 {
			t.Errorf("並び替え（%s）: %d", name, rec.Code)
		}
	}
	if rec := e.as(admin, "PUT", "/inventory/categories/order", fmt.Sprintf(`{"ids":[%q,%q,%q,%q]}`, ids[0], ids[1], ids[2], ids[3])); rec.Code != 200 {
		t.Fatalf("並び替え: %d %s", rec.Code, rec.Body)
	}
	got := e.categories(admin)
	if got[0].ID != tool.ID || got[1].Name != "機材" || got[3].Name != "その他" {
		t.Errorf("並び替えの結果: %+v", got)
	}
}

// 使用中のカテゴリは、移し先を指定したときだけ削除でき、物品は消えずに移る。最後の1件は消せない。
func TestInventoryCategoryDelete(t *testing.T) {
	e, admin, _ := invSetup(t)
	cs := e.categories(admin)
	gear, _ := catByName(cs, "機材")
	books, _ := catByName(cs, "書籍")
	_, it := e.newItem(admin, fmt.Sprintf(`{"name":"カメラ","mode":"individual","categoryId":%q}`, gear.ID))
	if rec := e.as(admin, "DELETE", "/inventory/categories/"+gear.ID, ""); rec.Code != 409 {
		t.Fatalf("使用中の削除は拒否: %d %s", rec.Code, rec.Body)
	}
	if rec := e.as(admin, "DELETE", "/inventory/categories/"+gear.ID+"?moveTo="+gear.ID, ""); rec.Code != 422 {
		t.Errorf("自分自身への移動: %d", rec.Code)
	}
	if rec := e.as(admin, "DELETE", "/inventory/categories/"+gear.ID+"?moveTo=00000000-0000-0000-0000-000000000000", ""); rec.Code != 422 {
		t.Errorf("存在しない移し先: %d", rec.Code)
	}
	// 廃止した物品も移す（外部キーで削除できなくなるため）
	e.exec(`UPDATE inventory_items SET archived_at=now() WHERE id=$1::uuid`, it.ID)
	if rec := e.as(admin, "DELETE", "/inventory/categories/"+gear.ID+"?moveTo="+books.ID, ""); rec.Code != 204 {
		t.Fatalf("移して削除: %d %s", rec.Code, rec.Body)
	}
	e.exec(`UPDATE inventory_items SET archived_at=NULL WHERE id=$1::uuid`, it.ID)
	if got := e.item(admin, it.ID); got.Category != "書籍" {
		t.Errorf("物品が移っていない: %q", got.Category)
	}
	if _, ok := catByName(e.categories(admin), "機材"); ok {
		t.Error("削除されていない")
	}
	// 未使用は移し先なしで削除できる。最後の1件は削除できない
	other, _ := catByName(e.categories(admin), "その他")
	if rec := e.as(admin, "DELETE", "/inventory/categories/"+other.ID, ""); rec.Code != 204 {
		t.Errorf("未使用の削除: %d", rec.Code)
	}
	if rec := e.as(admin, "DELETE", "/inventory/categories/"+books.ID+"?moveTo="+books.ID, ""); rec.Code != 409 {
		t.Errorf("最後のカテゴリ: %d", rec.Code)
	}
}

// カテゴリの指定が無い物品は「その他」に入る。旧クライアントのカテゴリ名の指定でも作れる。存在しないIDは拒否。
func TestInventoryItemCategoryDefaultAndLegacy(t *testing.T) {
	e, admin, _ := invSetup(t)
	_, a := e.newItem(admin, `{"name":"A","mode":"quantity","totalQuantity":1}`)
	if a.Category != "その他" {
		t.Errorf("既定: %q", a.Category)
	}
	_, b := e.newItem(admin, `{"name":"B","mode":"quantity","totalQuantity":1,"category":"VR"}`)
	if b.Category != "VR" {
		t.Errorf("旧クライアントの名前指定: %q", b.Category)
	}
	if _, ok := catByName(e.categories(admin), "VR"); !ok {
		t.Error("名前指定でカテゴリが作られていない")
	}
	if c, _ := e.newItem(admin, `{"name":"C","mode":"quantity","totalQuantity":1,"categoryId":"00000000-0000-0000-0000-000000000000"}`); c != 422 {
		t.Errorf("存在しないカテゴリID: %d", c)
	}
	// 物品のカテゴリ変更
	cs := e.categories(admin)
	books, _ := catByName(cs, "書籍")
	if rec := e.as(admin, "PATCH", "/inventory/items/"+a.ID, fmt.Sprintf(`{"categoryId":%q}`, books.ID)); rec.Code != 200 {
		t.Fatalf("カテゴリ変更: %d %s", rec.Code, rec.Body)
	}
	if got := e.item(admin, a.ID); got.Category != "書籍" {
		t.Errorf("変更後: %q", got.Category)
	}
	// カテゴリIDでの絞り込み
	var list itemsResp
	decode(t, e.as(admin, "GET", "/inventory/items?categoryId="+books.ID, ""), &list)
	if len(list.Items) != 1 || list.Items[0].ID != a.ID {
		t.Errorf("categoryIdの絞り込み: %+v", list.Items)
	}
	if rec := e.as(admin, "GET", "/inventory/items?categoryId=x", ""); rec.Code != 400 {
		t.Errorf("不正なcategoryId: %d", rec.Code)
	}
}

// 複数画像: 順序つきの置き換え。先頭がメイン。管理者だけ。上限・他の用途・他人の画像は拒否。外した画像は配信されない。
func TestInventoryImages(t *testing.T) {
	e, as, _ := mediaSetup(t)
	_, item := e.newItem(as.admin, `{"name":"三脚","mode":"quantity","totalQuantity":2}`)
	path := "/inventory/items/" + item.ID + "/images"
	var ms []mediaOut
	for i := 0; i < 3; i++ {
		code, m := e.uploadPurpose(as.admin, "inventory", 10+i)
		if code != 201 {
			t.Fatalf("アップロード: %d", code)
		}
		ms = append(ms, m)
	}
	body := func(ids ...string) string { b, _ := json.Marshal(map[string]any{"mediaIds": ids}); return string(b) }
	for name, a := range map[string]actor{"メンバー": as.author, "Editor": as.editor} {
		if c := e.as(a, "PUT", path, body(ms[0].ID)).Code; c != 403 {
			t.Errorf("%s: %d", name, c)
		}
	}
	if c := e.do("PUT", path, body(ms[0].ID)).Code; c != 401 {
		t.Errorf("匿名: %d", c)
	}
	if c := e.as(as.admin, "PUT", "/inventory/items/00000000-0000-0000-0000-000000000000/images", body(ms[0].ID)).Code; c != 404 {
		t.Errorf("存在しない物品: %d", c)
	}
	_, cms := e.uploadPurpose(as.admin, "cms", 20)
	for name, b := range map[string]string{
		"用途が違う": body(cms.ID), "存在しない": body("00000000-0000-0000-0000-000000000000"), "不正なID": body("x"), "重複": body(ms[0].ID, ms[0].ID),
	} {
		if c := e.as(as.admin, "PUT", path, b).Code; c != 422 {
			t.Errorf("%s: %d", name, c)
		}
	}
	nine := make([]string, 9)
	for i := range nine {
		nine[i] = ms[0].ID
	}
	if c := e.as(as.admin, "PUT", path, body(nine...)).Code; c != 422 {
		t.Errorf("9枚: %d", c)
	}
	if e.mediaStatus(as.other, ms[0].ID) != 404 {
		t.Error("設定前にメンバーへ配信される")
	}
	// 3枚を設定。先頭がメイン（photoMediaId）、詳細のimagesは同じ順序
	if rec := e.as(as.admin, "PUT", path, body(ms[2].ID, ms[0].ID, ms[1].ID)); rec.Code != 200 {
		t.Fatalf("設定: %d %s", rec.Code, rec.Body)
	}
	var one struct {
		PhotoMediaID *string
		Images       []string
	}
	decode(t, e.as(as.other, "GET", "/inventory/items/"+item.ID, ""), &one)
	if one.PhotoMediaID == nil || *one.PhotoMediaID != ms[2].ID || len(one.Images) != 3 || one.Images[0] != ms[2].ID || one.Images[1] != ms[0].ID || one.Images[2] != ms[1].ID {
		t.Fatalf("詳細: %+v", one)
	}
	for _, m := range ms {
		if e.mediaStatus(as.other, m.ID) != 200 {
			t.Errorf("メンバーに配信されない: %s", m.ID)
		}
	}
	// 別の管理者が、既に付いている画像を含めて並べ替えられる（他人のアップロードでも、この物品に付いているものは可）
	e.addUser("admin2.example", "active", goodPass, "admin")
	admin2 := e.signIn("admin2.example")
	if c := e.as(admin2, "PUT", path, body(ms[1].ID, ms[2].ID)).Code; c != 200 {
		t.Errorf("別の管理者による並べ替え・削除: %d", c)
	}
	if c := e.as(admin2, "PUT", path, body(ms[0].ID)).Code; c != 422 {
		t.Errorf("外れた他人の画像は再指定できない: %d", c)
	}
	// ms[0]は外れたので配信されない。参照は2件
	if e.mediaStatus(as.other, ms[0].ID) != 404 || e.count(`SELECT count(*)::int FROM media_references WHERE owner_type='inventory_item'`) != 2 {
		t.Error("外した画像の参照・配信")
	}
	// 旧API（単一の写真）は画像を1枚にする。解除で全部外れる
	if c := e.as(as.admin, "PUT", "/inventory/items/"+item.ID+"/photo", fmt.Sprintf(`{"mediaId":%q}`, ms[0].ID)).Code; c != 204 {
		t.Fatalf("旧API: %d", c)
	}
	one.Images = nil
	decode(t, e.as(as.other, "GET", "/inventory/items/"+item.ID, ""), &one)
	if len(one.Images) != 1 || one.Images[0] != ms[0].ID {
		t.Errorf("旧APIの後: %+v", one.Images)
	}
	if c := e.as(as.admin, "PUT", path, body()).Code; c != 200 {
		t.Fatalf("全部外す: %d", c)
	}
	if e.count(`SELECT count(*)::int FROM inventory_item_images`) != 0 || e.count(`SELECT count(*)::int FROM inventory_items WHERE cover_media_id IS NOT NULL`) != 0 {
		t.Error("全部外した後にメインが残っている")
	}
}

// 一覧: 自分が借りている物（mine）・借りられる数の多い順（available）。borrowedByMe に自分の未返却数が入る。
func TestInventoryListMineAndSort(t *testing.T) {
	e := newEnv(t, opts{})
	e.addUser("admin.example", "active", goodPass, "admin")
	e.addUser("a.example", "active", goodPass)
	e.addUser("b.example", "active", goodPass)
	admin, a, b := e.signIn("admin.example"), e.signIn("a.example"), e.signIn("b.example")
	_, cable := e.newItem(admin, `{"name":"ケーブル","mode":"quantity","totalQuantity":10}`)
	_, tab := e.newItem(admin, `{"name":"ペンタブ","mode":"quantity","totalQuantity":3}`)
	_, cam := e.newItem(admin, `{"name":"カメラ","mode":"individual"}`)
	e.as(admin, "POST", "/inventory/items/"+cam.ID+"/units", `{"assetTag":"CAM-1"}`)
	if c, _ := e.loan(a, fmt.Sprintf(`{"itemId":%q,"quantity":4}`, cable.ID)); c != 201 {
		t.Fatalf("貸出: %d", c)
	}
	if c, _ := e.loan(b, fmt.Sprintf(`{"itemId":%q,"quantity":1}`, tab.ID)); c != 201 {
		t.Fatalf("貸出: %d", c)
	}
	var unitID string
	var camDetail struct{ Units []struct{ ID string } }
	decode(t, e.as(a, "GET", "/inventory/items/"+cam.ID, ""), &camDetail)
	unitID = camDetail.Units[0].ID
	if c, _ := e.loan(a, fmt.Sprintf(`{"itemId":%q,"unitId":%q}`, cam.ID, unitID)); c != 201 {
		t.Fatalf("個体の貸出: %d", c)
	}
	type row struct {
		ID           string
		BorrowedByMe int
	}
	var l struct{ Items []row }
	decode(t, e.as(a, "GET", "/inventory/items?mine=true", ""), &l)
	if len(l.Items) != 2 {
		t.Fatalf("Aの mine: %+v", l.Items)
	}
	got := map[string]int{}
	for _, it := range l.Items {
		got[it.ID] = it.BorrowedByMe
	}
	if got[cable.ID] != 4 || got[cam.ID] != 1 {
		t.Errorf("borrowedByMe: %+v", got)
	}
	decode(t, e.as(b, "GET", "/inventory/items?mine=true", ""), &l)
	if len(l.Items) != 1 || l.Items[0].ID != tab.ID || l.Items[0].BorrowedByMe != 1 {
		t.Errorf("Bの mine: %+v", l.Items)
	}
	// 詳細の borrowedByMe（数量・個体）
	var d struct{ BorrowedByMe int }
	decode(t, e.as(a, "GET", "/inventory/items/"+cable.ID, ""), &d)
	if d.BorrowedByMe != 4 {
		t.Errorf("数量の詳細: %d", d.BorrowedByMe)
	}
	decode(t, e.as(a, "GET", "/inventory/items/"+cam.ID, ""), &d)
	if d.BorrowedByMe != 1 {
		t.Errorf("個体の詳細: %d", d.BorrowedByMe)
	}
	decode(t, e.as(b, "GET", "/inventory/items/"+cam.ID, ""), &d)
	if d.BorrowedByMe != 0 {
		t.Errorf("他人の個体: %d", d.BorrowedByMe)
	}
	// 借りられる数の多い順: ケーブル6 > ペンタブ2 > カメラ0
	decode(t, e.as(a, "GET", "/inventory/items?sort=available", ""), &l)
	if len(l.Items) != 3 || l.Items[0].ID != cable.ID || l.Items[1].ID != tab.ID || l.Items[2].ID != cam.ID {
		t.Errorf("available順: %+v", l.Items)
	}
	if rec := e.as(a, "GET", "/inventory/items?sort=bogus", ""); rec.Code != 400 {
		t.Errorf("不正なsort: %d", rec.Code)
	}
	// ページング（offsetのカーソル）
	var p1 struct {
		Items      []row
		NextCursor *string
	}
	decode(t, e.as(a, "GET", "/inventory/items?limit=2", ""), &p1)
	if len(p1.Items) != 2 || p1.NextCursor == nil {
		t.Fatalf("1ページ目: %+v", p1)
	}
	var p2 struct {
		Items      []row
		NextCursor *string
	}
	decode(t, e.as(a, "GET", "/inventory/items?limit=2&cursor="+*p1.NextCursor, ""), &p2)
	if len(p2.Items) != 1 || p2.NextCursor != nil {
		t.Errorf("2ページ目: %+v", p2)
	}
}
