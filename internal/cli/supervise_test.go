package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/salgnt/berth/internal/agentstore"
	"github.com/salgnt/berth/internal/broker"
	"github.com/salgnt/berth/internal/runner"
)

// fakeModuleRun records starts and stops in $FAKE_DIR/events; superviseConfig
// replaces $FAKE_DIR with the test directory.
const fakeModuleRun = `#!/bin/sh
[ "$1" = --health ] && { [ -f "$FAKE_DIR/$2.ready" ]; exit; }
echo "start $1" >> "$FAKE_DIR/events"
env > "$FAKE_DIR/$1.env"
trap 'rm -f "$FAKE_DIR/$1.ready"; echo "stop $1" >> "$FAKE_DIR/events"; exit 0' TERM
touch "$FAKE_DIR/$1.ready" # healthy once the env file and the trap are in place
while :; do sleep 0.05; done
`

func superviseConfig(t *testing.T) (Config, string) {
	t.Helper()
	cfg := testConfig(t)
	dir := shortTempDir(t)
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// FAKE_DIR is baked in: services get an allowlisted environment.
	cfg.ModuleRun = write("berth-module-run", strings.ReplaceAll(fakeModuleRun, "$FAKE_DIR", dir))
	cfg.BerthPath = write("berth", "#!/bin/sh\necho \"$*\" >> \""+dir+"/berth.log\"\nexec sleep \"${FAKE_ENSURE_SLEEP:-0}\"\n")
	cfg.BrokerSocket = filepath.Join(dir, "broker.sock")
	cfg.ConfigDir = filepath.Join(dir, "config")
	cfg.Panel = &runner.Identity{UID: uint32(os.Getuid()), GID: uint32(os.Getgid())}
	cfg.PanelCredential = "me:pw"
	cfg.Environ = func() []string {
		return []string{"PATH=" + os.Getenv("PATH"), "FAKE_DIR=" + dir, "GITHUB_TOKEN=secret", "LANG=C.UTF-8"}
	}
	return cfg, dir
}

// supervise runs berth supervise until stop delivers sig.
func supervise(t *testing.T, cfg Config) (*syncBuffer, func(sig os.Signal) int) {
	t.Helper()
	ctx, cancel := context.WithCancelCause(context.Background())
	stderr := &syncBuffer{}
	done := make(chan int, 1)
	go func() { done <- Run(ctx, cfg, []string{"supervise"}, &syncBuffer{}, stderr) }()
	stopped := false
	stop := func(sig os.Signal) int {
		stopped = true
		cancel(SignalCause{Signal: sig})
		select {
		case code := <-done:
			return code
		case <-time.After(15 * time.Second):
			t.Fatalf("supervise did not stop; stderr:\n%s", stderr)
			return -1
		}
	}
	t.Cleanup(func() {
		if !stopped {
			stop(syscall.SIGTERM)
		}
	})
	return stderr, stop
}

func readEvents(dir string) []string {
	raw, _ := os.ReadFile(filepath.Join(dir, "events"))
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func waitServices(t *testing.T, c broker.Client, want ...string) broker.StatusReport {
	t.Helper()
	var st broker.StatusReport
	waitFor(t, "services "+strings.Join(want, ","), func() bool {
		var err error
		st, err = c.Status(context.Background())
		if err != nil || len(st.Services) != len(want) {
			return false
		}
		for i, s := range st.Services {
			if s.Name != want[i] || s.State != "running" {
				return false
			}
		}
		return true
	})
	return st
}

func TestSuperviseRequiresRootAndUsers(t *testing.T) {
	cfg, _ := superviseConfig(t)
	cfg.IsRoot = false
	if _, errOut, code := run(t, cfg, "supervise"); code != 4 || !strings.Contains(errOut, "must run as root") {
		t.Errorf("non-root: %d %s", code, errOut)
	}
	cfg, _ = superviseConfig(t)
	cfg.Panel, cfg.PanelErr = nil, os.ErrNotExist
	if out, _, code := run(t, cfg, "supervise", "--json"); code != 1 || !strings.Contains(out, "no_panel_user") {
		t.Errorf("no panel user: %d %s", code, out)
	}
	if _, _, code := run(t, cfg, "supervise", "extra"); code != 2 {
		t.Errorf("extra arg: %d", code)
	}
}

func TestSuperviseRunsReconcilesAndStops(t *testing.T) {
	cfg, dir := superviseConfig(t)
	stderr, stop := supervise(t, cfg)
	c := broker.NewClient(cfg.BrokerSocket)
	waitServices(t, c, "panel", "ttyd")
	if log, _ := os.ReadFile(filepath.Join(dir, "berth.log")); string(log) != "ensure\n" {
		t.Errorf("boot ran %q", log)
	}
	panelEnv, _ := os.ReadFile(filepath.Join(dir, "panel.env"))
	ttydEnv, _ := os.ReadFile(filepath.Join(dir, "ttyd.env"))
	for name, env := range map[string]string{"panel": string(panelEnv), "ttyd": string(ttydEnv)} {
		if strings.Contains(env, "GITHUB_TOKEN") || !strings.Contains(env, "LANG=C.UTF-8\n") || !strings.Contains(env, "TZ=UTC\n") {
			t.Errorf("%s env:\n%s", name, env)
		}
	}
	if !strings.Contains(string(panelEnv), "BERTH_PANEL_CREDENTIAL=me:pw\n") || strings.Contains(string(ttydEnv), "me:pw") {
		t.Errorf("panel credential must reach the panel only")
	}

	code, raw, err := c.Verb(context.Background(), "config.apply",
		map[string]any{"base_revision": 0, "patch": json.RawMessage(`{"frontends":{"ide":{"enabled":true}}}`)})
	if err != nil || code != 200 {
		t.Fatalf("apply = %d %s %v", code, raw, err)
	}
	waitServices(t, c, "panel", "ttyd", "code_server")
	if got := stop(syscall.SIGTERM); got != 143 {
		t.Fatalf("exit %d; stderr:\n%s", got, stderr)
	}
	ev := readEvents(dir)
	if !slices.Equal(ev[len(ev)-3:], []string{"stop code_server", "stop ttyd", "stop panel"}) {
		t.Errorf("events = %v", ev)
	}
	if _, err := os.Stat(cfg.BrokerSocket); !os.IsNotExist(err) {
		t.Errorf("socket left behind: %v", err)
	}
}

func TestSuperviseInterruptExits130(t *testing.T) {
	cfg, _ := superviseConfig(t)
	_, stop := supervise(t, cfg)
	waitServices(t, broker.NewClient(cfg.BrokerSocket), "panel", "ttyd")
	if got := stop(os.Interrupt); got != 130 {
		t.Errorf("exit %d", got)
	}
}

func TestSuperviseRecoveryMode(t *testing.T) {
	cfg, _ := superviseConfig(t)
	os.MkdirAll(cfg.ConfigDir, 0o755)
	os.WriteFile(filepath.Join(cfg.ConfigDir, "config.json"), []byte("{nope"), 0o600)
	stderr, _ := supervise(t, cfg)
	st := waitServices(t, broker.NewClient(cfg.BrokerSocket), "panel")
	if st.Mode != "recovery" || !strings.Contains(stderr.String(), "RECOVERY MODE") {
		t.Errorf("mode %s; stderr:\n%s", st.Mode, stderr)
	}
}

func TestSuperviseStopsDuringBoot(t *testing.T) {
	cfg, dir := superviseConfig(t)
	t.Setenv("FAKE_ENSURE_SLEEP", "30") // the fake berth inherits the test's environment
	_, stop := supervise(t, cfg)
	waitFor(t, "ensure to start", func() bool {
		log, _ := os.ReadFile(filepath.Join(dir, "berth.log"))
		return string(log) == "ensure\n"
	})
	start := time.Now()
	if got := stop(syscall.SIGTERM); got != 143 {
		t.Errorf("exit %d", got)
	}
	if took := time.Since(start); took > 12*time.Second {
		t.Errorf("stop took %s", took)
	}
	if _, err := os.Stat(filepath.Join(dir, "events")); !os.IsNotExist(err) {
		t.Errorf("services started during an interrupted boot")
	}
}

func TestSuperviseWarnsWhenEnsureFails(t *testing.T) {
	cfg, dir := superviseConfig(t)
	os.WriteFile(cfg.BerthPath, []byte("#!/bin/sh\necho \"$*\" >> \""+dir+"/berth.log\"\nexit 1\n"), 0o755)
	stderr, _ := supervise(t, cfg)
	waitServices(t, broker.NewClient(cfg.BrokerSocket), "panel", "ttyd")
	if !strings.Contains(stderr.String(), "NO AGENTS INSTALLED") {
		t.Errorf("stderr:\n%s", stderr)
	}
}

func TestServiceEnvKeepsOnlyAllowlist(t *testing.T) {
	got := serviceEnv([]string{"PATH=/bin", "GITHUB_TOKEN=x", "PANEL_CREDENTIAL=a:b", "LANG=C", "EDITOR=nvim", "PUID=1000"})
	if !slices.Equal(got, []string{"PATH=/bin", "LANG=C", "EDITOR=nvim"}) {
		t.Errorf("env = %v", got)
	}
}

// As root, reading a file agent owns would let agent point a symlink at a
// secret and read it back, so the broker's status never reads MiseConfig.
func TestAgentsReportSkipsUserTools(t *testing.T) {
	cfg := testConfig(t) // MiseConfig names a file with a tool in it
	a := &app{cfg: cfg, store: agentstore.Store{Root: cfg.AgentsDir}}
	rep, err := a.agentsReport()
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.UserTools) != 0 {
		t.Errorf("user tools read as root: %v", rep.UserTools)
	}
}
