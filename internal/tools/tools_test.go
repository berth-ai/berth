package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRepoPresetsAndBase(t *testing.T) {
	presets, err := LoadPresets("../../tools/presets")
	if err != nil {
		t.Fatal(err)
	}
	if len(presets) != 4 || presets[0].ID != "go" || presets[0].Name != "Go" || presets[0].Tools["go"] == "" {
		t.Errorf("presets = %+v", presets)
	}
	base, err := LoadBase("../../tools/mise.toml")
	if err != nil || len(base) < 25 {
		t.Errorf("base = %d %v", len(base), err)
	}
}

func TestLoadPresetsRejectsBadFiles(t *testing.T) {
	for name, body := range map[string]string{
		"Bad.toml":    "[preset]\nname='x'\ndescription='y'\n[tools]\ngo='1.0.0'\n",
		"empty.toml":  "[preset]\nname='x'\ndescription='y'\n[tools]\n",
		"noname.toml": "[preset]\ndescription='y'\n[tools]\ngo='1.0.0'\n",
		"bad.toml":    "[preset]\nname='x'\ndescription='y'\n[tools]\ngo='--x'\n",
	} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644)
		if _, err := LoadPresets(dir); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestPresetState(t *testing.T) {
	p := Preset{Tools: map[string]string{"go": "1.27.1", "gopls": "0.20.0"}}
	for _, c := range []struct {
		user []Tool
		want string
	}{
		{nil, StateNotInstalled},
		{[]Tool{{Name: "go", Version: "1.27.1"}}, StateNotInstalled},
		{[]Tool{{Name: "go", Version: "1.27.1"}, {Name: "gopls", Version: "0.20.0"}}, StateInstalled},
		{[]Tool{{Name: "go", Version: "1.26.0"}, {Name: "gopls", Version: "0.20.0"}}, StateOutdated},
	} {
		if got := PresetState(p, c.user); got != c.want {
			t.Errorf("%v: %s, want %s", c.user, got, c.want)
		}
	}
}

func TestToolArgumentsRejected(t *testing.T) {
	for s, ok := range map[string]bool{
		"jq": true, "node": true, "aqua:jqlang/jq": true, "npm:@biomejs/biome": true, "go:golang.org/x/tools/gopls": true,
		"": false, "-x": false, "--help": false, "a b": false, "jq;rm": false, "../x": false, "a/../b": false, "JQ$": false,
	} {
		if ValidTool(s) != ok {
			t.Errorf("ValidTool(%q) = %v", s, !ok)
		}
	}
	for s, ok := range map[string]bool{
		"1.8.2": true, "latest": true, "temurin-25.0.4+7.0.LTS": true, "3.14": true, "lts": true,
		"": false, "-1": false, "1;rm": false, "1 2": false, "1/2": false,
	} {
		if ValidVersion(s) != ok {
			t.Errorf("ValidVersion(%q) = %v", s, !ok)
		}
	}
	for s, ok := range map[string]bool{"go": true, "python3": true, "Go": false, "-go": false, "../go": false, "": false} {
		if ValidPresetID(s) != ok {
			t.Errorf("ValidPresetID(%q) = %v", s, !ok)
		}
	}
}
