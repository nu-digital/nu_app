package config

import (
	"net/netip"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

var base = map[string]string{"DATABASE_URL": "postgres://u:p@h/d", "APP_BASE_URL": "https://app.example.invalid"}

func with(extra map[string]string) map[string]string {
	m := map[string]string{}
	for k, v := range base {
		m[k] = v
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func TestLoadRequiresDatabaseURLAndBaseURL(t *testing.T) {
	if _, err := Load(env(nil)); err == nil {
		t.Fatal("DATABASE_URL 未設定でエラーにならない")
	}
	if _, err := Load(env(map[string]string{"DATABASE_URL": "x"})); err == nil {
		t.Fatal("APP_BASE_URL 未設定でエラーにならない")
	}
}

func TestLoadDefaultsAndOverride(t *testing.T) {
	c, err := Load(env(base))
	if err != nil || c.Addr != ":8080" || c.AllowedOrigin != "https://app.example.invalid" || !c.SecureCookies {
		t.Fatalf("既定値が想定と異なる: %+v %v", c, err)
	}
	c, _ = Load(env(with(map[string]string{"API_ADDR": "127.0.0.1:9000", "APP_BASE_URL": "https://app.example.invalid:8443/"})))
	if c.Addr != "127.0.0.1:9000" || c.AllowedOrigin != "https://app.example.invalid:8443" || c.AppBaseURL != "https://app.example.invalid:8443" {
		t.Fatalf("上書きが反映されない: %+v", c)
	}
}

func TestBaseURLValidation(t *testing.T) {
	for _, bad := range []string{"ftp://x", "app.example", "https://", "https://u:p@host", "https://host/path", "https://host?x=1", "https://host#f"} {
		if _, err := Load(env(with(map[string]string{"APP_BASE_URL": bad}))); err == nil {
			t.Errorf("不正な APP_BASE_URL %q が受理された", bad)
		}
	}
}

// 本番(https)でSecure無効の設定は起動できない。ローカルhttpはSecure無効を明示したときだけ起動できる。
func TestSecureCookieSafetyChecks(t *testing.T) {
	if _, err := Load(env(with(map[string]string{"API_INSECURE_COOKIES": "true"}))); err == nil {
		t.Fatal("httpsなのにSecure無効の設定で起動できてしまう")
	}
	if _, err := Load(env(with(map[string]string{"APP_BASE_URL": "http://127.0.0.1:3000"}))); err == nil {
		t.Fatal("httpで明示なしに起動できてしまう")
	}
	c, err := Load(env(with(map[string]string{"APP_BASE_URL": "http://127.0.0.1:3000", "API_INSECURE_COOKIES": "true"})))
	if err != nil || c.SecureCookies {
		t.Fatalf("ローカルhttpの開発設定: %+v %v", c, err)
	}
}

func TestTrustedProxyParsing(t *testing.T) {
	c, err := Load(env(with(map[string]string{"API_TRUSTED_PROXY_CIDRS": "172.18.0.0/16, 10.0.0.5 ,::1"})))
	if err != nil || len(c.TrustedProxies) != 3 {
		t.Fatalf("信頼プロキシの解析: %+v %v", c.TrustedProxies, err)
	}
	if !c.TrustedProxies[1].Contains(netip.MustParseAddr("10.0.0.5")) || c.TrustedProxies[1].Contains(netip.MustParseAddr("10.0.0.6")) {
		t.Fatal("単一アドレスの指定は/32になるはず")
	}
	if _, err := Load(env(with(map[string]string{"API_TRUSTED_PROXY_CIDRS": "not-an-ip"}))); err == nil {
		t.Fatal("不正な指定が受理された")
	}
	if c, _ := Load(env(base)); len(c.TrustedProxies) != 0 {
		t.Fatal("既定では誰も信頼しない")
	}
}
