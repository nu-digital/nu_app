// Package expense は会計機能（支出申請）のドメイン規則: 金額計算・状態遷移・予算集計。DBやHTTPに依存しない純粋な関数だけを置く。
// 金額はすべて整数円。AI（LLM）の出力に金額計算をさせず、ここで計算・検証する（docs/design/phase3-accounting.md §4）。
package expense

import (
	"errors"
	"fmt"
)

// 金額の上限（DBのCHECKと同じ）。
const (
	MaxQuantity   = 10000
	MaxAmountYen  = 100_000_000
	MaxTotalYen   = 1_000_000_000
	reapprovalPct = 10    // 実額が予定額を超えてよい割合（％）。暫定（要確認）
	reapprovalYen = 3_000 // 実額が予定額を超えてよい金額（円）。暫定（要確認）
)

var ErrAmount = errors.New("金額が不正です")

// Amounts は申請の金額の入力。
type Amounts struct {
	Quantity  int
	UnitYen   int64
	Shipping  int64
	DiscountY int64
}

// Total は 数量×単価＋送料−割引 を返す。範囲外・負の合計はエラー（オーバーフローしない上限内で計算する）。
func (a Amounts) Total() (int64, error) {
	if a.Quantity < 1 || a.Quantity > MaxQuantity {
		return 0, fmt.Errorf("%w: 数量は1〜%d", ErrAmount, MaxQuantity)
	}
	for name, v := range map[string]int64{"単価": a.UnitYen, "送料": a.Shipping, "割引": a.DiscountY} {
		if v < 0 || v > MaxAmountYen {
			return 0, fmt.Errorf("%w: %sは0〜%d円", ErrAmount, name, int64(MaxAmountYen))
		}
	}
	total := int64(a.Quantity)*a.UnitYen + a.Shipping - a.DiscountY
	if total < 0 {
		return 0, fmt.Errorf("%w: 割引が合計を超えています", ErrAmount)
	}
	if total > MaxTotalYen {
		return 0, fmt.Errorf("%w: 合計が大きすぎます", ErrAmount)
	}
	return total, nil
}

// NeedsReapproval は、購入報告の実額が予定額を大きく超えたか（予定額＋max(10%, 3,000円) を超えたら再承認が必要）。
// 予定額以下なら不要。しきい値は暫定（DECISIONSに記録・要確認）。
func NeedsReapproval(plannedYen, actualYen int64) bool {
	if actualYen <= plannedYen {
		return false
	}
	allow := plannedYen * reapprovalPct / 100
	if allow < reapprovalYen {
		allow = reapprovalYen
	}
	return actualYen > plannedYen+allow
}

// ForeignAmount は外貨の入力。円換算額は申請者が入力し、管理者が確認する（レートは記録のみ）。
// 通貨コードは英大文字3文字。円換算額の検証は Amounts と同じ。
func ValidCurrency(code string) bool {
	if len(code) != 3 {
		return false
	}
	for _, c := range code {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}
