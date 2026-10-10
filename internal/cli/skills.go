package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/output"
	"github.com/berth-ai/berth/internal/runner"
	"github.com/berth-ai/berth/internal/skills"
	"github.com/berth-ai/berth/internal/wsstate"
)

const skillsUsage = "usage: berth skills scan --repo URL --ref REF [--path P]... [--json] | berth skills sync | " +
	"berth skills vendor --workspace ID --key SOURCE/NAME --action overwrite|release [--json]"

// runSkills is the hidden `berth skills scan|sync`, run as agent: the
// broker scans a repository before adding or updating skills, and syncs
// after every change.
func runSkills(ctx context.Context, a *app, args []string) error {
	if len(args) == 0 {
		return usageErr(errors.New(skillsUsage))
	}
	g := skills.Git{AllowFile: a.cfg.Getenv("BERTH_SKILLS_ALLOW_FILE") == "1", Log: a.p.Stderr}
	switch args[0] {
	case "scan":
		return a.skillsScan(ctx, g, args[1:])
	case "sync":
		if len(args) != 1 {
			return usageErr(errors.New(skillsUsage))
		}
		return a.skillsSync(ctx, g)
	case "vendor":
		return a.skillsVendor(args[1:])
	}
	return usageErr(errors.New(skillsUsage))
}

// skillsVendor settles a vendored skill that local edits stopped:
// overwrite lets the next sync replace it, release makes Berth forget it.
func (a *app) skillsVendor(args []string) error {
	fs := newFlagSet("skills vendor")
	ws := fs.String("workspace", "", "workspace id")
	key := fs.String("key", "", "SOURCE/NAME of the skill")
	action := fs.String("action", "", "overwrite or release")
	jsonOut := fs.Bool("json", false, "print JSON")
	pos, err := parseArgs(fs, args)
	src, name, _ := strings.Cut(*key, "/")
	if err != nil || len(pos) > 0 || !config.WorkspaceIDPattern.MatchString(*ws) ||
		!config.WorkspaceIDPattern.MatchString(src) || !config.SkillIDPattern.MatchString(name) {
		return usageErr(errors.New(skillsUsage))
	}
	a.p.JSON = *jsonOut
	home, err := a.skillsHome()
	if err != nil {
		return err
	}
	store := wsstate.Store{Home: home, Published: filepath.Join(a.cfg.ConfigDir, "workspaces.json")}
	err = skills.Resolve(store, *ws, *key, *action)
	var se *skills.Error
	switch {
	case errors.As(err, &se):
		return output.Errorf(output.ExitFailure, se.Code, se.Field, "%s", se.Message)
	case errors.Is(err, wsstate.ErrGone):
		return output.Errorf(output.ExitFailure, "unknown_workspace", "workspace", "There is no workspace %s", *ws)
	case err != nil:
		return output.Errorf(output.ExitFailure, "resolve_failed", "", "%v", err)
	}
	res := map[string]string{"workspace": *ws, "key": *key, "action": *action}
	return a.p.Result(res, func(w io.Writer) { fmt.Fprintf(w, "%s %s in %s\n", *action, *key, *ws) })
}

func (a *app) skillsHome() (string, error) {
	home := a.cfg.Getenv("HOME")
	if home == "" {
		return "", output.Errorf(output.ExitFailure, "no_home", "skills", "HOME is not set")
	}
	return home, nil
}

// pathsFlag collects a repeatable --path.
type pathsFlag []string

func (p *pathsFlag) String() string     { return strings.Join(*p, ",") }
func (p *pathsFlag) Set(v string) error { *p = append(*p, v); return nil }

func (a *app) skillsScan(ctx context.Context, g skills.Git, args []string) error {
	fs := newFlagSet("skills scan")
	repo := fs.String("repo", "", "repository URL")
	ref := fs.String("ref", "", "branch, tag or commit")
	var paths pathsFlag
	fs.Var(&paths, "path", "folder to look in; repeatable")
	jsonOut := fs.Bool("json", false, "print JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return usageErr(err)
	}
	if len(pos) > 0 {
		return usageErr(errors.New(skillsUsage))
	}
	a.p.JSON = *jsonOut
	home, err := a.skillsHome()
	if err != nil {
		return err
	}
	res, err := g.Scan(ctx, home, skills.ScanRequest{Repo: *repo, Ref: *ref, Paths: paths})
	var se *skills.Error
	switch {
	case errors.As(err, &se):
		// The step names the field the panel shows the error on.
		return output.Errorf(output.ExitFailure, se.Code, se.Field, "%s", se.Message)
	case err != nil:
		return output.Errorf(output.ExitFailure, "fetch_failed", "repo", "%v", err)
	}
	a.p.Logf("%s at %s is commit %s; found %d skills", *repo, *ref, res.Commit, len(res.Skills))
	return a.p.Result(res, func(w io.Writer) {
		for _, s := range res.Skills {
			if s.Error != "" {
				fmt.Fprintf(w, "%s\t-\t%s\n", cmp.Or(s.Path, "."), s.Error)
			} else {
				fmt.Fprintf(w, "%s\t%s\n", cmp.Or(s.Path, "."), s.Name)
			}
		}
	})
}

func (a *app) skillsSync(ctx context.Context, g skills.Git) error {
	home, err := a.skillsHome()
	if err != nil {
		return err
	}
	set, err := a.agentSet(home)
	if err != nil {
		return output.Errorf(output.ExitFailure, "internal", "skills", "%v", err)
	}
	s := skills.Syncer{
		Set:        set,
		Layer:      filepath.Join(a.cfg.ConfigDir, "bin"),
		Home:       home,
		ConfigHome: a.cfg.Getenv("XDG_CONFIG_HOME"),
		DataHome:   a.cfg.Getenv("XDG_DATA_HOME"),
		Settings:   filepath.Join(a.cfg.ConfigDir, "skills.json"),
		Workspaces: filepath.Join(a.cfg.ConfigDir, "workspaces.json"),
		Git:        g,
		Log:        a.p.Stderr,
	}
	if err := s.Run(ctx); err != nil {
		return output.Errorf(output.ExitFailure, "skills_failed", "skills", "%v", err)
	}
	return nil
}

// setupSkills converges skills; failures are logged, never fatal.
func (a *app) setupSkills(ctx context.Context) {
	if len(a.cfg.SkillsSync) == 0 {
		return
	}
	cmd := runner.Cmd{Path: a.cfg.SkillsSync[0], Args: a.cfg.SkillsSync[1:], AsAgent: true, Timeout: skills.SyncTimeout}
	if err := a.cfg.Runner.Run(ctx, cmd); err != nil {
		a.p.Logf("warning: skills setup failed: %v", err)
	}
}
