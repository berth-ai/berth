package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// SkillsFileSchema is the schema of skills.json, the copy of the skill
// sources that berth skills sync reads: it runs as agent, which cannot
// read config.json. Schema 1 held C's flat skills map.
const SkillsFileSchema = 2

type skillsFile struct {
	Schema  int               `json:"schema"`
	Sources map[string]Source `json:"sources"`
}

// WriteSkillsFile publishes s for the agent-side converger; mode 0640,
// owned by uid:gid (-1 keeps it).
func WriteSkillsFile(path string, s map[string]Source, uid, gid int) error {
	raw, err := json.MarshalIndent(skillsFile{Schema: SkillsFileSchema, Sources: s}, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, append(raw, '\n'), 0o640, uid, gid)
}

// ReadSkillsFile reads skills.json; a missing file holds no sources. Any
// other schema is refused: the broker rewrites the file when it starts.
func ReadSkillsFile(path string) (map[string]Source, error) {
	out := map[string]Source{}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	var f skillsFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return out, fmt.Errorf("%s: %w", path, err)
	}
	if f.Schema != SkillsFileSchema {
		return out, fmt.Errorf("%s has schema %d; this Berth reads schema %d", path, f.Schema, SkillsFileSchema)
	}
	if f.Sources != nil {
		out = f.Sources
	}
	return out, nil
}
