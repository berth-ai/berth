package broker

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/login"
	"github.com/berth-ai/berth/internal/status"
	"github.com/berth-ai/berth/internal/supervisor"
)

// fakeExec scripts the berth binary.
type fakeExec struct {
	mu     sync.Mutex
	calls  [][]string
	stdout string
	exit   int
}

func (f *fakeExec) run(_ context.Context, args []string, stdout, stderr io.Writer) (int, error) {
	f.mu.Lock()
	f.calls = append(f.calls, args)
	out, exit := f.stdout, f.exit
	f.mu.Unlock()
	io.WriteString(stderr, "berth: working\n")
	io.WriteString(stdout, out)
	return exit, nil
}

func (f *fakeExec) last() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return nil
	}
	return f.calls[len(f.calls)-1]
}

// fakeServices records what the broker asks of the supervisor.
type fakeServices struct {
	mu         sync.Mutex
	reconciles [][]supervisor.Spec
	restarts   []string
	status     []supervisor.Status
	onRestart  func(name string) // runs after the restart is recorded
	started    []string          // what the next Reconcile reports started
}

func (f *fakeServices) Reconcile(specs []supervisor.Spec) supervisor.Changes {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reconciles = append(f.reconciles, specs)
	started := append([]string{}, f.started...)
	f.started = nil
	return supervisor.Changes{Started: started, Restarted: []string{}, Stopped: []string{}}
}

func (f *fakeServices) Restart(name string) error {
	f.mu.Lock()
	f.restarts = append(f.restarts, name)
	hook := f.onRestart
	f.mu.Unlock()
	if hook != nil {
		hook(name)
	}
	return nil
}

func (f *fakeServices) Status() []supervisor.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}

func (f *fakeServices) lastNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.reconciles) == 0 {
		return nil
	}
	var names []string
	for _, s := range f.reconciles[len(f.reconciles)-1] {
		names = append(names, s.Name)
	}
	return names
}

type testBroker struct {
	b      *Broker
	srv    *httptest.Server
	exec   *fakeExec
	svc    *fakeServices
	dir    string
	logins login.Store
}

func newTestBroker(t *testing.T) *testBroker {
	t.Helper()
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	jobs := NewJobs(ctx)
	f := &fakeExec{stdout: `{"changed":true,"schema":1}`}
	svc := &fakeServices{}
	logins := login.Store{Path: filepath.Join(dir, "secrets", "panel.json")}
	b := New(&config.Store{Dir: dir}, logins, svc, jobs, f.run)
	srv := httptest.NewServer(b.Handler())
	t.Cleanup(func() { srv.Close(); cancel(); jobs.Wait() })
	if err := logins.Set("me", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	return &testBroker{b: b, srv: srv, exec: f, svc: svc, dir: dir, logins: logins}
}

func (tb *testBroker) call(t *testing.T, method, path, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, tb.srv.URL+path, strings.NewReader(body))
	resp, err := tb.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("%s %s: decode: %v", method, path, err)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	return resp.StatusCode, out
}

func (tb *testBroker) verb(t *testing.T, verb, body string) (int, map[string]any) {
	t.Helper()
	return tb.call(t, "POST", "/verbs/"+verb, body)
}

func errCode(out map[string]any) string {
	e, _ := out["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

func (tb *testBroker) waitIdle(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if list := tb.b.Jobs.List(); len(list) > 0 && list[0].State != StateRunning {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("job did not finish")
}

func TestUpdateArgs(t *testing.T) {
	tb := newTestBroker(t)
	for body, want := range map[string][]string{
		`{}`:                  {"update", "--json", "--ref", "main"},
		``:                    {"update", "--json", "--ref", "main"},
		`{"ref":"v1.2.3"}`:    {"update", "--json", "--ref", "v1.2.3"},
		`{"image":true}`:      {"update", "--json", "--image"},
		`{"ref":"feature/x"}`: {"update", "--json", "--ref", "feature/x"},
	} {
		code, out := tb.verb(t, "update", body)
		if code != http.StatusAccepted {
			t.Fatalf("%s: %d %v", body, code, out)
		}
		tb.waitIdle(t)
		if got := tb.exec.last(); !slices.Equal(got, want) {
			t.Errorf("%s: args %q, want %q", body, got, want)
		}
	}
	tb.verb(t, "config.apply", `{"base_revision":0,"patch":{"agents":{"ref":"stable"}}}`)
	tb.verb(t, "update", `{}`)
	tb.waitIdle(t)
	if got := tb.exec.last(); !slices.Equal(got, []string{"update", "--json", "--ref", "stable"}) {
		t.Errorf("configured ref not used: %q", got)
	}
}

func TestRollbackAndGCArgs(t *testing.T) {
	tb := newTestBroker(t)
	for _, c := range []struct {
		verb, body string
		want       []string
	}{
		{"rollback", `{}`, []string{"rollback", "--json"}},
		{"rollback", `{"hash":"0123456789ab"}`, []string{"rollback", "--json", "0123456789ab"}},
		{"gc", `{}`, []string{"gc", "--json"}},
	} {
		if code, out := tb.verb(t, c.verb, c.body); code != http.StatusAccepted {
			t.Fatalf("%s %s: %d %v", c.verb, c.body, code, out)
		}
		tb.waitIdle(t)
		if got := tb.exec.last(); !slices.Equal(got, c.want) {
			t.Errorf("%s %s: %q", c.verb, c.body, got)
		}
	}
}

func TestVerbRejects(t *testing.T) {
	tb := newTestBroker(t)
	for _, c := range []struct {
		verb, body string
		code       int
		errCode    string
	}{
		{"frobnicate", `{}`, 404, "unknown_verb"},
		{"update", `{"lock_dir":"/tmp"}`, 400, "invalid_request"},
		{"update", `{"ref":"main","image":true}`, 400, "invalid_request"},
		{"update", `{"ref":"-x"}`, 400, "invalid_request"},
		{"update", `{"ref":"a/../b"}`, 400, "invalid_request"},
		{"update", `{"ref":"a b"}`, 400, "invalid_request"},
		{"update", `{} {}`, 400, "invalid_request"},
		{"update", `[]`, 400, "invalid_request"},
		{"rollback", `{"hash":"0123"}`, 400, "invalid_request"},
		{"rollback", `{"hash":"../../etc/xx"}`, 400, "invalid_request"},
		{"gc", `{"keep":1}`, 400, "invalid_request"},
		{"config.get", `{"x":1}`, 400, "invalid_request"},
		{"update", `{"ref":"` + strings.Repeat("a", maxBodyBytes) + `"}`, 413, "too_large"},
	} {
		code, out := tb.verb(t, c.verb, c.body)
		if code != c.code || errCode(out) != c.errCode {
			t.Errorf("%s %.40s: %d %v", c.verb, c.body, code, out)
		}
	}
	if len(tb.exec.calls) != 0 {
		t.Errorf("rejected verbs ran berth: %v", tb.exec.calls)
	}
}

func TestJobRoutes(t *testing.T) {
	tb := newTestBroker(t)
	tb.verb(t, "gc", `{}`)
	tb.waitIdle(t)
	code, out := tb.call(t, "GET", "/jobs", "")
	jobs, _ := out["jobs"].([]any)
	if code != 200 || len(jobs) != 1 {
		t.Fatalf("GET /jobs = %d %v", code, out)
	}
	code, out = tb.call(t, "GET", "/jobs/1", "")
	job, _ := out["job"].(map[string]any)
	if code != 200 || job["log"] != "berth: working\n" || job["state"] != "succeeded" {
		t.Errorf("GET /jobs/1 = %d %v", code, out)
	}
	for _, path := range []string{"/jobs/2", "/jobs/x"} {
		if code, out := tb.call(t, "GET", path, ""); code != 404 || errCode(out) != "not_found" {
			t.Errorf("GET %s = %d %v", path, code, out)
		}
	}
}

func TestStatus(t *testing.T) {
	tb := newTestBroker(t)
	tb.svc.status = []supervisor.Status{{Name: "panel", State: supervisor.StateRunning, Log: []string{}}}
	tb.b.Agents = func() (status.Report, error) { return status.Report{FollowImage: true}, nil }
	tb.b.Load()
	code, out := tb.call(t, "GET", "/status", "")
	if code != 200 || out["mode"] != "normal" || out["revision"] != float64(0) {
		t.Fatalf("status = %d %v", code, out)
	}
	if svcs := out["services"].([]any); len(svcs) != 1 {
		t.Errorf("services = %v", svcs)
	}
	if agents := out["agents"].(map[string]any); agents["follow_image"] != true {
		t.Errorf("agents = %v", agents)
	}
}

func TestConfigApply(t *testing.T) {
	tb := newTestBroker(t)
	tb.b.Load()
	tb.b.Reconcile()
	if got := tb.svc.lastNames(); !slices.Equal(got, []string{"panel", "ttyd"}) {
		t.Fatalf("initial services = %v", got)
	}
	code, out := tb.verb(t, "config.apply", `{"base_revision":0,"patch":{"frontends":{"ide":{"enabled":true}}}}`)
	if code != 200 || out["config"].(map[string]any)["revision"] != float64(1) || out["changes"] == nil {
		t.Fatalf("apply = %d %v", code, out)
	}
	if got := tb.svc.lastNames(); !slices.Equal(got, []string{"panel", "ttyd", "code_server"}) {
		t.Errorf("services after apply = %v", got)
	}
	code, out = tb.verb(t, "config.apply", `{"base_revision":0,"patch":{"agents":{"keep":4}}}`)
	if code != 409 || errCode(out) != "conflict" || out["config"].(map[string]any)["revision"] != float64(1) {
		t.Errorf("stale apply = %d %v", code, out)
	}
	code, out = tb.verb(t, "config.apply", `{"base_revision":1,"patch":{"agents":{"keep":99,"bogus":1}}}`)
	if code != 400 || errCode(out) != "invalid_config" {
		t.Fatalf("invalid apply = %d %v", code, out)
	}
	fields := out["error"].(map[string]any)["fields"].([]any)
	if len(fields) != 1 || fields[0].(map[string]any)["field"] != "agents.bogus" {
		t.Errorf("fields = %v", fields)
	}
	for _, body := range []string{`{"patch":{}}`, `{"base_revision":1}`} {
		if code, out := tb.verb(t, "config.apply", body); code != 400 || errCode(out) != "invalid_request" {
			t.Errorf("%s = %d %v", body, code, out)
		}
	}
	code, out = tb.verb(t, "config.get", ``)
	if code != 200 || out["mode"] != "normal" || out["config"].(map[string]any)["revision"] != float64(1) {
		t.Errorf("get = %d %v", code, out)
	}
	code, out = tb.verb(t, "config.revert", `{}`)
	if code != 200 || out["config"].(map[string]any)["frontends"].(map[string]any)["ide"].(map[string]any)["enabled"] != false {
		t.Errorf("revert = %d %v", code, out)
	}
	if got := tb.svc.lastNames(); !slices.Equal(got, []string{"panel", "ttyd"}) {
		t.Errorf("services after revert = %v", got)
	}
}

func TestRecoveryMode(t *testing.T) {
	tb := newTestBroker(t)
	writeCorrupt(t, tb.dir)
	tb.b.Load()
	tb.b.Reconcile()
	if got := tb.svc.lastNames(); !slices.Equal(got, []string{"panel"}) {
		t.Fatalf("recovery services = %v", got)
	}
	code, out := tb.call(t, "GET", "/status", "")
	if code != 200 || out["mode"] != "recovery" || !strings.Contains(out["config_error"].(string), "config.json") {
		t.Errorf("status = %v", out)
	}
	if code, out := tb.verb(t, "config.apply", `{"base_revision":0,"patch":{}}`); code != 409 || errCode(out) != "recovery" {
		t.Errorf("apply in recovery = %d %v", code, out)
	}
	if code, out := tb.verb(t, "config.export", `{}`); code != 409 || errCode(out) != "recovery" {
		t.Errorf("export in recovery = %d %v", code, out)
	}
	if code, out := tb.verb(t, "config.revert", `{}`); code != 409 || errCode(out) != "no_previous" {
		t.Errorf("revert without previous = %d %v", code, out)
	}
	code, out = tb.verb(t, "config.reset", `{}`)
	if code != 200 {
		t.Fatalf("reset = %d %v", code, out)
	}
	if got := tb.svc.lastNames(); !slices.Equal(got, []string{"panel", "ttyd"}) {
		t.Errorf("services after reset = %v", got)
	}
	if _, out := tb.call(t, "GET", "/status", ""); out["mode"] != "normal" {
		t.Errorf("mode after reset = %v", out["mode"])
	}
}

func TestServiceRestart(t *testing.T) {
	tb := newTestBroker(t)
	tb.b.Load()
	if code, out := tb.verb(t, "service.restart", `{"name":"code_server"}`); code != 404 || errCode(out) != "unknown_service" {
		t.Errorf("disabled service = %d %v", code, out)
	}
	if code, out := tb.verb(t, "service.restart", `{"name":"ttyd"}`); code != 200 {
		t.Errorf("ttyd = %d %v", code, out)
	}
	if !slices.Equal(tb.svc.restarts, []string{"ttyd"}) {
		t.Errorf("restarts = %v", tb.svc.restarts)
	}
}

func TestDesired(t *testing.T) {
	c := config.Defaults()
	c.Frontends.IDE.Enabled = true
	c.Runtime.TZ = "Europe/Rome"
	specs := Desired(c, ModeNormal)
	if len(specs) != 3 || specs[0].Name != "panel" || specs[1].Name != "ttyd" || specs[2].Name != "code_server" {
		t.Fatalf("specs = %+v", specs)
	}
	if specs[0].Soft["TZ"] != "Europe/Rome" || specs[0].Env["TZ"] != "" {
		t.Errorf("panel TZ must be soft: %+v", specs[0])
	}
	if specs[1].Env["TZ"] != "Europe/Rome" || specs[2].Env["TZ"] != "Europe/Rome" {
		t.Errorf("frontends restart on TZ: %+v", specs[1:])
	}
	if got := Desired(c, ModeRecovery); len(got) != 1 || got[0].Name != "panel" {
		t.Errorf("recovery = %+v", got)
	}
	if got := Desired(c, ModeSetup); len(got) != 1 || got[0].Name != "panel" {
		t.Errorf("setup = %+v", got)
	}
	c.Frontends.Terminal.Enabled = false
	if got := Desired(c, ModeNormal); len(got) != 2 || got[1].Name != "code_server" {
		t.Errorf("no terminal = %+v", got)
	}
}

func TestProblems(t *testing.T) {
	ok := StatusReport{Mode: ModeNormal, Services: []supervisor.Status{{Name: "panel", State: "running"}}}
	if p := Problems(ok); len(p) != 0 {
		t.Errorf("healthy: %v", p)
	}
	bad := StatusReport{Mode: ModeRecovery, ConfigError: "x: broken", Services: []supervisor.Status{
		{Name: "panel", State: "running"}, {Name: "ttyd", State: "failed"}}}
	if p := Problems(bad); !slices.Equal(p, []string{"recovery mode: x: broken", "ttyd is failed"}) {
		t.Errorf("problems = %v", p)
	}
	if p := Problems(StatusReport{Mode: ModeNormal}); !slices.Equal(p, []string{"no service has started yet"}) {
		t.Errorf("empty = %v", p)
	}
}

func TestClientOverSocket(t *testing.T) {
	dir, _ := os.MkdirTemp("", "brk")
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "b.sock")
	l, err := Listen(sock, -1)
	if err != nil {
		t.Fatal(err)
	}
	tb := newTestBroker(t)
	tb.b.Load()
	srv := &http.Server{Handler: tb.b.Handler(), ReadHeaderTimeout: time.Second}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	c := NewClient(sock)
	st, err := c.Status(context.Background())
	if err != nil || st.Mode != ModeNormal {
		t.Fatalf("Status = %+v %v", st, err)
	}
	code, raw, err := c.Verb(context.Background(), "config.get", nil)
	if err != nil || code != 200 || !strings.Contains(string(raw), `"revision":0`) {
		t.Fatalf("Verb = %d %s %v", code, raw, err)
	}
	if _, err := NewClient(filepath.Join(dir, "missing.sock")).Status(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "not reachable") {
		t.Errorf("missing socket: %v", err)
	}
}

func writeCorrupt(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
}
