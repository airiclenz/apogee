package tui

import (
	lipgloss "charm.land/lipgloss/v2"
)

// ----------------------------------------------------------------------------
// The shared selector-popup painter (selector-popup plan §1)
// ----------------------------------------------------------------------------
//
// renderPopup is the single place selector-popup chrome is painted: a titled, bordered pane
// holding a scrolled row list with the selected row highlighted and a key-hint footer. The
// /sessions history browser (sessions.go) and the command/file/skill autocomplete dropdown
// (autocomplete.go) both compose their pane through it, so every boxed selector shares one look
// and one right edge. The dependency points inward only — callers compose a popupSpec and hand
// it here; renderPopup reaches back into neither overlay's state.
//
// Contract:
//   - width is the TOTAL box width, in lipgloss v2 semantics: the rounded border and the padding
//     fold INTO width, so every rendered line is exactly width display cells (like
//     renderStartupBox). Callers pass transcriptWidth() so the pane's right border lands on the
//     same column the transcript's wrapped text ends at.
//   - The module owns the marker (glyphUser + a space on the selected row, two spaces
//     otherwise), the selected-row highlight (th.userBlock's full bar), and the scroll windowing
//     (popupRowWindow): callers hand over the FULL plain-text row list plus the global selected
//     index, and renderPopup windows around the selection itself. Rows arrive pre-composed and
//     escape-stripped; every content line is truncated to the inner budget, so no line can ever
//     wrap the box.
//
// The approval / ask prompts (model.go) keep their plain-text form for now; they are the
// deliberate future adopters of this module (plan D2).

// popupSpec describes one boxed selector popup. title and hint each drop their row when empty;
// rows are the pre-composed, escape-stripped plain labels; selected indexes rows (−1 = no
// highlight); maxRows caps the scroll window around the selection (≤ 0 shows every row).
type popupSpec struct {
	title    string
	rows     []string
	selected int
	hint     string
	maxRows  int
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

	lines := make([]string, 0, len(spec.rows)+2) //nolint:mnd // +2: the optional title and hint rows
	if spec.title != "" {
		lines = append(lines, th.presentTitle.Render(truncateLabel(spec.title, inner)))
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
			lines = append(lines, th.statusFaint.Render(row))
		}
	}

	if spec.hint != "" {
		lines = append(lines, th.statusFaint.Render(truncateLabel(spec.hint, inner)))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return th.popupBorder.Width(width).Render(content)
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
