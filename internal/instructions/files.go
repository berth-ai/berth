package instructions

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"unicode/utf8"

	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/wsstate"
)

// MaxSize caps an instruction file the panel edits.
const MaxSize = 256 << 10

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}\.md$`)

// ValidName reports whether name can be an instruction file: a .md file at
// the workspace's root, as descriptors name them.
func ValidName(name string) bool { return namePattern.MatchString(name) }

// Error is a refusal the panel shows as it is.
type Error struct{ Code, Message string }

func (e *Error) Error() string { return e.Message }

func refuse(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// File is one instruction file as the panel shows it.
type File struct {
	Name     string `json:"file"`
	State    string `json:"state"` // file, missing, symlink or other
	Target   string `json:"target,omitempty"`
	Owned    bool   `json:"owned,omitempty"` // Berth made the link
	Content  string `json:"content"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	Editable bool   `json:"editable"`
	Reason   string `json:"reason,omitempty"` // why it is not editable
	Tracked  bool   `json:"tracked"`
}

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// folder returns workspace id's folder from workspaces.json.
func folder(store wsstate.Store, id string) (string, error) {
	pub, err := store.Workspaces()
	if err != nil {
		return "", err
	}
	ws, ok := pub[id]
	if !ok {
		return "", wsstate.ErrGone
	}
	if fi, err := os.Stat(ws); err != nil || !fi.IsDir() {
		return "", refuse("workspace_missing", "The folder %s is missing", ws)
	}
	return ws, nil
}

// Read reports instruction file name of workspace id. git is the git
// binary; empty means git on PATH.
func Read(ctx context.Context, store wsstate.Store, git, id, name string) (File, error) {
	f := File{Name: name}
	if !ValidName(name) {
		return f, refuse("not_allowed", "%s is not an instruction file", name)
	}
	ws, err := folder(store, id)
	if err != nil {
		return f, err
	}
	p := filepath.Join(ws, name)
	fi, err := os.Lstat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		f.State, f.Editable = "missing", true
		return f, nil
	case err != nil:
		return f, err
	case fi.Mode()&fs.ModeSymlink != 0:
		f.State, f.Target = "symlink", readlink(p)
		f.Reason = name + " is a link to " + f.Target + "; edit that file instead"
		if st, err := store.Load(id); err == nil && st.Instructions.Path == ws {
			f.Owned = slices.Contains(st.Instructions.Links, name)
		}
	case !fi.Mode().IsRegular():
		f.State, f.Reason = "other", name+" is not a file"
	default:
		f.State, f.Size = "file", fi.Size()
		raw, err := os.ReadFile(p)
		if err != nil {
			return f, err
		}
		f.SHA256 = digest(raw)
		switch {
		case len(raw) > MaxSize:
			f.Reason = name + " is larger than 256 KiB; edit it in the IDE"
		case !utf8.Valid(raw):
			f.Reason = name + " is not UTF-8 text; edit it in the IDE"
		default:
			f.Content, f.Editable = string(raw), true
		}
	}
	f.Tracked, _ = wsstate.Tracked(ctx, git, ws, name)
	return f, nil
}

// Write replaces file name of workspace id with content when the file on
// disk still has sha256 base (empty: the file must not exist), and returns
// the new sha256. It keeps the file's mode and never writes through a link.
func Write(store wsstate.Store, id, name, content, base string) (string, error) {
	switch {
	case !ValidName(name):
		return "", refuse("not_allowed", "%s is not an instruction file", name)
	case len(content) > MaxSize:
		return "", refuse("too_large", "%s would be larger than 256 KiB", name)
	case !utf8.ValidString(content):
		return "", refuse("not_text", "The content must be UTF-8 text")
	}
	ws, err := folder(store, id)
	if err != nil {
		return "", err
	}
	p := filepath.Join(ws, name)
	perm := fs.FileMode(0o644)
	cur := ""
	fi, err := os.Lstat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return "", err
	case fi.Mode()&fs.ModeSymlink != 0:
		return "", refuse("is_symlink", "%s is a link to %s; edit that file instead", name, readlink(p))
	case !fi.Mode().IsRegular():
		return "", refuse("not_allowed", "%s is not a file", name)
	default:
		raw, err := os.ReadFile(p)
		if err != nil {
			return "", err
		}
		cur, perm = digest(raw), fi.Mode().Perm()
		if cur == base {
			// A file the panel cannot show is not the panel's to replace.
			switch {
			case len(raw) > MaxSize:
				return "", refuse("too_large", "%s is larger than 256 KiB; edit it in the IDE", name)
			case !utf8.Valid(raw):
				return "", refuse("not_text", "%s is not UTF-8 text; edit it in the IDE", name)
			}
		}
	}
	if cur != base {
		return "", refuse("changed_on_disk", "%s changed on disk since you opened it", name)
	}
	if err := config.WriteFileAtomic(p, []byte(content), perm, -1, -1); err != nil {
		return "", err
	}
	return digest([]byte(content)), nil
}

// Merge appends file name to AGENTS.md, after a blank line, and replaces
// it with Berth's link to AGENTS.md, under the workspace's lock. The
// workspace must share instructions.
func Merge(store wsstate.Store, id, name string) error {
	if !ValidName(name) || name == Shared {
		return refuse("not_allowed", "%s cannot be merged into %s", name, Shared)
	}
	pub, err := config.ReadWorkspacesFile(store.Published)
	if err != nil {
		return err
	}
	if w, ok := pub[id]; !ok {
		return wsstate.ErrGone
	} else if !w.Shared {
		return refuse("not_shared", "Turn on shared instructions first")
	}
	return store.Update(id, func(ws string, st *wsstate.State) error {
		src := filepath.Join(ws, name)
		fi, err := os.Lstat(src)
		if err != nil || !fi.Mode().IsRegular() {
			return refuse("not_a_file", "%s is not a file with its own content", name)
		}
		dst := filepath.Join(ws, Shared)
		var head []byte
		perm := fs.FileMode(0o644)
		switch di, err := os.Lstat(dst); {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			return err
		case di.Mode()&fs.ModeSymlink != 0:
			return refuse("agents_md_symlink", "%s is a symlink; Berth does not write through it", Shared)
		case !di.Mode().IsRegular():
			return refuse("not_a_file", "%s is not a file", Shared)
		default:
			if head, err = os.ReadFile(dst); err != nil {
				return err
			}
			perm = di.Mode().Perm()
		}
		body, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		out := head
		if len(out) > 0 {
			out = append(bytes.TrimRight(out, "\n"), '\n', '\n')
		}
		out = append(out, body...)
		if len(out) > MaxSize {
			return refuse("too_large", "%s and %s together are larger than 256 KiB", Shared, name)
		}
		if err := config.WriteFileAtomic(dst, out, perm, -1, -1); err != nil {
			return err
		}
		if err := os.Remove(src); err != nil {
			return err
		}
		if err := os.Symlink(Shared, src); err != nil {
			return err
		}
		if st.Instructions.Path != ws {
			st.Instructions = wsstate.Instructions{Path: ws}
		}
		st.Instructions.Links = append(st.Instructions.Links, name)
		slices.Sort(st.Instructions.Links)
		st.Instructions.Links = slices.Compact(st.Instructions.Links)
		delete(st.Instructions.Skipped, name)
		return nil
	})
}
