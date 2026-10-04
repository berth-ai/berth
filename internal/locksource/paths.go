package locksource

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HomeDirs expands descriptor paths for one user.
type HomeDirs struct {
	Home       string
	ConfigHome string // $XDG_CONFIG_HOME; empty or relative means Home/.config
	DataHome   string // $XDG_DATA_HOME; empty or relative means Home/.local/share
}

// Expand returns the absolute path of a descriptor path; ParseDescriptors
// guarantees it starts with ~/, $XDG_CONFIG_HOME/ or $XDG_DATA_HOME/.
func (h HomeDirs) Expand(p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		return filepath.Join(h.Home, filepath.FromSlash(rest))
	}
	if rest, ok := strings.CutPrefix(p, "$XDG_DATA_HOME/"); ok {
		return filepath.Join(xdgBase(h.DataHome, h.Home, ".local", "share"), filepath.FromSlash(rest))
	}
	rest := strings.TrimPrefix(p, "$XDG_CONFIG_HOME/")
	return filepath.Join(xdgBase(h.ConfigHome, h.Home, ".config"), filepath.FromSlash(rest))
}

// xdgBase is dir when it is absolute (the XDG rule), else home joined with
// the default.
func xdgBase(dir, home string, def ...string) string {
	if filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(append([]string{home}, def...)...)
}

// ReadInstalled reads the package.json of an installed agent set in dir.
func ReadInstalled(dir string) (map[string]Meta, Descriptors, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return nil, Descriptors{}, err
	}
	var p struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, Descriptors{}, fmt.Errorf("package.json: %w", err)
	}
	meta, err := ParseMeta(raw, p.Dependencies)
	if err != nil {
		return nil, Descriptors{}, err
	}
	desc, err := ParseDescriptors(raw, meta)
	return meta, desc, err
}
