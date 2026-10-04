package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/berth-ai/berth/internal/agentlayer"
	"github.com/berth-ai/berth/internal/agentset"
	"github.com/berth-ai/berth/internal/agentstore"
	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/locksource"
	"github.com/berth-ai/berth/internal/output"
	"github.com/berth-ai/berth/internal/runner"
	"github.com/berth-ai/berth/internal/status"
)

func runVersion(_ context.Context, a *app, args []string) error {
	if len(args) > 0 {
		return usageErr(fmt.Errorf("version takes no arguments"))
	}
	fmt.Fprintf(a.p.Stdout, "berth %s (%s)\n", a.cfg.Version, a.cfg.Commit)
	return nil
}

func runStatus(ctx context.Context, a *app, args []string) error {
	fs := newFlagSet("status")
	check := fs.Bool("check", false, "compare with the lock on main")
	jsonOut := fs.Bool("json", false, "print JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return usageErr(err)
	}
	if len(pos) > 0 {
		return usageErr(fmt.Errorf("status takes no arguments"))
	}
	a.p.JSON = *jsonOut
	rep, err := status.Collect(status.Inputs{
		Store: a.store, SeedDir: a.cfg.SeedDir, BuildInfo: a.cfg.BuildInfo, MiseConfig: a.cfg.MiseConfig,
	})
	if err != nil {
		return output.Errorf(output.ExitFailure, "internal", "status", "%v", err)
	}
	if *check {
		lock, err := a.cfg.GitHub.Fetch(ctx, "main", a.cfg.Now())
		if err != nil {
			return sourceErr(err)
		}
		rep.Check = &status.Check{
			Ref: "main", Commit: lock.Source.Commit, Hash: lock.Hash,
			UpdateAvailable: rep.Current == nil || rep.Current.Hash != lock.Hash,
		}
	}
	return a.p.Result(rep, func(w io.Writer) { status.WriteHuman(w, rep) })
}

type updateResult struct {
	Changed     bool                `json:"changed"`
	Installed   bool                `json:"installed"`
	FollowImage bool                `json:"follow_image"`
	Current     *status.VersionView `json:"current"`
	Previous    *status.VersionView `json:"previous"`
}

func (r updateResult) human(w io.Writer) {
	switch {
	case !r.Changed:
		fmt.Fprintf(w, "agents already at %s\n", hashOrNone(r.Current))
	case r.Installed:
		fmt.Fprintf(w, "installed and activated %s (previous: %s)\n", hashOrNone(r.Current), hashOrNone(r.Previous))
	default:
		fmt.Fprintf(w, "activated %s (previous: %s)\n", hashOrNone(r.Current), hashOrNone(r.Previous))
	}
}

func runUpdate(ctx context.Context, a *app, args []string) error {
	fs := newFlagSet("update")
	ref := fs.String("ref", "", "GitHub ref to read agents/ from (default main)")
	image := fs.Bool("image", false, "install the image seed and follow the image again")
	lockDir := fs.String("lock-dir", "", "install from a local directory")
	jsonOut := fs.Bool("json", false, "print JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return usageErr(err)
	}
	a.p.JSON = *jsonOut
	if len(pos) > 0 {
		return usageErr(fmt.Errorf("update takes no arguments"))
	}
	selected := 0
	for _, set := range []bool{*ref != "", *image, *lockDir != ""} {
		if set {
			selected++
		}
	}
	if selected > 1 {
		return usageErr(fmt.Errorf("--ref, --image and --lock-dir are mutually exclusive"))
	}
	return a.locked("update", func() error {
		var lock *locksource.Lock
		var err error
		switch {
		case *image:
			lock, err = a.seed()
		case *lockDir != "":
			lock, err = locksource.FromDir(*lockDir, locksource.Source{Origin: locksource.OriginDir, FetchedAt: a.cfg.Now().UTC()})
		default:
			r := *ref
			if r == "" {
				r = "main"
			}
			lock, err = a.cfg.GitHub.Fetch(ctx, r, a.cfg.Now())
		}
		if err != nil {
			return sourceErr(err)
		}
		res, err := a.apply(ctx, lock, *image)
		if err != nil {
			return err
		}
		a.setupMCP(ctx, a.cfg.MCPTimeout)
		a.setupSkills(ctx)
		a.gcQuietly()
		return a.p.Result(res, res.human)
	})
}

// apply installs lock if needed, flips to it and records the follow state.
func (a *app) apply(ctx context.Context, lock *locksource.Lock, follow bool) (*updateResult, error) {
	in := agentstore.Installer{
		Store: a.store, Runner: a.cfg.Runner, Smoke: a.cfg.Smoke,
		Agent: a.cfg.Agent, Now: a.cfg.Now, Logf: a.p.Logf,
	}
	v, installed, err := in.Install(ctx, lock)
	if err != nil {
		return nil, installErr(err)
	}
	changed, err := a.store.Flip(v.Hash)
	if err != nil {
		return nil, output.Errorf(output.ExitFailure, "flip_failed", "flip", "%v", err)
	}
	if err := a.store.WriteState(agentstore.State{FollowImage: follow}); err != nil {
		return nil, output.Errorf(output.ExitFailure, "flip_failed", "flip", "%v", err)
	}
	a.rebuildLayer()
	cur, prev, err := a.views()
	if err != nil {
		return nil, output.Errorf(output.ExitFailure, "internal", "flip", "%v", err)
	}
	return &updateResult{Changed: changed, Installed: installed, FollowImage: follow, Current: cur, Previous: prev}, nil
}

// layer is the wrapper layer under ConfigDir.
func (a *app) layer() agentlayer.Builder {
	b := agentlayer.Builder{Root: a.cfg.ConfigDir, Store: a.store, KeysPath: a.cfg.KeysPath, RegistryRoot: a.cfg.RegistryDir}
	if a.cfg.Agent != nil {
		b.AddedRoot = agentset.StoreRoot(a.cfg.Agent.Home)
	}
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
	if _, err := a.store.GC(a.cfg.Keep, agentstore.InUsePaths(a.cfg.ProcRoot)); err != nil {
		a.p.Logf("warning: gc failed: %v", err)
	}
}

type rollbackResult struct {
	Current  *status.VersionView `json:"current"`
	Previous *status.VersionView `json:"previous"`
}

func runRollback(ctx context.Context, a *app, args []string) error {
	fs := newFlagSet("rollback")
	jsonOut := fs.Bool("json", false, "print JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return usageErr(err)
	}
	a.p.JSON = *jsonOut
	if len(pos) > 1 {
		return usageErr(fmt.Errorf("rollback takes at most one HASH"))
	}
	target := ""
	if len(pos) == 1 {
		target = pos[0]
	}
	return a.locked("rollback", func() error {
		_, err := a.store.Rollback(target)
		switch {
		case errors.Is(err, agentstore.ErrNoPrevious):
			return output.Errorf(output.ExitFailure, "no_previous", "rollback", "%v", err)
		case errors.Is(err, agentstore.ErrNotFound):
			return output.Errorf(output.ExitFailure, "not_found", "rollback", "no complete agent set %q", target)
		case err != nil:
			return output.Errorf(output.ExitFailure, "flip_failed", "flip", "%v", err)
		}
		if err := a.store.WriteState(agentstore.State{FollowImage: false}); err != nil {
			return output.Errorf(output.ExitFailure, "flip_failed", "flip", "%v", err)
		}
		a.rebuildLayer()
		a.setupMCP(ctx, a.cfg.MCPTimeout)
		a.setupSkills(ctx)
		cur, prev, err := a.views()
		if err != nil {
			return output.Errorf(output.ExitFailure, "internal", "rollback", "%v", err)
		}
		res := rollbackResult{Current: cur, Previous: prev}
		return a.p.Result(res, func(w io.Writer) {
			fmt.Fprintf(w, "rolled back to %s (previous: %s)\n", hashOrNone(cur), hashOrNone(prev))
		})
	})
}

type ensureResult struct {
	Action   string              `json:"action"`
	Reason   string              `json:"reason"`
	SeedHash string              `json:"seed_hash"`
	Current  *status.VersionView `json:"current"`
}

func runEnsure(ctx context.Context, a *app, args []string) error {
	fs := newFlagSet("ensure")
	jsonOut := fs.Bool("json", false, "print JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return usageErr(err)
	}
	a.p.JSON = *jsonOut
	if len(pos) > 0 {
		return usageErr(fmt.Errorf("ensure takes no arguments"))
	}
	return a.locked("ensure", func() error {
		seed, err := a.seed()
		if err != nil {
			return sourceErr(err)
		}
		cur, err := a.store.Current()
		if err != nil {
			return output.Errorf(output.ExitFailure, "internal", "ensure", "%v", err)
		}
		state, err := a.store.ReadState()
		if err != nil {
			return output.Errorf(output.ExitFailure, "internal", "ensure", "%v", err)
		}
		res := ensureResult{Action: "none", SeedHash: seed.Hash}
		switch {
		case cur == nil:
			res.Reason = "no-current"
		case !state.FollowImage:
			res.Reason = "pinned"
		case cur.Hash != seed.Hash:
			res.Reason = "seed-changed"
		case agentstore.PackageJSONStale(cur.Dir, seed.PackageJSON):
			res.Reason = "descriptors-changed"
		default:
			res.Reason = "up-to-date"
		}
		if res.Reason != "pinned" && res.Reason != "up-to-date" {
			if _, err := a.apply(ctx, seed, true); err != nil {
				return err
			}
			res.Action = "installed"
			if res.Reason == "descriptors-changed" {
				res.Action = "refreshed"
			}
			a.gcQuietly()
		}
		if res.Current, _, err = a.views(); err != nil {
			return output.Errorf(output.ExitFailure, "internal", "ensure", "%v", err)
		}
		return a.p.Result(res, func(w io.Writer) {
			fmt.Fprintf(w, "agents %s (%s, %s)\n", hashOrNone(res.Current), res.Action, res.Reason)
		})
	})
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
