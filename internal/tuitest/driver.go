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

	// The program's input. input is handed to tea.WithInput; writer is where keys and the
	// emulator's answers go, both under writeMu so a pumped answer cannot land inside a keystroke.
	input   *driverInput
	writer  *os.File
	writeMu sync.Mutex
	// When a lone Esc was last sent, so the next write can let the reader's escape timeout expire
	// first. Guarded by writeMu.
	lastEsc time.Time

	// The attached program: the Send target [Driver.Resize] needs, and the cancel [Driver.Kill]
	// pulls. Both are nil until Attach.
	mu     sync.Mutex
	prog   *tea.Program
	cancel context.CancelFunc

	done chan error
	// ended mirrors done as a bare signal. A wait that only needs to know the run is over must not
	// receive from done: done carries one buffered error, and receiving it would take the result
	// out from under the caller who is waiting for it.
	ended     chan struct{}
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
		input:  &driverInput{File: reader, released: make(chan struct{})},
		writer: writer,
		done:   make(chan error, 1),
		ended:  make(chan struct{}),
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
		tea.WithInput(d.input),
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
		close(d.ended)
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
// The input is ended FIRST, and the read loop is joined SECOND, and neither is politeness. A killed
// program skips bubbletea's waitForReadLoop and closes its cancel reader out from under the read
// loop (tea.go:1249-1255); with a live reader on the other side that is a genuine data race,
// reported by -race in the kit's own tests, and a loop still parked in that reader when it closes
// never wakes at all ([Driver.joinReadLoop]). Ending the input hands the loop an EOF to leave on —
// which does not quit the program, so what the cancel below kills is still a running program — and
// the join is what makes sure it has actually left before the cancel arrives.
func (d *Driver) Kill() {
	d.t.Helper()

	d.mu.Lock()
	cancel := d.cancel
	d.mu.Unlock()
	if cancel == nil {
		d.t.Fatal("tuitest: Kill needs the program's cancel; Attach was never called with one")
	}
	d.endInput()
	d.joinReadLoop()
	cancel()
	select {
	case <-d.done:
	case <-time.After(DefaultTimeout):
		waiter{screen: d.screen, what: "the program to end after its context was cancelled"}.fail(d.t)
	}
}

// Close tears the driver down: the answer pump stops, the input ends (which is what ends the
// program's read loop), the read loop is joined, and the pipe is released. It is idempotent and
// registered as a cleanup, so a test only calls it to end the input early.
func (d *Driver) Close() {
	d.closing.Do(func() {
		// Order matters. The screen closes first so the pump's blocking read returns; only once
		// the pump has finished is what it writes into safe to close.
		d.screen.Close()
		<-d.pumped
		d.endInput()
		// The read end belongs to the program while it runs — bubbletea's cancel reader reads the
		// file's descriptor from its own goroutine — so it is released only once the run is over
		// AND that goroutine has let go of it.
		d.joinReadLoop()
		d.mu.Lock()
		attached := d.attached
		d.mu.Unlock()
		if attached {
			select {
			case <-d.done:
			case <-time.After(DefaultTimeout):
			}
		}
		_ = d.input.Close()
	})
}

// joinReadLoop waits for bubbletea's input read loop to let go of the driver's input, and it is
// what keeps a torn-down driver from leaving a goroutine behind it.
//
// That loop parks in its cancel reader's EpollWait, and the reader is closed out from under it: a
// killed program skips waitForReadLoop entirely (tea.go:1249-1255) and a graceful one gives it
// 500 ms. Closing an epoll descriptor does NOT wake the EpollWait already parked on it, so a loop
// still parked there when the reader closes is parked for the life of the process — and because
// [CheckLeaks] scans goroutines package-globally, the straggler is reported against whichever LATER
// test happens to look. Ending the input hands the loop an EOF to leave on; this waits for it to
// leave, so nothing that follows can strand it.
//
// Two cases need no waiting. A run that has already returned was joined by bubbletea itself on the
// way out, and an input no read loop ever touched has nothing to join. The timeout is the backstop
// for neither being true — a driver whose program never started — and it fails nothing on its own:
// [CheckLeaks] is what reports a goroutine that really did outlive its test.
func (d *Driver) joinReadLoop() {
	if !d.input.touched() {
		return
	}
	select {
	case <-d.input.released:
	case <-d.ended:
	case <-time.After(DefaultTimeout):
	}
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

// driverInput is the read end of the driver's input pipe with one observation added: whether
// bubbletea's read loop is still holding it. That is the whole reason [Driver.joinReadLoop] can
// exist, and it costs nothing else.
//
// It stays an *os.File underneath because it must: muesli/cancelreader chooses its epoll
// implementation by asserting the input to a cancelreader.File — an io.ReadWriteCloser that also
// has Fd and Name — and an embedded *os.File answers every one of those but the method overridden
// here. Anything less and bubbletea falls back to a reader it cannot cancel, which is the very
// thing the pipe was chosen to avoid.
type driverInput struct {
	*os.File

	mu       sync.Mutex
	everRead bool // guarded by mu: a read loop has come through Read at least once
	// released is closed by the read that returns an error. The read loop makes no further read
	// after one, so that error is the moment it lets go of the file — and of the epoll descriptor
	// its cancel reader parks on.
	released chan struct{}
	closing  sync.Once
}

// Read is the pipe's own read, remembering that a loop came through and closing [released] on the
// error that ends it.
func (in *driverInput) Read(p []byte) (int, error) {
	in.mu.Lock()
	in.everRead = true
	in.mu.Unlock()

	n, err := in.File.Read(p)
	if err != nil {
		in.closing.Do(func() { close(in.released) })
	}
	return n, err //nolint:wrapcheck
}

// touched reports whether any read loop has ever read this input.
func (in *driverInput) touched() bool {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.everRead
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
