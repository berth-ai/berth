package mcpsync

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/berth-ai/berth/internal/agentlayer"
	"github.com/berth-ai/berth/internal/agentset"
	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/testutil"
)

const fixturePkg = `{
  "dependencies": {"a-cli": "1.0.0", "c-cli": "1.0.0", "o-cli": "1.0.0", "p-mcp": "1.0.0", "k-mcp": "1.0.0"},
  "berth": {
    "a-cli": {"kind": "agent", "name": "A", "keys": []},
    "c-cli": {"kind": "agent", "name": "C", "keys": []},
    "o-cli": {"kind": "agent", "name": "O", "keys": []},
    "p-mcp": {"kind": "mcp", "name": "P", "keys": []},
    "k-mcp": {"kind": "mcp", "name": "K", "keys": []}
  },
  "berthAgents": {
    "schema": 1,
    "agents": {
      "a-cli": {"mcp": {"file": "~/.a/settings.json", "style": "claude", "key": "mcpServers"}},
      "c-cli": {"mcp": {"file": "~/.c/config.toml", "style": "codex-toml"}},
      "o-cli": {"mcp": {"file": "$XDG_CONFIG_HOME/o/o.json", "style": "opencode", "key": "mcp"}}
    },
    "servers": {
      "p-mcp": {"id": "playwright", "bin": "p-mcp", "args": ["--headless"]},
      "k-mcp": {"id": "context7", "bin": "k-mcp"}
    }
  }
}`

var fixtureDeps = []string{"a-cli", "c-cli", "o-cli", "p-mcp", "k-mcp"}

type fixture struct {
	t         *testing.T
	s         Syncer
	log       *bytes.Buffer
	cur       string
	addedRoot string
	added     []agentset.Added
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func newFixture(t *testing.T) *fixture {
	root := t.TempDir()
	cur := filepath.Join(root, "current")
	for _, dep := range fixtureDeps {
		dir := filepath.Join(cur, "node_modules", dep)
		must(t, os.MkdirAll(dir, 0o755))
		must(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"`+dep+`","bin":{"`+dep+`":"cli.js"}}`), 0o644))
	}
	must(t, os.WriteFile(filepath.Join(cur, "package.json"), []byte(fixturePkg), 0o644))
	// The fixture's agents stand in for the ones berth-mcp-setup managed.
	saved := legacyAgents
	legacyAgents = []string{"a-cli", "c-cli", "o-cli"}
	t.Cleanup(func() { legacyAgents = saved })
	f := &fixture{t: t, log: &bytes.Buffer{}, cur: cur}
	f.s = Syncer{Layer: filepath.Join(root, "bin"), Home: filepath.Join(root, "home"), Log: f.log,
		Settings: filepath.Join(root, "mcp.json"), Keys: filepath.Join(root, "agents.env")}
	must(t, os.MkdirAll(f.s.Layer, 0o755))
	must(t, os.MkdirAll(f.s.Home, 0o755))
	for _, bin := range fixtureDeps {
		f.enable(bin)
	}
	f.load()
	return f
}

func (f *fixture) enable(bin string) {
	must(f.t, os.WriteFile(filepath.Join(f.s.Layer, bin), []byte("#!/bin/sh\n"), 0o755))
}
func (f *fixture) disable(bin string)     { must(f.t, os.Remove(filepath.Join(f.s.Layer, bin))) }
func (f *fixture) path(rel string) string { return filepath.Join(f.s.Home, rel) }

func (f *fixture) write(rel, content string) {
	must(f.t, os.MkdirAll(filepath.Dir(f.path(rel)), 0o755))
	must(f.t, os.WriteFile(f.path(rel), []byte(content), 0o644))
}

func (f *fixture) read(rel string) string {
	data, err := os.ReadFile(f.path(rel))
	must(f.t, err)
	return string(data)
}

// servers returns the member names under key of a JSON config, in order.
func (f *fixture) servers(rel, key string) []string {
	keys, err := objectKeys([]byte(f.read(rel)), key)
	must(f.t, err)
	return keys
}

// load reads the set as it is on disk now; tests that call Run directly
// use the set newFixture loaded.
func (f *fixture) load() {
	f.t.Helper()
	set, err := agentset.Load(&agentset.Locked{Dir: f.cur, Link: f.cur}, f.addedRoot, f.added)
	must(f.t, err)
	f.s.Set = set
}

// sync loads the set as it is on disk now, then converges it.
func (f *fixture) sync() {
	f.t.Helper()
	f.load()
	must(f.t, f.s.Run())
}

func (f *fixture) state() map[string][]string {
	var st map[string][]string
	data, err := os.ReadFile(StatePath(f.s.Home))
	must(f.t, err)
	must(f.t, json.Unmarshal(data, &st))
	return st
}

const (
	aFile = ".a/settings.json"
	cFile = ".c/config.toml"
	oFile = ".config/o/o.json"
)

func TestRegistersWithEveryAgent(t *testing.T) {
	f := newFixture(t)
	f.sync()
	if got := f.servers(aFile, "mcpServers"); !slices.Equal(got, []string{"context7", "playwright"}) {
		t.Errorf("A servers = %v", got)
	}
	if !strings.Contains(f.read(aFile), `"command": "`+filepath.Join(f.s.Layer, "p-mcp")+`"`) {
		t.Errorf("A config:\n%s", f.read(aFile))
	}
	if got := f.servers(oFile, "mcp"); !slices.Equal(got, []string{"context7", "playwright"}) {
		t.Errorf("O servers = %v", got)
	}
	toml := f.read(cFile)
	for _, want := range []string{blockStart, "[mcp_servers.context7]", "[mcp_servers.playwright]", `args = ["--headless"]`} {
		if !strings.Contains(toml, want) {
			t.Errorf("C config lacks %q:\n%s", want, toml)
		}
	}
	for _, rel := range []string{aFile, cFile, oFile} {
		fi, err := os.Stat(f.path(rel))
		must(t, err)
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v, want 0600", rel, fi.Mode().Perm())
		}
	}
	st := f.state()
	if !slices.Equal(st[f.path(aFile)], []string{"context7", "playwright"}) || len(st) != 3 {
		t.Errorf("state = %v", st)
	}
	if !strings.Contains(f.log.String(), "berth mcp sync: converged MCP servers for A") {
		t.Errorf("log = %s", f.log)
	}
}

func TestDisabledServerIsRemovedEverywhere(t *testing.T) {
	f := newFixture(t)
	f.sync()
	f.disable("k-mcp")
	f.sync()
	if got := f.servers(aFile, "mcpServers"); !slices.Equal(got, []string{"playwright"}) {
		t.Errorf("A servers = %v", got)
	}
	if got := f.servers(oFile, "mcp"); !slices.Equal(got, []string{"playwright"}) {
		t.Errorf("O servers = %v", got)
	}
	if strings.Contains(f.read(cFile), "context7") {
		t.Errorf("C still has context7:\n%s", f.read(cFile))
	}
	if got := f.state()[f.path(aFile)]; !slices.Equal(got, []string{"playwright"}) {
		t.Errorf("state = %v", got)
	}
}

func TestUserEntriesSurvive(t *testing.T) {
	f := newFixture(t)
	f.write(aFile, `{"theme":"dark","mcpServers":{"mine":{"command":"m"}}}`)
	f.sync()
	f.disable("k-mcp")
	f.disable("p-mcp")
	f.sync()
	if got := f.servers(aFile, "mcpServers"); !slices.Equal(got, []string{"mine"}) {
		t.Errorf("A servers = %v", got)
	}
	if !strings.HasPrefix(f.read(aFile), "{\n  \"theme\": \"dark\",") {
		t.Errorf("A config:\n%s", f.read(aFile))
	}
}

func TestUserServerWithBerthIDIsSkipped(t *testing.T) {
	f := newFixture(t)
	// Berth has synced this file before (with nothing owned) when the user
	// adds their own playwright.
	must(t, os.MkdirAll(filepath.Dir(StatePath(f.s.Home)), 0o755))
	st, _ := json.Marshal(map[string][]string{f.path(aFile): {}})
	must(t, os.WriteFile(StatePath(f.s.Home), st, 0o600))
	f.write(aFile, `{"mcpServers":{"playwright":{"command":"my-own"}}}`)
	f.sync()
	if !strings.Contains(f.read(aFile), `"command": "my-own"`) {
		t.Errorf("user playwright overwritten:\n%s", f.read(aFile))
	}
	if got := f.state()[f.path(aFile)]; !slices.Equal(got, []string{"context7"}) {
		t.Errorf("owned = %v, want only context7", got)
	}
	if !strings.Contains(f.log.String(), "playwright") {
		t.Errorf("no warning logged: %s", f.log)
	}
	f.disable("p-mcp")
	f.sync()
	if !strings.Contains(f.read(aFile), `"command": "my-own"`) {
		t.Errorf("user playwright removed:\n%s", f.read(aFile))
	}
}

func TestSeedsLegacyOwnership(t *testing.T) {
	f := newFixture(t)
	f.write(aFile, `{"mcpServers":{"playwright":{"type":"stdio","command":"/old","args":[],"env":{}},"context7":{"type":"stdio","command":"/old","args":[],"env":{}}}}`)
	f.write(cFile, "[mcp_servers.context7]\ncommand = \"/old\"\nargs = []\n")
	f.disable("k-mcp")
	f.sync()
	if got := f.servers(aFile, "mcpServers"); !slices.Equal(got, []string{"playwright"}) {
		t.Errorf("A servers = %v", got)
	}
	if strings.Contains(f.read(aFile), "/old") || strings.Contains(f.read(cFile), "/old") {
		t.Errorf("legacy entries left:\n%s\n%s", f.read(aFile), f.read(cFile))
	}
}

func TestLegacyOwnershipSurvivesDisabledAgent(t *testing.T) {
	f := newFixture(t)
	f.write(aFile, `{"mcpServers":{"context7":{"command":"/old"}}}`)
	f.disable("a-cli")
	f.sync()
	if !strings.Contains(f.read(aFile), "/old") {
		t.Fatal("a disabled agent's file was touched")
	}
	f.enable("a-cli")
	f.disable("k-mcp")
	f.sync()
	if got := f.servers(aFile, "mcpServers"); !slices.Equal(got, []string{"playwright"}) {
		t.Errorf("A servers = %v, want the legacy context7 removed", got)
	}
}

func TestNoWriteWhenUnchanged(t *testing.T) {
	f := newFixture(t)
	f.sync()
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	for _, rel := range []string{aFile, cFile, oFile} {
		must(t, os.Chtimes(f.path(rel), past, past))
	}
	f.sync()
	for _, rel := range []string{aFile, cFile, oFile} {
		fi, err := os.Stat(f.path(rel))
		must(t, err)
		if !fi.ModTime().Equal(past) {
			t.Errorf("%s rewritten", rel)
		}
	}
}

func TestSymlinkAndModeKept(t *testing.T) {
	f := newFixture(t)
	target := filepath.Join(t.TempDir(), "dotfiles-settings.json")
	must(t, os.WriteFile(target, []byte("{}\n"), 0o640))
	must(t, os.MkdirAll(filepath.Dir(f.path(aFile)), 0o755))
	must(t, os.Symlink(target, f.path(aFile)))
	f.sync()
	fi, err := os.Lstat(f.path(aFile))
	must(t, err)
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink was replaced")
	}
	tfi, err := os.Stat(target)
	must(t, err)
	if tfi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want 0640", tfi.Mode().Perm())
	}
	if !strings.Contains(f.read(aFile), "playwright") {
		t.Errorf("target not converged:\n%s", f.read(aFile))
	}
}

func TestDanglingSymlinkFails(t *testing.T) {
	f := newFixture(t)
	must(t, os.MkdirAll(filepath.Dir(f.path(aFile)), 0o755))
	must(t, os.Symlink(filepath.Join(t.TempDir(), "missing.json"), f.path(aFile)))
	err := f.s.Run()
	if err == nil || !strings.Contains(err.Error(), "A") {
		t.Fatalf("err = %v, want A to fail", err)
	}
	fi, lerr := os.Lstat(f.path(aFile))
	must(t, lerr)
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Error("the dangling symlink was replaced")
	}
}

func TestNotPlainJSONIsLeftAloneAndOthersConverge(t *testing.T) {
	f := newFixture(t)
	bad := "{\"mcp\": {}, // c\n}"
	f.write(oFile, bad)
	err := f.s.Run()
	if err == nil || !strings.Contains(err.Error(), "O") {
		t.Fatalf("err = %v, want O to fail", err)
	}
	if f.read(oFile) != bad {
		t.Error("the JSONC file was rewritten")
	}
	if !strings.Contains(f.log.String(), "is not plain JSON; leaving it unchanged") {
		t.Errorf("log = %s", f.log)
	}
	if got := f.servers(aFile, "mcpServers"); len(got) != 2 {
		t.Errorf("A not converged: %v", got)
	}
}

func TestDisabledAgentIsSkipped(t *testing.T) {
	f := newFixture(t)
	f.disable("a-cli")
	f.sync()
	if _, err := os.Stat(f.path(aFile)); !os.IsNotExist(err) {
		t.Errorf("a disabled agent's config was created: %v", err)
	}
	if !strings.Contains(f.log.String(), "A not enabled; skipping") {
		t.Errorf("log = %s", f.log)
	}
}

func TestRetriesWhenFileChangesDuringEdit(t *testing.T) {
	f := newFixture(t)
	f.write(aFile, "{}\n")
	calls := 0
	target, err := filepath.EvalSymlinks(f.path(aFile))
	must(t, err)
	f.s.beforeRename = func(path string) {
		if path == target && calls == 0 {
			must(t, os.WriteFile(path, []byte(`{"session":1}`), 0o644))
		}
		calls++
	}
	f.sync()
	got := f.read(aFile)
	if !strings.Contains(got, `"session": 1`) || !strings.Contains(got, "playwright") {
		t.Errorf("merge lost a side:\n%s", got)
	}
}

func TestGivesUpWhenFileKeepsChanging(t *testing.T) {
	f := newFixture(t)
	f.write(aFile, "{}\n")
	n := 0
	target, err := filepath.EvalSymlinks(f.path(aFile))
	must(t, err)
	f.s.beforeRename = func(path string) {
		if path == target {
			n++
			must(t, os.WriteFile(path, []byte(`{"session":`+strings.Repeat("1", n)+`}`), 0o644))
		}
	}
	if err := f.s.Run(); err == nil || !strings.Contains(err.Error(), "A") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(f.log.String(), "kept changing") {
		t.Errorf("log = %s", f.log)
	}
}

func TestCorruptStateStartsOver(t *testing.T) {
	f := newFixture(t)
	must(t, os.MkdirAll(filepath.Dir(StatePath(f.s.Home)), 0o755))
	must(t, os.WriteFile(StatePath(f.s.Home), []byte("not json"), 0o600))
	f.sync()
	if got := f.servers(aFile, "mcpServers"); len(got) != 2 {
		t.Errorf("A servers = %v", got)
	}
	if len(f.state()) != 3 {
		t.Errorf("state not rewritten: %v", f.state())
	}
}

func TestConfigHome(t *testing.T) {
	f := newFixture(t)
	f.s.ConfigHome = filepath.Join(t.TempDir(), "xdg")
	f.sync()
	if _, err := os.Stat(filepath.Join(f.s.ConfigHome, "o", "o.json")); err != nil {
		t.Errorf("XDG_CONFIG_HOME ignored: %v", err)
	}
}

func TestLockWithoutDescriptorsKeepsLegacyOwnership(t *testing.T) {
	f := newFixture(t)
	f.write(aFile, `{"mcpServers":{"context7":{"command":"/old"}}}`)
	pkg := filepath.Join(f.cur, "package.json")
	var p map[string]any
	must(t, json.Unmarshal([]byte(fixturePkg), &p))
	delete(p, "berthAgents")
	old, _ := json.Marshal(p)
	must(t, os.WriteFile(pkg, old, 0o644))
	f.sync()
	must(t, os.WriteFile(pkg, []byte(fixturePkg), 0o644))
	f.disable("k-mcp")
	f.sync()
	if got := f.servers(aFile, "mcpServers"); !slices.Equal(got, []string{"playwright"}) {
		t.Errorf("A servers = %v, want the legacy context7 removed", got)
	}
}

func TestOnlyLegacyAgentsAreSeeded(t *testing.T) {
	f := newFixture(t)
	saved := legacyAgents
	legacyAgents = []string{"c-cli", "o-cli"} // a-cli plays a new agent, like Gemini
	t.Cleanup(func() { legacyAgents = saved })
	f.write(aFile, `{"mcpServers":{"playwright":{"command":"my-own"}}}`)
	f.sync()
	if !strings.Contains(f.read(aFile), `"command": "my-own"`) {
		t.Errorf("a new agent's user server was claimed:\n%s", f.read(aFile))
	}
}

func (f *fixture) settings(m config.MCP) {
	if m.Servers == nil {
		m.Servers = map[string]config.MCPServer{}
	}
	if m.Available == nil {
		m.Available = map[string][]string{}
	}
	must(f.t, config.WriteMCPFile(f.s.Settings, m, -1, -1))
}

func (f *fixture) keys(k map[string]string) {
	must(f.t, agentlayer.WriteKeys(f.s.Keys, k, -1, -1))
}

var linear = map[string]config.MCPServer{
	"linear": {URL: "https://mcp.example.com/mcp", Auth: config.AuthBearer, Enabled: true},
}

func TestRemoteServerReachesEveryAgent(t *testing.T) {
	f := newFixture(t)
	f.settings(config.MCP{Servers: linear})
	f.keys(map[string]string{"MCP_LINEAR_TOKEN": "tok"})
	f.sync()
	if got := f.servers(aFile, "mcpServers"); !slices.Equal(got, []string{"context7", "linear", "playwright"}) {
		t.Errorf("A servers = %v", got)
	}
	for rel, want := range map[string]string{
		aFile: `"Authorization": "Bearer tok"`,
		oFile: `"type": "remote"`,
		cFile: `http_headers = { "Authorization" = "Bearer tok" }`,
	} {
		if !strings.Contains(f.read(rel), want) {
			t.Errorf("%s lacks %q:\n%s", rel, want, f.read(rel))
		}
	}
	for _, rel := range []string{aFile, oFile, cFile} {
		if fi, _ := os.Stat(f.path(rel)); fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v", rel, fi.Mode().Perm())
		}
	}
}

func TestBearerWithoutTokenHasNoHeaders(t *testing.T) {
	f := newFixture(t)
	f.settings(config.MCP{Servers: linear})
	f.sync()
	if strings.Contains(f.read(aFile), "Authorization") || !strings.Contains(f.read(aFile), `"url": "https://mcp.example.com/mcp"`) {
		t.Errorf("A config:\n%s", f.read(aFile))
	}
}

func TestTokenIgnoredUnlessBearer(t *testing.T) {
	f := newFixture(t)
	f.settings(config.MCP{Servers: map[string]config.MCPServer{
		"linear": {URL: "https://mcp.example.com/mcp", Auth: config.AuthOAuth, Enabled: true},
	}})
	f.keys(map[string]string{"MCP_LINEAR_TOKEN": "tok"})
	f.sync()
	if strings.Contains(f.read(aFile), "Authorization") {
		t.Errorf("A config:\n%s", f.read(aFile))
	}
}

func TestAvailableNarrowsAndRemoves(t *testing.T) {
	f := newFixture(t)
	f.settings(config.MCP{Servers: linear})
	f.sync()
	f.settings(config.MCP{Servers: linear, Available: map[string][]string{
		"playwright": {"a-cli"},
		"linear":     {"c-cli", "gone-cli"},
	}})
	f.sync()
	if got := f.servers(aFile, "mcpServers"); !slices.Equal(got, []string{"context7", "playwright"}) {
		t.Errorf("A servers = %v", got)
	}
	if got := f.servers(oFile, "mcp"); !slices.Equal(got, []string{"context7"}) {
		t.Errorf("O servers = %v", got)
	}
	toml := f.read(cFile)
	if strings.Contains(toml, "mcp_servers.playwright") || !strings.Contains(toml, "mcp_servers.linear") {
		t.Errorf("C config:\n%s", toml)
	}
}

func TestOnlyUninstalledAgentsMeansNowhere(t *testing.T) {
	f := newFixture(t)
	f.settings(config.MCP{Servers: linear, Available: map[string][]string{"linear": {"gone-cli"}}})
	f.sync()
	if slices.Contains(f.servers(aFile, "mcpServers"), "linear") {
		t.Error("linear registered with A")
	}
}

func TestDisabledRemoteServerIsRemoved(t *testing.T) {
	f := newFixture(t)
	f.settings(config.MCP{Servers: linear})
	f.sync()
	off := linear["linear"]
	off.Enabled = false
	f.settings(config.MCP{Servers: map[string]config.MCPServer{"linear": off}})
	f.sync()
	if slices.Contains(f.servers(aFile, "mcpServers"), "linear") {
		t.Error("disabled linear still registered")
	}
}

func TestRemoteClashWithLockIDIsSkipped(t *testing.T) {
	f := newFixture(t)
	f.settings(config.MCP{Servers: map[string]config.MCPServer{
		"playwright": {URL: "https://mcp.example.com/mcp", Auth: config.AuthNone, Enabled: true},
	}})
	f.sync()
	if !strings.Contains(f.log.String(), "MCP server playwright clashes with a built-in server; skipping it") {
		t.Errorf("log = %s", f.log)
	}
	if strings.Contains(f.read(aFile), "mcp.example.com") {
		t.Errorf("remote replaced the lock server:\n%s", f.read(aFile))
	}
}

func TestNewerSettingsSchemaFails(t *testing.T) {
	f := newFixture(t)
	must(t, os.WriteFile(f.s.Settings, []byte(`{"schema":2}`), 0o644))
	if err := f.s.Run(); err == nil || !strings.Contains(err.Error(), "schema 2") {
		t.Errorf("err = %v", err)
	}
}

func TestRegistersWithAddedAgent(t *testing.T) {
	f := newFixture(t)
	f.addedRoot = t.TempDir()
	testutil.AddedInstall(t, f.addedRoot, "copilot", "0123456789ab", "@github/copilot", "1.0.91", testutil.CopilotPackageJSON, "copilot")
	f.added, _ = agentset.Installed(f.addedRoot)
	f.sync()
	if _, err := os.Stat(f.path(".copilot/mcp-config.json")); err == nil {
		t.Fatal("a switched-off added agent got servers")
	}
	f.enable("copilot")
	f.sync()
	if got := f.servers(".copilot/mcp-config.json", "mcpServers"); !slices.Equal(got, []string{"context7", "playwright"}) {
		t.Errorf("copilot servers = %v", got)
	}
}
