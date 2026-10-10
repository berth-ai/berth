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
	wsFile                    string
	repo                      *fixture
	commit                    string
	addedRoot                 string
	added                     []agentset.Added
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{t: t, current: t.TempDir(), layer: t.TempDir(), home: t.TempDir()}
	w.set = filepath.Join(t.TempDir(), "skills.json")
	w.wsFile = filepath.Join(t.TempDir(), "workspaces.json")
	pkg := map[string]any{
		"dependencies": map[string]string{"c": "1", "o": "1", "x": "1"},
		"berth": map[string]any{
			"c": map[string]any{"kind": "agent", "name": "C", "keys": []string{}},
			"o": map[string]any{"kind": "agent", "name": "O", "keys": []string{}},
			"x": map[string]any{"kind": "agent", "name": "X", "keys": []string{}},
		},
		"berthAgents": map[string]any{"schema": 1, "agents": map[string]any{
			"c": map[string]any{"skills": map[string]any{"dir": "~/.c/skills", "project": ".c/skills"}},
			"o": map[string]any{"skills": map[string]any{"dir": "$XDG_CONFIG_HOME/o/skills", "also": []string{"~/.c/skills"},
				"project": ".o/skills", "project_also": []string{".c/skills"}}},
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

// source is the fixture repository at w.commit, with pdf and other found,
// attached as att says.
func (w *world) source(att map[string]config.Attachment) config.Source {
	return config.Source{Repo: w.repo.url, Ref: "main", Commit: w.commit,
		Found: map[string]config.Found{
			"pdf":   {Path: "skills/pdf", Description: "PDF tools"},
			"other": {Path: "other", Description: "o"},
		},
		Attached: att}
}

// to attaches a skill, enabled, to agents; none means every agent.
func to(agents ...string) config.Attachment {
	return config.Attachment{Enabled: true, Agents: agents, Workspaces: map[string]string{}}
}

func (w *world) write(s map[string]config.Source) {
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
		Settings: w.set, Workspaces: w.wsFile, Git: testGit(), Log: &log}
	err = s.Run(context.Background())
	return log.String(), err
}

func (w *world) mustRun() string {
	w.t.Helper()
	log, err := w.run()
	if err != nil {
		w.t.Fatalf("%v\n%s", err, log)
	}
	return log
}

func (w *world) link(dir, name string) string {
	l, err := os.Readlink(filepath.Join(w.home, dir, name))
	if err != nil {
		return ""
	}
	return l
}

func (w *world) export(source, name string) string {
	return filepath.Join(StoreDir(w.home), source, name)
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

func TestSyncLinksEveryReachedAgent(t *testing.T) {
	w := newWorld(t)
	w.write(map[string]config.Source{"fx": w.source(map[string]config.Attachment{"pdf": to()})})
	w.mustRun()
	store := w.export("fx", "pdf")
	if body, _ := os.ReadFile(filepath.Join(store, "SKILL.md")); string(body) != pdfSkill {
		t.Errorf("export SKILL.md = %q", body)
	}
	if got := w.link(".c/skills", "pdf"); got != store {
		t.Errorf("c link = %q", got)
	}
	// o reads c's directory, so it gets no link of its own.
	if got := w.link(".config/o/skills", "pdf"); got != "" {
		t.Errorf("o was linked to %q", got)
	}
	if exists(w.export("fx", "other")) {
		t.Error("an unattached skill was exported")
	}
}

func TestSyncNarrowDisableDetachRemove(t *testing.T) {
	w := newWorld(t)
	att := map[string]config.Attachment{"pdf": to()}
	w.write(map[string]config.Source{"fx": w.source(att)})
	w.mustRun()
	att["pdf"] = to("o")
	w.write(map[string]config.Source{"fx": w.source(att)})
	w.mustRun()
	if w.link(".c/skills", "pdf") != "" || w.link(".config/o/skills", "pdf") == "" {
		t.Errorf("narrowed: c=%q o=%q", w.link(".c/skills", "pdf"), w.link(".config/o/skills", "pdf"))
	}
	att["pdf"] = config.Attachment{Enabled: true, Agents: []string{}}
	w.write(map[string]config.Source{"fx": w.source(att)})
	w.mustRun()
	if w.link(".config/o/skills", "pdf") != "" {
		t.Error("an attachment to no agent is still linked")
	}
	att["pdf"] = config.Attachment{Enabled: false}
	w.write(map[string]config.Source{"fx": w.source(att)})
	w.mustRun()
	if !exists(w.export("fx", "pdf")) {
		t.Error("disabling removed the export")
	}
	w.write(map[string]config.Source{"fx": w.source(map[string]config.Attachment{})})
	w.mustRun()
	if exists(w.export("fx", "pdf")) {
		t.Error("detaching kept the export")
	}
	w.write(map[string]config.Source{})
	w.mustRun()
	if exists(filepath.Join(StoreDir(w.home), "fx")) {
		t.Error("removing the source kept its folder")
	}
	if caches, _ := filepath.Glob(filepath.Join(CacheRoot(w.home), "*.git")); len(caches) > 0 {
		t.Errorf("caches left: %v", caches)
	}
}

func TestSyncDisabledAgentLosesLinks(t *testing.T) {
	w := newWorld(t)
	w.write(map[string]config.Source{"fx": w.source(map[string]config.Attachment{"pdf": to()})})
	w.mustRun()
	w.enable("c", false)
	w.mustRun()
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
	w.write(map[string]config.Source{"fx": w.source(map[string]config.Attachment{"pdf": to(), "other": to("o")})})
	log := w.mustRun()
	if body, _ := os.ReadFile(filepath.Join(own, "SKILL.md")); string(body) != "mine" {
		t.Error("the user's skill was replaced")
	}
	if l, _ := os.Readlink(foreign); l != "/somewhere/else" {
		t.Errorf("the user's link now points to %q", l)
	}
	if !strings.Contains(log, "already defined by the user") {
		t.Errorf("log = %s", log)
	}
	w.write(map[string]config.Source{})
	w.mustRun()
	if !exists(own) {
		t.Error("removing the source removed the user's directory")
	}
}

func TestSyncFailingSkillKeepsLinksAndOthersConverge(t *testing.T) {
	w := newWorld(t)
	w.write(map[string]config.Source{"fx": w.source(map[string]config.Attachment{"pdf": to()})})
	w.mustRun()
	broken := w.source(map[string]config.Attachment{"pdf": to()})
	broken.Commit = "0123456789abcdef0123456789abcdef01234567" // not in the repository
	broken.Ref = "gone"
	w.write(map[string]config.Source{"fx": broken, "fy": w.source(map[string]config.Attachment{"other": to()})})
	log, err := w.run()
	if err == nil || !strings.Contains(err.Error(), "pdf from fx") {
		t.Errorf("err = %v", err)
	}
	if w.link(".c/skills", "pdf") == "" {
		t.Error("the failing skill lost its link")
	}
	if w.link(".c/skills", "other") != w.export("fy", "other") {
		t.Errorf("the other source did not converge:\n%s", log)
	}
}

func TestSyncUpdatesToNewCommit(t *testing.T) {
	w := newWorld(t)
	src := w.source(map[string]config.Attachment{"pdf": to()})
	w.write(map[string]config.Source{"fx": src})
	w.mustRun()
	src.Commit = w.repo.commit(t, map[string]string{"skills/pdf/new.md": "n"})
	w.write(map[string]config.Source{"fx": src})
	w.mustRun()
	if !exists(filepath.Join(w.export("fx", "pdf"), "new.md")) {
		t.Error("the export was not updated")
	}
}

func TestSyncMissingAttachmentKeepsLinks(t *testing.T) {
	w := newWorld(t)
	src := w.source(map[string]config.Attachment{"pdf": to(), "other": to()})
	w.write(map[string]config.Source{"fx": src})
	w.mustRun()
	// An update dropped pdf and broke other's SKILL.md.
	src.Commit = w.repo.commit(t, map[string]string{"README.md": "r"})
	src.Found = map[string]config.Found{"other": {Path: "other", Error: "SKILL.md has no description"}}
	w.write(map[string]config.Source{"fx": src})
	log := w.mustRun()
	if w.link(".c/skills", "pdf") != w.export("fx", "pdf") || w.link(".c/skills", "other") != w.export("fx", "other") {
		t.Errorf("missing attachments lost their links:\n%s", log)
	}
	if !strings.Contains(log, "pdf is not in fx") || !strings.Contains(log, "other is not in fx") {
		t.Errorf("log = %s", log)
	}
	src.Attached = map[string]config.Attachment{}
	w.write(map[string]config.Source{"fx": src})
	w.mustRun()
	if w.link(".c/skills", "pdf") != "" || exists(w.export("fx", "pdf")) {
		t.Error("detaching a missing skill kept its link or export")
	}
}

func TestSyncMigratesLegacyCopies(t *testing.T) {
	w := newWorld(t)
	store := StoreDir(w.home)
	// C2 left copies at <store>/pdf and <store>/fx (a skill named like the
	// new source), with pdf linked for c.
	for _, id := range []string{"pdf", "fx"} {
		os.MkdirAll(filepath.Join(store, id), 0o755)
		os.WriteFile(filepath.Join(store, id, "SKILL.md"), []byte("old"), 0o644)
	}
	cLink := filepath.Join(w.home, ".c", "skills", "pdf")
	os.MkdirAll(filepath.Dir(cLink), 0o755)
	os.Symlink(filepath.Join(store, "pdf"), cLink)
	old, _ := json.Marshal(map[string]any{"commits": map[string]string{"pdf": w.commit, "fx": w.commit}, "links": []string{cLink}})
	os.MkdirAll(filepath.Dir(StatePath(w.home)), 0o755)
	os.WriteFile(StatePath(w.home), old, 0o600)

	w.write(map[string]config.Source{"fx": w.source(map[string]config.Attachment{"pdf": to()})})
	w.mustRun()
	if got := w.link(".c/skills", "pdf"); got != w.export("fx", "pdf") {
		t.Errorf("c link = %q", got)
	}
	if exists(filepath.Join(store, "pdf")) || exists(filepath.Join(store, "fx", "SKILL.md")) {
		t.Error("C2's copies were kept")
	}
	if body, _ := os.ReadFile(filepath.Join(w.export("fx", "pdf"), "SKILL.md")); string(body) != pdfSkill {
		t.Errorf("export = %q", body)
	}
	if raw, _ := os.ReadFile(StatePath(w.home)); strings.Contains(string(raw), "commits") {
		t.Errorf("state still lists C2's copies: %s", raw)
	}
}

func TestSyncMissingAndOtherSettings(t *testing.T) {
	w := newWorld(t)
	if _, err := w.run(); err != nil {
		t.Errorf("missing skills.json: %v", err)
	}
	os.WriteFile(w.set, []byte(`{"schema":3,"sources":{}}`), 0o640)
	if _, err := w.run(); err == nil || !strings.Contains(err.Error(), "schema 3") {
		t.Errorf("newer skills.json: %v", err)
	}
	os.WriteFile(w.set, []byte(`{"schema":1,"skills":{}}`), 0o640)
	if _, err := w.run(); err == nil || !strings.Contains(err.Error(), "schema 1") {
		t.Errorf("C2's skills.json: %v", err)
	}
}

func TestSyncLinksAddedAgent(t *testing.T) {
	w := newWorld(t)
	w.addedRoot = t.TempDir()
	testutil.AddedInstall(t, w.addedRoot, "copilot", "0123456789ab", "@github/copilot", "1.0.91", testutil.CopilotPackageJSON, "copilot")
	w.added, _ = agentset.Installed(w.addedRoot)
	w.enable("copilot", true)
	w.write(map[string]config.Source{"fx": w.source(map[string]config.Attachment{"pdf": to()})})
	w.mustRun()
	if got := w.link(".copilot/skills", "pdf"); got != w.export("fx", "pdf") {
		t.Errorf("copilot link = %q", got)
	}
}
