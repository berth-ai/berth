package agentlayer

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"syscall"

	"github.com/berth-ai/berth/internal/agentset"
	"github.com/berth-ai/berth/internal/agentstore"
	"github.com/berth-ai/berth/internal/locksource"
	"github.com/berth-ai/berth/internal/registry"
)

// Key reports whether an API key has a value, never the value itself.
type Key struct {
	Name string `json:"name"`
	Set  bool   `json:"set"`
}

// Builder builds Root/bin from the merged agent set: the locked set in
// Store and the added agents in agent's home.
//
// Nothing in Berth runs a layer binary as root: login, status, MCP and
// skills sync and sessions all run as agent. That matters because an added
// agent's wrapper points into a tree agent can write. Root never opens a
// file there either: it learns the installs from Added, which runs berth
// agents status as agent.
type Builder struct {
	Root     string // /var/lib/berth
	Store    agentstore.Store
	KeysPath string // /var/lib/berth/secrets/agents.env
	// AddedRoot is agentset.StoreRoot of agent's home; wrappers of added
	// agents exec AddedRoot/<id>/current/bin/<bin>.
	AddedRoot string
	// Added reports the added agents installed in agent's home; nil means none.
	Added func() agentset.Report
	// RegistryRoot names and icons an added agent that is not installed.
	RegistryRoot string
	// SystemDirs are the directories after the layer on root's PATH; nil
	// means the image's. An added agent's bin may not shadow a command there.
	SystemDirs []string
}

// Package is one package of the merged set as the panel shows it.
type Package struct {
	Package       string   `json:"package"`
	Name          string   `json:"name"`
	Kind          string   `json:"kind"`
	Version       string   `json:"version"`
	Origin        string   `json:"origin"` // locked or added
	Enabled       bool     `json:"enabled"`
	Bins          []string `json:"bins"`
	Keys          []Key    `json:"keys"`
	MCPID         string   `json:"mcp_id,omitempty"`         // an MCP server's id in agent configs
	SkillsDir     string   `json:"skills_dir,omitempty"`     // an agent's skills directory
	SkillsVia     []string `json:"skills_via,omitempty"`     // agents whose skills directory it also reads
	SkillsProject string   `json:"skills_project,omitempty"` // an agent's skills folder in a workspace
	MCPProject    string   `json:"mcp_project,omitempty"`    // an agent's MCP file in a workspace
	MCPTrust      bool     `json:"mcp_trust,omitempty"`      // the agent reads MCPProject only in folders it trusts
	Icon          string   `json:"icon,omitempty"`           // a panel icon slug from the lock
	Previous      string   `json:"previous,omitempty"`       // an added agent's rollback version
	Error         string   `json:"error,omitempty"`          // why an added agent is missing or out of date
}

// systemDirs are the image's PATH after the layer (Dockerfile ENV PATH).
var systemDirs = []string{"/opt/mise/wrappers", "/usr/local/sbin", "/usr/local/bin", "/usr/sbin", "/usr/bin", "/sbin", "/bin"}

// notInstalled explains an added agent without an install yet.
const notInstalled = "Not installed yet; Berth installs it in the background."

// Packages describes the merged set for added (agents.added) with disabled
// applied: agents first, then MCP servers, each by name. An added agent
// that is not installed, or failed to load, still gets a row with Error.
func (b Builder) Packages(disabled, added []string) ([]Package, error) {
	pkgs, _, err := b.rows(disabled, added)
	return pkgs, err
}

func (b Builder) rows(disabled, added []string) ([]Package, agentset.Set, error) {
	locked, err := agentset.FromStore(b.Store)
	if err != nil {
		return nil, agentset.Set{}, err
	}
	var rep agentset.Report
	if b.Added != nil && len(added) > 0 {
		rep = b.Added()
	}
	// The layer leads root's PATH as well as agent's, and an added agent's
	// bins come from a tree agent can write: one named like a system command
	// would make root run agent's code. Such an agent gets no wrappers.
	refused := map[string]string{}
	picked := slices.DeleteFunc(agentset.Pick(rep.Agents, added), func(a agentset.Added) bool {
		for _, bin := range a.Bins {
			if cmd := b.shadowed(bin); cmd != "" {
				refused[a.Package] = fmt.Sprintf("its bin %s would shadow %s", bin, cmd)
				return true
			}
		}
		return false
	})
	set, err := agentset.Load(locked, b.AddedRoot, picked)
	if err != nil {
		return nil, agentset.Set{}, err
	}
	keys, err := ReadKeys(b.KeysPath)
	if err != nil {
		return nil, agentset.Set{}, err
	}
	out := []Package{}
	for _, name := range set.Names() {
		p := set.Packages[name]
		pkg := Package{
			Package: name, Name: p.Meta.Name, Kind: p.Meta.Kind, Version: p.Version, Origin: p.Origin,
			Enabled: !slices.Contains(disabled, name), Bins: append([]string{}, p.Bins...), Keys: []Key{},
			MCPID:    set.Descriptors.Servers[name].ID,
			Icon:     cmp.Or(set.Descriptors.Agents[name].Icon, set.Descriptors.Servers[name].Icon),
			Previous: p.PreviousVersion, Error: rep.Errors[name],
		}
		if t := set.Descriptors.Agents[name].Skills; t != nil {
			pkg.SkillsDir, pkg.SkillsVia, pkg.SkillsProject = t.Dir, set.Descriptors.SkillsVia(name), t.Project
		}
		if t := set.Descriptors.Agents[name].MCP; t != nil && t.Project != nil {
			pkg.MCPProject, pkg.MCPTrust = t.Project.File, t.Project.Trust
		}
		for _, k := range p.Meta.Keys {
			pkg.Keys = append(pkg.Keys, Key{Name: k, Set: keys[k] != ""})
		}
		out = append(out, pkg)
	}
	for _, name := range added {
		if _, ok := set.Packages[name]; !ok {
			out = append(out, b.missing(name, disabled, cmp.Or(refused[name], set.Errors[name], rep.Errors[name], notInstalled)))
		}
	}
	slices.SortStableFunc(out, func(a, b Package) int {
		return cmp.Or(cmp.Compare(kindOrder(a.Kind), kindOrder(b.Kind)), cmp.Compare(a.Name, b.Name))
	})
	return out, set, nil
}

// shadowed is the system command bin would shadow on root's PATH, or "".
func (b Builder) shadowed(bin string) string {
	if !locksource.ValidBinName(bin) {
		return "" // agentset.Load refuses it
	}
	dirs := b.SystemDirs
	if dirs == nil {
		dirs = systemDirs
	}
	for _, dir := range dirs {
		if p := filepath.Join(dir, bin); exists(p) {
			return p
		}
	}
	return ""
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// missing is the row of an added agent that is not in the set.
func (b Builder) missing(name string, disabled []string, msg string) Package {
	p := Package{Package: name, Name: name, Kind: locksource.KindAgent, Origin: agentset.OriginAdded,
		Enabled: !slices.Contains(disabled, name), Bins: []string{}, Keys: []Key{}, Error: msg}
	if b.RegistryRoot != "" {
		entries, _ := registry.Load(b.RegistryRoot)
		if e, ok := registry.ByPackage(entries, name); ok {
			p.Name, p.Icon = e.Meta.Name, e.Agent.Icon
		}
	}
	return p
}

// KeyNames is the union of the merged set's API key names, sorted.
func (b Builder) KeyNames(added []string) ([]string, error) {
	pkgs, err := b.Packages(nil, added)
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

// IsExecutable reports whether path is an executable regular file.
func IsExecutable(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0
}

func kindOrder(kind string) int {
	if kind == locksource.KindAgent {
		return 0
	}
	return 1
}

// Build writes a fresh layer for disabled and added and swaps Root/bin to it. A shell
// that already started a wrapper keeps running; the next invocation sees
// the new set. A file lock keeps the CLI and the supervisor from building
// at once.
func (b Builder) Build(disabled, added []string) ([]Package, error) {
	layers := filepath.Join(b.Root, "layers")
	if err := os.MkdirAll(layers, 0o755); err != nil {
		return nil, err
	}
	unlock, err := lockFile(filepath.Join(b.Root, ".layer.lock"))
	if err != nil {
		return nil, err
	}
	defer unlock()
	pkgs, set, err := b.rows(disabled, added)
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
	for _, p := range pkgs {
		sp, ok := set.Packages[p.Package]
		if !p.Enabled || !ok {
			continue
		}
		keys := make([]string, len(p.Keys))
		for i, k := range p.Keys {
			keys[i] = k.Name
		}
		// Bin names were validated: by WriteWrappers for the locked set,
		// and by agentset.Load for the added agents agent reported.
		for _, bin := range p.Bins {
			script := wrapperScript(filepath.Join(sp.Link, "bin", bin), b.KeysPath, keys, p.Kind == locksource.KindAgent)
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
