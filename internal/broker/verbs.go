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
	"regexp"
	"slices"
	"time"

	"github.com/salgnt/berth/internal/config"
	"github.com/salgnt/berth/internal/supervisor"
)

// agentsGroup serializes the jobs that change the agent store.
const agentsGroup = "agents"

const checkTimeout = 2 * time.Minute

var hashPattern = regexp.MustCompile(`^[0-9a-f]{12}$`)

type verbFunc func(ctx context.Context, body []byte) (int, any)

func (b *Broker) verbs() map[string]verbFunc {
	return map[string]verbFunc{
		"update":          b.update,
		"rollback":        b.rollback,
		"gc":              b.gc,
		"update.check":    b.updateCheck,
		"service.restart": b.serviceRestart,
		"config.get":      b.configGet,
		"config.export":   b.configExport,
		"config.apply":    b.configApply,
		"config.revert":   b.configRevert,
		"config.reset":    b.configReset,
		"auth.reset": func(context.Context, []byte) (int, any) {
			return http.StatusNotImplemented, errorJSON("internal", "not yet")
		},
	}
}

// decode reads exactly one JSON object with no unknown fields; an empty body
// is {}.
func decode(body []byte, v any) error {
	if len(bytes.TrimSpace(body)) == 0 {
		body = []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid request: %w", err)
	}
	if dec.More() {
		return errTrailing
	}
	return nil
}

func fail(code int, errCode, format string, args ...any) (int, any) {
	return code, errorJSON(errCode, fmt.Sprintf(format, args...))
}

func badRequest(err error) (int, any) {
	return fail(http.StatusBadRequest, "invalid_request", "%v", err)
}

func (b *Broker) update(_ context.Context, body []byte) (int, any) {
	var in struct {
		Ref   string `json:"ref"`
		Image bool   `json:"image"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	args := []string{"update", "--json"}
	switch {
	case in.Ref != "" && in.Image:
		return badRequest(errors.New("ref and image are exclusive"))
	case in.Image:
		args = append(args, "--image")
	default:
		cfg, _, _ := b.snapshot()
		ref := cmp.Or(in.Ref, cfg.Agents.Ref)
		if !config.ValidRef(ref) {
			return badRequest(fmt.Errorf("invalid ref %q", ref))
		}
		args = append(args, "--ref", ref)
	}
	return b.startJob("update", args)
}

func (b *Broker) rollback(_ context.Context, body []byte) (int, any) {
	var in struct {
		Hash string `json:"hash"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	args := []string{"rollback", "--json"}
	if in.Hash != "" {
		if !hashPattern.MatchString(in.Hash) {
			return badRequest(fmt.Errorf("invalid hash %q", in.Hash))
		}
		args = append(args, in.Hash)
	}
	return b.startJob("rollback", args)
}

func (b *Broker) gc(_ context.Context, body []byte) (int, any) {
	if err := decode(body, &struct{}{}); err != nil {
		return badRequest(err)
	}
	return b.startJob("gc", []string{"gc", "--json"})
}

func (b *Broker) startJob(verb string, args []string) (int, any) {
	job, err := b.Jobs.Start(verb, agentsGroup, args, berthWork(b.Exec, args))
	if errors.Is(err, ErrJobRunning) {
		return fail(http.StatusConflict, "job_running", "%v", err)
	}
	if err != nil {
		return fail(http.StatusInternalServerError, "internal", "%v", err)
	}
	return http.StatusAccepted, map[string]any{"job": job}
}

// berthWork runs berth args as a job whose result is berth's --json output.
func berthWork(exec Exec, args []string) Work {
	return func(ctx context.Context, out io.Writer) (int, json.RawMessage, error) {
		var stdout bytes.Buffer
		exit, err := exec(ctx, args, &stdout, out)
		if err != nil {
			return -1, nil, err
		}
		return exit, bytes.TrimSpace(stdout.Bytes()), nil
	}
}

// updateCheck compares current with the lock on main, as berth status --check
// does. Plan 3 replaces it with a cached, ref-aware check.
func (b *Broker) updateCheck(ctx context.Context, body []byte) (int, any) {
	if err := decode(body, &struct{}{}); err != nil {
		return badRequest(err)
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	var stdout bytes.Buffer
	exit, err := b.Exec(ctx, []string{"status", "--json", "--check"}, &stdout, b.logWriter())
	raw := bytes.TrimSpace(stdout.Bytes())
	switch {
	case err != nil:
		return fail(http.StatusBadGateway, "exec_failed", "%v", err)
	case len(raw) == 0 || !json.Valid(raw):
		return fail(http.StatusBadGateway, "bad_output", "berth status printed no valid JSON")
	case exit != 0:
		return http.StatusBadGateway, json.RawMessage(raw)
	}
	var rep struct {
		Check json.RawMessage `json:"check"`
	}
	_ = json.Unmarshal(raw, &rep)
	return http.StatusOK, map[string]any{"check": rep.Check}
}

func (b *Broker) serviceRestart(_ context.Context, body []byte) (int, any) {
	var in struct {
		Name string `json:"name"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	b.change.Lock()
	defer b.change.Unlock()
	b.mu.Lock()
	specs := b.desired()
	b.mu.Unlock()
	if !slices.ContainsFunc(specs, func(s supervisor.Spec) bool { return s.Name == in.Name }) {
		return fail(http.StatusNotFound, "unknown_service", "%q is not an enabled service", in.Name)
	}
	if err := b.Services.Restart(in.Name); err != nil {
		return fail(http.StatusInternalServerError, "internal", "%v", err)
	}
	return http.StatusOK, map[string]any{"restarted": in.Name}
}

// configView is the answer to config.get.
type configView struct {
	Config config.Config `json:"config"`
	Mode   string        `json:"mode"`
	Error  string        `json:"error,omitempty"`
}

func (b *Broker) configGet(_ context.Context, body []byte) (int, any) {
	if err := decode(body, &struct{}{}); err != nil {
		return badRequest(err)
	}
	cfg, mode, modeErr := b.snapshot()
	return http.StatusOK, configView{Config: cfg, Mode: mode, Error: modeErr}
}

func (b *Broker) configExport(_ context.Context, body []byte) (int, any) {
	if err := decode(body, &struct{}{}); err != nil {
		return badRequest(err)
	}
	cfg, mode, _ := b.snapshot()
	if mode == ModeRecovery {
		return fail(http.StatusConflict, "recovery", "the config is unusable; revert it or reset it to the defaults first")
	}
	return http.StatusOK, map[string]any{"config": cfg}
}

func (b *Broker) configApply(_ context.Context, body []byte) (int, any) {
	var in struct {
		Patch        json.RawMessage `json:"patch"`
		BaseRevision *int            `json:"base_revision"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	if in.BaseRevision == nil {
		return badRequest(errors.New("base_revision is required"))
	}
	if len(in.Patch) == 0 {
		return badRequest(errors.New("patch is required"))
	}
	b.change.Lock()
	defer b.change.Unlock()
	if _, mode, _ := b.snapshot(); mode == ModeRecovery {
		return fail(http.StatusConflict, "recovery", "the config is unusable; revert it or reset it to the defaults first")
	}
	old, next, err := b.Store.Apply(in.Patch, *in.BaseRevision)
	return b.changed(old, next, err)
}

func (b *Broker) configRevert(_ context.Context, body []byte) (int, any) {
	if err := decode(body, &struct{}{}); err != nil {
		return badRequest(err)
	}
	b.change.Lock()
	defer b.change.Unlock()
	old, next, err := b.Store.Revert()
	return b.changed(old, next, err)
}

func (b *Broker) configReset(_ context.Context, body []byte) (int, any) {
	if err := decode(body, &struct{}{}); err != nil {
		return badRequest(err)
	}
	b.change.Lock()
	defer b.change.Unlock()
	old, next, err := b.Store.Reset()
	return b.changed(old, next, err)
}

// changed answers a store change. On success it leaves recovery mode and
// reconciles the services. The caller holds b.change.
func (b *Broker) changed(old, next config.Config, err error) (int, any) {
	var ve *config.ValidationError
	switch {
	case errors.As(err, &ve):
		return http.StatusBadRequest, map[string]any{"error": map[string]any{
			"code": "invalid_config", "message": ve.Error(), "fields": ve.Fields}}
	case errors.Is(err, config.ErrConflict):
		return http.StatusConflict, map[string]any{"error": map[string]any{
			"code": "conflict", "message": err.Error()}, "config": old}
	case errors.Is(err, config.ErrNoPrevious):
		return fail(http.StatusConflict, "no_previous", "%v", err)
	case err != nil:
		return fail(http.StatusInternalServerError, "write_failed", "%v", err)
	}
	b.mu.Lock()
	b.cfg, b.mode, b.modeErr = next, ModeNormal, ""
	specs := b.desired()
	b.mu.Unlock()
	ch := b.Services.Reconcile(specs)
	b.logf("config revision %d applied (started %v, restarted %v, stopped %v)",
		next.Revision, ch.Started, ch.Restarted, ch.Stopped)
	return http.StatusOK, map[string]any{"config": next, "changes": ch}
}
