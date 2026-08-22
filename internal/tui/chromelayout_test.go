package tui

import (
	"math/rand"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/airiclenz/apogee/internal/scheme"
)

// ----------------------------------------------------------------------------
// inputContentRows sizes the prompt box to what the textarea actually draws
// ----------------------------------------------------------------------------

// TestInputContentRows pins the box-sizing count against the textarea's own wrap, including the
// edge that used to under-count: a logical line whose final wrapped segment exactly fills the
// width takes one extra visual row (the widget reserves a trailing row for the caret past a full
// line). Under-counting it left the box a row short at the wrap boundary, stranding the scroll the
// layout re-seat then could not clamp (ISSUES #2).
//
// The CR cases carry the numbers the widget itself answers, spelled out rather than compared: the
// sanitizer rewrites each '\r' AND each '\n' as one newline before the split, so a bare CR opens a
// row and a CRLF opens two. Reading them as one boundary — the intuitive line ending — would put
// this table one row under the widget for every CRLF, which is the failure the count already had.
func TestInputContentRows(t *testing.T) {
	const w = 10
	cases := []struct {
		name  string
		value string
		want  int
	}{
		{"empty is one row", "", 1},
		{"short line", "abc", 1},
		{"one under the width", strings.Repeat("a", 9), 1},
		{"exact width gains a trailing row", strings.Repeat("a", 10), 2},
		{"one over the width", strings.Repeat("a", 11), 2},
		{"two full widths", strings.Repeat("a", 20), 3},
		{"two full widths plus one", strings.Repeat("a", 21), 3},
		{"trailing newline adds a row", "abc\n", 2},
		{"two logical lines", "abc\ndef", 2},
		{"a bare CR is a row boundary too", "abc\rdef", 2},
		{"trailing CR adds a row", "abc\r", 2},
		{"CRLF is two boundaries, one per rune", "abc\r\ndef", 3},
		{"each full logical line gets its trailing row", strings.Repeat("a", 10) + "\n" + strings.Repeat("b", 10), 4},
		{"wide glyphs count by display cells", strings.Repeat("あ", 5), 2}, // 5×2 = 10 cells = exact width
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := inputContentRows(c.value, w); got != c.want {
				t.Errorf("inputContentRows(%q, %d) = %d, want %d", c.value, w, got, c.want)
			}
		})
	}
}

// A zero or negative width floors to one column rather than dividing by zero, and still returns at
// least one row.
func TestInputContentRowsZeroWidth(t *testing.T) {
	if got := inputContentRows("ab", 0); got < 1 {
		t.Errorf("inputContentRows with zero width = %d, want >= 1 (width floored to one)", got)
	}
}

// widgetContentRows is the row count a REAL textarea draws for value at width, and the effective
// text width it settled on. It is the oracle inputContentRows mirrors, read straight off the widget
// rather than re-derived: DynamicHeight makes the textarea publish its own totalVisualLines as its
// height (bubbles/v2@v2.1.0/textarea/textarea.go:1666-1692), and MaxHeight is cleared so nothing
// clamps that answer. It is the whole-value counterpart of the per-line LineInfo.Height oracle
// TestWrapRowStartsMirrorsTheWidget pins wrapRowStarts to — the same widget, asked the same
// question the box-sizing path asks.
func widgetContentRows(t *testing.T, value string, width int) (rows, effWidth int) {
	t.Helper()
	ta := textarea.New()
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.MaxHeight = 0 // uncapped: the height it reports is then its raw visual-line total
	ta.MinHeight = 1
	ta.DynamicHeight = true
	ta.SetWidth(width)
	ta.SetValue(value)
	return ta.Height(), ta.Width()
}

// TestInputContentRowsMirrorsTheWidget pins the box-sizing count to the widget itself, which the
// old Wordwrap+Hardwrap approximation was not: it under-counted "hello world" at width 5 (four
// widget rows, it said three) and "a b  c" at width 3 (three, it said two), and over-counted
// "a-b-c-d" at width 3 (three, it said four). The count now delegates to wrapRowStarts, so the box's
// height and the rows the accent pass paints on come off one ruler.
//
// Tabs are in the table because the count now sanitises each line the way the widget's own sanitizer
// did (sanitizeInputLine, inputaccent.go): the oracle sets the raw value on a real textarea, which
// keeps four spaces per tab, so a mirror still measuring the tab as written would come up short here.
// The runes that sanitizer DROPS — utf8.RuneError and the other control runes — are in the table for
// the mirror image of that reason: the textarea keeps none of them, so a mirror that measured one
// would come up long.
//
// The CR cases are the third face of the same sanitizer: '\r' is neither kept nor dropped but
// rewritten as a newline, one per rune, so the widget opens a row on a bare CR and two on a CRLF.
// The mirror split on '\n' alone until 2026-08-14 and came up a row short for either; asking the
// real widget is what settles that CRLF is two rows here and not the one a line ending suggests.
func TestInputContentRowsMirrorsTheWidget(t *testing.T) {
	cases := []struct {
		name  string
		value string
		width int
	}{
		// The three the follow-up finding named, each a concrete failure of the old count.
		{"word-wrapped prose", "hello world", 5},
		{"a double space between words", "a b  c", 3},
		{"a hyphen run", "a-b-c-d", 3},

		{"empty", "", 10},
		{"short line", "abc", 10},
		{"one under the width", strings.Repeat("a", 9), 10},
		{"exact width", strings.Repeat("a", 10), 10},
		{"one over the width", strings.Repeat("a", 11), 10},
		{"two full widths", strings.Repeat("a", 20), 10},
		{"trailing newline", "abc\n", 10},
		{"two logical lines", "abc\ndef", 10},
		{"two full logical lines", strings.Repeat("a", 10) + "\n" + strings.Repeat("b", 10), 10},
		{"a blank line between two", "abc\n\ndef", 10},
		{"a bare CR", "abc\rdef", 10},
		{"a trailing CR", "abc\r", 10},
		{"a leading CR", "\rabc", 10},
		{"a CRLF pair", "abc\r\ndef", 10},
		{"CR between two width-filling lines", strings.Repeat("a", 10) + "\r" + strings.Repeat("b", 10), 10},
		{"a CR inside a wrapped word", "averyvery\rlongwordindeed", 6},
		{"a line of nothing but spaces", "     ", 3},
		{"trailing space at a row boundary", "aaa aaa aaa aaax ", 8},
		{"a word longer than the row", "averyveryverylongwordindeed", 6},
		{"wide glyphs count by display cells", strings.Repeat("あ", 5), 10},
		{"wide runes wrapping mid-word", "日本語のテキスト 絵文字", 7},
		{"an emoji carrying VS16", "warn ⚠️ here", 7},
		{"a VS16 run filling the row", "⚠️⚠️⚠️ end", 6},
		{"VS16 inside a word too wide for the row", "aa⚠️bb⚠️cc", 4},
		{"a leading tab", "\tabc def", 6},
		{"a tab inside a word", "ab\tcd efgh", 6},
		{"a tab at the wrap column", "abcd\tefg", 6},
		{"a line of nothing but tabs", "\t\t", 5},
		{"a tab on the second logical line", "abc\n\tdef ghi", 6},
		{"a replacement character and a control rune", "ab\uFFFDcd\x07ef gh", 5},
		{"a realistic draft", "/grill-me check @internal/tui/model.go and /code-adit", 20},
		{"a multi-line draft", "fix the wrap bug\n\nsee @internal/tui/render.go — the mirror under-counts", 24},
		{"one column", "ab cd", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want, w := widgetContentRows(t, c.value, c.width)
			if got := inputContentRows(c.value, w); got != want {
				t.Errorf("inputContentRows(%q, %d) = %d, the widget draws %d rows", c.value, w, got, want)
			}
		})
	}
}

// The same oracle over a spread of generated prompt-shaped drafts, which is what turns "the three
// named cases agree" into "the mirror is faithful": the old count differed from the widget on
// roughly 41% of inputs like these, so any regression to an approximation fails here loudly rather
// than on one lucky fixture. Deterministic — a fixed seed, so a failure is reproducible.
func TestInputContentRowsMirrorsTheWidgetOnGeneratedDrafts(t *testing.T) {
	// The alphabet is chosen for the boundaries the two mirrors disagreed at: spaces (the widget's
	// word/space grouping), hyphens (a breakpoint to ansi.Wordwrap but not to the widget), a wide
	// rune and a VS16 cluster (grapheme-vs-rune measurement), newlines (logical lines), and tabs
	// (the widget's sanitizer expands each into four spaces before it wraps).
	glyphs := []string{"a", "b", "c", " ", " ", "-", "@", "/", "あ", "⚠️", "\n", "\t"}
	rng := rand.New(rand.NewSource(20260731))
	for _, width := range []int{1, 2, 3, 5, 8, 13, 40} {
		for i := 0; i < 300; i++ {
			var sb strings.Builder
			for n := rng.Intn(24); n > 0; n-- {
				sb.WriteString(glyphs[rng.Intn(len(glyphs))])
			}
			value := sb.String()
			want, w := widgetContentRows(t, value, width)
			if got := inputContentRows(value, w); got != want {
				t.Fatalf("inputContentRows(%q, %d) = %d, the widget draws %d rows", value, w, got, want)
			}
		}
	}
}

// The clamp above the count still holds now that it can report MORE rows than the old
// approximation: the box never grows past maxInputRows (past it the widget scrolls internally) and
// never shrinks below minInputRows, whatever the mirror returns. inputContentRows itself stays
// unclamped — layout() derives the viewport's height from the clamped box height, not from this.
func TestPromptEditorRowsClampsTheWidgetCount(t *testing.T) {
	const width = 8
	cases := []string{
		"",
		"short",
		strings.Repeat("a", width*maxInputRows*2),   // one very long soft-wrapped line
		strings.Repeat("word ", width*maxInputRows), // many wrapped words
		strings.Repeat("line\n", maxInputRows*3),    // many logical lines
		strings.Repeat("⚠️", width*maxInputRows),    // VS16 clusters, the widest measure gap
		strings.Repeat("a", width) + "\n" + "b",     // an exact width fill plus a line
	}
	for _, value := range cases {
		e := newPromptEditor(defaultCursorShape, lipgloss.Color(scheme.Default().Surface))
		e.input.SetValue(value)
		raw := inputContentRows(e.input.Value(), width) // what the editor holds, not what was handed to it
		got := e.rows(width)
		if got < minInputRows || got > maxInputRows {
			t.Fatalf("rows(%q) = %d, outside [%d, %d] (unclamped count %d)", value, got, minInputRows, maxInputRows, raw)
		}
		if want := clampInt(raw, minInputRows, maxInputRows); got != want {
			t.Errorf("rows(%q) = %d, want %d (clamp of the unclamped count %d)", value, got, want, raw)
		}
	}
}

// ----------------------------------------------------------------------------
// The frame publishes BOTH overlay slots' geometry (model.go, frameSpans)
// ----------------------------------------------------------------------------

// frameBlockRow reports the screen row m.View() draws block's first line on, or −1 when the composed
// frame does not draw it at all. It FINDS the block in the frame — matching the block's own lines
// against the frame's — rather than re-deriving the row arithmetic the assertions below exist to
// check: a test that recomputed the sum would agree with a wrong sum exactly as happily as with a
// right one.
func frameBlockRow(t *testing.T, m Model, block string) int {
	t.Helper()
	if block == "" {
		return -1
	}
	frame := strings.Split(plain(m.View()), "\n")
	want := strings.Split(ansiPattern.ReplaceAllString(block, ""), "\n")
	for y := 0; y+len(want) <= len(frame); y++ {
		drawn := true
		for i, line := range want {
			// The frame squares every line to the window width (joinFrame), so a drawn line can carry
			// padding the block itself does not.
			if strings.TrimRight(frame[y+i], " ") != strings.TrimRight(line, " ") {
				drawn = false
				break
			}
		}
		if drawn {
			return y
		}
	}
	return -1
}

// dropdownFrame is a laid-out 100×30 model with the "/" command menu open over a full transcript and
// staged interjections queued behind it — the state in which the lower slot has to take its rows from
// something already occupying them.
func dropdownFrame(t *testing.T, staged int) Model {
	t.Helper()
	m := withStagedRows(modelWithOverlayRoomAt(t, 100, 30, Options{Workspace: "."}), staged)
	m.input.SetValue("/")
	m.autocomplete = m.computeAutocomplete(m.caretByteOffset())
	m.layout()
	if !m.autocomplete.active || len(m.autocomplete.items) == 0 {
		t.Fatal(`the "/" menu did not open — test premise broken`)
	}
	return m
}

// TestDropdownSpanMatchesTheDrawnFrame is what publishing the input-side slot's geometry buys: the
// rectangle a pointer asks for the autocomplete dropdown is the rectangle the frame actually drew it
// in. It was the one open overlay with no geometry at all — a notch or a click over it could name
// nothing — and its row now comes down from the walk that stacked it (stackInputSlot), off the row the
// transcript-side slot reported it ended above, rather than from a second sum over the same blocks.
func TestDropdownSpanMatchesTheDrawnFrame(t *testing.T) {
	m := dropdownFrame(t, 0)

	y0, h, ok := m.frameSpans().pane(paneDropdown)

	if !ok {
		t.Fatal("the open dropdown is not on the published frame")
	}
	block := m.frameOverlays().dropdown
	if want := frameBlockRow(t, m, block); y0 != want {
		t.Errorf("the dropdown's span starts at row %d, the frame draws it at row %d", y0, want)
	}
	if want := lipgloss.Height(block); h != want {
		t.Errorf("the dropdown's span is %d rows, the frame draws %d", h, want)
	}
}

// The staged-interjection strip shares the lower slot and is stacked BELOW the dropdown — it is what
// ⏎ just put there, closest to the box (ADR 0025) — so it may not disturb the rectangle the dropdown
// publishes. The strip is no framePane and has no span of its own; what pins it is where the frame
// draws it, directly on the row the dropdown's rectangle ends.
func TestDropdownSpanWithAStagedStripBelowIt(t *testing.T) {
	const staged = 2
	m := dropdownFrame(t, staged)

	y0, h, ok := m.frameSpans().pane(paneDropdown)

	if !ok {
		t.Fatal("the open dropdown is not on the published frame beside the staged strip")
	}
	ov := m.frameOverlays()
	if ov.queued == "" {
		t.Fatalf("%d staged interjections drew no strip — test premise broken", staged)
	}
	if want := frameBlockRow(t, m, ov.dropdown); y0 != want {
		t.Errorf("beside the strip the dropdown's span starts at row %d, the frame draws it at row %d", y0, want)
	}
	if want := lipgloss.Height(ov.dropdown); h != want {
		t.Errorf("beside the strip the dropdown's span is %d rows, the frame draws %d", h, want)
	}
	if got := frameBlockRow(t, m, ov.queued); got != y0+h {
		t.Errorf("the staged strip is drawn at row %d, want %d — directly below the dropdown's %d rows",
			got, y0+h, h)
	}
}

// A closed dropdown answers what every closed pane answers: the zero span, meaning it is not on this
// frame. That is the whole of what the entry means now — it no longer doubles as "nothing addresses
// this pane by rectangle".
func TestDropdownSpanIsAbsentWithNoMenuOpen(t *testing.T) {
	m := modelWithOverlayRoomAt(t, 100, 30, Options{Workspace: "."})

	if y0, h, ok := m.frameSpans().pane(paneDropdown); ok {
		t.Errorf("a closed dropdown reports a rectangle at row %d, %d rows tall", y0, h)
	}
}

// TestFrameSpansKeepTheTranscriptSlotWhereItIsDrawn is the other half of the bargain: composing the
// lower slot may not move the upper one. Every pane of the transcript-side slot still publishes the
// row the frame draws it on — asserted against the drawn frame rather than against a recorded number,
// so the walk above the chrome stays pinned to the painter whatever is stacked below it.
func TestFrameSpansKeepTheTranscriptSlotWhereItIsDrawn(t *testing.T) {
	cases := []struct {
		name string
		pane framePane
		open func(Model) Model
	}{
		{
			name: "sessions browser",
			pane: paneBrowser,
			open: func(m Model) Model { m.sessionBrowser = sessionBrowser{open: true}; return m },
		},
		{
			name: "model picker",
			pane: panePicker,
			open: func(m Model) Model { m.picker = picker{open: true, kind: pickerModel}; return m },
		},
		{
			name: "settings pane",
			pane: paneSettings,
			open: func(m Model) Model { m.settings.open = true; return m },
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := c.open(modelWithOverlayRoomAt(t, 100, 30, Options{Workspace: "."}))
			block := m.frameOverlays().block(c.pane)
			if block == "" {
				t.Fatalf("the %s drew nothing — test premise broken", c.name)
			}

			y0, h, ok := m.frameSpans().pane(c.pane)

			if !ok {
				t.Fatalf("the open %s is not on the published frame", c.name)
			}
			if want := frameBlockRow(t, m, block); y0 != want {
				t.Errorf("the %s's span starts at row %d, the frame draws it at row %d", c.name, y0, want)
			}
			if want := lipgloss.Height(block); h != want {
				t.Errorf("the %s's span is %d rows, the frame draws %d", c.name, h, want)
			}
		})
	}
}
