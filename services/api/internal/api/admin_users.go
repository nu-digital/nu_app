package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nu-digital/nu_app/services/api/internal/auth/session"
	"github.com/nu-digital/nu_app/services/api/internal/auth/token"
	"github.com/nu-digital/nu_app/services/api/internal/authz"
	"github.com/nu-digital/nu_app/services/api/internal/httpx"
	"github.com/nu-digital/nu_app/services/api/internal/store"
)

// 暫定値（DECISIONS P1-02-2）。
const (
	inviteTTL = 72 * time.Hour
	resetTTL  = 24 * time.Hour
)

var loginIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,62}$`)

type adminUserView struct {
	ID          string `json:"id"`
	LoginID     string `json:"loginId"`
	DisplayName string `json:"displayName,omitempty"`
	Status      string `json:"status"`
}

type linkResponse struct {
	User      *adminUserView `json:"user,omitempty"`
	URL       string         `json:"url"`
	ExpiresAt time.Time      `json:"expiresAt"`
}

// errTx はトランザクション内で「レスポンスを決めて巻き戻す」ためのエラー。
type errTx struct {
	status  int
	code    string
	message string
}

func (e *errTx) Error() string { return e.code }

func (s *server) writeTxErr(w http.ResponseWriter, r *http.Request, err error) {
	var te *errTx
	if errors.As(err, &te) {
		httpx.Error(w, r, te.status, te.code, te.message)
		return
	}
	if errors.Is(err, token.ErrConflict) {
		httpx.Error(w, r, http.StatusConflict, "CONFLICT", token.ErrConflict.Error())
		return
	}
	s.internal(w, r)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			httpx.Error(w, r, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "送信するデータが大きすぎます")
		} else {
			httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", "入力内容を確認してください")
		}
		return false
	}
	return true
}

func parseUserID(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	var id pgtype.UUID
	if err := id.Scan(r.PathValue("id")); err != nil || !id.Valid {
		httpx.Error(w, r, http.StatusNotFound, "NOT_FOUND", "見つかりません")
		return pgtype.UUID{}, false
	}
	return id, true
}

func (s *server) audit(ctx context.Context, q *store.Queries, actor, target pgtype.UUID, action, loginID string) error {
	summary, _ := json.Marshal(map[string]string{"login_id": loginID}) // token・URLは入れない
	return q.InsertAuditLog(ctx, store.InsertAuditLogParams{
		ActorUserID: actor, Action: action, TargetType: pgtype.Text{String: "user", Valid: true},
		TargetID: pgtype.Text{String: target.String(), Valid: true}, SummaryJson: summary,
	})
}

// privilege は対象ユーザーの管理者権限（Ownerは両方true）。トランザクション内で読む。
func privilege(ctx context.Context, q *store.Queries, id pgtype.UUID) (authz.Resource, error) {
	roles, err := q.ListUserRoles(ctx, id)
	if err != nil {
		return authz.Resource{}, err
	}
	var res authz.Resource
	for _, ro := range roles {
		switch ro {
		case "owner":
			res.TargetIsOwner, res.TargetIsAdmin = true, true
		case "admin":
			res.TargetIsAdmin = true
		}
	}
	return res, nil
}

// deny は認可表の拒否理由を、トランザクション内のエラーに変換する。
func deny(d authz.Decision) error {
	if d.Allowed {
		return nil
	}
	if d.Reason == authz.ReasonUnauthed || d.Reason == authz.ReasonInactive {
		return &errTx{http.StatusUnauthorized, "UNAUTHENTICATED", "ログインが必要です"}
	}
	return &errTx{http.StatusForbidden, "FORBIDDEN", "この操作を行う権限がありません"}
}

// createUser は POST /admin/users。
func (s *server) createUser(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "admin.users.create", authz.Resource{})
	if !ok {
		return
	}
	var req struct {
		LoginID     string `json:"loginId"`
		DisplayName string `json:"displayName"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	loginID := strings.TrimSpace(req.LoginID)
	name := strings.TrimSpace(req.DisplayName)
	if !loginIDPattern.MatchString(loginID) {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_LOGIN_ID", "ログインIDは小文字英数字と . _ - の2〜63文字（先頭は英数字）で入力してください")
		return
	}
	if name == "" || utf8.RuneCountInString(name) > 64 {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_DISPLAY_NAME", "表示名は1〜64文字で入力してください")
		return
	}
	var resp linkResponse
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		u, err := q.CreateUser(r.Context(), store.CreateUserParams{LoginID: loginID, Status: "invited"})
		if err != nil {
			var pe *pgconn.PgError
			if errors.As(err, &pe) && pe.Code == "23505" {
				return &errTx{http.StatusConflict, "LOGIN_ID_TAKEN", "そのログインIDは既に使われています"}
			}
			return err
		}
		if err := q.CreateProfile(r.Context(), store.CreateProfileParams{UserID: u.ID, DisplayName: name}); err != nil {
			return err
		}
		raw, exp, err := token.Issue(r.Context(), tx, u.ID, token.PurposeInvite, inviteTTL, a.Identity.UserID, s.Now())
		if err != nil {
			return err
		}
		resp = linkResponse{User: &adminUserView{ID: u.ID.String(), LoginID: loginID, DisplayName: name, Status: "invited"}, URL: s.AppBaseURLFor("invite", raw), ExpiresAt: exp}
		return s.audit(r.Context(), q, a.Identity.UserID, u.ID, "admin.user_created", loginID)
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, resp)
}

// AppBaseURLFor は招待・再設定URL（トークンはフラグメントに入れ、アクセスログやRefererに残さない）。
func (s *server) AppBaseURLFor(kind, tok string) string {
	return s.Cfg.AppBaseURL + "/" + kind + "#token=" + tok
}

func (s *server) inTx(ctx context.Context, f func(q *store.Queries, tx pgx.Tx) error) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := f(store.New(tx), tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// issueLink は invite-link / reset-link の共通処理。requiredStatus のユーザーにだけ発行できる。
func (s *server) issueLink(w http.ResponseWriter, r *http.Request, action, kind, purpose, requiredStatus string, ttl time.Duration, auditAction string) {
	a, ok := s.authorize(w, r, action, authz.Resource{})
	if !ok {
		return
	}
	target, ok := parseUserID(w, r)
	if !ok {
		return
	}
	var resp linkResponse
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		u, err := q.GetUserForUpdate(r.Context(), target)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"}
			}
			return err
		}
		// 管理者権限を持つ対象はOwnerのみ（同じトランザクションで対象の権限を読んで判定する）
		res, err := privilege(r.Context(), q, target)
		if err != nil {
			return err
		}
		if err := deny(authz.Decide(a.Identity.Subject(), action, res)); err != nil {
			return err
		}
		if u.Status != requiredStatus {
			return &errTx{http.StatusConflict, "INVALID_STATE", "このユーザーの状態ではリンクを発行できません"}
		}
		raw, exp, err := token.Issue(r.Context(), tx, target, purpose, ttl, a.Identity.UserID, s.Now())
		if err != nil {
			return err
		}
		resp = linkResponse{URL: s.AppBaseURLFor(kind, raw), ExpiresAt: exp}
		return s.audit(r.Context(), q, a.Identity.UserID, target, auditAction, u.LoginID)
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func (s *server) inviteLink(w http.ResponseWriter, r *http.Request) {
	s.issueLink(w, r, "admin.users.invite_link", "invite", token.PurposeInvite, "invited", inviteTTL, "admin.invite_link_issued")
}

func (s *server) resetLink(w http.ResponseWriter, r *http.Request) {
	s.issueLink(w, r, "admin.users.reset_link", "reset", token.PurposePasswordReset, "active", resetTTL, "admin.reset_link_issued")
}

// setStatus は PATCH /admin/users/{id}/status。
//
// 停止: 全セッションと未使用トークンを失効（再有効化しても古いトークンは復活しない）。自分自身の停止は不可。
// 最後のOwnerの停止は拒否する。Owner行をロックしたトランザクションで判定するため、
// 2人のOwnerが同時に互いを停止しても、必ず1人は残る。
// 再有効化: パスワード設定済みならactive、未設定ならinvited（新しい招待リンクが必要）。
func (s *server) setStatus(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "admin.users.status", authz.Resource{})
	if !ok {
		return
	}
	target, ok := parseUserID(w, r)
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Status != "disabled" && req.Status != "active" {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_STATUS", "状態は disabled か active を指定してください")
		return
	}
	var out adminUserView
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		// 先にOwner行をロックする（ロック順を固定してデッドロックを避ける）。
		if _, err := q.LockOwnerRows(r.Context()); err != nil {
			return err
		}
		u, err := q.GetUserForUpdate(r.Context(), target)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"}
			}
			return err
		}
		res, err := privilege(r.Context(), q, target)
		if err != nil {
			return err
		}
		if err := deny(authz.Decide(a.Identity.Subject(), "admin.users.status", res)); err != nil {
			return err
		}
		out = adminUserView{ID: u.ID.String(), LoginID: u.LoginID, Status: u.Status}

		if req.Status == "disabled" {
			if target == a.Identity.UserID {
				return &errTx{http.StatusConflict, "CANNOT_DISABLE_SELF", "自分自身は停止できません"}
			}
			if u.Status == "disabled" {
				return nil // 冪等
			}
			if res.TargetIsOwner {
				if s.BeforeOwnerCount != nil {
					s.BeforeOwnerCount() // テスト専用: 判定の直前で止めて、並行トランザクションとの競合を確実に起こす
				}
				n, err := q.CountActiveOwnersExcept(r.Context(), target)
				if err != nil {
					return err
				}
				if n == 0 {
					return &errTx{http.StatusConflict, "LAST_OWNER", "最後のOwnerは停止できません"}
				}
			}
			if err := q.SetUserDisabled(r.Context(), target); err != nil {
				return err
			}
			if _, err := session.RevokeAll(r.Context(), tx, target); err != nil {
				return err
			}
			if _, err := q.RevokeActiveTokens(r.Context(), store.RevokeActiveTokensParams{UserID: target, Column2: ""}); err != nil {
				return err
			}
			out.Status = "disabled"
			return s.audit(r.Context(), q, a.Identity.UserID, target, "admin.user_disabled", u.LoginID)
		}

		// 再有効化
		if u.Status != "disabled" {
			return nil // 冪等（既に有効/招待中）
		}
		hasPw, err := q.HasPassword(r.Context(), target)
		if err != nil {
			return err
		}
		next := "invited"
		if hasPw {
			next = "active"
		}
		if err := q.SetUserEnabled(r.Context(), store.SetUserEnabledParams{ID: target, Status: next}); err != nil {
			return err
		}
		out.Status = next
		return s.audit(r.Context(), q, a.Identity.UserID, target, "admin.user_enabled", u.LoginID)
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}
