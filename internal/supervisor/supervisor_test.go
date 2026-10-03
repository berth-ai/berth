package supervisor

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeModuleRun stands in for berth-module-run. Each service behaves
// according to files in $FAKE_DIR: NAME.crash exits 3, NAME.clean exits 0,
// NAME.stubborn ignores TERM, NAME.port is printed by --health; otherwise
// --health passes once the service has written NAME.ready.
const fakeModuleRun = `#!/bin/sh
if [ "$1" = --health ]; then
  [ -f "$FAKE_DIR/$2.unhealthy" ] && exit 1
  [ -f "$FAKE_DIR/$2.port" ] && { cat "$FAKE_DIR/$2.port"; exit 0; }
  [ -f "$FAKE_DIR/$2.ready" ]
  exit
fi
name=$1
echo "start $name" >> "$FAKE_DIR/events"
env > "$FAKE_DIR/$name.env"
echo "hello from $name"
[ -f "$FAKE_DIR/$name.crash" ] && { echo "crashing" >&2; printf 'partial line'; exit 3; }
[ -f "$FAKE_DIR/$name.clean" ] && exit 0
if [ -f "$FAKE_DIR/$name.stubborn" ]; then
  trap '' TERM
else
  trap 'rm -f "$FAKE_DIR/$name.ready"; echo "stop $name" >> "$FAKE_DIR/events"; exit 0' TERM
fi
# Healthy only once the env file and the trap are in place.
touch "$FAKE_DIR/$name.ready"
while :; do sleep 0.05; done
`

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

type harness struct {
	t   *testing.T
	dir string
	sup *Supervisor
	log *syncBuffer
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	run := filepath.Join(dir, "berth-module-run")
	if err := os.WriteFile(run, []byte(fakeModuleRun), 0o755); err != nil {
		t.Fatal(err)
	}
	log := &syncBuffer{}
	sup := New(run, []string{"PATH=" + os.Getenv("PATH"), "FAKE_DIR=" + dir, "BASE=yes"}, log)
	sup.HealthInterval = 20 * time.Millisecond
	sup.StopGrace = 2 * time.Second
	sup.Backoff = Backoff{Initial: 10 * time.Millisecond, Max: 40 * time.Millisecond, Window: time.Minute, Limit: 3}
	t.Cleanup(sup.Shutdown)
	return &harness{t: t, dir: dir, sup: sup, log: log}
}

func (h *harness) touch(name, content string) {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(h.dir, name), []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) events() []string {
	raw, _ := os.ReadFile(filepath.Join(h.dir, "events"))
	return strings.Fields(strings.ReplaceAll(string(raw), " ", ","))
}

func (h *harness) status(name string) Status {
	for _, s := range h.sup.Status() {
		if s.Name == name {
			return s
		}
	}
	return Status{}
}

func (h *harness) waitState(name, state string) Status {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if s := h.status(name); s.State == state {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.t.Fatalf("%s never reached %s (now %+v)\nlog:\n%s", name, state, h.status(name), h.log)
	return Status{}
}

func spec(name string, env ...string) Spec {
	s := Spec{Name: name, Env: map[string]string{}, Soft: map[string]string{}}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		s.Env[k] = v
	}
	return s
}

func TestStartsAndReportsRunning(t *testing.T) {
	h := newHarness(t)
	sp := spec("panel", "BERTH_PANEL_X=1")
	sp.Soft["TZ"] = "Europe/Rome"
	ch := h.sup.Reconcile([]Spec{sp})
	if !slices.Equal(ch.Started, []string{"panel"}) || len(ch.Restarted)+len(ch.Stopped) != 0 {
		t.Errorf("changes = %+v", ch)
	}
	st := h.waitState("panel", StateRunning)
	if st.PID == 0 || st.StartedAt == nil || st.Restarts != 0 {
		t.Errorf("status = %+v", st)
	}
	env, _ := os.ReadFile(filepath.Join(h.dir, "panel.env"))
	for _, want := range []string{"BERTH_PANEL_X=1", "TZ=Europe/Rome", "BASE=yes"} {
		if !strings.Contains(string(env), want+"\n") {
			t.Errorf("env lacks %s:\n%s", want, env)
		}
	}
	if !slices.Contains(st.Log, "hello from panel") {
		t.Errorf("log = %q", st.Log)
	}
	if !strings.Contains(h.log.String(), "panel | hello from panel\n") {
		t.Errorf("container log = %q", h.log)
	}
}

func TestHealthWaitsForPort(t *testing.T) {
	h := newHarness(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close() // nothing listens yet
	h.touch("ttyd.port", strconv.Itoa(port))
	h.sup.Reconcile([]Spec{spec("ttyd")})
	time.Sleep(200 * time.Millisecond)
	if s := h.status("ttyd"); s.State != StateStarting {
		t.Fatalf("state = %s before the port answers", s.State)
	}
	l, err = net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	h.waitState("ttyd", StateRunning)
}

func TestCrashBackoffThenFailed(t *testing.T) {
	h := newHarness(t)
	h.touch("ttyd.crash", "")
	h.sup.Reconcile([]Spec{spec("ttyd")})
	st := h.waitState("ttyd", StateFailed)
	if st.Restarts != 2 || st.PID != 0 {
		t.Errorf("status = %+v", st)
	}
	if got := h.events(); len(got) != 3 {
		t.Errorf("events = %v, want 3 starts", got)
	}
	if !slices.Contains(st.Log, "crashing") || !slices.Contains(st.Log, "partial line") {
		t.Errorf("log = %q", st.Log)
	}
	if !strings.Contains(h.log.String(), "ttyd crashed 3 times within 1m0s") {
		t.Errorf("container log = %q", h.log)
	}
}

func TestCleanExitCountsAsCrash(t *testing.T) {
	h := newHarness(t)
	h.touch("ttyd.clean", "")
	h.sup.Reconcile([]Spec{spec("ttyd")})
	h.waitState("ttyd", StateFailed)
}

func TestRestartClearsFailed(t *testing.T) {
	h := newHarness(t)
	h.touch("ttyd.crash", "")
	h.sup.Reconcile([]Spec{spec("ttyd")})
	h.waitState("ttyd", StateFailed)
	os.Remove(filepath.Join(h.dir, "ttyd.crash"))
	if err := h.sup.Restart("ttyd"); err != nil {
		t.Fatal(err)
	}
	h.waitState("ttyd", StateRunning)
	if err := h.sup.Restart("nope"); err != ErrUnknownService {
		t.Errorf("Restart(nope) = %v", err)
	}
}

func TestBackoffDelay(t *testing.T) {
	b := Backoff{Initial: time.Second, Max: time.Minute}
	for n, want := range map[int]time.Duration{1: time.Second, 2: 2 * time.Second, 3: 4 * time.Second, 7: time.Minute, 500: time.Minute} {
		if got := b.delay(n); got != want {
			t.Errorf("delay(%d) = %s, want %s", n, got, want)
		}
	}
}

func TestLogRingKeepsTail(t *testing.T) {
	r := newRing(3)
	for _, l := range []string{"a", "b", "c", "d"} {
		r.add(l)
	}
	if got := r.lines(); !slices.Equal(got, []string{"b", "c", "d"}) {
		t.Errorf("lines = %v", got)
	}
	if got := newRing(3).lines(); got == nil {
		t.Error("empty ring must return an empty, non-nil list")
	}
}
