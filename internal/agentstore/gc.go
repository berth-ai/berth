package agentstore

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// GCResult lists what gc did, by version hash (tmp dirs by name).
type GCResult struct {
	Removed []string `json:"removed"`
	Kept    []string `json:"kept"`
	InUse   []string `json:"in_use"`
}

// GC keeps current, previous and the newest keep other complete versions,
// never removes a version referenced by inUse, and deletes stale tmp dirs.
// The caller must hold the store lock.
func (s Store) GC(keep int, inUse []string) (GCResult, error) {
	res := GCResult{Removed: []string{}, Kept: []string{}, InUse: []string{}}
	protected := map[string]bool{}
	for _, get := range []func() (*Version, error){s.Current, s.Previous} {
		v, err := get()
		if err != nil {
			return res, err
		}
		if v != nil {
			protected[v.Hash] = true
		}
	}
	versions, err := s.List()
	if err != nil {
		return res, err
	}
	extra := 0
	for _, v := range versions {
		switch {
		case protected[v.Hash]:
			res.Kept = append(res.Kept, v.Hash)
		case extra < keep:
			extra++
			res.Kept = append(res.Kept, v.Hash)
		case referenced(inUse, v.Dir):
			res.InUse = append(res.InUse, v.Hash)
		default:
			if err := os.RemoveAll(v.Dir); err != nil {
				return res, err
			}
			res.Removed = append(res.Removed, v.Hash)
		}
	}
	entries, err := os.ReadDir(s.versionsDir())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return res, err
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			if err := os.RemoveAll(filepath.Join(s.versionsDir(), e.Name())); err != nil {
				return res, err
			}
			res.Removed = append(res.Removed, e.Name())
		}
	}
	return res, nil
}

func referenced(paths []string, dir string) bool {
	for _, p := range paths {
		p = filepath.Clean(p)
		if p == dir || strings.HasPrefix(p, dir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// InUsePaths collects every path a running process references through its
// executable, working directory, memory maps, arguments or
// PLAYWRIGHT_BROWSERS_PATH. Arguments and environment matter because Node
// agents run the shared Node binary and read, not map, their JavaScript.
func InUsePaths(procRoot string) []string {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil
	}
	var paths []string
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		pid := filepath.Join(procRoot, e.Name())
		for _, link := range []string{"exe", "cwd"} {
			if target, err := os.Readlink(filepath.Join(pid, link)); err == nil {
				paths = append(paths, target)
			}
		}
		if raw, err := os.ReadFile(filepath.Join(pid, "maps")); err == nil {
			for line := range strings.SplitSeq(string(raw), "\n") {
				if f := strings.Fields(line); len(f) >= 6 {
					paths = append(paths, strings.Join(f[5:], " "))
				}
			}
		}
		if raw, err := os.ReadFile(filepath.Join(pid, "cmdline")); err == nil {
			paths = append(paths, strings.Split(string(raw), "\x00")...)
		}
		if raw, err := os.ReadFile(filepath.Join(pid, "environ")); err == nil {
			for kv := range strings.SplitSeq(string(raw), "\x00") {
				if v, ok := strings.CutPrefix(kv, "PLAYWRIGHT_BROWSERS_PATH="); ok {
					paths = append(paths, v)
				}
			}
		}
	}
	return paths
}
