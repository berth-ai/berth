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

func TestFakeSkills(t *testing.T) {
	f := newFake("normal")
	f.step = 0
	code, out := f.verb("skills.add", map[string]any{"repo": "https://github.com/anthropics/skills", "path": "skills/pdf", "ref": "main"})
	if code != http.StatusAccepted {
		t.Fatalf("add = %d %v", code, out)
	}
	waitFakeJobs(t, f)
	skills := f.cfg["skills"].(map[string]any)
	if pdf, ok := skills["pdf"].(map[string]any); !ok || pdf["enabled"] != true {
		t.Fatalf("skills = %v", skills)
	}
	f.verb("skills.add", map[string]any{"repo": "https://github.com/acme/missing", "ref": "main"})
	waitFakeJobs(t, f)
	last := f.jobs[len(f.jobs)-1]
	if last.State != "failed" || !strings.Contains(string(last.Result), `"field":"ref"`) {
		t.Errorf("missing = %+v %s", last, last.Result)
	}
	if code, _ := f.verb("skills.update", map[string]any{"id": "nothere"}); code != http.StatusNotFound {
		t.Errorf("unknown update = %d", code)
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
