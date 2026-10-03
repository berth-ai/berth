package locksource

import (
	"encoding/json"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
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

// AgentDescriptor describes one agent CLI.
type AgentDescriptor struct {
	MCP    *MCPTarget    `json:"mcp"`
	Skills *SkillsTarget `json:"skills"`
}

// SkillsTarget is the directory an agent loads user skills from.
type SkillsTarget struct {
	Dir  string   `json:"dir"`  // below ~/ or $XDG_CONFIG_HOME/
	Also []string `json:"also"` // other agents' skills directories it reads too
}

// MCPTarget is the file an agent reads its MCP servers from.
type MCPTarget struct {
	File  string `json:"file"`  // below ~/ or $XDG_CONFIG_HOME/
	Style string `json:"style"` // a Style constant
	Key   string `json:"key"`   // top-level JSON key; empty for codex-toml
}

// ServerDescriptor describes one MCP server package.
type ServerDescriptor struct {
	ID   string   `json:"id"`
	Bin  string   `json:"bin"`
	Args []string `json:"args"`
}

var (
	serverIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	jsonKeyPattern  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)
	binNamePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

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
		return invalid("package.json berthAgents block: %s mcp.file %q must be a path below ~/ or $XDG_CONFIG_HOME/", name, t.File)
	}
	return nil
}

// homePath reports whether p is a plain path below ~/ or $XDG_CONFIG_HOME/.
func homePath(p string) bool {
	rest, ok := strings.CutPrefix(p, "~/")
	if !ok {
		rest, ok = strings.CutPrefix(p, "$XDG_CONFIG_HOME/")
	}
	return ok && rest != "" && rest != "." && rest != ".." && !path.IsAbs(rest) &&
		path.Clean(rest) == rest && !strings.HasPrefix(rest, "../")
}

func validSkills(name string, t SkillsTarget) error {
	if !homePath(t.Dir) {
		return invalid("package.json berthAgents block: %s skills.dir %q must be a path below ~/ or $XDG_CONFIG_HOME/", name, t.Dir)
	}
	for _, d := range t.Also {
		if !homePath(d) || d == t.Dir {
			return invalid("package.json berthAgents block: %s skills.also %q must be another agent's path below ~/ or $XDG_CONFIG_HOME/", name, d)
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
