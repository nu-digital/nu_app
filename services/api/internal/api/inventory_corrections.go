package api

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nu-digital/nu_app/services/api/internal/authz"
	"github.com/nu-digital/nu_app/services/api/internal/httpx"
	"github.com/nu-digital/nu_app/services/api/internal/store"
)

// 物品の訂正（D09）。元の貸出・返却は更新・削除せず、訂正イベントを追加して有効数量へ反映する。
// 物品行をロックして通常の貸出・返却と直列化し、追加後に不変条件（在庫が負にならない・返却数が貸出数を超えない）を確認して、
// 破れていればトランザクションごと巻き戻して409にする。同じIdempotency-Keyの再送は二重適用せず同じ結果を返す。

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._~:-]{8,100}$`)

type correctionReq struct {
	TargetType    string `json:"targetType"`
	TargetID      string `json:"targetId"`
	Kind          string `json:"kind"`
	DeltaQuantity *int   `json:"deltaQuantity"`
	Reason        string `json:"reason"`
}

type correctionResp struct {
	ID            string    `json:"id"`
	TargetType    string    `json:"targetType"`
	TargetID      string    `json:"targetId"`
	Kind          string    `json:"kind"`
	DeltaQuantity *int32    `json:"deltaQuantity"`
	Reason        string    `json:"reason"`
	CreatedAt     time.Time `json:"createdAt"`
	Replayed      bool      `json:"replayed"`
	// 訂正後の物品の状態
	ItemOutstanding int64 `json:"itemOutstanding"`
	ItemAvailable   int64 `json:"itemAvailable"`
}

func fingerprint(r correctionReq, reason string) string {
	delta := "-"
	if r.DeltaQuantity != nil {
		delta = strconv.Itoa(*r.DeltaQuantity)
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{r.TargetType, r.TargetID, r.Kind, delta, reason}, "\x1f")))
	return hex.EncodeToString(sum[:])
}

func (s *server) createCorrection(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "inventory.corrections.create", authz.Resource{})
	if !ok {
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !idempotencyKeyPattern.MatchString(key) {
		httpx.Error(w, r, http.StatusBadRequest, "IDEMPOTENCY_KEY_REQUIRED", "Idempotency-Key ヘッダー（8〜100文字の英数字と . _ ~ : -）が必要です")
		return
	}
	var req correctionReq
	if !decodeJSON(w, r, &req) {
		return
	}
	reason := strings.TrimSpace(req.Reason)
	var targetID pgtype.UUID
	switch {
	case req.TargetType != "loan" && req.TargetType != "return":
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "訂正の対象（targetType）は loan か return を指定してください")
		return
	case targetID.Scan(req.TargetID) != nil || !targetID.Valid:
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "訂正する貸出・返却を指定してください")
		return
	case req.Kind != "adjust" && req.Kind != "void":
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "kind は adjust（数量の訂正）か void（誤登録の取消）を指定してください")
		return
	case req.Kind == "adjust" && (req.DeltaQuantity == nil || *req.DeltaQuantity == 0 || *req.DeltaQuantity > maxQuantity || *req.DeltaQuantity < -maxQuantity):
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "数量の訂正には、0以外の増減（deltaQuantity）を指定してください")
		return
	case req.Kind == "void" && req.DeltaQuantity != nil:
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "取消には数量を指定しません")
		return
	case reason == "" || utf8.RuneCountInString(reason) > 200:
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "訂正の理由を200文字以内で入力してください（個人情報は書かないでください）")
		return
	}
	fp := fingerprint(req, reason)
	var out correctionResp
	status := http.StatusCreated
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		// 対象の物品を特定して先にロックする（貸出・返却・総数変更と同じ順序）。
		var loanID pgtype.UUID
		if req.TargetType == "loan" {
			loanID = targetID
		} else {
			ret, err := q.GetRawReturn(r.Context(), targetID)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"}
				}
				return err
			}
			loanID = ret.LoanID
		}
		rawLoan, err := q.GetInventoryLoan(r.Context(), loanID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"}
			}
			return err
		}
		item, err := q.GetInventoryItemForUpdate(r.Context(), rawLoan.ItemID)
		if err != nil {
			return err
		}
		if s.BeforeInventoryCheck != nil {
			s.BeforeInventoryCheck() // テスト専用: 判定の直前で止めて競合を確実に起こす
		}
		// ロック取得後に、同じキーが既に適用されていないか確認する（同時再送でも二重適用しない）
		if prev, err := q.GetCorrectionByKey(r.Context(), key); err == nil {
			if prev.RequestFingerprint != fp {
				return &errTx{http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REUSED", "同じIdempotency-Keyで、内容の異なる訂正は送れません"}
			}
			status = http.StatusOK
			return s.fillCorrection(r, q, item, prev, true, &out)
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		isUnit := rawLoan.UnitID.Valid
		if req.Kind == "adjust" && isUnit {
			return invalid("個体管理の貸出・返却は数量を訂正できません。誤りは取消（void）してください")
		}
		var before int32
		if req.TargetType == "loan" {
			eff, err := q.GetLoanEff(r.Context(), targetID)
			if err != nil {
				return err
			}
			before = eff.Quantity
		} else {
			eff, err := q.GetReturnEff(r.Context(), targetID)
			if err != nil {
				return err
			}
			before = eff.Quantity
		}
		if before == 0 && req.Kind == "void" {
			return &errTx{http.StatusConflict, "ALREADY_VOIDED", "この記録は既に取り消されています"}
		}
		if before == 0 {
			return &errTx{http.StatusConflict, "CORRECTION_INVALID", "取り消された記録の数量は訂正できません"}
		}
		var delta pgtype.Int4
		if req.Kind == "adjust" {
			delta = pgtype.Int4{Int32: int32(*req.DeltaQuantity), Valid: true}
		}
		ev, err := q.InsertCorrection(r.Context(), store.InsertCorrectionParams{
			ItemID: item.ID, TargetType: req.TargetType, TargetID: targetID, Kind: req.Kind, DeltaQuantity: delta, Reason: reason,
			ActorUserID: a.Identity.UserID, IdempotencyKey: key, RequestFingerprint: fp, CreatedAt: pgtype.Timestamptz{Time: s.Now(), Valid: true},
		})
		if err != nil {
			var pe *pgconn.PgError
			if errors.As(err, &pe) && pe.Code == "23505" {
				return &errTx{http.StatusConflict, "ALREADY_VOIDED", "この記録は既に取り消されています"}
			}
			return err
		}
		// 不変条件の確認。破れていれば巻き戻す（元記録は変わらず、訂正イベントも残らない）。
		var after int32
		if req.TargetType == "loan" {
			eff, _ := q.GetLoanEff(r.Context(), targetID)
			after = eff.Quantity
		} else {
			eff, _ := q.GetReturnEff(r.Context(), targetID)
			after = eff.Quantity
		}
		if req.Kind == "adjust" && after < 1 {
			return &errTx{http.StatusConflict, "CORRECTION_INVALID", "訂正後の数量は1以上にしてください（なくす場合は取消にしてください）"}
		}
		loanOut, err := q.LoanOutstanding(r.Context(), loanID)
		if err != nil {
			return err
		}
		if loanOut < 0 {
			return &errTx{http.StatusConflict, "CORRECTION_INVALID", "返却の数が貸出の数を超えてしまうため訂正できません（先に返却を訂正してください）"}
		}
		itemOut, err := q.ItemOutstanding(r.Context(), item.ID)
		if err != nil {
			return err
		}
		if itemOut < 0 || (item.Mode == "quantity" && itemOut > int64(item.TotalQuantity)) {
			return &errTx{http.StatusConflict, "CORRECTION_INVALID", "在庫がマイナスになってしまうため訂正できません"}
		}
		// 個体管理: 貸出状態を有効数量に合わせる
		if isUnit {
			switch {
			case req.TargetType == "loan": // 貸出の取消 → 個体を空ける
				if err := q.ClearUnitLoan(r.Context(), loanID); err != nil {
					return err
				}
			case req.TargetType == "return" && loanOut > 0: // 返却の取消 → 個体を再び貸出中にする（他へ貸出中なら不可）
				n, err := q.SetUnitLoan(r.Context(), store.SetUnitLoanParams{ID: rawLoan.UnitID, ItemID: item.ID, CurrentLoanID: loanID})
				if err != nil {
					return err
				}
				if n != 1 {
					return &errTx{http.StatusConflict, "UNIT_UNAVAILABLE", "この個体は別の貸出で使われているため、返却を取り消せません"}
				}
			}
		}
		if err := s.auditEvent(r.Context(), q, a.Identity.UserID, a.Identity.UserID, "inventory.correction_created", map[string]any{"itemId": item.ID.String(), "targetType": req.TargetType, "targetId": targetID.String(), "kind": req.Kind}); err != nil {
			return err
		}
		return s.fillCorrection(r, q, item, ev, false, &out)
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, status, out)
}

func (s *server) fillCorrection(r *http.Request, q *store.Queries, item store.InventoryItem, ev store.InventoryCorrectionEvent, replayed bool, out *correctionResp) error {
	itemOut, err := q.ItemOutstanding(r.Context(), item.ID)
	if err != nil {
		return err
	}
	avail := int64(item.TotalQuantity) - itemOut
	if item.Mode == "individual" {
		if avail, err = availableUnits(r, q, item.ID); err != nil {
			return err
		}
	}
	*out = correctionResp{ID: ev.ID.String(), TargetType: ev.TargetType, TargetID: ev.TargetID.String(), Kind: ev.Kind, Reason: ev.Reason,
		CreatedAt: ev.CreatedAt.Time, Replayed: replayed, ItemOutstanding: itemOut, ItemAvailable: avail}
	if ev.DeltaQuantity.Valid {
		d := ev.DeltaQuantity.Int32
		out.DeltaQuantity = &d
	}
	return nil
}
