// Package supervisor runs Berth's services as child processes. It starts
// each one through berth-module-run, restarts crashes with backoff, gives up
// on crash loops, applies a new desired set by touching only what changed,
// and stops everything in reverse start order.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"sync"
	"time"
)

// Service states.
const (
	StateStarting   = "starting"
	StateRunning    = "running"
	StateRestarting = "restarting"
	StateFailed     = "failed"
	StateStopped    = "stopped"
)

const logLines = 200

// ErrUnknownService is returned by Restart for a service that is not running.
var ErrUnknownService = errors.New("unknown service")

// Spec is one service the supervisor should run.
type Spec struct {
	Name string
	// Env holds the service's settings; a change restarts the service.
	Env map[string]string
	// Soft is added to the environment at start but never forces a restart.
	Soft map[string]string
}

// Status is a snapshot of one service.
type Status struct {
	Name      string     `json:"name"`
	State     string     `json:"state"`
	PID       int        `json:"pid"`
	StartedAt *time.Time `json:"started_at"`
	Restarts  int        `json:"restarts"`
	Log       []string   `json:"log"`
}

// Changes lists what a Reconcile did, by service name.
type Changes struct {
	Started   []string `json:"started"`
	Restarted []string `json:"restarted"`
	Stopped   []string `json:"stopped"`
}

// Backoff controls crash handling: the delay before a restart starts at
// Initial and doubles up to Max. Limit crashes within Window mark a service
// failed. A run that lasted at least Window resets the doubling.
type Backoff struct {
	Initial time.Duration
	Max     time.Duration
	Window  time.Duration
	Limit   int
}

func (b Backoff) delay(consecutive int) time.Duration {
	d := b.Initial
	for i := 1; i < consecutive && d < b.Max; i++ {
		d *= 2
	}
	return min(d, b.Max)
}

// Supervisor runs services. Create it with New.
type Supervisor struct {
	ModuleRun      string    // path of berth-module-run
	BaseEnv        []string  // environment every service starts with
	Log            io.Writer // container log
	Now            func() time.Time
	StopGrace      time.Duration
	Backoff        Backoff
	HealthInterval time.Duration
	HealthTimeout  time.Duration

	op       sync.Mutex // serializes Reconcile, Restart and Shutdown
	mu       sync.Mutex // guards the fields below and every service's state
	order    []string
	services map[string]*service
	closed   bool
}

type service struct {
	spec   Spec
	cancel context.CancelFunc
	done   chan struct{}
	logs   *ring

	// Guarded by Supervisor.mu.
	state    string
	pid      int
	started  time.Time
	restarts int
}

// New returns a supervisor with the production tunables.
func New(moduleRun string, baseEnv []string, log io.Writer) *Supervisor {
	if log == nil {
		log = io.Discard
	}
	return &Supervisor{
		ModuleRun:      moduleRun,
		BaseEnv:        baseEnv,
		Log:            log,
		Now:            time.Now,
		StopGrace:      10 * time.Second,
		Backoff:        Backoff{Initial: time.Second, Max: time.Minute, Window: 2 * time.Minute, Limit: 5},
		HealthInterval: time.Second,
		HealthTimeout:  2 * time.Second,
		services:       map[string]*service{},
	}
}

// Reconcile makes the running set match specs, in their order. Services
// missing from specs stop (in reverse start order), new ones start, and
// services whose Env changed or that have failed restart. A Soft change
// applies at the next start. It does nothing after Shutdown.
func (s *Supervisor) Reconcile(specs []Spec) Changes {
	s.op.Lock()
	defer s.op.Unlock()
	ch := Changes{Started: []string{}, Restarted: []string{}, Stopped: []string{}}
	s.mu.Lock()
	closed, names := s.closed, slices.Clone(s.order)
	s.mu.Unlock()
	if closed {
		return ch
	}
	want := map[string]bool{}
	for _, sp := range specs {
		want[sp.Name] = true
	}
	for _, name := range slices.Backward(names) {
		if !want[name] {
			s.halt(name)
			s.forget(name)
			ch.Stopped = append(ch.Stopped, name)
		}
	}
	for _, sp := range specs {
		s.mu.Lock()
		svc, ok := s.services[sp.Name]
		var changed bool
		if ok {
			changed = !maps.Equal(svc.spec.Env, sp.Env) || svc.state == StateFailed
			if !changed {
				svc.spec.Soft = sp.Soft
			}
		}
		s.mu.Unlock()
		switch {
		case !ok:
			s.launch(sp)
			ch.Started = append(ch.Started, sp.Name)
		case changed:
			s.halt(sp.Name)
			s.launch(sp)
			ch.Restarted = append(ch.Restarted, sp.Name)
		}
	}
	return ch
}

// Restart stops and starts one service, clearing failed.
func (s *Supervisor) Restart(name string) error {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	svc, ok := s.services[name]
	closed := s.closed
	s.mu.Unlock()
	if !ok || closed {
		return ErrUnknownService
	}
	s.halt(name)
	s.launch(svc.spec)
	return nil
}

// Status returns every service in start order.
func (s *Supervisor) Status() []Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Status, 0, len(s.order))
	for _, name := range s.order {
		svc := s.services[name]
		st := Status{Name: name, State: svc.state, PID: svc.pid, Restarts: svc.restarts, Log: svc.logs.lines()}
		if svc.pid != 0 {
			started := svc.started.UTC()
			st.StartedAt = &started
		}
		out = append(out, st)
	}
	return out
}

// Shutdown stops every service in reverse start order and refuses further
// changes.
func (s *Supervisor) Shutdown() {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	s.closed = true
	names := slices.Clone(s.order)
	s.mu.Unlock()
	for _, name := range slices.Backward(names) {
		s.halt(name)
	}
}

// launch starts sp's run loop, keeping the log and restart count of an
// earlier run of the same service.
func (s *Supervisor) launch(sp Spec) {
	ctx, cancel := context.WithCancel(context.Background())
	svc := &service{spec: sp, cancel: cancel, done: make(chan struct{}), logs: newRing(logLines), state: StateStarting}
	s.mu.Lock()
	if old, ok := s.services[sp.Name]; ok {
		svc.logs, svc.restarts = old.logs, old.restarts
	} else {
		s.order = append(s.order, sp.Name)
	}
	s.services[sp.Name] = svc
	s.mu.Unlock()
	s.logf("starting %s", sp.Name)
	go s.loop(ctx, svc)
}

// halt stops a service's run loop and waits for it.
func (s *Supervisor) halt(name string) {
	s.mu.Lock()
	svc := s.services[name]
	s.mu.Unlock()
	if svc == nil {
		return
	}
	svc.cancel()
	<-svc.done
}

func (s *Supervisor) forget(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.services, name)
	s.order = slices.DeleteFunc(s.order, func(n string) bool { return n == name })
}

// loop runs svc until ctx ends or it crashes too often.
func (s *Supervisor) loop(ctx context.Context, svc *service) {
	defer close(svc.done)
	name := svc.spec.Name
	var crashes []time.Time
	consecutive := 0
	for {
		s.setState(svc, StateStarting)
		began := s.Now()
		err := s.runOnce(ctx, svc)
		if ctx.Err() != nil {
			s.setState(svc, StateStopped)
			return
		}
		now := s.Now()
		s.logf("%s exited: %v", name, err)
		if now.Sub(began) >= s.Backoff.Window {
			consecutive = 0
		}
		consecutive++
		crashes = append(slices.DeleteFunc(crashes, func(t time.Time) bool {
			return now.Sub(t) > s.Backoff.Window
		}), now)
		if len(crashes) >= s.Backoff.Limit {
			s.setState(svc, StateFailed)
			s.logf("%s crashed %d times within %s; not restarting it until it is restarted or the config changes",
				name, len(crashes), s.Backoff.Window)
			return
		}
		delay := s.Backoff.delay(consecutive)
		s.mu.Lock()
		svc.state, svc.pid = StateRestarting, 0
		svc.restarts++
		s.mu.Unlock()
		s.logf("restarting %s in %s", name, delay)
		select {
		case <-ctx.Done():
			s.setState(svc, StateStopped)
			return
		case <-time.After(delay):
		}
	}
}

func (s *Supervisor) setState(svc *service, state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	svc.state = state
	if state != StateStarting {
		svc.pid = 0
	}
}

func (s *Supervisor) logf(format string, args ...any) {
	fmt.Fprintf(s.Log, "berth: "+format+"\n", args...)
}
