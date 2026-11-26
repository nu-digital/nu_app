// Package config はぬる（services/agent）の設定。環境変数から読む。
// 秘密（Discordのtoken・Claude APIキー・DBのパスワード）は値を保持するが、文字列化・ログには決して出さない。
package config

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type Config struct {
	DiscordToken string
	AnthropicKey string
	DBHost       string
	DBName       string
	DBUser       string
	DBPassword   string
	GuildID      uint64
	TimesChannel uint64
	AdminRole    uint64
	HealthAddr   string
}

// Load は環境変数を読む。getenv を差し替えて試験できる。
func Load(getenv func(string) string) (Config, error) {
	c := Config{
		DiscordToken: strings.TrimSpace(getenv("NUL_DISCORD_TOKEN")),
		AnthropicKey: strings.TrimSpace(getenv("ANTHROPIC_API_KEY")),
		DBHost:       orDefault(getenv("NUL_DB_HOST"), "db:5432"),
		DBName:       strings.TrimSpace(getenv("NUL_DB_NAME")),
		DBUser:       orDefault(getenv("NUL_DB_USER"), "nu_agent"),
		DBPassword:   getenv("NUL_DB_PASSWORD"),
		HealthAddr:   orDefault(getenv("NUL_HEALTH_ADDR"), "127.0.0.1:8090"),
	}
	var errs []error
	for _, f := range []struct {
		name string
		dst  *uint64
	}{{"NUL_GUILD_ID", &c.GuildID}, {"NUL_TIMES_CHANNEL_ID", &c.TimesChannel}, {"NUL_ADMIN_ROLE_ID", &c.AdminRole}} {
		v := strings.TrimSpace(getenv(f.name))
		if v == "" {
			continue
		}
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil || n == 0 {
			errs = append(errs, fmt.Errorf("%s は数字のIDにしてください", f.name))
			continue
		}
		*f.dst = n
	}
	return c, errors.Join(errs...)
}

func orDefault(v, d string) string {
	if v = strings.TrimSpace(v); v != "" {
		return v
	}
	return d
}

// Missing は、ぬるを有効にするために足りない設定の名前（値は含まない）。空なら有効にできる。
func (c Config) Missing() []string {
	var m []string
	add := func(ok bool, name string) {
		if !ok {
			m = append(m, name)
		}
	}
	add(c.DiscordToken != "", "NUL_DISCORD_TOKEN")
	add(c.AnthropicKey != "", "ANTHROPIC_API_KEY")
	add(c.DBPassword != "", "NUL_DB_PASSWORD")
	add(c.DBName != "", "NUL_DB_NAME")
	add(c.GuildID != 0, "NUL_GUILD_ID")
	add(c.TimesChannel != 0, "NUL_TIMES_CHANNEL_ID")
	add(c.AdminRole != 0, "NUL_ADMIN_ROLE_ID")
	return m
}

// Enabled は、必要な設定がすべて揃っているか。揃っていなければ「無効モード」（Discordに接続せず、healthzだけ応答する）。
func (c Config) Enabled() bool { return len(c.Missing()) == 0 }

// DatabaseURL はPostgreSQLの接続URL（パスワードを含む。ログに出さない）。
func (c Config) DatabaseURL() string {
	u := url.URL{Scheme: "postgres", User: url.UserPassword(c.DBUser, c.DBPassword), Host: c.DBHost, Path: "/" + c.DBName}
	q := u.Query()
	q.Set("sslmode", "disable")
	u.RawQuery = q.Encode()
	return u.String()
}

// String は秘密を含めない。
func (c Config) String() string {
	return fmt.Sprintf("Config{enabled=%v guild=%d times=%d adminRole=%d db=%s/%s user=%s}", c.Enabled(), c.GuildID, c.TimesChannel, c.AdminRole, c.DBHost, c.DBName, c.DBUser)
}
