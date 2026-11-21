//go:build integration

package api_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type loanOut struct {
	ID, ItemID, BorrowedAt string
	UnitID                 *string
	Quantity, Outstanding  int
	Remaining              int
}

type outLoan struct {
	ID, ItemName, AssetTag, BorrowerDisplayName string
	Quantity, ElapsedDays                       int
	Mine                                        bool
}

type histEvent struct {
	Kind, ItemName, BorrowerDisplayName, ActorDisplayName string
	Quantity                                              int
}

// 借り手を複数用意する。表示名にログインIDを含めない（応答にログインIDが出ないことを検査できるように）。
func borrowers(t *testing.T, e *env, n int) []actor {
	t.Helper()
	var as []actor
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("user%02d.example", i)
		e.addUser(id, "active", goodPass)
		a := e.signIn(id)
		if rec := e.patchProfile(a, fmt.Sprintf(`{"displayName":"借り手%c"}`, 'A'+i)); rec.Code != 200 {
			t.Fatalf("表示名: %d", rec.Code)
		}
		as = append(as, a)
	}
	return as
}

func (e *env) loan(a actor, body string) (int, loanOut) {
	rec := e.as(a, "POST", "/inventory/loans", body)
	var l loanOut
	_ = json.Unmarshal(rec.Body.Bytes(), &l)
	return rec.Code, l
}

func (e *env) ret(a actor, loanID, body string) int {
	return e.as(a, "POST", "/inventory/loans/"+loanID+"/returns", body).Code
}

func (e *env) outstandingOf(itemID string) int {
	return e.count(`SELECT (COALESCE((SELECT SUM(quantity) FROM inventory_loans WHERE item_id=$1),0) - COALESCE((SELECT SUM(r.quantity) FROM inventory_returns r JOIN inventory_loans l ON l.id=r.loan_id WHERE l.item_id=$1),0))::int`, itemID)
}

func quantityItem(t *testing.T, e *env, admin actor, total int) itemOut {
	c, it := e.newItem(admin, fmt.Sprintf(`{"name":"HDMIケーブル","mode":"quantity","totalQuantity":%d}`, total))
	if c != 201 {
		t.Fatalf("物品作成: %d", c)
	}
	return it
}

// barrierHook は、n個のリクエストが判定の直前に揃うまで全員を待たせる（揃ったら一斉に進む）。
// ロックが無ければ全員が同じ在庫を見て判定するため、必ず競合する（待ち時間の固定では、先に着いた要求が先に終わって競合を見逃す）。
func barrierHook(n int) func() {
	var mu sync.Mutex
	arrived := 0
	all := make(chan struct{})
	return func() {
		mu.Lock()
		arrived++
		if arrived == n {
			close(all)
		}
		mu.Unlock()
		select {
		case <-all:
		case <-time.After(500 * time.Millisecond): // ロックで直列化されている実装では、後続は前の要求の終了まで来られない（その場合は待たずに進む）
		}
	}
}

// シナリオ1: 5本をAが2本・Bが3本借りると残0になり、それ以上は借りられない。
func TestFiveCablesTwoAndThree(t *testing.T) {
	e, admin, _ := invSetup(t)
	m := borrowers(t, e, 3)
	it := quantityItem(t, e, admin, 5)
	c, l := e.loan(m[0], fmt.Sprintf(`{"itemId":%q,"quantity":2}`, it.ID))
	if c != 201 || l.Remaining != 3 || l.Quantity != 2 {
		t.Fatalf("A: %d %+v", c, l)
	}
	if c, l := e.loan(m[1], fmt.Sprintf(`{"itemId":%q,"quantity":3}`, it.ID)); c != 201 || l.Remaining != 0 {
		t.Fatalf("B: %d %+v", c, l)
	}
	if c, _ := e.loan(m[2], fmt.Sprintf(`{"itemId":%q,"quantity":1}`, it.ID)); c != 409 {
		t.Fatalf("残0で借りられた: %d", c)
	}
	if got := e.item(m[2], it.ID); got.Available != 0 || got.Outstanding != 5 || len(got.Loans) != 2 {
		t.Fatalf("詳細: %+v", got)
	}
	// 借り手は常にセッションのユーザー: 他人の名前では借りられない（リクエストに借り手を指定する項目が無い）
	if rec := e.as(m[2], "POST", "/inventory/loans", fmt.Sprintf(`{"itemId":%q,"quantity":1,"borrowerUserId":"%s"}`, it.ID, e.userID("user00.example").String())); rec.Code != 400 && rec.Code != 422 {
		t.Errorf("借り手の指定が受け付けられた: %d", rec.Code)
	}
}

// シナリオ2: 残1に10人が同時に貸出要求しても、成功は1件だけ。履歴が矛盾しない。
func TestConcurrentLoansOfTheLastItemSucceedOnce(t *testing.T) {
	e, admin, _ := invSetup(t)
	e.invHook = barrierHook(10)
	e.h = e.build()
	m := borrowers(t, e, 10)
	it := quantityItem(t, e, admin, 1)
	var wg sync.WaitGroup
	start := make(chan struct{})
	codes := make([]int, len(m))
	for i := range m {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			codes[i], _ = e.loan(m[i], fmt.Sprintf(`{"itemId":%q,"quantity":1}`, it.ID))
		}(i)
	}
	close(start)
	wg.Wait()
	ok := 0
	for _, c := range codes {
		if c == 201 {
			ok++
		} else if c != 409 {
			t.Fatalf("想定外: %v", codes)
		}
	}
	if ok != 1 {
		t.Fatalf("成功が%d件（1件のはず）: %v", ok, codes)
	}
	if n := e.count(`SELECT count(*) FROM inventory_loans WHERE item_id=$1`, it.ID); n != 1 {
		t.Fatalf("貸出の行が%d件（拒否した要求が行を残した）", n)
	}
	if e.outstandingOf(it.ID) != 1 {
		t.Fatal("貸出中の数量が総数と矛盾する")
	}
}

// シナリオ3・4: 代理返却ではborrower=A・return_actor=Bが分かる。部分返却で未返却が減る。
func TestProxyAndPartialReturn(t *testing.T) {
	e, admin, _ := invSetup(t)
	m := borrowers(t, e, 2)
	it := quantityItem(t, e, admin, 5)
	_, l := e.loan(m[0], fmt.Sprintf(`{"itemId":%q,"quantity":3}`, it.ID))
	*e.clock = e.clock.Add(time.Minute) // 実際の操作と同様に、返却は貸出より後の時刻にする（同時刻だと履歴の順序が不定になるため）
	// Bが1本だけ代理返却
	rec := e.as(m[1], "POST", "/inventory/loans/"+l.ID+"/returns", `{"quantity":1}`)
	var rr struct{ Returned, Outstanding int }
	decode(t, rec, &rr)
	if rec.Code != 200 || rr.Returned != 1 || rr.Outstanding != 2 {
		t.Fatalf("部分返却: %d %+v", rec.Code, rr)
	}
	if got := e.item(m[1], it.ID); got.Available != 3 || got.Outstanding != 2 {
		t.Fatalf("残数: %+v", got)
	}
	var hl struct{ Items []histEvent }
	decode(t, e.as(m[0], "GET", "/inventory/history?itemId="+it.ID, ""), &hl)
	if len(hl.Items) != 2 || hl.Items[0].Kind != "return" || hl.Items[0].BorrowerDisplayName != "借り手A" || hl.Items[0].ActorDisplayName != "借り手B" || hl.Items[1].Kind != "loan" || hl.Items[1].ActorDisplayName != "借り手A" {
		t.Fatalf("履歴（borrower=A, return_actor=B）: %+v", hl.Items)
	}
	// 未返却の数を超える返却・二重返却は拒否され、何も変わらない
	for name, body := range map[string]string{"超過": `{"quantity":3}`, "0": `{"quantity":0}`, "負": `{"quantity":-1}`} {
		if c := e.ret(m[1], l.ID, body); c == 200 {
			t.Errorf("%s: 返却できてしまった", name)
		}
	}
	if e.outstandingOf(it.ID) != 2 {
		t.Fatal("拒否した返却で数量が変わった")
	}
	if c := e.ret(m[0], l.ID, `{}`); c != 200 { // 数量省略=残りをすべて返却
		t.Fatalf("残りの返却: %d", c)
	}
	if c := e.ret(m[0], l.ID, `{"quantity":1}`); c != 409 {
		t.Fatalf("二重返却: %d（409のはず）", c)
	}
	if got := e.item(m[0], it.ID); got.Available != 5 || got.Outstanding != 0 {
		t.Fatalf("全返却後: %+v", got)
	}
	if rec := e.as(m[0], "POST", "/inventory/loans/00000000-0000-0000-0000-000000000000/returns", `{}`); rec.Code != 404 {
		t.Errorf("存在しない貸出: %d", rec.Code)
	}
}

// シナリオ5: 個体管理の同じ個体は二重に貸出できない。同時要求でも成功は1件。返却すると再び借りられる。
func TestIndividualUnitCannotBeLoanedTwice(t *testing.T) {
	e, admin, _ := invSetup(t)
	m := borrowers(t, e, 8)
	_, pico := e.newItem(admin, `{"name":"Pico","mode":"individual"}`)
	var u1, u2 struct{ ID string }
	decode(t, e.as(admin, "POST", "/inventory/items/"+pico.ID+"/units", `{"assetTag":"PICO-01"}`), &u1)
	decode(t, e.as(admin, "POST", "/inventory/items/"+pico.ID+"/units", `{"assetTag":"PICO-02"}`), &u2)
	c, l := e.loan(m[0], fmt.Sprintf(`{"itemId":%q,"unitId":%q}`, pico.ID, u1.ID))
	if c != 201 || l.UnitID == nil || l.Remaining != 1 {
		t.Fatalf("個体の貸出: %d %+v", c, l)
	}
	if c, _ := e.loan(m[1], fmt.Sprintf(`{"itemId":%q,"unitId":%q}`, pico.ID, u1.ID)); c != 409 {
		t.Fatalf("貸出中の個体を二重に貸出できた: %d", c)
	}
	if got := e.item(m[1], pico.ID); got.Available != 1 || got.Units[0].Loaned == nil || got.Units[0].Loaned.BorrowerDisplayName != "借り手A" {
		t.Fatalf("個体の状況: %+v", got)
	}
	// 同時に別の個体を8人が借りようとして、成功は1件
	var wg sync.WaitGroup
	start := make(chan struct{})
	codes := make([]int, len(m))
	for i := range m {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			codes[i], _ = e.loan(m[i], fmt.Sprintf(`{"itemId":%q,"unitId":%q}`, pico.ID, u2.ID))
		}(i)
	}
	close(start)
	wg.Wait()
	ok := 0
	for _, c := range codes {
		if c == 201 {
			ok++
		} else if c != 409 {
			t.Fatalf("想定外: %v", codes)
		}
	}
	if ok != 1 {
		t.Fatalf("同時貸出の成功が%d件（1件のはず）: %v", ok, codes)
	}
	if n := e.count(`SELECT count(*) FROM inventory_loans WHERE unit_id=$1`, u2.ID); n != 1 {
		t.Fatalf("個体の貸出行が%d件", n)
	}
	// 代理返却すると、再び借りられる
	if c := e.ret(m[2], l.ID, ``); c != 200 {
		t.Fatalf("個体の返却: %d", c)
	}
	if c, _ := e.loan(m[3], fmt.Sprintf(`{"itemId":%q,"unitId":%q}`, pico.ID, u1.ID)); c != 201 {
		t.Fatalf("返却後の再貸出: %d", c)
	}
	// 個体管理の検証: 数量指定・個体なし・他物品の個体・存在しない個体
	_, other := e.newItem(admin, `{"name":"Pico2","mode":"individual"}`)
	var ou struct{ ID string }
	decode(t, e.as(admin, "POST", "/inventory/items/"+other.ID+"/units", `{"assetTag":"X1"}`), &ou)
	for name, body := range map[string]string{
		"個体なし": fmt.Sprintf(`{"itemId":%q}`, pico.ID), "数量2": fmt.Sprintf(`{"itemId":%q,"unitId":%q,"quantity":2}`, pico.ID, u1.ID),
		"他物品の個体": fmt.Sprintf(`{"itemId":%q,"unitId":%q}`, pico.ID, ou.ID), "存在しない個体": fmt.Sprintf(`{"itemId":%q,"unitId":"00000000-0000-0000-0000-000000000000"}`, pico.ID),
	} {
		if c, _ := e.loan(m[4], body); c == 201 {
			t.Errorf("%s: 貸出できてしまった", name)
		}
	}
}

// 同時に同じ貸出を返却しても、返却の合計が貸出数を超えない。
func TestConcurrentReturnsNeverExceedTheLoan(t *testing.T) {
	e, admin, _ := invSetup(t)
	e.invHook = barrierHook(6) // 判定の直前で全員を揃えて、ロックが無ければ必ず競合するようにする
	e.h = e.build()
	m := borrowers(t, e, 6)
	it := quantityItem(t, e, admin, 5)
	_, l := e.loan(m[0], fmt.Sprintf(`{"itemId":%q,"quantity":2}`, it.ID))
	var wg sync.WaitGroup
	start := make(chan struct{})
	codes := make([]int, len(m))
	for i := range m {
		wg.Add(1)
		go func(i int) { defer wg.Done(); <-start; codes[i] = e.ret(m[i], l.ID, `{"quantity":1}`) }(i)
	}
	close(start)
	wg.Wait()
	ok := 0
	for _, c := range codes {
		if c == 200 {
			ok++
		} else if c != 409 {
			t.Fatalf("想定外: %v", codes)
		}
	}
	if ok != 2 {
		t.Fatalf("返却の成功が%d件（2件のはず）: %v", ok, codes)
	}
	if n := e.count(`SELECT COALESCE(SUM(quantity),0)::int FROM inventory_returns WHERE loan_id=$1`, l.ID); n != 2 {
		t.Fatalf("返却の合計が%d（貸出数2を超えた/不足）", n)
	}
	if e.outstandingOf(it.ID) != 0 {
		t.Fatal("未返却が残っている")
	}
}

// 貸出と総数の減少・廃止の同時実行でも、不変条件（貸出中≦総数、貸出中の物品は廃止されない）が守られる。
func TestLoanVersusShrinkAndArchiveKeepInvariants(t *testing.T) {
	for round := 0; round < 6; round++ {
		e, admin, _ := invSetup(t)
		m := borrowers(t, e, 1)
		it := quantityItem(t, e, admin, 3)
		var wg sync.WaitGroup
		start := make(chan struct{})
		var loanCode, patchCode int
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			loanCode, _ = e.loan(m[0], fmt.Sprintf(`{"itemId":%q,"quantity":3}`, it.ID))
		}()
		go func() {
			defer wg.Done()
			<-start
			patchCode = e.as(admin, "PATCH", "/inventory/items/"+it.ID, `{"totalQuantity":1,"reason":"紛失"}`).Code
		}()
		close(start)
		wg.Wait()
		out := e.outstandingOf(it.ID)
		var total int
		e.pool.QueryRow(ctxBG(), `SELECT total_quantity FROM inventory_items WHERE id=$1`, it.ID).Scan(&total)
		if out > total {
			t.Fatalf("round %d: 貸出中(%d)が総数(%d)を超えた loan=%d patch=%d", round, out, total, loanCode, patchCode)
		}
		if !((loanCode == 201 && patchCode == 409) || (loanCode == 409 && patchCode == 200)) {
			t.Fatalf("round %d: 一貫しない結果 loan=%d patch=%d", round, loanCode, patchCode)
		}
	}
	for round := 0; round < 6; round++ {
		e, admin, _ := invSetup(t)
		m := borrowers(t, e, 1)
		it := quantityItem(t, e, admin, 2)
		var wg sync.WaitGroup
		start := make(chan struct{})
		var loanCode, archCode int
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			loanCode, _ = e.loan(m[0], fmt.Sprintf(`{"itemId":%q,"quantity":1}`, it.ID))
		}()
		go func() {
			defer wg.Done()
			<-start
			archCode = e.as(admin, "PATCH", "/inventory/items/"+it.ID, `{"archived":true}`).Code
		}()
		close(start)
		wg.Wait()
		archived := e.count(`SELECT count(*) FROM inventory_items WHERE id=$1 AND archived_at IS NOT NULL`, it.ID) == 1
		if archived && e.outstandingOf(it.ID) > 0 {
			t.Fatalf("round %d: 貸出中の物品が廃止された loan=%d archive=%d", round, loanCode, archCode)
		}
		if !((loanCode == 201 && archCode == 409) || (loanCode == 409 && archCode == 200)) {
			t.Fatalf("round %d: 一貫しない結果 loan=%d archive=%d", round, loanCode, archCode)
		}
	}
}

func TestLoanValidationAndAuthorization(t *testing.T) {
	e, admin, _ := invSetup(t)
	m := borrowers(t, e, 1)
	it := quantityItem(t, e, admin, 2)
	if rec := e.do("POST", "/inventory/loans", fmt.Sprintf(`{"itemId":%q,"quantity":1}`, it.ID)); rec.Code != 401 {
		t.Errorf("匿名: %d", rec.Code)
	}
	if rec := e.do("POST", "/inventory/loans", fmt.Sprintf(`{"itemId":%q,"quantity":1}`, it.ID), withCookie(m[0].cookie)); rec.Code != 403 {
		t.Errorf("CSRFなし: %d", rec.Code)
	}
	for name, body := range map[string]string{
		"数量なし": fmt.Sprintf(`{"itemId":%q}`, it.ID), "0": fmt.Sprintf(`{"itemId":%q,"quantity":0}`, it.ID), "負": fmt.Sprintf(`{"itemId":%q,"quantity":-1}`, it.ID),
		"残り超過": fmt.Sprintf(`{"itemId":%q,"quantity":3}`, it.ID), "個体指定": fmt.Sprintf(`{"itemId":%q,"unitId":"00000000-0000-0000-0000-000000000000","quantity":1}`, it.ID),
		"物品なし": `{"quantity":1}`, "存在しない物品": `{"itemId":"00000000-0000-0000-0000-000000000000","quantity":1}`,
	} {
		if c, _ := e.loan(m[0], body); c == 201 {
			t.Errorf("%s: 貸出できてしまった", name)
		}
	}
	if n := e.count(`SELECT count(*) FROM inventory_loans`); n != 0 {
		t.Fatalf("拒否した要求が貸出を残した: %d", n)
	}
	// 廃止した物品は借りられない。貸出中は廃止できない
	if c, _ := e.loan(m[0], fmt.Sprintf(`{"itemId":%q,"quantity":1}`, it.ID)); c != 201 {
		t.Fatal("貸出")
	}
	if rec := e.as(admin, "PATCH", "/inventory/items/"+it.ID, `{"archived":true}`); rec.Code != 409 {
		t.Errorf("貸出中の廃止: %d", rec.Code)
	}
	if rec := e.as(admin, "PATCH", "/inventory/items/"+it.ID, `{"totalQuantity":0,"reason":"x"}`); rec.Code != 409 {
		t.Errorf("貸出中の数量を下回る総数: %d", rec.Code)
	}
}

// 貸出中の一覧は経過日数を表示時点で計算する（返却期限は持たない）。自分の分だけも絞れる。
func TestOutstandingListElapsedDaysAndNoPersonalData(t *testing.T) {
	e, admin, _ := invSetup(t)
	m := borrowers(t, e, 2)
	it := quantityItem(t, e, admin, 5)
	e.loan(m[0], fmt.Sprintf(`{"itemId":%q,"quantity":2}`, it.ID))
	*e.clock = e.clock.Add(3*24*time.Hour + time.Hour)
	e.loan(m[1], fmt.Sprintf(`{"itemId":%q,"quantity":1}`, it.ID))
	rec := e.as(m[0], "GET", "/inventory/loans", "")
	var ll struct{ Items []outLoan }
	decode(t, rec, &ll)
	if len(ll.Items) != 2 || ll.Items[0].BorrowerDisplayName != "借り手A" || ll.Items[0].ElapsedDays != 3 || ll.Items[0].Quantity != 2 || !ll.Items[0].Mine || ll.Items[1].ElapsedDays != 0 || ll.Items[1].Mine {
		t.Fatalf("貸出中の一覧: %+v", ll.Items)
	}
	decode(t, e.as(m[0], "GET", "/inventory/loans?mine=true", ""), &ll)
	if len(ll.Items) != 1 || ll.Items[0].BorrowerDisplayName != "借り手A" {
		t.Fatalf("自分の分だけ: %+v", ll.Items)
	}
	decode(t, e.as(m[0], "GET", "/inventory/loans?q="+"借り手B", ""), &ll)
	if len(ll.Items) != 1 {
		t.Fatalf("借りた人の表示名で検索: %d", len(ll.Items))
	}
	// 一覧・履歴にログインIDや個人情報が出ない
	for _, p := range []string{"/inventory/loans", "/inventory/history", "/inventory/items/" + it.ID} {
		body := e.as(m[0], "GET", p, "").Body.String()
		for _, secret := range []string{"user00", "user01", "example", "loginId", "legalName", "email", "borrowerUserId"} {
			if strings.Contains(body, secret) {
				t.Fatalf("%s に %q が含まれる: %s", p, secret, body)
			}
		}
	}
	if rec := e.as(m[0], "GET", "/inventory/loans?cursor=!!!", ""); rec.Code != 400 {
		t.Errorf("不正なcursor: %d", rec.Code)
	}
	// 返却済みは一覧から消え、履歴は残る
	var l struct{ Items []outLoan }
	decode(t, e.as(m[0], "GET", "/inventory/loans?mine=true", ""), &l)
	e.ret(m[1], l.Items[0].ID, ``)
	decode(t, e.as(m[0], "GET", "/inventory/loans", ""), &ll)
	if len(ll.Items) != 1 {
		t.Fatalf("返却済みが一覧に残る: %d", len(ll.Items))
	}
	var hl struct {
		Items      []histEvent
		NextCursor *string
	}
	decode(t, e.as(m[0], "GET", "/inventory/history?limit=2", ""), &hl)
	if len(hl.Items) != 2 || hl.NextCursor == nil {
		t.Fatalf("履歴のページング: %+v", hl)
	}
}
