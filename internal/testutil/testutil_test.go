package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	RunFakeToolIfRequested()
	os.Exit(m.Run())
}

func TestFakeNPMInstallsDependencies(t *testing.T) {
	FakeTools(t)
	dir := t.TempDir()
	raw, _ := os.ReadFile(filepath.Join(LockDir("lock-v1"), "package.json"))
	os.WriteFile(filepath.Join(dir, "package.json"), raw, 0o644)
	cmd := exec.Command("npm", "ci")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, p := range []string{"node_modules/fake-agent/cli.js", "node_modules/playwright-core/package.json"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFakeNPMFailSwitch(t *testing.T) {
	FakeTools(t)
	t.Setenv("BERTH_FAKE_NPM_FAIL", "1")
	if err := exec.Command("npm", "ci").Run(); err == nil {
		t.Fatal("expected failure")
	}
}
