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
	"github.com/berth-ai/berth/internal/instructions"
	"github.com/berth-ai/berth/internal/mcpsync"
	"github.com/berth-ai/berth/internal/output"
	"github.com/berth-ai/berth/internal/runner"
	"github.com/berth-ai/berth/internal/skills"
	"github.com/berth-ai/berth/internal/workspace"
	"github.com/berth-ai/berth/internal/wsstate"
)

const workspacesUsage = "usage: berth workspaces clone --repo URL [--ref REF] --path P | probe --path P | " +
	"status [--ws ID=PATH]... | candidates [--exclude PATH]... | remove --path P | " +
	"launch --id ID --path P [--window NAME] [--run PROGRAM] | kill --id ID | release --id ID | report --id ID | " +
	"instructions --action read|write|merge --id ID --file F [--base-sha256 H] | sync, each with --json"

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
	action := fs.String("action", "", "read, write or merge")
	file := fs.String("file", "", "instruction file name")
	base := fs.String("base-sha256", "", "sha256 of the file the content replaces; empty for a new file")
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
	case "sync":
		return a.workspacesSync(ctx)
	case "instructions":
		if !config.WorkspaceIDPattern.MatchString(*id) || !instructions.ValidName(*file) {
			return usageErr(errors.New(workspacesUsage))
		}
		return a.workspaceInstructions(ctx, *action, *id, *file, *base)
	case "release", "report":
		if !config.WorkspaceIDPattern.MatchString(*id) {
			return usageErr(errors.New(workspacesUsage))
		}
		home := a.cfg.Getenv("HOME")
		if home == "" {
			return output.Errorf(output.ExitFailure, "no_home", "workspaces", "HOME is not set")
		}
		store := wsstate.Store{Home: home, Published: filepath.Join(a.cfg.ConfigDir, "workspaces.json")}
		if args[0] == "report" {
			return a.workspaceResult(workspaceReport(store, *id))
		}
		kept := []string{}
		err := store.Release(*id, func(st *wsstate.State) {
			kept = append(kept, skills.Syncer{Home: home, Log: a.p.Stderr}.Release(*id, &st.Skills)...)
			kept = append(kept, mcpsync.Syncer{Home: home, Log: a.p.Stderr}.Release(&st.MCP)...)
			kept = append(kept, instructions.Syncer{Home: home, Log: a.p.Stderr}.Release(&st.Instructions)...)
		})
		return a.workspaceResult(map[string]any{"kept": kept}, err)
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

// workspaceReport is what the panel shows of a workspace's state: no file
// hashes and no copies of the user's files.
func workspaceReport(store wsstate.Store, id string) (any, error) {
	st, err := store.Load(id)
	if err != nil {
		return nil, err
	}
	for k, e := range st.Skills.Entries {
		e.Files = nil
		st.Skills.Entries[k] = e
	}
	for k, f := range st.MCP.Files {
		f.Original = ""
		st.MCP.Files[k] = f
	}
	return map[string]any{"skills": st.Skills, "mcp": st.MCP, "instructions": st.Instructions}, nil
}

// setupWorkspaces converges shared instructions at boot; failures are
// logged, never fatal.
func (a *app) setupWorkspaces(ctx context.Context) {
	if len(a.cfg.WorkspacesSync) == 0 {
		return
	}
	cmd := runner.Cmd{Path: a.cfg.WorkspacesSync[0], Args: a.cfg.WorkspacesSync[1:], AsAgent: true, Timeout: instructions.SyncTimeout}
	if err := a.cfg.Runner.Run(ctx, cmd); err != nil {
		a.p.Logf("warning: workspaces setup failed: %v", err)
	}
}

// workspacesSync is the instructions converger.
func (a *app) workspacesSync(ctx context.Context) error {
	home := a.cfg.Getenv("HOME")
	if home == "" {
		return output.Errorf(output.ExitFailure, "no_home", "workspaces", "HOME is not set")
	}
	set, err := a.agentSet(home)
	if err != nil {
		return output.Errorf(output.ExitFailure, "internal", "workspaces", "%v", err)
	}
	s := instructions.Syncer{Set: set, Layer: filepath.Join(a.cfg.ConfigDir, "bin"), Home: home,
		Workspaces: filepath.Join(a.cfg.ConfigDir, "workspaces.json"), Log: a.p.Stderr}
	if err := s.Run(ctx); err != nil {
		return output.Errorf(output.ExitFailure, "instructions_failed", "workspaces", "%v", err)
	}
	return a.workspaceResult(map[string]any{"synced": true}, nil)
}

// workspaceInstructions reads, writes or merges one instruction file.
func (a *app) workspaceInstructions(ctx context.Context, action, id, file, base string) error {
	home := a.cfg.Getenv("HOME")
	if home == "" {
		return output.Errorf(output.ExitFailure, "no_home", "workspaces", "HOME is not set")
	}
	store := wsstate.Store{Home: home, Published: filepath.Join(a.cfg.ConfigDir, "workspaces.json")}
	var (
		res any
		err error
	)
	switch action {
	case "read":
		res, err = instructions.Read(ctx, store, "", id, file)
	case "write":
		var raw []byte
		if a.cfg.Stdin != nil {
			raw, err = io.ReadAll(io.LimitReader(a.cfg.Stdin, instructions.MaxSize+1))
		}
		if err == nil {
			var sum string
			sum, err = instructions.Write(store, id, file, string(raw), base)
			res = map[string]any{"file": file, "sha256": sum}
		}
	case "merge":
		err = instructions.Merge(store, id, file)
		res = map[string]any{"file": file, "merged": true}
	default:
		return usageErr(errors.New(workspacesUsage))
	}
	var ie *instructions.Error
	switch {
	case errors.As(err, &ie):
		return output.Errorf(output.ExitFailure, ie.Code, "file", "%s", ie.Message)
	case errors.Is(err, wsstate.ErrGone):
		return output.Errorf(output.ExitFailure, "unknown_workspace", "id", "There is no workspace %s", id)
	}
	return a.workspaceResult(res, err)
}
