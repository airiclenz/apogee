//go:build windows

package tui

// The ConPTY regression harness: apogee's start-up ordering, measured inside a real Windows
// pseudoconsole rather than argued about.
//
// Why a pseudoconsole at all. The Windows ghosting bug (altscreen_windows.go) is not visible in
// anything apogee writes — every byte it emits is correct, and was measured correct throughout the
// investigation. The corruption is applied to those bytes by the CONSOLE, on the way to the screen,
// and only when the console mode word of the buffer being painted lacks DISABLE_NEWLINE_AUTO_RETURN.
// So the defect only exists where a console exists: no unit test over the renderer, the model or the
// emitted stream can see it, however carefully it is written. That is what buys the setup cost here.
//
// What it does. The parent creates a pseudoconsole, re-executes this test binary inside it (the
// house helper-process idiom, TestConPTYChildProcess below), and the child then does what
// cmd/apogee does: apogee's own terminal prologue, then a bubbletea program built with apogee's own
// programOptions. The child measures two things and writes them to a file the parent reads:
//
//   - THE MECHANISM. With the prologue done and before a frame is painted, it parks the cursor at a
//     known column, writes one bare LF, and asks the console API where the cursor went. Column
//     preserved ⇒ the flag reached the buffer that is about to be painted. Column 1 ⇒ the console
//     is rewriting LF as CRLF, which is the ghost's whole mechanism (plan findings 43-45).
//   - THE FRAME. After the program has painted, it reads the console's OWN screen buffer back with
//     ReadConsoleOutputCharacterW and compares it against the frame the model intended. This is the
//     rendered-buffer assertion the plan item asks for: not the byte stream, not a re-serialization
//     of it, but the cells the console decided to hold.
//
// What it can and cannot see, measured rather than assumed (2026-08-07, Windows 11 26200, arm64).
// CreatePseudoConsole always launches the SYSTEM conhost, which re-serializes the client's stream
// out of its own buffer instead of forwarding it, and keeps one console mode word across both
// screen buffers. Two consequences, both load-bearing:
//
//   - Running the pre-fix arm here, the MECHANISM shows (a bare LF lands at column 1) but the FRAME
//     still comes back clean, because bubbletea's own late SetConsoleMode does reach the live buffer
//     on this host. So the LF measurement is what discriminates here; the frame comparison is the
//     assertion that fires on a forwarding host — Windows Terminal's or VS Code's own OpenConsole,
//     where the plan's finding 41 captured the ghost — and no test may assume one is installed.
//   - The ORDERING half of the fix (console mode before the switch, not after) has no observable
//     effect on this host at all, since one mode word cannot land on the wrong buffer. It is
//     asserted directly instead, in altscreen_test.go.
//
// Nothing here reaches for a terminal emulator, and nothing is timing-sensitive beyond one settle
// delay; a host that will not give out a pseudoconsole skips.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unsafe"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/sys/windows"
)

// The child's contract with the parent, all of it: where to write its findings, and which ordering
// to start up in. Both are read only by TestConPTYChildProcess, which is inert without the first.
const (
	conptyResultEnv = "APOGEE_TEST_CONPTY_RESULT" // the file the child writes its measurements to
	conptyArmEnv    = "APOGEE_TEST_CONPTY_ARM"    // which start-up ordering the child uses

	// conptyArmFixed is apogee's real prologue — claimTerminalScreen, whatever it does today. The
	// child calls the production function rather than a copy of its two steps, so a future edit
	// that drops or weakens the console preparation turns this test red.
	conptyArmFixed = "fixed"
	// conptyArmUnfixed is the NEGATIVE CONTROL: apogee as it was before the fix — the alternate
	// screen claimed and nothing said to the console, leaving bubbletea's own (too late)
	// SetConsoleMode as the only one.
	//
	// It is not decoration. A regression test that has never been red is not a regression test, and
	// this defect is specifically one that does not reproduce on every console host: the plan's
	// findings 27 and 41 record the system pseudoconsole painting a pre-fix build perfectly, and a
	// harness with that blind spot would pass either way and guard nothing. So the control runs
	// first, and everything asserted afterwards is conditional on it having actually shown the
	// defect on the host at hand.
	conptyArmUnfixed = "unfixed"
)

// The geometry and the pacing. 100x24 is wide enough that every row the child paints is a
// full-width row — the shape apogee paints and the shape this bug lives on — and small enough to
// read back quickly.
const (
	conptyCols   = 100
	conptyRows   = 24
	conptyFrames = 12                     // one full lap of the snake ring, so every glyph is painted
	conptyTick   = 30 * time.Millisecond  // faster than the renderer's frame budget, so frames coalesce as they do live
	conptySettle = 400 * time.Millisecond // let the last frame reach the console before reading it back
	// conptyLFColumn is where the mechanism probe parks the cursor. Any column but 1 will do — 1 is
	// the answer a translated LF gives, so it must not also be the question.
	conptyLFColumn = 10
	// conptyGiveUpTicks bounds the wait for the pseudoconsole's first window size, so a host that
	// never sends one ends the run with a reason rather than with a process the parent has to kill.
	conptyGiveUpTicks = 100
)

// conptyResult is what one child run measured. It crosses the process boundary as JSON, so every
// field is plain data and an error is a string.
type conptyResult struct {
	Err      string   `json:"err"`      // non-empty ⇒ the child could not complete; the rest is unusable
	Width    int      `json:"width"`    // the size the child was actually given, which is the pty's
	Height   int      `json:"height"`   //
	LFColumn int      `json:"lfColumn"` // 1-based console column after a bare LF at conptyLFColumn
	Intended []string `json:"intended"` // the frame the model painted, row by row
	Screen   []string `json:"screen"`   // what the console buffer holds, row by row

	// BareLFs is filled in by the PARENT, from the child's --tui-trace: how many bare LFs the
	// renderer emitted while painting. Zero means the run never exercised the mechanism at all.
	BareLFs int `json:"-"`
}

// ghostRows reports the rows on which the console's buffer disagrees with the frame the model
// intended, as "row N: want … got …" lines. Trailing blanks are not a disagreement: the model pads
// to the full width and a console cell that was never written reads as NUL, which consoleScreen
// already renders as a space.
func (r conptyResult) ghostRows() []string {
	var diffs []string
	for i := range r.Intended {
		want := strings.TrimRight(r.Intended[i], " ")
		var got string
		if i < len(r.Screen) {
			got = strings.TrimRight(r.Screen[i], " ")
		}
		if want != got {
			diffs = append(diffs, fmt.Sprintf("row %2d:\n  want |%s|\n  got  |%s|", i+1, want, got))
		}
	}
	return diffs
}

// ----------------------------------------------------------------------------
// The test
// ----------------------------------------------------------------------------

// TestConPTYPaintsTheIntendedFrame is the regression test for the Windows ghosting bug: inside a
// real pseudoconsole, running apogee's own terminal prologue and apogee's own program options, a
// bare LF still means "same column" on the buffer being painted, and the console's screen buffer
// ends up holding exactly the frame the program intended.
//
// The negative control runs FIRST, and everything after it is conditional on what it showed. This
// is the honest shape for a defect that is host-dependent: measured on this repo's host (2026-08-07,
// Windows 11 26200, arm64), the pre-fix arm reproduces the mechanism — a bare LF written after
// apogee's prologue lands at column 1 instead of the column it was written from — but its FRAME
// still comes back clean, because the system pseudoconsole is a re-serializing host on which
// bubbletea's late SetConsoleMode does reach the live buffer (plan findings 27, 41, 42). So on this
// host the LF measurement is the discriminating assertion and the frame comparison corroborates it;
// on a host that forwards the client's stream (Windows Terminal's own OpenConsole, finding 41) the
// frame is where the ghost appears, which is why both are asserted. If a host shows neither, the
// test skips rather than reporting a pass it has not earned.
func TestConPTYPaintsTheIntendedFrame(t *testing.T) {
	control := runConPTYChild(t, conptyArmUnfixed)
	if control.Err != "" {
		t.Fatalf("negative control could not run: %s", control.Err)
	}
	controlGhosts := control.ghostRows()
	if control.LFColumn == conptyLFColumn && len(controlGhosts) == 0 {
		t.Skipf("this console host does not reproduce the defect without apogee's fix "+
			"(a bare LF written at column %d still left the cursor at column %d, and the frame came "+
			"back clean), so asserting the same things against the fixed build would pass either way "+
			"and prove nothing. The mechanism is pinned by measurement instead — plan findings 43-46.",
			conptyLFColumn, control.LFColumn)
	}
	t.Logf("negative control reproduced the defect: a bare LF written at column %d landed at column %d, "+
		"%d row(s) ghosted", conptyLFColumn, control.LFColumn, len(controlGhosts))

	fixed := runConPTYChild(t, conptyArmFixed)
	if fixed.Err != "" {
		t.Fatalf("child: %s", fixed.Err)
	}
	// The harness's own precondition: the frames just painted have to have used the byte this test
	// is about, or every assertion below is measuring an empty room.
	if fixed.BareLFs == 0 {
		t.Fatalf("the renderer emitted no bare LF while painting %d frames, so this run exercised "+
			"nothing the ghost rides on", conptyFrames)
	}
	if fixed.LFColumn != conptyLFColumn {
		t.Errorf("after apogee's terminal prologue, a bare LF written at column %d left the cursor at "+
			"column %d, want %d: the console is rewriting LF as CR LF on the buffer about to be "+
			"painted, which is the ghost (see altscreen_windows.go)",
			conptyLFColumn, fixed.LFColumn, conptyLFColumn)
	}
	if diffs := fixed.ghostRows(); len(diffs) > 0 {
		t.Errorf("the console buffer does not hold the frame the program painted — %d of %d row(s) "+
			"ghosted after %d frames and %d bare LFs:\n%s",
			len(diffs), len(fixed.Intended), conptyFrames, fixed.BareLFs, strings.Join(diffs, "\n"))
	}
}

// runConPTYChild opens a pseudoconsole, runs this test binary inside it in the named arm, and
// returns what the child measured. It skips — never fails — when the host will not create a
// pseudoconsole, and it leaves no process behind on any path.
func runConPTYChild(t *testing.T, arm string) conptyResult {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("locate the test binary: %v", err)
	}
	resultPath := filepath.Join(t.TempDir(), "conpty-"+arm+".json")

	pty, err := newConPTY(conptyCols, conptyRows)
	if err != nil {
		t.Skipf("no pseudoconsole on this host: %v", err)
	}
	defer pty.close()

	// A child whose output pipe fills stops writing and never reaches its read-back, so the parent
	// drains continuously. The bytes themselves are not the measurement — the console's buffer is —
	// so they are discarded.
	go func() { _, _ = io.Copy(io.Discard, pty.out) }()

	proc, err := pty.start(exe,
		[]string{"-test.run=^TestConPTYChildProcess$", "-test.count=1", "-test.timeout=60s"},
		append(os.Environ(), conptyResultEnv+"="+resultPath, conptyArmEnv+"="+arm))
	if err != nil {
		t.Fatalf("start the child inside the pseudoconsole: %v", err)
	}
	defer proc.close()
	if err := proc.wait(45 * time.Second); err != nil {
		t.Fatalf("child (%s arm): %v", arm, err)
	}

	raw, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("child (%s arm) wrote no result: %v", arm, err)
	}
	var res conptyResult
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("child (%s arm) result: %v", arm, err)
	}
	if res.Err != "" {
		// The child said what went wrong; its trace is beside the point and probably absent.
		return res
	}
	res.BareLFs, err = conptyBareLFs(conptyTracePath(resultPath))
	if err != nil {
		t.Fatalf("child (%s arm) trace: %v", arm, err)
	}
	return res
}

// conptyTracePath is where a child writes --tui-trace, derived from its result path so the two
// always belong to the same run and the same t.TempDir.
func conptyTracePath(resultPath string) string { return resultPath + ".trace" }

// conptyBareLFs counts the bare LFs in the renderer's own emitted stream — the byte that means
// "next row, SAME column" to ultraviolet and that a console without DISABLE_NEWLINE_AUTO_RETURN
// rewrites as CR LF, which is the ghost (plan finding 43).
//
// It exists to keep the harness honest about itself. Everything this test asserts is downstream of
// the renderer choosing that byte; if a future renderer, option or frame shape stopped emitting it,
// every assertion here would keep passing while measuring nothing at all. The trace format is
// --tui-trace's own: one write per line, as a quoted Go string.
func conptyBareLFs(path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	bare := 0
	for _, line := range strings.Split(string(raw), "\n") {
		if line == "" {
			continue
		}
		write, err := strconv.Unquote(line)
		if err != nil {
			return 0, fmt.Errorf("trace line %q: %w", line, err)
		}
		for i := 0; i < len(write); i++ {
			if write[i] == '\n' && (i == 0 || write[i-1] != '\r') {
				bare++
			}
		}
	}
	return bare, nil
}

// ----------------------------------------------------------------------------
// The child
// ----------------------------------------------------------------------------

// TestConPTYChildProcess is the other half of TestConPTYPaintsTheIntendedFrame, running in a second
// process with a pseudoconsole for a terminal. It is inert — a skip — in an ordinary test run.
func TestConPTYChildProcess(t *testing.T) {
	resultPath := os.Getenv(conptyResultEnv)
	if resultPath == "" {
		t.Skip("not the ConPTY child process")
	}
	if err := conptyChild(resultPath, os.Getenv(conptyArmEnv)); err != nil {
		writeConPTYResult(resultPath, conptyResult{Err: err.Error()})
	}
}

// conptyChild is the child's whole job: start apogee's terminal up, measure what the console does
// to a bare LF on the buffer that is about to be painted, then paint and read the result back.
func conptyChild(resultPath, arm string) error {
	if err := conptyAttachConsole(); err != nil {
		return err
	}

	release, err := conptyClaimTerminal(arm)
	if err != nil {
		return err
	}
	defer release()

	lfColumn, err := conptyLFTranslation(os.Stdout)
	if err != nil {
		return err
	}

	// Built exactly as Run builds it, including the two Windows-only rules (the terminal apogee
	// names itself as, and the mode-2026 decline) — a program constructed differently from the
	// shipping one would be measuring a terminal apogee never talks to. --tui-trace is on so the
	// parent can check that the frames below really do exercise the bare-LF path this bug rides on
	// (conptyBareLFs); without that check the harness could go on passing after some future
	// renderer stopped emitting the byte the whole test is about.
	opts, traced, err := programOptions(context.Background(),
		Options{TracePath: conptyTracePath(resultPath)}, programEnviron(), programDeclinesSyncOutput())
	if err != nil {
		return err
	}
	if traced != nil {
		defer func() { _ = traced.Close() }()
	}
	_, err = tea.NewProgram(conptyModel{resultPath: resultPath, lfColumn: lfColumn}, opts...).Run()
	return err
}

// conptyAttachConsole points the child's standard handles at the pseudoconsole it was given.
//
// It is needed because of how this child is launched, not because of anything apogee does. The
// pseudoconsole attribute gives the child the pty as its CONSOLE, but a child created by a parent
// whose own stdout is a pipe (which `go test`'s is) starts out with that pipe for a stdout — so
// without this, the program under test would be writing into the test harness's pipe and every
// console question would be asked of something that is not a console. Opening CONOUT$/CONIN$ is how
// any console program reaches its console regardless of redirection, and SetStdHandle plus the
// os.Std* re-points make it the one every later reader finds — including bubbletea's, which is the
// point: the program has to see exactly what it sees when a human launches apogee in a terminal.
func conptyAttachConsole() error {
	out, err := conptyOpenConsoleFile("CONOUT$")
	if err != nil {
		return fmt.Errorf("the child was given no console: %w", err)
	}
	in, err := conptyOpenConsoleFile("CONIN$")
	if err != nil {
		return fmt.Errorf("the child's console has no input: %w", err)
	}
	if err := windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, windows.Handle(out.Fd())); err != nil {
		return fmt.Errorf("SetStdHandle(stdout): %w", err)
	}
	if err := windows.SetStdHandle(windows.STD_INPUT_HANDLE, windows.Handle(in.Fd())); err != nil {
		return fmt.Errorf("SetStdHandle(stdin): %w", err)
	}
	os.Stdout, os.Stdin = out, in
	return nil
}

// conptyOpenConsoleFile opens one of the console's own device names.
func conptyOpenConsoleFile(name string) (*os.File, error) {
	name16, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(name16,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", name, err)
	}
	return os.NewFile(uintptr(h), name), nil
}

// conptyClaimTerminal starts the terminal up in the named arm and returns the matching release.
//
// The fixed arm calls the production prologue and does nothing of its own, which is the point: what
// this test measures is claimTerminalScreen itself, not a copy of its steps that could stay right
// while the real one drifted. The unfixed arm is apogee before the fix — the alternate screen
// claimed, the console left alone — and exists only so the harness can prove it can see the defect.
func conptyClaimTerminal(arm string) (func(), error) {
	if arm == conptyArmUnfixed {
		if err := claimAltScreen(os.Stdout); err != nil {
			return nil, err
		}
		return func() {
			_, _ = io.WriteString(os.Stdout, ansiExitAltScreen)
		}, nil
	}
	return claimTerminalScreen(os.Stdout)
}

// conptyLFTranslation measures the one console behaviour this whole plan turned on: what a bare LF
// does to the cursor's COLUMN on the screen buffer apogee is about to paint.
//
// It parks the cursor on row 1 at conptyLFColumn, writes a single LF, and asks the console API
// where the cursor ended up. DISABLE_NEWLINE_AUTO_RETURN in force ⇒ the column is preserved, which
// is what ultraviolet's "next row, same column" LF means. Absent ⇒ the console has rewritten the LF
// as CR LF and the answer is column 1, and every row the renderer paints that way lands 1-based
// instead of column-relative. No glyph is written, so nothing is left on the screen but a cursor;
// the erase afterwards hands the program a clean buffer regardless.
func conptyLFTranslation(f *os.File) (int, error) {
	if _, err := fmt.Fprintf(f, "\x1b[1;%dH\n", conptyLFColumn); err != nil {
		return 0, fmt.Errorf("the LF probe could not write: %w", err)
	}
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(f.Fd()), &info); err != nil {
		return 0, fmt.Errorf("the LF probe could not read the cursor back: %w", err)
	}
	if _, err := io.WriteString(f, "\x1b[2J\x1b[H"); err != nil {
		return 0, fmt.Errorf("the LF probe could not clean up: %w", err)
	}
	return int(info.CursorPosition.X-info.Window.Left) + 1, nil
}

// conptyTickMsg advances the spinner; conptyDoneMsg says the read-back is written.
type (
	conptyTickMsg struct{}
	conptyDoneMsg struct{}
)

// conptyModel is the smallest program that paints the shape this bug lives on: full-width rows,
// changing at the SAME column on every row from one frame to the next. That is what makes the
// renderer move down a row while keeping its column — ultraviolet's bare LF — which is the byte the
// console rewrites when the mode is wrong. It carries apogee's own snake spinner because the
// handoff's tightest symptom was the spinner ghosting with nothing else on screen.
//
// It is a plain value like every other model in this package (ADR 0011); the read-back travels
// through a file rather than a field.
type conptyModel struct {
	resultPath string
	lfColumn   int
	w, h       int
	tick       int
	settling   bool
}

func (m conptyModel) Init() tea.Cmd { return conptyTickCmd() }

func (m conptyModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil
	case conptyTickMsg:
		if m.settling {
			return m, nil
		}
		m.tick++
		if m.w == 0 {
			// No size yet, so there is nothing painted to read back. Wait — but not forever: a
			// pseudoconsole that never reports one has to end this run with a result the parent can
			// read, rather than with a killed process and no explanation for it.
			if m.tick > conptyGiveUpTicks {
				m.settling = true
				return m, m.abandon("the pseudoconsole never reported a window size")
			}
			return m, conptyTickCmd()
		}
		if m.tick < conptyFrames {
			return m, conptyTickCmd()
		}
		// The last frame is painted from the model this returns, so the read-back is queued from
		// here and the ticking stops: nothing may change the screen between the frame and the read.
		m.settling = true
		return m, m.readBack()
	case conptyDoneMsg:
		return m, tea.Quit
	}
	return m, nil
}

// View is the frame: h full-width rows, a vertical stripe of snake glyphs stepping down them at a
// fixed column, and apogee's status-line shape on the last row.
func (m conptyModel) View() tea.View {
	v := tea.NewView(strings.Join(m.rows(), "\n"))
	v.AltScreen = true
	return v
}

// rows is the frame as the model means it, one string per row, each padded to the full width. It is
// both what View renders and what the read-back is compared against, so the comparison can never
// drift from what was painted.
func (m conptyModel) rows() []string {
	if m.w == 0 || m.h == 0 {
		return []string{"apogee — starting…"}
	}
	rows := make([]string, 0, m.h)
	for i := 0; i < m.h-1; i++ {
		// A line of ordinary text with the moving glyph pair at a fixed column, so consecutive rows
		// differ from the previous frame at the same column and the renderer walks down them.
		body := fmt.Sprintf("row %02d %s the quick brown fox jumps over the lazy dog", i+1, snakeGlyph(m.tick+i))
		rows = append(rows, conptyPad(body, m.w))
	}
	rows = append(rows, conptyPad(fmt.Sprintf("%s  apogee · frame %02d · ready", snakeGlyph(m.tick), m.tick), m.w))
	return rows
}

// conptyPad squares a row off to width columns. Every glyph in this frame is one cell wide (the
// snake pair is two braille runes, one cell each — plan finding 6), so a rune count is the width.
func conptyPad(s string, width int) string {
	if n := len([]rune(s)); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return string([]rune(s)[:width])
}

func conptyTickCmd() tea.Cmd {
	return tea.Tick(conptyTick, func(time.Time) tea.Msg { return conptyTickMsg{} })
}

// readBack is the measurement: let the frame this model just produced reach the console, read the
// console's own buffer, and write it beside the frame the model intended.
func (m conptyModel) readBack() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(conptySettle)
		res := conptyResult{Width: m.w, Height: m.h, LFColumn: m.lfColumn, Intended: m.rows()}
		screen, err := conptyConsoleScreen(os.Stdout.Fd(), m.w, m.h)
		if err != nil {
			res.Err = err.Error()
		}
		res.Screen = screen
		writeConPTYResult(m.resultPath, res)
		return conptyDoneMsg{}
	}
}

// abandon ends the run with a reason instead of a measurement, so the parent reports what happened
// rather than a timeout.
func (m conptyModel) abandon(reason string) tea.Cmd {
	return func() tea.Msg {
		writeConPTYResult(m.resultPath, conptyResult{Err: reason})
		return conptyDoneMsg{}
	}
}

// writeConPTYResult hands the measurement back to the parent. A failure here has nowhere to go —
// the child's stdout is the pseudoconsole — so the parent reports the missing file instead.
func writeConPTYResult(path string, res conptyResult) {
	if raw, err := json.Marshal(res); err == nil {
		_ = os.WriteFile(path, raw, 0o600)
	}
}

// ----------------------------------------------------------------------------
// The console read-back
// ----------------------------------------------------------------------------

// procReadConsoleOutputCharacterW reads a run of characters straight out of a screen buffer.
// Neither golang.org/x/sys/windows nor charmbracelet/x/windows wraps it, so it is resolved here.
var procReadConsoleOutputCharacterW = windows.NewLazySystemDLL("kernel32.dll").
	NewProc("ReadConsoleOutputCharacterW")

// conptyConsoleScreen reads height rows of width cells out of the console's visible window. This is
// the rendered buffer — what the console decided to hold — and not a re-serialization of the stream
// that produced it, which is the distinction the plan item insists on: a stream assertion would
// re-encode the renderer's own model of the screen and agree with itself while the screen is wrong.
func conptyConsoleScreen(fd uintptr, width, height int) ([]string, error) {
	handle := windows.Handle(fd)
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(handle, &info); err != nil {
		return nil, fmt.Errorf("the read-back needs a console screen buffer: %w", err)
	}
	rows := make([]string, 0, height)
	cells := make([]uint16, width)
	for row := 0; row < height; row++ {
		origin := windows.Coord{X: info.Window.Left, Y: info.Window.Top + int16(row)}
		var read uint32
		rc, _, err := procReadConsoleOutputCharacterW.Call(
			uintptr(handle),
			uintptr(unsafe.Pointer(&cells[0])),
			uintptr(uint32(width)),
			uintptr(*(*uint32)(unsafe.Pointer(&origin))),
			uintptr(unsafe.Pointer(&read)),
		)
		if rc == 0 {
			return rows, fmt.Errorf("ReadConsoleOutputCharacterW row %d: %w", row+1, err)
		}
		// A cell that was never written reads as NUL and occupies a column exactly as a space does.
		out := make([]uint16, read)
		for i, c := range cells[:read] {
			if c == 0 {
				c = ' '
			}
			out[i] = c
		}
		rows = append(rows, string(utf16.Decode(out)))
	}
	return rows, nil
}

// ----------------------------------------------------------------------------
// The pseudoconsole
// ----------------------------------------------------------------------------

// conPTY is a live Windows pseudoconsole and the two pipe ends the parent keeps.
type conPTY struct {
	handle windows.Handle
	// in is the write end of the child's console input. Nothing types at it — the run is scripted
	// by a ticker, not by keys — but the parent holds it open, because a console whose input has
	// hit EOF is not the console a human's apogee talks to and the renderer's input reader would
	// see the session end under it.
	in  *os.File
	out *os.File // the child's console output, drained by the parent and not otherwise read
}

// newConPTY creates a pseudoconsole of the given size. The failure it most plausibly meets — a host
// that forbids one — is reported as an error for the caller to skip on, never as a fatal.
func newConPTY(cols, rows int) (*conPTY, error) {
	var inRead, inWrite, outRead, outWrite windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return nil, fmt.Errorf("input pipe: %w", err)
	}
	if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		_ = windows.CloseHandle(inRead)
		_ = windows.CloseHandle(inWrite)
		return nil, fmt.Errorf("output pipe: %w", err)
	}
	var handle windows.Handle
	err := windows.CreatePseudoConsole(
		windows.Coord{X: int16(cols), Y: int16(rows)}, inRead, outWrite, 0, &handle)
	// The pseudoconsole duplicated the ends it needs; the parent keeps only its own two.
	_ = windows.CloseHandle(inRead)
	_ = windows.CloseHandle(outWrite)
	if err != nil {
		_ = windows.CloseHandle(inWrite)
		_ = windows.CloseHandle(outRead)
		return nil, fmt.Errorf("CreatePseudoConsole: %w", err)
	}
	return &conPTY{
		handle: handle,
		in:     os.NewFile(uintptr(inWrite), "conpty-in"),
		out:    os.NewFile(uintptr(outRead), "conpty-out"),
	}, nil
}

// close tears the pseudoconsole down. Closing the pseudoconsole first is what releases the child's
// console and lets the drain goroutine's read end see EOF.
func (p *conPTY) close() {
	windows.ClosePseudoConsole(p.handle)
	_ = p.in.Close()
	_ = p.out.Close()
}

// procUpdateProcThreadAttribute is called directly rather than through the x/sys wrapper for one
// reason: PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE takes the pseudoconsole HANDLE in the value slot, not
// a pointer to it, and spelling that as an unsafe.Pointer is exactly the misuse `go vet`'s unsafeptr
// check exists to catch. Allocation still goes through the wrapper's container.
var procUpdateProcThreadAttribute = windows.NewLazySystemDLL("kernel32.dll").
	NewProc("UpdateProcThreadAttribute")

// conptyProcess is a running child and the handles the parent must give back.
type conptyProcess struct {
	process windows.Handle
	thread  windows.Handle
}

// start launches exe inside the pseudoconsole. The child gets the pty as its console through the
// process attribute list, which is what makes its stdout a real console handle — the precondition
// for everything this file measures.
func (p *conPTY) start(exe string, args, env []string) (*conptyProcess, error) {
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, fmt.Errorf("attribute list: %w", err)
	}
	defer attrs.Delete()
	rc, _, callErr := procUpdateProcThreadAttribute.Call(
		uintptr(unsafe.Pointer(attrs.List())),
		0,
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		uintptr(p.handle),
		unsafe.Sizeof(p.handle),
		0,
		0,
	)
	if rc == 0 {
		return nil, fmt.Errorf("UpdateProcThreadAttribute: %w", callErr)
	}

	exe16, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return nil, err
	}
	cmd16, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{exe}, args...)))
	if err != nil {
		return nil, err
	}
	env16, err := conptyEnvBlock(env)
	if err != nil {
		return nil, err
	}

	si := &windows.StartupInfoEx{
		StartupInfo:             windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfoEx{}))},
		ProcThreadAttributeList: attrs.List(),
	}
	var pi windows.ProcessInformation
	if err := windows.CreateProcess(exe16, cmd16, nil, nil, false,
		windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT,
		env16, nil, &si.StartupInfo, &pi); err != nil {
		return nil, fmt.Errorf("CreateProcess: %w", err)
	}
	return &conptyProcess{process: pi.Process, thread: pi.Thread}, nil
}

// wait blocks until the child exits, and reports an error if it overruns or exits non-zero. A
// pseudoconsole that misbehaves must not be able to hang the suite, which is what the timeout is
// for; close then kills whatever is left.
func (c *conptyProcess) wait(timeout time.Duration) error {
	switch ev, err := windows.WaitForSingleObject(c.process, uint32(timeout.Milliseconds())); {
	case err != nil:
		return fmt.Errorf("waiting for the child: %w", err)
	case ev == uint32(windows.WAIT_TIMEOUT):
		return errors.New("the child did not finish in time")
	}
	var code uint32
	if err := windows.GetExitCodeProcess(c.process, &code); err != nil {
		return fmt.Errorf("child exit code: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("the child exited with status %d", code)
	}
	return nil
}

// close terminates the child if it is still running and gives its handles back. It is deferred on
// every path, so a stray process cannot outlive the test however the run ends.
func (c *conptyProcess) close() {
	_ = windows.TerminateProcess(c.process, 1)
	_ = windows.CloseHandle(c.thread)
	_ = windows.CloseHandle(c.process)
}

// conptyEnvBlock packs an environment into the doubly-NUL-terminated UTF-16 block CreateProcess
// wants. Building it here rather than mutating this process's own environment keeps the two arms
// independent of each other and of whatever else the suite is doing.
func conptyEnvBlock(env []string) (*uint16, error) {
	var block []uint16
	for _, entry := range env {
		encoded, err := windows.UTF16FromString(entry)
		if err != nil {
			// An entry with an embedded NUL cannot be spelled; dropping it is better than failing
			// the run over something the test did not set.
			continue
		}
		block = append(block, encoded...)
	}
	block = append(block, 0)
	return &block[0], nil
}
