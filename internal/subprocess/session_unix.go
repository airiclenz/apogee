//go:build !windows

package subprocess

import (
	"os/exec"
	"syscall"
)

// startConfinedSession makes a confined child the leader of a new session, which detaches it
// from apogee's controlling terminal: /dev/tty opens fail with ENXIO, so a confined command
// cannot push keystrokes into the operator's terminal with TIOCSTI or read it behind the TUI's
// back. The Console's pty path is the one confined run that keeps a terminal — its own pty,
// made controlling there (internal/console) — and does not come through here.
//
// Setsid replaces the Setpgid the teardown and the Confiner set rather than joining it: Go runs
// setsid and then setpgid in the child, and setpgid on a session leader fails with EPERM, so
// the start would fail. The teardown loses nothing — a session leader's PGID equals its PID,
// which is exactly the group platform.NewProcessTeardown's negative-PID kill aims at.
//
// SysProcAttr is shared with the teardown and the Confiner, so the struct is created if absent
// and never replaced.
func startConfinedSession(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
	cmd.SysProcAttr.Setpgid = false
}
