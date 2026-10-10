package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// WorkspacesFileSchema is the schema of workspaces.json, which tells
// berth-term and the agent side where each workspace lives: they run as
// agent, which cannot read config.json.
const WorkspacesFileSchema = 1

// PublishedWorkspace is one workspace as workspaces.json carries it.
type PublishedWorkspace struct {
	Path string `json:"path"` // absolute
}

type workspacesFile struct {
	Schema     int                           `json:"schema"`
	Workspaces map[string]PublishedWorkspace `json:"workspaces"`
}

// PublishWorkspaces maps each workspace to its absolute folder under root.
func PublishWorkspaces(root string, ws map[string]Workspace) map[string]PublishedWorkspace {
	out := make(map[string]PublishedWorkspace, len(ws))
	for id, w := range ws {
		out[id] = PublishedWorkspace{Path: filepath.Join(root, filepath.FromSlash(w.Path))}
	}
	return out
}

// WriteWorkspacesFile publishes ws for the agent side; mode 0640, owned by
// uid:gid (-1 keeps it).
func WriteWorkspacesFile(path string, ws map[string]PublishedWorkspace, uid, gid int) error {
	if ws == nil {
		ws = map[string]PublishedWorkspace{}
	}
	raw, err := json.MarshalIndent(workspacesFile{Schema: WorkspacesFileSchema, Workspaces: ws}, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, append(raw, '\n'), 0o640, uid, gid)
}

// ReadWorkspacesFile reads workspaces.json; a missing file holds no workspaces.
func ReadWorkspacesFile(path string) (map[string]PublishedWorkspace, error) {
	out := map[string]PublishedWorkspace{}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	var f workspacesFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return out, fmt.Errorf("%s: %w", path, err)
	}
	if f.Schema > WorkspacesFileSchema {
		return out, fmt.Errorf("%s has schema %d; this Berth reads schema %d", path, f.Schema, WorkspacesFileSchema)
	}
	if f.Workspaces != nil {
		out = f.Workspaces
	}
	return out, nil
}
