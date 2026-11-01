// Package orcasync keeps Orca's local projects in step with Berth's
// workspaces: it adds each workspace folder to Orca and removes the ones
// it added once their workspace is gone. It drives Orca through its own
// CLI, orca-ide, as agent, and records what it added in a state file, so
// a project the user added in Orca is never removed.
package orcasync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/berth-ai/berth/internal/config"
)

// Timeout bounds one run: the wait for Orca plus a call per workspace.
const Timeout = 5 * time.Minute

// ErrUnavailable means Orca did not answer within the wait.
var ErrUnavailable = errors.New("no answer from Orca")

// Syncer converges once.
type Syncer struct {
	// Orca runs orca-ide with args and returns its stdout; an error carries
	// what Orca said.
	Orca       func(ctx context.Context, args ...string) ([]byte, error)
	Workspaces string        // workspaces.json
	State      string        // the record of what Berth added
	Wait       time.Duration // how long to wait for Orca to answer
	Poll       time.Duration // the pause between tries
}

// Result is one run: workspace folders added and removed, and those left
// in Orca or out of it, with why.
type Result struct {
	Added   []string          `json:"added"`
	Removed []string          `json:"removed"`
	Kept    map[string]string `json:"kept"`   // Orca still uses it; the next run tries again
	Failed  map[string]string `json:"failed"` // Orca refused the change
}

// state is the record of folders Berth added to Orca, and how the last run
// went.
type state struct {
	Schema   int               `json:"schema"`
	Added    []string          `json:"added"`            // absolute folders, sorted
	Failed   map[string]string `json:"failed,omitempty"` // folder → why the last run could not add it
	SyncedAt *time.Time        `json:"synced_at,omitempty"`
}

// Project is one workspace as Orca sees it.
type Project struct {
	Path   string `json:"path"`
	InOrca bool   `json:"in_orca"`
	Error  string `json:"error,omitempty"` // why the last run could not add it
}

// Status is each workspace, by id, as Orca sees it, and when the last run
// ended; SyncedAt is nil before the first.
type Status struct {
	Workspaces map[string]Project `json:"workspaces"`
	SyncedAt   *time.Time         `json:"synced_at"`
}

// setup is the part of an Orca project host setup the sync reads.
type setup struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	HostID string `json:"hostId"`
}

// Run adds every workspace folder that exists and is not in Orca yet, and
// removes the folders it added whose workspace is gone. Orca refuses to
// remove a project with open terminals or worktree details; such a folder
// stays recorded and the next run tries again.
func (s Syncer) Run(ctx context.Context) (Result, error) {
	res := Result{Added: []string{}, Removed: []string{}, Kept: map[string]string{}, Failed: map[string]string{}}
	pub, err := config.ReadWorkspacesFile(s.Workspaces)
	if err != nil {
		return res, err
	}
	unlock, err := lock(s.State + ".lock")
	if err != nil {
		return res, err
	}
	defer unlock()
	st, err := readState(s.State)
	if err != nil {
		return res, err
	}
	local, err := s.setups(ctx)
	if err != nil {
		return res, err
	}
	want := map[string]bool{}
	for _, w := range pub {
		if fi, err := os.Stat(w.Path); err == nil && fi.IsDir() {
			want[filepath.Clean(w.Path)] = true
		}
	}
	added := map[string]bool{}
	for _, p := range st.Added {
		added[p] = true
	}
	for _, p := range sortedKeys(want) {
		if _, ok := local[p]; ok {
			continue
		}
		if _, err := s.Orca(ctx, "repo", "add", "--path", p, "--json"); err != nil {
			res.Failed[p] = err.Error()
			continue
		}
		added[p] = true
		res.Added = append(res.Added, p)
	}
	for _, p := range sortedKeys(added) {
		if want[p] {
			continue
		}
		id, ok := local[p]
		if !ok { // removed in Orca already
			delete(added, p)
			continue
		}
		if _, err := s.Orca(ctx, "project", "setup-delete", "--setup", id, "--json"); err != nil {
			res.Kept[p] = err.Error()
			continue
		}
		delete(added, p)
		res.Removed = append(res.Removed, p)
	}
	now := time.Now().UTC()
	st.Added, st.Failed, st.SyncedAt = sortedKeys(added), res.Failed, &now
	return res, writeState(s.State, st)
}

// Status reports whether Orca has each workspace folder, with the error
// the last run hit adding one it does not. It changes nothing.
func (s Syncer) Status(ctx context.Context) (Status, error) {
	out := Status{Workspaces: map[string]Project{}}
	pub, err := config.ReadWorkspacesFile(s.Workspaces)
	if err != nil {
		return out, err
	}
	st, err := readState(s.State)
	if err != nil {
		return out, err
	}
	local, err := s.setups(ctx)
	if err != nil {
		return out, err
	}
	out.SyncedAt = st.SyncedAt
	for id, w := range pub {
		p := filepath.Clean(w.Path)
		_, in := local[p]
		pr := Project{Path: p, InOrca: in}
		if !in {
			pr.Error = st.Failed[p]
		}
		out.Workspaces[id] = pr
	}
	return out, nil
}

// setups maps each folder set up on this machine to its setup id, waiting
// for Orca to answer.
func (s Syncer) setups(ctx context.Context) (map[string]string, error) {
	deadline := time.Now().Add(s.Wait)
	for {
		out, err := s.Orca(ctx, "project", "setups", "--host", "local", "--json")
		if err == nil {
			var v struct {
				Result struct {
					Setups []setup `json:"setups"`
				} `json:"result"`
			}
			if err := json.Unmarshal(out, &v); err != nil {
				return nil, fmt.Errorf("orca-ide project setups printed no setups: %w", err)
			}
			local := map[string]string{}
			for _, su := range v.Result.Setups {
				if su.HostID == "local" && su.ID != "" && filepath.IsAbs(su.Path) {
					local[filepath.Clean(su.Path)] = su.ID
				}
			}
			return local, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%w after %s: %w", ErrUnavailable, s.Wait, err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(s.Poll):
		}
	}
}

func readState(path string) (state, error) {
	var st state
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return state{Schema: 1}, nil
	}
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return st, fmt.Errorf("%s: %w", path, err)
	}
	st.Added = slices.DeleteFunc(st.Added, func(p string) bool { return !filepath.IsAbs(p) })
	return st, nil
}

func writeState(path string, st state) error {
	st.Schema = 1
	if st.Added == nil {
		st.Added = []string{}
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(path, append(raw, '\n'), 0o600, -1, -1)
}

// lock holds an exclusive lock on path until the returned func runs, so a
// manual sync and the broker's never interleave.
func lock(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Message trims what orca-ide printed into one line for a report.
func Message(stdout, stderr []byte) string {
	var v struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(stdout, &v) == nil && v.Error.Message != "" {
		return v.Error.Message
	}
	for _, b := range [][]byte{stderr, stdout} {
		if s := strings.TrimSpace(string(b)); s != "" {
			if i := strings.LastIndexByte(s, '\n'); i >= 0 {
				s = s[i+1:]
			}
			return s
		}
	}
	return ""
}
