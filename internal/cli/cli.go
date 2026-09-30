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
	"strconv"
	"time"

	"github.com/salgnt/berth/internal/agentstore"
	"github.com/salgnt/berth/internal/locksource"
	"github.com/salgnt/berth/internal/output"
	"github.com/salgnt/berth/internal/runner"
	"github.com/salgnt/berth/internal/smoke"
	"github.com/salgnt/berth/internal/status"
)

// Config holds every path and dependency the commands use. Tests build it
// directly; the binary uses DefaultConfig.
type Config struct {
	AgentsDir   string
	SeedDir     string
	BuildInfo   string
	MiseConfig  string
	ProcRoot    string
	MCPSetup    string
	GitHub      locksource.GitHub
	Keep        int
	LockTimeout time.Duration
	IsRoot      bool
	Agent       *runner.Identity
	AgentErr    error // set when running as root and agent does not exist
	Runner      runner.Runner
	Smoke       func(ctx context.Context, dir string, bins []string) error
	Now         func() time.Time
	Version     string
	Commit      string
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
	keep := 3
	if v := getenv("AGENTS_KEEP"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return Config{}, fmt.Errorf("AGENTS_KEEP must be a non-negative integer, got %q", v)
		}
		keep = n
	}
	repo := b.Repo
	if v := getenv("BERTH_REPO"); v != "" {
		repo = v
	}
	cfg := Config{
		AgentsDir:   "/opt/agents",
		SeedDir:     "/usr/share/berth/agents",
		BuildInfo:   "/usr/share/berth/build-info.json",
		MiseConfig:  "/home/agent/.config/mise/config.toml",
		ProcRoot:    "/proc",
		MCPSetup:    "/usr/local/bin/berth-mcp-setup",
		GitHub:      locksource.GitHub{Repo: repo, Token: getenv("GITHUB_TOKEN")},
		Keep:        keep,
		LockTimeout: 30 * time.Second,
		IsRoot:      isRoot,
		Now:         time.Now,
		Version:     b.Version,
		Commit:      b.Commit,
	}
	if cfg.IsRoot {
		// Only the mutating commands need agent; report a missing user there.
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
	"status":   {"status [--check] [--json]            show installed agent sets", runStatus},
	"update":   {"update [--ref R | --image | --lock-dir D] [--json]  install and activate an agent set", runUpdate},
	"rollback": {"rollback [HASH] [--json]             return to the previous (or named) agent set", runRollback},
	"ensure":   {"ensure [--json]                      boot-time install of the image seed", runEnsure},
	"gc":       {"gc [--json]                          remove unused agent sets", runGC},
	"version":  {"version                              print the berth version", runVersion},
}

var order = []string{"status", "update", "rollback", "ensure", "gc", "version"}

// Run executes one berth invocation and returns its exit code.
func Run(ctx context.Context, cfg Config, args []string, stdout, stderr io.Writer) int {
	if cfg.Now == nil {
		cfg.Now = time.Now
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
