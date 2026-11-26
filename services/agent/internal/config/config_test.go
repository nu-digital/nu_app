package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

var full = map[string]string{
	"NUL_DISCORD_TOKEN": "tok-secret-1", "ANTHROPIC_API_KEY": "sk-secret-2", "NUL_DB_PASSWORD": "pw-secret-3", "NUL_DB_NAME": "nu_app",
	"NUL_GUILD_ID": "111", "NUL_TIMES_CHANNEL_ID": "222", "NUL_ADMIN_ROLE_ID": "333",
}

func TestEnabledOnlyWhenEverythingIsSet(t *testing.T) {
	c, err := Load(env(full))
	if err != nil || !c.Enabled() || len(c.Missing()) != 0 {
		t.Fatalf("%v %v %v", err, c.Enabled(), c.Missing())
	}
	for k := range full { // どれか1つ欠けたら無効
		m := map[string]string{}
		for kk, v := range full {
			if kk != k {
				m[kk] = v
			}
		}
		c, err := Load(env(m))
		if err != nil || c.Enabled() || len(c.Missing()) != 1 || c.Missing()[0] != k {
			t.Errorf("%s が無いのに有効: %v %v", k, c.Enabled(), c.Missing())
		}
	}
	if c, _ := Load(env(nil)); c.Enabled() || len(c.Missing()) != 7 {
		t.Errorf("空の環境: %v", c.Missing())
	}
}

func TestIDsMustBeNumbers(t *testing.T) {
	m := map[string]string{"NUL_GUILD_ID": "abc", "NUL_TIMES_CHANNEL_ID": "0", "NUL_ADMIN_ROLE_ID": "-1"}
	if _, err := Load(env(m)); err == nil || strings.Count(err.Error(), "数字のID") != 3 {
		t.Errorf("%v", err)
	}
}

func TestSecretsNeverAppearInStringOrMissing(t *testing.T) {
	c, _ := Load(env(full))
	s := c.String() + strings.Join(c.Missing(), ",")
	for _, secret := range []string{"tok-secret-1", "sk-secret-2", "pw-secret-3"} {
		if strings.Contains(s, secret) {
			t.Errorf("文字列に秘密が出ている: %s", secret)
		}
	}
}

func TestDatabaseURLEscapesPassword(t *testing.T) {
	c, _ := Load(env(map[string]string{"NUL_DB_PASSWORD": "p@ss/w:rd", "NUL_DB_NAME": "nu_app"}))
	if u := c.DatabaseURL(); u != "postgres://nu_agent:p%40ss%2Fw%3Ard@db:5432/nu_app?sslmode=disable" {
		t.Errorf("%s", u)
	}
}
