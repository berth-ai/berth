package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/berth-ai/berth/internal/agentlayer"
	"github.com/berth-ai/berth/internal/agentstore"
	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/testutil"
)

// withAgents gives tb a current set with packages agent-a (key A_KEY) and
// mcp-b, a layer builder, and a fake MCP setup script.
func withAgents(t *testing.T, tb *testBroker) string {
	t.Helper()
	root := filepath.Join(tb.dir, "agents")
	dir := testutil.MakeVersion(t, root, "0123456789ab", "image", time.Now())
	testutil.Link(t, root, "current", "0123456789ab")
	raw, _ := json.Marshal(map[string]any{
		"dependencies": map[string]string{"agent-a": "1.0.0", "mcp-b": "2.0.0"},
		"berth": map[string]any{
			"agent-a": map[string]any{"kind": "agent", "name": "Agent A", "keys": []string{"A_KEY"}},
			"mcp-b":   map[string]any{"kind": "mcp", "name": "B", "keys": []string{}},
		},
	})
	os.WriteFile(filepath.Join(dir, "package.json"), raw, 0o644)
	for dep, bin := range map[string]string{"agent-a": "a", "mcp-b": "b-mcp"} {
		d := filepath.Join(dir, "node_modules", dep)
		os.MkdirAll(d, 0o755)
		m, _ := json.Marshal(map[string]any{"name": dep, "bin": map[string]string{bin: "cli.js"}})
		os.WriteFile(filepath.Join(d, "package.json"), m, 0o644)
	}
	tb.b.Layer = &agentlayer.Builder{Root: tb.dir, Store: agentstore.Store{Root: root},
		KeysPath: filepath.Join(tb.dir, "secrets", "agents.env")}
	mcp := filepath.Join(tb.dir, "mcp-setup")
	os.WriteFile(mcp, []byte("#!/bin/sh\necho registered >> \""+tb.dir+"/mcp.log\"\n"), 0o755)
	tb.b.MCPSetup = []string{mcp}
	tb.b.MCPSettings = filepath.Join(tb.dir, "mcp.json")
	tb.b.Load()
	return tb.b.Layer.KeysPath
}

func TestSecretsAreWriteOnly(t *testing.T) {
	tb := newTestBroker(t)
	keysPath := withAgents(t, tb)
	code, out := tb.verb(t, "config.apply", `{"secrets":{"A_KEY":"sk-a=="}}`)
	if code != 200 {
		t.Fatalf("apply = %d %v", code, out)
	}
	keys, _ := agentlayer.ReadKeys(keysPath)
	if keys["A_KEY"] != "sk-a==" {
		t.Errorf("keys = %v", keys)
	}
	_, out = tb.verb(t, "config.get", `{}`)
	if s := out["secrets"].(map[string]any)["A_KEY"].(map[string]any); s["set"] != true {
		t.Errorf("secrets = %v", out["secrets"])
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "sk-a==") {
		t.Error("config.get leaked the key")
	}
	_, out = tb.verb(t, "config.export", `{}`)
	if raw, _ := json.Marshal(out); strings.Contains(string(raw), "secrets") || strings.Contains(string(raw), "sk-a") {
		t.Errorf("export = %s", raw)
	}
	cfgFile, _ := os.ReadFile(filepath.Join(tb.dir, "config.json"))
	if strings.Contains(string(cfgFile), "sk-a") {
		t.Error("key in config.json")
	}
	if code, _ := tb.verb(t, "config.apply", `{"secrets":{"A_KEY":null}}`); code != 200 {
		t.Fatal("remove")
	}
	if keys, _ := agentlayer.ReadKeys(keysPath); len(keys) != 0 {
		t.Errorf("after remove = %v", keys)
	}
}

func TestSecretValidation(t *testing.T) {
	tb := newTestBroker(t)
	withAgents(t, tb)
	for body, field := range map[string]string{
		`{"secrets":{"OTHER_KEY":"x"}}`:     "secrets.OTHER_KEY",
		`{"secrets":{"A_KEY":"has space"}}`: "secrets.A_KEY",
	} {
		code, out := tb.verb(t, "config.apply", body)
		fields, _ := out["error"].(map[string]any)["fields"].([]any)
		if code != 400 || len(fields) != 1 || fields[0].(map[string]any)["field"] != field {
			t.Errorf("%s = %d %v", body, code, out)
		}
	}
	if code, out := tb.verb(t, "config.apply", `{}`); code != 400 || errCode(out) != "invalid_request" {
		t.Errorf("empty apply = %d %v", code, out)
	}
	if code, out := tb.verb(t, "config.apply", `{"patch":{"agents":{"keep":2}}}`); code != 400 || errCode(out) != "invalid_request" {
		t.Errorf("patch without base_revision = %d %v", code, out)
	}
}

func TestDisablingRebuildsLayerAndRunsMCP(t *testing.T) {
	tb := newTestBroker(t)
	withAgents(t, tb)
	tb.b.Layer.Build(nil, nil)
	code, out := tb.verb(t, "config.apply", `{"base_revision":0,"patch":{"agents":{"disabled":["mcp-b"]}}}`)
	if code != 200 {
		t.Fatalf("apply = %d %v", code, out)
	}
	entries, _ := os.ReadDir(filepath.Join(tb.dir, "bin"))
	if len(entries) != 1 || entries[0].Name() != "a" {
		t.Errorf("layer = %v", entries)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if list := tb.b.Jobs.List(); len(list) > 0 && list[0].Verb == "mcp.setup" && list[0].State == StateSucceeded {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if log, _ := os.ReadFile(filepath.Join(tb.dir, "mcp.log")); string(log) != "registered\n" {
		t.Errorf("mcp setup ran %q; jobs %+v", log, tb.b.Jobs.List())
	}
	// A change that leaves disabled alone runs no MCP setup.
	tb.verb(t, "config.apply", `{"base_revision":1,"patch":{"agents":{"keep":2}}}`)
	if n := len(tb.b.Jobs.List()); n != 1 {
		t.Errorf("jobs = %d", n)
	}
}

func TestStatusListsPackages(t *testing.T) {
	tb := newTestBroker(t)
	withAgents(t, tb)
	st := tb.b.Status()
	if len(st.Packages) != 2 || st.Packages[0].Package != "agent-a" || st.Packages[0].Origin != "locked" || !slices.ContainsFunc(st.Packages, func(p agentlayer.Package) bool { return p.Kind == "mcp" }) {
		t.Errorf("packages = %+v", st.Packages)
	}
}

func TestAgentWork(t *testing.T) {
	tb := newTestBroker(t)
	script := filepath.Join(tb.dir, "s")
	os.WriteFile(script, []byte("#!/bin/sh\necho out\nexit 3\n"), 0o755)
	var buf strings.Builder
	exit, res, err := tb.b.agentWork(script, nil, time.Minute)(context.Background(), &buf)
	if exit != 3 || err != nil || res != nil || buf.String() != "out\n" {
		t.Errorf("exit %d res %s err %v log %q", exit, res, err, buf.String())
	}
}

// mcpRuns counts the MCP converger's runs in the fake script's log.
func mcpRuns(tb *testBroker) int {
	raw, _ := os.ReadFile(filepath.Join(tb.dir, "mcp.log"))
	return strings.Count(string(raw), "registered\n")
}

// waitMCPRuns waits until the converger has run n times and no run is left.
func waitMCPRuns(t *testing.T, tb *testBroker, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		idle := true
		for _, j := range tb.b.Jobs.List() {
			idle = idle && j.State != StateRunning
		}
		if mcpRuns(tb) >= n && idle {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("mcp setup ran %d times, want %d: %+v", mcpRuns(tb), n, tb.b.Jobs.List())
}

const addLinear = `{"base_revision":0,"patch":{"mcp":{"servers":{"linear":{"url":"https://mcp.example.com/mcp","auth":"bearer"}}}},"secrets":{"MCP_LINEAR_TOKEN":"tok-1"}}`

func TestAddingBearerServerStoresTokenAndPublishes(t *testing.T) {
	tb := newTestBroker(t)
	keysPath := withAgents(t, tb)
	if code, out := tb.verb(t, "config.apply", addLinear); code != 200 {
		t.Fatalf("apply = %d %v", code, out)
	}
	if keys, _ := agentlayer.ReadKeys(keysPath); keys["MCP_LINEAR_TOKEN"] != "tok-1" {
		t.Errorf("keys = %v", keys)
	}
	m, err := config.ReadMCPFile(tb.b.MCPSettings)
	if err != nil || m.Servers["linear"].URL != "https://mcp.example.com/mcp" {
		t.Errorf("mcp.json = %+v, %v", m, err)
	}
	raw, _ := os.ReadFile(tb.b.MCPSettings)
	if strings.Contains(string(raw), "tok-1") {
		t.Error("mcp.json holds the token")
	}
	_, out := tb.verb(t, "config.get", `{}`)
	if s, _ := out["secrets"].(map[string]any)["MCP_LINEAR_TOKEN"].(map[string]any); s["set"] != true {
		t.Errorf("secrets = %v", out["secrets"])
	}
	waitMCPRuns(t, tb, 1)
}

func TestTokenOnlyForBearerServers(t *testing.T) {
	tb := newTestBroker(t)
	withAgents(t, tb)
	code, out := tb.verb(t, "config.apply", `{"secrets":{"MCP_LINEAR_TOKEN":"tok"}}`)
	if code != 400 || !strings.Contains(fmt.Sprint(out), "secrets.MCP_LINEAR_TOKEN") {
		t.Errorf("token without server = %d %v", code, out)
	}
	code, out = tb.verb(t, "config.apply", `{"base_revision":0,"patch":{"mcp":{"servers":{"linear":{"url":"https://mcp.example.com/mcp","auth":"oauth"}}}},"secrets":{"MCP_LINEAR_TOKEN":"tok"}}`)
	if code != 400 {
		t.Errorf("token for oauth server = %d %v", code, out)
	}
	if _, out := tb.verb(t, "config.get", `{}`); len(out["config"].(map[string]any)["mcp"].(map[string]any)["servers"].(map[string]any)) != 0 {
		t.Errorf("a refused apply changed the config: %v", out["config"])
	}
}

func TestRemovingServerDeletesToken(t *testing.T) {
	tb := newTestBroker(t)
	keysPath := withAgents(t, tb)
	tb.verb(t, "config.apply", addLinear)
	code, out := tb.verb(t, "config.apply", `{"base_revision":1,"patch":{"mcp":{"servers":{"linear":null}}}}`)
	if code != 200 {
		t.Fatalf("remove = %d %v", code, out)
	}
	if keys, _ := agentlayer.ReadKeys(keysPath); keys["MCP_LINEAR_TOKEN"] != "" {
		t.Errorf("token kept: %v", keys)
	}
	if m, _ := config.ReadMCPFile(tb.b.MCPSettings); len(m.Servers) != 0 {
		t.Errorf("mcp.json = %+v", m)
	}
}

func TestSwitchingAwayFromBearerDeletesToken(t *testing.T) {
	tb := newTestBroker(t)
	keysPath := withAgents(t, tb)
	tb.verb(t, "config.apply", addLinear)
	if code, out := tb.verb(t, "config.apply", `{"base_revision":1,"patch":{"mcp":{"servers":{"linear":{"auth":"oauth"}}}}}`); code != 200 {
		t.Fatalf("switch = %d %v", code, out)
	}
	if keys, _ := agentlayer.ReadKeys(keysPath); keys["MCP_LINEAR_TOKEN"] != "" {
		t.Errorf("token kept: %v", keys)
	}
}

func TestTokenChangeAloneRunsSync(t *testing.T) {
	tb := newTestBroker(t)
	withAgents(t, tb)
	tb.verb(t, "config.apply", addLinear)
	waitMCPRuns(t, tb, 1)
	tb.verb(t, "config.apply", `{"secrets":{"MCP_LINEAR_TOKEN":"tok-2"}}`)
	waitMCPRuns(t, tb, 2)
	tb.verb(t, "config.apply", `{"secrets":{"A_KEY":"k"}}`) // not an MCP token
	time.Sleep(100 * time.Millisecond)
	if n := mcpRuns(tb); n != 2 {
		t.Errorf("mcp setup runs = %d, want 2", n)
	}
}

func TestChangeDuringSyncRunsItAgain(t *testing.T) {
	tb := newTestBroker(t)
	withAgents(t, tb)
	slow := filepath.Join(tb.dir, "slow-setup")
	os.WriteFile(slow, []byte("#!/bin/sh\nsleep 0.3\necho registered >> \""+tb.dir+"/mcp.log\"\n"), 0o755)
	tb.b.MCPSetup = []string{slow}
	tb.verb(t, "config.apply", addLinear)
	if code, out := tb.verb(t, "config.apply", `{"base_revision":1,"patch":{"mcp":{"servers":{"linear":null}}}}`); code != 200 {
		t.Fatalf("remove = %d %v", code, out)
	}
	waitMCPRuns(t, tb, 2)
}

func TestTokenNameClashWithLockKey(t *testing.T) {
	tb := newTestBroker(t)
	withAgents(t, tb)
	// Make the agent set declare the key the server's token would use.
	cur := filepath.Join(tb.dir, "agents", "current", "package.json")
	raw, _ := os.ReadFile(cur)
	os.WriteFile(cur, []byte(strings.Replace(string(raw), `["A_KEY"]`, `["A_KEY","MCP_LINEAR_TOKEN"]`, 1)), 0o644)
	code, out := tb.verb(t, "config.apply", `{"base_revision":0,"patch":{"mcp":{"servers":{"linear":{"url":"https://mcp.example.com/mcp","auth":"bearer"}}}}}`)
	if code != 400 || !strings.Contains(fmt.Sprint(out), "mcp.servers.linear") {
		t.Errorf("clash = %d %v", code, out)
	}
}

func TestRevertRepublishesAndSyncs(t *testing.T) {
	tb := newTestBroker(t)
	withAgents(t, tb)
	tb.verb(t, "config.apply", addLinear)
	waitMCPRuns(t, tb, 1)
	tb.verb(t, "config.apply", `{"base_revision":1,"patch":{"mcp":{"servers":{"linear":null}}}}`)
	waitMCPRuns(t, tb, 2)
	if code, out := tb.verb(t, "config.revert", `{}`); code != 200 {
		t.Fatalf("revert = %d %v", code, out)
	}
	if m, _ := config.ReadMCPFile(tb.b.MCPSettings); m.Servers["linear"].URL == "" {
		t.Errorf("mcp.json after revert = %+v", m)
	}
	waitMCPRuns(t, tb, 3)
}

func TestLoadPublishesOnlyAUsableConfig(t *testing.T) {
	tb := newTestBroker(t)
	withAgents(t, tb)
	tb.verb(t, "config.apply", addLinear)
	os.Remove(tb.b.MCPSettings)
	tb.b.Load()
	if m, _ := config.ReadMCPFile(tb.b.MCPSettings); m.Servers["linear"].URL == "" {
		t.Errorf("Load did not republish: %+v", m)
	}
	// A corrupt config must not overwrite mcp.json with the defaults.
	os.WriteFile(filepath.Join(tb.dir, "config.json"), []byte(`{"schema":`), 0o600)
	tb.b.Load()
	if m, _ := config.ReadMCPFile(tb.b.MCPSettings); m.Servers["linear"].URL == "" {
		t.Errorf("recovery overwrote mcp.json: %+v", m)
	}
}

func TestResetFromRecoveryRepublishes(t *testing.T) {
	tb := newTestBroker(t)
	withAgents(t, tb)
	tb.verb(t, "config.apply", addLinear)
	waitMCPRuns(t, tb, 1)
	os.WriteFile(filepath.Join(tb.dir, "config.json"), []byte(`{"schema":`), 0o600)
	tb.b.Load()
	if code, out := tb.verb(t, "config.reset", `{}`); code != 200 {
		t.Fatalf("reset = %d %v", code, out)
	}
	if m, _ := config.ReadMCPFile(tb.b.MCPSettings); len(m.Servers) != 0 {
		t.Errorf("mcp.json after reset = %+v", m)
	}
	waitMCPRuns(t, tb, 2)
}
