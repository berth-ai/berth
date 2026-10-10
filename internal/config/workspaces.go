package config

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// Workspace is a project folder under /workspaces that Berth cloned or
// adopted. Env and Instructions are used by a later phase; schema 5 holds
// them already so that phase needs no schema change.
type Workspace struct {
	Path         string                  `json:"path"` // relative to /workspaces
	Repo         string                  `json:"repo"` // the clone URL; empty for an adopted folder
	Env          map[string]WorkspaceEnv `json:"env"`
	Instructions Instructions            `json:"instructions"`
}

// WorkspaceEnv is one variable of a workspace: a plain value, or a secret
// whose value is stored outside the config.
type WorkspaceEnv struct {
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
}

// Instructions says whether a workspace's agent instruction files share AGENTS.md.
type Instructions struct {
	Shared bool `json:"shared"`
}

// TempPrefix starts the folder a clone is made in before it is renamed into
// place; no workspace path crosses one.
const TempPrefix = ".berth-tmp-"

const (
	maxEnvKeys  = 100
	maxEnvValue = 4 << 10
)

var (
	// WorkspaceIDPattern names a workspace; its tmux session is ws-<id>.
	WorkspaceIDPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	envKeyPattern       = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,127}$`)
	reservedEnv         = []string{"PATH", "HOME", "USER", "SHELL", "TERM"}
	reservedEnvPrefixes = []string{"TMUX", "LD_", "BERTH_", "GIT_", "SSH_"}
)

// CheckWorkspacePath returns why p cannot be a workspace folder, or "".
func CheckWorkspacePath(p string) string {
	if p == "" || !ValidSkillPath(p) {
		return "must be a folder inside /workspaces, such as acme/api"
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".git" || strings.HasPrefix(seg, TempPrefix) {
			return "must not be inside a .git or " + TempPrefix + "* folder"
		}
	}
	return ""
}

// PathsOverlap reports whether workspace paths a and b are the same folder
// or one holds the other.
func PathsOverlap(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

func validateWorkspaces(v *validator, ws map[string]Workspace) {
	ids := slices.Sorted(maps.Keys(ws))
	for i, id := range ids {
		w, field := ws[id], "workspaces."+id
		if !WorkspaceIDPattern.MatchString(id) {
			v.add(field, "%q is not a workspace id: use 1 to 64 lowercase letters, digits and dashes", id)
			continue
		}
		if msg := CheckWorkspacePath(w.Path); msg != "" {
			v.add(field+".path", "%s", msg)
		} else {
			for _, other := range ids[:i] {
				o := ws[other].Path
				if WorkspaceIDPattern.MatchString(other) && CheckWorkspacePath(o) == "" && PathsOverlap(w.Path, o) {
					v.add(field+".path", "overlaps workspace %s (%s); workspaces cannot share or nest folders", other, o)
					break
				}
			}
		}
		if w.Repo != "" {
			if msg := CheckSkillRepo(w.Repo); msg != "" {
				v.add(field+".repo", "%s", msg)
			}
		}
		validateEnv(v, field+".env", w.Env)
	}
}

func validateEnv(v *validator, field string, env map[string]WorkspaceEnv) {
	if len(env) > maxEnvKeys {
		v.add(field, "must hold at most %d variables", maxEnvKeys)
		return
	}
	for _, k := range slices.Sorted(maps.Keys(env)) {
		e, f := env[k], field+"."+k
		if msg := checkEnvKey(k); msg != "" {
			v.add(f, "%s", msg)
			continue
		}
		switch {
		case e.Secret && e.Value != "":
			v.add(f+".value", "must be empty for a secret; secrets are stored outside the config")
		case len(e.Value) > maxEnvValue:
			v.add(f+".value", "must be at most %d bytes", maxEnvValue)
		case !utf8.ValidString(e.Value):
			v.add(f+".value", "must be UTF-8 text")
		}
	}
}

func checkEnvKey(k string) string {
	if !envKeyPattern.MatchString(k) {
		return fmt.Sprintf("%q is not a variable name: use capital letters, digits and underscores", k)
	}
	if slices.Contains(reservedEnv, k) || slices.ContainsFunc(reservedEnvPrefixes, func(p string) bool { return strings.HasPrefix(k, p) }) {
		return k + " is reserved for Berth and the terminal"
	}
	return ""
}
