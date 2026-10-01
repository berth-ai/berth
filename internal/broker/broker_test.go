package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRequestArgs(t *testing.T) {
	tests := []struct {
		body string
		want []string
	}{
		{`{"action":"update"}`, []string{"update", "--json"}},
		{`{"action":"update","ref":"v1.2.3"}`, []string{"update", "--json", "--ref", "v1.2.3"}},
		{`{"action":"update","ref":"feature/x_y-1"}`, []string{"update", "--json", "--ref", "feature/x_y-1"}},
		{`{"action":"update","image":true}`, []string{"update", "--json", "--image"}},
		{`{"action":"rollback"}`, []string{"rollback", "--json"}},
		{`{"action":"rollback","hash":"0123456789ab"}`, []string{"rollback", "--json", "0123456789ab"}},
	}
	for _, tt := range tests {
		t.Run(tt.body, func(t *testing.T) {
			req, err := ParseRequest(strings.NewReader(tt.body))
			if err != nil {
				t.Fatalf("ParseRequest: %v", err)
			}
			got, err := req.Args()
			if err != nil {
				t.Fatalf("Args: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Args = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRequestRejects(t *testing.T) {
	for _, body := range []string{
		``,
		`[]`,
		`{"action":"gc"}`,
		`{"action":"status"}`,
		`{"action":"ensure"}`,
		`{"action":"update","lock_dir":"/tmp/x"}`,
		`{"action":"update","ref":"main","image":true}`,
		`{"action":"update","ref":"-x"}`,
		`{"action":"update","ref":"--lock-dir=/tmp"}`,
		`{"action":"update","ref":"a/../b"}`,
		`{"action":"update","ref":"a b"}`,
		`{"action":"update","ref":"` + strings.Repeat("a", 201) + `"}`,
		`{"action":"update","hash":"0123456789ab"}`,
		`{"action":"rollback","hash":"0123"}`,
		`{"action":"rollback","hash":"0123456789AB"}`,
		`{"action":"rollback","hash":"../../etc/xx"}`,
		`{"action":"rollback","ref":"main"}`,
		`{"action":"rollback","image":true}`,
		`{"action":"update"} {"action":"rollback"}`,
		`{"action":"update","x":` + strings.Repeat(" ", maxRequestBytes) + `1}`,
	} {
		t.Run(body[:min(len(body), 40)], func(t *testing.T) {
			req, err := ParseRequest(strings.NewReader(body))
			if err == nil {
				_, err = req.Args()
			}
			if err == nil {
				t.Errorf("accepted %q", body)
			}
		})
	}
}

// fakeExec scripts the berth binary.
type fakeExec struct {
	mu      sync.Mutex
	calls   [][]string
	release chan struct{} // when non-nil, jobs block until it is closed
	stdout  string
	stderr  string
	exit    int
	err     error
}

func (f *fakeExec) run(ctx context.Context, args []string, stdout, stderr io.Writer) (int, error) {
	f.mu.Lock()
	f.calls = append(f.calls, args)
	f.mu.Unlock()
	io.WriteString(stderr, f.stderr)
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return -1, nil
		}
	}
	io.WriteString(stdout, f.stdout)
	return f.exit, f.err
}

func (f *fakeExec) lastCall() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return nil
	}
	return f.calls[len(f.calls)-1]
}

func newTestBroker(t *testing.T, f *fakeExec) (*Broker, *httptest.Server) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	b := New(ctx, f.run)
	b.Now = func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }
	srv := httptest.NewServer(b.Handler())
	t.Cleanup(func() {
		srv.Close()
		cancel()
		b.Wait()
	})
	return b, srv
}

func do(t *testing.T, srv *httptest.Server, method, path, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Do(req)
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

func waitState(t *testing.T, srv *httptest.Server, want string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, out := do(t, srv, "GET", "/jobs/current", "")
		if job, ok := out["job"].(map[string]any); ok && job["state"] == want {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job never reached %s", want)
	return nil
}

func TestStatusPassesThroughJSON(t *testing.T) {
	f := &fakeExec{stdout: `{"current":null,"schema":1}`}
	_, srv := newTestBroker(t, f)
	code, out := do(t, srv, "GET", "/status", "")
	if code != 200 || out["schema"] != float64(1) {
		t.Fatalf("status = %d %v", code, out)
	}
	if got := f.lastCall(); !slices.Equal(got, []string{"status", "--json"}) {
		t.Errorf("args = %q", got)
	}
	do(t, srv, "GET", "/status?check=1", "")
	if got := f.lastCall(); !slices.Equal(got, []string{"status", "--json", "--check"}) {
		t.Errorf("args = %q", got)
	}
}

func TestStatusFailureIsBadGateway(t *testing.T) {
	f := &fakeExec{stdout: `{"error":{"code":"fetch_failed","step":"resolve","message":"x"},"schema":1}`, exit: 1}
	_, srv := newTestBroker(t, f)
	code, out := do(t, srv, "GET", "/status?check=1", "")
	if code != http.StatusBadGateway || out["error"].(map[string]any)["code"] != "fetch_failed" {
		t.Fatalf("status = %d %v", code, out)
	}
}

func TestStatusInvalidJSON(t *testing.T) {
	f := &fakeExec{stdout: "not json"}
	_, srv := newTestBroker(t, f)
	code, out := do(t, srv, "GET", "/status", "")
	if code != http.StatusBadGateway || out["error"].(map[string]any)["code"] != "bad_output" {
		t.Fatalf("status = %d %v", code, out)
	}
}

func TestNoJobYet(t *testing.T) {
	_, srv := newTestBroker(t, &fakeExec{})
	code, out := do(t, srv, "GET", "/jobs/current", "")
	if code != 200 || out["job"] != nil {
		t.Fatalf("got %d %v", code, out)
	}
}

func TestJobLifecycle(t *testing.T) {
	f := &fakeExec{release: make(chan struct{}), stderr: "berth: installing\n", stdout: `{"changed":true,"schema":1}`}
	_, srv := newTestBroker(t, f)

	code, out := do(t, srv, "POST", "/jobs", `{"action":"update","ref":"v2"}`)
	if code != http.StatusAccepted {
		t.Fatalf("POST = %d %v", code, out)
	}
	job := out["job"].(map[string]any)
	if job["state"] != "running" || job["action"] != "update" || job["id"] != float64(1) {
		t.Errorf("job = %v", job)
	}

	code, out = do(t, srv, "POST", "/jobs", `{"action":"rollback"}`)
	if code != http.StatusConflict || out["error"].(map[string]any)["code"] != "job_running" {
		t.Fatalf("second POST = %d %v", code, out)
	}

	running := waitState(t, srv, "running")
	if running["log"] != "berth: installing\n" {
		t.Errorf("live log = %q", running["log"])
	}
	close(f.release)
	done := waitState(t, srv, "succeeded")
	if done["exit_code"] != float64(0) || done["finished_at"] == nil {
		t.Errorf("job = %v", done)
	}
	if done["result"].(map[string]any)["changed"] != true {
		t.Errorf("result = %v", done["result"])
	}
	if got := f.lastCall(); !slices.Equal(got, []string{"update", "--json", "--ref", "v2"}) {
		t.Errorf("args = %q", got)
	}

	f.release = nil
	f.exit, f.stdout = 3, `{"error":{"code":"lock_busy","step":"lock","message":"busy"},"schema":1}`
	if code, out := do(t, srv, "POST", "/jobs", `{"action":"rollback"}`); code != http.StatusAccepted {
		t.Fatalf("third POST = %d %v", code, out)
	}
	failed := waitState(t, srv, "failed")
	if failed["exit_code"] != float64(3) || failed["id"] != float64(2) {
		t.Errorf("job = %v", failed)
	}
}

func TestJobExecError(t *testing.T) {
	f := &fakeExec{exit: -1, err: fmt.Errorf("no such file")}
	_, srv := newTestBroker(t, f)
	do(t, srv, "POST", "/jobs", `{"action":"update"}`)
	job := waitState(t, srv, "failed")
	errObj := job["result"].(map[string]any)["error"].(map[string]any)
	if errObj["code"] != "exec_failed" || !strings.Contains(errObj["message"].(string), "no such file") {
		t.Errorf("result = %v", job["result"])
	}
}

func TestBadJobRequest(t *testing.T) {
	_, srv := newTestBroker(t, &fakeExec{})
	code, out := do(t, srv, "POST", "/jobs", `{"action":"update","lock_dir":"/"}`)
	if code != http.StatusBadRequest || out["error"].(map[string]any)["code"] != "invalid_request" {
		t.Fatalf("got %d %v", code, out)
	}
}

func TestJobSurvivesClientAndStopsWithBroker(t *testing.T) {
	f := &fakeExec{release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	b := New(ctx, f.run)
	if _, err := b.Start(Request{Action: "update"}); err != nil {
		t.Fatal(err)
	}
	if got := b.Current().State; got != StateRunning {
		t.Fatalf("state = %s", got)
	}
	cancel()
	b.Wait()
	if got := b.Current().State; got != StateFailed {
		t.Errorf("state after shutdown = %s", got)
	}
}

func TestLogKeepsTail(t *testing.T) {
	var tb tailBuffer
	tb.limit = 8
	io.WriteString(&tb, "0123")
	io.WriteString(&tb, "456789ab")
	got, truncated := tb.snapshot()
	if got != "456789ab" || !truncated {
		t.Errorf("tail = %q truncated=%v", got, truncated)
	}
	io.WriteString(&tb, "0123456789")
	if got, _ := tb.snapshot(); got != "23456789" {
		t.Errorf("tail = %q", got)
	}
}

func TestListenSocketMode(t *testing.T) {
	dir, err := os.MkdirTemp("", "brk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "broker.sock")
	if err := os.WriteFile(path, nil, 0o600); err != nil { // stale file
		t.Fatal(err)
	}
	l, err := Listen(path, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSocket == 0 || fi.Mode().Perm() != 0o660 {
		t.Errorf("mode = %v", fi.Mode())
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}

func TestCommandExec(t *testing.T) {
	script := filepath.Join(t.TempDir(), "berth")
	body := "#!/bin/sh\necho \"$*\"\necho log >&2\ncase $1 in\n  fail) exit 3 ;;\n  hang) trap 'exit 143' TERM; while :; do sleep 0.05; done ;;\nesac\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	run := CommandExec(script)
	var out, errOut strings.Builder
	if exit, err := run(context.Background(), []string{"ok", "x"}, &out, &errOut); exit != 0 || err != nil {
		t.Fatalf("ok: %d %v", exit, err)
	}
	if out.String() != "ok x\n" || errOut.String() != "log\n" {
		t.Errorf("out=%q err=%q", out.String(), errOut.String())
	}
	if exit, err := run(context.Background(), []string{"fail"}, io.Discard, io.Discard); exit != 3 || err != nil {
		t.Errorf("fail: %d %v", exit, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if exit, err := run(ctx, []string{"hang"}, io.Discard, io.Discard); exit != 143 || err != nil {
		t.Errorf("hang: %d %v (want SIGTERM handled, exit 143)", exit, err)
	}
	if _, err := CommandExec(filepath.Join(t.TempDir(), "missing"))(context.Background(), nil, io.Discard, io.Discard); err == nil {
		t.Error("missing binary: want error")
	}
}
