package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspacesFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspaces.json")
	pub := PublishWorkspaces("/workspaces", map[string]Workspace{"api": {Path: "acme/api"}})
	if pub["api"].Path != "/workspaces/acme/api" {
		t.Fatalf("published = %+v", pub)
	}
	if err := WriteWorkspacesFile(path, pub, -1, -1); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v", fi.Mode())
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `"api": {`) || !strings.Contains(string(raw), `"schema": 1`) {
		t.Errorf("file = %s", raw)
	}
	got, err := ReadWorkspacesFile(path)
	if err != nil || got["api"].Path != "/workspaces/acme/api" {
		t.Errorf("read = %+v, %v", got, err)
	}
}

func TestWorkspacesFileMissingAndNewer(t *testing.T) {
	dir := t.TempDir()
	if ws, err := ReadWorkspacesFile(filepath.Join(dir, "none.json")); err != nil || ws == nil || len(ws) != 0 {
		t.Errorf("missing = %v, %v", ws, err)
	}
	newer := filepath.Join(dir, "newer.json")
	os.WriteFile(newer, []byte(`{"schema":2,"workspaces":{}}`), 0o640)
	if _, err := ReadWorkspacesFile(newer); err == nil || !strings.Contains(err.Error(), "schema 2") {
		t.Errorf("newer = %v", err)
	}
}
