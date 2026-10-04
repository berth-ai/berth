package skills

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berth-ai/berth/internal/agentset"
	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/testutil"
)

// world is an agent set with three agents (c, o reads c's directory, x has
// no skills descriptor), their wrappers, a home and a skills.json.
type world struct {
	t                         *testing.T
	current, layer, home, set string
	repo                      *fixture
	commit                    string
	addedRoot                 string
	added                     []agentset.Added
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{t: t, current: t.TempDir(), layer: t.TempDir(), home: t.TempDir()}
	w.set = filepath.Join(t.TempDir(), "skills.json")
	pkg := map[string]any{
		"dependencies": map[string]string{"c": "1", "o": "1", "x": "1"},
		"berth": map[string]any{
			"c": map[string]any{"kind": "agent", "name": "C", "keys": []string{}},
			"o": map[string]any{"kind": "agent", "name": "O", "keys": []string{}},
			"x": map[string]any{"kind": "agent", "name": "X", "keys": []string{}},
		},
		"berthAgents": map[string]any{"schema": 1, "agents": map[string]any{
			"c": map[string]any{"skills": map[string]any{"dir": "~/.c/skills"}},
			"o": map[string]any{"skills": map[string]any{"dir": "$XDG_CONFIG_HOME/o/skills", "also": []string{"~/.c/skills"}}},
		}},
	}
	raw, _ := json.Marshal(pkg)
	os.WriteFile(filepath.Join(w.current, "package.json"), raw, 0o644)
	for _, dep := range []string{"c", "o", "x"} {
		d := filepath.Join(w.current, "node_modules", dep)
		os.MkdirAll(d, 0o755)
		m, _ := json.Marshal(map[string]any{"name": dep, "bin": map[string]string{dep: "cli.js"}})
		os.WriteFile(filepath.Join(d, "package.json"), m, 0o644)
		w.enable(dep, true)
	}
	w.repo = newFixture(t, map[string]string{"skills/pdf/SKILL.md": pdfSkill, "skills/pdf/run.sh": "x", "other/SKILL.md": "---\nname: other\ndescription: o\n---\n"})
	w.commit = w.repo.git(t, "rev-parse", "HEAD")
	return w
}

func (w *world) enable(dep string, on bool) {
	p := filepath.Join(w.layer, dep)
	if on {
		os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755)
	} else {
		os.Remove(p)
	}
}

func (w *world) skill(path string) config.Skill {
	return config.Skill{Repo: w.repo.url, Path: path, Ref: "main", Commit: w.commit, Description: "d", Enabled: true}
}

func (w *world) write(s map[string]config.Skill) {
	if err := config.WriteSkillsFile(w.set, s, -1, -1); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) run() (string, error) {
	var log strings.Builder
	set, err := agentset.Load(&agentset.Locked{Dir: w.current, Link: w.current}, w.addedRoot, w.added)
	if err != nil {
		return "", err
	}
	s := Syncer{Set: set, Layer: w.layer, Home: w.home, ConfigHome: filepath.Join(w.home, ".config"),
		Settings: w.set, Git: testGit(), Log: &log}
	err = s.Run(context.Background())
	return log.String(), err
}

func (w *world) link(dir, id string) string {
	l, err := os.Readlink(filepath.Join(w.home, dir, id))
	if err != nil {
		return ""
	}
	return l
}

func TestSyncLinksEveryReachedAgent(t *testing.T) {
	w := newWorld(t)
	w.write(map[string]config.Skill{"pdf": w.skill("skills/pdf")})
	if log, err := w.run(); err != nil {
		t.Fatalf("%v\n%s", err, log)
	}
	store := filepath.Join(StoreDir(w.home), "pdf")
	if body, _ := os.ReadFile(filepath.Join(store, "SKILL.md")); string(body) != pdfSkill {
		t.Errorf("store SKILL.md = %q", body)
	}
	if got := w.link(".c/skills", "pdf"); got != store {
		t.Errorf("c link = %q", got)
	}
	// o reads c's directory, so it gets no link of its own.
	if got := w.link(".config/o/skills", "pdf"); got != "" {
		t.Errorf("o was linked to %q", got)
	}
}

func TestSyncNarrowDisableRemove(t *testing.T) {
	w := newWorld(t)
	s := w.skill("skills/pdf")
	w.write(map[string]config.Skill{"pdf": s})
	w.run()
	s.Agents = []string{"o"}
	w.write(map[string]config.Skill{"pdf": s})
	w.run()
	if w.link(".c/skills", "pdf") != "" || w.link(".config/o/skills", "pdf") == "" {
		t.Errorf("narrowed: c=%q o=%q", w.link(".c/skills", "pdf"), w.link(".config/o/skills", "pdf"))
	}
	s.Enabled = false
	w.write(map[string]config.Skill{"pdf": s})
	w.run()
	if w.link(".config/o/skills", "pdf") != "" {
		t.Error("disabled skill still linked")
	}
	if _, err := os.Stat(filepath.Join(StoreDir(w.home), "pdf")); err != nil {
		t.Error("disabling removed the store copy")
	}
	w.write(map[string]config.Skill{})
	w.run()
	if _, err := os.Stat(filepath.Join(StoreDir(w.home), "pdf")); err == nil {
		t.Error("removing kept the store copy")
	}
	if caches, _ := filepath.Glob(filepath.Join(CacheRoot(w.home), "*.git")); len(caches) > 0 {
		t.Errorf("caches left: %v", caches)
	}
}

func TestSyncDisabledAgentLosesLinks(t *testing.T) {
	w := newWorld(t)
	w.write(map[string]config.Skill{"pdf": w.skill("skills/pdf")})
	w.run()
	w.enable("c", false)
	w.run()
	if w.link(".c/skills", "pdf") != "" {
		t.Error("disabled agent keeps its link")
	}
	// o no longer reaches pdf through c, so it gets its own link.
	if w.link(".config/o/skills", "pdf") == "" {
		t.Error("o lost the skill")
	}
}

func TestSyncLeavesUserSkillsAlone(t *testing.T) {
	w := newWorld(t)
	own := filepath.Join(w.home, ".c", "skills", "pdf")
	os.MkdirAll(own, 0o755)
	os.WriteFile(filepath.Join(own, "SKILL.md"), []byte("mine"), 0o644)
	foreign := filepath.Join(w.home, ".config", "o", "skills", "other")
	os.MkdirAll(filepath.Dir(foreign), 0o755)
	os.Symlink("/somewhere/else", foreign)
	w.write(map[string]config.Skill{"pdf": w.skill("skills/pdf"), "other": func() config.Skill {
		s := w.skill("other")
		s.Agents = []string{"o"}
		return s
	}()})
	log, err := w.run()
	if err != nil {
		t.Fatalf("%v\n%s", err, log)
	}
	if body, _ := os.ReadFile(filepath.Join(own, "SKILL.md")); string(body) != "mine" {
		t.Error("the user's skill was replaced")
	}
	if l, _ := os.Readlink(foreign); l != "/somewhere/else" {
		t.Errorf("the user's link now points to %q", l)
	}
	if !strings.Contains(log, "already defined by the user") {
		t.Errorf("log = %s", log)
	}
	w.write(map[string]config.Skill{})
	w.run()
	if _, err := os.Stat(own); err != nil {
		t.Error("removing the skill removed the user's directory")
	}
}

func TestSyncFailingSkillKeepsLinksAndOthersConverge(t *testing.T) {
	w := newWorld(t)
	pdf, other := w.skill("skills/pdf"), w.skill("other")
	w.write(map[string]config.Skill{"pdf": pdf})
	w.run()
	pdf.Commit = "0123456789abcdef0123456789abcdef01234567" // not in the repository
	pdf.Ref = "gone"
	w.write(map[string]config.Skill{"pdf": pdf, "other": other})
	log, err := w.run()
	if err == nil || !strings.Contains(err.Error(), "pdf") {
		t.Errorf("err = %v", err)
	}
	if w.link(".c/skills", "pdf") == "" {
		t.Error("the failing skill lost its link")
	}
	if w.link(".c/skills", "other") == "" {
		t.Errorf("the other skill did not converge:\n%s", log)
	}
}

func TestSyncUpdatesToNewCommit(t *testing.T) {
	w := newWorld(t)
	s := w.skill("skills/pdf")
	w.write(map[string]config.Skill{"pdf": s})
	w.run()
	s.Commit = w.repo.commit(t, map[string]string{"skills/pdf/new.md": "n"})
	w.write(map[string]config.Skill{"pdf": s})
	if log, err := w.run(); err != nil {
		t.Fatalf("%v\n%s", err, log)
	}
	if _, err := os.Stat(filepath.Join(StoreDir(w.home), "pdf", "new.md")); err != nil {
		t.Error("the store was not updated")
	}
}

func TestSyncMissingAndNewerSettings(t *testing.T) {
	w := newWorld(t)
	if _, err := w.run(); err != nil {
		t.Errorf("missing skills.json: %v", err)
	}
	os.WriteFile(w.set, []byte(`{"schema":2,"skills":{}}`), 0o640)
	if _, err := w.run(); err == nil || !strings.Contains(err.Error(), "schema 2") {
		t.Errorf("newer skills.json: %v", err)
	}
}

func TestSyncLinksAddedAgent(t *testing.T) {
	w := newWorld(t)
	w.addedRoot = t.TempDir()
	testutil.AddedInstall(t, w.addedRoot, "copilot", "0123456789ab", "@github/copilot", "1.0.91", testutil.CopilotPackageJSON, "copilot")
	w.added, _ = agentset.Installed(w.addedRoot)
	w.enable("copilot", true)
	w.write(map[string]config.Skill{"pdf": w.skill("skills/pdf")})
	if log, err := w.run(); err != nil {
		t.Fatalf("%v\n%s", err, log)
	}
	if got := w.link(".copilot/skills", "pdf"); got != filepath.Join(StoreDir(w.home), "pdf") {
		t.Errorf("copilot link = %q", got)
	}
}
