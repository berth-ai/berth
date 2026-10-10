// Package mcpsync registers Berth's enabled MCP servers with every enabled
// agent by editing the agents' own configuration files, as the lock's
// berthAgents descriptors describe them. It only changes entries it wrote.
package mcpsync

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/berth-ai/berth/internal/agentlayer"
	"github.com/berth-ai/berth/internal/agentset"
	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/locksource"
)

// maxAttempts bounds retries when a file changes while Berth edits it.
const maxAttempts = 3

var errChanged = errors.New("changed while Berth edited it")

// Syncer converges MCP registrations for the installed agent set. An agent
// or server is enabled when its wrapper exists in Layer.
type Syncer struct {
	Set        agentset.Set // the merged agent set
	Layer      string       // the wrapper layer, /var/lib/berth/bin
	Home       string       // the agent user's home
	ConfigHome string       // $XDG_CONFIG_HOME; empty means Home/.config
	DataHome   string       // $XDG_DATA_HOME; empty means Home/.local/share
	Settings   string       // mcp.json, the user's MCP settings; empty means defaults
	Keys       string       // agents.env, which holds bearer tokens; empty means none
	Workspaces string       // workspaces.json; empty means no workspaces
	Git        string       // the git binary for tracked-file checks; empty means git
	Log        io.Writer

	// beforeRename runs between writing a temp file and replacing path;
	// tests use it to simulate a concurrent writer.
	beforeRename func(path string)
}

// Run converges every enabled agent that has an MCP descriptor. One agent
// failing does not stop the others; the error names every failed agent.
func (s Syncer) Run() error {
	desc := s.Set.Descriptors
	var err error
	settings := config.Defaults().MCP
	if s.Settings != "" {
		if settings, err = config.ReadMCPFile(s.Settings); err != nil {
			return err
		}
	}
	keys := map[string]string{}
	if s.Keys != "" {
		if keys, err = agentlayer.ReadKeys(s.Keys); err != nil {
			return err
		}
	}
	servers := s.desired(desc, settings, keys, false)
	trust, wsFailed := s.syncWorkspaces(desc, settings)

	statePath := StatePath(s.Home)
	st, err := loadState(statePath)
	if err != nil {
		s.logf("%v; starting over", err)
		st = newState()
	}

	var failed []string
	for _, dep := range slices.Sorted(maps.Keys(desc.Agents)) {
		t, name := desc.Agents[dep].MCP, s.Set.Packages[dep].Meta.Name
		if t == nil {
			continue
		}
		if !s.Set.Enabled(s.Layer, dep) {
			s.logf("%s not enabled; skipping", name)
			continue
		}
		path := s.resolve(t.File)
		owned, synced := st.Files[path]
		if !synced && slices.Contains(legacyAgents, dep) {
			// Berth never synced this file; berth-mcp-setup may have
			// registered its servers there.
			owned = slices.Clone(legacyIDs)
		}
		want := slices.Sorted(slices.Values(trust[dep]))
		ids, trusted, err := s.syncFile(path, *t, fileEdit{servers: availableTo(servers, settings.Available, dep),
			owned: owned, trust: want, trusted: st.Trust[path]})
		if err != nil {
			s.logf("failed to converge MCP servers for %s: %v", name, err)
			failed = append(failed, name)
			continue
		}
		for _, p := range want {
			if !slices.Contains(trusted, p) {
				s.logf("%s already says whether to trust %s; leaving it", path, p)
			}
		}
		st.Files[path] = ids
		if len(trusted) > 0 {
			st.Trust[path] = trusted
		} else {
			delete(st.Trust, path)
		}
		s.logf("converged MCP servers for %s", name)
	}
	if err := saveState(statePath, st); err != nil {
		return fmt.Errorf("save %s: %w", statePath, err)
	}
	failed = append(failed, wsFailed...)
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
	return locksource.HomeDirs{Home: s.Home, ConfigHome: s.ConfigHome, DataHome: s.DataHome}.Expand(file)
}

func (s Syncer) enabledServers(d locksource.Descriptors) []server {
	var out []server
	for _, dep := range slices.Sorted(maps.Keys(d.Servers)) {
		sd := d.Servers[dep]
		wrapper := filepath.Join(s.Layer, sd.Bin)
		if agentlayer.IsExecutable(wrapper) {
			out = append(out, server{ID: sd.ID, Command: wrapper, Args: sd.Args})
		}
	}
	slices.SortFunc(out, func(a, b server) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// desired lists every enabled server, local ones from the lock and remote
// ones from settings, by id. A remote server whose id a lock server uses is
// left out. With ref, bearer servers reference their token's variable
// (workspace files); otherwise they carry its value (the agents' own files).
func (s Syncer) desired(d locksource.Descriptors, m config.MCP, keys map[string]string, ref bool) []server {
	out := s.enabledServers(d)
	lock := map[string]bool{}
	for _, sd := range d.Servers {
		lock[sd.ID] = true
	}
	for _, id := range slices.Sorted(maps.Keys(m.Servers)) {
		rs := m.Servers[id]
		switch {
		case !rs.Enabled:
		case lock[id]:
			s.logf("MCP server %s clashes with a built-in server; skipping it", id)
		default:
			sv := server{ID: id, URL: rs.URL}
			switch tok := keys[config.MCPTokenName(id)]; {
			case rs.Auth == config.AuthBearer && ref:
				sv.TokenVar = config.MCPTokenName(id)
			case rs.Auth == config.AuthBearer && tok != "":
				sv.Headers = map[string]string{"Authorization": "Bearer " + tok}
			}
			out = append(out, sv)
		}
	}
	slices.SortFunc(out, func(a, b server) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// availableTo keeps the servers that reach agent dep: those without a list,
// and those whose list names dep.
func availableTo(servers []server, avail map[string][]string, dep string) []server {
	var out []server
	for _, sv := range servers {
		if list, ok := avail[sv.ID]; ok && !slices.Contains(list, dep) {
			continue
		}
		out = append(out, sv)
	}
	return out
}

// fileEdit is what one config file should hold of Berth's.
type fileEdit struct {
	servers []server
	owned   []string // server ids Berth wrote there before
	trust   []string // folders to mark trusted (codex-toml only)
	trusted []string // folders Berth marked trusted there before
}

// syncFile converges one config file and returns the ids Berth now owns
// there.
func (s Syncer) syncFile(path string, t locksource.MCPTarget, e fileEdit) ([]string, []string, error) {
	real, err := realPath(path)
	if err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
		return nil, nil, err
	}
	for range maxAttempts {
		data, perm, err := readConfig(real)
		if err != nil {
			return nil, nil, err
		}
		out, ids, trusted, err := s.render(path, t, data, e)
		if err != nil {
			return nil, nil, err
		}
		if bytes.Equal(out, data) && (perm != 0 || len(out) == 0) {
			return ids, trusted, nil
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
			return nil, nil, err
		}
		return ids, trusted, nil
	}
	return nil, nil, fmt.Errorf("%s kept changing while Berth edited it", path)
}

// render computes a file's new content and the ids Berth owns in it.
func (s Syncer) render(path string, t locksource.MCPTarget, data []byte, e fileEdit) ([]byte, []string, []string, error) {
	var out []byte
	var skipped, trusted []string
	if t.Style == locksource.StyleCodexTOML {
		var err error
		out, skipped, trusted, err = editCodex(data, e.servers, e.owned, e.trust, e.trusted)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%s: %w; leaving it unchanged", path, err)
		}
	} else {
		existing, err := objectKeys(data, t.Key)
		if errors.Is(err, errNotPlainJSON) {
			return nil, nil, nil, fmt.Errorf("%s is not plain JSON; leaving it unchanged", path)
		}
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%s: %w", path, err)
		}
		var keep []server
		for _, sv := range e.servers {
			if slices.Contains(existing, sv.ID) && !slices.Contains(e.owned, sv.ID) {
				skipped = append(skipped, sv.ID)
				continue
			}
			keep = append(keep, sv)
		}
		entries, err := jsonEntries(t.Style, keep)
		if err != nil {
			return nil, nil, nil, err
		}
		var del []string
		for _, id := range e.owned {
			if !slices.ContainsFunc(keep, func(sv server) bool { return sv.ID == id }) {
				del = append(del, id)
			}
		}
		if out, err = editJSON(data, t.Key, entries, del); err != nil {
			return nil, nil, nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	for _, id := range skipped {
		s.logf("%s already defines the MCP server %s; not registering Berth's there", path, id)
	}
	return out, idsExcept(e.servers, skipped), trusted, nil
}
