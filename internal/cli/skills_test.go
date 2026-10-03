package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berth-ai/berth/internal/testutil"
)

// skillRepo makes a file:// repository holding SKILL.md at skills/demo.
func skillRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "skills", "demo"), 0o755)
	os.WriteFile(filepath.Join(dir, "skills", "demo", "SKILL.md"), []byte("---\nname: demo\ndescription: Demo\n---\n"), 0o644)
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"},
		{"-c", "user.name=t", "-c", "user.email=t@example.org", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "c"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return "file://" + dir
}

func TestSkillsResolve(t *testing.T) {
	cfg := testConfig(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BERTH_SKILLS_ALLOW_FILE", "1")
	cfg.Getenv = os.Getenv
	repo := skillRepo(t)
	out, errOut, code := run(t, cfg, "skills", "resolve", "--json", "--repo", repo, "--path", "skills/demo", "--ref", "main")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var res struct{ Name, Description, Commit string }
	if err := json.Unmarshal([]byte(out), &res); err != nil || res.Name != "demo" || len(res.Commit) != 40 {
		t.Errorf("out = %s (%v)", out, err)
	}
	out, _, code = run(t, cfg, "skills", "resolve", "--json", "--repo", repo, "--path", "skills/demo", "--ref", "nope")
	if e := resolveError(t, out); code != 1 || e.Code != "ref_not_found" || e.Step != "ref" {
		t.Errorf("missing ref: exit %d, %s", code, out)
	}
}

func TestSkillsResolveRefusesFileByDefault(t *testing.T) {
	cfg := testConfig(t)
	t.Setenv("HOME", t.TempDir())
	cfg.Getenv = os.Getenv
	out, _, code := run(t, cfg, "skills", "resolve", "--json", "--repo", skillRepo(t), "--ref", "main")
	if e := resolveError(t, out); code != 1 || e.Step != "repo" {
		t.Errorf("exit %d, %s", code, out)
	}
}

type cliError struct{ Code, Step, Message string }

func resolveError(t *testing.T, out string) cliError {
	t.Helper()
	var doc struct{ Error cliError }
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("%v: %q", err, out)
	}
	return doc.Error
}

func TestSkillsSyncAfterUpdate(t *testing.T) {
	cfg := testConfig(t)
	t.Setenv("HOME", t.TempDir())
	cfg.Getenv = os.Getenv
	if _, errOut, code := run(t, cfg, "update", "--lock-dir", testutil.LockDir("lock-v1")); code != 0 {
		t.Fatalf("update exit %d: %s", code, errOut)
	}
	if _, errOut, code := run(t, cfg, "skills", "sync"); code != 0 {
		t.Fatalf("sync exit %d: %s", code, errOut)
	}
}

func TestSkillsUsage(t *testing.T) {
	cfg := testConfig(t)
	for _, args := range [][]string{{"skills"}, {"skills", "list"}, {"skills", "sync", "extra"}, {"skills", "resolve", "extra"}} {
		if _, _, code := run(t, cfg, args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
	out, _, _ := run(t, cfg, "help")
	if strings.Contains(out, "skills") {
		t.Errorf("skills is listed in help:\n%s", out)
	}
}

// convergers points MCPSetup and SkillsSync at scripts that append their
// name to the returned file, so tests can check the order they ran in.
func convergers(t *testing.T, cfg *Config) string {
	t.Helper()
	dir := t.TempDir()
	order := filepath.Join(dir, "order")
	for _, name := range []string{"mcp", "skills"} {
		script := filepath.Join(dir, name)
		os.WriteFile(script, []byte("#!/bin/sh\necho "+name+" >> "+order+"\n"), 0o755)
		if name == "mcp" {
			cfg.MCPSetup = []string{script}
		} else {
			cfg.SkillsSync = []string{script}
		}
	}
	return order
}

func TestUpdateAndRollbackSyncSkillsAfterMCP(t *testing.T) {
	cfg := testConfig(t)
	order := convergers(t, &cfg)
	run(t, cfg, "update", "--lock-dir", testutil.LockDir("lock-v1"))
	if got, _ := os.ReadFile(order); string(got) != "mcp\nskills\n" {
		t.Errorf("update ran %q", got)
	}
	run(t, cfg, "update", "--lock-dir", testutil.LockDir("lock-v2"))
	os.Remove(order)
	if _, errOut, code := run(t, cfg, "rollback"); code != 0 {
		t.Fatalf("rollback exit %d: %s", code, errOut)
	}
	if got, _ := os.ReadFile(order); string(got) != "mcp\nskills\n" {
		t.Errorf("rollback ran %q", got)
	}
}
