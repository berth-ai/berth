package agentlogin

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/berth-ai/berth/internal/agentlayer"
	"github.com/berth-ai/berth/internal/agentstore"
	"github.com/berth-ai/berth/internal/locksource"
)

// ErrUnknown means the agent is not installed, not enabled, or has no such
// sign-in method.
var ErrUnknown = errors.New("no such agent or sign-in method")

// Env locates the installed agents and the agent user's home.
type Env struct {
	Current string // the installed agent set; empty means none
	Bins    string // the set's bin directory, /opt/agents/current/bin
	Layer   string // the wrapper layer, /var/lib/berth/bin; a wrapper there means enabled
	Dirs    locksource.HomeDirs
	Environ []string // the CLIs' environment; nil means os.Environ()
}

// Agent is an enabled agent with sign-in methods.
type Agent struct {
	Package string
	Name    string
	Bin     string // the package's own bin, not its wrapper
	Methods []locksource.LoginMethod
}

// Agents lists the enabled agents that describe a login, by package name.
func (e Env) Agents() ([]Agent, error) {
	if e.Current == "" {
		return nil, nil
	}
	meta, desc, err := locksource.ReadInstalled(e.Current)
	if err != nil {
		return nil, err
	}
	var out []Agent
	for _, dep := range slices.Sorted(maps.Keys(desc.Agents)) {
		l := desc.Agents[dep].Login
		if l == nil {
			continue
		}
		bin := e.enabledBin(dep)
		if bin == "" {
			continue
		}
		out = append(out, Agent{Package: dep, Name: meta[dep].Name, Bin: filepath.Join(e.Bins, bin), Methods: l.Methods})
	}
	return out, nil
}

// enabledBin is the first of dep's bins, sorted, with a wrapper in Layer.
// The wrapper loads API keys, so callers run the bin itself.
func (e Env) enabledBin(dep string) string {
	bins, err := agentstore.PackageBins(e.Current, dep)
	if err != nil {
		return ""
	}
	for _, b := range slices.Sorted(maps.Keys(bins)) {
		if agentlayer.IsExecutable(filepath.Join(e.Layer, b)) {
			return b
		}
	}
	return ""
}

// Find returns an enabled agent and one of its methods.
func (e Env) Find(pkg, id string) (Agent, locksource.LoginMethod, error) {
	agents, err := e.Agents()
	if err != nil {
		return Agent{}, locksource.LoginMethod{}, err
	}
	for _, a := range agents {
		if a.Package != pkg {
			continue
		}
		for _, m := range a.Methods {
			if m.ID == id {
				return a, m, nil
			}
		}
	}
	return Agent{}, locksource.LoginMethod{}, fmt.Errorf("%w: %s %s (is the agent switched on?)", ErrUnknown, pkg, id)
}

func (e Env) environ() []string {
	if e.Environ != nil {
		return slices.Clone(e.Environ)
	}
	return os.Environ()
}
