package main

// T-20 of the v0.17.1 release checklist — the multi-select tick and the width authority — as a test.
//
// The checklist calls both halves manual for the same reason: "`[✔]` (U+2714) and the width authority
// are both claims about what a terminal and its font do ... Neither is visible to a test that measures
// strings." The first half of that is right and the second is what the emulator answers: a Frame's
// cells carry the width the TERMINAL gave each grapheme, so "the tick is one cell" and "the table's
// columns did not move" are claims about cells rather than about rune counts (ADR 0062, call 9). What
// is left over is the font — whether the reader's own font HAS U+2714, or draws tofu — and no driver
// can see that.
//
// The terminal these tests run in answers the mode-2027 (Unicode core) query, because
// unicodeCoreTerminal sets the mode on the emulator before the program is launched. That is not a
// convenience: on a terminal that does not answer it the width authority never moves off wcwidth,
// and the checklist itself says such a terminal cannot regress step 5 and must be recorded as not
// covered rather than as a pass.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The three answers testdata/stubllm/widths.yaml offers, and the prompts that reach its two turns.
const (
	askPrompt   = "Ask me which fruit I want"
	tablePrompt = "Print a small markdown table for me"
	askHint     = "␣ toggle"
)

// TestE2EWidthTicksMultiSelectChoices is T-20 steps 2 to 4: the checkbox column of a multi-select
// question, the tick that goes in it, and the labels that have to stay in one column beside it.
func TestE2EWidthTicksMultiSelectChoices(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "widths"))
	drv := unicodeCoreTerminal(t, e2eSize)
	sess := launchTUI(t, drv, stub)

	// Step 2 — the pane opens with every option unticked, and the three boxes stand in one column.
	drv.WaitText("Send a message")
	submit(drv, askPrompt)
	drv.WaitText(askHint)
	drv.WaitQuiet(settled)
	offered := drv.Frame()
	assertOneColumn(t, offered, "the unticked boxes",
		"[ ]  apples", "[ ]  pears", "[ ]  plums,")
	assertOneColumn(t, offered, "the labels", "apples", "pears", "plums,")

	// Step 3 — ␣ ticks the highlighted row, ↓ moves, ␣ ticks the next. Two rows then read [✔], and
	// the tick is ONE cell wide by the terminal's own measure, which is the whole claim: a glyph the
	// terminal made two cells wide would push every label on that row out of the column above it.
	for _, key := range []tuitest.Key{tuitest.Space, tuitest.Down, tuitest.Space} {
		pressAndRepaint(drv, key)
	}
	ticked := drv.Frame()
	if got := strings.Count(ticked.String(), "[✔]"); got != 2 {
		t.Fatalf("%d rows are ticked after ␣ ↓ ␣; want 2:\n%s", got, ticked)
	}
	if strings.Contains(ticked.String(), "[x]") {
		t.Errorf("the pane ticks with [x] rather than the bracketed tick:\n%s", ticked)
	}
	x, y, ok := ticked.Find("[✔]")
	if !ok {
		t.Fatalf("no ticked row on the frame:\n%s", ticked)
	}
	if cell := ticked.Cell(x+1, y); cell.Rune != "✔" || cell.Width != 1 {
		t.Errorf("the tick cell is %q at width %d; the terminal must measure U+2714 as one cell",
			cell.Rune, cell.Width)
	}

	// Step 4 — the boxes and the labels are each still one column, and the option too long for the
	// pane wraps under its LABEL rather than under the box beside it (popupRowHangingIndent).
	assertOneColumn(t, ticked, "the boxes after ticking",
		"[✔]  apples", "[✔]  pears", "[ ]  plums,")
	assertOneColumn(t, ticked, "the labels and the wrapped row",
		"apples", "pears", "plums,", "late summer")

	// And answering it sends what was ticked.
	pressAndRepaint(drv, tuitest.Enter)
	drv.WaitText("Noted.")
	if answer := lastToolResult(t, stub); !strings.Contains(answer, "apples") ||
		!strings.Contains(answer, "pears") || strings.Contains(answer, "plums") {
		t.Errorf("the answer handed back is %q; want the two ticked options and no third", answer)
	}

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestE2EWidthSurvivesAColourSchemeSwitch is T-20 steps 5 to 7: a reply carrying wide runes and a
// VS16 emoji is laid out, the colour scheme is switched LIVE through the settings pane, and the
// layout has to land in exactly the columns it was in.
//
// The measure is what is under test. Before the switch the run is on the grapheme measure, because
// the terminal answered mode 2027 — asserted from the --tui-diag log, since a run that never got the
// answer could not regress and a green test would then mean nothing. A theme rebuild that dropped
// the authority back to wcwidth would re-measure `⚠️` as one cell and every column right of it would
// move.
func TestE2EWidthSurvivesAColourSchemeSwitch(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "widths"))
	drv := unicodeCoreTerminal(t, e2eSize)
	diag := filepath.Join(t.TempDir(), "tui-diag.txt")
	sess := launchTUI(t, drv, stub, "--tui-diag", diag)

	// Step 5 — the table, and the columns it landed in.
	drv.WaitText("Send a message")
	submit(drv, tablePrompt)
	drv.WaitText("both at once")
	drv.WaitQuiet(settled)
	before := tableColumns(t, drv.Frame())
	assertUnicodeCore(t, diag)

	// Step 6 — the live switch, through the pane a human would use.
	setColourScheme(t, drv, "light")
	drv.WaitText("both at once")
	drv.WaitQuiet(settled)
	after := tableColumns(t, drv.Frame())
	if before != after {
		t.Errorf("the wide-rune layout moved across the scheme switch:\n before %+v\n after  %+v\n%s",
			before, after, drv.Frame())
	}

	// Step 7 — and back, so the run ends in the palette it started in.
	setColourScheme(t, drv, "dark")
	drv.WaitText("both at once")
	drv.WaitQuiet(settled)
	if back := tableColumns(t, drv.Frame()); back != before {
		t.Errorf("the layout moved on the way back to dark:\n before %+v\n back   %+v", before, back)
	}

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// ----------------------------------------------------------------------------
// A terminal that measures in grapheme clusters
// ----------------------------------------------------------------------------

// unicodeCoreTerminal is a driver whose emulator has mode 2027 (Unicode core) SET before anything is
// painted into it — a Ghostty or a recent kitty rather than an Apple Terminal.
//
// It is set by writing the sequence a program would set it with, which is the only way a terminal is
// ever configured and needs no seam of its own. Two things follow from it, and the test needs both:
// the emulator measures graphemes the way it measures them, and it answers bubbletea's start-up
// DECRQM with "set" — which is what moves the PAINTER, and with it apogee's own width authority
// (internal/tui/width.go), off wcwidth.
func unicodeCoreTerminal(t *testing.T, size tuitest.Size) *tuitest.Driver {
	t.Helper()

	drv := tuitest.NewDriver(t, size)
	if _, err := drv.Screen().Write([]byte("\x1b[?2027h")); err != nil {
		t.Fatalf("put the terminal into Unicode-core mode: %v", err)
	}
	return drv
}

// assertUnicodeCore reads the run's own --tui-diag log back and pins the two lines that say the
// terminal was asked and answered: the mode report, and the measure apogee took from it. Without
// them the scheme-switch assertion above would be comparing two wcwidth layouts, which cannot
// disagree and so cannot fail.
func assertUnicodeCore(t *testing.T, path string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the --tui-diag log: %v", err)
	}
	log := string(data)
	if !strings.Contains(log, "mode 2027: 1 (set)") {
		t.Fatalf("the terminal did not answer the Unicode-core query as set; the width authority "+
			"cannot have moved and this test cannot regress:\n%s", log)
	}
	if !strings.Contains(log, "width-method: grapheme") {
		t.Fatalf("apogee did not take the grapheme measure from the answer:\n%s", log)
	}
}

// ----------------------------------------------------------------------------
// Reading a laid-out frame
// ----------------------------------------------------------------------------

// tableGeometry is where a rendered markdown table landed: the columns its rules cross the column
// dividers at, and the column the transcript's scroll rail stands in. It is a value so that "the
// layout did not move" is one comparison rather than a loop over two slices.
type tableGeometry struct {
	crossings string // the ┼ columns of the first rule row, joined
	ruleEnd   int    // the column the rule itself ends at
	rail      int    // the column the transcript's scroll rail stands in
}

// tableColumns measures the table on a frame. The rule row is the one the table's own crossings are
// on, so a single row carries every horizontal fact the claim is about; the rail is read off the same
// row, which is what makes "the bar is still flush" a claim about the same instant.
func tableColumns(t *testing.T, f tuitest.Frame) tableGeometry {
	t.Helper()

	y := rowIndexContaining(t, f, "┼")
	crossings := columnsOf(f, y, "┼")
	if len(crossings) != 2 {
		t.Fatalf("the table rule crosses %d dividers; the fixture's table has two:\n%s", len(crossings), f)
	}
	end := lastColumnOf(f, y, "─")
	rail := lastVisibleColumn(f, y)
	if rail <= end {
		t.Fatalf("no scroll rail right of the table on row %d; the transcript did not overflow:\n%s", y, f)
	}
	return tableGeometry{crossings: joinColumns(crossings), ruleEnd: end, rail: rail}
}

// columnsOf is every column of row y whose cell is glyph.
func columnsOf(f tuitest.Frame, y int, glyph string) []int {
	var cols []int
	for x := range f.Width() {
		if f.Cell(x, y).Rune == glyph {
			cols = append(cols, x)
		}
	}
	return cols
}

// lastColumnOf is the rightmost column of row y whose cell is glyph, or −1.
func lastColumnOf(f tuitest.Frame, y int, glyph string) int {
	cols := columnsOf(f, y, glyph)
	if len(cols) == 0 {
		return -1
	}
	return cols[len(cols)-1]
}

// lastVisibleColumn is the rightmost column of row y carrying anything but a blank.
func lastVisibleColumn(f tuitest.Frame, y int) int {
	for x := f.Width() - 1; x >= 0; x-- {
		if rune := f.Cell(x, y).Rune; rune != "" && rune != " " {
			return x
		}
	}
	return -1
}

// joinColumns spells a column list for a comparison and for a failure message.
func joinColumns(xs []int) string {
	parts := make([]string, 0, len(xs))
	for _, x := range xs {
		parts = append(parts, strconv.Itoa(x))
	}
	return strings.Join(parts, ",")
}

// assertOneColumn is the alignment claim, made the way alignment is actually read: every one of the
// named strings starts in the same terminal COLUMN. Each has to be unique on the frame, since a
// column is looked up by finding it.
func assertOneColumn(t *testing.T, f tuitest.Frame, what string, texts ...string) {
	t.Helper()

	want, _, ok := f.Find(texts[0])
	if !ok {
		t.Fatalf("%s: %q is not on the frame:\n%s", what, texts[0], f)
	}
	for _, text := range texts[1:] {
		got, _, ok := f.Find(text)
		if !ok {
			t.Fatalf("%s: %q is not on the frame:\n%s", what, text, f)
		}
		if got != want {
			t.Errorf("%s: %q starts in column %d and %q in column %d:\n%s",
				what, text, got, texts[0], want, f)
		}
	}
}

// pressAndRepaint sends a key and waits for the paint it caused. A WaitQuiet taken straight after a
// keypress is not that wait: the screen has been quiet since before the key was sent, so the check
// passes on a frame the program has not answered yet — the trap [tuitest.Driver.Resize] documents,
// in the form a key press takes.
func pressAndRepaint(drv *tuitest.Driver, key tuitest.Key) {
	painted := drv.Screen().BytesWritten()
	drv.Press(key)
	awaitRepaint(drv, painted)
	drv.WaitQuiet(settled)
}

// setColourScheme performs the checklist's own route: /settings → the `ui.color-scheme` row → the
// value → esc. It is the route rather than the /color-scheme verb deliberately — the verb goes
// through the same validate-persist-apply seam (internal/tui/colorscheme.go says so), and the pane is
// what the step describes a human doing.
func setColourScheme(t *testing.T, drv *tuitest.Driver, name string) {
	t.Helper()

	submit(drv, "/settings")
	drv.WaitText("Upstream")
	settingsGoTo(t, drv, settingKeyColorScheme)
	pressAndRepaint(drv, tuitest.Enter)
	// A ceiling on the walk, not an expectation: the schemes folder decides how many values there are.
	const maxValues = 20
	for range maxValues {
		if strings.HasPrefix(paneCursor(drv), "❯ "+name) {
			break
		}
		pressAndRepaint(drv, tuitest.Down)
	}
	if got := paneCursor(drv); !strings.HasPrefix(got, "❯ "+name) {
		t.Fatalf("the scheme sub-list never highlighted %q (it stopped on %q)", name, got)
	}
	pressAndRepaint(drv, tuitest.Enter)
	closePane(drv, settingsHint)
}

// paneCursor is the row a short popup list highlights, found from the BOTTOM of the frame up.
// [settingsCursor] scans from the top, which is right for the full-height key list — nothing else is
// on the screen then — and wrong for a value sub-list, where the transcript above it is visible and a
// prompt row starts with the very same ❯.
func paneCursor(drv *tuitest.Driver) string {
	rows := drv.Frame().Rows()
	for y := len(rows) - 1; y >= 0; y-- {
		if i := strings.Index(rows[y], "❯ "); i >= 0 {
			return strings.TrimSpace(strings.Trim(rows[y][i:], "│┃ "))
		}
	}
	return ""
}

// settingKeyColorScheme is the registry path of the row the switch is made from — spelled here for
// the reason cmd/apogee spells the other two it has to recognise (settingsrows.go): a row that has to
// be found at all is found by its path.
const settingKeyColorScheme = "ui.color-scheme"
