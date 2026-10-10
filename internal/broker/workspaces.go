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
	"slices"
	"time"

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
