package addedagents

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berth-ai/berth/internal/agentset"
	"github.com/berth-ai/berth/internal/agentstore"
	"github.com/berth-ai/berth/internal/registry"
	"github.com/berth-ai/berth/internal/runner"
	"github.com/berth-ai/berth/internal/smoke"
	"github.com/berth-ai/berth/internal/testutil"
)

func TestMain(m *testing.M) {
	testutil.RunFakeToolIfRequested()
	os.Exit(m.Run())
}

var ctx = context.Background()

// entryPkg is a registry entry for pkg at version. The fake npm installs
// it with one bin named after the package.
func entryPkg(pkg, version string) string {
	return `{"name":"x","private":true,"dependencies":{"` + pkg + `":"` + version + `"},
	  "berth":{"` + pkg + `":{"kind":"agent","name":"` + pkg + `","description":"A fake agent","keys":[]}},
	  "berthAgents":{"schema":1,"agents":{"` + pkg + `":{"icon":"x"}}}}`
}

type world struct {
	t         *testing.T
	home, reg string
	c         Converger
	plain     runner.Runner
	log       *bytes.Buffer
}

func newWorld(t *testing.T) *world {
	t.Helper()
	testutil.FakeTools(t)
	w := &world{t: t, home: t.TempDir(), reg: t.TempDir(), log: &bytes.Buffer{}}
	w.plain = runner.Exec{Stdout: io.Discard, Stderr: io.Discard}
	w.c = Converger{Home: w.home, Runner: w.plain, Smoke: smoke.Checker{Runner: w.plain, Timeout: 30 * time.Second}.Bins,
		Now: time.Now, InUse: func() []string { return nil }, Log: w.log}
	return w
}

// publish makes the registry hold entry id for pkg at version.
func (w *world) publish(id, pkg, version string) {
	w.t.Helper()
	os.RemoveAll(filepath.Join(w.reg, id))
	testutil.RegistryEntry(w.t, w.reg, id, entryPkg(pkg, version))
	w.reload()
}

func (w *world) unpublish(id string) {
	w.t.Helper()
	os.RemoveAll(filepath.Join(w.reg, id))
	w.reload()
}

func (w *world) reload() {
	w.t.Helper()
	entries, errs := registry.Load(w.reg)
	if len(errs) != 0 {
		w.t.Fatal(errs)
	}
	w.c.Registry = entries
}

func (w *world) store(id string) agentstore.Store {
	return agentstore.Store{Root: filepath.Join(agentset.StoreRoot(w.home), id)}
}

// version is the version of id's current (or, with prev, previous) install.
func (w *world) version(id string, prev bool) string {
	w.t.Helper()
	get := w.store(id).Current
	if prev {
		get = w.store(id).Previous
	}
	v, err := get()
	if err != nil {
		w.t.Fatal(err)
	}
	if v == nil {
		return ""
	}
	for _, ver := range v.Manifest.Packages {
		return ver
	}
	return ""
}

func (w *world) sync(wanted ...string) error { return w.c.Sync(ctx, wanted) }

// failIn fails npm ci for installs below dir, like a dropped network.
type failIn struct {
	runner.Runner
	dir string
}

func (f failIn) Run(ctx context.Context, c runner.Cmd) error {
	if strings.HasPrefix(c.Dir, f.dir) {
		return errors.New("npm ci: network down")
	}
	return f.Runner.Run(ctx, c)
}

func TestInstall(t *testing.T) {
	w := newWorld(t)
	w.publish("fake", "fake-agent", "1.0.0")
	got, err := w.c.Install(ctx, "fake")
	if err != nil || got.ID != "fake" || got.Package != "fake-agent" || got.Version != "1.0.0" {
		t.Fatalf("install = %+v, %v", got, err)
	}
	if w.version("fake", false) != "1.0.0" {
		t.Errorf("current = %q", w.version("fake", false))
	}
	st, _ := w.store("fake").ReadState()
	if !st.FollowImage || st.ImageHash != w.c.Registry[0].Lock.Hash {
		t.Errorf("state = %+v", st)
	}
	out, err := exec.Command(filepath.Join(w.store("fake").Root, "current", "bin", "fake-agent")).Output()
	if err != nil || !strings.HasPrefix(string(out), "fake-agent 1.0.0") {
		t.Errorf("bin = %q %v", out, err)
	}
	if _, err := w.c.Install(ctx, "nope"); !errors.Is(err, ErrUnknown) {
		t.Errorf("unknown id: %v", err)
	}
}

func TestSyncFollowsImage(t *testing.T) {
	w := newWorld(t)
	w.publish("fake", "fake-agent", "1.0.0")
	if _, err := w.c.Install(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	w.publish("fake", "fake-agent", "1.1.0")
	if err := w.sync("fake-agent"); err != nil {
		t.Fatal(err)
	}
	if w.version("fake", false) != "1.1.0" || w.version("fake", true) != "1.0.0" {
		t.Errorf("current %q previous %q", w.version("fake", false), w.version("fake", true))
	}
}

func TestRollbackStaysUntilImageChanges(t *testing.T) {
	w := newWorld(t)
	w.publish("fake", "fake-agent", "1.0.0")
	if _, err := w.c.Install(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.c.Rollback("fake-agent"); !errors.Is(err, agentstore.ErrNoPrevious) {
		t.Errorf("rollback without previous: %v", err)
	}
	w.publish("fake", "fake-agent", "1.1.0")
	w.sync("fake-agent")
	got, err := w.c.Rollback("fake-agent")
	if err != nil || got.Version != "1.0.0" || w.version("fake", false) != "1.0.0" {
		t.Fatalf("rollback = %+v, %v", got, err)
	}
	if err := w.sync("fake-agent"); err != nil || w.version("fake", false) != "1.0.0" {
		t.Errorf("sync undid the rollback: %q %v", w.version("fake", false), err)
	}
	if !strings.Contains(w.log.String(), "stays on 1.0.0") {
		t.Errorf("log = %q", w.log.String())
	}
	w.publish("fake", "fake-agent", "1.2.0")
	if err := w.sync("fake-agent"); err != nil || w.version("fake", false) != "1.2.0" {
		t.Errorf("new image pin not followed: %q %v", w.version("fake", false), err)
	}
	if _, err := w.c.Rollback("nope"); !errors.Is(err, ErrNotAdded) {
		t.Errorf("unknown package: %v", err)
	}
}

func TestSyncRemovesUnwanted(t *testing.T) {
	w := newWorld(t)
	w.publish("fake", "fake-agent", "1.0.0")
	w.c.Install(ctx, "fake")
	if err := w.sync(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(w.store("fake").Root); !os.IsNotExist(err) {
		t.Errorf("store kept: %v", err)
	}
}

func TestSyncKeepsDroppedEntry(t *testing.T) {
	w := newWorld(t)
	w.publish("fake", "fake-agent", "1.0.0")
	w.c.Install(ctx, "fake")
	w.unpublish("fake")
	if err := w.sync("fake-agent"); err != nil {
		t.Fatal(err)
	}
	if w.version("fake", false) != "1.0.0" {
		t.Error("an entry the registry dropped was removed")
	}
	rep, problems := w.c.Status()
	if len(problems) != 0 || len(rep.Agents) != 1 || rep.Errors["fake-agent"] != NotInRegistry {
		t.Errorf("status = %+v %v", rep, problems)
	}
}

func TestOneFailureDoesNotStopAnother(t *testing.T) {
	w := newWorld(t)
	w.publish("a", "agent-a", "1.0.0")
	w.publish("b", "agent-b", "1.0.0")
	w.c.Runner = failIn{Runner: w.plain, dir: w.store("b").Root}
	err := w.sync("agent-a", "agent-b")
	if err == nil || !strings.Contains(err.Error(), "agent-b") {
		t.Errorf("err = %v", err)
	}
	if w.version("a", false) != "1.0.0" || w.version("b", false) != "" {
		t.Errorf("a %q b %q", w.version("a", false), w.version("b", false))
	}
	if rep, _ := w.c.Status(); !strings.Contains(rep.Errors["agent-b"], "network down") {
		t.Errorf("errors = %v", rep.Errors)
	}
	w.c.Runner = w.plain
	if err := w.sync("agent-a", "agent-b"); err != nil {
		t.Fatal(err)
	}
	if rep, _ := w.c.Status(); len(rep.Errors) != 0 {
		t.Errorf("errors kept after a good sync: %v", rep.Errors)
	}
}

// TestFailedFollowKeepsCurrent: an image upgrade whose install fails leaves
// the old version current and runnable, with the failure recorded.
func TestFailedFollowKeepsCurrent(t *testing.T) {
	w := newWorld(t)
	w.publish("fake", "fake-agent", "1.0.0")
	w.c.Install(ctx, "fake")
	w.publish("fake", "fake-agent", "1.1.0")
	w.c.Runner = failIn{Runner: w.plain, dir: agentset.StoreRoot(w.home)}
	if err := w.sync("fake-agent"); err == nil {
		t.Fatal("no error")
	}
	if w.version("fake", false) != "1.0.0" {
		t.Errorf("current = %q", w.version("fake", false))
	}
	if out, err := exec.Command(filepath.Join(w.store("fake").Root, "current", "bin", "fake-agent")).Output(); err != nil ||
		!strings.HasPrefix(string(out), "fake-agent 1.0.0") {
		t.Errorf("bin = %q %v", out, err)
	}
	if rep, _ := w.c.Status(); !strings.Contains(rep.Errors["fake-agent"], "network down") {
		t.Errorf("errors = %v", rep.Errors)
	}
	tmps, _ := filepath.Glob(filepath.Join(w.store("fake").Root, "versions", "*.tmp"))
	if len(tmps) != 0 {
		t.Errorf("partial installs left: %v", tmps)
	}
}

func TestRemoveSkipsBusyStore(t *testing.T) {
	w := newWorld(t)
	w.publish("fake", "fake-agent", "1.0.0")
	w.c.Install(ctx, "fake")
	unlock, err := w.store("fake").Lock(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	w.sync()
	if _, err := os.Stat(w.store("fake").Root); err != nil || !strings.Contains(w.log.String(), "busy") {
		t.Errorf("busy store: %v; log %q", err, w.log.String())
	}
	unlock()
	w.sync()
	if _, err := os.Stat(w.store("fake").Root); !os.IsNotExist(err) {
		t.Errorf("store kept after unlock: %v", err)
	}
}

func TestGCKeepsCurrentAndPrevious(t *testing.T) {
	w := newWorld(t)
	for i, v := range []string{"1.0.0", "1.1.0", "1.2.0"} {
		w.publish("fake", "fake-agent", v)
		if i == 0 {
			w.c.Install(ctx, "fake")
		} else if err := w.sync("fake-agent"); err != nil {
			t.Fatal(err)
		}
	}
	versions, _ := os.ReadDir(filepath.Join(w.store("fake").Root, "versions"))
	if len(versions) != 2 {
		t.Errorf("versions = %d, want current and previous", len(versions))
	}
}

func TestStatusEmpty(t *testing.T) {
	w := newWorld(t)
	rep, problems := w.c.Status()
	if rep.Agents == nil || len(rep.Agents) != 0 || rep.Errors == nil || len(problems) != 0 {
		t.Errorf("status = %+v %v", rep, problems)
	}
}
