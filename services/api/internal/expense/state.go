package expense

import "fmt"

// Status は申請の内部状態。画面では「次にやること」だけを見せる（NextAction）。
type Status string

const (
	Draft            Status = "draft"
	Submitted        Status = "submitted"
	Approved         Status = "approved"
	Returned         Status = "returned" // 差し戻し（申請者が直して再申請）
	Rejected         Status = "rejected"
	Purchased        Status = "purchased"         // 購入済み・領収書未提出
	ReceiptSubmitted Status = "receipt_submitted" // 領収書提出済み・確認待ち
	ReceiptVerified  Status = "receipt_verified"  // 領収書確認済み・精算待ち
	Settled          Status = "settled"
	Cancelled        Status = "cancelled"
)

// Actor は操作者の立場。申請者本人か、会計の管理者（Admin/Owner）か。申請者本人が管理者でも、自分の申請は承認・領収書確認・精算ができない（職務分掌）。
type Actor struct {
	IsRequester bool
	IsAdmin     bool
}

type transition struct {
	from, to  Status
	requester bool // 申請者本人ができる
	admin     bool // 管理者ができる（申請者本人でない場合）
}

var transitions = []transition{
	{Draft, Submitted, true, false},
	{Draft, Cancelled, true, false},
	{Submitted, Approved, false, true},
	{Submitted, Returned, false, true},
	{Submitted, Rejected, false, true},
	{Submitted, Cancelled, true, false},
	{Returned, Submitted, true, false},
	{Returned, Cancelled, true, false},
	{Approved, Purchased, true, false}, // 購入報告（再承認が必要なら Submitted へ戻す。ReportPurchase）
	{Approved, Cancelled, true, true},
	{Purchased, ReceiptSubmitted, true, false},
	{Purchased, Cancelled, false, true},
	{ReceiptSubmitted, ReceiptVerified, false, true},
	{ReceiptSubmitted, Purchased, false, true}, // 領収書の不備（差し戻し→再提出）
	{ReceiptVerified, Settled, false, true},
	{ReceiptVerified, ReceiptSubmitted, false, true}, // 確認の取り消し（誤操作の訂正。履歴に残る）
}

// CanTransition は、その立場の人が from→to の遷移をしてよいか。管理者による承認・確認・精算は、申請者本人には許さない。
func CanTransition(from, to Status, a Actor) error {
	for _, t := range transitions {
		if t.from != from || t.to != to {
			continue
		}
		if a.IsRequester && t.requester {
			return nil
		}
		if a.IsAdmin && t.admin && !a.IsRequester {
			return nil
		}
		if a.IsAdmin && t.admin && a.IsRequester {
			return fmt.Errorf("自分の申請は、自分では承認・確認・精算できません")
		}
		return fmt.Errorf("この操作は許可されていません")
	}
	return fmt.Errorf("%s から %s へは変更できません", from, to)
}

// ReportPurchase は購入報告（Approved→）の遷移先を決める。実額が予定額を大きく超えたら、再承認のため Submitted に戻す。
func ReportPurchase(plannedYen, actualYen int64) (to Status, reapproval bool) {
	if NeedsReapproval(plannedYen, actualYen) {
		return Submitted, true
	}
	return Purchased, false
}

// Editable は、申請者が内容（品目・金額・理由）を直せる状態か。申請後（承認待ち以降）は、管理者の差し戻しを経る。
func (s Status) Editable() bool { return s == Draft || s == Returned }

// Terminal は、これ以上変わらない状態。
func (s Status) Terminal() bool { return s == Rejected || s == Settled || s == Cancelled }

// NextAction は申請者・管理者の画面に出す「次にやること」（内部状態を意識させない）。
func NextAction(s Status, a Actor) string {
	switch s {
	case Draft:
		return pick(a.IsRequester, "内容を確認して申請する", "")
	case Submitted:
		return pick(a.IsAdmin && !a.IsRequester, "承認・差し戻し・却下を判断する", "承認待ち")
	case Returned:
		return pick(a.IsRequester, "指摘を直して再申請する", "申請者の修正待ち")
	case Approved:
		return pick(a.IsRequester, "購入したら購入報告をする", "購入待ち")
	case Purchased:
		return pick(a.IsRequester, "領収書を提出する", "領収書の提出待ち")
	case ReceiptSubmitted:
		return pick(a.IsAdmin && !a.IsRequester, "領収書を確認する", "領収書の確認待ち")
	case ReceiptVerified:
		return pick(a.IsAdmin && !a.IsRequester, "精算する", "精算待ち")
	case Settled:
		return "完了"
	case Rejected:
		return "却下"
	case Cancelled:
		return "取り消し"
	}
	return ""
}

func pick(cond bool, yes, no string) string {
	if cond {
		return yes
	}
	return no
}
