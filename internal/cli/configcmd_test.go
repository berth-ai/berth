package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/berth-ai/berth/internal/broker"
	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/login"
	"github.com/berth-ai/berth/internal/supervisor"
)

type staticServices struct {
	mu     sync.Mutex
	status []supervisor.Status
}

func (s *staticServices) Reconcile([]supervisor.Spec) supervisor.Changes {
	return supervisor.Changes{Started: []string{"code_server"}, Restarted: []string{}, Stopped: []string{}}
}
func (s *staticServices) Restart(string) error { return nil }
func (s *staticServices) Status() []supervisor.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// brokerConfig serves a real broker with fake services on a temp socket.
func brokerConfig(t *testing.T, svc broker.Services) Config {
	t.Helper()
	cfg := testConfig(t)
	dir := shortTempDir(t)
	cfg.BrokerSocket = filepath.Join(dir, "b.sock")
	ctx, cancel := context.WithCancel(context.Background())
	jobs := broker.NewJobs(ctx)
	logins := login.Store{Path: filepath.Join(dir, "cfg", "secrets", "panel.json")}
	b := broker.New(&config.Store{Dir: filepath.Join(dir, "cfg")}, logins, svc, jobs, nil)
	if err := logins.Set("me", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	b.PeerUID = func(*http.Request) (int, bool) { return 0, true }
	b.Load()
	l, err := broker.Listen(cfg.BrokerSocket, -1)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: b.Handler(), ReadHeaderTimeout: time.Second}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close(); cancel(); jobs.Wait() })
	return cfg
}

func running(names ...string) *staticServices {
	s := &staticServices{}
	for _, n := range names {
		s.status = append(s.status, supervisor.Status{Name: n, State: "running", Log: []string{}})
	}
	return s
}

func TestConfigGetAndSet(t *testing.T) {
	cfg := brokerConfig(t, running("panel", "ttyd"))
	out, errOut, code := run(t, cfg, "config", "get", "--json")
	if code != 0 {
		t.Fatalf("get: %d %s", code, errOut)
	}
	golden(t, "config_get", out)

	out, errOut, code = run(t, cfg, "config", "set", "frontends.ide.enabled=true", "runtime.tz=Europe/Rome",
		`agents.disabled=["opencode-ai"]`, "--json")
	if code != 0 {
		t.Fatalf("set: %d %s %s", code, out, errOut)
	}
	golden(t, "config_set", out)

	_, _, code = run(t, cfg, "config", "set", "agents.keep=99")
	if code != 1 {
		t.Fatalf("invalid set: %d", code)
	}
	_, errOut, _ = run(t, cfg, "config", "set", "agents.keep=99")
	if !strings.Contains(errOut, "agents.keep: must be between 0 and 50") {
		t.Errorf("stderr = %q", errOut)
	}
	out, _, _ = run(t, cfg, "config", "set", "agents.keep=4")
	if out != "applied revision 2; started code_server\n" {
		t.Errorf("human set = %q", out)
	}
}

func TestConfigUsage(t *testing.T) {
	cfg := brokerConfig(t, running("panel"))
	for _, args := range [][]string{
		{"config"}, {"config", "frob"}, {"config", "set"}, {"config", "set", "novalue"},
		{"config", "get", "extra"}, {"config", "import"},
	} {
		if _, _, code := run(t, cfg, args...); code != 2 {
			t.Errorf("%v: exit %d", args, code)
		}
	}
	cfg.IsRoot = false
	if _, _, code := run(t, cfg, "config", "get"); code != 4 {
		t.Errorf("non-root: exit %d", code)
	}
}

func TestConfigExportImportRoundTrip(t *testing.T) {
	cfg := brokerConfig(t, running("panel"))
	run(t, cfg, "config", "set", "agents.keep=7")
	exported, _, code := run(t, cfg, "config", "export")
	if code != 0 {
		t.Fatalf("export: %d", code)
	}
	run(t, cfg, "config", "reset")
	file := filepath.Join(t.TempDir(), "berth.json")
	os.WriteFile(file, []byte(exported), 0o600)
	if _, errOut, code := run(t, cfg, "config", "import", file); code != 0 {
		t.Fatalf("import: %d %s", code, errOut)
	}
	out, _, _ := run(t, cfg, "config", "get", "--json")
	var got struct{ Config config.Config }
	json.Unmarshal([]byte(out), &got)
	if got.Config.Agents.Keep != 7 || got.Config.Revision != 3 {
		t.Errorf("after import: %+v", got.Config)
	}
}

func TestSetPatch(t *testing.T) {
	got, err := setPatch([]string{"agents.keep=5", "agents.ref=v1", "runtime.tz=Europe/Rome", "frontends.ide.enabled=true", "agents.ref=null"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"agents":{"keep":5,"ref":null},"frontends":{"ide":{"enabled":true}},"runtime":{"tz":"Europe/Rome"}}`
	if string(got) != want {
		t.Errorf("patch = %s", got)
	}
	for _, bad := range []string{"=x", "novalue", "a..b=1", ".a=1"} {
		if _, err := setPatch([]string{bad}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestImportPatch(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(body), 0o600)
		return p
	}
	got, err := importPatch(write("a.json", `{"config":{"schema":1,"revision":9,"agents":{"keep":2}},"schema":1}`))
	if err != nil || string(got) != `{"agents":{"keep":2}}` {
		t.Errorf("wrapped = %s %v", got, err)
	}
	got, err = importPatch(write("b.json", `{"schema":1,"runtime":{"tz":"UTC"}}`))
	if err != nil || string(got) != `{"runtime":{"tz":"UTC"}}` {
		t.Errorf("bare = %s %v", got, err)
	}
	got, err = importPatch(write("d.json", `{"schema":2,"mcp":{"servers":{}}}`))
	if err != nil || string(got) != `{"mcp":{"servers":{}}}` {
		t.Errorf("schema 2 = %s %v", got, err)
	}
	got, err = importPatch(write("f.json", `{"schema":5,"skills":{"pdf":{"repo":"https://github.com/anthropics/skills","path":"skills/pdf","ref":"main","commit":"0123456789abcdef0123456789abcdef01234567","description":"PDF tools","agents":null,"enabled":true}}}`))
	if err != nil || !strings.Contains(string(got), `"sources":{"anthropics-skills":{`) || strings.Contains(string(got), `"skills":{"pdf"`) {
		t.Errorf("schema 5 skills = %s %v", got, err)
	}
	if _, err := importPatch(write("e.json", `{"schema":7}`)); err == nil || !strings.Contains(err.Error(), "config schema 7") {
		t.Errorf("schema 7: %v", err)
	}
	for _, body := range []string{`{"schema":7}`, `[]`, `{"agents":{}}`, `nope`} {
		if _, err := importPatch(write("c.json", body)); err == nil {
			t.Errorf("%s accepted", body)
		}
	}
}

func TestHealth(t *testing.T) {
	svc := running("panel", "ttyd")
	cfg := brokerConfig(t, svc)
	out, _, code := run(t, cfg, "health")
	if code != 0 || out != "healthy\n" {
		t.Fatalf("healthy: %d %q", code, out)
	}
	svc.mu.Lock()
	svc.status[1].State = "failed"
	svc.mu.Unlock()
	out, _, code = run(t, cfg, "health", "--json")
	if code != 1 {
		t.Fatalf("unhealthy: %d", code)
	}
	golden(t, "health_unhealthy", out)
	if out, _, _ := run(t, cfg, "health"); out != "unhealthy: ttyd is failed\n" {
		t.Errorf("human = %q", out)
	}
	cfg.BrokerSocket = filepath.Join(shortTempDir(t), "missing.sock")
	if out, _, code := run(t, cfg, "health"); code != 1 || !strings.Contains(out, "not reachable") {
		t.Errorf("no supervisor: %d %q", code, out)
	}
}

func TestConfigResetLogin(t *testing.T) {
	cfg := brokerConfig(t, running("panel"))
	out, errOut, code := run(t, cfg, "config", "reset-login", "--json")
	if code != 0 || !strings.Contains(out, `"setup_url": "http://127.0.0.1:7690/setup#token=`) {
		t.Fatalf("reset-login: %d %s %s", code, out, errOut)
	}
	out, _, _ = run(t, cfg, "config", "reset-login")
	if !strings.HasPrefix(out, "login removed; finish setup at http://127.0.0.1:7690/setup#token=") {
		t.Errorf("human = %q", out)
	}
}
