//go:build !windows

package tools

import (
	"os/exec"
	"syscall"
	"time"
)

// setProcessGroupTeardown wires the §2.4 process-group teardown onto cmd (POSIX):
//
//   - SysProcAttr.Setpgid so the child and its descendants share a process group. The
//     Confiner backend also sets this when it wraps the command (seatbelt / landlock
//     re-exec); setting it here too is what gives an UNCONFINED subprocess (the lower
//     modes, or confine-to-workspace=false) the same clean teardown.
//   - cmd.Cancel signals the whole group — SIGKILL to the negative PID (-pgid) — when
//     the run's context is cancelled or times out, so a cancelled/timed-out command
//     never orphans its children (or, when confined, an orphaned sandbox-exec wrapper).
//   - cmd.WaitDelay bounds how long Wait blocks for I/O to drain after the process exits
//     or is killed, so a child that leaves a pipe open cannot wedge the tool forever.
//
// It must be called after exec.CommandContext built cmd but before cmd.Run/Output, and
// before any Confine (the Confiner only appends to SysProcAttr, never clearing Cancel).
func setProcessGroupTeardown(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true

	// cmd.Cancel runs when the CommandContext's ctx is done. Killing -PID targets the
	// whole process group (Setpgid put the child in its own group whose PGID == its PID),
	// reaping descendants the wrapper spawned. A "process already finished" is benign.
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			// The group may already be gone (the process exited between Done and here);
			// fall back to killing the leader directly, ignoring "no such process".
			_ = cmd.Process.Kill()
		}
		return nil
	}
	cmd.WaitDelay = processWaitDelay
}

// processWaitDelay bounds the post-exit drain so a child holding a pipe open cannot wedge
// Wait indefinitely after the process has been signalled.
const processWaitDelay = 5 * time.Second
