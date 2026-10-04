//go:build linux

package agentlogin

import "syscall"

// procAttr starts the CLI in its own session; Pdeathsig kills it when the
// helper dies, so a cancelled job never leaves a sign-in running.
func procAttr(tty bool) *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true, Setctty: tty, Pdeathsig: syscall.SIGKILL}
}
