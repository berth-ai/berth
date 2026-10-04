//go:build !linux

package agentlogin

import "syscall"

// procAttr starts the CLI in its own session.
func procAttr(tty bool) *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true, Setctty: tty}
}
