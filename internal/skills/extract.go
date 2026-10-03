package skills

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

const (
	maxSkillBytes = 50 << 20
	maxSkillFiles = 5000
)

// extract unpacks a git archive tar into dir within the size and file
// limits. Symlinks that are absolute or have a .. segment are dropped, so
// no chain of them can lead outside the skill.
func extract(r io.Reader, dir string, logf func(string, ...any)) error {
	tr := tar.NewReader(r)
	var files int
	var total int64
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		name := path.Clean(h.Name)
		if name == "." {
			continue
		}
		if path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return fmt.Errorf("archive entry %q leaves the skill", h.Name)
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		case tar.TypeReg, tar.TypeSymlink:
		default:
			logf("dropped %s: unsupported entry type", name)
			continue
		}
		if files++; files > maxSkillFiles {
			return fmt.Errorf("the skill has more than %d files", maxSkillFiles)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if h.Typeflag == tar.TypeSymlink {
			if path.IsAbs(h.Linkname) || slices.Contains(strings.Split(h.Linkname, "/"), "..") {
				logf("dropped symlink %s: it points outside the skill", name)
				continue
			}
			if err := os.Symlink(h.Linkname, target); err != nil {
				return err
			}
			continue
		}
		if total += h.Size; total > maxSkillBytes {
			return fmt.Errorf("the skill is larger than %d MiB", maxSkillBytes>>20)
		}
		mode := os.FileMode(0o644)
		if h.Mode&0o111 != 0 {
			mode = 0o755
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return err
		}
		_, err = io.Copy(f, io.LimitReader(tr, h.Size))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
}
