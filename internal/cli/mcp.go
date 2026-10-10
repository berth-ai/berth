package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/berth-ai/berth/internal/mcpsync"
	"github.com/berth-ai/berth/internal/output"
)

// runMCP is the hidden `berth mcp sync`, run as agent after every change to
// the agent set or the wrapper layer.
func runMCP(_ context.Context, a *app, args []string) error {
	if len(args) != 1 || args[0] != "sync" {
		return usageErr(fmt.Errorf("usage: berth mcp sync"))
	}
	home := a.cfg.Getenv("HOME")
	if home == "" {
		return output.Errorf(output.ExitFailure, "no_home", "mcp", "HOME is not set")
	}
	set, err := a.agentSet(home)
	if err != nil {
		return output.Errorf(output.ExitFailure, "internal", "mcp", "%v", err)
	}
	if len(set.Packages) == 0 {
		a.p.Logf("no agent set installed; nothing to register")
		return nil
	}
	s := mcpsync.Syncer{
		Set:        set,
		Layer:      filepath.Join(a.cfg.ConfigDir, "bin"),
		Home:       home,
		ConfigHome: a.cfg.Getenv("XDG_CONFIG_HOME"),
		DataHome:   a.cfg.Getenv("XDG_DATA_HOME"),
		Settings:   filepath.Join(a.cfg.ConfigDir, "mcp.json"),
		Workspaces: filepath.Join(a.cfg.ConfigDir, "workspaces.json"),
		Keys:       a.cfg.KeysPath,
		Log:        a.p.Stderr,
	}
	if err := s.Run(); err != nil {
		return output.Errorf(output.ExitFailure, "mcp_failed", "mcp", "%v", err)
	}
	return nil
}
