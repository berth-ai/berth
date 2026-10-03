package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"time"

	"github.com/berth-ai/berth/internal/runner"
)

// HomeCommands are the programs home-side verbs run as agent.
type HomeCommands struct {
	Mise  string
	Git   string
	GH    string
	Glab  string
	Berth string
}

// DefaultHome resolves the programs on agent's PATH, as the image ships them.
func DefaultHome(berth string) HomeCommands {
	return HomeCommands{Mise: "mise", Git: "git", GH: "gh", Glab: "glab", Berth: berth}
}

const defaultSyncTimeout = time.Minute

var errTimeout = errors.New("timed out")

// agentRun runs path as agent and captures its output, for quick verbs that
// answer synchronously.
func (b *Broker) agentRun(ctx context.Context, timeout time.Duration, path string, args ...string) (string, string, int, error) {
	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	r := runner.Exec{Agent: b.Agent, Stdout: &stdout, Stderr: &stderr}
	err := r.Run(ctx, runner.Cmd{Path: path, Args: args, AsAgent: true})
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return stdout.String(), stderr.String(), -1, errTimeout
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return stdout.String(), stderr.String(), ee.ExitCode(), nil
	}
	if err != nil {
		return stdout.String(), stderr.String(), -1, err
	}
	return stdout.String(), stderr.String(), 0, nil
}

// homeWork is agentWork with mise told not to prompt.
func (b *Broker) homeWork(path string, args []string, timeout time.Duration) Work {
	return func(ctx context.Context, out io.Writer) (int, json.RawMessage, error) {
		r := runner.Exec{Agent: b.Agent, Stdout: out, Stderr: out}
		err := r.Run(ctx, runner.Cmd{Path: path, Args: args, Env: []string{"MISE_YES=1"}, AsAgent: true, Timeout: timeout})
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode(), nil, nil
		}
		if err != nil {
			return -1, nil, err
		}
		return 0, nil, nil
	}
}

func (b *Broker) syncTimeout() time.Duration {
	if b.SyncTimeout > 0 {
		return b.SyncTimeout
	}
	return defaultSyncTimeout
}
