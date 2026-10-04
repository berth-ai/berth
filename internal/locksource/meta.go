package locksource

import (
	"bytes"
	"encoding/json"
	"maps"
	"regexp"
	"slices"
)

// Package kinds.
const (
	KindAgent = "agent"
	KindMCP   = "mcp"
)

var keyNamePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

// Meta describes one package of an agent set for the panel: what it is,
// what to call it, a one-line description, and which API keys it reads.
type Meta struct {
	Kind        string   `json:"kind"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Keys        []string `json:"keys"`
}

// ParseMeta reads the berth block of package.json. Every dependency gets an
// entry; one the block does not describe is an agent named after its package.
func ParseMeta(pkg []byte, deps map[string]string) (map[string]Meta, error) {
	var p struct {
		Berth json.RawMessage `json:"berth"`
	}
	if err := json.Unmarshal(pkg, &p); err != nil {
		return nil, invalid("package.json: %v", err)
	}
	given := map[string]Meta{}
	if len(p.Berth) > 0 {
		dec := json.NewDecoder(bytes.NewReader(p.Berth))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&given); err != nil {
			return nil, invalid("package.json berth block: %v", err)
		}
	}
	owner := map[string]string{}
	for _, name := range slices.Sorted(maps.Keys(given)) {
		m := given[name]
		if _, ok := deps[name]; !ok {
			return nil, invalid("package.json berth block describes %s, which is not a dependency", name)
		}
		if m.Kind != KindAgent && m.Kind != KindMCP {
			return nil, invalid("package.json berth block: %s has kind %q, want agent or mcp", name, m.Kind)
		}
		if m.Name == "" {
			return nil, invalid("package.json berth block: %s has no name", name)
		}
		if m.Description != "" && !printable(m.Description, 120) {
			return nil, invalid("package.json berth block: %s description must be 1 to 120 printable characters", name)
		}
		for _, k := range m.Keys {
			if !keyNamePattern.MatchString(k) {
				return nil, invalid("package.json berth block: %s key %q is not an environment variable name", name, k)
			}
			if prev, ok := owner[k]; ok {
				return nil, invalid("package.json berth block: key %s is declared by both %s and %s", k, prev, name)
			}
			owner[k] = name
		}
	}
	out := make(map[string]Meta, len(deps))
	for name := range deps {
		m, ok := given[name]
		if !ok {
			m = Meta{Kind: KindAgent, Name: name}
		}
		if m.Keys == nil {
			m.Keys = []string{}
		}
		out[name] = m
	}
	return out, nil
}
