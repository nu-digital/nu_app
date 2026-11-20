package config

import "testing"

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoadRequiresDatabaseURL(t *testing.T) {
	if _, err := Load(env(nil)); err == nil {
		t.Fatal("DATABASE_URL 未設定でエラーにならない")
	}
}

func TestLoadDefaultsAndOverride(t *testing.T) {
	c, err := Load(env(map[string]string{"DATABASE_URL": "postgres://u:p@h/d"}))
	if err != nil || c.Addr != ":8080" {
		t.Fatalf("既定値が想定と異なる: %+v %v", c, err)
	}
	c, _ = Load(env(map[string]string{"DATABASE_URL": "x", "API_ADDR": "127.0.0.1:9000"}))
	if c.Addr != "127.0.0.1:9000" {
		t.Fatalf("API_ADDR が反映されない: %s", c.Addr)
	}
}
