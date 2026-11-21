// Package password はパスワードのハッシュ(Argon2id)と方針検査を提供する。平文は保存・ログ出力しない。
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Params はArgon2idのパラメータ。保存文字列に含まれるため、変更しても既存ハッシュは検証できる（NeedsRehashで移行）。
type Params struct {
	MemoryKiB uint32
	Time      uint32
	Threads   uint8
	SaltLen   uint32
	KeyLen    uint32
}

// DefaultParams は暫定値（docs/design/p1-02-auth.md、DECISIONS P1-02-3）。
var DefaultParams = Params{MemoryKiB: 64 * 1024, Time: 3, Threads: 1, SaltLen: 16, KeyLen: 32}

// Hasher はハッシュ計算の同時実行数を制限する（メモリを大量に使うため、4GB環境を守る）。
type Hasher struct {
	params Params
	sem    chan struct{}
}

func NewHasher(p Params, maxConcurrent int) *Hasher {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &Hasher{params: p, sem: make(chan struct{}, maxConcurrent)}
}

func (h *Hasher) acquire() func() {
	h.sem <- struct{}{}
	return func() { <-h.sem }
}

// Hash は "$argon2id$v=19$m=…,t=…,p=…$<salt>$<hash>" 形式の文字列を返す。
func (h *Hasher) Hash(pw string) (string, error) {
	salt := make([]byte, h.params.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	defer h.acquire()()
	key := argon2.IDKey([]byte(pw), salt, h.params.Time, h.params.MemoryKiB, h.params.Threads, h.params.KeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, h.params.MemoryKiB, h.params.Time, h.params.Threads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// ErrMalformed は保存されたハッシュの形式が不正なとき。
var ErrMalformed = errors.New("パスワードハッシュの形式が不正です")

// Verify は pw が encoded と一致するかを返す（定数時間比較）。needsRehash は現行パラメータと異なる場合にtrue。
func (h *Hasher) Verify(encoded, pw string) (ok bool, needsRehash bool, err error) {
	p, salt, want, err := decode(encoded)
	if err != nil {
		return false, false, err
	}
	defer h.acquire()()
	got := argon2.IDKey([]byte(pw), salt, p.Time, p.MemoryKiB, p.Threads, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return false, false, nil
	}
	return true, p != paramsOf(h.params, len(want)), nil
}

// dummyHash は存在しないユーザーのログインでも同程度の計算時間をかけるための固定ハッシュ（時間差によるユーザー列挙の緩和）。
func (h *Hasher) VerifyDummy(pw string) {
	defer h.acquire()()
	salt := make([]byte, h.params.SaltLen)
	_ = argon2.IDKey([]byte(pw), salt, h.params.Time, h.params.MemoryKiB, h.params.Threads, h.params.KeyLen)
}

func paramsOf(p Params, keyLen int) Params { p.KeyLen = uint32(keyLen); return p }

func decode(encoded string) (Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return Params{}, nil, nil, ErrMalformed
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return Params{}, nil, nil, ErrMalformed
	}
	var p Params
	var threads uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.MemoryKiB, &p.Time, &threads); err != nil || threads == 0 || threads > 255 {
		return Params{}, nil, nil, ErrMalformed
	}
	p.Threads = uint8(threads)
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 {
		return Params{}, nil, nil, ErrMalformed
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) < 16 {
		return Params{}, nil, nil, ErrMalformed
	}
	// 異常に大きなパラメータでメモリを使い切らされるのを防ぐ（保存値が改ざんされた場合の保護）。
	if p.MemoryKiB > 1024*1024 || p.Time > 10 || p.MemoryKiB < 8 {
		return Params{}, nil, nil, ErrMalformed
	}
	p.SaltLen, p.KeyLen = uint32(len(salt)), uint32(len(key))
	return p, salt, key, nil
}

// Policy違反の理由。利用者に見せてよい短い文言。
var (
	ErrTooShort  = errors.New("パスワードは10文字以上にしてください")
	ErrTooLong   = errors.New("パスワードは128文字以内にしてください")
	ErrSameAsID  = errors.New("ログインIDと同じパスワードは使えません")
	ErrRepeated  = errors.New("同じ文字の繰り返しだけのパスワードは使えません")
	ErrCommon    = errors.New("よく使われるパスワードのため使えません")
	ErrInvalidUT = errors.New("パスワードに使えない文字が含まれています")
)

// Validate はパスワード方針（暫定。DECISIONS P1-02-3）を検査する。長さは文字数(rune)で数える。
func Validate(loginID, pw string) error {
	if !utf8.ValidString(pw) || strings.ContainsRune(pw, 0) {
		return ErrInvalidUT
	}
	n := utf8.RuneCountInString(pw)
	if n < 10 {
		return ErrTooShort
	}
	if n > 128 {
		return ErrTooLong
	}
	lower := strings.ToLower(pw)
	if lower == strings.ToLower(loginID) {
		return ErrSameAsID
	}
	if repeated(pw) {
		return ErrRepeated
	}
	if _, bad := common[lower]; bad {
		return ErrCommon
	}
	return nil
}

func repeated(s string) bool {
	var first rune
	for i, r := range s {
		if i == 0 {
			first = r
			continue
		}
		if r != first {
			return false
		}
	}
	return true
}

// common は10文字以上でよく使われるパスワードの小さな一覧（網羅ではない。ごく一般的なものを弾くだけ）。
var common = func() map[string]struct{} {
	list := []string{
		"password12", "password123", "password1234", "1234567890", "12345678910", "123456789012", "qwertyuiop", "qwerty12345",
		"abcdefghij", "iloveyou12", "welcome123", "administrator", "changeme123", "letmein1234", "passw0rd123", "p@ssw0rd123",
		"0123456789", "1q2w3e4r5t", "asdfghjkl1", "nudigital123", "nu_digital1", "nu_digital123",
	}
	m := make(map[string]struct{}, len(list))
	for _, v := range list {
		m[v] = struct{}{}
	}
	return m
}()
