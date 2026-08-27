package tuitest

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
)

// Screen is the terminal both drivers paint into: a real VT emulator ([vt.Emulator]) behind a
// mutex, fed the renderer's bytes and asked, at any moment, what it currently shows. It is the
// frame authority of ADR 0062 — a claim about the screen is settled against cells this emulator
// laid out, never against a View() string or the raw bytes on the wire, because neither of those
// knows what the terminal did with them.
//
// It is also a terminal in the other direction. A renderer asks questions (DA1, DECRQM for mode
// 2027, DSR/CPR) and waits for answers; the emulator writes those answers, and [Screen.Answers]
// is where a driver reads them to hand back. Nothing else has to know they happened.
type Screen struct {
	mu       sync.Mutex
	term     *vt.Emulator
	written  int64
	repaints int
	lastByte time.Time
	closed   bool

	answers *answerQueue
	pumped  chan struct{}
}

// errScreenClosed is what a write to a closed Screen gets. A driver that keeps painting after its
// program was torn down is a bug in the driver, and a silent success would hide it.
var errScreenClosed = errors.New("tuitest: the screen is closed")

// closeSentinel ends the answer pump. It travels through the emulator's own reply pipe, which is
// the ONLY thing the pump is blocked on, and it cannot collide with a real answer: no terminal
// reply contains a NUL. It exists because [vt.Emulator.Close] sets a flag the pump reads without
// synchronisation — a data race the kit would then carry into every test that uses it.
const closeSentinel = "\x00tuitest-close\x00"

// fullRepaintMarks are the sequences that mean "the whole screen is being redrawn from scratch":
// erase-all (with and without scrollback) and a move to the home cell. Counting the writes that
// carry one is the T-24 flicker proxy — felt flicker is not observable, a renderer that repaints
// the world on every streamed token is.
var fullRepaintMarks = [][]byte{
	[]byte("\x1b[2J"),
	[]byte("\x1b[3J"),
	[]byte("\x1b[H"),
	[]byte("\x1b[;H"),
	[]byte("\x1b[1;1H"),
}

// NewScreen builds a w×h emulator and starts draining its reply pipe. Close it when the program
// that paints into it has stopped — the drain is a goroutine, and [CheckLeaks] counts it.
func NewScreen(w, h int) *Screen {
	s := &Screen{
		term:     vt.NewEmulator(w, h),
		lastByte: time.Now(),
		answers:  newAnswerQueue(),
		pumped:   make(chan struct{}),
	}
	go s.pump()
	return s
}

// Write feeds the renderer's bytes to the emulator. It is the [io.Writer] a driver hands to
// tui.Build as the program's output.
func (s *Screen) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, errScreenClosed
	}
	s.written += int64(len(p))
	s.lastByte = time.Now()
	for _, mark := range fullRepaintMarks {
		if bytes.Contains(p, mark) {
			s.repaints++
			break
		}
	}
	return s.term.Write(p) //nolint:wrapcheck
}

// Snapshot freezes what the terminal currently shows into a [Frame].
func (s *Screen) Snapshot() Frame {
	s.mu.Lock()
	defer s.mu.Unlock()
	pos := s.term.CursorPosition()
	return newFrame(s.term.Width(), s.term.Height(), s.term.CellAt, pos.X, pos.Y, s.term.Render())
}

// Render is the current screen with its SGR sequences intact — what a failing wait prints beneath
// the plain frame so a colour bug is visible in the output rather than only in a rerun.
func (s *Screen) Render() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.term.Render()
}

// Resize changes the emulator's dimensions. A driver pairs it with whatever tells the program the
// size changed — a tea.WindowSizeMsg in process, a real SIGWINCH through a pty.
func (s *Screen) Resize(w, h int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.term.Resize(w, h)
}

// Answers is the terminal's side of the conversation: the DA1, DECRQM and CPR replies the emulator
// produced in response to the renderer's queries, in order. A driver pumps it back into the
// program's input (in process) or the pty master (black box). Reads block until there is something
// to answer with, and return [io.EOF] once the screen is closed.
func (s *Screen) Answers() io.Reader { return s.answers }

// Quiet reports whether the screen has received no bytes for d — the settle rule both drivers wait
// on before reading a frame, since a half-painted frame is the one thing a snapshot must not catch.
func (s *Screen) Quiet(d time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Since(s.lastByte) >= d
}

// BytesWritten is how many bytes the renderer has painted into this screen. Half of the flicker
// proxy, and the only half that is comparable across runs of the same driver.
func (s *Screen) BytesWritten() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.written
}

// FullRepaints is how many of those writes carried a full-screen erase or a cursor-home — the
// other half of the flicker proxy. It counts WRITES, not sequences: one write is one thing the
// terminal was asked to do at once.
func (s *Screen) FullRepaints() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.repaints
}

// Close stops the answer pump and refuses further writes. It is idempotent, and it waits: a
// goroutine still running when the test ends is the leak [CheckLeaks] exists to catch.
func (s *Screen) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	pipe := s.term.InputPipe()
	s.mu.Unlock()
	// Written outside the lock on purpose: the pipe blocks until the pump reads, and the pump
	// must be free to finish whatever it is doing first.
	_, _ = io.WriteString(pipe, closeSentinel)
	<-s.pumped
}

// ReplayTrace rebuilds a --tui-trace file into a Screen of the given size. The trace is every
// write the renderer made, one Go-quoted string per line (internal/tui's tracedOutput), so
// replaying it in order reconstructs both the picture the terminal ended on and the two counters:
// how many bytes were painted, and how many of those writes repainted the world.
//
// It is how a BLACK-BOX test measures what a driver inside the process reads off its own screen.
// The in-process driver refuses a trace — it would wrap an os.Stdout nothing paints into — so the
// PTY driver is the one that exercises the seam, and this is where its bytes come back.
//
// The returned Screen is already closed: nothing is going to paint into it again, and a live one
// would leave a goroutine behind for [CheckLeaks] to find.
func ReplayTrace(t testing.TB, path string, size Size) *Screen {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("tuitest: read the trace at %s: %v", path, err)
	}
	s := NewScreen(size.W, size.H)
	for i, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		// %q escapes every newline, so one line of the file is exactly one write.
		write, uerr := strconv.Unquote(line)
		if uerr != nil {
			t.Fatalf("tuitest: %s line %d is not a quoted trace write: %v", path, i+1, uerr)
		}
		if _, werr := s.Write([]byte(write)); werr != nil {
			t.Fatalf("tuitest: replay %s line %d: %v", path, i+1, werr)
		}
	}
	s.Close()
	return s
}

// pump drains the emulator's reply pipe into the answer queue. The pipe is unbuffered — an
// undrained answer would block the emulator mid-write, and with it the renderer — so this
// goroutine runs for the whole life of the screen.
func (s *Screen) pump() {
	defer close(s.pumped)
	defer s.answers.close()

	buf := make([]byte, 4096)
	for {
		n, err := s.term.Read(buf)
		if n > 0 {
			if bytes.Equal(buf[:n], []byte(closeSentinel)) {
				return
			}
			s.answers.write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

// answerQueue is an unbounded pipe with a blocking read: the emulator's replies on one side, a
// driver's pump on the other. A bounded one would stall the emulator, and a non-blocking read
// would make a driver spin.
type answerQueue struct {
	mu     sync.Mutex
	ready  *sync.Cond
	buf    []byte
	closed bool
}

func newAnswerQueue() *answerQueue {
	q := &answerQueue{}
	q.ready = sync.NewCond(&q.mu)
	return q
}

// Read blocks until there is something to hand back, or until the queue is closed.
func (q *answerQueue) Read(p []byte) (int, error) {
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

func (q *answerQueue) write(p []byte) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.buf = append(q.buf, p...)
	q.ready.Broadcast()
}

func (q *answerQueue) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	q.ready.Broadcast()
}
