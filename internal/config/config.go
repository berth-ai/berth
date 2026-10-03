// Package config defines Berth's container-side settings: the schema, its
// defaults and validation, merge patches, and the root-owned store in
// /var/lib/berth that keeps the current and the previous revision.
package config

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	_ "time/tzdata" // validate zones the same way on every host
)

// Schema is the config schema this binary understands.
const Schema = 1

// Config is the content of /var/lib/berth/config.json.
type Config struct {
	Schema    int       `json:"schema"`
	Revision  int       `json:"revision"`
	Frontends Frontends `json:"frontends"`
	Agents    Agents    `json:"agents"`
	Runtime   Runtime   `json:"runtime"`
}

// Frontends switches the optional browser frontends.
type Frontends struct {
	Terminal Toggle `json:"terminal"`
	IDE      Toggle `json:"ide"`
}

// Toggle is an on/off setting.
type Toggle struct {
	Enabled bool `json:"enabled"`
}

// Agents holds the agent track's settings.
type Agents struct {
	Ref         string   `json:"ref"`
	Disabled    []string `json:"disabled"`
	Keep        int      `json:"keep"`
	BootTimeout int      `json:"boot_timeout"`
	MCPTimeout  int      `json:"mcp_timeout"`
}

// Runtime holds settings every service sees.
type Runtime struct {
	TZ string `json:"tz"`
}

// Defaults is the config in effect before anything was applied.
func Defaults() Config {
	return Config{
		Schema:    Schema,
		Frontends: Frontends{Terminal: Toggle{Enabled: true}},
		Agents:    Agents{Ref: "main", Disabled: []string{}, Keep: 3, BootTimeout: 600, MCPTimeout: 120},
		Runtime:   Runtime{TZ: "UTC"},
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
)

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
	v.between("agents.keep", c.Agents.Keep, 0, 50)
	v.between("agents.boot_timeout", c.Agents.BootTimeout, 30, 3600)
	v.between("agents.mcp_timeout", c.Agents.MCPTimeout, 10, 1800)
	if !validZone(c.Runtime.TZ) {
		v.add("runtime.tz", "%q is not a time zone such as UTC or Europe/Rome", c.Runtime.TZ)
	}
	return v.err()
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
