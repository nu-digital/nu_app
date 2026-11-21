// Package token は招待・再設定の使い捨てトークンを扱う。
//   - 32バイトの暗号論的乱数。DBにはSHA-256のみ保存し、平文は発行時に一度だけ返す。
//   - 使用は1つの条件付きUPDATEでアトミックに確保する（同時に使っても成功は1件）。
//   - 発行時に同じ(user,purpose)の有効なトークンを失効させる（再発行）。
package token

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nu-digital/nu_app/services/api/internal/store"
)

const (
	PurposeInvite        = "invite"
	PurposePasswordReset = "password_reset"
)

var (
	// ErrInvalid は無効・期限切れ・使用済み・失効済みを区別せずに表す（理由を利用者に教えない）。
	ErrInvalid = errors.New("リンクが無効または期限切れです")
	// ErrConflict は同じ(user,purpose)の再発行が同時に走って衝突したとき。呼び出し側は409にする。
	ErrConflict = errors.New("同時に発行されたため、もう一度お試しください")
)

// New は新しいtoken(URL安全なbase64、43文字)とそのSHA-256を返す。
func New() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(raw)
	return base64.RawURLEncoding.EncodeToString(raw), sum[:], nil
}

// Hash は受け取ったtoken文字列のSHA-256を返す。形式が不正ならfalse。
func Hash(tok string) ([]byte, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil || len(raw) != 32 {
		return nil, false
	}
	sum := sha256.Sum256(raw)
	return sum[:], true
}

// Issue は(user,purpose)の有効なトークンを失効させて新しいトークンを発行する。呼び出し側のトランザクション内で使う。
// createdBy が無効(NULL)なら発行者なし（CLI）。
func Issue(ctx context.Context, db store.DBTX, userID pgtype.UUID, purpose string, ttl time.Duration, createdBy pgtype.UUID, now time.Time) (string, time.Time, error) {
	q := store.New(db)
	raw, hash, err := New()
	if err != nil {
		return "", time.Time{}, err
	}
	if _, err := q.RevokeActiveTokens(ctx, store.RevokeActiveTokensParams{UserID: userID, Column2: purpose}); err != nil {
		return "", time.Time{}, err
	}
	expires := now.Add(ttl)
	err = q.CreateOneTimeToken(ctx, store.CreateOneTimeTokenParams{
		UserID: userID, Purpose: purpose, TokenHash: hash,
		ExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true}, CreatedBy: createdBy,
	})
	if err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" {
			return "", time.Time{}, ErrConflict
		}
		return "", time.Time{}, err
	}
	return raw, expires, nil
}

// Consume はトークンを使用済みにして所有者のユーザーIDを返す。呼び出し側のトランザクション内で使い、
// 後続の処理（パスワード保存など）が失敗したらトランザクションごと巻き戻してトークンを消費しない。
func Consume(ctx context.Context, db store.DBTX, tok, purpose string, now time.Time) (pgtype.UUID, error) {
	hash, ok := Hash(tok)
	if !ok {
		return pgtype.UUID{}, ErrInvalid
	}
	row, err := store.New(db).ConsumeToken(ctx, store.ConsumeTokenParams{
		TokenHash: hash, Purpose: purpose, UsedAt: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pgtype.UUID{}, ErrInvalid
		}
		return pgtype.UUID{}, err
	}
	return row.UserID, nil
}
