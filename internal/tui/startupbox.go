package tui

import (
	"strings"
)

// renderPresentedBlock renders a presented document (ADR 0019, rung 0) — the one block that is
// deliberately NOT shaped like a tool card, because a deliverable is the point of the work and
// not plumbing. It leads with the ▤ marker and the document's title where the model gave one,
// then the workspace-relative path, then the served URL if there is one, then a dim status line:
//
//	▤ Architecture review
//	  docs/review.html
//	  http://192.168.64.2:51234/d/…/review.html
//	  cmd+click to open
//
// The path and the URL are emitted RAW — no style, no clip, and while they fit, one token per
// line — and that is the whole mechanism: the terminal is what turns them into something
// clickable, so a hanging indent inserted mid-token or an SGR run wrapped around them would break
// the only rung that always works. width is not ignored for them, though (rawLink): the viewport
// no longer soft-wraps (newModel, model.go), so a line left over the width would be CLIPPED at the
// right edge and the tail of the path lost. Past the width they are hard-wrapped here instead —
// still unstyled, still without a hanging indent, so the rows join to the whole token even where
// no terminal can linkify it any more.
// The marker keeps the title's styling even when there is no title, so the block opens the same
// way either way.
func renderPresentedBlock(th theme, v presentedView, width int) []string {
	// The two raw lines are the one surface in this block that no style and no wrap passes, so a TAB
	// in a path survived every measure the transcript took of the line and was still a tab when the
	// viewport rendered the whole frame — which spent four cells on it that nothing had counted
	// (expandTabs). Settling them here keeps "emitted raw" a statement about styling and wrapping,
	// which is what makes the token clickable, rather than about a control byte the terminal never
	// gets to see anyway. The title and the status line go through hangingWrap, which expands its
	// own.
	v.Path, v.Location = expandTabs(v.Path), expandTabs(v.Location)

	marker := glyphPresented + " "

	var out []string
	if v.Title != "" {
		out = append(out, hangingWrap(th, th.presentTitle, marker, v.Title, width)...)
		out = append(out, rawLink(th, bodyIndent, v.Path, width)...)
	} else {
		out = append(out, rawLink(th, th.presentTitle.Render(marker), v.Path, width)...)
	}
	if v.Location != "" {
		out = append(out, rawLink(th, bodyIndent, v.Location, width)...)
	}
	return append(out, hangingWrap(th, th.noteText, bodyIndent, presentedStatus(v), width)...)
}

// rawLink lays out one of the presented block's link lines — the path or the served URL — as the
// rows the transcript stores. lead is what the row opens with (the block's two-space indent, or the
// styled ▤ marker when there is no title); token is the raw path or URL, which no style and no
// break may enter while it fits, because a whole token on a line is the only thing that makes a
// terminal linkify it.
//
// Past the width it IS broken, because the alternative is worse: the viewport stopped soft-wrapping
// (newModel, model.go), so an over-wide row is clipped at the right edge and everything past it is
// simply gone. The break is the painter's own wrapText, at the width the lead leaves — no hanging
// indent on the rows it opens and no style anywhere near them — so the rows read as, and join back
// to, the whole token. Linkification is lost on those windows; the token is not.
func rawLink(th theme, lead, token string, width int) []string {
	rows := wrapText(th, token, max(1, width-th.measure.Width(lead)))
	if len(rows) == 0 {
		return []string{lead}
	}
	rows[0] = lead + rows[0]
	return rows
}

// startupWideMinGap is the smallest gap, in columns, the wide start-up layout keeps between the logo
// and the right-aligned info block. When the content cannot fit the logo, this gap, and the info
// block side by side, renderStartupBox falls back to the stacked layout instead. It is the switch's
// only tuning knob — raise it if the two-column layout engages while still looking cramped.
const startupWideMinGap = 4

// startupInfoRow is one label/value pair of the start-up box's info block (host / model / context /
// version). An empty value drops the row.
type startupInfoRow struct{ label, value string }

// renderStartupBox renders the one-time start-up card, choosing a layout by the width it is handed.
// It reuses the prompt box's rounded border glyphs through th.startupBorder while dropping the black
// fill, so the card reads as the same chrome without the input box's solid field. It is
// [renderPresentedBlock]'s sibling — the entry holds the facts, this composes the lines.
//
// When there is room, the WIDE layout paints the logo on the left and a right-aligned
// host / model / context / version block on the right (renderStartupWide). When the width does not
// allow it, the STACKED fallback paints the original card — logo, a blank line, then host / model /
// version below it, no context (renderStartupStacked).
//
// Either way the card spans the full content width: width is the same railed inner budget every
// other transcript entry is laid out to (transcriptWidth), so the box's right border lands on the
// exact column the rest of the transcript's content ends at. The border and its padding fold INTO
// that width, so the rendered lines are exactly width columns. Both layouts frame their lines with
// drawBox rather than with th.startupBorder.Width(width): the rows are DRAWN in the painter's own
// measure, so a card whose info block carries a VARIATION SELECTOR-16 grapheme stays as many painted
// rows as it composed instead of having lipgloss fold one of them in two (ADR 0030 §5).
func renderStartupBox(th theme, v startupView, width int) []string {
	// inner is the content-column budget inside the rounded border and its padding — the room the
	// two layouts actually lay out to. GetHorizontalFrameSize tracks the border + padding, so the
	// arithmetic follows the style rather than a hard-coded 4.
	inner := width - th.startupBorder.GetHorizontalFrameSize()

	// The facts are composed into the info rows PLAIN — only the label is styled (startupInfoLine) —
	// so a TAB in one of them was measured as nothing by every width this card takes (the label
	// column, the info block's width, the layout switch, the row fit and drawBox's own squaring) and
	// was still a tab when the viewport rendered the frame, four cells the card had not budgeted for:
	// the row overran its own border, which then painted right of every other row's (expandTabs).
	// Expanded here rather than at the row, because the two layouts build their rows separately and
	// both measure before they compose. A host or a model id comes from config or the CLI, where a
	// stray tab is a typo away.
	v.Host, v.Model = expandTabs(v.Host), expandTabs(v.Model)
	v.Context, v.Version = expandTabs(v.Context), expandTabs(v.Version)

	rows := make([]startupInfoRow, 0, 4)
	for _, r := range []startupInfoRow{
		{"host", v.Host}, {"model", v.Model}, {"context", v.Context}, {"version", v.Version},
	} {
		if r.value != "" { // an unknown fact (context 0) drops its row, mirroring the footer's nonEmpty
			rows = append(rows, r)
		}
	}

	logo := strings.Split(v.Logo, "\n")
	logoW := 0
	for _, ln := range logo {
		logoW = max(logoW, th.measure.Width(ln))
	}
	labelW := startupLabelWidth(th, rows)
	infoW := startupInfoWidth(th, rows, labelW)

	if inner >= logoW+startupWideMinGap+infoW {
		return renderStartupWide(th, logo, rows, labelW, infoW, width, inner)
	}
	return renderStartupStacked(th, v, width, inner)
}

// renderStartupWide paints the wide start-up card: the logo on the left, the info block right-
// aligned against the right content edge (left column inner-infoW), so the widest info row sits
// flush against the right padding and shorter rows trail off toward it. Logo line i pairs with info
// row i (top-aligned) and whichever side is shorter blank-fills — the four logo lines pair directly
// with the four info rows, so there is no blank spacer. drawBox pads every line out to the card's
// content budget, in the painter's own measure (renderStartupBox's contract). The caller guarantees
// inner ≥ logoW + startupWideMinGap + infoW, so the per-line pad count is at least
// startupWideMinGap.
func renderStartupWide(th theme, logo []string, rows []startupInfoRow, labelW, infoW, width, inner int) []string {
	left := inner - infoW // the info block's left column
	n := max(len(logo), len(rows))
	lines := make([]string, 0, n)
	for i := 0; i < n; i++ {
		logoLine := ""
		if i < len(logo) {
			logoLine = logo[i]
		}
		line := logoLine + strings.Repeat(" ", max(0, left-th.measure.Width(logoLine)))
		if i < len(rows) {
			line += startupInfoLine(th, rows[i], labelW)
		}
		lines = append(lines, line)
	}
	return drawBox(th.measure, th.startupBorder, lines, width)
}

// renderStartupStacked paints the narrow fallback: the logo, one blank line, then the host / model /
// version rows stacked below it (no context), dim labels aligned in a column and plain values. This
// is the card's original layout, kept for widths too narrow for the two-column wide layout.
//
// It fits its OWN info rows to inner, the card's content budget, rather than handing the box a row
// it has no room for. drawBox squares every row it is given, so an over-long one comes back cut at
// the border and says nothing about it: on a card of 29 columns or less a host of
// "192.168.64.1:1111" was painted as "192.168.64.1:111" — a port silently one digit short, which
// reads as a fact rather than as a cut. Fitting here ends such a row in the same "…" every other
// overflowing surface in this package carries (truncateToWidth), so what was eaten is visible.
// The value is not wrapped onto a further line instead: a host, a model name and a version are
// single unbreakable tokens, so a wrap would only move the same cut one line down. A row that
// already fits comes back untouched, which is every row at the widths this card is normally drawn
// at. The logo above them is left to the box: block art has no tail to elide, and the widths where
// it overruns are far below the ones where the info rows do.
func renderStartupStacked(th theme, v startupView, width, inner int) []string {
	content := strings.Split(v.Logo, "\n")
	content = append(content, "") // one blank line between the logo and the info rows

	rows := []startupInfoRow{{"host", v.Host}, {"model", v.Model}, {"version", v.Version}}
	labelW := startupLabelWidth(th, rows)
	for _, r := range rows {
		content = append(content, truncateToWidth(th, startupInfoLine(th, r, labelW), inner))
	}
	return drawBox(th.measure, th.startupBorder, content, width)
}

// startupInfoLine renders one info row — the dim label padded to the block's label column, two
// spaces, then the plain value — shared by both start-up layouts so their rows never drift.
func startupInfoLine(th theme, r startupInfoRow, labelW int) string {
	padded := r.label + strings.Repeat(" ", max(0, labelW-th.measure.Width(r.label)))
	return th.noteText.Render(padded) + "  " + r.value
}

// startupLabelWidth is the widest label among the info rows — the column every value aligns past.
func startupLabelWidth(th theme, rows []startupInfoRow) int {
	w := 0
	for _, r := range rows {
		w = max(w, th.measure.Width(r.label))
	}
	return w
}

// startupInfoWidth is the info block's rendered width: the widest label-padded row (labelW, two
// spaces, then the value). It is the block the wide layout right-aligns and the term the width
// switch measures against.
func startupInfoWidth(th theme, rows []startupInfoRow, labelW int) int {
	w := 0
	for _, r := range rows {
		w = max(w, labelW+2+th.measure.Width(r.value))
	}
	return w
}
