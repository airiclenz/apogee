//go:build !windows

package tools

import "syscall"

// syscallKill0 sends signal 0 to pid — a liveness probe that delivers no signal. A nil error
// means the process exists; ESRCH means it is gone. Used by the process-group teardown tests.
func syscallKill0(pid int) error {
	return syscall.Kill(pid, 0)
}

// killPID terminates pid, best-effort — the POSIX counterpart of the Windows helper of the same
// name. It exists so a test that might leave a backgrounded process behind cannot leak it into
// the machine, whichever way its assertion goes.
func killPID(pid int) {
	_ = syscall.Kill(pid, syscall.SIGKILL)
}
