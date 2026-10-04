package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/berth-ai/berth/internal/agentlayer"
	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/testutil"
)

const copilotPkg = "@github/copilot"

// withRegistry is withAgents plus a registry holding Copilot and a fake
// berth agents that logs its arguments to agents.log. In tb.dir, the files
// fail and noprev make install and rollback fail; report.json is what
// status prints.
func withRegistry(t *testing.T, tb *testBroker) {
	t.Helper()
	withAgents(t, tb)
	reg := filepath.Join(tb.dir, "registry")
	testutil.RegistryEntry(t, reg, "copilot", testutil.CopilotPackageJSON)
	d := tb.dir
	script := filepath.Join(d, "berth-agents")
	os.WriteFile(script, []byte(`#!/bin/sh
echo "$*" >> "`+d+`/agents.log"
case "$1" in
status) if [ -f "`+d+`/report.json" ]; then cat "`+d+`/report.json"; else echo '{"agents":[],"errors":{},"schema":1}'; fi ;;
install) if [ -f "`+d+`/fail" ]; then echo "npm ERR! network down" >&2
    echo '{"error":{"code":"install_failed","step":"install","message":"npm ci failed"},"schema":1}'; exit 1; fi
  echo '{"id":"'"$3"'","package":"@github/copilot","version":"1.0.91","hash":"0123456789ab","schema":1}' ;;
rollback) if [ -f "`+d+`/noprev" ]; then echo '{"error":{"code":"no_previous","step":"rollback","message":"no previous version"},"schema":1}'; exit 1; fi
  echo '{"id":"copilot","package":"'"$3"'","version":"1.0.90","hash":"0123456789ab","schema":1}' ;;
esac
`), 0o755)
	tb.b.Registry = reg
	tb.b.AgentsCmd = []string{script}
	tb.b.AgentsSettings = filepath.Join(d, "agents.json")
	tb.b.Layer.AddedRoot = filepath.Join(d, "home", ".local", "share", "berth", "agents")
	tb.b.Layer.RegistryRoot = reg
	tb.b.Layer.Added = tb.b.AddedReport
}

func agentsLog(tb *testBroker) string {
	raw, _ := os.ReadFile(filepath.Join(tb.dir, "agents.log"))
	return string(raw)
}

func jobID(t *testing.T, out map[string]any) int {
	t.Helper()
	job, ok := out["job"].(map[string]any)
	if !ok {
		t.Fatalf("no job in %v", out)
	}
	return int(job["id"].(float64))
}

// waitVerb waits for a finished job of verb.
func waitVerb(t *testing.T, tb *testBroker, verb string) Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, j := range tb.b.Jobs.List() {
			if j.Verb == verb && j.State != StateRunning {
				return j
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no finished %s job", verb)
	return Job{}
}

// setAgents saves added and disabled straight to the store, as an older
// revision or another tab would have.
func setAgents(t *testing.T, tb *testBroker, added, disabled []string) {
	t.Helper()
	cfg, _, _ := tb.b.snapshot()
	patch, _ := json.Marshal(map[string]any{"agents": map[string]any{"added": added, "disabled": disabled}})
	if _, _, err := tb.b.Store.Apply(patch, cfg.Revision); err != nil {
		t.Fatal(err)
	}
	tb.b.Load()
}

func TestAgentsRegistryLists(t *testing.T) {
	tb := newTestBroker(t)
	withRegistry(t, tb)
	os.WriteFile(filepath.Join(tb.dir, "report.json"), []byte(`{"agents":[{"id":"copilot","package":"@github/copilot",
	  "version":"1.0.90","hash":"0123456789ab","package_json":"e30=","bins":["copilot"]}],
	  "errors":{"@github/copilot":"updating failed"},"schema":1}`), 0o644)
	tb.b.RefreshAdded(context.Background())
	code, out := tb.verb(t, "agents.registry", `{}`)
	rows, _ := out["agents"].([]any)
	if code != 200 || len(rows) != 1 {
		t.Fatalf("registry = %d %v", code, out)
	}
	r := rows[0].(map[string]any)
	if r["id"] != "copilot" || r["package"] != copilotPkg || r["name"] != "GitHub Copilot CLI" || r["version"] != "1.0.91" ||
		r["icon"] != "githubcopilot" || r["added"] != false || r["installed_version"] != "1.0.90" || r["error"] != "updating failed" ||
		!strings.HasPrefix(r["description"].(string), "GitHub's coding agent") {
		t.Errorf("row = %v", r)
	}
	if keys, login := r["keys"].([]any), r["login"].([]any); len(keys) != 1 || keys[0] != "COPILOT_GITHUB_TOKEN" || len(login) != 1 || login[0] != "GitHub" {
		t.Errorf("keys %v login %v", keys, login)
	}
	setAgents(t, tb, []string{copilotPkg}, nil)
	_, out = tb.verb(t, "agents.registry", `{}`)
	if out["agents"].([]any)[0].(map[string]any)["added"] != true {
		t.Error("added not reported")
	}
}

func TestAgentsAddInstallsBeforeSaving(t *testing.T) {
	tb := newTestBroker(t)
	withRegistry(t, tb)
	code, out := tb.verb(t, "agents.add", `{"id":"copilot"}`)
	if code != 202 {
		t.Fatalf("add = %d %v", code, out)
	}
	job := waitJob(t, tb.b.Jobs, jobID(t, out), StateSucceeded)
	var res map[string]any
	json.Unmarshal(job.Result, &res)
	if res["version"] != "1.0.91" || res["revision"] == nil {
		t.Errorf("result = %s", job.Result)
	}
	cfg, _, _ := tb.b.snapshot()
	if !slices.Equal(cfg.Agents.Added, []string{copilotPkg}) {
		t.Errorf("added = %v", cfg.Agents.Added)
	}
	if pub, _ := config.ReadAgentsFile(tb.b.AgentsSettings); !slices.Equal(pub, []string{copilotPkg}) {
		t.Errorf("agents.json = %v", pub)
	}
	waitVerb(t, tb, "agents.sync")
	log := agentsLog(tb)
	if i, j := strings.Index(log, "install --json copilot"), strings.Index(log, "sync\n"); i < 0 || j < i {
		t.Errorf("log = %q", log)
	}
	if code, out := tb.verb(t, "agents.add", `{"id":"copilot"}`); code != 409 || !strings.Contains(errCode(out), "already_added") {
		t.Errorf("second add = %d %v", code, out)
	}
}

func TestAgentsAddFailedInstallSavesNothing(t *testing.T) {
	tb := newTestBroker(t)
	withRegistry(t, tb)
	os.WriteFile(filepath.Join(tb.dir, "fail"), nil, 0o644)
	_, out := tb.verb(t, "agents.add", `{"id":"copilot"}`)
	job := waitJob(t, tb.b.Jobs, jobID(t, out), StateFailed)
	if !strings.Contains(string(job.Result), "install_failed") || !strings.Contains(job.Log, "npm ERR!") {
		t.Errorf("job = %s / %q", job.Result, job.Log)
	}
	if cfg, _, _ := tb.b.snapshot(); len(cfg.Agents.Added) != 0 {
		t.Errorf("added = %v", cfg.Agents.Added)
	}
	if strings.Contains(agentsLog(tb), "sync") {
		t.Error("a failed add started a sync")
	}
}

func TestAgentsAddUnknown(t *testing.T) {
	tb := newTestBroker(t)
	withRegistry(t, tb)
	if code, out := tb.verb(t, "agents.add", `{"id":"nope"}`); code != 404 || errCode(out) != "unknown_agent" {
		t.Errorf("add = %d %v", code, out)
	}
}

// TestAgentsRemoveClearsDisabled: a switched-off agent leaves
// agents.disabled too, so adding it again starts switched on.
func TestAgentsRemoveClearsDisabled(t *testing.T) {
	tb := newTestBroker(t)
	withRegistry(t, tb)
	setAgents(t, tb, []string{copilotPkg}, []string{copilotPkg, "mcp-b"})
	if code, out := tb.verb(t, "agents.remove", `{"package":"@github/copilot"}`); code != 200 {
		t.Fatalf("remove = %d %v", code, out)
	}
	cfg, _, _ := tb.b.snapshot()
	if len(cfg.Agents.Added) != 0 || !slices.Equal(cfg.Agents.Disabled, []string{"mcp-b"}) {
		t.Errorf("agents = %+v", cfg.Agents)
	}
	waitVerb(t, tb, "agents.sync")
	if code, out := tb.verb(t, "agents.remove", `{"package":"@github/copilot"}`); code != 404 || errCode(out) != "not_added" {
		t.Errorf("second remove = %d %v", code, out)
	}
}

func TestAgentsRollback(t *testing.T) {
	tb := newTestBroker(t)
	withRegistry(t, tb)
	if code, out := tb.verb(t, "agents.rollback", `{"package":"@github/copilot"}`); code != 404 || errCode(out) != "not_added" {
		t.Errorf("rollback of a package not added = %d %v", code, out)
	}
	setAgents(t, tb, []string{copilotPkg}, nil)
	_, out := tb.verb(t, "agents.rollback", `{"package":"@github/copilot"}`)
	job := waitJob(t, tb.b.Jobs, jobID(t, out), StateSucceeded)
	if !strings.Contains(string(job.Result), `"1.0.90"`) || !strings.Contains(agentsLog(tb), "rollback --json @github/copilot") {
		t.Errorf("job %s, log %q", job.Result, agentsLog(tb))
	}
	os.WriteFile(filepath.Join(tb.dir, "noprev"), nil, 0o644)
	_, out = tb.verb(t, "agents.rollback", `{"package":"@github/copilot"}`)
	if job := waitJob(t, tb.b.Jobs, jobID(t, out), StateFailed); !strings.Contains(string(job.Result), "no_previous") {
		t.Errorf("job = %s", job.Result)
	}
}

func TestConfigApplyChecksTheRegistry(t *testing.T) {
	tb := newTestBroker(t)
	withRegistry(t, tb)
	cfg, _, _ := tb.b.snapshot()
	code, out := tb.verb(t, "config.apply", `{"base_revision":`+itoa(cfg.Revision)+`,"patch":{"agents":{"added":["@nope/agent"]}}}`)
	if code != 400 || !strings.Contains(fmtJSON(out), "@nope/agent is not in this image's registry") ||
		!strings.Contains(fmtJSON(out), `"field":"agents.added"`) {
		t.Errorf("apply = %d %v", code, out)
	}
	// A package already added stays valid when the image drops it.
	setAgents(t, tb, []string{"@gone/agent"}, nil)
	cfg, _, _ = tb.b.snapshot()
	if code, out := tb.verb(t, "config.apply", `{"base_revision":`+itoa(cfg.Revision)+`,"patch":{"runtime":{"tz":"Europe/Rome"}}}`); code != 200 {
		t.Errorf("unrelated apply = %d %v", code, out)
	}
}

// TestResetRemovesAddedAgents: revert, reset and import change
// agents.added too, and must run the converger like config.apply.
func TestResetRemovesAddedAgents(t *testing.T) {
	tb := newTestBroker(t)
	withRegistry(t, tb)
	setAgents(t, tb, []string{copilotPkg}, nil)
	if code, out := tb.verb(t, "config.reset", `{}`); code != 200 {
		t.Fatalf("reset = %d %v", code, out)
	}
	if pub, _ := config.ReadAgentsFile(tb.b.AgentsSettings); len(pub) != 0 {
		t.Errorf("agents.json = %v", pub)
	}
	waitVerb(t, tb, "agents.sync")
}

// TestStatusSurvivesBadAgentsStatus: garbage from berth agents status
// costs only the added agents' details.
func TestStatusSurvivesBadAgentsStatus(t *testing.T) {
	tb := newTestBroker(t)
	withRegistry(t, tb)
	var log bytes.Buffer
	tb.b.Log = &log
	setAgents(t, tb, []string{copilotPkg}, nil)
	os.WriteFile(filepath.Join(tb.dir, "report.json"), []byte("garbage"), 0o644)
	tb.b.RefreshAdded(context.Background())
	st := tb.b.Status()
	i := slices.IndexFunc(st.Packages, func(p agentlayer.Package) bool { return p.Package == copilotPkg })
	if i < 0 || !strings.HasPrefix(st.Packages[i].Error, "Not installed yet") || st.Packages[i].Name != "GitHub Copilot CLI" {
		t.Errorf("packages = %+v", st.Packages)
	}
	if !strings.Contains(log.String(), "WARNING") {
		t.Errorf("log = %q", log.String())
	}
}

func fmtJSON(v any) string { raw, _ := json.Marshal(v); return string(raw) }

func itoa(n int) string { raw, _ := json.Marshal(n); return string(raw) }
