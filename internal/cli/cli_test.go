package cli

import (
	"bytes"
	"context"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/salgnt/berth/internal/locksource"
	"github.com/salgnt/berth/internal/runner"
	"github.com/salgnt/berth/internal/testutil"
)

var update = flag.Bool("update", false, "rewrite golden files")

func TestMain(m *testing.M) {
	testutil.RunFakeToolIfRequested()
	os.Exit(m.Run())
}

var now = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

func testConfig(t *testing.T) Config {
	t.Helper()
	testutil.FakeTools(t)
	root := t.TempDir()
	abs, _ := filepath.Abs("testdata")
	return Config{
		AgentsDir:   filepath.Join(root, "agents"),
		SeedDir:     testutil.LockDir("lock-v1"),
		BuildInfo:   filepath.Join(abs, "build-info.json"),
		MiseConfig:  filepath.Join(abs, "mise.toml"),
		ProcRoot:    filepath.Join(root, "proc"),
		GitHub:      locksource.GitHub{Repo: "example/berth"},
		Keep:        3,
		LockTimeout: 200 * time.Millisecond,
		IsRoot:      true,
		Runner:      runner.Exec{Stdout: io.Discard, Stderr: io.Discard},
		Now:         func() time.Time { return now },
		Version:     "1.2.3",
		Commit:      "abc1234",
	}
}

func run(t *testing.T, cfg Config, args ...string) (string, string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Run(context.Background(), cfg, args, &out, &errOut)
	return out.String(), errOut.String(), code
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".json")
	if *update {
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte(got), 0o644)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/cli -update)", err)
	}
	if got != string(want) {
		t.Fatalf("%s mismatch\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

func TestVersion(t *testing.T) {
	out, _, code := run(t, testConfig(t), "version")
	if code != 0 || out != "berth 1.2.3 (abc1234)\n" {
		t.Fatalf("%d %q", code, out)
	}
}

func TestUsage(t *testing.T) {
	cfg := testConfig(t)
	if _, _, code := run(t, cfg); code != 2 {
		t.Fatalf("no args: %d", code)
	}
	if _, _, code := run(t, cfg, "frobnicate"); code != 2 {
		t.Fatalf("unknown: %d", code)
	}
	if out, _, code := run(t, cfg, "help"); code != 0 || !strings.Contains(out, "rollback") {
		t.Fatalf("help: %d %q", code, out)
	}
	out, _, code := run(t, cfg, "status", "--bogus", "--json")
	if code != 2 {
		t.Fatalf("unknown flag: %d", code)
	}
	golden(t, "error_usage", out)
}

func TestStatusEmptyAsNonRoot(t *testing.T) {
	cfg := testConfig(t)
	cfg.IsRoot = false
	out, _, code := run(t, cfg, "status", "--json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	golden(t, "status_empty", out)
}

