package supervisor

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestReconcileTouchesOnlyWhatChanged(t *testing.T) {
	h := newHarness(t)
	h.sup.Reconcile([]Spec{spec("panel"), spec("ttyd", "TZ=UTC"), spec("code_server", "TZ=UTC")})
	panel := h.waitState("panel", StateRunning)
	h.waitState("ttyd", StateRunning)
	h.waitState("code_server", StateRunning)

	ch := h.sup.Reconcile([]Spec{spec("panel"), spec("ttyd", "TZ=Europe/Rome")})
	if !slices.Equal(ch.Stopped, []string{"code_server"}) || !slices.Equal(ch.Restarted, []string{"ttyd"}) || len(ch.Started) != 0 {
		t.Errorf("changes = %+v", ch)
	}
	if got := h.waitState("panel", StateRunning); got.PID != panel.PID {
		t.Errorf("panel restarted: pid %d -> %d", panel.PID, got.PID)
	}
	ttyd := h.waitState("ttyd", StateRunning)
	if ttyd.Restarts != 0 {
		t.Errorf("a reconcile restart is not a crash: %+v", ttyd)
	}
	names := []string{}
	for _, s := range h.sup.Status() {
		names = append(names, s.Name)
	}
	if !slices.Equal(names, []string{"panel", "ttyd"}) {
		t.Errorf("services = %v", names)
	}
	env, _ := os.ReadFile(filepath.Join(h.dir, "ttyd.env"))
	if !slices.Contains(splitLines(env), "TZ=Europe/Rome") {
		t.Errorf("ttyd env:\n%s", env)
	}
}

func TestSoftChangeDoesNotRestart(t *testing.T) {
	h := newHarness(t)
	sp := spec("panel")
	sp.Soft["TZ"] = "UTC"
	h.sup.Reconcile([]Spec{sp})
	before := h.waitState("panel", StateRunning)
	sp2 := spec("panel")
	sp2.Soft["TZ"] = "Europe/Rome"
	ch := h.sup.Reconcile([]Spec{sp2})
	if len(ch.Started)+len(ch.Restarted)+len(ch.Stopped) != 0 {
		t.Errorf("changes = %+v", ch)
	}
	time.Sleep(100 * time.Millisecond)
	if after := h.status("panel"); after.PID != before.PID {
		t.Fatalf("panel restarted")
	}
	h.sup.Restart("panel")
	h.waitState("panel", StateRunning)
	env, _ := os.ReadFile(filepath.Join(h.dir, "panel.env"))
	if !slices.Contains(splitLines(env), "TZ=Europe/Rome") {
		t.Errorf("new soft value not used at next start:\n%s", env)
	}
}

func TestReconcileRestartsFailed(t *testing.T) {
	h := newHarness(t)
	h.touch("ttyd.crash", "")
	h.sup.Reconcile([]Spec{spec("ttyd")})
	h.waitState("ttyd", StateFailed)
	os.Remove(filepath.Join(h.dir, "ttyd.crash"))
	ch := h.sup.Reconcile([]Spec{spec("ttyd")})
	if !slices.Equal(ch.Restarted, []string{"ttyd"}) {
		t.Errorf("changes = %+v", ch)
	}
	h.waitState("ttyd", StateRunning)
}

func TestShutdownStopsInReverseOrder(t *testing.T) {
	h := newHarness(t)
	h.sup.Reconcile([]Spec{spec("panel"), spec("ttyd"), spec("code_server")})
	for _, n := range []string{"panel", "ttyd", "code_server"} {
		h.waitState(n, StateRunning)
	}
	h.sup.Shutdown()
	want := []string{"start,panel", "start,ttyd", "start,code_server", "stop,code_server", "stop,ttyd", "stop,panel"}
	got := h.events()
	// Starts may interleave; the stops must be exactly reversed.
	if !slices.Equal(got[3:], want[3:]) {
		t.Errorf("events = %v", got)
	}
	for _, s := range h.sup.Status() {
		if s.State != StateStopped || s.PID != 0 {
			t.Errorf("%s after shutdown: %+v", s.Name, s)
		}
	}
	if ch := h.sup.Reconcile([]Spec{spec("panel")}); len(ch.Started) != 0 {
		t.Errorf("reconcile after shutdown started %v", ch.Started)
	}
}

func TestStubbornServiceIsKilled(t *testing.T) {
	h := newHarness(t)
	h.sup.StopGrace = 200 * time.Millisecond
	h.touch("ttyd.stubborn", "")
	h.sup.Reconcile([]Spec{spec("ttyd")})
	h.waitState("ttyd", StateRunning)
	start := time.Now()
	h.sup.Shutdown()
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("shutdown took %s", took)
	}
	if h.status("ttyd").State != StateStopped {
		t.Errorf("state = %s", h.status("ttyd").State)
	}
}

func splitLines(b []byte) []string { return strings.Split(string(b), "\n") }
