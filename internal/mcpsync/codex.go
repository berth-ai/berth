package mcpsync

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

const (
	blockStart = "# >>> berth mcp (managed; edits are overwritten)"
	blockEnd   = "# <<< berth mcp"
	// marker stands in for the old block's position while lines are filtered.
	marker = "\x00berth-block"
)

// errUnsafeTOML marks a Codex config that line-based editing could corrupt.
var errUnsafeTOML = errors.New("cannot edit safely (an unterminated multi-line string or Berth block)")

var mcpTable = regexp.MustCompile(`^\[\s*mcp_servers\.(?:"([A-Za-z0-9_-]+)"|([A-Za-z0-9_-]+))(?:\.[^\]]*)?\s*\]\s*(?:#.*)?$`)

// tableID returns the server id of an [mcp_servers.<id>…] header, or "".
func tableID(header string) string {
	m := mcpTable.FindStringSubmatch(header)
	if m == nil {
		return ""
	}
	return m[1] + m[2]
}

// editCodex rewrites Berth's block in a Codex config.toml so it holds
// servers, and removes the top-level [mcp_servers.<id>] tables of owned ids
// that `codex mcp add` wrote before Berth kept a block. A server the user
// defines outside the block is left out and returned in skipped. It returns
// data itself when nothing changes.
func editCodex(data []byte, servers []server, owned []string) ([]byte, []string, error) {
	text := string(data)
	if strings.Count(text, `"""`)%2 == 1 || strings.Count(text, `'''`)%2 == 1 {
		return nil, nil, errUnsafeTOML
	}
	lines := strings.Split(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	// Cut the old block out, leaving a marker where it started. Codex's own
	// edits can land between the markers (it appends tables before trailing
	// comments), so only Berth's tables go; anything else moves after the
	// block.
	berths := func(id string) bool {
		return slices.Contains(owned, id) || slices.ContainsFunc(servers, func(s server) bool { return s.ID == id })
	}
	var kept, foreign []string
	inBlock, placed, ours := false, false, true
	for _, l := range lines {
		switch t := strings.TrimSpace(l); {
		case !inBlock && t == blockStart:
			inBlock, ours = true, true
			if !placed {
				kept = append(kept, marker)
				placed = true
			}
		case inBlock && t == blockEnd:
			inBlock = false
			kept = append(kept, foreign...)
			foreign = nil
		case inBlock:
			if strings.HasPrefix(t, "[") {
				id := tableID(t)
				ours = id != "" && berths(id)
			}
			if !ours {
				foreign = append(foreign, l)
			}
		default:
			kept = append(kept, l)
		}
	}
	if inBlock {
		return nil, nil, errUnsafeTOML
	}

	// Drop legacy tables of owned ids; note the ids the user defines.
	var rest []string
	user := map[string]bool{}
	dropping := false
	for _, l := range kept {
		if l == marker {
			dropping = false
			rest = append(rest, l)
			continue
		}
		if t := strings.TrimSpace(l); strings.HasPrefix(t, "[") {
			id := tableID(t)
			dropping = id != "" && slices.Contains(owned, id)
			if id != "" && !dropping {
				user[id] = true
			}
		}
		if !dropping {
			rest = append(rest, l)
		}
	}

	var block, skipped []string
	for _, s := range servers {
		if user[s.ID] {
			skipped = append(skipped, s.ID)
			continue
		}
		block = append(block, "[mcp_servers."+s.ID+"]", "command = "+tomlString(s.Command), "args = "+tomlArray(s.Args))
	}
	if len(block) > 0 {
		block = append(append([]string{blockStart}, block...), blockEnd)
	}

	var res []string
	for _, l := range rest {
		if l == marker {
			res = append(res, block...)
			continue
		}
		res = append(res, l)
	}
	if !placed && len(block) > 0 {
		if len(res) > 0 && strings.TrimSpace(res[len(res)-1]) != "" {
			res = append(res, "")
		}
		res = append(res, block...)
	}
	out := []byte(strings.Join(res, "\n"))
	if len(res) > 0 {
		out = append(out, '\n')
	}
	if bytes.Equal(out, data) {
		return data, skipped, nil
	}
	return out, skipped, nil
}

// tomlString quotes s as a TOML basic string.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func tomlArray(items []string) string {
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = tomlString(s)
	}
	return "[" + strings.Join(q, ", ") + "]"
}
