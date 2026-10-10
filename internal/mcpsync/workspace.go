package mcpsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/locksource"
	"github.com/berth-ai/berth/internal/wsstate"
)

// projectTarget is one enabled agent's MCP file in a workspace.
type projectTarget struct {
	dep, style string
	file       string // relative to the workspace
	key        string
	trust      bool
}

// projectTargets lists the enabled agents' project files by package; two
// agents naming one file share the first one's entry.
func (s Syncer) projectTargets(d locksource.Descriptors) []projectTarget {
	var out []projectTarget
	seen := map[string]bool{}
	for _, dep := range slices.Sorted(maps.Keys(d.Agents)) {
		t := d.Agents[dep].MCP
		if t == nil || t.Project == nil || seen[t.Project.File] || !s.Set.Enabled(s.Layer, dep) {
			continue
		}
		seen[t.Project.File] = true
		out = append(out, projectTarget{dep: dep, style: t.Style, file: t.Project.File, key: t.Project.Key, trust: t.Project.Trust})
	}
	return out
}

// attachedTo keeps the servers whose mcp.workspaces list names id.
func attachedTo(servers []server, lists map[string][]string, id string) []server {
	var out []server
	for _, sv := range servers {
		if slices.Contains(lists[sv.ID], id) {
			out = append(out, sv)
		}
	}
	return out
}

// syncWorkspaces writes every published workspace's project files and
// releases forgotten workspaces. It returns, per agent package, the
// folders whose project file Berth wrote into and the agent must trust,
// and a label per workspace that failed.
func (s Syncer) syncWorkspaces(d locksource.Descriptors, m config.MCP) (map[string][]string, []string) {
	trust := map[string][]string{}
	if s.Workspaces == "" {
		return trust, nil
	}
	store := wsstate.Store{Home: s.Home, Published: s.Workspaces}
	pub, err := store.Workspaces()
	if err != nil {
		s.logf("%v", err)
		return trust, []string{"workspaces"}
	}
	all := s.desired(d, m, nil, true)
	targets := s.projectTargets(d)
	var failed []string
	for _, id := range slices.Sorted(maps.Keys(pub)) {
		servers := attachedTo(all, m.Workspaces, id)
		err := store.Update(id, func(ws string, st *wsstate.State) error {
			err := s.convergeWorkspace(ws, &st.MCP, servers, targets)
			st.MCP.Trust = false
			for _, t := range targets {
				if t.trust && len(st.MCP.Files[t.file].IDs) > 0 {
					trust[t.dep] = append(trust[t.dep], ws)
					st.MCP.Trust = true
				}
			}
			return err
		})
		if err != nil && !errors.Is(err, wsstate.ErrGone) {
			s.logf("workspace %s: %v", id, err)
			failed = append(failed, "workspace "+id)
		}
	}
	ids, err := store.IDs()
	if err != nil {
		s.logf("%v", err)
	}
	for _, id := range ids {
		if _, ok := pub[id]; ok {
			continue
		}
		if err := store.Release(id, func(st *wsstate.State) { s.Release(&st.MCP) }); err != nil {
			s.logf("releasing workspace %s: %v", id, err)
		}
	}
	return trust, failed
}

// convergeWorkspace makes each project file in ws hold exactly servers as
// Berth's entries, and drops Berth's entries from files no enabled agent
// reads any more.
func (s Syncer) convergeWorkspace(ws string, sec *wsstate.MCP, servers []server, targets []projectTarget) error {
	if sec.Path != "" && sec.Path != ws {
		s.Release(sec) // the id now names another folder
	}
	if sec.Files == nil {
		sec.Files = map[string]wsstate.MCPFile{}
	}
	sec.Path, sec.Error, sec.Skipped = ws, "", map[string]string{}
	if fi, err := os.Stat(ws); err != nil || !fi.IsDir() {
		if len(servers) == 0 && len(sec.Files) == 0 {
			return nil // nothing to write or remove
		}
		sec.Error = fmt.Sprintf("the folder %s is missing", ws)
		return errors.New(sec.Error)
	}
	var errs []error
	want := map[string]bool{}
	for _, t := range targets {
		want[t.file] = true
		if err := s.syncProjectFile(ws, t, servers, sec); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", t.file, err))
		}
	}
	for _, rel := range slices.Sorted(maps.Keys(sec.Files)) {
		if want[rel] {
			continue
		}
		f := sec.Files[rel]
		if err := s.syncProjectFile(ws, projectTarget{style: f.Style, file: rel, key: f.Key}, nil, sec); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", rel, err))
		}
	}
	sec.Dirs = wsstate.RemoveEmpty(ws, sec.Dirs)
	if err := errors.Join(errs...); err != nil {
		sec.Error = err.Error()
		return err
	}
	return nil
}

// syncProjectFile makes Berth's entries in ws/t.file be servers, recording
// what it owns in sec. A file reached through a symlink is left alone.
func (s Syncer) syncProjectFile(ws string, t projectTarget, servers []server, sec *wsstate.MCP) error {
	f, had := sec.Files[t.file]
	if len(servers) == 0 && !had {
		return nil
	}
	if err := wsstate.Plain(ws, t.file); err != nil {
		sec.Skipped[t.file] = err.Error()
		s.logf("%s: %v; leaving it alone", t.file, err)
		return nil
	}
	full := filepath.Join(ws, filepath.FromSlash(t.file))
	before, perm, err := readConfig(full)
	if err != nil {
		return err
	}
	existed := perm != 0
	if dir := path.Dir(t.file); len(servers) > 0 && dir != "." {
		made, err := wsstate.MkdirAll(ws, dir)
		sec.Dirs = wsstate.AddDirs(sec.Dirs, made...)
		if err != nil {
			return err
		}
	}
	if existed && !had && len(before) <= wsstate.MaxOriginal {
		f.Original = string(before)
	}
	ids, _, err := s.syncFile(full, locksource.MCPTarget{File: t.file, Style: t.style, Key: t.key}, fileEdit{servers: servers, owned: f.IDs})
	if err != nil {
		return err
	}
	f.Style, f.Key, f.IDs = t.style, t.key, ids
	if !existed && len(ids) > 0 {
		f.Created = true
	}
	if len(ids) == 0 {
		delete(sec.Files, t.file)
		return s.settle(full, t, f)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if f.Tracked, err = wsstate.Tracked(ctx, s.Git, ws, t.file); err != nil {
		s.logf("cannot tell whether git tracks %s: %v", full, err)
	}
	sec.Files[t.file] = f
	return nil
}

// settle tidies a file Berth no longer has entries in: one it created and
// that now holds nothing goes, and one whose rest is unchanged gets its
// original bytes back.
func (s Syncer) settle(full string, t projectTarget, f wsstate.MCPFile) error {
	data, perm, err := readConfig(full)
	if err != nil || perm == 0 {
		return err
	}
	switch {
	case f.Created && holdsNothing(data, t):
		s.logf("removed %s", full)
		return os.Remove(full)
	case f.Original != "" && sameDoc(data, []byte(f.Original), t):
		return writeAtomic(full, []byte(f.Original), perm, nil)
	}
	return nil
}

// holdsNothing reports whether data is empty: no TOML left, or a JSON
// object that is empty or holds only an empty t.key.
func holdsNothing(data []byte, t projectTarget) bool {
	if t.style == locksource.StyleCodexTOML {
		return len(bytes.TrimSpace(data)) == 0
	}
	members, err := parseObject(data)
	if err != nil {
		return false
	}
	members = dropEmpty(members, t.key)
	return len(members) == 0
}

// sameDoc reports whether a and b say the same: equal TOML text, or equal
// JSON once an empty t.key is dropped from each.
func sameDoc(a, b []byte, t projectTarget) bool {
	if t.style == locksource.StyleCodexTOML {
		return strings.TrimSpace(string(a)) == strings.TrimSpace(string(b))
	}
	ca, okA := compactWithout(a, t.key)
	cb, okB := compactWithout(b, t.key)
	return okA && okB && bytes.Equal(ca, cb)
}

func dropEmpty(members []member, key string) []member {
	return slices.DeleteFunc(slices.Clone(members), func(m member) bool {
		inner, err := parseObject(m.val)
		return m.key == key && err == nil && len(inner) == 0
	})
}

// compactWithout renders data's members, minus an empty key, compactly.
func compactWithout(data []byte, key string) ([]byte, bool) {
	members, err := parseObject(data)
	if err != nil {
		return nil, false
	}
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range dropEmpty(members, key) {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := marshal(m.key)
		b.Write(k)
		b.WriteByte(':')
		if err := json.Compact(&b, m.val); err != nil {
			return nil, false
		}
	}
	b.WriteByte('}')
	return b.Bytes(), true
}

// Release removes Berth's entries from every file sec records, for a
// workspace Berth forgot or whose id now names another folder, empties sec
// and returns what it could not remove.
func (s Syncer) Release(sec *wsstate.MCP) []string {
	var kept []string
	if ws := sec.Path; ws != "" {
		sec.Skipped = map[string]string{}
		for _, rel := range slices.Sorted(maps.Keys(sec.Files)) {
			f := sec.Files[rel]
			if err := s.syncProjectFile(ws, projectTarget{style: f.Style, file: rel, key: f.Key}, nil, sec); err != nil {
				msg := fmt.Sprintf("%s: %v", filepath.Join(ws, rel), err)
				s.logf("%s; leaving it in place", msg)
				kept = append(kept, msg)
			}
		}
		wsstate.RemoveEmpty(ws, sec.Dirs)
	}
	*sec = wsstate.MCP{}
	return kept
}
