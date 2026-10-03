package skills

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// Git runs git for skills: HTTPS only, never prompting, against bare cache
// repositories in the agent's home. The user's global git config applies,
// so the gh and glab credential helpers sign private fetches in.
type Git struct {
	Path      string    // the git binary; empty means git on PATH
	AllowFile bool      // tests only: also allow file:// repositories
	Log       io.Writer // receives git's error output; nil discards it
}

// CacheRoot holds the bare cache repositories.
func CacheRoot(home string) string { return filepath.Join(home, ".cache", "berth", "skills") }

// CacheDir is the bare repository that caches repo.
func CacheDir(home, repo string) string {
	sum := sha256.Sum256([]byte(repo))
	return filepath.Join(CacheRoot(home), hex.EncodeToString(sum[:8])+".git")
}

// gitError is a failed git command; stderr is what it said.
type gitError struct {
	stderr string
	err    error
}

func (e *gitError) Error() string {
	if e.stderr != "" {
		lines := strings.Split(e.stderr, "\n")
		return lines[len(lines)-1]
	}
	return e.err.Error()
}

func (g Git) run(ctx context.Context, gitDir string, stdout io.Writer, args ...string) error {
	full := []string{"-c", "protocol.allow=never", "-c", "protocol.https.allow=always"}
	if g.AllowFile {
		full = append(full, "-c", "protocol.file.allow=always")
	}
	if gitDir != "" {
		full = append(full, "--git-dir="+gitDir)
	}
	cmd := exec.CommandContext(ctx, cmp.Or(g.Path, "git"), append(full, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never")
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = stdout, &stderr
	err := cmd.Run()
	if err != nil {
		if g.Log != nil && stderr.Len() > 0 {
			g.Log.Write(stderr.Bytes())
		}
		return &gitError{stderr: strings.TrimSpace(stderr.String()), err: err}
	}
	return nil
}

// lock takes the cache repository's lock; wait false fails at once when
// another process holds it.
func lock(dir string, wait bool) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(dir+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	how := syscall.LOCK_EX
	if !wait {
		how |= syscall.LOCK_NB
	}
	if err := syscall.Flock(int(f.Fd()), how); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

// Fetch fetches ref from repo into the cache at dir, creating it, and
// returns the commit ref names.
func (g Git) Fetch(ctx context.Context, dir, repo, ref string) (string, error) {
	unlock, err := lock(dir, true)
	if err != nil {
		return "", err
	}
	defer unlock()
	if _, err := os.Stat(filepath.Join(dir, "HEAD")); err != nil {
		if err := g.run(ctx, "", io.Discard, "init", "--bare", "-q", dir); err != nil {
			return "", err
		}
	}
	if err := g.run(ctx, dir, io.Discard, "fetch", "-q", "--depth", "1", "--no-tags", "--no-recurse-submodules", repo, ref); err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := g.run(ctx, dir, &out, "rev-parse", "--verify", "-q", "FETCH_HEAD^{commit}"); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

// HasCommit reports whether the cache at dir holds commit.
func (g Git) HasCommit(ctx context.Context, dir, commit string) bool {
	return g.run(ctx, dir, io.Discard, "cat-file", "-e", commit+"^{commit}") == nil
}

// Show returns file at commit.
func (g Git) Show(ctx context.Context, dir, commit, file string) ([]byte, error) {
	var out bytes.Buffer
	if err := g.run(ctx, dir, &out, "cat-file", "blob", commit+":"+file); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// Archive streams the tar of sub (the root when empty) at commit to extract.
// When extract stops early, git is stopped and extract's error returned.
func (g Git) Archive(ctx context.Context, dir, commit, sub string, extract func(io.Reader) error) error {
	unlock, err := lock(dir, true)
	if err != nil {
		return err
	}
	defer unlock()
	treeish := commit
	if sub != "" {
		treeish += ":" + sub
	}
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := g.run(ctx, dir, pw, "archive", "--format=tar", treeish)
		pw.CloseWithError(err)
		done <- err
	}()
	xerr := extract(pr)
	pr.CloseWithError(fmt.Errorf("extraction stopped"))
	gerr := <-done
	if xerr != nil {
		return xerr
	}
	return gerr
}
