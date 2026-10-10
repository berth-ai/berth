package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	devUser     = "dev"
	devPassword = "dev password"
	devToken    = "dev-token"
)

type job struct {
	ID           int               `json:"id"`
	Verb         string            `json:"verb"`
	Group        string            `json:"group"`
	Args         []string          `json:"args"`
	State        string            `json:"state"`
	StartedAt    time.Time         `json:"started_at"`
	FinishedAt   *time.Time        `json:"finished_at"`
	ExitCode     *int              `json:"exit_code"`
	Result       json.RawMessage   `json:"result"`
	Log          string            `json:"log,omitempty"`
	LogTruncated bool              `json:"log_truncated"`
	Device       map[string]string `json:"device,omitempty"`
	Login        map[string]any    `json:"login,omitempty"`

	input     chan string   // agent.login: pasted codes
	cancel    chan struct{} // closed by login.cancel
	cancelled bool
}

// fake answers like the broker for everything the panel's browser side
// calls. Config is kept as decoded JSON so patches merge like the real
// store's.
type fake struct {
	mu          sync.Mutex
	mode        string
	user        string
	cfg         map[string]any
	prev        map[string]any
	keys        map[string]bool
	git         map[string]string
	forges      map[string]bool
	tools       []map[string]string
	jobs        []*job
	started     time.Time
	step        time.Duration
	signedIn    map[string]bool     // pkg + " " + method
	sessions    map[string][]string // workspace id → window names
	overwritten map[string]bool     // "<ws>/<source>/<name>" after Overwrite
}

func newFake(mode string) *fake {
	return &fake{
		signedIn:    map[string]bool{},
		sessions:    map[string][]string{},
		overwritten: map[string]bool{},
		mode:        mode, user: devUser, started: time.Now().UTC(), step: 400 * time.Millisecond,
		cfg:    defaultConfig(1),
		keys:   map[string]bool{"EXAMPLE_API_KEY": false, "COPILOT_GITHUB_TOKEN": false},
		git:    map[string]string{"name": "", "email": ""},
		forges: map[string]bool{"gh": false, "glab": false},
		tools:  []map[string]string{{"name": "terraform", "version": "1.1.0"}},
	}
}

func defaultConfig(rev int) map[string]any {
	return map[string]any{
		"schema": 6, "revision": rev,
		"frontends":  map[string]any{"terminal": map[string]any{"enabled": true}, "ide": map[string]any{"enabled": false}},
		"agents":     map[string]any{"ref": "main", "disabled": []any{}, "added": []any{}, "keep": 2, "boot_timeout": 600, "mcp_timeout": 300},
		"runtime":    map[string]any{"tz": "Etc/UTC"},
		"mcp":        map[string]any{"servers": map[string]any{}, "available": map[string]any{}, "workspaces": map[string]any{}},
		"skills":     map[string]any{"sources": map[string]any{}},
		"workspaces": map[string]any{},
	}
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	_ = json.NewDecoder(r.Body).Decode(&in)
	f.mu.Lock()
	defer f.mu.Unlock()
	code, out := f.answer(r.URL.Path, in)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(out)
}

func (f *fake) answer(path string, in map[string]any) (int, any) {
	switch {
	case path == "/mode":
		return http.StatusOK, map[string]string{"mode": f.mode}
	case path == "/status":
		return http.StatusOK, f.status()
	case path == "/jobs":
		list := make([]job, 0, len(f.jobs))
		for i := len(f.jobs) - 1; i >= 0; i-- {
			j := *f.jobs[i]
			j.Log = ""
			list = append(list, j)
		}
		return http.StatusOK, map[string]any{"jobs": list}
	case strings.HasPrefix(path, "/jobs/"):
		id, _ := strconv.Atoi(strings.TrimPrefix(path, "/jobs/"))
		for _, j := range f.jobs {
			if j.ID == id {
				return http.StatusOK, map[string]any{"job": j}
			}
		}
		return http.StatusNotFound, apiError("not_found", "no such job")
	case strings.HasPrefix(path, "/verbs/"):
		return f.verb(strings.TrimPrefix(path, "/verbs/"), in)
	}
	return http.StatusNotFound, apiError("not_found", "unknown path")
}

func (f *fake) verb(name string, in map[string]any) (int, any) {
	switch name {
	case "auth.verify":
		return http.StatusOK, map[string]bool{"valid": in["user"] == f.user && in["password"] == devPassword}
	case "setup.complete":
		if f.mode != "setup" {
			return http.StatusConflict, apiError("not_in_setup", "Berth is already set up")
		}
		if in["token"] != devToken {
			return http.StatusForbidden, apiError("invalid_token", "the setup token is not valid")
		}
		f.user, _ = in["user"].(string)
		f.mode = "normal"
		if p, ok := in["patch"].(map[string]any); ok {
			merge(f.cfg, p)
		}
		return http.StatusOK, map[string]any{"user": f.user, "config": f.cfg}
	case "auth.change":
		if in["current_password"] != devPassword {
			return http.StatusForbidden, apiError("wrong_password", "the current password is wrong")
		}
		if pw, _ := in["password"].(string); len(pw) < 10 {
			return http.StatusBadRequest, fieldError("invalid_login", "password", "use at least 10 characters")
		}
		f.user, _ = in["user"].(string)
		return http.StatusOK, map[string]any{"user": f.user}
	case "config.get", "config.export":
		return http.StatusOK, map[string]any{"config": f.cfg, "mode": f.mode, "secrets": f.secrets()}
	case "config.apply":
		return f.apply(in)
	case "config.revert":
		if f.prev == nil {
			return http.StatusConflict, apiError("no_previous", "there is no previous revision")
		}
		return f.replace(f.prev)
	case "config.reset":
		return f.replace(defaultConfig(0))
	case "update.check":
		return http.StatusOK, map[string]any{"check": f.check()}
	case "service.restart":
		return http.StatusOK, map[string]any{"restarted": in["name"]}
	case "tools.list":
		return http.StatusOK, map[string]any{
			"base": []map[string]string{{"name": "node", "version": "24.21.0"}, {"name": "ripgrep", "version": "15.2.0"}, {"name": "gh", "version": "2.101.0"}},
			"user": f.tools,
			"presets": []map[string]any{
				{"id": "python", "name": "Python", "description": "Python with uv.", "tools": map[string]string{"python": "3.14.0", "uv": "0.12.20"}, "state": "not_installed"},
				{"id": "go", "name": "Go", "description": "The Go toolchain.", "tools": map[string]string{"go": "1.27.1"}, "state": "installed"},
			},
			"warnings": []string{},
		}
	case "tools.versions":
		if t, _ := in["tool"].(string); t == "" || t == "nosuchtool" {
			return http.StatusBadRequest, apiError("unknown_tool", "mise does not know that tool")
		}
		return http.StatusOK, map[string]any{"versions": []string{"1.0.0", "1.1.0", "2.0.0"}}
	case "tools.registry":
		return http.StatusOK, map[string]any{"tools": []string{"bun", "deno", "go", "kubectl", "node", "python", "rust", "terraform"}}
	case "forge.status":
		forge := func(id, host string) map[string]any {
			user := ""
			if f.forges[id] {
				user = "example-user"
			}
			return map[string]any{"host": host, "signed_in": f.forges[id], "user": user}
		}
		return http.StatusOK, map[string]any{"git": f.git, "forges": map[string]any{"gh": forge("gh", "github.com"), "glab": forge("glab", "gitlab.com")}}
	case "git.identity":
		name, _ := in["name"].(string)
		email, _ := in["email"].(string)
		if !strings.Contains(email, "@") {
			return http.StatusBadRequest, apiError("invalid_request", "the email must look like name@example.org")
		}
		f.git = map[string]string{"name": name, "email": email}
		return http.StatusOK, f.git
	case "skills.sources.preview", "skills.sources.update":
		if id := str(in["id"]); (id != "" || name == "skills.sources.update") && f.source(id) == nil {
			return http.StatusNotFound, apiError("unknown_source", "No source named "+id)
		}
		return f.start(name, in)
	case "skills.sources.add":
		if other := f.tracking(str(in["repo"]), str(in["ref"]), str(in["path"])); other != "" {
			return http.StatusBadRequest, fieldError("source_exists", "repo", "Source "+other+" already tracks this repository at that ref and path")
		}
		if id := str(in["id"]); id != "" && f.source(id) != nil {
			return http.StatusBadRequest, fieldError("id_exists", "id", "A source named "+id+" exists; choose another id")
		}
		return f.start(name, in)
	case "skills.sources.remove":
		id := str(in["id"])
		if f.source(id) == nil {
			return http.StatusNotFound, apiError("unknown_source", "No source named "+id)
		}
		next := clone(f.cfg)
		delete(sourcesOf(next), id)
		f.replace(next)
		f.start("skills.sync", nil)
		return http.StatusOK, map[string]any{"id": id, "revision": f.cfg["revision"]}
	case "config.upgrade":
		cfg, _ := in["config"].(map[string]any)
		switch {
		case cfg == nil:
			return http.StatusBadRequest, apiError("invalid_request", "send the exported config")
		case num(cfg["schema"]) > 6:
			return http.StatusBadRequest, apiError("invalid_config", "That file is not a config this Berth can read: its schema is newer")
		}
		out := clone(cfg)
		if num(out["schema"]) < 6 {
			out["skills"] = map[string]any{"sources": map[string]any{}} // the real broker migrates C2's skills
		}
		out["schema"] = 6
		return http.StatusOK, map[string]any{"config": out}
	case "login.status":
		return http.StatusOK, f.loginStatus("")
	case "login.start":
		if fakeFlow(str(in["agent"]), str(in["method"])) == "" {
			return http.StatusBadRequest, apiError("invalid_request", "no such sign-in method")
		}
		return f.start("agent.login", in)
	case "login.input", "login.cancel":
		j := f.findJob(num(in["job"]))
		switch {
		case j == nil:
			return http.StatusNotFound, apiError("no_job", "no such job")
		case j.Verb != "agent.login" && (name != "login.cancel" || j.Verb != "forge.login"):
			return http.StatusBadRequest, apiError("not_a_sign_in", "that job is not a sign-in")
		case j.State != "running" || j.cancelled:
			return http.StatusConflict, apiError("job_finished", "the sign-in has ended; start it again")
		case name == "login.cancel":
			j.cancelled = true
			close(j.cancel)
			return http.StatusOK, map[string]any{"ok": true}
		case j.Login == nil || j.Login["flow"] != "paste":
			return http.StatusBadRequest, apiError("no_code", "this sign-in takes no code")
		}
		select {
		case j.input <- strings.TrimSpace(str(in["code"])):
		default:
		}
		return http.StatusOK, map[string]any{"ok": true}
	case "login.logout":
		delete(f.signedIn, str(in["agent"])+" "+str(in["method"]))
		return http.StatusOK, f.loginStatus(str(in["agent"]))
	case "agents.registry":
		return http.StatusOK, map[string]any{"agents": f.registry()}
	case "agents.add":
		if str(in["id"]) != "copilot" {
			return http.StatusNotFound, apiError("unknown_agent", "no agent "+strconv.Quote(str(in["id"]))+" in this image's registry")
		}
		if f.added(copilotPkg) {
			return http.StatusConflict, apiError("already_added", "GitHub Copilot CLI is already added")
		}
		return f.start(name, in)
	case "agents.remove", "agents.rollback":
		if !f.added(str(in["package"])) {
			return http.StatusNotFound, apiError("not_added", str(in["package"])+" is not an added agent")
		}
		if name == "agents.rollback" {
			return f.start(name, in)
		}
		f.setAdded(str(in["package"]), false)
		return http.StatusOK, map[string]any{"config": f.cfg}
	case "workspaces.status":
		return http.StatusOK, f.workspaceStatus()
	case "workspaces.candidates":
		return http.StatusOK, map[string]any{"candidates": f.candidates()}
	case "workspaces.launch":
		return f.launch(in)
	case "workspaces.report":
		id := str(in["id"])
		if f.workspacePath(id) == "" {
			return http.StatusNotFound, apiError("unknown_workspace", "No workspace named "+id)
		}
		return http.StatusOK, map[string]any{"report": f.report(id)}
	case "skills.vendor.resolve":
		switch {
		case in["action"] != "overwrite" && in["action"] != "release":
			return http.StatusBadRequest, fieldError("invalid_request", "action", "The action must be overwrite or release")
		case f.workspacePath(str(in["workspace"])) == "":
			return http.StatusNotFound, apiError("unknown_workspace", "No workspace named "+str(in["workspace"]))
		}
		return f.start(name, in)
	case "workspaces.forget":
		w, ok := f.cfg["workspaces"].(map[string]any)[str(in["id"])].(map[string]any)
		switch {
		case !ok:
			return http.StatusNotFound, apiError("unknown_workspace", "No workspace named "+str(in["id"]))
		case in["delete"] == true && in["confirm"] != w["path"]:
			return http.StatusBadRequest, apiError("confirm_mismatch", "Type the folder's path, "+str(w["path"])+", to delete it")
		}
		return f.start(name, in)
	case "workspaces.add":
		return f.start(name, in)
	case "update", "rollback", "gc", "tools.add", "tools.remove", "preset.install", "forge.login":
		return f.start(name, in)
	}
	return http.StatusNotFound, apiError("unknown_verb", "unknown verb")
}

func (f *fake) apply(in map[string]any) (int, any) {
	if f.mode == "recovery" {
		return http.StatusConflict, apiError("recovery", "the config is unusable; revert it or reset it to the defaults first")
	}
	next := f.cfg
	if patch, ok := in["patch"].(map[string]any); ok {
		if num(in["base_revision"]) != num(f.cfg["revision"]) {
			return http.StatusConflict, map[string]any{
				"error": map[string]any{"code": "conflict", "message": "the config changed since it was read"}, "config": f.cfg}
		}
		next = clone(f.cfg)
		merge(next, patch)
		if tz, _ := next["runtime"].(map[string]any)["tz"].(string); strings.TrimSpace(tz) == "" {
			return http.StatusBadRequest, fieldError("invalid_config", "runtime.tz", "set a time zone such as Europe/Rome")
		}
	}
	if secrets, ok := in["secrets"].(map[string]any); ok {
		for k, v := range secrets {
			if _, known := f.keys[k]; !known && !isMCPToken(k) {
				return http.StatusBadRequest, fieldError("invalid_config", "secrets."+k, "no installed package reads this key")
			}
			f.keys[k] = v != nil
		}
	}
	if _, ok := in["patch"]; !ok {
		return http.StatusOK, map[string]any{"config": f.cfg, "changes": changes(f.cfg, f.cfg)}
	}
	// Like the broker, forget the tokens of servers that no longer use one.
	servers, _ := next["mcp"].(map[string]any)["servers"].(map[string]any)
	keep := map[string]bool{}
	for id, s := range servers {
		if m, _ := s.(map[string]any); m["auth"] == "bearer" {
			keep[mcpToken(id)] = true
		}
	}
	for k := range f.keys {
		if isMCPToken(k) && !keep[k] {
			delete(f.keys, k)
		}
	}
	syncSkills := !reflect.DeepEqual(f.cfg["skills"], next["skills"])
	code, out := f.replace(next)
	if syncSkills {
		f.start("skills.sync", nil)
	}
	return code, out
}

func isMCPToken(k string) bool { return strings.HasPrefix(k, "MCP_") && strings.HasSuffix(k, "_TOKEN") }

func mcpToken(id string) string {
	return "MCP_" + strings.ToUpper(strings.ReplaceAll(id, "-", "_")) + "_TOKEN"
}

func (f *fake) secrets() map[string]any {
	out := map[string]any{}
	for k, set := range f.keys {
		out[k] = map[string]bool{"set": set}
	}
	return out
}

// replace makes next the current config as a new revision.
func (f *fake) replace(next map[string]any) (int, any) {
	old := f.cfg
	next = clone(next)
	next["revision"] = num(old["revision"]) + 1
	f.prev, f.cfg, f.mode = old, next, "normal"
	return http.StatusOK, map[string]any{"config": f.cfg, "changes": changes(old, next)}
}

func changes(old, next map[string]any) map[string][]string {
	out := map[string][]string{"started": {}, "restarted": {}, "stopped": {}}
	for _, s := range [][2]string{{"terminal", "ttyd"}, {"ide", "code_server"}} {
		was, now := enabled(old, s[0]), enabled(next, s[0])
		switch {
		case !was && now:
			out["started"] = append(out["started"], s[1])
		case was && !now:
			out["stopped"] = append(out["stopped"], s[1])
		case was && now && tz(old) != tz(next):
			out["restarted"] = append(out["restarted"], s[1])
		}
	}
	return out
}

func (f *fake) status() map[string]any {
	ago := func(d time.Duration) time.Time { return f.started.Add(-d) }
	services := []map[string]any{{"name": "panel", "state": "running", "pid": 101, "started_at": ago(3 * time.Hour), "restarts": 0, "log": []string{}}}
	if f.mode == "normal" && enabled(f.cfg, "terminal") {
		services = append(services, map[string]any{"name": "ttyd", "state": "running", "pid": 102, "started_at": ago(3 * time.Hour), "restarts": 0, "log": []string{}})
	}
	if f.mode == "normal" && enabled(f.cfg, "ide") {
		services = append(services, map[string]any{"name": "code_server", "state": "failed", "pid": 0, "started_at": nil, "restarts": 3,
			"log": []string{"code-server: listen tcp 127.0.0.1:8080: bind: address already in use"}})
	}
	current := map[string]any{"hash": "3f9c2a7d1e0b4c65a8f7e2d9c1b0a3f4e5d6c7b8a9f0e1d2c3b4a5f6e7d8c9b0",
		"source":   map[string]any{"origin": "image", "fetched_at": ago(72 * time.Hour)},
		"packages": map[string]string{"@example/agent-cli": "2.1.0", "@example/other-cli": "1.4.0", "@example/mcp-files": "0.6.2"}, "installed_at": ago(72 * time.Hour)}
	previous := map[string]any{"hash": "8b7a6f5e4d3c2b1a0f9e8d7c6b5a4f3e2d1c0b9a8f7e6d5c4b3a2f1e0d9c8b7a",
		"source":   map[string]any{"origin": "ref", "ref": "main", "commit": "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678", "fetched_at": ago(240 * time.Hour)},
		"packages": map[string]string{"@example/agent-cli": "2.0.4", "@example/mcp-files": "0.6.0"}, "installed_at": ago(240 * time.Hour)}
	st := map[string]any{
		"mode": f.mode, "revision": f.cfg["revision"], "services": services,
		"agents": map[string]any{"current": current, "previous": previous, "versions": []any{current, previous},
			"follow_image": true, "seed": nil, "user_tools": f.tools, "warnings": []string{},
			"build": map[string]string{"version": "0.0.0-dev", "commit": "dev", "arch": "arm64"}},
		"packages": f.packages(),
		"check":    f.check(),
		"host":     map[string]string{"bind_address": "127.0.0.1", "panel_port": "7690", "puid": "1000", "pgid": "1000", "workspaces_dir": "./workspaces"},
	}
	if f.mode == "recovery" {
		st["config_error"] = "config.json: invalid character '}' looking for beginning of object key string"
	}
	return st
}

func (f *fake) packages() []map[string]any {
	off := map[string]bool{}
	for _, p := range f.cfg["agents"].(map[string]any)["disabled"].([]any) {
		off[p.(string)] = true
	}
	pkgs := []map[string]any{
		{"package": "@example/agent-cli", "name": "Example Agent", "kind": "agent", "version": "2.1.0", "origin": "locked", "enabled": !off["@example/agent-cli"], "icon": "claude",
			"bins": []string{"example"}, "keys": []map[string]any{{"name": "EXAMPLE_API_KEY", "set": f.keys["EXAMPLE_API_KEY"]}},
			"skills_dir": "~/.example/skills", "skills_project": ".example/skills", "mcp_project": ".mcp.json"},
		{"package": "@example/other-cli", "name": "Other Agent", "kind": "agent", "version": "1.4.0", "origin": "locked", "enabled": !off["@example/other-cli"],
			"bins": []string{"other"}, "keys": []map[string]any{},
			"skills_dir": "~/.other/skills", "skills_via": []string{"@example/agent-cli"},
			"skills_project": ".agents/skills", "mcp_project": ".other/config.toml", "mcp_trust": true},
		{"package": "@example/mcp-files", "name": "Files MCP", "kind": "mcp", "version": "0.6.2", "origin": "locked", "enabled": !off["@example/mcp-files"],
			"bins": []string{}, "keys": []map[string]any{}, "mcp_id": "files"},
	}
	if f.added(copilotPkg) {
		pkgs = slices.Insert(pkgs, 2, map[string]any{"package": copilotPkg, "name": "GitHub Copilot CLI", "kind": "agent",
			"version": "1.0.91", "origin": "added", "enabled": !off[copilotPkg], "icon": "githubcopilot",
			"bins": []string{"copilot"}, "keys": []map[string]any{{"name": "COPILOT_GITHUB_TOKEN", "set": f.keys["COPILOT_GITHUB_TOKEN"]}},
			"skills_dir": "~/.copilot/skills"})
	}
	return pkgs
}

func (f *fake) check() map[string]any {
	return map[string]any{"ref": f.cfg["agents"].(map[string]any)["ref"], "hash": "9e8d7c6b5a4f3e2d1c0b9a8f7e6d5c4b3a2f1e0d9c8b7a6f5e4d3c2b1a0f9e8d",
		"update_available": true, "checked_at": time.Now().UTC()}
}

var groups = map[string]string{"update": "agents", "rollback": "agents", "gc": "agents",
	"tools.add": "tools", "tools.remove": "tools", "preset.install": "tools", "forge.login": "forge",
	"skills.sources.preview": "skills", "skills.sources.add": "skills", "skills.sources.update": "skills", "skills.sync": "skills-sync", "skills.vendor.resolve": "skills", "agent.login": "agent-login",
	"agents.add": "agents", "agents.rollback": "agents", "workspaces.add": "workspaces", "workspaces.forget": "workspaces"}

func (f *fake) start(verb string, in map[string]any) (int, any) {
	group := groups[verb]
	for _, j := range f.jobs {
		if j.Group == group && j.State == "running" {
			return http.StatusConflict, apiError("job_running", "another job of this kind is running")
		}
	}
	args := []string{verb}
	switch verb {
	case "tools.add":
		args = []string{"/opt/mise/bin/mise", "use", "-g", str(in["tool"]) + "@" + str(in["version"])}
	case "tools.remove":
		args = []string{"/opt/mise/bin/mise", "unuse", "-g", str(in["tool"])}
	case "preset.install":
		args = []string{"/opt/mise/bin/mise", "install", str(in["preset"])}
	case "forge.login":
		args = []string{"/usr/bin/" + str(in["forge"]), "auth", "login"}
	case "agent.login":
		args = []string{"/usr/local/bin/berth", "login", "run", str(in["agent"]), str(in["method"])}
	case "skills.sources.preview", "skills.sources.add", "skills.sources.update":
		repo, ref, p := str(in["repo"]), cmp.Or(str(in["commit"]), str(in["ref"])), str(in["path"])
		if s := f.source(str(in["id"])); s != nil {
			repo, p, ref = str(s["repo"]), str(s["path"]), cmp.Or(str(in["commit"]), str(in["ref"]), str(s["ref"]))
		}
		args = []string{"/usr/local/bin/berth", "skills", "scan", "--json", "--repo", repo, "--ref", ref}
		if p != "" {
			args = append(args, "--path", p)
		}
	case "agents.add":
		args = []string{"/usr/local/bin/berth", "agents", "install", "--json", str(in["id"])}
	case "agents.rollback":
		args = []string{"/usr/local/bin/berth", "agents", "rollback", "--json", str(in["package"])}
	case "workspaces.add":
		args = []string{"/usr/local/bin/berth", "workspaces", "probe", "--json", "--path", str(in["path"])}
		if in["mode"] == "clone" {
			args = []string{"/usr/local/bin/berth", "workspaces", "clone", "--json", "--repo", str(in["repo"]), "--path", str(in["path"])}
		}
	case "workspaces.forget":
		args = []string{"/usr/local/bin/berth", "workspaces", "kill", "--json", "--id", str(in["id"])}
	case "skills.vendor.resolve":
		args = []string{"/usr/local/bin/berth", "skills", "vendor", "--json", "--workspace", str(in["workspace"]),
			"--key", str(in["source"]) + "/" + str(in["name"]), "--action", str(in["action"])}
	}
	j := &job{ID: len(f.jobs) + 1, Verb: verb, Group: group, Args: args, State: "running", StartedAt: time.Now().UTC(),
		Result: json.RawMessage("null"), input: make(chan string, 1), cancel: make(chan struct{})}
	f.jobs = append(f.jobs, j)
	go f.run(j, in)
	return http.StatusAccepted, map[string]any{"job": j}
}

func (f *fake) run(j *job, in map[string]any) {
	if j.Verb == "agent.login" {
		f.runLogin(j, in)
		return
	}
	lines := []string{"starting " + j.Verb, "resolving versions", "installing", "done"}
	switch j.Verb {
	case "rollback":
		lines = []string{"starting rollback", "error: no previous agent set to switch to"}
	case "agents.add":
		lines = []string{"berth agents: installing GitHub Copilot CLI 1.0.91", "added 2 packages in 4s",
			"berth agents: smoke-testing copilot", "installed @github/copilot 1.0.91"}
	case "agents.rollback":
		lines = []string{"starting rollback", "error: no previous version to roll back to"}
	case "forge.login":
		lines = []string{"! First copy your one-time code", "waiting for approval", "waiting for approval", "waiting for approval", "approved", "logged in"}
	case "workspaces.add":
		lines = []string{"probe     /workspaces/" + str(in["path"])}
		if in["mode"] == "clone" {
			lines = []string{"Cloning into '/workspaces/" + str(in["path"]) + "'...", "remote: Enumerating objects: 42, done.",
				"Receiving objects: 100% (42/42), done.", "Resolving deltas: 100% (7/7), done."}
		}
	case "workspaces.forget":
		lines = []string{"kill      ws-" + str(in["id"]), "release   what Berth wrote into " + str(in["id"])}
		if in["delete"] == true {
			lines = append(lines, "remove    the folder of "+str(in["id"])) // run reads f.cfg only under f.mu
		}
	case "skills.vendor.resolve":
		lines = []string{str(in["action"]) + "   " + str(in["name"]) + " in " + str(in["workspace"])}
	case "skills.sources.preview", "skills.sources.add", "skills.sources.update":
		lines = []string{"fetch     " + j.Args[5] + "@" + j.Args[7], "list      SKILL.md files", "read      frontmatter"}
	}
	for i, line := range lines {
		select {
		case <-j.cancel:
			f.mu.Lock()
			f.finish(j, 1, "failed", `{"error":{"code":"cancelled","message":"cancelled"}}`)
			f.mu.Unlock()
			return
		case <-time.After(f.step):
		}
		f.mu.Lock()
		j.Log += line + "\n"
		if j.Verb == "forge.login" && i == 0 {
			url := "https://github.com/login/device"
			if in["forge"] == "glab" {
				url = "https://gitlab.com/oauth/device"
			}
			j.Device = map[string]string{"code": "ABCD-1234", "url": url}
		}
		f.mu.Unlock()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	now, code := time.Now().UTC(), 0
	j.State, j.Result = "succeeded", json.RawMessage("{}")
	switch j.Verb {
	case "rollback":
		code, j.State = 1, "failed"
		j.Result = json.RawMessage(`{"error":{"code":"rollback_failed","step":"switch","message":"no previous agent set to switch to"}}`)
	case "workspaces.add":
		code, j.State, j.Result = f.addWorkspace(in)
	case "workspaces.forget":
		id := str(in["id"])
		path := f.workspacePath(id)
		next := clone(f.cfg)
		forgetRefs(next, id)
		f.replace(next)
		delete(f.sessions, id)
		j.Result, _ = json.Marshal(map[string]any{"id": id, "path": path, "deleted": in["delete"] == true, "left": []string{}, "kept": []string{}})
	case "skills.vendor.resolve":
		ws, src, name := str(in["workspace"]), str(in["source"]), str(in["name"])
		if in["action"] == "overwrite" {
			f.overwritten[ws+"/"+src+"/"+name] = true
			f.start("skills.sync", nil)
		} else {
			next := clone(f.cfg)
			attached, _ := sourcesOf(next)[src].(map[string]any)["attached"].(map[string]any)
			if a, ok := attached[name].(map[string]any); ok {
				wss, _ := a["workspaces"].(map[string]any)
				delete(wss, ws)
				if agents, isList := a["agents"].([]any); isList && len(agents) == 0 && len(wss) == 0 {
					delete(attached, name)
				}
				f.replace(next)
				f.start("skills.sync", nil)
			}
		}
		j.Result, _ = json.Marshal(map[string]any{"action": in["action"], "workspace": ws, "name": name, "changed": in["action"] == "release"})
	case "agents.add":
		f.setAdded(copilotPkg, true)
		j.Result = json.RawMessage(`{"id":"copilot","package":"@github/copilot","version":"1.0.91","hash":"0123456789ab"}`)
	case "agents.rollback":
		code, j.State = 1, "failed"
		j.Result = json.RawMessage(`{"error":{"code":"no_previous","message":"no previous version to roll back to"}}`)
	case "forge.login":
		f.forges[str(in["forge"])] = true
	case "tools.add", "tools.remove":
		// Like mise use -g / unuse -g: one version per tool.
		name := str(in["tool"])
		f.tools = slices.DeleteFunc(f.tools, func(t map[string]string) bool { return t["name"] == name })
		if j.Verb == "tools.add" {
			f.tools = append(f.tools, map[string]string{"name": name, "version": str(in["version"])})
		}
	case "skills.sources.preview", "skills.sources.add", "skills.sources.update":
		res, failure := f.fakeSource(j.Verb, in)
		if failure != "" {
			code, j.State, j.Result = 1, "failed", json.RawMessage(failure)
			break
		}
		j.Result, _ = json.Marshal(res)
	}
	j.FinishedAt, j.ExitCode = &now, &code
}

const (
	fakeCommit  = "0123456789abcdef0123456789abcdef01234567"
	fakeCommit2 = "89abcdef0123456789abcdef0123456789abcdef"
)

var fakeCatalog = []string{"brand-guidelines", "canvas-design", "docx", "frontend-design", "mcp-builder",
	"pdf", "pptx", "skill-creator", "webapp-testing", "xlsx"}

// sourcesOf is cfg's skill sources.
func sourcesOf(cfg map[string]any) map[string]any {
	return cfg["skills"].(map[string]any)["sources"].(map[string]any)
}

// source is the current source id, or nil.
func (f *fake) source(id string) map[string]any {
	s, _ := sourcesOf(f.cfg)[id].(map[string]any)
	return s
}

// tracking names the source that tracks repo at ref and path, or "".
func (f *fake) tracking(repo, ref, path string) string {
	for _, id := range slices.Sorted(maps.Keys(sourcesOf(f.cfg))) {
		if s := f.source(id); str(s["repo"]) == repo && str(s["ref"]) == ref && str(s["path"]) == path {
			return id
		}
	}
	return ""
}

// sourceID derives an id the way the broker does, close enough for the
// fake: owner-repo, then -<ref>, then -2.
func (f *fake) sourceID(repo, ref string) string {
	segs := strings.Split(strings.TrimSuffix(repo, ".git"), "/")
	base := strings.ToLower(strings.Join(segs[max(0, len(segs)-2):], "-"))
	id := base
	if f.source(id) != nil {
		id = base + "-" + strings.ToLower(strings.ReplaceAll(ref, "/", "-"))
	}
	for n := 2; f.source(id) != nil; n++ {
		id = fmt.Sprintf("%s-%s-%d", base, ref, n)
	}
	return id
}

// provided says whether any source attaches name.
func (f *fake) provided(name string) bool {
	for _, s := range sourcesOf(f.cfg) {
		if s.(map[string]any)["attached"].(map[string]any)[name] != nil {
			return true
		}
	}
	return false
}

// fakeScan is the devserver's stand-in for berth skills scan: the commit
// ref resolves to and the found rows there (new maps on every call), or
// the job's failure JSON.
func fakeScan(repo, ref string) (string, map[string]any, string) {
	if strings.Contains(repo+ref, "missing") {
		return "", nil, `{"error":{"code":"ref_not_found","field":"ref","message":"Ref ` + ref + ` not found in that repository"}}`
	}
	found := map[string]any{}
	if strings.HasSuffix(repo, "/anthropics/skills") {
		for _, id := range fakeCatalog {
			found[id] = map[string]any{"path": "skills/" + id, "description": "A skill from anthropics/skills"}
		}
		return fakeCommit, found, ""
	}
	found["broken"] = map[string]any{"path": "skills/broken", "error": "SKILL.md has no description"}
	if strings.Contains(ref, "v2") || ref == fakeCommit2 {
		found["release-notes"] = map[string]any{"path": "skills/release-notes", "description": "Draft release notes from merged pull requests and tags"}
		found["triage"] = map[string]any{"path": "skills/triage", "description": "Label and route new issues"}
		return fakeCommit2, found, ""
	}
	found["changelog"] = map[string]any{"path": "skills/changelog", "description": "Keep CHANGELOG.md in step with tags"}
	found["release-notes"] = map[string]any{"path": "skills/release-notes", "description": "Draft release notes from merged pull requests"}
	return fakeCommit, found, ""
}

// fakeSource finishes a skills.sources job like the broker: a preview of a
// new or an existing source, an add or an update.
func (f *fake) fakeSource(verb string, in map[string]any) (map[string]any, string) {
	id := str(in["id"])
	src := f.source(id)
	repo, ref, p := str(in["repo"]), str(in["ref"]), str(in["path"])
	if src != nil {
		repo, p, ref = str(src["repo"]), str(src["path"]), cmp.Or(ref, str(src["ref"]))
	}
	commit, found, failure := fakeScan(repo, cmp.Or(str(in["commit"]), ref))
	if failure != "" {
		return nil, failure
	}
	switch {
	case verb == "skills.sources.preview" && src == nil:
		return map[string]any{"commit": commit, "found": found, "id": f.sourceID(repo, ref), "exists": f.tracking(repo, ref, p)}, ""
	case verb == "skills.sources.preview":
		return fakeDiff(id, ref, src, commit, found), ""
	case verb == "skills.sources.add":
		id = cmp.Or(id, f.sourceID(repo, ref))
		attached, names, skipped := map[string]any{}, []string{}, []string{}
		for _, name := range slices.Sorted(maps.Keys(found)) {
			switch {
			case in["attach_all"] != true || found[name].(map[string]any)["error"] != nil:
			case f.provided(name):
				skipped = append(skipped, name)
			default:
				attached[name] = map[string]any{"enabled": true, "agents": nil, "workspaces": map[string]any{}}
				names = append(names, name)
			}
		}
		next := clone(f.cfg)
		sourcesOf(next)[id] = map[string]any{"repo": repo, "ref": ref, "path": p, "commit": commit, "found": found, "attached": attached}
		f.replace(next)
		f.start("skills.sync", nil) // start takes no lock; the broker syncs after every skills change
		return map[string]any{"id": id, "commit": commit, "attached": names, "skipped": skipped, "changed": true, "revision": f.cfg["revision"]}, ""
	}
	next := clone(f.cfg)
	s := sourcesOf(next)[id].(map[string]any)
	changed := s["commit"] != commit || s["ref"] != ref
	missing := []string{}
	for _, name := range slices.Sorted(maps.Keys(s["attached"].(map[string]any))) {
		if row, _ := found[name].(map[string]any); row == nil || row["error"] != nil {
			missing = append(missing, name)
		}
	}
	if changed {
		s["ref"], s["commit"], s["found"] = ref, commit, found
		f.replace(next)
		f.start("skills.sync", nil) // start takes no lock; the broker syncs after every skills change
	}
	return map[string]any{"id": id, "commit": commit, "changed": changed, "revision": f.cfg["revision"], "missing": missing}, ""
}

// fakeDiff is a preview of source id at ref, with the broker's diff kinds.
func fakeDiff(id, ref string, src map[string]any, commit string, found map[string]any) map[string]any {
	d := map[string]any{"id": id, "ref": ref, "from": src["commit"], "commit": commit,
		"changed": commit != src["commit"] || ref != src["ref"], "added": []any{}, "removed": []any{}, "moved": []any{}, "described": []any{}}
	if commit == src["commit"] {
		return d
	}
	old, _ := src["found"].(map[string]any)
	attached, _ := src["attached"].(map[string]any)
	row := func(name string, fr map[string]any) map[string]any {
		r := map[string]any{"name": name, "path": fr["path"], "attached": attached[name] != nil}
		for _, k := range []string{"description", "error"} {
			if v, ok := fr[k]; ok {
				r[k] = v
			}
		}
		return r
	}
	add := func(kind string, r map[string]any) { d[kind] = append(d[kind].([]any), r) }
	for _, name := range slices.Sorted(maps.Keys(found)) {
		n, r := found[name].(map[string]any), row(name, found[name].(map[string]any))
		switch o, had := old[name].(map[string]any); {
		case !had:
			add("added", r)
		case o["path"] != n["path"]:
			r["from"] = o["path"]
			add("moved", r)
		case o["description"] != n["description"] || o["error"] != n["error"]:
			add("described", r)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(old)) {
		if found[name] == nil {
			add("removed", row(name, old[name].(map[string]any)))
		}
	}
	return d
}

func strs(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, x := range list {
		out = append(out, str(x))
	}
	return out
}

// fakeLogins are the fake agents' sign-in methods.
var fakeLogins = []struct {
	pkg, name string
	methods   [][3]string // id, label, flow
}{
	{"@example/agent-cli", "Example Agent", [][3]string{{"account", "Example account", "paste"}}},
	{"@example/other-cli", "Other Agent", [][3]string{{"chatgpt", "ChatGPT", "device"}, {"copilot", "GitHub Copilot", "device"}}},
	{copilotPkg, "GitHub Copilot CLI", [][3]string{{"github", "GitHub", "device"}}},
}

func fakeFlow(pkg, id string) string {
	for _, a := range fakeLogins {
		for _, m := range a.methods {
			if a.pkg == pkg && m[0] == id {
				return m[2]
			}
		}
	}
	return ""
}

// loginStatus answers login.status; only, when set, keeps one agent.
func (f *fake) loginStatus(only string) map[string]any {
	off := map[string]bool{}
	for _, p := range f.cfg["agents"].(map[string]any)["disabled"].([]any) {
		off[p.(string)] = true
	}
	agents := map[string]any{}
	for _, a := range fakeLogins {
		if off[a.pkg] || (only != "" && a.pkg != only) || (a.pkg == copilotPkg && !f.added(copilotPkg)) {
			continue
		}
		methods := []map[string]any{}
		for _, m := range a.methods {
			signed := f.signedIn[a.pkg+" "+m[0]]
			account := ""
			if signed {
				account = "you@example.org"
			}
			methods = append(methods, map[string]any{"id": m[0], "label": m[1], "flow": m[2],
				"signed_in": signed, "account": account, "error": ""})
		}
		agents[a.pkg] = map[string]any{"name": a.name, "methods": methods}
	}
	return map[string]any{"agents": agents, "schema": 1}
}

func (f *fake) findJob(id int) *job {
	for _, j := range f.jobs {
		if j.ID == id {
			return j
		}
	}
	return nil
}

// finish ends j. Caller holds f.mu.
func (f *fake) finish(j *job, code int, state, result string) {
	now := time.Now().UTC()
	j.State, j.Result, j.FinishedAt, j.ExitCode = state, json.RawMessage(result), &now, &code
}

// runLogin plays berth login run: a device sign-in approves itself; a paste
// sign-in waits for a code, and "wrong" gets a new URL.
func (f *fake) runLogin(j *job, in map[string]any) {
	pkg, id := str(in["agent"]), str(in["method"])
	flow := fakeFlow(pkg, id)
	login := map[string]any{"agent": pkg, "method": id, "flow": flow, "url": "", "code": "",
		"waiting": false, "inputs": 0, "signed_in": false, "account": "", "failed": ""}
	cancelled := func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.finish(j, 1, "failed", `{"error":{"code":"cancelled","message":"cancelled"}}`)
	}
	step := func(line string, change func()) bool {
		select {
		case <-j.cancel:
			cancelled()
			return false
		case <-time.After(f.step):
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		j.Log += line + "\n"
		if change != nil {
			change()
		}
		j.Login = clone(login)
		return true
	}
	signed := func() {
		f.signedIn[pkg+" "+id] = true
		login["signed_in"], login["account"] = true, "you@example.org"
	}
	if !step("berth: login start "+flow, nil) {
		return
	}
	if flow == "device" {
		url := "https://example.com/device"
		if !step("berth: login url "+url, func() { login["url"] = url }) ||
			!step("berth: login device-code WXYZ-1234", func() { login["code"] = "WXYZ-1234" }) ||
			!step("│ Waiting for authorization", nil) || !step("│ Waiting for authorization", nil) ||
			!step("berth: login signed in as you@example.org", signed) {
			return
		}
	} else {
		for n := 1; ; n++ {
			url := fmt.Sprintf("https://example.com/authorize?attempt=%d", n)
			if !step("berth: login url "+url, func() { login["url"], login["waiting"] = url, false }) ||
				!step("berth: login waiting for code", func() { login["waiting"] = true }) {
				return
			}
			var code string
			select {
			case code = <-j.input:
			case <-j.cancel:
				cancelled()
				return
			}
			if !step("berth: login input received", func() {
				login["inputs"] = num(login["inputs"]) + 1
				login["waiting"] = false
			}) {
				return
			}
			if code != "wrong" {
				break
			}
			if !step("│ Failed to authenticate with authorization code:invalid_grant", nil) {
				return
			}
		}
		if !step("berth: login signed in as you@example.org", signed) {
			return
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finish(j, 0, "succeeded", "{}")
}

func apiError(code, msg string) map[string]any {
	return map[string]any{"error": map[string]any{"code": code, "message": msg}}
}

func fieldError(code, field, msg string) map[string]any {
	return map[string]any{"error": map[string]any{"code": code, "message": field + ": " + msg,
		"fields": []map[string]string{{"field": field, "message": msg}}}}
}

func merge(dst, patch map[string]any) {
	for k, v := range patch {
		if v == nil {
			delete(dst, k)
			continue
		}
		if sub, ok := v.(map[string]any); ok {
			if d, ok := dst[k].(map[string]any); ok {
				merge(d, sub)
				continue
			}
		}
		dst[k] = v
	}
}

func clone(m map[string]any) map[string]any {
	raw, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

func num(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	}
	return -1
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func enabled(cfg map[string]any, frontend string) bool {
	on, _ := cfg["frontends"].(map[string]any)[frontend].(map[string]any)["enabled"].(bool)
	return on
}

func tz(cfg map[string]any) string {
	return str(cfg["runtime"].(map[string]any)["tz"])
}

const copilotPkg = "@github/copilot"

// added reports whether pkg is in agents.added. Caller holds f.mu.
func (f *fake) added(pkg string) bool {
	return slices.Contains(strs(f.cfg["agents"].(map[string]any)["added"]), pkg)
}

func (f *fake) registry() []map[string]any {
	return []map[string]any{{"id": "copilot", "package": copilotPkg, "name": "GitHub Copilot CLI",
		"description": "GitHub's coding agent, signed in with your GitHub account", "version": "1.0.91",
		"icon": "githubcopilot", "keys": []string{"COPILOT_GITHUB_TOKEN"}, "login": []string{"GitHub"},
		"added": f.added(copilotPkg)}}
}

// setAdded replaces agents.added (and drops pkg from disabled on removal)
// as a new revision. Caller holds f.mu.
func (f *fake) setAdded(pkg string, on bool) {
	next := clone(f.cfg)
	agents := next["agents"].(map[string]any)
	list := slices.DeleteFunc(strs(agents["added"]), func(p string) bool { return p == pkg })
	if on {
		list = append(list, pkg)
		slices.Sort(list)
	} else {
		agents["disabled"] = toAny(slices.DeleteFunc(strs(agents["disabled"]), func(p string) bool { return p == pkg }))
	}
	agents["added"] = toAny(list)
	f.replace(next)
}

func toAny(list []string) []any {
	out := make([]any, len(list))
	for i, s := range list {
		out[i] = s
	}
	return out
}

// workspacePath is the folder of workspace id, or "".
func (f *fake) workspacePath(id string) string {
	w, _ := f.cfg["workspaces"].(map[string]any)[id].(map[string]any)
	return str(w["path"])
}

// candidates are the fake /workspaces folders no workspace uses.
func (f *fake) candidates() []map[string]any {
	out := []map[string]any{}
	for _, c := range []map[string]any{{"path": "scratch", "git": false}, {"path": "vendor/readonly-repo", "git": true}} {
		taken := false
		for _, w := range f.cfg["workspaces"].(map[string]any) {
			taken = taken || w.(map[string]any)["path"] == c["path"]
		}
		if !taken {
			out = append(out, c)
		}
	}
	return out
}

// addWorkspace finishes a workspaces.add job: its exit code, state and result.
func (f *fake) addWorkspace(in map[string]any) (int, string, json.RawMessage) {
	repo, path, id := str(in["repo"]), str(in["path"]), str(in["id"])
	failed := func(code, field, msg string) (int, string, json.RawMessage) {
		raw, _ := json.Marshal(map[string]any{"error": map[string]any{"code": code, "field": field, "message": msg}})
		return 1, "failed", raw
	}
	switch {
	case strings.Contains(repo, "private"):
		return failed("auth_required", "repo", "Repository not found, or it is private. To use a private repository, sign in to github.com on the Accounts page.")
	case strings.Contains(repo, "missing"):
		return failed("ref_not_found", "ref", "Ref "+cmp.Or(str(in["ref"]), "HEAD")+" not found in that repository")
	case in["mode"] == "clone" && slices.ContainsFunc(f.candidates(), func(c map[string]any) bool { return c["path"] == path }):
		return failed("path_exists", "path", "That folder exists; adopt it instead")
	}
	next := clone(f.cfg)
	next["workspaces"].(map[string]any)[id] = map[string]any{"path": path, "repo": repo, "env": map[string]any{},
		"instructions": map[string]any{"shared": false}}
	f.replace(next)
	raw, _ := json.Marshal(map[string]any{"id": id, "path": path, "writable": !strings.Contains(path, "readonly"),
		"git": in["mode"] == "clone" || strings.Contains(path, "repo"), "changed": true})
	return 0, "succeeded", raw
}

// workspaceStatus answers workspaces.status.
func (f *fake) workspaceStatus() map[string]any {
	out := map[string]any{}
	for id, w := range f.cfg["workspaces"].(map[string]any) {
		path := str(w.(map[string]any)["path"])
		windows := append([]string{}, f.sessions[id]...)
		out[id] = map[string]any{"exists": true, "writable": !strings.Contains(path, "readonly"),
			"git": str(w.(map[string]any)["repo"]) != "" || strings.Contains(path, "repo"), "branch": "main",
			"head": "1a2b3c4d5e6f", "dirty": len(id) % 3, "session": len(windows) > 0, "windows": windows, "unknown": false}
	}
	return map[string]any{"workspaces": out}
}

// launch answers workspaces.launch: it adds a window to the session.
func (f *fake) launch(in map[string]any) (int, any) {
	id := str(in["id"])
	if f.workspacePath(id) == "" {
		return http.StatusNotFound, apiError("unknown_workspace", "No workspace named "+id)
	}
	window := "zsh"
	if agent := str(in["agent"]); agent != "" {
		i := slices.IndexFunc(f.packages(), func(p map[string]any) bool { return p["package"] == agent && p["kind"] == "agent" })
		switch {
		case i < 0:
			return http.StatusBadRequest, apiError("unknown_agent", agent+" is not an installed agent")
		case f.packages()[i]["enabled"] != true:
			return http.StatusConflict, apiError("agent_disabled", str(f.packages()[i]["name"])+" is switched off; turn it on on the Agents page")
		}
		window = f.packages()[i]["bins"].([]string)[0]
	}
	f.sessions[id] = append(f.sessions[id], window)
	reply := map[string]any{"session": "ws-" + id, "window": ""}
	if str(in["agent"]) != "" {
		reply["window"] = window
	}
	return http.StatusOK, reply
}

// forgetRefs removes workspace id and every reference to it from cfg.
func forgetRefs(cfg map[string]any, id string) {
	delete(cfg["workspaces"].(map[string]any), id)
	for _, s := range sourcesOf(cfg) {
		attached, _ := s.(map[string]any)["attached"].(map[string]any)
		for _, a := range attached {
			if wss, ok := a.(map[string]any)["workspaces"].(map[string]any); ok {
				delete(wss, id)
			}
		}
	}
	lists, _ := cfg["mcp"].(map[string]any)["workspaces"].(map[string]any)
	for server, l := range lists {
		rest := slices.DeleteFunc(strs(l), func(w string) bool { return w == id })
		if len(rest) == 0 {
			delete(lists, server)
		} else {
			lists[server] = toAny(rest)
		}
	}
}

// report answers workspaces.report from the config, as the converger
// would have left the workspace.
func (f *fake) report(id string) map[string]any {
	folder := "/workspaces/" + f.workspacePath(id)
	entries := map[string]any{}
	for sid, s := range sourcesOf(f.cfg) {
		src := s.(map[string]any)
		attached, _ := src["attached"].(map[string]any)
		for name, a := range attached {
			att := a.(map[string]any)
			wss, _ := att["workspaces"].(map[string]any)
			mode := str(wss[id])
			if mode == "" || att["enabled"] == false {
				continue
			}
			key := sid + "/" + name
			e := map[string]any{"name": name, "mode": mode, "commit": src["commit"], "from": name,
				"links": []string{".agents/skills/" + name, ".example/skills/" + name}}
			if mode == "vendor" {
				e["dir"], e["links"] = ".agents/skills/"+name, []string{".example/skills/" + name}
				e["edited"] = name == "release-notes" && !f.overwritten[id+"/"+key]
			}
			entries[key] = e
		}
	}
	var ids []string
	lists, _ := f.cfg["mcp"].(map[string]any)["workspaces"].(map[string]any)
	for server, l := range lists {
		if slices.Contains(strs(l), id) {
			ids = append(ids, server)
		}
	}
	slices.Sort(ids)
	files := map[string]any{}
	if len(ids) > 0 {
		cloned := str(f.cfg["workspaces"].(map[string]any)[id].(map[string]any)["repo"]) != ""
		files[".mcp.json"] = map[string]any{"style": "claude", "key": "mcpServers", "ids": ids, "created": !cloned, "tracked": cloned}
		files[".other/config.toml"] = map[string]any{"style": "codex-toml", "ids": ids, "created": true}
	}
	return map[string]any{
		"skills": map[string]any{"path": folder, "entries": entries, "dirs": []string{}},
		"mcp":    map[string]any{"path": folder, "files": files, "dirs": []string{}, "trust": len(ids) > 0},
	}
}
