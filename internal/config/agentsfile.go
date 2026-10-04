package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// AgentsFileSchema is the schema of agents.json, the copy of agents.added
// that the agent-side commands (berth agents sync, mcp sync, skills sync,
// login) read: they run as agent, which cannot read config.json.
const AgentsFileSchema = 1

type agentsFile struct {
	Schema int      `json:"schema"`
	Added  []string `json:"added"`
}

// WriteAgentsFile publishes added for the agent side; mode 0640, owned by
// uid:gid (-1 keeps it).
func WriteAgentsFile(path string, added []string, uid, gid int) error {
	if added == nil {
		added = []string{}
	}
	raw, err := json.MarshalIndent(agentsFile{Schema: AgentsFileSchema, Added: added}, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, append(raw, '\n'), 0o640, uid, gid)
}

// ReadAgentsFile reads agents.json; a missing file adds nothing.
func ReadAgentsFile(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var f agentsFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if f.Schema > AgentsFileSchema {
		return nil, fmt.Errorf("%s has schema %d; this Berth reads schema %d", path, f.Schema, AgentsFileSchema)
	}
	for _, p := range f.Added {
		if !packagePattern.MatchString(p) {
			return nil, fmt.Errorf("%s: %q is not an npm package name", path, p)
		}
	}
	if f.Added == nil {
		f.Added = []string{}
	}
	return f.Added, nil
}
