package expense

import "sort"

// Item は集計の入力（1申請）。ActualYen は購入報告で確定した実額（無ければ nil）。
type Item struct {
	Status     Status
	PlannedYen int64
	ActualYen  *int64
	Category   string // カテゴリID（または名前）
	Project    string // 作品・イベントのID。空なら「紐付けなし」
}

// Summary は予算ダッシュボードの数値。**各申請は状態に応じて1か所だけに計上する**（二重計上しない）:
//   - 申請中（submitted）→ UnapprovedYen（未承認の申請額）
//   - 承認済み・未購入（approved）→ ScheduledYen（購入予定額）
//   - 購入済み以降（purchased・receipt_submitted・receipt_verified・settled）→ PurchasedYen（実額。実額が無ければ予定額）
//   - 下書き・差し戻し・却下・取り消しは集計に入れない
type Summary struct {
	BudgetYen       int64 // 使用可能額
	PurchasedYen    int64
	ScheduledYen    int64
	UnapprovedYen   int64
	RemainingYen    int64   // 使用可能額 − 購入済額
	AfterScheduled  int64   // 使用可能額 − 購入済額 − 購入予定額
	UnsettledYen    int64   // 購入済みで未精算（精算済み以外）
	UnallocatedYen  int64   // 使用可能額 − カテゴリ予算枠の合計
	ReceiptMissing  int     // 購入済み・領収書未提出
	ReceiptPending  int     // 領収書の確認待ち
	SettlePending   int     // 精算待ち（領収書確認済み）
	ConsumptionRate float64 // 購入済額 ÷ 使用可能額（参考値。目標ではない）。使用可能額0なら0
	ByCategory      map[string]Breakdown
	ByProject       map[string]Breakdown
}

// Breakdown は区分（カテゴリ・作品/イベント）ごとの内訳。
type Breakdown struct {
	PurchasedYen, ScheduledYen, UnapprovedYen int64
	Count                                     int
}

func amountOf(it Item) int64 {
	if it.ActualYen != nil {
		return *it.ActualYen
	}
	return it.PlannedYen
}

// Summarize は申請の一覧と年度の予算から集計する。allocations はカテゴリごとの予算枠。
func Summarize(budgetYen int64, allocations map[string]int64, items []Item) Summary {
	s := Summary{BudgetYen: budgetYen, ByCategory: map[string]Breakdown{}, ByProject: map[string]Breakdown{}}
	for _, it := range items {
		var pur, sch, un int64
		switch it.Status {
		case Submitted:
			un = it.PlannedYen
		case Approved:
			sch = it.PlannedYen
		case Purchased, ReceiptSubmitted, ReceiptVerified, Settled:
			pur = amountOf(it)
		default:
			continue
		}
		s.PurchasedYen += pur
		s.ScheduledYen += sch
		s.UnapprovedYen += un
		switch it.Status {
		case Purchased:
			s.ReceiptMissing++
		case ReceiptSubmitted:
			s.ReceiptPending++
		case ReceiptVerified:
			s.SettlePending++
		}
		if pur > 0 && it.Status != Settled {
			s.UnsettledYen += pur
		}
		add := func(m map[string]Breakdown, k string) {
			b := m[k]
			b.PurchasedYen += pur
			b.ScheduledYen += sch
			b.UnapprovedYen += un
			b.Count++
			m[k] = b
		}
		add(s.ByCategory, it.Category)
		add(s.ByProject, it.Project)
	}
	var alloc int64
	for _, v := range allocations {
		alloc += v
	}
	s.RemainingYen = budgetYen - s.PurchasedYen
	s.AfterScheduled = s.RemainingYen - s.ScheduledYen
	s.UnallocatedYen = budgetYen - alloc
	if budgetYen > 0 {
		s.ConsumptionRate = float64(s.PurchasedYen) / float64(budgetYen)
	}
	return s
}

// SortedKeys は内訳のキーを、購入済額の多い順（同額はキー順）で返す。
func SortedKeys(m map[string]Breakdown) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := m[keys[i]], m[keys[j]]
		if a.PurchasedYen != b.PurchasedYen {
			return a.PurchasedYen > b.PurchasedYen
		}
		return keys[i] < keys[j]
	})
	return keys
}
