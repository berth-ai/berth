package agentlogin

import (
	"io"
	"maps"
	"os"
	"os/exec"
	"slices"
	"syscall"
	"time"

	"github.com/creack/pty"

	"github.com/berth-ai/berth/internal/locksource"
)

// child is a running sign-in CLI.
type child struct {
	cmd *exec.Cmd
	out io.ReadCloser // the terminal, or the read end of stdout and stderr
	in  io.Writer     // the terminal, or stdin
}

// start runs m's sign-in with a's bin, under a 1000x50 pty when m asks for
// one, else with pipes.
func (e Env) start(a Agent, m locksource.LoginMethod) (*child, error) {
	cmd := exec.Command(a.Bin, m.Args...)
	cmd.Env = e.environ()
	if m.PTY {
		cmd.Env = append(cmd.Env, "TERM=xterm-256color", "COLUMNS=1000", "LINES=50")
	} else {
		cmd.Env = append(cmd.Env, "TERM=dumb")
	}
	for _, k := range slices.Sorted(maps.Keys(m.Env)) {
		cmd.Env = append(cmd.Env, k+"="+m.Env[k]) // later entries win
	}
	if m.PTY {
		f, err := pty.StartWithAttrs(cmd, &pty.Winsize{Rows: 50, Cols: 1000}, procAttr(true))
		if err != nil {
			return nil, err
		}
		return &child{cmd: cmd, out: f, in: f}, nil
	}
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		r.Close()
		w.Close()
		return nil, err
	}
	cmd.Stdout, cmd.Stderr = w, w
	cmd.SysProcAttr = procAttr(false)
	err = cmd.Start()
	w.Close() // the CLI holds its own copy
	if err != nil {
		r.Close()
		return nil, err
	}
	return &child{cmd: cmd, out: r, in: stdin}, nil
}

// stop ends the CLI's session: SIGTERM now and SIGKILL after grace, or
// SIGKILL at once when grace is 0.
func (c *child) stop(grace time.Duration) {
	pid := c.cmd.Process.Pid
	if grace <= 0 {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	time.AfterFunc(grace, func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
}
