package mcpsync

import (
	"fmt"

	"github.com/berth-ai/berth/internal/locksource"
)

// server is one enabled MCP server as agents launch it.
type server struct {
	ID      string
	Command string
	Args    []string
}

// jsonEntries renders servers in a JSON style.
func jsonEntries(style string, servers []server) ([]entry, error) {
	out := make([]entry, 0, len(servers))
	for _, s := range servers {
		args := s.Args
		if args == nil {
			args = []string{}
		}
		var v any
		switch style {
		case locksource.StyleClaude:
			v = struct {
				Command string   `json:"command"`
				Args    []string `json:"args"`
			}{s.Command, args}
		case locksource.StyleOpenCode:
			v = struct {
				Type    string   `json:"type"`
				Command []string `json:"command"`
				Enabled bool     `json:"enabled"`
			}{"local", append([]string{s.Command}, args...), true}
		default:
			return nil, fmt.Errorf("style %s is not a JSON style", style)
		}
		raw, err := marshal(v)
		if err != nil {
			return nil, err
		}
		out = append(out, entry{id: s.ID, val: raw})
	}
	return out, nil
}
