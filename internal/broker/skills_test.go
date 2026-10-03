package broker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berth-ai/berth/internal/config"
)

const skillCommit = "0123456789abcdef0123456789abcdef01234567"

const addPDF = `{"base_revision":0,"patch":{"skills":{"pdf":{"repo":"https://github.com/anthropics/skills","path":"skills/pdf","ref":"main","commit":"` + skillCommit + `","description":"PDF tools"}}}}`

// withSkills is withAgents plus a fake skills converger and skills.json path.
func withSkills(t *testing.T, tb *testBroker) {
	t.Helper()
	withAgents(t, tb)
	sync := filepath.Join(tb.dir, "skills-sync")
	os.WriteFile(sync, []byte("#!/bin/sh\necho synced >> \""+tb.dir+"/skills.log\"\n"), 0o755)
	tb.b.SkillsSync = []string{sync}
	tb.b.SkillsSettings = filepath.Join(tb.dir, "skills.json")
	tb.b.Load()
}

func skillRuns(tb *testBroker) int {
	raw, _ := os.ReadFile(filepath.Join(tb.dir, "skills.log"))
	return strings.Count(string(raw), "synced\n")
}

func waitSkillRuns(t *testing.T, tb *testBroker, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		idle := true
		for _, j := range tb.b.Jobs.List() {
			idle = idle && j.State != StateRunning
		}
		if skillRuns(tb) >= n && idle {
			if got := skillRuns(tb); got != n {
				t.Fatalf("skills sync ran %d times, want %d", got, n)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("skills sync ran %d times, want %d: %+v", skillRuns(tb), n, tb.b.Jobs.List())
}

func TestSkillChangePublishesAndSyncs(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	if code, out := tb.verb(t, "config.apply", addPDF); code != 200 {
		t.Fatalf("apply = %d %v", code, out)
	}
	s, err := config.ReadSkillsFile(tb.b.SkillsSettings)
	if err != nil || s["pdf"].Commit != skillCommit {
		t.Errorf("skills.json = %+v, %v", s, err)
	}
	if fi, _ := os.Stat(tb.b.SkillsSettings); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v", fi.Mode())
	}
	waitSkillRuns(t, tb, 1)
	waitMCPRuns(t, tb, 0) // an MCP-only converger is not run for a skills change
	if mcpRuns(tb) != 0 {
		t.Errorf("mcp ran %d times", mcpRuns(tb))
	}
}

func TestUnrelatedChangeDoesNotSync(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	if code, out := tb.verb(t, "config.apply", `{"base_revision":0,"patch":{"runtime":{"tz":"Europe/Rome"}}}`); code != 200 {
		t.Fatalf("apply = %d %v", code, out)
	}
	// refreshAgents starts jobs before the verb returns.
	for _, j := range tb.b.Jobs.List() {
		if j.Verb == "skills.sync" {
			t.Errorf("skills.sync started: %+v", j)
		}
	}
}

func TestDisablingAnAgentSyncsSkills(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	tb.verb(t, "config.apply", `{"base_revision":0,"patch":{"agents":{"disabled":["agent-a"]}}}`)
	waitSkillRuns(t, tb, 1)
}

func TestSkillChangeDuringSyncRunsItAgain(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	slow := filepath.Join(tb.dir, "slow-sync")
	os.WriteFile(slow, []byte("#!/bin/sh\nsleep 0.3\necho synced >> \""+tb.dir+"/skills.log\"\n"), 0o755)
	tb.b.SkillsSync = []string{slow}
	tb.verb(t, "config.apply", addPDF)
	tb.verb(t, "config.apply", `{"base_revision":1,"patch":{"skills":{"pdf":{"enabled":false}}}}`)
	waitSkillRuns(t, tb, 2)
}

func TestRecoveryDoesNotPublishSkills(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	tb.verb(t, "config.apply", addPDF)
	waitSkillRuns(t, tb, 1)
	os.Remove(tb.b.SkillsSettings)
	tb.b.Load()
	if s, _ := config.ReadSkillsFile(tb.b.SkillsSettings); s["pdf"].Commit == "" {
		t.Errorf("Load did not republish: %+v", s)
	}
	os.WriteFile(filepath.Join(tb.dir, "config.json"), []byte(`{"schema":`), 0o600)
	tb.b.Load()
	if s, _ := config.ReadSkillsFile(tb.b.SkillsSettings); s["pdf"].Commit == "" {
		t.Errorf("recovery overwrote skills.json: %+v", s)
	}
}

func TestRevertRepublishesSkills(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	tb.verb(t, "config.apply", addPDF)
	waitSkillRuns(t, tb, 1)
	tb.verb(t, "config.apply", `{"base_revision":1,"patch":{"skills":{"pdf":null}}}`)
	waitSkillRuns(t, tb, 2)
	if code, out := tb.verb(t, "config.revert", `{}`); code != 200 {
		t.Fatalf("revert = %d %v", code, out)
	}
	if s, _ := config.ReadSkillsFile(tb.b.SkillsSettings); s["pdf"].Commit != skillCommit {
		t.Errorf("skills.json after revert = %+v", s)
	}
	waitSkillRuns(t, tb, 3)
}

// fakeResolve makes Home.Berth answer `skills resolve`: skill pdf at
// skillCommit, ref "nope" not found, ref "other" holding skill other, and
// ref "v2" a new commit of pdf.
func fakeResolve(t *testing.T, tb *testBroker) {
	t.Helper()
	script := `#!/bin/sh
case "$*" in
  *"--ref nope"*) echo '{"schema":1,"error":{"code":"ref_not_found","step":"ref","message":"Ref nope not found in that repository"}}'; exit 1 ;;
  *"--ref other"*) echo '{"schema":1,"name":"other","description":"Other","commit":"` + skillCommit + `"}' ;;
  *"--ref v2"*) echo '{"schema":1,"name":"pdf","description":"PDF tools v2","commit":"89abcdef0123456789abcdef0123456789abcdef"}' ;;
  *) echo '{"schema":1,"name":"pdf","description":"PDF tools","commit":"` + skillCommit + `"}' ;;
esac
echo "$*" >> "` + tb.dir + `/resolve.log"
`
	path := filepath.Join(tb.dir, "fake-berth")
	os.WriteFile(path, []byte(script), 0o755)
	tb.b.Home.Berth = path
}

func skillJob(t *testing.T, tb *testBroker, verb, body string) Job {
	t.Helper()
	code, out := tb.verb(t, verb, body)
	if code != 202 {
		t.Fatalf("%s = %d %v", verb, code, out)
	}
	id := int(out["job"].(map[string]any)["id"].(float64))
	for range 500 {
		if j, _ := tb.b.Jobs.Get(id); j.State != StateRunning {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %d did not finish", id)
	return Job{}
}

func TestSkillsAdd(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	fakeResolve(t, tb)
	j := skillJob(t, tb, "skills.add", `{"repo":"https://github.com/anthropics/skills","path":"skills/pdf","ref":"main","agents":["agent-a"]}`)
	if j.State != StateSucceeded || !strings.Contains(string(j.Result), `"skill":"pdf"`) {
		t.Fatalf("job = %+v %s", j, j.Result)
	}
	_, out := tb.verb(t, "config.get", `{}`)
	pdf := out["config"].(map[string]any)["skills"].(map[string]any)["pdf"].(map[string]any)
	if pdf["commit"] != skillCommit || pdf["enabled"] != true || pdf["description"] != "PDF tools" {
		t.Errorf("pdf = %v", pdf)
	}
	if a, _ := pdf["agents"].([]any); len(a) != 1 || a[0] != "agent-a" {
		t.Errorf("agents = %v", pdf["agents"])
	}
	waitSkillRuns(t, tb, 1)
	raw, _ := os.ReadFile(filepath.Join(tb.dir, "resolve.log"))
	if !strings.Contains(string(raw), "skills resolve --json --repo https://github.com/anthropics/skills --ref main --path skills/pdf") {
		t.Errorf("resolve args = %s", raw)
	}

	again := skillJob(t, tb, "skills.add", `{"repo":"https://github.com/anthropics/skills","path":"skills/pdf","ref":"main"}`)
	if again.State != StateFailed || !strings.Contains(string(again.Result), "already installed") {
		t.Errorf("duplicate = %+v %s", again, again.Result)
	}
}

func TestSkillsAddResolveFailure(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	fakeResolve(t, tb)
	j := skillJob(t, tb, "skills.add", `{"repo":"https://github.com/anthropics/skills","path":"skills/pdf","ref":"nope"}`)
	if j.State != StateFailed || !strings.Contains(string(j.Result), `"field":"ref"`) ||
		!strings.Contains(string(j.Result), "Ref nope not found") {
		t.Errorf("job = %+v %s", j, j.Result)
	}
	if _, out := tb.verb(t, "config.get", `{}`); len(out["config"].(map[string]any)["skills"].(map[string]any)) != 0 {
		t.Error("a failed resolve saved a skill")
	}
}

func TestSkillsAddValidation(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	for body, field := range map[string]string{
		`{"repo":"http://x.example/r","ref":"main"}`:                "repo",
		`{"repo":"https://x.example/r","path":"../x","ref":"main"}`: "path",
		`{"repo":"https://x.example/r","ref":"-x"}`:                 "ref",
		`{"repo":"https://x.example/r","ref":"main","agents":[]}`:   "agents",
	} {
		code, out := tb.verb(t, "skills.add", body)
		if code != 400 || !strings.Contains(fmt.Sprint(out), "field:"+field) {
			t.Errorf("%s = %d %v", body, code, out)
		}
	}
}

func TestSkillsUpdate(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	fakeResolve(t, tb)
	skillJob(t, tb, "skills.add", `{"repo":"https://github.com/anthropics/skills","path":"skills/pdf","ref":"main"}`)
	same := skillJob(t, tb, "skills.update", `{"id":"pdf"}`)
	if same.State != StateSucceeded || !strings.Contains(string(same.Result), `"changed":false`) {
		t.Errorf("unchanged = %s", same.Result)
	}
	moved := skillJob(t, tb, "skills.update", `{"id":"pdf","ref":"v2"}`)
	if moved.State != StateSucceeded || !strings.Contains(string(moved.Result), `"changed":true`) {
		t.Errorf("moved = %s", moved.Result)
	}
	_, out := tb.verb(t, "config.get", `{}`)
	pdf := out["config"].(map[string]any)["skills"].(map[string]any)["pdf"].(map[string]any)
	if pdf["ref"] != "v2" || pdf["description"] != "PDF tools v2" {
		t.Errorf("pdf = %v", pdf)
	}
	renamed := skillJob(t, tb, "skills.update", `{"id":"pdf","ref":"other"}`)
	if renamed.State != StateFailed || !strings.Contains(string(renamed.Result), "holds skill other, not pdf") {
		t.Errorf("renamed = %s", renamed.Result)
	}
	if code, _ := tb.verb(t, "skills.update", `{"id":"nothere"}`); code != 404 {
		t.Errorf("unknown = %d", code)
	}
}
