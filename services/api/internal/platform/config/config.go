// Package config は環境変数から設定を読む。値は秘密を含むためログへ出さない。
package config

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"
)

type Config struct {
	Addr        string
	DatabaseURL string
	// ReadyTimeout は /readyz のDB確認の待ち時間。
	ReadyTimeout time.Duration

	// AppBaseURL はアプリの公開URL（例: https://app.nu-digital.jp、ローカルは http://127.0.0.1:3000）。
	// 許可するOrigin、招待/再設定URLの生成、Cookieの Secure 判定に使う。
	AppBaseURL string
	// AllowedOrigin は AppBaseURL から導いた "scheme://host[:port]"。状態変更リクエストのOrigin検証に使う。
	AllowedOrigin string
	// SecureCookies は Cookie に Secure を付ける。AppBaseURL が https なら必ず true（無効化は起動失敗）。
	SecureCookies bool
	// TrustedProxies は X-Forwarded-For を信用するプロキシのアドレス範囲。空なら信用しない（RemoteAddrのみ）。
	TrustedProxies []netip.Prefix
	// DiscordBotToken はDiscord Botのtoken（秘密）。環境変数 DISCORD_BOT_TOKEN からだけ読む。
	// 空ならDiscordへ送るworkerを起動せず、通知はoutboxに溜まる。ログ・DB・エラー文言に出さない。
	DiscordBotToken string
	// MediaDir は画像の保存先（永続ボリューム。絶対パス）。空ならメディアのAPIは503。Git・イメージの外に置く。
	MediaDir string
}

// Load は環境変数を読む。DATABASE_URL と APP_BASE_URL は必須（未設定なら起動しない）。
func Load(getenv func(string) string) (Config, error) {
	c := Config{
		Addr:         valueOr(getenv("API_ADDR"), ":8080"),
		DatabaseURL:  getenv("DATABASE_URL"),
		ReadyTimeout: 2 * time.Second,
	}
	if c.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL が未設定です")
	}
	base := strings.TrimRight(getenv("APP_BASE_URL"), "/")
	if base == "" {
		return Config{}, errors.New("APP_BASE_URL が未設定です（例: https://app.nu-digital.jp）")
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return Config{}, errors.New("APP_BASE_URL は http(s)://host[:port] の形式にしてください")
	}
	c.AppBaseURL = base
	c.AllowedOrigin = u.Scheme + "://" + u.Host

	insecure := getenv("API_INSECURE_COOKIES") == "true"
	switch {
	case u.Scheme == "https" && insecure:
		// 開発用の設定が本番に紛れてCookieが平文HTTPで送られるのを防ぐ。
		return Config{}, errors.New("APP_BASE_URL がhttpsのとき API_INSECURE_COOKIES は使えません")
	case u.Scheme == "http" && !insecure:
		return Config{}, errors.New("httpの APP_BASE_URL（ローカル開発）では API_INSECURE_COOKIES=true を明示してください")
	}
	c.SecureCookies = !insecure

	for _, part := range strings.Split(getenv("API_TRUSTED_PROXY_CIDRS"), ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		p, err := parsePrefix(part)
		if err != nil {
			return Config{}, fmt.Errorf("API_TRUSTED_PROXY_CIDRS が不正です: %q", part)
		}
		c.TrustedProxies = append(c.TrustedProxies, p)
	}
	c.MediaDir = strings.TrimSpace(getenv("MEDIA_DIR"))
	if c.MediaDir != "" && !strings.HasPrefix(c.MediaDir, "/") {
		return Config{}, errors.New("MEDIA_DIR は絶対パスで指定してください")
	}
	c.DiscordBotToken = strings.TrimSpace(getenv("DISCORD_BOT_TOKEN"))
	for _, r := range c.DiscordBotToken {
		if r < 0x21 || r == 0x7f {
			// 値は出さない（秘密のため）。改行などが混ざるとHTTPヘッダーを壊すため拒否する
			return Config{}, errors.New("DISCORD_BOT_TOKEN に空白や制御文字を含めないでください")
		}
	}
	return c, nil
}

func parsePrefix(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		return netip.ParsePrefix(s)
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(a, a.BitLen()), nil
}

func valueOr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// FromOS は os.Getenv を使うLoad用の別名。
func FromOS() (Config, error) { return Load(os.Getenv) }
