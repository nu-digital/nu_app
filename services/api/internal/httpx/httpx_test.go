package httpx

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func req(remote string, hdr map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/x", nil)
	r.RemoteAddr = remote
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	return r
}

func TestClientIP(t *testing.T) {
	proxy := []netip.Prefix{netip.MustParsePrefix("172.18.0.0/16")}
	cases := []struct {
		name    string
		r       *http.Request
		trusted []netip.Prefix
		want    string
	}{
		{"信頼プロキシなしならXFFを無視(偽装を防ぐ)", req("203.0.113.9:5555", map[string]string{"X-Forwarded-For": "1.2.3.4"}), nil, "203.0.113.9"},
		{"信頼プロキシ外からのXFFは無視", req("203.0.113.9:5555", map[string]string{"X-Forwarded-For": "1.2.3.4"}), proxy, "203.0.113.9"},
		{"信頼プロキシ経由は右端を採用(左の偽装値は無視)", req("172.18.0.2:5555", map[string]string{"X-Forwarded-For": "6.6.6.6, 198.51.100.7"}), proxy, "198.51.100.7"},
		{"信頼プロキシでXFFなしならプロキシ自身", req("172.18.0.2:5555", nil), proxy, "172.18.0.2"},
		{"右端が不正ならプロキシ自身", req("172.18.0.2:5555", map[string]string{"X-Forwarded-For": "1.2.3.4, not-an-ip"}), proxy, "172.18.0.2"},
		{"IPv4射影IPv6", req("[::ffff:203.0.113.9]:1", nil), nil, "203.0.113.9"},
	}
	for _, c := range cases {
		if got := ClientIP(c.r, c.trusted); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

func TestOriginAllowed(t *testing.T) {
	const ok = "https://app.example.invalid"
	cases := []struct {
		name string
		hdr  map[string]string
		want bool
	}{
		{"Origin一致", map[string]string{"Origin": ok}, true},
		{"Origin不一致", map[string]string{"Origin": "https://evil.example"}, false},
		{"Origin: null", map[string]string{"Origin": "null"}, false},
		{"どちらも無い", nil, false},
		{"Refererのオリジンが一致", map[string]string{"Referer": ok + "/page?x=1"}, true},
		{"Refererが別オリジン", map[string]string{"Referer": "https://evil.example/" + ok}, false},
		{"Refererが不正", map[string]string{"Referer": "::bad"}, false},
		{"cross-siteは一致でも拒否", map[string]string{"Origin": ok, "Sec-Fetch-Site": "cross-site"}, false},
		{"same-originは許可", map[string]string{"Origin": ok, "Sec-Fetch-Site": "same-origin"}, true},
		{"サブドメインの偽装", map[string]string{"Origin": "https://app.example.invalid.evil.example"}, false},
	}
	for _, c := range cases {
		if got := OriginAllowed(req("127.0.0.1:1", c.hdr), ok); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestErrorShapeAndRequestID(t *testing.T) {
	h := WithRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Error(w, r, http.StatusForbidden, "FORBIDDEN", "権限がありません")
	}))
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Request-Id", "attacker-controlled")
	h.ServeHTTP(rec, r)
	id := rec.Header().Get("X-Request-Id")
	if id == "" || id == "attacker-controlled" {
		t.Fatalf("リクエストIDは受け取らず新規発行する: %q", id)
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Code != 403 {
		t.Fatalf("ヘッダー/ステータス: %v %d", rec.Header(), rec.Code)
	}
	body := rec.Body.String()
	if want := `"requestId":"` + id + `"`; !contains(body, want) || !contains(body, `"code":"FORBIDDEN"`) {
		t.Fatalf("エラー形式がAPI.mdと異なる: %s", body)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
