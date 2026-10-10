package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

const goodCommit2 = "89abcdef0123456789abcdef0123456789abcdef"

func goodSource() Source {
	return Source{Repo: "https://github.com/anthropics/skills", Ref: "main", Path: "skills", Commit: goodCommit,
		Found: map[string]Found{
			"pdf":    {Path: "skills/pdf", Description: "PDF tools"},
			"docx":   {Path: "skills/docx", Description: "Word"},
			"broken": {Path: "skills/broken", Error: "SKILL.md has no description"},
		},
		Attached: map[string]Attachment{"pdf": {Enabled: true}}}
}

func one(name string, agents []string) map[string]Attachment {
	return map[string]Attachment{name: {Enabled: true, Agents: agents}}
}

// sourceErrors validates sources against workspace a and returns the
// fields with errors.
func sourceErrors(sources map[string]Source) []string {
	v := &validator{}
	validateSources(v, sources, map[string]Workspace{"a": {Path: "a"}})
	var out []string
	for _, f := range v.fields {
		out = append(out, f.Field)
	}
	return out
}

func TestValidateSources(t *testing.T) {
	many := map[string]Found{}
	for i := range MaxFound + 1 {
		many[fmt.Sprintf("s%d", i)] = Found{Path: fmt.Sprintf("skills/s%d", i), Description: "d"}
	}
	for name, tc := range map[string]struct {
		edit  func(*Source)
		field string
	}{
		"ok":                 {func(*Source) {}, ""},
		"root path":          {func(s *Source) { s.Path = ""; s.Found = map[string]Found{"root": {Path: "", Description: "r"}} }, ""},
		"tag ref":            {func(s *Source) { s.Ref = "v1.2.0" }, ""},
		"sha ref":            {func(s *Source) { s.Ref = goodCommit }, ""},
		"sha256 commit":      {func(s *Source) { s.Commit = strings.Repeat("a", 64) }, ""},
		"no agents":          {func(s *Source) { s.Attached = one("pdf", []string{}) }, ""},
		"some agents":        {func(s *Source) { s.Attached = one("pdf", []string{"@openai/codex"}) }, ""},
		"linked":             {func(s *Source) { s.Attached["pdf"] = Attachment{Workspaces: map[string]string{"a": ModeLink}} }, ""},
		"missing attachment": {func(s *Source) { s.Attached = one("gone", nil) }, ""},
		"http repo":          {func(s *Source) { s.Repo = "http://github.com/a/b" }, "skills.sources.src.repo"},
		"ssh repo":           {func(s *Source) { s.Repo = "git@github.com:a/b.git" }, "skills.sources.src.repo"},
		"userinfo":           {func(s *Source) { s.Repo = "https://me:pw@github.com/a/b" }, "skills.sources.src.repo"},
		"query":              {func(s *Source) { s.Repo = "https://github.com/a/b?x=1" }, "skills.sources.src.repo"},
		"empty ref":          {func(s *Source) { s.Ref = "" }, "skills.sources.src.ref"},
		"dash ref":           {func(s *Source) { s.Ref = "-main" }, "skills.sources.src.ref"},
		"dotdot ref":         {func(s *Source) { s.Ref = "a..b" }, "skills.sources.src.ref"},
		"lock ref":           {func(s *Source) { s.Ref = "x.lock" }, "skills.sources.src.ref"},
		"dotdot path":        {func(s *Source) { s.Path = "skills/../x"; s.Found = nil }, "skills.sources.src.path"},
		"trailing slash":     {func(s *Source) { s.Path = "skills/"; s.Found = nil }, "skills.sources.src.path"},
		"short commit":       {func(s *Source) { s.Commit = "0123456" }, "skills.sources.src.commit"},
		"upper commit":       {func(s *Source) { s.Commit = strings.ToUpper(goodCommit) }, "skills.sources.src.commit"},
		"too many found":     {func(s *Source) { s.Found = many; s.Attached = nil }, "skills.sources.src.found"},
		"bad found name":     {func(s *Source) { s.Found["PDF"] = Found{Path: "skills/PDF", Description: "d"} }, "skills.sources.src.found.PDF"},
		"found outside":      {func(s *Source) { s.Found["pdf"] = Found{Path: "other/pdf", Description: "d"} }, "skills.sources.src.found.pdf.path"},
		"found prefix only":  {func(s *Source) { s.Found["pdf"] = Found{Path: "skills2/pdf", Description: "d"} }, "skills.sources.src.found.pdf.path"},
		"no description":     {func(s *Source) { s.Found["pdf"] = Found{Path: "skills/pdf"} }, "skills.sources.src.found.pdf.description"},
		"long description":   {func(s *Source) { s.Found["pdf"] = Found{Path: "skills/pdf", Description: strings.Repeat("é", 1025)} }, "skills.sources.src.found.pdf.description"},
		"error and desc":     {func(s *Source) { s.Found["pdf"] = Found{Path: "skills/pdf", Description: "d", Error: "e"} }, "skills.sources.src.found.pdf.description"},
		"long error":         {func(s *Source) { s.Found["broken"] = Found{Path: "skills/broken", Error: strings.Repeat("e", 1025)} }, "skills.sources.src.found.broken.error"},
		"bad attached name":  {func(s *Source) { s.Attached = one("PDF", nil) }, "skills.sources.src.attached.PDF"},
		"bad agent":          {func(s *Source) { s.Attached = one("pdf", []string{"Not A Package"}) }, "skills.sources.src.attached.pdf.agents[0]"},
		"unknown workspace":  {func(s *Source) { s.Attached["pdf"] = Attachment{Workspaces: map[string]string{"b": ModeLink}} }, "skills.sources.src.attached.pdf.workspaces.b"},
		"bad workspace mode": {func(s *Source) { s.Attached["pdf"] = Attachment{Workspaces: map[string]string{"a": "copy"}} }, "skills.sources.src.attached.pdf.workspaces.a"},
	} {
		t.Run(name, func(t *testing.T) {
			s := goodSource()
			tc.edit(&s)
			got := sourceErrors(map[string]Source{"src": s})
			if tc.field == "" && len(got) != 0 || tc.field != "" && !slices.Equal(got, []string{tc.field}) {
				t.Fatalf("errors on %v, want %q", got, tc.field)
			}
		})
	}
	if got := sourceErrors(map[string]Source{"Bad_ID": goodSource()}); !slices.Equal(got, []string{"skills.sources.Bad_ID"}) {
		t.Errorf("bad id: %v", got)
	}
}

func TestOneProviderPerTarget(t *testing.T) {
	pair := func(a, b map[string]Attachment) map[string]Source {
		x, y := goodSource(), goodSource()
		x.Attached, y.Attached = a, b
		y.Ref = "v2"
		return map[string]Source{"x": x, "y": y}
	}
	ws := func(name string, ids ...string) map[string]Attachment {
		w := map[string]string{}
		for _, id := range ids {
			w[id] = ModeLink
		}
		return map[string]Attachment{name: {Agents: []string{}, Workspaces: w}}
	}
	for name, tc := range map[string]struct {
		sources map[string]Source
		field   string
	}{
		"all and all":     {pair(one("pdf", nil), one("pdf", nil)), "skills.sources.y.attached.pdf.agents"},
		"all and one":     {pair(one("pdf", nil), one("pdf", []string{"c"})), "skills.sources.y.attached.pdf.agents"},
		"one and all":     {pair(one("pdf", []string{"c"}), one("pdf", nil)), "skills.sources.y.attached.pdf.agents"},
		"shared agent":    {pair(one("pdf", []string{"c", "d"}), one("pdf", []string{"d", "e"})), "skills.sources.y.attached.pdf.agents"},
		"disjoint agents": {pair(one("pdf", []string{"c"}), one("pdf", []string{"d"})), ""},
		"none and all":    {pair(one("pdf", []string{}), one("pdf", nil)), ""},
		"other names":     {pair(one("pdf", nil), one("docx", nil)), ""},
		"same workspace":  {pair(ws("pdf", "a"), ws("pdf", "a")), "skills.sources.y.attached.pdf.workspaces.a"},
		"disabled counts": {pair(map[string]Attachment{"pdf": {Enabled: false}}, one("pdf", nil)), "skills.sources.y.attached.pdf.agents"},
	} {
		t.Run(name, func(t *testing.T) {
			got := sourceErrors(tc.sources)
			if tc.field == "" && len(got) != 0 || tc.field != "" && !slices.Equal(got, []string{tc.field}) {
				t.Fatalf("errors on %v, want %q", got, tc.field)
			}
		})
	}
	v := &validator{}
	validateSources(v, pair(one("pdf", []string{"c"}), one("pdf", []string{"c"})), nil)
	if msg := v.fields[0].Message; msg != "c already gets pdf from x" {
		t.Errorf("message = %q", msg)
	}
}

func TestProviderOf(t *testing.T) {
	x := goodSource()
	x.Attached = one("pdf", []string{"c"})
	sources := map[string]Source{"x": x}
	for _, tc := range []struct {
		agents        []string
		skip, src, by string
	}{
		{nil, "", "x", "c"},
		{[]string{"c"}, "", "x", "c"},
		{[]string{"d"}, "", "", ""},
		{[]string{}, "", "", ""},
		{nil, "x", "", ""},
	} {
		if src, by := ProviderOf(sources, "pdf", tc.agents, tc.skip); src != tc.src || by != tc.by {
			t.Errorf("ProviderOf(%v, skip %q) = %q %q", tc.agents, tc.skip, src, by)
		}
	}
	x.Attached = one("pdf", nil)
	if _, by := ProviderOf(map[string]Source{"x": x}, "pdf", nil, ""); by != "every agent" {
		t.Errorf("all and all = %q", by)
	}
}

func TestMissing(t *testing.T) {
	s := goodSource()
	s.Attached = map[string]Attachment{"pdf": {}, "gone": {}, "broken": {}}
	if got := s.Missing(); !slices.Equal(got, []string{"broken", "gone"}) {
		t.Errorf("Missing = %v", got)
	}
}

func TestIDs(t *testing.T) {
	for in, want := range map[string]string{
		"Obra-Superpowers": "obra-superpowers", "my_skills.v2": "my-skills-v2", "--x--": "x", "...": "",
		strings.Repeat("ab-", 30): strings.Repeat("ab-", 21) + "a",
	} {
		if got := IDFrom(in); got != want {
			t.Errorf("IDFrom(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Suffixed(strings.Repeat("a", 64), 2); got != strings.Repeat("a", 62)+"-2" {
		t.Errorf("Suffixed = %q", got)
	}
	none := func(string) bool { return false }
	takenOf := func(ids ...string) func(string) bool {
		return func(id string) bool { return slices.Contains(ids, id) }
	}
	for _, tc := range []struct {
		repo, ref string
		taken     func(string) bool
		want      string
	}{
		{"https://github.com/obra/superpowers", "main", none, "obra-superpowers"},
		{"https://github.com/acme/api.git", "main", none, "acme-api"},
		{"https://gitlab.com/group/sub/tools", "main", none, "sub-tools"},
		{"https://example.com/one", "main", none, "one"},
		{"https://example.com/", "main", none, "source"},
		{"https://github.com/acme/tools", "v2", takenOf("acme-tools"), "acme-tools-v2"},
		{"https://github.com/acme/tools", "v2", takenOf("acme-tools", "acme-tools-v2"), "acme-tools-v2-2"},
		{"https://github.com/acme/tools", "feature/X", takenOf("acme-tools"), "acme-tools-feature-x"},
	} {
		if got := SourceID(tc.repo, tc.ref, tc.taken); got != tc.want {
			t.Errorf("SourceID(%s, %s) = %q, want %q", tc.repo, tc.ref, got, tc.want)
		}
	}
}

func TestValidateMCPWorkspaces(t *testing.T) {
	ws := map[string]Workspace{"a": {Path: "a"}, "b": {Path: "b"}}
	for name, tc := range map[string]struct {
		m     map[string][]string
		field string
	}{
		"ok":            {map[string][]string{"context7": {"a", "b"}}, ""},
		"empty list":    {map[string][]string{"context7": {}}, "mcp.workspaces.context7"},
		"unknown":       {map[string][]string{"context7": {"a", "c"}}, "mcp.workspaces.context7[1]"},
		"bad server id": {map[string][]string{"Context7": {"a"}}, "mcp.workspaces.Context7"},
	} {
		t.Run(name, func(t *testing.T) {
			v := &validator{}
			validateMCPWorkspaces(v, tc.m, ws)
			err := v.err()
			var ve *ValidationError
			switch {
			case tc.field == "" && err != nil:
				t.Fatalf("err = %v", err)
			case tc.field != "" && (!errors.As(err, &ve) || len(ve.Fields) != 1 || ve.Fields[0].Field != tc.field):
				t.Fatalf("err = %v, want one on %s", err, tc.field)
			}
		})
	}
}

func TestNormalizeSkills(t *testing.T) {
	s := Skills{Sources: map[string]Source{"x": {Attached: map[string]Attachment{"pdf": {Agents: []string{"b", "a", "b"}}, "docx": {}}}}}
	s.normalize()
	x := s.Sources["x"]
	if x.Found == nil || !slices.Equal(x.Attached["pdf"].Agents, []string{"a", "b"}) ||
		x.Attached["docx"].Agents != nil || x.Attached["docx"].Workspaces == nil {
		t.Errorf("normalized = %+v", x)
	}
	var empty Skills
	empty.normalize()
	if empty.Sources == nil {
		t.Error("nil sources must become an empty map")
	}
}
