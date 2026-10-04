package agentlogin

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/berth-ai/berth/internal/agentset"
	"github.com/berth-ai/berth/internal/locksource"
)

// ErrUnknown means the agent is not installed, not enabled, or has no such
// sign-in method.
var ErrUnknown = errors.New("no such agent or sign-in method")

// Env locates the agents and the agent user's home.
type Env struct {
	Set     agentset.Set // the merged agent set
	Layer   string       // the wrapper layer, /var/lib/berth/bin; a wrapper there means enabled
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
// Bin is the package's own bin, not its wrapper: the wrapper loads API keys.
func (e Env) Agents() ([]Agent, error) {
	var out []Agent
	for _, dep := range slices.Sorted(maps.Keys(e.Set.Descriptors.Agents)) {
		l := e.Set.Descriptors.Agents[dep].Login
		if l == nil {
			continue
		}
		bin := e.Set.EnabledBin(e.Layer, dep)
		if bin == "" {
			continue
		}
		p := e.Set.Packages[dep]
		out = append(out, Agent{Package: dep, Name: p.Meta.Name, Bin: filepath.Join(p.Link, "bin", bin), Methods: l.Methods})
	}
	return out, nil
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
