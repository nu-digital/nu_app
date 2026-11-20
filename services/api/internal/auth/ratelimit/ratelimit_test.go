package ratelimit

import (
	"fmt"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newLimiter(cfg Config) (*Limiter, *clock) {
	c := &clock{t: time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)}
	return New(cfg, c.now), c
}

func fail(l *Limiter, id, ip string, n int) {
	for i := 0; i < n; i++ {
		l.Failure(id, ip)
	}
}

func TestPairLockAfterFiveFailures(t *testing.T) {
	l, c := newLimiter(DefaultConfig)
	fail(l, "alice.example", "203.0.113.1", 4)
	if ok, _ := l.Check("alice.example", "203.0.113.1"); !ok {
		t.Fatal("4回ではロックされない")
	}
	l.Failure("alice.example", "203.0.113.1")
	ok, retry := l.Check("alice.example", "203.0.113.1")
	if ok || retry != 15*time.Minute {
		t.Fatalf("5回でロックされるはず: ok=%v retry=%v", ok, retry)
	}
	// 別のIPからは同じIDでもロックされない（標的型DoSの緩和）
	if ok, _ := l.Check("alice.example", "203.0.113.2"); !ok {
		t.Fatal("別IPがロックされた")
	}
	c.advance(15*time.Minute + time.Second)
	if ok, _ := l.Check("alice.example", "203.0.113.1"); !ok {
		t.Fatal("ロック期間後は解除される")
	}
}

func TestIPLockAcrossManyIDs(t *testing.T) {
	l, _ := newLimiter(DefaultConfig)
	for i := 0; i < 10; i++ {
		l.Failure(fmt.Sprintf("user%d.example", i), "198.51.100.7")
	}
	if ok, _ := l.Check("brand-new.example", "198.51.100.7"); ok {
		t.Fatal("IPごとの上限(10回)でそのIPがロックされるはず")
	}
	if ok, _ := l.Check("brand-new.example", "198.51.100.8"); !ok {
		t.Fatal("別IPはロックされない")
	}
}

func TestIDLockIsLooserAndSpansIPs(t *testing.T) {
	l, _ := newLimiter(DefaultConfig)
	for i := 0; i < 29; i++ {
		l.Failure("owner.example", fmt.Sprintf("192.0.2.%d", i+1))
	}
	if ok, _ := l.Check("owner.example", "192.0.2.200"); !ok {
		t.Fatal("29回ではIDはロックされない")
	}
	l.Failure("owner.example", "192.0.2.150")
	if ok, _ := l.Check("owner.example", "192.0.2.201"); ok {
		t.Fatal("IDの上限(30回)で、どのIPからでもロックされる")
	}
}

func TestIDsAreNormalizedAndUnknownIDsCountTheSame(t *testing.T) {
	l, _ := newLimiter(DefaultConfig)
	fail(l, "Alice.Example", "203.0.113.1", 3)
	fail(l, " alice.example ", "203.0.113.1", 2)
	if ok, _ := l.Check("ALICE.EXAMPLE", "203.0.113.1"); ok {
		t.Fatal("大文字小文字・前後空白を同じIDとして数えるはず")
	}
	// 存在しないIDも同じ閾値でロックされる（存在の有無が429の有無から分からない）
	fail(l, "no-such-user", "203.0.113.9", 5)
	if ok, _ := l.Check("no-such-user", "203.0.113.9"); ok {
		t.Fatal("存在しないIDも同じようにロックされるはず")
	}
}

func TestSuccessClearsPairAndIDButNotIP(t *testing.T) {
	l, _ := newLimiter(DefaultConfig)
	fail(l, "alice.example", "203.0.113.1", 4)
	l.Success("alice.example", "203.0.113.1")
	l.Failure("alice.example", "203.0.113.1")
	if ok, _ := l.Check("alice.example", "203.0.113.1"); !ok {
		t.Fatal("成功後はカウンターがリセットされるはず")
	}
	// IPのカウンターは残る: 他のIDへの失敗と合わせて10回でロック
	for i := 0; i < 5; i++ {
		l.Failure(fmt.Sprintf("other%d.example", i), "203.0.113.1")
	}
	if ok, _ := l.Check("x.example", "203.0.113.1"); ok {
		t.Fatal("IPの失敗は成功で消えず、累積でロックされるはず")
	}
}

func TestResetClearsAllCountersForID(t *testing.T) {
	l, _ := newLimiter(DefaultConfig)
	fail(l, "alice.example", "203.0.113.1", 5)
	fail(l, "alice.example", "203.0.113.2", 5)
	l.Reset("alice.example")
	if ok, _ := l.Check("alice.example", "203.0.113.1"); !ok {
		t.Fatal("Reset後は解除されるはず")
	}
}

func TestWindowExpiryResetsCount(t *testing.T) {
	l, c := newLimiter(DefaultConfig)
	fail(l, "alice.example", "203.0.113.1", 4)
	c.advance(16 * time.Minute)
	l.Failure("alice.example", "203.0.113.1")
	if ok, _ := l.Check("alice.example", "203.0.113.1"); !ok {
		t.Fatal("窓を過ぎた失敗は数え直しになるはず")
	}
}

func TestInvalidIDsCollapseAndEntriesAreBounded(t *testing.T) {
	cfg := DefaultConfig
	cfg.MaxEntries = 100
	l, _ := newLimiter(cfg)
	for i := 0; i < 5000; i++ {
		l.Failure(fmt.Sprintf("Invalid ID with spaces %d!!", i), fmt.Sprintf("10.0.%d.%d", i/250, i%250))
	}
	if n := l.Len(); n > cfg.MaxEntries+5 {
		t.Fatalf("キー数が上限を超えている: %d", n)
	}
	l2, _ := newLimiter(DefaultConfig)
	for i := 0; i < 200; i++ {
		l2.Failure(fmt.Sprintf("bad id %d", i), "203.0.113.1")
	}
	// 形式外のIDは1つのキーに丸まり、IDキーが増えない（pair・ip・idの3種類×1）
	if n := l2.Len(); n != 3 {
		t.Fatalf("形式外のIDがキーを増やしている: %d", n)
	}
}
