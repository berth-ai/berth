package main

import (
	"encoding/json"
	"net/http"
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
}

// fake answers like the broker for everything the panel's browser side
// calls. Config is kept as decoded JSON so patches merge like the real
// store's.
type fake struct {
	mu      sync.Mutex
	mode    string
	user    string
	cfg     map[string]any
	prev    map[string]any
	keys    map[string]bool
	git     map[string]string
	forges  map[string]bool
	tools   []map[string]string
	jobs    []*job
	started time.Time
	step    time.Duration
}

func newFake(mode string) *fake {
	return &fake{
		mode: mode, user: devUser, started: time.Now().UTC(), step: 400 * time.Millisecond,
		cfg:    defaultConfig(1),
		keys:   map[string]bool{"EXAMPLE_API_KEY": false},
		git:    map[string]string{"name": "", "email": ""},
		forges: map[string]bool{"gh": false, "glab": false},
		tools:  []map[string]string{{"name": "terraform", "version": "1.1.0"}},
	}
}

func defaultConfig(rev int) map[string]any {
	return map[string]any{
		"schema": 1, "revision": rev,
		"frontends": map[string]any{"terminal": map[string]any{"enabled": true}, "ide": map[string]any{"enabled": false}},
		"agents":    map[string]any{"ref": "main", "disabled": []any{}, "keep": 2, "boot_timeout": 600, "mcp_timeout": 300},
		"runtime":   map[string]any{"tz": "Etc/UTC"},
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
		return http.StatusOK, map[string]any{"config": f.cfg, "mode": f.mode, "secrets": map[string]any{}}
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
			if _, known := f.keys[k]; !known {
				return http.StatusBadRequest, fieldError("invalid_config", "secrets."+k, "no installed package reads this key")
			}
			f.keys[k] = v != nil
		}
	}
	if _, ok := in["patch"]; !ok {
		return http.StatusOK, map[string]any{"config": f.cfg, "changes": changes(f.cfg, f.cfg)}
	}
	return f.replace(next)
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
		"packages": map[string]string{"@example/agent-cli": "2.1.0", "@example/mcp-files": "0.6.2"}, "installed_at": ago(72 * time.Hour)}
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
	return []map[string]any{
		{"package": "@example/agent-cli", "name": "Example Agent", "kind": "agent", "version": "2.1.0", "enabled": !off["@example/agent-cli"],
			"bins": []string{"example"}, "keys": []map[string]any{{"name": "EXAMPLE_API_KEY", "set": f.keys["EXAMPLE_API_KEY"]}}},
		{"package": "@example/mcp-files", "name": "Files MCP", "kind": "mcp", "version": "0.6.2", "enabled": !off["@example/mcp-files"],
			"bins": []string{}, "keys": []map[string]any{}},
	}
}

func (f *fake) check() map[string]any {
	return map[string]any{"ref": f.cfg["agents"].(map[string]any)["ref"], "hash": "9e8d7c6b5a4f3e2d1c0b9a8f7e6d5c4b3a2f1e0d9c8b7a6f5e4d3c2b1a0f9e8d",
		"update_available": true, "checked_at": time.Now().UTC()}
}

var groups = map[string]string{"update": "agents", "rollback": "agents", "gc": "agents",
	"tools.add": "tools", "tools.remove": "tools", "preset.install": "tools", "forge.login": "forge"}

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
	}
	j := &job{ID: len(f.jobs) + 1, Verb: verb, Group: group, Args: args, State: "running", StartedAt: time.Now().UTC(), Result: json.RawMessage("null")}
	f.jobs = append(f.jobs, j)
	go f.run(j, in)
	return http.StatusAccepted, map[string]any{"job": j}
}

func (f *fake) run(j *job, in map[string]any) {
	lines := []string{"starting " + j.Verb, "resolving versions", "installing", "done"}
	switch j.Verb {
	case "rollback":
		lines = []string{"starting rollback", "error: no previous agent set to switch to"}
	case "forge.login":
		lines = []string{"! First copy your one-time code", "waiting for approval", "waiting for approval", "waiting for approval", "approved", "logged in"}
	}
	for i, line := range lines {
		time.Sleep(f.step)
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
	case "forge.login":
		f.forges[str(in["forge"])] = true
	case "tools.add", "tools.remove":
		// Like mise use -g / unuse -g: one version per tool.
		name := str(in["tool"])
		f.tools = slices.DeleteFunc(f.tools, func(t map[string]string) bool { return t["name"] == name })
		if j.Verb == "tools.add" {
			f.tools = append(f.tools, map[string]string{"name": name, "version": str(in["version"])})
		}
	}
	j.FinishedAt, j.ExitCode = &now, &code
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
