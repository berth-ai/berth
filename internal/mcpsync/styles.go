package mcpsync

import (
	"fmt"

	"github.com/berth-ai/berth/internal/locksource"
)

// server is one enabled MCP server as agents reach it: a local command, or
// a remote URL when URL is set.
type server struct {
	ID       string
	Command  string
	Args     []string
	URL      string
	Headers  map[string]string // sent with every request to URL
	TokenVar string            // a bearer token's agents.env variable, referenced instead of sent (workspace files)
}

// jsonEntries renders servers in a JSON style.
func jsonEntries(style string, servers []server) ([]entry, error) {
	if style != locksource.StyleClaude && style != locksource.StyleOpenCode {
		return nil, fmt.Errorf("style %s is not a JSON style", style)
	}
	out := make([]entry, 0, len(servers))
	for _, s := range servers {
		raw, err := marshal(jsonEntry(style, s))
		if err != nil {
			return nil, err
		}
		out = append(out, entry{id: s.ID, val: raw})
	}
	return out, nil
}

func jsonEntry(style string, s server) any {
	type remote struct {
		Type    string            `json:"type"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers,omitempty"`
	}
	args := s.Args
	if args == nil {
		args = []string{}
	}
	if s.TokenVar != "" {
		ref := "${" + s.TokenVar + "}"
		if style == locksource.StyleOpenCode {
			ref = "{env:" + s.TokenVar + "}"
		}
		s.Headers = map[string]string{"Authorization": "Bearer " + ref}
	}
	switch {
	case s.URL != "" && style == locksource.StyleClaude:
		return remote{"http", s.URL, s.Headers}
	case s.URL != "":
		return struct {
			remote
			Enabled bool `json:"enabled"`
		}{remote{"remote", s.URL, s.Headers}, true}
	case style == locksource.StyleClaude:
		return struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		}{s.Command, args}
	default:
		return struct {
			Type    string   `json:"type"`
			Command []string `json:"command"`
			Enabled bool     `json:"enabled"`
		}{"local", append([]string{s.Command}, args...), true}
	}
}
