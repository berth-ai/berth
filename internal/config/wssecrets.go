package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// WorkspaceSecretsSchema is the schema of a workspace's secrets file.
const WorkspaceSecretsSchema = 1

// WorkspaceSecretPrefix starts the secrets-change name of a workspace
// variable: workspaces.<id>.<KEY>.
const WorkspaceSecretPrefix = "workspaces."

type workspaceSecrets struct {
	Schema int               `json:"schema"`
	Env    map[string]string `json:"env"`
}

// WorkspaceSecretName names key of workspace id in a secrets change.
func WorkspaceSecretName(id, key string) string { return WorkspaceSecretPrefix + id + "." + key }

// ParseWorkspaceSecretName splits a secrets-change name into a valid
// workspace id and variable name.
func ParseWorkspaceSecretName(name string) (string, string, bool) {
	rest, ok := strings.CutPrefix(name, WorkspaceSecretPrefix)
	if !ok {
		return "", "", false
	}
	id, key, ok := strings.Cut(rest, ".")
	if !ok || !WorkspaceIDPattern.MatchString(id) || checkEnvKey(key) != "" {
		return "", "", false
	}
	return id, key, true
}

func workspaceSecretsFile(dir, id string) string { return filepath.Join(dir, id+".json") }

// ReadWorkspaceSecrets reads workspace id's secrets; a missing file has none.
func ReadWorkspaceSecrets(dir, id string) (map[string]string, error) {
	out := map[string]string{}
	if !WorkspaceIDPattern.MatchString(id) {
		return out, fmt.Errorf("%q is not a workspace id", id)
	}
	p := workspaceSecretsFile(dir, id)
	raw, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	var f workspaceSecrets
	if err := json.Unmarshal(raw, &f); err != nil {
		return out, fmt.Errorf("%s: %w", p, err)
	}
	if f.Schema > WorkspaceSecretsSchema {
		return out, fmt.Errorf("%s has schema %d; this Berth reads schema %d", p, f.Schema, WorkspaceSecretsSchema)
	}
	for k, v := range f.Env {
		out[k] = v
	}
	return out, nil
}

// WriteWorkspaceSecrets replaces workspace id's secrets, owned by uid:gid
// (-1 keeps it), mode 0640; no secrets deletes the file. The folder is
// 0711: agent may open a file by name but never list them.
func WriteWorkspaceSecrets(dir, id string, env map[string]string, uid, gid int) error {
	if !WorkspaceIDPattern.MatchString(id) {
		return fmt.Errorf("%q is not a workspace id", id)
	}
	if len(env) == 0 {
		if err := os.Remove(workspaceSecretsFile(dir, id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	for k, v := range env {
		if checkEnvKey(k) != "" || CheckEnvValue(v) != "" {
			return fmt.Errorf("invalid workspace secret %s", k)
		}
	}
	if err := os.MkdirAll(dir, 0o711); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o711); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(workspaceSecrets{Schema: WorkspaceSecretsSchema, Env: env}, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(workspaceSecretsFile(dir, id), append(raw, '\n'), 0o640, uid, gid)
}

// WorkspaceSecretIDs lists, sorted, the workspaces with a secrets file.
func WorkspaceSecretIDs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if id, ok := strings.CutSuffix(e.Name(), ".json"); ok && WorkspaceIDPattern.MatchString(id) {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out, nil
}
