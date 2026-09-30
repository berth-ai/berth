package agentstore

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Lock takes an exclusive flock on Root/.lock, waiting up to timeout.
func (s Store) Lock(timeout time.Duration) (func(), error) {
	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(s.Root, ".lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	fd := int(f.Fd())
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(fd, syscall.LOCK_UN)
				_ = f.Close()
			}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			f.Close()
			return nil, err
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, ErrLockBusy
		}
		time.Sleep(100 * time.Millisecond)
	}
}
