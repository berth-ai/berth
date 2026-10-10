package broker

import (
	"encoding/json"
	"fmt"
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
release)
  if [ -n "$FAKE_WS_KEPT" ]; then
    printf '{"kept":["docx in api has local edits"],"schema":1}'
  else
    printf '{"kept":[],"schema":1}'
  fi ;;
report) printf '{"skills":{"path":"/ws/acme/api","entries":{"fx/pdf":{"name":"pdf","mode":"link","commit":"","from":"skills/pdf","links":[".claude/skills/pdf"]}},"dirs":[]},"mcp":{"path":"","files":{},"dirs":[]},"schema":1}' ;;
vendor) printf '{"workspace":"api","key":"fx/pdf","action":"overwrite","schema":1}' ;;
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

// addWorkspace saves workspace id at path through config.apply.
func addWorkspace(t *testing.T, tb *testBroker, id, path string) {
	t.Helper()
	cur, _, _ := tb.b.snapshot()
	body := fmt.Sprintf(`{"base_revision":%d,"patch":{"workspaces":{%q:{"path":%q}}}}`, cur.Revision, id, path)
	if code, out := tb.verb(t, "config.apply", body); code != 200 {
		t.Fatalf("apply = %d %v", code, out)
	}
}

func TestWorkspaceForget(t *testing.T) {
	tb := newTestBroker(t)
	log := withWorkspaces(t, tb)
	addWorkspace(t, tb, "api", "acme/api")
	_, out := tb.verb(t, "workspaces.forget", `{"id":"api"}`)
	j := finishJob(t, tb, out)
	if res := resultOf(t, j); j.State != StateSucceeded || res["deleted"] != false || res["path"] != "acme/api" {
		t.Fatalf("job = %+v %s", j, j.Result)
	}
	if !slices.Equal(calls(log), []string{"workspaces kill --json --id api", "workspaces release --json --id api"}) {
		t.Errorf("calls = %v", calls(log))
	}
	if cfg, _, _ := tb.b.snapshot(); len(cfg.Workspaces) != 0 {
		t.Errorf("workspaces = %v", cfg.Workspaces)
	}
	if ws, _ := config.ReadWorkspacesFile(tb.b.WorkspacesSettings); len(ws) != 0 {
		t.Errorf("workspaces.json = %v", ws)
	}
}

func TestWorkspaceForgetAndDelete(t *testing.T) {
	tb := newTestBroker(t)
	log := withWorkspaces(t, tb)
	addWorkspace(t, tb, "api", "acme/api")
	for body, want := range map[string]string{
		`{"id":"api","delete":true}`:                  "confirm_mismatch",
		`{"id":"api","delete":true,"confirm":"acme"}`: "confirm_mismatch",
		`{"id":"api","confirm":"acme/api"}`:           "invalid_request",
	} {
		if code, out := tb.verb(t, "workspaces.forget", body); code != 400 || errCode(out) != want {
			t.Errorf("%s: %d %v, want 400 %s", body, code, out, want)
		}
	}
	if code, out := tb.verb(t, "workspaces.forget", `{"id":"nope"}`); code != 404 || errCode(out) != "unknown_workspace" {
		t.Errorf("unknown = %d %v", code, out)
	}
	_, out := tb.verb(t, "workspaces.forget", `{"id":"api","delete":true,"confirm":"acme/api"}`)
	j := finishJob(t, tb, out)
	if res := resultOf(t, j); j.State != StateSucceeded || res["deleted"] != true {
		t.Fatalf("job = %+v %s", j, j.Result)
	}
	if want := []string{"workspaces kill --json --id api", "workspaces release --json --id api", "workspaces remove --json --path acme/api"}; !slices.Equal(calls(log), want) {
		t.Errorf("calls = %v", calls(log))
	}
}

func TestWorkspaceDeleteIncomplete(t *testing.T) {
	tb := newTestBroker(t)
	withWorkspaces(t, tb)
	addWorkspace(t, tb, "api", "acme/api")
	t.Setenv("FAKE_WS_LEFT", "1")
	_, out := tb.verb(t, "workspaces.forget", `{"id":"api","delete":true,"confirm":"acme/api"}`)
	j := finishJob(t, tb, out)
	e, _ := resultOf(t, j)["error"].(map[string]any)
	if j.State != StateFailed || e["code"] != "delete_incomplete" || fmt.Sprint(e["left"]) != "[acme/api/locked/f]" {
		t.Errorf("job = %+v %s", j, j.Result)
	}
	if cfg, _, _ := tb.b.snapshot(); len(cfg.Workspaces) != 0 {
		t.Error("the workspace must stay forgotten")
	}
}

func TestWorkspaceStatusAndCandidates(t *testing.T) {
	tb := newTestBroker(t)
	log := withWorkspaces(t, tb)
	if code, out := tb.verb(t, "workspaces.status", `{}`); code != 200 || len(out["workspaces"].(map[string]any)) != 0 || len(calls(log)) != 0 {
		t.Fatalf("empty status = %d %v, calls %v", code, out, calls(log))
	}
	addWorkspace(t, tb, "web", "acme/web")
	addWorkspace(t, tb, "api", "acme/api")
	code, out := tb.verb(t, "workspaces.status", `{}`)
	ws := out["workspaces"].(map[string]any)
	api, web := ws["api"].(map[string]any), ws["web"].(map[string]any)
	if code != 200 || api["dirty"] != 2.0 || api["session"] != true || web["unknown"] != true || web["dirty"] != -1.0 {
		t.Errorf("status = %d %v", code, out)
	}
	code, out = tb.verb(t, "workspaces.candidates", `{}`)
	if c, _ := out["candidates"].([]any); code != 200 || len(c) != 1 || c[0].(map[string]any)["path"] != "scratch" {
		t.Errorf("candidates = %d %v", code, out)
	}
	want := []string{
		"workspaces status --json --ws api=acme/api --ws web=acme/web",
		"workspaces candidates --json --exclude acme/api --exclude acme/web",
	}
	if !slices.Equal(calls(log), want) {
		t.Errorf("calls = %v", calls(log))
	}
}

func TestWorkspaceLaunch(t *testing.T) {
	tb := newTestBroker(t)
	withAgents(t, tb)
	log := withWorkspaces(t, tb)
	addWorkspace(t, tb, "api", "acme/api")
	if code, out := tb.verb(t, "workspaces.launch", `{"id":"api","agent":"agent-a"}`); code != 200 || out["session"] != "ws-api" || out["window"] != "a" {
		t.Errorf("agent launch = %d %v", code, out)
	}
	if code, out := tb.verb(t, "workspaces.launch", `{"id":"api"}`); code != 200 || out["window"] != "" {
		t.Errorf("shell launch = %d %v", code, out)
	}
	want := []string{
		"workspaces launch --json --id api --path acme/api --window a --run " + filepath.Join(tb.dir, "bin", "a"),
		"workspaces launch --json --id api --path acme/api",
	}
	if !slices.Equal(calls(log), want) {
		t.Errorf("calls = %v", calls(log))
	}
	for body, want := range map[string]struct {
		code int
		err  string
	}{
		`{"id":"nope"}`:                {404, "unknown_workspace"},
		`{"id":"api","agent":"mcp-b"}`: {400, "unknown_agent"},
		`{"id":"api","agent":"nope"}`:  {400, "unknown_agent"},
	} {
		if code, out := tb.verb(t, "workspaces.launch", body); code != want.code || errCode(out) != want.err {
			t.Errorf("%s: %d %v, want %d %s", body, code, out, want.code, want.err)
		}
	}
	cur, _, _ := tb.b.snapshot()
	tb.verb(t, "config.apply", fmt.Sprintf(`{"base_revision":%d,"patch":{"agents":{"disabled":["agent-a"]}}}`, cur.Revision))
	if code, out := tb.verb(t, "workspaces.launch", `{"id":"api","agent":"agent-a"}`); code != 409 || errCode(out) != "agent_disabled" {
		t.Errorf("disabled = %d %v", code, out)
	}
	t.Setenv("FAKE_WS_MISSING", "1")
	if code, out := tb.verb(t, "workspaces.launch", `{"id":"api"}`); code != 409 || errCode(out) != "workspace_missing" {
		t.Errorf("missing folder = %d %v", code, out)
	}
}

func TestForgetPatch(t *testing.T) {
	cur := config.Defaults()
	for _, id := range []string{"a", "b"} {
		cur.Workspaces[id] = config.Workspace{Path: "acme/" + id, Env: map[string]config.WorkspaceEnv{}}
	}
	cur.Skills.Sources["x"] = config.Source{Repo: "https://github.com/a/b", Ref: "main", Path: "skills", Commit: skillCommit,
		Found: map[string]config.Found{"pdf": {Path: "skills/pdf", Description: "d"}, "docx": {Path: "skills/docx", Description: "d"}},
		Attached: map[string]config.Attachment{
			"pdf":  {Enabled: true, Agents: []string{}, Workspaces: map[string]string{"a": config.ModeLink, "b": config.ModeVendor}},
			"docx": {Enabled: true, Agents: []string{}, Workspaces: map[string]string{"a": config.ModeLink}},
		}}
	cur.MCP.Workspaces = map[string][]string{"context7": {"a"}, "linear": {"a", "b"}}
	if err := cur.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(forgetPatch(cur, "a"))
	next, err := config.Patch(cur, raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := next.Validate(); err != nil {
		t.Fatalf("forgetting a leaves references: %v", err)
	}
	x := next.Skills.Sources["x"]
	if _, ok := next.Workspaces["a"]; ok || len(x.Attached["docx"].Workspaces) != 0 || x.Attached["pdf"].Workspaces["b"] != config.ModeVendor {
		t.Errorf("skills after = %+v", x.Attached)
	}
	if _, ok := next.MCP.Workspaces["context7"]; ok || !slices.Equal(next.MCP.Workspaces["linear"], []string{"b"}) {
		t.Errorf("mcp.workspaces after = %v", next.MCP.Workspaces)
	}
	if p := forgetPatch(cur, "zz"); len(p) != 1 {
		t.Errorf("an unreferenced workspace touches only workspaces: %v", p)
	}
}

func TestWorkspaceForgetReleases(t *testing.T) {
	tb := newTestBroker(t)
	withWorkspaces(t, tb)
	addWorkspace(t, tb, "api", "acme/api")
	t.Setenv("FAKE_WS_KEPT", "1")
	_, out := tb.verb(t, "workspaces.forget", `{"id":"api","delete":true,"confirm":"acme/api"}`)
	j := finishJob(t, tb, out)
	res := resultOf(t, j)
	if j.State != StateSucceeded || res["deleted"] != true || fmt.Sprint(res["kept"]) != "[docx in api has local edits]" {
		t.Errorf("job = %+v %s", j, j.Result)
	}
}

func TestWorkspaceReport(t *testing.T) {
	tb := newTestBroker(t)
	log := withWorkspaces(t, tb)
	addWorkspace(t, tb, "api", "acme/api")
	code, out := tb.verb(t, "workspaces.report", `{"id":"api"}`)
	rep, _ := out["report"].(map[string]any)
	if code != 200 || rep["skills"] == nil || rep["mcp"] == nil || rep["schema"] != nil {
		t.Fatalf("report = %d %v", code, out)
	}
	if c := calls(log); c[len(c)-1] != "workspaces report --json --id api" {
		t.Errorf("calls = %v", c)
	}
	if code, out := tb.verb(t, "workspaces.report", `{"id":"nope"}`); code != 404 || errCode(out) != "unknown_workspace" {
		t.Errorf("unknown = %d %v", code, out)
	}
}
