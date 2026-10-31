// Package broker is the root side of the web panel, served by berth
// supervise on a unix socket that only root and berth-panel can reach. It
// accepts a fixed set of verbs, builds every argv itself and never runs a
// shell. It owns the config store and decides which services run.
package broker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/berth-ai/berth/internal/agentlayer"
	"github.com/berth-ai/berth/internal/agentset"
	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/locksource"
	"github.com/berth-ai/berth/internal/login"
	"github.com/berth-ai/berth/internal/registry"
	"github.com/berth-ai/berth/internal/runner"
	"github.com/berth-ai/berth/internal/status"
	"github.com/berth-ai/berth/internal/supervisor"
)

const maxBodyBytes = 64 << 10

// bodyLimit is the largest request body verb accepts: an instruction file
// is up to 256 KiB, which JSON escaping can grow several times.
func bodyLimit(verb string) int64 {
	if verb == "workspaces.instructions.write" {
		return 2 << 20
	}
	return maxBodyBytes
}

// rootOnlyVerbs may be called only by root (berth config inside the
// container), never by the panel.
var rootOnlyVerbs = map[string]bool{"auth.reset": true}

// Modes.
const (
	ModeNormal   = "normal"
	ModeRecovery = "recovery"
)

// Exec runs berth with args and returns its exit code. err is non-nil only
// when the process could not be run at all.
type Exec func(ctx context.Context, args []string, stdout, stderr io.Writer) (exit int, err error)

// Services is the part of the supervisor the broker drives.
type Services interface {
	Reconcile(specs []supervisor.Spec) supervisor.Changes
	Restart(name string) error
	Status() []supervisor.Status
}

// Broker serves the verbs. Create it with New, call Load once, and call
// Reconcile when boot work is done.
type Broker struct {
	Store    *config.Store
	Services Services
	Jobs     *Jobs
	Exec     Exec
	// Agents reports the agent store; nil reports nothing.
	Agents func() (status.Report, error)
	// PanelEnv is added to the panel service's settings.
	PanelEnv map[string]string
	Logins   login.Store
	// Host is shown in status and used for the setup link.
	Host Host
	Log  io.Writer
	// PeerUID identifies the caller; it defaults to the uid ConnContext stored.
	PeerUID func(r *http.Request) (uid int, ok bool)
	// Layer builds /var/lib/berth/bin; nil disables agent toggles and keys.
	Layer *agentlayer.Builder
	// Agent is who agent-side jobs run as; nil runs them as the caller.
	Agent *runner.Identity
	// MCPSetup is the MCP converger's argv; empty skips MCP registration.
	MCPSetup []string
	// MCPSettings is where the mcp section is published for the converger,
	// /var/lib/berth/mcp.json; empty skips it.
	MCPSettings string
	// SkillsSync is the skills converger's argv; empty skips it.
	SkillsSync []string
	// SkillsSettings is where the skills section is published for the
	// converger, /var/lib/berth/skills.json; empty skips it.
	SkillsSettings string
	// WorkspacesSync is the instructions converger's argv; empty skips it.
	WorkspacesSync []string
	// Registry is the image's registry of addable agents,
	// /usr/share/berth/registry; empty means none.
	Registry string
	// AgentsSettings is where agents.added is published for the agent
	// side, /var/lib/berth/agents.json; empty skips it.
	AgentsSettings string
	// AgentsCmd is the argv prefix of berth agents, run as agent; empty
	// means this broker manages no added agents.
	AgentsCmd []string
	// WorkspacesRoot is the folder workspace paths are relative to,
	// /workspaces; empty means that.
	WorkspacesRoot string
	// WorkspacesSettings is where the workspace folders are published for
	// berth-term and the agent side, /var/lib/berth/workspaces.json; empty
	// skips it.
	WorkspacesSettings string
	// WorkspaceSecrets holds each workspace's secret variables,
	// /var/lib/berth/secrets/workspaces; empty refuses them.
	WorkspaceSecrets string
	// Fetch reads the agent lock on a ref for update.check.
	Fetch func(ctx context.Context, ref string) (*locksource.Lock, error)
	// Home holds the programs home-side verbs run as agent.
	Home HomeCommands
	// BaseTools and PresetsDir are the pins shipped in the image.
	BaseTools  string
	PresetsDir string
	// SyncTimeout bounds synchronous agent commands; 0 means one minute.
	SyncTimeout time.Duration
	// LoginTimeout bounds forge.login; 0 means 15 minutes.
	LoginTimeout time.Duration
	// OrcaInstalled reports whether the image ships Orca; nil means it does.
	OrcaInstalled func() bool
	// OrcaRunDir holds the pairing request and readiness files,
	// /run/berth/orca; empty means that.
	OrcaRunDir string
	// OrcaPairTimeout bounds orca.pair; 0 means one minute.
	OrcaPairTimeout time.Duration
	// OrcaSync is the argv of the Orca projects converger; empty skips it.
	OrcaSync []string
	// OrcaHome is the home whose .config/orca holds Orca's paired devices; empty
	// means agent's.
	OrcaHome string
	// Now and CurrentHash default to the clock and the current agent set.
	Now         func() time.Time
	CurrentHash func() string

	change  sync.Mutex // serializes config changes and service restarts
	mu      sync.Mutex // guards the fields below
	cfg     config.Config
	mode    string
	modeErr string
	token   string

	verifyMu   sync.Mutex
	lastVerify time.Time

	checkMu sync.Mutex
	checks  map[string]cachedLock // update.check results by ref

	mcpRuns    serialJob // mcp.setup
	skillsRuns serialJob // skills.sync
	wsRuns     serialJob // workspaces.sync
	orcaRuns   serialJob // orca.sync
	regOnce    sync.Once
	reg        []registry.Entry
	addedMu    sync.Mutex
	added      agentset.Report // berth agents status, refreshed after each change
	addedRuns  serialJob       // agents.sync
}

// New returns a broker in normal mode with the default config and the
// default host values.
func New(store *config.Store, logins login.Store, services Services, jobs *Jobs, exec Exec) *Broker {
	if jobs != nil {
		jobs.Annotate = func(job *Job, log string) {
			switch job.Verb {
			case "forge.login":
				job.Device = ParseDevice(log)
			case loginVerb:
				job.Login = ParseLogin(job.Args, log)
			}
		}
	}
	return &Broker{Store: store, Logins: logins, Services: services, Jobs: jobs, Exec: exec,
		Host: HostFromEnv(func(string) string { return "" }),
		cfg:  config.Defaults(), mode: ModeNormal, PeerUID: peerFromContext, checks: map[string]cachedLock{}}
}

// Load reads the login and the config and picks the mode: setup without a
// login, recovery with an unusable config, normal otherwise.
func (b *Broker) Load() config.Config {
	cfg, err := b.Store.Load()
	hasLogin, loginErr := b.Logins.Exists()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cfg, b.mode, b.modeErr = cfg, ModeNormal, ""
	if err != nil {
		b.cfg, b.modeErr = config.Defaults(), err.Error()
	}
	switch {
	case loginErr != nil:
		b.mode, b.modeErr = ModeRecovery, "panel login: "+loginErr.Error()
		b.logf("RECOVERY MODE: cannot read the panel login: %v", loginErr)
	case !hasLogin:
		b.enterSetup()
	case err != nil:
		b.mode = ModeRecovery
		b.logf("RECOVERY MODE: %v; only the panel runs until the config is reverted or reset", err)
	}
	if err == nil {
		// Repair a missing or stale copy; never publish recovery defaults.
		if werr := b.writeMCPSettings(cfg.MCP); werr != nil {
			b.logf("WARNING: MCP settings: %v", werr)
		}
		if werr := b.writeSkillsSettings(cfg.Skills); werr != nil {
			b.logf("WARNING: skills settings: %v", werr)
		}
		if werr := b.writeAgentsSettings(cfg.Agents.Added); werr != nil {
			b.logf("WARNING: agent settings: %v", werr)
		}
		if werr := b.writeWorkspacesSettings(cfg.Workspaces); werr != nil {
			b.logf("WARNING: workspace settings: %v", werr)
		}
		b.pruneWorkspaceSecrets(cfg)
	}
	return b.cfg
}

// Reconcile starts or adjusts the services for the current config and mode.
func (b *Broker) Reconcile() supervisor.Changes {
	b.change.Lock()
	defer b.change.Unlock()
	b.mu.Lock()
	specs := b.desired()
	b.mu.Unlock()
	ch := b.Services.Reconcile(specs)
	b.afterReconcile(ch)
	return ch
}

// desired is Desired plus the panel's extra settings. Caller holds b.mu.
func (b *Broker) desired() []supervisor.Spec {
	specs := Desired(b.cfg, b.mode)
	for k, v := range b.PanelEnv {
		specs[0].Env[k] = v
	}
	return specs
}

func (b *Broker) snapshot() (config.Config, string, string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cfg, b.mode, b.modeErr
}

// StatusReport is the answer to GET /status.
type StatusReport struct {
	Mode        string               `json:"mode"`
	ConfigError string               `json:"config_error,omitempty"`
	Revision    int                  `json:"revision"`
	Services    []supervisor.Status  `json:"services"`
	Agents      *status.Report       `json:"agents"`
	AgentsError string               `json:"agents_error,omitempty"`
	Packages    []agentlayer.Package `json:"packages"`
	Check       *Check               `json:"check"`
	Host        Host                 `json:"host"`
}

// Status reports the mode, the services and the agent store.
func (b *Broker) Status() StatusReport {
	cfg, mode, modeErr := b.snapshot()
	st := StatusReport{Mode: mode, ConfigError: modeErr, Revision: cfg.Revision, Services: b.Services.Status(), Host: b.Host}
	if b.Agents != nil {
		rep, err := b.Agents()
		if err != nil {
			st.AgentsError = err.Error()
		} else {
			st.Agents = &rep
		}
	}
	st.Check = b.cachedCheck(cfg.Agents.Ref)
	st.Packages = []agentlayer.Package{}
	if b.Layer != nil {
		// An error here already shows as agents_error.
		if pkgs, err := b.Layer.Packages(cfg.Agents.Disabled, cfg.Agents.Added); err == nil {
			st.Packages = pkgs
		}
	}
	return st
}

// Problems lists why st is unhealthy; an empty list means healthy.
func Problems(st StatusReport) []string {
	var out []string
	if st.Mode == ModeRecovery {
		out = append(out, "recovery mode: "+st.ConfigError)
	}
	if len(st.Services) == 0 {
		out = append(out, "no service has started yet")
	}
	for _, s := range st.Services {
		if s.State != supervisor.StateRunning {
			out = append(out, s.Name+" is "+s.State)
		}
	}
	return out
}

// Handler serves the broker API.
func (b *Broker) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, b.Status())
	})
	mux.HandleFunc("GET /mode", func(w http.ResponseWriter, _ *http.Request) {
		_, mode, _ := b.snapshot()
		writeJSON(w, http.StatusOK, map[string]string{"mode": mode})
	})
	mux.HandleFunc("GET /jobs", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"jobs": b.Jobs.List()})
	})
	mux.HandleFunc("GET /jobs/{id}", b.handleJob)
	mux.HandleFunc("POST /verbs/{verb}", b.handleVerb)
	return mux
}

func (b *Broker) handleJob(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	var job Job
	if err == nil {
		job, err = b.Jobs.Get(id)
	}
	if err != nil {
		writeJSON(w, http.StatusNotFound, errorJSON("not_found", "no such job"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (b *Broker) handleVerb(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("verb")
	run, ok := b.verbs()[name]
	if !ok {
		writeJSON(w, http.StatusNotFound, errorJSON("unknown_verb", fmt.Sprintf("unknown verb %q", name)))
		return
	}
	if rootOnlyVerbs[name] {
		if uid, ok := b.PeerUID(r); !ok || uid != 0 {
			writeJSON(w, http.StatusForbidden, errorJSON("forbidden", name+" may only be run as root inside the container"))
			return
		}
	}
	limit := bodyLimit(name)
	body, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorJSON("invalid_request", err.Error()))
		return
	}
	if int64(len(body)) > limit {
		writeJSON(w, http.StatusRequestEntityTooLarge, errorJSON("too_large", "the request is too large"))
		return
	}
	code, v := run(r.Context(), body)
	writeJSON(w, code, v)
}

func (b *Broker) logf(format string, args ...any) {
	if b.Log != nil {
		fmt.Fprintf(b.Log, "berth: "+format+"\n", args...)
	}
}

func errorJSON(code, message string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"error": map[string]string{"code": code, "message": message}})
	return raw
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

var errTrailing = errors.New("invalid request: trailing data")
