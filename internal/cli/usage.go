package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"text/tabwriter"

	"github.com/berth-ai/berth/internal/agentusage"
	"github.com/berth-ai/berth/internal/locksource"
	"github.com/berth-ai/berth/internal/output"
)

const usageHelp = "usage: berth usage [--json]"

// runAgentUsage is berth usage: the token usage the agents recorded in their
// own session files in agent's home, over the last days.
func runAgentUsage(ctx context.Context, a *app, args []string) error {
	fs := newFlagSet("usage")
	jsonOut := fs.Bool("json", false, "print JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return usageErr(err)
	}
	a.p.JSON = *jsonOut
	if len(pos) != 0 {
		return usageErr(errors.New(usageHelp))
	}
	if done, err := a.rerunAsAgent(ctx, "usage", args); done {
		return err
	}
	home := a.cfg.Getenv("HOME")
	if home == "" {
		return output.Errorf(output.ExitFailure, "no_home", "usage", "HOME is not set")
	}
	set, err := a.agentSet(home)
	if err != nil {
		return output.Errorf(output.ExitFailure, "internal", "usage", "%v", err)
	}
	now := a.cfg.Now()
	env := agentusage.Env{
		Set: set,
		Dirs: locksource.HomeDirs{Home: home, ConfigHome: a.cfg.Getenv("XDG_CONFIG_HOME"),
			DataHome: a.cfg.Getenv("XDG_DATA_HOME")},
		Now: now,
		Loc: now.Location(), // days follow the container's TZ
	}
	rep := env.Report()
	return a.p.Result(rep, func(w io.Writer) { writeAgentUsage(w, rep.Agents) })
}

func writeAgentUsage(w io.Writer, agents map[string]agentusage.Agent) {
	if len(agents) == 0 {
		fmt.Fprintln(w, "No agent is installed.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "AGENT\tTODAY\tLAST %d DAYS\tSESSIONS\tLAST ACTIVE\n", agentusage.Days)
	for _, pkg := range slices.Sorted(maps.Keys(agents)) {
		u := agents[pkg]
		if !u.Tracked {
			fmt.Fprintf(tw, "%s\tnot tracked\t\t\t\n", u.Name)
			continue
		}
		last := "never"
		if u.LastActive != nil {
			last = u.LastActive.Format("2006-01-02 15:04")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n", u.Name, tokens(u.Today.Total), tokens(u.Week.Total), u.Week.Sessions, last)
		if u.Error != "" {
			fmt.Fprintf(tw, "\twarning: %s\t\t\t\n", u.Error)
		}
	}
	_ = tw.Flush()
}

// tokens is a token count for people: 950, 12.3k, 4.5M.
func tokens(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprint(n)
	case n < 1_000_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	case n < 1_000_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	}
	return fmt.Sprintf("%.1fB", float64(n)/1e9)
}
