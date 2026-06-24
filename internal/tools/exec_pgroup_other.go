//go:build windows

package tools

import (
	"os/exec"
	"time"
)

// setProcessGroupTeardown is the Windows stub of the §2.4 teardown. POSIX process groups
// (Setpgid + negative-PID kill) have no portable Windows equivalent, and no Confiner backend
// exists for Windows until Phase 5, so a confined subprocess never runs here. The stub still
// wires cmd.Cancel (kill the leader) + cmd.WaitDelay so a cancelled/timed-out command on
// Windows is at least reaped at the leader and Wait cannot block forever on a held pipe.
//
// TODO(phase-5): real Windows job-object teardown (kill the whole tree).
func setProcessGroupTeardown(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return cmd.Process.Kill()
	}
	cmd.WaitDelay = processWaitDelay
}

// processWaitDelay bounds the post-exit drain so a child holding a pipe open cannot wedge
// Wait indefinitely after the process has been signalled.
const processWaitDelay = 5 * time.Second
