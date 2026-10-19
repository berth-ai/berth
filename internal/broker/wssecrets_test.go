package broker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berth-ai/berth/internal/config"
)

// withWorkspaceSecrets gives tb a secrets folder and workspace api with a
// plain LOG and a secret KEY.
func withWorkspaceSecrets(t *testing.T, tb *testBroker) string {
	t.Helper()
	dir := filepath.Join(tb.dir, "secrets", "workspaces")
	tb.b.WorkspaceSecrets = dir
	tb.b.Load()
	code, out := tb.verb(t, "config.apply", `{"base_revision":0,"patch":{"workspaces":{"api":{"path":"acme/api",`+
		`"env":{"LOG":{"value":"debug"},"KEY":{"secret":true}}}}}}`)
	if code != 200 {
		t.Fatalf("apply = %d %v", code, out)
	}
	return dir
}

func TestWorkspaceSecretSetAndReported(t *testing.T) {
	tb := newTestBroker(t)
	dir := withWorkspaceSecrets(t, tb)
	val := "multi\nline $(x)"
	raw, _ := json.Marshal(map[string]any{"secrets": map[string]string{"workspaces.api.KEY": val}})
	if code, out := tb.verb(t, "config.apply", string(raw)); code != 200 {
		t.Fatalf("set = %d %v", code, out)
	}
	env, _ := config.ReadWorkspaceSecrets(dir, "api")
	if env["KEY"] != val {
		t.Errorf("file = %v", env)
	}
	_, out := tb.verb(t, "config.get", `{}`)
	s, _ := out["secrets"].(map[string]any)["workspaces.api.KEY"].(map[string]any)
	if s["set"] != true {
		t.Errorf("secrets = %v", out["secrets"])
	}
	if b, _ := json.Marshal(out); strings.Contains(string(b), "multi") {
		t.Error("config.get leaked the value")
	}
	if code, _ := tb.verb(t, "config.apply", `{"secrets":{"workspaces.api.KEY":null}}`); code != 200 {
		t.Fatal("clear failed")
	}
	if _, err := os.Stat(filepath.Join(dir, "api.json")); !os.IsNotExist(err) {
		t.Errorf("clearing the last secret kept the file: %v", err)
	}
}

func TestWorkspaceSecretRefusals(t *testing.T) {
	tb := newTestBroker(t)
	withWorkspaceSecrets(t, tb)
	for body, field := range map[string]string{
		`{"secrets":{"workspaces.api.LOG":"x"}}`:       "secrets.workspaces.api.LOG",  // plain, not secret
		`{"secrets":{"workspaces.nope.KEY":"x"}}`:      "secrets.workspaces.nope.KEY", // no such workspace
		`{"secrets":{"workspaces.api.KEY":"a\u0000"}}`: "secrets.workspaces.api.KEY",  // NUL
	} {
		code, out := tb.verb(t, "config.apply", body)
		if code != 400 || !strings.Contains(jsonOf(out), field) {
			t.Errorf("%s = %d %v", body, code, out)
		}
	}
}

func TestWorkspaceSecretsPrunedOnForget(t *testing.T) {
	tb := newTestBroker(t)
	dir := withWorkspaceSecrets(t, tb)
	tb.verb(t, "config.apply", `{"secrets":{"workspaces.api.KEY":"v"}}`)
	// Turning KEY plain drops its secret.
	cur, _, _ := tb.b.snapshot()
	tb.verb(t, "config.apply", `{"base_revision":`+itoa(cur.Revision)+`,"patch":{"workspaces":{"api":{"env":{"KEY":{"secret":false,"value":"p"}}}}}}`)
	if env, _ := config.ReadWorkspaceSecrets(dir, "api"); len(env) != 0 {
		t.Errorf("a plain variable kept its secret: %v", env)
	}
	cur, _, _ = tb.b.snapshot()
	tb.verb(t, "config.apply", `{"base_revision":`+itoa(cur.Revision)+`,"patch":{"workspaces":{"api":{"env":{"KEY":{"secret":true,"value":""}}}}}}`)
	tb.verb(t, "config.apply", `{"secrets":{"workspaces.api.KEY":"v2"}}`)
	// Forgetting the workspace deletes the file; the same id added again has no secret.
	cur, _, _ = tb.b.snapshot()
	tb.verb(t, "config.apply", `{"base_revision":`+itoa(cur.Revision)+`,"patch":{"workspaces":{"api":null}}}`)
	if _, err := os.Stat(filepath.Join(dir, "api.json")); !os.IsNotExist(err) {
		t.Errorf("forget kept the secrets file: %v", err)
	}
	cur, _, _ = tb.b.snapshot()
	tb.verb(t, "config.apply", `{"base_revision":`+itoa(cur.Revision)+`,"patch":{"workspaces":{"api":{"path":"acme/api","env":{"KEY":{"secret":true}}}}}}`)
	_, out := tb.verb(t, "config.get", `{}`)
	if s := out["secrets"].(map[string]any)["workspaces.api.KEY"].(map[string]any); s["set"] != false {
		t.Errorf("an old secret came back: %v", s)
	}
	// Load repairs a stale file.
	config.WriteWorkspaceSecrets(dir, "gone", map[string]string{"X": "y"}, -1, -1)
	tb.b.Load()
	if _, err := os.Stat(filepath.Join(dir, "gone.json")); !os.IsNotExist(err) {
		t.Errorf("Load kept a forgotten workspace's secrets: %v", err)
	}
}

func jsonOf(v any) string { b, _ := json.Marshal(v); return string(b) }
