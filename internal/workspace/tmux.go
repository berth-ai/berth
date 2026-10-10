package workspace

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Tmux drives agent's tmux server, where each workspace has one session,
// ws-<id>, as berth-term names it.
type Tmux struct {
	Path string // the tmux binary; empty means tmux on PATH
}

// SessionName is the tmux session of workspace id.
func SessionName(id string) string { return "ws-" + id }

func (t Tmux) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, cmp.Or(t.Path, "tmux"), args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %s", args[0], cmp.Or(strings.TrimSpace(stderr.String()), err.Error()))
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// noSession reports whether err says the session, or the whole server, is not there.
func noSession(err error) bool {
	s := err.Error()
	return strings.Contains(s, "can't find session") || strings.Contains(s, "no server running") ||
		strings.Contains(s, "error connecting to")
}

// Windows lists each session's window names; no server means no sessions.
func (t Tmux) Windows(ctx context.Context) (map[string][]string, error) {
	out, err := t.run(ctx, "list-windows", "-a", "-F", "#{session_name}\t#{window_name}")
	if err != nil {
		if noSession(err) {
			return map[string][]string{}, nil
		}
		return nil, err
	}
	m := map[string][]string{}
	for _, line := range strings.Split(out, "\n") {
		if s, w, ok := strings.Cut(line, "\t"); ok {
			m[s] = append(m[s], w)
		}
	}
	return m, nil
}

// Window is what Launch opens: a program, or the default shell when Run is empty.
type Window struct {
	Name string // the window's name; empty lets tmux name it
	Run  string // an absolute program path
}

// Launch opens w in workspace id's session, creating the session in dir
// when it is not running; a new session's first window is w itself. A
// program runs, then the default shell, so its output stays readable
// after it exits.
func (t Tmux) Launch(ctx context.Context, id, dir string, w Window) error {
	s := SessionName(id)
	var opts []string
	if w.Name != "" {
		opts = append(opts, "-n", w.Name)
	}
	if w.Run != "" {
		// The broker's agent environment has no SHELL; ask tmux which shell it runs.
		shell, err := t.run(ctx, "start-server", ";", "show-options", "-gv", "default-shell")
		if err != nil {
			return err
		}
		opts = append(opts, shellQuote(w.Run)+"; exec "+shellQuote(shell))
	}
	if _, err := t.run(ctx, "has-session", "-t", "="+s); err != nil {
		if !noSession(err) {
			return err
		}
		_, err := t.run(ctx, append([]string{"new-session", "-d", "-s", s, "-c", dir}, opts...)...)
		if err == nil || !strings.Contains(err.Error(), "duplicate session") {
			return err
		}
		// Another launch created the session meanwhile: add a window to it.
	}
	_, err := t.run(ctx, append([]string{"new-window", "-t", "=" + s + ":", "-c", dir}, opts...)...)
	return err
}

// Kill ends workspace id's session and reports whether one was running.
func (t Tmux) Kill(ctx context.Context, id string) (bool, error) {
	if _, err := t.run(ctx, "kill-session", "-t", "="+SessionName(id)); err != nil {
		if noSession(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
