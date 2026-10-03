// Package runner runs subprocesses, dropping to the agent user when berth
// runs as root so third-party install scripts never run privileged.
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Identity is the unprivileged user commands run as.
type Identity struct {
	UID  uint32
	GID  uint32
	Home string
}

// LookupUser resolves name from the user database.
func LookupUser(name string) (*Identity, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return nil, err
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return nil, err
	}
	gid, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		return nil, err
	}
	return &Identity{UID: uint32(uid), GID: uint32(gid), Home: u.HomeDir}, nil
}

// Cmd describes one subprocess. Env entries are added to the parent's
// environment and win over it.
type Cmd struct {
	Path    string
	Args    []string
	Dir     string
	Env     []string
	AsAgent bool
	Timeout time.Duration
}

// Runner runs commands.
type Runner interface {
	Run(ctx context.Context, c Cmd) error
}

// Exec runs commands with os/exec.
type Exec struct {
	Agent  *Identity
	Stdout io.Writer
	Stderr io.Writer
}

// Run implements Runner.
func (e Exec) Run(ctx context.Context, c Cmd) error {
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, c.Path, c.Args...)
	cmd.Dir = c.Dir
	cmd.Env = append(os.Environ(), c.Env...)
	cmd.Stdout, cmd.Stderr = e.Stdout, e.Stderr
	cmd.WaitDelay = 5 * time.Second
	// Each command leads its own process group, and a timeout or cancel kills
	// the whole group: a child that holds the output pipe (git under mise,
	// say) would otherwise outlive the command and stall Wait.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	if c.AsAgent && e.Agent != nil {
		cmd.SysProcAttr.Credential = &syscall.Credential{
			Uid: e.Agent.UID, Gid: e.Agent.GID, Groups: []uint32{},
		}
		cmd.Env = agentEnv(os.Environ(), c.Env, e.Agent)
	}
	err := cmd.Run()
	name := strings.TrimSpace(c.Path + " " + strings.Join(c.Args, " "))
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%s timed out after %s", name, c.Timeout)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// agentEnvKeep lists the variables root passes on to commands run as agent.
// Everything else, tokens and frontend secrets included, is dropped so that
// third-party install scripts never see it.
var agentEnvKeep = []string{
	"PATH", "LANG", "LANGUAGE", "LC_ALL", "TZ", "TERM",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy",
	"NODE_EXTRA_CA_CERTS", "SSL_CERT_FILE", "SSL_CERT_DIR",
}

// agentEnv builds the environment of a command run as agent: the allowlisted
// part of environ, then extra, then the agent's identity.
func agentEnv(environ, extra []string, id *Identity) []string {
	var env []string
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if slices.Contains(agentEnvKeep, name) {
			env = append(env, kv)
		}
	}
	env = append(env, extra...)
	return append(env, "HOME="+id.Home, "USER=agent", "LOGNAME=agent")
}
