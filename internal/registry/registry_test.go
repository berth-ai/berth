package registry

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berth-ai/berth/internal/locksource"
	"github.com/berth-ai/berth/internal/testutil"
)

const acmePkg = `{"name":"x","private":true,"dependencies":{"@acme/agent":"1.2.3"},
  "berth":{"@acme/agent":{"kind":"agent","name":"Acme","description":"Acme's coding agent","keys":["ACME_TOKEN"]}},
  "berthAgents":{"schema":1,"agents":{"@acme/agent":{"skills":{"dir":"~/.acme/skills"},"icon":"acme"}}}}`

const twoPkg = `{"dependencies":{"a":"1.0.0","b":"1.0.0"},
  "berth":{"a":{"kind":"agent","name":"A","keys":[]},"b":{"kind":"agent","name":"B","keys":[]}}}`

func TestLoad(t *testing.T) {
	root := t.TempDir()
	testutil.RegistryEntry(t, root, "acme", acmePkg)
	testutil.RegistryEntry(t, root, "copilot", testutil.CopilotPackageJSON)
	entries, errs := Load(root)
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if len(entries) != 2 || entries[0].ID != "acme" || entries[1].ID != "copilot" {
		t.Fatalf("entries = %+v", entries)
	}
	e := entries[0]
	if e.Package != "@acme/agent" || e.Version != "1.2.3" || e.Meta.Name != "Acme" ||
		e.Meta.Description != "Acme's coding agent" || e.Agent.Icon != "acme" ||
		e.Lock == nil || e.Lock.Source.Origin != locksource.OriginImage {
		t.Errorf("entry = %+v", e)
	}
	if got, ok := ByID(entries, "copilot"); !ok || got.Package != "@github/copilot" || got.Agent.Login == nil {
		t.Errorf("ByID = %+v %v", got, ok)
	}
	if got, ok := ByPackage(entries, "@acme/agent"); !ok || got.ID != "acme" {
		t.Errorf("ByPackage = %+v %v", got, ok)
	}
	if _, ok := ByID(entries, "nope"); ok {
		t.Error("ByID found nope")
	}
}

func TestLoadMissingRoot(t *testing.T) {
	entries, errs := Load(filepath.Join(t.TempDir(), "none"))
	if entries != nil || len(errs) != 0 {
		t.Errorf("entries = %v, errs = %v", entries, errs)
	}
}

func TestLoadRejectsBadEntries(t *testing.T) {
	root := t.TempDir()
	testutil.RegistryEntry(t, root, "acme", acmePkg)
	bad := map[string]string{
		"Bad_ID":   acmePkg,
		"two":      twoPkg,
		"none":     `{"dependencies":{}}`,
		"server":   `{"dependencies":{"m":"1.0.0"},"berth":{"m":{"kind":"mcp","name":"M","keys":[]}}}`,
		"badicon":  strings.Replace(acmePkg, `"icon":"acme"`, `"icon":"Bad Icon"`, 1),
		"longdesc": strings.Replace(acmePkg, "Acme's coding agent", strings.Repeat("x", 121), 1),
	}
	for id, pkg := range bad {
		testutil.RegistryEntry(t, root, id, pkg)
	}
	entries, errs := Load(root)
	if len(entries) != 1 || entries[0].ID != "acme" {
		t.Errorf("entries = %+v", entries)
	}
	for id := range bad {
		if errs[id] == nil {
			t.Errorf("%s was not rejected", id)
		}
	}
}

func TestLoadLogged(t *testing.T) {
	root := t.TempDir()
	testutil.RegistryEntry(t, root, "two", twoPkg)
	var log bytes.Buffer
	if got := LoadLogged(root, &log); len(got) != 0 {
		t.Errorf("entries = %+v", got)
	}
	if !strings.Contains(log.String(), "registry entry two left out") {
		t.Errorf("log = %q", log.String())
	}
}
