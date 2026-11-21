package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/berth-ai/berth/internal/agentlayer"
	"github.com/berth-ai/berth/internal/agentset"
	"github.com/berth-ai/berth/internal/agentstore"
	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/output"
	"github.com/berth-ai/berth/internal/runner"
)

func runVersion(_ context.Context, a *app, args []string) error {
	if len(args) > 0 {
		return usageErr(fmt.Errorf("version takes no arguments"))
	}
	fmt.Fprintf(a.p.Stdout, "berth %s (%s)\n", a.cfg.Version, a.cfg.Commit)
	return nil
}

// layer is the wrapper layer under ConfigDir.
func (a *app) layer() agentlayer.Builder {
	b := agentlayer.Builder{Root: a.cfg.ConfigDir, Store: a.store, KeysPath: a.cfg.KeysPath, RegistryRoot: a.cfg.RegistryDir}
	if a.cfg.Agent != nil {
		b.AddedRoot = agentset.StoreRoot(a.cfg.Agent.Home)
	}
	b.Added = a.addedReport
	return b
}

// rebuildLayer refreshes /var/lib/berth/bin for the current set and the
// configured disabled packages. Failures are logged: the agent set itself
// changed successfully.
func (a *app) rebuildLayer() {
	if a.cfg.ConfigDir == "" {
		return
	}
	c, err := (&config.Store{Dir: a.cfg.ConfigDir}).Load()
	if err != nil {
		c = config.Defaults()
	}
	if _, err := a.layer().Build(c.Agents.Disabled, c.Agents.Added); err != nil {
		a.p.Logf("warning: agent layer: %v", err)
	}
}

// setupMCP converges MCP registrations; failures are logged, never fatal.
func (a *app) setupMCP(ctx context.Context, timeout time.Duration) {
	if len(a.cfg.MCPSetup) == 0 {
		return
	}
	cmd := runner.Cmd{Path: a.cfg.MCPSetup[0], Args: a.cfg.MCPSetup[1:], AsAgent: true, Timeout: timeout}
	if err := a.cfg.Runner.Run(ctx, cmd); err != nil {
		a.p.Logf("warning: MCP setup failed: %v", err)
	}
}

func (a *app) gcQuietly() {
	inUse := agentstore.InUsePaths(a.cfg.ProcRoot)
	if _, err := a.store.GC(a.cfg.Keep, inUse); err != nil {
		a.p.Logf("warning: gc failed: %v", err)
	}
	if a.toolsOn() {
		if _, err := a.toolStore().GC(a.cfg.Keep, inUse); err != nil {
			a.p.Logf("warning: tools gc failed: %v", err)
		}
	}
	// An Orca version is some 600 MB: keep only current and previous.
	if a.orcaOn() {
		if _, err := a.orcaStore().GC(0, inUse); err != nil {
			a.p.Logf("warning: orca gc failed: %v", err)
		}
	}
}

func runGC(_ context.Context, a *app, args []string) error {
	fs := newFlagSet("gc")
	jsonOut := fs.Bool("json", false, "print JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return usageErr(err)
	}
	a.p.JSON = *jsonOut
	if len(pos) > 0 {
		return usageErr(fmt.Errorf("gc takes no arguments"))
	}
	return a.locked("gc", func() error {
		res, err := a.store.GC(a.cfg.Keep, agentstore.InUsePaths(a.cfg.ProcRoot))
		if err != nil {
			return output.Errorf(output.ExitFailure, "internal", "gc", "%v", err)
		}
		return a.p.Result(res, func(w io.Writer) {
			fmt.Fprintf(w, "removed %d, kept %d, in use %d\n", len(res.Removed), len(res.Kept), len(res.InUse))
		})
	})
}
