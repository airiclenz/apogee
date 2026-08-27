package tuitest

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// write paints a sequence into a fresh screen and returns the snapshot, closing the screen when
// the test ends so its answer pump is not a leak.
func write(t *testing.T, w, h int, seq string) (*Screen, Frame) {
	t.Helper()

	s := NewScreen(w, h)
	t.Cleanup(s.Close)
	if _, err := s.Write([]byte(seq)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return s, s.Snapshot()
}

// TestFrameReadsTheEmulatorsText: the plain picture is the emulator's, rows and all, with the
// blank tail of each row trimmed.
func TestFrameReadsTheEmulatorsText(t *testing.T) {
	t.Parallel()

	_, f := write(t, 12, 3, "\x1b[31mred\x1b[0m ok\r\nsecond")
	if got, want := f.String(), "red ok\nsecond\n"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if got, want := f.Row(1), "second"; got != want {
		t.Errorf("Row(1) = %q, want %q", got, want)
	}
	if got, want := len(f.Rows()), 3; got != want {
		t.Errorf("Rows() has %d entries, want %d (one per terminal row)", got, want)
	}
	if got, want := f.Width(), 12; got != want {
		t.Errorf("Width() = %d, want %d", got, want)
	}
	if got, want := f.Height(), 3; got != want {
		t.Errorf("Height() = %d, want %d", got, want)
	}
}

// TestFrameMeasuresAWideRune: the emulator is the authority on how many columns a grapheme takes,
// which is the whole of the glyph-alignment claim (T-20) — counting runes gets it wrong.
func TestFrameMeasuresAWideRune(t *testing.T) {
	t.Parallel()

	_, f := write(t, 12, 2, "世界x")
	if got, want := f.Cell(0, 0).Rune, "世"; got != want {
		t.Errorf("Cell(0,0).Rune = %q, want %q", got, want)
	}
	if got, want := f.Cell(0, 0).Width, 2; got != want {
		t.Errorf("Cell(0,0).Width = %d, want %d", got, want)
	}
	if got := f.Cell(1, 0); got.Width != 0 || got.Rune != "" {
		t.Errorf("Cell(1,0) = %+v, want the empty continuation of the wide cell", got)
	}
	if got, want := f.Cell(4, 0).Rune, "x"; got != want {
		t.Errorf("Cell(4,0).Rune = %q, want %q — the x sits two columns past two wide runes", got, want)
	}
	// Find answers in COLUMNS: "x" is byte 6 of the row and column 4 of the terminal.
	x, y, ok := f.Find("x")
	if !ok || x != 4 || y != 0 {
		t.Errorf("Find(%q) = (%d, %d, %v), want (4, 0, true)", "x", x, y, ok)
	}
}

// TestFrameStyleRunsReportTheirBounds: the colour assertion primitive. A run is a span of columns,
// so a bound means the same thing whatever is inside it.
func TestFrameStyleRunsReportTheirBounds(t *testing.T) {
	t.Parallel()

	_, f := write(t, 12, 2, "\x1b[31mred\x1b[0m ok")
	runs := f.StyleRuns(0)
	if len(runs) != 2 {
		t.Fatalf("StyleRuns(0) = %d runs, want 2 (the red word and the default rest): %+v", len(runs), runs)
	}
	if got, want := runs[0].Text, "red"; got != want {
		t.Errorf("first run text = %q, want %q", got, want)
	}
	if runs[0].X != 0 || runs[0].Width != 3 {
		t.Errorf("first run bounds = (x %d, width %d), want (0, 3)", runs[0].X, runs[0].Width)
	}
	if !SameColor(runs[0].FG, ansi.Red) {
		t.Errorf("first run FG = %v, want ANSI red", runs[0].FG)
	}
	if got, want := runs[1].Text, " ok"; got != want {
		t.Errorf("second run text = %q, want %q — the blank tail is trimmed with the row", got, want)
	}
	if runs[1].FG != nil {
		t.Errorf("second run FG = %v, want the terminal's default", runs[1].FG)
	}
}

// TestFrameCursorFollowsTheProgram: a cursor position is a claim a pane makes (the input caret),
// so the frame carries it.
func TestFrameCursorFollowsTheProgram(t *testing.T) {
	t.Parallel()

	_, f := write(t, 12, 4, "\x1b[3;5Hx")
	x, y := f.Cursor()
	if x != 5 || y != 2 {
		t.Errorf("Cursor() = (%d, %d), want (5, 2) — one past the x written at row 3, column 5", x, y)
	}
}

// TestScreenResizeReflows: a resize is not a repaint. The emulator reflows, and the frame after it
// is what the terminal would show.
func TestScreenResizeReflows(t *testing.T) {
	t.Parallel()

	s, _ := write(t, 12, 3, "hello")
	s.Resize(40, 10)
	f := s.Snapshot()
	if f.Width() != 40 || f.Height() != 10 {
		t.Errorf("after Resize the frame is %dx%d, want 40x10", f.Width(), f.Height())
	}
	if got, want := f.Row(0), "hello"; got != want {
		t.Errorf("Row(0) after resize = %q, want %q", got, want)
	}
}

// TestScreenCountsBytesAndFullRepaints: the T-24 flicker proxy. Bytes are bytes; a repaint is a
// write that told the terminal to start the picture over.
func TestScreenCountsBytesAndFullRepaints(t *testing.T) {
	t.Parallel()

	s := NewScreen(12, 3)
	t.Cleanup(s.Close)
	if _, err := s.Write([]byte("plain")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := s.FullRepaints(); got != 0 {
		t.Errorf("FullRepaints() = %d after a plain write, want 0", got)
	}
	if _, err := s.Write([]byte("\x1b[2Jrepainted")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got, want := s.FullRepaints(), 1; got != want {
		t.Errorf("FullRepaints() = %d, want %d — the erase-all write is one repaint", got, want)
	}
	if got, want := s.BytesWritten(), int64(len("plain")+len("\x1b[2Jrepainted")); got != want {
		t.Errorf("BytesWritten() = %d, want %d", got, want)
	}
}

// TestScreenAnswersTheRenderersQuestions: the emulator IS the terminal. A renderer that asks for
// the primary device attributes gets an answer, and a driver reads it here to hand back — an
// undrained answer would block the emulator mid-write and hang the program painting into it.
func TestScreenAnswersTheRenderersQuestions(t *testing.T) {
	t.Parallel()

	s := NewScreen(12, 3)
	t.Cleanup(s.Close)
	if _, err := s.Write([]byte("\x1b[c")); err != nil { // DA1
		t.Fatalf("Write: %v", err)
	}
	buf := make([]byte, 64)
	n, err := s.Answers().Read(buf)
	if err != nil {
		t.Fatalf("Answers().Read: %v", err)
	}
	if got := string(buf[:n]); !strings.HasPrefix(got, "\x1b[?") {
		t.Errorf("DA1 answer = %q, want a CSI ? device-attributes report", got)
	}
}

// TestScreenCloseStopsThePumpAndRefusesWrites: Close is what keeps the answer pump out of
// [CheckLeaks]' report, so it has to actually end — and a write after it is a driver bug, not a
// silent no-op.
func TestScreenCloseStopsThePumpAndRefusesWrites(t *testing.T) {
	t.Parallel()

	s := NewScreen(12, 3)
	s.Close()
	s.Close() // idempotent
	if _, err := s.Write([]byte("x")); err == nil {
		t.Errorf("Write after Close succeeded, want an error")
	}
	if _, err := s.Answers().Read(make([]byte, 8)); err == nil {
		t.Errorf("Answers().Read after Close succeeded, want io.EOF")
	}
}
