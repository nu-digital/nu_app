// Package config は環境変数から設定を読む。値は秘密を含むためログへ出さない。
package config

import (
	"errors"
	"os"
	"time"
)

type Config struct {
	Addr        string
	DatabaseURL string
	// ReadyTimeout は /readyz のDB確認の待ち時間。
	ReadyTimeout time.Duration
}

// Load は環境変数を読む。DATABASE_URL は必須（未設定なら起動しない）。
func Load(getenv func(string) string) (Config, error) {
	c := Config{
		Addr:         valueOr(getenv("API_ADDR"), ":8080"),
		DatabaseURL:  getenv("DATABASE_URL"),
		ReadyTimeout: 2 * time.Second,
	}
	if c.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL が未設定です")
	}
	return c, nil
}

func valueOr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// OS は os.Getenv を使うLoad用の別名。
func FromOS() (Config, error) { return Load(os.Getenv) }
