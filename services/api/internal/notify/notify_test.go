package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testToken = "TEST-TOKEN-do-not-leak-0123456789"

type fakeDiscord struct {
	mu      sync.Mutex
	handler func(w http.ResponseWriter, r *http.Request, body string)
	calls   []recorded
	srv     *httptest.Server
}
type recorded struct {
	Path, Auth, Body, UA string
}

func newFake(t *testing.T, h func(w http.ResponseWriter, r *http.Request, body string)) *fakeDiscord {
	f := &fakeDiscord{handler: h}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, recorded{r.URL.Path, r.Header.Get("Authorization"), string(b), r.Header.Get("User-Agent")})
		hd := f.handler
		f.mu.Unlock()
		hd(w, r, string(b))
	}))
	t.Cleanup(f.srv.Close)
	return f
}
func (f *fakeDiscord) set(h func(w http.ResponseWriter, r *http.Request, body string)) {
	f.mu.Lock()
	f.handler = h
	f.mu.Unlock()
}
func (f *fakeDiscord) n() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.calls) }

func ok(w http.ResponseWriter, _ *http.Request, _ string) {
	w.WriteHeader(200)
	_, _ = w.Write([]byte(`{"id":"1"}`))
}

func TestClientSendsBotAuthAndDisablesMentions(t *testing.T) {
	f := newFake(t, ok)
	c, err := NewClient(testToken, f.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Send(context.Background(), "123456789012345678", "こんにちは @everyone"); err != nil {
		t.Fatal(err)
	}
	r := f.calls[0]
	if r.Path != "/channels/123456789012345678/messages" || r.Auth != "Bot "+testToken || !strings.Contains(r.UA, "nu_app") {
		t.Errorf("リクエスト: %+v", r)
	}
	if !strings.Contains(r.Body, `"allowed_mentions":{"parse":[]}`) {
		t.Errorf("メンションが無効化されていない: %s", r.Body)
	}
}

func TestClientRejectsBadChannelIDWithoutRequest(t *testing.T) {
	f := newFake(t, ok)
	c, _ := NewClient(testToken, f.srv.URL)
	for _, id := range []string{"", "abc", "../../users/@me", "123/456", "12"} {
		err := c.Send(context.Background(), id, "x")
		se, isSE := err.(*SendError)
		if !isSE || se.Kind != KindPermanent {
			t.Errorf("%q: %v", id, err)
		}
	}
	if f.n() != 0 {
		t.Errorf("不正なIDでリクエストが飛んだ: %d", f.n())
	}
}

func TestClientClassifiesFailuresAndNeverLeaksToken(t *testing.T) {
	// 応答本文にtokenや個人情報を入れて返す偽サーバー: エラー文言に一切出ないこと
	leak := `{"message":"` + testToken + ` 秘密の応答本文"}`
	cases := []struct {
		name   string
		status int
		header string
		body   string
		kind   ErrKind
		after  time.Duration
	}{
		{"429ヘッダー", 429, "7", leak, KindRateLimited, 7 * time.Second},
		{"429本文の小数", 429, "", `{"retry_after": 2.5}`, KindRateLimited, 2500 * time.Millisecond},
		{"429で値なし", 429, "", leak, KindRateLimited, time.Second},
		{"429で巨大な値は1時間まで", 429, "999999", leak, KindRateLimited, time.Hour},
		{"500", 500, "", leak, KindTransient, 0},
		{"503", 503, "", leak, KindTransient, 0},
		{"401", 401, "", leak, KindPermanent, 0},
		{"403", 403, "", leak, KindPermanent, 0},
		{"404", 404, "", leak, KindPermanent, 0},
		{"400", 400, "", leak, KindPermanent, 0},
	}
	for _, c := range cases {
		f := newFake(t, func(w http.ResponseWriter, _ *http.Request, _ string) {
			if c.header != "" {
				w.Header().Set("Retry-After", c.header)
			}
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		})
		cl, _ := NewClient(testToken, f.srv.URL)
		err := cl.Send(context.Background(), "123456789012345678", "x")
		se, isSE := err.(*SendError)
		if !isSE {
			t.Fatalf("%s: SendErrorでない: %v", c.name, err)
		}
		if se.Kind != c.kind || (c.kind == KindRateLimited && se.RetryAfter != c.after) {
			t.Errorf("%s: kind=%v after=%v", c.name, se.Kind, se.RetryAfter)
		}
		if strings.Contains(se.Summary, testToken) || strings.Contains(se.Summary, "秘密") || strings.Contains(se.Error(), "Bot ") {
			t.Errorf("%s: 要約に機密が含まれる: %q", c.name, se.Summary)
		}
	}
}

func TestClientNetworkErrorsAreTransientAndDoNotLeak(t *testing.T) {
	f := newFake(t, ok)
	base := f.srv.URL
	f.srv.Close() // 接続できない
	c, _ := NewClient(testToken, base)
	err := c.Send(context.Background(), "123456789012345678", "x")
	se, isSE := err.(*SendError)
	if !isSE || se.Kind != KindTransient || strings.Contains(se.Summary, base) || strings.Contains(se.Summary, testToken) {
		t.Errorf("接続不可: %v", err)
	}
	// タイムアウト
	slow := newFake(t, func(w http.ResponseWriter, _ *http.Request, _ string) {
		time.Sleep(300 * time.Millisecond)
		ok(w, nil, "")
	})
	c2, _ := NewClient(testToken, slow.srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err = c2.Send(ctx, "123456789012345678", "x")
	if se, isSE := err.(*SendError); !isSE || se.Kind != KindTransient || !strings.Contains(se.Summary, "タイムアウト") {
		t.Errorf("タイムアウト: %v", err)
	}
}

func TestClientDoesNotFollowRedirectsWithToken(t *testing.T) {
	var leaked bool
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			leaked = true
		}
		w.WriteHeader(200)
	}))
	defer evil.Close()
	f := newFake(t, func(w http.ResponseWriter, r *http.Request, _ string) {
		http.Redirect(w, r, evil.URL, http.StatusTemporaryRedirect)
	})
	c, _ := NewClient(testToken, f.srv.URL)
	err := c.Send(context.Background(), "123456789012345678", "x")
	if leaked {
		t.Fatal("リダイレクト先へAuthorizationが送られた")
	}
	if se, isSE := err.(*SendError); !isSE || se.Kind != KindPermanent {
		t.Errorf("リダイレクトは成功扱いにしない: %v", err)
	}
}

func TestClientStringAndConstructorDoNotExposeToken(t *testing.T) {
	c, _ := NewClient(testToken, "")
	if strings.Contains(c.String(), testToken) {
		t.Error("Stringにtokenが出る")
	}
	for _, bad := range []string{"", "a b", "a\nb", "tok\x00en"} {
		if _, err := NewClient(bad, ""); err == nil {
			t.Errorf("不正なtokenを受け付けた: %q", bad)
		} else if strings.Contains(err.Error(), bad) && bad != "" {
			t.Errorf("エラーにtokenが出る")
		}
	}
}

func TestFormat(t *testing.T) {
	rem := int64(3)
	mk := func(s string) []byte { return []byte(s) }
	got, err := Format("inventory.loan_created", "id-1", mk(`{"v":1,"itemName":"HDMIケーブル","quantity":2,"borrower":"借り手A","remaining":3}`))
	if err != nil || got != "【貸出】借り手Aさんが「HDMIケーブル」を2個借りました（残り3）\n通知ID: id-1" {
		t.Errorf("貸出: %q %v", got, err)
	}
	_ = rem
	got, _ = Format("inventory.loan_created", "id-2", mk(`{"v":1,"itemName":"Pico","assetTag":"PICO-01","quantity":1,"borrower":"借り手A","remaining":1}`))
	if got != "【貸出】借り手Aさんが「Pico」（PICO-01）を借りました（残り1）\n通知ID: id-2" {
		t.Errorf("個体の貸出: %q", got)
	}
	got, _ = Format("inventory.loan_returned", "id-3", mk(`{"v":1,"itemName":"HDMIケーブル","quantity":1,"borrower":"借り手A","actor":"借り手B","proxy":true}`))
	if got != "【返却】借り手Bさんが「HDMIケーブル」を1個返却しました（借りた人: 借り手Aさん）\n通知ID: id-3" {
		t.Errorf("代理返却: %q", got)
	}
	got, _ = Format("inventory.correction_created", "id-4", mk(`{"v":1,"itemName":"HDMIケーブル","kind":"adjust","target":"loan","delta":-1,"actor":"管理者"}`))
	if got != "【訂正】管理者さんが「HDMIケーブル」の貸出の数量を-1訂正しました\n通知ID: id-4" {
		t.Errorf("訂正: %q", got)
	}
	got, _ = Format("inventory.correction_created", "id-5", mk(`{"v":1,"itemName":"X","kind":"void","target":"return","actor":"管理者"}`))
	if !strings.Contains(got, "返却の記録を取り消しました") {
		t.Errorf("取消: %q", got)
	}
	if _, err := Format("unknown", "x", mk(`{}`)); err == nil {
		t.Error("未対応の種別を受け付けた")
	}
	if _, err := Format("inventory.loan_created", "x", mk(`not json`)); err == nil {
		t.Error("壊れたpayloadを受け付けた")
	}
}

// 利用者が決める表示名・物品名に、メンション・改行・長文があっても、本文が1行で無害になる。
func TestFormatNeutralizesUserControlledText(t *testing.T) {
	evil := `@everyone <@123456789012345678> <#123456789012345678>` + "\n偽の2行目\r " + strings.Repeat("あ", 500)
	got, err := Format("inventory.loan_created", "id", []byte(`{"v":1,"itemName":`+jsonStr(evil)+`,"quantity":1,"borrower":`+jsonStr(evil)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	body := strings.SplitN(got, "\n通知ID:", 2)[0]
	for _, bad := range []string{"@", "<", ">", "\n", "\r", " "} {
		if strings.Contains(body, bad) {
			t.Errorf("本文に %q が残る", bad)
		}
	}
	if n := len([]rune(body)); n > 400 {
		t.Errorf("長さが制限されていない: %d", n)
	}
}

func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestBackoff(t *testing.T) {
	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 30 * time.Minute, 30 * time.Minute}
	for i, w := range want {
		if got := backoff(int32(i + 1)); got != w {
			t.Errorf("attempt %d: %v（%vのはず）", i+1, got, w)
		}
	}
}
