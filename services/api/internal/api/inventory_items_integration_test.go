//go:build integration

package api_test

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

type itemOut struct {
	ID, Name, Description, Category, Location, Mode string
	TotalQuantity                                   int
	Outstanding, UnitCount, Available               int
	Archived                                        bool
	Units                                           []struct {
		ID, AssetTag, State string
		SerialNumber        *string
		Loaned              *struct {
			BorrowerDisplayName string
			ElapsedDays         int
		}
	}
	Loans []struct {
		ID, BorrowerDisplayName string
		Quantity, ElapsedDays   int
	}
}

type itemsResp struct {
	Items      []itemOut
	NextCursor *string
	Categories []string
}

func (e *env) newItem(a actor, body string) (int, itemOut) {
	rec := e.as(a, "POST", "/inventory/items", body)
	var it itemOut
	_ = json.Unmarshal(rec.Body.Bytes(), &it)
	return rec.Code, it
}

func (e *env) item(a actor, id string) itemOut {
	e.t.Helper()
	rec := e.as(a, "GET", "/inventory/items/"+id, "")
	if rec.Code != 200 {
		e.t.Fatalf("物品詳細: %d %s", rec.Code, rec.Body)
	}
	var it itemOut
	decode(e.t, rec, &it)
	return it
}

func invSetup(t *testing.T) (*env, actor, actor) {
	e := newEnv(t, opts{})
	e.addUser("admin.example", "active", goodPass, "admin")
	e.addUser("member.example", "active", goodPass)
	return e, e.signIn("admin.example"), e.signIn("member.example")
}

func TestInventoryItemAuthorization(t *testing.T) {
	e, admin, member := invSetup(t)
	body := `{"name":"HDMIケーブル","mode":"quantity","totalQuantity":5}`
	if rec := e.do("POST", "/inventory/items", body); rec.Code != 401 {
		t.Errorf("匿名: %d", rec.Code)
	}
	for _, c := range []struct{ m, p, b string }{{"POST", "/inventory/items", body}, {"PATCH", "/inventory/items/00000000-0000-0000-0000-000000000000", `{"name":"x"}`}, {"POST", "/inventory/items/00000000-0000-0000-0000-000000000000/units", `{"assetTag":"A"}`}} {
		if rec := e.as(member, c.m, c.p, c.b); rec.Code != 403 {
			t.Errorf("Memberの %s %s = %d（403のはず）", c.m, c.p, rec.Code)
		}
	}
	code, it := e.newItem(admin, body)
	if code != 201 || it.Mode != "quantity" || it.TotalQuantity != 5 || it.Available != 5 {
		t.Fatalf("作成: %d %+v", code, it)
	}
	// Memberは閲覧できる
	if got := e.item(member, it.ID); got.Name != "HDMIケーブル" || got.Available != 5 {
		t.Fatalf("Memberの閲覧: %+v", got)
	}
	if rec := e.as(member, "GET", "/inventory/items", ""); rec.Code != 200 {
		t.Errorf("Memberの一覧: %d", rec.Code)
	}
}

func TestCreateItemValidation(t *testing.T) {
	e, admin, _ := invSetup(t)
	for name, body := range map[string]string{
		"名前なし": `{"mode":"quantity"}`, "名前101字": `{"name":"` + strings.Repeat("a", 101) + `","mode":"quantity"}`, "方式なし": `{"name":"x"}`, "方式不正": `{"name":"x","mode":"both"}`,
		"総数負": `{"name":"x","mode":"quantity","totalQuantity":-1}`, "総数過大": `{"name":"x","mode":"quantity","totalQuantity":100001}`,
		"個体管理に総数": `{"name":"x","mode":"individual","totalQuantity":3}`, "説明長すぎ": `{"name":"x","mode":"quantity","description":"` + strings.Repeat("あ", 1001) + `"}`,
		"未知の項目": `{"name":"x","mode":"quantity","foo":1}`,
	} {
		if c, _ := e.newItem(admin, body); c != 422 && c != 400 {
			t.Errorf("%s: %d", name, c)
		}
	}
	if n := e.count(`SELECT count(*) FROM inventory_items`); n != 0 {
		t.Fatalf("拒否した入力で物品が作られた: %d", n)
	}
	if c, it := e.newItem(admin, `{"name":"  Pico  ","mode":"individual","category":"VR","location":"部室A"}`); c != 201 || it.Name != "Pico" || it.TotalQuantity != 0 || it.Available != 0 {
		t.Fatalf("個体管理の作成: %d %+v", c, it)
	}
}

func TestAdjustTotalRequiresReasonAndRecordsHistory(t *testing.T) {
	e, admin, _ := invSetup(t)
	_, it := e.newItem(admin, `{"name":"HDMIケーブル","mode":"quantity","totalQuantity":5}`)
	// 理由なしの総数変更は拒否、同じ値なら理由不要
	if rec := e.as(admin, "PATCH", "/inventory/items/"+it.ID, `{"totalQuantity":8}`); rec.Code != 422 {
		t.Errorf("理由なしの総数変更: %d", rec.Code)
	}
	if rec := e.as(admin, "PATCH", "/inventory/items/"+it.ID, `{"totalQuantity":5,"name":"HDMI(長)"}`); rec.Code != 200 {
		t.Errorf("総数が同じなら理由不要: %d", rec.Code)
	}
	rec := e.as(admin, "PATCH", "/inventory/items/"+it.ID, `{"totalQuantity":8,"reason":"追加購入"}`)
	var got itemOut
	decode(t, rec, &got)
	if rec.Code != 200 || got.TotalQuantity != 8 || got.Available != 8 || got.Name != "HDMI(長)" {
		t.Fatalf("総数変更: %d %+v", rec.Code, got)
	}
	if n := e.count(`SELECT count(*) FROM inventory_adjustments WHERE item_id=$1 AND before_quantity=5 AND after_quantity=8 AND reason='追加購入' AND actor_user_id IS NOT NULL`, it.ID); n != 1 {
		t.Fatalf("調整履歴: %d", n)
	}
	// 調整履歴は追記専用
	if _, err := e.pool.Exec(ctxBG(), `UPDATE inventory_adjustments SET reason='改ざん'`); err == nil {
		t.Fatal("調整履歴を更新できてしまう")
	}
	for name, body := range map[string]string{"負": `{"totalQuantity":-1,"reason":"x"}`, "過大": `{"totalQuantity":100001,"reason":"x"}`, "名前を空に": `{"name":null}`, "未知": `{"mode2":1}`} {
		if rec := e.as(admin, "PATCH", "/inventory/items/"+it.ID, body); rec.Code != 422 && rec.Code != 400 {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	if rec := e.as(admin, "PATCH", "/inventory/items/00000000-0000-0000-0000-000000000000", `{"name":"x"}`); rec.Code != 404 {
		t.Errorf("存在しない: %d", rec.Code)
	}
}

func TestUnitsForIndividualItemsOnly(t *testing.T) {
	e, admin, _ := invSetup(t)
	_, pico := e.newItem(admin, `{"name":"Pico","mode":"individual"}`)
	_, cable := e.newItem(admin, `{"name":"ケーブル","mode":"quantity","totalQuantity":3}`)
	rec := e.as(admin, "POST", "/inventory/items/"+pico.ID+"/units", `{"assetTag":"PICO-01","serialNumber":"SN123"}`)
	if rec.Code != 201 {
		t.Fatalf("個体登録: %d %s", rec.Code, rec.Body)
	}
	if rec := e.as(admin, "POST", "/inventory/items/"+pico.ID+"/units", `{"assetTag":"PICO-01"}`); rec.Code != 409 {
		t.Errorf("管理番号の重複: %d", rec.Code)
	}
	// 別の物品なら同じ管理番号でよい
	_, pico2 := e.newItem(admin, `{"name":"Pico2","mode":"individual"}`)
	if rec := e.as(admin, "POST", "/inventory/items/"+pico2.ID+"/units", `{"assetTag":"PICO-01"}`); rec.Code != 201 {
		t.Errorf("別物品の同じ管理番号: %d", rec.Code)
	}
	if rec := e.as(admin, "POST", "/inventory/items/"+cable.ID+"/units", `{"assetTag":"X"}`); rec.Code != 409 {
		t.Errorf("数量管理の物品への個体登録: %d", rec.Code)
	}
	for _, bad := range []string{`{}`, `{"assetTag":"  "}`, `{"assetTag":"` + strings.Repeat("a", 51) + `"}`} {
		if rec := e.as(admin, "POST", "/inventory/items/"+pico.ID+"/units", bad); rec.Code != 422 {
			t.Errorf("不正な管理番号 %s: %d", bad, rec.Code)
		}
	}
	got := e.item(admin, pico.ID)
	if got.UnitCount != 1 || got.Available != 1 || len(got.Units) != 1 || got.Units[0].AssetTag != "PICO-01" || got.Units[0].Loaned != nil {
		t.Fatalf("個体の状況: %+v", got)
	}
}

func TestModeChangeAndArchiveRules(t *testing.T) {
	e, admin, _ := invSetup(t)
	_, it := e.newItem(admin, `{"name":"x","mode":"quantity","totalQuantity":2}`)
	// 履歴・個体が無ければ方式を変更できる（総数は0になる）
	rec := e.as(admin, "PATCH", "/inventory/items/"+it.ID, `{"mode":"individual"}`)
	var got itemOut
	decode(t, rec, &got)
	if rec.Code != 200 || got.Mode != "individual" || got.TotalQuantity != 0 {
		t.Fatalf("方式変更: %d %+v", rec.Code, got)
	}
	e.as(admin, "POST", "/inventory/items/"+it.ID+"/units", `{"assetTag":"A1"}`)
	// 個体があると方式は変更できない
	if rec := e.as(admin, "PATCH", "/inventory/items/"+it.ID, `{"mode":"quantity"}`); rec.Code != 409 {
		t.Errorf("個体がある物品の方式変更: %d", rec.Code)
	}
	// 数量管理でない物品に総数は指定できない
	if rec := e.as(admin, "PATCH", "/inventory/items/"+it.ID, `{"totalQuantity":3,"reason":"x"}`); rec.Code != 422 {
		t.Errorf("個体管理に総数: %d", rec.Code)
	}
	// 廃止すると一覧から消え、個体も登録できない。元に戻せる
	if rec := e.as(admin, "PATCH", "/inventory/items/"+it.ID, `{"archived":true}`); rec.Code != 200 {
		t.Fatalf("廃止: %d", rec.Code)
	}
	var lr itemsResp
	decode(t, e.as(admin, "GET", "/inventory/items", ""), &lr)
	if len(lr.Items) != 0 {
		t.Fatal("廃止した物品が一覧に出る")
	}
	if rec := e.as(admin, "POST", "/inventory/items/"+it.ID+"/units", `{"assetTag":"A2"}`); rec.Code != 409 {
		t.Errorf("廃止した物品への個体登録: %d", rec.Code)
	}
	if rec := e.as(admin, "PATCH", "/inventory/items/"+it.ID, `{"archived":false}`); rec.Code != 200 {
		t.Errorf("廃止の取り消し: %d", rec.Code)
	}
}

func TestListSearchCategoryAvailabilityAndPaging(t *testing.T) {
	e, admin, member := invSetup(t)
	for i := 0; i < 5; i++ {
		e.newItem(admin, fmt.Sprintf(`{"name":"ケーブル%02d","mode":"quantity","totalQuantity":%d,"category":"ケーブル","location":"部室A"}`, i, i))
	}
	e.newItem(admin, `{"name":"Pico","mode":"individual","category":"VR","location":"部室B"}`)
	get := func(q string) itemsResp {
		rec := e.as(member, "GET", "/inventory/items"+q, "")
		if rec.Code != 200 {
			t.Fatalf("%s: %d", q, rec.Code)
		}
		var lr itemsResp
		decode(t, rec, &lr)
		return lr
	}
	if lr := get(""); len(lr.Items) != 6 || len(lr.Categories) != 2 {
		t.Fatalf("全件: %d カテゴリ=%v", len(lr.Items), lr.Categories)
	}
	if lr := get("?q=" + url.QueryEscape("部室B")); len(lr.Items) != 1 || lr.Items[0].Name != "Pico" {
		t.Fatalf("保管場所で検索: %+v", lr.Items)
	}
	if lr := get("?category=" + url.QueryEscape("VR")); len(lr.Items) != 1 {
		t.Fatalf("カテゴリ絞り込み: %d", len(lr.Items))
	}
	if lr := get("?q=" + url.QueryEscape("%")); len(lr.Items) != 0 {
		t.Fatal("%がワイルドカードとして働いた")
	}
	// 利用可能なものだけ: 総数0のケーブル00と、個体のないPicoは借りられない
	if lr := get("?available=true"); len(lr.Items) != 4 {
		t.Fatalf("利用可能のみ: %d（総数が1以上の4件のはず）", len(lr.Items))
	}
	seen := map[string]bool{}
	cursor := ""
	for pages := 0; pages < 5; pages++ {
		q := "?limit=4"
		if cursor != "" {
			q += "&cursor=" + url.QueryEscape(cursor)
		}
		lr := get(q)
		for _, it := range lr.Items {
			if seen[it.ID] {
				t.Fatal("重複")
			}
			seen[it.ID] = true
		}
		if lr.NextCursor == nil {
			break
		}
		cursor = *lr.NextCursor
	}
	if len(seen) != 6 {
		t.Fatalf("ページングで全件: %d", len(seen))
	}
	for _, bad := range []string{"?limit=0", "?cursor=!!!", "?q=" + strings.Repeat("a", 101)} {
		if rec := e.as(member, "GET", "/inventory/items"+bad, ""); rec.Code != 400 {
			t.Errorf("%s = %d", bad, rec.Code)
		}
	}
	if rec := e.as(member, "GET", "/inventory/items/not-a-uuid", ""); rec.Code != 404 {
		t.Errorf("不正なID: %d", rec.Code)
	}
}
