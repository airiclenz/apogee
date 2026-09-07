package subprocess

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform"
)

// fakeConfiner is a caps-injected Confiner for the core's own tests. It records each Confine
// call; when unavailable it returns ErrConfinementUnavailable so the demote path is exercisable.
// Its no-op Confine leaves cmd as the real subprocess so a confined run still executes /bin/sh in
// these hermetic tests (the dev host has no landlock, contract §6).
type fakeConfiner struct {
	caps        domain.ConfinementCaps
	unavailable bool

	mu       sync.Mutex
	confined int
}

func (c *fakeConfiner) Capabilities() domain.ConfinementCaps { return c.caps }

func (c *fakeConfiner) Confine(_ context.Context, _ domain.ConfinementBox, _ *exec.Cmd) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.unavailable {
		return fmt.Errorf("%w: fake", domain.ErrConfinementUnavailable)
	}
	c.confined++
	return nil
}

// TestRunSubprocessNilConfinerFailsClosed pins the §2.2 posture on the one handle shape the
// confine guard used to wave through: a Confinement installed with no Confiner behind it. That
// is broken wiring, not permission to run free — it must surface as ErrConfinementUnavailable,
// which dispatch turns into the truthful demote to Approval, and the command must never run.
func TestRunSubprocessNilConfinerFailsClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell canary; the guard it pins is platform-independent")
	}
	t.Parallel()

	// The canary is a file the command would create: its absence is the proof that nothing
	// ran, which an error alone cannot give.
	canary := filepath.Join(t.TempDir(), "ran")
	ctx := domain.WithConfinement(context.Background(), domain.Confinement{
		Confiner: nil,
		Box:      domain.ConfinementBox{WorkspaceRoot: t.TempDir()},
	})

	_, err := RunSubprocess(ctx, SubprocessSpec{
		Argv: []string{"/bin/sh", "-c", fmt.Sprintf("touch %s", strconv.Quote(canary))},
	})
	if !errors.Is(err, domain.ErrConfinementUnavailable) {
		t.Fatalf("RunSubprocess err = %v, want ErrConfinementUnavailable (a handle with no Confiner must fail closed)", err)
	}
	if _, statErr := os.Stat(canary); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("stat %s = %v, want not-exist — the command must not have run unconfined", canary, statErr)
	}
}

// TestRunSubprocessReportsAWedgedDrain pins the second half of the same finding: when something
// the command left running still holds the output pipe, exec cuts the drain off at
// platform.ProcessWaitDelay and returns exec.ErrWaitDelay — which is not an *exec.ExitError, so
// the exit code falls through to the leader's own status. The leader exited 0, so the call used
// to render as a green tick with a silently truncated tail.
func TestRunSubprocessReportsAWedgedDrain(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell; the exit-code mapping it pins is platform-independent")
	}
	// platform.ProcessWaitDelay is a package var, so this test cannot run in parallel;
	// shrinking it is what keeps a five-second drain out of the suite.
	prev := platform.ProcessWaitDelay
	platform.ProcessWaitDelay = 250 * time.Millisecond
	t.Cleanup(func() { platform.ProcessWaitDelay = prev })

	// The sleep INHERITS the captured pipes and outlives the shell, so the output copy cannot
	// finish: Wait blocks until the delay expires. The sleep is short enough that a failed
	// reap cannot leave a process around for long.
	res, err := RunSubprocess(context.Background(), SubprocessSpec{Argv: []string{"/bin/sh", "-c", `sleep 10 &`}})
	if err != nil {
		t.Fatalf("RunSubprocess err = %v, want nil (a wedged drain is a result, not a Go error)", err)
	}
	if !res.DrainWedged {
		t.Fatalf("DrainWedged = false, want true — the pipe was still held when the delay expired (exit code %d)", res.ExitCode)
	}
	if res.ExitCode == 0 {
		t.Errorf("ExitCode = 0 for a run whose descendants held the pipe and were killed; the operator would read that as a clean success")
	}
}

// TestRunSubprocessRecordsConfined pins the confined flag on the result: true exactly when a
// Confinement handle wrapped the run, false on a plain unconfined run — the structural half
// the terminal's denial label keys on, so an unconfined EPERM can never be blamed on the box.
func TestRunSubprocessRecordsConfined(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell canary; the flag it pins is platform-independent")
	}
	t.Parallel()

	spec := SubprocessSpec{Argv: []string{"/bin/sh", "-c", "true"}}

	res, err := RunSubprocess(context.Background(), spec)
	if err != nil {
		t.Fatalf("unconfined RunSubprocess err = %v, want nil", err)
	}
	if res.Confined {
		t.Error("unconfined run reported Confined = true")
	}

	ctx := domain.WithConfinement(context.Background(), domain.Confinement{
		Confiner: &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: true}},
		Box:      domain.ConfinementBox{WorkspaceRoot: t.TempDir()},
	})
	res, err = RunSubprocess(ctx, spec)
	if err != nil {
		t.Fatalf("confined RunSubprocess err = %v, want nil", err)
	}
	if !res.Confined {
		t.Error("confined run reported Confined = false")
	}
}

// TestRunSubprocessDenialWatchKillsConfinedRun proves fix A of the 2026-08-22
// workspace-clobber incident at the funnel: a CONFINED run whose stream carries an
// OS-denial signature is killed by the live watch before its later, unguarded write line
// runs — the job `set -e` cannot do for an AND-OR list, since POSIX exempts every command
// of one but the last. The script mimics the incident: the "denial", intervening work
// (the sleep, which the incident's own commands stood in for — the kill is asynchronous),
// then the destructive write that must never land.
func TestRunSubprocessDenialWatchKillsConfinedRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell script; the watch keys on POSIX EPERM spellings only")
	}
	t.Parallel()

	dir := t.TempDir()
	clobber := filepath.Join(dir, "clobber.txt")
	script := `echo "mkdir: cannot create directory: Operation not permitted" >&2` + "\n" +
		"sleep 5\n" +
		"echo clobbered > " + clobber + "\n"
	ctx := domain.WithConfinement(context.Background(), domain.Confinement{
		Confiner: &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: true}},
		Box:      domain.ConfinementBox{WorkspaceRoot: dir},
	})

	res, err := RunSubprocess(ctx, SubprocessSpec{Argv: []string{"/bin/sh", "-c", script}})

	if err != nil {
		t.Fatalf("RunSubprocess err = %v, want nil (a denial kill is a result, not a Go error)", err)
	}
	if !res.DenialStopped {
		t.Error("DenialStopped = false, want the watch to have matched and killed the run")
	}
	if res.ExitCode == 0 {
		t.Error("ExitCode = 0, want non-zero for the killed run")
	}
	if res.TimedOut {
		t.Error("timedOut = true, want the denial kill reported as a kill, not a timeout")
	}
	if _, statErr := os.Stat(clobber); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("stat %q = %v, want not-exist — the kill must land before the unguarded write", clobber, statErr)
	}
}

// TestRunSubprocessDenialWatchNeverWatchesUnconfined pins the watch's structural gate: the
// identical denial-shaped output on an UNCONFINED run is not scanned, not killed, and not
// flagged — an unconfined EPERM can never be blamed on the box (the same gate the confined
// flag itself pins above).
func TestRunSubprocessDenialWatchNeverWatchesUnconfined(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell script; the gate it pins is platform-independent")
	}
	t.Parallel()

	script := `echo "mkdir: cannot create directory: Operation not permitted" >&2`

	res, err := RunSubprocess(context.Background(), SubprocessSpec{Argv: []string{"/bin/sh", "-c", script}})

	if err != nil {
		t.Fatalf("RunSubprocess err = %v, want nil", err)
	}
	if res.DenialStopped {
		t.Error("DenialStopped = true on an unconfined run")
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0 — the run must complete untouched", res.ExitCode)
	}
}

// oversizeStdoutScript is a POSIX line printing a deterministic payload past the output cap, so
// the two stdout paths — the capped one and the streaming one — can be measured against the SAME
// bytes. yes/head is used rather than a Go writer because the point is what a real child process
// pushes down a pipe.
const oversizeStdoutScript = `yes 0123456789abcdefghijklmnopqrstuvwxyz | head -c 400000`

// oversizeStdoutBytes is what that script prints: 400000 bytes, comfortably past the cap.
const oversizeStdoutBytes = 400000

// TestRunSubprocessCapsAnOversizeStdout pins the ceiling on the capped path: a child printing far
// more than MaxSubprocessOutputBytes keeps exactly the cap's worth and says how much it dropped,
// so a runaway command can neither exhaust memory nor flood a context window.
func TestRunSubprocessCapsAnOversizeStdout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell script; the cap it pins is platform-independent")
	}
	t.Parallel()

	res, err := RunSubprocess(context.Background(), SubprocessSpec{
		Argv: []string{"/bin/sh", "-c", oversizeStdoutScript},
	})
	if err != nil {
		t.Fatalf("RunSubprocess err = %v, want nil", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", res.ExitCode)
	}
	marker := fmt.Sprintf("… [output truncated: %d more bytes]", oversizeStdoutBytes-MaxSubprocessOutputBytes)
	if !strings.HasSuffix(res.CombinedOutput, marker) {
		t.Errorf("CombinedOutput does not end with %q — the model would not know the tail is missing", marker)
	}
	if kept := len(res.CombinedOutput) - len("\n") - len(marker); kept != MaxSubprocessOutputBytes {
		t.Errorf("kept %d bytes before the marker, want %d (MaxSubprocessOutputBytes)", kept, MaxSubprocessOutputBytes)
	}
}

// TestRunSubprocessToStreamsStdoutUncapped pins the streaming variant against the capped one: the
// SAME oversize payload reaches the caller's writer whole and byte-identical, because a caller
// splicing a child's stdout into a file has no business receiving a truncation marker in the
// middle of it. The diagnostics stay capped and stay out of the payload.
func TestRunSubprocessToStreamsStdoutUncapped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell script; the streaming path it pins is platform-independent")
	}
	t.Parallel()

	var got bytes.Buffer
	res, err := RunSubprocessTo(context.Background(), SubprocessSpec{
		Argv: []string{"/bin/sh", "-c", oversizeStdoutScript + " ; echo diagnostic >&2"},
	}, &got)
	if err != nil {
		t.Fatalf("RunSubprocessTo err = %v, want nil", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0 (output %q)", res.ExitCode, res.CombinedOutput)
	}
	if got.Len() != oversizeStdoutBytes {
		t.Errorf("streamed %d bytes, want %d — the payload must not be capped", got.Len(), oversizeStdoutBytes)
	}
	want := strings.Repeat("0123456789abcdefghijklmnopqrstuvwxyz\n", 1+oversizeStdoutBytes/37)[:oversizeStdoutBytes]
	if got.String() != want {
		t.Error("streamed bytes differ from what the child printed")
	}
	if strings.TrimSpace(res.CombinedOutput) != "diagnostic" {
		t.Errorf("CombinedOutput = %q, want the diagnostics alone", res.CombinedOutput)
	}
	if res.Stdout != "" {
		t.Errorf("Stdout = %q, want empty — the payload left through the writer", res.Stdout)
	}
}

// TestRunSubprocessRefusesAnEmptyArgv pins the one argument check the core makes for itself: a
// spec with no program is a caller bug, refused before a process is built rather than handed to
// exec as an empty name.
func TestRunSubprocessRefusesAnEmptyArgv(t *testing.T) {
	t.Parallel()

	if _, err := RunSubprocess(context.Background(), SubprocessSpec{}); err == nil {
		t.Fatal("RunSubprocess err = nil for an empty argv, want a refusal")
	}
}
