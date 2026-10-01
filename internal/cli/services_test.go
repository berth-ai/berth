package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/salgnt/berth/internal/panel"
	"github.com/salgnt/berth/internal/runner"
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
	for _, cmd := range []string{"broker", "panel"} {
		if _, _, code := run(t, cfg, cmd, "extra"); code != 2 {
			t.Errorf("%s extra: exit %d", cmd, code)
		}
	}
}

func TestBrokerRequiresRoot(t *testing.T) {
	cfg := testConfig(t)
	cfg.IsRoot = false
	_, errOut, code := run(t, cfg, "broker")
	if code != 4 || !strings.Contains(errOut, "must run as root") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

func TestBrokerServesSocket(t *testing.T) {
	dir := shortTempDir(t)
	fake := filepath.Join(dir, "berth")
	os.WriteFile(fake, []byte("#!/bin/sh\nprintf '{\"argv\":\"%s\",\"schema\":1}' \"$*\"\n"), 0o755)
	cfg := testConfig(t)
	cfg.Agent = &runner.Identity{UID: uint32(os.Getuid()), GID: uint32(os.Getgid())}
	cfg.BrokerSocket = filepath.Join(dir, "broker.sock")
	cfg.BerthPath = fake

	stderr, stop := serve(t, cfg, "broker")
	waitFor(t, "broker socket", func() bool { _, err := os.Stat(cfg.BrokerSocket); return err == nil })
	fi, _ := os.Stat(cfg.BrokerSocket)
	if fi.Mode().Perm() != 0o660 {
		t.Errorf("socket mode %v", fi.Mode())
	}
	resp, err := panel.UnixClient(cfg.BrokerSocket).Get(panel.BrokerURL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"argv":"status --json"`) {
		t.Fatalf("status = %d %s", resp.StatusCode, body)
	}
	if code := stop(); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if _, err := os.Stat(cfg.BrokerSocket); !os.IsNotExist(err) {
		t.Errorf("socket left behind: %v", err)
	}
}

func TestPanelRefusesRoot(t *testing.T) {
	cfg := testConfig(t)
	cfg.PanelCredential = "me:pw"
	_, errOut, code := run(t, cfg, "panel")
	if code != 1 || !strings.Contains(errOut, "must not run as root") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

func TestPanelConfigErrors(t *testing.T) {
	for name, tc := range map[string]struct{ cred, listen, want string }{
		"no credential":  {"", "127.0.0.1:0", "PANEL_CREDENTIAL"},
		"bad credential": {"nopassword", "127.0.0.1:0", "PANEL_CREDENTIAL"},
		"bad port":       {"me:pw", "0.0.0.0:http", "PANEL_PORT"},
		"port too large": {"me:pw", "0.0.0.0:70000", "PANEL_PORT"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := testConfig(t)
			cfg.IsRoot = false
			cfg.PanelCredential, cfg.PanelListen = tc.cred, tc.listen
			_, errOut, code := run(t, cfg, "panel")
			if code != 1 || !strings.Contains(errOut, tc.want) {
				t.Fatalf("exit %d: %s", code, errOut)
			}
		})
	}
}

func TestPanelServes(t *testing.T) {
	cfg := testConfig(t)
	cfg.IsRoot = false
	cfg.PanelCredential = "me:pw"
	cfg.PanelListen = "127.0.0.1:0"
	cfg.BrokerSocket = filepath.Join(shortTempDir(t), "missing.sock")

	stderr, stop := serve(t, cfg, "panel")
	listening := regexp.MustCompile(`listening on (127\.0\.0\.1:\d+)`)
	waitFor(t, "panel to listen", func() bool { return listening.MatchString(stderr.String()) })
	addr := listening.FindStringSubmatch(stderr.String())[1]

	req, _ := http.NewRequest("GET", "http://"+addr+"/api/status", nil)
	req.SetBasicAuth("me", "pw")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway || !strings.Contains(string(body), "broker_unavailable") {
		t.Fatalf("status = %d %s", resp.StatusCode, body)
	}
	if code := stop(); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
}

func TestDefaultConfigServices(t *testing.T) {
	env := map[string]string{"PANEL_CREDENTIAL": "me:pw", "PANEL_PORT": "9000"}
	cfg, err := defaultConfig(func(k string) string { return env[k] }, Build{}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PanelCredential != "me:pw" || cfg.PanelListen != "0.0.0.0:9000" ||
		cfg.BrokerSocket != "/run/berth/broker.sock" || cfg.BerthPath != "/usr/local/bin/berth" {
		t.Errorf("cfg = %+v", cfg)
	}
	cfg, _ = defaultConfig(func(string) string { return "" }, Build{}, false, nil)
	if cfg.PanelListen != "0.0.0.0:7690" {
		t.Errorf("default listen = %q", cfg.PanelListen)
	}
}
