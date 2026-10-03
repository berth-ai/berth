package config

import (
	"io/fs"
	"os"
	"path/filepath"
)

// WriteFileAtomic replaces path with data. It writes a temporary file in the
// same directory, sets its mode (and owner, unless uid and gid are -1), and
// calls fsync. Then it renames the file over path and syncs the directory, so
// readers see either the old or the new content and a power loss keeps one.
func WriteFileAtomic(path string, data []byte, perm fs.FileMode, uid, gid int) (err error) {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(tmp)
		}
	}()
	if err = f.Chmod(perm); err != nil {
		return err
	}
	if uid != -1 || gid != -1 {
		if err = f.Chown(uid, gid); err != nil {
			return err
		}
	}
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
