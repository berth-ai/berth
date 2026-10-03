package cli

import (
	"strings"
	"testing"

	"github.com/berth-ai/berth/internal/testutil"
)

func TestMCPSyncWithoutAgentSet(t *testing.T) {
	cfg := testConfig(t)
	t.Setenv("HOME", t.TempDir())
	_, errOut, code := run(t, cfg, "mcp", "sync")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(errOut, "no agent set installed") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestMCPSyncAfterUpdate(t *testing.T) {
	cfg := testConfig(t)
	t.Setenv("HOME", t.TempDir())
	if _, errOut, code := run(t, cfg, "update", "--lock-dir", testutil.LockDir("lock-v1")); code != 0 {
		t.Fatalf("update exit %d: %s", code, errOut)
	}
	if _, errOut, code := run(t, cfg, "mcp", "sync"); code != 0 {
		t.Fatalf("sync exit %d: %s", code, errOut)
	}
}

func TestMCPUsage(t *testing.T) {
	cfg := testConfig(t)
	for _, args := range [][]string{{"mcp"}, {"mcp", "list"}, {"mcp", "sync", "extra"}} {
		if _, _, code := run(t, cfg, args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
	out, _, _ := run(t, cfg, "help")
	if strings.Contains(out, "mcp") {
		t.Errorf("mcp is listed in help:\n%s", out)
	}
}
