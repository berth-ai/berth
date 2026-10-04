package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/berth-ai/berth/internal/output"
	"github.com/berth-ai/berth/internal/runner"
	"github.com/berth-ai/berth/internal/skills"
)

const skillsUsage = "usage: berth skills resolve --repo URL [--path P] --ref REF [--json] | berth skills sync"

// runSkills is the hidden `berth skills resolve|sync`, run as agent: the
// broker resolves a skill before saving it, and syncs after every change.
func runSkills(ctx context.Context, a *app, args []string) error {
	if len(args) == 0 {
		return usageErr(errors.New(skillsUsage))
	}
	g := skills.Git{AllowFile: a.cfg.Getenv("BERTH_SKILLS_ALLOW_FILE") == "1", Log: a.p.Stderr}
	switch args[0] {
	case "resolve":
		return a.skillsResolve(ctx, g, args[1:])
	case "sync":
		if len(args) != 1 {
			return usageErr(errors.New(skillsUsage))
		}
		return a.skillsSync(ctx, g)
	}
	return usageErr(errors.New(skillsUsage))
}

func (a *app) skillsHome() (string, error) {
	home := a.cfg.Getenv("HOME")
	if home == "" {
		return "", output.Errorf(output.ExitFailure, "no_home", "skills", "HOME is not set")
	}
	return home, nil
}

func (a *app) skillsResolve(ctx context.Context, g skills.Git, args []string) error {
	fs := newFlagSet("skills resolve")
	repo := fs.String("repo", "", "repository URL")
	path := fs.String("path", "", "folder holding SKILL.md")
	ref := fs.String("ref", "", "branch, tag or commit")
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
	res, err := g.Resolve(ctx, home, skills.Request{Repo: *repo, Path: *path, Ref: *ref})
	var se *skills.Error
	switch {
	case errors.As(err, &se):
		// The step names the field the panel shows the error on.
		return output.Errorf(output.ExitFailure, se.Code, se.Field, "%s", se.Message)
	case err != nil:
		return output.Errorf(output.ExitFailure, "fetch_failed", "repo", "%v", err)
	}
	a.p.Logf("%s at %s is skill %s (%s)", *repo, *ref, res.Name, res.Commit)
	return a.p.Result(res, func(w io.Writer) { fmt.Fprintf(w, "%s %s\n", res.Name, res.Commit) })
}

func (a *app) skillsSync(ctx context.Context, g skills.Git) error {
	home, err := a.skillsHome()
	if err != nil {
		return err
	}
	cur, err := a.store.Current()
	if err != nil {
		return output.Errorf(output.ExitFailure, "internal", "skills", "%v", err)
	}
	current := ""
	if cur != nil {
		current = cur.Dir
	}
	s := skills.Syncer{
		Current:    current,
		Layer:      filepath.Join(a.cfg.ConfigDir, "bin"),
		Home:       home,
		ConfigHome: a.cfg.Getenv("XDG_CONFIG_HOME"),
		DataHome:   a.cfg.Getenv("XDG_DATA_HOME"),
		Settings:   filepath.Join(a.cfg.ConfigDir, "skills.json"),
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
