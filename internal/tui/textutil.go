package tui

import (
	"strconv"
	"strings"
)

// ----------------------------------------------------------------------------
// Text utilities — the clip, the count, the line split and the clamp every surface shares
// ----------------------------------------------------------------------------
//
// This file holds the generic helpers the package spells ONCE (ADR 0043), and it holds them
// apart from the tool display they grew up in because not one of them knows a tool call:
// [clipDetail] and [clipRunes] bound how much of a single line reaches the screen, [plural] words
// a count, [firstLine] / [splitLines] cut a string into the physical lines a row-per-line
// surface paints, and [clampInt] holds an index or a size inside the range its surface allows.
//
// They are pure — no lipgloss, no I/O, no Model — and they are called from every display seam in
// the package: the transcript and its tool cards, the per-tool registry hooks, the diff bodies,
// the approval pane, the skills and schedule surfaces, plus every caret, scroll and selection
// arithmetic that spends the clamp. A helper that belonged to one of those would have moved with
// it; these are the ones that belong to all of them.
//
// Where a bound is spent in RUNES rather than in the cells the screen bills is a decision and not
// an oversight — [detailClipRunes] states it, and names the probe that measured it.

// detailClipRunes caps one detail/target line so a minified blob or a wall-of-text report cannot
// flood the transcript (the painter hard-wraps at the width authority, so an uncapped line
// becomes many rows).
//
// The cap is a FLOOD bound and it is deliberately spent in RUNES, not in the cells the screen
// bills. No rune paints more than two cells, so 160 runes buy at most 320 cells and therefore at
// most twice the rows the same 160 runes of ASCII take — a wall of double-width text costs scroll,
// never content. Cell-exactness is the STATUS LINE's requirement, not the transcript's: that row is
// shared with the context gauge, so an over-wide left slot pushes something the reader needs off the
// screen — which is why that row carries the tool's verb alone now (toolActivityVerb, activity.go)
// rather than a target it would have to cap in cells through the width authority. The
// transcript shares nothing — the painter wraps a wide line onto rows of its own and the block
// behind it paints lower down, whole. TestPaintedWideDetailLineWrapsWithoutDisplacement
// (paint_test.go) is the probe that measured all three of those claims and the pin that keeps
// them true.
//
// It bounds a line a slot beside it already summarises, which is every body but one: a failed call
// whose tool words no verdict of its own says the bare word `error` and keeps its message in the
// body alone, so that body is laid out under no clip at all (failureBody, toolregistry.go).
const detailClipRunes = 160

// clipDetail truncates s to detailClipRunes runes with an ellipsis.
func clipDetail(s string) string {
	return clipRunes(s, detailClipRunes)
}

// clipRunes truncates s to n runes with an ellipsis, counting runes rather than bytes so a
// multi-byte path is not cut mid-character. Its callers are clipDetail and the approval pane's
// Sub-agent line (approvalTaskClipRunes), and in both the rune spend is settled at the caller
// rather than being a shortfall to be swept: see detailClipRunes for why the transcript's bound is
// allowed to be a rune count where the status line's is not.
func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// plural renders "1 result" / "3 results" — count plus the word, naively pluralised.
func plural(n int, word string) string {
	return strconv.Itoa(n) + " " + pluralNoun(n, word)
}

// pluralNoun is the WORD alone that [plural] spells a count of n with — the bare word for one, a
// trailing "s" for anything else. It is stated apart from the phrase so a count carried as a value
// can be spelled by the very same rule without a phrase being built and taken apart again
// (pluralStat, statValue.spell).
func pluralNoun(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// firstLine returns the first line of s (without its newline), or s when it has none.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// splitLines splits s on newlines into its physical lines.
func splitLines(s string) []string {
	return strings.Split(s, "\n")
}

// clampInt clamps n to [lo, hi].
func clampInt(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}
