// Package bootstrap は初期Owner(最初の管理者)の作成を行う。公開API・Webからは呼ばれず、管理CLI(nuctl)だけが使う。
//
// 安全性:
//   - 作成は1回だけ。bootstrap_state(最大1行)の確保とユーザー作成を同一トランザクションで行い、
//     並行実行・再実行・別ログインIDでの再実行は必ず拒否される。
//   - 失敗(不正なログインID等)ではトランザクションが巻き戻り、bootstrapは消費されない。
//   - 招待tokenは暗号論的乱数で、DBにはSHA-256のみ保存する。平文は呼び出し元へ一度だけ返し、ログ・監査に残さない。
package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nu-digital/nu_app/services/api/internal/auth/token"
	"github.com/nu-digital/nu_app/services/api/internal/store"
)

var (
	// ErrAlreadyBootstrapped は初期Ownerが既に作成済み(または他で作成中)のとき。
	ErrAlreadyBootstrapped = errors.New("初期Ownerは既に作成されています")
	// ErrOwnerExists はbootstrap_stateが未記録でもownerが既に存在するとき（想定外の状態。作成しない）。
	ErrOwnerExists  = errors.New("Ownerが既に存在するため作成できません")
	ErrInvalidInput = errors.New("入力が不正です")
)

// DefaultTokenTTL は初期Owner用の招待tokenの既定の有効期間。運用者が同席して作る前提で短くしている。
const DefaultTokenTTL = 24 * time.Hour

type Params struct {
	LoginID     string
	DisplayName string
	TokenTTL    time.Duration
}

type Result struct {
	UserID pgtype.UUID
	// InviteToken は一度だけ表示する平文token。DBにはhashのみ。
	InviteToken string
	ExpiresAt   time.Time
}

// Owner は初期Ownerを作成する。
func Owner(ctx context.Context, pool *pgxpool.Pool, p Params) (Result, error) {
	return owner(ctx, pool, p, time.Now)
}

func owner(ctx context.Context, pool *pgxpool.Pool, p Params, now func() time.Time) (Result, error) {
	name := strings.TrimSpace(p.DisplayName)
	if name == "" || len([]rune(name)) > 64 {
		return Result{}, fmt.Errorf("%w: 表示名は1〜64文字で入力してください", ErrInvalidInput)
	}
	ttl := p.TokenTTL
	if ttl <= 0 {
		ttl = DefaultTokenTTL
	}
	tok, hash, err := token.New()
	if err != nil {
		return Result{}, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := store.New(tx)

	// 1回だけ確保する。既に確保済みなら（並行中の別トランザクションのコミット後も含め）行が返らない。
	if _, err := q.ClaimBootstrap(ctx); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Result{}, ErrAlreadyBootstrapped
		}
		return Result{}, err
	}
	if n, err := q.CountUsersWithRole(ctx, "owner"); err != nil {
		return Result{}, err
	} else if n > 0 {
		return Result{}, ErrOwnerExists
	}

	user, err := q.CreateUser(ctx, store.CreateUserParams{LoginID: p.LoginID, Status: "invited"})
	if err != nil {
		// ログインID形式違反・一意制約違反はここで出る。bootstrapは巻き戻されて消費されない。
		return Result{}, fmt.Errorf("%w: ログインIDを確認してください", ErrInvalidInput)
	}
	if err := q.CreateProfile(ctx, store.CreateProfileParams{UserID: user.ID, DisplayName: name}); err != nil {
		return Result{}, err
	}
	// assigned_by は NULL（CLIによる作成）。
	if err := q.GrantRole(ctx, store.GrantRoleParams{UserID: user.ID, Role: "owner"}); err != nil {
		return Result{}, err
	}
	expires := now().Add(ttl)
	if err := q.CreateOneTimeToken(ctx, store.CreateOneTimeTokenParams{
		UserID: user.ID, Purpose: "invite", TokenHash: hash,
		ExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true},
	}); err != nil {
		return Result{}, err
	}
	// 監査には秘密値（token・パスワード）を入れない。
	summary, _ := json.Marshal(map[string]string{"login_id": p.LoginID, "via": "nuctl"})
	if err := q.InsertAuditLog(ctx, store.InsertAuditLogParams{
		Action: "bootstrap.owner_created", TargetType: pgtype.Text{String: "user", Valid: true},
		TargetID: pgtype.Text{String: uuidString(user.ID), Valid: true}, SummaryJson: summary,
	}); err != nil {
		return Result{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	return Result{UserID: user.ID, InviteToken: tok, ExpiresAt: expires}, nil
}

// HashToken は受け取ったtoken文字列のSHA-256を返す（検証側で使う）。不正な形式ならfalse。
func HashToken(tok string) ([]byte, bool) { return token.Hash(tok) }

func uuidString(u pgtype.UUID) string {
	b := u.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
