//go:build integration

package api_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type corrOut struct {
	ID, TargetType, TargetID, Kind, Reason string
	DeltaQuantity                          *int
	Replayed                               bool
	ItemOutstanding, ItemAvailable         int
}

var keySeq int

func newKey() string { keySeq++; return fmt.Sprintf("test-key-%06d", keySeq) }

func (e *env) correct(a actor, key, body string) (int, corrOut, string) {
	e.t.Helper()
	opts := []reqOpt{}
	if key != "" {
		opts = append(opts, withHeader("Idempotency-Key", key))
	}
	rec := e.as(a, "POST", "/admin/inventory/corrections", body, opts...)
	var c corrOut
	_ = json.Unmarshal(rec.Body.Bytes(), &c)
	return rec.Code, c, rec.Body.String()
}

func adjustBody(typ, id string, delta int, reason string) string {
	return fmt.Sprintf(`{"targetType":%q,"targetId":%q,"kind":"adjust","deltaQuantity":%d,"reason":%q}`, typ, id, delta, reason)
}
func voidBody(typ, id, reason string) string {
	return fmt.Sprintf(`{"targetType":%q,"targetId":%q,"kind":"void","reason":%q}`, typ, id, reason)
}

func corrSetup(t *testing.T) (*env, actor, []actor) {
	e, admin, _ := invSetup(t)
	return e, admin, borrowers(t, e, 3)
}

func TestCorrectionAuthorizationAndValidation(t *testing.T) {
	e, admin, m := corrSetup(t)
	it := quantityItem(t, e, admin, 5)
	_, l := e.loan(m[0], fmt.Sprintf(`{"itemId":%q,"quantity":2}`, it.ID))
	body := adjustBody("loan", l.ID, 1, "数え間違い")
	if rec := e.do("POST", "/admin/inventory/corrections", body, withHeader("Idempotency-Key", newKey())); rec.Code != 401 {
		t.Errorf("匿名: %d", rec.Code)
	}
	if c, _, _ := e.correct(m[0], newKey(), body); c != 403 {
		t.Errorf("Member（借りた本人でも）: %d（403のはず）", c)
	}
	e.addUser("owner.example", "active", goodPass, "owner")
	if c, _, _ := e.correct(e.signIn("owner.example"), newKey(), body); c != 201 {
		t.Errorf("Owner: %d", c)
	}
	// Idempotency-Keyが必須・形式
	for name, k := range map[string]string{"なし": "", "短い": "abc", "不正な文字": "key with space!!", "長い": strings.Repeat("a", 101)} {
		if c, _, _ := e.correct(admin, k, adjustBody("loan", l.ID, 1, "x")); c != 400 {
			t.Errorf("キー%s: %d（400のはず）", name, c)
		}
	}
	for name, b := range map[string]string{
		"理由なし": adjustBody("loan", l.ID, 1, ""), "理由空白": adjustBody("loan", l.ID, 1, "  "), "理由201字": adjustBody("loan", l.ID, 1, strings.Repeat("あ", 201)),
		"0の増減": adjustBody("loan", l.ID, 0, "x"), "対象不正": adjustBody("invoice", l.ID, 1, "x"), "ID不正": adjustBody("loan", "nope", 1, "x"),
		"kind不正": `{"targetType":"loan","targetId":"` + l.ID + `","kind":"delete","reason":"x"}`, "voidに数量": `{"targetType":"loan","targetId":"` + l.ID + `","kind":"void","deltaQuantity":1,"reason":"x"}`,
		"adjustに数量なし": `{"targetType":"loan","targetId":"` + l.ID + `","kind":"adjust","reason":"x"}`, "未知の項目": `{"targetType":"loan","targetId":"` + l.ID + `","kind":"void","reason":"x","extra":1}`,
	} {
		if c, _, _ := e.correct(admin, newKey(), b); c != 422 && c != 400 {
			t.Errorf("%s: %d", name, c)
		}
	}
	if c, _, _ := e.correct(admin, newKey(), adjustBody("loan", "00000000-0000-0000-0000-000000000000", 1, "x")); c != 404 {
		t.Errorf("存在しない貸出: %d", c)
	}
	if c, _, _ := e.correct(admin, newKey(), adjustBody("return", "00000000-0000-0000-0000-000000000000", 1, "x")); c != 404 {
		t.Errorf("存在しない返却: %d", c)
	}
	// 拒否した試行は何も残さない（成功した1件＝Ownerの訂正だけ）
	if n := e.count(`SELECT count(*) FROM inventory_correction_events`); n != 1 {
		t.Fatalf("訂正イベントが%d件（拒否した試行が残った）", n)
	}
}

// 貸出数量の訂正は元記録を変えずに、残数・貸出中の一覧・履歴へ反映される。
func TestAdjustLoanQuantityKeepsOriginalAndUpdatesEffective(t *testing.T) {
	e, admin, m := corrSetup(t)
	it := quantityItem(t, e, admin, 5)
	_, l := e.loan(m[0], fmt.Sprintf(`{"itemId":%q,"quantity":2}`, it.ID))
	*e.clock = e.clock.Add(time.Hour)
	c, ev, body := e.correct(admin, newKey(), adjustBody("loan", l.ID, 1, "3本借りていたため"))
	if c != 201 || ev.Kind != "adjust" || ev.DeltaQuantity == nil || *ev.DeltaQuantity != 1 || ev.ItemOutstanding != 3 || ev.ItemAvailable != 2 || ev.Replayed {
		t.Fatalf("訂正: %d %s", c, body)
	}
	if got := e.item(m[1], it.ID); got.Outstanding != 3 || got.Available != 2 || got.Loans[0].Quantity != 3 {
		t.Fatalf("残数・貸出中が有効数量になっていない: %+v", got)
	}
	// 元記録は不変（数量・借りた人・日時）。訂正者は別に記録される
	if n := e.count(`SELECT count(*) FROM inventory_loans WHERE id=$1 AND quantity=2 AND borrower_user_id IS NOT NULL`, l.ID); n != 1 {
		t.Fatal("元の貸出が変更された")
	}
	if n := e.count(`SELECT count(*) FROM inventory_correction_events WHERE target_id=$1 AND actor_user_id <> (SELECT borrower_user_id FROM inventory_loans WHERE id=$1) AND reason='3本借りていたため'`, l.ID); n != 1 {
		t.Fatal("訂正者・理由が記録されていない")
	}
	// 履歴: 元の貸出は元の数量のまま、訂正イベントが別に出る（訂正者=管理者、借りた人=A）
	var hl struct {
		Items []struct {
			Kind, ItemName, BorrowerDisplayName, ActorDisplayName, CorrectionKind, TargetType, Reason string
			Quantity                                                                                  int
		}
	}
	decode(t, e.as(admin, "GET", "/inventory/history?itemId="+it.ID, ""), &hl)
	if len(hl.Items) != 2 || hl.Items[0].Kind != "correction" || hl.Items[0].CorrectionKind != "adjust" || hl.Items[0].Quantity != 1 || hl.Items[0].BorrowerDisplayName != "借り手A" || hl.Items[0].Reason != "3本借りていたため" ||
		hl.Items[1].Kind != "loan" || hl.Items[1].Quantity != 2 {
		t.Fatalf("履歴(管理者): %+v", hl.Items)
	}
	// 理由（自由記述）は管理者以外には返さない
	hl.Items = nil // 前の応答の値が残って検査をすり抜けないようにする
	decode(t, e.as(m[1], "GET", "/inventory/history?itemId="+it.ID, ""), &hl)
	if hl.Items[0].Kind != "correction" || hl.Items[0].Reason != "" {
		t.Fatalf("Memberに訂正の理由が見える: %+v", hl.Items[0])
	}
	// 減らす訂正
	if c, ev, _ := e.correct(admin, newKey(), adjustBody("loan", l.ID, -2, "1本だった")); c != 201 || ev.ItemOutstanding != 1 {
		t.Fatalf("減らす訂正: %d %+v", c, ev)
	}
}

func TestCorrectionsThatBreakStockAreRejectedAndLeaveNothing(t *testing.T) {
	e, admin, m := corrSetup(t)
	it := quantityItem(t, e, admin, 5)
	_, l := e.loan(m[0], fmt.Sprintf(`{"itemId":%q,"quantity":3}`, it.ID))
	e.ret(m[1], l.ID, `{"quantity":2}`)
	var rets struct{ Items []struct{ ID, Kind string } }
	decode(t, e.as(admin, "GET", "/inventory/history?itemId="+it.ID, ""), &rets)
	retID := ""
	for _, ev := range rets.Items {
		if ev.Kind == "return" {
			retID = ev.ID
		}
	}
	for name, b := range map[string]string{
		"在庫が負になる(貸出を+5)":   adjustBody("loan", l.ID, 5, "x"), // 有効8−返却2=6 > 総数5
		"数量が0以下(貸出を-3)":    adjustBody("loan", l.ID, -3, "x"),
		"返却が貸出を超える(貸出を-2)": adjustBody("loan", l.ID, -2, "x"),   // 有効1 < 返却2
		"返却を超過(+2)":        adjustBody("return", retID, 2, "x"), // 返却4 > 貸出3
		"返却が0以下(-2)":       adjustBody("return", retID, -2, "x"),
		"貸出の取消(返却が残る)":     voidBody("loan", l.ID, "x"), // 返却が貸出を超える
	} {
		if c, _, body := e.correct(admin, newKey(), b); c != 409 {
			t.Errorf("%s: %d（409のはず）%s", name, c, body)
		}
	}
	if n := e.count(`SELECT count(*) FROM inventory_correction_events`); n != 0 {
		t.Fatalf("拒否した訂正がイベントを残した: %d", n)
	}
	if got := e.item(admin, it.ID); got.Outstanding != 1 || got.Available != 4 {
		t.Fatalf("拒否した訂正で数量が変わった: %+v", got)
	}
}

func TestVoidAndReturnCorrectionsRestoreEffectiveQuantities(t *testing.T) {
	e, admin, m := corrSetup(t)
	it := quantityItem(t, e, admin, 5)
	_, l := e.loan(m[0], fmt.Sprintf(`{"itemId":%q,"quantity":3}`, it.ID))
	e.ret(m[1], l.ID, `{"quantity":1}`)
	var h struct{ Items []struct{ ID, Kind string } }
	decode(t, e.as(admin, "GET", "/inventory/history?itemId="+it.ID, ""), &h)
	retID := ""
	for _, ev := range h.Items {
		if ev.Kind == "return" {
			retID = ev.ID
		}
	}
	// 返却の数量訂正(+1 → 2) → 未返却1
	if c, ev, _ := e.correct(admin, newKey(), adjustBody("return", retID, 1, "2本返却していた")); c != 201 || ev.ItemOutstanding != 1 {
		t.Fatalf("返却の訂正: %d %+v", c, ev)
	}
	// 返却の取消 → 未返却3に戻る
	if c, ev, _ := e.correct(admin, newKey(), voidBody("return", retID, "誤登録")); c != 201 || ev.ItemOutstanding != 3 {
		t.Fatalf("返却の取消: %d %+v", c, ev)
	}
	if c, _, _ := e.correct(admin, newKey(), voidBody("return", retID, "二重")); c != 409 {
		t.Errorf("二重の取消: %d", c)
	}
	if c, _, _ := e.correct(admin, newKey(), adjustBody("return", retID, 1, "x")); c != 409 {
		t.Errorf("取消後の数量訂正: %d", c)
	}
	// 貸出の取消 → 在庫は全部戻り、貸出中の一覧から消える。元の貸出行は残る
	if c, ev, _ := e.correct(admin, newKey(), voidBody("loan", l.ID, "誤登録")); c != 201 || ev.ItemOutstanding != 0 || ev.ItemAvailable != 5 {
		t.Fatalf("貸出の取消: %d %+v", c, ev)
	}
	var ll struct{ Items []outLoan }
	decode(t, e.as(admin, "GET", "/inventory/loans", ""), &ll)
	if len(ll.Items) != 0 {
		t.Fatal("取り消した貸出が貸出中の一覧に残る")
	}
	if n := e.count(`SELECT count(*) FROM inventory_loans WHERE id=$1`, l.ID); n != 1 {
		t.Fatal("元の貸出行が失われた")
	}
	// 取り消した後は借りる数が戻っている（全5本借りられる）
	if c, _ := e.loan(m[2], fmt.Sprintf(`{"itemId":%q,"quantity":5}`, it.ID)); c != 201 {
		t.Fatalf("取消後に5本借りられない: %d", c)
	}
}

func TestIdempotencyReplayAndKeyReuse(t *testing.T) {
	e, admin, m := corrSetup(t)
	it := quantityItem(t, e, admin, 5)
	_, l := e.loan(m[0], fmt.Sprintf(`{"itemId":%q,"quantity":2}`, it.ID))
	key := newKey()
	b := adjustBody("loan", l.ID, 1, "数え間違い")
	c1, ev1, _ := e.correct(admin, key, b)
	c2, ev2, _ := e.correct(admin, key, b)
	if c1 != 201 || c2 != 200 || ev1.ID != ev2.ID || !ev2.Replayed || ev1.Replayed {
		t.Fatalf("再送: %d/%d %+v %+v", c1, c2, ev1, ev2)
	}
	if n := e.count(`SELECT count(*) FROM inventory_correction_events`); n != 1 {
		t.Fatalf("再送でイベントが%d件", n)
	}
	if got := e.item(admin, it.ID); got.Outstanding != 3 {
		t.Fatalf("再送で二重に適用された: %+v", got)
	}
	// 同じキーで内容が違うものは拒否
	if c, _, _ := e.correct(admin, key, adjustBody("loan", l.ID, 1, "別の理由")); c != 422 {
		t.Errorf("同じキーで別内容: %d（422のはず）", c)
	}
	if c, _, _ := e.correct(admin, key, adjustBody("loan", l.ID, 2, "数え間違い")); c != 422 {
		t.Errorf("同じキーで別の数量: %d", c)
	}
	if got := e.item(admin, it.ID); got.Outstanding != 3 {
		t.Fatal("拒否した再送で数量が変わった")
	}
}

// 同じ訂正を同時に何度送っても、反映されるのは1回だけ。
func TestConcurrentSameKeyAppliesOnce(t *testing.T) {
	e, admin, m := corrSetup(t)
	e.invHook = barrierHook(8)
	e.h = e.build()
	it := quantityItem(t, e, admin, 5)
	_, l := e.loan(m[0], fmt.Sprintf(`{"itemId":%q,"quantity":1}`, it.ID))
	key := newKey()
	b := adjustBody("loan", l.ID, 1, "数え間違い")
	var wg sync.WaitGroup
	start := make(chan struct{})
	codes := make([]int, 8)
	for i := range codes {
		wg.Add(1)
		go func(i int) { defer wg.Done(); <-start; codes[i], _, _ = e.correct(admin, key, b) }(i)
	}
	close(start)
	wg.Wait()
	created, replay := 0, 0
	for _, c := range codes {
		switch c {
		case 201:
			created++
		case 200:
			replay++
		default:
			t.Fatalf("想定外: %v", codes)
		}
	}
	if created != 1 || replay != 7 {
		t.Fatalf("201が%d件・200が%d件（1件・7件のはず）: %v", created, replay, codes)
	}
	if n := e.count(`SELECT count(*) FROM inventory_correction_events`); n != 1 {
		t.Fatalf("イベントが%d件", n)
	}
	if got := e.item(admin, it.ID); got.Outstanding != 2 {
		t.Fatalf("二重に適用された: outstanding=%d（2のはず）", got.Outstanding)
	}
}

func TestIndividualUnitCorrections(t *testing.T) {
	e, admin, m := corrSetup(t)
	_, pico := e.newItem(admin, `{"name":"Pico","mode":"individual"}`)
	var u struct{ ID string }
	decode(t, e.as(admin, "POST", "/inventory/items/"+pico.ID+"/units", `{"assetTag":"PICO-01"}`), &u)
	_, l := e.loan(m[0], fmt.Sprintf(`{"itemId":%q,"unitId":%q}`, pico.ID, u.ID))
	// 個体の貸出は数量を訂正できない
	if c, _, _ := e.correct(admin, newKey(), adjustBody("loan", l.ID, 1, "x")); c != 422 {
		t.Errorf("個体の数量訂正: %d（422のはず）", c)
	}
	// 貸出を取り消すと個体が空き、別の人が借りられる
	if c, ev, _ := e.correct(admin, newKey(), voidBody("loan", l.ID, "誤登録")); c != 201 || ev.ItemAvailable != 1 {
		t.Fatalf("個体の貸出の取消: %d %+v", c, ev)
	}
	_, l2 := e.loan(m[1], fmt.Sprintf(`{"itemId":%q,"unitId":%q}`, pico.ID, u.ID))
	if l2.ID == "" {
		t.Fatal("取消後に個体を借りられない")
	}
	// 返却して、その返却を取り消すと再び貸出中に戻る
	e.ret(m[2], l2.ID, ``)
	var h struct{ Items []struct{ ID, Kind string } }
	decode(t, e.as(admin, "GET", "/inventory/history?itemId="+pico.ID, ""), &h)
	retID := ""
	for _, ev := range h.Items {
		if ev.Kind == "return" {
			retID = ev.ID
		}
	}
	if c, ev, _ := e.correct(admin, newKey(), voidBody("return", retID, "誤返却")); c != 201 || ev.ItemAvailable != 0 {
		t.Fatalf("個体の返却の取消: %d %+v", c, ev)
	}
	if got := e.item(admin, pico.ID); got.Units[0].Loaned == nil {
		t.Fatal("返却の取消後も個体が貸出中に戻らない")
	}
	// 返却を取り消したいが、その個体が別の貸出で使われている場合は拒否される（二重貸出の防止）
	var u2 struct{ ID string }
	decode(t, e.as(admin, "POST", "/inventory/items/"+pico.ID+"/units", `{"assetTag":"PICO-02"}`), &u2)
	_, l4 := e.loan(m[0], fmt.Sprintf(`{"itemId":%q,"unitId":%q}`, pico.ID, u2.ID))
	*e.clock = e.clock.Add(time.Minute)
	e.ret(m[1], l4.ID, ``)
	*e.clock = e.clock.Add(time.Minute)
	if c, l5 := e.loan(m[2], fmt.Sprintf(`{"itemId":%q,"unitId":%q}`, pico.ID, u2.ID)); c != 201 || l5.ID == "" {
		t.Fatalf("返却後の再貸出: %d", c)
	}
	var h2 struct {
		Items []struct{ ID, Kind, AssetTag string }
	}
	decode(t, e.as(admin, "GET", "/inventory/history?itemId="+pico.ID, ""), &h2)
	ret2 := ""
	for _, ev := range h2.Items {
		if ev.Kind == "return" && ev.AssetTag == "PICO-02" {
			ret2 = ev.ID
		}
	}
	before := e.count(`SELECT count(*) FROM inventory_correction_events`)
	if c, _, body := e.correct(admin, newKey(), voidBody("return", ret2, "誤返却")); c != 409 || !strings.Contains(body, "UNIT_UNAVAILABLE") {
		t.Fatalf("個体が別の貸出で使われているのに返却を取り消せた: %d %s", c, body)
	}
	if e.count(`SELECT count(*) FROM inventory_correction_events`) != before {
		t.Fatal("拒否した訂正がイベントを残した")
	}
}

// 訂正と通常の貸出の同時実行でも、貸出中が総数を超えない。
func TestCorrectionVersusLoanKeepsStockInvariant(t *testing.T) {
	for round := 0; round < 6; round++ {
		e, admin, m := corrSetup(t)
		e.invHook = barrierHook(2)
		e.h = e.build()
		it := quantityItem(t, e, admin, 5)
		_, l := e.loan(m[0], fmt.Sprintf(`{"itemId":%q,"quantity":3}`, it.ID))
		var wg sync.WaitGroup
		start := make(chan struct{})
		var corrCode, loanCode int
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			corrCode, _, _ = e.correct(admin, newKey(), adjustBody("loan", l.ID, 2, "5本だった"))
		}()
		go func() {
			defer wg.Done()
			<-start
			loanCode, _ = e.loan(m[1], fmt.Sprintf(`{"itemId":%q,"quantity":1}`, it.ID))
		}()
		close(start)
		wg.Wait()
		out := e.outstandingOf(it.ID)
		if out > 5 || out < 0 {
			t.Fatalf("round %d: 貸出中(%d)が総数(5)を超えた/負 corr=%d loan=%d", round, out, corrCode, loanCode)
		}
		if !((corrCode == 201 && loanCode == 409) || (corrCode == 409 && loanCode == 201)) {
			t.Fatalf("round %d: 一貫しない結果 corr=%d loan=%d", round, corrCode, loanCode)
		}
	}
}

func TestCorrectionEventsAreAppendOnly(t *testing.T) {
	e, admin, m := corrSetup(t)
	it := quantityItem(t, e, admin, 5)
	_, l := e.loan(m[0], fmt.Sprintf(`{"itemId":%q,"quantity":1}`, it.ID))
	e.correct(admin, newKey(), adjustBody("loan", l.ID, 1, "x"))
	for _, sql := range []string{`UPDATE inventory_correction_events SET reason='改ざん'`, `DELETE FROM inventory_correction_events`} {
		if _, err := e.pool.Exec(ctxBG(), sql); err == nil {
			t.Fatalf("訂正イベントを変更・削除できてしまう: %s", sql)
		}
	}
}

var _ = httptest.NewRecorder
