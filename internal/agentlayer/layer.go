package agentlayer

import (
	"cmp"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"syscall"

	"github.com/salgnt/berth/internal/agentstore"
	"github.com/salgnt/berth/internal/locksource"
)

// Builder builds Root/bin from the current agent set.
type Builder struct {
	Root     string // /var/lib/berth
	Store    agentstore.Store
	KeysPath string // /var/lib/berth/secrets/agents.env
}

// Key reports whether an API key has a value, never the value itself.
type Key struct {
	Name string `json:"name"`
	Set  bool   `json:"set"`
}

// Package is one package of the current set as the panel shows it.
type Package struct {
	Package string   `json:"package"`
	Name    string   `json:"name"`
	Kind    string   `json:"kind"`
	Version string   `json:"version"`
	Enabled bool     `json:"enabled"`
	Bins    []string `json:"bins"`
	Keys    []Key    `json:"keys"`
}

// Packages describes the current set with disabled applied: agents first,
// then MCP servers, each by name. It is empty when nothing is installed.
func (b Builder) Packages(disabled []string) ([]Package, error) {
	out := []Package{}
	cur, err := b.Store.Current()
	if err != nil || cur == nil {
		return out, err
	}
	raw, err := os.ReadFile(filepath.Join(cur.Dir, "package.json"))
	if err != nil {
		return nil, err
	}
	var p struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	meta, err := locksource.ParseMeta(raw, p.Dependencies)
	if err != nil {
		return nil, err
	}
	keys, err := ReadKeys(b.KeysPath)
	if err != nil {
		return nil, err
	}
	for _, dep := range slices.Sorted(maps.Keys(p.Dependencies)) {
		bins, err := agentstore.PackageBins(cur.Dir, dep)
		if err != nil {
			return nil, err
		}
		m := meta[dep]
		pkg := Package{
			Package: dep, Name: m.Name, Kind: m.Kind, Version: cur.Manifest.Packages[dep],
			Enabled: !slices.Contains(disabled, dep), Bins: slices.Sorted(maps.Keys(bins)), Keys: []Key{},
		}
		for _, k := range m.Keys {
			pkg.Keys = append(pkg.Keys, Key{Name: k, Set: keys[k] != ""})
		}
		out = append(out, pkg)
	}
	slices.SortStableFunc(out, func(a, b Package) int {
		return cmp.Or(cmp.Compare(kindOrder(a.Kind), kindOrder(b.Kind)), cmp.Compare(a.Name, b.Name))
	})
	return out, nil
}

func kindOrder(kind string) int {
	if kind == locksource.KindAgent {
		return 0
	}
	return 1
}

// KeyNames is the union of the current set's API key names, sorted.
func (b Builder) KeyNames() ([]string, error) {
	pkgs, err := b.Packages(nil)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, p := range pkgs {
		for _, k := range p.Keys {
			names = append(names, k.Name)
		}
	}
	slices.Sort(names)
	return names, nil
}

// Build writes a fresh layer for disabled and swaps Root/bin to it. A shell
// that already started a wrapper keeps running; the next invocation sees
// the new set. A file lock keeps the CLI and the supervisor from building
// at once.
func (b Builder) Build(disabled []string) ([]Package, error) {
	layers := filepath.Join(b.Root, "layers")
	if err := os.MkdirAll(layers, 0o755); err != nil {
		return nil, err
	}
	unlock, err := lockFile(filepath.Join(b.Root, ".layer.lock"))
	if err != nil {
		return nil, err
	}
	defer unlock()
	pkgs, err := b.Packages(disabled)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(layers, "layer-")
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		return nil, err
	}
	current := filepath.Join(b.Store.Root, "current", "bin")
	for _, p := range pkgs {
		if !p.Enabled {
			continue
		}
		keys := make([]string, len(p.Keys))
		for i, k := range p.Keys {
			keys[i] = k.Name
		}
		for _, bin := range p.Bins {
			script := wrapperScript(filepath.Join(current, bin), b.KeysPath, keys)
			if err := os.WriteFile(filepath.Join(dir, bin), []byte(script), 0o755); err != nil {
				return nil, err
			}
		}
	}
	if err := swapLink(filepath.Join(b.Root, "bin"), filepath.Join("layers", filepath.Base(dir))); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(layers)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(dir) {
			_ = os.RemoveAll(filepath.Join(layers, e.Name()))
		}
	}
	return pkgs, nil
}

// swapLink atomically points path at target, replacing a symlink or (once,
// on an image older than the layer) a plain directory.
func swapLink(path, target string) error {
	tmp := path + ".tmp"
	if err := os.Remove(tmp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	if fi, err := os.Lstat(path); err == nil && fi.IsDir() {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return os.Rename(tmp, path)
}

func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
