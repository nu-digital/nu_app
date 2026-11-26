package discord

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestClipKeepsWithinDiscordLimit(t *testing.T) {
	long := strings.Repeat("あ", 2500)
	if n := len([]rune(clip(long))); n != maxMessageRunes {
		t.Errorf("n=%d", n)
	}
	if clip("short") != "short" {
		t.Error("短い文はそのまま")
	}
}

func TestImageHostAllowlist(t *testing.T) {
	for u, want := range map[string]bool{
		"https://cdn.discordapp.com/attachments/1/2/a.png": true,
		"https://media.discordapp.net/a.png":               true,
		"http://cdn.discordapp.com/a.png":                  false,
		"https://evil.example.com/a.png":                   false,
		"https://cdn.discordapp.com.evil.example/a.png":    false,
		"https://127.0.0.1/a.png":                          false,
		"https://169.254.169.254/latest/meta-data":         false,
	} {
		p, _ := url.Parse(u)
		if allowedURL(p) != want {
			t.Errorf("%s: want %v", u, want)
		}
	}
	f := NewImageFetcher()
	if _, err := f.Fetch(context.Background(), Attachment{URL: "https://evil.example.com/a.png", Size: 10}); !errors.Is(err, ErrImageHost) {
		t.Errorf("許可外のホストは取得しない: %v", err)
	}
}
