//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func (e *env) str(sql string, args ...any) string {
	e.t.Helper()
	var v string
	if err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&v); err != nil {
		e.t.Fatal(err)
	}
	return v
}

// 返却場所: 記録され、履歴・通知に出て、物品の保管場所に反映される。空なら保管場所は変わらない。入力は検査される。
func TestReturnLocation(t *testing.T) {
	e, admin, _ := invSetup(t)
	bs := borrowers(t, e, 2)
	_, it := e.newItem(admin, `{"name":"三脚","mode":"quantity","totalQuantity":3,"location":"部室の棚"}`)
	code, l1 := e.loan(bs[0], fmt.Sprintf(`{"itemId":%q,"quantity":2}`, it.ID))
	if code != 201 {
		t.Fatalf("貸出: %d", code)
	}
	// 場所なしの返却: 保管場所は変わらない
	if c := e.ret(bs[0], l1.ID, `{"quantity":1}`); c != 200 {
		t.Fatalf("返却: %d", c)
	}
	if got := e.item(admin, it.ID).Location; got != "部室の棚" {
		t.Errorf("場所なしで保管場所が変わった: %q", got)
	}
	// 不正な場所は拒否（返却は記録されない）
	before := e.count(`SELECT count(*)::int FROM inventory_returns`)
	for name, body := range map[string]string{"長すぎる": fmt.Sprintf(`{"quantity":1,"location":%q}`, strings.Repeat("あ", 51)), "改行": `{"quantity":1,"location":"a\nb"}`} {
		if c := e.ret(bs[0], l1.ID, body); c != 422 {
			t.Errorf("%s: %d", name, c)
		}
	}
	if n := e.count(`SELECT count(*)::int FROM inventory_returns`); n != before {
		t.Errorf("拒否した返却が記録された")
	}
	// 場所つきの返却（代理返却でもよい）。保管場所が更新され、記録・履歴・通知に残る
	if c := e.ret(bs[1], l1.ID, `{"quantity":1,"location":"  倉庫A  "}`); c != 200 {
		t.Fatalf("場所つきの返却: %d", c)
	}
	if got := e.item(admin, it.ID).Location; got != "倉庫A" {
		t.Errorf("保管場所が更新されていない: %q", got)
	}
	if got := e.str(`SELECT location FROM inventory_returns WHERE location <> ''`); got != "倉庫A" {
		t.Errorf("返却の記録に場所が無い: %q", got)
	}
	rec := e.as(admin, "GET", "/inventory/history?itemId="+it.ID, "")
	var h struct {
		Items []struct{ Kind, Location string }
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &h)
	found := false
	for _, ev := range h.Items {
		if ev.Kind == "return" && ev.Location == "倉庫A" {
			found = true
		}
	}
	if !found {
		t.Errorf("履歴に返却場所が出ない: %s", rec.Body)
	}
	ps := e.outboxPayloads("inventory.loan_returned")
	if len(ps) != 2 || strings.Contains(ps[0], "location") || !strings.Contains(ps[1], `"location":"倉庫A"`) {
		t.Errorf("通知の本文: %v", ps)
	}
}

// 「貸出中」の絞り込み: 一部でも貸出中なら含まれる。貸出可能は借りられる数が1以上。解除すると全件。個体の詳細は loanId を返す。
func TestInventoryLoanedFilterAndUnitLoanID(t *testing.T) {
	e, admin, _ := invSetup(t)
	bs := borrowers(t, e, 1)
	_, free := e.newItem(admin, `{"name":"未貸出","mode":"quantity","totalQuantity":2}`)
	_, part := e.newItem(admin, `{"name":"一部","mode":"quantity","totalQuantity":2}`)
	_, full := e.newItem(admin, `{"name":"全部","mode":"quantity","totalQuantity":1}`)
	e.loan(bs[0], fmt.Sprintf(`{"itemId":%q,"quantity":1}`, part.ID))
	e.loan(bs[0], fmt.Sprintf(`{"itemId":%q,"quantity":1}`, full.ID))
	names := func(q string) string {
		rec := e.as(admin, "GET", "/inventory/items?"+q, "")
		var r struct{ Items []struct{ Name string } }
		_ = json.Unmarshal(rec.Body.Bytes(), &r)
		var out []string
		for _, i := range r.Items {
			out = append(out, i.Name)
		}
		return strings.Join(out, ",")
	}
	if got := names("loaned=true"); got != "一部,全部" && got != "全部,一部" {
		t.Errorf("貸出中: %s", got)
	}
	if got := names("available=true"); !strings.Contains(got, "未貸出") || !strings.Contains(got, "一部") || strings.Contains(got, "全部") {
		t.Errorf("貸出可能: %s", got)
	}
	if got := names(""); !strings.Contains(got, "未貸出") || !strings.Contains(got, "全部") {
		t.Errorf("解除で全件: %s", got)
	}
	_ = free
	// 個体管理: 貸出中の個体の詳細に loanId が出る（詳細画面から返却できる）
	_, ind := e.newItem(admin, `{"name":"カメラ","mode":"individual"}`)
	if rec := e.as(admin, "POST", "/inventory/items/"+ind.ID+"/units", `{"assetTag":"CAM-1"}`); rec.Code != 201 {
		t.Fatalf("個体: %d %s", rec.Code, rec.Body)
	}
	d := e.item(admin, ind.ID)
	var unit string
	_ = unit
	rec := e.as(admin, "GET", "/inventory/items/"+ind.ID, "")
	var det struct{ Units []struct{ ID, LoanID string } }
	_ = json.Unmarshal(rec.Body.Bytes(), &det)
	if len(det.Units) != 1 || det.Units[0].LoanID != "" {
		t.Fatalf("貸出前: %s", rec.Body)
	}
	_ = d
	code, ln := e.loan(bs[0], fmt.Sprintf(`{"itemId":%q,"unitId":%q}`, ind.ID, det.Units[0].ID))
	if code != 201 {
		t.Fatalf("個体の貸出: %d", code)
	}
	rec = e.as(admin, "GET", "/inventory/items/"+ind.ID, "")
	_ = json.Unmarshal(rec.Body.Bytes(), &det)
	if det.Units[0].LoanID != ln.ID {
		t.Errorf("loanId: %q want %q", det.Units[0].LoanID, ln.ID)
	}
}
