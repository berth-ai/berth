package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berth-ai/bert/internal/agentstore"
	"github.com/berth-ai/bert/internal/locksource"
	"github.com/berth-ai/bert/internal/testutil"
)

func hashOf(t *testing.T, fixture string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(testutil.LockDir(fixture), "package-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	return locksource.Hash(raw)
}

func current(t *testing.T, cfg Config) string {
	t.Helper()
	v, _ := agentstore.Store{Root: cfg.AgentsDir}.Current()
	if v == nil {
		return ""
	}
	return v.Hash
}

func decode(t *testing.T, out string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("%v: %q", err, out)
	}
	return m
}

func TestUpdateFromLockDir(t *testing.T) {
	cfg := testConfig(t)
	marker := filepath.Join(t.TempDir(), "mcp-ran")
	script := filepath.Join(t.TempDir(), "mcp-setup")
	os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755)
	cfg.MCPSetup = script

	out, errOut, code := run(t, cfg, "update", "--lock-dir", testutil.LockDir("lock-v1"), "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	golden(t, "update_lockdir", out)
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("berth-mcp-setup did not run after update")
	}
	out, _, code = run(t, cfg, "update", "--lock-dir", testutil.LockDir("lock-v1"), "--json")
	if code != 0 {
		t.Fatalf("second update exit %d", code)
	}
	golden(t, "update_noop", out)
}

func TestUpdateInvalidLock(t *testing.T) {
	cfg := testConfig(t)
	out, _, code := run(t, cfg, "update", "--lock-dir", testutil.LockDir("lock-invalid"), "--json")
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	golden(t, "error_invalid_lock", out)
}

func TestFailedUpdateKeepsCurrent(t *testing.T) {
	cfg := testConfig(t)
	run(t, cfg, "update", "--lock-dir", testutil.LockDir("lock-v1"))
	t.Setenv("BERTH_FAKE_CHROMIUM_FAIL", "1")
	out, _, code := run(t, cfg, "update", "--lock-dir", testutil.LockDir("lock-v2"), "--json")
	if code != 1 || decode(t, out)["error"].(map[string]any)["step"] != "smoke" {
		t.Fatalf("exit %d %s", code, out)
	}
	if current(t, cfg) != hashOf(t, "lock-v1") {
		t.Fatal("current changed after a failed update")
	}
}

func TestRollback(t *testing.T) {
	cfg := testConfig(t)
	out, _, code := run(t, cfg, "rollback", "--json")
	if code != 1 || !strings.Contains(out, "no_previous") {
		t.Fatalf("empty rollback: %d %s", code, out)
	}
	run(t, cfg, "update", "--lock-dir", testutil.LockDir("lock-v1"))
	run(t, cfg, "update", "--lock-dir", testutil.LockDir("lock-v2"))
	out, _, code = run(t, cfg, "rollback", "--json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	golden(t, "rollback", out)
	if current(t, cfg) != hashOf(t, "lock-v1") {
		t.Fatal("rollback did not restore v1")
	}
	out, _, code = run(t, cfg, "rollback", "../../etc", "--json")
	if code != 1 || !strings.Contains(out, "not_found") {
		t.Fatalf("bad target: %d %s", code, out)
	}
}

func TestEnsure(t *testing.T) {
	cfg := testConfig(t)
	out, _, code := run(t, cfg, "ensure", "--json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	golden(t, "ensure_install", out)
	out, _, _ = run(t, cfg, "ensure", "--json")
	if m := decode(t, out); m["action"] != "none" || m["reason"] != "up-to-date" {
		t.Fatalf("second ensure %v", m)
	}
	cfg.SeedDir = testutil.LockDir("lock-v2") // the operator pulled a newer image
	out, _, _ = run(t, cfg, "ensure", "--json")
	if m := decode(t, out); m["reason"] != "seed-changed" || current(t, cfg) != hashOf(t, "lock-v2") {
		t.Fatalf("seed change %v", m)
	}
}

func TestEnsureRespectsPinnedChoices(t *testing.T) {
	cfg := testConfig(t)
	run(t, cfg, "ensure")
	run(t, cfg, "update", "--lock-dir", testutil.LockDir("lock-v2"))
	cfg.SeedDir = testutil.LockDir("lock-v1")
	out, _, _ := run(t, cfg, "ensure", "--json")
	if m := decode(t, out); m["reason"] != "pinned" || current(t, cfg) != hashOf(t, "lock-v2") {
		t.Fatalf("pinned %v", m)
	}
	run(t, cfg, "update", "--image")
	if current(t, cfg) != hashOf(t, "lock-v1") {
		t.Fatal("update --image must return to the seed")
	}
	cfg.SeedDir = testutil.LockDir("lock-v2")
	if out, _, _ := run(t, cfg, "ensure", "--json"); decode(t, out)["reason"] != "seed-changed" {
		t.Fatal("update --image must resume following the image")
	}
}

func TestEnsureRespectsRollback(t *testing.T) {
	cfg := testConfig(t)
	run(t, cfg, "ensure")                        // v1 from the seed
	cfg.SeedDir = testutil.LockDir("lock-v2")    // newer image
	run(t, cfg, "ensure")                        // follows it to v2
	run(t, cfg, "rollback")                      // operator goes back to v1
	out, _, _ := run(t, cfg, "ensure", "--json") // container restart
	if m := decode(t, out); m["reason"] != "pinned" || current(t, cfg) != hashOf(t, "lock-v1") {
		t.Fatalf("restart undid the rollback: %v", m)
	}
}

func TestUpdateFromRef(t *testing.T) {
	cfg := testConfig(t)
	sha := strings.Repeat("ab", 20)
	pkg, _ := os.ReadFile(filepath.Join(testutil.LockDir("lock-v2"), "package.json"))
	lock, _ := os.ReadFile(filepath.Join(testutil.LockDir("lock-v2"), "package-lock.json"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/example/berth/commits/main", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(sha)) })
	mux.HandleFunc("GET /example/berth/"+sha+"/agents/package.json", func(w http.ResponseWriter, _ *http.Request) { w.Write(pkg) })
	mux.HandleFunc("GET /example/berth/"+sha+"/agents/package-lock.json", func(w http.ResponseWriter, _ *http.Request) { w.Write(lock) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	cfg.GitHub.APIBase, cfg.GitHub.RawBase = srv.URL, srv.URL

	out, errOut, code := run(t, cfg, "update", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	src := decode(t, out)["current"].(map[string]any)["source"].(map[string]any)
	if src["origin"] != "ref" || src["ref"] != "main" || src["commit"] != sha {
		t.Fatalf("source %v", src)
	}
	out, _, _ = run(t, cfg, "status", "--check", "--json")
	if chk := decode(t, out)["check"].(map[string]any); chk["update_available"] != false {
		t.Fatalf("check %v", chk)
	}
}

func TestExclusiveSourceFlags(t *testing.T) {
	out, _, code := run(t, testConfig(t), "update", "--ref", "x", "--image", "--json")
	if code != 2 || !strings.Contains(out, "mutually exclusive") {
		t.Fatalf("exit %d %s", code, out)
	}
}

func TestNotRoot(t *testing.T) {
	cfg := testConfig(t)
	cfg.IsRoot = false
	for _, cmd := range []string{"update", "rollback", "ensure", "gc"} {
		out, _, code := run(t, cfg, cmd, "--json")
		if code != 4 {
			t.Fatalf("%s: exit %d", cmd, code)
		}
		if cmd == "update" {
			golden(t, "error_not_root", out)
		}
	}
}

func TestLockBusy(t *testing.T) {
	cfg := testConfig(t)
	unlock, err := agentstore.Store{Root: cfg.AgentsDir}.Lock(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	out, _, code := run(t, cfg, "update", "--lock-dir", testutil.LockDir("lock-v1"), "--json")
	if code != 3 {
		t.Fatalf("exit %d", code)
	}
	golden(t, "error_lock_busy", out)
	if _, err := os.Stat(filepath.Join(cfg.AgentsDir, "versions")); !os.IsNotExist(err) {
		t.Fatal("store touched while locked")
	}
}

func TestGC(t *testing.T) {
	cfg := testConfig(t)
	cfg.Keep = 1
	for i, h := range []string{"a00000000000", "b00000000000", "c00000000000", "d00000000000"} {
		testutil.MakeVersion(t, cfg.AgentsDir, h, "image", now.Add(time.Duration(i)*time.Hour))
	}
	testutil.Link(t, cfg.AgentsDir, "current", "a00000000000")
	out, _, code := run(t, cfg, "gc", "--json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	golden(t, "gc", out)
}
