package password

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// テストでは計算量を下げたパラメータを使う（本番の既定値は DefaultParams）。
var fast = Params{MemoryKiB: 64, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}

func TestHashAndVerify(t *testing.T) {
	h := NewHasher(fast, 2)
	a, err := h.Hash("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := h.Hash("correct horse battery")
	if a == b {
		t.Fatal("同じパスワードでもsaltが違うためハッシュは異なるはず")
	}
	if !strings.HasPrefix(a, "$argon2id$v=19$m=64,t=1,p=1$") || strings.Contains(a, "correct horse") {
		t.Fatalf("形式が不正、または平文を含む: %s", a)
	}
	ok, rehash, err := h.Verify(a, "correct horse battery")
	if err != nil || !ok || rehash {
		t.Fatalf("正しいパスワードの検証: ok=%v rehash=%v err=%v", ok, rehash, err)
	}
	if ok, _, _ := h.Verify(a, "correct horse batterY"); ok {
		t.Fatal("誤ったパスワードが通った")
	}
}

func TestNeedsRehashWhenParamsChange(t *testing.T) {
	old := NewHasher(fast, 1)
	enc, _ := old.Hash("some long password")
	stronger := fast
	stronger.Time = 2
	ok, rehash, err := NewHasher(stronger, 1).Verify(enc, "some long password")
	if err != nil || !ok || !rehash {
		t.Fatalf("パラメータ変更後は再ハッシュが必要になるはず: ok=%v rehash=%v err=%v", ok, rehash, err)
	}
}

func TestVerifyRejectsMalformedAndHostileHashes(t *testing.T) {
	h := NewHasher(fast, 1)
	for _, bad := range []string{
		"", "plain-text-password", "$argon2id$", "$argon2i$v=19$m=64,t=1,p=1$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5",
		"$argon2id$v=19$m=64,t=1,p=0$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5a2V5",
		"$argon2id$v=19$m=9999999,t=1,p=1$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5a2V5", // メモリ使い切りを狙う改ざん値
		"$argon2id$v=19$m=64,t=999,p=1$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5a2V5",
		"$argon2id$v=19$m=64,t=1,p=1$!!$a2V5a2V5a2V5a2V5a2V5a2V5",
	} {
		if ok, _, err := h.Verify(bad, "x"); ok || err == nil {
			t.Errorf("不正なハッシュ %q が受理された (ok=%v err=%v)", bad, ok, err)
		}
	}
}

func TestConcurrencyIsLimited(t *testing.T) {
	const limit = 2
	h := NewHasher(Params{MemoryKiB: 4096, Time: 2, Threads: 1, SaltLen: 16, KeyLen: 32}, limit)
	var cur, peak int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Hash内部のsemaphoreの外側から観測できないため、acquireを直接使って上限を確認する
			release := h.acquire()
			n := atomic.AddInt32(&cur, 1)
			for {
				p := atomic.LoadInt32(&peak)
				if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			atomic.AddInt32(&cur, -1)
			release()
		}()
	}
	wg.Wait()
	if peak > limit {
		t.Fatalf("同時実行数が上限(%d)を超えた: %d", limit, peak)
	}
	// Hash/Verify/VerifyDummyも同じ上限で動き、デッドロックしない
	var wg2 sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			enc, err := h.Hash("another long password")
			if err != nil {
				t.Error(err)
				return
			}
			h.Verify(enc, "another long password")
			h.VerifyDummy("x")
		}()
	}
	wg2.Wait()
}

func TestValidatePolicy(t *testing.T) {
	cases := []struct {
		id, pw string
		want   error
	}{
		{"alice.example", "short", ErrTooShort},
		{"alice.example", "123456789", ErrTooShort},
		{"alice.example", strings.Repeat("a", 129), ErrTooLong},
		{"alice.example", "Alice.Example", ErrSameAsID}, // 10文字以上のログインIDと同じ（大文字小文字を区別しない）
		{"alice.example", "aaaaaaaaaaaa", ErrRepeated},
		{"alice.example", "Password123", ErrCommon},
		{"alice.example", "1234567890", ErrCommon},
		{"alice.example", "pass\x00word-long", ErrInvalidUT},
		{"alice.example", "ok-passphrase-1", nil},
		{"alice.example", strings.Repeat("あ", 10), ErrRepeated}, // 長さは文字数で数え、繰り返しは文字単位で判定
		{"alice.example", "ひらがなのぱすわーど", nil},                    // 日本語・10文字
		{"alice.example", strings.Repeat("a1", 64), nil},        // 128文字ちょうど
	}
	for _, c := range cases {
		if got := Validate(c.id, c.pw); got != c.want {
			t.Errorf("Validate(%q, %q) = %v, 期待 %v", c.id, c.pw, got, c.want)
		}
	}
}
