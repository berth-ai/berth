// Package cli implements the berth command line on top of the agent store.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/berth-ai/berth/internal/agentstore"
	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/locksource"
	"github.com/berth-ai/berth/internal/output"
	"github.com/berth-ai/berth/internal/registry"
	"github.com/berth-ai/berth/internal/runner"
	"github.com/berth-ai/berth/internal/smoke"
	"github.com/berth-ai/berth/internal/status"
)

// Config holds every path and dependency the commands use. Tests build it
// directly; the binary uses DefaultConfig.
type Config struct {
	AgentsDir      string
	SeedDir        string
	BuildInfo      string
	MiseConfig     string
	ProcRoot       string
	MCPSetup       []string      // argv of the MCP converger; empty skips it
	SkillsSync     []string      // argv of the skills converger; empty skips it
	WorkspacesSync []string      // argv of the instructions converger; empty skips it
	OrcaSync       []string      // argv of the Orca projects converger; empty skips it
	OrcaIDE        string        // Orca's CLI, orca-ide
	OrcaWait       time.Duration // how long orca sync waits for Orca; 0 means 90s
	Stdin          io.Reader     // what hidden commands read input from; nil reads nothing
	RegistryDir    string        // the image's registry of addable agents
	AgentsCmd      []string      // argv prefix of berth agents, the added-agent converger; empty skips added agents
	GitHub         locksource.GitHub
	Keep           int
	LockTimeout    time.Duration
	IsRoot         bool
	Agent          *runner.Identity
	AgentErr       error // set when running as root and agent does not exist
	Runner         runner.Runner
	Smoke          func(ctx context.Context, dir string, bins []string) error
	Now            func() time.Time
	Version        string
	Commit         string

	// Services (berth supervise, berth panel, berth config, berth health).
	BrokerSocket  string
	BerthPath     string
	ModuleRun     string
	ConfigDir     string
	WorkspacesDir string // the projects' bind mount, /workspaces
	Tmux          string // the tmux binary; empty means tmux on PATH
	WsEnv         string // berth-ws-env; empty runs windows without workspace variables
	KeysPath      string // agents.env, the API keys the wrappers load
	MCPTimeout    time.Duration
	Panel         *runner.Identity // the berth-panel user; set when running as root
	PanelErr      error            // set when running as root and berth-panel does not exist
	PanelListen   string
	LoginPath     string              // the panel login, readable by root only
	Getenv        func(string) string // reads the BERTH_HOST_* values for display
	Environ       func() []string
}

// Build carries values injected with -ldflags.
type Build struct {
	Version string
	Commit  string
	Repo    string
}

// DefaultConfig returns the in-container configuration.
func DefaultConfig(getenv func(string) string, b Build) (Config, error) {
	return defaultConfig(getenv, b, os.Geteuid() == 0, runner.LookupUser)
}

func defaultConfig(getenv func(string) string, b Build, isRoot bool, lookup func(string) (*runner.Identity, error)) (Config, error) {
	repo := b.Repo
	if v := getenv("BERTH_REPO"); v != "" {
		repo = v
	}
	cfg := Config{
		AgentsDir:      "/opt/agents",
		SeedDir:        "/usr/share/berth/agents",
		BuildInfo:      "/usr/share/berth/build-info.json",
		MiseConfig:     "/home/agent/.config/mise/config.toml",
		ProcRoot:       "/proc",
		MCPSetup:       []string{"/usr/local/bin/berth", "mcp", "sync"},
		SkillsSync:     []string{"/usr/local/bin/berth", "skills", "sync"},
		WorkspacesSync: []string{"/usr/local/bin/berth", "workspaces", "sync"},
		OrcaSync:       []string{"/usr/local/bin/berth", "orca", "sync", "--json"},
		OrcaIDE:        "/usr/bin/orca-ide",
		Stdin:          os.Stdin,
		RegistryDir:    registry.DefaultRoot,
		AgentsCmd:      []string{"/usr/local/bin/berth", "agents"},
		GitHub:         locksource.GitHub{Repo: repo, Token: getenv("GITHUB_TOKEN")},
		LockTimeout:    30 * time.Second,
		IsRoot:         isRoot,
		Now:            time.Now,
		Version:        b.Version,
		Commit:         b.Commit,

		BrokerSocket:  "/run/berth/broker.sock",
		BerthPath:     "/usr/local/bin/berth",
		ModuleRun:     "/usr/local/bin/berth-module-run",
		ConfigDir:     "/var/lib/berth",
		WorkspacesDir: "/workspaces",
		Tmux:          "tmux",
		WsEnv:         "/usr/local/bin/berth-ws-env",
		KeysPath:      "/var/lib/berth/secrets/agents.env",
		PanelListen:   "0.0.0.0:7690",
		LoginPath:     "/var/lib/berth/secrets/panel.json",
		Getenv:        getenv,
		Environ:       os.Environ,
	}
	cfg.Keep, cfg.MCPTimeout = agentSettings(cfg.ConfigDir)
	if cfg.IsRoot {
		// Only some commands need these users; report a missing one there.
		if panel, err := lookup("berth-panel"); err != nil {
			cfg.PanelErr = fmt.Errorf("look up user berth-panel: %w", err)
		} else {
			cfg.Panel = panel
		}
		agent, err := lookup("agent")
		if err != nil {
			cfg.AgentErr = fmt.Errorf("look up user agent: %w", err)
			return cfg, nil
		}
		cfg.Agent = agent
		cfg.MiseConfig = filepath.Join(agent.Home, ".config", "mise", "config.toml")
	}
	return cfg, nil
}

// agentSettings reads the agent tunables from the config store. Without a
// usable config, or as non-root (which cannot read it), the defaults apply.
func agentSettings(dir string) (keep int, mcp time.Duration) {
	c, err := (&config.Store{Dir: dir}).Load()
	if err != nil {
		c = config.Defaults()
	}
	return c.Agents.Keep, time.Duration(c.Agents.MCPTimeout) * time.Second
}

type app struct {
	cfg   Config
	store agentstore.Store
	p     output.Printer
}

type command struct {
	summary string
	run     func(ctx context.Context, a *app, args []string) error
}

var commands = map[string]command{
	"status":     {"status [--check] [--json]            show installed agent sets", runStatus},
	"update":     {"update [--ref R | --image | --lock-dir D] [--json]  install and activate an agent set", runUpdate},
	"rollback":   {"rollback [HASH] [--json]             return to the previous (or named) agent set", runRollback},
	"ensure":     {"ensure [--json]                      boot-time install of the image seed", runEnsure},
	"gc":         {"gc [--json]                          remove unused agent sets", runGC},
	"config":     {"config get|set K=V...|revert|reset|export|import FILE|reset-login [--json]  read or change settings", runConfig},
	"login":      {"login status|logout AGENT METHOD [--json]  the agents' own sign-ins", runLogin},
	"health":     {"health [--json]                      report whether every enabled service runs", runHealth},
	"orca":       {"orca pair [--mobile]|devices|sync [--json]  pair an Orca app, list paired ones, or add workspaces to Orca", runOrca},
	"version":    {"version                              print the berth version", runVersion},
	"supervise":  {"supervise                            run the container's services (the entrypoint starts it)", runSupervise},
	"mcp":        {"mcp sync                             register MCP servers with the agents (internal)", runMCP},
	"skills":     {"skills scan|sync                     list skills in a repository and link them for the agents (internal)", runSkills},
	"workspaces": {"workspaces clone|probe|status|candidates|remove|launch|kill  the panel's workspaces (internal)", runWorkspaces},
	"agents":     {"agents status|sync|install|rollback   install the agents added from the registry (internal)", runAgents},
	"panel":      {"panel                                serve the web panel (the panel module starts it)", runPanel},
}

var order = []string{"status", "update", "rollback", "ensure", "gc", "config", "login", "orca", "health", "version", "supervise", "panel"}

// Run executes one berth invocation and returns its exit code.
func Run(ctx context.Context, cfg Config, args []string, stdout, stderr io.Writer) int {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Environ == nil {
		cfg.Environ = os.Environ
	}
	if cfg.Getenv == nil {
		cfg.Getenv = os.Getenv
	}
	if cfg.MCPTimeout == 0 {
		cfg.MCPTimeout = 2 * time.Minute
	}
	if cfg.Runner == nil {
		cfg.Runner = runner.Exec{Agent: cfg.Agent, Stdout: stderr, Stderr: stderr}
	}
	if cfg.Smoke == nil {
		cfg.Smoke = smoke.Checker{Runner: cfg.Runner, Timeout: 2 * time.Minute}.Check
	}
	a := &app{cfg: cfg, store: agentstore.Store{Root: cfg.AgentsDir}}
	a.p = output.Printer{JSON: wantsJSON(args), Stdout: stdout, Stderr: stderr}
	if len(args) == 0 {
		writeUsage(stderr)
		return output.ExitUsage
	}
	switch args[0] {
	case "help", "-h", "--help":
		writeUsage(stdout)
		return output.ExitOK
	}
	cmd, ok := commands[args[0]]
	if !ok {
		return a.p.Fail(usageErr(fmt.Errorf("unknown command %q; run berth help", args[0])))
	}
	if err := cmd.run(ctx, a, args[1:]); err != nil {
		return a.p.Fail(err)
	}
	return output.ExitOK
}

func writeUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: berth <command> [flags]\n\ncommands:")
	for _, name := range order {
		fmt.Fprintf(w, "  %s\n", commands[name].summary)
	}
}

func wantsJSON(args []string) bool {
	for _, a := range args {
		if a == "--json" || a == "-json" || a == "--json=true" {
			return true
		}
	}
	return false
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

// parseArgs allows flags after positional arguments.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func usageErr(err error) error {
	return output.Errorf(output.ExitUsage, "usage", "", "%v", err)
}

// locked runs fn as root while holding the store lock.
func (a *app) locked(name string, fn func() error) error {
	if !a.cfg.IsRoot {
		return output.Errorf(output.ExitNotRoot, "not_root", "", "berth %s must run as root (docker exec -u root <container> berth %s)", name, name)
	}
	if a.cfg.AgentErr != nil {
		return output.Errorf(output.ExitFailure, "no_agent_user", "", "%v", a.cfg.AgentErr)
	}
	unlock, err := a.store.Lock(a.cfg.LockTimeout)
	if errors.Is(err, agentstore.ErrLockBusy) {
		return output.Errorf(output.ExitLockBusy, "lock_busy", "lock", "another berth command held the agent store for %s", a.cfg.LockTimeout)
	}
	if err != nil {
		return output.Errorf(output.ExitFailure, "internal", "lock", "%v", err)
	}
	defer unlock()
	return fn()
}

func sourceErr(err error) error {
	if errors.Is(err, locksource.ErrInvalid) {
		return output.Errorf(output.ExitFailure, "invalid_lock", "validate", "%v", err)
	}
	return output.Errorf(output.ExitFailure, "fetch_failed", "resolve", "%v", err)
}

func installErr(err error) error {
	var se *agentstore.StepError
	if errors.As(err, &se) {
		code := "install_failed"
		if se.Step == "smoke" {
			code = "smoke_failed"
		}
		return output.Errorf(output.ExitFailure, code, se.Step, "%v", se.Err)
	}
	return output.Errorf(output.ExitFailure, "internal", "", "%v", err)
}

func (a *app) views() (*status.VersionView, *status.VersionView, error) {
	cur, err := a.store.Current()
	if err != nil {
		return nil, nil, err
	}
	prev, err := a.store.Previous()
	if err != nil {
		return nil, nil, err
	}
	return status.View(cur), status.View(prev), nil
}

func (a *app) seed() (*locksource.Lock, error) {
	return locksource.FromDir(a.cfg.SeedDir, locksource.Source{Origin: locksource.OriginImage, FetchedAt: a.cfg.Now().UTC()})
}

func hashOrNone(v *status.VersionView) string {
	if v == nil {
		return "none"
	}
	return v.Hash
}
