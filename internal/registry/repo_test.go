package registry

import (
	"encoding/json"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"testing"

	"github.com/berth-ai/berth/internal/locksource"
)

// TestRepositoryRegistryHasNoCollisions loads agents/ and agents/registry/
// from this repository: every entry must load, and no package, bin or API
// key may appear twice across the main lock and the entries.
func TestRepositoryRegistryHasNoCollisions(t *testing.T) {
	agents := filepath.Join("..", "..", "agents")
	main, err := locksource.FromDir(agents, locksource.Source{Origin: locksource.OriginImage})
	if err != nil {
		t.Fatal(err)
	}
	entries, errs := Load(filepath.Join(agents, "registry"))
	if len(errs) != 0 {
		t.Fatalf("entries rejected: %v", errs)
	}
	if len(entries) == 0 {
		t.Fatal("the repository registry is empty")
	}
	seen := map[string]map[string]string{"package": {}, "bin": {}, "key": {}}
	claim := func(kind, name, owner string) {
		if prev, ok := seen[kind][name]; ok {
			t.Errorf("%s %s is in both %s and %s", kind, name, prev, owner)
		}
		seen[kind][name] = owner
	}
	add := func(owner string, lock *locksource.Lock) {
		for _, dep := range slices.Sorted(maps.Keys(lock.Deps)) {
			claim("package", dep, owner)
			for _, b := range lockBins(t, lock.PackageLock, dep) {
				claim("bin", b, owner)
			}
			for _, k := range lock.Meta[dep].Keys {
				claim("key", k, owner)
			}
		}
	}
	add("the main lock", main)
	for _, e := range entries {
		add("registry entry "+e.ID, e.Lock)
	}
}

// lockBins reads the bins dep declares from its package-lock.json entry.
func lockBins(t *testing.T, lock []byte, dep string) []string {
	t.Helper()
	var l struct {
		Packages map[string]struct {
			Bin json.RawMessage `json:"bin"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(lock, &l); err != nil {
		t.Fatal(err)
	}
	raw := l.Packages["node_modules/"+dep].Bin
	if len(raw) == 0 {
		return nil
	}
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{path.Base(dep)}
	}
	var many map[string]string
	if err := json.Unmarshal(raw, &many); err != nil {
		t.Fatalf("%s bin: %v", dep, err)
	}
	return slices.Sorted(maps.Keys(many))
}
