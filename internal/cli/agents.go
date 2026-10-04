package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"text/tabwriter"
	"time"

	"github.com/berth-ai/berth/internal/addedagents"
	"github.com/berth-ai/berth/internal/agentset"
	"github.com/berth-ai/berth/internal/agentstore"
	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/output"
	"github.com/berth-ai/berth/internal/registry"
	"github.com/berth-ai/berth/internal/runner"
	"github.com/berth-ai/berth/internal/smoke"
)

const agentsUsage = "usage: berth agents status --json | berth agents sync | berth agents install ID [--json] | berth agents rollback PACKAGE [--json]"

// addedSyncTimeout bounds one berth agents sync started by berth ensure.
const addedSyncTimeout = 30 * time.Minute

// runAgents is the hidden berth agents: the converger for the agents a
// user added from the registry. They install in agent's home, so it runs
// as agent; root re-runs it as agent.
func runAgents(ctx context.Context, a *app, args []string) error {
	fs := newFlagSet("agents")
	jsonOut := fs.Bool("json", false, "print JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return usageErr(err)
	}
	a.p.JSON = *jsonOut
	want := map[string]int{"status": 0, "sync": 0, "install": 1, "rollback": 1}
	if len(pos) == 0 {
		return usageErr(errors.New(agentsUsage))
	}
	sub := pos[0]
	pos = pos[1:]
	if n, ok := want[sub]; !ok || len(pos) != n {
		return usageErr(errors.New(agentsUsage))
	}
	if done, err := a.rerunAsAgent(ctx, "agents", args); done {
		return err
	}
	home := a.cfg.Getenv("HOME")
	if home == "" {
		return output.Errorf(output.ExitFailure, "no_home", "agents", "HOME is not set")
	}
	c := a.converger(home)
	switch sub {
	case "status":
		rep, problems := c.Status()
		for _, p := range problems {
			a.p.Logf("warning: %v", p)
		}
		return a.p.Result(rep, func(w io.Writer) { writeAddedStatus(w, rep) })
	case "sync":
		wanted, err := config.ReadAgentsFile(a.agentsFile())
		if err != nil {
			return output.Errorf(output.ExitFailure, "internal", "agents", "%v", err)
		}
		if err := c.Sync(ctx, wanted); err != nil {
			return output.Errorf(output.ExitFailure, "sync_failed", "agents", "%v", err)
		}
		return nil
	case "install":
		if !registry.IDPattern.MatchString(pos[0]) {
			return usageErr(fmt.Errorf("invalid registry id %q", pos[0]))
		}
		res, err := c.Install(ctx, pos[0])
		switch {
		case errors.Is(err, addedagents.ErrUnknown):
			return output.Errorf(output.ExitUsage, "unknown_agent", "agents", "%v", err)
		case err != nil:
			return installErr(err)
		}
		return a.p.Result(addedResult(res), func(w io.Writer) { fmt.Fprintf(w, "installed %s %s\n", res.Package, res.Version) })
	}
	if !config.ValidPackage(pos[0]) {
		return usageErr(fmt.Errorf("invalid package %q", pos[0]))
	}
	res, err := c.Rollback(pos[0])
	switch {
	case errors.Is(err, addedagents.ErrNotAdded):
		return output.Errorf(output.ExitFailure, "not_added", "rollback", "%v", err)
	case errors.Is(err, agentstore.ErrNoPrevious):
		return output.Errorf(output.ExitFailure, "no_previous", "rollback", "%v", err)
	case err != nil:
		return output.Errorf(output.ExitFailure, "flip_failed", "flip", "%v", err)
	}
	return a.p.Result(addedResult(res), func(w io.Writer) { fmt.Fprintf(w, "rolled %s back to %s\n", res.Package, res.Version) })
}

func addedResult(a agentset.Added) map[string]string {
	return map[string]string{"id": a.ID, "package": a.Package, "version": a.Version, "hash": a.Hash}
}

func (a *app) converger(home string) addedagents.Converger {
	return addedagents.Converger{
		Home:     home,
		Registry: registry.LoadLogged(a.cfg.RegistryDir, a.p.Stderr),
		Runner:   a.cfg.Runner,
		Smoke:    smoke.Checker{Runner: a.cfg.Runner, Timeout: 2 * time.Minute}.Bins,
		Now:      a.cfg.Now,
		InUse:    func() []string { return agentstore.InUsePaths(a.cfg.ProcRoot) },
		Log:      a.p.Stderr,
	}
}

func writeAddedStatus(w io.Writer, rep agentset.Report) {
	if len(rep.Agents) == 0 && len(rep.Errors) == 0 {
		fmt.Fprintln(w, "No agent is added.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "PACKAGE\tVERSION\tERROR")
	for _, ad := range rep.Agents {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", ad.Package, ad.Version, rep.Errors[ad.Package])
	}
	for pkg, msg := range rep.Errors {
		if !slices.ContainsFunc(rep.Agents, func(ad agentset.Added) bool { return ad.Package == pkg }) {
			fmt.Fprintf(tw, "%s\t-\t%s\n", pkg, msg)
		}
	}
	_ = tw.Flush()
}

// agentsArgs is AgentsCmd's arguments followed by more.
func (a *app) agentsArgs(more ...string) []string {
	return append(slices.Clone(a.cfg.AgentsCmd[1:]), more...)
}

// syncAdded runs berth agents sync as agent; failures are logged, never
// fatal: a failed install keeps the previous version.
func (a *app) syncAdded(ctx context.Context) {
	if len(a.cfg.AgentsCmd) == 0 {
		return
	}
	cmd := runner.Cmd{Path: a.cfg.AgentsCmd[0], Args: a.agentsArgs("sync"), AsAgent: true, Timeout: addedSyncTimeout}
	if err := a.cfg.Runner.Run(ctx, cmd); err != nil {
		a.p.Logf("warning: added agents: %v", err)
	}
}

// addedReport runs berth agents status as agent, so that root never opens
// a file in agent's home. Any failure reports no added agents.
func (a *app) addedReport() agentset.Report {
	if len(a.cfg.AgentsCmd) == 0 {
		return agentset.Report{}
	}
	var out bytes.Buffer
	r := runner.Exec{Agent: a.cfg.Agent, Stdout: &out, Stderr: a.p.Stderr}
	cmd := runner.Cmd{Path: a.cfg.AgentsCmd[0], Args: a.agentsArgs("status", "--json"), AsAgent: true, Timeout: time.Minute}
	if err := r.Run(context.Background(), cmd); err != nil {
		a.p.Logf("warning: added agents: %v", err)
		return agentset.Report{}
	}
	rep, err := agentset.DecodeReport(out.Bytes())
	if err != nil {
		a.p.Logf("warning: %v", err)
		return agentset.Report{}
	}
	return rep
}
