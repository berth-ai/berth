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

	"github.com/salgnt/berth/internal/config"
	"github.com/salgnt/berth/internal/status"
	"github.com/salgnt/berth/internal/supervisor"
)

const maxBodyBytes = 64 << 10

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
	Log      io.Writer
	// PeerUID identifies the caller; it defaults to the uid ConnContext stored.
	PeerUID func(r *http.Request) (uid int, ok bool)

	change  sync.Mutex // serializes config changes and service restarts
	mu      sync.Mutex // guards the fields below
	cfg     config.Config
	mode    string
	modeErr string
}

// New returns a broker in normal mode with the default config.
func New(store *config.Store, services Services, jobs *Jobs, exec Exec) *Broker {
	return &Broker{Store: store, Services: services, Jobs: jobs, Exec: exec, PeerUID: peerFromContext, cfg: config.Defaults(), mode: ModeNormal}
}

// Load reads the config and picks the mode: recovery when it is unusable,
// with only the panel running until the config is reverted or reset.
func (b *Broker) Load() config.Config {
	cfg, err := b.Store.Load()
	b.mu.Lock()
	defer b.mu.Unlock()
	if err != nil {
		b.cfg, b.mode, b.modeErr = config.Defaults(), ModeRecovery, err.Error()
		b.logf("RECOVERY MODE: %v; only the panel runs until the config is reverted or reset", err)
		return b.cfg
	}
	b.cfg, b.mode, b.modeErr = cfg, ModeNormal, ""
	return cfg
}

// Reconcile starts or adjusts the services for the current config and mode.
func (b *Broker) Reconcile() supervisor.Changes {
	b.change.Lock()
	defer b.change.Unlock()
	b.mu.Lock()
	specs := b.desired()
	b.mu.Unlock()
	return b.Services.Reconcile(specs)
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
	Mode        string              `json:"mode"`
	ConfigError string              `json:"config_error,omitempty"`
	Revision    int                 `json:"revision"`
	Services    []supervisor.Status `json:"services"`
	Agents      *status.Report      `json:"agents"`
	AgentsError string              `json:"agents_error,omitempty"`
}

// Status reports the mode, the services and the agent store.
func (b *Broker) Status() StatusReport {
	cfg, mode, modeErr := b.snapshot()
	st := StatusReport{Mode: mode, ConfigError: modeErr, Revision: cfg.Revision, Services: b.Services.Status()}
	if b.Agents != nil {
		rep, err := b.Agents()
		if err != nil {
			st.AgentsError = err.Error()
		} else {
			st.Agents = &rep
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
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorJSON("invalid_request", err.Error()))
		return
	}
	if len(body) > maxBodyBytes {
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

func (b *Broker) logWriter() io.Writer {
	if b.Log == nil {
		return io.Discard
	}
	return b.Log
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
