package broker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/salgnt/berth/internal/agentlayer"
	"github.com/salgnt/berth/internal/agentstore"
	"github.com/salgnt/berth/internal/testutil"
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
	tb.b.MCPSetup = mcp
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
	tb.b.Layer.Build(nil)
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
	if len(st.Packages) != 2 || st.Packages[0].Package != "agent-a" || !slices.ContainsFunc(st.Packages, func(p agentlayer.Package) bool { return p.Kind == "mcp" }) {
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
