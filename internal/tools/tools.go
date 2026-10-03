// Package tools reads the base-track pins and the curated presets shipped
// in the image, and validates the tool names and versions the panel may
// pass to mise.
package tools

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/berth-ai/berth/internal/status"
)

// Tool is one mise tool and the version it is pinned to.
type Tool = status.Tool

// Preset states.
const (
	StateInstalled    = "installed"
	StateOutdated     = "outdated"
	StateNotInstalled = "not_installed"
)

// Preset is a curated set of pinned tools for the user track.
type Preset struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Tools       map[string]string `json:"tools"`
	State       string            `json:"state"`
}

var (
	toolPattern    = regexp.MustCompile(`^(?:[a-z][a-z0-9-]*:)?[A-Za-z0-9@][A-Za-z0-9@._/+-]{0,199}$`)
	versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)
	presetPattern  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
)

// ValidTool reports whether s may be passed to mise as a tool name.
func ValidTool(s string) bool {
	return toolPattern.MatchString(s) && !strings.Contains(s, "..")
}

// ValidVersion reports whether s may be passed to mise as a version.
func ValidVersion(s string) bool { return versionPattern.MatchString(s) }

// ValidPresetID reports whether s may name a preset file.
func ValidPresetID(s string) bool { return presetPattern.MatchString(s) }

// LoadBase lists the base track's [tools] from the image's mise.toml.
func LoadBase(path string) ([]Tool, error) {
	return status.UserTools(path)
}

// LoadPresets reads every <id>.toml in dir, sorted by id.
func LoadPresets(dir string) ([]Preset, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.toml"))
	if err != nil {
		return nil, err
	}
	slices.Sort(files)
	out := []Preset{}
	for _, f := range files {
		p, err := loadPreset(f)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(f), err)
		}
		out = append(out, p)
	}
	return out, nil
}

func loadPreset(path string) (Preset, error) {
	id := strings.TrimSuffix(filepath.Base(path), ".toml")
	if !ValidPresetID(id) {
		return Preset{}, fmt.Errorf("invalid preset id %q", id)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Preset{}, err
	}
	var doc struct {
		Preset struct {
			Name        string `toml:"name"`
			Description string `toml:"description"`
		} `toml:"preset"`
		Tools map[string]string `toml:"tools"`
	}
	if err := toml.Unmarshal(raw, &doc); err != nil {
		return Preset{}, err
	}
	if doc.Preset.Name == "" || doc.Preset.Description == "" {
		return Preset{}, fmt.Errorf("[preset] needs a name and a description")
	}
	if len(doc.Tools) == 0 {
		return Preset{}, fmt.Errorf("[tools] is empty")
	}
	for _, t := range slices.Sorted(maps.Keys(doc.Tools)) {
		if !ValidTool(t) || !ValidVersion(doc.Tools[t]) {
			return Preset{}, fmt.Errorf("invalid pin %s = %q", t, doc.Tools[t])
		}
	}
	return Preset{ID: id, Name: doc.Preset.Name, Description: doc.Preset.Description, Tools: doc.Tools}, nil
}

// PresetState compares a preset with the user track.
func PresetState(p Preset, user []Tool) string {
	have := map[string]string{}
	for _, t := range user {
		have[t.Name] = t.Version
	}
	state := StateInstalled
	for tool, version := range p.Tools {
		got, ok := have[tool]
		switch {
		case !ok:
			return StateNotInstalled
		case got != version:
			state = StateOutdated
		}
	}
	return state
}
