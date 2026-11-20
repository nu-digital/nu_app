package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nu-digital/nu_app/services/api/internal/auth/password"
	"github.com/nu-digital/nu_app/services/api/internal/auth/session"
	"github.com/nu-digital/nu_app/services/api/internal/auth/token"
	"github.com/nu-digital/nu_app/services/api/internal/httpx"
	"github.com/nu-digital/nu_app/services/api/internal/store"
)

type tokenPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

const invalidLinkMessage = "リンクが無効または期限切れです。管理者に再発行を依頼してください"

// acceptInvite は POST /auth/accept-invite。招待トークンでパスワードを設定し、アカウントを有効化する（自動ログインはしない）。
func (s *server) acceptInvite(w http.ResponseWriter, r *http.Request) {
	s.setPassword(w, r, token.PurposeInvite, "invited", "auth.invite_accepted")
}

// resetPassword は POST /auth/reset-password。再設定トークンでパスワードを更新し、全セッションを失効する。
func (s *server) resetPassword(w http.ResponseWriter, r *http.Request) {
	s.setPassword(w, r, token.PurposePasswordReset, "active", "auth.password_reset")
}

// setPassword は招待受諾・再設定の共通処理。
//
// 順序（docs/design/p1-02-auth.md）:
//  1. トークンを消費せずに確認（無効なら同一の400）。ここで方針検証に必要なログインIDを得る。
//  2. パスワード方針の検証と、Argon2idの計算を**トランザクションの前**に行う（方針違反・混雑でトークンを失わない）。
//  3. 1つのトランザクションで、トークンの使用の確保（条件付きUPDATE）・状態の確認・パスワード保存・
//     （受諾なら有効化、再設定なら全セッションと他トークンの失効）・監査を確定する。
func (s *server) setPassword(w http.ResponseWriter, r *http.Request, purpose, requiredStatus, auditAction string) {
	ctx := r.Context()
	var req tokenPasswordRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			httpx.Error(w, r, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "送信するデータが大きすぎます")
			return
		}
		httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", "入力内容を確認してください")
		return
	}
	hash, ok := token.Hash(req.Token)
	if !ok {
		httpx.Error(w, r, http.StatusBadRequest, "INVALID_LINK", invalidLinkMessage)
		return
	}
	q := store.New(s.Pool)
	peek, err := q.PeekToken(ctx, store.PeekTokenParams{TokenHash: hash, Purpose: purpose, ExpiresAt: pgtype.Timestamptz{Time: s.Now(), Valid: true}})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpx.Error(w, r, http.StatusBadRequest, "INVALID_LINK", invalidLinkMessage)
			return
		}
		s.internal(w, r)
		return
	}
	if peek.Status != requiredStatus {
		// 停止中・有効化済みなど。状態を変えず、無効なリンクと同じ応答にする（停止ユーザーを再有効化させない）。
		httpx.Error(w, r, http.StatusBadRequest, "INVALID_LINK", invalidLinkMessage)
		return
	}
	if len(req.Password) > maxPasswordLen {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "WEAK_PASSWORD", password.ErrTooLong.Error())
		return
	}
	if err := password.Validate(peek.LoginID, req.Password); err != nil {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "WEAK_PASSWORD", err.Error())
		return
	}
	enc, err := s.Hasher.Hash(req.Password)
	if err != nil {
		if errors.Is(err, password.ErrBusy) {
			w.Header().Set("Retry-After", "5")
			httpx.Error(w, r, http.StatusServiceUnavailable, "SERVICE_BUSY", "混み合っています。しばらくしてからお試しください")
			return
		}
		s.internal(w, r)
		return
	}

	if err := s.commitPassword(ctx, req.Token, purpose, enc, auditAction); err != nil {
		if errors.Is(err, token.ErrInvalid) {
			httpx.Error(w, r, http.StatusBadRequest, "INVALID_LINK", invalidLinkMessage)
			return
		}
		s.internal(w, r)
		return
	}
	if purpose == token.PurposePasswordReset {
		s.Limiter.Reset(peek.LoginID) // 管理者の本人確認を経た再設定が済んだので、失敗カウントも消す
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) commitPassword(ctx context.Context, tok, purpose, encodedHash, auditAction string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := store.New(tx)

	userID, err := token.Consume(ctx, tx, tok, purpose, s.Now())
	if err != nil {
		return err // 同時に他の人が使った・失効した場合は ErrInvalid（ロールバックされる）
	}
	if purpose == token.PurposeInvite {
		// invited のユーザーだけを有効化する。停止中・有効化済みは0行になり、トランザクションごと巻き戻る。
		n, err := q.ActivateInvitedUser(ctx, store.ActivateInvitedUserParams{ID: userID, ActivatedAt: pgtype.Timestamptz{Time: s.Now(), Valid: true}})
		if err != nil {
			return err
		}
		if n != 1 {
			return token.ErrInvalid
		}
	} else {
		// 再設定は active のユーザーだけ（停止中は拒否）。ロックして状態を確定させる。
		u, err := q.GetUserByID(ctx, userID)
		if err != nil {
			return err
		}
		if u.Status != "active" {
			return token.ErrInvalid
		}
		if _, err := session.RevokeAll(ctx, tx, userID); err != nil {
			return err
		}
		if _, err := q.RevokeActiveTokens(ctx, store.RevokeActiveTokensParams{UserID: userID, Column2: ""}); err != nil {
			return err
		}
	}
	if err := q.UpsertPasswordHash(ctx, store.UpsertPasswordHashParams{UserID: userID, PasswordHash: encodedHash}); err != nil {
		return err
	}
	// 監査にはパスワード・トークンを入れない。
	if err := q.InsertAuditLog(ctx, store.InsertAuditLogParams{
		ActorUserID: userID, Action: auditAction, TargetType: pgtype.Text{String: "user", Valid: true},
		TargetID: pgtype.Text{String: userID.String(), Valid: true}, SummaryJson: []byte(`{}`),
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *server) internal(w http.ResponseWriter, r *http.Request) {
	s.Log.Error("内部エラー", "requestId", httpx.RequestID(r.Context()))
	httpx.Error(w, r, http.StatusInternalServerError, "INTERNAL", "サーバーでエラーが発生しました")
}
