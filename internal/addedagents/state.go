package addedagents

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/berth-ai/berth/internal/config"
)

// StatePath is where Sync records failures, in the agent user's home.
func StatePath(home string) string {
	return filepath.Join(home, ".local", "state", "berth", "agents-state.json")
}

type state struct {
	Errors map[string]string `json:"errors"` // by package
}

func loadState(path string) (state, error) {
	st := state{Errors: map[string]string{}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return state{Errors: map[string]string{}}, fmt.Errorf("%s: %w", path, err)
	}
	if st.Errors == nil {
		st.Errors = map[string]string{}
	}
	return st, nil
}

func saveState(path string, st state) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(path, append(raw, '\n'), 0o644, -1, -1)
}
