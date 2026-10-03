package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berth-ai/bert/internal/agentlayer"
	"github.com/berth-ai/bert/internal/runner"
)

func layerConfig(t *testing.T) Config {
	t.Helper()
	cfg := testConfig(t)
	cfg.ConfigDir = filepath.Join(t.TempDir(), "berth")
	cfg.KeysPath = filepath.Join(cfg.ConfigDir, "secrets", "agents.env")
	return cfg
}

func TestUpdateBuildsLayer(t *testing.T) {
	cfg := layerConfig(t)
	if _, errOut, code := run(t, cfg, "update", "--lock-dir", cfg.SeedDir, "--json"); code != 0 {
		t.Fatalf("update: %d %s", code, errOut)
	}
	entries, err := os.ReadDir(filepath.Join(cfg.ConfigDir, "bin"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("layer = %v %v", entries, err)
	}
	wrapper, _ := os.ReadFile(filepath.Join(cfg.ConfigDir, "bin", entries[0].Name()))
	if !strings.Contains(string(wrapper), filepath.Join(cfg.AgentsDir, "current", "bin")) {
		t.Errorf("wrapper = %s", wrapper)
	}
}

func TestLayerHonoursDisabled(t *testing.T) {
	cfg := layerConfig(t)
	os.MkdirAll(cfg.ConfigDir, 0o755)
	os.WriteFile(filepath.Join(cfg.ConfigDir, "config.json"), []byte(`{"schema":1,"revision":1,"agents":{"disabled":["fake-agent"]}}`), 0o600)
	run(t, cfg, "update", "--lock-dir", cfg.SeedDir, "--json")
	if _, err := os.Stat(filepath.Join(cfg.ConfigDir, "bin", "fake-agent")); !os.IsNotExist(err) {
		t.Errorf("disabled package has a wrapper: %v", err)
	}
}

func TestBootFixesKeysGroup(t *testing.T) {
	cfg := layerConfig(t)
	agentlayer.WriteKeys(cfg.KeysPath, map[string]string{"A_KEY": "x"}, -1, -1)
	got := -2
	fixOwner = func(path string, gid int) error { got = gid; return nil }
	t.Cleanup(func() { fixOwner = agentlayer.FixOwner })
	cfg.Agent = &runner.Identity{UID: 1000, GID: 4242}
	a := &app{cfg: cfg}
	a.fixKeysOwner()
	if got != 4242 {
		t.Errorf("FixOwner gid = %d", got)
	}
}
