//go:build !windows

package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"

	"github.com/airiclenz/apogee/internal/tuitest"
)

// maxTermSize is the largest terminal a take may ask for: a pty's window is a pair of uint16s, and
// a storyboard asking for more has a typo in it rather than an unusual terminal.
const maxTermSize = 4096

// replySentinel ends the reply pump. It travels through the emulator's own reply pipe — the only
// thing that pump is blocked on — and cannot collide with a real reply, since no terminal reply
// holds a NUL. tuitest.Screen ends its pump the same way, for the same reason: [vt.Emulator.Close]
// sets a flag its reader reads unsynchronised.
const replySentinel = "\x00demorig-close\x00"

// drainTimeout bounds how long [Terminal.Close] waits, after the program is reaped, for the pty to
// hand over the program's last bytes.
const drainTimeout = 2 * time.Second

// TermOptions is the terminal a take is recorded in.
type TermOptions struct {
	// Cols and Rows are the terminal's size in cells.
	Cols, Rows int
	// FPS caps how often the screen is sampled into the take — the storyboard's fps, since a
	// snapshot the render can never show is only size.
	FPS int
	// Env is the child's WHOLE environment: nothing is inherited, so a take says what the program
	// saw. TERM and COLORTERM are appended last — what the terminal claims to be is the
	// terminal's business, not the caller's.
	Env []string
	// Dir is the child's working directory; "" is demorig's own.
	Dir string
	// FromFirstPaint starts the take at the program's first paint rather than at its launch:
	// nothing is recorded until the terminal is on the alternate screen with something drawn on
	// it, and the take's clock starts there. Whatever ran before — a shell's banner, a script's
	// output — never reaches the take.
	FromFirstPaint bool
}

func (o TermOptions) validate() error {
	if o.Cols <= 0 || o.Rows <= 0 || o.Cols > maxTermSize || o.Rows > maxTermSize {
		return fmt.Errorf("a terminal needs a size within 1..%d, got %dx%d", maxTermSize, o.Cols, o.Rows)
	}
	if o.FPS <= 0 {
		return fmt.Errorf("a terminal needs a positive fps, got %d", o.FPS)
	}
	return nil
}

// Terminal is a program running under a real pseudo-terminal, emulated by x/vt and recorded into a
// [Take] as it paints. It is the recorder's whole view of the program: bytes the program writes go
// into the emulator, the replies the emulator produces (DA1, DECRQM, CPR) go back to the program as
// input — a terminal that never answered would hang the renderer on its first query — and the
// rig's own input (typed keys, mouse reports) goes in through [Terminal.Send].
//
// It is tuitest's PTYDriver without the testing.TB: the same session-leader-with-a-controlling-tty
// start, the same emulator with the same margin clamp ([tuitest.ClampMargins]), the same drained
// reply pipe, but errors are returned to a command line rather than failed into a test.
type Terminal struct {
	opts   TermOptions
	start  time.Time
	cmd    *exec.Cmd
	master *os.File

	// mu guards the emulator and everything read off it: a snapshot is only consistent if no
	// output is parsed while it is taken.
	mu            sync.Mutex
	emu           *vt.Emulator
	cursorVisible bool
	dirty         bool
	take          *Take
	// awaitingPaint is true while a FromFirstPaint take has not seen its first paint; painted
	// closes when it has (at once for a take that records from launch).
	awaitingPaint bool
	painted       chan struct{}

	// writeMu serialises the two writers of the master — the rig's input and the emulator's
	// replies — so a reply cannot be spliced into the middle of a key's byte sequence.
	writeMu sync.Mutex
	replies *replyQueue

	exitCode int
	exited   chan struct{}

	stopSampling chan struct{}
	outputDone   chan struct{}
	repliesDone  chan struct{}
	answersDone  chan struct{}
	samplerDone  chan struct{}

	closing sync.Once
}

// StartTerminal starts name with args under an opts.Cols×opts.Rows pty and begins recording. The
// take opens with the blank screen at 0 s. Close the terminal to end the recording and get the
// take.
func StartTerminal(name string, args []string, opts TermOptions) (*Terminal, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	cmd := exec.Command(name, args...) //nolint:gosec // the rig runs the program its operator named
	// exec keeps the LAST definition of a duplicated variable, so appending is how these two win.
	cmd.Env = append(append([]string{}, opts.Env...), "TERM=xterm-256color", "COLORTERM=truecolor")
	cmd.Dir = opts.Dir

	emu := vt.NewEmulator(opts.Cols, opts.Rows)
	tuitest.ClampMargins(emu)
	t := &Terminal{
		opts:          opts,
		emu:           emu,
		cursorVisible: true,
		awaitingPaint: opts.FromFirstPaint,
		painted:       make(chan struct{}),
		replies:       newReplyQueue(),
		exited:        make(chan struct{}),
		stopSampling:  make(chan struct{}),
		outputDone:    make(chan struct{}),
		repliesDone:   make(chan struct{}),
		answersDone:   make(chan struct{}),
		samplerDone:   make(chan struct{}),
	}
	// The callback runs inside emu.Write, which only ever runs with mu held.
	emu.SetCallbacks(vt.Callbacks{CursorVisibility: func(visible bool) { t.cursorVisible = visible }})

	master, err := pty.StartWithAttrs(cmd,
		&pty.Winsize{Rows: uint16(opts.Rows), Cols: uint16(opts.Cols)}, //nolint:gosec // bounded by validate
		&syscall.SysProcAttr{Setsid: true, Setctty: true})
	if err != nil {
		return nil, fmt.Errorf("start %s under a pty: %w", name, err)
	}
	t.cmd, t.master = cmd, master
	t.start = time.Now()
	t.take = &Take{Cols: opts.Cols, Rows: opts.Rows, FPS: opts.FPS, Started: t.start.UTC().Round(0)}
	if !opts.FromFirstPaint {
		t.mu.Lock()
		t.sampleLocked(0)
		t.mu.Unlock()
		close(t.painted)
	}

	go t.pumpOutput()
	go t.pumpReplies()
	go t.pumpAnswers()
	go t.sample()
	go t.reap()
	return t, nil
}

// Send writes p to the program as input: typed text, a key's escape sequence, a mouse report.
func (t *Terminal) Send(p []byte) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if _, err := t.master.Write(p); err != nil {
		return fmt.Errorf("write to the terminal: %w", err)
	}
	return nil
}

// Event logs one thing the rig did or saw, stamped on the take's clock.
func (t *Terminal) Event(kind, detail string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.take.Events = append(t.take.Events, TakeEvent{At: time.Since(t.start), Kind: kind, Detail: detail})
}

// Current is what the terminal shows right now, stamped on the take's clock. It is read, not
// recorded: the take gets its snapshots from the sampler and from [Terminal.Match] alone.
func (t *Terminal) Current() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.snapshotLocked(time.Since(t.start))
}

// Match reports whether match holds for the screen as it stands, and when it does records that
// very screen into the take, stamped now. The sampler takes one snapshot a frame, so a screen the
// rig waited on could otherwise come and go between two samples — and a beat's seen expect judges
// the take, not what the rig saw. Matching and recording happen under one lock, so the screen
// recorded is exactly the screen that matched.
func (t *Terminal) Match(match func(Snapshot) bool) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	screen := t.snapshotLocked(time.Since(t.start))
	if !match(screen) {
		return false
	}
	if !t.awaitingPaint {
		t.dirty = false
		t.take.addSnapshot(screen)
	}
	return true
}

// Painted closes at the program's first paint when the take records from it, and is already
// closed otherwise.
func (t *Terminal) Painted() <-chan struct{} { return t.painted }

// Done closes once the program has exited and been reaped.
func (t *Terminal) Done() <-chan struct{} { return t.exited }

// ExitCode is the program's exit status; it is valid once [Terminal.Done] has closed. A program
// ended by a signal reports -1.
func (t *Terminal) ExitCode() int {
	<-t.exited
	return t.exitCode
}

// Close ends the recording and returns the take. A program still running is killed with its whole
// process group; whatever it painted before it went is flushed into the take first. Close waits for
// every goroutine the terminal started, and a second call returns the same take.
func (t *Terminal) Close() *Take {
	t.closing.Do(func() {
		select {
		case <-t.exited:
		default:
			// Setsid made the child its own process-group leader, so -pid reaches everything it
			// started — a grandchild still holding the pty would keep the output pump alive.
			_ = syscall.Kill(-t.cmd.Process.Pid, syscall.SIGKILL)
		}
		<-t.exited
		// The pump ends by itself when the last holder of the pty's slave is gone and its buffer
		// is read. Something the program left behind holding the slave (a grandchild that kept
		// its stdio) never lets that happen — its bytes were pumped long ago, so after
		// drainTimeout closing the master is what ends the pump.
		drain := time.NewTimer(drainTimeout)
		select {
		case <-t.outputDone:
		case <-drain.C:
		}
		drain.Stop()
		_ = t.master.Close()
		<-t.outputDone
		close(t.stopSampling)
		<-t.samplerDone

		// Ends the reply pump, and through the queue closing behind it, the answer pump.
		_, _ = io.WriteString(t.emu.InputPipe(), replySentinel)
		<-t.repliesDone
		<-t.answersDone
	})
	return t.take
}

// pumpOutput feeds everything the program paints into the emulator. It ends when reading the
// master fails, which is what a pty master does once the program and everything it started are
// gone.
func (t *Terminal) pumpOutput() {
	defer close(t.outputDone)
	buf := make([]byte, 32*1024)
	for {
		n, err := t.master.Read(buf)
		if n > 0 {
			t.mu.Lock()
			_, _ = t.emu.Write(buf[:n])
			t.dirty = true
			if t.awaitingPaint && t.emu.IsAltScreen() && t.screenDrawnLocked() {
				t.startAtPaintLocked()
			}
			t.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

// pumpReplies drains the emulator's reply pipe into the reply queue. The pipe is unbuffered: an
// undrained reply would block emu.Write, and with it the output pump, while mu is held.
func (t *Terminal) pumpReplies() {
	defer close(t.repliesDone)
	defer t.replies.close()
	buf := make([]byte, 4096)
	for {
		n, err := t.emu.Read(buf)
		if n > 0 {
			if bytes.Equal(buf[:n], []byte(replySentinel)) {
				return
			}
			t.replies.write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

// pumpAnswers hands the queued replies to the program. It is apart from pumpReplies so a program
// that is slow to read its input stalls only this goroutine, never the emulator.
func (t *Terminal) pumpAnswers() {
	defer close(t.answersDone)
	buf := make([]byte, 4096)
	for {
		n, err := t.replies.Read(buf)
		if n > 0 {
			// A program that has exited reads nothing; its answers have nowhere to go.
			_ = t.Send(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

// sample records the screen at most FPS times a second, and only when output arrived since the
// last sample. A snapshot is stamped with the tick that took it, so consecutive samples are never
// closer than one frame ([Terminal.Match] may add a matched screen between two). On stop it takes one last sample, so the take ends on what the
// program last painted.
func (t *Terminal) sample() {
	defer close(t.samplerDone)
	ticker := time.NewTicker(time.Second / time.Duration(t.opts.FPS))
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
		case <-t.stopSampling:
			t.mu.Lock()
			if t.dirty && !t.awaitingPaint {
				t.sampleLocked(time.Since(t.start))
			}
			t.mu.Unlock()
			return
		}
		t.mu.Lock()
		if t.dirty && !t.awaitingPaint {
			t.sampleLocked(time.Since(t.start))
		}
		t.mu.Unlock()
	}
}

// sampleLocked adds the current screen to the take unless it matches the last snapshot. The caller
// holds mu.
func (t *Terminal) sampleLocked(at time.Duration) {
	t.dirty = false
	t.take.addSnapshot(t.snapshotLocked(at))
}

// screenDrawnLocked reports whether any cell of the screen shows something other than a blank.
// The caller holds mu.
func (t *Terminal) screenDrawnLocked() bool {
	for y := range t.emu.Height() {
		for x := range t.emu.Width() {
			if c := t.emu.CellAt(x, y); c != nil && c.Content != "" && c.Content != " " {
				return true
			}
		}
	}
	return false
}

// startAtPaintLocked restarts the take's clock at the first paint and records that paint as its
// first snapshot, at 0 s. An event logged before it is moved to 0 s rather than dropped. The
// caller holds mu.
func (t *Terminal) startAtPaintLocked() {
	now := time.Now()
	shift := now.Sub(t.start)
	for i := range t.take.Events {
		t.take.Events[i].At = max(t.take.Events[i].At-shift, 0)
	}
	t.start = now
	t.take.Started = now.UTC().Round(0)
	t.awaitingPaint = false
	t.sampleLocked(0)
	close(t.painted)
}

// snapshotLocked reads the emulator into a snapshot. The caller holds mu.
func (t *Terminal) snapshotLocked(at time.Duration) Snapshot {
	pos := t.emu.CursorPosition()
	return Snapshot{
		At:     at,
		Cursor: Cursor{X: pos.X, Y: pos.Y, Visible: t.cursorVisible},
		Cells:  gridOf(t.emu.Width(), t.emu.Height(), t.emu.CellAt),
	}
}

// reap waits for the program and records how it ended.
func (t *Terminal) reap() {
	err := t.cmd.Wait()
	code := 0
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		code = exitErr.ExitCode()
	default:
		code = -1
	}
	t.exitCode = code
	close(t.exited)
}

// replyQueue is an unbounded pipe with a blocking read, between the emulator's replies and the
// program's input. A bounded one could stall the emulator; a non-blocking read would spin.
type replyQueue struct {
	mu     sync.Mutex
	ready  *sync.Cond
	buf    []byte
	closed bool
}

func newReplyQueue() *replyQueue {
	q := &replyQueue{}
	q.ready = sync.NewCond(&q.mu)
	return q
}

// Read blocks until there is a reply to hand on, or the queue is closed and drained.
func (q *replyQueue) Read(p []byte) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.buf) == 0 && !q.closed {
		q.ready.Wait()
	}
	if len(q.buf) == 0 {
		return 0, io.EOF
	}
	n := copy(p, q.buf)
	q.buf = q.buf[n:]
	return n, nil
}

func (q *replyQueue) write(p []byte) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.buf = append(q.buf, p...)
	q.ready.Broadcast()
}

func (q *replyQueue) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	q.ready.Broadcast()
}
