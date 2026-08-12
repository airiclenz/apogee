package tools

import "os/exec"

// treeKillAction names what the §2.4 cancel path must do to a running subprocess when the
// run's context is cancelled or its timeout fires. It exists so the one decision both
// backends share — "is there a container holding the descendants, or only a leader?" — is a
// pure function testable on every OS, not logic buried in a syscall path that only compiles
// on the host that owns it.
type treeKillAction int

const (
	// treeKillNothing: the process never started, so there is nothing to reap. Cancel can
	// still be invoked in that window (Start failed, or ctx was already done), and a kill
	// against a nil Process would panic.
	treeKillNothing treeKillAction = iota
	// treeKillLeader: the process started but nothing holds its descendants, so only the
	// leader can be killed. This is the degraded rung — a descendant may be orphaned — and
	// is reached on Windows when the Job Object could not be created or the assignment was
	// refused. POSIX never reaches it (see planTreeKill).
	treeKillLeader
	// treeKillTree: a container holds the whole tree — a POSIX process group (Setpgid, killed
	// as the negative PID) or a Windows Job Object (TerminateJobObject) — so one call reaps
	// the descendants too. This is the contract's intent: a cancelled command orphans nothing.
	treeKillTree
)

// planTreeKill decides how a cancelled run is reaped. started reports whether the process was
// launched at all (cmd.Process != nil); treeHeld reports whether this platform is holding the
// whole tree. POSIX passes treeHeld=true unconditionally because the kernel establishes the
// process group at fork, before the child can spawn anything; Windows can only assign to its
// Job Object after CreateProcess returns, so it passes whether that assignment succeeded.
func planTreeKill(started, treeHeld bool) treeKillAction {
	switch {
	case !started:
		return treeKillNothing
	case treeHeld:
		return treeKillTree
	default:
		return treeKillLeader
	}
}

// processTeardown is the per-run state the §2.4 teardown needs *after* the process exists.
// setProcessGroupTeardown builds it while wiring cmd.Cancel and cmd.WaitDelay; runWithTeardown
// drives it around cmd.Wait. POSIX needs no state to hold the tree (the process group is a
// fork-time property of the cmd, so contain and release stay noTeardown's no-ops) and implements
// the post-Wait reap alone; Windows has no fork-time hook at all, so its implementation assigns
// the started process to a Job Object, terminates it, and releases the handle when the run is
// over.
type processTeardown interface {
	// contain places the started process (cmd.Process is non-nil) under whatever holds its
	// descendants. It is best-effort: a failure degrades the cancel path to a leader-only
	// kill (planTreeKill), never an error the tool surfaces — teardown is a safety net, not
	// the confinement fence (ADR 0020).
	contain(cmd *exec.Cmd)
	// reap tears the tree down once the run is over — the clean-exit counterpart of
	// cmd.Cancel, which only ever fires when the run's context is done. The execution tools
	// are one-shot by contract (ADR 0008: a fresh process per call, no persistent shell), so
	// a descendant the command backgrounded must not outlive the call that started it; before
	// this hook existed only the cancelled and timed-out paths reaped anything, and a
	// backgrounded grandchild survived a clean exit while the call still rendered as success.
	// It runs after Wait has returned, so the leader is already gone and only descendants can
	// still be held. Best-effort like contain: teardown is a safety net, never the
	// confinement fence (ADR 0020).
	reap(cmd *exec.Cmd)
	// release drops any OS resource the containment holds. The resource exists from the
	// moment the teardown was built, which is before the process does, so release runs on
	// every exit from the run — after Wait has returned on the normal path, and equally on
	// the confine-refusal and Start-failure paths that never reach Wait. It is deferred once
	// by the caller that built the teardown (runSubprocess), and stays idempotent so a second
	// call could never double-free.
	release()
}

// newProcessTeardown builds the per-run teardown for cmd. It is the package's seam onto the
// platform constructor (setProcessGroupTeardown, one per build tag) — a package var so a test
// can substitute a fake processTeardown and observe the release lifecycle on every OS, the
// same idiom as shellHost. Production code never reassigns it.
var newProcessTeardown = setProcessGroupTeardown

// noTeardown is the inert processTeardown: every hook is a no-op. POSIX embeds it
// (exec_pgroup_unix.go) because the process group is established by the kernel at fork, so that
// backend needs neither a post-start step nor an owned handle and overrides reap alone.
type noTeardown struct{}

func (noTeardown) contain(*exec.Cmd) {}

func (noTeardown) reap(*exec.Cmd) {}

func (noTeardown) release() {}

// runWithTeardown starts cmd, hands the started process to the platform teardown, waits for it,
// and reaps whatever the command left behind — the Start/Wait split that cmd.Run() would
// otherwise hide. The split is load-bearing on Windows: a process can only be assigned to a Job
// Object once CreateProcess has returned, so the teardown needs the gap between Start and Wait.
//
// The reap runs on EVERY path out of Wait, not only the cancelled one. cmd.Cancel fires on
// context cancellation alone, so before this a command that backgrounded a grandchild left it
// running past the call and the tool still reported success — a persistence primitive rendered
// as a green tick. The one-shot contract the execution tools already document (terminal.go,
// ADR 0008) says otherwise, so the tree goes when the call does. Wait's error is returned
// unchanged: the reap is teardown, never a result the tool reports.
//
// It does NOT release td: the resource exists from the moment the teardown was built, which is
// before this function is reached, so releasing it belongs to the caller that built it
// (runSubprocess, its only caller). That is the only placement a Start failure — or a Confine
// failure, which never gets here at all — also drops.
func runWithTeardown(cmd *exec.Cmd, td processTeardown) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	td.contain(cmd)
	err := cmd.Wait()
	td.reap(cmd)
	return err
}
