package config

import (
	"encoding/json"
	"strings"
	"testing"
)

const goodCommit = "0123456789abcdef0123456789abcdef01234567"

func goodSkill() Skill {
	return Skill{Repo: "https://github.com/anthropics/skills", Path: "skills/pdf", Ref: "main",
		Commit: goodCommit, Description: "PDF tools", Enabled: true}
}

func TestDefaultsHaveNoSkills(t *testing.T) {
	if d := Defaults(); d.Skills == nil || len(d.Skills) != 0 {
		t.Errorf("skills = %v", d.Skills)
	}
}

func TestValidateSkills(t *testing.T) {
	cases := map[string]struct {
		id    string
		edit  func(*Skill)
		field string
	}{
		"ok":               {"pdf", func(*Skill) {}, ""},
		"root path":        {"pdf", func(s *Skill) { s.Path = "" }, ""},
		"sha ref":          {"pdf", func(s *Skill) { s.Ref = goodCommit }, ""},
		"tag ref":          {"pdf", func(s *Skill) { s.Ref = "v1.2.0" }, ""},
		"sha256 commit":    {"pdf", func(s *Skill) { s.Commit = strings.Repeat("a", 64) }, ""},
		"some agents":      {"pdf", func(s *Skill) { s.Agents = []string{"@openai/codex"} }, ""},
		"bad id":           {"PDF", func(*Skill) {}, "skills.PDF"},
		"long id":          {strings.Repeat("a", 65), func(*Skill) {}, "skills." + strings.Repeat("a", 65)},
		"http repo":        {"pdf", func(s *Skill) { s.Repo = "http://github.com/a/b" }, "skills.pdf.repo"},
		"ssh repo":         {"pdf", func(s *Skill) { s.Repo = "git@github.com:a/b.git" }, "skills.pdf.repo"},
		"userinfo":         {"pdf", func(s *Skill) { s.Repo = "https://me:pw@github.com/a/b" }, "skills.pdf.repo"},
		"query":            {"pdf", func(s *Skill) { s.Repo = "https://github.com/a/b?x=1" }, "skills.pdf.repo"},
		"empty repo":       {"pdf", func(s *Skill) { s.Repo = "" }, "skills.pdf.repo"},
		"dotdot path":      {"pdf", func(s *Skill) { s.Path = "skills/../x" }, "skills.pdf.path"},
		"abs path":         {"pdf", func(s *Skill) { s.Path = "/skills/pdf" }, "skills.pdf.path"},
		"trailing slash":   {"pdf", func(s *Skill) { s.Path = "skills/pdf/" }, "skills.pdf.path"},
		"empty ref":        {"pdf", func(s *Skill) { s.Ref = "" }, "skills.pdf.ref"},
		"dash ref":         {"pdf", func(s *Skill) { s.Ref = "-main" }, "skills.pdf.ref"},
		"dotdot ref":       {"pdf", func(s *Skill) { s.Ref = "a..b" }, "skills.pdf.ref"},
		"space ref":        {"pdf", func(s *Skill) { s.Ref = "my branch" }, "skills.pdf.ref"},
		"lock ref":         {"pdf", func(s *Skill) { s.Ref = "x.lock" }, "skills.pdf.ref"},
		"short commit":     {"pdf", func(s *Skill) { s.Commit = "0123456" }, "skills.pdf.commit"},
		"upper commit":     {"pdf", func(s *Skill) { s.Commit = strings.ToUpper(goodCommit) }, "skills.pdf.commit"},
		"no description":   {"pdf", func(s *Skill) { s.Description = "" }, "skills.pdf.description"},
		"long description": {"pdf", func(s *Skill) { s.Description = strings.Repeat("é", 1025) }, "skills.pdf.description"},
		"empty agents":     {"pdf", func(s *Skill) { s.Agents = []string{} }, "skills.pdf.agents"},
		"bad agent":        {"pdf", func(s *Skill) { s.Agents = []string{"Not A Package"} }, "skills.pdf.agents[0]"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := Defaults()
			s := goodSkill()
			tc.edit(&s)
			c.Skills = map[string]Skill{tc.id: s}
			err := c.Validate()
			if tc.field == "" {
				if err != nil {
					t.Fatalf("Validate = %v", err)
				}
				return
			}
			ve, ok := err.(*ValidationError)
			if !ok || len(ve.Fields) != 1 || ve.Fields[0].Field != tc.field {
				t.Fatalf("Validate = %v, want one error on %s", err, tc.field)
			}
		})
	}
}

func TestPatchSkills(t *testing.T) {
	c := Defaults()
	add := `{"skills":{"pdf":{"repo":"https://github.com/anthropics/skills","path":"skills/pdf","ref":"main","commit":"` + goodCommit + `","description":"PDF tools"}}}`
	c, err := Patch(c, json.RawMessage(add))
	if err != nil {
		t.Fatal(err)
	}
	if s := c.Skills["pdf"]; !s.Enabled || s.Agents != nil || s.Commit != goodCommit {
		t.Fatalf("added = %+v", s)
	}
	c, err = Patch(c, json.RawMessage(`{"skills":{"pdf":{"agents":["@openai/codex"],"enabled":false}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if s := c.Skills["pdf"]; s.Enabled || len(s.Agents) != 1 || s.Ref != "main" {
		t.Fatalf("merged = %+v", s)
	}
	c, err = Patch(c, json.RawMessage(`{"skills":{"pdf":{"agents":null}}}`))
	if err != nil || c.Skills["pdf"].Agents != nil {
		t.Fatalf("agents reset = %+v, %v", c.Skills["pdf"], err)
	}
	c, err = Patch(c, json.RawMessage(`{"skills":{"pdf":null}}`))
	if err != nil || len(c.Skills) != 0 {
		t.Fatalf("delete = %v, %v", c.Skills, err)
	}
}

func TestPatchIncompleteSkillFailsValidation(t *testing.T) {
	c, err := Patch(Defaults(), json.RawMessage(`{"skills":{"pdf":{"repo":"https://github.com/a/b"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	ve, ok := c.Validate().(*ValidationError)
	if !ok {
		t.Fatal("an incomplete skill validated")
	}
	got := map[string]bool{}
	for _, f := range ve.Fields {
		got[f.Field] = true
	}
	for _, f := range []string{"skills.pdf.ref", "skills.pdf.commit", "skills.pdf.description"} {
		if !got[f] {
			t.Errorf("no error on %s: %v", f, ve)
		}
	}
}

func TestPatchSkillUnknownField(t *testing.T) {
	_, err := Patch(Defaults(), json.RawMessage(`{"skills":{"pdf":{"url":"x"}}}`))
	if err == nil || !strings.Contains(err.Error(), "skills.pdf.url") {
		t.Errorf("Patch = %v", err)
	}
}

func TestNormalizeSortsSkillAgents(t *testing.T) {
	c := Defaults()
	s := goodSkill()
	s.Agents = []string{"b", "a", "b"}
	c.Skills = map[string]Skill{"pdf": s}
	c.normalize()
	if got := c.Skills["pdf"].Agents; len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("agents = %v", got)
	}
}
