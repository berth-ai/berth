package config

import (
	"maps"
	"slices"
	"testing"
)

func legacy(repo, ref, path, commit, desc string, agents []string, on bool) LegacySkill {
	return LegacySkill{Repo: repo, Ref: ref, Path: path, Commit: commit, Description: desc, Agents: agents, Enabled: on}
}

func TestMigrateSkills(t *testing.T) {
	const anth = "https://github.com/anthropics/skills"
	t.Run("one group", func(t *testing.T) {
		got := MigrateSkills(map[string]LegacySkill{
			"pdf":  legacy(anth, "main", "skills/pdf", goodCommit, "PDF tools", nil, true),
			"docx": legacy(anth, "main", "skills/docx", goodCommit, "Word", []string{"@openai/codex"}, false),
		})
		s, ok := got["anthropics-skills"]
		if len(got) != 1 || !ok || s.Repo != anth || s.Ref != "main" || s.Path != "skills" || s.Commit != goodCommit {
			t.Fatalf("sources = %+v", got)
		}
		if s.Found["pdf"] != (Found{Path: "skills/pdf", Description: "PDF tools"}) || s.Found["docx"].Description != "Word" {
			t.Errorf("found = %+v", s.Found)
		}
		pdf, docx := s.Attached["pdf"], s.Attached["docx"]
		if !pdf.Enabled || pdf.Agents != nil || pdf.Workspaces == nil || len(pdf.Workspaces) != 0 {
			t.Errorf("pdf = %+v (absent agents must stay every agent)", pdf)
		}
		if docx.Enabled || !slices.Equal(docx.Agents, []string{"@openai/codex"}) {
			t.Errorf("docx = %+v", docx)
		}
	})
	t.Run("drifted", func(t *testing.T) {
		got := MigrateSkills(map[string]LegacySkill{
			"pdf":  legacy(anth, "main", "skills/pdf", goodCommit2, "PDF tools", nil, true),
			"docx": legacy(anth, "main", "skills/docx", goodCommit, "Word", nil, true),
			"xlsx": legacy(anth, "main", "skills/xlsx", goodCommit, "Sheets", nil, true),
		})
		a, b := got["anthropics-skills"], got["anthropics-skills-2"]
		if len(got) != 2 || a.Commit != goodCommit || b.Commit != goodCommit2 {
			t.Fatalf("sources = %+v", got)
		}
		if len(a.Found) != 2 || len(b.Found) != 1 || b.Found["pdf"].Path != "skills/pdf" || len(b.Attached) != 1 {
			t.Errorf("no skill may move commit: %+v / %+v", a, b)
		}
	})
	t.Run("ids", func(t *testing.T) {
		got := MigrateSkills(map[string]LegacySkill{
			"a": legacy("https://github.com/acme/tools", "main", "a", goodCommit, "A", nil, true),
			"b": legacy("https://github.com/acme/tools", "v2", "b", goodCommit2, "B", nil, true),
			"c": legacy("https://gitlab.com/acme/tools", "main", "", goodCommit, "C", nil, true),
		})
		if len(got) != 3 || got["acme-tools"].Ref != "main" || got["acme-tools"].Repo != "https://github.com/acme/tools" ||
			got["acme-tools-v2"].Ref != "v2" || got["acme-tools-main"].Repo != "https://gitlab.com/acme/tools" {
			t.Fatalf("ids = %v", slices.Sorted(maps.Keys(got)))
		}
		if got["acme-tools-main"].Path != "" || got["acme-tools"].Path != "" {
			t.Errorf("a root skill and a top-level folder track the whole repository: %+v", got)
		}
	})
	t.Run("validates", func(t *testing.T) {
		got := MigrateSkills(map[string]LegacySkill{
			"pdf":  legacy(anth, "main", "skills/pdf", goodCommit2, "PDF tools", nil, true),
			"docx": legacy(anth, "main", "skills/docx", goodCommit, "Word", []string{"@openai/codex"}, true),
		})
		v := &validator{}
		validateSources(v, got, nil)
		if err := v.err(); err != nil {
			t.Errorf("migrated sources do not validate: %v", err)
		}
	})
	if got := MigrateSkills(nil); got == nil || len(got) != 0 {
		t.Errorf("no skills = %v", got)
	}
}

func TestCommonParent(t *testing.T) {
	for _, tc := range []struct {
		paths []string
		want  string
	}{
		{[]string{"skills/pdf"}, "skills"},
		{[]string{"skills/pdf", "skills/docx"}, "skills"},
		{[]string{"a/b/c", "a/b/d", "a/e"}, "a"},
		{[]string{"x/one", "y/two"}, ""},
		{[]string{"", "skills/pdf"}, ""},
		{[]string{"pdf"}, ""},
	} {
		if got := commonParent(tc.paths); got != tc.want {
			t.Errorf("commonParent(%v) = %q, want %q", tc.paths, got, tc.want)
		}
	}
}
func TestUpgrade(t *testing.T) {
	const five = `{"schema":5,"revision":3,"skills":{"pdf":{"repo":"https://github.com/anthropics/skills","path":"skills/pdf","ref":"main","commit":"` + goodCommit + `","description":"PDF tools","agents":null,"enabled":true}},"workspaces":{}}`
	c, err := Upgrade([]byte(five))
	if err != nil {
		t.Fatal(err)
	}
	s := c.Skills.Sources["anthropics-skills"]
	if c.Schema != Schema || c.Revision != 3 || s.Commit != goodCommit || s.Attached["pdf"].Agents != nil || !s.Attached["pdf"].Enabled {
		t.Errorf("upgraded = %+v", c)
	}
	if c, err := Upgrade([]byte(`{"schema":1,"revision":1}`)); err != nil || c.Schema != Schema || c.Skills.Sources == nil {
		t.Errorf("schema 1 = %+v, %v", c, err)
	}
	for name, raw := range map[string]string{
		"newer":            `{"schema":7}`,
		"not an object":    `[1]`,
		"bad old skill":    `{"schema":5,"skills":{"pdf":{"url":"x"}}}`,
		"flat skills at 6": `{"schema":6,"skills":{"pdf":{}}}`,
		"unknown key":      `{"schema":6,"surprise":true}`,
	} {
		if _, err := Upgrade([]byte(raw)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
