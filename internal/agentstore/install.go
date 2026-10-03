package agentstore

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/berth-ai/berth/internal/locksource"
	"github.com/berth-ai/berth/internal/runner"
)

// StepError is an install failure tagged with its pipeline step.
type StepError struct {
	Step string
	Err  error
}

func (e *StepError) Error() string { return e.Step + ": " + e.Err.Error() }
func (e *StepError) Unwrap() error { return e.Err }

// Installer builds complete versions. Agent is the unprivileged owner of the
// tmp dir and the user install scripts run as; nil when berth is not root.
type Installer struct {
	Store  Store
	Runner runner.Runner
	Smoke  func(ctx context.Context, dir string, bins []string) error
	Agent  *runner.Identity
	Now    func() time.Time
	Logf   func(format string, args ...any)
}

// Install makes versions/<lock.Hash> complete. It returns the version and
// whether it had to be built. Any failure removes the partial tree.
func (in Installer) Install(ctx context.Context, lock *locksource.Lock) (*Version, bool, error) {
	v, err := in.Store.Get(lock.Hash)
	if err == nil {
		return v, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, false, &StepError{Step: "prepare", Err: err}
	}
	final := in.Store.VersionDir(lock.Hash)
	tmp := final + ".tmp"
	if err := in.build(ctx, final, tmp, lock); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, false, err
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, false, &StepError{Step: "publish", Err: err}
	}
	v, err = in.Store.Get(lock.Hash)
	if err != nil {
		return nil, false, &StepError{Step: "publish", Err: err}
	}
	return v, true, nil
}

func (in Installer) build(ctx context.Context, final, tmp string, lock *locksource.Lock) error {
	if err := in.prepare(final, tmp, lock); err != nil {
		return &StepError{Step: "prepare", Err: err}
	}
	in.logf("installing agent set %s", lock.Hash)
	env := []string{
		"npm_config_cache=" + filepath.Join(tmp, ".npm-cache"),
		"PLAYWRIGHT_BROWSERS_PATH=" + filepath.Join(tmp, "ms-playwright"),
	}
	npmCI := runner.Cmd{Path: "npm", Args: []string{"ci", "--omit=dev", "--no-audit", "--no-fund"}, Dir: tmp, Env: env, AsAgent: true}
	if err := in.Runner.Run(ctx, npmCI); err != nil {
		return &StepError{Step: "install", Err: err}
	}
	// Playwright usually arrives hoisted from @playwright/mcp, which pins its
	// own (often pre-release) version; it need not be a direct dependency.
	playwrightCLI := filepath.Join("node_modules", "playwright", "cli.js")
	if _, err := os.Stat(filepath.Join(tmp, playwrightCLI)); err != nil {
		return &StepError{Step: "install", Err: errors.New("node_modules/playwright is missing; the agent lock must pull in Playwright (for example through @playwright/mcp)")}
	}
	browsers := runner.Cmd{Path: "node", Args: []string{playwrightCLI, "install", "chromium"}, Dir: tmp, Env: env, AsAgent: true}
	if err := in.Runner.Run(ctx, browsers); err != nil {
		return &StepError{Step: "install", Err: err}
	}
	if err := os.RemoveAll(filepath.Join(tmp, ".npm-cache")); err != nil {
		return &StepError{Step: "install", Err: err}
	}
	deps := slices.Sorted(maps.Keys(lock.Deps))
	bins, err := WriteWrappers(tmp, deps)
	if err != nil {
		return &StepError{Step: "seal", Err: err}
	}
	if err := checkServerBins(tmp, lock.Descriptors); err != nil {
		return &StepError{Step: "install", Err: err}
	}
	if err := Seal(tmp, in.Agent != nil); err != nil {
		return &StepError{Step: "seal", Err: err}
	}
	in.logf("smoke-testing agent set %s", lock.Hash)
	if err := in.Smoke(ctx, tmp, bins); err != nil {
		return &StepError{Step: "smoke", Err: err}
	}
	pkgs, err := installedVersions(tmp, deps)
	if err != nil {
		return &StepError{Step: "complete", Err: err}
	}
	manifest := Manifest{Packages: pkgs, InstalledAt: in.Now().UTC()}
	if err := writeJSON(filepath.Join(tmp, completeFile), manifest); err != nil {
		return &StepError{Step: "complete", Err: err}
	}
	return nil
}

// prepare removes stale trees, then writes the lock and source into tmp,
// owned by the agent user so npm can run unprivileged.
func (in Installer) prepare(final, tmp string, lock *locksource.Lock) error {
	for _, dir := range []string{tmp, final} { // final is known incomplete here
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	files := map[string][]byte{"package.json": lock.PackageJSON, "package-lock.json": lock.PackageLock}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(tmp, name), data, 0o644); err != nil {
			return err
		}
	}
	if err := writeJSON(filepath.Join(tmp, sourceFile), lock.Source); err != nil {
		return err
	}
	if in.Agent == nil {
		return nil
	}
	uid, gid := int(in.Agent.UID), int(in.Agent.GID)
	return filepath.WalkDir(tmp, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Lchown(p, uid, gid)
	})
}

func installedVersions(dir string, deps []string) (map[string]string, error) {
	out := make(map[string]string, len(deps))
	for _, dep := range deps {
		var pkg struct {
			Version string `json:"version"`
		}
		if err := readJSON(filepath.Join(dir, "node_modules", filepath.FromSlash(dep), "package.json"), &pkg); err != nil {
			return nil, err
		}
		out[dep] = pkg.Version
	}
	return out, nil
}

func (in Installer) logf(format string, args ...any) {
	if in.Logf != nil {
		in.Logf(format, args...)
	}
}

// checkServerBins verifies that every MCP server descriptor names a bin its
// package ships, so the converger never registers a wrapper that cannot exist.
func checkServerBins(dir string, d locksource.Descriptors) error {
	for _, dep := range slices.Sorted(maps.Keys(d.Servers)) {
		bins, err := PackageBins(dir, dep)
		if err != nil {
			return err
		}
		if _, ok := bins[d.Servers[dep].Bin]; !ok {
			return fmt.Errorf("%s does not ship the bin %q its MCP descriptor names", dep, d.Servers[dep].Bin)
		}
	}
	return nil
}
