package supervisor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const maxLineBytes = 16 << 10

// runOnce starts svc and waits until it exits or ctx ends. Any exit is
// reported as an error, since a service is meant to run until stopped.
func (s *Supervisor) runOnce(ctx context.Context, svc *service) error {
	cmd := exec.Command(s.ModuleRun, svc.spec.Name)
	s.mu.Lock()
	cmd.Env = s.env(svc.spec)
	s.mu.Unlock()
	out := &lineWriter{name: svc.spec.Name, ring: svc.logs, log: s.Log}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// A daemon that keeps our pipe open (tmux's server, say) must not keep
	// Wait from returning once the service itself has exited.
	cmd.WaitDelay = time.Second
	if err := cmd.Start(); err != nil {
		return err
	}
	defer out.flush()
	pid := cmd.Process.Pid
	s.mu.Lock()
	svc.pid, svc.started = pid, s.Now()
	s.mu.Unlock()

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	probeCtx, stopProbe := context.WithCancel(ctx)
	defer stopProbe()
	go s.probe(probeCtx, svc, pid)

	select {
	case err := <-exited:
		if err == nil {
			return errors.New("exited with status 0")
		}
		return err
	case <-ctx.Done():
		s.terminate(svc.spec.Name, pid, exited)
		return nil
	}
}

// env is BaseEnv plus Soft plus Env; later entries win. Caller holds s.mu.
func (s *Supervisor) env(sp Spec) []string {
	env := slices.Clone(s.BaseEnv)
	for _, m := range []map[string]string{sp.Soft, sp.Env} {
		for _, k := range slices.Sorted(maps.Keys(m)) {
			env = append(env, k+"="+m[k])
		}
	}
	return env
}

// terminate sends TERM to the service's process group, then KILL after
// StopGrace.
func (s *Supervisor) terminate(name string, pid int, exited <-chan error) {
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	select {
	case <-exited:
	case <-time.After(s.StopGrace):
		s.logf("%s did not stop within %s; killing it", name, s.StopGrace)
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		<-exited
	}
}

// probe marks svc running once its health check passes.
func (s *Supervisor) probe(ctx context.Context, svc *service, pid int) {
	t := time.NewTicker(s.HealthInterval)
	defer t.Stop()
	for {
		if s.healthy(ctx, svc.spec.Name) {
			s.mu.Lock()
			if svc.pid == pid && svc.state == StateStarting {
				svc.state = StateRunning
			}
			s.mu.Unlock()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Supervisor) healthy(ctx context.Context, name string) bool {
	ctx, cancel := context.WithTimeout(ctx, s.HealthTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.ModuleRun, "--health", name)
	cmd.Env = s.BaseEnv
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	port := strings.TrimSpace(string(out))
	if port == "" {
		return true
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return false
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// lineWriter splits a service's output into lines for its ring and the
// container log, where each line is prefixed with the service name.
type lineWriter struct {
	mu   sync.Mutex
	name string
	ring *ring
	log  io.Writer
	buf  []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		w.emit(w.buf[:i])
		w.buf = w.buf[i+1:]
	}
	if len(w.buf) > maxLineBytes {
		w.emit(w.buf)
		w.buf = nil
	}
	return len(p), nil
}

func (w *lineWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) > 0 {
		w.emit(w.buf)
		w.buf = nil
	}
}

func (w *lineWriter) emit(b []byte) {
	line := strings.TrimSuffix(string(b), "\r")
	w.ring.add(line)
	fmt.Fprintf(w.log, "%s | %s\n", w.name, line)
}

// ring keeps the last max lines.
type ring struct {
	mu    sync.Mutex
	max   int
	items []string
}

func newRing(max int) *ring { return &ring{max: max} }

func (r *ring) add(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = append(r.items, line)
	if over := len(r.items) - r.max; over > 0 {
		r.items = slices.Clone(r.items[over:])
	}
}

func (r *ring) lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.items...)
}
