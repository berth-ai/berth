package broker

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/berth-ai/berth/internal/agentlayer"
	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/workspace"
)

func (b *Broker) workspacesRoot() string { return cmp.Or(b.WorkspacesRoot, "/workspaces") }

// writeWorkspacesSettings publishes where each workspace lives, readable
// by the agent group.
func (b *Broker) writeWorkspacesSettings(ws map[string]config.Workspace) error {
	if b.WorkspacesSettings == "" {
		return nil
	}
	uid, gid := -1, -1
	if b.Agent != nil {
		uid, gid = 0, int(b.Agent.GID)
	}
	return config.WriteWorkspacesFile(b.WorkspacesSettings, config.PublishWorkspaces(b.workspacesRoot(), ws), uid, gid)
}

// workspacesStale reports whether WorkspacesSettings differs from ws.
func (b *Broker) workspacesStale(ws map[string]config.Workspace) bool {
	if b.WorkspacesSettings == "" {
		return false
	}
	pub, err := config.ReadWorkspacesFile(b.WorkspacesSettings)
	return err != nil || !maps.Equal(pub, config.PublishWorkspaces(b.workspacesRoot(), ws))
}

const (
	// workspacesGroup serializes workspaces.add and workspaces.forget.
	workspacesGroup = "workspaces"
	probeTimeout    = time.Minute
)

// workspaceConflict says why workspace id cannot be added at path to cfg;
// nil means it fits.
func workspaceConflict(cfg config.Config, id, path string) *fieldError {
	if !config.WorkspaceIDPattern.MatchString(id) {
		return &fieldError{Code: "invalid_request", Field: "id", Message: "The id must be 1 to 64 lowercase letters, digits and dashes"}
	}
	if msg := config.CheckWorkspacePath(path); msg != "" {
		return &fieldError{Code: "invalid_request", Field: "path", Message: "The path " + msg}
	}
	if _, ok := cfg.Workspaces[id]; ok {
		return &fieldError{Code: "id_exists", Field: "id", Message: fmt.Sprintf("A workspace named %s exists; choose another id", id)}
	}
	for _, other := range slices.Sorted(maps.Keys(cfg.Workspaces)) {
		switch o := cfg.Workspaces[other].Path; {
		case o == path:
			return &fieldError{Code: "path_taken", Field: "path", Message: fmt.Sprintf("Workspace %s already uses this folder", other)}
		case config.PathsOverlap(o, path):
			return &fieldError{Code: "nested", Field: "path", Message: fmt.Sprintf("This folder overlaps workspace %s (%s); workspaces cannot nest", other, o)}
		}
	}
	return nil
}

// rejectJob answers a job verb that cannot start. It is never 409, which
// the panel reads as another job running.
func rejectJob(fe *fieldError) (int, any) {
	return http.StatusBadRequest, map[string]any{"error": map[string]any{"code": fe.Code, "message": fe.Message,
		"fields": []config.FieldError{{Field: fe.Field, Message: fe.Message}}}}
}

func (b *Broker) workspacesAdd(_ context.Context, body []byte) (int, any) {
	var in struct {
		Mode string `json:"mode"`
		Repo string `json:"repo"`
		Ref  string `json:"ref"`
		Path string `json:"path"`
		ID   string `json:"id"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	var args []string
	timeout := probeTimeout
	switch in.Mode {
	case "clone":
		if msg := config.CheckSkillRepo(in.Repo); msg != "" {
			return rejectJob(&fieldError{Code: "invalid_request", Field: "repo", Message: "The repository URL " + msg})
		}
		if in.Ref != "" && !config.ValidGitRef(in.Ref) {
			return rejectJob(&fieldError{Code: "invalid_request", Field: "ref", Message: "The ref must be a branch, tag or commit"})
		}
		args = []string{"workspaces", "clone", "--json", "--repo", in.Repo, "--path", in.Path}
		if in.Ref != "" {
			args = append(args, "--ref", in.Ref)
		}
		timeout = workspace.CloneTimeout
	case "adopt":
		if in.Repo != "" || in.Ref != "" {
			return badRequest(errors.New("an adopted folder takes no repo or ref"))
		}
		args = []string{"workspaces", "probe", "--json", "--path", in.Path}
	default:
		return rejectJob(&fieldError{Code: "invalid_request", Field: "mode", Message: "The mode must be clone or adopt"})
	}
	if code, v, ok := b.normalMode(); !ok {
		return code, v
	}
	cur, _, _ := b.snapshot()
	if fe := workspaceConflict(cur, in.ID, in.Path); fe != nil {
		return rejectJob(fe)
	}
	return b.cliJob("workspaces.add", workspacesGroup, args, timeout, func(_ context.Context, _ io.Writer, stdout []byte) (json.RawMessage, error) {
		p := workspace.Probe{Writable: true, Git: true} // agent made the clone
		if in.Mode == "adopt" {
			if err := json.Unmarshal(stdout, &p); err != nil {
				return nil, &fieldError{Code: "bad_output", Message: "berth workspaces probe printed no result"}
			}
		}
		next, _, err := b.applyPatch(func(cur config.Config) (map[string]any, error) {
			if fe := workspaceConflict(cur, in.ID, in.Path); fe != nil {
				return nil, fe
			}
			return map[string]any{"workspaces": map[string]any{in.ID: map[string]any{"path": in.Path, "repo": in.Repo}}}, nil
		})
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"id": in.ID, "path": in.Path, "writable": p.Writable, "git": p.Git,
			"changed": true, "revision": next.Revision})
	})
}

// statusTimeout leaves berth workspaces status its budget plus a start.
const statusTimeout = workspace.StatusBudget + 5*time.Second

// forgetPatch removes workspace id and every reference to it: skill
// attachments and MCP workspace lists, in one patch.
func forgetPatch(cur config.Config, id string) map[string]any {
	patch := map[string]any{"workspaces": map[string]any{id: nil}}
	sources := map[string]any{}
	for sid, src := range cur.Skills.Sources {
		attached := map[string]any{}
		for name, a := range src.Attached {
			if _, ok := a.Workspaces[id]; ok {
				attached[name] = map[string]any{"workspaces": map[string]any{id: nil}}
			}
		}
		if len(attached) > 0 {
			sources[sid] = map[string]any{"attached": attached}
		}
	}
	if len(sources) > 0 {
		patch["skills"] = map[string]any{"sources": sources}
	}
	lists := map[string]any{}
	for server, list := range cur.MCP.Workspaces {
		if !slices.Contains(list, id) {
			continue
		}
		if rest := slices.DeleteFunc(slices.Clone(list), func(w string) bool { return w == id }); len(rest) > 0 {
			lists[server] = rest
		} else {
			lists[server] = nil
		}
	}
	if len(lists) > 0 {
		patch["mcp"] = map[string]any{"workspaces": lists}
	}
	return patch
}

func (b *Broker) workspacesForget(_ context.Context, body []byte) (int, any) {
	var in struct {
		ID      string `json:"id"`
		Delete  bool   `json:"delete"`
		Confirm string `json:"confirm"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	if code, v, ok := b.normalMode(); !ok {
		return code, v
	}
	cur, _, _ := b.snapshot()
	w, ok := cur.Workspaces[in.ID]
	switch {
	case !ok:
		return fail(http.StatusNotFound, "unknown_workspace", "No workspace named %s", in.ID)
	case in.Delete && in.Confirm != w.Path:
		return rejectJob(&fieldError{Code: "confirm_mismatch", Field: "confirm", Message: "Type the folder's path, " + w.Path + ", to delete it"})
	case !in.Delete && in.Confirm != "":
		return badRequest(errors.New("confirm goes with delete"))
	}
	args := []string{"workspaces", "kill", "--json", "--id", in.ID}
	return b.cliJob("workspaces.forget", workspacesGroup, args, b.syncTimeout(), func(ctx context.Context, log io.Writer, _ []byte) (json.RawMessage, error) {
		next, _, err := b.applyPatch(func(cur config.Config) (map[string]any, error) {
			if now, ok := cur.Workspaces[in.ID]; !ok || now.Path != w.Path {
				return nil, &fieldError{Code: "unknown_workspace", Message: fmt.Sprintf("The workspace %s was removed or changed meanwhile", in.ID)}
			}
			return forgetPatch(cur, in.ID), nil
		})
		if err != nil {
			return nil, err
		}
		res := map[string]any{"id": in.ID, "path": w.Path, "deleted": false, "left": []string{}, "revision": next.Revision}
		if !in.Delete {
			return json.Marshal(res)
		}
		rmArgs := []string{"workspaces", "remove", "--json", "--path", w.Path}
		stdout, code, err := b.runCLI(ctx, log, rmArgs, workspace.RemoveTimeout)
		var rm workspace.Removal
		switch {
		case err != nil:
			return nil, err
		case code != 0:
			return nil, cliError(stdout, rmArgs)
		case json.Unmarshal(stdout, &rm) != nil:
			return nil, &fieldError{Code: "bad_output", Message: "berth workspaces remove printed no result"}
		case !rm.Removed:
			return nil, &fieldError{Code: "delete_incomplete", Field: "confirm", Left: rm.Left,
				Message: fmt.Sprintf("Berth forgot %s, but agent could not delete everything in /workspaces/%s", in.ID, w.Path)}
		}
		res["deleted"] = true
		return json.Marshal(res)
	})
}

func (b *Broker) workspacesStatus(ctx context.Context, body []byte) (int, any) {
	if err := decode(body, &struct{}{}); err != nil {
		return badRequest(err)
	}
	cur, _, _ := b.snapshot()
	states := make(map[string]workspace.State, len(cur.Workspaces))
	if len(cur.Workspaces) == 0 {
		return http.StatusOK, map[string]any{"workspaces": states}
	}
	args := []string{"workspaces", "status", "--json"}
	for _, id := range slices.Sorted(maps.Keys(cur.Workspaces)) {
		args = append(args, "--ws", id+"="+cur.Workspaces[id].Path)
	}
	var res struct {
		Workspaces map[string]workspace.State `json:"workspaces"`
	}
	stdout, stderr, code, err := b.agentRun(ctx, statusTimeout, b.Home.Berth, args...)
	if err != nil || code != 0 || json.Unmarshal([]byte(stdout), &res) != nil {
		b.logf("WARNING: workspace status: %v %s", err, strings.TrimSpace(stderr))
	}
	for id := range cur.Workspaces {
		st, ok := res.Workspaces[id]
		if !ok {
			st = workspace.State{Unknown: true, Dirty: -1, Windows: []string{}}
		}
		states[id] = st
	}
	return http.StatusOK, map[string]any{"workspaces": states}
}

func (b *Broker) workspacesCandidates(ctx context.Context, body []byte) (int, any) {
	if err := decode(body, &struct{}{}); err != nil {
		return badRequest(err)
	}
	cur, _, _ := b.snapshot()
	args := []string{"workspaces", "candidates", "--json"}
	for _, id := range slices.Sorted(maps.Keys(cur.Workspaces)) {
		args = append(args, "--exclude", cur.Workspaces[id].Path)
	}
	var res struct {
		Candidates []workspace.Candidate `json:"candidates"`
	}
	stdout, _, code, err := b.agentRun(ctx, b.syncTimeout(), b.Home.Berth, args...)
	switch {
	case err != nil:
		return fail(http.StatusInternalServerError, "candidates_failed", "Could not list /workspaces: %v", err)
	case code != 0:
		return fail(http.StatusInternalServerError, "candidates_failed", "Could not list /workspaces: %s", cliError([]byte(stdout), args).Message)
	case json.Unmarshal([]byte(stdout), &res) != nil:
		return fail(http.StatusInternalServerError, "candidates_failed", "berth workspaces candidates printed no result")
	}
	if res.Candidates == nil {
		res.Candidates = []workspace.Candidate{}
	}
	return http.StatusOK, map[string]any{"candidates": res.Candidates}
}

func (b *Broker) workspacesLaunch(ctx context.Context, body []byte) (int, any) {
	var in struct {
		ID    string `json:"id"`
		Agent string `json:"agent"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	if code, v, ok := b.normalMode(); !ok {
		return code, v
	}
	cur, _, _ := b.snapshot()
	w, ok := cur.Workspaces[in.ID]
	if !ok {
		return fail(http.StatusNotFound, "unknown_workspace", "No workspace named %s", in.ID)
	}
	args := []string{"workspaces", "launch", "--json", "--id", in.ID, "--path", w.Path}
	window := ""
	if in.Agent != "" {
		bin, code, v := b.launchBin(cur, in.Agent)
		if bin == "" {
			return code, v
		}
		window = bin
		args = append(args, "--window", bin, "--run", filepath.Join(b.Layer.Root, "bin", bin))
	}
	stdout, stderr, code, err := b.agentRun(ctx, b.syncTimeout(), b.Home.Berth, args...)
	switch {
	case err != nil:
		return fail(http.StatusInternalServerError, "launch_failed", "%v", err)
	case code != 0:
		fe := cliError([]byte(stdout), args)
		if fe.Code == "missing" {
			return fail(http.StatusConflict, "workspace_missing", "%s", fe.Message)
		}
		return fail(http.StatusInternalServerError, "launch_failed", "%s", cmp.Or(strings.TrimSpace(stderr), fe.Message))
	}
	return http.StatusOK, map[string]any{"session": workspace.SessionName(in.ID), "window": window}
}

// launchBin is the program that starts agent pkg: its first bin, as
// agentset.EnabledBin picks it. On refusal it returns "" and the answer.
func (b *Broker) launchBin(cur config.Config, pkg string) (string, int, any) {
	if b.Layer == nil {
		code, v := badRequest(errors.New("this broker manages no agents"))
		return "", code, v
	}
	pkgs, err := b.Layer.Packages(cur.Agents.Disabled, cur.Agents.Added)
	if err != nil {
		code, v := fail(http.StatusInternalServerError, "internal", "%v", err)
		return "", code, v
	}
	i := slices.IndexFunc(pkgs, func(p agentlayer.Package) bool { return p.Package == pkg && p.Kind == "agent" })
	var (
		code int
		v    any
	)
	switch {
	case i < 0:
		code, v = fail(http.StatusBadRequest, "unknown_agent", "%s is not an installed agent", pkg)
	case !pkgs[i].Enabled:
		code, v = fail(http.StatusConflict, "agent_disabled", "%s is switched off; turn it on on the Agents page", pkgs[i].Name)
	case pkgs[i].Error != "" || len(pkgs[i].Bins) == 0:
		code, v = fail(http.StatusConflict, "agent_unavailable", "%s cannot start: %s", pkgs[i].Name, cmp.Or(pkgs[i].Error, "it has no program"))
	default:
		return pkgs[i].Bins[0], 0, nil
	}
	return "", code, v
}
