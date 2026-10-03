package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMCPFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	m := MCP{
		Servers:   map[string]MCPServer{"linear": {URL: "https://mcp.example.com/mcp", Auth: AuthBearer, Enabled: true}},
		Available: map[string][]string{"playwright": {"a-cli"}},
	}
	if err := WriteMCPFile(path, m, -1, -1); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `"schema": 1`) {
		t.Errorf("file = %s", raw)
	}
	got, err := ReadMCPFile(path)
	if err != nil || got.Servers["linear"] != m.Servers["linear"] || got.Available["playwright"][0] != "a-cli" {
		t.Errorf("read = %+v, %v", got, err)
	}
}

func TestReadMCPFileMissingIsDefaults(t *testing.T) {
	m, err := ReadMCPFile(filepath.Join(t.TempDir(), "none.json"))
	if err != nil || m.Servers == nil || m.Available == nil {
		t.Errorf("m = %+v, %v", m, err)
	}
}

func TestReadMCPFileNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte(`{"schema":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadMCPFile(path); err == nil || !strings.Contains(err.Error(), "schema 2") {
		t.Errorf("err = %v", err)
	}
}

func TestMCPTokenName(t *testing.T) {
	if got := MCPTokenName("my-server-2"); got != "MCP_MY_SERVER_2_TOKEN" {
		t.Errorf("got %s", got)
	}
	if n := len(MCPTokenName(strings.Repeat("a", 48))); n > 64 {
		t.Errorf("longest token name is %d characters", n)
	}
}
