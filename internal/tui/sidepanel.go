package tui

// The side panel's column (ADR 0110, docs/layout/side-panel-layout.md): a fixed column right of the
// transcript slot, on every row above the session-name rule, drawn while [Model.panelShown] holds.

const (
	// sidePanelWidth is the column's painted width, its separator included.
	sidePanelWidth = 36
	// sidePanelAutoFloor is the transcript width `ui.side-panel.show: auto` keeps beside the column,
	// counting the scroll-bar column painted or not: the column shows from a 137-column window.
	sidePanelAutoFloor = 100
	// sidePanelOnFloor is the transcript width `ui.side-panel.show: on` never goes below, on the same
	// count: the column shows from a 117-column window.
	sidePanelOnFloor = 80
	// sidePanelSeparator is the rule on the column's left edge, one cell in either width measure.
	sidePanelSeparator = "│"
	// sidePanelGutter is the blank cell between the separator and the column's content.
	sidePanelGutter = " "
	// sidePanelEmpty is the one dim line an empty column paints at its top.
	sidePanelEmpty = "nothing live"
)

// sidePanelContentWidth is the columns the panel's content has: the column less its separator and
// the gutter beside it.
const sidePanelContentWidth = sidePanelWidth - len(sidePanelGutter) - 1

// sidePanelColumn paints the column's rows rows, each exactly sidePanelWidth columns in the width
// authority's measure: the separator, the gutter, and the content squared to the rest. With no
// section to show it paints `nothing live` on its top row and leaves the rest blank — the column
// stays on the frame either way, because nothing it shows may move it ([Model.panelShown]).
func (m Model) sidePanelColumn(rows int) []string {
	rule := m.th.statusFaint.Render(sidePanelSeparator) + sidePanelGutter
	blank := rule + squareLine(m.th.measure, "", sidePanelContentWidth)
	column := make([]string, rows)
	for i := range column {
		column[i] = blank
	}
	if rows > 0 {
		empty := squareLine(m.th.measure, sidePanelEmpty, sidePanelContentWidth)
		column[0] = rule + m.th.statusFaint.Render(empty)
	}
	return column
}
