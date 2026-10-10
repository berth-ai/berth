// Package workspace does the agent-side work behind the panel's
// workspaces: cloning and adopting folders under /workspaces, reporting
// on them, removing them, and their tmux sessions. berth workspaces runs
// it as agent, never as root, and it never changes ownership.
package workspace

import (
	"cmp"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Error is a failure the panel shows next to one field of its dialog.
type Error struct {
	Code    string // invalid_request, path_exists, ref_not_found, auth_required, fetch_failed, missing, not_dir
	Field   string // repo, ref or path
	Message string
}

func (e *Error) Error() string { return e.Message }

func invalid(field, msg string) *Error {
	return &Error{Code: "invalid_request", Field: field, Message: msg}
}

// Git runs git for workspaces: clones are HTTPS only, never prompt and
// skip submodules; the user's global git config applies, so the gh and
// glab credential helpers sign private clones in.
type Git struct {
	Path      string // the git binary; empty means git on PATH
	AllowFile bool   // tests only: also allow file:// repositories
}

// command builds a hardened git command in dir with stderr to log.
func (g Git) command(ctx context.Context, dir string, log io.Writer, args ...string) *exec.Cmd {
	full := []string{"-c", "protocol.allow=never", "-c", "protocol.https.allow=always", "-c", "submodule.recurse=false"}
	if g.AllowFile {
		full = append(full, "-c", "protocol.file.allow=always")
	}
	cmd := exec.CommandContext(ctx, cmp.Or(g.Path, "git"), append(full, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never")
	cmd.Stderr = log
	cmd.WaitDelay = time.Second
	return cmd
}

// output runs a read-only git command in dir and returns its trimmed
// stdout. It takes no optional locks and runs no fsmonitor hook.
func (g Git) output(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, cmp.Or(g.Path, "git"), append([]string{"-c", "core.fsmonitor=false"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// tail keeps the last 4 KiB written to it.
type tail struct{ buf []byte }

func (t *tail) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - 4096; over > 0 {
		t.buf = t.buf[over:]
	}
	return len(p), nil
}

func (t *tail) String() string { return string(t.buf) }
