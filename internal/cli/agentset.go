package cli

import (
	"maps"
	"path/filepath"
	"slices"

	"github.com/berth-ai/berth/internal/agentset"
	"github.com/berth-ai/berth/internal/config"
)

// agentsFile is agents.json, the broker's copy of agents.added for the
// commands that run as agent.
func (a *app) agentsFile() string { return filepath.Join(a.cfg.ConfigDir, "agents.json") }

// agentSet loads the merged set as agent: the locked set and the installs
// in home of the packages agents.json lists. Problems with added agents
// are logged and leave only those agents out.
func (a *app) agentSet(home string) (agentset.Set, error) {
	locked, err := agentset.FromStore(a.store)
	if err != nil {
		return agentset.Set{}, err
	}
	wanted, err := config.ReadAgentsFile(a.agentsFile())
	if err != nil {
		a.p.Logf("warning: %v; leaving the added agents out", err)
	}
	root := agentset.StoreRoot(home)
	installed, problems := agentset.Installed(root)
	for _, p := range problems {
		a.p.Logf("warning: %v", p)
	}
	set, err := agentset.Load(locked, root, agentset.Pick(installed, wanted))
	for _, pkg := range slices.Sorted(maps.Keys(set.Errors)) {
		a.p.Logf("warning: %s left out: %s", pkg, set.Errors[pkg])
	}
	return set, err
}
