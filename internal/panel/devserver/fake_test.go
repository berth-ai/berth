package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/berth-ai/berth/internal/panel"
)

type client struct {
	t    *testing.T
	base string
	c    *http.Client
}

func newClient(t *testing.T, mode string) (*client, *fake) {
	t.Helper()
	f := newFake(mode)
	f.step = time.Millisecond
	b := httptest.NewServer(f)
	t.Cleanup(b.Close)
	p := httptest.NewServer(panel.New(b.Client(), b.URL, panel.Upstreams{}))
	t.Cleanup(p.Close)
	jar, _ := cookiejar.New(nil)
	return &client{t: t, base: p.URL, c: &http.Client{Jar: jar}}, f
}

func (c *client) call(method, path, body string) (int, map[string]any) {
	c.t.Helper()
	req, _ := http.NewRequest(method, c.base+path, strings.NewReader(body))
	if method == "POST" {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Berth-Panel", "1")
	}
	resp, err := c.c.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestFakeRunsJobsAndConflicts(t *testing.T) {
	c, _ := newClient(t, "normal")
	if code, out := c.call("POST", "/api/login", `{"user":"dev","password":"dev password"}`); code != 200 {
		t.Fatalf("login = %d %v", code, out)
	}
	code, out := c.call("POST", "/api/verbs/update", `{}`)
	if code != 202 {
		t.Fatalf("update = %d %v", code, out)
	}
	id := int(out["job"].(map[string]any)["id"].(float64))
	state := "running"
	for deadline := time.Now().Add(2 * time.Second); state == "running" && time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		_, out = c.call("GET", fmt.Sprintf("/api/jobs/%d", id), "")
		state = out["job"].(map[string]any)["state"].(string)
	}
	if state != "succeeded" {
		t.Fatalf("update ended %s", state)
	}
	if code, out := c.call("POST", "/api/verbs/rollback", `{}`); code != 202 || out["job"] == nil {
		t.Fatalf("rollback = %d %v", code, out)
	}
	if code, out := c.call("POST", "/api/verbs/config.apply", `{"base_revision":0,"patch":{"runtime":{"tz":"Europe/Rome"}}}`); code != 409 || out["config"] == nil {
		t.Errorf("stale apply = %d %v", code, out)
	}
	if code, out := c.call("POST", "/api/verbs/config.apply", `{"base_revision":1,"patch":{"runtime":{"tz":""}}}`); code != 400 || !strings.Contains(fmt.Sprint(out), "runtime.tz") {
		t.Errorf("empty tz = %d %v", code, out)
	}
	code, out = c.call("POST", "/api/verbs/config.apply", `{"base_revision":1,"patch":{"frontends":{"ide":{"enabled":true}}}}`)
	if code != 200 || fmt.Sprint(out["changes"].(map[string]any)["started"]) != "[code_server]" {
		t.Errorf("apply = %d %v", code, out)
	}
	_, out = c.call("GET", "/api/status", "")
	if !strings.Contains(fmt.Sprint(out["services"]), "failed") {
		t.Errorf("IDE should show as failed once enabled: %v", out["services"])
	}
}

func TestFakeSetup(t *testing.T) {
	c, f := newClient(t, "setup")
	if code, _ := c.call("POST", "/api/setup", `{"token":"nope","user":"dev","password":"dev password"}`); code != 403 {
		t.Errorf("bad token = %d", code)
	}
	time.Sleep(1100 * time.Millisecond) // the panel's backoff after a failed attempt
	if code, out := c.call("POST", "/api/setup", `{"token":"dev-token","user":"dev","password":"dev password","patch":{"frontends":{"ide":{"enabled":true}}}}`); code != 200 {
		t.Fatalf("setup = %d %v", code, out)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.mode != "normal" || !enabled(f.cfg, "ide") {
		t.Errorf("after setup mode=%s ide=%v", f.mode, enabled(f.cfg, "ide"))
	}
}

func TestFakeToolsReplaceAndRemove(t *testing.T) {
	c, _ := newClient(t, "normal")
	c.call("POST", "/api/login", `{"user":"dev","password":"dev password"}`)
	wait := func(verb, body string) {
		t.Helper()
		code, out := c.call("POST", "/api/verbs/"+verb, body)
		if code != 202 {
			t.Fatalf("%s = %d %v", verb, code, out)
		}
		id := int(out["job"].(map[string]any)["id"].(float64))
		for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
			if _, out = c.call("GET", fmt.Sprintf("/api/jobs/%d", id), ""); out["job"].(map[string]any)["state"] != "running" {
				return
			}
		}
		t.Fatalf("%s never finished", verb)
	}
	user := func() string {
		_, out := c.call("POST", "/api/verbs/tools.list", `{}`)
		return fmt.Sprint(out["user"])
	}
	wait("tools.add", `{"tool":"terraform","version":"2.0.0"}`)
	if got := user(); got != "[map[name:terraform version:2.0.0]]" {
		t.Errorf("after upgrade, user tools = %s", got)
	}
	wait("tools.remove", `{"tool":"terraform"}`)
	if got := user(); got != "[]" {
		t.Errorf("after remove, user tools = %s", got)
	}
}

func TestFakeMCPServers(t *testing.T) {
	c, _ := newClient(t, "normal")
	if code, out := c.call("POST", "/api/login", `{"user":"dev","password":"dev password"}`); code != 200 {
		t.Fatalf("login = %d %v", code, out)
	}
	code, out := c.call("POST", "/api/verbs/config.apply",
		`{"base_revision":1,"patch":{"mcp":{"servers":{"linear":{"url":"https://mcp.example.com/mcp","auth":"bearer","enabled":true}}}},"secrets":{"MCP_LINEAR_TOKEN":"t"}}`)
	if code != 200 {
		t.Fatalf("add = %d %v", code, out)
	}
	_, out = c.call("POST", "/api/verbs/config.get", `{}`)
	if !strings.Contains(fmt.Sprint(out["secrets"]), "MCP_LINEAR_TOKEN:map[set:true]") ||
		!strings.Contains(fmt.Sprint(out["config"]), "linear") {
		t.Errorf("config.get = %v", out)
	}
	code, _ = c.call("POST", "/api/verbs/config.apply", `{"base_revision":2,"patch":{"mcp":{"servers":{"linear":null}}}}`)
	_, out = c.call("POST", "/api/verbs/config.get", `{}`)
	if code != 200 || strings.Contains(fmt.Sprint(out["config"]), "linear") || strings.Contains(fmt.Sprint(out["secrets"]), "MCP_LINEAR_TOKEN") {
		t.Errorf("after remove = %d %v", code, out)
	}
	_, st := c.call("GET", "/api/status", "")
	if s := fmt.Sprint(st["packages"]); !strings.Contains(s, "mcp_id:files") || !strings.Contains(s, "Other Agent") {
		t.Errorf("packages = %s", s)
	}
}

func TestFakeSources(t *testing.T) {
	f := newFake("normal")
	f.step = 0
	last := func() *job {
		waitFakeJobs(t, f)
		f.mu.Lock()
		defer f.mu.Unlock()
		for i := len(f.jobs) - 1; i >= 0; i-- {
			if f.jobs[i].Verb != "skills.sync" {
				return f.jobs[i]
			}
		}
		return nil
	}
	res := func(j *job) map[string]any {
		var m map[string]any
		json.Unmarshal(j.Result, &m)
		return m
	}
	names := func(v any) string {
		var out []string
		for _, r := range v.([]any) {
			out = append(out, r.(map[string]any)["name"].(string))
		}
		return fmt.Sprint(out)
	}
	repo := "https://github.com/acme/eng-skills"
	f.verb("skills.sources.preview", map[string]any{"repo": repo, "ref": "main", "path": ""})
	if r := res(last()); r["id"] != "acme-eng-skills" || r["commit"] != fakeCommit || len(r["found"].(map[string]any)) != 3 {
		t.Fatalf("preview new = %v", r)
	}
	f.verb("skills.sources.add", map[string]any{"repo": repo, "ref": "main", "path": "", "commit": fakeCommit, "attach_all": true})
	if r := res(last()); fmt.Sprint(r["attached"]) != "[changelog release-notes]" {
		t.Fatalf("add = %v", r)
	}
	if s := f.source("acme-eng-skills"); s == nil || s["commit"] != fakeCommit || len(s["attached"].(map[string]any)) != 2 {
		t.Fatalf("source = %v", s)
	}
	if code, out := f.verb("skills.sources.add", map[string]any{"repo": repo, "ref": "main", "path": ""}); code != http.StatusBadRequest ||
		!strings.Contains(fmt.Sprint(out), "source_exists") {
		t.Errorf("again = %d %v", code, out)
	}
	f.verb("skills.sources.preview", map[string]any{"id": "acme-eng-skills", "ref": "v2"})
	d := res(last())
	if d["changed"] != true || names(d["removed"]) != "[changelog]" || names(d["added"]) != "[triage]" || names(d["described"]) != "[release-notes]" {
		t.Errorf("diff = %v", d)
	}
	if !d["removed"].([]any)[0].(map[string]any)["attached"].(bool) {
		t.Error("the removed skill is attached")
	}
	f.verb("skills.sources.update", map[string]any{"id": "acme-eng-skills", "ref": "v2", "commit": fakeCommit2})
	if r := res(last()); r["changed"] != true || fmt.Sprint(r["missing"]) != "[changelog]" {
		t.Errorf("update = %v", r)
	}
	if s := f.source("acme-eng-skills"); s["ref"] != "v2" || s["commit"] != fakeCommit2 {
		t.Errorf("after update = %v", s)
	}
	f.verb("skills.sources.preview", map[string]any{"id": "acme-eng-skills"})
	if r := res(last()); r["changed"] != false {
		t.Errorf("up to date = %v", r)
	}
	f.verb("skills.sources.preview", map[string]any{"repo": "https://github.com/acme/missing", "ref": "main"})
	if j := last(); j.State != "failed" || !strings.Contains(string(j.Result), `"field":"ref"`) {
		t.Errorf("missing = %s", j.Result)
	}
	if code, _ := f.verb("skills.sources.preview", map[string]any{"id": "nope"}); code != http.StatusNotFound {
		t.Errorf("unknown preview = %d", code)
	}
	if code, out := f.verb("skills.sources.remove", map[string]any{"id": "acme-eng-skills"}); code != http.StatusOK || f.source("acme-eng-skills") != nil {
		t.Errorf("remove = %d %v", code, out)
	}
	code, out := f.verb("config.upgrade", map[string]any{"config": map[string]any{"schema": 4, "skills": map[string]any{"pdf": map[string]any{}}}})
	up, _ := out.(map[string]any)["config"].(map[string]any)
	if code != http.StatusOK || num(up["schema"]) != 6 || up["skills"].(map[string]any)["sources"] == nil {
		t.Errorf("upgrade = %d %v", code, out)
	}
}

func waitFakeJobs(t *testing.T, f *fake) {
	t.Helper()
	for range 200 {
		f.mu.Lock()
		running := slices.ContainsFunc(f.jobs, func(j *job) bool { return j.State == "running" })
		f.mu.Unlock()
		if !running {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("fake jobs did not finish")
}

func waitFakeJob(t *testing.T, c *client, id string, ok func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, out := c.call("GET", "/api/jobs/"+id, "")
		if job, _ := out["job"].(map[string]any); job != nil && ok(job) {
			return job
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %s never got there", id)
	return nil
}

func loginOf(job map[string]any) map[string]any {
	l, _ := job["login"].(map[string]any)
	return l
}

func TestFakeSignIns(t *testing.T) {
	c, _ := newClient(t, "normal")
	c.call("POST", "/api/login", `{"user":"dev","password":"dev password"}`)

	code, out := c.call("POST", "/api/verbs/login.start", `{"agent":"@example/agent-cli","method":"account"}`)
	if code != 202 {
		t.Fatalf("start = %d %v", code, out)
	}
	if code, _ := c.call("POST", "/api/verbs/login.start", `{"agent":"@example/other-cli","method":"chatgpt"}`); code != 409 {
		t.Errorf("a second sign-in started: %d", code)
	}
	job := waitFakeJob(t, c, "1", func(j map[string]any) bool { return loginOf(j)["waiting"] == true })
	first := loginOf(job)["url"]
	c.call("POST", "/api/verbs/login.input", `{"job":1,"code":"wrong"}`)
	job = waitFakeJob(t, c, "1", func(j map[string]any) bool {
		return loginOf(j)["waiting"] == true && loginOf(j)["url"] != first
	})
	if loginOf(job)["inputs"] != float64(1) {
		t.Errorf("login = %v", loginOf(job))
	}
	c.call("POST", "/api/verbs/login.input", `{"job":1,"code":"good"}`)
	waitFakeJob(t, c, "1", func(j map[string]any) bool { return j["state"] == "succeeded" })

	_, st := c.call("POST", "/api/verbs/login.status", `{}`)
	ex := st["agents"].(map[string]any)["@example/agent-cli"].(map[string]any)["methods"].([]any)[0].(map[string]any)
	if ex["signed_in"] != true || ex["account"] != "you@example.org" {
		t.Errorf("status = %v", ex)
	}

	c.call("POST", "/api/verbs/login.start", `{"agent":"@example/other-cli","method":"copilot"}`)
	job = waitFakeJob(t, c, "2", func(j map[string]any) bool { return j["state"] == "succeeded" })
	if job["args"].([]any)[4] != "copilot" {
		t.Errorf("args = %v", job["args"])
	}

	c.call("POST", "/api/verbs/login.start", `{"agent":"@example/agent-cli","method":"account"}`)
	waitFakeJob(t, c, "3", func(j map[string]any) bool { return loginOf(j)["waiting"] == true })
	if code, _ := c.call("POST", "/api/verbs/login.cancel", `{"job":3}`); code != 200 {
		t.Fatalf("cancel = %d", code)
	}
	if code, _ := c.call("POST", "/api/verbs/login.cancel", `{"job":3}`); code != 409 {
		t.Errorf("second cancel = %d", code)
	}
	job = waitFakeJob(t, c, "3", func(j map[string]any) bool { return j["state"] == "failed" })
	if job["result"].(map[string]any)["error"].(map[string]any)["code"] != "cancelled" {
		t.Errorf("result = %v", job["result"])
	}

	if code, _ := c.call("POST", "/api/verbs/login.logout", `{"agent":"@example/agent-cli","method":"account"}`); code != 200 {
		t.Errorf("logout = %d", code)
	}
	_, st = c.call("POST", "/api/verbs/login.status", `{}`)
	ex = st["agents"].(map[string]any)["@example/agent-cli"].(map[string]any)["methods"].([]any)[0].(map[string]any)
	if ex["signed_in"] != false {
		t.Errorf("still signed in after logout: %v", ex)
	}

	_, status := c.call("GET", "/api/status", "")
	if pkgs := status["packages"].([]any); pkgs[0].(map[string]any)["icon"] != "claude" {
		t.Errorf("first package = %v", pkgs[0])
	}
}

func TestFakeAddsAndRemovesCopilot(t *testing.T) {
	f := newFake("normal")
	f.step = time.Millisecond
	// verb expects f.mu held, as ServeHTTP holds it; jobs take it too.
	call := func(name string, in map[string]any) (int, any) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.verb(name, in)
	}
	code, out := call("agents.registry", nil)
	rows := out.(map[string]any)["agents"].([]map[string]any)
	if code != 200 || len(rows) != 1 || rows[0]["id"] != "copilot" || rows[0]["added"] != false {
		t.Fatalf("registry = %d %v", code, out)
	}
	code, out = call("agents.add", map[string]any{"id": "copilot"})
	if code != 202 {
		t.Fatalf("add = %d %v", code, out)
	}
	j := out.(map[string]any)["job"].(*job)
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		f.mu.Lock()
		state := j.State
		f.mu.Unlock()
		if state != "running" {
			break
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock() // from here on, f.verb runs with the lock held
	if j.State != "succeeded" {
		t.Fatalf("job = %+v", j)
	}
	pkgs := f.packages()
	i := slices.IndexFunc(pkgs, func(p map[string]any) bool { return p["package"] == "@github/copilot" })
	if i < 0 || pkgs[i]["origin"] != "added" || pkgs[0]["origin"] != "locked" {
		t.Errorf("packages = %v", pkgs)
	}
	if _, ok := f.loginStatus("")["agents"].(map[string]any)["@github/copilot"]; !ok {
		t.Error("an added Copilot has no sign-in")
	}
	if code, _ := f.verb("agents.add", map[string]any{"id": "copilot"}); code != 409 {
		t.Errorf("second add = %d", code)
	}
	if code, _ := f.verb("agents.remove", map[string]any{"package": "@github/copilot"}); code != 200 {
		t.Errorf("remove = %d", code)
	}
	if slices.ContainsFunc(f.packages(), func(p map[string]any) bool { return p["package"] == "@github/copilot" }) {
		t.Error("Copilot still listed after remove")
	}
}

func TestFakeWorkspaces(t *testing.T) {
	f := newFake("normal")
	f.step = 0
	last := func(verb string) *job {
		waitFakeJobs(t, f)
		f.mu.Lock()
		defer f.mu.Unlock()
		for i := len(f.jobs) - 1; i >= 0; i-- {
			if f.jobs[i].Verb == verb {
				return f.jobs[i]
			}
		}
		t.Fatalf("no %s job", verb)
		return nil
	}
	ws := func() map[string]any { return f.cfg["workspaces"].(map[string]any) }

	f.verb("workspaces.add", map[string]any{"mode": "clone", "repo": "https://github.com/acme/private", "path": "acme/private", "id": "private"})
	if j := last("workspaces.add"); j.State != "failed" || !strings.Contains(string(j.Result), `"code":"auth_required"`) {
		t.Errorf("private = %s", j.Result)
	}
	f.verb("workspaces.add", map[string]any{"mode": "clone", "repo": "https://github.com/acme/api", "path": "acme/api", "id": "acme-api"})
	if j := last("workspaces.add"); j.State != "succeeded" || ws()["acme-api"].(map[string]any)["path"] != "acme/api" {
		t.Fatalf("clone = %+v %s", j, j.Result)
	}
	f.verb("workspaces.add", map[string]any{"mode": "adopt", "path": "vendor/readonly-repo", "id": "vendor"})
	if j := last("workspaces.add"); !strings.Contains(string(j.Result), `"writable":false`) {
		t.Errorf("adopt = %s", j.Result)
	}
	if _, out := f.verb("workspaces.candidates", nil); fmt.Sprint(out) != "map[candidates:[map[git:false path:scratch]]]" {
		t.Errorf("candidates = %v", out)
	}
	if code, out := f.verb("workspaces.launch", map[string]any{"id": "acme-api", "agent": "@example/agent-cli"}); code != 200 || out.(map[string]any)["window"] != "example" {
		t.Errorf("launch = %d %v", code, out)
	}
	_, out := f.verb("workspaces.status", nil)
	st := out.(map[string]any)["workspaces"].(map[string]any)["acme-api"].(map[string]any)
	if st["session"] != true || fmt.Sprint(st["windows"]) != "[example]" || st["git"] != true {
		t.Errorf("status = %v", st)
	}
	f.verb("workspaces.forget", map[string]any{"id": "acme-api", "delete": true, "confirm": "acme/api"})
	if j := last("workspaces.forget"); j.State != "succeeded" || !strings.Contains(string(j.Result), `"deleted":true`) || ws()["acme-api"] != nil {
		t.Errorf("forget = %+v %s", j, j.Result)
	}
	if code, _ := f.verb("workspaces.forget", map[string]any{"id": "vendor", "delete": true, "confirm": "wrong"}); code != 400 {
		t.Errorf("a wrong confirm = %d", code)
	}
}

func TestFakeWorkspaceAttachments(t *testing.T) {
	f := newFake("normal")
	f.step = 0
	f.mu.Lock()
	next := clone(f.cfg)
	next["workspaces"].(map[string]any)["acme-api"] = map[string]any{"path": "acme/api", "repo": "https://github.com/acme/api",
		"env": map[string]any{}, "instructions": map[string]any{"shared": false}}
	sourcesOf(next)["acme-eng-skills"] = map[string]any{"repo": "https://github.com/acme/eng-skills", "ref": "main", "path": "",
		"commit": fakeCommit, "found": map[string]any{
			"changelog":     map[string]any{"path": "changelog", "description": "Keep CHANGELOG.md"},
			"release-notes": map[string]any{"path": "release-notes", "description": "Notes"}},
		"attached": map[string]any{
			"changelog":     map[string]any{"enabled": true, "agents": nil, "workspaces": map[string]any{"acme-api": "link"}},
			"release-notes": map[string]any{"enabled": true, "agents": []any{}, "workspaces": map[string]any{"acme-api": "vendor"}}}}
	next["mcp"].(map[string]any)["workspaces"] = map[string]any{"files": []any{"acme-api"}}
	f.replace(next)
	f.mu.Unlock()

	code, out := f.verb("workspaces.report", map[string]any{"id": "acme-api"})
	rep := out.(map[string]any)["report"].(map[string]any)
	entries := rep["skills"].(map[string]any)["entries"].(map[string]any)
	if code != http.StatusOK || entries["acme-eng-skills/changelog"].(map[string]any)["mode"] != "link" ||
		entries["acme-eng-skills/release-notes"].(map[string]any)["edited"] != true {
		t.Fatalf("report = %d %v", code, out)
	}
	mcp := rep["mcp"].(map[string]any)
	if mcp["trust"] != true || mcp["files"].(map[string]any)[".mcp.json"].(map[string]any)["tracked"] != true {
		t.Errorf("mcp = %v", mcp)
	}
	if code, _ := f.verb("workspaces.report", map[string]any{"id": "nope"}); code != http.StatusNotFound {
		t.Errorf("unknown = %d", code)
	}

	resolve := func(action string) {
		t.Helper()
		f.verb("skills.vendor.resolve", map[string]any{"source": "acme-eng-skills", "name": "release-notes", "workspace": "acme-api", "action": action})
		waitFakeJobs(t, f)
	}
	resolve("overwrite")
	_, out = f.verb("workspaces.report", map[string]any{"id": "acme-api"})
	entries = out.(map[string]any)["report"].(map[string]any)["skills"].(map[string]any)["entries"].(map[string]any)
	if entries["acme-eng-skills/release-notes"].(map[string]any)["edited"] == true {
		t.Error("overwrite kept the edit")
	}
	resolve("release")
	if _, ok := sourcesOf(f.cfg)["acme-eng-skills"].(map[string]any)["attached"].(map[string]any)["release-notes"]; ok {
		t.Error("release kept a workspace-only attachment")
	}

	f.verb("workspaces.forget", map[string]any{"id": "acme-api"})
	waitFakeJobs(t, f)
	a := sourcesOf(f.cfg)["acme-eng-skills"].(map[string]any)["attached"].(map[string]any)["changelog"].(map[string]any)
	if len(a["workspaces"].(map[string]any)) != 0 || len(f.cfg["mcp"].(map[string]any)["workspaces"].(map[string]any)) != 0 {
		t.Errorf("forget kept references: %v %v", a, f.cfg["mcp"])
	}
}
