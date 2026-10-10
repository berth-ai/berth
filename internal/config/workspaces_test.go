package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func ws(p string) Workspace { return Workspace{Path: p, Env: map[string]WorkspaceEnv{}} }

func envWS(env map[string]WorkspaceEnv) map[string]Workspace {
	return map[string]Workspace{"a": {Path: "a", Env: env}}
}

func TestDefaultsHaveNoWorkspaces(t *testing.T) {
	if d := Defaults(); d.Workspaces == nil || len(d.Workspaces) != 0 {
		t.Errorf("workspaces = %v", d.Workspaces)
	}
}

func TestValidateWorkspaces(t *testing.T) {
	many := map[string]WorkspaceEnv{}
	for i := range 101 {
		many[fmt.Sprintf("K%d", i)] = WorkspaceEnv{Value: "x"}
	}
	for name, tc := range map[string]struct {
		ws    map[string]Workspace
		field string
	}{
		"cloned":          {map[string]Workspace{"api": {Path: "acme/api", Repo: "https://github.com/acme/api"}}, ""},
		"adopted":         {map[string]Workspace{"api": ws("api")}, ""},
		"siblings":        {map[string]Workspace{"a": ws("acme/api"), "b": ws("acme/api2")}, ""},
		"env":             {envWS(map[string]WorkspaceEnv{"LOG_LEVEL": {Value: "debug"}, "STRIPE_KEY": {Secret: true}}), ""},
		"bad id":          {map[string]Workspace{"Api": ws("api")}, "workspaces.Api"},
		"long id":         {map[string]Workspace{strings.Repeat("a", 65): ws("api")}, "workspaces." + strings.Repeat("a", 65)},
		"empty path":      {map[string]Workspace{"a": ws("")}, "workspaces.a.path"},
		"absolute path":   {map[string]Workspace{"a": ws("/acme")}, "workspaces.a.path"},
		"dotdot":          {map[string]Workspace{"a": ws("acme/../x")}, "workspaces.a.path"},
		"trailing slash":  {map[string]Workspace{"a": ws("acme/")}, "workspaces.a.path"},
		"temp folder":     {map[string]Workspace{"a": ws("acme/.berth-tmp-x1/api")}, "workspaces.a.path"},
		"git folder":      {map[string]Workspace{"a": ws("acme/.git")}, "workspaces.a.path"},
		"http repo":       {map[string]Workspace{"a": {Path: "a", Repo: "http://github.com/a/b"}}, "workspaces.a.repo"},
		"same path":       {map[string]Workspace{"a": ws("acme/api"), "b": ws("acme/api")}, "workspaces.b.path"},
		"nested":          {map[string]Workspace{"a": ws("acme"), "b": ws("acme/api")}, "workspaces.b.path"},
		"holds earlier":   {map[string]Workspace{"a": ws("acme/api"), "b": ws("acme")}, "workspaces.b.path"},
		"lowercase key":   {envWS(map[string]WorkspaceEnv{"log_level": {}}), "workspaces.a.env.log_level"},
		"reserved key":    {envWS(map[string]WorkspaceEnv{"PATH": {}}), "workspaces.a.env.PATH"},
		"reserved prefix": {envWS(map[string]WorkspaceEnv{"GIT_DIR": {}}), "workspaces.a.env.GIT_DIR"},
		"secret value":    {envWS(map[string]WorkspaceEnv{"KEY": {Value: "x", Secret: true}}), "workspaces.a.env.KEY.value"},
		"long value":      {envWS(map[string]WorkspaceEnv{"KEY": {Value: strings.Repeat("x", 4097)}}), "workspaces.a.env.KEY.value"},
		"too many":        {envWS(many), "workspaces.a.env"},
	} {
		t.Run(name, func(t *testing.T) {
			c := Defaults()
			c.Workspaces = tc.ws
			err := c.Validate()
			if tc.field == "" {
				if err != nil {
					t.Fatalf("Validate = %v", err)
				}
				return
			}
			var ve *ValidationError
			if !errors.As(err, &ve) || len(ve.Fields) != 1 || ve.Fields[0].Field != tc.field {
				t.Fatalf("Validate = %v, want one error on %s", err, tc.field)
			}
		})
	}
}

func TestPathsOverlap(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"acme/api", "acme/api", true}, {"acme", "acme/api", true}, {"acme/api", "acme", true},
		{"acme/api", "acme/api2", false}, {"acme-x", "acme", false}, {"a/b", "c/b", false},
	} {
		if got := PathsOverlap(tc.a, tc.b); got != tc.want {
			t.Errorf("PathsOverlap(%q, %q) = %v", tc.a, tc.b, got)
		}
	}
}

func TestPatchWorkspaces(t *testing.T) {
	c, err := Patch(Defaults(), json.RawMessage(`{"workspaces":{"a":{"path":"acme/a"},"b":{"path":"acme/b","env":{"LOG_LEVEL":{"value":"debug"}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if a := c.Workspaces["a"]; a.Path != "acme/a" || a.Repo != "" || len(a.Env) != 0 || a.Instructions.Shared {
		t.Errorf("a = %+v (the template's env must not be shared)", a)
	}
	if b := c.Workspaces["b"]; b.Env["LOG_LEVEL"] != (WorkspaceEnv{Value: "debug"}) {
		t.Errorf("b = %+v", b)
	}
	c, err = Patch(c, json.RawMessage(`{"workspaces":{"c":{"path":"acme/c"},"b":{"env":{"LOG_LEVEL":{"value":"","secret":true}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Workspaces["c"].Env) != 0 || c.Workspaces["b"].Env["LOG_LEVEL"] != (WorkspaceEnv{Secret: true}) {
		t.Errorf("after the second patch = %+v", c.Workspaces)
	}
	if env := userMaps["workspaces"]["env"].(map[string]any); len(env) != 0 {
		t.Errorf("the template was modified: %v", env)
	}
	c, err = Patch(c, json.RawMessage(`{"workspaces":{"a":null,"b":{"env":{"LOG_LEVEL":null}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Workspaces["a"]; ok || len(c.Workspaces["b"].Env) != 0 {
		t.Errorf("after removal = %+v", c.Workspaces)
	}
	if _, err := Patch(c, json.RawMessage(`{"workspaces":{"b":{"env":{"X":{"bogus":1}}}}}`)); err == nil ||
		!strings.Contains(err.Error(), "workspaces.b.env.X.bogus: is not a setting") {
		t.Errorf("unknown env field = %v", err)
	}
}

func TestNormalizeFillsWorkspaceEnv(t *testing.T) {
	c := Defaults()
	c.Workspaces = map[string]Workspace{"a": {Path: "a"}}
	c.normalize()
	if c.Workspaces["a"].Env == nil {
		t.Error("nil env must become an empty map")
	}
	c.Workspaces = nil
	c.normalize()
	if c.Workspaces == nil {
		t.Error("nil workspaces must become an empty map")
	}
}
