package broker

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"slices"
	"time"

	"github.com/berth-ai/berth/internal/agentset"
	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/registry"
	"github.com/berth-ai/berth/internal/runner"
)

const (
	// addedSyncGroup serializes agents.sync, the added-agent converger.
	addedSyncGroup  = "agents-sync"
	addedJobTimeout = 30 * time.Minute
)

// agentError is a failure of an added-agent verb, with its code.
type agentError struct{ Code, Message string }

func (e *agentError) Error() string { return e.Message }

// registry is the image's registry, read once.
func (b *Broker) registry() []registry.Entry {
	b.regOnce.Do(func() {
		if b.Registry != "" {
			b.reg = registry.LoadLogged(b.Registry, cmp.Or[io.Writer](b.Log, io.Discard))
		}
	})
	return b.reg
}

func (b *Broker) agentsArgs(more ...string) []string {
	return append(slices.Clone(b.AgentsCmd[1:]), more...)
}

// AddedReport is the cached berth agents status; the layer builder reads it.
func (b *Broker) AddedReport() agentset.Report {
	b.addedMu.Lock()
	defer b.addedMu.Unlock()
	return b.added
}

// RefreshAdded re-reads the added agents' installs by running berth agents
// status as agent: root never opens files in agent's home. A failure
// empties the cache and logs a warning.
func (b *Broker) RefreshAdded(ctx context.Context) {
	if len(b.AgentsCmd) == 0 {
		return
	}
	stdout, stderr, exit, err := b.agentRun(ctx, b.syncTimeout(), b.AgentsCmd[0], b.agentsArgs("status", "--json")...)
	var rep agentset.Report
	switch {
	case err != nil:
		b.logf("WARNING: added agents: %v", err)
	case exit != 0:
		b.logf("WARNING: added agents: berth agents status exited %d: %s", exit, lastLine(stderr))
	default:
		if rep, err = agentset.DecodeReport([]byte(stdout)); err != nil {
			b.logf("WARNING: %v", err)
		}
	}
	b.addedMu.Lock()
	b.added = rep
	b.addedMu.Unlock()
}

// writeAgentsSettings publishes added to AgentsSettings, readable by the
// agent group.
func (b *Broker) writeAgentsSettings(added []string) error {
	if b.AgentsSettings == "" {
		return nil
	}
	uid, gid := -1, -1
	if b.Agent != nil {
		uid, gid = 0, int(b.Agent.GID)
	}
	return config.WriteAgentsFile(b.AgentsSettings, added, uid, gid)
}

// agentsStale reports whether AgentsSettings differs from added.
func (b *Broker) agentsStale(added []string) bool {
	if b.AgentsSettings == "" {
		return false
	}
	pub, err := config.ReadAgentsFile(b.AgentsSettings)
	return err != nil || !slices.Equal(pub, added)
}

// startAgentsSync runs the converger as a job, then refreshes for the new set.
func (b *Broker) startAgentsSync() {
	sync := b.agentWork(b.AgentsCmd[0], b.agentsArgs("sync"), addedJobTimeout)
	work := func(ctx context.Context, out io.Writer) (int, json.RawMessage, error) {
		code, res, err := sync(ctx, out)
		b.afterAddedChange(ctx, out)
		return code, res, err
	}
	if err := b.addedRuns.start(b.Jobs, "agents.sync", addedSyncGroup, work, "The added agents changed during the run; running again."); err != nil {
		b.logf("WARNING: added agents sync not started: %v", err)
	}
}

// afterAddedChange refreshes the report, rebuilds the layer for the
// current config, and starts MCP and skills sync, in that order.
func (b *Broker) afterAddedChange(ctx context.Context, out io.Writer) {
	b.RefreshAdded(ctx)
	cfg, _, _ := b.snapshot()
	if _, err := b.Layer.Build(cfg.Agents.Disabled, cfg.Agents.Added); err != nil {
		fmt.Fprintf(out, "warning: agent layer: %v\n", err)
		return
	}
	if len(b.MCPSetup) > 0 {
		b.startMCPSetup(time.Duration(cfg.Agents.MCPTimeout) * time.Second)
	}
	b.startSkillsSync()
	b.startWorkspacesSync()
}

// checkAdded refuses packages a change newly adds to agents.added that
// this image's registry does not have; ones already added stay valid.
func (b *Broker) checkAdded(cur, next config.Config) error {
	var fields []config.FieldError
	for _, pkg := range next.Agents.Added {
		if slices.Contains(cur.Agents.Added, pkg) {
			continue
		}
		if _, ok := registry.ByPackage(b.registry(), pkg); !ok {
			fields = append(fields, config.FieldError{Field: "agents.added", Message: pkg + " is not in this image's registry"})
		}
	}
	if fields != nil {
		return &config.ValidationError{Fields: fields}
	}
	return nil
}

// addedReady answers when this broker cannot change added agents now.
func (b *Broker) addedReady() (int, any, bool) {
	if b.Layer == nil || len(b.AgentsCmd) == 0 {
		code, v := fail(http.StatusBadRequest, "invalid_request", "this broker manages no added agents")
		return code, v, false
	}
	return b.normalMode()
}

type registryRow struct {
	ID               string   `json:"id"`
	Package          string   `json:"package"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	Version          string   `json:"version"`
	Icon             string   `json:"icon,omitempty"`
	Keys             []string `json:"keys"`
	Login            []string `json:"login"`
	Added            bool     `json:"added"`
	InstalledVersion string   `json:"installed_version,omitempty"`
	Error            string   `json:"error,omitempty"`
}

func (b *Broker) agentsRegistry(_ context.Context, body []byte) (int, any) {
	if err := decode(body, &struct{}{}); err != nil {
		return badRequest(err)
	}
	cfg, _, _ := b.snapshot()
	rep := b.AddedReport()
	rows := []registryRow{}
	for _, e := range b.registry() {
		r := registryRow{ID: e.ID, Package: e.Package, Name: e.Meta.Name, Description: e.Meta.Description,
			Version: e.Version, Icon: e.Agent.Icon, Keys: append([]string{}, e.Meta.Keys...), Login: []string{},
			Added: slices.Contains(cfg.Agents.Added, e.Package), Error: rep.Errors[e.Package]}
		if e.Agent.Login != nil {
			for _, m := range e.Agent.Login.Methods {
				r.Login = append(r.Login, m.Label)
			}
		}
		for _, a := range rep.Agents {
			if a.Package == e.Package {
				r.InstalledVersion = a.Version
			}
		}
		rows = append(rows, r)
	}
	slices.SortFunc(rows, func(x, y registryRow) int { return cmp.Compare(x.Name, y.Name) })
	return http.StatusOK, map[string]any{"agents": rows}
}

func (b *Broker) agentsAdd(_ context.Context, body []byte) (int, any) {
	var in struct {
		ID string `json:"id"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	e, ok := registry.ByID(b.registry(), in.ID)
	if !ok {
		return fail(http.StatusNotFound, "unknown_agent", "no agent %q in this image's registry", in.ID)
	}
	if code, v, ok := b.addedReady(); !ok {
		return code, v
	}
	if cfg, _, _ := b.snapshot(); slices.Contains(cfg.Agents.Added, e.Package) {
		return fail(http.StatusConflict, "already_added", "%s is already added", e.Meta.Name)
	}
	// Install first; only a complete install is saved.
	return b.agentsJob("agents.add", []string{"install", "--json", e.ID}, func(ctx context.Context, _ io.Writer, stdout []byte) (json.RawMessage, error) {
		b.RefreshAdded(ctx)
		next, err := b.patchAgents(func(cur config.Agents) (map[string]any, error) {
			if slices.Contains(cur.Added, e.Package) {
				return nil, &agentError{"already_added", e.Meta.Name + " is already added"}
			}
			return map[string]any{"added": with(cur.Added, e.Package)}, nil
		})
		if err != nil {
			return nil, err
		}
		res := map[string]any{}
		_ = json.Unmarshal(stdout, &res)
		delete(res, "schema")
		res["revision"] = next.Revision
		return json.Marshal(res)
	})
}

func (b *Broker) agentsRemove(_ context.Context, body []byte) (int, any) {
	var in struct {
		Package string `json:"package"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	if !config.ValidPackage(in.Package) {
		return badRequest(fmt.Errorf("invalid package %q", in.Package))
	}
	if code, v, ok := b.addedReady(); !ok {
		return code, v
	}
	next, err := b.patchAgents(func(cur config.Agents) (map[string]any, error) {
		if !slices.Contains(cur.Added, in.Package) {
			return nil, &agentError{"not_added", in.Package + " is not an added agent"}
		}
		return map[string]any{"added": without(cur.Added, in.Package), "disabled": without(cur.Disabled, in.Package)}, nil
	})
	if err != nil {
		return agentChangeResponse(err)
	}
	return http.StatusOK, map[string]any{"config": next}
}

func (b *Broker) agentsRollback(_ context.Context, body []byte) (int, any) {
	var in struct {
		Package string `json:"package"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	if !config.ValidPackage(in.Package) {
		return badRequest(fmt.Errorf("invalid package %q", in.Package))
	}
	if code, v, ok := b.addedReady(); !ok {
		return code, v
	}
	if cfg, _, _ := b.snapshot(); !slices.Contains(cfg.Agents.Added, in.Package) {
		return fail(http.StatusNotFound, "not_added", "%s is not an added agent", in.Package)
	}
	return b.agentsJob("agents.rollback", []string{"rollback", "--json", in.Package}, func(ctx context.Context, out io.Writer, stdout []byte) (json.RawMessage, error) {
		b.afterAddedChange(ctx, out)
		return stdout, nil
	})
}

// agentsJob runs berth agents args as agent in an agents-group job: stdout
// is its result, stderr its log. then runs after a success and makes the
// job's result.
func (b *Broker) agentsJob(verb string, args []string, then func(ctx context.Context, out io.Writer, stdout []byte) (json.RawMessage, error)) (int, any) {
	full := b.agentsArgs(args...)
	shown := append([]string{b.AgentsCmd[0]}, full...)
	job, err := b.Jobs.Start(verb, agentsGroup, shown, func(ctx context.Context, out io.Writer) (int, json.RawMessage, error) {
		var stdout bytes.Buffer
		r := runner.Exec{Agent: b.Agent, Stdout: &stdout, Stderr: out}
		err := r.Run(ctx, runner.Cmd{Path: b.AgentsCmd[0], Args: full, AsAgent: true, Timeout: addedJobTimeout})
		var ee *exec.ExitError
		switch {
		case errors.As(err, &ee):
			return ee.ExitCode(), cliFailure(stdout.Bytes()), nil
		case err != nil:
			return -1, nil, err
		}
		res, err := then(ctx, out, bytes.TrimSpace(stdout.Bytes()))
		if err != nil {
			fmt.Fprintf(out, "error: %v\n", err)
			return 1, agentFailure(err), nil
		}
		return 0, res, nil
	})
	if errors.Is(err, ErrJobRunning) {
		return fail(http.StatusConflict, "job_running", "%v", err)
	}
	if err != nil {
		return fail(http.StatusInternalServerError, "internal", "%v", err)
	}
	return http.StatusAccepted, map[string]any{"job": job}
}

// patchAgents patches the agents section build returns on top of the
// current revision under the change lock, and refreshes like config.apply.
func (b *Broker) patchAgents(build func(cur config.Agents) (map[string]any, error)) (config.Config, error) {
	b.change.Lock()
	defer b.change.Unlock()
	if _, _, ok := b.normalMode(); !ok {
		return config.Config{}, &agentError{"not_ready", "Berth is in setup or recovery mode; finish setup or fix the config first"}
	}
	cur, _, _ := b.snapshot()
	agents, err := build(cur.Agents)
	if err != nil {
		return config.Config{}, err
	}
	patch, err := json.Marshal(map[string]any{"agents": agents})
	if err != nil {
		return config.Config{}, err
	}
	old, next, err := b.Store.Apply(patch, cur.Revision)
	if err != nil {
		return config.Config{}, err
	}
	b.changed(old, next, nil)
	b.refreshAgents(old, next, false)
	return next, nil
}

func agentChangeResponse(err error) (int, any) {
	var ae *agentError
	switch {
	case errors.As(err, &ae) && ae.Code == "not_added":
		return fail(http.StatusNotFound, ae.Code, "%s", ae.Message)
	case errors.As(err, &ae):
		return fail(http.StatusConflict, ae.Code, "%s", ae.Message)
	case errors.Is(err, config.ErrConflict):
		return fail(http.StatusConflict, "conflict", "%v", err)
	}
	return validationResponse(err)
}

// cliFailure turns berth's --json error into the job result.
func cliFailure(stdout []byte) json.RawMessage {
	var doc struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	if json.Unmarshal(bytes.TrimSpace(stdout), &doc) != nil || doc.Error.Code == "" {
		return errorJSON("agents_failed", "berth agents failed; see the log")
	}
	return errorJSON(doc.Error.Code, doc.Error.Message)
}

func agentFailure(err error) json.RawMessage {
	var ae *agentError
	if errors.As(err, &ae) {
		return errorJSON(ae.Code, ae.Message)
	}
	return errorJSON("apply_failed", err.Error())
}

func with(list []string, s string) []string {
	out := append(slices.Clone(list), s)
	slices.Sort(out)
	return slices.Compact(out)
}

func without(list []string, s string) []string {
	return slices.DeleteFunc(append([]string{}, list...), func(x string) bool { return x == s })
}
