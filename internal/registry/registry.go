// Package registry reads the curated agents a user may add, shipped in the
// image at /usr/share/berth/registry: one folder per entry, named by its
// id, holding a one-package agent lock (package.json and
// package-lock.json). Collisions with the main lock are checked by a
// repository test, not at runtime.
package registry

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/berth-ai/berth/internal/locksource"
)

// DefaultRoot is where the image ships the registry.
const DefaultRoot = "/usr/share/berth/registry"

// IDPattern is the shape of a registry id, which names the entry's folder
// and its store in agent's home.
var IDPattern = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

// Entry is one agent a user may add.
type Entry struct {
	ID      string
	Package string
	Version string // the pinned version of Package
	Meta    locksource.Meta
	Agent   locksource.AgentDescriptor
	Lock    *locksource.Lock
}

// Load reads every entry under root, sorted by id. A missing root is an
// empty registry. An entry that fails validation is left out and returned
// in errs, keyed by folder name; the rest still load.
func Load(root string) ([]Entry, map[string]error) {
	errs := map[string]error{}
	dirs, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, errs
	}
	if err != nil {
		errs["."] = err
		return nil, errs
	}
	var out []Entry
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		e, err := load(filepath.Join(root, d.Name()), d.Name())
		if err != nil {
			errs[d.Name()] = err
			continue
		}
		out = append(out, e)
	}
	return out, errs
}

func load(dir, id string) (Entry, error) {
	if !IDPattern.MatchString(id) {
		return Entry{}, fmt.Errorf("id %q must match %s", id, IDPattern)
	}
	lock, err := locksource.FromDir(dir, locksource.Source{Origin: locksource.OriginImage})
	if err != nil {
		return Entry{}, err
	}
	if len(lock.Deps) != 1 {
		return Entry{}, fmt.Errorf("package.json must have exactly one dependency, not %d", len(lock.Deps))
	}
	pkg := slices.Collect(maps.Keys(lock.Deps))[0]
	m := lock.Meta[pkg]
	if m.Kind != locksource.KindAgent {
		return Entry{}, fmt.Errorf("%s has kind %q, want agent", pkg, m.Kind)
	}
	return Entry{ID: id, Package: pkg, Version: lock.Deps[pkg], Meta: m, Agent: lock.Descriptors.Agents[pkg], Lock: lock}, nil
}

// LoadLogged is Load with every rejected entry written to log.
func LoadLogged(root string, log io.Writer) []Entry {
	entries, errs := Load(root)
	for _, name := range slices.Sorted(maps.Keys(errs)) {
		fmt.Fprintf(log, "berth: registry entry %s left out: %v\n", name, errs[name])
	}
	return entries
}

// ByID finds the entry with id.
func ByID(entries []Entry, id string) (Entry, bool) {
	i := slices.IndexFunc(entries, func(e Entry) bool { return e.ID == id })
	if i < 0 {
		return Entry{}, false
	}
	return entries[i], true
}

// ByPackage finds the entry that installs pkg.
func ByPackage(entries []Entry, pkg string) (Entry, bool) {
	i := slices.IndexFunc(entries, func(e Entry) bool { return e.Package == pkg })
	if i < 0 {
		return Entry{}, false
	}
	return entries[i], true
}
