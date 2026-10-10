package cli

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/output"
	"github.com/berth-ai/berth/internal/workspace"
)

const workspacesUsage = "usage: berth workspaces clone --repo URL [--ref REF] --path P | probe --path P | " +
	"status [--ws ID=PATH]... | candidates [--exclude PATH]... | remove --path P | " +
	"launch --id ID --path P [--window NAME] [--run PROGRAM] | kill --id ID, each with --json"

var windowName = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]{0,63})?$`)

// runWorkspaces is the hidden `berth workspaces`, run as agent: the broker
// clones, adopts, reports on and removes workspaces through it, and drives
// their tmux sessions.
func runWorkspaces(ctx context.Context, a *app, args []string) error {
	if len(args) == 0 {
		return usageErr(errors.New(workspacesUsage))
	}
	fs := newFlagSet("workspaces " + args[0])
	jsonOut := fs.Bool("json", false, "print JSON")
	repo := fs.String("repo", "", "repository URL")
	ref := fs.String("ref", "", "branch, tag or commit")
	rel := fs.String("path", "", "folder below the workspaces root")
	id := fs.String("id", "", "workspace id")
	window := fs.String("window", "", "window name")
	program := fs.String("run", "", "absolute path of the program the window runs")
	var dirs, exclude pathsFlag
	fs.Var(&dirs, "ws", "ID=PATH of a workspace; repeatable")
	fs.Var(&exclude, "exclude", "a workspace's path; repeatable")
	pos, err := parseArgs(fs, args[1:])
	if err != nil || len(pos) > 0 {
		return usageErr(errors.New(workspacesUsage))
	}
	a.p.JSON = *jsonOut
	root := cmp.Or(a.cfg.WorkspacesDir, "/workspaces")
	g := workspace.Git{AllowFile: a.cfg.Getenv("BERTH_SKILLS_ALLOW_FILE") == "1"}
	t := workspace.Tmux{Path: a.cfg.Tmux}
	switch args[0] {
	case "clone":
		res, err := g.Clone(ctx, root, workspace.CloneRequest{Repo: *repo, Ref: *ref, Path: *rel}, a.p.Stderr)
		return a.workspaceResult(res, err)
	case "probe":
		res, err := workspace.ProbeFolder(root, *rel)
		return a.workspaceResult(res, err)
	case "status":
		abs := map[string]string{}
		for _, d := range dirs {
			k, v, ok := strings.Cut(d, "=")
			if !ok || !config.WorkspaceIDPattern.MatchString(k) || config.CheckWorkspacePath(v) != "" {
				return usageErr(fmt.Errorf("--ws %q: want ID=PATH", d))
			}
			abs[k] = filepath.Join(root, filepath.FromSlash(v))
		}
		states := workspace.Status(ctx, g, t, abs, workspace.StatusBudget)
		return a.workspaceResult(map[string]any{"workspaces": states}, nil)
	case "candidates":
		res, err := workspace.Candidates(root, exclude)
		return a.workspaceResult(map[string]any{"candidates": res}, err)
	case "remove":
		res, err := workspace.Remove(root, *rel)
		return a.workspaceResult(res, err)
	case "launch":
		if !config.WorkspaceIDPattern.MatchString(*id) || config.CheckWorkspacePath(*rel) != "" ||
			(*program != "" && !filepath.IsAbs(*program)) || !windowName.MatchString(*window) {
			return usageErr(errors.New(workspacesUsage))
		}
		dir := filepath.Join(root, filepath.FromSlash(*rel))
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			return a.workspaceResult(nil, &workspace.Error{Code: "missing", Field: "path",
				Message: "The workspace folder is missing; restore it or forget the workspace"})
		}
		err := t.Launch(ctx, *id, dir, workspace.Window{Name: *window, Run: *program})
		return a.workspaceResult(map[string]any{"session": workspace.SessionName(*id)}, err)
	case "kill":
		if !config.WorkspaceIDPattern.MatchString(*id) {
			return usageErr(errors.New(workspacesUsage))
		}
		killed, err := t.Kill(ctx, *id)
		return a.workspaceResult(map[string]any{"killed": killed}, err)
	}
	return usageErr(errors.New(workspacesUsage))
}

// workspaceResult prints res, or err with the field it concerns as its step.
func (a *app) workspaceResult(res any, err error) error {
	var we *workspace.Error
	switch {
	case errors.As(err, &we):
		return output.Errorf(output.ExitFailure, we.Code, we.Field, "%s", we.Message)
	case err != nil:
		return output.Errorf(output.ExitFailure, "workspace_failed", "", "%v", err)
	}
	return a.p.Result(res, func(w io.Writer) {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(res)
	})
}
