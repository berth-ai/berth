// Package mcpsync registers Berth's enabled MCP servers with every enabled
// agent by editing the agents' own configuration files, as the lock's
// berthAgents descriptors describe them. It only changes entries it wrote.
package mcpsync

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/berth-ai/berth/internal/agentstore"
	"github.com/berth-ai/berth/internal/locksource"
)

// maxAttempts bounds retries when a file changes while Berth edits it.
const maxAttempts = 3

var errChanged = errors.New("changed while Berth edited it")

// Syncer converges MCP registrations for the installed agent set. An agent
// or server is enabled when its wrapper exists in Layer.
type Syncer struct {
	Current    string // the installed agent set, /opt/agents/current
	Layer      string // the wrapper layer, /var/lib/berth/bin
	Home       string // the agent user's home
	ConfigHome string // $XDG_CONFIG_HOME; empty means Home/.config
	Log        io.Writer

	// beforeRename runs between writing a temp file and replacing path;
	// tests use it to simulate a concurrent writer.
	beforeRename func(path string)
}

// Run converges every enabled agent that has an MCP descriptor. One agent
// failing does not stop the others; the error names every failed agent.
func (s Syncer) Run() error {
	raw, err := os.ReadFile(filepath.Join(s.Current, "package.json"))
	if err != nil {
		return err
	}
	var p struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("package.json: %w", err)
	}
	meta, err := locksource.ParseMeta(raw, p.Dependencies)
	if err != nil {
		return err
	}
	desc, err := locksource.ParseDescriptors(raw, meta)
	if err != nil {
		return err
	}
	servers := s.enabledServers(desc)

	statePath := StatePath(s.Home)
	st, err := loadState(statePath)
	if err != nil {
		s.logf("%v; starting over", err)
		st = map[string][]string{}
	}

	var failed []string
	for _, dep := range slices.Sorted(maps.Keys(desc.Agents)) {
		t, name := desc.Agents[dep].MCP, meta[dep].Name
		if t == nil {
			continue
		}
		if !s.agentEnabled(dep) {
			s.logf("%s not enabled; skipping", name)
			continue
		}
		path := s.resolve(t.File)
		owned, synced := st[path]
		if !synced && slices.Contains(legacyAgents, dep) {
			// Berth never synced this file; berth-mcp-setup may have
			// registered its servers there.
			owned = slices.Clone(legacyIDs)
		}
		ids, err := s.syncFile(path, *t, servers, owned)
		if err != nil {
			s.logf("failed to converge MCP servers for %s: %v", name, err)
			failed = append(failed, name)
			continue
		}
		st[path] = ids
		s.logf("converged MCP servers for %s", name)
	}
	if err := saveState(statePath, st); err != nil {
		return fmt.Errorf("save %s: %w", statePath, err)
	}
	if len(failed) > 0 {
		return fmt.Errorf("MCP registration failed for %s", strings.Join(failed, ", "))
	}
	return nil
}

func (s Syncer) logf(format string, args ...any) {
	if s.Log != nil {
		fmt.Fprintf(s.Log, "berth mcp sync: "+format+"\n", args...)
	}
}

// resolve expands a descriptor path; ParseDescriptors guarantees the prefix.
func (s Syncer) resolve(file string) string {
	if rest, ok := strings.CutPrefix(file, "~/"); ok {
		return filepath.Join(s.Home, filepath.FromSlash(rest))
	}
	base := s.ConfigHome
	if base == "" {
		base = filepath.Join(s.Home, ".config")
	}
	return filepath.Join(base, filepath.FromSlash(strings.TrimPrefix(file, "$XDG_CONFIG_HOME/")))
}

func (s Syncer) enabledServers(d locksource.Descriptors) []server {
	var out []server
	for _, dep := range slices.Sorted(maps.Keys(d.Servers)) {
		sd := d.Servers[dep]
		wrapper := filepath.Join(s.Layer, sd.Bin)
		if isExecutable(wrapper) {
			out = append(out, server{ID: sd.ID, Command: wrapper, Args: sd.Args})
		}
	}
	slices.SortFunc(out, func(a, b server) int { return strings.Compare(a.ID, b.ID) })
	return out
}

func (s Syncer) agentEnabled(dep string) bool {
	bins, err := agentstore.PackageBins(s.Current, dep)
	if err != nil {
		return false
	}
	for bin := range bins {
		if isExecutable(filepath.Join(s.Layer, bin)) {
			return true
		}
	}
	return false
}

func isExecutable(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0
}

// syncFile converges one config file and returns the ids Berth now owns
// there.
func (s Syncer) syncFile(path string, t locksource.MCPTarget, servers []server, owned []string) ([]string, error) {
	real, err := realPath(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
		return nil, err
	}
	for range maxAttempts {
		data, perm, err := readConfig(real)
		if err != nil {
			return nil, err
		}
		out, ids, err := s.render(path, t, data, servers, owned)
		if err != nil {
			return nil, err
		}
		if bytes.Equal(out, data) && (perm != 0 || len(out) == 0) {
			return ids, nil
		}
		if perm == 0 {
			perm = 0o600
		}
		err = writeAtomic(real, out, perm, func() error {
			if s.beforeRename != nil {
				s.beforeRename(real)
			}
			now, _, err := readConfig(real)
			if err != nil {
				return err
			}
			if !bytes.Equal(now, data) {
				return errChanged
			}
			return nil
		})
		if errors.Is(err, errChanged) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return ids, nil
	}
	return nil, fmt.Errorf("%s kept changing while Berth edited it", path)
}

// render computes a file's new content and the ids Berth owns in it.
func (s Syncer) render(path string, t locksource.MCPTarget, data []byte, servers []server, owned []string) ([]byte, []string, error) {
	var out []byte
	var skipped []string
	if t.Style == locksource.StyleCodexTOML {
		var err error
		out, skipped, err = editCodex(data, servers, owned)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w; leaving it unchanged", path, err)
		}
	} else {
		existing, err := objectKeys(data, t.Key)
		if errors.Is(err, errNotPlainJSON) {
			return nil, nil, fmt.Errorf("%s is not plain JSON; leaving it unchanged", path)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", path, err)
		}
		var keep []server
		for _, sv := range servers {
			if slices.Contains(existing, sv.ID) && !slices.Contains(owned, sv.ID) {
				skipped = append(skipped, sv.ID)
				continue
			}
			keep = append(keep, sv)
		}
		entries, err := jsonEntries(t.Style, keep)
		if err != nil {
			return nil, nil, err
		}
		var del []string
		for _, id := range owned {
			if !slices.ContainsFunc(keep, func(sv server) bool { return sv.ID == id }) {
				del = append(del, id)
			}
		}
		if out, err = editJSON(data, t.Key, entries, del); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	for _, id := range skipped {
		s.logf("%s already defines the MCP server %s; not registering Berth's there", path, id)
	}
	return out, idsExcept(servers, skipped), nil
}
