package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

const (
	currentFile = "config.json"
	prevFile    = "config.prev.json"
	corruptFile = "config.corrupt.json"
)

var (
	// ErrConflict means the caller's base revision is not the current one.
	ErrConflict = errors.New("the config changed since it was read; reload and try again")
	// ErrNoPrevious means there is no revision to revert to.
	ErrNoPrevious = errors.New("there is no previous config revision")
)

// CorruptError reports a config file Berth cannot use. The container then
// runs in recovery mode until the config is reverted or reset.
type CorruptError struct {
	Path string
	Err  error
}

func (e *CorruptError) Error() string { return fmt.Sprintf("%s: %v", e.Path, e.Err) }
func (e *CorruptError) Unwrap() error { return e.Err }

// Store keeps config.json and config.prev.json in Dir. It is safe for
// concurrent use; berth supervise is its only writer.
type Store struct {
	Dir string
	mu  sync.Mutex
}

// Load returns the current config: the defaults when there is no file, a
// *CorruptError when the file cannot be used.
func (s *Store) Load() (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

func (s *Store) load() (Config, error) {
	c, err := read(s.path(currentFile))
	if errors.Is(err, fs.ErrNotExist) {
		return Defaults(), nil
	}
	return c, err
}

// Apply merges patch into the current config. When baseRevision is the
// current revision, it writes the result as the next revision and keeps the
// replaced one as previous. It returns the config before and after.
func (s *Store) Apply(patch json.RawMessage, baseRevision int) (Config, Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, err := s.load()
	if err != nil {
		return Config{}, Config{}, err
	}
	if old.Revision != baseRevision {
		return old, old, ErrConflict
	}
	next, err := Patch(old, patch)
	if err == nil {
		err = next.Validate()
	}
	if err != nil {
		return old, old, err
	}
	next.normalize()
	next.Revision = old.Revision + 1
	if err := s.commit(&old, next); err != nil {
		return old, old, err
	}
	return old, next, nil
}

// Revert makes the previous revision current again, as a new revision.
func (s *Store) Revert() (Config, Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, err := read(s.path(prevFile))
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, Config{}, ErrNoPrevious
	}
	if err != nil {
		return Config{}, Config{}, err
	}
	return s.replace(prev)
}

// Reset makes the defaults current, as a new revision.
func (s *Store) Reset() (Config, Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.replace(Defaults())
}

// replace makes next current. A usable current config becomes previous. An
// unusable one is moved to config.corrupt.json for diagnosis, and previous
// stays as it is.
func (s *Store) replace(next Config) (Config, Config, error) {
	old, err := s.load()
	keep := &old
	var ce *CorruptError
	switch {
	case errors.As(err, &ce):
		if err := os.Rename(s.path(currentFile), s.path(corruptFile)); err != nil {
			return Config{}, Config{}, err
		}
		keep, old = nil, Config{}
		if p, err := read(s.path(prevFile)); err == nil {
			old.Revision = p.Revision
		}
	case err != nil:
		return Config{}, Config{}, err
	}
	next.Revision = max(old.Revision, next.Revision) + 1
	if err := s.commit(keep, next); err != nil {
		return old, old, err
	}
	return old, next, nil
}

func (s *Store) commit(prev *Config, next Config) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	if prev != nil {
		if err := writeConfig(s.path(prevFile), *prev); err != nil {
			return err
		}
	}
	return writeConfig(s.path(currentFile), next)
}

func (s *Store) path(name string) string { return filepath.Join(s.Dir, name) }

// read parses and validates one config file. A missing file is returned as
// fs.ErrNotExist; anything else that makes it unusable is a *CorruptError.
// Keys missing from the file keep their defaults.
func read(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, err
	}
	if err != nil {
		return Config{}, &CorruptError{Path: path, Err: err}
	}
	var probe struct {
		Schema int `json:"schema"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return Config{}, &CorruptError{Path: path, Err: err}
	}
	if probe.Schema > Schema {
		return Config{}, &CorruptError{Path: path, Err: fmt.Errorf(
			"schema %d is newer than this version of Berth understands (%d)", probe.Schema, Schema)}
	}
	c := Defaults()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Config{}, &CorruptError{Path: path, Err: err}
	}
	if err := c.Validate(); err != nil {
		return Config{}, &CorruptError{Path: path, Err: err}
	}
	c.normalize()
	return c, nil
}

func writeConfig(path string, c Config) error {
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, append(raw, '\n'), 0o600, -1, -1)
}
