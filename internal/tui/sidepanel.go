package tui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
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
	// sidePanelSectionsSource is the label a failed pin write-back's transcript error carries — the
	// key's own name, taskListOpenSource's posture.
	sidePanelSectionsSource = "side-panel.sections"
)

// sidePanelContentWidth is the columns the panel's content has: the column less its separator and
// the gutter beside it.
const sidePanelContentWidth = sidePanelWidth - len(sidePanelGutter) - 1

// panelSection is one section the column shows: which it is, what its header says and its body
// rows, painted.
type panelSection struct {
	kind     domain.SidePanelSection
	title    string
	summary  string
	isPinned bool
	body     []string
}

// foldSidePanel folds e into the side panel's state, copy-on-write: an Event the panel reads goes
// into a clone of the held State (a fresh one on the first such Event) and the clone replaces the
// pointer, so a State another Model copy still holds is never written (ADR 0011). window is the
// Event's run's bound window, the fill's fallback where a reading names none. A landed write is
// folded under its workspace-relative key ([Model.panelWriteKey]), the key a resumed record's write
// is rebuilt under ([Model.rebuildSidePanel]), so a file rewritten after a resume stays one row.
func (m Model) foldSidePanel(e domain.Event, window int) Model {
	if !sidepanel.Reads(e) {
		return m
	}
	if result, ok := e.(domain.ToolResultEvent); ok && result.WriteTarget != "" {
		result.WriteTarget = m.panelWriteKey(result.WriteTarget)
		e = result
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

// panelLingerMsg is the repaint armed for the instant the sub-agents section's linger runs out
// ([Model.armPanelLinger]). gen is the arm that scheduled it (sessionView.panelLingerGen); a tick
// for a deadline an Event has since moved or retired is dropped.
type panelLingerMsg struct{ gen int }

// armPanelLinger keeps one repaint armed for the end of the sub-agents section's linger. The
// section's live rule is judged only at paint time, so without it an idle frame would show the
// section until some unrelated repaint. The deadline and the time left to it are the panel's own
// answer on the panel's own clock ([sidepanel.State.SubAgentLinger]); a deadline unchanged since the
// last arm arms nothing, and any change — a later end, or a delegation starting — retires the tick
// before it by moving the generation.
func (m Model) armPanelLinger() (Model, tea.Cmd) {
	linger := m.panel.SubAgentLinger()
	if linger.Until.Equal(m.panelLingerUntil) {
		return m, nil
	}
	m.panelLingerUntil = linger.Until
	m.panelLingerGen++
	if linger.Until.IsZero() {
		return m, nil
	}
	gen := m.panelLingerGen
	return m, tea.Tick(linger.Left, func(time.Time) tea.Msg { return panelLingerMsg{gen: gen} })
}

// foldPanelLinger takes the armed linger tick. Returning the Model is the repaint; the armed
// deadline is spent, and arming again re-reads the panel, so a tick that landed before the panel's
// clock reached the deadline arms the remainder instead of leaving the section up.
func (m Model) foldPanelLinger(msg panelLingerMsg) (Model, tea.Cmd) {
	if msg.gen != m.panelLingerGen {
		return m, nil
	}
	m.panelLingerUntil = time.Time{}
	return m.armPanelLinger()
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
	layout := panelLayout(sections, rows)
	lines := make([]string, 0, len(layout))
	for _, line := range layout {
		switch {
		case line.more > 0:
			lines = append(lines, m.th.statusFaint.Render("+"+strconv.Itoa(line.more)+sidePanelMore))
		case line.body < 0:
			lines = append(lines, m.panelHeaderRow(sections[line.section]))
		default:
			lines = append(lines, sections[line.section].body[line.body])
		}
	}
	return lines
}

// panelLine is what one content row of the column is: a section's header (body −1) or one of its
// body rows, or — section −1, more the count — the `+N more` row.
type panelLine struct {
	section int
	body    int
	more    int
}

// panelLayout lays the shown sections out over rows rows by the overflow rule ([panelAllocation]):
// one panelLine per content row, from the top. It is the one layout the paint
// ([Model.sidePanelContent]) and the pointer ([Model.panelTargetAt]) both read, so a click names
// the row it lands on.
func panelLayout(sections []panelSection, rows int) []panelLine {
	kept, isExpanded := panelAllocation(sections, rows)
	lines := make([]panelLine, 0, rows)
	for i := range sections[:kept] {
		lines = append(lines, panelLine{section: i, body: -1})
		if !isExpanded[i] {
			continue
		}
		for j := range sections[i].body {
			lines = append(lines, panelLine{section: i, body: j})
		}
	}
	if left := len(sections) - kept; left > 0 {
		lines = append(lines, panelLine{section: -1, body: -1, more: left})
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
		section.kind, section.isPinned = entry.Section, entry.Mode == domain.SidePanelModePinned
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

// panelTargetKind is what a click on one cell of the column does (docs/layout/side-panel-layout.md,
// "Pointer").
type panelTargetKind int

const (
	panelTargetNone  panelTargetKind = iota // a header title, the `+N more` row, a blank cell: nothing
	panelTargetPin                          // a header's pin glyph: toggle pinned ↔ auto, written back
	panelTargetRun                          // a sub-agent row: open that run's Run view
	panelTargetBlock                        // a task or changed-file row: unfold its block and scroll to it
	panelTargetUsage                        // a usage row: open /usage
)

// panelTarget is one column cell's click: what it does, and what it does it to — the section a pin
// toggles, or the transcript entry a run row opens or a block row unfolds, with the run whose view
// shows that block.
type panelTarget struct {
	kind     panelTargetKind
	section  domain.SidePanelSection
	isPinned bool
	entry    int
	run      runRef
}

// panelCell reports whether (x, y) is one of the column's cells: right of the transcript slot, on
// a row above the session-name rule, while the column shows.
func (m Model) panelCell(x, y int) bool {
	return m.panelShown() && x >= m.slotWidth() && x < m.width && y >= 0 && y < m.frameSpans().slotBottom
}

// panelTargetAt answers what a click at (x, y) does on the column of the frame m composes (pre in
// the click chain, which carries that frame's spans), and whether (x, y) is a column cell at all. A
// cell is claimed whether or not it targets anything, so no click on the column falls through to
// the panes or the transcript under it; the hover shape asks the same question (pointershape.go),
// so the pointer promises a click exactly where one acts.
func (m Model) panelTargetAt(x, y int) (panelTarget, bool) {
	if !m.panelCell(x, y) {
		return panelTarget{}, false
	}
	sections := m.panelSections()
	layout := panelLayout(sections, m.frameSpans().slotBottom)
	if len(sections) == 0 || y >= len(layout) || layout[y].section < 0 {
		return panelTarget{}, true
	}
	line, section := layout[y], sections[layout[y].section]
	if line.body >= 0 {
		return m.panelBodyTarget(section.kind, line.body), true
	}
	if x == m.width-1 {
		return panelTarget{kind: panelTargetPin, section: section.kind, isPinned: section.isPinned}, true
	}
	return panelTarget{}, true
}

// panelBodyTarget is the click on body row row of section kind: the detail view that row opens,
// or none where the transcript holds nothing it could open.
func (m Model) panelBodyTarget(kind domain.SidePanelSection, row int) panelTarget {
	switch kind {
	case domain.SidePanelSubAgents:
		rows := m.panel.SubAgents().Rows
		if row >= len(rows) {
			return panelTarget{}
		}
		at := m.panelRunHeadAt(rows[row].Run)
		if _, ok := m.runOpenableAt(at); !ok {
			return panelTarget{}
		}
		return panelTarget{kind: panelTargetRun, entry: at}
	case domain.SidePanelTasks:
		if at, ok := m.panelTaskCardAt(); ok {
			return panelTarget{kind: panelTargetBlock, entry: at, run: m.viewedRun()}
		}
	case domain.SidePanelChangedFiles:
		rows := m.panel.ChangedFiles(panelRunKey(m.viewedRun())).Rows
		if row >= len(rows) {
			return panelTarget{}
		}
		if at, ok := m.panelWriteAt(rows[row]); ok {
			return panelTarget{kind: panelTargetBlock, entry: at, run: m.transcript.entries[at].run()}
		}
	case domain.SidePanelUsage:
		return panelTarget{kind: panelTargetUsage}
	}
	return panelTarget{}
}

// panelRunHeadAt is where the delegation heading run sits in the transcript, −1 where none does.
func (m Model) panelRunHeadAt(run sidepanel.RunKey) int {
	entries := m.transcript.entries
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].headsRun() && panelRunKey(entries[i].spawned()) == run {
			return i
		}
	}
	return -1
}

// panelTaskCardAt is where the viewed run's latest task-list card sits: the card the task section
// reads, since each task-list call replaces the list.
func (m Model) panelTaskCardAt() (int, bool) {
	entries, scope := m.transcript.entries, m.viewedRun()
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].tool.collapsesToHeader && entries[i].run() == scope {
			return i, true
		}
	}
	return -1, false
}

// panelWriteAt is where the tool block of row's latest write sits: the call of that id in the run
// that landed it, and only where the view could show it — the viewed run's own, or a run with a
// head to open.
func (m Model) panelWriteAt(row sidepanel.FileRow) (int, bool) {
	if row.CallID == "" {
		return -1, false
	}
	entries := m.transcript.entries
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.kind != entryToolCall || e.callID != row.CallID || panelRunKey(e.run()) != row.Run {
			continue
		}
		if run := e.run(); run != m.viewedRun() && run != (runRef{}) {
			if _, ok := runHeadAt(entries, run); !ok {
				return -1, false
			}
		}
		return i, true
	}
	return -1, false
}

// handleSidePanelClick answers a left-click on the column, asked before every other region of the
// click chain: the column claims each of its cells, and a click there drops the prompt's and the
// transcript's selections, so nothing under it — a pane's row, a block's header, a drag anchor —
// is reached through it. A target acts on the press (followPanelTarget); a cell that targets
// nothing is claimed and does nothing. pre is the pre-click frame the chain reads geometry off.
func (m Model) handleSidePanelClick(pre Model, msg tea.MouseClickMsg) (tea.Model, tea.Cmd, bool) {
	target, isPanel := pre.panelTargetAt(msg.X, msg.Y)
	if !isPanel {
		return m, nil, false
	}
	m.sel, m.transcriptSel = fieldSel{}, transcriptSel{}
	next, cmd := m.followPanelTarget(target)
	return next, cmd, true
}

// followPanelTarget does what a click on target means: a pin toggles its section between pinned
// and auto, a sub-agent row opens its Run view, a task or changed-file row unfolds that one block
// — never the shared task-list fold, whose toggle writes `ui.task-list-open` — and scrolls it into
// view (opening the run that holds it first), and a usage row opens /usage.
func (m Model) followPanelTarget(target panelTarget) (tea.Model, tea.Cmd) {
	switch target.kind {
	case panelTargetPin:
		mode := domain.SidePanelModePinned
		if target.isPinned {
			mode = domain.SidePanelModeAuto
		}
		return m.setPanelSectionMode(target.section, mode), nil
	case panelTargetRun:
		next, _ := m.openRunAt(target.entry)
		return next, nil
	case panelTargetBlock:
		if target.run != m.viewedRun() {
			m = m.openRun(target.run)
		}
		m.transcript.setExpanded(target.entry, true)
		m.revealEntry(target.entry)
		return m, nil
	case panelTargetUsage:
		return m.runUsageCommand()
	default:
		return m, nil
	}
}

// revealEntry repaints the transcript and scrolls it so entries[index]'s first marked line sits on
// the top row the sticky header leaves uncovered — [Model.followBlockCursor]'s park, asked again
// after the first one because the header's span is a function of the offset. An entry the paint
// marks no line for leaves the view where the repaint put it.
func (m *Model) revealEntry(index int) {
	m.refreshViewport() // geometry: the lookup below reads lineTargets off the repaint that carries the block just unfolded, and the anchor reads its line
	line := slices.IndexFunc(m.lineTargets, func(t lineTarget) bool {
		return t.kind != targetNone && t.entry == index
	})
	if line < 0 {
		return
	}
	m.refreshViewportAnchored(line, 0)
	if _, header := m.stickyHeaderSpan(); header > 0 && header < m.transcriptRows() {
		m.refreshViewportAnchored(line, header)
	}
}

// setPanelSectionMode sets one section's mode — what a pin-glyph click, and `/panel pin|unpin|hide`,
// mean (ADR 0110) — and writes it back through [SettingsHost.WriteSidePanelSection]. The column
// moves first: the screen shows what the human asked for whatever the file then does. A landed
// write puts the list the files resolve to in force and journals it (recordSettingEdit); a failed
// one speaks as one transcript error naming the key, and the change stands for the session — the
// task-list fold's posture (toggleTaskListFold). A nil seam changes the session alone, the Driver
// degrade (ADR 0031).
func (m Model) setPanelSectionMode(section domain.SidePanelSection, mode domain.SidePanelMode) Model {
	m.opts.UI.SidePanelSections = withSectionMode(m.opts.UI.SidePanelSections, section, mode)
	if m.opts.Settings == nil {
		return m
	}
	sections, err := m.opts.Settings.WriteSidePanelSection(section, mode)
	if err != nil {
		m.transcript.addError(sidePanelSectionsSource, taskListNotSavedNote+err.Error(), runRef{})
		return m
	}
	m.opts.UI.SidePanelSections = sections
	return m.recordSettingEdit(settingEdit{path: domain.UIKeySidePanelSections, value: string(sections)})
}

// withSectionMode is list with section's entry set to mode, every other entry kept with its mode
// and in its order, in the canonical spelling.
func withSectionMode(
	list domain.SidePanelSections, section domain.SidePanelSection, mode domain.SidePanelMode,
) domain.SidePanelSections {
	entries, err := domain.ParseSidePanelSections(string(list))
	if err != nil {
		// Unreachable from a running session, for panelSections' reason: the default list stands in.
		entries, _ = domain.ParseSidePanelSections("")
	}
	for i := range entries {
		if entries[i].Section == section {
			entries[i].Mode = mode
		}
	}
	return domain.SpellSidePanelSections(entries)
}

// sidePanelLeastWindow is the narrowest window show draws the column in: the floor the transcript
// keeps beside it — sidePanelAutoFloor under `auto`, sidePanelOnFloor under `on` — plus the column
// and the scroll-bar column, counted painted or not ([Model.panelShown]). ok is false for a mode
// that never draws it: `off`, and the zero UIPrefs' empty mode.
func sidePanelLeastWindow(show domain.SidePanelShow) (least int, ok bool) {
	switch show {
	case domain.SidePanelShowAuto:
		return sidePanelAutoFloor + sidePanelWidth + scrollbarWidth, true
	case domain.SidePanelShowOn:
		return sidePanelOnFloor + sidePanelWidth + scrollbarWidth, true
	default:
		return 0, false
	}
}

// ----------------------------------------------------------------------------
// /panel — the side panel's settings from the prompt box (ADR 0110 decision 8)
// ----------------------------------------------------------------------------

// sidePanelShowSource is the label a failed `/panel on|off|auto` write's transcript error carries —
// the key's own name, sidePanelSectionsSource's posture.
const sidePanelShowSource = "side-panel.show"

// panelAction is the subcommand of a parsed /panel line. The zero value is panelReport, so a bare
// "/panel" — and a line the parser never ran this grammar over, which is what the completion menu's
// accept builds — reports rather than writes anything (the /skills posture).
type panelAction int

const (
	panelReport     panelAction = iota // report the show mode and every section's mode
	panelSetShow                       // set `ui.side-panel.show`
	panelSetSection                    // set one section's mode in `ui.side-panel.sections`
)

// panelArgs is the parsed argument list of a /panel line: what was asked for, and the show mode or
// the section and mode it was asked of.
type panelArgs struct {
	action  panelAction
	show    domain.SidePanelShow
	section domain.SidePanelSection
	mode    domain.SidePanelMode
}

// panelUsage is the one-line grammar every /panel argument error carries, so a mistyped line teaches
// the syntax instead of quietly reporting as though nothing had been asked.
const panelUsage = "usage: /panel | /panel on|off|auto | /panel pin|unpin|hide <section>"

// panelSectionVerbs maps each section subcommand onto the mode it sets: `unpin` takes a section back
// to `auto`, the mode a pin-glyph click unpins it to (followPanelTarget).
var panelSectionVerbs = map[string]domain.SidePanelMode{
	"pin":   domain.SidePanelModePinned,
	"unpin": domain.SidePanelModeAuto,
	"hide":  domain.SidePanelModeHidden,
}

// parsePanel parses the argument tokens that followed a "/panel" verb. No arguments means the
// report; a show mode (`on`, `off`, `auto`) alone sets the show mode; `pin`, `unpin` or `hide` takes
// exactly one section this build knows. Everything else is an error carrying panelUsage — a refused
// section names the sections there are.
func parsePanel(args []string) (panelArgs, error) {
	if len(args) == 0 {
		return panelArgs{action: panelReport}, nil
	}
	if show, err := domain.ParseSidePanelShow(args[0]); err == nil {
		if len(args) != 1 {
			return panelArgs{}, fmt.Errorf("/panel %s takes nothing after it. %s", args[0], panelUsage)
		}
		return panelArgs{action: panelSetShow, show: show}, nil
	}
	mode, ok := panelSectionVerbs[args[0]]
	if !ok {
		return panelArgs{}, fmt.Errorf("unknown /panel subcommand %q. %s", args[0], panelUsage)
	}
	if len(args) != 2 {
		return panelArgs{}, fmt.Errorf("/panel %s takes exactly one section. %s", args[0], panelUsage)
	}
	sections := domain.SidePanelSectionNames()
	section := domain.SidePanelSection(args[1])
	if !slices.Contains(sections, section) {
		known := make([]string, 0, len(sections))
		for _, name := range sections {
			known = append(known, string(name))
		}
		return panelArgs{}, fmt.Errorf("unknown side-panel section %q (known sections: %s). %s",
			args[1], strings.Join(known, ", "), panelUsage)
	}
	return panelArgs{action: panelSetSection, section: section, mode: mode}, nil
}

// runPanelCommand routes a parsed /panel line: set the show mode, or one section's mode, and either
// way — the bare report included — answer with the settings now in force (panelReportNote), so
// the human sees what the panel does even where this window is too narrow to draw it.
//
// No upstream call and no worker: a write is one config key through the seams the settings pane and
// the pin glyph use, synchronous like theirs.
func (m Model) runPanelCommand(args panelArgs) (tea.Model, tea.Cmd) {
	switch args.action {
	case panelSetShow:
		m = m.setPanelShow(args.show)
	case panelSetSection:
		m = m.setPanelSectionMode(args.section, args.mode)
	}
	m.transcript.addNote(m.panelReportNote())
	return m, nil
}

// setPanelShow sets `ui.side-panel.show` and writes it back through [SettingsHost.Write], with
// setPanelSectionMode's whole contract: the column moves first, a landed write is journalled
// (recordSettingEdit), a failed one speaks as one transcript error naming the key and the change
// stands for the session, and a nil seam changes the session alone (ADR 0031).
func (m Model) setPanelShow(show domain.SidePanelShow) Model {
	m.opts.UI.SidePanelShow = show
	if m.opts.Settings == nil {
		return m
	}
	if err := m.opts.Settings.Write(domain.UIKeySidePanelShow, string(show)); err != nil {
		m.transcript.addError(sidePanelShowSource, taskListNotSavedNote+err.Error(), runRef{})
		return m
	}
	return m.recordSettingEdit(settingEdit{path: domain.UIKeySidePanelShow, value: string(show)})
}

// panelReportNote words the side panel's settings in force: the show mode, then every section with
// its mode in display order — `side panel: auto — sub-agents auto · tasks pinned · …` — and, where
// the mode would draw the column but this window is too narrow for it, the width it needs.
func (m Model) panelReportNote() string {
	show := m.opts.UI.SidePanelShow
	if show == "" {
		show = domain.SidePanelShowOff // the renderer draws the zero UIPrefs' empty mode as off (panelShown)
	}
	entries, err := domain.ParseSidePanelSections(string(m.opts.UI.SidePanelSections))
	if err != nil {
		// Unreachable from a running session, for panelSections' reason: the default list stands in.
		entries, _ = domain.ParseSidePanelSections("")
	}
	modes := make([]string, 0, len(entries))
	for _, entry := range entries {
		modes = append(modes, string(entry.Section)+" "+string(entry.Mode))
	}
	note := "side panel: " + string(show) + " — " + strings.Join(modes, sidePanelPartSeparator)
	if least, ok := sidePanelLeastWindow(show); ok && !m.panelShown() {
		note += " (not drawn: the window is narrower than " + strconv.Itoa(least) + " columns)"
	}
	return note
}

// ----------------------------------------------------------------------------
// Resume — the panel rebuilt from a stored record (ADR 0110 decision 4)
// ----------------------------------------------------------------------------

// taskListToolName is the model's own checklist tool (ADR 0072), whose cards a resumed session's
// task lists are rebuilt from.
const taskListToolName = "task_list"

// panelWriterTargetArgs names, for each of apogee's workspace-scoped writer tools — the tools whose
// landed results carry a write target (domain.ToolResultEvent.WriteTarget) — the argument naming
// that target. A stored card keeps its call's arguments (toolView.argsWire) and not the engine's
// resolved target, so a resumed session's writes are rebuilt from this argument.
var panelWriterTargetArgs = map[string]string{
	"write_file":              "path",
	"edit_existing_file":      "path",
	"single_find_and_replace": "path",
	"multi_find_and_replace":  "path",
	"delete_file":             "path",
	"copy_file":               "destination",
	"move_file":               "destination",
}

// rebuildSidePanel folds a resumed record's replayed entries into a fresh panel state, in place of
// the Event journal a resumed session does not have (docs/layout/side-panel-layout.md, "Replay").
// Every card that came off counts and none that failed, was refused or was interrupted does — the
// live fold's rule, which reads only a successful result: each run's task list is its LAST such
// task_list card, keyed by the run that called it, so a child's card sets the child's list alone;
// and each such writer card is a landed write, keyed as the live fold keys one
// ([Model.panelWriteKey]). Sub-agents starts empty — the record keeps no phase to fold — and usage
// is seeded by the caller once the record's accounting is restored ([Model.seedSidePanelUsage]).
//
// The state is replaced whole, and left nil where the record holds nothing the panel shows: the
// caller has already dropped the outgoing session's.
func (m *Model) rebuildSidePanel(entries []entry) {
	var state *sidepanel.State
	ensure := func() *sidepanel.State {
		if state == nil {
			state = sidepanel.New(time.Now)
		}
		return state
	}
	for _, e := range entries {
		if e.kind != entryToolCall || !e.done || e.tool.Summary.failed {
			continue
		}
		run := panelRunKey(e.run())
		if e.tool.name == taskListToolName {
			ensure().SetTasks(run, panelTaskItems(e.tool.Details.all()))
			continue
		}
		if path, ok := m.replayedWriteKey(e.tool); ok {
			ensure().AddWrite(run, e.depth, e.callID, path, regionsStat(e.tool.Regions))
		}
	}
	if state != nil {
		m.panel = state
	}
}

// panelTaskItems reads a task_list card's tasks back off the rows its body lays out — the rows
// taskListCount counts, by the same two markers — in order. A card with no rows is a cleared list.
func panelTaskItems(lines []detailLine) []domain.TaskItem {
	items := make([]domain.TaskItem, 0, len(lines))
	for _, ln := range lines {
		switch {
		case strings.HasPrefix(ln.Text, taskListDoneMarker):
			items = append(items, domain.TaskItem{Text: strings.TrimPrefix(ln.Text, taskListDoneMarker), Done: true})
		case strings.HasPrefix(ln.Text, taskListOpenMarker):
			items = append(items, domain.TaskItem{Text: strings.TrimPrefix(ln.Text, taskListOpenMarker)})
		}
	}
	return items
}

// replayedWriteKey is the key a stored writer card's write folds under: its target argument
// (panelWriterTargetArgs) as panelWriteKey keys it. A card of any other tool, or one whose stored
// arguments no longer name the target — elided whole, or the path itself bounded to a byte count
// (boundedArgValue's `…[N bytes]`) — names no write.
func (m Model) replayedWriteKey(tv toolView) (string, bool) {
	arg, ok := panelWriterTargetArgs[tv.name]
	if !ok {
		return "", false
	}
	path, ok := parseArgs(tv.argsWire)[arg].(string)
	if !ok || path == "" || strings.HasPrefix(path, "…[") {
		return "", false
	}
	return m.panelWriteKey(path), true
}

// regionsStat is the diffstat a write's Edit regions add up to — what the live fold reads off the
// result's domain.EditRegions — and nil for a write that kept none.
func regionsStat(regions []domain.EditRegion) *domain.DiffStat {
	if len(regions) == 0 {
		return nil
	}
	stat := domain.EditRegions{Regions: regions}.Stat()
	return &stat
}

// panelWriteKey is the one key a changed file is folded under, live and on resume alike: the path
// relative to the workspace where it lies inside it, cleaned and absolute where it lies outside or
// no workspace is known. A relative path is read against the workspace, as the writer tools read
// it. One key on both sides is what keeps a file rewritten after a resume on one row, whether the
// stored argument spelled it relative or absolute; the painter shortens either the same way
// (panelFileRow).
func (m Model) panelWriteKey(path string) string {
	root := m.transcript.ws.root
	if !filepath.IsAbs(path) && root != "" {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)
	if root == "" {
		return path
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(rel) {
		return path
	}
	return rel
}

// seedSidePanelUsage seeds the usage section's totals from a resumed record's accounting, once both
// resume doors have restored it (replayResumed, resumeLoaded): the main agent's base and the
// delegates' total, as /usage sums the session. The restored ctxUsed rides along for the State's
// own fill, but this Driver's section reads the gauge's ([Model.panelFill]), which that same
// restored ctxUsed relights. A record that counted no call seeds nothing.
func (m *Model) seedSidePanelUsage() {
	totals := domain.Sum(m.usageBase, m.delegateUsageTotal())
	if totals.Calls <= 0 {
		return
	}
	next := m.panel.Clone()
	if next == nil {
		next = sidepanel.New(time.Now)
	}
	next.SetUsage(sidepanel.RunKey{}, totals, m.ctxUsed)
	m.panel = next
}
