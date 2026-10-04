package broker

import (
	"encoding/json"
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

func skillsOf(t *testing.T, tb *testBroker) (map[string]any, float64) {
	t.Helper()
	_, out := tb.verb(t, "config.get", `{}`)
	cfg := out["config"].(map[string]any)
	return cfg["skills"].(map[string]any), cfg["revision"].(float64)
}

func TestSkillsScan(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	fakeScan(t, tb)
	tb.verb(t, "config.apply", addPDF)
	j := skillJob(t, tb, "skills.scan", `{"repo":"https://github.com/anthropics/skills","ref":"main","path":"skills"}`)
	if j.State != StateSucceeded {
		t.Fatalf("job = %+v %s", j, j.Result)
	}
	var res struct {
		Commit string
		Skills []struct {
			Path, Name, Error string
			Installed         bool
		}
	}
	json.Unmarshal(j.Result, &res)
	if res.Commit != skillCommit || len(res.Skills) != 4 {
		t.Fatalf("result = %s", j.Result)
	}
	for _, r := range res.Skills {
		if r.Installed != (r.Name == "pdf") {
			t.Errorf("row %+v: installed = %v", r, r.Installed)
		}
	}
	if !strings.Contains(string(j.Result), `"error":"SKILL.md has no description"`) {
		t.Errorf("bad row lost: %s", j.Result)
	}
	if got := lastScan(t, tb); got != "skills scan --json --repo https://github.com/anthropics/skills --ref main --path skills" {
		t.Errorf("scan args = %s", got)
	}
	if _, rev := skillsOf(t, tb); rev != 1 {
		t.Errorf("a scan changed the config: revision %v", rev)
	}
	skillJob(t, tb, "skills.scan", `{"repo":"https://github.com/anthropics/skills","ref":"main"}`)
	if got := lastScan(t, tb); strings.Contains(got, "--path") {
		t.Errorf("root scan args = %s", got)
	}
}

func TestSkillsScanFailure(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	fakeScan(t, tb)
	j := skillJob(t, tb, "skills.scan", `{"repo":"https://github.com/anthropics/skills","ref":"nope"}`)
	if j.State != StateFailed || !strings.Contains(string(j.Result), `"field":"ref"`) ||
		!strings.Contains(string(j.Result), "Ref nope not found") {
		t.Errorf("job = %+v %s", j, j.Result)
	}
}

func TestSkillsScanValidation(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	for body, field := range map[string]string{
		`{"repo":"http://x.example/r","ref":"main"}`:                "repo",
		`{"repo":"https://x.example/r","ref":"-x"}`:                 "ref",
		`{"repo":"https://x.example/r","ref":"main","path":"../x"}`: "path",
	} {
		code, out := tb.verb(t, "skills.scan", body)
		if code != 400 || !strings.Contains(fmt.Sprint(out), "field:"+field) {
			t.Errorf("%s = %d %v", body, code, out)
		}
	}
}

func TestSkillsAddBatch(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	fakeScan(t, tb)
	j := skillJob(t, tb, "skills.add", `{"repo":"https://github.com/anthropics/skills","ref":"main","commit":"`+skillCommit+
		`","paths":["skills/pdf","skills/docx"],"agents":["agent-a"]}`)
	if j.State != StateSucceeded || !strings.Contains(string(j.Result), `"skills":["docx","pdf"]`) ||
		!strings.Contains(string(j.Result), `"revision":1`) {
		t.Fatalf("job = %+v %s", j, j.Result)
	}
	if got := lastScan(t, tb); got != "skills scan --json --repo https://github.com/anthropics/skills --ref "+skillCommit+" --path skills/pdf --path skills/docx" {
		t.Errorf("scan args = %s", got)
	}
	skills, rev := skillsOf(t, tb)
	if rev != 1 || len(skills) != 2 {
		t.Fatalf("revision %v, skills %v", rev, skills)
	}
	for id, desc := range map[string]string{"pdf": "PDF tools", "docx": "Word"} {
		s := skills[id].(map[string]any)
		if s["commit"] != skillCommit || s["ref"] != "main" || s["path"] != "skills/"+id || s["description"] != desc || s["enabled"] != true {
			t.Errorf("%s = %v", id, s)
		}
		if a, _ := s["agents"].([]any); len(a) != 1 || a[0] != "agent-a" {
			t.Errorf("%s agents = %v", id, s["agents"])
		}
	}
	waitSkillRuns(t, tb, 1)
}

func TestSkillsAddRootSkill(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	fakeScan(t, tb)
	j := skillJob(t, tb, "skills.add", `{"repo":"https://github.com/anthropics/skills","ref":"main","commit":"`+skillCommit+`","paths":["","skills/pdf"]}`)
	if j.State != StateSucceeded {
		t.Fatalf("job = %+v %s", j, j.Result)
	}
	if got := lastScan(t, tb); strings.Contains(got, "--path") {
		t.Errorf("a root skill must scan the whole tree: %s", got)
	}
	skills, _ := skillsOf(t, tb)
	if root, ok := skills["root"].(map[string]any); !ok || root["path"] != "" {
		t.Errorf("root = %v", skills["root"])
	}
}

func TestSkillsAddFailsWholeBatch(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	fakeScan(t, tb)
	tb.verb(t, "config.apply", addPDF)
	add := func(paths string) Job {
		return skillJob(t, tb, "skills.add", `{"repo":"https://github.com/anthropics/skills","ref":"main","commit":"`+skillCommit+`","paths":`+paths+`}`)
	}
	for paths, want := range map[string]string{
		`["skills/docx","skills/pdf"]`:  `"code":"skill_exists"`,
		`["skills/docx","skills/bad"]`:  `"code":"bad_skill"`,
		`["skills/docx","skills/none"]`: `"code":"no_skill"`,
	} {
		j := add(paths)
		if j.State != StateFailed || !strings.Contains(string(j.Result), want) || !strings.Contains(string(j.Result), `"field":"path"`) {
			t.Errorf("%s: %+v %s", paths, j, j.Result)
		}
	}
	if j := add(`["skills/docx","skills/pdf"]`); !strings.Contains(string(j.Result), "A skill named pdf is already installed") {
		t.Errorf("message = %s", j.Result)
	}
	if skills, rev := skillsOf(t, tb); rev != 1 || len(skills) != 1 {
		t.Errorf("a failed batch saved something: revision %v, %v", rev, skills)
	}
}

func TestSkillsAddValidation(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	many := make([]string, maxSkillBatch+1)
	for i := range many {
		many[i] = fmt.Sprintf(`"s%d"`, i)
	}
	base := `"repo":"https://x.example/r","ref":"main","commit":"` + skillCommit + `"`
	for body, field := range map[string]string{
		`{"repo":"http://x.example/r","ref":"main","commit":"` + skillCommit + `","paths":["a"]}`: "repo",
		`{"repo":"https://x.example/r","ref":"-x","commit":"` + skillCommit + `","paths":["a"]}`:  "ref",
		`{"repo":"https://x.example/r","ref":"main","commit":"abc","paths":["a"]}`:                "commit",
		`{"repo":"https://x.example/r","ref":"main","paths":["a"]}`:                               "commit",
		`{` + base + `,"paths":[]}`:                                "paths",
		`{` + base + `}`:                                           "paths",
		`{` + base + `,"paths":["../x"]}`:                          "paths",
		`{` + base + `,"paths":["a","a"]}`:                         "paths",
		`{` + base + `,"paths":[` + strings.Join(many, ",") + `]}`: "paths",
		`{` + base + `,"paths":["a"],"agents":[]}`:                 "agents",
	} {
		code, out := tb.verb(t, "skills.add", body)
		if code != 400 || !strings.Contains(fmt.Sprint(out), "field:"+field) {
			t.Errorf("%.80s = %d %v", body, code, out)
		}
	}
}

// addPair adds pdf and docx at main in one revision.
func addPair(t *testing.T, tb *testBroker) {
	t.Helper()
	j := skillJob(t, tb, "skills.add", `{"repo":"https://github.com/anthropics/skills","ref":"main","commit":"`+skillCommit+`","paths":["skills/pdf","skills/docx"]}`)
	if j.State != StateSucceeded {
		t.Fatalf("add = %s", j.Result)
	}
}

func TestSkillsUpdateGroup(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	fakeScan(t, tb)
	addPair(t, tb)
	same := skillJob(t, tb, "skills.update", `{"ids":["pdf","docx"]}`)
	if same.State != StateSucceeded || !strings.Contains(string(same.Result), `"changed":false`) || !strings.Contains(string(same.Result), `"skills":[]`) {
		t.Errorf("unchanged = %s", same.Result)
	}
	if got := lastScan(t, tb); got != "skills scan --json --repo https://github.com/anthropics/skills --ref main --path skills/docx --path skills/pdf" {
		t.Errorf("each skill's own path is scanned: %s", got)
	}
	moved := skillJob(t, tb, "skills.update", `{"ids":["pdf","docx"],"ref":"v2"}`)
	if moved.State != StateSucceeded || !strings.Contains(string(moved.Result), `"skills":["docx","pdf"]`) || !strings.Contains(string(moved.Result), `"revision":2`) {
		t.Fatalf("moved = %s", moved.Result)
	}
	skills, rev := skillsOf(t, tb)
	if rev != 2 {
		t.Errorf("one update, one revision: %v", rev)
	}
	for id, desc := range map[string]string{"pdf": "PDF tools v2", "docx": "Word v2"} {
		s := skills[id].(map[string]any)
		if s["ref"] != "v2" || s["commit"] != skillCommit2 || s["description"] != desc {
			t.Errorf("%s = %v", id, s)
		}
	}
}

func TestSkillsUpdateOne(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	fakeScan(t, tb)
	addPair(t, tb)
	j := skillJob(t, tb, "skills.update", `{"ids":["pdf"],"ref":"v2"}`)
	if j.State != StateSucceeded || !strings.Contains(string(j.Result), `"skills":["pdf"]`) {
		t.Fatalf("job = %s", j.Result)
	}
	skills, _ := skillsOf(t, tb)
	if docx := skills["docx"].(map[string]any); docx["ref"] != "main" || docx["commit"] != skillCommit {
		t.Errorf("docx moved too: %v", docx)
	}
}

func TestSkillsUpdateFailsWholeBatch(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	fakeScan(t, tb)
	addPair(t, tb)
	for ref, want := range map[string]string{
		"renamed": "skills/pdf in renamed holds skill other, not pdf",
		"moved":   "pdf is no longer at skills/pdf in moved; remove it or update the others alone",
	} {
		j := skillJob(t, tb, "skills.update", `{"ids":["docx","pdf"],"ref":"`+ref+`"}`)
		if j.State != StateFailed || !strings.Contains(string(j.Result), want) || !strings.Contains(string(j.Result), `"field":"ref"`) {
			t.Errorf("%s: %s", ref, j.Result)
		}
	}
	if skills, rev := skillsOf(t, tb); rev != 1 || skills["docx"].(map[string]any)["ref"] != "main" {
		t.Errorf("a failed batch saved something: revision %v, %v", rev, skills)
	}
}

func TestSkillsUpdateDoesNotResurrect(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	fakeScan(t, tb)
	addPair(t, tb)
	code, out := tb.verb(t, "skills.update", `{"ids":["pdf"],"ref":"slow"}`)
	if code != 202 {
		t.Fatalf("update = %d %v", code, out)
	}
	if code, out := tb.verb(t, "config.apply", `{"base_revision":1,"patch":{"skills":{"pdf":null}}}`); code != 200 {
		t.Fatalf("remove = %d %v", code, out)
	}
	j := waitSkillJob(t, tb, int(out["job"].(map[string]any)["id"].(float64)))
	if j.State != StateFailed || !strings.Contains(string(j.Result), `"code":"unknown_skill"`) {
		t.Errorf("job = %s", j.Result)
	}
	if skills, _ := skillsOf(t, tb); skills["pdf"] != nil {
		t.Errorf("pdf came back: %v", skills["pdf"])
	}
}

func TestSkillsUpdateValidation(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	fakeScan(t, tb)
	addPair(t, tb)
	tb.verb(t, "config.apply", `{"base_revision":1,"patch":{"skills":{"elsewhere":{"repo":"https://x.example/r","path":"","ref":"main","commit":"`+skillCommit+`","description":"E"}}}}`)
	for body, field := range map[string]string{
		`{"ids":[]}`:                  "ids",
		`{}`:                          "ids",
		`{"ids":["nothere"]}`:         "ids",
		`{"ids":["pdf","pdf"]}`:       "ids",
		`{"ids":["pdf","elsewhere"]}`: "ids",
		`{"ids":["pdf"],"ref":"-x"}`:  "ref",
	} {
		code, out := tb.verb(t, "skills.update", body)
		if code != 400 || !strings.Contains(fmt.Sprint(out), "field:"+field) {
			t.Errorf("%s = %d %v", body, code, out)
		}
	}
}
