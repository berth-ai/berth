package skills

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/wsstate"
)

// publish writes workspaces.json with ws (id → folder).
func (w *world) publish(ws map[string]string) {
	w.t.Helper()
	pub := map[string]config.PublishedWorkspace{}
	for id, p := range ws {
		pub[id] = config.PublishedWorkspace{Path: p}
	}
	if err := config.WriteWorkspacesFile(w.wsFile, pub, -1, -1); err != nil {
		w.t.Fatal(err)
	}
}

// repoFolder makes a git repository to use as a workspace.
func repoFolder(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	return dir
}

// inWS attaches a skill to no agent and to workspaces in mode.
func inWS(mode string, ids ...string) config.Attachment {
	ws := map[string]string{}
	for _, id := range ids {
		ws[id] = mode
	}
	return config.Attachment{Enabled: true, Agents: []string{}, Workspaces: ws}
}

func readlink(p string) string { l, _ := os.Readlink(p); return l }

func exclude(t *testing.T, ws string) string {
	t.Helper()
	raw, _ := os.ReadFile(filepath.Join(ws, ".git", "info", "exclude"))
	return string(raw)
}

func (w *world) state(id string) wsstate.State {
	st, err := wsstate.Store{Home: w.home, Published: w.wsFile}.Load(id)
	if err != nil {
		w.t.Fatal(err)
	}
	return st
}

func TestWorkspaceLinks(t *testing.T) {
	w := newWorld(t)
	ws := repoFolder(t)
	w.publish(map[string]string{"api": ws})
	w.write(map[string]config.Source{"fx": w.source(map[string]config.Attachment{"pdf": inWS(config.ModeLink, "api")})})
	w.mustRun()
	link := filepath.Join(ws, ".c", "skills", "pdf")
	if got := readlink(link); got != w.export("fx", "pdf") {
		t.Fatalf("c link = %q", got)
	}
	// o reads c's project folder, so it gets no link of its own.
	if exists(filepath.Join(ws, ".o", "skills", "pdf")) {
		t.Error("o was linked too")
	}
	// The attachment names no agent: home folders stay empty.
	if w.link(".c/skills", "pdf") != "" {
		t.Error("a workspace-only skill was linked into the home")
	}
	if got := exclude(t, ws); !strings.Contains(got, wsstate.BlockStart+"\n/.c/skills/pdf\n"+wsstate.BlockEnd) {
		t.Errorf("exclude = %q", got)
	}
	if out, _ := exec.Command("git", "-C", ws, "status", "--porcelain").Output(); len(out) != 0 {
		t.Errorf("git status = %s", out)
	}
	if e := w.state("api").Skills.Entries["fx/pdf"]; e.Mode != config.ModeLink || !slices.Equal(e.Links, []string{".c/skills/pdf"}) {
		t.Errorf("state = %+v", e)
	}

	// With c disabled, o needs its own link.
	w.enable("c", false)
	w.mustRun()
	if exists(link) || readlink(filepath.Join(ws, ".o", "skills", "pdf")) != w.export("fx", "pdf") {
		t.Errorf("after disabling c: c=%v o=%q", exists(link), readlink(filepath.Join(ws, ".o", "skills", "pdf")))
	}
	if got := exclude(t, ws); !strings.Contains(got, "/.o/skills/pdf") || strings.Contains(got, "/.c/skills/pdf") {
		t.Errorf("exclude = %q", got)
	}

	// Detaching removes the link, the folders Berth made and the block.
	w.write(map[string]config.Source{"fx": w.source(map[string]config.Attachment{})})
	w.mustRun()
	for _, p := range []string{".o", ".c"} {
		if exists(filepath.Join(ws, p)) {
			t.Errorf("%s left behind", p)
		}
	}
	if strings.Contains(exclude(t, ws), wsstate.BlockStart) {
		t.Error("exclude block left behind")
	}
	if _, err := os.Stat(filepath.Join(wsstate.Store{Home: w.home}.Dir(), "api.json")); !os.IsNotExist(err) {
		t.Errorf("state file left: %v", err)
	}
}

func TestWorkspaceLinkAlongsideAgents(t *testing.T) {
	w := newWorld(t)
	ws := t.TempDir() // not a git repository: links, no exclude block
	w.publish(map[string]string{"api": ws})
	att := to()
	att.Workspaces = map[string]string{"api": config.ModeLink}
	w.write(map[string]config.Source{"fx": w.source(map[string]config.Attachment{"pdf": att})})
	w.mustRun()
	if w.link(".c/skills", "pdf") == "" || readlink(filepath.Join(ws, ".c", "skills", "pdf")) == "" {
		t.Error("home and workspace links expected")
	}
	if exists(filepath.Join(ws, ".git")) {
		t.Error("created a .git")
	}
	// Disabling the attachment removes workspace links too; the export stays.
	att.Enabled = false
	w.write(map[string]config.Source{"fx": w.source(map[string]config.Attachment{"pdf": att})})
	w.mustRun()
	if exists(filepath.Join(ws, ".c", "skills", "pdf")) || !exists(w.export("fx", "pdf")) {
		t.Error("disabled: workspace link kept or export removed")
	}
}

func TestWorkspaceLinkLeavesUserEntries(t *testing.T) {
	w := newWorld(t)
	ws := repoFolder(t)
	own := filepath.Join(ws, ".c", "skills", "pdf")
	os.MkdirAll(own, 0o755)
	os.WriteFile(filepath.Join(own, "SKILL.md"), []byte("mine"), 0o644)
	w.publish(map[string]string{"api": ws})
	w.write(map[string]config.Source{"fx": w.source(map[string]config.Attachment{"pdf": inWS(config.ModeLink, "api")})})
	log := w.mustRun()
	if body, _ := os.ReadFile(filepath.Join(own, "SKILL.md")); string(body) != "mine" {
		t.Error("the user's skill was replaced")
	}
	if !strings.Contains(log, "already exists") || w.state("api").Skills.Skipped[".c/skills/pdf"] == "" {
		t.Errorf("not reported: %s %+v", log, w.state("api").Skills)
	}
	if strings.Contains(exclude(t, ws), "pdf") {
		t.Error("the user's folder was hidden from git")
	}
	w.write(map[string]config.Source{})
	w.mustRun()
	if !exists(own) {
		t.Error("detaching removed the user's folder")
	}
}

func TestWorkspaceLinkSkipsSymlinkedFolder(t *testing.T) {
	w := newWorld(t)
	ws := repoFolder(t)
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(ws, ".c"))
	w.publish(map[string]string{"api": ws})
	w.write(map[string]config.Source{"fx": w.source(map[string]config.Attachment{"pdf": inWS(config.ModeLink, "api")})})
	w.mustRun()
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("wrote through the symlink: %v", entries)
	}
	if w.state("api").Skills.Skipped[".c/skills/pdf"] == "" {
		t.Error("the skipped path is not reported")
	}
}

func TestWorkspaceMissingFailsAlone(t *testing.T) {
	w := newWorld(t)
	ok := repoFolder(t)
	gone := filepath.Join(t.TempDir(), "gone")
	w.publish(map[string]string{"api": ok, "old": gone})
	att := to()
	att.Workspaces = map[string]string{"api": config.ModeLink, "old": config.ModeLink}
	w.write(map[string]config.Source{"fx": w.source(map[string]config.Attachment{"pdf": att})})
	log, err := w.run()
	if err == nil || !strings.Contains(err.Error(), "workspace old") || strings.Contains(err.Error(), "workspace api") {
		t.Fatalf("err = %v\n%s", err, log)
	}
	if readlink(filepath.Join(ok, ".c", "skills", "pdf")) == "" || w.link(".c/skills", "pdf") == "" {
		t.Error("the other targets did not converge")
	}
	if e := w.state("old").Skills.Error; !strings.Contains(e, "missing") {
		t.Errorf("old error = %q", e)
	}
}

func TestWorkspaceReadOnlyFailsAlone(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores modes")
	}
	w := newWorld(t)
	ro := repoFolder(t)
	os.Chmod(ro, 0o555)
	t.Cleanup(func() { os.Chmod(ro, 0o755) })
	w.publish(map[string]string{"ro": ro})
	att := to()
	att.Workspaces = map[string]string{"ro": config.ModeLink}
	w.write(map[string]config.Source{"fx": w.source(map[string]config.Attachment{"pdf": att})})
	if _, err := w.run(); err == nil || !strings.Contains(err.Error(), "workspace ro") {
		t.Errorf("err = %v", err)
	}
	if w.link(".c/skills", "pdf") == "" {
		t.Error("the home link did not converge")
	}
}

func TestWorkspaceMissingAttachmentHolds(t *testing.T) {
	w := newWorld(t)
	ws := repoFolder(t)
	w.publish(map[string]string{"api": ws})
	src := w.source(map[string]config.Attachment{"pdf": inWS(config.ModeLink, "api")})
	w.write(map[string]config.Source{"fx": src})
	w.mustRun()
	delete(src.Found, "pdf")
	w.write(map[string]config.Source{"fx": src})
	w.mustRun()
	if readlink(filepath.Join(ws, ".c", "skills", "pdf")) == "" {
		t.Error("a missing attachment lost its workspace link")
	}
}

func TestWorkspaceForgottenIsReleased(t *testing.T) {
	w := newWorld(t)
	ws := repoFolder(t)
	w.publish(map[string]string{"api": ws})
	w.write(map[string]config.Source{"fx": w.source(map[string]config.Attachment{"pdf": inWS(config.ModeLink, "api")})})
	w.mustRun()
	// The broker forgets api: workspaces.json and the attachment drop it.
	w.publish(map[string]string{})
	w.write(map[string]config.Source{"fx": w.source(map[string]config.Attachment{})})
	w.mustRun()
	if exists(filepath.Join(ws, ".c")) || strings.Contains(exclude(t, ws), wsstate.BlockStart) {
		t.Error("forgotten workspace keeps Berth's entries")
	}
	if ids, _ := (wsstate.Store{Home: w.home}).IDs(); len(ids) != 0 {
		t.Errorf("state files left: %v", ids)
	}
}

func TestProjectDirs(t *testing.T) {
	tg := func(dep, project string, also ...string) target {
		return target{dep: dep, project: project, projectAlso: also}
	}
	claude, codex := tg("claude", ".claude/skills"), tg("codex", ".agents/skills")
	opencode := tg("opencode", ".opencode/skills", ".claude/skills", ".agents/skills")
	gemini := tg("gemini", ".gemini/skills", ".agents/skills")
	for _, tc := range []struct {
		in   []target
		want []string
	}{
		{[]target{claude, codex, opencode, gemini}, []string{".agents/skills", ".claude/skills"}},
		{[]target{opencode, gemini}, []string{".gemini/skills", ".opencode/skills"}},
		{[]target{claude, opencode}, []string{".claude/skills"}},
		{[]target{tg("x", "")}, nil},
	} {
		if got := projectDirs(tc.in); !slices.Equal(got, tc.want) {
			t.Errorf("projectDirs = %v, want %v", got, tc.want)
		}
	}
}
