package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// MCPFileSchema is the schema of mcp.json, the copy of the mcp section that
// berth mcp sync reads: it runs as agent, which cannot read config.json.
// Schema 1 meant all agents for a server missing from available; schema 2
// means none, as config schema 7 does, so a schema 1 file's lists are
// dropped as Upgrade drops them.
const MCPFileSchema = 2

type mcpFile struct {
	Schema int `json:"schema"`
	MCP
}

// WriteMCPFile publishes m for the agent-side converger. It holds no
// secrets; mode 0640, owned by uid:gid (-1 keeps it).
func WriteMCPFile(path string, m MCP, uid, gid int) error {
	raw, err := json.MarshalIndent(mcpFile{Schema: MCPFileSchema, MCP: m}, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, append(raw, '\n'), 0o640, uid, gid)
}

// ReadMCPFile reads mcp.json; a missing file is the defaults.
func ReadMCPFile(path string) (MCP, error) {
	m := Defaults().MCP
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return m, err
	}
	var f mcpFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return m, fmt.Errorf("%s: %w", path, err)
	}
	if f.Schema > MCPFileSchema {
		return m, fmt.Errorf("%s has schema %d; this Berth reads schema %d", path, f.Schema, MCPFileSchema)
	}
	if f.Servers != nil {
		m.Servers = f.Servers
	}
	if f.Available != nil && f.Schema >= MCPFileSchema {
		m.Available = f.Available
	}
	if f.Workspaces != nil {
		m.Workspaces = f.Workspaces
	}
	return m, nil
}

// MCPTokenName is the agents.env key that holds a bearer server's token.
func MCPTokenName(id string) string {
	return "MCP_" + strings.ToUpper(strings.ReplaceAll(id, "-", "_")) + "_TOKEN"
}
