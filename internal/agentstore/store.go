// Package agentstore manages the agent store at /opt/agents: complete
// versions named by lock hash, atomic current/previous links, installs and gc.
package agentstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/berth-ai/bert/internal/locksource"
)

var (
	ErrNotFound   = errors.New("agent set not found")
	ErrNoPrevious = errors.New("no previous agent set to roll back to")
	ErrLockBusy   = errors.New("agent store is locked by another berth command")
)

const (
	completeFile = ".complete"
	sourceFile   = "source.json"
	stateFile    = "state.json"
)

var hashPattern = regexp.MustCompile(`^[0-9a-f]{12}$`)

// Manifest is the content of .complete, written last by an install.
type Manifest struct {
	Packages    map[string]string `json:"packages"`
	InstalledAt time.Time         `json:"installed_at"`
}

// Version is one complete installed agent set.
type Version struct {
	Hash     string
	Dir      string
	Source   locksource.Source
	Manifest Manifest
}

// State records whether boot-time ensure follows the image seed.
type State struct {
	FollowImage bool `json:"follow_image"`
}

// Store is the agent store rooted at Root.
type Store struct{ Root string }

func (s Store) versionsDir() string { return filepath.Join(s.Root, "versions") }

// VersionDir is where version hash lives once complete.
func (s Store) VersionDir(hash string) string { return filepath.Join(s.versionsDir(), hash) }

// Get returns a complete version or ErrNotFound.
func (s Store) Get(hash string) (*Version, error) {
	if !hashPattern.MatchString(hash) {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, hash)
	}
	dir := s.VersionDir(hash)
	var m Manifest
	var src locksource.Source
	for file, v := range map[string]any{completeFile: &m, sourceFile: &src} {
		if err := readJSON(filepath.Join(dir, file), v); err != nil {
			// A missing or truncated file (say, after a power loss) means the
			// install never finished; reinstalling it heals the store.
			if errors.Is(err, fs.ErrNotExist) || isDecodeError(err) {
				return nil, fmt.Errorf("%w: %s", ErrNotFound, hash)
			}
			return nil, err
		}
	}
	return &Version{Hash: hash, Dir: dir, Source: src, Manifest: m}, nil
}

// Current returns the active version, or nil when there is none.
func (s Store) Current() (*Version, error) { return s.link("current") }

// Previous returns the rollback target, or nil when there is none.
func (s Store) Previous() (*Version, error) { return s.link("previous") }

func (s Store) link(name string) (*Version, error) {
	target, err := os.Readlink(filepath.Join(s.Root, name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v, err := s.Get(filepath.Base(target))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return v, err
}

// List returns all complete versions, newest install first.
func (s Store) List() ([]Version, error) {
	entries, err := os.ReadDir(s.versionsDir())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Version
	for _, e := range entries {
		if !e.IsDir() || !hashPattern.MatchString(e.Name()) {
			continue
		}
		v, err := s.Get(e.Name())
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Manifest.InstalledAt.Equal(out[j].Manifest.InstalledAt) {
			return out[i].Manifest.InstalledAt.After(out[j].Manifest.InstalledAt)
		}
		return out[i].Hash < out[j].Hash
	})
	return out, nil
}

// Flip makes hash current and the old current previous. It reports whether
// anything changed.
func (s Store) Flip(hash string) (bool, error) {
	if _, err := s.Get(hash); err != nil {
		return false, err
	}
	cur, err := s.Current()
	if err != nil {
		return false, err
	}
	if cur != nil && cur.Hash == hash {
		return false, nil
	}
	if cur != nil {
		if err := s.swapLink("previous", cur.Hash); err != nil {
			return false, err
		}
	}
	return true, s.swapLink("current", hash)
}

// swapLink atomically replaces root/name with a relative link to versions/hash.
func (s Store) swapLink(name, hash string) error {
	tmp := filepath.Join(s.Root, name+".tmp")
	if err := os.Remove(tmp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Symlink(filepath.Join("versions", hash), tmp); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.Root, name))
}

// Rollback flips to hash, or to previous when hash is empty.
func (s Store) Rollback(hash string) (*Version, error) {
	if hash == "" {
		prev, err := s.Previous()
		if err != nil {
			return nil, err
		}
		if prev == nil {
			return nil, ErrNoPrevious
		}
		hash = prev.Hash
	}
	if _, err := s.Flip(hash); err != nil {
		return nil, err
	}
	return s.Current()
}

// ReadState returns the follow-image state. Without a state file it follows
// the image unless current came from a ref or a directory.
func (s Store) ReadState() (State, error) {
	var st State
	err := readJSON(filepath.Join(s.Root, stateFile), &st)
	if errors.Is(err, fs.ErrNotExist) {
		cur, err := s.Current()
		if err != nil {
			return State{}, err
		}
		return State{FollowImage: cur == nil || cur.Source.Origin == locksource.OriginImage}, nil
	}
	return st, err
}

// WriteState records the follow-image state.
func (s Store) WriteState(st State) error {
	return writeJSON(filepath.Join(s.Root, stateFile), st)
}

func readJSON(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func isDecodeError(err error) bool {
	var syntax *json.SyntaxError
	var typ *json.UnmarshalTypeError
	return errors.As(err, &syntax) || errors.As(err, &typ)
}
