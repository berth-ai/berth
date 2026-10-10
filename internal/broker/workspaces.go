package broker

import (
	"cmp"
	"maps"

	"github.com/berth-ai/berth/internal/config"
)

func (b *Broker) workspacesRoot() string { return cmp.Or(b.WorkspacesRoot, "/workspaces") }

// writeWorkspacesSettings publishes where each workspace lives, readable
// by the agent group.
func (b *Broker) writeWorkspacesSettings(ws map[string]config.Workspace) error {
	if b.WorkspacesSettings == "" {
		return nil
	}
	uid, gid := -1, -1
	if b.Agent != nil {
		uid, gid = 0, int(b.Agent.GID)
	}
	return config.WriteWorkspacesFile(b.WorkspacesSettings, config.PublishWorkspaces(b.workspacesRoot(), ws), uid, gid)
}

// workspacesStale reports whether WorkspacesSettings differs from ws.
func (b *Broker) workspacesStale(ws map[string]config.Workspace) bool {
	if b.WorkspacesSettings == "" {
		return false
	}
	pub, err := config.ReadWorkspacesFile(b.WorkspacesSettings)
	return err != nil || !maps.Equal(pub, config.PublishWorkspaces(b.workspacesRoot(), ws))
}
