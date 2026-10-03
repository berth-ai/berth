package agentlayer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeysRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets", "agents.env")
	if keys, err := ReadKeys(path); err != nil || len(keys) != 0 {
		t.Fatalf("missing file = %v %v", keys, err)
	}
	in := map[string]string{"OPENAI_API_KEY": "sk-b", "ANTHROPIC_API_KEY": "sk-a=="}
	if err := WriteKeys(path, in, -1, -1); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "ANTHROPIC_API_KEY=sk-a==\nOPENAI_API_KEY=sk-b\n" {
		t.Errorf("file = %q", raw)
	}
	fi, _ := os.Stat(path)
	di, _ := os.Stat(filepath.Dir(path))
	if fi.Mode().Perm() != 0o640 || di.Mode().Perm() != 0o711 {
		t.Errorf("modes %v %v", fi.Mode(), di.Mode())
	}
	got, err := ReadKeys(path)
	if err != nil || got["ANTHROPIC_API_KEY"] != "sk-a==" || len(got) != 2 {
		t.Errorf("read = %v %v", got, err)
	}
	if err := WriteKeys(path, map[string]string{}, -1, -1); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(path); len(raw) != 0 {
		t.Errorf("empty set = %q", raw)
	}
}

func TestKeyValues(t *testing.T) {
	for v, ok := range map[string]bool{
		"sk-ant-api03-AbC_d": true, "a=b$c`d;e": true, "": false, "has space": false,
		"new\nline": false, "tab\t": false, strings.Repeat("x", 4097): false, "é": false,
	} {
		if (ValidKeyValue(v) == nil) != ok {
			t.Errorf("%q valid=%v", v, !ok)
		}
	}
}

func TestReadKeysSkipsJunk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.env")
	os.WriteFile(path, []byte("# comment\nA_KEY=1\nnot a line\nlower=x\nB_KEY=2=3\n"), 0o640)
	got, _ := ReadKeys(path)
	if len(got) != 2 || got["B_KEY"] != "2=3" {
		t.Errorf("keys = %v", got)
	}
}
