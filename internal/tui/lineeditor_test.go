package tui

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	"github.com/airiclenz/apogee/internal/scheme"
)

// The input fold pinned where it is DECIDED rather than where it shows. [lineEditor.flattenLine]
// folds a pasted newline, tab or carriage return to a space before a one-line field can hold one,
// but no in-package door reaches those branches with the character still intact: every write into a
// bubbles textarea runs through the widget's own rune sanitizer first, and it spends a tab as four
// spaces and a carriage return as a newline. The widget-level coverage therefore pins the END STATE
// of that whole pipeline (TestSettingsPasteLandsInTheOpenField), not this substitution — so the
// field's own invariant, the one that must survive that sanitizer being reconfigured or replaced,
// is pinned here directly on the replacer.
//
// One rune for one rune is the half the caret rests on: flattenLine reads the caret as a rune offset,
// substitutes, then seats it back at that same offset ([lineEditor.caretRune] / [lineEditor.caretToRune]),
// which only names what it named if the fold never changes the value's length in runes. A "\r\n" is
// therefore two spaces, never one. And a folded value is already folded — running the substitution
// again must not move anything, or a second pass over a field would drift the caret it just seated.
func TestLineBreaksFoldsNewlineTabAndCarriageReturn(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"a value with none of the three is returned unchanged", "http://box:1111", "http://box:1111"},
		{"a newline becomes the space the two words stood apart by", "one\ntwo", "one two"},
		{"a tab becomes one too", "one\ttwo", "one two"},
		{"a carriage return becomes one as well", "one\rtwo", "one two"},
		{"a CRLF is two runes, so it is two spaces", "one\r\ntwo", "one  two"},
		{"all three fold in the same pass", "a\tb\nc\rd", "a b c d"},
		{"each one is its own space, never collapsed", "a\t\t\nb", "a   b"},
		{"a trailing line ending folds like any other", "/some/path\r\n", "/some/path  "},
		{"an ordinary space is left alone", "one two", "one two"},
		{"non-ASCII text is not layout", "héllo\t世界", "héllo 世界"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := lineBreaks.Replace(tc.in)

			if got != tc.want {
				t.Errorf("lineBreaks.Replace(%q) = %q; want %q", tc.in, got, tc.want)
			}
			if strings.ContainsAny(got, "\n\t\r") {
				t.Errorf("lineBreaks.Replace(%q) left a break behind: %q", tc.in, got)
			}
			if in, out := len([]rune(tc.in)), len([]rune(got)); in != out {
				t.Errorf("lineBreaks.Replace(%q) is %d runes wide; the value was %d", tc.in, out, in)
			}
			if again := lineBreaks.Replace(got); again != got {
				t.Errorf("lineBreaks.Replace is not idempotent: %q became %q", got, again)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// The caret glyph a field carries
// ----------------------------------------------------------------------------

// A field draws the glyph it was BUILT with, and draws it where the caret stands. That is the whole
// of what the glyph parameter buys: a field painted into a popup row has no seat for the terminal's
// own cursor (popup.go styles rows whole and takes plain cells), so the honest report of where the
// next keystroke lands is a glyph AT the offset — and the four surfaces that need one do not agree
// on which glyph, so the field carries its own rather than every painter naming one.
//
// A field built with NO glyph draws nothing, which is the chat box's case: its caret is the
// terminal's real one and is on the screen already (newPromptEditor).
func TestTextWithCaretDrawsTheFieldsOwnGlyph(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		glyph string
		want  string
	}{
		{"the picker's filter and the /sessions browser's", pickerFilterCursor, "abc▌"},
		{"the /sessions rename row", sessionRenameCaret, "abc▏"},
		{"the /settings value row", settingsCaret, "abc▏"},
		{"a field the terminal's own cursor sits in", "", "abc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := testPopupField(tc.glyph, "abc")
			if got := e.textWithCaret(); got != tc.want {
				t.Errorf("textWithCaret() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The glyph follows the CARET rather than closing the line — the difference between a field and the
// string buffers the three overlays used to keep, which could only ever draw a cursor after the last
// rune. Sliced in RUNES, so a caret inside multi-byte text lands between characters rather than
// splitting one.
func TestTextWithCaretDrawsTheGlyphWhereTheCaretStands(t *testing.T) {
	t.Parallel()

	e := testPopupField(pickerFilterCursor, "héllo")
	if got, want := e.textWithCaret(), "héllo▌"; got != want {
		t.Fatalf("a fresh field draws %q, want the caret at the end (%q)", got, want)
	}
	e.caretToRune(2)
	if got, want := e.textWithCaret(), "hé▌llo"; got != want {
		t.Errorf("textWithCaret() = %q, want the glyph two runes in (%q)", got, want)
	}
}

// The caret glyph's shift is stated once, on the field that draws it (caretGlyph), and read both ways:
// a painted offset maps back to the value with the glyph's cell naming the caret, and a value span maps
// forward to the painted runes that draw exactly it — a span opening at the caret opens past the glyph,
// a span ending at it stops before it. A field with no glyph shifts nothing either way.
func TestCaretGlyphMapsBetweenThePaintedTextAndTheValue(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name           string
		glyph          string
		caret          int
		painted        int // a painted offset to map back
		wantValue      int
		lo, hi         int // a value span to map forward
		wantLo, wantHi int
	}{
		{"before the glyph", settingsCaret, 3, 2, 2, 0, 2, 0, 2},
		{"on the glyph", settingsCaret, 3, 3, 3, 1, 3, 1, 3},
		{"past the glyph", settingsCaret, 3, 5, 4, 3, 5, 4, 6},
		{"a span across the glyph", settingsCaret, 3, 6, 5, 1, 5, 1, 6},
		{"no glyph", "", 3, 5, 5, 3, 5, 3, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := testPopupField(tc.glyph, "abcdefgh")
			e.caretToRune(tc.caret)

			g := e.caretGlyph()

			if got := g.toValue(tc.painted); got != tc.wantValue {
				t.Errorf("toValue(%d) = %d, want %d", tc.painted, got, tc.wantValue)
			}
			if lo, hi := g.toPainted(tc.lo, tc.hi); lo != tc.wantLo || hi != tc.wantHi {
				t.Errorf("toPainted(%d, %d) = (%d, %d), want (%d, %d)", tc.lo, tc.hi, lo, hi, tc.wantLo, tc.wantHi)
			}
		})
	}
}

// Merge policy (plan 2026-08-19 §Ratified design calls 2): routing the three raw buffers through one
// field must leave every surface drawing the caret it drew before. The glyph is the parameter that
// keeps them apart, so the three are pinned by value here — a shared field stays behaviour-preserving
// only while these do.
func TestEachSurfaceKeepsItsOwnCaretGlyph(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		glyph string
		want  string
	}{
		{"the filter line both filtering overlays paint", pickerFilterCursor, "▌"},
		{"the /sessions rename row", sessionRenameCaret, "▏"},
		{"the /settings value row", settingsCaret, "▏"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.glyph != tc.want {
				t.Errorf("caret glyph = %q, want %q — the surface's caret changed", tc.glyph, tc.want)
			}
		})
	}
}

// A field a whole-struct reset left behind is the inert zero value: it answers "" and reports itself
// unbuilt, which is what lets an overlay's own zeroing clear its filter and its rename buffer
// (`m.picker = picker{}`) while the field is still built on the first key that reaches it
// (typeIntoOverlayFilter).
func TestZeroFieldIsInertAndSaysSo(t *testing.T) {
	t.Parallel()

	var zero lineEditor
	if zero.isBuilt() {
		t.Error("the zero value reports itself built; a whole-struct reset would leave a live field")
	}
	if got := zero.value(); got != "" {
		t.Errorf("value() = %q, want the zero field to hold nothing", got)
	}
	if built := testPopupField(pickerFilterCursor, ""); !built.isBuilt() {
		t.Error("a constructed field reports itself unbuilt; it would be rebuilt under every keystroke")
	}
}

// testPopupField builds a popup-painted field the way the three overlays do, off the default scheme
// and cursor shape — the fields are painted as plain text, so neither reaches what these tests read.
func testPopupField(glyph, seed string) lineEditor {
	return newPopupField(defaultCursorShape, lipgloss.Color(scheme.Default().Surface), glyph, seed)
}

// The wrap memo sized to the draft (lineEditor.fitWrapMemo). bubbles rebuilds its wrap memo at
// capacity MaxHeight on every Update, and a keypress wraps every logical line above the caret: left
// at the default 99, a longer draft evicted its own lines inside that one pass and rewrapped all of
// them on every key. These pin the three things the sizing promises — a keypress's cost stops
// scaling past the draft's own edit, the memo's retained heap stays bounded on one long line, and
// the draft's line limit is the widget's own 10000 however the newlines arrive.

// longDraft is lines logical lines of ordinary prose, each narrower than the test field so every
// line wraps to one visual row. Each line is numbered: the memo is keyed on a line's content, so
// identical lines would share one entry and no draft could outgrow it.
func longDraft(lines int) string {
	var b strings.Builder
	for i := range lines - 1 {
		fmt.Fprintf(&b, "%d the quick brown fox jumps over the lazy dog\n", i)
	}
	b.WriteString("tail")
	return b.String()
}

// testDraftField builds an unconfined field at a fixed width holding value, caret at its end.
func testDraftField(value string) lineEditor {
	e := newLineEditor(defaultCursorShape, lipgloss.Color(scheme.Default().Surface), "")
	e.input.SetWidth(testDraftWidth)
	e.setValue(value)
	return e
}

// testDraftWidth is the field width the draft tests type at: wider than a longDraft line.
const testDraftWidth = 80

// keypressAllocs is the allocations one keypress at the end of a lines-line draft costs, steady
// state: the first key is typed before measuring so the memo is warm.
func keypressAllocs(lines int) float64 {
	e := testDraftField(longDraft(lines))
	e.editKey(keyRune('x'))
	return testing.AllocsPerRun(20, func() { e.editKey(keyRune('x')) })
}

// Not parallel: testing.AllocsPerRun refuses to run inside a parallel test.
func TestLineEditorKeypressAllocsDoNotRewrapTheDraft(t *testing.T) {
	const shortLines, longLines, maxRatio = 40, 400, 15.0

	short := keypressAllocs(shortLines)
	long := keypressAllocs(longLines)

	if ratio := long / short; ratio > maxRatio {
		t.Fatalf("a keypress on a %d-line draft allocates %.0f, %.1f× the %.0f on a %d-line draft; want ≤ %.0f×",
			longLines, long, ratio, short, shortLines, maxRatio)
	}
}

// The memo keeps every stale version of an edited line it has room for, so capacity is heap: editing
// ONE long line must not retain one wrap per keystroke, which an unbounded memo (MaxHeight 0) did —
// ~240 MB after 3000 keys on a 10k-character line. The keys alternate a typed rune at the line's
// start with a forward delete, so every key makes a NEW version of a line whose length stays put:
// the retained heap then measures the memo's capacity alone (~1 MB fitted, ~25 MB unbounded here).
// Not parallel: HeapInuse is process-wide, and a neighbour's allocations would be read as this one's.
func TestLineEditorLongLineKeepsTheMemoHeapBounded(t *testing.T) {
	const (
		lineRunes = 1500
		keys      = 2 * lineRunes // every key a new version; the deletes never run out of line
		maxGrowth = 2400 << 10    // ≥ 10× under the unbounded memo's growth, ~2× over the fitted one's
	)
	var stats runtime.MemStats
	e := testDraftField(strings.Repeat("a", lineRunes))
	e.input.MoveToBegin()
	runtime.GC()
	runtime.ReadMemStats(&stats)
	before := stats.HeapInuse

	for i := range keys {
		if i%2 == 0 {
			e.editKey(keyRune('b'))
		} else {
			e.editKey(tea.KeyPressMsg{Code: tea.KeyDelete})
		}
	}
	runtime.GC()
	runtime.ReadMemStats(&stats)

	if grown := int64(stats.HeapInuse) - int64(before); grown > maxGrowth {
		t.Fatalf("%d edits of one %d-rune line grew HeapInuse by %d KB; want ≤ %d KB",
			keys, lineRunes, grown>>10, maxGrowth>>10)
	}
	runtime.KeepAlive(e)
}

// A typed newline used to be refused on the 99th logical line (the widget's legacy MaxHeight
// check), while a paste could already reach 10000. Driven through Model.Update on a draft the
// recall route installs (SetValue), because the prompt's keys reach the widget without editKey.
func TestPromptTypedNewlinesPassNinetyNineLines(t *testing.T) {
	t.Parallel()
	const draftLines = 150
	m := newTestModel(t)
	m.input.SetValue(longDraft(draftLines))

	m = step(t, m, tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})
	m = step(t, m, tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	m = step(t, m, keyRune('z'))

	if got := m.input.LineCount(); got != draftLines+2 {
		t.Fatalf("alt+enter and ctrl+j on a %d-line draft left %d lines; want %d", draftLines, got, draftLines+2)
	}
	if !strings.HasSuffix(m.input.Value(), "tail\n\nz") {
		t.Fatalf("the typed rune did not land after the new lines: value ends %q", lastRunes(m.input.Value(), 12))
	}
}

// The lifted cap stops where the widget's own does: typed newlines end at 10000 lines, the limit a
// paste obeys and a later SetValue truncates at.
func TestLineEditorTypedNewlinesStopAtTheWidgetLimit(t *testing.T) {
	t.Parallel()
	e := testDraftField(strings.Repeat("\n", wrapMemoCeiling-3))

	for range 5 {
		e.editKey(keyEnter()) // the unconfigured field's own newline binding
	}

	if got := e.input.LineCount(); got != wrapMemoCeiling {
		t.Fatalf("typing newlines past the limit left %d lines; want %d", got, wrapMemoCeiling)
	}
}

// lastRunes is the final n runes of s, for a failure message that cannot print a whole draft.
func lastRunes(s string, n int) string {
	r := []rune(s)
	return string(r[max(0, len(r)-n):])
}

// BenchmarkPromptKeyLongDraft is one printable key typed through Model.Update at the end of a
// 400-line draft — the whole prompt path a keystroke takes, not the widget alone.
func BenchmarkPromptKeyLongDraft(b *testing.B) {
	const draftLines = 400
	m := newModel(context.Background(), &fakeEngine{}, testOpts, nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(Model)
	m.input.SetValue(longDraft(draftLines))
	b.ResetTimer()

	for range b.N {
		next, _ = m.Update(keyRune('x'))
		m = next.(Model)
	}
}

// Caret seating (lineEditor.seatCaret) as a function of the draft. The seat used to walk every
// logical line above its target with CursorEnd+CursorDown, and each CursorDown re-counted every
// visual row above the caret (the widget's repositionView): O(lines²) per seat, ~84 % of a 40k
// bracketed paste, because the box grows under a paste and every height change re-seats the caret
// (reseatInput). The direct seat rebuilds the value around the target instead, so the tests below
// pin both halves of that change: the caret lands exactly where the walk put it — row, column,
// scroll and the drawn frame — and a paste's allocations stop scaling with the square of its size.

// walkSeat is the retired seat, kept as the oracle the direct one is held to: MoveToBegin, one
// CursorEnd+CursorDown per logical line above the target, then the column and the re-clamp.
func walkSeat(e *lineEditor, row, col int) {
	e.input.MoveToBegin()
	for e.input.Line() < row {
		before := e.input.Line()
		e.input.CursorEnd()
		e.input.CursorDown()
		if e.input.Line() == before {
			break
		}
	}
	e.input.SetCursorColumn(col)
	e.input.SetHeight(e.input.Height())
}

// seatProbeField builds a narrow, short field holding value with its caret at the END and its view
// drawn once, so the widget's viewport holds content and a scroll offset a seat has to move off.
// Each probe builds its own: copies of one field share the widget's viewport pointer.
func seatProbeField(value string) lineEditor {
	e := newLineEditor(defaultCursorShape, lipgloss.Color(scheme.Default().Surface), "")
	e.input.SetWidth(seatProbeWidth)
	e.input.SetHeight(3)
	e.setValue(value)
	_ = e.input.View()
	return e
}

// seatProbeWidth is narrow enough that every probe draft soft-wraps.
const seatProbeWidth = 12

// Every row the value has, and every column of it plus one either side (the widget clamps those).
// A row OUTSIDE the value is not compared: no caller names one (stepLine clamps, offsetToLineCol
// clamps, reseatInput re-seats where the caret stands), and the walk did not clamp it — it ran
// on to the last line's END, scrolling to show that line's last sub-row, before the column landed.
// Not parallel: the table is cheap, and the fields it builds are compared frame for frame.
func TestLineEditorSeatCaretMatchesTheLineWalk(t *testing.T) {
	drafts := map[string]string{
		"single":       "hello",
		"empty":        "",
		"multi-line":   "one\ntwo\n\nfour five\nsix",
		"soft-wrapped": "alpha beta gamma delta epsilon\nzeta eta theta iota kappa lambda mu\nnu",
		// A line whose content ends with a space exactly at a row boundary wraps to a PHANTOM
		// trailing sub-line — the case a bare CursorDown walk could not cross.
		"phantom":  "abcdefghij \nnext line here\nabcdefghij \nend",
		"wide":     "日本語のテキストです。長い行\nplain\n😀😀😀😀😀😀😀😀",
		"trailing": "text\n\n\n",
	}
	for name, draft := range drafts {
		lines := strings.Split(draft, "\n")
		for row := range lines {
			width := len([]rune(lines[row]))
			for col := -1; col <= width+1; col++ {
				want := seatProbeField(draft)
				walkSeat(&want, row, col)
				got := seatProbeField(draft)
				got.seatCaret(row, col)

				if got.input.Line() != want.input.Line() || got.input.Column() != want.input.Column() {
					t.Errorf("%s (%d,%d): seated at (%d,%d); the walk seats (%d,%d)", name, row, col,
						got.input.Line(), got.input.Column(), want.input.Line(), want.input.Column())
				}
				if got.input.ScrollYOffset() != want.input.ScrollYOffset() {
					t.Errorf("%s (%d,%d): scroll offset %d; the walk leaves %d", name, row, col,
						got.input.ScrollYOffset(), want.input.ScrollYOffset())
				}
				if got.value() != draft {
					t.Errorf("%s (%d,%d): the seat changed the value to %q", name, row, col, got.value())
				}
				if gv, wv := got.input.View(), want.input.View(); gv != wv {
					t.Errorf("%s (%d,%d): frame differs from the walk's\n got: %q\nwant: %q", name, row, col, gv, wv)
				}
			}
		}
	}
}

// pasteDraft is a bracketed paste of about runes runes: numbered prose lines, each narrower than the
// 80-column test box, so the paste grows the box and every line is a logical line the seat crosses.
func pasteDraft(runes int) string {
	var b strings.Builder
	for i := 0; b.Len() < runes; i++ {
		fmt.Fprintf(&b, "%d the quick brown fox jumps over the lazy dog\n", i)
	}
	return b.String()
}

// pasteAllocs is the allocations one bracketed paste of about runes runes costs through the prompt's
// own route (Model.foldPaste → layout → reseatInput), on a fresh empty prompt.
func pasteAllocs(t *testing.T, runes int) uint64 {
	t.Helper()
	m := newTestModel(t)
	msg := tea.PasteMsg{Content: pasteDraft(runes)}
	var stats runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&stats)
	before := stats.Mallocs
	next, _ := m.Update(msg)
	runtime.ReadMemStats(&stats)
	runtime.KeepAlive(next)
	return stats.Mallocs - before
}

// Not parallel: Mallocs is process-wide, and a neighbour's allocations would be read as this one's.
func TestPromptPasteAllocsScaleLinearly(t *testing.T) {
	const shortRunes, longRunes, maxRatio = 4000, 40000, 40.0

	short := pasteAllocs(t, shortRunes)
	long := pasteAllocs(t, longRunes)

	if ratio := float64(long) / float64(short); ratio > maxRatio {
		t.Fatalf("a %d-rune paste allocates %d, %.1f× the %d of a %d-rune paste; want ≤ %.0f×",
			longRunes, long, ratio, short, shortRunes, maxRatio)
	}
}

// BenchmarkPromptPaste is one 40k-rune bracketed paste folded into an empty prompt through
// Model.Update — the widget's insert, the box's re-flow and the caret's re-seat together.
func BenchmarkPromptPaste(b *testing.B) {
	m := newModel(context.Background(), &fakeEngine{}, testOpts, nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(Model)
	msg := tea.PasteMsg{Content: pasteDraft(40000)}
	b.ResetTimer()

	for range b.N {
		b.StopTimer()
		fresh := m
		fresh.input.SetValue("")
		b.StartTimer()
		fresh.Update(msg)
	}
}
