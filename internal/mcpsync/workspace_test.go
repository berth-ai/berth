package mcpsync

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

// workspace publishes a fresh git repository as workspace id.
func (f *fixture) workspace(id string) string {
	f.t.Helper()
	ws := f.t.TempDir()
	if out, err := exec.Command("git", "init", "-q", ws).CombinedOutput(); err != nil {
		f.t.Fatalf("git init: %v %s", err, out)
	}
	f.publish(map[string]string{id: ws})
	return ws
}

func (f *fixture) publish(ws map[string]string) {
	pub := map[string]config.PublishedWorkspace{}
	for id, p := range ws {
		pub[id] = config.PublishedWorkspace{Path: p}
	}
	must(f.t, config.WriteWorkspacesFile(f.s.Workspaces, pub, -1, -1))
}

func (f *fixture) wsState(id string) wsstate.State {
	st, err := wsstate.Store{Home: f.s.Home, Published: f.s.Workspaces}.Load(id)
	must(f.t, err)
	return st
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	raw, _ := os.ReadFile(p)
	return string(raw)
}

func gitStatus(t *testing.T, ws string) string {
	t.Helper()
	out, _ := exec.Command("git", "-C", ws, "status", "--porcelain", "--untracked-files=all").Output()
	return strings.TrimSpace(string(out))
}

func TestProjectFiles(t *testing.T) {
	f := newFixture(t)
	ws := f.workspace("api")
	f.keys(map[string]string{"MCP_LINEAR_TOKEN": "s3cret"})
	f.settings(config.MCP{Servers: linear, Workspaces: map[string][]string{"context7": {"api"}, "linear": {"api"}}})
	f.sync()
	mcpJSON := readFile(t, filepath.Join(ws, ".mcp.json"))
	for _, want := range []string{`"context7"`, `"linear"`, `"Bearer ${MCP_LINEAR_TOKEN}"`} {
		if !strings.Contains(mcpJSON, want) {
			t.Errorf(".mcp.json lacks %s:\n%s", want, mcpJSON)
		}
	}
	if strings.Contains(mcpJSON, "playwright") {
		t.Error("a server not attached to the workspace was written")
	}
	if got := readFile(t, filepath.Join(ws, "opencode.json")); !strings.Contains(got, `"Bearer {env:MCP_LINEAR_TOKEN}"`) {
		t.Errorf("opencode.json = %s", got)
	}
	if got := readFile(t, filepath.Join(ws, ".c", "config.toml")); !strings.Contains(got, `bearer_token_env_var = "MCP_LINEAR_TOKEN"`) {
		t.Errorf(".c/config.toml = %s", got)
	}
	for _, p := range []string{".mcp.json", "opencode.json", ".c/config.toml"} {
		if strings.Contains(readFile(t, filepath.Join(ws, p)), "s3cret") {
			t.Errorf("%s holds the token", p)
		}
	}
	if s := gitStatus(t, ws); s != "" {
		t.Errorf("created files are not hidden from git: %s", s)
	}
	st := f.wsState("api").MCP
	if fl := st.Files[".mcp.json"]; !fl.Created || fl.Tracked || !slices.Equal(fl.IDs, []string{"context7", "linear"}) {
		t.Errorf("state = %+v", fl)
	}
	// The user-level file still carries the token's value.
	if !strings.Contains(f.read(aFile), "Bearer s3cret") {
		t.Error("the user-level registration changed")
	}

	// Detaching both removes the files Berth created and the folder it made.
	f.settings(config.MCP{Servers: linear})
	f.sync()
	for _, p := range []string{".mcp.json", "opencode.json", ".c"} {
		if _, err := os.Lstat(filepath.Join(ws, p)); err == nil {
			t.Errorf("%s left behind", p)
		}
	}
	if strings.Contains(readFile(t, filepath.Join(ws, ".git", "info", "exclude")), wsstate.BlockStart) {
		t.Error("exclude block left behind")
	}
}

func TestTrackedFileRestored(t *testing.T) {
	f := newFixture(t)
	ws := f.workspace("api")
	original := "{\n    \"mcpServers\": {\n        \"mine\": { \"command\": \"x\" }\n    }\n}\n"
	os.WriteFile(filepath.Join(ws, ".mcp.json"), []byte(original), 0o644)
	exec.Command("git", "-C", ws, "add", ".mcp.json").Run()
	exec.Command("git", "-C", ws, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-qm", "x").Run()
	f.settings(config.MCP{Workspaces: map[string][]string{"context7": {"api"}}})
	f.sync()
	got := readFile(t, filepath.Join(ws, ".mcp.json"))
	if !strings.Contains(got, `"mine"`) || !strings.Contains(got, `"context7"`) {
		t.Fatalf("merged = %s", got)
	}
	if fl := f.wsState("api").MCP.Files[".mcp.json"]; fl.Created || !fl.Tracked {
		t.Errorf("state = %+v", fl)
	}
	if strings.Contains(readFile(t, filepath.Join(ws, ".git", "info", "exclude")), ".mcp.json") {
		t.Error("a tracked file was excluded")
	}
	f.settings(config.MCP{})
	f.sync()
	if got := readFile(t, filepath.Join(ws, ".mcp.json")); got != original {
		t.Errorf("not restored:\n%s", got)
	}
	if s := gitStatus(t, ws); s != "" {
		t.Errorf("git status = %s", s)
	}
}

func TestUntrackedUserFileMerged(t *testing.T) {
	f := newFixture(t)
	ws := f.workspace("api")
	os.WriteFile(filepath.Join(ws, "opencode.json"), []byte(`{"model":"m"}`), 0o644)
	f.settings(config.MCP{Workspaces: map[string][]string{"context7": {"api"}}})
	f.sync()
	if got := readFile(t, filepath.Join(ws, "opencode.json")); !strings.Contains(got, `"model": "m"`) || !strings.Contains(got, `"context7"`) {
		t.Errorf("opencode.json = %s", got)
	}
	if fl := f.wsState("api").MCP.Files["opencode.json"]; fl.Created || fl.Tracked {
		t.Errorf("state = %+v", fl)
	}
	if strings.Contains(readFile(t, filepath.Join(ws, ".git", "info", "exclude")), "opencode.json") {
		t.Error("the user's file was excluded")
	}
	f.settings(config.MCP{})
	f.sync()
	if got := readFile(t, filepath.Join(ws, "opencode.json")); got != `{"model":"m"}` {
		t.Errorf("after detach = %q", got)
	}
}

func TestProjectUserServerKept(t *testing.T) {
	f := newFixture(t)
	ws := f.workspace("api")
	os.WriteFile(filepath.Join(ws, ".mcp.json"), []byte(`{"mcpServers":{"context7":{"command":"mine"}}}`), 0o644)
	f.settings(config.MCP{Workspaces: map[string][]string{"context7": {"api"}}})
	f.sync()
	if got := readFile(t, filepath.Join(ws, ".mcp.json")); !strings.Contains(got, `"mine"`) {
		t.Errorf("the user's server was replaced: %s", got)
	}
	if _, ok := f.wsState("api").MCP.Files[".mcp.json"]; ok {
		t.Error("Berth recorded a file it owns nothing in")
	}
}

func TestProjectFileSymlinkIsSkipped(t *testing.T) {
	f := newFixture(t)
	ws := f.workspace("api")
	f.write(".claude.json", `{"mcpServers":{}}`)
	os.Symlink(f.path(".claude.json"), filepath.Join(ws, ".mcp.json"))
	f.settings(config.MCP{Workspaces: map[string][]string{"context7": {"api"}}})
	f.sync()
	if got := f.read(".claude.json"); got != `{"mcpServers":{}}` {
		t.Errorf("wrote through the link: %s", got)
	}
	if f.wsState("api").MCP.Skipped[".mcp.json"] == "" {
		t.Error("the skipped file is not reported")
	}
}

func TestDisabledAgentLosesProjectEntries(t *testing.T) {
	f := newFixture(t)
	ws := f.workspace("api")
	f.settings(config.MCP{Workspaces: map[string][]string{"context7": {"api"}}})
	f.sync()
	f.disable("a-cli")
	f.sync()
	if _, err := os.Lstat(filepath.Join(ws, ".mcp.json")); err == nil {
		t.Error("a disabled agent's project file stayed")
	}
	if _, err := os.Lstat(filepath.Join(ws, "opencode.json")); err != nil {
		t.Error("another agent's file went too")
	}
}

func TestForgottenWorkspaceReleasesProjectFiles(t *testing.T) {
	f := newFixture(t)
	ws := f.workspace("api")
	f.settings(config.MCP{Workspaces: map[string][]string{"context7": {"api"}}})
	f.sync()
	f.publish(map[string]string{})
	f.settings(config.MCP{})
	f.sync()
	if _, err := os.Lstat(filepath.Join(ws, ".mcp.json")); err == nil {
		t.Error("a forgotten workspace kept Berth's file")
	}
	if ids, _ := (wsstate.Store{Home: f.s.Home}).IDs(); len(ids) != 0 {
		t.Errorf("state left: %v", ids)
	}
}

func TestMissingWorkspaceFailsAlone(t *testing.T) {
	f := newFixture(t)
	f.publish(map[string]string{"gone": filepath.Join(t.TempDir(), "gone")})
	f.settings(config.MCP{Workspaces: map[string][]string{"context7": {"gone"}}})
	f.load()
	err := f.s.Run()
	if err == nil || !strings.Contains(err.Error(), "workspace gone") {
		t.Errorf("err = %v", err)
	}
	if !slices.Contains(f.servers(aFile, "mcpServers"), "context7") {
		t.Error("the user-level files did not converge")
	}
}

func TestCodexTrustsTheWorkspace(t *testing.T) {
	f := newFixture(t)
	ws := f.workspace("api")
	header := `[projects."` + ws + `"]`
	f.settings(config.MCP{Workspaces: map[string][]string{"context7": {"api"}}})
	f.sync()
	got := f.read(cFile)
	start, end := strings.Index(got, blockStart), strings.Index(got, blockEnd)
	if i := strings.Index(got, header); i < start || i > end || !strings.Contains(got, `trust_level = "trusted"`) {
		t.Fatalf("trust not in Berth's block:\n%s", got)
	}
	if !f.wsState("api").MCP.Trust {
		t.Error("the report does not say Codex trusts the folder")
	}
	f.settings(config.MCP{})
	f.sync()
	if strings.Contains(f.read(cFile), header) {
		t.Errorf("trust kept after detach:\n%s", f.read(cFile))
	}
}

func TestCodexTrustLeavesUserTables(t *testing.T) {
	f := newFixture(t)
	ws := f.workspace("api")
	header := `[projects."` + ws + `"]`
	f.write(cFile, header+"\ntrust_level = \"untrusted\"\n")
	f.settings(config.MCP{Workspaces: map[string][]string{"context7": {"api"}}})
	f.sync()
	if got := f.read(cFile); strings.Count(got, header) != 1 || !strings.Contains(got, `"untrusted"`) {
		t.Errorf("the user's choice was overridden:\n%s", got)
	}
}

func TestCodexTableInsideBlockIsKept(t *testing.T) {
	// Codex appends its own tables where it likes, sometimes inside Berth's block.
	in := blockStart + "\n[mcp_servers.context7]\ncommand = \"x\"\n[projects.\"/elsewhere\"]\ntrust_level = \"trusted\"\n" + blockEnd + "\n"
	out, _, trusted, err := editCodex([]byte(in), nil, []string{"context7"}, nil, nil)
	if err != nil || !strings.Contains(string(out), `[projects."/elsewhere"]`) || strings.Contains(string(out), blockStart) || len(trusted) != 0 {
		t.Errorf("out = %s, trusted = %v, %v", out, trusted, err)
	}
}

func TestOldStateFileIsRead(t *testing.T) {
	f := newFixture(t)
	f.write(".local/state/berth/mcp.json", `{"`+f.path(aFile)+`":["context7"]}`)
	f.write(aFile, `{"mcpServers":{"context7":{"command":"old"}}}`)
	f.sync()
	if strings.Contains(f.read(aFile), `"old"`) {
		t.Error("ownership from the old state was lost")
	}
	if got := f.read(".local/state/berth/mcp.json"); !strings.Contains(got, `"schema": 2`) {
		t.Errorf("state = %s", got)
	}
}

func TestMissingWorkspaceWithoutServersIsQuiet(t *testing.T) {
	f := newFixture(t)
	f.publish(map[string]string{"gone": filepath.Join(t.TempDir(), "gone")})
	f.settings(config.MCP{})
	f.load()
	if err := f.s.Run(); err != nil {
		t.Errorf("a missing folder with nothing attached failed the sync: %v", err)
	}
	if ids, _ := (wsstate.Store{Home: f.s.Home}).IDs(); len(ids) != 0 {
		t.Errorf("state files: %v", ids)
	}
}
