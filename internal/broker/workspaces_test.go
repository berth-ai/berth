package broker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/berth-ai/berth/internal/config"
)

func TestWorkspacesFilePublished(t *testing.T) {
	tb := newTestBroker(t)
	tb.b.WorkspacesRoot = "/ws"
	tb.b.WorkspacesSettings = filepath.Join(tb.dir, "workspaces.json")
	tb.b.Load()
	if ws, err := config.ReadWorkspacesFile(tb.b.WorkspacesSettings); err != nil || len(ws) != 0 {
		t.Fatalf("at load = %v, %v", ws, err)
	}
	if _, err := os.Stat(tb.b.WorkspacesSettings); err != nil {
		t.Fatalf("Load did not write the file: %v", err)
	}
	if code, out := tb.verb(t, "config.apply", `{"base_revision":0,"patch":{"workspaces":{"api":{"path":"acme/api"}}}}`); code != 200 {
		t.Fatalf("apply = %d %v", code, out)
	}
	ws, _ := config.ReadWorkspacesFile(tb.b.WorkspacesSettings)
	if ws["api"].Path != "/ws/acme/api" {
		t.Errorf("after apply = %+v", ws)
	}
	if fi, _ := os.Stat(tb.b.WorkspacesSettings); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v", fi.Mode())
	}
	os.Remove(tb.b.WorkspacesSettings)
	tb.b.Load()
	if ws, _ := config.ReadWorkspacesFile(tb.b.WorkspacesSettings); ws["api"].Path == "" {
		t.Errorf("Load did not republish: %+v", ws)
	}
	os.WriteFile(filepath.Join(tb.dir, "config.json"), []byte(`{"schema":`), 0o600)
	tb.b.Load()
	if ws, _ := config.ReadWorkspacesFile(tb.b.WorkspacesSettings); ws["api"].Path == "" {
		t.Errorf("recovery overwrote workspaces.json: %+v", ws)
	}
}

// withWorkspaces points tb's broker at a fake berth whose workspaces
// commands answer like the real ones, steered by FAKE_WS_* variables, and
// returns the file logging its calls, one per line.
func withWorkspaces(t *testing.T, tb *testBroker) string {
	t.Helper()
	log := filepath.Join(tb.dir, "berth.log")
	script := `#!/bin/sh
echo "$*" >> "` + log + `"
[ -n "$FAKE_WS_SLOW" ] && sleep "$FAKE_WS_SLOW"
case "$2" in
clone)
  if [ -n "$FAKE_WS_FAIL" ]; then
    printf '{"error":{"code":"ref_not_found","step":"ref","message":"Ref nope not found in that repository"},"schema":1}'
    exit 1
  fi
  printf '{"path":"acme/api","commit":"` + skillCommit + `","branch":"main","schema":1}' ;;
probe) printf '{"writable":false,"git":true,"schema":1}' ;;
kill) printf '{"killed":true,"schema":1}' ;;
remove)
  if [ -n "$FAKE_WS_LEFT" ]; then
    printf '{"removed":false,"left":["acme/api/locked/f"],"schema":1}'
  else
    printf '{"removed":true,"left":[],"schema":1}'
  fi ;;
status) printf '{"workspaces":{"api":{"exists":true,"writable":true,"git":true,"branch":"main","head":"0123456789ab","dirty":2,"session":true,"windows":["zsh"],"unknown":false}},"schema":1}' ;;
candidates) printf '{"candidates":[{"path":"scratch","git":false}],"schema":1}' ;;
launch)
  if [ -n "$FAKE_WS_MISSING" ]; then
    printf '{"error":{"code":"missing","step":"path","message":"The workspace folder is missing"},"schema":1}'
    exit 1
  fi
  printf '{"session":"ws-api","schema":1}' ;;
esac
`
	p := filepath.Join(tb.dir, "berth-fake")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	tb.b.Home = HomeCommands{Berth: p}
	tb.b.WorkspacesRoot = "/ws"
	tb.b.WorkspacesSettings = filepath.Join(tb.dir, "workspaces.json")
	tb.b.Load()
	return log
}

func calls(log string) []string {
	raw, _ := os.ReadFile(log)
	if len(raw) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
}

// finishJob waits for the job a verb answered with.
func finishJob(t *testing.T, tb *testBroker, out map[string]any) Job {
	t.Helper()
	job, ok := out["job"].(map[string]any)
	if !ok {
		t.Fatalf("no job in %v", out)
	}
	id := int(job["id"].(float64))
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if j, err := tb.b.Jobs.Get(id); err == nil && j.State != StateRunning {
			return j
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %d did not finish", id)
	return Job{}
}

func resultOf(t *testing.T, j Job) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(j.Result, &m); err != nil {
		t.Fatalf("result %s: %v", j.Result, err)
	}
	return m
}

func TestWorkspaceCloneAdds(t *testing.T) {
	tb := newTestBroker(t)
	log := withWorkspaces(t, tb)
	code, out := tb.verb(t, "workspaces.add", `{"mode":"clone","repo":"https://github.com/acme/api","ref":"v1","path":"acme/api","id":"api"}`)
	if code != 202 {
		t.Fatalf("add = %d %v", code, out)
	}
	j := finishJob(t, tb, out)
	res := resultOf(t, j)
	if j.State != StateSucceeded || res["id"] != "api" || res["writable"] != true || res["git"] != true || res["changed"] != true {
		t.Fatalf("job = %+v %v", j, res)
	}
	if want := "workspaces clone --json --repo https://github.com/acme/api --path acme/api --ref v1"; !slices.Equal(calls(log), []string{want}) {
		t.Errorf("calls = %v", calls(log))
	}
	cfg, _, _ := tb.b.snapshot()
	if w := cfg.Workspaces["api"]; w.Path != "acme/api" || w.Repo != "https://github.com/acme/api" {
		t.Errorf("workspace = %+v", w)
	}
	if ws, _ := config.ReadWorkspacesFile(tb.b.WorkspacesSettings); ws["api"].Path != "/ws/acme/api" {
		t.Errorf("workspaces.json = %+v", ws)
	}
}

func TestWorkspaceCloneFailure(t *testing.T) {
	tb := newTestBroker(t)
	withWorkspaces(t, tb)
	t.Setenv("FAKE_WS_FAIL", "1")
	_, out := tb.verb(t, "workspaces.add", `{"mode":"clone","repo":"https://github.com/acme/api","ref":"nope","path":"acme/api","id":"api"}`)
	j := finishJob(t, tb, out)
	e, _ := resultOf(t, j)["error"].(map[string]any)
	if j.State != StateFailed || e["code"] != "ref_not_found" || e["field"] != "ref" {
		t.Errorf("job = %+v %s", j, j.Result)
	}
	if cfg, _, _ := tb.b.snapshot(); len(cfg.Workspaces) != 0 {
		t.Errorf("workspaces = %v", cfg.Workspaces)
	}
}

func TestWorkspaceAdopt(t *testing.T) {
	tb := newTestBroker(t)
	log := withWorkspaces(t, tb)
	_, out := tb.verb(t, "workspaces.add", `{"mode":"adopt","path":"scratch","id":"scratch"}`)
	j := finishJob(t, tb, out)
	if res := resultOf(t, j); j.State != StateSucceeded || res["writable"] != false || res["git"] != true {
		t.Fatalf("job = %+v %v", j, res)
	}
	if !slices.Equal(calls(log), []string{"workspaces probe --json --path scratch"}) {
		t.Errorf("calls = %v", calls(log))
	}
	if cfg, _, _ := tb.b.snapshot(); cfg.Workspaces["scratch"].Path != "scratch" || cfg.Workspaces["scratch"].Repo != "" {
		t.Errorf("workspace = %+v", cfg.Workspaces["scratch"])
	}
}

func TestWorkspaceAddRejects(t *testing.T) {
	tb := newTestBroker(t)
	log := withWorkspaces(t, tb)
	if code, out := tb.verb(t, "config.apply", `{"base_revision":0,"patch":{"workspaces":{"api":{"path":"acme/api"}}}}`); code != 200 {
		t.Fatalf("apply = %d %v", code, out)
	}
	for body, want := range map[string]string{
		`{"mode":"fork","path":"x","id":"x"}`:                                             "invalid_request",
		`{"mode":"clone","repo":"http://github.com/a/b","path":"x","id":"x"}`:             "invalid_request",
		`{"mode":"clone","repo":"https://github.com/a/b","ref":"-x","path":"x","id":"x"}`: "invalid_request",
		`{"mode":"adopt","path":"x","id":"Bad"}`:                                          "invalid_request",
		`{"mode":"adopt","path":"/x","id":"x"}`:                                           "invalid_request",
		`{"mode":"adopt","path":"x/.berth-tmp-1","id":"x"}`:                               "invalid_request",
		`{"mode":"adopt","repo":"https://github.com/a/b","path":"x","id":"x"}`:            "invalid_request",
		`{"mode":"adopt","path":"other","id":"api"}`:                                      "id_exists",
		`{"mode":"adopt","path":"acme/api","id":"x"}`:                                     "path_taken",
		`{"mode":"adopt","path":"acme","id":"x"}`:                                         "nested",
		`{"mode":"clone","repo":"https://github.com/a/b","path":"acme/api/sub","id":"x"}`: "nested",
	} {
		if code, out := tb.verb(t, "workspaces.add", body); code != 400 || errCode(out) != want {
			t.Errorf("%s: %d %v, want 400 %s", body, code, out, want)
		}
	}
	if len(tb.b.Jobs.List()) != 0 || len(calls(log)) != 0 {
		t.Errorf("a refused add ran: %+v %v", tb.b.Jobs.List(), calls(log))
	}
}

func TestWorkspaceAddRechecksAfterTheJob(t *testing.T) {
	tb := newTestBroker(t)
	withWorkspaces(t, tb)
	t.Setenv("FAKE_WS_SLOW", "0.3")
	_, out := tb.verb(t, "workspaces.add", `{"mode":"clone","repo":"https://github.com/acme/api","path":"acme/api","id":"api"}`)
	if code, out := tb.verb(t, "config.apply", `{"base_revision":0,"patch":{"workspaces":{"api":{"path":"other/api"}}}}`); code != 200 {
		t.Fatalf("apply = %d %v", code, out)
	}
	j := finishJob(t, tb, out)
	if e, _ := resultOf(t, j)["error"].(map[string]any); j.State != StateFailed || e["code"] != "id_exists" {
		t.Errorf("job = %+v %s", j, j.Result)
	}
}
