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

func TestExecTimeoutKillsChildren(t *testing.T) {
	// A grandchild that holds the output pipe must not keep Run waiting
	// past the timeout.
	var out bytes.Buffer
	start := time.Now()
	err := Exec{Stdout: &out, Stderr: &out}.Run(context.Background(),
		Cmd{Path: "sh", Args: []string{"-c", "sleep 5; echo done"}, Timeout: 100 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("Run returned after %s", time.Since(start))
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

func TestAgentEnvKeepsOnlyAllowlistedVariables(t *testing.T) {
	environ := []string{
		"PATH=/usr/bin", "LANG=C.UTF-8", "TZ=UTC", "HTTPS_PROXY=http://proxy:3128",
		"GITHUB_TOKEN=secret", "GH_TOKEN=secret", "CODE_SERVER_PASSWORD=pw",
		"npm_config_registry=http://evil", "HOME=/root", "USER=root",
	}
	got := agentEnv(environ, []string{"npm_config_cache=/tmp/c"}, &Identity{UID: 1000, GID: 1000, Home: "/home/agent"})
	want := []string{
		"PATH=/usr/bin", "LANG=C.UTF-8", "TZ=UTC", "HTTPS_PROXY=http://proxy:3128",
		"npm_config_cache=/tmp/c", "HOME=/home/agent", "USER=agent", "LOGNAME=agent",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}
