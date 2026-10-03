package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/salgnt/berth/internal/agentlayer"
	"github.com/salgnt/berth/internal/broker"
	"github.com/salgnt/berth/internal/config"
	"github.com/salgnt/berth/internal/login"
	"github.com/salgnt/berth/internal/output"
	"github.com/salgnt/berth/internal/status"
	"github.com/salgnt/berth/internal/supervisor"
)

// SignalCause cancels berth's root context when a signal arrives, so that
// berth supervise can exit with 128 plus the signal number.
type SignalCause struct {
	Signal os.Signal
}

func (c SignalCause) Error() string { return "received " + c.Signal.String() }

// serviceEnvKeep lists the variables services inherit from berth supervise.
// Everything else the operator passed to the container (tokens, host-side
// compose values) stays with the supervisor.
var serviceEnvKeep = []string{
	"PATH", "LANG", "LANGUAGE", "LC_ALL",
	"EDITOR", "VISUAL", "XDG_CONFIG_DIRS", "DISABLE_AUTOUPDATER", "BERTH_REPO",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy",
	"NODE_EXTRA_CA_CERTS", "SSL_CERT_FILE", "SSL_CERT_DIR",
}

func serviceEnv(environ []string) []string {
	var env []string
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if slices.Contains(serviceEnvKeep, name) {
			env = append(env, kv)
		}
	}
	return env
}

// runSupervise is the container's main process. It serves the broker socket,
// installs the image's agent set when needed, runs the services the config
// asks for, and stops them in reverse order on TERM or INT.
func runSupervise(ctx context.Context, a *app, args []string) error {
	fs := newFlagSet("supervise")
	jsonOut := fs.Bool("json", false, "print errors as JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return usageErr(err)
	}
	a.p.JSON = *jsonOut
	if len(pos) > 0 {
		return usageErr(fmt.Errorf("supervise takes no arguments"))
	}
	if !a.cfg.IsRoot {
		return output.Errorf(output.ExitNotRoot, "not_root", "", "berth supervise must run as root")
	}
	if a.cfg.AgentErr != nil {
		return output.Errorf(output.ExitFailure, "no_agent_user", "", "%v", a.cfg.AgentErr)
	}
	if a.cfg.PanelErr != nil {
		return output.Errorf(output.ExitFailure, "no_panel_user", "", "%v", a.cfg.PanelErr)
	}

	sup := supervisor.New(a.cfg.ModuleRun, serviceEnv(a.cfg.Environ()), a.p.Stderr)
	jobs := broker.NewJobs(ctx)
	jobs.Log = a.p.Stderr
	b := broker.New(&config.Store{Dir: a.cfg.ConfigDir}, login.Store{Path: a.cfg.LoginPath}, sup, jobs, broker.CommandExec(a.cfg.BerthPath))
	b.Agents = a.agentsReport
	b.Host = broker.HostFromEnv(a.cfg.Getenv)
	b.Log = a.p.Stderr
	cfg := b.Load()

	if err := os.MkdirAll(dirOf(a.cfg.BrokerSocket), 0o755); err != nil {
		return output.Errorf(output.ExitFailure, "listen_failed", "listen", "%v", err)
	}
	l, err := broker.Listen(a.cfg.BrokerSocket, int(a.cfg.Panel.GID))
	if err != nil {
		return output.Errorf(output.ExitFailure, "listen_failed", "listen", "%v", err)
	}
	served := make(chan error, 1)
	go func() {
		served <- serveServer(ctx, l, &http.Server{Handler: b.Handler(), ConnContext: broker.ConnContext})
	}()
	a.p.Logf("supervisor listening on %s", a.cfg.BrokerSocket)

	var bg sync.WaitGroup
	a.boot(ctx, cfg, &bg)
	if ctx.Err() == nil {
		b.Reconcile()
	}

	select {
	case <-ctx.Done():
		err = <-served
	case err = <-served:
	}
	a.p.Logf("stopping services")
	sup.Shutdown()
	jobs.Wait()
	bg.Wait()
	if err != nil {
		return output.Errorf(output.ExitFailure, "serve_failed", "serve", "%v", err)
	}
	return exitForCause(ctx)
}

// boot installs the image's agent set when needed and starts MCP setup in
// the background. Neither failure stops the container: the panel must come
// up for diagnosis.
func (a *app) boot(ctx context.Context, cfg config.Config, bg *sync.WaitGroup) {
	ectx, cancel := context.WithTimeout(ctx, time.Duration(cfg.Agents.BootTimeout)*time.Second)
	exit, err := broker.CommandExec(a.cfg.BerthPath)(ectx, []string{"ensure"}, a.p.Stderr, a.p.Stderr)
	cancel()
	if ctx.Err() != nil {
		return
	}
	if err != nil || exit != 0 {
		if cur, _ := a.store.Current(); cur != nil {
			a.p.Logf("WARNING: berth ensure failed; continuing with the installed agents")
		} else {
			a.p.Logf("WARNING: NO AGENTS INSTALLED: berth ensure failed and no agent set is available. " +
				"The panel starts for diagnosis; run berth update and read the log above.")
		}
	}
	a.fixKeysOwner()
	a.rebuildLayer()
	bg.Add(1)
	go func() {
		defer bg.Done()
		a.setupMCP(ctx, time.Duration(cfg.Agents.MCPTimeout)*time.Second)
	}()
}

// fixOwner is a test seam for agentlayer.FixOwner.
var fixOwner = agentlayer.FixOwner

// fixKeysOwner gives agents.env to the agent group after PGID remapping.
func (a *app) fixKeysOwner() {
	if a.cfg.Agent == nil {
		return
	}
	if err := fixOwner(a.cfg.KeysPath, int(a.cfg.Agent.GID)); err != nil {
		a.p.Logf("WARNING: cannot re-own %s: %v", a.cfg.KeysPath, err)
	}
}

// agentsReport is the agent store part of the broker's status. It reads no
// file the agent user owns: as root, that would let agent point a symlink at
// a secret and read it back in an error message.
func (a *app) agentsReport() (status.Report, error) {
	return status.Collect(status.Inputs{Store: a.store, SeedDir: a.cfg.SeedDir, BuildInfo: a.cfg.BuildInfo})
}

// exitForCause maps the signal that stopped berth to the conventional exit code.
func exitForCause(ctx context.Context) error {
	var sc SignalCause
	if errors.As(context.Cause(ctx), &sc) {
		switch sc.Signal {
		case syscall.SIGTERM:
			return output.ExitCode(143)
		case os.Interrupt:
			return output.ExitCode(130)
		}
	}
	return nil
}

func dirOf(path string) string {
	if i := strings.LastIndexByte(path, '/'); i > 0 {
		return path[:i]
	}
	return "."
}
