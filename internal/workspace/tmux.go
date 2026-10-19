package workspace

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// Tmux drives agent's tmux server, where each workspace has one session,
// ws-<id>, as berth-term names it.
type Tmux struct {
	Path   string // the tmux binary; empty means tmux on PATH
	EnvCmd string // berth-ws-env; windows run through it, so they get the workspace's variables
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
// after it exits. With EnvCmd, every window runs through it, and the
// session's default-command does too, so windows the user opens get the
// workspace's variables as well; it is set on every launch, which also
// covers sessions berth-term or tmux-continuum created.
func (t Tmux) Launch(ctx context.Context, id, dir string, w Window) error {
	s := SessionName(id)
	var opts []string
	if w.Name != "" {
		opts = append(opts, "-n", w.Name)
	}
	var after []string // commands run in the same tmux call, after the window
	switch {
	case t.EnvCmd != "":
		shell, err := t.shell(ctx)
		if err != nil {
			return err
		}
		env := shellQuote(t.EnvCmd) + " " + shellQuote(id) + " -- "
		def := "exec " + env + shellQuote(shell) + " -l"
		if w.Run != "" {
			opts = append(opts, env+shellQuote(w.Run)+"; exec "+env+shellQuote(shell))
		} else {
			opts = append(opts, def)
		}
		after = []string{";", "set-option", "-t", "=" + s, "default-command", def}
	case w.Run != "":
		shell, err := t.shell(ctx)
		if err != nil {
			return err
		}
		opts = append(opts, shellQuote(w.Run)+"; exec "+shellQuote(shell))
	}
	if _, err := t.run(ctx, "has-session", "-t", "="+s); err != nil {
		if !noSession(err) {
			return err
		}
		args := append(append([]string{"new-session", "-d", "-s", s, "-c", dir}, opts...), after...)
		_, err := t.run(ctx, args...)
		if err == nil || !strings.Contains(err.Error(), "duplicate session") {
			return err
		}
		// Another launch created the session meanwhile: add a window to it.
	}
	var args []string
	if len(after) > 0 {
		args = append(slices.Clone(after[1:]), ";") // set the option first, then the window
	}
	args = append(append(args, "new-window", "-t", "="+s+":", "-c", dir), opts...)
	_, err := t.run(ctx, args...)
	return err
}

// shell is the shell tmux runs; the broker's agent environment has no SHELL.
func (t Tmux) shell(ctx context.Context) (string, error) {
	return t.run(ctx, "start-server", ";", "show-options", "-gv", "default-shell")
}

// Restart ends workspace id's session and opens a new one with one shell
// window, which reads the workspace's variables afresh.
func (t Tmux) Restart(ctx context.Context, id, dir string) (bool, error) {
	killed, err := t.Kill(ctx, id)
	if err != nil {
		return false, err
	}
	return killed, t.Launch(ctx, id, dir, Window{})
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
