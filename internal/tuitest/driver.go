package tuitest

import (
	"bytes"
	"context"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
)

// Size is a terminal's dimensions in cells.
type Size struct{ W, H int }

// Driver is the in-process half of the kit: a terminal for a Bubble Tea program running inside the
// test binary. It owns the two ends of the conversation — the [Screen] the renderer paints into,
// and the input the key parser reads — and nothing else. What program it drives, and how that
// program was constructed, is the caller's business: the cmd/apogee half hands it the REAL
// composition through the launcher seam (ADR 0062), so what this driver sees is what a user sees.
//
// The input is one os.Pipe, and that is a decision rather than a detail. Bubble Tea wraps its input
// in a cancel reader, and the epoll-backed implementation is only available for an *os.File
// (cancelreader_linux.go); handed anything else it falls back to a reader it cannot cancel, and
// every Quit then pays a 500 ms wait for the read loop plus a goroutine that outlives the test. So
// the program reads the pipe's read end, and everything a driver sends — typed keys and the
// terminal's own answers to the renderer's queries — goes in through the write end.
type Driver struct {
	t      testing.TB
	screen *Screen
	size   Size

	// The program's input. Reader is handed to tea.WithInput; writer is where keys and the
	// emulator's answers go, both under writeMu so a pumped answer cannot land inside a keystroke.
	reader, writer *os.File
	writeMu        sync.Mutex
	// When a lone Esc was last sent, so the next write can let the reader's escape timeout expire
	// first. Guarded by writeMu.
	lastEsc time.Time

	// The attached program: the Send target [Driver.Resize] needs, and the cancel [Driver.Kill]
	// pulls. Both are nil until Attach.
	mu     sync.Mutex
	prog   *tea.Program
	cancel context.CancelFunc

	done      chan error
	finishing sync.Once
	pumped    chan struct{}
	closing   sync.Once
	ending    sync.Once
	attached  bool
}

// NewDriver builds a driver for a size×size terminal and registers its teardown. The screen is live
// immediately — a program can be attached later, and the frames it paints land here from the first
// byte.
func NewDriver(t testing.TB, size Size) *Driver {
	t.Helper()

	if size.W <= 0 || size.H <= 0 {
		t.Fatalf("tuitest: NewDriver needs a positive terminal size, got %dx%d", size.W, size.H)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("tuitest: create the driver's input pipe: %v", err)
	}
	d := &Driver{
		t:      t,
		screen: NewScreen(size.W, size.H),
		size:   size,
		reader: reader,
		writer: writer,
		done:   make(chan error, 1),
		pumped: make(chan struct{}),
	}
	go d.pumpAnswers()
	t.Cleanup(d.Close)
	return d
}

// ProgramOptions are the Bubble Tea options a driver's program is built with. They are handed to
// tui.Build as its trailing options, where they are appended LAST and therefore win.
//
// The output is deliberately NOT among them: it is tui.Build's own `out` argument ([Driver.Output]),
// because the production path wraps os.Stdout in the trace and the sync-query filter and a driver
// output has to replace that half rather than race it.
func (d *Driver) ProgramOptions() []tea.ProgramOption {
	return []tea.ProgramOption{
		tea.WithInput(d.reader),
		tea.WithWindowSize(d.size.W, d.size.H),
		// No signals and no signal handler: a test process's SIGWINCH and SIGINT belong to the
		// test binary, and a program that installed handlers for them would answer for the whole
		// suite. A driver resizes and interrupts through its own seams instead.
		tea.WithoutSignals(),
		tea.WithoutSignalHandler(),
		// A real terminal underneath would be asked; there is none, so the profile is stated. True
		// colour keeps the renderer's own colours intact all the way to the cells, which is what a
		// colour assertion reads.
		tea.WithColorProfile(colorprofile.TrueColor),
		tea.WithEnvironment([]string{"TERM=xterm-256color", "COLORTERM=truecolor"}),
	}
}

// Output is the writer the renderer paints into: the [Screen] behind a newline translation, which
// is to say the emulator as this program believes it to be. It is an io.Writer because that is what
// tui.Build takes; [Driver.Screen] hands back the screen itself for the assertions that need more
// than writing.
func (d *Driver) Output() io.Writer { return onlcrWriter{w: d.screen} }

// onlcrWriter is the driver's output side, and it exists because of one line in bubbletea: with a
// non-tty INPUT the renderer is put in map-newline mode (tea.go:1075, a workaround for emulated
// ptys left in cooked mode), and from then on it moves the cursor down with a bare LF while
// believing the column reset to 0 (ultraviolet/terminal_renderer.go:1382). A raw terminal does no
// such thing — LF moves down and holds the column — so without this the renderer's model of where
// it is drifts two columns at the first full-width row and the frame comes apart.
//
// The honest fix is to BE the terminal it thinks it is talking to: a line discipline with ONLCR,
// which maps every LF to CR LF. The PTY driver must not do this — a real pty in raw mode does not
// — which is one of the reasons the two drivers' byte counters are not comparable.
type onlcrWriter struct{ w io.Writer }

// nl and crlf are the translation, spelled once.
var (
	nl   = []byte("\n")
	crlf = []byte("\r\n")
)

// Write maps every LF to CR LF and reports the caller's own length, so a renderer that counts what
// it wrote is not told it wrote more than it did.
func (o onlcrWriter) Write(p []byte) (int, error) {
	if !bytes.Contains(p, nl) {
		return o.w.Write(p) //nolint:wrapcheck
	}
	if _, err := o.w.Write(bytes.ReplaceAll(p, nl, crlf)); err != nil {
		return 0, err //nolint:wrapcheck
	}
	return len(p), nil
}

// Attach binds the driver to the program it drives. prog is the Send target [Driver.Resize] uses,
// and cancel ends the program's context for [Driver.Kill]; either may be nil, and the method that
// needs it says so rather than panicking.
func (d *Driver) Attach(prog *tea.Program, cancel context.CancelFunc) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.prog = prog
	d.cancel = cancel
	d.attached = true
}

// Finished reports the result of the run — what program.Run (or the composition around it) returned
// — to whoever is waiting on [Driver.Done] or blocked in [Driver.Quit]. Only the first call counts:
// a run ends once.
func (d *Driver) Finished(err error) {
	d.finishing.Do(func() {
		d.done <- err
		close(d.done)
	})
}

// Done delivers the run's result once [Driver.Finished] has it. It is closed afterwards, so a
// second receive yields nil rather than blocking.
func (d *Driver) Done() <-chan error { return d.done }

// Type sends text as a run of keystrokes, exactly as a human typing it would.
func (d *Driver) Type(text string) {
	d.t.Helper()
	d.send([]byte(text))
}

// Press sends one key's byte sequence.
func (d *Driver) Press(key Key) {
	d.t.Helper()
	d.send([]byte(key))
}

// Resize changes the terminal's size: the emulator reflows, the program is told through a
// WindowSizeMsg — the in-process stand-in for the SIGWINCH a real terminal would raise, which the
// PTY driver raises for real — and the call waits for the repaint that answers it.
//
// The wait is part of the method rather than the caller's business because the alternative is a
// trap: the emulator resizes the instant it is asked, so a frame read straight afterwards is the
// OLD frame at the NEW size, and even a settle check passes, since the screen has genuinely been
// quiet — the program has not been given a chance to paint yet.
func (d *Driver) Resize(w, h int) {
	d.t.Helper()

	if w <= 0 || h <= 0 {
		d.t.Fatalf("tuitest: Resize needs a positive terminal size, got %dx%d", w, h)
	}
	d.size = Size{W: w, H: h}
	before := d.screen.BytesWritten()
	d.screen.Resize(w, h)
	prog := d.program()
	if prog == nil {
		return
	}
	prog.Send(tea.WindowSizeMsg{Width: w, Height: h})
	WaitFor(d.t, func() bool { return d.screen.BytesWritten() > before }, On(d.screen),
		Awaiting("the program to repaint after the resize"))
}

// Frame is what the terminal shows right now.
func (d *Driver) Frame() Frame { return d.screen.Snapshot() }

// Screen is the emulator behind the frames, for the assertions that need its counters
// ([Screen.BytesWritten], [Screen.FullRepaints]) or its settle rule.
func (d *Driver) Screen() *Screen { return d.screen }

// WaitFor polls cond on this driver's screen — the passthrough that saves every call site from
// naming the screen twice.
func (d *Driver) WaitFor(cond func() bool, opts ...Option) {
	d.t.Helper()
	WaitFor(d.t, cond, append([]Option{On(d.screen)}, opts...)...)
}

// WaitText waits for text to appear on screen.
func (d *Driver) WaitText(text string) {
	d.t.Helper()
	WaitText(d.t, d.screen, text)
}

// WaitGone waits for text to leave the screen.
func (d *Driver) WaitGone(text string) {
	d.t.Helper()
	WaitGone(d.t, d.screen, text)
}

// WaitQuiet waits for the screen to stop changing for d — what a test does before pinning a golden.
func (d *Driver) WaitQuiet(quiet time.Duration) {
	d.t.Helper()
	WaitQuiet(d.t, d.screen, quiet)
}

// Quit ends the program the way a human does — Ctrl+C twice, inside apogee's own confirm window —
// and waits for the run to return, handing back its error. A run that does not end is a failure
// with the last frame printed, because "the quit hung" without the screen is the least useful
// failure the kit could produce.
func (d *Driver) Quit() error {
	d.t.Helper()

	d.Press(CtrlC)
	d.Press(CtrlC)
	select {
	case err := <-d.done:
		return err
	case <-time.After(DefaultTimeout):
		waiter{screen: d.screen, what: "the program to quit after Ctrl+C twice"}.fail(d.t)
		return nil // unreachable: fail is a t.Fatalf
	}
}

// Kill ends the program the way a crash does: the context goes, and no final frame, no graceful
// teardown and no farewell follow. It is the in-process half of the "reopen after an abrupt end"
// claim (T-03) — the PTY driver SIGKILLs a real pid for the other half.
//
// The input is ended FIRST, and that is not politeness. A killed program skips
// bubbletea's waitForReadLoop and closes its cancel reader out from under the read loop
// (tea.go:1249-1255); with a live reader on the other side that is a genuine data race, reported by
// -race in the kit's own tests. Ending the input lets the read loop finish on EOF — which does not
// quit the program, so what the cancel below kills is still a running program.
func (d *Driver) Kill() {
	d.t.Helper()

	d.mu.Lock()
	cancel := d.cancel
	d.mu.Unlock()
	if cancel == nil {
		d.t.Fatal("tuitest: Kill needs the program's cancel; Attach was never called with one")
	}
	d.endInput()
	cancel()
	select {
	case <-d.done:
	case <-time.After(DefaultTimeout):
		waiter{screen: d.screen, what: "the program to end after its context was cancelled"}.fail(d.t)
	}
}

// Close tears the driver down: the answer pump stops, the input ends (which is what ends the
// program's read loop), and the pipe is released. It is idempotent and registered as a cleanup, so
// a test only calls it to end the input early.
func (d *Driver) Close() {
	d.closing.Do(func() {
		// Order matters. The screen closes first so the pump's blocking read returns; only once
		// the pump has finished is what it writes into safe to close.
		d.screen.Close()
		<-d.pumped
		d.endInput()
		// The read end belongs to the program while it runs — bubbletea's cancel reader reads the
		// file's descriptor from its own goroutine — so it is released only once the run is over.
		d.mu.Lock()
		attached := d.attached
		d.mu.Unlock()
		if attached {
			select {
			case <-d.done:
			case <-time.After(DefaultTimeout):
			}
		}
		_ = d.reader.Close()
	})
}

// endInput closes the write end of the program's input, which the read loop sees as EOF. It is
// idempotent, and it is separate from Close because [Driver.Kill] needs this half on its own.
func (d *Driver) endInput() {
	d.ending.Do(func() {
		d.writeMu.Lock()
		defer d.writeMu.Unlock()
		_ = d.writer.Close()
	})
}

// escapeGap is how long the driver leaves the input alone after a lone Esc. A terminal cannot tell
// the Escape KEY from the start of an escape SEQUENCE by looking, so every reader resolves it on a
// timeout — ultraviolet's is 50 ms (DefaultEscTimeout). Type a "/" 5 ms after an Esc and the program
// is handed one alt+/ instead of the two keys that were pressed. This is the terminal's rule, not a
// race: it is the one place a driver has to wait on a clock rather than on the screen.
const escapeGap = 70 * time.Millisecond

// send writes into the program's input under the same lock the answer pump holds, so a terminal
// answer can never be spliced into the middle of a keystroke's byte sequence.
func (d *Driver) send(p []byte) {
	d.t.Helper()

	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if wait := escapeGap - time.Since(d.lastEsc); !d.lastEsc.IsZero() && wait > 0 {
		time.Sleep(wait)
	}
	d.lastEsc = time.Time{}
	if _, err := d.writer.Write(p); err != nil {
		d.t.Fatalf("tuitest: write %q to the program's input: %v", p, err)
	}
	if string(p) == string(Esc) {
		d.lastEsc = time.Now()
	}
}

// pumpAnswers hands the emulator's replies — DA1, DECRQM, CPR — back to the program as input. It
// is the other half of being a terminal: the renderer asks what it is talking to and waits, and a
// driver that only typed keys would hang it.
func (d *Driver) pumpAnswers() {
	defer close(d.pumped)

	answers := d.screen.Answers()
	buf := make([]byte, 256)
	for {
		n, err := answers.Read(buf)
		if n > 0 {
			d.writeMu.Lock()
			_, werr := d.writer.Write(buf[:n])
			d.writeMu.Unlock()
			if werr != nil {
				return // the input is closed; the program is no longer listening
			}
		}
		if err != nil {
			return
		}
	}
}

// program is the attached program, or nil.
func (d *Driver) program() *tea.Program {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.prog
}
