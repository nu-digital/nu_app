package api

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nu-digital/nu_app/services/api/internal/authz"
	"github.com/nu-digital/nu_app/services/api/internal/httpx"
	"github.com/nu-digital/nu_app/services/api/internal/store"
)

// 在庫の整合性（docs/DECISIONS.md P2-02-1）:
//   - 貸出・返却・総数変更・廃止は、まず対象の物品行を FOR UPDATE でロックしてから検証・記録する（ロック順は常に物品→個体）。
//   - 残数は「総数 − (貸出合計 − 返却合計)」を、ロック取得後に導出して判定する。
//   - 借りた本人（borrower）・返却した人（actor）はセッションのユーザー。クライアントの入力を信用しない。

var (
	errInsufficient = &errTx{http.StatusConflict, "INSUFFICIENT_STOCK", "貸出できる数が足りません"}
	errUnitBusy     = &errTx{http.StatusConflict, "UNIT_UNAVAILABLE", "この個体は貸出中、または貸出できません"}
)

type loanView struct {
	ID          string    `json:"id"`
	ItemID      string    `json:"itemId"`
	UnitID      *string   `json:"unitId"`
	Quantity    int32     `json:"quantity"`
	BorrowedAt  time.Time `json:"borrowedAt"`
	Remaining   int64     `json:"remaining"` // 貸出後の物品の残数（数量管理）または利用可能な個体数（個体管理）
	Outstanding int       `json:"outstanding"`
}

// createLoan は POST /inventory/loans。
func (s *server) createLoan(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "inventory.loans.create", authz.Resource{})
	if !ok {
		return
	}
	var req struct {
		ItemID   string `json:"itemId"`
		Quantity *int   `json:"quantity"`
		UnitID   string `json:"unitId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	var itemID pgtype.UUID
	if err := itemID.Scan(req.ItemID); err != nil || !itemID.Valid {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "借りる物品を指定してください")
		return
	}
	var out loanView
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		it, err := q.GetInventoryItemForUpdate(r.Context(), itemID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"}
			}
			return err
		}
		if it.ArchivedAt.Valid {
			return &errTx{http.StatusConflict, "ARCHIVED", "この物品は廃止されています"}
		}
		now := pgtype.Timestamptz{Time: s.Now(), Valid: true}
		var unitID pgtype.UUID
		qty := int32(1)
		switch it.Mode {
		case "quantity":
			if req.UnitID != "" {
				return invalid("数量管理の物品では、個体ではなく数量を指定してください")
			}
			if req.Quantity == nil || *req.Quantity < 1 || *req.Quantity > maxQuantity {
				return invalid("借りる数量を1以上で指定してください")
			}
			if s.BeforeInventoryCheck != nil {
				s.BeforeInventoryCheck()
			}
			outstanding, err := q.ItemOutstanding(r.Context(), itemID)
			if err != nil {
				return err
			}
			if int64(*req.Quantity) > int64(it.TotalQuantity)-outstanding {
				return &errTx{http.StatusConflict, "INSUFFICIENT_STOCK", "残りは" + strconv.FormatInt(int64(it.TotalQuantity)-outstanding, 10) + "個です。それ以上は借りられません"}
			}
			qty = int32(*req.Quantity)
		case "individual":
			if req.Quantity != nil && *req.Quantity != 1 {
				return invalid("個体管理の物品は1つずつ借ります")
			}
			if err := unitID.Scan(req.UnitID); err != nil || !unitID.Valid {
				return invalid("借りる個体を指定してください")
			}
			if _, err := q.GetInventoryUnitForUpdate(r.Context(), store.GetInventoryUnitForUpdateParams{ID: unitID, ItemID: itemID}); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"}
				}
				return err
			}
		}
		loan, err := q.CreateInventoryLoan(r.Context(), store.CreateInventoryLoanParams{ItemID: itemID, UnitID: unitID, BorrowerUserID: a.Identity.UserID, Quantity: qty, BorrowedAt: now})
		if err != nil {
			return err
		}
		if it.Mode == "individual" {
			// 0行＝既に貸出中・廃止済み。DBのUNIQUE制約も二重貸出を防ぐ。
			n, err := q.SetUnitLoan(r.Context(), store.SetUnitLoanParams{ID: unitID, ItemID: itemID, CurrentLoanID: loan.ID})
			if err != nil {
				return err
			}
			if n != 1 {
				return errUnitBusy
			}
		}
		out = loanView{ID: loan.ID.String(), ItemID: itemID.String(), Quantity: qty, BorrowedAt: loan.BorrowedAt.Time, Outstanding: int(qty)}
		if unitID.Valid {
			u := unitID.String()
			out.UnitID = &u
		}
		outstanding, err := q.ItemOutstanding(r.Context(), itemID)
		if err != nil {
			return err
		}
		if it.Mode == "quantity" {
			out.Remaining = int64(it.TotalQuantity) - outstanding
		} else {
			avail, err := availableUnits(r, q, itemID)
			if err != nil {
				return err
			}
			out.Remaining = avail
		}
		if err := s.auditEvent(r.Context(), q, a.Identity.UserID, a.Identity.UserID, "inventory.loan_created", map[string]any{"itemId": itemID.String(), "loanId": loan.ID.String(), "quantity": qty}); err != nil {
			return err
		}
		// 通知は業務と同じトランザクションでoutboxへ（送信はworker。Discordの障害は貸出に影響しない）
		tag := ""
		if unitID.Valid {
			tag = s.unitTag(r, q, itemID, unitID)
		}
		rem := out.Remaining
		return s.enqueueNotification(r.Context(), q, EventLoanCreated, "loan:"+loan.ID.String(), NotificationPayload{
			ItemName: it.Name, AssetTag: tag, Quantity: int(qty), Borrower: s.displayName(r.Context(), q, a.Identity.UserID), Remaining: &rem})
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

func availableUnits(r *http.Request, q *store.Queries, itemID pgtype.UUID) (int64, error) {
	units, err := q.ListItemUnits(r.Context(), itemID)
	if err != nil {
		return 0, err
	}
	var n int64
	for _, u := range units {
		if u.State == "active" && !u.CurrentLoanID.Valid {
			n++
		}
	}
	return n, nil
}

// returnLoan は POST /inventory/loans/{id}/returns。誰でも他人の貸出を返却できる（代理返却）。部分返却可。
func (s *server) returnLoan(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "inventory.loans.return", authz.Resource{})
	if !ok {
		return
	}
	loanID, ok := parseItemID(w, r)
	if !ok {
		return
	}
	var req struct {
		Quantity *int `json:"quantity"`
	}
	if r.ContentLength != 0 && !decodeJSON(w, r, &req) {
		return
	}
	var out struct {
		LoanID      string `json:"loanId"`
		Returned    int32  `json:"returned"`
		Outstanding int    `json:"outstanding"`
	}
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		// 物品を先にロックする（貸出の作成・総数変更・廃止と同じ順序）。そのため貸出の物品IDだけ先に読む。
		loan, err := q.GetInventoryLoan(r.Context(), loanID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"}
			}
			return err
		}
		retItem, err := q.GetInventoryItemForUpdate(r.Context(), loan.ItemID)
		if err != nil {
			return err
		}
		if s.BeforeInventoryCheck != nil {
			s.BeforeInventoryCheck() // テスト専用: 判定の直前で止めて競合を確実に起こす
		}
		outstanding, err := q.LoanOutstanding(r.Context(), loanID) // ロック取得後に確定させる
		if err != nil {
			return err
		}
		qty := int(outstanding)
		if req.Quantity != nil {
			qty = *req.Quantity
		}
		if qty < 1 {
			return invalid("返却する数量を1以上で指定してください")
		}
		if outstanding == 0 {
			return &errTx{http.StatusConflict, "ALREADY_RETURNED", "この貸出は既にすべて返却されています"}
		}
		if qty > int(outstanding) {
			return &errTx{http.StatusConflict, "EXCEEDS_OUTSTANDING", "未返却の数（" + strconv.Itoa(int(outstanding)) + "）を超えて返却できません"}
		}
		ret, err := q.CreateInventoryReturn(r.Context(), store.CreateInventoryReturnParams{LoanID: loanID, ActorUserID: a.Identity.UserID, Quantity: int32(qty), ReturnedAt: pgtype.Timestamptz{Time: s.Now(), Valid: true}})
		if err != nil {
			return err
		}
		left := int(outstanding) - qty
		if left == 0 && loan.UnitID.Valid {
			if err := q.ClearUnitLoan(r.Context(), loanID); err != nil {
				return err
			}
		}
		out.LoanID, out.Returned, out.Outstanding = loanID.String(), int32(qty), left
		if err := s.auditEvent(r.Context(), q, a.Identity.UserID, loan.BorrowerUserID, "inventory.loan_returned", map[string]any{"itemId": loan.ItemID.String(), "loanId": loanID.String(), "quantity": qty, "proxy": loan.BorrowerUserID != a.Identity.UserID}); err != nil {
			return err
		}
		tag := ""
		if loan.UnitID.Valid {
			tag = s.unitTag(r, q, loan.ItemID, loan.UnitID)
		}
		return s.enqueueNotification(r.Context(), q, EventLoanReturned, "return:"+ret.ID.String(), NotificationPayload{
			ItemName: retItem.Name, AssetTag: tag, Quantity: qty, Borrower: s.displayName(r.Context(), q, loan.BorrowerUserID),
			Actor: s.displayName(r.Context(), q, a.Identity.UserID), Proxy: loan.BorrowerUserID != a.Identity.UserID})
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

type outstandingRow struct {
	ID                  string    `json:"id"`
	ItemID              string    `json:"itemId"`
	ItemName            string    `json:"itemName"`
	AssetTag            string    `json:"assetTag"`
	BorrowerDisplayName string    `json:"borrowerDisplayName"`
	Quantity            int       `json:"quantity"` // 未返却の数量
	BorrowedAt          time.Time `json:"borrowedAt"`
	ElapsedDays         int       `json:"elapsedDays"`
	Mine                bool      `json:"mine"`
}

func encodeTimeCursor(t time.Time, id pgtype.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano) + "|" + id.String()))
}

func decodeTimeCursor(c string) (time.Time, pgtype.UUID, bool) { return decodeCursor(c) }

func pageLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", "limit は1〜100で指定してください")
			return 0, false
		}
		limit = n
	}
	return limit, true
}

func optionalUUID(w http.ResponseWriter, r *http.Request, name string) (pgtype.UUID, bool) {
	var id pgtype.UUID
	if v := r.URL.Query().Get(name); v != "" {
		if err := id.Scan(v); err != nil || !id.Valid {
			httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", name+" が正しくありません")
			return pgtype.UUID{}, false
		}
	}
	return id, true
}

// listLoans は GET /inventory/loans（貸出中の一覧。古い順。経過日数つき）。
func (s *server) listLoans(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "inventory.loans.list", authz.Resource{})
	if !ok {
		return
	}
	qs := r.URL.Query()
	search := strings.TrimSpace(qs.Get("q"))
	if utf8.RuneCountInString(search) > 100 {
		httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", "検索語は100文字以内にしてください")
		return
	}
	limit, ok := pageLimit(w, r)
	if !ok {
		return
	}
	itemID, ok := optionalUUID(w, r, "itemId")
	if !ok {
		return
	}
	p := store.ListOutstandingLoansParams{Column1: qs.Get("mine") == "true", Column2: a.Identity.UserID, Column3: likeEscape(search), Column4: itemID, Limit: int32(limit + 1)}
	if c := qs.Get("cursor"); c != "" {
		t, id, ok := decodeTimeCursor(c)
		if !ok {
			httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", "cursor が正しくありません")
			return
		}
		p.Column5, p.Column6, p.Column7 = true, pgtype.Timestamptz{Time: t, Valid: true}, id
	}
	rows, err := store.New(s.Pool).ListOutstandingLoans(r.Context(), p)
	if err != nil {
		s.internal(w, r)
		return
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		c := encodeTimeCursor(last.BorrowedAt.Time, last.ID)
		next = &c
	}
	now := s.Now()
	items := make([]outstandingRow, 0, len(rows))
	for _, x := range rows {
		items = append(items, outstandingRow{ID: x.ID.String(), ItemID: x.ItemID.String(), ItemName: x.ItemName, AssetTag: x.AssetTag, BorrowerDisplayName: x.BorrowerDisplayName,
			Quantity: int(x.Outstanding), BorrowedAt: x.BorrowedAt.Time, ElapsedDays: elapsedDays(x.BorrowedAt.Time, now), Mine: x.BorrowerUserID == a.Identity.UserID})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": next})
}

// history は GET /inventory/history（貸出・返却の履歴。新しい順）。借りた人と返却した人を別に表示する。
func (s *server) history(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "inventory.history", authz.Resource{})
	if !ok {
		return
	}
	isAdmin := false
	for _, ro := range a.Identity.Roles {
		if ro == authz.RoleAdmin || ro == authz.RoleOwner {
			isAdmin = true
		}
	}
	limit, ok := pageLimit(w, r)
	if !ok {
		return
	}
	itemID, ok := optionalUUID(w, r, "itemId")
	if !ok {
		return
	}
	p := store.ListInventoryHistoryParams{Column1: itemID, Limit: int32(limit + 1)}
	if c := r.URL.Query().Get("cursor"); c != "" {
		t, id, ok := decodeTimeCursor(c)
		if !ok {
			httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", "cursor が正しくありません")
			return
		}
		p.Column2, p.Column3, p.Column4 = true, pgtype.Timestamptz{Time: t, Valid: true}, id
	}
	rows, err := store.New(s.Pool).ListInventoryHistory(r.Context(), p)
	if err != nil {
		s.internal(w, r)
		return
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		c := encodeTimeCursor(last.At.Time, last.EventID)
		next = &c
	}
	type event struct {
		ID                  string    `json:"id"`
		Kind                string    `json:"kind"` // loan / return
		ItemID              string    `json:"itemId"`
		ItemName            string    `json:"itemName"`
		AssetTag            string    `json:"assetTag"`
		BorrowerDisplayName string    `json:"borrowerDisplayName"`
		ActorDisplayName    string    `json:"actorDisplayName"` // 貸出=借りた人、返却=返却した人
		Quantity            int32     `json:"quantity"`
		At                  time.Time `json:"at"`
		// 訂正イベントのとき: kind=correction、correctionKind=adjust/void、targetType=loan/return。actor=訂正した人。
		CorrectionKind string `json:"correctionKind,omitempty"`
		TargetType     string `json:"targetType,omitempty"`
		Reason         string `json:"reason,omitempty"` // 理由は管理者にだけ返す（自由記述のため）
	}
	items := make([]event, 0, len(rows))
	for _, x := range rows {
		items = append(items, event{ID: x.EventID.String(), Kind: x.Kind, ItemID: x.ItemID.String(), ItemName: x.ItemName, AssetTag: x.AssetTag,
			BorrowerDisplayName: x.BorrowerDisplayName, ActorDisplayName: x.ActorDisplayName, Quantity: x.Quantity, At: x.At.Time,
			CorrectionKind: x.CorrectionKind, TargetType: x.TargetType, Reason: reasonFor(isAdmin, x.Reason)})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": next})
}

func reasonFor(admin bool, reason string) string {
	if admin {
		return reason
	}
	return ""
}

// unitTag は個体の管理番号（通知本文用）。見つからなければ空。
func (s *server) unitTag(r *http.Request, q *store.Queries, itemID, unitID pgtype.UUID) string {
	u, err := q.GetInventoryUnitForUpdate(r.Context(), store.GetInventoryUnitForUpdateParams{ID: unitID, ItemID: itemID})
	if err != nil {
		return ""
	}
	return u.AssetTag
}
