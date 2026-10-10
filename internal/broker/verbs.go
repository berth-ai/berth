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

	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/supervisor"
)

// agentsGroup serializes the jobs that change the agent store.
const agentsGroup = "agents"

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
		"skills.add":      b.skillsAdd,
		"agents.registry": b.agentsRegistry,
		"agents.add":      b.agentsAdd,
		"agents.remove":   b.agentsRemove,
		"agents.rollback": b.agentsRollback,
		"skills.scan":     b.skillsScan,
		"skills.update":   b.skillsUpdate,
		"config.revert":   b.configRevert,
		"config.reset":    b.configReset,
		"auth.verify":     b.authVerify,
		"auth.change":     b.authChange,
		"setup.complete":  b.setupComplete,
		"auth.reset":      b.authReset,
		"tools.list":      b.toolsList,
		"tools.versions":  b.toolsVersions,
		"tools.registry":  b.toolsRegistry,
		"tools.add":       b.toolsAdd,
		"tools.remove":    b.toolsRemove,
		"preset.install":  b.presetInstall,
		"git.identity":    b.gitIdentity,
		"forge.status":    b.forgeStatus,
		"forge.login":     b.forgeLogin,
		"login.status":    b.loginStatus,
		"login.start":     b.loginStart,
		"login.input":     b.loginInput,
		"login.cancel":    b.loginCancel,
		"login.logout":    b.loginLogout,
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
	Config  config.Config              `json:"config"`
	Mode    string                     `json:"mode"`
	Error   string                     `json:"error,omitempty"`
	Secrets map[string]map[string]bool `json:"secrets"`
}

func (b *Broker) configGet(_ context.Context, body []byte) (int, any) {
	if err := decode(body, &struct{}{}); err != nil {
		return badRequest(err)
	}
	cfg, mode, modeErr := b.snapshot()
	return http.StatusOK, configView{Config: cfg, Mode: mode, Error: modeErr, Secrets: b.secretStates(cfg)}
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
		Patch        json.RawMessage    `json:"patch"`
		BaseRevision *int               `json:"base_revision"`
		Secrets      map[string]*string `json:"secrets"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	hasPatch := len(in.Patch) > 0 && string(in.Patch) != "null"
	switch {
	case !hasPatch && len(in.Secrets) == 0:
		return badRequest(errors.New("send a patch, secrets, or both"))
	case hasPatch && in.BaseRevision == nil:
		return badRequest(errors.New("base_revision is required with a patch"))
	case len(in.Secrets) > 0 && b.Layer == nil:
		return badRequest(errors.New("this broker manages no agent keys"))
	}
	b.change.Lock()
	defer b.change.Unlock()
	cur, mode, _ := b.snapshot()
	switch mode {
	case ModeSetup:
		return fail(http.StatusConflict, "setup_required", "no login exists yet; finish setup first")
	case ModeRecovery:
		return fail(http.StatusConflict, "recovery", "the config is unusable; revert it or reset it to the defaults first")
	}
	preview := cur
	if hasPatch {
		if p, err := config.Patch(cur, in.Patch); err == nil {
			preview = p // Store.Apply reports a bad patch
		}
	}
	if err := b.checkAdded(cur, preview); err != nil {
		return validationResponse(err)
	}
	if err := b.checkSecrets(in.Secrets, cur, preview); err != nil {
		return validationResponse(err)
	}
	old, next := cur, cur
	if hasPatch {
		var err error
		if old, next, err = b.Store.Apply(in.Patch, *in.BaseRevision); err != nil {
			return b.changed(old, next, err)
		}
	}
	secrets := in.Secrets
	if gone := staleTokens(old.MCP, next.MCP); len(gone) > 0 && b.Layer != nil {
		if secrets == nil {
			secrets = map[string]*string{}
		}
		for name := range gone {
			if _, set := secrets[name]; !set {
				secrets[name] = nil
			}
		}
	}
	if len(secrets) > 0 {
		if err := b.writeSecrets(secrets); err != nil {
			return fail(http.StatusInternalServerError, "write_failed", "%v", err)
		}
	}
	code, v := b.changed(old, next, nil)
	b.refreshAgents(old, next, touchesTokens(secrets, old.MCP, next.MCP))
	return code, v
}

func (b *Broker) configRevert(_ context.Context, body []byte) (int, any) {
	if err := decode(body, &struct{}{}); err != nil {
		return badRequest(err)
	}
	b.change.Lock()
	defer b.change.Unlock()
	if _, mode, _ := b.snapshot(); mode == ModeSetup {
		return fail(http.StatusConflict, "setup_required", "no login exists yet; finish setup first")
	}
	before, _, _ := b.snapshot()
	old, next, err := b.Store.Revert()
	code, v := b.changed(old, next, err)
	if code == http.StatusOK {
		b.refreshAgents(before, next, false)
	}
	return code, v
}

func (b *Broker) configReset(_ context.Context, body []byte) (int, any) {
	if err := decode(body, &struct{}{}); err != nil {
		return badRequest(err)
	}
	b.change.Lock()
	defer b.change.Unlock()
	if _, mode, _ := b.snapshot(); mode == ModeSetup {
		return fail(http.StatusConflict, "setup_required", "no login exists yet; finish setup first")
	}
	before, _, _ := b.snapshot()
	old, next, err := b.Store.Reset()
	code, v := b.changed(old, next, err)
	if code == http.StatusOK {
		b.refreshAgents(before, next, false)
	}
	return code, v
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

// applyPatch applies the patch build returns on top of the current
// revision in one change, under the change lock, and refreshes like
// config.apply. A nil patch changes nothing. It returns the config after
// and whether it changed.
func (b *Broker) applyPatch(build func(cur config.Config) (map[string]any, error)) (config.Config, bool, error) {
	b.change.Lock()
	defer b.change.Unlock()
	if _, _, ok := b.normalMode(); !ok {
		return config.Config{}, false, &fieldError{Code: "not_ready", Message: "Berth is in setup or recovery mode; finish setup or fix the config first"}
	}
	cur, _, _ := b.snapshot()
	patch, err := build(cur)
	if err != nil || patch == nil {
		return cur, false, err
	}
	raw, err := json.Marshal(patch)
	if err != nil {
		return cur, false, err
	}
	old, next, err := b.Store.Apply(raw, cur.Revision)
	if err != nil {
		return cur, false, err
	}
	b.changed(old, next, nil)
	b.refreshAgents(old, next, false)
	return next, true, nil
}
