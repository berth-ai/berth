package broker

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/berth-ai/berth/internal/config"
)

const (
	anthRepo     = "https://github.com/anthropics/skills"
	skillCommit3 = "fedcba9876543210fedcba9876543210fedcba98"
)

// addSource adds anthropics/skills at main, the whole repository, through
// the broker; extra is more JSON fields, each starting with a comma.
func addSource(t *testing.T, tb *testBroker, extra string) Job {
	t.Helper()
	j := skillJob(t, tb, "skills.sources.add", `{"repo":"`+anthRepo+`","ref":"main"`+extra+`}`)
	if j.State != StateSucceeded {
		t.Fatalf("add = %s", j.Result)
	}
	return j
}

func result(t *testing.T, j Job) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(j.Result, &m); err != nil {
		t.Fatalf("result %s: %v", j.Result, err)
	}
	return m
}

func sourcesNow(t *testing.T, tb *testBroker) (map[string]config.Source, int) {
	t.Helper()
	cfg, err := tb.b.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Skills.Sources, cfg.Revision
}

// applyNow applies patch on top of the current revision.
func applyNow(t *testing.T, tb *testBroker, patch string) (int, map[string]any) {
	t.Helper()
	_, rev := sourcesNow(t, tb)
	return tb.verb(t, "config.apply", fmt.Sprintf(`{"base_revision":%d,"patch":%s}`, rev, patch))
}

// attach is the patch that attaches name of anthropics-skills to every agent.
func attach(name string) string {
	return `{"skills":{"sources":{"anthropics-skills":{"attached":{"` + name + `":{}}}}}}`
}

// diffNames lists the names of a preview's diff rows.
func diffNames(v any) string {
	var names []string
	for _, r := range v.([]any) {
		names = append(names, r.(map[string]any)["name"].(string))
	}
	return fmt.Sprint(names)
}

func TestSourcesPreviewNew(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	fakeScan(t, tb)
	j := skillJob(t, tb, "skills.sources.preview", `{"repo":"`+anthRepo+`","ref":"main","path":"skills"}`)
	r := result(t, j)
	found, _ := r["found"].(map[string]any)
	if j.State != StateSucceeded || r["commit"] != skillCommit || r["id"] != "anthropics-skills" || r["exists"] != "" || len(found) != 4 {
		t.Fatalf("preview = %s", j.Result)
	}
	if bad := found["bad"].(map[string]any); bad["path"] != "skills/bad" || bad["error"] != "SKILL.md has no description" || bad["description"] != nil {
		t.Errorf("error row = %v", bad)
	}
	if got := lastScan(t, tb); got != "skills scan --json --repo "+anthRepo+" --ref main --path skills" {
		t.Errorf("scan = %s", got)
	}
	if srcs, rev := sourcesNow(t, tb); len(srcs) != 0 || rev != 0 {
		t.Errorf("a preview saved: %v %d", srcs, rev)
	}
}

func TestSourcesPreviewNewSaysExists(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	fakeScan(t, tb)
	addSource(t, tb, "")
	r := result(t, skillJob(t, tb, "skills.sources.preview", `{"repo":"`+anthRepo+`","ref":"main"}`))
	if r["exists"] != "anthropics-skills" || r["id"] != "anthropics-skills-main" {
		t.Errorf("preview = %v", r)
	}
}

func TestSourcesAdd(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	fakeScan(t, tb)
	r := result(t, addSource(t, tb, ""))
	if r["id"] != "anthropics-skills" || r["commit"] != skillCommit || r["revision"] != float64(1) ||
		fmt.Sprint(r["attached"]) != "[]" || fmt.Sprint(r["skipped"]) != "[]" {
		t.Fatalf("add = %v", r)
	}
	srcs, rev := sourcesNow(t, tb)
	s := srcs["anthropics-skills"]
	if rev != 1 || s.Repo != anthRepo || s.Ref != "main" || s.Path != "" || s.Commit != skillCommit || len(s.Attached) != 0 {
		t.Errorf("source = %+v", s)
	}
	if len(s.Found) != 4 || s.Found["pdf"].Description != "PDF tools" || s.Found["root"].Path != "" || s.Found["bad"].Valid() {
		t.Errorf("found = %+v", s.Found)
	}
	if got := lastScan(t, tb); got != "skills scan --json --repo "+anthRepo+" --ref main" {
		t.Errorf("scan = %s", got)
	}
	waitSkillRuns(t, tb, 1)
}

func TestSourcesAddAtCommitWithID(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	fakeScan(t, tb)
	addSource(t, tb, `,"id":"anth","commit":"`+skillCommit2+`"`)
	srcs, _ := sourcesNow(t, tb)
	if s := srcs["anth"]; s.Commit != skillCommit2 || s.Ref != "main" || s.Found["pdf"].Description != "PDF tools v2" {
		t.Errorf("source = %+v", s)
	}
	if got := lastScan(t, tb); !strings.Contains(got, "--ref "+skillCommit2) {
		t.Errorf("the add must scan the previewed commit: %s", got)
	}
}

func TestSourcesAddAttachAll(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	fakeScan(t, tb)
	// Another source already gives docx to every agent.
	word := `{"skills":{"sources":{"word":{"repo":"https://github.com/acme/word","ref":"main","path":"","commit":"` + skillCommit +
		`","found":{"docx":{"path":"docx","description":"Word"}},"attached":{"docx":{}}}}}}`
	if code, out := applyNow(t, tb, word); code != 200 {
		t.Fatalf("apply = %d %v", code, out)
	}
	r := result(t, addSource(t, tb, `,"attach_all":true`))
	if fmt.Sprint(r["attached"]) != "[pdf root]" || fmt.Sprint(r["skipped"]) != "[docx]" {
		t.Fatalf("add = %v", r)
	}
	srcs, _ := sourcesNow(t, tb)
	if a := srcs["anthropics-skills"].Attached; len(a) != 2 || !a["pdf"].Enabled || a["pdf"].Agents != nil {
		t.Errorf("attached = %+v", a)
	}
}

func TestSourcesAddRefusals(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	fakeScan(t, tb)
	addSource(t, tb, "")
	waitSkillRuns(t, tb, 1)
	for name, tc := range map[string]struct{ body, code string }{
		"same source": {`{"repo":"` + anthRepo + `","ref":"main"}`, "source_exists"},
		"id taken":    {`{"repo":"` + anthRepo + `","ref":"v2","id":"anthropics-skills"}`, "id_exists"},
		"bad id":      {`{"repo":"` + anthRepo + `","ref":"v2","id":"Bad"}`, "invalid_request"},
		"bad repo":    {`{"repo":"http://example.com/a/b","ref":"main"}`, "invalid_request"},
		"bad ref":     {`{"repo":"` + anthRepo + `","ref":"-x"}`, "invalid_request"},
		"bad path":    {`{"repo":"` + anthRepo + `","ref":"v2","path":"../x"}`, "invalid_request"},
		"bad commit":  {`{"repo":"` + anthRepo + `","ref":"v2","commit":"abc"}`, "invalid_request"},
	} {
		t.Run(name, func(t *testing.T) {
			before := len(tb.b.Jobs.List())
			code, out := tb.verb(t, "skills.sources.add", tc.body)
			if code != 400 || errCode(out) != tc.code || len(tb.b.Jobs.List()) != before {
				t.Errorf("= %d %v (jobs %d → %d)", code, out, before, len(tb.b.Jobs.List()))
			}
		})
	}
}

func TestSourcesPreviewExisting(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	fakeScan(t, tb)
	addSource(t, tb, "")
	if code, out := applyNow(t, tb, attach("pdf")); code != 200 {
		t.Fatalf("attach = %d %v", code, out)
	}
	same := result(t, skillJob(t, tb, "skills.sources.preview", `{"id":"anthropics-skills"}`))
	if same["changed"] != false || same["commit"] != skillCommit || diffNames(same["removed"]) != "[]" {
		t.Errorf("unchanged = %v", same)
	}
	d := result(t, skillJob(t, tb, "skills.sources.preview", `{"id":"anthropics-skills","ref":"v3"}`))
	if d["changed"] != true || d["from"] != skillCommit || d["commit"] != skillCommit3 || d["ref"] != "v3" {
		t.Fatalf("diff = %v", d)
	}
	for kind, want := range map[string]string{"added": "[xlsx]", "removed": "[bad root]", "moved": "[docx]", "described": "[pdf]"} {
		if got := diffNames(d[kind]); got != want {
			t.Errorf("%s = %s, want %s", kind, got, want)
		}
	}
	if m := d["moved"].([]any)[0].(map[string]any); m["from"] != "skills/docx" || m["path"] != "skills/word" || m["attached"] != false {
		t.Errorf("moved = %v", m)
	}
	if p := d["described"].([]any)[0].(map[string]any); p["description"] != "PDF tools v3" || p["attached"] != true {
		t.Errorf("described = %v", p)
	}
	if _, rev := sourcesNow(t, tb); rev != 2 {
		t.Errorf("a preview saved: revision %d", rev)
	}
	if code, out := tb.verb(t, "skills.sources.preview", `{"id":"nope"}`); code != 404 || errCode(out) != "unknown_source" {
		t.Errorf("unknown = %d %v", code, out)
	}
	if code, _ := tb.verb(t, "skills.sources.preview", `{"id":"anthropics-skills","repo":"`+anthRepo+`"}`); code != 400 {
		t.Errorf("id with repo = %d", code)
	}
}

func TestConfigApplyChecksNewAttachments(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	fakeScan(t, tb)
	addSource(t, tb, "")
	if code, out := applyNow(t, tb, attach("pdf")); code != 200 {
		t.Fatalf("attach pdf = %d %v", code, out)
	}
	if code, out := applyNow(t, tb, attach("nope")); code != 400 || !strings.Contains(fmt.Sprint(out), "anthropics-skills has no skill named nope") {
		t.Errorf("unknown name = %d %v", code, out)
	}
	if code, out := applyNow(t, tb, attach("bad")); code != 400 || !strings.Contains(fmt.Sprint(out), "bad can't be attached: SKILL.md has no description") {
		t.Errorf("error row = %d %v", code, out)
	}
	// An existing attachment stays patchable after its row is gone.
	if code, out := applyNow(t, tb, `{"skills":{"sources":{"anthropics-skills":{"found":{"pdf":null}}}}}`); code != 200 {
		t.Fatalf("drop the row = %d %v", code, out)
	}
	if code, out := applyNow(t, tb, `{"skills":{"sources":{"anthropics-skills":{"attached":{"pdf":{"enabled":false}}}}}}`); code != 200 {
		t.Errorf("patch a missing attachment = %d %v", code, out)
	}
}

func TestConfigApplyRefusesSecondProvider(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	fakeScan(t, tb)
	addSource(t, tb, "")
	if j := skillJob(t, tb, "skills.sources.add", `{"repo":"`+anthRepo+`","ref":"v2"}`); j.State != StateSucceeded {
		t.Fatalf("add v2 = %s", j.Result)
	}
	if code, out := applyNow(t, tb, attach("pdf")); code != 200 {
		t.Fatalf("attach = %d %v", code, out)
	}
	code, out := applyNow(t, tb, `{"skills":{"sources":{"anthropics-skills-v2":{"attached":{"pdf":{}}}}}}`)
	if code != 400 || !strings.Contains(fmt.Sprint(out), "every agent already gets pdf from anthropics-skills") {
		t.Errorf("second provider = %d %v", code, out)
	}
}

func TestSourcesUpdateReplacesFound(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	fakeScan(t, tb)
	addSource(t, tb, "")
	applyNow(t, tb, `{"skills":{"sources":{"anthropics-skills":{"attached":{"pdf":{},"docx":{"agents":["agent-a"]}}}}}}`)
	j := skillJob(t, tb, "skills.sources.update", `{"id":"anthropics-skills","ref":"v2","commit":"`+skillCommit2+`"}`)
	r := result(t, j)
	if j.State != StateSucceeded || r["changed"] != true || r["commit"] != skillCommit2 || r["revision"] != float64(3) || fmt.Sprint(r["missing"]) != "[]" {
		t.Fatalf("update = %s", j.Result)
	}
	srcs, _ := sourcesNow(t, tb)
	s := srcs["anthropics-skills"]
	if s.Ref != "v2" || s.Commit != skillCommit2 || len(s.Found) != 2 || s.Found["pdf"].Description != "PDF tools v2" {
		t.Errorf("source = %+v (root and bad must be gone)", s)
	}
	if len(s.Attached) != 2 || s.Attached["docx"].Agents[0] != "agent-a" {
		t.Errorf("attachments = %+v", s.Attached)
	}
	if got := lastScan(t, tb); got != "skills scan --json --repo "+anthRepo+" --ref "+skillCommit2 {
		t.Errorf("the update must scan the previewed commit: %s", got)
	}
}

func TestSourcesUpdateReportsMissing(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	fakeScan(t, tb)
	addSource(t, tb, "")
	applyNow(t, tb, attach("root"))
	r := result(t, skillJob(t, tb, "skills.sources.update", `{"id":"anthropics-skills","commit":"`+skillCommit2+`"}`))
	if fmt.Sprint(r["missing"]) != "[root]" {
		t.Fatalf("update = %v", r)
	}
	srcs, _ := sourcesNow(t, tb)
	if s := srcs["anthropics-skills"]; s.Ref != "main" || !slices.Equal(s.Missing(), []string{"root"}) {
		t.Errorf("source = %+v", s)
	}
}

func TestSourcesUpdateUnchanged(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	fakeScan(t, tb)
	addSource(t, tb, "")
	r := result(t, skillJob(t, tb, "skills.sources.update", `{"id":"anthropics-skills","commit":"`+skillCommit+`"}`))
	if r["changed"] != false || r["revision"] != float64(1) {
		t.Errorf("update = %v", r)
	}
}

func TestSourcesUpdateRefusals(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	fakeScan(t, tb)
	addSource(t, tb, "")
	for name, tc := range map[string]struct {
		body string
		code int
		err  string
	}{
		"unknown":   {`{"id":"nope","commit":"` + skillCommit + `"}`, 404, "unknown_source"},
		"no commit": {`{"id":"anthropics-skills"}`, 400, "invalid_request"},
		"bad ref":   {`{"id":"anthropics-skills","ref":"a..b","commit":"` + skillCommit + `"}`, 400, "invalid_request"},
		"extra":     {`{"id":"anthropics-skills","commit":"` + skillCommit + `","path":"x"}`, 400, "invalid_request"},
	} {
		t.Run(name, func(t *testing.T) {
			if code, out := tb.verb(t, "skills.sources.update", tc.body); code != tc.code || errCode(out) != tc.err {
				t.Errorf("= %d %v", code, out)
			}
		})
	}
}

func TestSourcesRemove(t *testing.T) {
	tb := newTestBroker(t)
	withSkills(t, tb)
	fakeScan(t, tb)
	addSource(t, tb, "")
	applyNow(t, tb, attach("pdf"))
	waitSkillRuns(t, tb, 2)
	code, out := tb.verb(t, "skills.sources.remove", `{"id":"anthropics-skills"}`)
	if code != 200 || out["id"] != "anthropics-skills" || out["revision"] != float64(3) {
		t.Fatalf("remove = %d %v", code, out)
	}
	if srcs, _ := sourcesNow(t, tb); len(srcs) != 0 {
		t.Errorf("sources = %v", srcs)
	}
	waitSkillRuns(t, tb, 3)
	if code, out := tb.verb(t, "skills.sources.remove", `{"id":"anthropics-skills"}`); code != 404 || errCode(out) != "unknown_source" {
		t.Errorf("again = %d %v", code, out)
	}
}

func TestConfigUpgrade(t *testing.T) {
	tb := newTestBroker(t)
	five := `{"schema":5,"revision":9,"skills":{"pdf":{"repo":"` + anthRepo + `","path":"skills/pdf","ref":"main","commit":"` + skillCommit + `","description":"PDF tools","agents":null,"enabled":true}}}`
	code, out := tb.verb(t, "config.upgrade", `{"config":`+five+`}`)
	if code != 200 {
		t.Fatalf("upgrade = %d %v", code, out)
	}
	cfg := out["config"].(map[string]any)
	src := cfg["skills"].(map[string]any)["sources"].(map[string]any)["anthropics-skills"].(map[string]any)
	if cfg["schema"] != float64(config.Schema) || src["commit"] != skillCommit || src["path"] != "skills" {
		t.Errorf("upgraded = %v", cfg)
	}
	if _, rev := sourcesNow(t, tb); rev != 0 {
		t.Error("config.upgrade changed the config")
	}
	for _, body := range []string{`{"config":{"schema":7}}`, `{"config":[1]}`, `{}`} {
		if code, out := tb.verb(t, "config.upgrade", body); code != 400 {
			t.Errorf("%s = %d %v", body, code, out)
		}
	}
}
