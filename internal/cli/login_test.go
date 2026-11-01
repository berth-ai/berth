package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLoginStatusWithoutAgentSet(t *testing.T) {
	cfg := testConfig(t)
	t.Setenv("HOME", t.TempDir())
	out, errOut, code := run(t, cfg, "login", "status", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	golden(t, "login_status", out)
	var doc struct {
		Agents map[string]any `json:"agents"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil || doc.Agents == nil || len(doc.Agents) != 0 {
		t.Errorf("doc = %+v, %v", doc, err)
	}
	human, _, code := run(t, cfg, "login", "status")
	if code != 0 || !strings.Contains(human, "No agent with a sign-in is switched on.") {
		t.Errorf("human = %q (exit %d)", human, code)
	}
}

func TestLoginUnknownAgent(t *testing.T) {
	cfg := testConfig(t)
	t.Setenv("HOME", t.TempDir())
	out, _, code := run(t, cfg, "login", "logout", "nope", "m", "--json")
	if code != 2 || !strings.Contains(out, `"code": "unknown"`) {
		t.Errorf("logout unknown = %d %s", code, out)
	}
	out, _, code = run(t, cfg, "login", "run", "nope", "m")
	if code != 2 || !strings.Contains(out, "berth: login failed: ") {
		t.Errorf("run unknown = %d %q", code, out)
	}
}

func TestLoginUsage(t *testing.T) {
	cfg := testConfig(t)
	t.Setenv("HOME", t.TempDir())
	for _, args := range [][]string{{"login"}, {"login", "list"}, {"login", "logout", "a"}, {"login", "run", "a"}, {"login", "status", "extra"}} {
		if _, _, code := run(t, cfg, args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
	out, _, _ := run(t, cfg, "help")
	if !strings.Contains(out, "login status|logout") || strings.Contains(out, "login run") {
		t.Errorf("help:\n%s", out)
	}
}

func TestUsageWithoutAgentSet(t *testing.T) {
	cfg := testConfig(t)
	t.Setenv("HOME", t.TempDir())
	out, errOut, code := run(t, cfg, "usage", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	golden(t, "usage", out)
	human, _, code := run(t, cfg, "usage")
	if code != 0 || !strings.Contains(human, "No agent is installed.") {
		t.Errorf("human = %q (exit %d)", human, code)
	}
	if _, _, code := run(t, cfg, "usage", "extra"); code != 2 {
		t.Errorf("usage extra: exit %d, want 2", code)
	}
	help, _, _ := run(t, cfg, "help")
	if !strings.Contains(help, "usage [--json]") {
		t.Errorf("help:\n%s", help)
	}
}
