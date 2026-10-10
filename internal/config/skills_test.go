package config

import (
	"strings"
	"testing"
)

const goodCommit = "0123456789abcdef0123456789abcdef01234567"

func TestDefaultsHaveNoSources(t *testing.T) {
	d := Defaults()
	if d.Skills.Sources == nil || len(d.Skills.Sources) != 0 || d.MCP.Workspaces == nil {
		t.Errorf("skills = %+v, mcp.workspaces = %v", d.Skills, d.MCP.Workspaces)
	}
}

func TestRepoPathRefCommitRules(t *testing.T) {
	for _, repo := range []string{"https://github.com/a/b", "https://gitlab.example.com/g/sub/r.git"} {
		if msg := CheckSkillRepo(repo); msg != "" {
			t.Errorf("CheckSkillRepo(%q) = %q", repo, msg)
		}
	}
	for _, p := range []string{"", "skills", "skills/pdf"} {
		if !ValidSkillPath(p) {
			t.Errorf("ValidSkillPath(%q) = false", p)
		}
	}
	for _, ref := range []string{"main", "v1.2.0", "feature/x", goodCommit} {
		if !ValidGitRef(ref) {
			t.Errorf("ValidGitRef(%q) = false", ref)
		}
	}
	if !ValidCommit(goodCommit) || !ValidCommit(strings.Repeat("a", 64)) || ValidCommit("0123456") {
		t.Error("ValidCommit")
	}
}
