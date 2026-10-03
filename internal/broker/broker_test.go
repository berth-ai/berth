package broker

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berth-ai/bert/internal/status"
)

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

func TestAgentsReportErrorIsReported(t *testing.T) {
	tb := newTestBroker(t)
	tb.b.Agents = func() (status.Report, error) { return status.Report{}, errors.New("store unreadable") }
	st := tb.b.Status()
	if st.Agents != nil || st.AgentsError != "store unreadable" {
		t.Errorf("status = %+v", st)
	}
}
