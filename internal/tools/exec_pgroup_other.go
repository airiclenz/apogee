//go:build windows

package tools

import (
	"os/exec"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// setProcessGroupTeardown wires the §2.4 teardown onto cmd (Windows). POSIX process groups
// (Setpgid + a negative-PID kill) have no Windows equivalent; the Windows facility that holds
// a whole process tree is a **Job Object**, so this backend implements the same contract with
// one:
//
//   - A Job Object is created here, before the process starts, carrying
//     JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE. That limit is the crash net: if apogee dies mid-run
//     the handle closes with it and the kernel reaps the tree, the closest Windows has to the
//     POSIX guarantee that a cancelled command never leaves descendants behind.
//   - runWithTeardown assigns the started process to the job (contain). A child cannot escape
//     a job it is assigned to unless the job permits breakaway, which this one does not, so
//     every descendant it spawns from then on is held too.
//   - cmd.Cancel terminates the JOB — not the leader — when the run's context is cancelled or
//     times out, which is the negative-PID kill's counterpart. If the job never took the
//     process it falls back to killing the leader alone (planTreeKill's degraded rung).
//   - cmd.WaitDelay bounds how long Wait blocks draining I/O after the kill, so a descendant
//     holding a pipe open cannot wedge the tool forever — identical to POSIX.
//
// It must be called after exec.CommandContext built cmd but before cmd.Start, and before any
// Confine: the Windows Confiner sets SysProcAttr.Token and nothing else (contract §9.2), and
// never touches cmd.Cancel, so the two compose. Job objects are teardown, never a fence
// (ADR 0020) — the confinement boundary is the token, not this.
//
// Known, documented gap: Windows offers no way to create a process directly into a job
// through os/exec (that needs PROC_THREAD_ATTRIBUTE_JOB_LIST on a STARTUPINFOEX, which
// syscall.SysProcAttr cannot express), so there is a sub-millisecond window between
// CreateProcess returning and the assignment in which a descendant spawned by the child would
// escape the job. A real shell has to start and parse its command line first, so the window
// closes long before it can spawn anything; the alternative — a suspended start — is
// unreachable because os/exec closes the process's initial thread handle, leaving nothing to
// resume.
func setProcessGroupTeardown(cmd *exec.Cmd) processTeardown {
	td := &jobTeardown{job: windows.InvalidHandle}
	if job, err := newTreeJob(); err == nil {
		td.job = job
	}

	cmd.Cancel = func() error { return td.cancel(cmd) }
	cmd.WaitDelay = processWaitDelay
	return td
}

// processWaitDelay bounds the post-exit drain so a child holding a pipe open cannot wedge
// Wait indefinitely after the process has been signalled.
const processWaitDelay = 5 * time.Second

// jobTeardown is the Windows processTeardown: the Job Object holding one run's process tree.
// cmd.Cancel runs on os/exec's watchdog goroutine while contain/release run on the goroutine
// driving the command, so the handle and the assignment flag are mutex-guarded.
type jobTeardown struct {
	mu sync.Mutex
	// job is the Job Object holding the tree, or windows.InvalidHandle when one could not be
	// created (teardown then degrades to a leader-only kill) or after release closed it.
	job windows.Handle
	// assigned reports that the process actually joined the job, which is what makes a job
	// termination reap the whole tree rather than nothing.
	assigned bool
}

// newTreeJob creates an unnamed Job Object that kills everything still in it when its last
// handle closes.
func newTreeJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return windows.InvalidHandle, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := setJobLimits(job, &info); err != nil {
		_ = windows.CloseHandle(job)
		return windows.InvalidHandle, err
	}
	return job, nil
}

// setJobLimits applies info as the job's extended limit information.
func setJobLimits(job windows.Handle, info *windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION) (int, error) {
	return windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(info)),
		uint32(unsafe.Sizeof(*info)),
	)
}

// contain assigns the freshly started process to the job, so a cancel reaps its whole tree.
// Every failure is silent by design: the run continues, and cancel degrades to killing the
// leader (planTreeKill) rather than failing a command the user asked for.
func (t *jobTeardown) contain(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.job == windows.InvalidHandle {
		return
	}
	// Opening by PID is race-free here even though PIDs are recycled: os/exec still holds an
	// open handle to the process (until Wait releases it), and Windows cannot reuse a PID
	// while any handle to it is open.
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return
	}
	defer func() { _ = windows.CloseHandle(h) }()
	if err := windows.AssignProcessToJobObject(t.job, h); err != nil {
		return
	}
	t.assigned = true
}

// cancel is cmd.Cancel: it reaps the run when the context is cancelled or the timeout fires.
// It returns nil like the POSIX backend — the kill is the teardown, not an error the tool
// reports; Wait surfaces the outcome.
func (t *jobTeardown) cancel(cmd *exec.Cmd) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	switch planTreeKill(cmd.Process != nil, t.assigned && t.job != windows.InvalidHandle) {
	case treeKillTree:
		// Terminating the job is explicit and immediate; the KILL_ON_JOB_CLOSE limit only
		// covers the path where nobody is left to make this call.
		if err := windows.TerminateJobObject(t.job, 1); err != nil {
			// The job may already be empty (the process exited between Done and here);
			// fall back to the leader, ignoring "process already finished".
			_ = cmd.Process.Kill()
		}
	case treeKillLeader:
		_ = cmd.Process.Kill()
	case treeKillNothing:
	}
	return nil
}

// release drops the job handle when the run is over — after Wait on the normal path, and also
// on the confine-refusal and Start-failure paths, which never reach Wait but own the handle
// from the moment setProcessGroupTeardown created it. The KILL_ON_JOB_CLOSE limit is cleared
// first: on a clean completion a process the command deliberately left running must outlive
// the call, exactly as a backgrounded process outlives its POSIX process-group leader. The
// limit exists for the crash path, and the cancel path has already terminated the job
// explicitly, so clearing it here costs nothing and keeps the two backends' observable
// behaviour the same.
func (t *jobTeardown) release() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.job == windows.InvalidHandle {
		return
	}
	var noLimits windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	_, _ = setJobLimits(t.job, &noLimits)
	_ = windows.CloseHandle(t.job)
	t.job = windows.InvalidHandle
	t.assigned = false
}
