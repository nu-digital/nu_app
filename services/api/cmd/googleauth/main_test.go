package main

import (
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthorizeURLHasRequiredParameters(t *testing.T) {
	u, err := url.Parse(AuthorizeURL("cid", "http://127.0.0.1:1234/callback", "st", "ch"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	for k, want := range map[string]string{"client_id": "cid", "redirect_uri": "http://127.0.0.1:1234/callback", "response_type": "code",
		"access_type": "offline", "prompt": "consent", "state": "st", "code_challenge": "ch", "code_challenge_method": "S256"} {
		if q.Get(k) != want {
			t.Errorf("%s=%q", k, q.Get(k))
		}
	}
	if !strings.Contains(q.Get("scope"), "drive.file") || !strings.Contains(q.Get("scope"), "spreadsheets") || strings.Contains(q.Get("scope"), "auth/drive ") {
		t.Errorf("scope: %q", q.Get("scope"))
	}
}

func TestPKCEAndState(t *testing.T) {
	v, c := pkce()
	if len(v) != 48 || c == "" || c == v {
		t.Errorf("%q %q", v, c)
	}
	if randomString(24) == randomString(24) {
		t.Error("乱数が同じ")
	}
}

func TestCheckScopesRequiresAll(t *testing.T) {
	if checkScopes("https://www.googleapis.com/auth/drive.file https://www.googleapis.com/auth/spreadsheets") != nil {
		t.Error("そろっているのに拒否")
	}
	if checkScopes("https://www.googleapis.com/auth/drive.file") == nil {
		t.Error("不足を検出できない")
	}
}

func TestWriteSecretFileIsPrivateAndNeverOverwrites(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tok.env")
	if err := writeSecretFile(p, "A=1\n"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("権限: %v %v", fi, err)
	}
	if err := writeSecretFile(p, "B=2\n"); err == nil {
		t.Error("既存のファイルを上書きできた")
	}
	if b, _ := os.ReadFile(p); string(b) != "A=1\n" {
		t.Errorf("中身が変わった: %q", b)
	}
}

func TestReadCredentialsRejectsNonTerminal(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "in")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, _, err := readCredentials(f, io.Discard); err == nil {
		t.Error("端末でない入力（ファイル・パイプ）を受け付けた")
	}
}
