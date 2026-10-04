package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"text/tabwriter"

	"github.com/berth-ai/berth/internal/agentlogin"
	"github.com/berth-ai/berth/internal/locksource"
	"github.com/berth-ai/berth/internal/output"
	"github.com/berth-ai/berth/internal/runner"
)

const loginUsage = "usage: berth login status [--json] | berth login logout AGENT METHOD [--json]"

// runLogin is berth login: the agents' own sign-ins, which live in agent's
// home. run is the broker's, for the panel's sign-in dialog.
func runLogin(ctx context.Context, a *app, args []string) error {
	if len(args) == 0 {
		return usageErr(errors.New(loginUsage))
	}
	fs := newFlagSet("login " + args[0])
	jsonOut := fs.Bool("json", false, "print JSON")
	pos, err := parseArgs(fs, args[1:])
	if err != nil {
		return usageErr(err)
	}
	a.p.JSON = *jsonOut
	want := map[string]int{"status": 0, "logout": 2, "run": 2}
	n, ok := want[args[0]]
	if !ok || len(pos) != n {
		return usageErr(errors.New(loginUsage))
	}
	if done, err := a.loginAsAgent(ctx, args); done {
		return err
	}
	env, err := a.loginEnv()
	if err != nil {
		return err
	}
	switch args[0] {
	case "status":
		rep, err := env.Status(ctx)
		if err != nil {
			return output.Errorf(output.ExitFailure, "status_failed", "login", "%v", err)
		}
		return a.p.Result(rep, func(w io.Writer) { writeLoginStatus(w, rep.Agents) })
	case "logout":
		return a.loginLogout(ctx, env, pos[0], pos[1])
	}
	return a.loginRun(ctx, env, pos[0], pos[1])
}

// loginAsAgent re-runs the command as agent when root runs it, since the
// sign-ins live in agent's home. It reports whether it did.
func (a *app) loginAsAgent(ctx context.Context, args []string) (bool, error) {
	if !a.cfg.IsRoot || a.cfg.Agent == nil {
		return false, nil
	}
	r := runner.Exec{Agent: a.cfg.Agent, Stdout: a.p.Stdout, Stderr: a.p.Stderr}
	err := r.Run(ctx, runner.Cmd{Path: a.cfg.BerthPath, Args: append([]string{"login"}, args...), AsAgent: true})
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return true, output.ExitCode(ee.ExitCode())
	}
	return true, err
}

func (a *app) loginEnv() (agentlogin.Env, error) {
	home := a.cfg.Getenv("HOME")
	if home == "" {
		return agentlogin.Env{}, output.Errorf(output.ExitFailure, "no_home", "login", "HOME is not set")
	}
	cur, err := a.store.Current()
	if err != nil {
		return agentlogin.Env{}, output.Errorf(output.ExitFailure, "internal", "login", "%v", err)
	}
	env := agentlogin.Env{
		Bins:  filepath.Join(a.cfg.AgentsDir, "current", "bin"),
		Layer: filepath.Join(a.cfg.ConfigDir, "bin"),
		Dirs: locksource.HomeDirs{Home: home, ConfigHome: a.cfg.Getenv("XDG_CONFIG_HOME"),
			DataHome: a.cfg.Getenv("XDG_DATA_HOME")},
	}
	if cur != nil {
		env.Current = cur.Dir
	}
	return env, nil
}

func (a *app) loginLogout(ctx context.Context, env agentlogin.Env, pkg, id string) error {
	err := env.Logout(ctx, pkg, id)
	switch {
	case errors.Is(err, agentlogin.ErrUnknown):
		return output.Errorf(output.ExitUsage, "unknown", "login", "%v", err)
	case errors.Is(err, agentlogin.ErrStillSignedIn):
		return output.Errorf(output.ExitFailure, "still_signed_in", "login", "%v", err)
	case err != nil:
		return output.Errorf(output.ExitFailure, "logout_failed", "login", "%v", err)
	}
	rep, err := env.Status(ctx)
	if err != nil {
		return output.Errorf(output.ExitFailure, "status_failed", "login", "%v", err)
	}
	one := map[string]agentlogin.AgentStatus{pkg: rep.Agents[pkg]}
	return a.p.Result(map[string]any{"agents": one}, func(w io.Writer) {
		fmt.Fprintf(w, "Signed %s out of %s.\n", rep.Agents[pkg].Name, id)
	})
}

// loginRun is one sign-in for the broker: the log on stdout, pasted codes
// on stdin. A failure is already a marker line, so it exits quietly.
func (a *app) loginRun(ctx context.Context, env agentlogin.Env, pkg, id string) error {
	ag, m, err := env.Find(pkg, id)
	if err != nil {
		fmt.Fprintf(a.p.Stdout, "berth: login failed: %v\n", err)
		return output.ExitCode(output.ExitUsage)
	}
	s := agentlogin.Session{Env: env, Agent: ag, Method: m, In: os.Stdin, Out: a.p.Stdout}
	if err := s.Run(ctx); err != nil {
		if !errors.Is(err, agentlogin.ErrNotSignedIn) {
			fmt.Fprintf(a.p.Stdout, "berth: login failed: %v\n", err)
		}
		return output.ExitCode(output.ExitFailure)
	}
	return nil
}

func writeLoginStatus(w io.Writer, agents map[string]agentlogin.AgentStatus) {
	if len(agents) == 0 {
		fmt.Fprintln(w, "No agent with a sign-in is switched on.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "AGENT\tMETHOD\tSTATUS")
	for _, pkg := range slices.Sorted(maps.Keys(agents)) {
		st := agents[pkg]
		for _, m := range st.Methods {
			status := "not signed in"
			switch {
			case m.Error != "":
				status = "error: " + m.Error
			case m.SignedIn && m.Account != "":
				status = "signed in as " + m.Account
			case m.SignedIn:
				status = "signed in"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\n", st.Name, m.Label, status)
		}
	}
	_ = tw.Flush()
}
