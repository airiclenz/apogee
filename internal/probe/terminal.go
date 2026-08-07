package probe

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// ----------------------------------------------------------------------------
// The terminal probe — `apogee probe terminal`
// ----------------------------------------------------------------------------
//
// This half of `probe` measures the TERMINAL instead of trusting it. Every row it prints is an
// observation: a sequence is written, DSR-CPR (CSI 6n) reads back where the cursor actually
// landed, and the answer is set beside what apogee's renderer assumes. That distinction is the
// whole point — the 2026-08 Windows ghosting investigation spent a session on hypotheses that
// were all statements about a terminal nobody had measured, and the two that survived
// (a last-column wrap taken early, and a painter that believes the terminal cannot address a
// column) are both invisible from inside the program.
//
// Like the host half it writes nothing to disk, runs no agent and calls no model. Unlike the
// host half it is not read-only about the SCREEN: it paints on the alternate screen for the
// duration and restores the primary one afterwards, which is why the command refuses outright
// when stdout is not a terminal rather than degrading to a partial answer.

// terminalReplyTimeout bounds the wait for one reply. A terminal that answers DSR-CPR answers it
// immediately — the round trip is a pipe and a parser, not a network — so a second is generous,
// and a probe that hangs is worse than one that reports a silent terminal, because the silence
// is itself a finding the report can state.
const terminalReplyTimeout = time.Second

// maxReplyBytes caps the reply buffer. Nothing the probe asks for answers in more than a few
// dozen bytes; the cap exists so a terminal spewing input (a paste, a mouse report storm) cannot
// grow the buffer without bound while a reply that never comes is waited for.
const maxReplyBytes = 4096

// The geometry the measurements need. The wrap section writes the final column of a row and then
// one more character, so the row below it must exist without scrolling; the glyph, tab and
// capability sections each want a row of their own and a run of columns to sweep.
const (
	minTerminalWidth  = 40
	minTerminalHeight = 8
)

// The rows the sections measure on. They are distinct so one section cannot inherit the leftovers
// of another, and they are all inside minTerminalHeight.
const (
	glyphRow = 2
	tabRow   = 4
	capRow   = 5
	wrapRow  = 6
)

// The sequences the probe writes. They are spelled out rather than composed through a helper
// because a measurement command's whole value is that a reader can see exactly what was sent and
// re-send it by hand.
const (
	csi = "\x1b["

	dsrCPR         = csi + "6n"     // "where is the cursor?" — every measurement rests on it
	decst8c        = csi + "?5W"    // reset tab stops to the every-8 column model
	enterAltScreen = csi + "?1049h" // the measurements paint here, never on the user's screen
	leaveAltScreen = csi + "?1049l" //
	hideCursor     = csi + "?25l"   //
	showCursor     = csi + "?25h"   //
	eraseLine      = csi + "2K"     //
	eraseScreen    = csi + "2J"     //
	setMode2027    = csi + "?2027h" // Unicode core: grapheme measurement
	resetMode2027  = csi + "?2027l" //
	queryMode      = csi + "?%d$p"  // DECRQM
	cursorPosition = csi + "%d;%dH" // CUP
	cursorColumn   = csi + "%dG"    // CHA
	cursorRow      = csi + "%dd"    // VPA
	eraseChars     = csi + "%dX"    // ECH
	insertChars    = csi + "%d@"    // ICH
	repeatChar     = csi + "%db"    // REP
)

// The DECRQM answer values, as the VT specification numbers them.
const (
	modeNotRecognized    = 0
	modeSet              = 1
	modeReset            = 2
	modePermanentlySet   = 3
	modePermanentlyReset = 4
)

// The five capability bits ultraviolet's renderer branches on that this probe can measure, with
// the same values its own `capabilities` bitset uses (terminal_renderer.go:23-50). They are
// mirrored rather than imported because they are unexported there, and mirrored EXACTLY because
// the report's whole claim is "this is what the painter believes about your terminal".
const (
	capVPA = 1 << iota
	capHPA
	capCHA
	capCHT
	capCBT
	capREP
	capECH
	capICH
	capSD
	capSU
)

// allCaps is ultraviolet's own full set, the starting point of every branch in xtermCaps.
const allCaps = capVPA | capHPA | capCHA | capCHT | capCBT | capREP | capECH | capICH | capSD | capSU

// CursorPos is a cursor position as the terminal reports it — 1-based row and column, the
// numbering CUP and DSR-CPR both use.
type CursorPos struct {
	Row, Col int
}

// ModeReport is one DECRPM answer: the mode number the terminal is talking about and the setting
// value it reports for it (0 not recognised, 1 set, 2 reset, 3 permanently set, 4 permanently
// reset).
type ModeReport struct {
	Mode, Setting int
}

// TerminalInputs is the live terminal one measurement session talks to. Everything the session
// needs is injected: raw mode, VT processing on the output handle and a read that can time out
// are all host concerns the composition root has already dealt with, and injecting them is what
// keeps this package free of an os.Stdin of its own.
type TerminalInputs struct {
	// Out is the terminal's output side — os.Stdout, already in VT-processing mode.
	Out io.Writer
	// Read returns whatever the terminal has sent since the last call, waiting up to timeout for
	// the first byte. It returns nil when nothing arrived in time; a probe measures a silent
	// terminal by giving up on it, never by blocking on it.
	Read func(timeout time.Duration) []byte
	// Width and Height are the terminal's size in cells.
	Width, Height int
	// Fd is the terminal's descriptor. It is used only for the OS-level screen read-back that
	// answers the two questions VT replies cannot (did a tab erase what it passed over, did ECH
	// and ICH actually change the cells) — see consoleRow. Zero disables it.
	Fd uintptr
	// TERM is the resolved TERM. It is what ultraviolet's xtermCaps switches on, so it is the
	// input to everything the capability section compares its measurements against.
	TERM string
	// Timeout overrides terminalReplyTimeout. Zero takes the default.
	Timeout time.Duration
}

// TerminalSection is one measured section of the report: a table of observations and the one-line
// verdict that says whether they agree with what the renderer assumes.
type TerminalSection struct {
	Title string
	Head  []string
	Rows  [][]string
	// Flagged marks the rows that disagree with the assumption, index-parallel to Rows. A short
	// or nil slice flags nothing.
	Flagged []bool
	// Notes are free lines printed under the table — the context a bare number cannot carry.
	Notes []string
	// Summary is the section's one-line verdict, printed after "OK — " or "MISMATCH — ".
	Summary  string
	Mismatch bool
}

// Terminal is the finished terminal report: every section that was measured, in the order it was
// measured. It is a value so a caller can read a verdict without parsing the text Report renders.
type Terminal struct {
	Sections []TerminalSection
	// Aborted is why the probe stopped before measuring anything — "" when it ran to the end. An
	// aborted run is a MISMATCH for exit-status purposes: the question was not answered.
	Aborted       string
	Width, Height int
	TERM          string
}

// Mismatch reports whether anything disagreed with what apogee's renderer assumes — the value the
// command turns into its exit status, so `apogee probe terminal` can be checked mechanically.
func (t Terminal) Mismatch() bool {
	if t.Aborted != "" {
		return true
	}
	for _, s := range t.Sections {
		if s.Mismatch {
			return true
		}
	}
	return false
}

// Report renders the whole measurement as a plain table with a one-line verdict per section, so
// pasting it into an issue is enough.
func (t Terminal) Report() string {
	lines := []string{
		"apogee probe terminal — measured, not assumed",
		"  (nothing is written; the screen is restored)",
		"",
		field("size", fmt.Sprintf("%d columns × %d rows", t.Width, t.Height)),
		field("TERM", orUnset(t.TERM)),
		field("painter caps", capsLine(xtermCaps(t.TERM))),
	}
	if t.Aborted != "" {
		lines = append(lines, "", "ABORTED — "+t.Aborted)
		return strings.Join(lines, "\n")
	}
	for _, s := range t.Sections {
		lines = append(lines, "", s.Title)
		lines = append(lines, terminalTable(s.Head, s.Rows, s.Flagged)...)
		for _, note := range s.Notes {
			lines = append(lines, "    "+note)
		}
		verdict := "OK — "
		if s.Mismatch {
			verdict = "MISMATCH — "
		}
		lines = append(lines, "  "+verdict+s.Summary)
	}
	lines = append(lines, "")
	if t.Mismatch() {
		lines = append(lines, "MISMATCH — at least one section disagrees with what the painter assumes")
	} else {
		lines = append(lines, "OK — every section agrees with what the painter assumes")
	}
	return strings.Join(lines, "\n")
}

// terminalTable renders rows under head with every column padded to its widest cell, and a "!"
// gutter on the rows that disagree — so a reader scanning the left edge finds the findings
// without reading the numbers.
func terminalTable(head []string, rows [][]string, flagged []bool) []string {
	widths := make([]int, len(head))
	for i, h := range head {
		widths[i] = ansi.StringWidth(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && ansi.StringWidth(cell) > widths[i] {
				widths[i] = ansi.StringWidth(cell)
			}
		}
	}

	out := make([]string, 0, len(rows)+1)
	out = append(out, "    "+padRow(head, widths))
	for i, row := range rows {
		gutter := "    "
		if i < len(flagged) && flagged[i] {
			gutter = "  ! "
		}
		out = append(out, gutter+padRow(row, widths))
	}
	return out
}

// padRow lays one row out against the column widths, trimming the trailing run so no line carries
// invisible whitespace.
func padRow(cells []string, widths []int) string {
	var b strings.Builder
	for i, cell := range cells {
		b.WriteString(cell)
		if i == len(cells)-1 {
			break
		}
		// A row longer than the headings has no width to pad against; one space keeps its cells
		// apart rather than panicking on a report a caller built by hand.
		width := 0
		if i < len(widths) {
			width = widths[i]
		}
		for pad := width - ansi.StringWidth(cell); pad >= 0; pad-- {
			b.WriteByte(' ')
		}
		b.WriteByte(' ')
	}
	return b.String()
}

// orUnset renders an empty environment value unambiguously. An unset TERM is the single most
// consequential fact this report can carry on Windows — it is what leaves the painter with no
// capabilities at all — so it may never render as a blank space.
func orUnset(s string) string {
	if s == "" {
		return "(unset)"
	}
	return s
}

// GatherTerminal runs the whole measurement against a live terminal and returns the report. It
// paints on the ALTERNATE screen and restores the primary one on every exit path, including the
// aborted ones, so a run leaves the user's terminal as it found it.
func GatherTerminal(in TerminalInputs) Terminal {
	report := Terminal{Width: in.Width, Height: in.Height, TERM: in.TERM}
	if in.Width < minTerminalWidth || in.Height < minTerminalHeight {
		report.Aborted = fmt.Sprintf(
			"this terminal is %d×%d; the measurements need at least %d×%d — resize it and run again",
			in.Width, in.Height, minTerminalWidth, minTerminalHeight)
		return report
	}

	s := &termSession{in: in, timeout: in.Timeout}
	if s.timeout <= 0 {
		s.timeout = terminalReplyTimeout
	}

	s.send(enterAltScreen + hideCursor + eraseScreen)
	defer s.send(showCursor + leaveAltScreen)

	// The preflight. Every section below reads the cursor back, so a terminal that does not
	// answer DSR-CPR cannot be measured at all — and saying so once beats five sections each
	// timing out into an empty row.
	if _, ok := s.cursor(); !ok {
		report.Aborted = "this terminal did not answer DSR-CPR (CSI 6n) — the probe reads every " +
			"measurement back with it, so nothing here can be measured. A multiplexer or a " +
			"recording wrapper that swallows replies is the usual cause."
		return report
	}

	modes, active2027 := s.measureModes()
	report.Sections = append(report.Sections, modes)
	report.Sections = append(report.Sections, s.measureGlyphs(false))
	report.Sections = append(report.Sections, s.measureGlyphs(true))
	// The mode is left where the first sweep found it, so nothing after this measures a terminal
	// the probe silently reconfigured.
	s.restoreMode2027(active2027)
	report.Sections = append(report.Sections, s.measureTabs())
	report.Sections = append(report.Sections, s.measureWrap())
	report.Sections = append(report.Sections, s.measureCapabilities())
	return report
}

// termSession is one measurement session: the terminal, the reply buffer that survives between
// measurements (a terminal may answer two queries in one read), and the deadline each reply is
// waited for.
type termSession struct {
	in      TerminalInputs
	buf     []byte
	timeout time.Duration
}

// send writes a sequence to the terminal. The error is dropped deliberately: a terminal whose
// writes fail answers nothing, and every measurement below already reports a missing answer as a
// finding rather than as an error — one failure mode, reported one way.
func (s *termSession) send(sequence string) {
	_, _ = io.WriteString(s.in.Out, sequence)
}

// sendf is send for the parameterised sequences. It is a separate method rather than a variadic
// send so that every literal call site passes a literal, which is what lets vet check the ones
// that do carry a format string.
func (s *termSession) sendf(format string, a ...any) {
	_, _ = fmt.Fprintf(s.in.Out, format, a...)
}

// await drains the terminal until scan finds what it is looking for or the deadline passes. scan
// returns the number of bytes to consume up to and including the reply it matched, so anything
// the terminal interleaved before it (a keystroke, a report for another query) is skipped rather
// than mistaken for the answer.
func (s *termSession) await(scan func([]byte) (int, bool)) bool {
	deadline := time.Now().Add(s.timeout)
	for {
		if n, ok := scan(s.buf); ok {
			s.buf = append(s.buf[:0], s.buf[n:]...)
			return true
		}
		wait := time.Until(deadline)
		if wait <= 0 {
			return false
		}
		chunk := s.in.Read(wait)
		if len(chunk) == 0 {
			return false
		}
		s.buf = append(s.buf, chunk...)
		if len(s.buf) > maxReplyBytes {
			s.buf = append(s.buf[:0], s.buf[len(s.buf)-maxReplyBytes:]...)
		}
	}
}

// cursor asks the terminal where the cursor is and returns its answer. This is the primitive the
// entire probe is built from: every other measurement writes something and then calls this.
func (s *termSession) cursor() (CursorPos, bool) {
	s.send(dsrCPR)
	var pos CursorPos
	ok := s.await(func(buf []byte) (int, bool) {
		p, n, found := scanCursorPos(buf)
		pos = p
		return n, found
	})
	return pos, ok
}

// modeReport asks DECRQM about one mode and returns the terminal's answer.
func (s *termSession) modeReport(mode int) (ModeReport, bool) {
	s.sendf(queryMode, mode)
	var rep ModeReport
	ok := s.await(func(buf []byte) (int, bool) {
		r, n, found := scanModeReport(buf, mode)
		rep = r
		return n, found
	})
	return rep, ok
}

// moveTo parks the cursor at a known cell. Every measurement starts from one, because an advance
// is a difference between two columns and the first of them may not be a guess.
func (s *termSession) moveTo(row, col int) {
	s.sendf(cursorPosition, row, col)
}

// clearRow blanks a measurement row so the row-text read-back below sees this measurement's
// cells and not the previous one's.
func (s *termSession) clearRow(row int) {
	s.moveTo(row, 1)
	s.send(eraseLine)
}

// advanceOf writes text at column 1 of row and reports how many columns the cursor moved — the
// terminal's own answer to "how wide is this?", which is the number the painter has to agree with.
func (s *termSession) advanceOf(row int, text string) (int, bool) {
	s.clearRow(row)
	s.send(text)
	pos, ok := s.cursor()
	if !ok {
		return 0, false
	}
	return pos.Col - 1, true
}

// ----------------------------------------------------------------------------
// Section 1 — mode negotiation
// ----------------------------------------------------------------------------

// measureModes reports what the terminal says about modes 2026 (synchronized output) and 2027
// (Unicode core), and then whether setting 2027 actually STICKS. The second half is the one that
// matters: bubbletea switches the painter to grapheme measurement on the strength of this
// terminal's answer (cursed_renderer.go:690-701), so a terminal that acknowledges the mode
// without honouring it desyncs the two sides of every subsequent width decision.
//
// It also returns the setting 2027 was found in, so the caller can put it back.
func (s *termSession) measureModes() (TerminalSection, int) {
	section := TerminalSection{
		Title: "mode negotiation",
		Head:  []string{"mode", "name", "reported", "after set"},
	}

	before2026, ok2026 := s.modeReport(2026)
	if !ok2026 {
		before2026 = ModeReport{Mode: 2026, Setting: -1}
	}
	before2027, ok2027 := s.modeReport(2027)
	if !ok2027 {
		before2027 = ModeReport{Mode: 2027, Setting: -1}
	}

	s.send(setMode2027)
	after2027, okAfter := s.modeReport(2027)
	if !okAfter {
		after2027 = ModeReport{Mode: 2027, Setting: -1}
	}

	stuck := after2027.Setting == modeSet || after2027.Setting == modePermanentlySet
	// A terminal that never heard of the mode is not lying about it; only one that answers the
	// query and then does not honour the set is.
	known := before2027.Setting != modeNotRecognized && before2027.Setting != -1
	section.Mismatch = known && !stuck

	section.Rows = [][]string{
		{"2026", "synchronized output", modeSettingName(before2026.Setting), "—"},
		{"2027", "unicode core", modeSettingName(before2027.Setting), modeSettingName(after2027.Setting)},
	}
	section.Flagged = []bool{false, section.Mismatch}

	switch {
	case section.Mismatch:
		section.Summary = "the terminal answers DECRQM for mode 2027 but CSI ?2027h does not take — " +
			"bubbletea switches the painter to grapheme measurement on that answer, so the two sides disagree"
	case before2027.Setting == modeNotRecognized:
		section.Summary = "mode 2027 is not recognised, so the painter keeps ultraviolet's wcwidth default"
	case before2027.Setting == -1:
		section.Summary = "the terminal did not answer DECRQM for mode 2027; bubbletea leaves the painter on wcwidth"
	default:
		section.Summary = "mode 2027 reads back as set after CSI ?2027h"
	}
	if before2026.Setting == modeNotRecognized || before2026.Setting == -1 {
		section.Notes = append(section.Notes,
			"mode 2026 is unavailable here: the renderer falls back to hiding the cursor around each frame")
	}
	return section, before2027.Setting
}

// restoreMode2027 puts the mode back where the probe found it. A terminal that reports the mode
// permanently set or permanently reset has nothing to restore — the setting is not ours to move.
func (s *termSession) restoreMode2027(setting int) {
	if setting == modeReset || setting == modeNotRecognized {
		s.send(resetMode2027)
	}
}

// ----------------------------------------------------------------------------
// Section 2 — glyph advance
// ----------------------------------------------------------------------------

// terminalGlyphs is apogee's painted vocabulary — the exact set the ghosting investigation's
// finding 6 measured against the painter's width tables. The two spinner entries are the shapes
// the status line animates (a two-cell snake pair and a one-cell classic frame), and the last four
// are the glyphs where wcwidth and grapheme measurement can disagree, which is what makes them
// worth a row of their own.
var terminalGlyphs = []struct{ Name, Glyph string }{
	{"snake spinner", "⣿⣿"},
	{"classic spinner", "⣾"},
	{"middle dot", "·"},
	{"sparkle", "✦"},
	{"ellipsis", "…"},
	{"box rule", "─"},
	{"box side", "│"},
	{"check", "✓"},
	{"bullet", "•"},
	{"arrow", "→"},
	{"block", "█"},
	{"warning (VS16)", "⚠️"},
	{"info (VS16)", "ℹ️"},
	{"sparkles", "✨"},
	{"wide han", "中"},
}

// measureGlyphs writes each glyph at a known column and reads back how far the cursor moved, then
// sets that against both of the painter's width tables. The sweep runs twice — mode 2027 off, then
// on — because a difference between the two runs is itself the finding: it means the terminal's
// measurement moves with the mode, which is the only way the mode can be the cause of a width
// disagreement rather than a bystander.
func (s *termSession) measureGlyphs(unicodeCore bool) TerminalSection {
	if unicodeCore {
		s.send(setMode2027)
	} else {
		s.send(resetMode2027)
	}
	// What the terminal actually did with that request decides which of the painter's two tables
	// is the one it is supposed to agree with.
	inForce, ok := s.modeReport(2027)
	if !ok {
		// A terminal that says nothing is not a terminal that said "not recognised": the title
		// below prints the difference, because they are different findings.
		inForce = ModeReport{Mode: 2027, Setting: -1}
	}
	grapheme := inForce.Setting == modeSet || inForce.Setting == modePermanentlySet

	title := "glyph advance (mode 2027 off"
	if unicodeCore {
		title = "glyph advance (mode 2027 on"
	}
	section := TerminalSection{
		Title: title + ", terminal reports " + modeSettingName(inForce.Setting) + ")",
		Head:  []string{"glyph", "name", "terminal", "wcwidth", "grapheme"},
	}

	method := ansi.WcWidth
	assumed := "wcwidth"
	if grapheme {
		method, assumed = ansi.GraphemeWidth, "grapheme"
	}

	silent := 0
	for _, g := range terminalGlyphs {
		wc := ansi.WcWidth.StringWidth(g.Glyph)
		gr := ansi.GraphemeWidth.StringWidth(g.Glyph)
		advance, ok := s.advanceOf(glyphRow, g.Glyph)
		measured := strconv.Itoa(advance)
		flag := advance != method.StringWidth(g.Glyph)
		if !ok {
			measured, flag = "no answer", true
			silent++
		}
		section.Rows = append(section.Rows,
			[]string{g.Glyph, g.Name, measured, strconv.Itoa(wc), strconv.Itoa(gr)})
		section.Flagged = append(section.Flagged, flag)
		section.Mismatch = section.Mismatch || flag
	}

	if silent > 0 {
		section.Notes = append(section.Notes,
			fmt.Sprintf("%d of %d glyphs got no cursor report back", silent, len(terminalGlyphs)))
	}
	if section.Mismatch {
		section.Summary = "the terminal advances at least one glyph by a width the painter's " +
			assumed + " table does not predict"
	} else {
		section.Summary = "every glyph advances by the width the painter's " + assumed + " table predicts"
	}
	return section
}

// ----------------------------------------------------------------------------
// Section 3 — hard tabs
// ----------------------------------------------------------------------------

// measureTabs walks the row one tab at a time and records where the cursor lands, both before and
// after DECST8C. bubbletea hardcodes hard-tab cursor movement on Windows (termios_windows.go:9 sets
// useHardTabs unconditionally, next to its own "TODO: check if we can optimize cursor movements on
// Windows" at tty_windows.go:55), and ultraviolet then moves the cursor against an every-8 tab-stop
// model. Enforcement of that model is split across the two layers, and only one of them still
// emits: ultraviolet's renderer-level DECST8C is commented out upstream behind a TODO
// (terminal_renderer.go:1200-1205), while bubbletea writes DECST8C once before the first frame
// whenever hard tabs are on (cursed_renderer.go:282-284). So every session on Windows does ask the
// terminal for stops every 8 — and one that ignores the request moves the cursor to a column the
// renderer is not expecting.
//
// The probe sends DECST8C itself, which is why the stops are read twice: the "before" row is the
// terminal's untouched state, the "after" row is the state a Windows session actually paints into,
// and the pair separates "the stops were already every 8" from "the stops moved when this terminal
// was asked".
//
// The second half asks the other tab question: whether the tab ERASED what it passed over. That
// one cannot be answered from a cursor position — it needs the cells themselves — so it is
// answered by the OS-level screen read-back where there is one, and reported as unverified where
// there is not.
func (s *termSession) measureTabs() TerminalSection {
	section := TerminalSection{
		Title: "hard tabs",
		Head:  []string{"measurement", "observed", "renderer assumes"},
	}

	before, okBefore := s.tabStops()
	s.send(decst8c)
	after, okAfter := s.tabStops()
	assumed := everyEightStops(s.in.Width, len(after))

	stopsAgree := okAfter && sameInts(after, assumed)
	section.Rows = append(section.Rows,
		[]string{"tab stops (before DECST8C)", stopList(before, okBefore), stopList(everyEightStops(s.in.Width, len(before)), true)},
		[]string{"tab stops (after DECST8C)", stopList(after, okAfter), stopList(assumed, true)},
	)
	section.Flagged = append(section.Flagged, false, !stopsAgree)
	section.Mismatch = !stopsAgree

	effect := "no change"
	if !sameInts(before, after) {
		effect = "moved the stops"
	}
	// bubbletea sends DECST8C before the first frame whenever hard tabs are on, which on Windows is
	// always (cursed_renderer.go:282-284, termios_windows.go:9), so the renderer really does assert
	// the every-8 stops this row resets. Ultraviolet's own emission being commented out upstream
	// changes WHICH layer sends it, not whether it is sent.
	section.Rows = append(section.Rows,
		[]string{"DECST8C (CSI ?5W)", effect, "every 8 — bubbletea sends DECST8C at start"})
	section.Flagged = append(section.Flagged, false)

	// Does a tab erase what it moves over? Write a known run, jump back to the start, tab across
	// it, and read the cells back.
	const marker = "ABCDEFGHIJKLMNOP"
	s.clearRow(tabRow)
	s.send(marker)
	s.moveTo(tabRow, 1)
	s.send("\t")
	observed, erasedKnown := s.rowText(tabRow)
	verdict := "unverified — no screen read-back on this OS"
	erased := false
	if erasedKnown {
		if strings.HasPrefix(observed, marker) {
			verdict = "moved over the cells, left them intact"
		} else {
			verdict, erased = "ERASED the cells it passed over: "+quoteCells(observed, len(marker)), true
		}
	}
	section.Rows = append(section.Rows, []string{"tab over written cells", verdict, "moves only, never erases"})
	section.Flagged = append(section.Flagged, erased)
	section.Mismatch = section.Mismatch || erased

	switch {
	case erased:
		section.Summary = "a tab erased the cells it moved over; the renderer moves the cursor with tabs and expects them untouched"
	case !stopsAgree:
		section.Summary = "the terminal's tab stops are not the every-8 model the renderer moves against"
	default:
		section.Summary = "tab stops are every 8 columns and a tab moves without erasing"
	}
	return section
}

// tabStops walks one row with tabs from column 1 and returns the columns the cursor landed on. It
// stops when a tab no longer advances — the last stop on the row — or when the row is exhausted.
func (s *termSession) tabStops() ([]int, bool) {
	s.clearRow(tabRow)
	stops := make([]int, 0, 8)
	last := 1
	for range s.in.Width/8 + 2 {
		s.send("\t")
		pos, ok := s.cursor()
		if !ok {
			return stops, false
		}
		if pos.Col <= last {
			break
		}
		last = pos.Col
		stops = append(stops, pos.Col)
		if len(stops) == 8 || pos.Col >= s.in.Width {
			break
		}
	}
	return stops, true
}

// everyEightStops is the renderer's own tab-stop model, rendered as the columns a cursor tabbing
// from column 1 would land on: ultraviolet counts stops every 8 columns, and on Windows bubbletea
// asserts that model on the terminal by writing DECST8C before the first frame
// (cursed_renderer.go:282-284, termios_windows.go:9). Ultraviolet's own renderer-level emission is
// commented out upstream (terminal_renderer.go:1200-1205), so off that Windows path the model is an
// assumption rather than a negotiated setting.
func everyEightStops(width, n int) []int {
	stops := make([]int, 0, n)
	for col := 9; col <= width && len(stops) < n; col += 8 {
		stops = append(stops, col)
	}
	return stops
}

// ----------------------------------------------------------------------------
// Section 4 — the last column
// ----------------------------------------------------------------------------

// measureWrap answers the question the 2026-08 ghosting investigation ended on: when a character
// is written in the FINAL column, does the cursor stay there with a wrap pending (xterm's
// deferred-wrap semantics, which is what ultraviolet's renderer emits against), or has the
// terminal already moved to column 1 of the next row?
//
// apogee paints full-width rows — the box rule, the separators and the status line all span the
// terminal — so nearly every frame writes that cell. A terminal that wraps immediately puts the
// cursor a whole row away from where the renderer's model says it is, and with no absolute column
// addressing (see the capability section) nothing re-anchors it until a full redraw.
func (s *termSession) measureWrap() TerminalSection {
	section := TerminalSection{
		Title: "last-column wrap",
		Head:  []string{"step", "cursor (CPR)", "console API", "deferred wrap would be"},
	}

	s.clearRow(wrapRow)
	s.clearRow(wrapRow + 1)
	s.moveTo(wrapRow, s.in.Width)
	s.send("X")
	afterX, okX := s.cursor()
	consoleX := s.consoleCursorCell()

	deferred := okX && afterX.Row == wrapRow && afterX.Col == s.in.Width
	immediate := okX && afterX.Row == wrapRow+1 && afterX.Col == 1

	s.send("Y")
	afterY, okY := s.cursor()

	section.Rows = append(section.Rows,
		[]string{"wrote the final column", cellOf(afterX, okX), consoleX, fmt.Sprintf("%d,%d (pending)", wrapRow, s.in.Width)},
		[]string{"wrote one more", cellOf(afterY, okY), s.consoleCursorCell(), fmt.Sprintf("%d,2", wrapRow+1)},
	)
	section.Flagged = append(section.Flagged, !deferred, false)
	section.Mismatch = !deferred

	switch {
	case deferred:
		section.Summary = "the terminal holds a pending wrap at the last column — the semantics the renderer emits against"
	case immediate:
		section.Summary = "the terminal WRAPPED IMMEDIATELY on the last column; the renderer assumes a pending wrap, " +
			"so its model of the cursor is a row out from the first full-width row it paints"
	case !okX:
		section.Summary = "the terminal did not report the cursor after the last column was written"
	default:
		section.Summary = fmt.Sprintf("the cursor landed at %d,%d after the last column — neither a pending wrap nor an immediate one",
			afterX.Row, afterX.Col)
	}
	return section
}

// ----------------------------------------------------------------------------
// Section 5 — the capabilities actually present
// ----------------------------------------------------------------------------

// measureCapabilities uses each of the five sequences ultraviolet branches on and reads back
// whether it worked, then sets the answer against what xtermCaps(TERM) claims for this terminal.
//
// This is the section that makes the Windows divergence visible from outside the program: with
// TERM unset — the normal state of every Windows shell — xtermCaps returns NO capabilities at all,
// so the renderer believes it cannot address a column absolutely and navigates the row with
// relative moves counted off its own model. One bad advance then poisons that model for the whole
// line, which is the mechanism behind "only a resize fixes it".
func (s *termSession) measureCapabilities() TerminalSection {
	section := TerminalSection{
		Title: "capabilities actually present",
		Head:  []string{"capability", "sequence", "terminal", "xtermCaps(TERM)"},
	}
	caps := xtermCaps(s.in.TERM)

	cha, chaKnown := s.probeCHA()
	vpa, vpaKnown := s.probeVPA()
	rep, repKnown := s.probeREP()
	ech, echKnown := s.probeCells(fmt.Sprintf(eraseChars, 4), "    EFGH")
	ich, ichKnown := s.probeCells(fmt.Sprintf(insertChars, 4), "    ABCD")
	measured := []capMeasurement{
		{"CHA", "CSI 20G", capCHA, cha, chaKnown},
		{"VPA", "CSI 5d", capVPA, vpa, vpaKnown},
		{"ECH", "CSI 4X", capECH, ech, echKnown},
		{"ICH", "CSI 4@", capICH, ich, ichKnown},
		{"REP", "CSI 5b", capREP, rep, repKnown},
	}

	unverified := 0
	for _, m := range measured {
		claimed := caps&m.bit != 0
		observed := "no"
		switch {
		case !m.known:
			observed = "unverified"
			unverified++
		case m.have:
			observed = "yes"
		}
		// The finding is a capability the terminal HAS and the painter does not know about: that
		// is the one that costs the renderer a recovery path it could have used.
		flag := m.known && m.have && !claimed
		section.Rows = append(section.Rows, []string{m.name, m.seq, observed, yesNo(claimed)})
		section.Flagged = append(section.Flagged, flag)
		section.Mismatch = section.Mismatch || flag
	}

	if unverified > 0 {
		section.Notes = append(section.Notes,
			"ECH and ICH change cells without moving the cursor, so they need a screen read-back; this OS has none")
	}
	if section.Mismatch {
		section.Summary = fmt.Sprintf("this terminal supports sequences xtermCaps(TERM=%s) does not grant the painter — "+
			"the renderer is navigating with less than the terminal offers", orUnset(s.in.TERM))
	} else {
		section.Summary = "the painter's capability set matches what the terminal actually does"
	}
	return section
}

// capMeasurement is one capability as the section reports it: what was sent, what the terminal
// did with it (have), and whether the question could be answered at all (known — ECH and ICH
// cannot be answered without a screen read-back).
type capMeasurement struct {
	name  string
	seq   string
	bit   int
	have  bool
	known bool
}

// probeCHA moves the cursor to an absolute column and reads back whether it went there.
func (s *termSession) probeCHA() (bool, bool) {
	s.moveTo(capRow, 1)
	s.sendf(cursorColumn, 20)
	pos, ok := s.cursor()
	return ok && pos.Col == 20, ok
}

// probeVPA moves the cursor to an absolute row and reads back whether it went there.
func (s *termSession) probeVPA() (bool, bool) {
	s.moveTo(1, 1)
	s.sendf(cursorRow, capRow)
	pos, ok := s.cursor()
	return ok && pos.Row == capRow, ok
}

// probeREP repeats the last graphic character. Unlike ECH and ICH this one IS visible from the
// cursor: a supported REP advances it by the repeat count, an ignored one leaves it where the
// single character put it.
func (s *termSession) probeREP() (bool, bool) {
	s.clearRow(capRow)
	s.send("A")
	s.sendf(repeatChar, 5)
	pos, ok := s.cursor()
	return ok && pos.Col == 7, ok
}

// probeCells writes a known run, applies seq at its start, and reads the cells back to see whether
// the sequence did what it claims. It is how ECH and ICH are measured: both change the row without
// moving the cursor, so nothing about them is observable through DSR-CPR.
func (s *termSession) probeCells(seq, want string) (bool, bool) {
	s.clearRow(capRow)
	s.send("ABCDEFGH")
	s.moveTo(capRow, 1)
	s.send(seq)
	observed, ok := s.rowText(capRow)
	if !ok {
		return false, false
	}
	return strings.HasPrefix(observed, want), true
}

// rowText reads one row of the terminal's screen back. There is no portable VT sequence that
// reports what is IN a cell, so this is an OS-level read of the console's own buffer and is
// available on Windows only — which is where the questions it answers were asked.
func (s *termSession) rowText(row int) (string, bool) {
	if s.in.Fd == 0 {
		return "", false
	}
	return consoleRow(s.in.Fd, row, s.in.Width)
}

// consoleCursorCell renders the OS's own answer to "where is the cursor" beside the terminal's VT
// answer. On the ConPTY path both come from the same headless conhost, so agreement is expected
// and a DISAGREEMENT would itself be the finding — which is why the report prints both rather than
// picking one.
func (s *termSession) consoleCursorCell() string {
	if s.in.Fd == 0 {
		return "—"
	}
	pos, ok := consoleCursor(s.in.Fd)
	if !ok {
		return "—"
	}
	return fmt.Sprintf("%d,%d", pos.Row, pos.Col)
}

// ----------------------------------------------------------------------------
// The reply parser
// ----------------------------------------------------------------------------

// csiSeq is one complete CSI sequence lifted out of a reply stream, split into the parts a report
// is identified by: the private marker ('?' on every DEC report), the parameters, the
// intermediates ('$' on DECRPM) and the final byte.
type csiSeq struct {
	private byte
	params  string
	inter   string
	final   byte
	end     int
}

// nextCSI returns the first COMPLETE CSI sequence at or after from. It returns false when what
// remains is a sequence still arriving — a reply cut in half by a read boundary must be waited
// for, never discarded, which is the single thing a naive parser gets wrong against a real
// terminal.
func nextCSI(buf []byte, from int) (csiSeq, bool) {
	for i := from; i < len(buf); i++ {
		if buf[i] != 0x1b {
			continue
		}
		if i+1 >= len(buf) {
			return csiSeq{}, false // an ESC with nothing after it yet
		}
		if buf[i+1] != '[' {
			continue // some other escape sequence: step over its ESC and keep looking
		}

		var seq csiSeq
		j := i + 2
		if j < len(buf) && buf[j] >= 0x3c && buf[j] <= 0x3f {
			seq.private = buf[j]
			j++
		}
		start := j
		for j < len(buf) && buf[j] >= 0x30 && buf[j] <= 0x3b {
			j++
		}
		seq.params = string(buf[start:j])
		start = j
		for j < len(buf) && buf[j] >= 0x20 && buf[j] <= 0x2f {
			j++
		}
		seq.inter = string(buf[start:j])
		if j >= len(buf) {
			return csiSeq{}, false // the final byte has not arrived
		}
		if buf[j] < 0x40 || buf[j] > 0x7e {
			continue // not a CSI at all; keep looking
		}
		seq.final, seq.end = buf[j], j+1
		return seq, true
	}
	return csiSeq{}, false
}

// scanCursorPos finds the first DSR-CPR report in buf and returns it with the number of bytes to
// consume up to and including it. Anything the terminal sent before the report — a keystroke, a
// report answering an earlier query — is skipped, because a terminal is under no obligation to
// answer one question at a time.
func scanCursorPos(buf []byte) (CursorPos, int, bool) {
	for at := 0; ; {
		seq, ok := nextCSI(buf, at)
		if !ok {
			return CursorPos{}, 0, false
		}
		at = seq.end
		// DECXCPR answers with a '?' marker and the same shape, so the marker is not checked.
		if seq.final != 'R' || seq.inter != "" {
			continue
		}
		row, col, ok := twoParams(seq.params)
		if !ok || row < 1 || col < 1 {
			continue
		}
		return CursorPos{Row: row, Col: col}, seq.end, true
	}
}

// scanModeReport finds the first DECRPM answer ABOUT mode in buf. The mode is matched rather than
// assumed: the probe has two queries outstanding at once at start-up, and a terminal that answers
// them out of order would otherwise have mode 2026's setting recorded against mode 2027.
func scanModeReport(buf []byte, mode int) (ModeReport, int, bool) {
	for at := 0; ; {
		seq, ok := nextCSI(buf, at)
		if !ok {
			return ModeReport{}, 0, false
		}
		at = seq.end
		if seq.final != 'y' || seq.inter != "$" || seq.private != '?' {
			continue
		}
		reported, setting, ok := twoParams(seq.params)
		if !ok || reported != mode {
			continue
		}
		return ModeReport{Mode: reported, Setting: setting}, seq.end, true
	}
}

// twoParams reads the "a;b" parameter pair both reports carry.
func twoParams(params string) (int, int, bool) {
	head, tail, found := strings.Cut(params, ";")
	if !found {
		return 0, 0, false
	}
	first, err := strconv.Atoi(head)
	if err != nil {
		return 0, 0, false
	}
	second, err := strconv.Atoi(tail)
	if err != nil {
		return 0, 0, false
	}
	return first, second, true
}

// ----------------------------------------------------------------------------
// What the painter believes
// ----------------------------------------------------------------------------

// xtermCaps mirrors ultraviolet's own xtermCaps (terminal_renderer.go:1590) — the function that
// turns TERM into the set of sequences the renderer will emit. It is duplicated rather than
// imported because it is unexported there, and it is duplicated EXACTLY because the capability
// section's whole claim is "this is what the painter believes about your terminal". An unset TERM
// matches no case and yields zero capabilities, which is the Windows default and the finding the
// section exists to make visible.
func xtermCaps(termType string) int {
	parts := strings.Split(termType, "-")
	if len(parts) == 0 {
		return 0
	}
	switch parts[0] {
	case "contour", "foot", "ghostty", "kitty", "rio", "st", "tmux", "wezterm":
		return allCaps
	case "xterm":
		if len(parts) > 1 && (parts[1] == "ghostty" || parts[1] == "kitty" || parts[1] == "rio") {
			return allCaps
		}
		return allCaps &^ capHPA &^ capCHT &^ capREP
	case "alacritty":
		return allCaps &^ capCHT
	case "screen":
		return allCaps &^ capREP
	case "linux":
		return capVPA | capCHA | capHPA | capECH | capICH
	}
	return 0
}

// capsLine names the capabilities a TERM grants the painter, so the header states the input to the
// capability section before the section argues with it.
func capsLine(caps int) string {
	names := []struct {
		bit  int
		name string
	}{
		{capVPA, "VPA"}, {capHPA, "HPA"}, {capCHA, "CHA"}, {capCHT, "CHT"}, {capCBT, "CBT"},
		{capREP, "REP"}, {capECH, "ECH"}, {capICH, "ICH"}, {capSD, "SD"}, {capSU, "SU"},
	}
	granted := make([]string, 0, len(names))
	for _, n := range names {
		if caps&n.bit != 0 {
			granted = append(granted, n.name)
		}
	}
	if len(granted) == 0 {
		return "none — the painter has no absolute column addressing and counts relative moves"
	}
	return strings.Join(granted, " ")
}

// ----------------------------------------------------------------------------
// Small renderings
// ----------------------------------------------------------------------------

// modeSettingName spells a DECRQM answer. The word is what a reader needs; the value is what the
// specification and every upstream issue quote, so both are printed.
func modeSettingName(setting int) string {
	switch setting {
	case modeNotRecognized:
		return "not recognised (0)"
	case modeSet:
		return "set (1)"
	case modeReset:
		return "reset (2)"
	case modePermanentlySet:
		return "permanently set (3)"
	case modePermanentlyReset:
		return "permanently reset (4)"
	case -1:
		return "no answer"
	}
	return fmt.Sprintf("unknown (%d)", setting)
}

// cellOf renders a cursor report, or says plainly that none came back.
func cellOf(pos CursorPos, ok bool) string {
	if !ok {
		return "no answer"
	}
	return fmt.Sprintf("%d,%d", pos.Row, pos.Col)
}

// stopList renders a run of measured tab stops.
func stopList(stops []int, ok bool) string {
	if !ok && len(stops) == 0 {
		return "no answer"
	}
	if len(stops) == 0 {
		return "none"
	}
	cols := make([]string, len(stops))
	for i, c := range stops {
		cols[i] = strconv.Itoa(c)
	}
	suffix := ""
	if !ok {
		suffix = " (truncated: no answer)"
	}
	return strings.Join(cols, " ") + suffix
}

// sameInts reports whether two runs of columns are identical.
func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// quoteCells renders the first n cells of a row read-back for a report line.
func quoteCells(row string, n int) string {
	runes := []rune(row)
	if len(runes) > n {
		runes = runes[:n]
	}
	return strconv.Quote(string(runes))
}

// yesNo renders a claim the report sets against a measurement.
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
