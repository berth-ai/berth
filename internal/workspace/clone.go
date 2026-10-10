package workspace

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/skills"
)

// CloneTimeout bounds one clone.
const CloneTimeout = 30 * time.Minute

// CloneRequest names a repository, an optional ref and the folder, relative
// to the workspaces root, to clone it into.
type CloneRequest struct {
	Repo string
	Ref  string // a branch, tag or full commit; empty means the default branch
	Path string
}

// Cloned is a finished clone.
type Cloned struct {
	Path   string `json:"path"`
	Commit string `json:"commit"`
	Branch string `json:"branch"` // empty when HEAD is detached
}

// Clone clones r.Repo into root/r.Path. It refuses a path that exists,
// clones into a temporary folder beside it and renames that into place, so
// the folder appears whole or not at all. Folders it created are removed
// again on failure. log receives git's progress.
func (g Git) Clone(ctx context.Context, root string, r CloneRequest, log io.Writer) (Cloned, error) {
	if err := g.check(r); err != nil {
		return Cloned{}, err
	}
	rt, err := os.OpenRoot(root)
	if err != nil {
		return Cloned{}, fmt.Errorf("open %s: %w", root, err)
	}
	defer rt.Close()
	switch _, err := rt.Lstat(r.Path); {
	case err == nil:
		return Cloned{}, &Error{Code: "path_exists", Field: "path", Message: "That folder exists; adopt it instead"}
	case !errors.Is(err, fs.ErrNotExist):
		return Cloned{}, invalid("path", "Berth cannot use that folder: "+err.Error())
	}
	parent := path.Dir(r.Path)
	made, err := mkdirs(rt, parent)
	if err != nil {
		removeEmpty(rt, made)
		return Cloned{}, invalid("path", "Could not create the parent folders: "+err.Error())
	}
	tmp, err := os.MkdirTemp(filepath.Join(root, filepath.FromSlash(parent)), config.TempPrefix)
	if err != nil {
		removeEmpty(rt, made)
		return Cloned{}, invalid("path", "Could not create a folder for the clone: "+err.Error())
	}
	res, err := g.cloneInto(ctx, tmp, r, log)
	if err == nil {
		if rerr := os.Rename(tmp, filepath.Join(root, filepath.FromSlash(r.Path))); rerr != nil {
			err = &Error{Code: "path_exists", Field: "path", Message: "That folder appeared while cloning; adopt it instead"}
		}
	}
	if err != nil {
		os.RemoveAll(tmp)
		removeEmpty(rt, made)
		return Cloned{}, err
	}
	res.Path = r.Path
	return res, nil
}

// cloneInto clones r.Repo into the empty folder dir and checks out r.Ref.
func (g Git) cloneInto(ctx context.Context, dir string, r CloneRequest, log io.Writer) (Cloned, error) {
	var said tail
	args := []string{"clone", "--progress", "--no-recurse-submodules"}
	byCommit := config.ValidCommit(r.Ref)
	if r.Ref != "" && !byCommit {
		args = append(args, "--branch", r.Ref)
	}
	args = append(args, "--", r.Repo, dir)
	if err := g.command(ctx, "", io.MultiWriter(log, &said), args...).Run(); err != nil {
		if ctx.Err() != nil {
			return Cloned{}, &Error{Code: "fetch_failed", Field: "repo", Message: "The clone timed out or was cancelled"}
		}
		se := skills.ClassifyFetch(r.Repo, cmp.Or(r.Ref, "HEAD"), said.String())
		return Cloned{}, &Error{Code: se.Code, Field: se.Field, Message: se.Message}
	}
	// git clone --branch takes no commit: check it out instead.
	if byCommit {
		if err := g.command(ctx, dir, log, "checkout", "-q", "--detach", r.Ref).Run(); err != nil {
			return Cloned{}, &Error{Code: "ref_not_found", Field: "ref", Message: fmt.Sprintf("Ref %s not found in that repository", r.Ref)}
		}
	}
	commit, _ := g.output(ctx, dir, "rev-parse", "HEAD")
	branch, _ := g.output(ctx, dir, "symbolic-ref", "--short", "-q", "HEAD")
	return Cloned{Commit: commit, Branch: branch}, nil
}

func (g Git) check(r CloneRequest) error {
	if !g.AllowFile || !strings.HasPrefix(r.Repo, "file://") {
		if msg := config.CheckSkillRepo(r.Repo); msg != "" {
			return invalid("repo", "The repository URL "+msg)
		}
	}
	if r.Ref != "" && !config.ValidGitRef(r.Ref) {
		return invalid("ref", "The ref must be a branch, tag or commit")
	}
	if msg := config.CheckWorkspacePath(r.Path); msg != "" {
		return invalid("path", "The path "+msg)
	}
	return nil
}

// mkdirs creates dir and its missing parents inside rt and returns the
// folders it created, outermost first. os.Root refuses a symlink out of root.
func mkdirs(rt *os.Root, dir string) ([]string, error) {
	if dir == "." {
		return nil, nil
	}
	var made []string
	cur := ""
	for _, seg := range strings.Split(dir, "/") {
		cur = path.Join(cur, seg)
		switch err := rt.Mkdir(cur, 0o755); {
		case err == nil:
			made = append(made, cur)
		case !errors.Is(err, fs.ErrExist):
			return made, err
		}
	}
	fi, err := rt.Stat(dir)
	if err != nil {
		return made, err
	}
	if !fi.IsDir() {
		return made, fmt.Errorf("%s is not a folder", dir)
	}
	return made, nil
}

// removeEmpty removes the folders mkdirs made, innermost first, while they are empty.
func removeEmpty(rt *os.Root, made []string) {
	for i := len(made) - 1; i >= 0; i-- {
		if rt.Remove(made[i]) != nil {
			return
		}
	}
}
