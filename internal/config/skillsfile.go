package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// SkillsFileSchema is the schema of skills.json, the copy of the skills
// section that berth skills sync reads: it runs as agent, which cannot read
// config.json.
const SkillsFileSchema = 1

type skillsFile struct {
	Schema int              `json:"schema"`
	Skills map[string]Skill `json:"skills"`
}

// WriteSkillsFile publishes s for the agent-side converger; mode 0640,
// owned by uid:gid (-1 keeps it).
func WriteSkillsFile(path string, s map[string]Skill, uid, gid int) error {
	raw, err := json.MarshalIndent(skillsFile{Schema: SkillsFileSchema, Skills: s}, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, append(raw, '\n'), 0o640, uid, gid)
}

// ReadSkillsFile reads skills.json; a missing file holds no skills.
func ReadSkillsFile(path string) (map[string]Skill, error) {
	out := map[string]Skill{}
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
	if f.Schema > SkillsFileSchema {
		return out, fmt.Errorf("%s has schema %d; this Berth reads schema %d", path, f.Schema, SkillsFileSchema)
	}
	if f.Skills != nil {
		out = f.Skills
	}
	return out, nil
}
