package agentset

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/berth-ai/berth/internal/agentstore"
	"github.com/berth-ai/berth/internal/locksource"
	"github.com/berth-ai/berth/internal/registry"
)

// maxReport bounds berth agents status's output, which root decodes.
const maxReport = 32 << 20

// StoreRoot is where added agents install in the agent user's home. It is
// always ~/.local/share: root derives wrapper targets from the home alone.
func StoreRoot(home string) string { return filepath.Join(home, ".local", "share", "berth", "agents") }

// Installed lists, by id, the added agents with a complete current install
// under root. Only agent calls it: it reads files agent owns. A store it
// cannot read is returned as a problem and left out.
func Installed(root string) ([]Added, []error) {
	dirs, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []error{err}
	}
	var out []Added
	var problems []error
	for _, d := range dirs {
		if !d.IsDir() || !registry.IDPattern.MatchString(d.Name()) {
			continue
		}
		a, err := installed(agentstore.Store{Root: filepath.Join(root, d.Name())}, d.Name())
		if err != nil {
			problems = append(problems, fmt.Errorf("added agent %s: %w", d.Name(), err))
			continue
		}
		if a != nil {
			out = append(out, *a)
		}
	}
	return out, problems
}

func installed(s agentstore.Store, id string) (*Added, error) {
	cur, err := s.Current()
	if err != nil || cur == nil {
		return nil, err
	}
	if len(cur.Manifest.Packages) != 1 {
		return nil, fmt.Errorf("its install records %d packages, want 1", len(cur.Manifest.Packages))
	}
	pkg := slices.Collect(maps.Keys(cur.Manifest.Packages))[0]
	raw, err := readLimited(filepath.Join(cur.Dir, "package.json"))
	if err != nil {
		return nil, err
	}
	bins, err := agentstore.PackageBins(cur.Dir, pkg)
	if err != nil {
		return nil, err
	}
	a := &Added{ID: id, Package: pkg, Version: cur.Manifest.Packages[pkg], Hash: cur.Hash,
		PackageJSON: raw, Bins: slices.Sorted(maps.Keys(bins))}
	if prev, err := s.Previous(); err == nil && prev != nil {
		a.PreviousVersion = prev.Manifest.Packages[pkg]
	}
	return a, nil
}

func readLimited(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, locksource.MaxFileSize+1))
	if err == nil && len(raw) > locksource.MaxFileSize {
		err = fmt.Errorf("%s is larger than %d bytes", path, locksource.MaxFileSize)
	}
	return raw, err
}

// Pick keeps the installs of the packages in wanted.
func Pick(all []Added, wanted []string) []Added {
	var out []Added
	for _, a := range all {
		if slices.Contains(wanted, a.Package) {
			out = append(out, a)
		}
	}
	return out
}

// DecodeReport reads berth agents status --json; root calls it on output it
// captured from a command run as agent.
func DecodeReport(raw []byte) (Report, error) {
	if len(raw) > maxReport {
		return Report{}, errors.New("the added-agent report is too large")
	}
	var r Report
	if err := json.Unmarshal(raw, &r); err != nil {
		return Report{}, fmt.Errorf("added-agent report: %w", err)
	}
	if r.Agents == nil {
		r.Agents = []Added{}
	}
	if r.Errors == nil {
		r.Errors = map[string]string{}
	}
	return r, nil
}
