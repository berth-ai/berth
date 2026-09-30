// Package locksource reads and validates the agents lockfile from the image
// seed, a GitHub ref or a local directory.
package locksource

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"
)

// MaxFileSize bounds package.json and package-lock.json.
const MaxFileSize = 10 << 20

// ErrInvalid marks a lock that fails validation.
var ErrInvalid = errors.New("invalid agent lock")

var exactVersion = regexp.MustCompile(`^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

// Origin says where a lock came from.
type Origin string

const (
	OriginImage Origin = "image"
	OriginRef   Origin = "ref"
	OriginDir   Origin = "dir"
)

// Source is recorded as source.json in every installed version.
type Source struct {
	Origin    Origin    `json:"origin"`
	Repo      string    `json:"repo,omitempty"`
	Ref       string    `json:"ref,omitempty"`
	Commit    string    `json:"commit,omitempty"`
	FetchedAt time.Time `json:"fetched_at"`
}

// Lock is a validated pair of agents/package.json and package-lock.json.
type Lock struct {
	PackageJSON []byte
	PackageLock []byte
	Deps        map[string]string
	Hash        string
	Source      Source
}

// Hash names a version: the first 12 hex digits of sha256(package-lock.json).
func Hash(packageLock []byte) string {
	sum := sha256.Sum256(packageLock)
	return hex.EncodeToString(sum[:])[:12]
}

// Parse validates the two files and returns the lock.
func Parse(pkg, lock []byte, src Source) (*Lock, error) {
	if len(pkg) > MaxFileSize || len(lock) > MaxFileSize {
		return nil, invalid("lock files must be at most 10 MiB")
	}
	var p struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(pkg, &p); err != nil {
		return nil, invalid("package.json: %v", err)
	}
	if len(p.Dependencies) == 0 {
		return nil, invalid("package.json has no dependencies")
	}
	for _, name := range slices.Sorted(maps.Keys(p.Dependencies)) {
		if v := p.Dependencies[name]; !exactVersion.MatchString(v) {
			return nil, invalid("package.json: %s@%s is not an exact version", name, v)
		}
	}
	var l struct {
		LockfileVersion int `json:"lockfileVersion"`
		Packages        map[string]struct {
			Dependencies map[string]string `json:"dependencies"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(lock, &l); err != nil {
		return nil, invalid("package-lock.json: %v", err)
	}
	if l.LockfileVersion != 3 {
		return nil, invalid("package-lock.json: lockfileVersion is %d, want 3", l.LockfileVersion)
	}
	root, ok := l.Packages[""]
	if !ok {
		return nil, invalid("package-lock.json has no root package")
	}
	if !maps.Equal(root.Dependencies, p.Dependencies) {
		return nil, invalid("package-lock.json root dependencies differ from package.json; run npm install")
	}
	return &Lock{PackageJSON: pkg, PackageLock: lock, Deps: p.Dependencies, Hash: Hash(lock), Source: src}, nil
}

// FromDir reads package.json and package-lock.json from dir.
func FromDir(dir string, src Source) (*Lock, error) {
	pkg, err := readLimited(filepath.Join(dir, "package.json"))
	if err != nil {
		return nil, err
	}
	lock, err := readLimited(filepath.Join(dir, "package-lock.json"))
	if err != nil {
		return nil, err
	}
	return Parse(pkg, lock, src)
}

func readLimited(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, invalid("%v", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxFileSize+1))
	if err != nil {
		return nil, invalid("%s: %v", path, err)
	}
	return data, nil
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}
