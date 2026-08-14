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
// The group holds every descendant that has not deliberately left it. A descendant that calls
// setsid or setpgid(0,0) becomes the leader of a new group, which no negative-PID kill aimed at
// this one reaches: it escapes both cmd.Cancel and the clean-exit reap and survives the call,
// unsupervised, while the tool reports the leader's own exit status. It stays inside whatever
// write-fence the Confiner installed — changing process group sheds no landlock ruleset and no
// seatbelt profile — so the residual is loss of supervision, not loss of confinement. It is
// accepted rather than enforced against (no subreaper, no descendant tracking): deliberately
// backgrounding a process is legitimate terminal use. The residual is POSIX-specific — Windows'
// Job Object holds a descendant unless the job permits breakaway, which it does not
// (exec_pgroup_other.go).
//
// It must be called after exec.CommandContext built cmd but before cmd.Start, and before any
// Confine (the Confiner only appends to SysProcAttr, never clearing Cancel). The returned
// processTeardown is a pgroupTeardown: the group is a fork-time property of the cmd, so POSIX
// needs no post-start step and owns no handle, and the one hook it implements is the clean-exit
// reap. The contain/release halves of the seam exist for Windows, whose Job Object can only
// take the process once it has been created (exec_pgroup_other.go).
func setProcessGroupTeardown(cmd *exec.Cmd) processTeardown {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true

	// cmd.Cancel runs when the CommandContext's ctx is done; the reap below runs when the
	// command finished on its own. Both want the same kill, so both go through
	// killProcessGroup.
	cmd.Cancel = func() error {
		killProcessGroup(cmd)
		return nil
	}
	cmd.WaitDelay = processWaitDelay
	return pgroupTeardown{}
}

// killProcessGroup SIGKILLs the whole process group. Killing -PID targets the group (Setpgid
// put the child in its own group whose PGID == its PID), reaping descendants the wrapper
// spawned. A "process already finished" is benign.
func killProcessGroup(cmd *exec.Cmd) {
	// treeHeld is unconditionally true here: Setpgid establishes the group at fork, before
	// the child can spawn anything, so the group holds every descendant that has not
	// deliberately left it. A descendant that called setsid/setpgid(0,0) is outside the
	// group and outside this kill (see setProcessGroupTeardown); treeHeld describes the
	// container, not escape from it. The leader-only rung is Windows', where the job
	// assignment can be refused.
	switch planTreeKill(cmd.Process != nil, true) {
	case treeKillTree:
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			// The group may already be gone (the process exited between Done and
			// here); fall back to killing the leader, ignoring "no such process".
			_ = cmd.Process.Kill()
		}
	case treeKillLeader:
		_ = cmd.Process.Kill()
	case treeKillNothing:
	}
}

// pgroupTeardown is the POSIX processTeardown. The group is a fork-time property of the cmd, so
// contain and release stay noTeardown's no-ops; the one step this backend owns is the clean-exit
// reap.
type pgroupTeardown struct{ noTeardown }

// reap kills the process group once the run is over, so a descendant the command backgrounded
// does not outlive the one-shot call (processTeardown.reap) — unless it left the group on its
// own, which nothing aimed at the group can reach (see setProcessGroupTeardown). By the time it
// runs the leader has been waited on, so only its descendants can still be in the group — and
// while the group holds a member the kernel cannot recycle its PGID, which is what keeps the
// negative-PID kill aimed at this run's tree and nothing else. An empty group answers ESRCH and
// the leader fallback answers "process already finished"; both are the ordinary case of a
// command that left nothing behind.
func (pgroupTeardown) reap(cmd *exec.Cmd) {
	killProcessGroup(cmd)
}

// processWaitDelay bounds the post-exit drain so a child holding a pipe open cannot wedge
// Wait indefinitely after the process has been signalled. It is a var rather than a const so a
// test can shrink it and exercise the drain-wedged path in milliseconds; production never
// reassigns it.
var processWaitDelay = 5 * time.Second
