package smoke

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/berth-ai/berth/internal/runner"
)

type recorder struct {
	calls []runner.Cmd
	fail  func(runner.Cmd) bool
}

func (r *recorder) Run(_ context.Context, c runner.Cmd) error {
	r.calls = append(r.calls, c)
	if r.fail != nil && r.fail(c) {
		return os.ErrInvalid
	}
	return nil
}

func core(t *testing.T, dir, rel, version string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	os.MkdirAll(p, 0o755)
	os.WriteFile(filepath.Join(p, "package.json"), []byte(`{"version":"`+version+`"}`), 0o644)
}

func TestCheckPasses(t *testing.T) {
	dir := t.TempDir()
	core(t, dir, "node_modules/playwright-core", "1.63.0")
	r := &recorder{}
	if err := (Checker{Runner: r}).Check(context.Background(), dir, []string{"claude", "codex"}); err != nil {
		t.Fatal(err)
	}
	last := r.calls[len(r.calls)-1]
	if last.Path != "node" || last.Args[0] != "-e" || !last.AsAgent ||
		!slices.Contains(last.Env, "PLAYWRIGHT_BROWSERS_PATH="+filepath.Join(dir, "ms-playwright")) {
		t.Fatalf("chromium check = %+v", last)
	}
}

func TestCheckFallsBackToHelp(t *testing.T) {
	dir := t.TempDir()
	core(t, dir, "node_modules/playwright-core", "1.63.0")
	r := &recorder{fail: func(c runner.Cmd) bool { return len(c.Args) > 0 && c.Args[0] == "--version" }}
	if err := (Checker{Runner: r}).Check(context.Background(), dir, []string{"context7-mcp"}); err != nil {
		t.Fatal(err)
	}
}

func TestCheckFailures(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	core(t, dir, "node_modules/playwright-core", "1.63.0")
	r := &recorder{fail: func(c runner.Cmd) bool { return strings.HasSuffix(c.Path, "/bin/broken") }}
	if err := (Checker{Runner: r}).Check(ctx, dir, []string{"broken"}); err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("broken bin: %v", err)
	}
	core(t, dir, "node_modules/x/node_modules/playwright-core", "1.62.0")
	err := (Checker{Runner: &recorder{}}).Check(ctx, dir, nil)
	if err == nil || !strings.Contains(err.Error(), "1.62.0, 1.63.0") {
		t.Fatalf("duplicate core: %v", err)
	}
}

func TestBinsChecksOnlyTheBins(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "bin"), 0o755)
	os.WriteFile(filepath.Join(dir, "bin", "ok"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
	os.WriteFile(filepath.Join(dir, "bin", "bad"), []byte("#!/bin/sh\nexit 3\n"), 0o755)
	c := Checker{Runner: runner.Exec{Stdout: io.Discard, Stderr: io.Discard}, Timeout: 10 * time.Second}
	if err := c.Bins(context.Background(), dir, []string{"ok"}); err != nil {
		t.Errorf("ok: %v", err) // no node_modules/playwright, and none needed
	}
	if err := c.Bins(context.Background(), dir, []string{"ok", "bad"}); err == nil || !strings.Contains(err.Error(), "bad answers neither") {
		t.Errorf("bad: %v", err)
	}
}
