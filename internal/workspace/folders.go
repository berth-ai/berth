package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/berth-ai/berth/internal/config"
)

const (
	// MaxCandidates caps the folders Candidates lists.
	MaxCandidates = 500
	// RemoveTimeout bounds deleting one workspace folder.
	RemoveTimeout = 10 * time.Minute
	maxLeft       = 50
	wOK           = 0x2 // access(2) W_OK
)

// Probe is what agent can do in a folder.
type Probe struct {
	Writable bool `json:"writable"` // agent can create files there and, in a git repository, in .git/info
	Git      bool `json:"git"`
}

// ProbeFolder reports on root/p, which must be a folder.
func ProbeFolder(root, p string) (Probe, error) {
	if msg := config.CheckWorkspacePath(p); msg != "" {
		return Probe{}, invalid("path", "The path "+msg)
	}
	rt, err := os.OpenRoot(root)
	if err != nil {
		return Probe{}, fmt.Errorf("open %s: %w", root, err)
	}
	defer rt.Close()
	fi, err := rt.Stat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Probe{}, &Error{Code: "missing", Field: "path", Message: "That folder does not exist"}
	case err != nil:
		return Probe{}, invalid("path", "Berth cannot use that folder: "+err.Error())
	case !fi.IsDir():
		return Probe{}, &Error{Code: "not_dir", Field: "path", Message: "That is a file, not a folder"}
	}
	return probe(filepath.Join(root, filepath.FromSlash(p))), nil
}

// probe reports on dir, which exists. access(2) answers for the calling
// user without writing anything.
func probe(dir string) Probe {
	p := Probe{Writable: writable(dir)}
	gitDir := filepath.Join(dir, ".git")
	fi, err := os.Stat(gitDir)
	if err != nil {
		return p
	}
	p.Git = true
	if fi.IsDir() {
		info := filepath.Join(gitDir, "info")
		if _, err := os.Stat(info); err != nil {
			info = gitDir
		}
		p.Writable = p.Writable && writable(info)
	}
	return p
}

func writable(dir string) bool { return syscall.Access(dir, wOK) == nil }

// Candidate is a folder that could be adopted.
type Candidate struct {
	Path string `json:"path"`
	Git  bool   `json:"git"`
}

// Candidates lists the folders at depth 1 and 2 under root that could
// become workspaces: none of taken, not inside or holding one of them, not
// hidden (temporary clones included), not a symlink and not inside a git
// repository. The list is sorted and capped at MaxCandidates.
func Candidates(root string, taken []string) ([]Candidate, error) {
	top, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	free := func(p string) bool {
		return !slices.ContainsFunc(taken, func(t string) bool { return config.PathsOverlap(p, t) })
	}
	out := []Candidate{}
	for _, e := range top {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p, dir := e.Name(), filepath.Join(root, e.Name())
		git := isGit(dir)
		if free(p) {
			out = append(out, Candidate{Path: p, Git: git})
		}
		if git || slices.Contains(taken, p) {
			continue
		}
		subs, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, s := range subs {
			if !s.IsDir() || strings.HasPrefix(s.Name(), ".") {
				continue
			}
			if sp := p + "/" + s.Name(); free(sp) {
				out = append(out, Candidate{Path: sp, Git: isGit(filepath.Join(dir, s.Name()))})
			}
		}
	}
	slices.SortFunc(out, func(a, b Candidate) int { return strings.Compare(a.Path, b.Path) })
	return out[:min(len(out), MaxCandidates)], nil
}

func isGit(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}

// Removal is the outcome of Remove.
type Removal struct {
	Removed bool     `json:"removed"`
	Left    []string `json:"left"` // up to 50 entries, relative to root, that could not be removed
}

// Remove deletes root/p and everything in it through an os.Root, so it
// touches nothing outside root: a symlink loses only itself, and a path
// through a symlink out of root is refused. A missing folder counts as
// removed.
func Remove(root, p string) (Removal, error) {
	if msg := config.CheckWorkspacePath(p); msg != "" {
		return Removal{}, invalid("path", "The path "+msg)
	}
	rt, err := os.OpenRoot(root)
	if err != nil {
		return Removal{}, fmt.Errorf("open %s: %w", root, err)
	}
	defer rt.Close()
	switch _, err := rt.Lstat(p); {
	case errors.Is(err, fs.ErrNotExist):
		return Removal{Removed: true, Left: []string{}}, nil
	case err != nil:
		return Removal{}, invalid("path", "Berth cannot delete that folder: "+err.Error())
	}
	if err := rt.RemoveAll(p); err == nil {
		return Removal{Removed: true, Left: []string{}}, nil
	}
	return Removal{Left: leftovers(rt, p)}, nil
}

// leftovers lists what is left below p: its files, or its folders when no
// file is left, at most maxLeft.
func leftovers(rt *os.Root, p string) []string {
	var files, dirs []string
	_ = fs.WalkDir(rt.FS(), p, func(name string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
		case d.IsDir():
			dirs = append(dirs, name)
		default:
			files = append(files, name)
		}
		if len(files) >= maxLeft {
			return fs.SkipAll
		}
		return nil
	})
	if len(files) == 0 {
		files = dirs
	}
	return files[:min(len(files), maxLeft)]
}
