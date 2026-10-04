// Package addedagents installs, follows, rolls back and removes the agents
// a user added from the image's registry. Each lives in its own agentstore
// in agent's home (agentset.StoreRoot). It runs as agent: root never
// writes there.
package addedagents

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/berth-ai/berth/internal/agentset"
	"github.com/berth-ai/berth/internal/agentstore"
	"github.com/berth-ai/berth/internal/registry"
	"github.com/berth-ai/berth/internal/runner"
)

var (
	ErrUnknown  = errors.New("no such agent in this image's registry")
	ErrNotAdded = errors.New("no such added agent is installed")
)

// NotInRegistry is the error recorded for an added package the image's
// registry no longer has; it stays installed until the user removes it.
const NotInRegistry = "not in this image's registry"

const lockTimeout = 30 * time.Second

// Converger acts on the added-agent stores in Home.
type Converger struct {
	Home     string
	Registry []registry.Entry
	Runner   runner.Runner
	Smoke    func(ctx context.Context, dir string, bins []string) error
	Now      func() time.Time
	InUse    func() []string // paths running processes use; gc keeps them
	Log      io.Writer
}

func (c Converger) root() string { return agentset.StoreRoot(c.Home) }

func (c Converger) store(id string) agentstore.Store {
	return agentstore.Store{Root: filepath.Join(c.root(), id)}
}

func (c Converger) logf(format string, args ...any) {
	if c.Log != nil {
		fmt.Fprintf(c.Log, "berth agents: "+format+"\n", args...)
	}
}

// Install makes entry id's pinned version current and follows the image,
// whatever agents.json says: the broker saves the config only after it
// succeeds.
func (c Converger) Install(ctx context.Context, id string) (agentset.Added, error) {
	e, ok := registry.ByID(c.Registry, id)
	if !ok {
		return agentset.Added{}, fmt.Errorf("%w: %s", ErrUnknown, id)
	}
	s := c.store(id)
	unlock, err := s.Lock(lockTimeout)
	if err != nil {
		return agentset.Added{}, err
	}
	defer unlock()
	c.logf("installing %s %s", e.Meta.Name, e.Version)
	if err := c.install(ctx, s, e); err != nil {
		return agentset.Added{}, err
	}
	c.clearError(e.Package)
	return agentset.Added{ID: id, Package: e.Package, Version: e.Version, Hash: e.Lock.Hash}, nil
}

// install builds e's version if needed, flips to it and follows the image.
// The caller holds s's lock.
func (c Converger) install(ctx context.Context, s agentstore.Store, e registry.Entry) error {
	lock := *e.Lock
	lock.Source.FetchedAt = c.Now().UTC()
	in := agentstore.Installer{Store: s, Runner: c.Runner, Smoke: c.Smoke, Now: c.Now, Logf: c.logf, NoBrowsers: true}
	v, _, err := in.Install(ctx, &lock)
	if err != nil {
		return err
	}
	if _, err := s.Flip(v.Hash); err != nil {
		return err
	}
	if err := s.WriteState(agentstore.State{FollowImage: true, ImageHash: e.Lock.Hash}); err != nil {
		return err
	}
	if _, err := s.GC(0, c.InUse()); err != nil {
		c.logf("gc of %s: %v", e.ID, err)
	}
	return nil
}

// Sync converges the stores on wanted, the packages agents.json lists: it
// installs or follows each registry entry, leaves an added package the
// registry dropped as it is, removes every other store, and records the
// failures for the panel. One agent failing does not stop the others.
func (c Converger) Sync(ctx context.Context, wanted []string) error {
	installed, problems := agentset.Installed(c.root())
	for _, p := range problems {
		c.logf("%v", p)
	}
	errs := map[string]string{}
	keep := map[string]bool{}
	var failed []string
	for _, pkg := range slices.Sorted(slices.Values(wanted)) {
		e, ok := registry.ByPackage(c.Registry, pkg)
		if !ok {
			errs[pkg] = NotInRegistry
			for _, a := range installed {
				if a.Package == pkg {
					keep[a.ID] = true
				}
			}
			c.logf("%s is %s; leaving it as it is", pkg, NotInRegistry)
			continue
		}
		keep[e.ID] = true
		if err := c.follow(ctx, e); err != nil {
			errs[pkg] = err.Error()
			failed = append(failed, e.Meta.Name)
			c.logf("%s: %v; keeping the installed version", e.Meta.Name, err)
		}
	}
	c.removeOthers(keep)
	if err := saveState(StatePath(c.Home), state{Errors: errs}); err != nil {
		return err
	}
	if len(failed) > 0 {
		return fmt.Errorf("added agents failed: %s", strings.Join(failed, ", "))
	}
	return nil
}

// follow installs e unless its store is at e's hash, or was rolled back
// from e's hash.
func (c Converger) follow(ctx context.Context, e registry.Entry) error {
	s := c.store(e.ID)
	unlock, err := s.Lock(lockTimeout)
	if err != nil {
		return err
	}
	defer unlock()
	cur, err := s.Current()
	if err != nil {
		return err
	}
	st, err := s.ReadState()
	if err != nil {
		return err
	}
	switch {
	case cur != nil && cur.Hash == e.Lock.Hash:
		return nil
	case cur != nil && !st.FollowImage && st.ImageHash == e.Lock.Hash:
		c.logf("%s stays on %s after a rollback", e.Meta.Name, cur.Manifest.Packages[e.Package])
		return nil
	}
	c.logf("installing %s %s", e.Meta.Name, e.Version)
	return c.install(ctx, s, e)
}

// removeOthers deletes every store not in keep. A store another berth
// command holds (an install in flight) is left for the next sync.
func (c Converger) removeOthers(keep map[string]bool) {
	dirs, err := os.ReadDir(c.root())
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			c.logf("%v", err)
		}
		return
	}
	for _, d := range dirs {
		if !d.IsDir() || keep[d.Name()] || !registry.IDPattern.MatchString(d.Name()) {
			continue
		}
		s := c.store(d.Name())
		unlock, err := s.Lock(0)
		if err != nil {
			c.logf("%s is busy; removing it on the next sync", d.Name())
			continue
		}
		err = os.RemoveAll(s.Root)
		unlock()
		if err != nil {
			c.logf("cannot remove %s: %v", d.Name(), err)
			continue
		}
		c.logf("removed %s", d.Name())
	}
}

// Rollback makes pkg's previous version current and stops following the
// image until the registry pins a different hash.
func (c Converger) Rollback(pkg string) (agentset.Added, error) {
	installed, _ := agentset.Installed(c.root())
	i := slices.IndexFunc(installed, func(a agentset.Added) bool { return a.Package == pkg })
	if i < 0 {
		return agentset.Added{}, fmt.Errorf("%w: %s", ErrNotAdded, pkg)
	}
	id := installed[i].ID
	s := c.store(id)
	unlock, err := s.Lock(lockTimeout)
	if err != nil {
		return agentset.Added{}, err
	}
	defer unlock()
	v, err := s.Rollback("")
	if err != nil {
		return agentset.Added{}, err
	}
	imageHash := ""
	if e, ok := registry.ByPackage(c.Registry, pkg); ok {
		imageHash = e.Lock.Hash
	}
	if err := s.WriteState(agentstore.State{FollowImage: false, ImageHash: imageHash}); err != nil {
		return agentset.Added{}, err
	}
	c.logf("rolled %s back to %s", pkg, v.Manifest.Packages[pkg])
	return agentset.Added{ID: id, Package: pkg, Version: v.Manifest.Packages[pkg], Hash: v.Hash}, nil
}

// Status reports the installs and the last sync's failures.
func (c Converger) Status() (agentset.Report, []error) {
	installed, problems := agentset.Installed(c.root())
	st, err := loadState(StatePath(c.Home))
	if err != nil {
		problems = append(problems, err)
	}
	if installed == nil {
		installed = []agentset.Added{}
	}
	return agentset.Report{Agents: installed, Errors: st.Errors}, problems
}

// clearError forgets pkg's recorded failure after a good install.
func (c Converger) clearError(pkg string) {
	path := StatePath(c.Home)
	st, err := loadState(path)
	if err != nil {
		c.logf("%v", err)
		return
	}
	if _, ok := st.Errors[pkg]; !ok {
		return
	}
	delete(st.Errors, pkg)
	if err := saveState(path, st); err != nil {
		c.logf("%v", err)
	}
}
