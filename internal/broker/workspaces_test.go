package broker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/berth-ai/berth/internal/config"
)

func TestWorkspacesFilePublished(t *testing.T) {
	tb := newTestBroker(t)
	tb.b.WorkspacesRoot = "/ws"
	tb.b.WorkspacesSettings = filepath.Join(tb.dir, "workspaces.json")
	tb.b.Load()
	if ws, err := config.ReadWorkspacesFile(tb.b.WorkspacesSettings); err != nil || len(ws) != 0 {
		t.Fatalf("at load = %v, %v", ws, err)
	}
	if _, err := os.Stat(tb.b.WorkspacesSettings); err != nil {
		t.Fatalf("Load did not write the file: %v", err)
	}
	if code, out := tb.verb(t, "config.apply", `{"base_revision":0,"patch":{"workspaces":{"api":{"path":"acme/api"}}}}`); code != 200 {
		t.Fatalf("apply = %d %v", code, out)
	}
	ws, _ := config.ReadWorkspacesFile(tb.b.WorkspacesSettings)
	if ws["api"].Path != "/ws/acme/api" {
		t.Errorf("after apply = %+v", ws)
	}
	if fi, _ := os.Stat(tb.b.WorkspacesSettings); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v", fi.Mode())
	}
	os.Remove(tb.b.WorkspacesSettings)
	tb.b.Load()
	if ws, _ := config.ReadWorkspacesFile(tb.b.WorkspacesSettings); ws["api"].Path == "" {
		t.Errorf("Load did not republish: %+v", ws)
	}
	os.WriteFile(filepath.Join(tb.dir, "config.json"), []byte(`{"schema":`), 0o600)
	tb.b.Load()
	if ws, _ := config.ReadWorkspacesFile(tb.b.WorkspacesSettings); ws["api"].Path == "" {
		t.Errorf("recovery overwrote workspaces.json: %+v", ws)
	}
}
