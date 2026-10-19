package broker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// withInstructions points tb at a fake berth whose instructions command
// logs its argv and stdin and answers like the real one.
func withInstructions(t *testing.T, tb *testBroker) (string, string) {
	t.Helper()
	withAgents(t, tb)
	log, stdin := filepath.Join(tb.dir, "berth.log"), filepath.Join(tb.dir, "stdin")
	script := `#!/bin/sh
echo "$*" >> "` + log + `"
case "$2" in
instructions)
  case "$5" in
  write) cat > "` + stdin + `"
    if [ -n "$FAKE_STALE" ]; then
      printf '{"error":{"code":"changed_on_disk","step":"file","message":"CLAUDE.md changed on disk since you opened it"},"schema":1}'; exit 1
    fi
    printf '{"file":"CLAUDE.md","sha256":"abc","schema":1}' ;;
  read) printf '{"file":"CLAUDE.md","state":"missing","content":"","sha256":"","size":0,"editable":true,"tracked":false,"schema":1}' ;;
  merge) printf '{"file":"CLAUDE.md","merged":true,"schema":1}' ;;
  esac ;;
esac
`
	p := filepath.Join(tb.dir, "berth-fake")
	os.WriteFile(p, []byte(script), 0o755)
	tb.b.Home = HomeCommands{Berth: p}
	tb.b.WorkspacesSettings = filepath.Join(tb.dir, "workspaces.json")
	tb.b.Load()
	// agent-a reads CLAUDE.md.
	tb.setInstructions(t, "agent-a", "CLAUDE.md")
	addWorkspace(t, tb, "api", "acme/api")
	return log, stdin
}

func TestInstructionsVerbs(t *testing.T) {
	tb := newTestBroker(t)
	log, stdin := withInstructions(t, tb)
	code, out := tb.verb(t, "workspaces.instructions.read", `{"id":"api","file":"CLAUDE.md"}`)
	if code != 200 || out["file"].(map[string]any)["state"] != "missing" {
		t.Fatalf("read = %d %v", code, out)
	}
	body, _ := json.Marshal(map[string]any{"id": "api", "file": "CLAUDE.md", "content": "# rules\n$(x)", "base_sha256": ""})
	if code, out := tb.verb(t, "workspaces.instructions.write", string(body)); code != 200 || out["sha256"] != "abc" {
		t.Fatalf("write = %d %v", code, out)
	}
	if raw, _ := os.ReadFile(stdin); string(raw) != "# rules\n$(x)" {
		t.Errorf("stdin = %q", raw)
	}
	for _, c := range calls(log) {
		if strings.Contains(c, "rules") {
			t.Errorf("content reached argv: %s", c)
		}
	}
	if got := calls(log); !slices.Contains(got, "workspaces instructions --json --action write --id api --file CLAUDE.md --base-sha256=") {
		t.Errorf("calls = %v", got)
	}
	t.Setenv("FAKE_STALE", "1")
	if code, out := tb.verb(t, "workspaces.instructions.write", string(body)); code != 409 || !strings.Contains(jsonOf(out), "changed_on_disk") {
		t.Errorf("stale = %d %v", code, out)
	}
}

func TestInstructionsVerbsRefuse(t *testing.T) {
	tb := newTestBroker(t)
	withInstructions(t, tb)
	big := strings.Repeat("x", 256<<10+1)
	for body, want := range map[string]string{
		`{"id":"nope","file":"CLAUDE.md"}`:                                         "unknown_workspace",
		`{"id":"api","file":"README.md"}`:                                          "not_allowed",
		`{"id":"api","file":"../CLAUDE.md"}`:                                       "not_allowed",
		`{"id":"api","file":"CLAUDE.md","content":"` + big + `","base_sha256":""}`: "too_large",
	} {
		verb := "workspaces.instructions.read"
		if strings.Contains(body, "content") {
			verb = "workspaces.instructions.write"
		}
		if _, out := tb.verb(t, verb, body); !strings.Contains(jsonOf(out), want) {
			t.Errorf("%s %.60s = %v", verb, body, out)
		}
	}
	// AGENTS.md is allowed while shared even when no agent names it.
	cur, _, _ := tb.b.snapshot()
	tb.verb(t, "config.apply", `{"base_revision":`+itoa(cur.Revision)+`,"patch":{"workspaces":{"api":{"instructions":{"shared":true}}}}}`)
	if code, out := tb.verb(t, "workspaces.instructions.read", `{"id":"api","file":"AGENTS.md"}`); code != 200 {
		t.Errorf("AGENTS.md while shared = %d %v", code, out)
	}
}

func TestBodyLimit(t *testing.T) {
	if bodyLimit("workspaces.instructions.write") != 2<<20 || bodyLimit("config.apply") != 64<<10 {
		t.Error("limits")
	}
}

// setInstructions gives agent dep of the withAgents set an instructions
// descriptor naming file.
func (tb *testBroker) setInstructions(t *testing.T, dep, file string) {
	t.Helper()
	p := filepath.Join(tb.dir, "agents", "versions", "0123456789ab", "package.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var pkg map[string]any
	if err := json.Unmarshal(raw, &pkg); err != nil {
		t.Fatal(err)
	}
	pkg["berthAgents"] = map[string]any{"schema": 1, "agents": map[string]any{dep: map[string]any{"instructions": file}}}
	raw, _ = json.Marshal(pkg)
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}
