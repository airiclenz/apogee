package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	lipgloss "charm.land/lipgloss/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/format"
	"github.com/airiclenz/apogee/internal/sidepanel"
)

// The side panel's column (ADR 0110, docs/layout/side-panel-layout.md): a fixed column right of the
// transcript slot, on every row above the session-name rule, drawn while [Model.panelShown] holds.
// It paints the sections the `ui.side-panel.sections` list shows, from the sidepanel.State the
// Event fold keeps ([Model.foldSidePanel]).

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
	// sidePanelMore is the bottom row's word when not every shown section's header fits: `+N more`.
	sidePanelMore = " more"
	// sidePanelPathElision leads a changed file's path trimmed from the left (`…/parse/parser.go`).
	sidePanelPathElision = "…/"
	// sidePanelPartSeparator parts the facts of one usage row (`in 12k · out 4k`).
	sidePanelPartSeparator = " · "
	// subAgentFactsFormat lays out a sub-agent row's right-hand facts — phase, elapsed, fill — at
	// fixed widths, so the facts of every row line up down the column.
	subAgentFactsFormat = "%-7s %6s %4s"
	// percentCap is a whole percentage: the scale a share is spelled in, and the most a fill
	// reads — a conversation carried into a smaller window overfills it (usageFillCell's clamp).
	percentCap = 100
)

// sidePanelContentWidth is the columns the panel's content has: the column less its separator and
// the gutter beside it.
const sidePanelContentWidth = sidePanelWidth - len(sidePanelGutter) - 1

// panelSection is one section the column shows: what its header says and its body rows, painted.
type panelSection struct {
	title    string
	summary  string
	isPinned bool
	body     []string
}

// foldSidePanel folds e into the side panel's state, copy-on-write: an Event the panel reads goes
// into a clone of the held State (a fresh one on the first such Event) and the clone replaces the
// pointer, so a State another Model copy still holds is never written (ADR 0011). window is the
// Event's run's bound window, the fill's fallback where a reading names none.
func (m Model) foldSidePanel(e domain.Event, window int) Model {
	if !sidepanel.Reads(e) {
		return m
	}
	next := m.panel.Clone()
	if next == nil {
		next = sidepanel.New(time.Now)
	}
	next.SetFallbackWindow(window)
	next.Fold(e)
	m.panel = next
	return m
}

// sidePanelColumn paints the column's rows rows, each exactly sidePanelWidth columns in the width
// authority's measure: the separator, the gutter, and the content squared to the rest. With no
// section to show it paints `nothing live` on its top row and leaves the rest blank — the column
// stays on the frame either way, because nothing it shows may move it ([Model.panelShown]).
func (m Model) sidePanelColumn(rows int) []string {
	rule := m.th.statusFaint.Render(sidePanelSeparator) + sidePanelGutter
	content := m.sidePanelContent(rows)
	column := make([]string, rows)
	for i := range column {
		line := ""
		if i < len(content) {
			line = content[i]
		}
		column[i] = rule + squareLine(m.th.measure, line, sidePanelContentWidth)
	}
	return column
}

// sidePanelContent is the column's content rows, at most rows of them: the shown sections laid out
// by the overflow rule ([panelAllocation]), or the one `nothing live` line.
func (m Model) sidePanelContent(rows int) []string {
	sections := m.panelSections()
	if len(sections) == 0 {
		return []string{m.th.statusFaint.Render(sidePanelEmpty)}
	}
	kept, isExpanded := panelAllocation(sections, rows)
	lines := make([]string, 0, rows)
	for i, section := range sections[:kept] {
		lines = append(lines, m.panelHeaderRow(section))
		if isExpanded[i] {
			lines = append(lines, section.body...)
		}
	}
	if left := len(sections) - kept; left > 0 {
		lines = append(lines, m.th.statusFaint.Render("+"+strconv.Itoa(left)+sidePanelMore))
	}
	return lines
}

// panelAllocation settles the overflow (docs/layout/side-panel-layout.md, "Overflow"): every shown
// section keeps its header row, and the rows left over go to whole bodies — pinned sections first,
// then auto ones, each pass in list order — so a section whose body does not fit folds to its
// header alone. When not even every header fits, the last ones in list order are left out and the
// bottom row is kept for `+N more`. kept is how many sections, from the front, are drawn.
func panelAllocation(sections []panelSection, rows int) (kept int, isExpanded []bool) {
	isExpanded = make([]bool, len(sections))
	if len(sections) > rows {
		return max(rows-1, 0), isExpanded
	}
	spare := rows - len(sections)
	for _, isPinnedPass := range []bool{true, false} {
		for i, section := range sections {
			if section.isPinned != isPinnedPass || len(section.body) == 0 || len(section.body) > spare {
				continue
			}
			isExpanded[i], spare = true, spare-len(section.body)
		}
	}
	return len(sections), isExpanded
}

// panelSections is the sections the column shows, in list order: every entry of
// `ui.side-panel.sections` that is pinned, or auto while its live rule holds.
func (m Model) panelSections() []panelSection {
	entries, err := domain.ParseSidePanelSections(string(m.opts.UI.SidePanelSections))
	if err != nil {
		// Unreachable from a running session: the config loader and /settings both refuse a list
		// this parse refuses, so a bad one never reaches the Options. The default list still paints.
		entries, _ = domain.ParseSidePanelSections("")
	}
	shown := make([]panelSection, 0, len(entries))
	for _, entry := range entries {
		if entry.Mode == domain.SidePanelModeHidden {
			continue
		}
		section, isLive := m.panelSection(entry.Section)
		if entry.Mode == domain.SidePanelModeAuto && !isLive {
			continue
		}
		section.isPinned = entry.Mode == domain.SidePanelModePinned
		shown = append(shown, section)
	}
	return shown
}

// panelSection paints one section from the folded state, and reports whether its live rule holds.
// Every section but sub-agents reads the run the human is looking at ([Model.viewedRun]) — the
// top-level run outside Run view — and the usage section measures that run's fill as the
// status-line gauge states it ([Model.viewedFill]). Sub-agents lists every run and marks the viewed
// one (ADR 0110 decision 12).
func (m Model) panelSection(section domain.SidePanelSection) (panelSection, bool) {
	scope := panelRunKey(m.viewedRun())
	switch section {
	case domain.SidePanelSubAgents:
		view := m.panel.SubAgents()
		paint := func(row sidepanel.SubAgentRow) string {
			return m.panelSubAgentRow(row, m.inRunView() && row.Run == scope)
		}
		return panelSection{title: "Sub-agents", summary: view.Summary, body: paintPanelRows(view.Rows, paint)},
			view.IsLive
	case domain.SidePanelTasks:
		view := m.panel.Tasks(scope)
		return panelSection{title: "Tasks", summary: view.Summary, body: paintPanelRows(view.Rows, m.panelTaskRow)},
			view.IsLive
	case domain.SidePanelChangedFiles:
		view := m.panel.ChangedFiles(scope)
		return panelSection{title: "Changed files", summary: view.Summary, body: paintPanelRows(view.Rows, m.panelFileRow)},
			view.IsLive
	case domain.SidePanelUsage:
		view := m.panel.Usage(scope).WithFill(m.panelFill())
		return panelSection{title: "Usage", summary: view.Summary, body: m.panelUsageRows(view)}, view.IsLive
	default:
		return panelSection{}, false
	}
}

// panelRunKey is ref as the side panel keys the Events of its run: [sidepanel.RunOf] over the same
// three values [runOf] reads off an Event's base, so a run's view and its folded state name it alike —
// by run id, or by depth and spawning call for a run recorded before run ids. The top-level run's
// zero ref is the zero key.
func panelRunKey(ref runRef) sidepanel.RunKey {
	return sidepanel.RunOf(domain.EventBase{Depth: ref.depth, CallID: ref.spawn, RunID: ref.id})
}

// panelFill is the usage section's fill: the gauge's own ([Model.viewedFill]), and none where the
// gauge states none.
func (m Model) panelFill() sidepanel.Fill {
	usage, ok := m.viewedFill()
	if !ok {
		return sidepanel.Fill{}
	}
	return sidepanel.Fill{Used: usage.Used, Window: usage.Limit, IsEstimate: usage.Estimated}
}

// paintPanelRows paints each of a section's rows with paint.
func paintPanelRows[R any](rows []R, paint func(R) string) []string {
	painted := make([]string, 0, len(rows))
	for _, row := range rows {
		painted = append(painted, paint(row))
	}
	return painted
}

// panelRow lays one content row out: lead (painted, measured as it stands), then text — clipped
// with an ellipsis to the room left — painted in style, then right (painted) flush against the
// column's right edge, one blank cell at least from the text.
func (m Model) panelRow(lead, text string, style lipgloss.Style, right string) string {
	room := sidePanelContentWidth - m.th.measure.Width(lead)
	if right != "" {
		room -= m.th.measure.Width(right) + 1
	}
	text = truncateToWidth(m.th, text, room)
	row := lead + style.Render(text)
	if right == "" {
		return row
	}
	return row + strings.Repeat(" ", room-m.th.measure.Width(text)+1) + right
}

// panelHeaderRow is a section's header: `◆ <Title>` with its summary, and the pin glyph — ⊙ while
// the section is auto, ● while it is pinned — at the column's right edge.
func (m Model) panelHeaderRow(section panelSection) string {
	text := section.title
	if section.summary != "" {
		text += " " + section.summary
	}
	pin := glyphPanelUnpinned
	if section.isPinned {
		pin = glyphPanelPinned
	}
	return m.panelRow(m.th.panelHeader.Render(glyphPanelHeader+" "), text, m.th.panelHeader, m.th.statusFaint.Render(pin))
}

// panelSubAgentRow is one delegation: its phase glyph — the status line's spinner frame while it
// runs — its name, and its phase, elapsed time and context fill. The run Run view shows (isViewed)
// is marked as the current task is: `▸` before its name, both in the current task's style.
func (m Model) panelSubAgentRow(row sidepanel.SubAgentRow, isViewed bool) string {
	var glyph, phase string
	switch row.Phase {
	case sidepanel.PhaseQueued:
		glyph, phase = m.th.statusFaint.Render(glyphPanelOpen), "queued"
	case sidepanel.PhaseRunning:
		glyph, phase = m.spin.drawn(m.th, m.opts.UI.Spinner), "running"
	case sidepanel.PhaseDone:
		glyph, phase = m.th.successMark.Render(m.th.glyphs.done), "done"
	default:
		glyph, phase = m.th.errorText.Render(m.th.glyphs.failure), "failed"
	}
	elapsed := ""
	if row.Elapsed > 0 {
		elapsed = formatElapsed(row.Elapsed)
	}
	fill := ""
	if percent := row.Fill.Percent(); percent > 0 {
		fill = strconv.Itoa(min(percent, percentCap)) + "%"
	}
	facts := m.th.statusFaint.Render(fmt.Sprintf(subAgentFactsFormat, phase, elapsed, fill))
	lead, style := glyph+" ", lipgloss.NewStyle()
	if isViewed {
		lead, style = lead+m.th.panelCurrent.Render(glyphPanelCurrent)+" ", m.th.panelCurrent
	}
	return m.panelRow(lead, row.Name, style, facts)
}

// panelTaskRow is one task: done, the current one (the first unfinished, marked ▸), or still open.
// The text is the model's own, so it is escape-stripped and cut to its first line.
func (m Model) panelTaskRow(row sidepanel.TaskRow) string {
	text := firstLine(stripEscapes(row.Text))
	switch {
	case row.IsDone:
		return m.panelRow(m.th.successMark.Render(m.th.glyphs.done)+" ", text, m.th.statusFaint, "")
	case row.IsCurrent:
		return m.panelRow(m.th.panelCurrent.Render(glyphPanelCurrent)+" ", text, m.th.panelCurrent, "")
	default:
		return m.panelRow(m.th.statusFaint.Render(glyphPanelOpen)+" ", text, lipgloss.NewStyle(), "")
	}
}

// panelFileRow is one changed file: its path, relative to the workspace where it lies inside it and
// trimmed from the left to the room the row has, then ↳ when a sub-agent wrote it and the `+N −M`
// its Edit regions add up to.
func (m Model) panelFileRow(row sidepanel.FileRow) string {
	facts := make([]string, 0, 2)
	if row.IsDelegated {
		facts = append(facts, glyphPanelDelegated)
	}
	if row.HasStat {
		facts = append(facts, diffCounts(row.Stat.Added, row.Stat.Removed))
	}
	right := ""
	if len(facts) > 0 {
		right = m.th.statusFaint.Render(strings.Join(facts, " "))
	}
	room := sidePanelContentWidth
	if right != "" {
		room -= m.th.measure.Width(right) + 1
	}
	path := m.transcript.ws.shorten(firstLine(stripEscapes(row.Path)))
	return m.panelRow("", trimPathLeft(m.th, path, room), lipgloss.NewStyle(), right)
}

// trimPathLeft cuts path from the left, a whole component at a time, behind `…/` until it fits
// room; a final component too long even alone is clipped from its right with an ellipsis.
func trimPathLeft(th theme, path string, room int) string {
	if th.measure.Width(path) <= room {
		return path
	}
	rest := path
	for {
		cut := strings.IndexAny(rest, `/\`)
		if cut < 0 {
			return truncateToWidth(th, rest, room)
		}
		rest = rest[cut+1:]
		if trimmed := sidePanelPathElision + rest; th.measure.Width(trimmed) <= room {
			return trimmed
		}
	}
}

// panelUsageRows is the usage section's body: the fill bar with the tokens it stands for, the
// system / tools / history split, the tokens in and out, the cache hit rate and the spend — each
// row only where the reading has it.
func (m Model) panelUsageRows(view sidepanel.UsageView) []string {
	plain := lipgloss.NewStyle()
	rows := make([]string, 0, 5)
	if fill := view.Fill; fill.Window > 0 && fill.Used > 0 {
		approx := ""
		if fill.IsEstimate {
			approx = "~"
		}
		bar := renderGaugeBar(m.th, fill.Used, fill.Window) + " "
		rows = append(rows, m.panelRow(bar, approx+format.Tokens(fill.Used)+"/"+format.Tokens(fill.Window), m.th.statusFaint, ""))
	}
	if view.HasSplit {
		split := "sys " + panelTokens(view.System) + sidePanelPartSeparator + "tools " + panelTokens(view.Tools) +
			sidePanelPartSeparator + "hist " + panelTokens(view.History)
		rows = append(rows, m.panelRow("", split, plain, ""))
	}
	totals := view.Totals
	if totals.Calls > 0 {
		inOut := "in " + panelTokens(totals.PromptTokens) + sidePanelPartSeparator + "out " + panelTokens(totals.CompletionTokens)
		rows = append(rows, m.panelRow("", inOut, plain, ""))
	}
	if totals.PromptTokens > 0 && totals.CachedPromptTokens > 0 {
		hit := totals.CachedPromptTokens * percentCap / totals.PromptTokens
		rows = append(rows, m.panelRow("", "cache hit "+strconv.Itoa(hit)+"%", plain, ""))
	}
	if spend := spendText(totals, m.currency()); spend != "" {
		rows = append(rows, m.panelRow("", "spend "+spend, plain, ""))
	}
	return rows
}

// panelTokens spells a count in the gauge's coarse form, and a zero as `0` rather than as the
// empty cell format.Tokens leaves for it: a usage row states a split, where none is a value.
func panelTokens(n int) string {
	if n <= 0 {
		return "0"
	}
	return format.Tokens(n)
}
