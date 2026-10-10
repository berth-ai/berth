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

// StoreDir holds the exports, one folder per source with one per attached skill.
func StoreDir(home string) string { return filepath.Join(home, ".local", "share", "berth", "skills") }

// StatePath records what Berth owns: store copies and links.
func StatePath(home string) string {
	return filepath.Join(home, ".local", "state", "berth", "skills-state.json")
}

type state struct {
	Exports map[string]export `json:"exports"` // "<source>/<name>" → what that store folder holds
	Links   []string          `json:"links"`   // links Berth created, sorted
	// Legacy lists C2's copies at StoreDir/<id>, by the commit they held.
	// The old state called them commits; the next sync removes them.
	Legacy map[string]string `json:"commits,omitempty"`
}

// export is what one store folder holds.
type export struct {
	Commit string `json:"commit"`
	Path   string `json:"path"`
}

func loadState(path string) (state, error) {
	st := newState()
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return newState(), fmt.Errorf("%s: %w", path, err)
	}
	if st.Exports == nil {
		st.Exports = map[string]export{}
	}
	if st.Links == nil {
		st.Links = []string{}
	}
	if st.Legacy == nil {
		st.Legacy = map[string]string{}
	}
	return st, nil
}

func newState() state {
	return state{Exports: map[string]export{}, Links: []string{}, Legacy: map[string]string{}}
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
