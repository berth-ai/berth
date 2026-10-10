package broker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berth-ai/berth/internal/config"
)

const skillCommit = "0123456789abcdef0123456789abcdef01234567"

const addPDF = `{"base_revision":0,"patch":{"skills":{"sources":{"anthropics-skills":{"repo":"https://github.com/anthropics/skills","ref":"main","path":"skills","commit":"` + skillCommit + `","found":{"pdf":{"path":"skills/pdf","description":"PDF tools"}},"attached":{"pdf":{}}}}}}}`

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

// publishedCommit is the commit skills.json holds for addPDF's source.
func publishedCommit(tb *testBroker) string {
	s, _ := config.ReadSkillsFile(tb.b.SkillsSettings)
	return s["anthropics-skills"].Commit
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
	if got := publishedCommit(tb); got != skillCommit {
		t.Errorf("skills.json commit = %q", got)
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
	tb.verb(t, "config.apply", `{"base_revision":1,"patch":{"skills":{"sources":{"anthropics-skills":{"attached":{"pdf":{"enabled":false}}}}}}}`)
	waitSkillRuns(t, tb, 2)
}

func TestRecoveryDoesNotPublishSkills(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	tb.verb(t, "config.apply", addPDF)
	waitSkillRuns(t, tb, 1)
	os.Remove(tb.b.SkillsSettings)
	tb.b.Load()
	if publishedCommit(tb) == "" {
		t.Errorf("Load did not republish")
	}
	os.WriteFile(filepath.Join(tb.dir, "config.json"), []byte(`{"schema":`), 0o600)
	tb.b.Load()
	if publishedCommit(tb) == "" {
		t.Errorf("recovery overwrote skills.json")
	}
}

func TestRevertRepublishesSkills(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	tb.verb(t, "config.apply", addPDF)
	waitSkillRuns(t, tb, 1)
	tb.verb(t, "config.apply", `{"base_revision":1,"patch":{"skills":{"sources":{"anthropics-skills":null}}}}`)
	waitSkillRuns(t, tb, 2)
	if code, out := tb.verb(t, "config.revert", `{}`); code != 200 {
		t.Fatalf("revert = %d %v", code, out)
	}
	if got := publishedCommit(tb); got != skillCommit {
		t.Errorf("skills.json after revert = %q", got)
	}
	waitSkillRuns(t, tb, 3)
}

func skillJob(t *testing.T, tb *testBroker, verb, body string) Job {
	t.Helper()
	code, out := tb.verb(t, verb, body)
	if code != 202 {
		t.Fatalf("%s = %d %v", verb, code, out)
	}
	return waitSkillJob(t, tb, int(out["job"].(map[string]any)["id"].(float64)))
}

func waitSkillJob(t *testing.T, tb *testBroker, id int) Job {
	t.Helper()
	for range 500 {
		if j, _ := tb.b.Jobs.Get(id); j.State != StateRunning {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %d did not finish", id)
	return Job{}
}

const skillCommit2 = "89abcdef0123456789abcdef0123456789abcdef"

// fakeScan makes Home.Berth answer `skills scan`, logging its arguments to
// scan.log. Ref nope is not found; ref v2 is a new commit of pdf and docx;
// at ref renamed skills/pdf holds skill other; at ref moved only docx is
// left; ref slow is v2 after half a second. Any other ref is skillCommit
// with root (path ""), a broken skill, docx and pdf.
func fakeScan(t *testing.T, tb *testBroker) {
	t.Helper()
	row := func(path, name, desc string) string {
		return `{"path":"` + path + `","name":"` + name + `","description":"` + desc + `"}`
	}
	at := func(commit string, rows ...string) string {
		return `echo '{"schema":1,"commit":"` + commit + `","skills":[` + strings.Join(rows, ",") + `]}'`
	}
	v2 := at(skillCommit2, row("skills/docx", "docx", "Word v2"), row("skills/pdf", "pdf", "PDF tools v2"))
	script := `#!/bin/sh
echo "$*" >> "` + tb.dir + `/scan.log"
case "$*" in
  *"--ref nope"*) echo '{"schema":1,"error":{"code":"ref_not_found","step":"ref","message":"Ref nope not found in that repository"}}'; exit 1 ;;
  *"--ref v2"*) ` + v2 + ` ;;
  *"--ref slow"*) sleep 0.5; ` + v2 + ` ;;
  *"--ref renamed"*) ` + at(skillCommit, row("skills/docx", "docx", "Word"), row("skills/pdf", "other", "Other")) + ` ;;
  *"--ref moved"*) ` + at(skillCommit, row("skills/docx", "docx", "Word")) + ` ;;
  *) ` + at(skillCommit, row("", "root", "Root"), `{"path":"skills/bad","error":"SKILL.md has no description"}`,
		row("skills/docx", "docx", "Word"), row("skills/pdf", "pdf", "PDF tools")) + ` ;;
esac
`
	path := filepath.Join(tb.dir, "fake-berth")
	os.WriteFile(path, []byte(script), 0o755)
	tb.b.Home.Berth = path
}

func lastScan(t *testing.T, tb *testBroker) string {
	t.Helper()
	raw, _ := os.ReadFile(filepath.Join(tb.dir, "scan.log"))
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	return lines[len(lines)-1]
}
