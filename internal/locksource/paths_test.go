package locksource

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHomeDirsExpand(t *testing.T) {
	h := HomeDirs{Home: "/home/agent"}
	for in, want := range map[string]string{
		"~/.claude.json":                    "/home/agent/.claude.json",
		"$XDG_CONFIG_HOME/opencode/x.json":  "/home/agent/.config/opencode/x.json",
		"$XDG_DATA_HOME/opencode/auth.json": "/home/agent/.local/share/opencode/auth.json",
	} {
		if got := h.Expand(in); got != want {
			t.Errorf("Expand(%q) = %q, want %q", in, got, want)
		}
	}
	h = HomeDirs{Home: "/home/agent", ConfigHome: "/cfg", DataHome: "/data"}
	if got := h.Expand("$XDG_DATA_HOME/a"); got != "/data/a" {
		t.Errorf("custom data home = %q", got)
	}
	if got := h.Expand("$XDG_CONFIG_HOME/a"); got != "/cfg/a" {
		t.Errorf("custom config home = %q", got)
	}
	h = HomeDirs{Home: "/home/agent", ConfigHome: "rel", DataHome: "rel"}
	if got := h.Expand("$XDG_DATA_HOME/a"); got != "/home/agent/.local/share/a" {
		t.Errorf("relative data home = %q", got)
	}
}

func TestReadInstalled(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), loginPkg(goodLogin, "claude"), 0o644)
	meta, desc, err := ReadInstalled(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta["a"].Name != "A" || desc.Agents["a"].Login == nil {
		t.Errorf("meta = %+v, desc = %+v", meta, desc)
	}
	if _, _, err := ReadInstalled(t.TempDir()); err == nil {
		t.Error("missing package.json accepted")
	}
}
