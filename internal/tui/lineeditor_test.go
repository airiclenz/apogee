package tui

import (
	"strings"
	"testing"
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
