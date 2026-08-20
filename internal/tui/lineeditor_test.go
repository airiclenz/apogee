package tui

import (
	"strings"
	"testing"

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
