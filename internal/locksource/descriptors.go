package locksource

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// DescriptorSchema is the berthAgents schema this build reads.
const DescriptorSchema = 1

// MCP registration styles: the shape of one server entry in an agent's
// configuration file.
const (
	StyleClaude    = "claude"     // JSON {"command", "args"}: Claude Code, Gemini CLI
	StyleOpenCode  = "opencode"   // JSON {"type": "local", "command": [...], "enabled": true}
	StyleCodexTOML = "codex-toml" // [mcp_servers.<id>] tables in a managed block
)

// Descriptors is the berthAgents block of package.json: how Berth wires each
// package into the agents' own configuration. Shipped images decode only the
// berth block, strictly, so every new field lands here.
type Descriptors struct {
	Agents  map[string]AgentDescriptor
	Servers map[string]ServerDescriptor
}

// Login flows.
const (
	FlowDevice = "device" // the CLI shows a URL and a code to enter there
	FlowPaste  = "paste"  // the CLI shows a URL and reads back the code that page shows
)

// AgentDescriptor describes one agent CLI.
type AgentDescriptor struct {
	MCP    *MCPTarget    `json:"mcp"`
	Skills *SkillsTarget `json:"skills"`
	Login  *LoginTarget  `json:"login"`
	Icon   string        `json:"icon"` // a panel icon slug; empty means a monogram
}

// LoginTarget lists the ways an agent CLI signs in to its vendor account.
type LoginTarget struct {
	Methods []LoginMethod `json:"methods"`
}

// LoginMethod is one sign-in an agent CLI offers.
type LoginMethod struct {
	ID      string            `json:"id"`
	Label   string            `json:"label"`
	Flow    string            `json:"flow"`
	Prompt  string            `json:"prompt"` // paste: what the CLI prints while it waits for the code
	PTY     bool              `json:"pty"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	Answers []LoginAnswer     `json:"answers"`
	Prepare *LoginPrepare     `json:"prepare"`
	Status  LoginStatus       `json:"status"`
	Logout  LoginLogout       `json:"logout"`
}

// LoginAnswer is a fixed keystroke sent once, when Expect first appears.
type LoginAnswer struct {
	Expect string `json:"expect"`
	Send   string `json:"send"`
}

// LoginPrepare sets one key of a JSON settings file before the sign-in.
type LoginPrepare struct {
	File  string `json:"file"`
	Set   string `json:"set"` // dot-separated object keys
	Value string `json:"value"`
}

// LoginStatus says whether a method is signed in: Args exits 0, or File
// exists (holding top-level Key when one is given).
type LoginStatus struct {
	Args        []string     `json:"args"`
	Account     string       `json:"account"` // a regex over Args' output with one group
	File        string       `json:"file"`
	Key         string       `json:"key"`
	AccountFile *AccountFile `json:"account_file"`
}

// AccountFile names the JSON file and top-level key that hold the account.
type AccountFile struct {
	File string `json:"file"`
	Key  string `json:"key"`
}

// LoginLogout signs a method out: run Args, or remove the files in Remove.
type LoginLogout struct {
	Args   []string `json:"args"`
	Remove []string `json:"remove"`
}

// SkillsTarget is where an agent loads skills from: its home directory and,
// in a workspace, its project folder.
type SkillsTarget struct {
	Dir         string   `json:"dir"`          // below ~/ or $XDG_CONFIG_HOME/
	Also        []string `json:"also"`         // other agents' skills directories it reads too
	Project     string   `json:"project"`      // relative to a workspace; empty means none
	ProjectAlso []string `json:"project_also"` // other agents' project folders it reads too
}

// MCPTarget is the file an agent reads its MCP servers from, and the file
// in a workspace it reads project servers from.
type MCPTarget struct {
	File    string      `json:"file"`    // below ~/ or $XDG_CONFIG_HOME/
	Style   string      `json:"style"`   // a Style constant
	Key     string      `json:"key"`     // top-level JSON key; empty for codex-toml
	Project *ProjectMCP `json:"project"` // nil means the agent reads no project file
}

// ProjectMCP is an agent's MCP file in a workspace, in its MCPTarget's
// style.
type ProjectMCP struct {
	File  string `json:"file"`  // relative to the workspace
	Key   string `json:"key"`   // top-level JSON key; empty for codex-toml
	Trust bool   `json:"trust"` // the agent reads it only in folders its user config trusts (codex-toml)
}

// ServerDescriptor describes one MCP server package.
type ServerDescriptor struct {
	ID   string   `json:"id"`
	Bin  string   `json:"bin"`
	Args []string `json:"args"`
	Icon string   `json:"icon"`
}

var (
	serverIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	jsonKeyPattern  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)
	binNamePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	// LoginIDPattern is the shape of a login method id.
	LoginIDPattern = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)
	envNamePattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`)
	iconPattern    = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)
	fileKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
)

// ValidBinName reports whether s may name a bin, and so a wrapper file.
func ValidBinName(s string) bool { return binNamePattern.MatchString(s) }

// ParseDescriptors reads the berthAgents block; meta is ParseMeta's result
// for the same file. A missing block yields empty descriptors. Unknown
// fields are ignored, so later parts add fields without a schema bump.
func ParseDescriptors(pkg []byte, meta map[string]Meta) (Descriptors, error) {
	out := Descriptors{Agents: map[string]AgentDescriptor{}, Servers: map[string]ServerDescriptor{}}
	var p struct {
		Block json.RawMessage `json:"berthAgents"`
	}
	if err := json.Unmarshal(pkg, &p); err != nil {
		return out, invalid("package.json: %v", err)
	}
	if len(p.Block) == 0 || string(p.Block) == "null" {
		return out, nil
	}
	var b struct {
		Schema  *int                        `json:"schema"`
		Agents  map[string]AgentDescriptor  `json:"agents"`
		Servers map[string]ServerDescriptor `json:"servers"`
	}
	if err := json.Unmarshal(p.Block, &b); err != nil {
		return out, invalid("package.json berthAgents block: %v", err)
	}
	switch {
	case b.Schema == nil:
		return out, invalid("package.json berthAgents block has no schema")
	case *b.Schema > DescriptorSchema:
		return out, invalid("agent lock needs a newer Berth image (descriptor schema %d)", *b.Schema)
	case *b.Schema < 1:
		return out, invalid("package.json berthAgents block: schema %d, want %d", *b.Schema, DescriptorSchema)
	}
	for _, name := range slices.Sorted(maps.Keys(b.Agents)) {
		a := b.Agents[name]
		if err := describes(meta, name, KindAgent); err != nil {
			return out, err
		}
		if a.MCP != nil {
			if err := validTarget(name, *a.MCP); err != nil {
				return out, err
			}
		}
		if a.Skills != nil {
			if err := validSkills(name, *a.Skills); err != nil {
				return out, err
			}
			if a.Skills.Also == nil {
				a.Skills.Also = []string{}
			}
			if a.Skills.ProjectAlso == nil {
				a.Skills.ProjectAlso = []string{}
			}
		}
		if a.Login != nil {
			if err := validLogin(name, a.Login); err != nil {
				return out, err
			}
		}
		if a.Icon != "" && !iconPattern.MatchString(a.Icon) {
			return out, invalid("package.json berthAgents block: %s icon %q must match %s", name, a.Icon, iconPattern)
		}
		out.Agents[name] = a
	}
	ids := map[string]string{}
	for _, name := range slices.Sorted(maps.Keys(b.Servers)) {
		s := b.Servers[name]
		if err := describes(meta, name, KindMCP); err != nil {
			return out, err
		}
		if !serverIDPattern.MatchString(s.ID) {
			return out, invalid("package.json berthAgents block: %s id %q must match %s", name, s.ID, serverIDPattern)
		}
		if prev, ok := ids[s.ID]; ok {
			return out, invalid("package.json berthAgents block: id %s is used by both %s and %s", s.ID, prev, name)
		}
		ids[s.ID] = name
		if !binNamePattern.MatchString(s.Bin) {
			return out, invalid("package.json berthAgents block: %s bin %q is not a bin name", name, s.Bin)
		}
		if s.Args == nil {
			s.Args = []string{}
		}
		if s.Icon != "" && !iconPattern.MatchString(s.Icon) {
			return out, invalid("package.json berthAgents block: %s icon %q must match %s", name, s.Icon, iconPattern)
		}
		out.Servers[name] = s
	}
	return out, nil
}

func describes(meta map[string]Meta, name, kind string) error {
	m, ok := meta[name]
	if !ok {
		return invalid("package.json berthAgents block describes %s, which is not a dependency", name)
	}
	if m.Kind != kind {
		return invalid("package.json berthAgents block: %s has kind %s in the berth block, want %s", name, m.Kind, kind)
	}
	return nil
}

func validTarget(name string, t MCPTarget) error {
	switch t.Style {
	case StyleClaude, StyleOpenCode:
		if !jsonKeyPattern.MatchString(t.Key) {
			return invalid("package.json berthAgents block: %s mcp.key %q is not a JSON key", name, t.Key)
		}
	case StyleCodexTOML:
		if t.Key != "" {
			return invalid("package.json berthAgents block: %s mcp.key must be empty for style %s", name, t.Style)
		}
	default:
		return invalid("package.json berthAgents block: %s mcp.style %q, want %s, %s or %s",
			name, t.Style, StyleClaude, StyleOpenCode, StyleCodexTOML)
	}
	if !homePath(t.File) {
		return invalid("package.json berthAgents block: %s mcp.file %q must be a path below ~/, $XDG_CONFIG_HOME/ or $XDG_DATA_HOME/", name, t.File)
	}
	if p := t.Project; p != nil {
		if !projectPath(p.File) {
			return invalid("package.json berthAgents block: %s mcp.project.file %q must be a plain path inside a workspace", name, p.File)
		}
		switch {
		case t.Style == StyleCodexTOML && p.Key != "":
			return invalid("package.json berthAgents block: %s mcp.project.key must be empty for style %s", name, t.Style)
		case t.Style != StyleCodexTOML && !jsonKeyPattern.MatchString(p.Key):
			return invalid("package.json berthAgents block: %s mcp.project.key %q is not a JSON key", name, p.Key)
		case p.Trust && t.Style != StyleCodexTOML:
			return invalid("package.json berthAgents block: %s mcp.project.trust needs style %s", name, StyleCodexTOML)
		}
	}
	return nil
}

// homePath reports whether p is a plain path below ~/, $XDG_CONFIG_HOME/ or
// $XDG_DATA_HOME/.
func homePath(p string) bool {
	var rest string
	ok := false
	for _, prefix := range []string{"~/", "$XDG_CONFIG_HOME/", "$XDG_DATA_HOME/"} {
		if rest, ok = strings.CutPrefix(p, prefix); ok {
			break
		}
	}
	return ok && rest != "" && rest != "." && rest != ".." && !path.IsAbs(rest) &&
		path.Clean(rest) == rest && !strings.HasPrefix(rest, "../")
}

// projectPath reports whether p is a plain path inside a workspace: relative,
// clean, not escaping, never in .git and never through ~ or a variable.
func projectPath(p string) bool {
	return p != "" && p != "." && p != ".." && !path.IsAbs(p) && path.Clean(p) == p &&
		!strings.HasPrefix(p, "../") && !strings.HasPrefix(p, "~") && !strings.HasPrefix(p, "$") &&
		!strings.Contains(p, `\`) && !slices.Contains(strings.Split(p, "/"), ".git")
}

func validSkills(name string, t SkillsTarget) error {
	if !homePath(t.Dir) {
		return invalid("package.json berthAgents block: %s skills.dir %q must be a path below ~/, $XDG_CONFIG_HOME/ or $XDG_DATA_HOME/", name, t.Dir)
	}
	for _, d := range t.Also {
		if !homePath(d) || d == t.Dir {
			return invalid("package.json berthAgents block: %s skills.also %q must be another agent's path below ~/, $XDG_CONFIG_HOME/ or $XDG_DATA_HOME/", name, d)
		}
	}
	if t.Project != "" && !projectPath(t.Project) {
		return invalid("package.json berthAgents block: %s skills.project %q must be a plain path inside a workspace", name, t.Project)
	}
	if len(t.ProjectAlso) > 0 && t.Project == "" {
		return invalid("package.json berthAgents block: %s skills.project_also needs skills.project", name)
	}
	for _, d := range t.ProjectAlso {
		if !projectPath(d) || d == t.Project {
			return invalid("package.json berthAgents block: %s skills.project_also %q must be another agent's project folder", name, d)
		}
	}
	return nil
}

// SkillsVia lists, sorted, the agents whose skills directory agent dep also
// reads: a skill linked there reaches dep too.
func (d Descriptors) SkillsVia(dep string) []string {
	own := d.Agents[dep].Skills
	if own == nil {
		return nil
	}
	var out []string
	for _, other := range slices.Sorted(maps.Keys(d.Agents)) {
		if t := d.Agents[other].Skills; other != dep && t != nil && slices.Contains(own.Also, t.Dir) {
			out = append(out, other)
		}
	}
	return out
}

func printable(s string, maxLen int) bool {
	return s != "" && len(s) <= maxLen && utf8.ValidString(s) && strings.IndexFunc(s, unicode.IsControl) < 0
}

func validArgs(args []string) bool {
	return !slices.ContainsFunc(args, func(a string) bool { return strings.ContainsRune(a, 0) })
}

// validKeyPath checks a dot-separated JSON key path of 1 to 8 keys.
func validKeyPath(p string) bool {
	keys := strings.Split(p, ".")
	return len(keys) <= 8 && !slices.ContainsFunc(keys, func(k string) bool { return !jsonKeyPattern.MatchString(k) })
}

func validLogin(name string, l *LoginTarget) error {
	bad := func(format string, args ...any) error {
		return invalid("package.json berthAgents block: %s login: "+format, append([]any{name}, args...)...)
	}
	if len(l.Methods) == 0 {
		return bad("needs at least one method")
	}
	seen := map[string]bool{}
	for i := range l.Methods {
		m := &l.Methods[i]
		if !LoginIDPattern.MatchString(m.ID) || seen[m.ID] {
			return bad("method id %q must match %s and be unique", m.ID, LoginIDPattern)
		}
		seen[m.ID] = true
		if !printable(m.Label, 64) {
			return bad("%s label must be 1 to 64 printable characters", m.ID)
		}
		switch m.Flow {
		case FlowPaste:
			if !printable(m.Prompt, 64) {
				return bad("%s is a paste method and needs a prompt of 1 to 64 printable characters", m.ID)
			}
		case FlowDevice:
			if m.Prompt != "" {
				return bad("%s is a device method and takes no prompt", m.ID)
			}
		default:
			return bad("%s flow %q, want %s or %s", m.ID, m.Flow, FlowDevice, FlowPaste)
		}
		if !validArgs(m.Args) {
			return bad("%s args must not contain NUL", m.ID)
		}
		if m.Args == nil {
			m.Args = []string{}
		}
		for k, v := range m.Env {
			if !envNamePattern.MatchString(k) || strings.ContainsRune(v, 0) {
				return bad("%s env %q must be a variable name with a value without NUL", m.ID, k)
			}
		}
		if len(m.Answers) > 4 {
			return bad("%s has more than 4 answers", m.ID)
		}
		for _, a := range m.Answers {
			if !printable(a.Expect, 64) || a.Send == "" || len(a.Send) > 16 {
				return bad("%s answers need an expect of 1 to 64 printable characters and a send of 1 to 16 bytes", m.ID)
			}
		}
		if p := m.Prepare; p != nil && (!homePath(p.File) || !validKeyPath(p.Set)) {
			return bad("%s prepare needs a file below ~/, $XDG_CONFIG_HOME/ or $XDG_DATA_HOME/ and a dotted key path", m.ID)
		}
		if err := validLoginStatus(m.Status); err != nil {
			return bad("%s status: %v", m.ID, err)
		}
		if err := validLogout(m.Logout); err != nil {
			return bad("%s logout: %v", m.ID, err)
		}
	}
	return nil
}

func validLoginStatus(s LoginStatus) error {
	switch {
	case (s.Args != nil) == (s.File != ""):
		return errors.New("needs exactly one of args or file")
	case s.Args != nil:
		if len(s.Args) == 0 || !validArgs(s.Args) {
			return errors.New("args must be non-empty and without NUL")
		}
		if s.Key != "" || s.AccountFile != nil {
			return errors.New("key and account_file go with file")
		}
		if s.Account != "" {
			re, err := regexp.Compile(s.Account)
			if err != nil || re.NumSubexp() != 1 {
				return errors.New("account must be a regex with exactly one group")
			}
		}
	default:
		if !homePath(s.File) {
			return errors.New("file must be below ~/, $XDG_CONFIG_HOME/ or $XDG_DATA_HOME/")
		}
		if s.Key != "" && !fileKeyPattern.MatchString(s.Key) {
			return fmt.Errorf("key %q must match %s", s.Key, fileKeyPattern)
		}
		if s.Account != "" {
			return errors.New("account goes with args; use account_file")
		}
		if a := s.AccountFile; a != nil && (!homePath(a.File) || !fileKeyPattern.MatchString(a.Key)) {
			return errors.New("account_file needs a home path and a key")
		}
	}
	return nil
}

func validLogout(l LoginLogout) error {
	switch {
	case (l.Args != nil) == (l.Remove != nil):
		return errors.New("needs exactly one of args or remove")
	case l.Args != nil:
		if len(l.Args) == 0 || !validArgs(l.Args) {
			return errors.New("args must be non-empty and without NUL")
		}
	default:
		if len(l.Remove) == 0 || len(l.Remove) > 4 || slices.ContainsFunc(l.Remove, func(f string) bool { return !homePath(f) }) {
			return errors.New("remove needs 1 to 4 paths below ~/, $XDG_CONFIG_HOME/ or $XDG_DATA_HOME/")
		}
	}
	return nil
}
