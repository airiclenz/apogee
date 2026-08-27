//go:build !windows

package tuitest

import (
	"os"
	"strings"
	"testing"
	"time"
)

// The PTY driver's own claims are the ones that cannot be made in process: that the child gets a
// REAL terminal, with real colours, real echo, a real window size and a real death. They are
// asserted against /bin/sh rather than against apogee, because what is under test here is the
// driver — a shell is a program whose behaviour under a terminal is not in doubt, so a failure is
// unambiguously the driver's.

// ptySettle is how long these tests let a shell go quiet before reading a frame. It is longer than
// the e2e settle because the thing being waited for is a process start, not a repaint.
const ptySettle = 200 * time.Millisecond

// shellEnv is the minimum a shell needs: a PATH to find its own utilities. TERM and COLORTERM are
// the driver's to add, which is half of what TestPTYDriverIsATerminal checks.
func shellEnv() []string { return []string{"PATH=" + os.Getenv("PATH")} }

// TestPTYDriverIsATerminal drives one shell through the whole surface: what it painted (in colour),
// what it echoed back, what size it believes the terminal is, and what happens when it is killed.
// One shell rather than four is deliberate — a pty per assertion is four process starts, and the
// claims are about one conversation. The shell loops at the end rather than exiting, because a
// child that has already gone cannot be killed and the last claim is about killing one.
func TestPTYDriverIsATerminal(t *testing.T) {
	CheckLeaks(t)

	// A red word, a plain one, then a line read back and echoed: the two directions of a terminal
	// in one script.
	drv := NewPTYDriver(t, "/bin/sh",
		[]string{"-c", `printf '\033[31mred\033[0m ok\n'; read x; stty size; echo "typed:$x"
			while :; do sleep 0.05; done`},
		shellEnv(), Size{W: 40, H: 10})

	// The colour is the emulator's, not the string's: what a terminal DID with the SGR.
	drv.WaitText("red ok")
	drv.WaitQuiet(ptySettle)
	red, found := findRun(drv.Frame(), "red")
	if !found {
		t.Fatalf("no run of \"red\" on the screen:\n%s", drv.Frame())
	}
	if red.FG == nil {
		t.Errorf("the \"red\" run has the terminal's default foreground; want the SGR 31 colour")
	}
	if plain, ok := findRun(drv.Frame(), "ok"); ok && plain.Style.Equal(red.Style) {
		t.Errorf("\"ok\" is painted like \"red\" (%s); the reset was not honoured", red.Style)
	}

	// Typing reaches the child, and the terminal echoes it: `read` is canonical-mode input, so
	// what comes back on screen is the line discipline's doing, not the shell's.
	drv.Type("hi\n")
	drv.WaitText("typed:hi")

	// The size the driver asked for is the size the child sees — `stty size` reports rows then
	// columns, straight out of the kernel's window for this pty.
	drv.WaitText("10 40")

	// Killing reaps: the exit status arrives, and it is the one a shell would report for SIGKILL.
	drv.Kill()
	select {
	case code := <-drv.Exited():
		if code != 137 {
			t.Errorf("the killed child exited %d; want 137 (128+SIGKILL)", code)
		}
	case <-time.After(DefaultTimeout):
		t.Fatal("the killed child was never reaped")
	}
}

// TestPTYDriverResizeReachesTheChild pins the SIGWINCH half on its own: a program that is ALREADY
// running when the terminal changes size is told about it. The shell traps the signal and prints
// the size it reads afterwards, so nothing here is inferred from the emulator.
func TestPTYDriverResizeReachesTheChild(t *testing.T) {
	CheckLeaks(t)

	drv := NewPTYDriver(t, "/bin/sh",
		[]string{"-c", `trap 'stty size' WINCH; echo ready; while :; do sleep 0.05; done`},
		shellEnv(), Size{W: 40, H: 10})

	drv.WaitText("ready")
	drv.Resize(72, 24)
	drv.WaitText("24 72")
}

// TestPTYDriverRestoresNothingItselfMeasures is the tty-state read-back: the driver reports the
// line discipline of the pty it owns, and it reports it truthfully in both states. A program that
// turns echo off is visible as such while it runs, and the terminal is back to cooked mode once it
// has put it back — which is the assertion the e2e "no `stty sane` needed" claim rests on.
func TestPTYDriverRestoresNothingItselfMeasures(t *testing.T) {
	CheckLeaks(t)

	drv := NewPTYDriver(t, "/bin/sh",
		[]string{"-c", `stty -echo -icanon; echo raw; read x; stty sane; echo cooked`},
		shellEnv(), Size{W: 40, H: 10})

	drv.WaitText("raw")
	if echo, canonical := drv.TTYState(); echo || canonical {
		t.Errorf("the pty reports echo=%v canonical=%v while the child holds it raw", echo, canonical)
	}

	// With -icanon the line discipline hands bytes straight through, but `read` still ends its
	// line on a newline: what makes the shell continue is the \n, not the keystroke before it.
	drv.Type("x\n")
	drv.WaitText("cooked")
	if code := <-drv.Exited(); code != 0 {
		t.Errorf("the shell exited %d; want 0", code)
	}
	if echo, canonical := drv.TTYState(); !echo || !canonical {
		t.Errorf("the pty reports echo=%v canonical=%v after the child restored it; want both true",
			echo, canonical)
	}
}

// TestReplayTraceRebuildsTheScreen pins the other half of the black-box measure: a --tui-trace file
// replays into the picture it recorded, and into the counters a flicker claim is made against.
func TestReplayTraceRebuildsTheScreen(t *testing.T) {
	path := t.TempDir() + "/trace.txt"
	// The shape internal/tui's tracedOutput writes: one Go-quoted write per line.
	if err := os.WriteFile(path,
		[]byte("\"\\x1b[2Jhello\"\n\"\\r\\n\"\n\"world\"\n"), 0o600); err != nil {
		t.Fatalf("write the trace fixture: %v", err)
	}

	screen := ReplayTrace(t, path, Size{W: 20, H: 4})

	if got := screen.Snapshot().Row(0); got != "hello" {
		t.Errorf("row 0 of the replayed trace = %q, want %q", got, "hello")
	}
	if got := screen.Snapshot().Row(1); got != "world" {
		t.Errorf("row 1 of the replayed trace = %q, want %q", got, "world")
	}
	if got := screen.BytesWritten(); got != 16 {
		t.Errorf("the replay counted %d bytes, want 16", got)
	}
	if got := screen.FullRepaints(); got != 1 {
		t.Errorf("the replay counted %d full repaints, want 1 (the \\x1b[2J)", got)
	}
}

// findRun returns the first style run on any row whose text contains want.
func findRun(f Frame, want string) (Run, bool) {
	for y := range f.Height() {
		for _, run := range f.StyleRuns(y) {
			if strings.Contains(run.Text, want) {
				return run, true
			}
		}
	}
	return Run{}, false
}
