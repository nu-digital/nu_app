// Package session はCookieセッションを管理する。
//   - セッションIDは32バイトの暗号論的乱数。DBにはSHA-256のみ保存する。
//   - 絶対期限・アイドル期限・失効・停止ユーザーを毎回確認し、理由を区別せず ErrInvalid を返す。
//   - ロールは毎回DBから読む（剥奪は次のリクエストで反映される）。
//   - CSRFトークンはセッションtokenから導出する（秘密鍵不要。HttpOnlyのCookie値を知らない攻撃者は計算できない）。
package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nu-digital/nu_app/services/api/internal/authz"
	"github.com/nu-digital/nu_app/services/api/internal/store"
)

// Config は暫定値（DECISIONS P1-02-1）。
type Config struct {
	AbsoluteTTL   time.Duration
	IdleTTL       time.Duration
	TouchInterval time.Duration
}

var DefaultConfig = Config{AbsoluteTTL: 14 * 24 * time.Hour, IdleTTL: 7 * 24 * time.Hour, TouchInterval: time.Minute}

// ErrInvalid はセッションが無い・不正・期限切れ・失効・停止のいずれか（区別しない）。
var ErrInvalid = errors.New("セッションが無効です")

type Service struct {
	pool *pgxpool.Pool
	cfg  Config
	now  func() time.Time
}

func New(pool *pgxpool.Pool, cfg Config) *Service {
	return &Service{pool: pool, cfg: cfg, now: time.Now}
}

// WithClock はテスト用に時計を差し替えた複製を返す。
func (s *Service) WithClock(now func() time.Time) *Service { c := *s; c.now = now; return &c }

// Identity は検証済みセッションの持ち主。
type Identity struct {
	SessionID pgtype.UUID
	UserID    pgtype.UUID
	Roles     []authz.Role
	ExpiresAt time.Time
}

// Subject は認可表に渡す操作者。
func (i Identity) Subject() authz.Subject {
	return authz.Subject{Authenticated: true, Status: authz.StatusActive, Roles: i.Roles}
}

// Create は新しいセッションを発行し、Cookieに入れる平文tokenを返す。常に新規発行する（セッション固定対策）。
func (s *Service) Create(ctx context.Context, db store.DBTX, userID pgtype.UUID) (string, time.Time, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	sum := sha256.Sum256(raw)
	expires := s.now().Add(s.cfg.AbsoluteTTL)
	if _, err := store.New(db).CreateSession(ctx, store.CreateSessionParams{
		UserID: userID, TokenHash: sum[:], ExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true},
	}); err != nil {
		return "", time.Time{}, err
	}
	return base64.RawURLEncoding.EncodeToString(raw), expires, nil
}

func hashOf(tok string) ([]byte, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil || len(raw) != 32 {
		return nil, false
	}
	sum := sha256.Sum256(raw)
	return sum[:], true
}

// Validate はtokenを検証して持ち主を返す。last_seen は TouchInterval 以上空いたときだけ更新する。
func (s *Service) Validate(ctx context.Context, tok string) (Identity, error) {
	hash, ok := hashOf(tok)
	if !ok {
		return Identity{}, ErrInvalid
	}
	q := store.New(s.pool)
	row, err := q.GetSessionByTokenHash(ctx, hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Identity{}, ErrInvalid
		}
		return Identity{}, err
	}
	now := s.now()
	if row.RevokedAt.Valid || row.Status != "active" ||
		!now.Before(row.ExpiresAt.Time) || !now.Before(row.LastSeenAt.Time.Add(s.cfg.IdleTTL)) {
		return Identity{}, ErrInvalid
	}
	if now.Sub(row.LastSeenAt.Time) >= s.cfg.TouchInterval {
		if err := q.TouchSession(ctx, store.TouchSessionParams{ID: row.ID, LastSeenAt: pgtype.Timestamptz{Time: now, Valid: true}}); err != nil {
			return Identity{}, err
		}
	}
	roles, err := q.ListUserRoles(ctx, row.UserID)
	if err != nil {
		return Identity{}, err
	}
	id := Identity{SessionID: row.ID, UserID: row.UserID, ExpiresAt: row.ExpiresAt.Time}
	for _, r := range roles {
		id.Roles = append(id.Roles, authz.Role(r))
	}
	return id, nil
}

// Revoke は1つのセッションを失効する（ログアウト）。存在しなくてもエラーにしない。
func (s *Service) Revoke(ctx context.Context, tok string) error {
	hash, ok := hashOf(tok)
	if !ok {
		return nil
	}
	return store.New(s.pool).RevokeSessionByTokenHash(ctx, hash)
}

// RevokeAll はユーザーの全セッションを失効する。停止・パスワード再設定のトランザクション内で使う。
func RevokeAll(ctx context.Context, db store.DBTX, userID pgtype.UUID) (int64, error) {
	return store.New(db).RevokeAllUserSessions(ctx, userID)
}

const csrfPrefix = "nu-csrf-v1:"

// CSRFToken はセッションtokenから導出するCSRFトークン。
func CSRFToken(sessionToken string) string {
	sum := sha256.Sum256([]byte(csrfPrefix + sessionToken))
	return hex.EncodeToString(sum[:])
}

// VerifyCSRF は提示されたトークンがセッションに対応するかを定数時間で確認する。
func VerifyCSRF(sessionToken, presented string) bool {
	want := CSRFToken(sessionToken)
	return subtle.ConstantTimeCompare([]byte(want), []byte(presented)) == 1
}
