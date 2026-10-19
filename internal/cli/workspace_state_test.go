package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/wsstate"
)

// stateConfig is a config whose agent home and config dir are fresh, with
// workspace api published at a new git repository.
func stateConfig(t *testing.T) (Config, wsstate.Store, string) {
	t.Helper()
	cfg := testConfig(t)
	cfg.ConfigDir = t.TempDir()
	home := t.TempDir()
	cfg.Getenv = func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}
	ws := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", ws).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	store := wsstate.Store{Home: home, Published: filepath.Join(cfg.ConfigDir, "workspaces.json")}
	if err := config.WriteWorkspacesFile(store.Published, map[string]config.PublishedWorkspace{"api": {Path: ws}}, -1, -1); err != nil {
		t.Fatal(err)
	}
	return cfg, store, ws
}

// seed records a linked pdf and a vendored docx in ws.
func seed(t *testing.T, store wsstate.Store, ws string) {
	t.Helper()
	export := filepath.Join(store.Home, ".local", "share", "berth", "skills", "fx", "pdf")
	os.MkdirAll(filepath.Join(ws, ".c", "skills"), 0o755)
	os.Symlink(export, filepath.Join(ws, ".c", "skills", "pdf"))
	os.MkdirAll(filepath.Join(ws, ".agents", "skills", "docx"), 0o755)
	err := store.Update("api", func(_ string, st *wsstate.State) error {
		st.Skills.Path = ws
		st.Skills.Dirs = []string{".agents", ".agents/skills", ".c", ".c/skills"}
		st.Skills.Entries["fx/pdf"] = wsstate.Skill{Name: "pdf", Mode: config.ModeLink, Links: []string{".c/skills/pdf"}}
		st.Skills.Entries["fx/docx"] = wsstate.Skill{Name: "docx", Mode: config.ModeVendor, Dir: ".agents/skills/docx",
			Files: map[string]string{}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWorkspacesRelease(t *testing.T) {
	cfg, store, ws := stateConfig(t)
	seed(t, store, ws)
	out, errOut, code := run(t, cfg, "workspaces", "release", "--json", "--id", "api")
	var res struct{ Kept []string }
	if err := json.Unmarshal([]byte(out), &res); err != nil || code != 0 || len(res.Kept) != 0 {
		t.Fatalf("release: exit %d, %s %s", code, out, errOut)
	}
	for _, p := range []string{".c", ".agents"} {
		if _, err := os.Lstat(filepath.Join(ws, p)); err == nil {
			t.Errorf("%s left behind", p)
		}
	}
	if ids, _ := store.IDs(); len(ids) != 0 {
		t.Errorf("state left: %v", ids)
	}
	// A workspace whose folder is gone releases cleanly too.
	seed(t, store, ws)
	os.RemoveAll(ws)
	if _, _, code := run(t, cfg, "workspaces", "release", "--json", "--id", "api"); code != 0 {
		t.Errorf("release of a missing folder: exit %d", code)
	}
	if ids, _ := store.IDs(); len(ids) != 0 {
		t.Errorf("state left: %v", ids)
	}
}

func TestWorkspacesReport(t *testing.T) {
	cfg, store, ws := stateConfig(t)
	seed(t, store, ws)
	store.Update("api", func(_ string, st *wsstate.State) error {
		e := st.Skills.Entries["fx/docx"]
		e.Files, e.Edited = map[string]string{"SKILL.md": "sha256:abc"}, true
		st.Skills.Entries["fx/docx"] = e
		st.MCP.Path = ws
		st.MCP.Files[".mcp.json"] = wsstate.MCPFile{Style: "claude", Key: "mcpServers", IDs: []string{"c7"}, Tracked: true, Original: "{}"}
		return nil
	})
	out, _, code := run(t, cfg, "workspaces", "report", "--json", "--id", "api")
	if code != 0 || strings.Contains(out, "sha256") || strings.Contains(out, "original") {
		t.Fatalf("report: exit %d, %s", code, out)
	}
	var rep struct {
		Skills wsstate.Skills
		MCP    wsstate.MCP
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil || !rep.Skills.Entries["fx/docx"].Edited || !rep.MCP.Files[".mcp.json"].Tracked {
		t.Errorf("report = %+v, %v", rep, err)
	}
	if _, _, code := run(t, cfg, "workspaces", "report", "--json", "--id", "Bad"); code != 2 {
		t.Errorf("bad id: exit %d", code)
	}
}

func TestSkillsVendorResolve(t *testing.T) {
	cfg, store, ws := stateConfig(t)
	seed(t, store, ws)
	if out, _, code := run(t, cfg, "skills", "vendor", "--json", "--workspace", "api", "--key", "fx/docx", "--action", "overwrite"); code != 0 {
		t.Fatalf("overwrite: exit %d, %s", code, out)
	}
	if st, _ := store.Load("api"); !st.Skills.Entries["fx/docx"].Force {
		t.Error("overwrite did not set the flag")
	}
	if _, _, code := run(t, cfg, "skills", "vendor", "--json", "--workspace", "api", "--key", "fx/docx", "--action", "release"); code != 0 {
		t.Fatalf("release: exit %d", code)
	}
	out, _, code := run(t, cfg, "skills", "vendor", "--json", "--workspace", "api", "--key", "fx/docx", "--action", "release")
	if code != 1 || !strings.Contains(out, `"code": "not_vendored"`) {
		t.Errorf("again: exit %d, %s", code, out)
	}
	out, _, code = run(t, cfg, "skills", "vendor", "--json", "--workspace", "web", "--key", "fx/docx", "--action", "release")
	if code != 1 || !strings.Contains(out, `"code": "unknown_workspace"`) {
		t.Errorf("unknown workspace: exit %d, %s", code, out)
	}
	for _, bad := range [][]string{
		{"--workspace", "api", "--key", "docx", "--action", "release"},
		{"--workspace", "api", "--key", "fx/Docx", "--action", "release"},
		{"--workspace", "Api", "--key", "fx/docx", "--action", "release"},
	} {
		if _, _, code := run(t, cfg, append([]string{"skills", "vendor", "--json"}, bad...)...); code != 2 {
			t.Errorf("%v: exit %d", bad, code)
		}
	}
}

func TestWorkspacesInstructionsCommands(t *testing.T) {
	cfg, store, ws := stateConfig(t)
	out, errOut, code := run(t, cfg, "workspaces", "instructions", "--action", "read", "--json", "--id", "api", "--file", "CLAUDE.md")
	var f struct {
		State, SHA256 string
		Editable      bool
	}
	if err := json.Unmarshal([]byte(out), &f); err != nil || code != 0 || f.State != "missing" || !f.Editable {
		t.Fatalf("read: exit %d, %s %s", code, out, errOut)
	}
	cfg.Stdin = strings.NewReader("# Rules\n")
	out, errOut, code = run(t, cfg, "workspaces", "instructions", "--action", "write", "--json", "--id", "api",
		"--file", "CLAUDE.md", "--base-sha256", "")
	if code != 0 || !strings.Contains(out, `"sha256"`) {
		t.Fatalf("write: exit %d, %s %s", code, out, errOut)
	}
	if raw, _ := os.ReadFile(filepath.Join(ws, "CLAUDE.md")); string(raw) != "# Rules\n" {
		t.Errorf("CLAUDE.md = %q", raw)
	}
	cfg.Stdin = strings.NewReader("again\n")
	out, _, code = run(t, cfg, "workspaces", "instructions", "--action", "write", "--json", "--id", "api",
		"--file", "CLAUDE.md", "--base-sha256", "")
	if code == 0 || !strings.Contains(out, `"changed_on_disk"`) {
		t.Errorf("stale write: exit %d, %s", code, out)
	}
	out, _, code = run(t, cfg, "workspaces", "instructions", "--action", "merge", "--json", "--id", "api", "--file", "CLAUDE.md")
	if code == 0 || !strings.Contains(out, `"not_shared"`) {
		t.Errorf("merge while not shared: exit %d, %s", code, out)
	}
	out, _, code = run(t, cfg, "workspaces", "instructions", "--action", "read", "--json", "--id", "nope", "--file", "CLAUDE.md")
	if code == 0 || !strings.Contains(out, `"unknown_workspace"`) {
		t.Errorf("unknown workspace: exit %d, %s", code, out)
	}
	for _, bad := range [][]string{{"--action", "read", "--id", "api"}, {"--action", "nope", "--id", "api", "--file", "CLAUDE.md"}} {
		if _, _, code := run(t, cfg, append([]string{"workspaces", "instructions", "--json"}, bad...)...); code != 2 {
			t.Errorf("%v: exit %d, want usage", bad, code)
		}
	}
	_ = store
}

func TestReleaseAndReportCoverInstructions(t *testing.T) {
	cfg, store, ws := stateConfig(t)
	os.WriteFile(filepath.Join(ws, "AGENTS.md"), []byte("a\n"), 0o644)
	os.Symlink("AGENTS.md", filepath.Join(ws, "CLAUDE.md"))
	store.Update("api", func(folder string, st *wsstate.State) error {
		st.Instructions = wsstate.Instructions{Path: folder, Links: []string{"CLAUDE.md"}, Skipped: map[string]string{"GEMINI.md": "GEMINI.md has its own content"}}
		return nil
	})
	out, _, code := run(t, cfg, "workspaces", "report", "--json", "--id", "api")
	if code != 0 || !strings.Contains(out, `"instructions"`) || !strings.Contains(out, "GEMINI.md has its own content") {
		t.Fatalf("report: exit %d, %s", code, out)
	}
	if _, _, code := run(t, cfg, "workspaces", "release", "--json", "--id", "api"); code != 0 {
		t.Fatalf("release: exit %d", code)
	}
	if _, err := os.Lstat(filepath.Join(ws, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Errorf("CLAUDE.md link kept: %v", err)
	}
	if raw, _ := os.ReadFile(filepath.Join(ws, "AGENTS.md")); string(raw) != "a\n" {
		t.Errorf("AGENTS.md = %q", raw)
	}
	if ids, _ := store.IDs(); len(ids) != 0 {
		t.Errorf("state left: %v", ids)
	}
}
