package skills

import (
	"context"
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

	"github.com/berth-ai/berth/internal/agentset"
	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/locksource"
)

// SyncTimeout bounds one berth skills sync.
const SyncTimeout = 10 * time.Minute

// Syncer converges skills for the installed agent set: one export per
// attached skill, at its source's commit, linked into the skills directory
// of every agent it reaches. It changes only links and exports it made
// itself.
type Syncer struct {
	Set        agentset.Set // the merged agent set
	Layer      string       // the wrapper layer, /var/lib/berth/bin
	Home       string       // the agent user's home
	ConfigHome string       // $XDG_CONFIG_HOME; empty means Home/.config
	DataHome   string       // $XDG_DATA_HOME; empty means Home/.local/share
	Settings   string       // skills.json; empty means no sources
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

// attachment is one attached skill of one source.
type attachment struct {
	source, name string
	src          config.Source
	att          config.Attachment
}

// key names the attachment's export, <source>/<name> under the store.
func (a attachment) key() string { return a.source + "/" + a.name }

func (a attachment) label() string { return a.name + " from " + a.source }

// attachments lists every attachment, by source then name.
func attachments(sources map[string]config.Source) []attachment {
	var out []attachment
	for _, id := range slices.Sorted(maps.Keys(sources)) {
		src := sources[id]
		for _, name := range slices.Sorted(maps.Keys(src.Attached)) {
			out = append(out, attachment{source: id, name: name, src: src, att: src.Attached[name]})
		}
	}
	return out
}

// Run converges every attachment. One that fails keeps its links and does
// not stop the others; the error names every failed one. A missing
// attachment (its source no longer holds a valid skill of that name) keeps
// its links and export and is only logged.
func (s Syncer) Run(ctx context.Context) error {
	targets, err := s.targets()
	if err != nil {
		return err
	}
	sources := map[string]config.Source{}
	if s.Settings != "" {
		if sources, err = config.ReadSkillsFile(s.Settings); err != nil {
			return err
		}
	}
	statePath := StatePath(s.Home)
	st, err := loadState(statePath)
	if err != nil {
		s.logf("%v; starting over", err)
	}
	// A C2 copy named like a source sits where that source's folder goes.
	s.clearLegacy(&st, func(id string) bool { _, ok := sources[id]; return ok })
	keep, used := map[string]bool{}, map[string]bool{}
	var failed []string
	for _, a := range attachments(sources) {
		used[a.key()] = true
		if !a.att.Enabled {
			continue
		}
		if f, ok := a.src.Found[a.name]; !ok || !f.Valid() {
			s.logf("%s is not in %s at %s; keeping its links", a.name, a.source, short(a.src.Commit))
			s.keepLinks(st, a.key(), keep)
			continue
		}
		if err := s.ensureExport(ctx, a, &st); err != nil {
			s.logf("%s: %v; leaving it as it was", a.label(), err)
			failed = append(failed, a.label())
			s.keepLinks(st, a.key(), keep)
			continue
		}
		for _, t := range reach(targets, a.att.Agents) {
			link := filepath.Join(t.dir, a.name)
			linked, err := s.link(link, a.key(), st.Links)
			if err != nil {
				s.logf("%s for %s: %v", a.label(), t.name, err)
				failed = append(failed, a.label())
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
	s.pruneExports(&st, used)
	s.clearLegacy(&st, func(string) bool { return true })
	s.pruneCaches(sources)
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

func short(commit string) string { return commit[:min(12, len(commit))] }

// targets lists the enabled agents with a skills descriptor, by package.
func (s Syncer) targets() ([]target, error) {
	var out []target
	for _, dep := range slices.Sorted(maps.Keys(s.Set.Descriptors.Agents)) {
		t := s.Set.Descriptors.Agents[dep].Skills
		if t == nil || !s.Set.Enabled(s.Layer, dep) {
			continue
		}
		out = append(out, target{dep: dep, name: s.Set.Packages[dep].Meta.Name, desc: t.Dir, dir: s.resolve(t.Dir), also: t.Also})
	}
	return out, nil
}

// resolve expands a descriptor path; ParseDescriptors guarantees the prefix.
func (s Syncer) resolve(p string) string {
	return locksource.HomeDirs{Home: s.Home, ConfigHome: s.ConfigHome, DataHome: s.DataHome}.Expand(p)
}

// reach lists the agents that get a link of their own: those in list (all
// when nil, none when empty), minus any that already see the skill
// through also.
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

// exportDir is where the store keeps the export key names.
func (s Syncer) exportDir(key string) string {
	return filepath.Join(StoreDir(s.Home), filepath.FromSlash(key))
}

// ensureExport makes a's export hold its folder at its source's commit.
func (s Syncer) ensureExport(ctx context.Context, a attachment, st *state) error {
	f, src := a.src.Found[a.name], a.src
	final := s.exportDir(a.key())
	want := export{Commit: src.Commit, Path: f.Path}
	if fi, err := os.Stat(final); err == nil && fi.IsDir() && st.Exports[a.key()] == want {
		return nil
	}
	switch {
	case !config.WorkspaceIDPattern.MatchString(a.source) || !config.SkillIDPattern.MatchString(a.name):
		return errors.New("the published source or skill name is malformed")
	case !config.ValidCommit(src.Commit):
		return fmt.Errorf("%q is not a commit", src.Commit)
	case !config.ValidSkillPath(f.Path):
		return fmt.Errorf("%q is not a folder", f.Path)
	}
	if !s.Git.AllowFile || !strings.HasPrefix(src.Repo, "file://") {
		if msg := config.CheckSkillRepo(src.Repo); msg != "" {
			return fmt.Errorf("repository %s", msg)
		}
	}
	cache := CacheDir(s.Home, src.Repo)
	if !s.Git.HasCommit(ctx, cache, src.Commit) {
		got, err := s.Git.Fetch(ctx, cache, src.Repo, src.Commit)
		if err != nil {
			return fmt.Errorf("fetch %s at %s: %w", src.Repo, short(src.Commit), err)
		}
		if got != src.Commit {
			return fmt.Errorf("fetching %s gave %s", short(src.Commit), short(got))
		}
	}
	dir := filepath.Dir(final)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(dir, ".tmp-"+a.name+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := s.Git.Archive(ctx, cache, src.Commit, f.Path, func(r io.Reader) error {
		return extract(r, tmp, func(format string, args ...any) { s.logf(a.label()+": "+format, args...) })
	}); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(tmp, "SKILL.md")); err != nil {
		return fmt.Errorf("no SKILL.md at %s in %s", f.Path, short(src.Commit))
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	if err := swap(tmp, final); err != nil {
		return err
	}
	st.Exports[a.key()] = want
	s.logf("installed %s at %s", a.label(), short(src.Commit))
	return nil
}

// keepLinks keeps the links that point to export key.
func (s Syncer) keepLinks(st state, key string, keep map[string]bool) {
	for _, l := range st.Links {
		if s.pointsTo(l, key) {
			keep[l] = true
		}
	}
}

// pruneExports deletes the exports no attachment names, then the source
// folders that leaves empty.
func (s Syncer) pruneExports(st *state, used map[string]bool) {
	for _, key := range slices.Sorted(maps.Keys(st.Exports)) {
		if used[key] {
			continue
		}
		dir := s.exportDir(key)
		if err := os.RemoveAll(dir); err != nil {
			s.logf("cannot remove %s: %v", key, err)
			continue
		}
		delete(st.Exports, key)
		os.Remove(filepath.Dir(dir)) // only succeeds once the source folder is empty
		s.logf("removed %s", key)
	}
}

// clearLegacy deletes the C2 copies (StoreDir/<id>) that drop picks.
func (s Syncer) clearLegacy(st *state, drop func(id string) bool) {
	for _, id := range slices.Sorted(maps.Keys(st.Legacy)) {
		if !drop(id) {
			continue
		}
		if config.SkillIDPattern.MatchString(id) {
			if err := os.RemoveAll(filepath.Join(StoreDir(s.Home), id)); err != nil {
				s.logf("cannot remove the old copy of %s: %v", id, err)
				continue
			}
			s.logf("removed the old copy of %s", id)
		}
		delete(st.Legacy, id)
	}
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

// link makes path a symlink to export key. Something at path that Berth
// did not create is left alone; linked is false then.
func (s Syncer) link(path, key string, owned []string) (bool, error) {
	want := s.exportDir(key)
	name := filepath.Base(path)
	cur, err := os.Readlink(path)
	switch {
	case err == nil && cur == want:
		return true, nil
	case err == nil && slices.Contains(owned, path):
	case errors.Is(err, fs.ErrNotExist):
	default:
		s.logf("skill %s is already defined by the user in %s; skipping it", name, filepath.Dir(path))
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
	s.logf("linked %s into %s", name, filepath.Dir(path))
	return true, nil
}

// owned reports whether path is still a link into the store.
func (s Syncer) owned(path string) bool {
	cur, err := os.Readlink(path)
	return err == nil && strings.HasPrefix(cur, StoreDir(s.Home)+string(filepath.Separator))
}

func (s Syncer) pointsTo(path, key string) bool {
	cur, err := os.Readlink(path)
	return err == nil && cur == s.exportDir(key)
}

// pruneCaches deletes cache repositories no source uses. A cache locked by
// a running resolve is left for the next sync.
func (s Syncer) pruneCaches(sources map[string]config.Source) {
	want := map[string]bool{}
	for _, src := range sources {
		want[CacheDir(s.Home, src.Repo)] = true
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
