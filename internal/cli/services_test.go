package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/berth-ai/berth/internal/runner"
)

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

// serve runs a long-lived berth command until stop is called.
func serve(t *testing.T, cfg Config, args ...string) (stderr *syncBuffer, stop func() int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	stderr = &syncBuffer{}
	done := make(chan int, 1)
	go func() { done <- Run(ctx, cfg, args, io.Discard, stderr) }()
	stopped := false
	stop = func() int {
		cancel()
		select {
		case code := <-done:
			stopped = true
			return code
		case <-time.After(10 * time.Second):
			t.Fatalf("berth %s did not stop; stderr:\n%s", args[0], stderr)
			return -1
		}
	}
	t.Cleanup(func() {
		if !stopped {
			stop()
		}
	})
	return stderr, stop
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "bcli") // unix socket paths are limited to ~104 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestServiceArgs(t *testing.T) {
	cfg := testConfig(t)
	for _, cmd := range []string{"panel", "health"} {
		if _, _, code := run(t, cfg, cmd, "extra"); code != 2 {
			t.Errorf("%s extra: exit %d", cmd, code)
		}
	}
}

func TestPanelRefusesRoot(t *testing.T) {
	cfg := testConfig(t)
	_, errOut, code := run(t, cfg, "panel")
	if code != 1 || !strings.Contains(errOut, "must not run as root") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

func TestPanelServes(t *testing.T) {
	cfg := testConfig(t)
	cfg.IsRoot = false
	cfg.PanelListen = "127.0.0.1:0"
	cfg.BrokerSocket = filepath.Join(shortTempDir(t), "missing.sock")

	stderr, stop := serve(t, cfg, "panel")
	listening := regexp.MustCompile(`listening on (127\.0\.0\.1:\d+)`)
	waitFor(t, "panel to listen", func() bool { return listening.MatchString(stderr.String()) })
	addr := listening.FindStringSubmatch(stderr.String())[1]

	resp, err := http.Get("http://" + addr + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if code := stop(); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
}

func TestDefaultConfigServices(t *testing.T) {
	env := map[string]string{"PANEL_PORT": "9000", "AGENTS_KEEP": "9"}
	cfg, err := defaultConfig(func(k string) string { return env[k] }, Build{}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LoginPath != "/var/lib/berth/secrets/panel.json" || cfg.PanelListen != "0.0.0.0:7690" || cfg.Keep != 3 ||
		cfg.BrokerSocket != "/run/berth/broker.sock" || cfg.ModuleRun != "/usr/local/bin/berth-module-run" ||
		cfg.ConfigDir != "/var/lib/berth" || cfg.MCPTimeout != 2*time.Minute {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestDefaultConfigLooksUpPanelUser(t *testing.T) {
	lookup := func(name string) (*runner.Identity, error) {
		if name == "berth-panel" {
			return nil, errors.New("unknown user berth-panel")
		}
		return &runner.Identity{UID: 1000, GID: 1000, Home: "/home/agent"}, nil
	}
	cfg, err := defaultConfig(func(string) string { return "" }, Build{}, true, lookup)
	if err != nil || cfg.Agent == nil || cfg.PanelErr == nil || !strings.Contains(cfg.PanelErr.Error(), "berth-panel") {
		t.Fatalf("cfg = %+v %v", cfg, err)
	}
}
