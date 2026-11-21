package api

import (
	"sync"
	"time"
)

// saveLimiter はコンテンツの保存系操作（作成・版の保存・共同編集者・制作メンバー）の頻度制限。
// 利用者ごとに1分あたり60回まで。巨大な本文の連続保存で容量・CPUを圧迫されないようにする（docs/design/p3-cms.md §4）。
type saveLimiter struct {
	mu sync.Mutex
	m  map[string]*saveWindow
}

type saveWindow struct {
	start time.Time
	n     int
}

const (
	saveWindowLen = time.Minute
	saveMax       = 60
)

func newSaveLimiter() *saveLimiter { return &saveLimiter{m: map[string]*saveWindow{}} }

// allow は1回分を数え、上限内ならtrue。上限を超えたら待つべき時間を返す。
func (l *saveLimiter) allow(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.m) > 10000 { // 古い窓を掃除（メモリの上限）
		for k, w := range l.m {
			if now.Sub(w.start) >= saveWindowLen {
				delete(l.m, k)
			}
		}
	}
	w := l.m[key]
	if w == nil || now.Sub(w.start) >= saveWindowLen {
		l.m[key] = &saveWindow{start: now, n: 1}
		return true, 0
	}
	if w.n >= saveMax {
		return false, saveWindowLen - now.Sub(w.start)
	}
	w.n++
	return true, 0
}
