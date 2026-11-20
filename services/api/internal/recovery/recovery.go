// Package recovery はOwnerの緊急復旧（運用責任者が管理CLIで行う手順）を実装する。
//
// 方針（D05）:
//   - 公開Web/APIに入口は無い。nuctl（サーバー上のCLI）だけが呼ぶ。GitHub Actionsのデプロイからは呼ばない（テストで検査）。
//   - 承認メモ（本人確認と承認の記録）と対象IDの再入力を必須にする。
//   - 対象が既存のOwnerなら、全セッション・未使用トークンを失効してから、新しいトークンを発行する。
//   - activeなOwnerが1人もいない場合に限り、既存ユーザーをOwnerに指名できる。activeなOwnerが居るのに別ユーザーをOwnerにすることはできない。
//   - 実行は emergency_recovery_events と監査ログに同一トランザクションで残す（トークン・パスワードは記録しない）。
package recovery

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nu-digital/nu_app/services/api/internal/auth/session"
	"github.com/nu-digital/nu_app/services/api/internal/auth/token"
	"github.com/nu-digital/nu_app/services/api/internal/store"
)

var (
	ErrInvalidInput = errors.New("入力が不正です")
	ErrNotFound     = errors.New("対象のユーザーが見つかりません")
	// ErrNotAllowed は、activeなOwnerが居るのに別ユーザーをOwnerにしようとしたとき。
	ErrNotAllowed = errors.New("activeなOwnerが存在するため、Ownerではないユーザーを復旧の対象にできません")
)

// TokenTTL は復旧トークンの有効期間（暫定。再設定と同じ24時間）。
const TokenTTL = 24 * time.Hour

type Params struct {
	LoginID        string
	ConfirmLoginID string
	// ApproverNote は運用責任者の本人確認と承認の記録（必須）。
	ApproverNote string
	// OSUser は実行したOSユーザー名（記録用）。
	OSUser string
}

type Result struct {
	// Token は一度だけ表示する平文トークン。DBにはSHA-256のみ。
	Token string
	// Purpose は invite（パスワード未設定）または password_reset。
	Purpose   string
	ExpiresAt time.Time
	// Designated は、全Ownerが不在のため指名によってOwnerにした場合にtrue。
	Designated bool
}

// RecoverOwner は緊急復旧を行う。
func RecoverOwner(ctx context.Context, pool *pgxpool.Pool, p Params) (Result, error) {
	return recoverOwner(ctx, pool, p, time.Now)
}

func recoverOwner(ctx context.Context, pool *pgxpool.Pool, p Params, now func() time.Time) (Result, error) {
	loginID := strings.ToLower(strings.TrimSpace(p.LoginID))
	note := strings.TrimSpace(p.ApproverNote)
	if loginID == "" || strings.ToLower(strings.TrimSpace(p.ConfirmLoginID)) != loginID {
		return Result{}, ErrInvalidInput
	}
	if note == "" || utf8.RuneCountInString(note) > 500 || strings.TrimSpace(p.OSUser) == "" {
		return Result{}, ErrInvalidInput
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := store.New(tx)

	// Ownerの変更を直列化する（移譲・停止との競合を避ける）。
	if _, err := q.LockOwnerRows(ctx); err != nil {
		return Result{}, err
	}
	u, err := q.GetUserByLoginIDForUpdate(ctx, loginID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Result{}, ErrNotFound
		}
		return Result{}, err
	}
	isOwner, err := q.HasRole(ctx, store.HasRoleParams{UserID: u.ID, Role: "owner"})
	if err != nil {
		return Result{}, err
	}
	activeOwners, err := q.CountActiveOwners(ctx)
	if err != nil {
		return Result{}, err
	}
	designated := false
	if !isOwner {
		if activeOwners > 0 {
			return Result{}, ErrNotAllowed
		}
		// 全Ownerが不在: 指名されたユーザーをOwnerにする（承認メモが必須）。
		if _, err := q.GrantRoleIfMissing(ctx, store.GrantRoleIfMissingParams{UserID: u.ID, Role: "owner"}); err != nil {
			return Result{}, err
		}
		designated = true
	}

	// 復旧対象の既存セッション・未使用トークンを失効し、停止中なら有効に戻す。
	if _, err := session.RevokeAll(ctx, tx, u.ID); err != nil {
		return Result{}, err
	}
	if _, err := q.RevokeActiveTokens(ctx, store.RevokeActiveTokensParams{UserID: u.ID, Column2: ""}); err != nil {
		return Result{}, err
	}
	hasPw, err := q.HasPassword(ctx, u.ID)
	if err != nil {
		return Result{}, err
	}
	next, purpose := "invited", token.PurposeInvite
	if hasPw {
		next, purpose = "active", token.PurposePasswordReset
	}
	if u.Status == "disabled" { // 停止中なら有効に戻す（パスワード設定済みならactive、未設定ならinvited）
		if err := q.SetUserEnabled(ctx, store.SetUserEnabledParams{ID: u.ID, Status: next}); err != nil {
			return Result{}, err
		}
	}
	raw, expires, err := token.Issue(ctx, tx, u.ID, purpose, TokenTTL, pgtype.UUID{}, now())
	if err != nil {
		return Result{}, err
	}
	if err := q.InsertRecoveryEvent(ctx, store.InsertRecoveryEventParams{ExecutedByOsUser: p.OSUser, TargetUserID: u.ID, ApproverNote: note}); err != nil {
		return Result{}, err
	}
	summary, _ := json.Marshal(map[string]any{"login_id": loginID, "designated": designated, "via": "nuctl"}) // 承認メモ・tokenは入れない
	if err := q.InsertAuditLog(ctx, store.InsertAuditLogParams{
		Action: "recovery.owner_recovered", TargetType: pgtype.Text{String: "user", Valid: true},
		TargetID: pgtype.Text{String: u.ID.String(), Valid: true}, SummaryJson: summary,
	}); err != nil {
		return Result{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	return Result{Token: raw, Purpose: purpose, ExpiresAt: expires, Designated: designated}, nil
}
