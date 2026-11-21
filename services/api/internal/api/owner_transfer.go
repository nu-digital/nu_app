package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nu-digital/nu_app/services/api/internal/authz"
	"github.com/nu-digital/nu_app/services/api/internal/httpx"
	"github.com/nu-digital/nu_app/services/api/internal/store"
)

// transferTTL はOwner移譲申請の有効期限（D05: 24時間）。
const transferTTL = 24 * time.Hour

type transferView struct {
	ID        string    `json:"id"`
	ToUserID  string    `json:"toUserId,omitempty"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// ownerOf は、userIDがactiveで owner ロールを持つか。
func ownerActive(r *http.Request, q *store.Queries, id pgtype.UUID) (bool, error) {
	u, err := q.GetUserByID(r.Context(), id)
	if err != nil {
		return false, err
	}
	if u.Status != "active" {
		return false, nil
	}
	return q.HasRole(r.Context(), store.HasRoleParams{UserID: id, Role: "owner"})
}

// createTransfer は POST /owner/transfers（申請）。
func (s *server) createTransfer(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "owner.transfers.create", authz.Resource{})
	if !ok {
		return
	}
	var req struct {
		ToUserID string `json:"toUserId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	var to pgtype.UUID
	if err := to.Scan(req.ToUserID); err != nil || !to.Valid {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_REQUEST", "移譲先のユーザーを指定してください")
		return
	}
	var out transferView
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		// Ownerの変更を直列化する（申請の時点でも、申請者が現在もactiveなOwnerであることを確定させる）。
		if _, err := q.LockOwnerRows(r.Context()); err != nil {
			return err
		}
		if isOwner, err := ownerActive(r, q, a.Identity.UserID); err != nil {
			return err
		} else if !isOwner {
			return &errTx{http.StatusForbidden, "FORBIDDEN", "この操作を行う権限がありません"}
		}
		if to == a.Identity.UserID {
			return &errTx{http.StatusUnprocessableEntity, "INVALID_TARGET", "自分自身には移譲できません"}
		}
		target, err := q.GetUserByID(r.Context(), to)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return &errTx{http.StatusUnprocessableEntity, "INVALID_TARGET", "移譲先は有効なユーザーを指定してください"}
			}
			return err
		}
		if target.Status != "active" {
			return &errTx{http.StatusUnprocessableEntity, "INVALID_TARGET", "移譲先は有効なユーザーを指定してください"}
		}
		if already, err := q.HasRole(r.Context(), store.HasRoleParams{UserID: to, Role: "owner"}); err != nil {
			return err
		} else if already {
			return &errTx{http.StatusUnprocessableEntity, "INVALID_TARGET", "移譲先は既にOwnerです"}
		}
		now := s.Now()
		if _, err := q.ExpireOwnerTransfers(r.Context(), store.ExpireOwnerTransfersParams{FromUserID: a.Identity.UserID, ResolvedAt: pgtype.Timestamptz{Time: now, Valid: true}}); err != nil {
			return err
		}
		t, err := q.CreateOwnerTransfer(r.Context(), store.CreateOwnerTransferParams{
			FromUserID: a.Identity.UserID, ToUserID: to,
			RequestedAt: pgtype.Timestamptz{Time: now, Valid: true}, ExpiresAt: pgtype.Timestamptz{Time: now.Add(transferTTL), Valid: true},
		})
		if err != nil {
			var pe *pgconn.PgError
			if errors.As(err, &pe) && pe.Code == "23505" {
				return &errTx{http.StatusConflict, "TRANSFER_PENDING", "未処理の移譲申請が既にあります。取り消してから申請してください"}
			}
			return err
		}
		out = transferView{ID: t.ID.String(), ToUserID: to.String(), Status: "pending", ExpiresAt: t.ExpiresAt.Time}
		return s.audit(r.Context(), q, a.Identity.UserID, to, "owner.transfer_requested", target.LoginID)
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

// loadTransfer は申請をロックして取得する。IDが不正・存在しない場合は404。
func (s *server) loadTransfer(r *http.Request, q *store.Queries) (store.OwnerTransferRequest, error) {
	var id pgtype.UUID
	if err := id.Scan(r.PathValue("id")); err != nil || !id.Valid {
		return store.OwnerTransferRequest{}, &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"}
	}
	t, err := q.GetOwnerTransferForUpdate(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.OwnerTransferRequest{}, &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"}
		}
		return store.OwnerTransferRequest{}, err
	}
	return t, nil
}

// expireIfNeeded は期限切れのpendingをexpiredにして、そのことを呼び出し側へ知らせる。
func (s *server) expireIfNeeded(r *http.Request, q *store.Queries, t *store.OwnerTransferRequest) (bool, error) {
	now := s.Now()
	if t.Status == "pending" && !now.Before(t.ExpiresAt.Time) {
		if err := q.ResolveOwnerTransfer(r.Context(), store.ResolveOwnerTransferParams{ID: t.ID, Status: "expired", ResolvedAt: pgtype.Timestamptz{Time: now, Valid: true}}); err != nil {
			return false, err
		}
		t.Status = "expired"
		return true, nil
	}
	return false, nil
}

var errTransferGone = &errTx{http.StatusConflict, "TRANSFER_NOT_PENDING", "この移譲申請は既に処理済みか、期限切れです"}

// cancelTransfer は POST /owner/transfers/{id}/cancel（申請者のみ）。
func (s *server) cancelTransfer(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	var out transferView
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		if _, err := q.LockOwnerRows(r.Context()); err != nil {
			return err
		}
		t, err := s.loadTransfer(r, q)
		if err != nil {
			return err
		}
		// 申請者以外には存在を隠す（404）。申請者でもOwner権限が必要（認可表）。
		if t.FromUserID != a.Identity.UserID {
			return &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"}
		}
		if err := deny(authz.Decide(a.Identity.Subject(), "owner.transfers.cancel", authz.Resource{IsRequester: true})); err != nil {
			return err
		}
		if expired, err := s.expireIfNeeded(r, q, &t); err != nil {
			return err
		} else if expired {
			return errExpiredCommit // 失効の記録は呼び出し側が別トランザクションで確定させる
		}
		if t.Status != "pending" {
			return errTransferGone
		}
		if err := q.ResolveOwnerTransfer(r.Context(), store.ResolveOwnerTransferParams{ID: t.ID, Status: "cancelled", ResolvedAt: pgtype.Timestamptz{Time: s.Now(), Valid: true}}); err != nil {
			return err
		}
		out = transferView{ID: t.ID.String(), Status: "cancelled", ExpiresAt: t.ExpiresAt.Time}
		return s.audit(r.Context(), q, a.Identity.UserID, t.ToUserID, "owner.transfer_cancelled", "")
	})
	if errors.Is(err, errExpiredCommit) {
		s.recordExpiry(r)
		httpx.Error(w, r, http.StatusConflict, "TRANSFER_NOT_PENDING", errTransferGone.message)
		return
	}
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// errExpiredCommit は「失効を記録（コミット）してから409を返す」ための目印。inTxはエラー時にロールバックするため、
// 失効の記録は別の小さなトランザクションで確定する。
var errExpiredCommit = errors.New("expired")

// incomingTransfers は GET /owner/transfers/incoming（自分宛の未処理・未失効の申請）。
func (s *server) incomingTransfers(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "owner.transfers.incoming", authz.Resource{})
	if !ok {
		return
	}
	rows, err := store.New(s.Pool).ListIncomingOwnerTransfers(r.Context(), store.ListIncomingOwnerTransfersParams{
		ToUserID: a.Identity.UserID, ExpiresAt: pgtype.Timestamptz{Time: s.Now(), Valid: true},
	})
	if err != nil {
		s.internal(w, r)
		return
	}
	type item struct {
		ID              string    `json:"id"`
		FromLoginID     string    `json:"fromLoginId"`
		FromDisplayName string    `json:"fromDisplayName"`
		ExpiresAt       time.Time `json:"expiresAt"`
	}
	out := make([]item, 0, len(rows))
	for _, x := range rows {
		out = append(out, item{ID: x.ID.String(), FromLoginID: x.FromLoginID, FromDisplayName: x.FromDisplayName, ExpiresAt: x.ExpiresAt.Time})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": out})
}

// acceptTransfer は POST /owner/transfers/{id}/accept（移譲先本人のみ）。
// 権限の移譲は1つのトランザクションで確定する。Ownerが0人になる状態は作らない。
func (s *server) acceptTransfer(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	var roles []string
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		if _, err := q.LockOwnerRows(r.Context()); err != nil {
			return err
		}
		t, err := s.loadTransfer(r, q)
		if err != nil {
			return err
		}
		if t.ToUserID != a.Identity.UserID {
			return &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"} // 移譲先以外には存在を隠す
		}
		if err := deny(authz.Decide(a.Identity.Subject(), "owner.transfers.accept", authz.Resource{IsSelf: true})); err != nil {
			return err
		}
		if expired, err := s.expireIfNeeded(r, q, &t); err != nil {
			return err
		} else if expired {
			return errExpiredCommit
		}
		if t.Status != "pending" {
			return errTransferGone
		}
		// 申請者が今もactiveなOwnerで、移譲先が今もactiveで非Ownerであることを確定させる。
		if fromOwner, err := ownerActive(r, q, t.FromUserID); err != nil {
			return err
		} else if !fromOwner {
			return &errTx{http.StatusConflict, "TRANSFER_INVALID", "申請者が既にOwnerではないため承諾できません"}
		}
		if isOwner, err := q.HasRole(r.Context(), store.HasRoleParams{UserID: t.ToUserID, Role: "owner"}); err != nil {
			return err
		} else if isOwner {
			return &errTx{http.StatusConflict, "TRANSFER_INVALID", "既にOwnerです"}
		}
		now := pgtype.Timestamptz{Time: s.Now(), Valid: true}
		// 先に新しいOwnerを付与してから旧Ownerを降ろす（途中でもOwnerが0人にならない順序）。
		if _, err := q.GrantRoleIfMissing(r.Context(), store.GrantRoleIfMissingParams{UserID: t.ToUserID, Role: "owner", AssignedBy: t.FromUserID}); err != nil {
			return err
		}
		if n, err := q.RevokeRole(r.Context(), store.RevokeRoleParams{UserID: t.FromUserID, Role: "owner"}); err != nil {
			return err
		} else if n != 1 {
			return &errTx{http.StatusConflict, "TRANSFER_INVALID", "申請者が既にOwnerではないため承諾できません"}
		}
		// 旧OwnerはAdminとして残る（D05）。
		if _, err := q.GrantRoleIfMissing(r.Context(), store.GrantRoleIfMissingParams{UserID: t.FromUserID, Role: "admin", AssignedBy: t.FromUserID}); err != nil {
			return err
		}
		if err := q.ResolveOwnerTransfer(r.Context(), store.ResolveOwnerTransferParams{ID: t.ID, Status: "accepted", ResolvedAt: now}); err != nil {
			return err
		}
		if n, err := q.CountActiveOwners(r.Context()); err != nil {
			return err
		} else if n < 1 {
			return &errTx{http.StatusInternalServerError, "INTERNAL", "サーバーでエラーが発生しました"} // 到達しない（保険）。巻き戻る
		}
		if err := s.audit(r.Context(), q, a.Identity.UserID, t.FromUserID, "owner.transfer_accepted", ""); err != nil {
			return err
		}
		roles, err = q.ListUserRoles(r.Context(), t.ToUserID)
		return err
	})
	if errors.Is(err, errExpiredCommit) {
		// 失効の記録をコミットする（上のトランザクションはロールバックされたため、別に確定させる）。
		s.recordExpiry(r)
		httpx.Error(w, r, http.StatusConflict, "TRANSFER_NOT_PENDING", errTransferGone.message)
		return
	}
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	if roles == nil {
		roles = []string{}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"roles": roles})
}

// recordExpiry は期限切れのpending申請をexpiredとして確定する（取消・承諾が期限切れで拒否されたとき）。
func (s *server) recordExpiry(r *http.Request) {
	_ = s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		t, err := s.loadTransfer(r, q)
		if err != nil {
			return nil
		}
		_, err = s.expireIfNeeded(r, q, &t)
		return err
	})
}

// requireSession は認証済みであることだけを確認する（関係に依存する認可は、対象の申請を読んだあとで行う）。
func (s *server) requireSession(w http.ResponseWriter, r *http.Request) (authCtx, bool) {
	a, ok := authFrom(r.Context())
	if !ok {
		httpx.Error(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "ログインが必要です")
		return authCtx{}, false
	}
	return a, true
}
