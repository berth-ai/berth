package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berth-ai/berth/internal/agentstore"
	"github.com/berth-ai/berth/internal/testutil"
	"github.com/berth-ai/berth/internal/upstream"
)

// npmRegistry serves /<pkg>/latest for each package in latest.
func npmRegistry(t *testing.T, latest map[string]string) upstream.NPM {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pkg, ok := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, "/"), "/latest")
		if v, found := latest[pkg]; ok && found {
			fmt.Fprintf(w, `{"version":%q}`, v)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return upstream.NPM{Registry: srv.URL}
}

type statusJSON struct {
	Pins []struct {
		Name, Track, Image, Current, Latest, Update, Error string
	} `json:"pins"`
	Tools struct {
		Current *struct {
			Packages map[string]string `json:"packages"`
		} `json:"current"`
	} `json:"tools"`
	CheckedAt *string `json:"checked_at"`
}

func statusOf(t *testing.T, cfg Config, args ...string) statusJSON {
	t.Helper()
	out, errOut, code := run(t, cfg, append([]string{"status", "--json"}, args...)...)
	if code != 0 {
		t.Fatalf("status: exit %d: %s", code, errOut)
	}
	var s statusJSON
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func pinOf(t *testing.T, s statusJSON, name string) (image, current, latest, update string) {
	t.Helper()
	for _, p := range s.Pins {
		if p.Name == name {
			return p.Image, p.Current, p.Latest, p.Update
		}
	}
	t.Fatalf("no pin %s in %+v", name, s.Pins)
	return
}

func upstreamConfig(t *testing.T) Config {
	t.Helper()
	cfg := testConfig(t)
	cfg.NPM = npmRegistry(t, map[string]string{"fake-agent": "1.2.0", "playwright": "1.0.0"})
	t.Setenv("BERTH_FAKE_MISE_LATEST", "gh=2.1.0,uv=1.0.0")
	return cfg
}

func TestStatusCheck(t *testing.T) {
	cfg := upstreamConfig(t)
	run(t, cfg, "ensure")
	s := statusOf(t, cfg)
	if s.CheckedAt != nil {
		t.Fatal("a plain status claims a check")
	}
	if img, cur, latest, _ := pinOf(t, s, "gh"); img != "2.0.0" || cur != "2.0.0" || latest != "" {
		t.Fatalf("gh before a check: %s %s %s", img, cur, latest)
	}
	s = statusOf(t, cfg, "--check")
	if s.CheckedAt == nil {
		t.Fatal("no checked_at")
	}
	for name, want := range map[string]string{"fake-agent": "minor", "gh": "minor", "uv": "major", "playwright": ""} {
		if _, _, _, got := pinOf(t, s, name); got != want {
			t.Errorf("%s update = %q, want %q", name, got, want)
		}
	}
	out, _, _ := run(t, cfg, "status", "--check")
	if !strings.Contains(out, "uv 0.1.0 -> 1.0.0 (major)") || !strings.Contains(out, "updates   3 available") {
		t.Errorf("human status:\n%s", out)
	}
}

func TestUpdateFromUpstream(t *testing.T) {
	cfg := upstreamConfig(t)
	run(t, cfg, "ensure")
	out, errOut, code := run(t, cfg, "update", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s %s", code, out, errOut)
	}
	res := decode(t, out)
	if picked := res["picked"].([]any); len(picked) != 2 {
		t.Fatalf("picked %v", picked)
	}
	if held := res["held"].([]any); len(held) != 1 || held[0].(map[string]any)["name"] != "uv" {
		t.Fatalf("held %v", held)
	}
	cur := res["current"].(map[string]any)
	if cur["source"].(map[string]any)["origin"] != "upstream" || cur["packages"].(map[string]any)["fake-agent"] != "1.2.0" {
		t.Fatalf("current %v", cur)
	}
	if res["follow_image"] != false || res["tools"].(map[string]any)["installed"] != true {
		t.Fatalf("result %v", res)
	}
	wrapper, err := os.ReadFile(filepath.Join(cfg.ToolsDir, "current", "wrappers", "gh"))
	if err != nil || !strings.Contains(string(wrapper), filepath.Join("installs", "gh", "2.1.0")) {
		t.Fatalf("gh wrapper: %s %v", wrapper, err)
	}
	if _, err := os.Stat(filepath.Join(cfg.ToolsDir, "current", "wrappers", "uv")); err == nil {
		t.Fatal("a held major got installed")
	}
	s := statusOf(t, cfg)
	if img, cur, _, _ := pinOf(t, s, "gh"); img != "2.0.0" || cur != "2.1.0" {
		t.Fatalf("gh pin %s %s", img, cur)
	}

	// A restart keeps both choices without asking the registry.
	cfg.NPM = upstream.NPM{Registry: "http://127.0.0.1:1"}
	t.Setenv("BERTH_FAKE_NPM_RESOLVE_FAIL", "1")
	out, _, _ = run(t, cfg, "ensure", "--json")
	if m := decode(t, out); m["reason"] != "pinned" || m["tools"].(map[string]any)["reason"] != "up-to-date" {
		t.Fatalf("ensure %v", m)
	}

	// Named updates: a major on request, then back to the image one by one.
	run(t, cfg, "update", "uv@1.0.0")
	run(t, cfg, "update", "--image", "gh")
	s = statusOf(t, cfg)
	if s.Tools.Current == nil || s.Tools.Current.Packages["uv"] != "1.0.0" || s.Tools.Current.Packages["gh"] != "" {
		t.Fatalf("tools %+v", s.Tools.Current)
	}
	out, _, code = run(t, cfg, "rollback", "--tools", "--json")
	if code != 0 || decode(t, out)["current"].(map[string]any)["packages"].(map[string]any)["gh"] != "2.1.0" {
		t.Fatalf("rollback --tools: %d %s", code, out)
	}
	out, _, code = run(t, cfg, "update", "--image", "--json")
	if code != 0 {
		t.Fatalf("update --image: %d %s", code, out)
	}
	res = decode(t, out)
	if res["follow_image"] != true || res["tools"].(map[string]any)["current"] != nil {
		t.Fatalf("update --image %v", res)
	}
	if current(t, cfg) != hashOf(t, "lock-v1") {
		t.Fatal("update --image did not return to the seed")
	}
	if _, err := os.Stat(filepath.Join(cfg.ToolsDir, "current")); !os.IsNotExist(err) {
		t.Fatal("a tool set is still current")
	}
}

func TestUpdateUnknownName(t *testing.T) {
	cfg := upstreamConfig(t)
	out, _, code := run(t, cfg, "update", "nope", "--json")
	if code != 2 || !strings.Contains(out, "unknown_name") {
		t.Fatalf("exit %d %s", code, out)
	}
}

func TestNewerImageDropsCaughtUpOverrides(t *testing.T) {
	cfg := upstreamConfig(t)
	run(t, cfg, "ensure")
	if _, errOut, code := run(t, cfg, "update", "fake-agent", "gh"); code != 0 {
		t.Fatal(errOut)
	}
	cfg.SeedDir = testutil.LockDir("lock-v2") // ships fake-agent 2.0.0, past the 1.2.0 override
	out, _, _ := run(t, cfg, "ensure", "--json")
	m := decode(t, out)
	if m["reason"] != "seed-changed" || fmt.Sprint(m["dropped"]) != "[fake-agent]" || current(t, cfg) != hashOf(t, "lock-v2") {
		t.Fatalf("ensure %v", m)
	}
	st, _ := agentstore.Store{Root: cfg.AgentsDir}.ReadState()
	if !st.FollowImage || len(st.Overrides) != 0 {
		t.Fatalf("state %+v", st)
	}

	base := filepath.Join(t.TempDir(), "mise.toml")
	os.WriteFile(base, []byte("[tools]\ngh = \"2.1.0\"\nuv = \"0.1.0\"\n"), 0o644)
	cfg.BaseTools = base // a newer image pins gh 2.1.0 itself
	out, _, _ = run(t, cfg, "ensure", "--json")
	tools := decode(t, out)["tools"].(map[string]any)
	if tools["action"] != "cleared" || fmt.Sprint(tools["dropped"]) != "[gh]" {
		t.Fatalf("tools %v", tools)
	}
}

func TestEnsureKeepsCurrentWhenUnresolved(t *testing.T) {
	cfg := upstreamConfig(t)
	run(t, cfg, "ensure")
	run(t, cfg, "update", "fake-agent")
	before := current(t, cfg)
	// A newer image moves playwright, so the overridden set must be resolved
	// again, but the registry is unreachable.
	seed := t.TempDir()
	pkg := `{"name":"berth-agents-fixture","private":true,"dependencies":{"fake-agent":"1.0.0","playwright":"1.1.0"}}`
	os.WriteFile(filepath.Join(seed, "package.json"), []byte(pkg), 0o644)
	os.WriteFile(filepath.Join(seed, "package-lock.json"), []byte(`{"lockfileVersion":3,"packages":{"":{"dependencies":{"fake-agent":"1.0.0","playwright":"1.1.0"}}}}`), 0o644)
	cfg.SeedDir = seed
	t.Setenv("BERTH_FAKE_NPM_RESOLVE_FAIL", "1")
	out, _, code := run(t, cfg, "ensure", "--json")
	if m := decode(t, out); code != 0 || m["reason"] != "unresolved" || current(t, cfg) != before {
		t.Fatalf("ensure %d %v", code, m)
	}
	os.Unsetenv("BERTH_FAKE_NPM_RESOLVE_FAIL")
	out, _, _ = run(t, cfg, "ensure", "--json")
	if m := decode(t, out); m["reason"] != "overrides-changed" || m["action"] != "installed" {
		t.Fatalf("ensure online %v", m)
	}
	v, _ := agentstore.Store{Root: cfg.AgentsDir}.Current()
	if v.Manifest.Packages["fake-agent"] != "1.2.0" || v.Manifest.Packages["playwright"] != "1.1.0" {
		t.Fatalf("packages %v", v.Manifest.Packages)
	}
}

// A state written before overrides existed pins the current set: ensure
// keeps it and records the overrides it implies.
func TestEnsureMigratesPinnedState(t *testing.T) {
	cfg := testConfig(t)
	run(t, cfg, "update", "--lock-dir", testutil.LockDir("lock-v2"))
	os.WriteFile(filepath.Join(cfg.AgentsDir, "state.json"), []byte(`{"follow_image": false}`), 0o644)
	out, _, _ := run(t, cfg, "ensure", "--json")
	if m := decode(t, out); m["reason"] != "pinned" || current(t, cfg) != hashOf(t, "lock-v2") {
		t.Fatalf("ensure %v", m)
	}
	st, _ := agentstore.Store{Root: cfg.AgentsDir}.ReadState()
	if st.Overrides["fake-agent"].Version != "2.0.0" || st.Overrides["fake-agent"].Image != "1.0.0" {
		t.Fatalf("state %+v", st)
	}
}

func TestToolInstallFailureKeepsTools(t *testing.T) {
	cfg := upstreamConfig(t)
	run(t, cfg, "ensure")
	t.Setenv("BERTH_FAKE_MISE_FAIL", "1")
	out, _, code := run(t, cfg, "update", "gh", "--json")
	if code != 1 || decode(t, out)["error"].(map[string]any)["step"] != "install" {
		t.Fatalf("exit %d %s", code, out)
	}
	st, _ := agentstore.Store{Root: cfg.ToolsDir}.ReadState()
	if len(st.Overrides) != 0 {
		t.Fatalf("a failed install recorded %v", st.Overrides)
	}
}
