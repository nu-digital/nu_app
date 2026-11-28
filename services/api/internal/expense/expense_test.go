package expense

import "testing"

func i64(v int64) *int64 { return &v }

func TestAmountsTotal(t *testing.T) {
	ok := []struct {
		a    Amounts
		want int64
	}{
		{Amounts{3, 1980, 0, 0}, 5940},
		{Amounts{2, 1000, 500, 200}, 2300},
		{Amounts{1, 0, 0, 0}, 0},
	}
	for _, c := range ok {
		if got, err := c.a.Total(); err != nil || got != c.want {
			t.Errorf("%+v: %d %v", c.a, got, err)
		}
	}
	bad := []Amounts{{0, 100, 0, 0}, {10001, 1, 0, 0}, {1, -1, 0, 0}, {1, 100, 0, 200}, {1, MaxAmountYen + 1, 0, 0}, {10000, MaxAmountYen, 0, 0}}
	for _, a := range bad {
		if _, err := a.Total(); err == nil {
			t.Errorf("受け付けた: %+v", a)
		}
	}
}

func TestNeedsReapproval(t *testing.T) {
	cases := []struct {
		planned, actual int64
		want            bool
	}{
		{10000, 9000, false},
		{10000, 10000, false},
		{10000, 13000, false}, // 小額は+3,000円（10%より大きい方）まで許容
		{10000, 13001, true},
		{1000, 4000, false},
		{1000, 4001, true},
		{100000, 110000, false},
		{100000, 110001, true},
	}
	for _, c := range cases {
		if NeedsReapproval(c.planned, c.actual) != c.want {
			t.Errorf("%d→%d: want %v", c.planned, c.actual, c.want)
		}
	}
	if to, re := ReportPurchase(10000, 20000); to != Submitted || !re {
		t.Error("大幅超過は再承認へ")
	}
	if to, re := ReportPurchase(10000, 10500); to != Purchased || re {
		t.Error("許容内は購入済みへ")
	}
}

func TestTransitionsAndSeparationOfDuties(t *testing.T) {
	req, adm, both := Actor{IsRequester: true}, Actor{IsAdmin: true}, Actor{IsRequester: true, IsAdmin: true}
	allowed := []struct {
		f, t Status
		a    Actor
	}{
		{Draft, Submitted, req}, {Submitted, Approved, adm}, {Submitted, Returned, adm}, {Submitted, Rejected, adm},
		{Returned, Submitted, req}, {Approved, Purchased, req}, {Purchased, ReceiptSubmitted, req},
		{ReceiptSubmitted, ReceiptVerified, adm}, {ReceiptSubmitted, Purchased, adm}, {ReceiptVerified, Settled, adm},
		{Draft, Submitted, both}, // 管理者でも自分の申請を出せる
	}
	for _, c := range allowed {
		if err := CanTransition(c.f, c.t, c.a); err != nil {
			t.Errorf("%s→%s %+v: %v", c.f, c.t, c.a, err)
		}
	}
	denied := []struct {
		f, t Status
		a    Actor
	}{
		{Submitted, Approved, req},  // 申請者は承認できない
		{Submitted, Approved, both}, // 自分の申請は自分で承認できない（職務分掌）
		{ReceiptSubmitted, ReceiptVerified, both},
		{ReceiptVerified, Settled, both},
		{Draft, Approved, adm},    // 飛び越し不可
		{Settled, Cancelled, adm}, // 終端
		{Rejected, Submitted, req},
		{Approved, Purchased, adm}, // 購入報告は申請者だけ
		{Purchased, Settled, adm},
	}
	for _, c := range denied {
		if err := CanTransition(c.f, c.t, c.a); err == nil {
			t.Errorf("許可された: %s→%s %+v", c.f, c.t, c.a)
		}
	}
}

func TestSummarizeNoDoubleCounting(t *testing.T) {
	items := []Item{
		{Draft, 9999, nil, "c1", "p1"},                  // 集計外
		{Submitted, 1000, nil, "c1", "p1"},              // 未承認
		{Approved, 2000, nil, "c1", ""},                 // 購入予定
		{Purchased, 3000, i64(3300), "c2", "p1"},        // 購入済（実額）・領収書未提出
		{ReceiptSubmitted, 4000, i64(4000), "c2", "p2"}, // 確認待ち
		{ReceiptVerified, 500, i64(500), "c1", "p2"},    // 精算待ち
		{Settled, 700, i64(700), "c1", "p1"},            // 精算済
		{Rejected, 5000, nil, "c1", "p1"}, {Cancelled, 6000, nil, "c1", "p1"}, {Returned, 7000, nil, "c1", "p1"},
	}
	s := Summarize(100000, map[string]int64{"c1": 60000, "c2": 50000}, items)
	if s.PurchasedYen != 3300+4000+500+700 || s.ScheduledYen != 2000 || s.UnapprovedYen != 1000 {
		t.Errorf("%+v", s)
	}
	if s.RemainingYen != 100000-8500 || s.AfterScheduled != 100000-8500-2000 || s.UnallocatedYen != -10000 {
		t.Errorf("残額: %+v", s)
	}
	if s.UnsettledYen != 3300+4000+500 || s.ReceiptMissing != 1 || s.ReceiptPending != 1 || s.SettlePending != 1 {
		t.Errorf("未精算・件数: %+v", s)
	}
	// 区分の内訳の合計 = 全体（二重計上・取りこぼしなし）
	var pur int64
	for _, b := range s.ByCategory {
		pur += b.PurchasedYen
	}
	var pur2 int64
	for _, b := range s.ByProject {
		pur2 += b.PurchasedYen
	}
	if pur != s.PurchasedYen || pur2 != s.PurchasedYen {
		t.Errorf("内訳の合計: %d %d != %d", pur, pur2, s.PurchasedYen)
	}
	if s.ByProject[""].ScheduledYen != 2000 {
		t.Error("紐付けなしの区分に入らない")
	}
	if keys := SortedKeys(s.ByCategory); keys[0] != "c2" && keys[0] != "c1" {
		t.Error("並び")
	}
	if z := Summarize(0, nil, nil); z.ConsumptionRate != 0 {
		t.Error("予算0で割り算しない")
	}
}

func TestNextActionHidesInternals(t *testing.T) {
	if NextAction(Purchased, Actor{IsRequester: true}) != "領収書を提出する" {
		t.Error("申請者の次の操作")
	}
	if NextAction(ReceiptSubmitted, Actor{IsAdmin: true}) != "領収書を確認する" {
		t.Error("管理者の次の操作")
	}
	if NextAction(ReceiptSubmitted, Actor{IsAdmin: true, IsRequester: true}) == "領収書を確認する" {
		t.Error("自分の申請は確認できない")
	}
	if !Returned.Editable() || Submitted.Editable() || !Settled.Terminal() {
		t.Error("状態の性質")
	}
}
