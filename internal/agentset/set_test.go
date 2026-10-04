package agentset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/berth-ai/berth/internal/testutil"
)

const addedRoot = "/home/agent/.local/share/berth/agents"

// lockedFixture is a locked set with agent-a (bin a, key A_KEY, a skills
// directory) and mcp-b (bin b-mcp, server id bee).
func lockedFixture(t *testing.T) *Locked {
	t.Helper()
	dir := t.TempDir()
	raw, _ := json.Marshal(map[string]any{
		"dependencies": map[string]string{"agent-a": "1.0.0", "mcp-b": "2.0.0"},
		"berth": map[string]any{
			"agent-a": map[string]any{"kind": "agent", "name": "Agent A", "keys": []string{"A_KEY"}},
			"mcp-b":   map[string]any{"kind": "mcp", "name": "B", "keys": []string{}},
		},
		"berthAgents": map[string]any{
			"schema":  1,
			"agents":  map[string]any{"agent-a": map[string]any{"skills": map[string]any{"dir": "~/.a/skills"}}},
			"servers": map[string]any{"mcp-b": map[string]any{"id": "bee", "bin": "b-mcp"}},
		},
	})
	os.WriteFile(filepath.Join(dir, "package.json"), raw, 0o644)
	for dep, bin := range map[string]string{"agent-a": "a", "mcp-b": "b-mcp"} {
		d := filepath.Join(dir, "node_modules", dep)
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, "package.json"), []byte(`{"name":"`+dep+`","bin":{"`+bin+`":"cli.js"}}`), 0o644)
	}
	return &Locked{Dir: dir, Link: "/opt/agents/current", Versions: map[string]string{"agent-a": "1.0.0", "mcp-b": "2.0.0"}}
}

// added is a Copilot-shaped added agent renamed to pkg, with key and bin.
func added(id, pkg, key, bin string) Added {
	r := strings.NewReplacer("@github/copilot", pkg, "COPILOT_GITHUB_TOKEN", key)
	return Added{ID: id, Package: pkg, Version: "1.0.91", Hash: "0123456789ab", PreviousVersion: "1.0.90",
		PackageJSON: []byte(r.Replace(testutil.CopilotPackageJSON)), Bins: []string{bin}}
}

func copilot() Added { return added("copilot", "@github/copilot", "COPILOT_GITHUB_TOKEN", "copilot") }

func TestLoadMergesLockedAndAdded(t *testing.T) {
	set, err := Load(lockedFixture(t), addedRoot, []Added{copilot()})
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Errors) != 0 {
		t.Fatalf("errors = %v", set.Errors)
	}
	c := set.Packages["@github/copilot"]
	if c.Origin != OriginAdded || c.ID != "copilot" || c.Version != "1.0.91" || c.PreviousVersion != "1.0.90" ||
		!slices.Equal(c.Bins, []string{"copilot"}) || c.Link != filepath.Join(addedRoot, "copilot", "current") ||
		c.Meta.Name != "GitHub Copilot CLI" || !slices.Equal(c.Meta.Keys, []string{"COPILOT_GITHUB_TOKEN"}) {
		t.Errorf("copilot = %+v", c)
	}
	a := set.Packages["agent-a"]
	if a.Origin != OriginLocked || a.Version != "1.0.0" || !slices.Equal(a.Bins, []string{"a"}) || a.Link != "/opt/agents/current" {
		t.Errorf("agent-a = %+v", a)
	}
	if set.Descriptors.Agents["@github/copilot"].Login == nil || set.Descriptors.Agents["agent-a"].Skills == nil ||
		set.Descriptors.Servers["mcp-b"].ID != "bee" {
		t.Errorf("descriptors = %+v", set.Descriptors)
	}
	if got := set.Names(); !slices.Equal(got, []string{"@github/copilot", "agent-a", "mcp-b"}) {
		t.Errorf("names = %v", got)
	}
}

func TestLoadIsolatesBadAddedAgents(t *testing.T) {
	unparsable := copilot()
	unparsable.PackageJSON = []byte("{not json")
	wrongPkg := copilot()
	wrongPkg.Package = "@github/other"
	badID := copilot()
	badID.ID = "Bad"
	for name, a := range map[string]Added{
		"unparsable":    unparsable,
		"other package": wrongPkg,
		"bad id":        badID,
		"package clash": added("a", "agent-a", "OTHER_KEY", "x"),
		"key clash":     added("k", "@acme/k", "A_KEY", "k"),
	} {
		t.Run(name, func(t *testing.T) {
			good := added("acme", "@acme/agent", "ACME_KEY", "acme")
			set, err := Load(lockedFixture(t), addedRoot, []Added{a, good})
			if err != nil {
				t.Fatal(err)
			}
			if set.Errors[a.Package] == "" {
				t.Errorf("no error for %s: %v", a.Package, set.Errors)
			}
			if p, ok := set.Packages[a.Package]; ok && p.Origin == OriginAdded {
				t.Errorf("%s was loaded", a.Package)
			}
			if _, ok := set.Packages["@acme/agent"]; !ok {
				t.Error("the good added agent was left out too")
			}
		})
	}
}

func TestLoadRefusesBadBins(t *testing.T) {
	for _, bin := range []string{"../escape", ".hidden", "a", "b-mcp"} {
		set, err := Load(lockedFixture(t), addedRoot, []Added{added("x", "@acme/x", "X_KEY", bin)})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := set.Packages["@acme/x"]; ok || set.Errors["@acme/x"] == "" {
			t.Errorf("bin %q: packages %v errors %v", bin, set.Names(), set.Errors)
		}
	}
	set, _ := Load(nil, addedRoot, []Added{added("one", "@acme/one", "ONE", "same"), added("two", "@acme/two", "TWO", "same")})
	if _, ok := set.Packages["@acme/one"]; !ok || set.Errors["@acme/two"] == "" {
		t.Errorf("shared bin: packages %v errors %v", set.Names(), set.Errors)
	}
}

func TestLoadFailsOnBadMainLock(t *testing.T) {
	l := lockedFixture(t)
	os.WriteFile(filepath.Join(l.Dir, "package.json"), []byte("{"), 0o644)
	if _, err := Load(l, addedRoot, nil); err == nil {
		t.Error("a broken main lock loaded")
	}
}

func TestLoadWithoutLockedSet(t *testing.T) {
	set, err := Load(nil, addedRoot, []Added{copilot()})
	if err != nil || !slices.Equal(set.Names(), []string{"@github/copilot"}) {
		t.Errorf("names = %v, %v", set.Names(), err)
	}
}

func TestEnabledBin(t *testing.T) {
	set, _ := Load(lockedFixture(t), addedRoot, []Added{copilot()})
	layer := t.TempDir()
	os.WriteFile(filepath.Join(layer, "copilot"), []byte("#!/bin/sh\n"), 0o755)
	os.WriteFile(filepath.Join(layer, "a"), []byte("#!/bin/sh\n"), 0o644) // not executable: off
	if got := set.EnabledBin(layer, "@github/copilot"); got != "copilot" || !set.Enabled(layer, "@github/copilot") {
		t.Errorf("copilot: %q", got)
	}
	if set.Enabled(layer, "agent-a") || set.Enabled(layer, "nope") {
		t.Error("agent-a or an unknown package counts as enabled")
	}
}
