package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berth-ai/berth/internal/agentset"
	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/output"
	"github.com/berth-ai/berth/internal/testutil"
)

const fakeEntry = `{"name":"x","private":true,"dependencies":{"fake-agent":"1.0.0"},
  "berth":{"fake-agent":{"kind":"agent","name":"Fake","description":"A fake agent","keys":[]}},
  "berthAgents":{"schema":1,"agents":{"fake-agent":{"icon":"x"}}}}`

// agentsConfig is testConfig with a home, a config dir and a registry
// holding entry "fake" (package fake-agent).
func agentsConfig(t *testing.T) (Config, string) {
	t.Helper()
	cfg := testConfig(t)
	home := t.TempDir()
	cfg.ConfigDir = t.TempDir()
	cfg.RegistryDir = t.TempDir()
	testutil.RegistryEntry(t, cfg.RegistryDir, "fake", fakeEntry)
	cfg.Getenv = func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}
	return cfg, home
}

func TestAgentsInstallStatusSync(t *testing.T) {
	cfg, home := agentsConfig(t)
	out, errOut, code := run(t, cfg, "agents", "install", "fake", "--json")
	if code != 0 || !strings.Contains(out, `"version": "1.0.0"`) || !strings.Contains(out, `"package": "fake-agent"`) {
		t.Fatalf("install: %d %s %s", code, out, errOut)
	}
	out, _, code = run(t, cfg, "agents", "status", "--json")
	rep, err := agentset.DecodeReport([]byte(out))
	if code != 0 || err != nil || len(rep.Agents) != 1 || rep.Agents[0].Package != "fake-agent" {
		t.Fatalf("status: %d %v %s", code, err, out)
	}
	// agents.json does not list it: sync removes it.
	if _, errOut, code := run(t, cfg, "agents", "sync"); code != 0 {
		t.Fatalf("sync: %d %s", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(agentset.StoreRoot(home), "fake")); !os.IsNotExist(err) {
		t.Errorf("unlisted agent kept: %v", err)
	}
	// Listed: sync installs it.
	config.WriteAgentsFile(filepath.Join(cfg.ConfigDir, "agents.json"), []string{"fake-agent"}, -1, -1)
	if _, errOut, code := run(t, cfg, "agents", "sync"); code != 0 {
		t.Fatalf("sync: %d %s", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(agentset.StoreRoot(home), "fake", "current")); err != nil {
		t.Errorf("listed agent not installed: %v", err)
	}
}

func TestAgentsErrors(t *testing.T) {
	cfg, _ := agentsConfig(t)
	code := func(args ...string) (int, string) {
		out, _, c := run(t, cfg, append(args, "--json")...)
		var doc struct{ Error struct{ Code string } }
		json.Unmarshal([]byte(out), &doc)
		return c, doc.Error.Code
	}
	for _, tc := range []struct {
		args []string
		exit int
		code string
	}{
		{[]string{"agents"}, 2, "usage"},
		{[]string{"agents", "install"}, 2, "usage"},
		{[]string{"agents", "install", "Bad_ID"}, 2, "usage"},
		{[]string{"agents", "install", "nope"}, 2, "unknown_agent"},
		{[]string{"agents", "rollback", "fake-agent"}, 1, "not_added"},
		{[]string{"agents", "frob"}, 2, "usage"},
	} {
		if exit, c := code(tc.args...); exit != tc.exit || c != tc.code {
			t.Errorf("%v: exit %d code %q, want %d %q", tc.args, exit, c, tc.exit, tc.code)
		}
	}
	run(t, cfg, "agents", "install", "fake")
	if exit, c := code("agents", "rollback", "fake-agent"); exit != 1 || c != "no_previous" {
		t.Errorf("rollback without previous: %d %q", exit, c)
	}
}

func TestAgentsHiddenFromHelp(t *testing.T) {
	out, _, _ := run(t, testConfig(t), "help")
	if strings.Contains(out, "agents ") {
		t.Errorf("agents is listed in help:\n%s", out)
	}
}

func TestEnsureSyncsAddedAgents(t *testing.T) {
	cfg := testConfig(t)
	dir := t.TempDir()
	log := filepath.Join(dir, "agents.log")
	script := filepath.Join(dir, "berth-agents")
	os.WriteFile(script, []byte("#!/bin/sh\necho \"$*\" >> "+log+"\n"), 0o755)
	cfg.AgentsCmd = []string{script}
	if _, errOut, code := run(t, cfg, "ensure", "--json"); code != 0 {
		t.Fatalf("ensure: %d %s", code, errOut)
	}
	if got, _ := os.ReadFile(log); !strings.Contains(string(got), "sync\n") {
		t.Errorf("ensure ran %q", got)
	}
}

func TestAddedReportRunsStatus(t *testing.T) {
	cfg := testConfig(t)
	dir := t.TempDir()
	script := filepath.Join(dir, "berth-agents")
	os.WriteFile(script, []byte(`#!/bin/sh
[ "$1 $2" = "status --json" ] || exit 2
echo '{"agents":[{"id":"copilot","package":"@github/copilot","version":"1.0.91","hash":"0123456789ab","package_json":"e30=","bins":["copilot"]}],"errors":{},"schema":1}'
`), 0o755)
	cfg.AgentsCmd = []string{script}
	a := &app{cfg: cfg, p: output.Printer{Stderr: io.Discard}}
	if rep := a.addedReport(); len(rep.Agents) != 1 || rep.Agents[0].ID != "copilot" {
		t.Errorf("report = %+v", rep)
	}
	os.WriteFile(script, []byte("#!/bin/sh\necho garbage\n"), 0o755)
	if rep := a.addedReport(); len(rep.Agents) != 0 {
		t.Errorf("garbage report = %+v", rep)
	}
}
