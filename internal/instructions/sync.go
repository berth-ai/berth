// Package instructions manages the instruction files agents read at a
// workspace's root (CLAUDE.md, AGENTS.md, GEMINI.md): the panel's editor
// reads and writes them as agent, and the converger makes each one a link
// to AGENTS.md while the workspace shares instructions. Berth records the
// links it made in the workspace's state and removes only those.
package instructions

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
	"github.com/berth-ai/berth/internal/wsstate"
)

// Shared is the file every other instruction file links to.
const Shared = "AGENTS.md"

// SyncTimeout bounds one berth workspaces sync.
const SyncTimeout = 2 * time.Minute

// Syncer converges every published workspace's shared links.
type Syncer struct {
	Set        agentset.Set
	Layer      string // the wrapper layer; an agent with a wrapper there is enabled
	Home       string // the agent user's home, where the state lives
	Workspaces string // workspaces.json
	Log        io.Writer
}

// Files lists, sorted, the instruction files of the enabled agents.
func (s Syncer) Files() []string {
	var deps []string
	for _, dep := range slices.Sorted(maps.Keys(s.Set.Descriptors.Agents)) {
		if s.Set.Enabled(s.Layer, dep) {
			deps = append(deps, dep)
		}
	}
	return s.Set.Descriptors.InstructionFiles(deps)
}

// Want lists the files to link to AGENTS.md: every file but AGENTS.md
// while shared is on, none otherwise.
func Want(files []string, shared bool) []string {
	if !shared {
		return nil
	}
	return slices.DeleteFunc(slices.Clone(files), func(f string) bool { return f == Shared })
}

// Run converges every published workspace, then releases what Berth wrote
// into workspaces no longer published. One workspace failing does not stop
// the others; the error names each.
func (s Syncer) Run(_ context.Context) error {
	store := wsstate.Store{Home: s.Home, Published: s.Workspaces}
	pub, err := s.published()
	if err != nil {
		return err
	}
	files := s.Files()
	var failed []string
	for _, id := range slices.Sorted(maps.Keys(pub)) {
		want := Want(files, pub[id].Shared)
		err := store.Update(id, func(ws string, st *wsstate.State) error {
			return s.converge(ws, &st.Instructions, want)
		})
		if err != nil && !errors.Is(err, wsstate.ErrGone) {
			s.logf("workspace %s: %v", id, err)
			failed = append(failed, fmt.Sprintf("workspace %s (%v)", id, err))
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
		if err := store.Release(id, func(st *wsstate.State) { s.Release(&st.Instructions) }); err != nil {
			s.logf("releasing workspace %s: %v", id, err)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("instructions failed in %s", strings.Join(failed, ", "))
	}
	return nil
}

// converge makes ws's links exactly want, as sec records them.
func (s Syncer) converge(ws string, sec *wsstate.Instructions, want []string) error {
	if sec.Path != "" && sec.Path != ws {
		s.Release(sec) // the id now names another folder
	}
	sec.Path, sec.Error, sec.Skipped = ws, "", map[string]string{}
	if fi, err := os.Stat(ws); err != nil || !fi.IsDir() {
		if len(want) == 0 && len(sec.Links) == 0 {
			sec.Path = ""
			return nil
		}
		sec.Error = fmt.Sprintf("the folder %s is missing", ws)
		return errors.New(sec.Error)
	}
	var kept []string
	for _, l := range sec.Links {
		if slices.Contains(want, l) && ownedLink(ws, l) {
			kept = append(kept, l)
			continue
		}
		s.unlink(ws, l)
	}
	sec.Links = kept
	if len(want) > 0 {
		if why := sharedProblem(ws); why != "" {
			// Nothing can link to AGENTS.md: drop Berth's links and say why.
			sec.Links = s.dropAll(ws, sec.Links)
			if strings.HasPrefix(why, Shared+" is a symlink") {
				sec.Skipped[Shared] = why
			} else {
				for _, f := range want {
					sec.Skipped[f] = why
				}
			}
			return nil
		}
	}
	for _, f := range want {
		if slices.Contains(sec.Links, f) {
			continue
		}
		p := filepath.Join(ws, f)
		fi, err := os.Lstat(p)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			if err := os.Symlink(Shared, p); err != nil {
				sec.Skipped[f] = err.Error()
				continue
			}
			sec.Links = append(sec.Links, f)
		case err != nil:
			sec.Skipped[f] = err.Error()
		case fi.Mode()&fs.ModeSymlink != 0 && readlink(p) == Shared:
			// The user's own link: it already does what shared asks.
		case fi.Mode().IsRegular():
			sec.Skipped[f] = f + " has its own content"
		default:
			sec.Skipped[f] = f + " is not a file Berth can replace"
		}
	}
	slices.Sort(sec.Links)
	if len(sec.Links) == 0 && len(sec.Skipped) == 0 {
		sec.Path = ""
	}
	return nil
}

// sharedProblem says why nothing can link to ws's AGENTS.md, or "".
func sharedProblem(ws string) string {
	fi, err := os.Lstat(filepath.Join(ws, Shared))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Shared + " is missing; write it or merge a file into it"
	case err != nil:
		return err.Error()
	case fi.Mode()&fs.ModeSymlink != 0:
		return Shared + " is a symlink; Berth links nothing to it"
	case !fi.Mode().IsRegular():
		return Shared + " is not a file"
	}
	return ""
}

// dropAll removes every owned link in links and returns those it could not.
func (s Syncer) dropAll(ws string, links []string) []string {
	var left []string
	for _, l := range links {
		if !s.unlink(ws, l) {
			left = append(left, l)
		}
	}
	return left
}

// unlink removes l when it is still Berth's link to AGENTS.md; a file the
// user put there instead stays. It reports whether l is gone or no longer
// Berth's.
func (s Syncer) unlink(ws, l string) bool {
	p := filepath.Join(ws, l)
	if !ownedLink(ws, l) {
		return true
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		s.logf("removing %s: %v", p, err)
		return false
	}
	return true
}

func ownedLink(ws, l string) bool {
	p := filepath.Join(ws, l)
	fi, err := os.Lstat(p)
	return err == nil && fi.Mode()&fs.ModeSymlink != 0 && readlink(p) == Shared
}

func readlink(p string) string { l, _ := os.Readlink(p); return l }

// Release removes the links sec records and empties it; it returns the
// links it had to leave.
func (s Syncer) Release(sec *wsstate.Instructions) []string {
	var kept []string
	if sec.Path != "" {
		for _, l := range s.dropAll(sec.Path, sec.Links) {
			kept = append(kept, fmt.Sprintf("%s in %s", l, sec.Path))
		}
	}
	*sec = wsstate.Instructions{}
	return kept
}

// published reads workspaces.json; no file means no workspaces.
func (s Syncer) published() (map[string]config.PublishedWorkspace, error) {
	if s.Workspaces == "" {
		return map[string]config.PublishedWorkspace{}, nil
	}
	return config.ReadWorkspacesFile(s.Workspaces)
}

func (s Syncer) logf(format string, args ...any) {
	if s.Log != nil {
		fmt.Fprintf(s.Log, "berth workspaces sync: "+format+"\n", args...)
	}
}
