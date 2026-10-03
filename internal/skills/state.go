package skills

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// StoreDir holds one exported copy of every installed skill.
func StoreDir(home string) string { return filepath.Join(home, ".local", "share", "berth", "skills") }

// StatePath records what Berth owns: store copies and links.
func StatePath(home string) string {
	return filepath.Join(home, ".local", "state", "berth", "skills-state.json")
}

type state struct {
	Commits map[string]string `json:"commits"` // skill id → commit in the store
	Links   []string          `json:"links"`   // links Berth created, sorted
}

func loadState(path string) (state, error) {
	st := state{Commits: map[string]string{}, Links: []string{}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return state{Commits: map[string]string{}, Links: []string{}}, fmt.Errorf("%s: %w", path, err)
	}
	if st.Commits == nil {
		st.Commits = map[string]string{}
	}
	if st.Links == nil {
		st.Links = []string{}
	}
	return st, nil
}

func saveState(path string, st state) error {
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, raw) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
