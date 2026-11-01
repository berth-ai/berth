package locksource

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
)

var descMeta = map[string]Meta{
	"@a/agent": {Kind: KindAgent, Name: "A"},
	"b-mcp":    {Kind: KindMCP, Name: "B"},
	"c-mcp":    {Kind: KindMCP, Name: "C"},
}

func TestParseDescriptors(t *testing.T) {
	pkg := []byte(`{"berthAgents":{"schema":1,"future":true,
		"agents":{"@a/agent":{"mcp":{"file":"~/.a/settings.json","style":"claude","key":"mcpServers"},"later":{"x":1}}},
		"servers":{"b-mcp":{"id":"b","bin":"b-mcp","args":["--x"]},"c-mcp":{"id":"c","bin":"c-mcp"}}}}`)
	d, err := ParseDescriptors(pkg, descMeta)
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Agents["@a/agent"].MCP; got == nil || got.File != "~/.a/settings.json" || got.Style != StyleClaude || got.Key != "mcpServers" {
		t.Errorf("agent = %+v", got)
	}
	if s := d.Servers["b-mcp"]; s.ID != "b" || s.Bin != "b-mcp" || !slices.Equal(s.Args, []string{"--x"}) {
		t.Errorf("server b = %+v", s)
	}
	if s := d.Servers["c-mcp"]; s.Args == nil || len(s.Args) != 0 {
		t.Errorf("server c args = %#v, want empty non-nil", s.Args)
	}
}

func TestParseDescriptorsMissingBlock(t *testing.T) {
	d, err := ParseDescriptors([]byte(`{"dependencies":{}}`), descMeta)
	if err != nil {
		t.Fatal(err)
	}
	if d.Agents == nil || d.Servers == nil || len(d.Agents)+len(d.Servers) != 0 {
		t.Errorf("descriptors = %+v, want empty non-nil maps", d)
	}
}

func TestParseDescriptorsNewerSchema(t *testing.T) {
	_, err := ParseDescriptors([]byte(`{"berthAgents":{"schema":2,"agents":{"x":{"y":1}}}}`), descMeta)
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "agent lock needs a newer Berth image (descriptor schema 2)") {
		t.Errorf("err = %v", err)
	}
}

func TestParseRejectsBadDescriptors(t *testing.T) {
	agent := func(mcp string) string { return `{"schema":1,"agents":{"@a/agent":{"mcp":` + mcp + `}}}` }
	for name, block := range map[string]string{
		"no schema":       `{"agents":{}}`,
		"schema zero":     `{"schema":0}`,
		"unknown agent":   `{"schema":1,"agents":{"other":{}}}`,
		"agent is mcp":    `{"schema":1,"agents":{"b-mcp":{}}}`,
		"server is agent": `{"schema":1,"servers":{"@a/agent":{"id":"a","bin":"a"}}}`,
		"bad style":       agent(`{"file":"~/.a.json","style":"yaml","key":"k"}`),
		"json no key":     agent(`{"file":"~/.a.json","style":"claude"}`),
		"bad key":         agent(`{"file":"~/.a.json","style":"claude","key":"a.b"}`),
		"toml with key":   agent(`{"file":"~/.a.toml","style":"codex-toml","key":"k"}`),
		"absolute file":   agent(`{"file":"/etc/a.json","style":"claude","key":"k"}`),
		"escaping file":   agent(`{"file":"~/../a.json","style":"claude","key":"k"}`),
		"dotdot inside":   agent(`{"file":"$XDG_CONFIG_HOME/a/../../b.json","style":"claude","key":"k"}`),
		"double slash":    agent(`{"file":"~//etc/a.json","style":"claude","key":"k"}`),
		"bare home":       agent(`{"file":"~/","style":"claude","key":"k"}`),
		"bad id":          `{"schema":1,"servers":{"b-mcp":{"id":"B_1","bin":"b"}}}`,
		"duplicate id":    `{"schema":1,"servers":{"b-mcp":{"id":"x","bin":"b"},"c-mcp":{"id":"x","bin":"c"}}}`,
		"bin with slash":  `{"schema":1,"servers":{"b-mcp":{"id":"b","bin":"../b"}}}`,
		"wrong type":      `{"schema":1,"servers":[]}`,
	} {
		_, err := ParseDescriptors([]byte(`{"berthAgents":`+block+`}`), descMeta)
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestParseCarriesDescriptors(t *testing.T) {
	pkg := []byte(`{"dependencies":{"b-mcp":"1.0.0"},
		"berth":{"b-mcp":{"kind":"mcp","name":"B","keys":[]}},
		"berthAgents":{"schema":1,"servers":{"b-mcp":{"id":"b","bin":"b-mcp"}}}}`)
	lock := []byte(`{"lockfileVersion":3,"packages":{"":{"dependencies":{"b-mcp":"1.0.0"}}}}`)
	l, err := Parse(pkg, lock, Source{Origin: OriginDir})
	if err != nil {
		t.Fatal(err)
	}
	if l.Descriptors.Servers["b-mcp"].ID != "b" {
		t.Errorf("descriptors = %+v", l.Descriptors)
	}
	bad := []byte(`{"dependencies":{"b-mcp":"1.0.0"},"berthAgents":{"schema":9}}`)
	if _, err := Parse(bad, lock, Source{Origin: OriginDir}); !errors.Is(err, ErrInvalid) {
		t.Errorf("Parse accepted schema 9: %v", err)
	}
}

func TestRepositoryLockDescriptors(t *testing.T) {
	lock, err := FromDir("../../agents", Source{Origin: OriginImage})
	if err != nil {
		t.Fatal(err)
	}
	for dep, style := range map[string]string{
		"@anthropic-ai/claude-code": StyleClaude,
		"@openai/codex":             StyleCodexTOML,
		"opencode-ai":               StyleOpenCode,
		"@google/gemini-cli":        StyleClaude,
	} {
		if a := lock.Descriptors.Agents[dep]; a.MCP == nil || a.MCP.Style != style {
			t.Errorf("%s descriptor = %+v, want style %s", dep, a.MCP, style)
		}
	}
	if s := lock.Descriptors.Servers["@playwright/mcp"]; s.ID != "playwright" || s.Bin != "playwright-mcp" {
		t.Errorf("playwright = %+v", s)
	}
	if s := lock.Descriptors.Servers["@upstash/context7-mcp"]; s.ID != "context7" || s.Bin != "context7-mcp" {
		t.Errorf("context7 = %+v", s)
	}
	if m := lock.Meta["@google/gemini-cli"]; m.Name != "Gemini CLI" || len(m.Keys) != 1 || m.Keys[0] != "GEMINI_API_KEY" {
		t.Errorf("gemini meta = %+v", m)
	}
	methods := map[string][]string{
		"@anthropic-ai/claude-code": {"claude"},
		"@openai/codex":             {"chatgpt"},
		"@google/gemini-cli":        {"google"},
		"opencode-ai":               {"chatgpt", "copilot"},
	}
	for dep, want := range methods {
		l := lock.Descriptors.Agents[dep].Login
		if l == nil {
			t.Errorf("%s has no login", dep)
			continue
		}
		var got []string
		for _, m := range l.Methods {
			got = append(got, m.ID)
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s methods = %v, want %v", dep, got, want)
		}
	}
	for dep, icon := range map[string]string{
		"@anthropic-ai/claude-code": "claude", "@google/gemini-cli": "googlegemini",
		"opencode-ai": "opencode", "@openai/codex": "codex",
	} {
		if got := lock.Descriptors.Agents[dep].Icon; got != icon {
			t.Errorf("%s icon = %q, want %q", dep, got, icon)
		}
	}
	for dep, want := range map[string]struct {
		project, mcp, key string
		also              []string
		trust             bool
	}{
		"@anthropic-ai/claude-code": {".claude/skills", ".mcp.json", "mcpServers", []string{}, false},
		"@openai/codex":             {".agents/skills", ".codex/config.toml", "", []string{}, true},
		"opencode-ai":               {".opencode/skills", "opencode.json", "mcp", []string{".claude/skills", ".agents/skills"}, false},
		"@google/gemini-cli":        {".gemini/skills", ".gemini/settings.json", "mcpServers", []string{".agents/skills"}, false},
	} {
		a := lock.Descriptors.Agents[dep]
		if a.Skills.Project != want.project || !slices.Equal(a.Skills.ProjectAlso, want.also) {
			t.Errorf("%s skills = %+v", dep, a.Skills)
		}
		if p := a.MCP.Project; p == nil || p.File != want.mcp || p.Key != want.key || p.Trust != want.trust {
			t.Errorf("%s mcp project = %+v", dep, p)
		}
	}
	if got := lock.Descriptors.Servers["@upstash/context7-mcp"].Icon; got != "upstash" {
		t.Errorf("context7 icon = %q", got)
	}
}

func TestSkillsDescriptors(t *testing.T) {
	pkg := []byte(`{
	  "dependencies": {"claude": "1.0.0", "opencode": "1.0.0"},
	  "berth": {
	    "claude": {"kind": "agent", "name": "Claude", "keys": []},
	    "opencode": {"kind": "agent", "name": "OpenCode", "keys": []}
	  },
	  "berthAgents": {"schema": 1, "agents": {
	    "claude": {"skills": {"dir": "~/.claude/skills"}},
	    "opencode": {"skills": {"dir": "$XDG_CONFIG_HOME/opencode/skills", "also": ["~/.claude/skills"]}}
	  }}
	}`)
	meta, err := ParseMeta(pkg, map[string]string{"claude": "1.0.0", "opencode": "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := ParseDescriptors(pkg, meta)
	if err != nil {
		t.Fatal(err)
	}
	if s := d.Agents["claude"].Skills; s == nil || s.Dir != "~/.claude/skills" || s.Also == nil || len(s.Also) != 0 {
		t.Errorf("claude = %+v", s)
	}
	if got := d.SkillsVia("opencode"); len(got) != 1 || got[0] != "claude" {
		t.Errorf("SkillsVia(opencode) = %v", got)
	}
	if got := d.SkillsVia("claude"); len(got) != 0 {
		t.Errorf("SkillsVia(claude) = %v", got)
	}
}

func TestSkillsDescriptorRejects(t *testing.T) {
	for name, skills := range map[string]string{
		"absolute dir": `{"dir": "/etc/skills"}`,
		"dotdot dir":   `{"dir": "~/../skills"}`,
		"bare home":    `{"dir": "~/"}`,
		"also itself":  `{"dir": "~/.a/skills", "also": ["~/.a/skills"]}`,
		"bad also":     `{"dir": "~/.a/skills", "also": ["/x"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			pkg := []byte(`{"dependencies": {"a": "1.0.0"},
			  "berth": {"a": {"kind": "agent", "name": "A", "keys": []}},
			  "berthAgents": {"schema": 1, "agents": {"a": {"skills": ` + skills + `}}}}`)
			meta, _ := ParseMeta(pkg, map[string]string{"a": "1.0.0"})
			if _, err := ParseDescriptors(pkg, meta); err == nil || !strings.Contains(err.Error(), "skills.") {
				t.Errorf("ParseDescriptors = %v", err)
			}
		})
	}
}

const goodLogin = `{"methods":[
  {"id":"chatgpt","label":"ChatGPT","flow":"device","args":["login","--device-auth"],
   "status":{"args":["login","status"]},"logout":{"args":["logout"]}},
  {"id":"google","label":"Google account","flow":"paste","prompt":"Enter the authorization code","pty":true,
   "env":{"NO_BROWSER":"true"},"answers":[{"expect":"deployment type","send":"\r"}],
   "prepare":{"file":"~/.a/settings.json","set":"security.auth.selectedType","value":"oauth-personal"},
   "status":{"file":"$XDG_DATA_HOME/a/auth.json","key":"github-copilot","account_file":{"file":"~/.a/accounts.json","key":"active"}},
   "logout":{"remove":["~/.a/creds.json"]}}]}`

func loginPkg(login, icon string) []byte {
	return []byte(`{"dependencies":{"a":"1.0.0","s":"1.0.0"},
	  "berth":{"a":{"kind":"agent","name":"A","keys":[]},"s":{"kind":"mcp","name":"S","keys":[]}},
	  "berthAgents":{"schema":1,"agents":{"a":{"login":` + login + `,"icon":"` + icon + `"}},
	    "servers":{"s":{"id":"s","bin":"s","icon":"upstash"}}}}`)
}

func parseLoginPkg(t *testing.T, pkg []byte) (Descriptors, error) {
	t.Helper()
	meta, err := ParseMeta(pkg, map[string]string{"a": "1.0.0", "s": "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	return ParseDescriptors(pkg, meta)
}

func TestLoginDescriptors(t *testing.T) {
	d, err := parseLoginPkg(t, loginPkg(goodLogin, "claude"))
	if err != nil {
		t.Fatal(err)
	}
	a := d.Agents["a"]
	if a.Icon != "claude" || d.Servers["s"].Icon != "upstash" {
		t.Errorf("icons = %q %q", a.Icon, d.Servers["s"].Icon)
	}
	if a.Login == nil || len(a.Login.Methods) != 2 {
		t.Fatalf("login = %+v", a.Login)
	}
	dev, paste := a.Login.Methods[0], a.Login.Methods[1]
	if dev.Flow != FlowDevice || dev.PTY || !slices.Equal(dev.Status.Args, []string{"login", "status"}) {
		t.Errorf("device = %+v", dev)
	}
	if paste.Flow != FlowPaste || !paste.PTY || paste.Prompt != "Enter the authorization code" ||
		paste.Env["NO_BROWSER"] != "true" || paste.Answers[0].Send != "\r" ||
		paste.Prepare.Set != "security.auth.selectedType" || paste.Status.Key != "github-copilot" ||
		paste.Status.AccountFile.Key != "active" || paste.Logout.Remove[0] != "~/.a/creds.json" {
		t.Errorf("paste = %+v", paste)
	}
}

func TestLoginArgsDefaultEmpty(t *testing.T) {
	login := `{"methods":[{"id":"x","label":"X","flow":"device","status":{"args":["s"]},"logout":{"args":["o"]}}]}`
	d, err := parseLoginPkg(t, loginPkg(login, ""))
	if err != nil {
		t.Fatal(err)
	}
	if args := d.Agents["a"].Login.Methods[0].Args; args == nil || len(args) != 0 {
		t.Errorf("args = %#v, want empty non-nil", args)
	}
}

func TestLoginDescriptorRejects(t *testing.T) {
	// A later duplicate JSON key wins, so method(`,"flow":"link"`) overrides
	// "device"; a duplicate object merges into the first, so "neither" cases
	// spell the method out.
	method := func(fields string) string {
		return `{"methods":[{"id":"x","label":"X","flow":"device","status":{"args":["s"]},"logout":{"args":["o"]}` + fields + `}]}`
	}
	one := `{"id":"x","label":"X","flow":"device","status":{"args":["s"]},"logout":{"args":["o"]}}`
	for name, login := range map[string]string{
		"no methods":         `{"methods":[]}`,
		"bad id":             `{"methods":[{"id":"X_1","label":"X","flow":"device","status":{"args":["s"]},"logout":{"args":["o"]}}]}`,
		"duplicate id":       `{"methods":[` + one + `,` + one + `]}`,
		"empty label":        `{"methods":[{"id":"x","label":"","flow":"device","status":{"args":["s"]},"logout":{"args":["o"]}}]}`,
		"bad flow":           method(`,"flow":"link"`),
		"device with prompt": method(`,"prompt":"Paste"`),
		"paste no prompt":    method(`,"flow":"paste"`),
		"bad env":            method(`,"env":{"lower":"x"}`),
		"nul arg":            method(`,"args":["a\u0000b"]`),
		"five answers":       method(`,"answers":[` + strings.Repeat(`{"expect":"a","send":"b"},`, 4) + `{"expect":"a","send":"b"}]`),
		"long send":          method(`,"answers":[{"expect":"a","send":"12345678901234567"}]`),
		"prepare outside":    method(`,"prepare":{"file":"/etc/x.json","set":"a","value":"b"}`),
		"prepare bad set":    method(`,"prepare":{"file":"~/.x.json","set":"a..b","value":"b"}`),
		"status both":        method(`,"status":{"args":["s"],"file":"~/.x"}`),
		"status neither":     `{"methods":[{"id":"x","label":"X","flow":"device","status":{},"logout":{"args":["o"]}}]}`,
		"status empty args":  method(`,"status":{"args":[]}`),
		"account two groups": method(`,"status":{"args":["s"],"account":"(a)(b)"}`),
		"account bad regex":  method(`,"status":{"args":["s"],"account":"("}`),
		"account with file":  method(`,"status":{"file":"~/.x","account":"(a)"}`),
		"key with args":      method(`,"status":{"args":["s"],"key":"k"}`),
		"bad status key":     method(`,"status":{"file":"~/.x","key":"a b"}`),
		"bad account file":   method(`,"status":{"file":"~/.x","account_file":{"file":"/x","key":"k"}}`),
		"logout both":        method(`,"logout":{"args":["o"],"remove":["~/.x"]}`),
		"logout neither":     `{"methods":[{"id":"x","label":"X","flow":"device","status":{"args":["s"]},"logout":{}}]}`,
		"remove outside":     method(`,"logout":{"remove":["~/../x"]}`),
		"remove five":        method(`,"logout":{"remove":["~/.a","~/.b","~/.c","~/.d","~/.e"]}`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseLoginPkg(t, loginPkg(login, "")); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "login") {
				t.Errorf("err = %v", err)
			}
		})
	}
	if _, err := parseLoginPkg(t, loginPkg(goodLogin, "Bad_Icon")); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad icon: err = %v", err)
	}
}

func TestDataHomePaths(t *testing.T) {
	pkg := []byte(`{"dependencies":{"a":"1.0.0"},"berth":{"a":{"kind":"agent","name":"A","keys":[]}},
	  "berthAgents":{"schema":1,"agents":{"a":{"skills":{"dir":"$XDG_DATA_HOME/a/skills"}}}}}`)
	meta, _ := ParseMeta(pkg, map[string]string{"a": "1.0.0"})
	if _, err := ParseDescriptors(pkg, meta); err != nil {
		t.Errorf("$XDG_DATA_HOME/ rejected: %v", err)
	}
}

func TestProjectDescriptors(t *testing.T) {
	pkg := []byte(`{"berthAgents":{"schema":1,"agents":{"@a/agent":{
		"skills":{"dir":"~/.a/skills","project":".a/skills","project_also":[".agents/skills"]},
		"mcp":{"file":"~/.a/config.toml","style":"codex-toml","project":{"file":".a/config.toml","trust":true}}}}}}`)
	d, err := ParseDescriptors(pkg, descMeta)
	if err != nil {
		t.Fatal(err)
	}
	a := d.Agents["@a/agent"]
	if a.Skills.Project != ".a/skills" || !slices.Equal(a.Skills.ProjectAlso, []string{".agents/skills"}) {
		t.Errorf("skills = %+v", a.Skills)
	}
	if p := a.MCP.Project; p == nil || p.File != ".a/config.toml" || p.Key != "" || !p.Trust {
		t.Errorf("mcp project = %+v", p)
	}
	plain := []byte(`{"berthAgents":{"schema":1,"agents":{"@a/agent":{"skills":{"dir":"~/.a/skills"},
		"mcp":{"file":"~/.a.json","style":"claude","key":"mcpServers"}}}}}`)
	if d, err = ParseDescriptors(plain, descMeta); err != nil {
		t.Fatal(err)
	}
	if a := d.Agents["@a/agent"]; a.Skills.Project != "" || a.Skills.ProjectAlso == nil || a.MCP.Project != nil {
		t.Errorf("without project fields = %+v %+v", a.Skills, a.MCP)
	}
}

func TestParseRejectsBadProjectDescriptors(t *testing.T) {
	skills := func(s string) string { return `{"schema":1,"agents":{"@a/agent":{"skills":` + s + `}}}` }
	mcp := func(style, key, project string) string {
		return `{"schema":1,"agents":{"@a/agent":{"mcp":{"file":"~/.a","style":"` + style + `","key":"` + key + `","project":` + project + `}}}}`
	}
	for name, block := range map[string]string{
		"absolute project":  skills(`{"dir":"~/.a/skills","project":"/a/skills"}`),
		"home project":      skills(`{"dir":"~/.a/skills","project":"~/.a/skills"}`),
		"variable project":  skills(`{"dir":"~/.a/skills","project":"$HOME/a"}`),
		"escaping project":  skills(`{"dir":"~/.a/skills","project":"../a/skills"}`),
		"dotdot inside":     skills(`{"dir":"~/.a/skills","project":"a/../../b"}`),
		"dot project":       skills(`{"dir":"~/.a/skills","project":"."}`),
		"git project":       skills(`{"dir":"~/.a/skills","project":".git/skills"}`),
		"backslash project": skills(`{"dir":"~/.a/skills","project":".a\\skills"}`),
		"unclean project":   skills(`{"dir":"~/.a/skills","project":".a//skills"}`),
		"also own project":  skills(`{"dir":"~/.a/skills","project":".a/skills","project_also":[".a/skills"]}`),
		"also bad":          skills(`{"dir":"~/.a/skills","project":".a/skills","project_also":["/b"]}`),
		"also, no project":  skills(`{"dir":"~/.a/skills","project_also":[".b/skills"]}`),
		"json project key":  mcp("claude", "mcpServers", `{"file":".a.json","key":"a.b"}`),
		"json no key":       mcp("claude", "mcpServers", `{"file":".a.json"}`),
		"toml project key":  mcp("codex-toml", "", `{"file":".a.toml","key":"k"}`),
		"trust on json":     mcp("claude", "mcpServers", `{"file":".a.json","key":"mcpServers","trust":true}`),
		"absolute mcp file": mcp("claude", "mcpServers", `{"file":"/etc/a.json","key":"mcpServers"}`),
		"git mcp file":      mcp("claude", "mcpServers", `{"file":".git/config","key":"mcpServers"}`),
	} {
		_, err := ParseDescriptors([]byte(`{"berthAgents":`+block+`}`), descMeta)
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestInstructionsDescriptor(t *testing.T) {
	meta := map[string]Meta{"a": {Kind: KindAgent, Name: "A"}, "b": {Kind: KindAgent, Name: "B"}, "c": {Kind: KindAgent, Name: "C"}}
	parse := func(instr string) (Descriptors, error) {
		raw := `{"berthAgents":{"schema":1,"agents":{"a":{"instructions":` + strconv.Quote(instr) + `},` +
			`"b":{"instructions":"AGENTS.md"},"c":{}}}}`
		return ParseDescriptors([]byte(raw), meta)
	}
	d, err := parse("CLAUDE.md")
	if err != nil {
		t.Fatal(err)
	}
	if got := d.InstructionFiles([]string{"a", "b", "c"}); !slices.Equal(got, []string{"AGENTS.md", "CLAUDE.md"}) {
		t.Errorf("InstructionFiles = %v", got)
	}
	if got := d.InstructionFiles([]string{"b", "b"}); !slices.Equal(got, []string{"AGENTS.md"}) {
		t.Errorf("InstructionFiles(b) = %v", got)
	}
	if got := d.InstructionFiles(nil); len(got) != 0 {
		t.Errorf("InstructionFiles(nil) = %v", got)
	}
	for _, bad := range []string{"docs/CLAUDE.md", "../CLAUDE.md", ".CLAUDE.md", "CLAUDE.txt", "CLAUDE", "a b.md", `x\y.md`, "/CLAUDE.md"} {
		if _, err := parse(bad); err == nil || !strings.Contains(err.Error(), "instructions") {
			t.Errorf("%q: err = %v", bad, err)
		}
	}
}

func TestUsageDescriptor(t *testing.T) {
	meta := map[string]Meta{"a": {Kind: KindAgent, Name: "A"}, "b": {Kind: KindAgent, Name: "B"}}
	parse := func(usage string) (Descriptors, error) {
		raw := `{"berthAgents":{"schema":1,"agents":{"a":{"usage":` + usage + `},"b":{}}}}`
		return ParseDescriptors([]byte(raw), meta)
	}
	d, err := parse(`{"format":"claude-code","dir":"~/.claude/projects"}`)
	if err != nil {
		t.Fatal(err)
	}
	if u := d.Agents["a"].Usage; u == nil || u.Format != "claude-code" || u.Dir != "~/.claude/projects" {
		t.Errorf("a usage = %+v", u)
	}
	if d.Agents["b"].Usage != nil {
		t.Errorf("b usage = %+v", d.Agents["b"].Usage)
	}
	// A format this build cannot read still parses: the reader reports it.
	if _, err := parse(`{"format":"from-the-future","dir":"$XDG_DATA_HOME/x"}`); err != nil {
		t.Errorf("unknown format: %v", err)
	}
	for _, bad := range []string{
		`{"format":"","dir":"~/.x"}`,
		`{"format":"Claude Code","dir":"~/.x"}`,
		`{"format":"claude-code","dir":"/etc"}`,
		`{"format":"claude-code","dir":"~/../etc"}`,
		`{"format":"claude-code"}`,
	} {
		if _, err := parse(bad); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "usage") {
			t.Errorf("%s: err = %v", bad, err)
		}
	}
}
