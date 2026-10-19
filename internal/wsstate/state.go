// Package wsstate records what Berth wrote into each workspace, so each
// converger changes only its own entries, and forgetting a workspace
// removes them all. One file per workspace,
// ~/.local/state/berth/workspaces/<id>.json, holds a section per converger:
// skills, MCP and instructions. A lock beside it serializes berth skills
// sync, berth mcp sync, berth workspaces sync and the forget job, and every
// change re-renders the workspace's exclude block from all three sections.
package wsstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/berth-ai/berth/internal/config"
)

// Schema is the state file's schema.
const Schema = 1

// MaxOriginal caps the copy of a file Berth keeps to restore it.
const MaxOriginal = 256 << 10

// ErrGone means the workspace is no longer in workspaces.json.
var ErrGone = errors.New("the workspace is no longer in workspaces.json")

// State is everything Berth wrote into one workspace.
type State struct {
	Schema       int          `json:"schema"`
	Skills       Skills       `json:"skills"`
	MCP          MCP          `json:"mcp"`
	Instructions Instructions `json:"instructions"`
}

// Instructions is the instructions converger's section: the files Berth
// made links to AGENTS.md, at the workspace's root.
type Instructions struct {
	Path    string            `json:"path"`
	Links   []string          `json:"links"`             // file names, sorted
	Skipped map[string]string `json:"skipped,omitempty"` // file name → why Berth left it alone
	Error   string            `json:"error,omitempty"`
}

// Empty reports whether the section records nothing.
func (in Instructions) Empty() bool {
	return len(in.Links) == 0 && len(in.Skipped) == 0 && in.Error == ""
}

// Skills is the skills converger's section.
type Skills struct {
	Path    string            `json:"path"`              // the folder its entries are in
	Entries map[string]Skill  `json:"entries"`           // by "<source>/<name>"
	Dirs    []string          `json:"dirs"`              // folders Berth created, relative
	Skipped map[string]string `json:"skipped,omitempty"` // relative path → why Berth left it alone
	Error   string            `json:"error,omitempty"`   // why the last sync failed here
}

// Skill is one skill Berth placed in a workspace: links to the store
// (link), or a copy with links to it (vendor).
type Skill struct {
	Name   string            `json:"name"`
	Mode   string            `json:"mode"`
	Commit string            `json:"commit"`           // the source commit it came from
	From   string            `json:"from"`             // the skill's folder in the source
	Links  []string          `json:"links"`            // relative paths of the links Berth made
	Dir    string            `json:"dir,omitempty"`    // vendor: the copy's folder, relative
	Files  map[string]string `json:"files,omitempty"`  // vendor: path below Dir → "sha256:<hex>" or "link:<target>"
	Edited bool              `json:"edited,omitempty"` // the copy has local edits; Berth leaves it alone
	Force  bool              `json:"force,omitempty"`  // Overwrite: the next sync discards local edits
}

// MCP is the MCP converger's section.
type MCP struct {
	Path    string             `json:"path"`
	Files   map[string]MCPFile `json:"files"` // by relative path
	Dirs    []string           `json:"dirs"`
	Trust   bool               `json:"trust,omitempty"` // Codex trusts the folder because of Berth
	Skipped map[string]string  `json:"skipped,omitempty"`
	Error   string             `json:"error,omitempty"`
}

// MCPFile is Berth's part of one project MCP file.
type MCPFile struct {
	Style    string   `json:"style"`
	Key      string   `json:"key,omitempty"`
	IDs      []string `json:"ids"`
	Created  bool     `json:"created,omitempty"`  // Berth created the file; it is in the exclude block
	Tracked  bool     `json:"tracked,omitempty"`  // git tracks the file
	Original string   `json:"original,omitempty"` // the file before Berth first edited it
}

// Empty reports whether the section records nothing.
func (s Skills) Empty() bool {
	return len(s.Entries) == 0 && len(s.Dirs) == 0 && len(s.Skipped) == 0 && s.Error == ""
}

// Empty reports whether the section records nothing. Trust lives in the
// agent's home, which berth mcp sync converges on its own.
func (m MCP) Empty() bool {
	return len(m.Files) == 0 && len(m.Dirs) == 0 && len(m.Skipped) == 0 && m.Error == ""
}

// Exclude lists, sorted, the exclude lines of the sections whose entries
// are in folder: linked skills and MCP files Berth created.
func (st State) Exclude(folder string) []string {
	var out []string
	if st.Skills.Path == folder {
		for _, e := range st.Skills.Entries {
			if e.Mode == config.ModeLink {
				for _, l := range e.Links {
					out = append(out, "/"+l)
				}
			}
		}
	}
	if st.MCP.Path == folder {
		for rel, f := range st.MCP.Files {
			if f.Created {
				out = append(out, "/"+rel)
			}
		}
	}
	if st.Instructions.Path == folder {
		for _, l := range st.Instructions.Links {
			out = append(out, "/"+l)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func (st State) folders() []string {
	var out []string
	for _, p := range []string{st.Skills.Path, st.MCP.Path, st.Instructions.Path} {
		if p != "" && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

func (st *State) fill() {
	st.Schema = Schema
	if st.Skills.Entries == nil {
		st.Skills.Entries = map[string]Skill{}
	}
	if st.Skills.Dirs == nil {
		st.Skills.Dirs = []string{}
	}
	if st.MCP.Files == nil {
		st.MCP.Files = map[string]MCPFile{}
	}
	if st.MCP.Dirs == nil {
		st.MCP.Dirs = []string{}
	}
	if st.Instructions.Links == nil {
		st.Instructions.Links = []string{}
	}
}

// Store is the agent's workspace states.
type Store struct {
	Home      string // the agent user's home
	Published string // workspaces.json
}

// Dir holds the state files and their locks.
func (s Store) Dir() string { return filepath.Join(s.Home, ".local", "state", "berth", "workspaces") }

func (s Store) file(id string) string { return filepath.Join(s.Dir(), id+".json") }

// Workspaces reads workspaces.json: id → folder.
func (s Store) Workspaces() (map[string]string, error) {
	out := map[string]string{}
	if s.Published == "" {
		return out, nil
	}
	pub, err := config.ReadWorkspacesFile(s.Published)
	if err != nil {
		return nil, err
	}
	for id, w := range pub {
		out[id] = w.Path
	}
	return out, nil
}

// IDs lists, sorted, the workspaces with a state file.
func (s Store) IDs() ([]string, error) {
	entries, err := os.ReadDir(s.Dir())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if id, ok := strings.CutSuffix(e.Name(), ".json"); ok && config.WorkspaceIDPattern.MatchString(id) {
			out = append(out, id)
		}
	}
	return out, nil
}

// Load reads id's state without the lock; a missing file is an empty
// state. Writes are atomic, so a reader sees one version or the next.
func (s Store) Load(id string) (State, error) {
	var st State
	if !config.WorkspaceIDPattern.MatchString(id) {
		return st, fmt.Errorf("%q is not a workspace id", id)
	}
	raw, err := os.ReadFile(s.file(id))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return st, err
	}
	if err == nil {
		if err := json.Unmarshal(raw, &st); err != nil {
			return st, fmt.Errorf("%s: %w", s.file(id), err)
		}
		if st.Schema > Schema {
			return st, fmt.Errorf("%s has schema %d; this Berth reads schema %d", s.file(id), st.Schema, Schema)
		}
	}
	st.fill()
	return st, nil
}

// Update runs fn on id's state with the workspace's folder, under the
// lock, then rewrites the exclude block of every folder the state names
// and saves it. ErrGone means workspaces.json no longer lists id.
func (s Store) Update(id string, fn func(ws string, st *State) error) error {
	if !config.WorkspaceIDPattern.MatchString(id) {
		return fmt.Errorf("%q is not a workspace id", id)
	}
	unlock, err := s.lock(id)
	if err != nil {
		return err
	}
	defer unlock()
	pub, err := s.Workspaces()
	if err != nil {
		return err
	}
	ws, ok := pub[id]
	if !ok {
		return ErrGone
	}
	st, err := s.Load(id)
	if err != nil {
		return err
	}
	before := st.folders()
	ferr := fn(ws, &st)
	return errors.Join(ferr, s.finish(id, st, append(before, st.folders()...)))
}

// Release runs fn on id's state under the lock, for a workspace Berth
// forgot, then rewrites the exclude blocks and deletes the state once
// nothing is left in it.
func (s Store) Release(id string, fn func(st *State)) error {
	if !config.WorkspaceIDPattern.MatchString(id) {
		return fmt.Errorf("%q is not a workspace id", id)
	}
	unlock, err := s.lock(id)
	if err != nil {
		return err
	}
	defer unlock()
	st, err := s.Load(id)
	if err != nil {
		return err
	}
	before := st.folders()
	fn(&st)
	st.fill()
	return s.finish(id, st, before)
}

func (s Store) finish(id string, st State, folders []string) error {
	var errs []error
	slices.Sort(folders)
	for _, f := range slices.Compact(folders) {
		if err := WriteExclude(f, st.Exclude(f)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, fmt.Errorf("exclude block in %s: %w", f, err))
		}
	}
	if st.Skills.Empty() && st.MCP.Empty() && st.Instructions.Empty() {
		if err := os.Remove(s.file(id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
		return errors.Join(errs...)
	}
	st.fill()
	raw, err := json.MarshalIndent(st, "", "  ")
	if err == nil {
		err = config.WriteFileAtomic(s.file(id), append(raw, '\n'), 0o600, -1, -1)
	}
	return errors.Join(append(errs, err)...)
}

// lock takes id's lock file, creating the folder. Lock files stay: removing
// one while another process waits on it would split the lock.
func (s Store) lock(id string) (func(), error) {
	if err := os.MkdirAll(s.Dir(), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(s.Dir(), id+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}
