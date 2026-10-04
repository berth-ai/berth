package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/berth-ai/berth/internal/agentlayer"
	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/locksource"
)

// SyncTimeout bounds one berth skills sync.
const SyncTimeout = 10 * time.Minute

// Syncer converges skills for the installed agent set: one exported copy
// per skill, linked into the skills directory of every agent it reaches.
// It changes only links and copies it made itself.
type Syncer struct {
	Current    string // the installed agent set, /opt/agents/current
	Layer      string // the wrapper layer, /var/lib/berth/bin
	Home       string // the agent user's home
	ConfigHome string // $XDG_CONFIG_HOME; empty means Home/.config
	DataHome   string // $XDG_DATA_HOME; empty means Home/.local/share
	Settings   string // skills.json; empty means no skills
	Git        Git
	Log        io.Writer
}

// target is one enabled agent that can receive skills.
type target struct {
	dep, name string
	desc      string   // the descriptor's dir, as also lists it
	dir       string   // desc resolved
	also      []string // descriptor paths
}

// Run converges every skill. A skill that fails keeps its links and does
// not stop the others; the error names every failed skill.
func (s Syncer) Run(ctx context.Context) error {
	targets, err := s.targets()
	if err != nil {
		return err
	}
	skills := map[string]config.Skill{}
	if s.Settings != "" {
		if skills, err = config.ReadSkillsFile(s.Settings); err != nil {
			return err
		}
	}
	statePath := StatePath(s.Home)
	st, err := loadState(statePath)
	if err != nil {
		s.logf("%v; starting over", err)
	}
	keep := map[string]bool{}
	var failed []string
	for _, id := range slices.Sorted(maps.Keys(skills)) {
		sk := skills[id]
		if !sk.Enabled {
			continue
		}
		if err := s.ensureStore(ctx, id, sk, &st); err != nil {
			s.logf("skill %s: %v; leaving it as it was", id, err)
			failed = append(failed, id)
			for _, l := range st.Links {
				if s.pointsTo(l, id) {
					keep[l] = true
				}
			}
			continue
		}
		for _, t := range reach(targets, sk.Agents) {
			link := filepath.Join(t.dir, id)
			linked, err := s.link(link, id, st.Links)
			if err != nil {
				s.logf("skill %s for %s: %v", id, t.name, err)
				failed = append(failed, id)
				continue
			}
			if linked {
				keep[link] = true
			}
		}
	}
	for _, l := range st.Links {
		if !keep[l] && s.owned(l) {
			if err := os.Remove(l); err != nil && !errors.Is(err, fs.ErrNotExist) {
				s.logf("cannot unlink %s: %v", l, err)
				keep[l] = true
				continue
			}
			s.logf("unlinked %s", l)
		}
	}
	st.Links = slices.Sorted(maps.Keys(keep))
	for _, id := range slices.Sorted(maps.Keys(st.Commits)) {
		if _, ok := skills[id]; !ok {
			if err := os.RemoveAll(filepath.Join(StoreDir(s.Home), id)); err != nil {
				s.logf("cannot remove %s: %v", id, err)
				continue
			}
			delete(st.Commits, id)
			s.logf("removed %s", id)
		}
	}
	s.pruneCaches(skills)
	if err := saveState(statePath, st); err != nil {
		return fmt.Errorf("save %s: %w", statePath, err)
	}
	if len(failed) > 0 {
		slices.Sort(failed)
		return fmt.Errorf("skills failed: %s", strings.Join(slices.Compact(failed), ", "))
	}
	return nil
}

func (s Syncer) logf(format string, args ...any) {
	if s.Log != nil {
		fmt.Fprintf(s.Log, "berth skills sync: "+format+"\n", args...)
	}
}

// targets lists the enabled agents with a skills descriptor, by package.
func (s Syncer) targets() ([]target, error) {
	if s.Current == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(filepath.Join(s.Current, "package.json"))
	if err != nil {
		return nil, err
	}
	var p struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("package.json: %w", err)
	}
	meta, err := locksource.ParseMeta(raw, p.Dependencies)
	if err != nil {
		return nil, err
	}
	desc, err := locksource.ParseDescriptors(raw, meta)
	if err != nil {
		return nil, err
	}
	var out []target
	for _, dep := range slices.Sorted(maps.Keys(desc.Agents)) {
		t := desc.Agents[dep].Skills
		if t == nil || !agentlayer.AgentEnabled(s.Current, s.Layer, dep) {
			continue
		}
		out = append(out, target{dep: dep, name: meta[dep].Name, desc: t.Dir, dir: s.resolve(t.Dir), also: t.Also})
	}
	return out, nil
}

// resolve expands a descriptor path; ParseDescriptors guarantees the prefix.
func (s Syncer) resolve(p string) string {
	return locksource.HomeDirs{Home: s.Home, ConfigHome: s.ConfigHome, DataHome: s.DataHome}.Expand(p)
}

// reach lists the agents that get a link of their own: those in list (all
// when nil), minus any that already see the skill through also.
func reach(targets []target, list []string) []target {
	var picked []target
	for _, t := range targets {
		if list == nil || slices.Contains(list, t.dep) {
			picked = append(picked, t)
		}
	}
	var out []target
	for _, t := range picked {
		through := slices.ContainsFunc(picked, func(o target) bool {
			return o.dep != t.dep && slices.Contains(t.also, o.desc) && !slices.Contains(o.also, t.desc)
		})
		if !through {
			out = append(out, t)
		}
	}
	return out
}

// ensureStore makes the store copy of id hold sk.Path at sk.Commit.
func (s Syncer) ensureStore(ctx context.Context, id string, sk config.Skill, st *state) error {
	root := StoreDir(s.Home)
	final := filepath.Join(root, id)
	if fi, err := os.Stat(final); err == nil && fi.IsDir() && st.Commits[id] == sk.Commit {
		return nil
	}
	if !config.ValidCommit(sk.Commit) {
		return fmt.Errorf("%q is not a commit", sk.Commit)
	}
	if !s.Git.AllowFile || !strings.HasPrefix(sk.Repo, "file://") {
		if msg := config.CheckSkillRepo(sk.Repo); msg != "" {
			return fmt.Errorf("repository %s", msg)
		}
	}
	dir := CacheDir(s.Home, sk.Repo)
	if !s.Git.HasCommit(ctx, dir, sk.Commit) {
		got, err := s.Git.Fetch(ctx, dir, sk.Repo, sk.Commit)
		if err != nil {
			return fmt.Errorf("fetch %s at %s: %w", sk.Repo, sk.Commit[:12], err)
		}
		if got != sk.Commit {
			return fmt.Errorf("fetching %s gave %s", sk.Commit[:12], got[:12])
		}
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(root, ".tmp-"+id+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := s.Git.Archive(ctx, dir, sk.Commit, sk.Path, func(r io.Reader) error {
		return extract(r, tmp, func(f string, a ...any) { s.logf(id+": "+f, a...) })
	}); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(tmp, "SKILL.md")); err != nil {
		return fmt.Errorf("no SKILL.md at %s in %s", sk.Path, sk.Commit[:12])
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	if err := swap(tmp, final); err != nil {
		return err
	}
	st.Commits[id] = sk.Commit
	s.logf("installed %s at %s", id, sk.Commit[:12])
	return nil
}

// swap replaces final with tmp, keeping final if the rename fails.
func swap(tmp, final string) error {
	old := final + ".berth-old"
	if err := os.RemoveAll(old); err != nil {
		return err
	}
	hadOld := false
	if _, err := os.Lstat(final); err == nil {
		if err := os.Rename(final, old); err != nil {
			return err
		}
		hadOld = true
	}
	if err := os.Rename(tmp, final); err != nil {
		if hadOld {
			_ = os.Rename(old, final) // best effort: put the previous copy back
		}
		return err
	}
	return os.RemoveAll(old)
}

// link makes path a symlink to the store copy of id. Something at path
// that Berth did not create is left alone; linked is false then.
func (s Syncer) link(path, id string, owned []string) (bool, error) {
	want := filepath.Join(StoreDir(s.Home), id)
	cur, err := os.Readlink(path)
	switch {
	case err == nil && cur == want:
		return true, nil
	case err == nil && slices.Contains(owned, path):
	case errors.Is(err, fs.ErrNotExist):
	default:
		s.logf("skill %s is already defined by the user in %s; skipping it", id, filepath.Dir(path))
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	tmp := path + ".berth-tmp"
	os.Remove(tmp)
	if err := os.Symlink(want, tmp); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return false, err
	}
	s.logf("linked %s into %s", id, filepath.Dir(path))
	return true, nil
}

// owned reports whether path is still a link into the store.
func (s Syncer) owned(path string) bool {
	cur, err := os.Readlink(path)
	return err == nil && strings.HasPrefix(cur, StoreDir(s.Home)+string(filepath.Separator))
}

func (s Syncer) pointsTo(path, id string) bool {
	cur, err := os.Readlink(path)
	return err == nil && cur == filepath.Join(StoreDir(s.Home), id)
}

// pruneCaches deletes cache repositories no configured skill uses. A cache
// locked by a running resolve is left for the next sync.
func (s Syncer) pruneCaches(skills map[string]config.Skill) {
	want := map[string]bool{}
	for _, sk := range skills {
		want[CacheDir(s.Home, sk.Repo)] = true
	}
	entries, err := os.ReadDir(CacheRoot(s.Home))
	if err != nil {
		return
	}
	for _, e := range entries {
		dir := filepath.Join(CacheRoot(s.Home), e.Name())
		if !strings.HasSuffix(e.Name(), ".git") || want[dir] {
			continue
		}
		unlock, err := lock(dir, false)
		if err != nil {
			continue
		}
		os.RemoveAll(dir)
		unlock()
		os.Remove(dir + ".lock")
	}
}
