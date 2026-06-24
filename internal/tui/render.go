package tui

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ----------------------------------------------------------------------------
// The line-oriented transcript renderer (P2.7 — TUI presentation pass)
// ----------------------------------------------------------------------------
//
// The renderer turns the transcript into the flat slice of physical lines the viewport
// shows. It returns []string (not a joined string) for two reasons: tool results carry
// embedded newlines, so the caller feeds viewport.SetContentLines without re-splitting; and
// the sticky-to-top scroll needs to measure the wrapped height of the lines above the last
// user prompt, which it can only do over the exact lines the viewport stores. Every element
// is a single physical line (no embedded newline), so wrappedOffset's soft-wrap arithmetic
// stays in lockstep with the viewport's own calculateLine (TestWrappedOffsetMatchesViewport).
//
// The look mirrors layout.md: the last user prompt is a full-width white-on-dark-gray block;
// the assistant and tool headers lead with ✦; tool detail hangs off a ┝/┕ tree branch; one
// blank line separates every block. Sub-agent depth (Phase 3) indents a whole block by two
// columns per level — the tree-branch and depth-indent primitives are built here now so the
// P3.14 sub-agent renderer extends these seams rather than reworking them.

// renderedTranscript is the renderer's output: the physical lines and the index, within
// them, of the last user prompt's first line (-1 when the transcript holds no user prompt).
// The caller pins that line to the top of the viewport unless the human has scrolled.
type renderedTranscript struct {
	lines         []string
	lastUserStart int
}

// renderView renders the committed entries plus any in-progress assistant buffer into the
// viewport's lines, recording where the last user block begins. Blocks are separated by one
// blank line (layout.md).
func (t *transcript) renderView(th theme, width int) renderedTranscript {
	if width < 1 {
		width = 1
	}
	var lines []string
	lastUserStart := -1

	appendBlock := func(isUser bool, block []string) {
		if len(lines) > 0 {
			lines = append(lines, "") // the single blank line between blocks
		}
		if isUser {
			lastUserStart = len(lines)
		}
		lines = append(lines, block...)
	}

	for _, e := range t.entries {
		appendBlock(e.kind == entryUser, renderEntryLines(th, e, width))
	}
	if t.streaming {
		appendBlock(false, renderEntryLines(th, entry{kind: entryAssistant, text: t.pending}, width))
	}
	return renderedTranscript{lines: lines, lastUserStart: lastUserStart}
}

// renderLines is the line slice alone — the viewport content and the substring-test surface.
func (t *transcript) renderLines(th theme, width int) []string {
	return t.renderView(th, width).lines
}

// renderEntryLines renders one committed entry into its physical lines, indented for its
// sub-agent depth. The user prompt is a full-width block; everything else hangs off a marker.
func renderEntryLines(th theme, e entry, width int) []string {
	switch e.kind {
	case entryUser:
		return indentLines(renderUserBlock(th, e.text, width), e.depth)
	case entryAssistant:
		return indentLines(hangingWrap(th.assistant, glyphAssistant+" ", e.text, width), e.depth)
	case entryToolCall:
		return renderToolBlock(th, e.tool, e.depth, width)
	case entryToolResult:
		return renderOrphanResult(th, e.text, e.depth, width)
	case entryError:
		return indentLines(hangingWrap(th.errorText, glyphAssistant+" ", e.text, width), e.depth)
	case entryNote:
		return indentLines(hangingWrap(th.noteText, "· ", e.text, width), e.depth)
	default:
		return nil
	}
}

// renderUserBlock renders the user prompt as a full-width white-on-dark-gray block: the ❯
// marker on the first line, a hanging two-column indent on wrapped continuation lines, and
// the dark-gray background padded across the whole width on every line.
func renderUserBlock(th theme, text string, width int) []string {
	wrapped := hangingPrefixes(glyphUser+" ", text, width)
	out := make([]string, len(wrapped))
	for i, ln := range wrapped {
		out[i] = th.userBlock.Width(width).Render(ln)
	}
	return out
}

// renderToolBlock renders a tool call: the ✦ [Label] target header, then each summary detail
// hanging off a ┝/┕ branch (the last line gets ┕). The whole block is indented for depth.
func renderToolBlock(th theme, tv toolView, depth, width int) []string {
	head := bracketLabel(tv)
	if tv.Target != "" {
		head += " " + tv.Target
	}
	out := hangingWrap(th.toolHeader, glyphAssistant+" ", head, width)
	out = append(out, renderDetails(th, tv.Details, width)...)
	return indentLines(out, depth)
}

// renderOrphanResult renders a tool result that matched no pending call (a defensive
// fallback — normally a result folds into its call by CallID). It reads as a result block:
// a ✦ [result] header with the raw content hanging off branches.
func renderOrphanResult(th theme, text string, depth, width int) []string {
	out := hangingWrap(th.toolHeader, glyphAssistant+" ", "[result]", width)
	details := make([]detailLine, 0)
	for _, ln := range splitLines(text) {
		details = append(details, detailLine{Text: ln})
	}
	out = append(out, renderDetails(th, details, width)...)
	return indentLines(out, depth)
}

// renderDetails renders tool-detail lines as ┝/┕ tree branches (the last line gets ┕),
// styled by their kind (plain dim, or red/green for the reserved diff kinds).
func renderDetails(th theme, details []detailLine, width int) []string {
	var out []string
	for i, d := range details {
		marker := "  " + glyphBranch + " "
		if i == len(details)-1 {
			marker = "  " + glyphBranchLast + " "
		}
		out = append(out, hangingWrap(detailStyle(th, d.Kind), marker, d.Text, width)...)
	}
	return out
}

// detailStyle maps a detail kind to its style: plain detail is dim; the diff kinds are
// red/green (reserved — no extractor emits them until an edit/diff tool exists).
func detailStyle(th theme, kind detailKind) lipgloss.Style {
	switch kind {
	case detailDiffAdded:
		return th.diffAdded
	case detailDiffRemoved:
		return th.diffRemoved
	default:
		return th.toolDetail
	}
}

// bracketLabel wraps a known tool's friendly label in [brackets]; an unknown tool's raw name
// is shown bare, signalling it has no presentation entry yet.
func bracketLabel(tv toolView) string {
	if tv.bracket {
		return "[" + tv.Label + "]"
	}
	return tv.Label
}

// ----------------------------------------------------------------------------
// Wrapping primitives
// ----------------------------------------------------------------------------

// hangingWrap word-wraps text under a leading marker, then styles each physical line: the
// marker leads the first line and a same-width blank indent leads every continuation line, so
// a wrapped block stays aligned under its marker (the ✦/┝ hanging indent of layout.md). The
// style colours the whole line; widths are ANSI-agnostic, so styling never perturbs the
// soft-wrap arithmetic.
func hangingWrap(style lipgloss.Style, marker, text string, width int) []string {
	prefixed := hangingPrefixes(marker, text, width)
	out := make([]string, len(prefixed))
	for i, ln := range prefixed {
		out[i] = style.Render(ln)
	}
	return out
}

// hangingPrefixes word-wraps text to the width left of the marker and prepends the marker to
// the first line and a matching blank indent to the rest, returning the unstyled lines. It is
// shared by the styled hanging wrap and the user block (which then pads each line to a
// full-width background).
func hangingPrefixes(marker, text string, width int) []string {
	mw := lipgloss.Width(marker)
	indent := strings.Repeat(" ", mw)
	lines := wrapText(text, max(1, width-mw))
	out := make([]string, len(lines))
	for i, ln := range lines {
		if i == 0 {
			out[i] = marker + ln
		} else {
			out[i] = indent + ln
		}
	}
	return out
}

// wrapText word-wraps text to limit columns, hard-breaking any word longer than the limit
// and preserving the text's own newlines. An empty string yields a single empty line so a
// just-opened assistant buffer still renders its marker.
func wrapText(text string, limit int) []string {
	if limit < 1 {
		limit = 1
	}
	return strings.Split(ansi.Wrap(text, limit, ""), "\n")
}

// indentLines prepends two columns per depth level to each line (a sub-agent's nested block,
// Phase 3). Depth 0 is the common case and returns the lines untouched.
func indentLines(lines []string, depth int) []string {
	if depth <= 0 {
		return lines
	}
	indent := strings.Repeat("  ", depth)
	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i] = indent + ln
	}
	return out
}

// ----------------------------------------------------------------------------
// Sticky-to-top offset
// ----------------------------------------------------------------------------

// ----------------------------------------------------------------------------
// Chrome layout helpers
// ----------------------------------------------------------------------------

// inputContentRows reports how many rows the input value wraps to at innerWidth, mirroring
// the textarea's own word-then-hard wrap so the box sizes to its content exactly. An empty
// value is one row.
func inputContentRows(value string, innerWidth int) int {
	if innerWidth < 1 {
		innerWidth = 1
	}
	wrapped := ansi.Hardwrap(ansi.Wordwrap(value, innerWidth, ""), innerWidth, true)
	return strings.Count(wrapped, "\n") + 1
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

// justify lays left and right at the two ends of a width-wide line with a run of spaces
// between them. When they do not both fit, the left side is kept and truncated, so the
// status line never overflows its row. Widths are ANSI-aware, so styled inputs justify
// correctly.
func justify(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return ansi.Truncate(left, max(0, width), "")
	}
	return left + strings.Repeat(" ", gap) + right
}

// fitLeftRight composes the footer's content body: " left … right " padded to exactly width
// columns, with one-column margins inside the borders. When the two segments do not fit, the
// left segment is kept and the body truncated, so the footer never overflows its row.
func fitLeftRight(left, right string, width int) string {
	if width < 1 {
		return ""
	}
	l := " " + left
	r := right + " "
	gap := width - lipgloss.Width(l) - lipgloss.Width(r)
	if gap >= 1 {
		return l + strings.Repeat(" ", gap) + r
	}
	truncated := ansi.Truncate(l, width, "…")
	return truncated + strings.Repeat(" ", max(0, width-lipgloss.Width(truncated)))
}

// ruleMix composes one of the footer's decorative rules: a heavy ━ bar with a lighter ─
// inset toward the right, echoing layout.md. The counts always sum to exactly n so the rule
// spans the full width between its corners.
func ruleMix(n int) string {
	if n <= 0 {
		return ""
	}
	light := n / 4
	rightHeavy := n / 8
	leftHeavy := n - light - rightHeavy
	return strings.Repeat("━", leftHeavy) + strings.Repeat("─", light) + strings.Repeat("━", rightHeavy)
}

// wrappedOffset returns the virtual (soft-wrapped) row at which the line after linesAbove
// begins — the Y offset that pins that line to the top of the viewport. It mirrors the
// viewport's calculateLine exactly: each physical line occupies max(1, ceil(width/vpWidth))
// rows. This holds only while the viewport has no border or gutter (maxWidth == Width, the
// current wiring); TestWrappedOffsetMatchesViewport guards the equality against drift.
func wrappedOffset(linesAbove []string, vpWidth int) int {
	if vpWidth < 1 {
		vpWidth = 1
	}
	total := 0
	for _, ln := range linesAbove {
		w := lipgloss.Width(ln)
		rows := 1
		if w > 0 {
			rows = (w + vpWidth - 1) / vpWidth // ceil(w / vpWidth)
		}
		total += rows
	}
	return total
}
