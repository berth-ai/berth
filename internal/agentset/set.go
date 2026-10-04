// Package agentset merges the locked agent set with the agents a user added
// from the image's registry into one view: packages, descriptors and bins.
// The wrapper layer, MCP and skills sync, and the agents' sign-ins all read
// it, so an added agent gets everything a locked one does.
//
// Root never opens a file in agent's home: the root side builds a Set from
// the Report that berth agents status prints as agent, and Load reads only
// the locked set's root-owned files.
package agentset

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/berth-ai/berth/internal/agentstore"
	"github.com/berth-ai/berth/internal/locksource"
	"github.com/berth-ai/berth/internal/registry"
)

// Origins of a package.
const (
	OriginLocked = "locked"
	OriginAdded  = "added"
)

// Locked is the installed locked set: Dir is its version directory, Link
// the store's current link that wrappers exec through.
type Locked struct {
	Dir      string
	Link     string
	Versions map[string]string // package → version, from .complete
}

// FromStore returns s's current set, or nil when none is installed.
func FromStore(s agentstore.Store) (*Locked, error) {
	cur, err := s.Current()
	if err != nil || cur == nil {
		return nil, err
	}
	return &Locked{Dir: cur.Dir, Link: filepath.Join(s.Root, "current"), Versions: cur.Manifest.Packages}, nil
}

// Added is one added agent's current install, as berth agents status
// reports it.
type Added struct {
	ID              string   `json:"id"`
	Package         string   `json:"package"`
	Version         string   `json:"version"`
	Hash            string   `json:"hash"`
	PreviousVersion string   `json:"previous_version,omitempty"`
	PackageJSON     []byte   `json:"package_json"`
	Bins            []string `json:"bins"`
}

// Report is berth agents status --json: the installs in agent's home and
// the last sync's failures, by package.
type Report struct {
	Agents []Added           `json:"agents"`
	Errors map[string]string `json:"errors"`
}

// Package is one package of the merged set.
type Package struct {
	Name            string
	Meta            locksource.Meta
	Origin          string
	ID              string // the registry id; added agents only
	Version         string
	PreviousVersion string   // the rollback target; added agents only
	Bins            []string // sorted
	Link            string   // the bins are Link/bin/<bin>
}

// Set is the merged agent set.
type Set struct {
	Packages    map[string]Package
	Descriptors locksource.Descriptors
	Errors      map[string]string // added agents left out, by package
}

// Load merges locked (nil when nothing is installed) with added, whose
// installs sit under addedRoot/<id>/current. A locked set that does not
// parse is an error. An added agent that does not parse, is not an agent,
// or claims a package, bin or API key the set already has is left out and
// named in Errors.
func Load(locked *Locked, addedRoot string, added []Added) (Set, error) {
	s := Set{
		Packages:    map[string]Package{},
		Descriptors: locksource.Descriptors{Agents: map[string]locksource.AgentDescriptor{}, Servers: map[string]locksource.ServerDescriptor{}},
		Errors:      map[string]string{},
	}
	if locked != nil {
		if err := s.addLocked(locked); err != nil {
			return Set{}, err
		}
	}
	sorted := slices.SortedFunc(slices.Values(added), func(a, b Added) int { return cmp.Compare(a.ID, b.ID) })
	for _, a := range sorted {
		if err := s.addAdded(addedRoot, a); err != nil {
			s.Errors[a.Package] = err.Error()
		}
	}
	return s, nil
}

func (s *Set) addLocked(l *Locked) error {
	raw, err := os.ReadFile(filepath.Join(l.Dir, "package.json"))
	if err != nil {
		return err
	}
	deps, meta, desc, err := parse(raw)
	if err != nil {
		return err
	}
	for _, dep := range slices.Sorted(maps.Keys(deps)) {
		bins, err := agentstore.PackageBins(l.Dir, dep)
		if err != nil {
			return err
		}
		s.Packages[dep] = Package{Name: dep, Meta: meta[dep], Origin: OriginLocked, Version: l.Versions[dep],
			Bins: slices.Sorted(maps.Keys(bins)), Link: l.Link}
	}
	maps.Copy(s.Descriptors.Agents, desc.Agents)
	maps.Copy(s.Descriptors.Servers, desc.Servers)
	return nil
}

func (s *Set) addAdded(root string, a Added) error {
	if !registry.IDPattern.MatchString(a.ID) {
		return fmt.Errorf("invalid registry id %q", a.ID)
	}
	deps, meta, desc, err := parse(a.PackageJSON)
	if err != nil {
		return err
	}
	if _, ok := deps[a.Package]; !ok || len(deps) != 1 {
		return fmt.Errorf("its lock does not hold exactly %s", a.Package)
	}
	m := meta[a.Package]
	if m.Kind != locksource.KindAgent {
		return fmt.Errorf("%s is not an agent", a.Package)
	}
	if p, ok := s.Packages[a.Package]; ok {
		return fmt.Errorf("%s is already in the %s set", a.Package, p.Origin)
	}
	for _, bin := range a.Bins {
		if !locksource.ValidBinName(bin) {
			return fmt.Errorf("bin %q is not a bin name", bin)
		}
		if owner := s.owner(func(p Package) bool { return slices.Contains(p.Bins, bin) }); owner != "" {
			return fmt.Errorf("its bin %s is also %s's", bin, owner)
		}
	}
	for _, k := range m.Keys {
		if owner := s.owner(func(p Package) bool { return slices.Contains(p.Meta.Keys, k) }); owner != "" {
			return fmt.Errorf("its key %s is also %s's", k, owner)
		}
	}
	s.Packages[a.Package] = Package{Name: a.Package, Meta: m, Origin: OriginAdded, ID: a.ID, Version: a.Version,
		PreviousVersion: a.PreviousVersion, Bins: slices.Sorted(slices.Values(a.Bins)), Link: filepath.Join(root, a.ID, "current")}
	if d, ok := desc.Agents[a.Package]; ok {
		s.Descriptors.Agents[a.Package] = d
	}
	return nil
}

// owner is the first package, by name, that has.
func (s *Set) owner(has func(Package) bool) string {
	for _, name := range s.Names() {
		if has(s.Packages[name]) {
			return name
		}
	}
	return ""
}

func parse(raw []byte) (map[string]string, map[string]locksource.Meta, locksource.Descriptors, error) {
	var p struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, nil, locksource.Descriptors{}, fmt.Errorf("package.json: %w", err)
	}
	if len(p.Dependencies) == 0 {
		return nil, nil, locksource.Descriptors{}, errors.New("package.json has no dependencies")
	}
	meta, err := locksource.ParseMeta(raw, p.Dependencies)
	if err != nil {
		return nil, nil, locksource.Descriptors{}, err
	}
	desc, err := locksource.ParseDescriptors(raw, meta)
	return p.Dependencies, meta, desc, err
}

// Names lists the packages, sorted.
func (s Set) Names() []string { return slices.Sorted(maps.Keys(s.Packages)) }

// EnabledBin is the first of pkg's bins, sorted, with an executable
// wrapper in layer, which is how Berth marks a package enabled; "" when
// pkg is switched off or unknown.
func (s Set) EnabledBin(layer, pkg string) string {
	for _, b := range s.Packages[pkg].Bins {
		if fi, err := os.Stat(filepath.Join(layer, b)); err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0 {
			return b
		}
	}
	return ""
}

// Enabled reports whether pkg has a wrapper in layer.
func (s Set) Enabled(layer, pkg string) bool { return s.EnabledBin(layer, pkg) != "" }
