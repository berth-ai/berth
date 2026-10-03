package login

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHashAndCheck(t *testing.T) {
	h, err := Hash("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=65536,t=1,p=4$") {
		t.Errorf("hash = %s", h)
	}
	if h2, _ := Hash("correct horse battery"); h2 == h {
		t.Error("salt is not random")
	}
	for pw, want := range map[string]bool{"correct horse battery": true, "correct horse batter": false, "": false} {
		if ok, err := Check(h, pw); err != nil || ok != want {
			t.Errorf("Check(%q) = %v %v", pw, ok, err)
		}
	}
}

func TestCheckRejectsMalformedHashes(t *testing.T) {
	for _, h := range []string{
		"", "plain", "$argon2i$v=19$m=65536,t=1,p=4$c2FsdA$a2V5",
		"$argon2id$v=18$m=65536,t=1,p=4$c2FsdA$a2V5",
		"$argon2id$v=19$m=9999999,t=1,p=4$c2FsdA$a2V5",
		"$argon2id$v=19$m=65536,t=1,p=4$!!$a2V5",
	} {
		if _, err := Check(h, "x"); err == nil {
			t.Errorf("Check accepted %q", h)
		}
	}
}

func TestValidation(t *testing.T) {
	for u, ok := range map[string]bool{"me": true, "ops@example.org": true, "a.b-c_d": true, "": false, "two words": false, strings.Repeat("a", 65): false} {
		if (ValidUser(u) == nil) != ok {
			t.Errorf("ValidUser(%q) ok=%v", u, !ok)
		}
	}
	for p, ok := range map[string]bool{"0123456789": true, "short": false, "tab\tinside1": false, strings.Repeat("a", 257): false, "unicodé passphrase": true} {
		if (ValidPassword(p) == nil) != ok {
			t.Errorf("ValidPassword(%q) ok=%v", p, !ok)
		}
	}
}

func TestStore(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "secrets", "panel.json")}
	if ok, err := s.Exists(); ok || err != nil {
		t.Fatalf("Exists before Set = %v %v", ok, err)
	}
	if err := s.Set("me", "short"); err == nil {
		t.Fatal("Set accepted a short password")
	}
	if err := s.Set("me", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(s.Path)
	di, _ := os.Stat(filepath.Dir(s.Path))
	if fi.Mode().Perm() != 0o600 || di.Mode().Perm() != 0o711 {
		t.Errorf("modes file %v dir %v", fi.Mode(), di.Mode())
	}
	raw, _ := os.ReadFile(s.Path)
	if strings.Contains(string(raw), "correct horse") {
		t.Fatal("password stored in clear")
	}
	if u, err := s.User(); u != "me" || err != nil {
		t.Errorf("User = %q %v", u, err)
	}
	for _, c := range []struct {
		user, pw string
		want     bool
	}{{"me", "correct horse battery", true}, {"you", "correct horse battery", false}, {"me", "wrong password!", false}} {
		if ok, err := s.Verify(c.user, c.pw); err != nil || ok != c.want {
			t.Errorf("Verify(%s, %s) = %v %v", c.user, c.pw, ok, err)
		}
	}
	if err := s.Delete(); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(); err != nil {
		t.Errorf("second Delete: %v", err)
	}
	if ok, _ := s.Exists(); ok {
		t.Error("login survives Delete")
	}
}

func TestTokens(t *testing.T) {
	a, b := NewToken(), NewToken()
	if len(a) != 32 || a == b || strings.Trim(a, "0123456789abcdef") != "" {
		t.Errorf("tokens %q %q", a, b)
	}
	if !TokenEqual(a, a) || TokenEqual(a, b) || TokenEqual(a, "") || TokenEqual("", "") {
		t.Error("TokenEqual")
	}
}
