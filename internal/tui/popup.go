package tui

import (
	"fmt"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
)

// ----------------------------------------------------------------------------
// The shared selector-popup painter (selector-popup plan §1)
// ----------------------------------------------------------------------------
//
// renderPopup is the single place overlay-pane chrome is painted: a titled, bordered pane holding
// an optional wrapping body block, a scrolled row list with the selected row highlighted, and a
// key-hint footer. The /sessions history browser (sessions.go), the command/file/skill autocomplete
// dropdown (autocomplete.go), and the ask and approval prompts (model.go) all compose their pane
// through it, so every overlay shares one look and one right edge. The dependency points inward
// only — callers compose a popupSpec and hand it here; renderPopup reaches back into neither
// overlay's state.
//
// Contract:
//   - width is the TOTAL box width, in lipgloss v2 semantics: the rounded border and the padding
//     fold INTO width, so every rendered line is exactly width display cells (like
//     renderStartupBox). Callers pass m.width — the full window width, the same value the input
//     box spans — so the pane's right border lands on the terminal's last column, flush with the
//     prompt box below it.
//   - The pane is filled solid black: the border style paints its border and padding cells black,
//     and renderPopup pads every content line to the full inner width on the same black field, so
//     no interior cell (including the gap after a short row) is left on the terminal background.
//   - The module owns the marker (glyphUser + a space on the selected row, two spaces
//     otherwise), the selected-row highlight (th.userBlock's full bar), and the scroll windowing
//     (popupRowWindow): callers hand over the FULL plain-text row list plus the global selected
//     index, and renderPopup windows around the selection itself. Rows arrive pre-composed and
//     escape-stripped; every content line is truncated to the inner budget, so no line can ever
//     wrap the box.
//   - The optional body block (spec.body) sits between the title and the rows and carries prose the
//     rows cannot: it is the ONE part the module word-wraps rather than truncates, because a
//     question or an approval reason/args body must break across lines instead of losing its tail
//     to an ellipsis. Embedded newlines are honoured as layout (the pretty-printed args'
//     indentation and blank separators survive), each segment is word-wrapped to the inner budget,
//     and the flattened block is capped at spec.maxBodyRows (≤ 0 = uncapped): past the cap the last
//     row becomes an explicit faint "… (+N more lines)" marker counting the hidden lines, so the
//     body never exceeds its cap and truncation is never silent. wrapText is ANSI-unaware, so body
//     arrives PLAIN and escape-stripped — the module wraps first and styles after.
//
// Every overlay pane — the /sessions browser, the command/file/skill dropdowns, and the ask and
// approval prompts — now paints through this module; no boxed overlay renders its own chrome
// (plan D3).

// popupSpec describes one boxed selector popup. title and hint each drop their row when empty;
// body is plain, escape-stripped prose the module word-wraps to the inner budget and caps at
// maxBodyRows (an empty body adds no rows); rows are the pre-composed, escape-stripped plain
// labels; selected indexes rows (−1 = no highlight); maxRows caps the scroll window around the
// selection (≤ 0 shows every row).
type popupSpec struct {
	title       string
	body        string
	maxBodyRows int
	rows        []string
	selected    int
	hint        string
	maxRows     int
}

// renderPopup paints the bordered selector pane described by spec at the given TOTAL width
// (lipgloss v2 folds the border and padding into width, so every returned line is exactly width
// cells). The inner content budget follows the border style — like renderStartupBox — rather
// than a hard-coded frame, and every content line (title, rows, hint) is truncated to it so none
// can wrap the box. The selected row within the scrolled window carries the glyphUser marker and
// the full-bar highlight; the others render faint.
func renderPopup(th theme, spec popupSpec, width int) string {
	frame := th.popupBorder.GetHorizontalFrameSize()
	if width <= frame {
		// No room for even one content cell inside the border + padding: lipgloss cannot render a
		// bordered box narrower than frame+1, so a box here would overflow the View. Degrade to
		// nothing instead — the same way footerView blanks below 3 columns (plan D3).
		return ""
	}
	inner := max(1, width-frame)

	// Every non-highlight line is padded to the full inner width on a solid-black field: the outer
	// border style only paints its own border and padding cells, so a line shorter than inner would
	// otherwise leave the gap between its text and the right border on the terminal's default
	// background (a black-hole strip). blackFill closes that — title, plain rows, and hint all read
	// as sitting on the same black pane. The selected row keeps th.userBlock's dark-gray highlight
	// bar (already full-inner-width) as its deliberate selection cue.
	blackFill := lipgloss.NewStyle().Background(colBlack).Width(inner)

	lines := make([]string, 0, len(spec.rows)+2) //nolint:mnd // +2: the optional title and hint rows
	if spec.title != "" {
		lines = append(lines, blackFill.Render(th.presentTitle.Render(truncateLabel(spec.title, inner))))
	}

	if spec.body != "" {
		lines = append(lines, popupBodyLines(th, spec.body, spec.maxBodyRows, inner, blackFill)...)
	}

	capRows := spec.maxRows
	if capRows <= 0 {
		capRows = len(spec.rows) // ≤ 0 shows every row (popupRowWindow returns [0, total) when total ≤ cap)
	}
	start, end := popupRowWindow(spec.selected, len(spec.rows), capRows)
	for i := start; i < end; i++ {
		selected := i == spec.selected
		marker := "  "
		if selected {
			marker = glyphUser + " "
		}
		row := truncateLabel(marker+spec.rows[i], inner)
		if selected {
			lines = append(lines, th.userBlock.Width(inner).Render(row))
		} else {
			lines = append(lines, blackFill.Render(th.statusFaint.Render(row)))
		}
	}

	if spec.hint != "" {
		lines = append(lines, blackFill.Render(th.statusFaint.Render(truncateLabel(spec.hint, inner))))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return th.popupBorder.Width(width).Render(content)
}

// popupBodyLines word-wraps spec.body into styled, black-filled content lines for the pane, sitting
// between the title and the rows. Each embedded newline is layout the caller composed (the approval
// args' JSON indentation and its blank separator lines), so the block is split on "\n" and each
// segment is word-wrapped to inner independently — an empty segment yields one blank row. When the
// flattened line count exceeds maxBodyRows (> 0), the block keeps the first maxBodyRows−1 lines and
// appends a faint "… (+N more lines)" marker counting the hidden lines, so it never exceeds
// maxBodyRows rows and the truncation is never silent; maxBodyRows ≤ 0 shows every wrapped line.
// Body lines render normal (th.popupBody) — the marker faint (th.statusFaint) — each padded on the
// same black field as every other content line and clipped to inner so, like every popup line, none
// can wrap the box.
func popupBodyLines(th theme, body string, maxBodyRows, inner int, blackFill lipgloss.Style) []string {
	var wrapped []string
	for _, seg := range strings.Split(body, "\n") {
		wrapped = append(wrapped, wrapText(seg, inner)...)
	}

	marker := ""
	if maxBodyRows > 0 && len(wrapped) > maxBodyRows {
		hidden := len(wrapped) - (maxBodyRows - 1)
		wrapped = wrapped[:maxBodyRows-1]
		marker = fmt.Sprintf("… (+%d more lines)", hidden)
	}

	out := make([]string, 0, len(wrapped)+1)
	for _, ln := range wrapped {
		out = append(out, blackFill.Render(th.popupBody.Render(truncateLabel(ln, inner))))
	}
	if marker != "" {
		out = append(out, blackFill.Render(th.statusFaint.Render(truncateLabel(marker, inner))))
	}
	return out
}

// popupRowWindow returns the [start, end) slice of a list of total rows to show at once, capped
// at capRows and scrolled to keep the selection roughly centred so a long list never overflows
// the pane.
func popupRowWindow(selected, total, capRows int) (int, int) {
	if total <= capRows {
		return 0, total
	}
	start := selected - capRows/2
	if start < 0 {
		start = 0
	}
	if start+capRows > total {
		start = total - capRows
	}
	return start, start + capRows
}
