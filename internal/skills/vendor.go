package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"

	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/wsstate"
)

// The ways Resolve settles a vendored skill with local edits.
const (
	ResolveOverwrite = "overwrite" // the next sync replaces or removes the copy anyway
	ResolveRelease   = "release"   // Berth forgets the copy and its links; the files stay
)

// vendorEntry keeps a copy of w's export in <ws>/.agents/skills/<name>,
// linked from every other project folder in dirs. A copy whose files the
// user changed is left as it is, unless e.Force says Overwrite.
func (s Syncer) vendorEntry(ws string, w wsWant, dirs []string, e *wsstate.Skill, sec *wsstate.Skills) error {
	dir := path.Join(VendorDir, w.name)
	full := filepath.Join(ws, filepath.FromSlash(dir))
	_, statErr := os.Lstat(full)
	if e.Mode == "" {
		if err := wsstate.Plain(ws, dir); err != nil {
			sec.Skipped[dir] = err.Error()
			s.logf("%s: %v; leaving it alone", dir, err)
			return nil
		}
		if statErr == nil {
			sec.Skipped[dir] = "already exists; Berth left it alone"
			s.logf("%s already exists in %s; leaving it alone", dir, ws)
			return nil
		}
	}
	if e.Mode == "" || e.Commit != w.commit || e.From != w.from || e.Force || statErr != nil {
		if e.Mode != "" && statErr == nil {
			clean, err := unedited(full, e.Files)
			if err != nil {
				return err
			}
			if !clean && !e.Force {
				e.Edited = true
				return errLocalEdits
			}
		}
		files, err := s.vendor(ws, w, sec)
		if err != nil {
			return err
		}
		*e = wsstate.Skill{Name: w.name, Mode: config.ModeVendor, Commit: w.commit, From: w.from, Dir: dir,
			Files: files, Links: e.Links}
		s.logf("vendored %s into %s at %s", w.name, ws, short(w.commit))
	}
	e.Edited = false
	others := slices.DeleteFunc(slices.Clone(dirs), func(d string) bool { return d == VendorDir })
	return s.placeLinks(ws, w.name, others, func(rel string) string { return s.linkTarget(w.key, *e, rel) }, e, sec)
}

// vendor copies w's export into the vendor folder through a temporary
// folder and the store's atomic swap, and returns the copy's files.
func (s Syncer) vendor(ws string, w wsWant, sec *wsstate.Skills) (map[string]string, error) {
	made, err := wsstate.MkdirAll(ws, VendorDir)
	sec.Dirs = wsstate.AddDirs(sec.Dirs, made...)
	if err != nil {
		return nil, err
	}
	parent := filepath.Join(ws, filepath.FromSlash(VendorDir))
	tmp, err := os.MkdirTemp(parent, ".berth-tmp-"+w.name+"-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	files, err := copyTree(w.export, tmp)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return nil, err
	}
	if err := swap(tmp, filepath.Join(parent, w.name)); err != nil {
		return nil, err
	}
	return files, nil
}

// copyTree copies the regular files, folders and symlinks of src into dst
// and returns what it copied: path → "sha256:<hex>" or "link:<target>".
func copyTree(src, dst string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if rel == "." {
			return nil
		}
		out := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.Mkdir(out, 0o755)
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			files[filepath.ToSlash(rel)] = "link:" + target
			return os.Symlink(target, out)
		case d.Type().IsRegular():
			sum, err := copyFile(p, out)
			files[filepath.ToSlash(rel)] = sum
			return err
		}
		return fmt.Errorf("%s is not a file, folder or link", rel)
	})
	return files, err
}

// copyFile copies one file, executable or not, and returns its digest.
func copyFile(src, dst string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return "", err
	}
	perm := fs.FileMode(0o644)
	if fi.Mode().Perm()&0o111 != 0 {
		perm = 0o755
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(out, h), in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), err
}

// treeFiles lists dir's files as copyTree records them.
func treeFiles(dir string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if d.Type()&fs.ModeSymlink != 0 {
			target, err := os.Readlink(p)
			files[filepath.ToSlash(rel)] = "link:" + target
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = "sha256:" + hex.EncodeToString(h.Sum(nil))
		return nil
	})
	return files, err
}

// unedited reports whether dir holds exactly files: no file changed,
// removed or added. A missing dir has nothing left to protect.
func unedited(dir string, files map[string]string) (bool, error) {
	if _, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	now, err := treeFiles(dir)
	if err != nil {
		return false, err
	}
	return maps.Equal(now, files), nil
}

// Resolve settles a vendored skill of workspace id that local edits
// stopped: ResolveOverwrite lets the next sync replace or remove it anyway,
// ResolveRelease makes Berth forget it, leaving its files and links.
func Resolve(store wsstate.Store, id, key, action string) error {
	if action != ResolveOverwrite && action != ResolveRelease {
		return &Error{Code: "invalid_request", Field: "action", Message: "The action must be overwrite or release"}
	}
	return store.Update(id, func(_ string, st *wsstate.State) error {
		e, ok := st.Skills.Entries[key]
		if !ok || e.Mode != config.ModeVendor {
			return &Error{Code: "not_vendored", Field: "name", Message: fmt.Sprintf("Berth has no vendored copy of %s in %s", key, id)}
		}
		if action == ResolveRelease {
			delete(st.Skills.Entries, key)
			return nil
		}
		e.Force = true
		st.Skills.Entries[key] = e
		return nil
	})
}
