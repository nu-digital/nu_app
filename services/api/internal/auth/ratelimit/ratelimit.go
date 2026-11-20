// Package ratelimit はログインのレート制限（メモリ上）。暫定仕様: DECISIONS P1-02-4。
//   - (ID,IP)・IP・ID の3つの失敗カウンターを持つ。存在するIDかどうかを区別せず、全て同じように数える。
//   - キーは小文字化・形式検証してから使い、形式外は固定キーに丸める。件数に上限を設け、古い順に捨てる。
//   - 再起動でリセットされ、複数台では共有されない（1台運用が前提）。
package ratelimit

import (
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type Rule struct {
	Limit  int
	Window time.Duration
	Lock   time.Duration
}

type Config struct {
	Pair, IP, ID Rule
	MaxEntries   int
}

// DefaultConfig は暫定値。
var DefaultConfig = Config{
	Pair:       Rule{Limit: 5, Window: 15 * time.Minute, Lock: 15 * time.Minute},
	IP:         Rule{Limit: 10, Window: 10 * time.Minute, Lock: 15 * time.Minute},
	ID:         Rule{Limit: 30, Window: 15 * time.Minute, Lock: 15 * time.Minute},
	MaxEntries: 20000,
}

type entry struct {
	count       int
	windowStart time.Time
	lockedUntil time.Time
}

type Limiter struct {
	mu      sync.Mutex
	cfg     Config
	now     func() time.Time
	entries map[string]*entry
}

func New(cfg Config, now func() time.Time) *Limiter {
	if now == nil {
		now = time.Now
	}
	return &Limiter{cfg: cfg, now: now, entries: map[string]*entry{}}
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,62}$`)

func normID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if !idPattern.MatchString(id) {
		return "(invalid)" // 任意の文字列でキーを増やせないようにする
	}
	return id
}

func (l *Limiter) keys(id, ip string) (pair, ipk, idk string) {
	n := normID(id)
	return "pair:" + n + "|" + ip, "ip:" + ip, "id:" + n
}

// Check は現在ロック中かを返す。ロック中なら最長の残り時間を返す。
func (l *Limiter) Check(id, ip string) (allowed bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	pair, ipk, idk := l.keys(id, ip)
	var worst time.Duration
	for _, k := range []string{pair, ipk, idk} {
		if e, ok := l.entries[k]; ok && now.Before(e.lockedUntil) {
			if d := e.lockedUntil.Sub(now); d > worst {
				worst = d
			}
		}
	}
	return worst == 0, worst
}

// Failure はログイン失敗を記録する。
func (l *Limiter) Failure(id, ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	pair, ipk, idk := l.keys(id, ip)
	l.bump(pair, l.cfg.Pair, now)
	l.bump(ipk, l.cfg.IP, now)
	l.bump(idk, l.cfg.ID, now)
}

// Success はログイン成功時に、(ID,IP)とIDのカウンターを消す（IPのカウンターは残す）。
func (l *Limiter) Success(id, ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	pair, _, idk := l.keys(id, ip)
	delete(l.entries, pair)
	delete(l.entries, idk)
}

// Reset は再設定成功時などに、指定IDの全カウンターを消す。
func (l *Limiter) Reset(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := normID(id)
	delete(l.entries, "id:"+n)
	prefix := "pair:" + n + "|"
	for k := range l.entries {
		if strings.HasPrefix(k, prefix) {
			delete(l.entries, k)
		}
	}
}

func (l *Limiter) bump(key string, r Rule, now time.Time) {
	e, ok := l.entries[key]
	if !ok {
		l.evictIfFull()
		e = &entry{windowStart: now}
		l.entries[key] = e
	}
	if now.Sub(e.windowStart) >= r.Window && !now.Before(e.lockedUntil) {
		e.count, e.windowStart = 0, now
	}
	e.count++
	if e.count >= r.Limit {
		e.lockedUntil = now.Add(r.Lock)
	}
}

// evictIfFull は上限を超えそうなときに古い方から1割を捨てる。
func (l *Limiter) evictIfFull() {
	if len(l.entries) < l.cfg.MaxEntries {
		return
	}
	type kv struct {
		k string
		t time.Time
	}
	all := make([]kv, 0, len(l.entries))
	for k, e := range l.entries {
		all = append(all, kv{k, e.windowStart})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].t.Before(all[j].t) })
	drop := len(all)/10 + 1
	for _, x := range all[:drop] {
		delete(l.entries, x.k)
	}
}

// Len はテスト用の現在のキー数。
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}
