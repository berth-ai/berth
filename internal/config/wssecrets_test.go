package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestWorkspaceSecretNames(t *testing.T) {
	if n := WorkspaceSecretName("acme-api", "STRIPE_KEY"); n != "workspaces.acme-api.STRIPE_KEY" {
		t.Errorf("name = %s", n)
	}
	for name, want := range map[string][3]any{
		"workspaces.acme-api.STRIPE_KEY": {"acme-api", "STRIPE_KEY", true},
		"workspaces.acme-api":            {"", "", false},
		"workspaces.Acme.KEY":            {"", "", false},
		"workspaces.api.lower":           {"", "", false},
		"workspaces.api.PATH":            {"", "", false},
		"ANTHROPIC_API_KEY":              {"", "", false},
		"workspaces.api.A.B":             {"", "", false},
	} {
		id, key, ok := ParseWorkspaceSecretName(name)
		if id != want[0] || key != want[1] || ok != want[2] {
			t.Errorf("%s = %q %q %v", name, id, key, ok)
		}
	}
}

func TestWorkspaceSecretsFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "workspaces")
	if env, err := ReadWorkspaceSecrets(dir, "api"); err != nil || len(env) != 0 {
		t.Fatalf("missing = %v, %v", env, err)
	}
	val := "line1\nline2 'q' \"d\" $(x)"
	if err := WriteWorkspaceSecrets(dir, "api", map[string]string{"KEY": val}, -1, -1); err != nil {
		t.Fatal(err)
	}
	env, err := ReadWorkspaceSecrets(dir, "api")
	if err != nil || env["KEY"] != val {
		t.Fatalf("read = %v, %v", env, err)
	}
	fi, _ := os.Stat(filepath.Join(dir, "api.json"))
	if fi.Mode().Perm() != 0o640 {
		t.Errorf("file mode = %v", fi.Mode())
	}
	if di, _ := os.Stat(dir); di.Mode().Perm() != 0o711 {
		t.Errorf("folder mode = %v", di.Mode())
	}
	if ids, _ := WorkspaceSecretIDs(dir); !slices.Equal(ids, []string{"api"}) {
		t.Errorf("ids = %v", ids)
	}
	if err := WriteWorkspaceSecrets(dir, "api", map[string]string{}, -1, -1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "api.json")); !os.IsNotExist(err) {
		t.Errorf("empty secrets kept the file: %v", err)
	}
	if err := WriteWorkspaceSecrets(dir, "api", map[string]string{"KEY": "a\x00b"}, -1, -1); err == nil {
		t.Error("a NUL value was written")
	}
	os.WriteFile(filepath.Join(dir, "x.json"), []byte(`{"schema":2,"env":{}}`), 0o640)
	if _, err := ReadWorkspaceSecrets(dir, "x"); err == nil {
		t.Error("a newer schema was read")
	}
}
