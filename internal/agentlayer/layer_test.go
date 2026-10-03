package agentlayer

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/berth-ai/bert/internal/agentstore"
	"github.com/berth-ai/bert/internal/testutil"
)

// fakeSet makes a current version with two packages and bins: agent-a
// (bin "a", key A_KEY) and mcp-b (bin "b-mcp").
func fakeSet(t *testing.T) (Builder, string) {
	t.Helper()
	root := t.TempDir()
	agents := filepath.Join(root, "agents")
	dir := testutil.MakeVersion(t, agents, "0123456789ab", "image", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	testutil.Link(t, agents, "current", "0123456789ab")
	pkg := map[string]any{
		"dependencies": map[string]string{"agent-a": "1.0.0", "mcp-b": "2.0.0"},
		"berth": map[string]any{
			"agent-a": map[string]any{"kind": "agent", "name": "Agent A", "keys": []string{"A_KEY"}},
			"mcp-b":   map[string]any{"kind": "mcp", "name": "B", "keys": []string{}},
		},
	}
	raw, _ := json.Marshal(pkg)
	os.WriteFile(filepath.Join(dir, "package.json"), raw, 0o644)
	for dep, bin := range map[string]string{"agent-a": "a", "mcp-b": "b-mcp"} {
		d := filepath.Join(dir, "node_modules", dep)
		os.MkdirAll(d, 0o755)
		m, _ := json.Marshal(map[string]any{"name": dep, "version": "1.0.0", "bin": map[string]string{bin: "cli.js"}})
		os.WriteFile(filepath.Join(d, "package.json"), m, 0o644)
		os.MkdirAll(filepath.Join(dir, "bin"), 0o755)
		os.WriteFile(filepath.Join(dir, "bin", bin), []byte("#!/bin/sh\necho "+bin+" \"$A_KEY\"\n"), 0o755)
	}
	// .complete lists the packages this fake set really has.
	complete, _ := json.Marshal(map[string]any{"packages": map[string]string{"agent-a": "1.0.0", "mcp-b": "2.0.0"}, "installed_at": time.Now()})
	os.WriteFile(filepath.Join(dir, ".complete"), complete, 0o644)
	berth := filepath.Join(root, "berth")
	return Builder{Root: berth, Store: agentstore.Store{Root: agents}, KeysPath: filepath.Join(berth, "secrets", "agents.env")}, berth
}

func names(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestPackages(t *testing.T) {
	b, _ := fakeSet(t)
	WriteKeys(b.KeysPath, map[string]string{"A_KEY": "k"}, -1, -1)
	pkgs, err := b.Packages([]string{"mcp-b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 2 || pkgs[0].Package != "agent-a" || !pkgs[0].Enabled || pkgs[1].Enabled ||
		pkgs[0].Name != "Agent A" || pkgs[0].Version != "1.0.0" || !slices.Equal(pkgs[0].Bins, []string{"a"}) ||
		len(pkgs[0].Keys) != 1 || !pkgs[0].Keys[0].Set || pkgs[1].Kind != "mcp" {
		t.Errorf("packages = %+v", pkgs)
	}
	if keys, _ := b.KeyNames(); !slices.Equal(keys, []string{"A_KEY"}) {
		t.Errorf("keys = %v", keys)
	}
	empty := Builder{Root: t.TempDir(), Store: agentstore.Store{Root: t.TempDir()}}
	if pkgs, err := empty.Packages(nil); err != nil || pkgs == nil || len(pkgs) != 0 {
		t.Errorf("no current = %v %v", pkgs, err)
	}
}

func TestBuildWritesEnabledWrappers(t *testing.T) {
	b, berth := fakeSet(t)
	WriteKeys(b.KeysPath, map[string]string{"A_KEY": "secret"}, -1, -1)
	if _, err := b.Build(nil); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(berth, "bin")
	if got := names(bin); !slices.Equal(got, []string{"a", "b-mcp"}) {
		t.Fatalf("bin = %v", got)
	}
	out, err := exec.Command(filepath.Join(bin, "a")).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "a secret" {
		t.Errorf("a = %q %v", out, err)
	}
	if _, err := b.Build([]string{"agent-a"}); err != nil {
		t.Fatal(err)
	}
	if got := names(bin); !slices.Equal(got, []string{"b-mcp"}) {
		t.Errorf("after disabling = %v", got)
	}
	if got := names(filepath.Join(berth, "layers")); len(got) != 1 {
		t.Errorf("old layers left: %v", got)
	}
}

func TestBuildSwapsAtomically(t *testing.T) {
	b, berth := fakeSet(t)
	b.Build(nil)
	first, _ := os.Readlink(filepath.Join(berth, "bin"))
	wrapper, _ := os.ReadFile(filepath.Join(berth, first, "a"))
	b.Build(nil)
	second, _ := os.Readlink(filepath.Join(berth, "bin"))
	if first == second || !strings.HasPrefix(second, "layers/layer-") {
		t.Errorf("links %q → %q", first, second)
	}
	if fi, err := os.Lstat(filepath.Join(berth, "bin")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("bin is not a symlink: %v %v", fi, err)
	}
	if got, _ := os.ReadFile(filepath.Join(berth, second, "a")); string(got) != string(wrapper) {
		t.Errorf("wrapper changed between builds")
	}
}

func TestBuildWithoutCurrentEmptiesLayer(t *testing.T) {
	b := Builder{Root: t.TempDir(), Store: agentstore.Store{Root: t.TempDir()}}
	if _, err := b.Build(nil); err != nil {
		t.Fatal(err)
	}
	if got := names(filepath.Join(b.Root, "bin")); len(got) != 0 {
		t.Errorf("bin = %v", got)
	}
}
