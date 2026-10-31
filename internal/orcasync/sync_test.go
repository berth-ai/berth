package orcasync

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/berth-ai/berth/internal/config"
)

// fakeOrca is an orca-ide that keeps local setups in memory.
type fakeOrca struct {
	setups  map[string]string // path → setup id
	inUse   map[string]bool   // paths whose delete Orca refuses
	down    int               // calls that fail before Orca answers
	addErr  map[string]string // path → error repo add prints
	calls   []string
	nextID  int
	foreign []map[string]any // setups on other hosts
}

func (f *fakeOrca) run(_ context.Context, args ...string) ([]byte, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	if f.down > 0 {
		f.down--
		return nil, errors.New("runtime_unavailable: Orca is not running")
	}
	flag := func(name string) string {
		i := slices.Index(args, "--"+name)
		return args[i+1]
	}
	switch strings.Join(args[:2], " ") {
	case "project setups":
		list := slices.Clone(f.foreign)
		for p, id := range f.setups {
			list = append(list, map[string]any{"id": id, "path": p, "hostId": "local", "repoId": "r-" + id})
		}
		return json.Marshal(map[string]any{"ok": true, "result": map[string]any{"setups": list}})
	case "repo add":
		p := flag("path")
		if msg, ok := f.addErr[p]; ok {
			return nil, errors.New(msg)
		}
		f.nextID++
		f.setups[p] = "s" + string(rune('0'+f.nextID))
		return json.Marshal(map[string]any{"ok": true, "result": map[string]any{"repo": map[string]any{"path": p}}})
	case "project setup-delete":
		id := flag("setup")
		for p, sid := range f.setups {
			if sid != id {
				continue
			}
			if f.inUse[p] {
				return nil, errors.New("project has 1 open terminal")
			}
			delete(f.setups, p)
			return []byte(`{"ok":true}`), nil
		}
		return nil, errors.New("Project host setup not found: " + id)
	}
	return nil, errors.New("unexpected call")
}

type fixture struct {
	t    *testing.T
	orca *fakeOrca
	s    Syncer
	root string
}

func newFixture(t *testing.T, folders ...string) *fixture {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "workspaces")
	for _, f := range folders {
		if err := os.MkdirAll(filepath.Join(root, f), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	orca := &fakeOrca{setups: map[string]string{}, inUse: map[string]bool{}, addErr: map[string]string{}}
	fx := &fixture{t: t, orca: orca, root: root}
	fx.s = Syncer{Orca: orca.run, Workspaces: filepath.Join(dir, "workspaces.json"),
		State: filepath.Join(dir, "state", "orca.json"), Wait: time.Second, Poll: time.Millisecond}
	fx.publish(folders...)
	return fx
}

// publish makes folders Berth's workspaces, with ids w0, w1, …
func (fx *fixture) publish(folders ...string) {
	ws := map[string]config.PublishedWorkspace{}
	for i, f := range folders {
		ws["w"+string(rune('0'+i))] = config.PublishedWorkspace{Path: filepath.Join(fx.root, f)}
	}
	if err := config.WriteWorkspacesFile(fx.s.Workspaces, ws, -1, -1); err != nil {
		fx.t.Fatal(err)
	}
}

func (fx *fixture) path(f string) string { return filepath.Join(fx.root, f) }

func (fx *fixture) run() Result {
	fx.t.Helper()
	res, err := fx.s.Run(context.Background())
	if err != nil {
		fx.t.Fatal(err)
	}
	return res
}

func TestSyncAddsWorkspaces(t *testing.T) {
	fx := newFixture(t, "a", "b")
	res := fx.run()
	if !slices.Equal(res.Added, []string{fx.path("a"), fx.path("b")}) || len(fx.orca.setups) != 2 {
		t.Fatalf("%+v %v", res, fx.orca.setups)
	}
	// A second run changes nothing.
	if res := fx.run(); len(res.Added)+len(res.Removed) != 0 {
		t.Errorf("second run: %+v", res)
	}
}

func TestSyncSkipsAMissingFolder(t *testing.T) {
	fx := newFixture(t, "a")
	fx.publish("a", "gone")
	if res := fx.run(); !slices.Equal(res.Added, []string{fx.path("a")}) {
		t.Errorf("%+v", res)
	}
}

func TestSyncRemovesOnlyWhatItAdded(t *testing.T) {
	fx := newFixture(t, "a", "b")
	fx.orca.setups[fx.path("mine")] = "u1" // the user's own, outside Berth
	fx.orca.setups[fx.path("b")] = "u2"    // already in Orca before Berth
	if res := fx.run(); !slices.Equal(res.Added, []string{fx.path("a")}) {
		t.Fatalf("%+v", res)
	}
	fx.publish() // both workspaces forgotten
	res := fx.run()
	if !slices.Equal(res.Removed, []string{fx.path("a")}) {
		t.Errorf("removed %v", res.Removed)
	}
	if _, ok := fx.orca.setups[fx.path("b")]; !ok {
		t.Error("removed a repo the user added to Orca")
	}
	if _, ok := fx.orca.setups[fx.path("mine")]; !ok {
		t.Error("removed a repo outside Berth")
	}
}

func TestSyncKeepsARepoInUse(t *testing.T) {
	fx := newFixture(t, "a")
	fx.run()
	fx.orca.inUse[fx.path("a")] = true
	fx.publish()
	res := fx.run()
	if len(res.Removed) != 0 || !strings.Contains(res.Kept[fx.path("a")], "open terminal") {
		t.Fatalf("%+v", res)
	}
	// Once Orca lets go, the next run removes it.
	fx.orca.inUse[fx.path("a")] = false
	if res := fx.run(); !slices.Equal(res.Removed, []string{fx.path("a")}) {
		t.Errorf("%+v", res)
	}
}

func TestSyncForgetsARepoRemovedInOrca(t *testing.T) {
	fx := newFixture(t, "a")
	fx.run()
	delete(fx.orca.setups, fx.path("a"))
	fx.publish()
	if res := fx.run(); len(res.Removed) != 0 || len(res.Kept) != 0 {
		t.Errorf("%+v", res)
	}
	if strings.Contains(strings.Join(fx.orca.calls, "\n"), "setup-delete") {
		t.Error("deleted a setup Orca no longer has")
	}
}

func TestSyncIgnoresOtherHosts(t *testing.T) {
	fx := newFixture(t, "a")
	fx.orca.foreign = []map[string]any{{"id": "x", "path": fx.path("a"), "hostId": "ssh:box"}}
	if res := fx.run(); !slices.Equal(res.Added, []string{fx.path("a")}) {
		t.Errorf("a remote setup counted as local: %+v", res)
	}
}

func TestSyncReportsAFailedAdd(t *testing.T) {
	fx := newFixture(t, "a", "b")
	fx.orca.addErr[fx.path("a")] = "not a folder Orca can open"
	res := fx.run()
	if !slices.Equal(res.Added, []string{fx.path("b")}) || !strings.Contains(res.Failed[fx.path("a")], "not a folder") {
		t.Errorf("%+v", res)
	}
}

func TestSyncWaitsForOrca(t *testing.T) {
	fx := newFixture(t, "a")
	fx.orca.down = 3
	if res := fx.run(); !slices.Equal(res.Added, []string{fx.path("a")}) {
		t.Errorf("%+v", res)
	}
}

func TestSyncGivesUpWhenOrcaNeverAnswers(t *testing.T) {
	fx := newFixture(t, "a")
	fx.orca.down = 1 << 30
	fx.s.Wait = 20 * time.Millisecond
	_, err := fx.s.Run(context.Background())
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("got %v", err)
	}
}
