package instructions

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/berth-ai/berth/internal/agentset"
	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/wsstate"
)

// world is an agent set with c (CLAUDE.md), x (AGENTS.md), g (GEMINI.md),
// all enabled, a home, and workspace api at a git repository.
type world struct {
	t                         *testing.T
	current, layer, home, pub string
	ws                        string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{t: t, current: t.TempDir(), layer: t.TempDir(), home: t.TempDir()}
	w.pub = filepath.Join(t.TempDir(), "workspaces.json")
	pkg := map[string]any{
		"dependencies": map[string]string{"c": "1", "x": "1", "g": "1"},
		"berth": map[string]any{
			"c": map[string]any{"kind": "agent", "name": "C", "keys": []string{}},
			"x": map[string]any{"kind": "agent", "name": "X", "keys": []string{}},
			"g": map[string]any{"kind": "agent", "name": "G", "keys": []string{}},
		},
		"berthAgents": map[string]any{"schema": 1, "agents": map[string]any{
			"c": map[string]any{"instructions": "CLAUDE.md"},
			"x": map[string]any{"instructions": "AGENTS.md"},
			"g": map[string]any{"instructions": "GEMINI.md"},
		}},
	}
	raw, _ := json.Marshal(pkg)
	os.WriteFile(filepath.Join(w.current, "package.json"), raw, 0o644)
	for _, dep := range []string{"c", "x", "g"} {
		d := filepath.Join(w.current, "node_modules", dep)
		os.MkdirAll(d, 0o755)
		m, _ := json.Marshal(map[string]any{"name": dep, "bin": map[string]string{dep: "cli.js"}})
		os.WriteFile(filepath.Join(d, "package.json"), m, 0o644)
		w.enable(dep, true)
	}
	w.ws = t.TempDir()
	if out, err := exec.Command("git", "init", "-q", w.ws).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	return w
}

func (w *world) enable(dep string, on bool) {
	if p := filepath.Join(w.layer, dep); on {
		os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755)
	} else {
		os.Remove(p)
	}
}

func (w *world) publish(shared bool) {
	w.t.Helper()
	pub := map[string]config.PublishedWorkspace{"api": {Path: w.ws, Shared: shared}}
	if err := config.WriteWorkspacesFile(w.pub, pub, -1, -1); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) syncer(log *strings.Builder) Syncer {
	set, err := agentset.Load(&agentset.Locked{Dir: w.current, Link: w.current}, "", nil)
	if err != nil {
		w.t.Fatal(err)
	}
	return Syncer{Set: set, Layer: w.layer, Home: w.home, Workspaces: w.pub, Log: log}
}

func (w *world) run() (string, error) {
	var log strings.Builder
	err := w.syncer(&log).Run(context.Background())
	return log.String(), err
}

func (w *world) mustRun() {
	w.t.Helper()
	if log, err := w.run(); err != nil {
		w.t.Fatalf("%v\n%s", err, log)
	}
}

func (w *world) state() wsstate.State {
	st, err := wsstate.Store{Home: w.home, Published: w.pub}.Load("api")
	if err != nil {
		w.t.Fatal(err)
	}
	return st
}

func (w *world) file(name, body string) { os.WriteFile(filepath.Join(w.ws, name), []byte(body), 0o644) }

func exclude(t *testing.T, ws string) string {
	raw, _ := os.ReadFile(filepath.Join(ws, ".git", "info", "exclude"))
	return string(raw)
}

func TestSharedLinksEveryOtherFile(t *testing.T) {
	w := newWorld(t)
	w.file("AGENTS.md", "# shared\n")
	w.publish(true)
	w.mustRun()
	for _, f := range []string{"CLAUDE.md", "GEMINI.md"} {
		if got := readlink(filepath.Join(w.ws, f)); got != "AGENTS.md" {
			t.Errorf("%s -> %q", f, got)
		}
	}
	if st := w.state(); !slices.Equal(st.Instructions.Links, []string{"CLAUDE.md", "GEMINI.md"}) || st.Instructions.Path != w.ws {
		t.Errorf("state = %+v", st.Instructions)
	}
	if got := exclude(t, w.ws); !strings.Contains(got, "/CLAUDE.md\n/GEMINI.md\n") {
		t.Errorf("exclude = %q", got)
	}
	w.mustRun() // idempotent
	// A disabled agent's link goes; turning shared off removes the rest.
	w.enable("g", false)
	w.mustRun()
	if _, err := os.Lstat(filepath.Join(w.ws, "GEMINI.md")); !os.IsNotExist(err) {
		t.Errorf("GEMINI.md kept after g was disabled: %v", err)
	}
	w.publish(false)
	w.mustRun()
	if _, err := os.Lstat(filepath.Join(w.ws, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Errorf("CLAUDE.md kept: %v", err)
	}
	if raw, _ := os.ReadFile(filepath.Join(w.ws, "AGENTS.md")); string(raw) != "# shared\n" {
		t.Errorf("AGENTS.md changed: %q", raw)
	}
	if ids, _ := (wsstate.Store{Home: w.home, Published: w.pub}).IDs(); len(ids) != 0 {
		t.Errorf("state left: %v", ids)
	}
	if strings.Contains(exclude(t, w.ws), wsstate.BlockStart) {
		t.Error("exclude block left")
	}
}

func TestSharedLeavesRealFilesAlone(t *testing.T) {
	w := newWorld(t)
	w.file("AGENTS.md", "a\n")
	w.file("CLAUDE.md", "my own\n")
	w.publish(true)
	w.mustRun()
	if raw, _ := os.ReadFile(filepath.Join(w.ws, "CLAUDE.md")); string(raw) != "my own\n" {
		t.Errorf("CLAUDE.md = %q", raw)
	}
	st := w.state()
	if st.Instructions.Skipped["CLAUDE.md"] != "CLAUDE.md has its own content" || !slices.Equal(st.Instructions.Links, []string{"GEMINI.md"}) {
		t.Errorf("state = %+v", st.Instructions)
	}
}

func TestSharedLeavesSymlinkedAgentsMD(t *testing.T) {
	w := newWorld(t)
	w.file("CLAUDE.md", "real\n")
	os.Symlink("CLAUDE.md", filepath.Join(w.ws, "AGENTS.md"))
	w.publish(true)
	w.mustRun()
	if readlink(filepath.Join(w.ws, "AGENTS.md")) != "CLAUDE.md" {
		t.Error("AGENTS.md changed")
	}
	if _, err := os.Lstat(filepath.Join(w.ws, "GEMINI.md")); !os.IsNotExist(err) {
		t.Errorf("GEMINI.md was linked through a symlinked AGENTS.md: %v", err)
	}
	st := w.state()
	if len(st.Instructions.Links) != 0 || st.Instructions.Skipped["AGENTS.md"] != "AGENTS.md is a symlink; Berth links nothing to it" {
		t.Errorf("state = %+v", st.Instructions)
	}
}

func TestSharedWithoutAgentsMD(t *testing.T) {
	w := newWorld(t)
	w.publish(true)
	w.mustRun()
	if _, err := os.Lstat(filepath.Join(w.ws, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Errorf("a dangling link was made: %v", err)
	}
	if got := w.state().Instructions.Skipped["CLAUDE.md"]; got != "AGENTS.md is missing; write it or merge a file into it" {
		t.Errorf("skipped = %q", got)
	}
}

func TestUnshareKeepsUserLinks(t *testing.T) {
	w := newWorld(t)
	w.file("AGENTS.md", "a\n")
	os.Symlink("AGENTS.md", filepath.Join(w.ws, "CLAUDE.md")) // the user's own
	w.publish(true)
	w.mustRun()
	if st := w.state(); slices.Contains(st.Instructions.Links, "CLAUDE.md") {
		t.Errorf("Berth claimed the user's link: %+v", st.Instructions)
	}
	w.publish(false)
	w.mustRun()
	if readlink(filepath.Join(w.ws, "CLAUDE.md")) != "AGENTS.md" {
		t.Error("the user's link was removed")
	}
}

func TestReplacedLinkIsNotRemoved(t *testing.T) {
	w := newWorld(t)
	w.file("AGENTS.md", "a\n")
	w.publish(true)
	w.mustRun()
	os.Remove(filepath.Join(w.ws, "CLAUDE.md"))
	w.file("CLAUDE.md", "now mine\n")
	w.publish(false)
	w.mustRun()
	if raw, _ := os.ReadFile(filepath.Join(w.ws, "CLAUDE.md")); string(raw) != "now mine\n" {
		t.Errorf("CLAUDE.md = %q", raw)
	}
}

func TestMissingWorkspaceFailsAlone(t *testing.T) {
	w := newWorld(t)
	w.file("AGENTS.md", "a\n")
	w.publish(true)
	w.mustRun()
	os.RemoveAll(w.ws)
	log, err := w.run()
	if err == nil || !strings.Contains(err.Error(), "workspace api") {
		t.Fatalf("Run = %v\n%s", err, log)
	}
	if st := w.state(); !strings.Contains(st.Instructions.Error, "missing") {
		t.Errorf("state = %+v", st.Instructions)
	}
	// Nothing to place and nothing recorded: no failure.
	w2 := newWorld(t)
	w2.publish(false)
	os.RemoveAll(w2.ws)
	if _, err := w2.run(); err != nil {
		t.Errorf("an idle missing workspace failed: %v", err)
	}
}

func TestForgottenWorkspaceIsReleased(t *testing.T) {
	w := newWorld(t)
	w.file("AGENTS.md", "a\n")
	w.publish(true)
	w.mustRun()
	config.WriteWorkspacesFile(w.pub, map[string]config.PublishedWorkspace{}, -1, -1)
	w.mustRun()
	if _, err := os.Lstat(filepath.Join(w.ws, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Errorf("CLAUDE.md kept: %v", err)
	}
	if ids, _ := (wsstate.Store{Home: w.home, Published: w.pub}).IDs(); len(ids) != 0 {
		t.Errorf("state left: %v", ids)
	}
}

func TestWant(t *testing.T) {
	if got := Want([]string{"AGENTS.md", "CLAUDE.md", "GEMINI.md"}, true); !slices.Equal(got, []string{"CLAUDE.md", "GEMINI.md"}) {
		t.Errorf("Want = %v", got)
	}
	if got := Want([]string{"CLAUDE.md"}, false); len(got) != 0 {
		t.Errorf("Want(off) = %v", got)
	}
}
