package main

// T-14 of the v0.17.1 release checklist — "Console ownership, and restore closes the outgoing
// Consoles" — as a test, for the half a human had to run `pgrep` for.
//
// It was manual because what is being proved is an OS fact OUTSIDE the process the unit tests run
// in: that no shell is left running on the host after a delegation ends, after a session switch, or
// after the program quits. That is exactly what the black-box driver can see and the in-process one
// cannot — a PTY run is a real child with a real pid, so its Consoles are real grandchildren, and
// `ps` is the same instrument the checklist reaches for.
//
// The engine-level twin (owner keys distinct, cross-run lookup refused, restore closes and resets)
// is internal/console/registry_test.go, internal/agent/console_test.go and
// internal/agent/restoresession_test.go, already green — this adds the host, not a second copy of
// them.

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// consoleToolsOn is the checklist's precondition as config: the Console family ships OFF (ADR 0057)
// and a `tools.enabled:` list is what lifts it. There is no flag and no environment variable, and
// the registry is built once at startup, so it has to be in the file before the binary launches.
const consoleToolsOn = "tools:\n  enabled: [console_open, console_send, console_read, console_close]\n"

// The two long-lived programs of the checklist, kept as its own numbers: one the parent run opens
// and one a delegation opens. They are distinct so a single `ps` sweep tells whose Console is whose,
// and they sleep long enough that a survivor is a survivor rather than a race.
const (
	parentSleep = "sleep 987"
	childSleep  = "sleep 654"
)

// TestE2EConsolesDieWithTheirOwner is T-14 steps 1–11 against the shipped binary: a Console belongs
// to the run that opened it, a delegation's Console dies with the delegation, a `/sessions` restore
// closes the outgoing conversation's Consoles, a REFUSED switch closes nothing, and quitting reaps
// whatever is left.
//
// Every process claim is made with `ps` against the pid of the binary the driver spawned, so a
// `sleep` some other test or some other developer left on the machine can neither satisfy nor break
// it. It skips on Windows, where console_open answers "console is not supported on Windows yet" and
// the item is BLOCKED for a human too (the PTY driver skips there anyway).
func TestE2EConsolesDieWithTheirOwner(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "console"))
	sess := launchPTYConfigured(t, stub, consoleToolsOn)

	// The checklist's precondition: at least one OTHER saved session to restore into. It is made
	// first, by a run that says something and quits — a session is a record on disk, and a record is
	// written by a conversation that happened.
	drv := sess.drv
	submit(drv, "Say hello.")
	drv.WaitText("Hello from the console run.")
	drv.WaitQuiet(settled)
	if code := drv.Quit(); code != 0 {
		t.Fatalf("the first run exited %d; want a clean quit", code)
	}

	// Step 1 — the run this test is about, on the same home, so the session above is there to be
	// restored into.
	drv = sess.Relaunch()
	drv.WaitText("Send a message")

	// Step 2 — the parent's own Console. "Always allow this session" is pressed rather than "Allow"
	// so the delegation's console_open two steps down is not a second pane to find: what is under
	// test is process ownership, not the approval pane (T-10 owns that).
	submit(drv, "Open a console running "+parentSleep+".")
	alwaysAllow(drv)
	drv.WaitText("The console is open.")
	drv.WaitQuiet(settled)

	// Step 3 — one real process, and it is a descendant of the apogee the driver spawned.
	if pids := descendants(t, drv.Pid(), parentSleep); len(pids) != 1 {
		t.Fatalf("%q runs as %v under apogee (pid %d); want exactly one", parentSleep, pids, drv.Pid())
	}

	// Step 4 — the delegation opens a Console of its own and reaches for the parent's. Its refusal is
	// read off the SESSION RECORD rather than off the child's report: the upstream is scripted, so
	// what the child SAYS proves nothing, while the tool result it was handed is the engine's own
	// answer.
	submit(drv, "Delegate the console check.")
	// The child asks for itself. A parent's allow-for-session does not cover a delegation's calls —
	// the grain is the run that made them — so the pane comes up a second time, and answering it is
	// part of driving the step rather than a claim being made.
	alwaysAllow(drv)
	drv.WaitText("The delegation is done.")
	drv.WaitQuiet(settled)

	record := sessionRecordText(t, sess.Home())
	if !strings.Contains(record, "no console 1 (open consoles: 2)") {
		t.Error("the delegation's console_read on the parent's Console was not refused with " +
			"`no console 1 (open consoles: 2)`; a child must learn nothing about a Console it does not own")
	}

	// Steps 5 and 6 — the child's Console died with the delegation, and the parent's did not.
	awaitGone(t, drv, drv.Pid(), childSleep, "the delegation's Console to be reaped when it ended")
	if pids := descendants(t, drv.Pid(), parentSleep); len(pids) != 1 {
		t.Fatalf("%q runs as %v after the delegation; the child's exit must not touch the parent's Console",
			parentSleep, pids)
	}

	// Steps 7 and 8 — the restore. The outgoing conversation's Console is closed at the switch: no
	// shell may survive a session the human has left.
	restoreOtherSession(t, drv)
	awaitGone(t, drv, drv.Pid(), parentSleep, "the outgoing conversation's Console to be closed by the restore")

	// Step 9 — and the restored conversation inherits no ids: console 1 is not "someone else's", it
	// is nothing at all, and the refusal lists no Consoles because there are none.
	submit(drv, "Read console 1.")
	drv.WaitText("That console is not open here.")
	drv.WaitQuiet(settled)
	if after := sessionRecordText(t, sess.Home()); !strings.Contains(after, "no console 1 (open consoles: none)") {
		t.Error("the restored conversation's console_read was not refused with " +
			"`no console 1 (open consoles: none)`; a restore inherits no Console ids")
	}

	// Step 10, the edge — a REFUSED switch reaps nothing. The refusal lands one layer earlier than
	// the checklist's hand-run does: `/sessions` is an idle-only command (internal/tui/command.go),
	// so mid-Exchange the TUI refuses the COMMAND and RestoreSession is never called. Either way the
	// claim under test is the same one, and it is the one the "Fails if" names: a switch that did not
	// happen must leave the host exactly as it was.
	submit(drv, "Open a console running "+parentSleep+".")
	allowIfAsked(drv)
	drv.WaitText("The console is open.")
	drv.WaitQuiet(settled)
	if pids := descendants(t, drv.Pid(), parentSleep); len(pids) != 1 {
		t.Fatalf("%q runs as %v after the second open; want exactly one", parentSleep, pids)
	}

	submit(drv, "Think about this for a while.")
	drv.WaitText("esc")
	submit(drv, "/sessions")
	// No quiet check here: the Turn is still hanging, so the spinner is still animating and a screen
	// that never goes quiet is the correct state to be asserting in.
	drv.WaitText("commands run at idle — not queued")
	if pids := descendants(t, drv.Pid(), parentSleep); len(pids) != 1 {
		t.Errorf("%q runs as %v after a REFUSED switch; a refusal must reap nothing", parentSleep, pids)
	}

	// Step 11 — quitting closes every open Console. The pid is read before the quit, because after it
	// there is no process to ask about.
	pid := drv.Pid()
	drv.Press(tuitest.Esc) // stop the hanging Turn so the quit is a quit and not a cancel-then-quit
	drv.WaitQuiet(settled)
	drv.Quit()
	awaitGone(t, drv, pid, parentSleep, "every Console to be reaped when apogee quits")
}

// ----------------------------------------------------------------------------
// Driving the panes this item passes through
// ----------------------------------------------------------------------------

// alwaysAllow answers the approval pane with "Always allow this session" (the `s` row,
// internal/tui/approval.go), so every later call of the same tool in this session runs unasked.
func alwaysAllow(drv *tuitest.PTYDriver) {
	drv.WaitText("Always allow this session")
	drv.WaitQuiet(settled)
	drv.Type("s")
}

// allowIfAsked answers an approval pane if one is up and does nothing if none is. A
// session-scoped allowance does not survive a session switch, and whether it should is not this
// item's claim — so the step that follows a restore asks the question of the screen rather than
// assuming either answer.
func allowIfAsked(drv *tuitest.PTYDriver) {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, ok := drv.Frame().Find("Always allow this session"); ok {
			drv.WaitQuiet(settled)
			drv.Type("s")
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// restoreOtherSession opens the `/sessions` browser and resumes the row below the top one — the
// session made before this run, since the browser lists the newest first and the newest is the
// conversation the run is already in.
func restoreOtherSession(t *testing.T, drv *tuitest.PTYDriver) {
	t.Helper()

	submit(drv, "/sessions")
	drv.WaitText("⏎ resume")
	drv.WaitQuiet(settled)
	drv.Press(tuitest.Down)
	drv.WaitQuiet(settled)
	drv.Press(tuitest.Enter)
	drv.WaitText("resumed:")
	drv.WaitQuiet(settled)
}

// ----------------------------------------------------------------------------
// Reading the host
// ----------------------------------------------------------------------------

// procRow is one line of the process table: a pid, its parent, and the command line it is running.
type procRow struct {
	pid, ppid int
	args      string
}

// processTable is every process on this machine, as `ps` reports it. The flags are the POSIX ones
// both Linux and macOS answer, with empty headers so the output is data and not a table.
func processTable(t *testing.T) []procRow {
	t.Helper()

	out, err := exec.Command("ps", "-eo", "pid=,ppid=,args=").Output()
	if err != nil {
		t.Fatalf("read the process table: %v", err)
	}
	var rows []procRow
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		rows = append(rows, procRow{pid: pid, ppid: ppid, args: strings.Join(fields[2:], " ")})
	}
	return rows
}

// descendants are the pids RUNNING want — an exact command line — that descend from root. It is the
// program itself and not the shell that started it: console_open runs its command line through a
// POSIX shell, so `sleep 987` is two processes on a host whose sh forks and one on a host whose sh
// execs, and a count that included the shell would be answering about the platform.
//
// The root check is what makes the claim about THIS run rather than about the machine: a `sleep 987`
// a developer left in another terminal descends from something else.
func descendants(t *testing.T, root int, want string) []int {
	return descendantsWhere(t, root, func(args string) bool { return args == want })
}

// lingering is descendants' wider sweep: anything whose command line so much as MENTIONS want,
// shells included. "Nothing survived" is the one claim that must not be narrowed — a Console whose
// program was reaped while its shell stayed is still a process left on the host — so the reaping
// assertions ask this and the counting ones ask [descendants].
func lingering(t *testing.T, root int, want string) []int {
	return descendantsWhere(t, root, func(args string) bool { return strings.Contains(args, want) })
}

// descendantsWhere is the ancestry walk both use: every process matching pick whose parent chain
// reaches root.
func descendantsWhere(t *testing.T, root int, pick func(args string) bool) []int {
	t.Helper()

	rows := processTable(t)
	parent := make(map[int]int, len(rows))
	for _, r := range rows {
		parent[r.pid] = r.ppid
	}
	var found []int
	for _, r := range rows {
		if !pick(r.args) {
			continue
		}
		// A bounded walk: an ancestry chain reaches init in a handful of steps, and a bound is what
		// keeps a cycle in a mangled table from hanging the suite.
		for pid, steps := r.pid, 0; pid > 1 && steps < 16; steps++ {
			if pid == root {
				found = append(found, r.pid)
				break
			}
			next, ok := parent[pid]
			if !ok {
				break
			}
			pid = next
		}
	}
	return found
}

// awaitGone blocks until nothing running want descends from root any more. Reaping is asynchronous —
// the engine closes a Console and the kernel gets to it when it gets to it — so the claim is "gone
// within the wait", never "gone the instant the frame said so".
func awaitGone(t *testing.T, drv *tuitest.PTYDriver, root int, want, what string) {
	t.Helper()

	drv.WaitFor(func() bool { return len(lingering(t, root, want)) == 0 }, tuitest.Awaiting(what))
}
