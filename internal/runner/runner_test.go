package runner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestExecEnvAndDir(t *testing.T) {
	var out bytes.Buffer
	dir := t.TempDir()
	err := Exec{Stdout: &out}.Run(context.Background(), Cmd{
		Path: "sh", Args: []string{"-c", `printf '%s %s' "$FOO" "$(pwd -P)"`},
		Dir: dir, Env: []string{"FOO=bar"},
	})
	if err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(dir) // macOS temp dirs live behind a symlink
	if out.String() != "bar "+real {
		t.Fatalf("got %q, want %q", out.String(), "bar "+real)
	}
}

func TestExecFailureNamesCommand(t *testing.T) {
	err := Exec{}.Run(context.Background(), Cmd{Path: "sh", Args: []string{"-c", "exit 3"}})
	if err == nil || !strings.Contains(err.Error(), "sh -c exit 3") {
		t.Fatalf("err = %v", err)
	}
}

func TestExecTimeout(t *testing.T) {
	start := time.Now()
	err := Exec{}.Run(context.Background(), Cmd{Path: "sleep", Args: []string{"5"}, Timeout: 100 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("timeout not enforced")
	}
}

func TestExecAsAgentWithoutIdentityRunsAsSelf(t *testing.T) {
	var out bytes.Buffer
	if err := (Exec{Stdout: &out}).Run(context.Background(), Cmd{Path: "id", Args: []string{"-u"}, AsAgent: true}); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != strconv.Itoa(os.Getuid()) {
		t.Fatalf("got uid %q", out.String())
	}
}
