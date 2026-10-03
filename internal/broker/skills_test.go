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
