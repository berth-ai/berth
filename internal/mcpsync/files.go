package mcpsync

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// legacyIDs are the servers berth-mcp-setup registered before Berth kept
// any state.
var legacyIDs = []string{"context7", "playwright"}

// StatePath is where Berth records, per config file, the ids it wrote.
func StatePath(home string) string {
	return filepath.Join(home, ".local", "state", "berth", "mcp.json")
}

// loadState reads the state file. seeded is true when there was none: the
// old script may then have registered its servers anywhere.
func loadState(path string) (st map[string][]string, seeded bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string][]string{}, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, false, fmt.Errorf("%s: %w", path, err)
	}
	if st == nil {
		st = map[string][]string{}
	}
	return st, false, nil
}

// saveState writes st when it differs from what is on disk.
func saveState(path string, st map[string][]string) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeAtomic(path, data, 0o600, nil)
}

// realPath follows symlinks, so a config linked from a dotfiles repo stays
// a link. A missing file resolves to itself; a dangling link is an error,
// because replacing it would silently detach the user's dotfiles.
func realPath(path string) (string, error) {
	real, err := filepath.EvalSymlinks(path)
	if err == nil {
		return real, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if _, lerr := os.Lstat(path); lerr == nil {
		return "", fmt.Errorf("%s is a dangling symlink", path)
	}
	return path, nil
}

// readConfig returns a file's content and permissions; perm 0 means the
// file does not exist.
func readConfig(path string) ([]byte, fs.FileMode, error) {
	fi, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	data, err := os.ReadFile(path)
	return data, fi.Mode().Perm(), err
}

// writeAtomic replaces path with data through a temp file in the same
// directory. check runs just before the rename; an error from it aborts.
func writeAtomic(path string, data []byte, perm fs.FileMode, check func() error) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".berth-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // fails harmlessly after the rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if check != nil {
		if err := check(); err != nil {
			return err
		}
	}
	return os.Rename(tmp.Name(), path)
}

// idsExcept lists the ids of servers not in skip.
func idsExcept(servers []server, skip []string) []string {
	ids := []string{}
	for _, s := range servers {
		if !slices.Contains(skip, s.ID) {
			ids = append(ids, s.ID)
		}
	}
	return ids
}
