//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/nu-digital/nu_app/services/api/internal/notify"
)

func (e *env) outboxPayloads(eventType string) []string {
	e.t.Helper()
	rows, err := e.pool.Query(context.Background(), `SELECT payload_json::text FROM notification_outbox WHERE event_type=$1 ORDER BY created_at, id`, eventType)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			e.t.Fatal(err)
		}
		var v map[string]any // jsonbの表記（空白入り）を詰めて比較しやすくする
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			e.t.Fatal(err)
		}
		b, _ := json.Marshal(v)
		out = append(out, string(b))
	}
	return out
}

// 貸出・返却（代理返却）・訂正が、業務と同じトランザクションでoutboxへ積まれ、本文に機密が入らない。
func TestNotificationsAreEnqueuedWithBusinessOperations(t *testing.T) {
	e, admin, _ := invSetup(t)
	m := borrowers(t, e, 2)
	// 借り手に私的情報を入れておき、通知の本文に出ないことを確かめる
	if rec := e.patchProfile(m[0], `{"legalName":"架空 太郎","studentNumber":"99X-0001","universityEmail":"taro@example.invalid","nearestStation":"架空駅"}`); rec.Code != 200 {
		t.Fatalf("プロフィール: %d", rec.Code)
	}
	// 表示名にログインIDを含めない（本文にログインIDが出ないことを検査できるように）
	if rec := e.patchProfile(admin, `{"displayName":"管理者X"}`); rec.Code != 200 {
		t.Fatalf("管理者の表示名: %d", rec.Code)
	}
	it := quantityItem(t, e, admin, 5)
	c, l := e.loan(m[0], fmt.Sprintf(`{"itemId":%q,"quantity":2}`, it.ID))
	if c != 201 {
		t.Fatalf("貸出: %d", c)
	}
	if got := e.ret(m[1], l.ID, `{"quantity":1}`); got != 200 { // 代理の部分返却
		t.Fatalf("返却: %d", got)
	}
	if c, _, body := e.correct(admin, newKey(), adjustBody("loan", l.ID, 1, "秘密の理由メモ")); c != 201 {
		t.Fatalf("訂正: %d %s", c, body)
	}
	loans, rets, corrs := e.outboxPayloads("inventory.loan_created"), e.outboxPayloads("inventory.loan_returned"), e.outboxPayloads("inventory.correction_created")
	if len(loans) != 1 || len(rets) != 1 || len(corrs) != 1 {
		t.Fatalf("outbox件数 loan=%d return=%d correction=%d", len(loans), len(rets), len(corrs))
	}
	if !strings.Contains(loans[0], `"itemName":"HDMIケーブル"`) || !strings.Contains(loans[0], `"borrower":"借り手A"`) || !strings.Contains(loans[0], `"remaining":3`) || !strings.Contains(loans[0], `"quantity":2`) {
		t.Errorf("貸出の本文: %s", loans[0])
	}
	if !strings.Contains(rets[0], `"borrower":"借り手A"`) || !strings.Contains(rets[0], `"actor":"借り手B"`) || !strings.Contains(rets[0], `"proxy":true`) {
		t.Errorf("代理返却の本文（借りた人と返却した人が別に分かる）: %s", rets[0])
	}
	if !strings.Contains(corrs[0], `"kind":"adjust"`) || !strings.Contains(corrs[0], `"delta":1`) {
		t.Errorf("訂正の本文: %s", corrs[0])
	}
	all := strings.Join(append(append(loans, rets...), corrs...), "\n")
	for _, secret := range []string{"架空 太郎", "99X-0001", "taro@example.invalid", "架空駅", "user00.example", "秘密の理由メモ", "admin.example"} {
		if strings.Contains(all, secret) {
			t.Errorf("通知の本文に機密が含まれる: %q", secret)
		}
	}
	// 訂正の再送（同じキー）では通知が増えない
	k := newKey()
	b := adjustBody("loan", l.ID, 1, "再送")
	e.correct(admin, k, b)
	e.correct(admin, k, b)
	if n := e.count(`SELECT count(*)::int FROM notification_outbox WHERE event_type='inventory.correction_created'`); n != 2 {
		t.Errorf("同じキーの再送で通知が増えた: %d（元の1+新しい1=2のはず）", n)
	}
}

// 設定が無効な種別は積まない。それでも業務操作は成功する。
func TestDisabledEventIsNotEnqueued(t *testing.T) {
	e, admin, _ := invSetup(t)
	m := borrowers(t, e, 1)
	if rec := e.as(admin, "PUT", "/admin/notifications/settings/inventory.loan_created", `{"enabled":false}`); rec.Code != 200 {
		t.Fatalf("設定: %d %s", rec.Code, rec.Body.String())
	}
	it := quantityItem(t, e, admin, 3)
	if c, _ := e.loan(m[0], fmt.Sprintf(`{"itemId":%q,"quantity":1}`, it.ID)); c != 201 {
		t.Fatalf("貸出: %d", c)
	}
	if n := e.count(`SELECT count(*)::int FROM notification_outbox`); n != 0 {
		t.Errorf("無効なのに積まれた: %d", n)
	}
}

// outboxへの記録に失敗したら、業務も確定しない（outboxと業務は常に一致する）。
func TestOutboxFailureRollsBackTheBusinessOperation(t *testing.T) {
	e, admin, _ := invSetup(t)
	m := borrowers(t, e, 1)
	it := quantityItem(t, e, admin, 3)
	if _, err := e.pool.Exec(context.Background(), `ALTER TABLE notification_outbox RENAME TO notification_outbox_broken`); err != nil {
		t.Fatal(err)
	}
	c, _ := e.loan(m[0], fmt.Sprintf(`{"itemId":%q,"quantity":1}`, it.ID))
	if c != 500 {
		t.Fatalf("outboxに積めないのに貸出が %d", c)
	}
	if n := e.count(`SELECT count(*)::int FROM inventory_loans`); n != 0 {
		t.Errorf("貸出が残った: %d", n)
	}
}

// 同時に借りても、成功した貸出の数だけ通知が積まれる（二重・欠落なし）。
func TestConcurrentLoansEnqueueExactlyOncePerSuccess(t *testing.T) {
	e, admin, _ := invSetup(t)
	m := borrowers(t, e, 10)
	it := quantityItem(t, e, admin, 3)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := range m {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if c, _ := e.loan(m[i], fmt.Sprintf(`{"itemId":%q,"quantity":1}`, it.ID)); c == 201 {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if n := e.count(`SELECT count(*)::int FROM notification_outbox WHERE event_type='inventory.loan_created'`); n != ok || ok != 3 {
		t.Errorf("成功=%d 通知=%d（3件ずつのはず）", ok, n)
	}
}

func TestNotificationSettingsAPI(t *testing.T) {
	e, admin, member := invSetup(t)
	const et = "inventory.loan_returned"
	if rec := e.do("GET", "/admin/notifications/settings", ""); rec.Code != 401 {
		t.Errorf("匿名: %d", rec.Code)
	}
	for _, c := range []struct{ m, p, b string }{{"GET", "/admin/notifications/settings", ""}, {"PUT", "/admin/notifications/settings/" + et, `{"enabled":true}`}, {"GET", "/admin/notifications/deliveries", ""}, {"POST", "/admin/notifications/deliveries/00000000-0000-0000-0000-000000000000/retry", ""}} {
		if rec := e.as(member, c.m, c.p, c.b); rec.Code != 403 {
			t.Errorf("Memberの %s %s = %d（403のはず）", c.m, c.p, rec.Code)
		}
	}
	rec := e.as(admin, "GET", "/admin/notifications/settings", "")
	var list struct {
		Items []struct {
			EventType        string
			Enabled          bool
			DiscordChannelID *string
			Label            string
		}
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if rec.Code != 200 || len(list.Items) != 4 || !list.Items[0].Enabled || list.Items[0].DiscordChannelID != nil || list.Items[0].Label == "" {
		t.Fatalf("一覧（初期は4種別・有効・チャンネル未設定）: %d %s", rec.Code, rec.Body.String())
	}
	for name, body := range map[string]string{"enabled無し": `{}`, "数字以外のチャンネル": `{"enabled":true,"discordChannelId":"abc"}`, "短すぎる": `{"enabled":true,"discordChannelId":"12"}`, "未知のフィールド": `{"enabled":true,"x":1}`} {
		if rec := e.as(admin, "PUT", "/admin/notifications/settings/"+et, body); rec.Code != 422 && rec.Code != 400 {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	if rec := e.as(admin, "PUT", "/admin/notifications/settings/unknown.event", `{"enabled":true}`); rec.Code != 404 {
		t.Errorf("未知の種別: %d", rec.Code)
	}
	if rec := e.as(admin, "PUT", "/admin/notifications/settings/"+et, `{"enabled":true,"discordChannelId":"123456789012345678"}`); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"discordChannelId":"123456789012345678"`) {
		t.Fatalf("設定: %d %s", rec.Code, rec.Body.String())
	}
	if n := e.count(`SELECT count(*)::int FROM audit_logs WHERE action='notifications.setting_updated'`); n != 1 {
		t.Errorf("監査ログ: %d", n)
	}
	// 空文字で未設定に戻せる
	if rec := e.as(admin, "PUT", "/admin/notifications/settings/"+et, `{"enabled":true,"discordChannelId":""}`); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"discordChannelId":null`) {
		t.Errorf("未設定に戻す: %d %s", rec.Code, rec.Body.String())
	}
}

func TestNotificationDeliveriesListAndRetry(t *testing.T) {
	e, admin, _ := invSetup(t)
	ctx := context.Background()
	ins := func(key, status string) string {
		var id string
		err := e.pool.QueryRow(ctx, `INSERT INTO notification_outbox (event_type, dedupe_key, payload_json, status, attempts, sent_at, last_error)
			VALUES ('inventory.loan_created', $1, '{"v":1,"itemName":"秘密にしたい本文"}', $2, 5, CASE WHEN $2='sent' THEN now() END, CASE WHEN $2='dead' THEN 'Discordが429を返した' END) RETURNING id::text`, key, status).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	ins("k-sent", "sent")
	dead := ins("k-dead", "dead")
	pending := ins("k-pending", "pending")
	rec := e.as(admin, "GET", "/admin/notifications/deliveries", "")
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "秘密にしたい本文") || strings.Contains(rec.Body.String(), "payload") {
		t.Fatalf("一覧は本文を返さない: %d %s", rec.Code, rec.Body.String())
	}
	var l struct{ Items []struct{ ID, Status string } }
	_ = json.Unmarshal(rec.Body.Bytes(), &l)
	if len(l.Items) != 3 {
		t.Fatalf("件数: %d", len(l.Items))
	}
	rec = e.as(admin, "GET", "/admin/notifications/deliveries?status=dead", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &l)
	if len(l.Items) != 1 || l.Items[0].ID != dead || !strings.Contains(rec.Body.String(), "Discordが429を返した") {
		t.Errorf("status絞り込み: %s", rec.Body.String())
	}
	if rec := e.as(admin, "GET", "/admin/notifications/deliveries?status=bogus", ""); rec.Code != 400 {
		t.Errorf("不正なstatus: %d", rec.Code)
	}
	// 再送: 止まっている(dead)ものだけ。送信待ち・送信済みは409
	if rec := e.as(admin, "POST", "/admin/notifications/deliveries/"+pending+"/retry", ""); rec.Code != 409 {
		t.Errorf("送信待ちの再送: %d", rec.Code)
	}
	if rec := e.as(admin, "POST", "/admin/notifications/deliveries/"+dead+"/retry", ""); rec.Code != 204 {
		t.Fatalf("deadの再送: %d", rec.Code)
	}
	if st := e.count(`SELECT count(*)::int FROM notification_outbox WHERE id=$1 AND status='pending' AND attempts=0 AND last_error IS NULL`, dead); st != 1 {
		t.Errorf("再送待ちに戻っていない")
	}
	if rec := e.as(admin, "POST", "/admin/notifications/deliveries/"+dead+"/retry", ""); rec.Code != 409 {
		t.Errorf("二重の再送: %d", rec.Code)
	}
	if n := e.count(`SELECT count(*)::int FROM audit_logs WHERE action='notifications.delivery_retried'`); n != 1 {
		t.Errorf("監査ログ: %d", n)
	}
}

type stubSender struct {
	mu    sync.Mutex
	err   error
	calls []string
}

func (s *stubSender) Send(_ context.Context, channelID, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, channelID+"|"+content)
	return s.err
}

func TestNotificationTestEndpoint(t *testing.T) {
	e, admin, member := invSetup(t)
	const ch = `{"discordChannelId":"123456789012345678"}`
	if rec := e.do("POST", "/admin/notifications/test", ch); rec.Code != 401 {
		t.Errorf("匿名: %d", rec.Code)
	}
	if rec := e.as(member, "POST", "/admin/notifications/test", ch); rec.Code != 403 {
		t.Errorf("Member: %d", rec.Code)
	}
	// Botが未設定の環境
	if rec := e.as(admin, "POST", "/admin/notifications/test", ch); rec.Code != 503 || !strings.Contains(rec.Body.String(), "BOT_NOT_CONFIGURED") {
		t.Errorf("Bot未設定: %d %s", rec.Code, rec.Body.String())
	}
	st := &stubSender{}
	e.notifier = st
	e.h = e.build()
	for name, body := range map[string]string{"空": `{}`, "数字以外": `{"discordChannelId":"../x"}`, "未知のフィールド": `{"discordChannelId":"123456789012345678","x":1}`} {
		if rec := e.as(admin, "POST", "/admin/notifications/test", body); rec.Code != 422 && rec.Code != 400 {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	if len(st.calls) != 0 {
		t.Fatalf("不正な入力で送信された: %v", st.calls)
	}
	if rec := e.as(admin, "POST", "/admin/notifications/test", ch); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("成功: %d %s", rec.Code, rec.Body.String())
	}
	if len(st.calls) != 1 || !strings.HasPrefix(st.calls[0], "123456789012345678|【接続テスト】") {
		t.Errorf("送信内容: %v", st.calls)
	}
	// Discord側の失敗は、種類ごとに分かる応答にする（応答にDiscordの本文・tokenは出ない）
	for _, c := range []struct {
		err    error
		status int
		code   string
	}{
		{&notify.SendError{Kind: notify.KindPermanent, Summary: "Discordが拒否しました（HTTP 403）。Botの権限と送信先チャンネルを確認してください"}, 502, "DISCORD_REJECTED"},
		{&notify.SendError{Kind: notify.KindRateLimited, RetryAfter: 3, Summary: "Discordの送信上限（429）。1秒後に再試行"}, 429, "DISCORD_RATE_LIMITED"},
		{&notify.SendError{Kind: notify.KindTransient, Summary: "通信エラー（タイムアウト）"}, 502, "DISCORD_UNAVAILABLE"},
		{fmt.Errorf("想定外のエラー TOKEN-LEAK"), 502, "DISCORD_UNAVAILABLE"},
	} {
		st.err = c.err
		rec := e.as(admin, "POST", "/admin/notifications/test", ch)
		if rec.Code != c.status || !strings.Contains(rec.Body.String(), c.code) || strings.Contains(rec.Body.String(), "TOKEN-LEAK") {
			t.Errorf("%v: %d %s", c.err, rec.Code, rec.Body.String())
		}
	}
	if n := e.count(`SELECT count(*)::int FROM audit_logs WHERE action='notifications.test_sent'`); n != 5 {
		t.Errorf("監査ログ %d件（成功1・失敗4の計5件のはず。Bot未設定は送らないので記録なし）", n)
	}
}

// Discordが落ちていても、貸出・返却は確定し、通知は送信待ちに残る（失敗が業務を巻き戻さない）。
func TestDiscordOutageDoesNotAffectLoans(t *testing.T) {
	e, admin, _ := invSetup(t)
	m := borrowers(t, e, 2)
	if rec := e.as(admin, "PUT", "/admin/notifications/settings/inventory.loan_created", `{"enabled":true,"discordChannelId":"123456789012345678"}`); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	if rec := e.as(admin, "PUT", "/admin/notifications/settings/inventory.loan_returned", `{"enabled":true,"discordChannelId":"123456789012345678"}`); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) }))
	defer down.Close()
	cl, _ := notify.NewClient("TEST-TOKEN-0123456789", down.URL)
	w := notify.NewWorker(e.pool, cl, slog.New(slog.NewTextHandler(io.Discard, nil)))
	it := quantityItem(t, e, admin, 5)
	c, l := e.loan(m[0], fmt.Sprintf(`{"itemId":%q,"quantity":2}`, it.ID))
	if c != 201 {
		t.Fatalf("貸出: %d", c)
	}
	if _, err := w.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := e.ret(m[1], l.ID, `{"quantity":1}`); got != 200 { // 障害中の返却も成功する
		t.Fatalf("返却: %d", got)
	}
	if _, err := w.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := e.item(m[1], it.ID); got.Outstanding != 1 || got.Available != 4 {
		t.Errorf("貸出・返却が確定していない: %+v", got)
	}
	if n := e.count(`SELECT count(*)::int FROM notification_outbox WHERE status='pending' AND attempts >= 1 AND last_error LIKE '%503%'`); n < 1 {
		t.Errorf("失敗が記録されていない")
	}
	if n := e.count(`SELECT count(*)::int FROM notification_outbox WHERE status='sent'`); n != 0 {
		t.Errorf("障害中に送信済みになった: %d", n)
	}
	// 管理者は送信状況で失敗を確認できる
	rec := e.as(admin, "GET", "/admin/notifications/deliveries?status=pending", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "503") {
		t.Errorf("一覧: %d %s", rec.Code, rec.Body.String())
	}
}
