package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/salgnt/berth/internal/output"
	"github.com/salgnt/berth/internal/status"
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

func runUpdate(context.Context, *app, []string) error   { return fmt.Errorf("not implemented") }
func runRollback(context.Context, *app, []string) error { return fmt.Errorf("not implemented") }
func runEnsure(context.Context, *app, []string) error   { return fmt.Errorf("not implemented") }
func runGC(context.Context, *app, []string) error       { return fmt.Errorf("not implemented") }
