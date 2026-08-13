package tui

import (
	"strings"
)

// ----------------------------------------------------------------------------
// Chrome layout helpers
// ----------------------------------------------------------------------------

// inputContentRows reports how many visual rows the input value occupies at innerWidth, mirroring
// the textarea's own wrap so the box sizes to exactly what the widget draws. It is the sum over
// logical lines of wrapRowStarts' row count (inputaccent.go) — the widget's own decomposition, which
// wraps each logical line independently and adds the counts (its totalVisualLines,
// bubbles/v2@v2.1.0/textarea/textarea.go:1666-1674). Delegating means the box's HEIGHT and the rows
// the accent pass paints on are read off one ruler; they used to be two separate derivations of the
// widget's wrap, and this one was an approximation (ansi.Wordwrap + ansi.Hardwrap) that disagreed
// with the widget on roughly 41% of prompt-shaped drafts, mostly by under-counting — "hello world"
// at width 5 is four widget rows and the old count said three.
//
// The trailing row a width-filling line keeps for a caret past its last cell comes with the mirror.
// Under-counting it leaves the box one row too short at a width-fill boundary — the source of the
// prompt-box scroll artifact the layout re-seat then can no longer reach (fixed in a7afbf1; its
// regression is [TestPromptScrollClampedWhileGrowing]). An empty value is one row.
//
// The count is deliberately unclamped: [promptEditor.rows] holds it to [minInputRows, maxInputRows],
// and past that cap the widget scrolls internally rather than the box growing further.
//
// KNOWN DIVERGENCE: both mirrors are still wrong on tabs, which the widget expands. See ISSUES.md,
// "The TUI width authority — what it did not convert".
//
// WIDGET MIRROR — deliberately NOT the width authority. This is one of the package's mirrors of a
// third-party widget's internal math, and a mirror's oracle is the widget, never apogee's
// painter-facing measure (width.go): the textarea wraps with uniseg.StringWidth
// (bubbles/v2@v2.1.0/textarea/textarea.go:1805-1852), which is what wrapRowStarts measures with
// (runesWidth) and is grapheme-clustered, unlike ansi.WcWidth. Sizing the box in the painter's
// measure would size it to something the widget never draws. The same rule governs the caret
// mirrors in inputaccent.go / mouse.go.
func inputContentRows(value string, innerWidth int) int {
	if innerWidth < 1 {
		innerWidth = 1
	}
	total := 0
	for _, line := range strings.Split(value, "\n") {
		total += len(wrapRowStarts([]rune(line), innerWidth))
	}
	return total
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
