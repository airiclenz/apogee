//go:build !windows

package tuitest

import (
	"os"
	"os/exec"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/termios"
	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// maxTerminalSize is the largest terminal a driver will ask a pty for. A pty's window is a pair of
// uint16s, and a test that asks for more than this has a bug in its arithmetic rather than an
// unusual terminal — so the ceiling is a failed test, not a truncated conversion.
const maxTerminalSize = 4096

// PTYDriver is the black-box half of the kit: the SHIPPED binary, started under a real
// pseudo-terminal, typed into through the pty master and read back through the same [Screen] the
// in-process [Driver] uses. Nothing about the program under test is arranged for the test — no
// launcher seam, no injected options, no in-process anything. It is the binary, in a terminal.
//
// That is the whole reason it exists beside the cheaper driver: a claim about what a REAL terminal
// does cannot be observed from inside the process that talks to it. Colour and wide runes go
// through a genuine `TERM=xterm-256color` negotiation; a resize is a genuine `TIOCSWINSZ` and the
// SIGWINCH the kernel raises for it; a kill is `SIGKILL` on a real pid; and the state the terminal
// is left in after the program exits — the alternate screen released, the cursor shown, echo and
// canonical mode restored — is a property OF the pty, readable here and nowhere else.
//
// The emulator is the terminal in both directions, as in the in-process driver: bytes the child
// writes are fed to the [Screen], and the answers the emulator produces (DA1, DECRQM, CPR) are
// written back into the master, which the child reads as input. A driver that only typed keys
// would hang the renderer on its first query.
type PTYDriver struct {
	t      testing.TB
	screen *Screen
	size   Size

	cmd *exec.Cmd
	// master is the pty master: the terminal's side of the pair. Everything written to it is
	// input to the child; everything read from it is what the child painted.
	master *os.File

	// writeMu serialises the two writers of the master — typed keys and the emulator's own
	// answers — so an answer cannot be spliced into the middle of a key's byte sequence. lastEsc
	// is the escape-timeout rule (see [escapeGap]), guarded by the same lock.
	writeMu sync.Mutex
	lastEsc time.Time

	// raw is every byte the child has written to the terminal, kept whole. The frames come from
	// the emulator; this is for the claims that are about the SEQUENCES themselves — the
	// alternate-screen release, the cursor-show, the last SGR — which no frame can show, because
	// a frame is what is left after the terminal has consumed them.
	rawMu sync.Mutex
	raw   []byte

	// reaped closes when the child has been waited for; code is its exit status, valid from then
	// on. exited is the public one-shot ([PTYDriver.Exited]).
	reaped chan struct{}
	code   int
	exited chan int

	// The two pumps' completion signals, joined by Close so a torn-down driver leaves no
	// goroutine behind for [CheckLeaks] to find.
	pumped   chan struct{}
	answered chan struct{}

	closing sync.Once
}

// NewPTYDriver starts bin under a size.W×size.H pseudo-terminal and returns the driver for it. env
// is the child's whole environment — nothing is inherited, so a test says what the binary sees —
// with TERM and COLORTERM appended last, because what the terminal claims to be is the driver's
// business rather than the caller's.
//
// The child is its own session leader with the pty as its controlling terminal, which is what makes
// it a foreground process in the terminal sense: job-control signals, SIGWINCH and the tty's own
// state all reach it the way they reach a program a human started. Teardown is registered here: a
// child still running when the test ends is killed and reaped.
func NewPTYDriver(t testing.TB, bin string, args []string, env []string, size Size) *PTYDriver {
	t.Helper()

	if bin == "" {
		t.Fatal("tuitest: NewPTYDriver needs the path of a built binary")
	}
	if size.W <= 0 || size.H <= 0 || size.W > maxTerminalSize || size.H > maxTerminalSize {
		t.Fatalf("tuitest: NewPTYDriver needs a terminal size within 1..%d, got %dx%d",
			maxTerminalSize, size.W, size.H)
	}

	cmd := exec.Command(bin, args...)
	// exec keeps the LAST definition of a duplicated variable, so appending is how these two win
	// over whatever the caller passed.
	cmd.Env = append(append([]string{}, env...), "TERM=xterm-256color", "COLORTERM=truecolor")

	master, err := pty.StartWithAttrs(cmd,
		&pty.Winsize{Rows: uint16(size.H), Cols: uint16(size.W)},
		&syscall.SysProcAttr{Setsid: true, Setctty: true})
	if err != nil {
		t.Fatalf("tuitest: start %s under a pty: %v", bin, err)
	}

	d := &PTYDriver{
		t:        t,
		screen:   NewScreen(size.W, size.H),
		size:     size,
		cmd:      cmd,
		master:   master,
		reaped:   make(chan struct{}),
		exited:   make(chan int, 1),
		pumped:   make(chan struct{}),
		answered: make(chan struct{}),
	}
	go d.pumpOutput()
	go d.pumpAnswers()
	go d.reap()
	t.Cleanup(d.Close)
	return d
}

// Frame is what the terminal shows right now.
func (d *PTYDriver) Frame() Frame { return d.screen.Snapshot() }

// Screen is the emulator behind the frames, for the assertions that need its counters or its
// settle rule.
func (d *PTYDriver) Screen() *Screen { return d.screen }

// Pid is the child's process id — a real one, which is what makes a real signal possible.
func (d *PTYDriver) Pid() int {
	if d.cmd.Process == nil {
		return 0
	}
	return d.cmd.Process.Pid
}

// Exited delivers the child's exit status once it has been reaped. It carries the status exactly
// once and is then closed, so a second receive yields 0 — a test that needs the status twice takes
// it from [PTYDriver.Quit] or keeps what it received.
func (d *PTYDriver) Exited() <-chan int { return d.exited }

// Bytes is every byte the child has written to the terminal so far. Frames are the picture; this is
// the wire, and the difference matters for the claims about what a terminal was LEFT in — the
// alternate-screen release and the final SGR reset are not on any screen, they are what the screen
// stopped being.
func (d *PTYDriver) Bytes() []byte {
	d.rawMu.Lock()
	defer d.rawMu.Unlock()
	return append([]byte(nil), d.raw...)
}

// TTYState reports the pty's line discipline as it stands: whether the terminal echoes what is
// typed, and whether input is canonical (line-at-a-time). Both are true for a terminal a shell
// would be happy to be handed back, and a full-screen program turns both off for the length of its
// run — so the pair being true again after the child is gone is the mechanical form of "no
// `stty sane` needed".
//
// It is read through the MASTER fd because [pty.StartWithAttrs] closes the slave once the child
// holds it. That is the same termios either way: a pty pair has one line discipline, and a mode
// ioctl on the master is answered for the pair (on Linux the kernel redirects it to the slave
// explicitly; the BSD masters pass it through).
func (d *PTYDriver) TTYState() (echo, canonical bool) {
	d.t.Helper()

	state, err := termios.GetTermios(int(d.master.Fd()))
	if err != nil {
		d.t.Fatalf("tuitest: read the pty's terminal attributes: %v", err)
	}
	return state.Lflag&unix.ECHO != 0, state.Lflag&unix.ICANON != 0
}

// Type sends text as a run of keystrokes, exactly as a human typing it would.
func (d *PTYDriver) Type(text string) {
	d.t.Helper()
	d.send([]byte(text))
}

// Press sends one key's byte sequence.
func (d *PTYDriver) Press(key Key) {
	d.t.Helper()
	d.send([]byte(key))
}

// Resize changes the terminal's size for real: the pty's window is set — which is the ioctl a
// terminal emulator performs, and the kernel raises SIGWINCH for it — and the signal is sent
// explicitly as well, because a child that was reparented out of the pty's foreground group would
// otherwise miss it. The emulator is reflowed to match, and the call waits for the repaint that
// answers it, for the same reason the in-process driver does: a frame read before the program has
// painted is the old frame at the new size, and it passes a settle check.
func (d *PTYDriver) Resize(w, h int) {
	d.t.Helper()

	if w <= 0 || h <= 0 || w > maxTerminalSize || h > maxTerminalSize {
		d.t.Fatalf("tuitest: Resize needs a terminal size within 1..%d, got %dx%d",
			maxTerminalSize, w, h)
	}
	before := d.screen.BytesWritten()
	d.size = Size{W: w, H: h}
	d.screen.Resize(w, h)
	if err := pty.Setsize(d.master, &pty.Winsize{Rows: uint16(h), Cols: uint16(w)}); err != nil {
		d.t.Fatalf("tuitest: resize the pty to %dx%d: %v", w, h, err)
	}
	if proc := d.cmd.Process; proc != nil {
		_ = proc.Signal(syscall.SIGWINCH)
	}
	WaitFor(d.t, func() bool { return d.screen.BytesWritten() > before }, On(d.screen),
		Awaiting("the binary to repaint after the SIGWINCH"))
}

// WaitFor polls cond on this driver's screen.
func (d *PTYDriver) WaitFor(cond func() bool, opts ...Option) {
	d.t.Helper()
	WaitFor(d.t, cond, append([]Option{On(d.screen)}, opts...)...)
}

// WaitText waits for text to appear on screen.
func (d *PTYDriver) WaitText(text string) {
	d.t.Helper()
	WaitText(d.t, d.screen, text)
}

// WaitGone waits for text to leave the screen.
func (d *PTYDriver) WaitGone(text string) {
	d.t.Helper()
	WaitGone(d.t, d.screen, text)
}

// WaitQuiet waits for the screen to stop changing for quiet — the settle rule, unchanged from the
// in-process driver, because it is a property of the picture and not of how the bytes arrived.
func (d *PTYDriver) WaitQuiet(quiet time.Duration) {
	d.t.Helper()
	WaitQuiet(d.t, d.screen, quiet)
}

// Quit ends the run the way a human does — Ctrl+C twice, inside apogee's own confirm window — and
// returns the child's exit status once it has gone. A child that does not go is a failure with the
// last frame printed: "the quit hung" without the screen is the least useful failure the kit could
// produce.
func (d *PTYDriver) Quit() int {
	d.t.Helper()

	d.Press(CtrlC)
	d.Press(CtrlC)
	return d.awaitExit("the binary to quit after Ctrl+C twice")
}

// Kill ends the run the way a crash does: SIGKILL to the child's whole session, no teardown, no
// final frame, nothing restored. It is the black-box half of the "reopen after an abrupt end"
// claim (T-03) — the in-process driver cancels a context for the other half — and it is the only
// way to prove what apogee did NOT get to do on the way out.
func (d *PTYDriver) Kill() {
	d.t.Helper()

	proc := d.cmd.Process
	if proc == nil {
		return
	}
	select {
	case <-d.reaped:
		return
	default:
	}
	// The child is a session leader (Setsid), so its pid is also its group id: the negative pid
	// takes anything it spawned with it. A group that is already gone is not an error here.
	if err := syscall.Kill(-proc.Pid, syscall.SIGKILL); err != nil {
		_ = proc.Kill()
	}
	d.awaitExit("the binary to die after SIGKILL")
}

// Close tears the driver down and joins everything it started: the child is killed if it is still
// running, the master is closed (which ends the output pump), and the screen is closed (which ends
// the answer pump). It is idempotent and registered as a cleanup, so a test calls it only to end a
// run early.
func (d *PTYDriver) Close() {
	d.closing.Do(func() {
		d.Kill()
		// The master closes first: the output pump is blocked reading it, and nothing else can
		// wake it. Its error on a closed file is the end of the pump, not a failure.
		_ = d.master.Close()
		<-d.pumped
		// Then the screen, whose own pump feeds the answer queue the answer pump reads: closing
		// it ends that queue with io.EOF, and the answer pump returns.
		d.screen.Close()
		<-d.answered
	})
}

// awaitExit blocks until the child has been reaped and returns its status, failing the test with
// the last frame if it never does.
func (d *PTYDriver) awaitExit(what string) int {
	d.t.Helper()

	select {
	case <-d.reaped:
		return d.code
	case <-time.After(DefaultTimeout):
		waiter{screen: d.screen, what: what}.fail(d.t)
		return 0 // unreachable: fail is a t.Fatalf
	}
}

// send writes into the pty master — the child's input — under the lock the answer pump also holds.
// A write that fails because the child is already gone is not a failure of the test that sent it:
// a driver types at a program that may have quit on the previous key, and the assertion that
// matters is the one about the screen, not about who noticed the pipe first.
func (d *PTYDriver) send(p []byte) {
	d.t.Helper()

	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if wait := escapeGap - time.Since(d.lastEsc); !d.lastEsc.IsZero() && wait > 0 {
		time.Sleep(wait)
	}
	d.lastEsc = time.Time{}
	_, _ = d.master.Write(p)
	if string(p) == string(Esc) {
		d.lastEsc = time.Now()
	}
}

// pumpOutput feeds everything the child paints to the emulator, and keeps the raw copy. It ends
// when the master is closed or the child's last descriptor on the slave goes — on Linux that read
// fails with EIO, which is this pty's way of spelling EOF.
func (d *PTYDriver) pumpOutput() {
	defer close(d.pumped)

	buf := make([]byte, 4096)
	for {
		n, err := d.master.Read(buf)
		if n > 0 {
			d.rawMu.Lock()
			d.raw = append(d.raw, buf[:n]...)
			d.rawMu.Unlock()
			if _, werr := d.screen.Write(buf[:n]); werr != nil {
				return // the screen is closed; the test is over
			}
		}
		if err != nil {
			return
		}
	}
}

// pumpAnswers hands the emulator's replies back to the child as input — the other half of being
// the terminal. Writes go through the same lock a keystroke does.
func (d *PTYDriver) pumpAnswers() {
	defer close(d.answered)

	answers := d.screen.Answers()
	buf := make([]byte, 256)
	for {
		n, err := answers.Read(buf)
		if n > 0 {
			d.writeMu.Lock()
			_, werr := d.master.Write(buf[:n])
			d.writeMu.Unlock()
			if werr != nil {
				return // the master is closed; nobody is listening
			}
		}
		if err != nil {
			return
		}
	}
}

// reap waits for the child and publishes its exit status. A signalled child (SIGKILL) has no exit
// code of its own, so what is recorded is the conventional 128+signal a shell would report.
func (d *PTYDriver) reap() {
	err := d.cmd.Wait()
	code := 0
	if err != nil {
		code = d.cmd.ProcessState.ExitCode()
		if code < 0 {
			code = 128
			if status, ok := d.cmd.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				code += int(status.Signal())
			}
		}
	}
	d.code = code
	close(d.reaped)
	d.exited <- code
	close(d.exited)
}
