package wsstate

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/berth-ai/berth/internal/config"
)

// The block Berth owns in a repository's info/exclude.
const (
	BlockStart = "# >>> berth (managed; do not edit)"
	BlockEnd   = "# <<< berth"
)

// ExcludePath returns the info/exclude file of the repository at ws, or ""
// when ws is not a git repository. A .git file (a worktree) is followed to
// its gitdir and that gitdir's commondir; a symlinked .git is refused.
func ExcludePath(ws string) (string, error) {
	dotgit := filepath.Join(ws, ".git")
	fi, err := os.Lstat(dotgit)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", nil
	case err != nil:
		return "", err
	case fi.IsDir():
		return filepath.Join(dotgit, "info", "exclude"), nil
	case !fi.Mode().IsRegular():
		return "", fmt.Errorf("%s is not a folder or a gitdir file", dotgit)
	}
	raw, err := os.ReadFile(dotgit)
	if err != nil {
		return "", err
	}
	dir, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir: ")
	if !ok || dir == "" {
		return "", fmt.Errorf("%s does not name a gitdir", dotgit)
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(ws, dir)
	}
	if c, err := os.ReadFile(filepath.Join(dir, "commondir")); err == nil {
		common := strings.TrimSpace(string(c))
		if !filepath.IsAbs(common) {
			common = filepath.Join(dir, common)
		}
		dir = common
	}
	return filepath.Join(filepath.Clean(dir), "info", "exclude"), nil
}

// WriteExclude makes Berth's block in ws's info/exclude hold lines, or
// removes it when lines is empty. Every other line stays as it was. A
// folder that is not a git repository has nothing to write.
func WriteExclude(ws string, lines []string) error {
	p, err := ExcludePath(ws)
	if err != nil || p == "" {
		return err
	}
	data, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var kept []string
	in := false
	for _, l := range strings.Split(string(data), "\n") {
		switch t := strings.TrimSpace(l); {
		case !in && t == BlockStart:
			in = true
		case in && t == BlockEnd:
			in = false
		case !in:
			kept = append(kept, l)
		}
	}
	if in {
		return fmt.Errorf("%s has a Berth block without its end line; fix it by hand", p)
	}
	for len(kept) > 0 && kept[len(kept)-1] == "" {
		kept = kept[:len(kept)-1]
	}
	if len(lines) > 0 {
		kept = append(append(append(kept, BlockStart), lines...), BlockEnd)
	}
	out := []byte(strings.Join(kept, "\n"))
	if len(kept) > 0 {
		out = append(out, '\n')
	}
	if bytes.Equal(out, data) || data == nil && len(out) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	perm := fs.FileMode(0o644)
	if fi, err := os.Stat(p); err == nil {
		perm = fi.Mode().Perm()
	}
	return config.WriteFileAtomic(p, out, perm, -1, -1)
}

// Plain reports an error when ws is not a folder, or when any part of rel
// that exists is a symlink: Berth never writes through a link in a
// workspace.
func Plain(ws, rel string) error {
	if fi, err := os.Lstat(ws); err != nil || !fi.IsDir() {
		return fmt.Errorf("the folder %s is missing", ws)
	}
	cur := ws
	for _, seg := range strings.Split(rel, "/") {
		cur = filepath.Join(cur, seg)
		fi, err := os.Lstat(cur)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symlink; Berth does not write through links in a workspace", strings.TrimPrefix(cur, ws+"/"))
		}
	}
	return nil
}

// MkdirAll creates the folders of rel under ws that are missing and returns
// them, relative and outermost first. It refuses to go through a symlink.
func MkdirAll(ws, rel string) ([]string, error) {
	if err := Plain(ws, rel); err != nil {
		return nil, err
	}
	var made []string
	cur := ""
	for _, seg := range strings.Split(rel, "/") {
		cur = path.Join(cur, seg)
		err := os.Mkdir(filepath.Join(ws, filepath.FromSlash(cur)), 0o755)
		switch {
		case err == nil:
			made = append(made, cur)
		case errors.Is(err, fs.ErrExist):
		default:
			return made, err
		}
	}
	return made, nil
}

// RemoveEmpty removes the folders of dirs that are empty, innermost first,
// and returns those still there.
func RemoveEmpty(ws string, dirs []string) []string {
	sorted := slices.Clone(dirs)
	slices.SortFunc(sorted, func(a, b string) int {
		return cmp.Or(cmp.Compare(strings.Count(b, "/"), strings.Count(a, "/")), strings.Compare(a, b))
	})
	left := []string{}
	for _, d := range sorted {
		err := os.Remove(filepath.Join(ws, filepath.FromSlash(d)))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			left = append(left, d)
		}
	}
	slices.Sort(left)
	return left
}

// AddDirs returns dirs with more added, sorted and without repeats.
func AddDirs(dirs []string, more ...string) []string {
	out := append(slices.Clone(dirs), more...)
	slices.Sort(out)
	return slices.Compact(out)
}

// Tracked reports whether git tracks rel in the repository at ws; false
// when ws is not a repository. It takes no optional locks and runs no
// fsmonitor hook.
func Tracked(ctx context.Context, git, ws, rel string) (bool, error) {
	if p, err := ExcludePath(ws); err != nil || p == "" {
		return false, err
	}
	cmd := exec.CommandContext(ctx, cmp.Or(git, "git"), "-c", "core.fsmonitor=false", "-C", ws,
		"ls-files", "--error-unmatch", "--", rel)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &ee) && ee.ExitCode() == 1:
		return false, nil
	}
	return false, err
}
