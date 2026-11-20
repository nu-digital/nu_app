package bootstrap

import (
	"testing"

	"github.com/nu-digital/nu_app/services/api/internal/auth/token"
)

func TestTokenIsRandomAndOnlyItsHashIsDerivable(t *testing.T) {
	a, ha, err := token.New()
	if err != nil {
		t.Fatal(err)
	}
	b, hb, _ := token.New()
	if a == b || string(ha) == string(hb) {
		t.Fatal("tokenが重複した")
	}
	if len(ha) != 32 {
		t.Fatalf("hash長が不正: %d", len(ha))
	}
	got, ok := HashToken(a)
	if !ok || string(got) != string(ha) {
		t.Fatal("HashTokenが保存hashと一致しない")
	}
	for _, bad := range []string{"", "short", "!!!!", a + "x"} {
		if _, ok := HashToken(bad); ok {
			t.Errorf("不正なtoken %q が受理された", bad)
		}
	}
}
