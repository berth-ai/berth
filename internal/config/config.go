// Package config defines Berth's container-side settings: the schema, its
// defaults and validation, merge patches, and the root-owned store in
// /var/lib/berth that keeps the current and the previous revision.
package config

import (
	"fmt"
	"maps"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	_ "time/tzdata" // validate zones the same way on every host
)

// Schema is the config schema this binary understands. Schema 1 had no mcp
// section, schema 2 no skills section, schema 3 no agents.added, schema 4
// no workspaces, and schemas 2 to 5 held skills as a flat map; Upgrade
// reads them all.
const Schema = 6

// Config is the content of /var/lib/berth/config.json.
type Config struct {
	Schema     int                  `json:"schema"`
	Revision   int                  `json:"revision"`
	Frontends  Frontends            `json:"frontends"`
	Agents     Agents               `json:"agents"`
	Runtime    Runtime              `json:"runtime"`
	MCP        MCP                  `json:"mcp"`
	Skills     Skills               `json:"skills"`
	Workspaces map[string]Workspace `json:"workspaces"`
}

// Frontends switches the optional frontends.
type Frontends struct {
	Terminal Toggle       `json:"terminal"`
	IDE      Toggle       `json:"ide"`
	Orca     OrcaFrontend `json:"orca"`
}

// OrcaFrontend switches Orca's headless server. PairingAddress is the
// address Orca apps dial; empty means the host bind address and Orca port.
type OrcaFrontend struct {
	Enabled        bool   `json:"enabled"`
	PairingAddress string `json:"pairing_address"`
}

// Toggle is an on/off setting.
type Toggle struct {
	Enabled bool `json:"enabled"`
}

// Agents holds the agent track's settings. Added lists the npm packages a
// user added from the image's registry.
type Agents struct {
	Ref         string   `json:"ref"`
	Disabled    []string `json:"disabled"`
	Added       []string `json:"added"`
	Keep        int      `json:"keep"`
	BootTimeout int      `json:"boot_timeout"`
	MCPTimeout  int      `json:"mcp_timeout"`
}

// Runtime holds settings every service sees.
type Runtime struct {
	TZ string `json:"tz"`
}

// MCP holds the MCP servers users add and the agents each server reaches.
type MCP struct {
	Servers    map[string]MCPServer `json:"servers"`
	Available  map[string][]string  `json:"available"`  // server id → agent packages; missing means all agents
	Workspaces map[string][]string  `json:"workspaces"` // server id → workspace ids; missing means none
}

// MCPServer is a remote MCP server reached over streamable HTTP.
type MCPServer struct {
	URL     string `json:"url"`
	Auth    string `json:"auth"`
	Enabled bool   `json:"enabled"`
}

// Remote server auth modes. OAuth is left to each agent's own sign-in.
const (
	AuthNone   = "none"
	AuthBearer = "bearer"
	AuthOAuth  = "oauth"
)

// Defaults is the config in effect before anything was applied.
func Defaults() Config {
	return Config{
		Schema:     Schema,
		Frontends:  Frontends{Terminal: Toggle{Enabled: true}},
		Agents:     Agents{Ref: "main", Disabled: []string{}, Added: []string{}, Keep: 3, BootTimeout: 600, MCPTimeout: 120},
		Runtime:    Runtime{TZ: "UTC"},
		MCP:        MCP{Servers: map[string]MCPServer{}, Available: map[string][]string{}, Workspaces: map[string][]string{}},
		Skills:     Skills{Sources: map[string]Source{}},
		Workspaces: map[string]Workspace{},
	}
}

// FieldError names one invalid setting by its dotted path.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationError lists every invalid setting of a config or a patch.
type ValidationError struct {
	Fields []FieldError
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		parts[i] = f.Field + ": " + f.Message
		if f.Field == "" {
			parts[i] = f.Message
		}
	}
	return "invalid config: " + strings.Join(parts, "; ")
}

var (
	refPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,199}$`)
	packagePattern = regexp.MustCompile(`^(@[a-z0-9][a-z0-9._-]*/)?[a-z0-9][a-z0-9._-]*$`)
	zonePattern    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z0-9_+-]+)*$`)
	// UserServerIDPattern caps ids at 48 characters so MCP_<ID>_TOKEN fits
	// agents.env's 64-character key names; serverIDPattern is the lock's rule.
	UserServerIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}$`)
	serverIDPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
)

// ValidPackage reports whether name is an npm package name.
func ValidPackage(name string) bool { return packagePattern.MatchString(name) }

// ValidRef reports whether ref may be passed to berth update --ref.
func ValidRef(ref string) bool {
	return refPattern.MatchString(ref) && !strings.Contains(ref, "..")
}

// Validate reports every invalid setting at once.
func (c Config) Validate() error {
	v := &validator{}
	if c.Schema != Schema {
		v.add("schema", "must be %d", Schema)
	}
	if c.Revision < 0 {
		v.add("revision", "must not be negative")
	}
	if !ValidRef(c.Agents.Ref) {
		v.add("agents.ref", "must be a branch, tag or commit of the Berth repository")
	}
	seen := map[string]bool{}
	for i, p := range c.Agents.Disabled {
		field := fmt.Sprintf("agents.disabled[%d]", i)
		switch {
		case !packagePattern.MatchString(p):
			v.add(field, "%q is not an npm package name", p)
		case seen[p]:
			v.add(field, "%q is listed twice", p)
		}
		seen[p] = true
	}
	seen = map[string]bool{}
	for i, p := range c.Agents.Added {
		field := fmt.Sprintf("agents.added[%d]", i)
		switch {
		case !packagePattern.MatchString(p):
			v.add(field, "%q is not an npm package name", p)
		case seen[p]:
			v.add(field, "%q is listed twice", p)
		}
		seen[p] = true
	}
	v.between("agents.keep", c.Agents.Keep, 0, 50)
	v.between("agents.boot_timeout", c.Agents.BootTimeout, 30, 3600)
	v.between("agents.mcp_timeout", c.Agents.MCPTimeout, 10, 1800)
	if !validZone(c.Runtime.TZ) {
		v.add("runtime.tz", "%q is not a time zone such as UTC or Europe/Rome", c.Runtime.TZ)
	}
	if msg := CheckPairingAddress(c.Frontends.Orca.PairingAddress); msg != "" {
		v.add("frontends.orca.pairing_address", "%s", msg)
	}
	c.MCP.validate(v)
	validateSources(v, c.Skills.Sources, c.Workspaces)
	validateMCPWorkspaces(v, c.MCP.Workspaces, c.Workspaces)
	validateWorkspaces(v, c.Workspaces)
	return v.err()
}

func (m MCP) validate(v *validator) {
	for _, id := range slices.Sorted(maps.Keys(m.Servers)) {
		s, field := m.Servers[id], "mcp.servers."+id
		if !UserServerIDPattern.MatchString(id) {
			v.add(field, "%q is not a server id: use 1 to 48 lowercase letters, digits and dashes", id)
			continue
		}
		if msg := checkMCPURL(s.URL); msg != "" {
			v.add(field+".url", "%s", msg)
		}
		if !slices.Contains([]string{AuthNone, AuthBearer, AuthOAuth}, s.Auth) {
			v.add(field+".auth", "must be none, bearer or oauth")
		}
	}
	for _, id := range slices.Sorted(maps.Keys(m.Available)) {
		field := "mcp.available." + id
		if !serverIDPattern.MatchString(id) {
			v.add(field, "%q is not a server id", id)
			continue
		}
		if len(m.Available[id]) == 0 {
			v.add(field, "must name at least one agent; remove it to make the server available to all agents")
		}
		for i, p := range m.Available[id] {
			if !packagePattern.MatchString(p) {
				v.add(fmt.Sprintf("%s[%d]", field, i), "%q is not an npm package name", p)
			}
		}
	}
}

// checkMCPURL returns why raw cannot be a remote MCP server's URL, or "".
func checkMCPURL(raw string) string {
	if raw == "" {
		return "is required"
	}
	if len(raw) > 2048 {
		return "must be at most 2048 characters"
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Host == "" || u.Opaque != "" {
		return "must be an absolute URL such as https://example.com/mcp"
	}
	if u.User != nil {
		return "must not contain a user name or password; use bearer auth for tokens"
	}
	switch u.Scheme {
	case "https":
		return ""
	case "http":
		if h := u.Hostname(); h == "localhost" || net.ParseIP(h).IsLoopback() {
			return ""
		}
		return "must use https unless the host is localhost"
	}
	return "must start with https://"
}

func validZone(tz string) bool {
	if tz == "Local" || !zonePattern.MatchString(tz) {
		return false
	}
	_, err := time.LoadLocation(tz)
	return err == nil
}

// normalize sorts list settings so that equal configs compare and print the same.
func (c *Config) normalize() {
	if c.Agents.Disabled == nil {
		c.Agents.Disabled = []string{}
	}
	slices.Sort(c.Agents.Disabled)
	if c.Agents.Added == nil {
		c.Agents.Added = []string{}
	}
	slices.Sort(c.Agents.Added)
	if c.MCP.Servers == nil {
		c.MCP.Servers = map[string]MCPServer{}
	}
	if c.MCP.Available == nil {
		c.MCP.Available = map[string][]string{}
	}
	for id, list := range c.MCP.Available {
		slices.Sort(list)
		c.MCP.Available[id] = slices.Compact(list)
	}
	if c.MCP.Workspaces == nil {
		c.MCP.Workspaces = map[string][]string{}
	}
	for id, list := range c.MCP.Workspaces {
		slices.Sort(list)
		c.MCP.Workspaces[id] = slices.Compact(list)
	}
	c.Skills.normalize()
	if c.Workspaces == nil {
		c.Workspaces = map[string]Workspace{}
	}
	for id, w := range c.Workspaces {
		if w.Env == nil {
			w.Env = map[string]WorkspaceEnv{}
			c.Workspaces[id] = w
		}
	}
}

type validator struct{ fields []FieldError }

func (v *validator) add(field, format string, args ...any) {
	v.fields = append(v.fields, FieldError{Field: field, Message: fmt.Sprintf(format, args...)})
}

func (v *validator) between(field string, n, lo, hi int) {
	if n < lo || n > hi {
		v.add(field, "must be between %d and %d", lo, hi)
	}
}

func (v *validator) err() error {
	if len(v.fields) == 0 {
		return nil
	}
	return &ValidationError{Fields: v.fields}
}
