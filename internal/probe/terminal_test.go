package probe

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"
)

// The response parser is the probe's testable core, and the cases below are the ones a real
// terminal produces that a naive parser gets wrong: a reply that arrives split across two reads, a
// reply preceded by a keystroke the user happened to press, and a reply preceded by the ANSWER to
// a different query. Every measurement in the probe rests on this, so a mis-parse would not fail
// loudly — it would report a confident wrong number.
func TestTerminalScanCursorPos(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		in       string
		wantPos  CursorPos
		wantN    int
		wantFind bool
	}{
		{name: "a clean report", in: "\x1b[12;34R", wantPos: CursorPos{12, 34}, wantN: 8, wantFind: true},
		{name: "at the origin", in: "\x1b[1;1R", wantPos: CursorPos{1, 1}, wantN: 6, wantFind: true},
		{
			name: "a keystroke arrived first", in: "q\x1b[3;5R",
			wantPos: CursorPos{3, 5}, wantN: 7, wantFind: true,
		},
		{
			name: "a function key arrived first", in: "\x1bOP\x1b[2;3R",
			wantPos: CursorPos{2, 3}, wantN: 9, wantFind: true,
		},
		{
			name: "a mode report arrived first", in: "\x1b[?2027;3$y\x1b[5;6R",
			wantPos: CursorPos{5, 6}, wantN: 17, wantFind: true,
		},
		{
			name: "DECXCPR spells it with a private marker", in: "\x1b[?3;7R",
			wantPos: CursorPos{3, 7}, wantN: 7, wantFind: true,
		},
		{name: "nothing but noise", in: "hello"},
		{name: "an escape and nothing else yet", in: "\x1b"},
		{name: "a CSI introducer and nothing else yet", in: "\x1b["},
		{name: "cut off mid-parameters", in: "\x1b[12;"},
		{name: "cut off before the final byte", in: "\x1b[12;34"},
		{name: "a report with no column", in: "\x1b[R"},
		{name: "a report the terminal has not finished after a keystroke", in: "abc\x1b[9;"},
		{name: "a zero row is not a position", in: "\x1b[0;5R"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pos, n, found := scanCursorPos([]byte(tc.in))
			if found != tc.wantFind {
				t.Fatalf("scanCursorPos(%q) found = %v; want %v", tc.in, found, tc.wantFind)
			}
			if !tc.wantFind {
				if n != 0 {
					t.Errorf("scanCursorPos(%q) consumed %d bytes without a report; a reply still "+
						"arriving must not be discarded", tc.in, n)
				}
				return
			}
			if pos != tc.wantPos || n != tc.wantN {
				t.Errorf("scanCursorPos(%q) = %v, %d; want %v, %d", tc.in, pos, n, tc.wantPos, tc.wantN)
			}
		})
	}
}

// A reply that arrives in pieces must be waited for, never discarded — the whole reason scan
// reports "nothing yet" rather than consuming what it could not parse. This walks one report in
// one byte at a time, which is the shape a slow terminal (or a busy pty) actually delivers.
func TestTerminalScanCursorPosSurvivesAByteAtATime(t *testing.T) {
	t.Parallel()
	const reply = "\x1b[24;80R"
	var buf []byte
	for i := range len(reply) - 1 {
		buf = append(buf, reply[i])
		if _, _, found := scanCursorPos(buf); found {
			t.Fatalf("scanCursorPos matched after %d of %d bytes", i+1, len(reply))
		}
	}
	buf = append(buf, reply[len(reply)-1])
	pos, n, found := scanCursorPos(buf)
	if !found || pos != (CursorPos{24, 80}) || n != len(reply) {
		t.Errorf("the completed reply parsed as %v, %d, %v; want {24 80}, %d, true", pos, n, found, len(reply))
	}
}

// The mode a DECRPM answer is ABOUT is matched rather than assumed. The probe has two queries
// outstanding at start-up (2026 and 2027), so a terminal that answers them out of order would
// otherwise have one mode's setting recorded against the other — and the setting is what decides
// whether the painter measures in grapheme clusters.
func TestTerminalScanModeReport(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		in       string
		mode     int
		wantRep  ModeReport
		wantN    int
		wantFind bool
	}{
		{
			name: "permanently set", in: "\x1b[?2027;3$y", mode: 2027,
			wantRep: ModeReport{2027, 3}, wantN: 11, wantFind: true,
		},
		{
			name: "not recognised", in: "\x1b[?2027;0$y", mode: 2027,
			wantRep: ModeReport{2027, 0}, wantN: 11, wantFind: true,
		},
		{
			name: "answered out of order", in: "\x1b[?2026;2$y\x1b[?2027;1$y", mode: 2027,
			wantRep: ModeReport{2027, 1}, wantN: 22, wantFind: true,
		},
		{
			name: "a cursor report arrived first", in: "\x1b[9;9R\x1b[?2027;4$y", mode: 2027,
			wantRep: ModeReport{2027, 4}, wantN: 17, wantFind: true,
		},
		{name: "the other mode never answered", in: "\x1b[?2027;3$y", mode: 2026},
		{name: "cut off before the intermediate", in: "\x1b[?2027;3", mode: 2027},
		{name: "cut off before the final byte", in: "\x1b[?2027;3$", mode: 2027},
		{name: "a cursor report is not a mode report", in: "\x1b[2027;3R", mode: 2027},
		{name: "nothing but noise", in: "not a reply", mode: 2027},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rep, n, found := scanModeReport([]byte(tc.in), tc.mode)
			if found != tc.wantFind {
				t.Fatalf("scanModeReport(%q, %d) found = %v; want %v", tc.in, tc.mode, found, tc.wantFind)
			}
			if !tc.wantFind {
				if n != 0 {
					t.Errorf("scanModeReport(%q, %d) consumed %d bytes without a report", tc.in, tc.mode, n)
				}
				return
			}
			if rep != tc.wantRep || n != tc.wantN {
				t.Errorf("scanModeReport(%q, %d) = %v, %d; want %v, %d", tc.in, tc.mode, rep, n, tc.wantRep, tc.wantN)
			}
		})
	}
}

// The capability section's whole claim is "this is what the painter believes about your terminal",
// so this table is a copy of ultraviolet's xtermCaps and has to stay one. The first row is the
// finding the section exists for: an unset TERM — the normal state of every Windows shell — leaves
// the renderer with no capabilities at all, including no way to address a column absolutely.
func TestTerminalXtermCapsMirrorsTheRenderer(t *testing.T) {
	t.Parallel()
	cases := []struct {
		term string
		want int
	}{
		{term: "", want: 0},
		{term: "dumb", want: 0},
		{term: "xterm", want: allCaps &^ capHPA &^ capCHT &^ capREP},
		{term: "xterm-256color", want: allCaps &^ capHPA &^ capCHT &^ capREP},
		{term: "xterm-ghostty", want: allCaps},
		{term: "xterm-kitty", want: allCaps},
		{term: "ghostty", want: allCaps},
		{term: "wezterm", want: allCaps},
		{term: "tmux-256color", want: allCaps},
		{term: "alacritty", want: allCaps &^ capCHT},
		{term: "screen-256color", want: allCaps &^ capREP},
		{term: "linux", want: capVPA | capCHA | capHPA | capECH | capICH},
	}

	for _, tc := range cases {
		t.Run("TERM="+orUnset(tc.term), func(t *testing.T) {
			t.Parallel()
			if got := xtermCaps(tc.term); got != tc.want {
				t.Errorf("xtermCaps(%q) = %010b; want %010b", tc.term, got, tc.want)
			}
		})
	}
	// The line the header prints for the Windows default has to say what zero capabilities MEAN,
	// because "none" alone reads like a formatting accident rather than the finding it is.
	if line := capsLine(xtermCaps("")); !strings.Contains(line, "no absolute column addressing") {
		t.Errorf("the capability line for an unset TERM does not explain itself: %q", line)
	}
}

// The report is the product, so the verdict has to be legible in it: a flagged row is marked, the
// section says MISMATCH, and the whole run says so once more at the end. Mismatch() is what the
// command turns into its exit status, which is what makes the command checkable by a script.
func TestTerminalReportStatesEveryVerdict(t *testing.T) {
	t.Parallel()
	report := Terminal{
		Width: 120, Height: 29, TERM: "",
		Sections: []TerminalSection{
			{
				Title: "mode negotiation", Head: []string{"mode", "reported"},
				Rows:    [][]string{{"2026", "permanently set (3)"}},
				Summary: "mode 2027 reads back as set",
			},
			{
				Title: "last-column wrap", Head: []string{"step", "cursor"},
				Rows:     [][]string{{"wrote the final column", "7,1"}},
				Flagged:  []bool{true},
				Summary:  "the terminal WRAPPED IMMEDIATELY on the last column",
				Mismatch: true,
			},
		},
	}

	if !report.Mismatch() {
		t.Fatal("a report carrying a mismatching section does not report a mismatch")
	}
	text := report.Report()
	for _, want := range []string{
		"apogee probe terminal",
		"120 columns × 29 rows",
		"(unset)",
		"OK — mode 2027 reads back as set",
		"MISMATCH — the terminal WRAPPED IMMEDIATELY on the last column",
		"  ! wrote the final column",
		"MISMATCH — at least one section disagrees",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the report does not state %q:\n%s", want, text)
		}
	}

	// The same report with nothing flagged must read as a clean bill, or a passing run is
	// indistinguishable from a failing one at a glance.
	clean := Terminal{Width: 120, Height: 29, TERM: "xterm-256color", Sections: report.Sections[:1]}
	if clean.Mismatch() {
		t.Error("a report with no flagged section reports a mismatch")
	}
	if !strings.Contains(clean.Report(), "OK — every section agrees") {
		t.Errorf("a clean report does not say so:\n%s", clean.Report())
	}
}

// A terminal too small to measure on is refused with the size it has and the size it needs, and
// the refusal counts as a mismatch — the question was not answered, and a command whose exit
// status is its verdict may not exit 0 on a run that measured nothing.
func TestGatherTerminalRefusesATerminalTooSmallToMeasureOn(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	report := GatherTerminal(TerminalInputs{
		Out:   &out,
		Read:  func(time.Duration) []byte { return nil },
		Width: 20, Height: 4,
	})

	if report.Aborted == "" {
		t.Fatal("a 20×4 terminal was accepted")
	}
	if !report.Mismatch() {
		t.Error("an aborted run does not report a mismatch, so the command would exit 0")
	}
	if !strings.Contains(report.Report(), "ABORTED") || !strings.Contains(report.Report(), "20×4") {
		t.Errorf("the refusal does not state the size it found:\n%s", report.Report())
	}
	if out.Len() != 0 {
		t.Errorf("a refused run still wrote %q to the terminal", out.String())
	}
}

// A terminal that never answers DSR-CPR cannot be measured at all, and saying so ONCE is the
// point: every section reads the cursor back, so without the refusal the report would be five
// tables of "no answer" that read like five findings. The screen must still be restored — the
// probe took the alternate screen before it discovered it had nothing to talk to.
func TestGatherTerminalAbortsOnASilentTerminal(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	report := GatherTerminal(TerminalInputs{
		Out:     &out,
		Read:    func(time.Duration) []byte { return nil },
		Width:   120,
		Height:  29,
		Timeout: time.Millisecond,
	})

	if !strings.Contains(report.Aborted, "DSR-CPR") {
		t.Fatalf("a silent terminal was not reported as silent: %q", report.Aborted)
	}
	if len(report.Sections) != 0 {
		t.Errorf("a silent terminal produced %d measured sections", len(report.Sections))
	}
	written := out.String()
	if !strings.Contains(written, enterAltScreen) || !strings.Contains(written, leaveAltScreen) {
		t.Errorf("the probe did not claim and release the alternate screen: %q", written)
	}
	if !strings.HasSuffix(written, showCursor+leaveAltScreen) {
		t.Errorf("the probe did not leave the screen as it found it: %q", written)
	}
}

// The scripted terminal is the instrument every measurement below is taken with, so it is proved
// first: the two replies the whole probe rests on, in the exact bytes a real terminal sends them
// in. A fake that answered a DECRPM in the wrong shape would make the probe's parser look broken
// from the outside while proving nothing about the terminal.
func TestFakeTerminalAnswersCPRAndDECRQM(t *testing.T) {
	t.Parallel()
	fake := newAgreeingTerminal()

	_, _ = fmt.Fprintf(fake, cursorPosition, 3, 7)
	_, _ = fake.Write([]byte(dsrCPR))

	if got, want := string(fake.Read(time.Millisecond)), csi+"3;7R"; got != want {
		t.Errorf("CSI 6n after CUP 3;7 answered %q; want %q", got, want)
	}

	_, _ = fmt.Fprintf(fake, queryMode, 2027)

	if got, want := string(fake.Read(time.Millisecond)), csi+"?2027;2$y"; got != want {
		t.Errorf("DECRQM for mode 2027 answered %q; want %q", got, want)
	}

	// A write cut mid-sequence is held until the rest arrives, never discarded — the same rule the
	// probe's own reply parser keeps, on the other side of the wire.
	_, _ = fake.Write([]byte(dsrCPR[:len(dsrCPR)-1]))

	if answer := fake.Read(time.Millisecond); answer != nil {
		t.Errorf("a half-written CSI 6n was answered with %q before it finished", answer)
	}

	_, _ = fake.Write([]byte(dsrCPR[len(dsrCPR)-1:]))

	if got, want := string(fake.Read(time.Millisecond)), csi+"3;7R"; got != want {
		t.Errorf("the completed CSI 6n answered %q; want %q", got, want)
	}
}

// The five measuring sections have never been exercised — only the two paths that abort before
// measuring anything were. A terminal scripted to do exactly what the painter assumes is the
// baseline every divergence test is read against: all six sections must come back clean, in order,
// with the wording the report prints, and the screen must be handed back the way it was borrowed.
func TestGatherTerminalMeasuresAnAgreeingTerminal(t *testing.T) {
	t.Parallel()
	fake := newAgreeingTerminal()

	report := GatherTerminal(fake.inputs("xterm-256color"))

	if report.Aborted != "" {
		t.Fatalf("an agreeing terminal was refused: %s", report.Aborted)
	}
	want := []struct{ title, summary string }{
		{"mode negotiation", "mode 2027 reads back as set after CSI ?2027h"},
		{
			"glyph advance (mode 2027 off, terminal reports reset (2))",
			"every glyph advances by the width the painter's wcwidth table predicts",
		},
		{
			"glyph advance (mode 2027 on, terminal reports set (1))",
			"every glyph advances by the width the painter's grapheme table predicts",
		},
		{"hard tabs", "tab stops are every 8 columns and a tab moves without erasing"},
		{
			"last-column wrap",
			"the terminal holds a pending wrap at the last column — the semantics the renderer emits against",
		},
		{"capabilities actually present", "the painter's capability set matches what the terminal actually does"},
	}
	if len(report.Sections) != len(want) {
		t.Fatalf("%d sections were measured; want %d:\n%s", len(report.Sections), len(want), report.Report())
	}

	for i, w := range want {
		section := report.Sections[i]
		if section.Title != w.title {
			t.Errorf("section %d is %q; want %q", i+1, section.Title, w.title)
		}
		if section.Mismatch {
			t.Errorf("section %q flagged an agreeing terminal: %s", section.Title, section.Summary)
		}
		if section.Summary != w.summary {
			t.Errorf("section %q summarised as\n  %q\nwant\n  %q", section.Title, section.Summary, w.summary)
		}
	}
	if report.Mismatch() {
		t.Errorf("an agreeing terminal reported a mismatch:\n%s", report.Report())
	}
	// Whether a tab ERASED what it passed over cannot be answered from a cursor position, and this
	// terminal has no console to read cells out of — so the row has to say so rather than guess.
	const noReadBack = "unverified — no screen read-back on this OS"
	if got := observedFor(t, report.Sections[3], "tab over written cells"); got != noReadBack {
		t.Errorf("the tab-erasure row reads %q; want %q", got, noReadBack)
	}

	written := string(fake.written)
	if !strings.HasSuffix(written, showCursor+leaveAltScreen) {
		t.Errorf("the probe did not leave the screen as it found it: %q", written)
	}
	// The mode was found reset, so it has to be left reset: a probe that reconfigured the terminal
	// it measured would be the one thing a measurement may not do.
	if last, ok := lastMode2027Request(written); !ok || last != resetMode2027 {
		t.Errorf("the last mode-2027 request was %q, %v; want %q — the setting it was found in",
			last, ok, resetMode2027)
	}
}

// observedFor returns the "observed" cell of the row a section labels label, so a test names the
// row it means rather than counting to it.
func observedFor(t *testing.T, section TerminalSection, label string) string {
	t.Helper()
	for _, row := range section.Rows {
		if len(row) > 1 && row[0] == label {
			return row[1]
		}
	}
	t.Fatalf("section %q has no %q row", section.Title, label)
	return ""
}
