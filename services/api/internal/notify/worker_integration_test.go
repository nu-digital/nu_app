//go:build integration

package notify_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nu-digital/nu_app/services/api/internal/notify"
	"github.com/nu-digital/nu_app/services/api/internal/testutil/pgtest"
)

const token = "TEST-TOKEN-do-not-leak-0123456789"
const channel = "123456789012345678"

type fake struct {
	mu       sync.Mutex
	status   int
	header   map[string]string
	body     string
	msgs     []string // 受け取った本文(content)
	srv      *httptest.Server
	inflight int
}

func newFake(t *testing.T) *fake {
	f := &fake{status: 200, body: `{"id":"1"}`}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		st, hd, body := f.status, f.header, f.body
		if st/100 == 2 {
			f.msgs = append(f.msgs, string(b))
		}
		f.mu.Unlock()
		for k, v := range hd {
			w.Header().Set(k, v)
		}
		w.WriteHeader(st)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(f.srv.Close)
	return f
}
func (f *fake) respond(status int, header map[string]string, body string) {
	f.mu.Lock()
	f.status, f.header, f.body = status, header, body
	f.mu.Unlock()
}
func (f *fake) delivered() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.msgs...)
}

type env struct {
	t    *testing.T
	pool *pgxpool.Pool
	f    *fake
	now  time.Time
	logs *bytes.Buffer
	w    *notify.Worker
}

func setup(t *testing.T) *env {
	pool, _ := pgtest.New(t)
	e := &env{t: t, pool: pool, f: newFake(t), now: time.Now().Add(time.Minute), logs: &bytes.Buffer{}}
	c, err := notify.NewClient(token, e.f.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	e.w = notify.NewWorker(pool, c, slog.New(slog.NewTextHandler(e.logs, nil)))
	e.w.Now = func() time.Time { return e.now }
	e.setChannel("inventory.loan_created", channel, true)
	e.setChannel("inventory.loan_returned", channel, true)
	return e
}
func (e *env) setChannel(event, ch string, enabled bool) {
	var c any
	if ch != "" {
		c = ch
	}
	if _, err := e.pool.Exec(context.Background(), `UPDATE notification_settings SET discord_channel_id=$2, enabled=$3 WHERE event_type=$1`, event, c, enabled); err != nil {
		e.t.Fatal(err)
	}
}
func (e *env) enqueue(key, event, item string) string {
	var id string
	err := e.pool.QueryRow(context.Background(), `INSERT INTO notification_outbox (event_type, dedupe_key, payload_json, next_attempt_at)
		VALUES ($1,$2,jsonb_build_object('v',1,'itemName',$3::text,'quantity',1,'borrower','借り手A','actor','借り手A'), now() - interval '1 minute') RETURNING id::text`, event, key, item).Scan(&id)
	if err != nil {
		e.t.Fatal(err)
	}
	return id
}

type row struct {
	Status    string
	Attempts  int
	LastError *string
	Next      time.Time
}

func (e *env) row(id string) row {
	var r row
	if err := e.pool.QueryRow(context.Background(), `SELECT status, attempts, last_error, next_attempt_at FROM notification_outbox WHERE id=$1`, id).Scan(&r.Status, &r.Attempts, &r.LastError, &r.Next); err != nil {
		e.t.Fatal(err)
	}
	return r
}
func (e *env) run() int {
	n, err := e.w.RunOnce(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	return n
}

func TestSendsAndMarksSentWithNotificationID(t *testing.T) {
	e := setup(t)
	id := e.enqueue("k1", "inventory.loan_created", "HDMIケーブル")
	if n := e.run(); n != 1 {
		t.Fatalf("処理件数 %d", n)
	}
	if r := e.row(id); r.Status != "sent" || r.Attempts != 1 || r.LastError != nil {
		t.Errorf("状態: %+v", r)
	}
	msgs := e.f.delivered()
	if len(msgs) != 1 || !strings.Contains(msgs[0], "通知ID: "+id) || !strings.Contains(msgs[0], "HDMIケーブル") {
		t.Errorf("本文（通知IDで重複を調べられる）: %v", msgs)
	}
	if e.run() != 0 || len(e.f.delivered()) != 1 {
		t.Error("送信済みが再送された")
	}
}

// チャンネル未設定・無効な種別は送らず、outboxに待機する。設定後に送られる。
func TestWaitsUntilChannelIsConfiguredAndSkipsDisabled(t *testing.T) {
	e := setup(t)
	e.setChannel("inventory.loan_created", "", true)
	id := e.enqueue("k1", "inventory.loan_created", "A")
	if e.run() != 0 || len(e.f.delivered()) != 0 {
		t.Fatal("チャンネル未設定なのに送った")
	}
	if r := e.row(id); r.Status != "pending" || r.Attempts != 0 {
		t.Errorf("待機のはず: %+v", r)
	}
	e.setChannel("inventory.loan_created", channel, false)
	if e.run() != 0 {
		t.Fatal("無効なのに送った")
	}
	e.setChannel("inventory.loan_created", channel, true)
	if e.run() != 1 || e.row(id).Status != "sent" {
		t.Error("設定後に送られていない")
	}
}

// Discordの障害（5xx）でも業務側は何も失わず、間隔を空けて再試行され、復旧後に1回だけ届く。
func TestTransientFailureRetriesWithBackoffAndRecovers(t *testing.T) {
	e := setup(t)
	id := e.enqueue("k1", "inventory.loan_returned", "HDMIケーブル")
	e.f.respond(503, nil, `{"message":"`+token+`"}`)
	e.run()
	r := e.row(id)
	if r.Status != "pending" || r.Attempts != 1 || r.LastError == nil || !strings.Contains(*r.LastError, "503") {
		t.Fatalf("1回目の失敗: %+v", r)
	}
	if d := r.Next.Sub(e.now); d < 29*time.Second || d > 31*time.Second {
		t.Errorf("次回試行まで %v（約30秒のはず）", d)
	}
	if e.run() != 0 {
		t.Error("待ち時間の前に再送した")
	}
	e.now = e.now.Add(31 * time.Second)
	e.run()
	if r := e.row(id); r.Attempts != 2 || r.Next.Sub(e.now) < 59*time.Second {
		t.Errorf("2回目は待ちが倍になる: %+v 待ち%v", r, r.Next.Sub(e.now))
	}
	e.f.respond(200, nil, `{}`)
	e.now = e.now.Add(2 * time.Minute)
	e.run()
	if r := e.row(id); r.Status != "sent" || r.Attempts != 3 || r.LastError != nil {
		t.Errorf("復旧後: %+v", r)
	}
	if n := len(e.f.delivered()); n != 1 {
		t.Errorf("届いた数 %d（1回のはず）", n)
	}
}

func TestGivesUpAfterMaxAttemptsAndAdminCanRetry(t *testing.T) {
	e := setup(t)
	e.w.MaxAttempts = 3
	id := e.enqueue("k1", "inventory.loan_created", "A")
	e.f.respond(500, nil, "")
	for i := 0; i < 3; i++ {
		e.run()
		e.now = e.now.Add(time.Hour)
	}
	r := e.row(id)
	if r.Status != "dead" || r.Attempts != 3 || r.LastError == nil || !strings.Contains(*r.LastError, "再試行の上限") {
		t.Fatalf("上限で止まるはず: %+v", r)
	}
	if e.run() != 0 {
		t.Error("deadを再送した")
	}
	// 管理者の再送（API: RetryNotificationDelivery と同じ更新）→ 復旧後に届く
	if _, err := e.pool.Exec(context.Background(), `UPDATE notification_outbox SET status='pending', attempts=0, next_attempt_at=now(), last_error=NULL WHERE id=$1 AND status='dead'`, id); err != nil {
		t.Fatal(err)
	}
	e.f.respond(200, nil, "{}")
	e.now = e.now.Add(time.Hour)
	e.run()
	if e.row(id).Status != "sent" || len(e.f.delivered()) != 1 {
		t.Error("再送で届いていない")
	}
}

func TestPermanentRejectionStopsImmediately(t *testing.T) {
	e := setup(t)
	id := e.enqueue("k1", "inventory.loan_created", "A")
	e.f.respond(403, nil, `{"message":"Missing Access `+token+`"}`)
	e.run()
	r := e.row(id)
	if r.Status != "dead" || r.Attempts != 1 || r.LastError == nil || !strings.Contains(*r.LastError, "403") {
		t.Errorf("権限エラーは再試行せず止める: %+v", r)
	}
}

// 429はRetry-Afterの時間だけ待ち、失敗として数えず、巡回を止める（残りは送らない）。
func TestRateLimitWaitsAndDoesNotCountAsFailure(t *testing.T) {
	e := setup(t)
	a := e.enqueue("k1", "inventory.loan_created", "A")
	b := e.enqueue("k2", "inventory.loan_created", "B")
	e.f.respond(429, map[string]string{"Retry-After": "7"}, `{"retry_after":7,"global":false}`)
	if n := e.run(); n != 1 {
		t.Errorf("429のあとも送り続けた: %d件処理", n)
	}
	ra, rb := e.row(a), e.row(b)
	if ra.Status != "pending" || ra.Attempts != 0 || ra.Next.Sub(e.now) < 6*time.Second || ra.Next.Sub(e.now) > 8*time.Second {
		t.Errorf("429の行: %+v 待ち%v", ra, ra.Next.Sub(e.now))
	}
	if rb.Status != "pending" || rb.Attempts != 0 {
		t.Errorf("後続の行に触れてはいけない: %+v", rb)
	}
	e.f.respond(200, nil, "{}")
	e.now = e.now.Add(8 * time.Second)
	if n := e.run(); n != 2 || e.row(a).Status != "sent" || e.row(b).Status != "sent" {
		t.Errorf("待ったあとは両方届く: %d", n)
	}
}

func TestBrokenPayloadGoesDead(t *testing.T) {
	e := setup(t)
	var id string
	if err := e.pool.QueryRow(context.Background(), `INSERT INTO notification_outbox (event_type, dedupe_key, payload_json, next_attempt_at) VALUES ('inventory.loan_created','bad','"文字列"'::jsonb, now()-interval '1 minute') RETURNING id::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	e.run()
	if r := e.row(id); r.Status != "dead" || len(e.f.delivered()) != 0 {
		t.Errorf("壊れた内容: %+v", r)
	}
}

// workerが送信の途中で落ちても（確保したまま）、リース期限後に取り直されて届く。期限前は二重に取らない。
func TestCrashedClaimIsRecoveredAfterLease(t *testing.T) {
	e := setup(t)
	id := e.enqueue("k1", "inventory.loan_created", "A")
	if _, err := e.pool.Exec(context.Background(), `UPDATE notification_outbox SET attempts = attempts + 1, next_attempt_at = $2 WHERE id=$1`, id, e.now.Add(e.w.Lease)); err != nil { // 確保して落ちた状態
		t.Fatal(err)
	}
	if e.run() != 0 {
		t.Fatal("リース期限前に取り直した")
	}
	e.now = e.now.Add(e.w.Lease + time.Second)
	if e.run() != 1 || e.row(id).Status != "sent" || len(e.f.delivered()) != 1 {
		t.Error("リース期限後に届いていない")
	}
}

// 複数のworkerが同時に巡回しても、各通知は1回だけ送られる（SKIP LOCKED）。
func TestConcurrentWorkersDeliverEachOnce(t *testing.T) {
	e := setup(t)
	const n = 30
	ids := map[string]bool{}
	for i := 0; i < n; i++ {
		ids[e.enqueue(fmt.Sprintf("k%02d", i), "inventory.loan_created", fmt.Sprintf("物品%02d", i))] = true
	}
	e.w.MaxPerRun = 100
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, _ := notify.NewClient(token, e.f.srv.URL)
			w := notify.NewWorker(e.pool, c, slog.New(slog.NewTextHandler(io.Discard, nil)))
			w.Now = func() time.Time { return e.now }
			w.MaxPerRun = 100
			_, _ = w.RunOnce(context.Background())
		}()
	}
	wg.Wait()
	seen := map[string]int{}
	for _, m := range e.f.delivered() {
		i := strings.Index(m, "通知ID: ")
		seen[m[i+len("通知ID: "):][:36]]++
	}
	if len(seen) != n {
		t.Errorf("届いた通知 %d種（%dのはず）", len(seen), n)
	}
	for id, c := range seen {
		if c != 1 || !ids[id] {
			t.Errorf("通知 %s が %d回届いた", id, c)
		}
	}
	var sent int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM notification_outbox WHERE status='sent'`).Scan(&sent)
	if sent != n {
		t.Errorf("sent=%d", sent)
	}
}

// tokenと応答本文は、DB・ログのどこにも残らない。
func TestTokenAndResponseBodiesNeverPersisted(t *testing.T) {
	e := setup(t)
	ids := []string{e.enqueue("k1", "inventory.loan_created", "A"), e.enqueue("k2", "inventory.loan_created", "B"), e.enqueue("k3", "inventory.loan_created", "C")}
	secret := token + " 応答本文の秘密"
	for _, st := range []int{500, 429, 403} {
		e.f.respond(st, map[string]string{"Retry-After": "1"}, `{"message":"`+secret+`"}`)
		e.now = e.now.Add(time.Hour)
		e.run()
	}
	for _, id := range ids {
		if r := e.row(id); r.LastError != nil && (strings.Contains(*r.LastError, token) || strings.Contains(*r.LastError, "応答本文")) {
			t.Errorf("last_errorに機密: %q", *r.LastError)
		}
	}
	var all string
	_ = e.pool.QueryRow(context.Background(), `SELECT COALESCE(string_agg(payload_json::text || coalesce(last_error,''), ' '),'') FROM notification_outbox`).Scan(&all)
	if strings.Contains(all, token) || strings.Contains(e.logs.String(), token) || strings.Contains(e.logs.String(), "応答本文") {
		t.Error("DBまたはログにtokenや応答本文が残っている")
	}
}
