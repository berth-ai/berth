package broker

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Listen replaces any stale file at path with a unix socket that only root
// and group gid can use (0660). gid < 0 keeps the caller's group.
func Listen(path string, gid int) (net.Listener, error) {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	// Created owner-only, so nobody else can connect before the chmod.
	old := syscall.Umask(0o177)
	l, err := net.Listen("unix", path)
	syscall.Umask(old)
	if err != nil {
		return nil, err
	}
	if gid >= 0 {
		if err := os.Chown(path, -1, gid); err != nil {
			l.Close()
			return nil, err
		}
	}
	if err := os.Chmod(path, 0o660); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}

// CommandExec runs the berth binary at path. Cancelling ctx sends SIGTERM,
// then SIGKILL after 10 s; berth's own failure handling keeps current intact.
func CommandExec(path string) Exec {
	return func(ctx context.Context, args []string, stdout, stderr io.Writer) (int, error) {
		cmd := exec.CommandContext(ctx, path, args...)
		cmd.Stdout, cmd.Stderr = stdout, stderr
		cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
		cmd.WaitDelay = 10 * time.Second
		err := cmd.Run()
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode(), nil
		}
		if err != nil {
			return -1, err
		}
		return 0, nil
	}
}
