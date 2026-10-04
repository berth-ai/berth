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

	"github.com/berth-ai/berth/internal/agentset"
	"github.com/berth-ai/berth/internal/agentstore"
	"github.com/berth-ai/berth/internal/testutil"
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
		"berthAgents": map[string]any{
			"schema":  1,
			"servers": map[string]any{"mcp-b": map[string]any{"id": "bee", "bin": "b-mcp", "icon": "upstash"}},
			"agents":  map[string]any{"agent-a": map[string]any{"skills": map[string]any{"dir": "~/.x/skills"}, "icon": "claude"}},
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
	pkgs, err := b.Packages([]string{"mcp-b"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 2 || pkgs[0].Origin != "locked" || pkgs[0].Package != "agent-a" || !pkgs[0].Enabled || pkgs[1].Enabled ||
		pkgs[0].Name != "Agent A" || pkgs[0].Version != "1.0.0" || !slices.Equal(pkgs[0].Bins, []string{"a"}) ||
		len(pkgs[0].Keys) != 1 || !pkgs[0].Keys[0].Set || pkgs[1].Kind != "mcp" {
		t.Errorf("packages = %+v", pkgs)
	}
	if pkgs[1].MCPID != "bee" || pkgs[0].MCPID != "" {
		t.Errorf("mcp ids = %q, %q", pkgs[0].MCPID, pkgs[1].MCPID)
	}
	if p := pkgs[0]; p.SkillsDir != "~/.x/skills" || p.SkillsVia != nil {
		t.Errorf("skills fields = %q %v", p.SkillsDir, p.SkillsVia)
	}
	if keys, _ := b.KeyNames(nil); !slices.Equal(keys, []string{"A_KEY"}) {
		t.Errorf("keys = %v", keys)
	}
	if pkgs[0].Icon != "claude" || pkgs[1].Icon != "upstash" {
		t.Errorf("icons = %q, %q", pkgs[0].Icon, pkgs[1].Icon)
	}
	empty := Builder{Root: t.TempDir(), Store: agentstore.Store{Root: t.TempDir()}}
	if pkgs, err := empty.Packages(nil, nil); err != nil || pkgs == nil || len(pkgs) != 0 {
		t.Errorf("no current = %v %v", pkgs, err)
	}
}

func TestBuildWritesEnabledWrappers(t *testing.T) {
	b, berth := fakeSet(t)
	WriteKeys(b.KeysPath, map[string]string{"A_KEY": "secret"}, -1, -1)
	if _, err := b.Build(nil, nil); err != nil {
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
	if _, err := b.Build([]string{"agent-a"}, nil); err != nil {
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
	b.Build(nil, nil)
	first, _ := os.Readlink(filepath.Join(berth, "bin"))
	wrapper, _ := os.ReadFile(filepath.Join(berth, first, "a"))
	b.Build(nil, nil)
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
	if _, err := b.Build(nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := names(filepath.Join(b.Root, "bin")); len(got) != 0 {
		t.Errorf("bin = %v", got)
	}
}

// copilotReport reports an installed Copilot with a rollback version, as
// berth agents status would, and errs as the last sync's failures.
func copilotReport(errs map[string]string) func() agentset.Report {
	return func() agentset.Report {
		return agentset.Report{Agents: []agentset.Added{{ID: "copilot", Package: "@github/copilot", Version: "1.0.91",
			Hash: "0123456789ab", PreviousVersion: "1.0.90", PackageJSON: []byte(testutil.CopilotPackageJSON),
			Bins: []string{"copilot"}}}, Errors: errs}
	}
}

func findPkg(pkgs []Package, name string) (Package, bool) {
	i := slices.IndexFunc(pkgs, func(p Package) bool { return p.Package == name })
	if i < 0 {
		return Package{}, false
	}
	return pkgs[i], true
}

func TestPackagesIncludesAddedAgents(t *testing.T) {
	b, _ := fakeSet(t)
	b.AddedRoot = "/home/agent/.local/share/berth/agents"
	b.Added = copilotReport(map[string]string{"@github/copilot": "updating to 1.0.92 failed"})
	pkgs, err := b.Packages(nil, []string{"@github/copilot"})
	if err != nil {
		t.Fatal(err)
	}
	c, ok := findPkg(pkgs, "@github/copilot")
	if !ok || c.Origin != "added" || c.Kind != "agent" || c.Name != "GitHub Copilot CLI" || c.Version != "1.0.91" ||
		c.Previous != "1.0.90" || c.Error != "updating to 1.0.92 failed" || c.Icon != "githubcopilot" ||
		c.SkillsDir != "~/.copilot/skills" || !c.Enabled || !slices.Equal(c.Bins, []string{"copilot"}) ||
		len(c.Keys) != 1 || c.Keys[0].Name != "COPILOT_GITHUB_TOKEN" {
		t.Errorf("copilot = %+v", c)
	}
	if a, _ := findPkg(pkgs, "agent-a"); a.Origin != "locked" || pkgs[0].Package != "agent-a" {
		t.Errorf("agents first by name: %+v", pkgs)
	}
	if keys, _ := b.KeyNames([]string{"@github/copilot"}); !slices.Equal(keys, []string{"A_KEY", "COPILOT_GITHUB_TOKEN"}) {
		t.Errorf("keys = %v", keys)
	}
	pkgs, _ = b.Packages(nil, nil)
	if _, ok := findPkg(pkgs, "@github/copilot"); ok {
		t.Error("an install not in agents.added is listed")
	}
}

func TestPackagesMissingAddedAgent(t *testing.T) {
	b, _ := fakeSet(t)
	reg := t.TempDir()
	testutil.RegistryEntry(t, reg, "copilot", testutil.CopilotPackageJSON)
	b.RegistryRoot = reg
	for name, report := range map[string]func() agentset.Report{
		"empty report": func() agentset.Report { return agentset.Report{} },
		"no reporter":  nil,
	} {
		b.Added = report
		pkgs, err := b.Packages([]string{"@github/copilot"}, []string{"@github/copilot"})
		if err != nil {
			t.Fatal(err)
		}
		c, ok := findPkg(pkgs, "@github/copilot")
		if !ok || c.Name != "GitHub Copilot CLI" || c.Icon != "githubcopilot" || c.Origin != "added" || c.Enabled ||
			!strings.HasPrefix(c.Error, "Not installed yet") || c.Bins == nil || c.Keys == nil {
			t.Errorf("%s: copilot = %+v", name, c)
		}
	}
}

func TestBuildWrapsAddedAgent(t *testing.T) {
	b, berth := fakeSet(t)
	b.AddedRoot = filepath.Join(t.TempDir(), "agents")
	b.Added = copilotReport(nil)
	added := []string{"@github/copilot"}
	if _, err := b.Build(nil, added); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(berth, "bin", "copilot"))
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(b.AddedRoot, "copilot", "current", "bin", "copilot")
	if !strings.Contains(string(raw), "exec "+target) || !strings.Contains(string(raw), "COPILOT_GITHUB_TOKEN") {
		t.Errorf("wrapper:\n%s", raw)
	}
	if _, err := b.Build([]string{"@github/copilot"}, added); err != nil {
		t.Fatal(err)
	}
	if got := names(filepath.Join(berth, "bin")); !slices.Equal(got, []string{"a", "b-mcp"}) {
		t.Errorf("after switching Copilot off = %v", got)
	}
}

// TestBuildRefusesHostileReport: bins from agent's report never name a file
// outside the layer and never replace a locked wrapper.
func TestBuildRefusesHostileReport(t *testing.T) {
	b, berth := fakeSet(t)
	b.AddedRoot = t.TempDir()
	b.Added = func() agentset.Report {
		rep := copilotReport(nil)()
		rep.Agents[0].Bins = []string{"../escape", "a"}
		return rep
	}
	pkgs, err := b.Build(nil, []string{"@github/copilot"})
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := findPkg(pkgs, "@github/copilot"); c.Error == "" || len(c.Bins) != 0 {
		t.Errorf("copilot = %+v", c)
	}
	if got := names(filepath.Join(berth, "bin")); !slices.Equal(got, []string{"a", "b-mcp"}) {
		t.Errorf("bin = %v", got)
	}
	for _, p := range []string{filepath.Join(berth, "escape"), filepath.Join(berth, "layers", "escape")} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("%s was written", p)
		}
	}
	raw, _ := os.ReadFile(filepath.Join(berth, "bin", "a"))
	if !strings.Contains(string(raw), filepath.Join(b.Store.Root, "current", "bin", "a")) {
		t.Errorf("a no longer runs the locked bin:\n%s", raw)
	}
}
