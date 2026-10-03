//go:build windows

package platform

import (
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// NewProcessTeardown wires the §2.4 teardown onto cmd (Windows). POSIX process groups
// (Setpgid + a negative-PID kill) have no Windows equivalent; the Windows facility that holds
// a whole process tree is a **Job Object**, so this backend implements the same contract with
// one:
//
//   - A Job Object is created here, before the process starts, carrying
//     JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE. That limit is the crash net: if apogee dies mid-run
//     the handle closes with it and the kernel reaps the tree, the closest Windows has to the
//     POSIX guarantee that a cancelled command never leaves descendants behind.
//   - The caller assigns the started process to the job (Contain) the moment Start returns —
//     RunWithTeardown for the execution tools, mcp's stdioTransport.Connect for a stdio MCP
//     server, before its handshake. A child cannot escape a job it is assigned to unless the job
//     permits breakaway, which this one does not, so every descendant it spawns from then on is
//     held too.
//   - cmd.Cancel terminates the JOB — not the leader — when the run's context is cancelled or
//     times out, which is the negative-PID kill's counterpart. If the job never took the
//     process it falls back to killing the leader alone (planTreeKill's degraded rung).
//   - RunWithTeardown reaps the job the same way once a run that was NOT cancelled has been
//     waited on, so a one-shot call leaves no descendants on any exit path (ProcessTeardown.Reap).
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
// resume (StartPseudoConsole, which calls CreateProcess itself, does start suspended and has no
// such window). The window stays that small only because every caller runs Contain straight after
// cmd.Start and before handing the process any input: a caller that let a protocol exchange
// run first (an MCP server's handshake) would widen it to that whole round-trip.
func NewProcessTeardown(cmd *exec.Cmd) ProcessTeardown {
	td := newJobTeardown()
	cmd.Cancel = func() error { return td.cancel(cmd) }
	cmd.WaitDelay = ProcessWaitDelay
	return td
}

// killedExitCode is the exit code a terminated job or leader reports.
const killedExitCode = 1

// jobTeardown is the Windows ProcessTeardown: the Job Object holding one run's process tree.
// cmd.Cancel runs on os/exec's watchdog goroutine while Contain/Release run on the goroutine
// driving the command, so the handle and the assignment flag are mutex-guarded.
type jobTeardown struct {
	mu sync.Mutex
	// job is the Job Object holding the tree, or windows.InvalidHandle when one could not be
	// created (teardown then degrades to a leader-only kill) or after Release closed it.
	job windows.Handle
	// assigned reports that the process actually joined the job, which is what makes a job
	// termination reap the whole tree rather than nothing.
	assigned bool
}

// newJobTeardown returns a teardown holding a fresh tree job, or holding windows.InvalidHandle
// when the job cannot be created — every operation then degrades to the leader alone.
//
// It is shared by both ways a process reaches the job: NewProcessTeardown's os/exec launch, which
// joins by PID after Start (Contain), and StartPseudoConsole's suspended launch, which holds the
// process handle from CreateProcess and joins before the first instruction runs (containHandle).
func newJobTeardown() *jobTeardown {
	td := &jobTeardown{job: windows.InvalidHandle}
	if job, err := newTreeJob(); err == nil {
		td.job = job
	}
	return td
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

// Contain assigns the freshly started process to the job, so a cancel reaps its whole tree.
// Every failure is silent by design: the run continues, and cancel degrades to killing the
// leader (planTreeKill) rather than failing a command the user asked for.
func (t *jobTeardown) Contain(cmd *exec.Cmd) {
	if cmd.Process == nil {
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
	t.containHandle(h)
}

// containHandle assigns the process behind handle to the job — Contain's handle-keyed form, for a
// launcher that holds the handle from CreateProcess. The handle needs PROCESS_SET_QUOTA and
// PROCESS_TERMINATE access, and stays the caller's to close. Failure is silent, as in Contain.
func (t *jobTeardown) containHandle(handle windows.Handle) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.job == windows.InvalidHandle {
		return
	}
	if err := windows.AssignProcessToJobObject(t.job, handle); err != nil {
		return
	}
	t.assigned = true
}

// cancel is cmd.Cancel: it reaps the run when the context is cancelled or the timeout fires.
// It returns nil like the POSIX backend — the kill is the teardown, not an error the tool
// reports; Wait surfaces the outcome.
func (t *jobTeardown) cancel(cmd *exec.Cmd) error {
	t.terminate(cmd.Process != nil, func() { _ = cmd.Process.Kill() })
	return nil
}

// terminateHandle ends the tree of a process the job was given by handle (containHandle). An
// invalid handle means the leader has already exited and its handle been closed: the job is then
// the only thing left that can reach a descendant, so it is still terminated, and the leader
// kill — which must never go through a closed handle — becomes a no-op.
func (t *jobTeardown) terminateHandle(handle windows.Handle) {
	t.terminate(true, func() {
		if handle != windows.InvalidHandle {
			_ = windows.TerminateProcess(handle, killedExitCode)
		}
	})
}

// terminate is the one kill plan behind cancel and terminateHandle: the job when the process
// joined it, else killLeader, and nothing for a process that never started.
func (t *jobTeardown) terminate(started bool, killLeader func()) {
	t.mu.Lock()
	defer t.mu.Unlock()

	switch planTreeKill(started, t.assigned && t.job != windows.InvalidHandle) {
	case treeKillTree:
		// Terminating the job is explicit and immediate; the KILL_ON_JOB_CLOSE limit only
		// covers the path where nobody is left to make this call.
		if err := windows.TerminateJobObject(t.job, killedExitCode); err != nil {
			// The job may already be empty (the process exited between Done and here);
			// fall back to the leader, ignoring "process already finished".
			killLeader()
		}
	case treeKillLeader:
		killLeader()
	case treeKillNothing:
	}
}

// Reap terminates the job once a run that was never cancelled has been waited on, so a process
// the command detached — which the job holds anyway, breakaway being denied — does not outlive
// the one-shot call (ProcessTeardown.Reap). It is cancel's clean-exit twin and goes through the
// same plan: the leader is already gone by now, so the degraded rung's Kill is a no-op and only
// the job termination can still reach anything.
func (t *jobTeardown) Reap(cmd *exec.Cmd) {
	_ = t.cancel(cmd)
}

// Release drops the job handle when the run is over — after Wait and the reap on the normal
// path, and also on the confine-refusal and Start-failure paths, which never reach Wait but own
// the handle from the moment NewProcessTeardown created it. The KILL_ON_JOB_CLOSE limit is
// cleared first so that closing the handle is never itself a teardown: every path that started
// a process has already terminated the job explicitly (cancel, or Reap on a clean exit), and the
// paths that started none hold an empty job. The limit exists for the crash path — apogee dying
// mid-run with nobody left to make either call.
func (t *jobTeardown) Release() {
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
