package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"

	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/wsstate"
)

// VendorDir is the workspace folder vendored skills are copied into; the
// other project folders in use link to the copy.
const VendorDir = ".agents/skills"

// errLocalEdits stops a vendored skill whose files the user changed.
var errLocalEdits = errors.New("has local edits")

// wsWant is one attached skill a workspace should have.
type wsWant struct {
	key, name, mode string
	export          string // the store folder links point to and copies come from
	commit, from    string // the export's commit and folder in the source
	hold            bool   // missing or failing: keep what is there, change nothing
}

func (s Syncer) wsWant(a attachment, ws string, hold bool) wsWant {
	return wsWant{key: a.key(), name: a.name, mode: a.att.Workspaces[ws], export: s.exportDir(a.key()),
		commit: a.src.Commit, from: a.src.Found[a.name].Path, hold: hold}
}

// projectDirs lists, sorted, the project folders skills are linked into:
// the enabled agents' own, minus those an agent reads through another
// agent's (reach's also rule, for workspaces).
func projectDirs(targets []target) []string {
	var picked []target
	for _, t := range targets {
		if t.project != "" {
			picked = append(picked, t)
		}
	}
	set := map[string]bool{}
	for _, t := range picked {
		through := slices.ContainsFunc(picked, func(o target) bool {
			return o.project != t.project && slices.Contains(t.projectAlso, o.project) && !slices.Contains(o.projectAlso, t.project)
		})
		if !through {
			set[t.project] = true
		}
	}
	if len(set) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(set))
}

// syncWorkspaces converges every published workspace's attached skills,
// then releases what Berth wrote into workspaces no longer published. It
// returns a label per workspace that failed.
func (s Syncer) syncWorkspaces(targets []target, wants map[string][]wsWant) []string {
	if s.Workspaces == "" {
		return nil
	}
	store := wsstate.Store{Home: s.Home, Published: s.Workspaces}
	pub, err := store.Workspaces()
	if err != nil {
		s.logf("%v", err)
		return []string{"workspaces"}
	}
	dirs := projectDirs(targets)
	var failed []string
	for _, id := range slices.Sorted(maps.Keys(pub)) {
		err := store.Update(id, func(ws string, st *wsstate.State) error {
			return s.convergeWorkspace(id, ws, &st.Skills, wants[id], dirs)
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
		if err := store.Release(id, func(st *wsstate.State) { s.Release(id, &st.Skills) }); err != nil {
			s.logf("releasing workspace %s: %v", id, err)
		}
	}
	return failed
}

// convergeWorkspace makes ws hold exactly the wanted skills, as sec
// records them. Entries no longer wanted are removed; held ones stay.
func (s Syncer) convergeWorkspace(id, ws string, sec *wsstate.Skills, wants []wsWant, dirs []string) error {
	if sec.Path != "" && sec.Path != ws {
		s.Release(id, sec) // the id now names another folder
	}
	if sec.Entries == nil {
		sec.Entries = map[string]wsstate.Skill{}
	}
	sec.Path, sec.Error, sec.Skipped = ws, "", map[string]string{}
	if fi, err := os.Stat(ws); err != nil || !fi.IsDir() {
		sec.Error = fmt.Sprintf("the folder %s is missing", ws)
		return errors.New(sec.Error)
	}
	wanted := map[string]wsWant{}
	for _, w := range wants {
		wanted[w.key] = w
	}
	var errs []error
	for _, key := range slices.Sorted(maps.Keys(sec.Entries)) {
		e := sec.Entries[key]
		if w, ok := wanted[key]; ok && (w.hold || w.mode == e.Mode) {
			continue
		}
		if err := s.removeEntry(ws, key, &e); err != nil {
			sec.Entries[key] = e
			errs = append(errs, entryError(e.Name, id, err))
			continue
		}
		delete(sec.Entries, key)
	}
	for _, w := range wants {
		e, had := sec.Entries[w.key]
		if w.hold || had && e.Mode != w.mode {
			continue // held, or a mode switch stopped by local edits
		}
		if w.mode != config.ModeLink {
			continue // vendored skills: Task 4
		}
		err := s.linkEntry(ws, w, dirs, &e, sec)
		if len(e.Links) > 0 {
			sec.Entries[w.key] = e
		} else {
			delete(sec.Entries, w.key)
		}
		if err != nil {
			errs = append(errs, entryError(w.name, id, err))
		}
	}
	sec.Dirs = wsstate.RemoveEmpty(ws, sec.Dirs)
	if err := errors.Join(errs...); err != nil {
		sec.Error = err.Error()
		return err
	}
	return nil
}

// entryError names the skill and workspace: "docx in api has local edits".
func entryError(name, id string, err error) error {
	if errors.Is(err, errLocalEdits) {
		return fmt.Errorf("%s in %s %w", name, id, err)
	}
	return fmt.Errorf("%s in %s: %w", name, id, err)
}

// linkEntry links w into every project folder in dirs.
func (s Syncer) linkEntry(ws string, w wsWant, dirs []string, e *wsstate.Skill, sec *wsstate.Skills) error {
	*e = wsstate.Skill{Name: w.name, Mode: config.ModeLink, Commit: w.commit, From: w.from, Links: e.Links}
	return s.placeLinks(ws, w.name, dirs, func(string) string { return w.export }, e, sec)
}

// placeLinks makes <dir>/<name> a link to target(rel) for each dir in dirs,
// and removes e's links into folders no longer listed.
func (s Syncer) placeLinks(ws, name string, dirs []string, target func(rel string) string, e *wsstate.Skill, sec *wsstate.Skills) error {
	want := map[string]bool{}
	for _, d := range dirs {
		want[path.Join(d, name)] = true
	}
	var links []string
	for _, l := range e.Links {
		if !want[l] {
			s.unlinkOwned(ws, l, target(l))
		}
	}
	var errs []error
	for _, rel := range slices.Sorted(maps.Keys(want)) {
		ok, err := s.placeLink(ws, rel, target(rel), slices.Contains(e.Links, rel), sec)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if ok {
			links = append(links, rel)
		}
	}
	e.Links = links
	return errors.Join(errs...)
}

// placeLink makes ws/rel a symlink to target. Something there that Berth
// did not make (owned false), or that is no longer a link, is left alone
// and noted in sec.Skipped; placed is false then.
func (s Syncer) placeLink(ws, rel, target string, owned bool, sec *wsstate.Skills) (bool, error) {
	if err := wsstate.Plain(ws, path.Dir(rel)); err != nil {
		sec.Skipped[rel] = err.Error()
		s.logf("%s: %v; leaving it alone", rel, err)
		return false, nil
	}
	full := filepath.Join(ws, filepath.FromSlash(rel))
	cur, err := os.Readlink(full)
	switch {
	case err == nil && cur == target:
		return true, nil
	case err == nil && owned:
	case errors.Is(err, fs.ErrNotExist):
	default:
		sec.Skipped[rel] = "already exists; Berth left it alone"
		s.logf("%s already exists in %s; leaving it alone", rel, ws)
		return false, nil
	}
	made, err := wsstate.MkdirAll(ws, path.Dir(rel))
	sec.Dirs = wsstate.AddDirs(sec.Dirs, made...)
	if err != nil {
		return false, err
	}
	tmp := full + ".berth-tmp"
	os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, full); err != nil {
		os.Remove(tmp)
		return false, err
	}
	s.logf("linked %s into %s", path.Base(rel), filepath.Join(ws, filepath.FromSlash(path.Dir(rel))))
	return true, nil
}

// unlinkOwned removes ws/rel if it is still the link Berth made to target.
func (s Syncer) unlinkOwned(ws, rel, target string) {
	full := filepath.Join(ws, filepath.FromSlash(rel))
	cur, err := os.Readlink(full)
	switch {
	case err == nil && cur == target:
		if err := os.Remove(full); err != nil {
			s.logf("cannot unlink %s: %v", full, err)
			return
		}
		s.logf("unlinked %s", full)
	case !errors.Is(err, fs.ErrNotExist):
		s.logf("%s changed; Berth no longer manages it", full)
	}
}

// linkTarget is what e's link at rel points to.
func (s Syncer) linkTarget(key string, e wsstate.Skill, rel string) string {
	if e.Mode == config.ModeVendor {
		r, _ := filepath.Rel(filepath.FromSlash(path.Dir(rel)), filepath.FromSlash(e.Dir))
		return r
	}
	return s.exportDir(key)
}

// removeEntry removes everything e placed in ws.
func (s Syncer) removeEntry(ws, key string, e *wsstate.Skill) error {
	for _, l := range e.Links {
		s.unlinkOwned(ws, l, s.linkTarget(key, *e, l))
	}
	e.Links = nil
	return nil
}

// Release removes everything sec records, for a workspace Berth forgot or
// whose id now names another folder, empties sec and returns what it had
// to leave in place.
func (s Syncer) Release(id string, sec *wsstate.Skills) []string {
	var kept []string
	if ws := sec.Path; ws != "" {
		for _, key := range slices.Sorted(maps.Keys(sec.Entries)) {
			e := sec.Entries[key]
			e.Force = false
			if err := s.removeEntry(ws, key, &e); err != nil {
				msg := entryError(e.Name, id, err).Error()
				s.logf("%s; leaving it in place", msg)
				kept = append(kept, msg)
			}
		}
		wsstate.RemoveEmpty(ws, sec.Dirs)
	}
	*sec = wsstate.Skills{}
	return kept
}
