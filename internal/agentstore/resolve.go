package agentstore

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/berth-ai/berth/internal/locksource"
	"github.com/berth-ai/berth/internal/runner"
)

// Resolve builds the lock for desired: seed's package.json, descriptors
// and all, with its dependencies set to desired, resolved by npm against
// the registry as agent. npm records each package's integrity, which npm ci
// verifies when the set is installed.
func (in Installer) Resolve(ctx context.Context, seed *locksource.Lock, desired map[string]string) (*locksource.Lock, error) {
	pkg, err := locksource.WithDeps(seed.PackageJSON, desired)
	if err != nil {
		return nil, &StepError{Step: "resolve", Err: err}
	}
	if err := os.MkdirAll(in.Store.versionsDir(), 0o755); err != nil {
		return nil, &StepError{Step: "resolve", Err: err}
	}
	// A .tmp name, so gc removes it should berth die here.
	tmp, err := os.MkdirTemp(in.Store.versionsDir(), "resolve-*.tmp")
	if err != nil {
		return nil, &StepError{Step: "resolve", Err: err}
	}
	defer os.RemoveAll(tmp)
	if err := os.WriteFile(filepath.Join(tmp, "package.json"), pkg, 0o644); err != nil {
		return nil, &StepError{Step: "resolve", Err: err}
	}
	if err := in.ownAll(tmp); err != nil {
		return nil, &StepError{Step: "resolve", Err: err}
	}
	in.logf("resolving the agent set against the npm registry")
	cmd := runner.Cmd{
		Path: "npm", Args: []string{"install", "--package-lock-only", "--ignore-scripts", "--no-audit", "--no-fund"},
		Dir: tmp, Env: []string{"npm_config_cache=" + filepath.Join(tmp, ".npm-cache")}, AsAgent: true,
	}
	if err := in.Runner.Run(ctx, cmd); err != nil {
		return nil, &StepError{Step: "resolve", Err: err}
	}
	src := locksource.Source{Origin: locksource.OriginUpstream, FetchedAt: in.Now().UTC()}
	lock, err := locksource.FromDir(tmp, src)
	if err != nil {
		return nil, &StepError{Step: "resolve", Err: err}
	}
	return lock, nil
}

// ownAll hands dir to the agent user, when berth runs as root.
func (in Installer) ownAll(dir string) error {
	if in.Agent == nil {
		return nil
	}
	uid, gid := int(in.Agent.UID), int(in.Agent.GID)
	return filepath.WalkDir(dir, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Lchown(p, uid, gid)
	})
}
