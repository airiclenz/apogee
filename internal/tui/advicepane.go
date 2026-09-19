package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/airiclenz/apogee/internal/domain"
)

// The /advice pane — every advise firing the model was handed, as plain text
//
// This file holds the pane whole, the way thinkingpane.go holds /thinking: the BOARD the fold keeps
// ([Model.advice], written by [Model.foldAdvice] and read by nothing else), the rows the pane
// composes out of it, and the [reportContent] it hands the shared report module (reportpane.go).
// The pane itself keeps no state beyond its reportPane — the rows are derived for the frame that
// asks for them, so a firing arriving between two paints can never leave the pane showing a list
// nobody folded.
//
// What the pane is FOR: reading what the model was TOLD beside its own reply. An advise reaction
// speaks to the model inside the tool result it fired on — a user `advise:` entry's fenced text
// under its `[advice — …]` header, the context-fill notice's rung (ADR 0076 D7, ADR 0077) — and the
// transcript never shows that text: it is the model's reading, not the human's conversation. The
// engine books each of those as a domain.ReactionFiredEvent whose Action is "advise" (an armed
// advise entry, Detail = the CAPPED text the fence carried — internal/agent/reactions.go) or
// "notice" (the context-fill notice, Detail = the rung and percent it fired at —
// internal/agent/fillnotice.go), and those two are exactly what the board folds. Every other
// action — a Floor guard's retry, an intercept, a gate's verdict — is engine behaviour correcting
// the model rather than advice it read, and stays out.
//
// ENGINE NOTES ARE ABSENT, and deliberately. The structural notes the engine appends to a tool
// result — the step notice, the token-budget notice, the delegations ledger, the cancelled cut
// (domain.RenderEngineNote) — are not reactions: no reaction fires to write them, so no
// ReactionFiredEvent books them and nothing here can see them. A reader who wants the whole tail
// the model saw has /inspect for the bytes; this pane is the advice slot alone.
//
// The scope is the WHOLE SESSION, every run's firings in arrival order, grouped by the Turn they
// fired in. Unlike /thinking it does not narrow under a run view: a delegate's advice is named by
// its run in the heading, and the question the pane answers — "what has the model been told?" —
// is asked of the session rather than of the agent on screen. The board is BOUNDED like its
// siblings (maxThinkingRecords, maxWireRecords): past [maxAdviceRecords] the OLDEST firing is
// dropped, because a user advise entry fires on every tool call with a Detail up to
// domain.AdviceCap, and "whole session" has to stay a size the value-copied Model carries.
//
// The rows are wrapped at the pane's REAL width for this frame ([Model.thinkingWrapColumn]), the
// rule thinkingpane.go states and this pane shares: report rows are TRUNCATED at the border and
// never re-wrapped (popup.go), and a pane with one rendering and no horizontal scroll has no way
// back to a cut line.

// adviceTitle names the pane, and adviceHint spells the keys it owns — the four scroll keys and
// esc, with no ctrl+r: one rendering, the same contract as /thinking's.
const (
	adviceTitle = "advice"
	adviceHint  = "↑/↓ scroll · esc close"
)

// adviceEmptyRow is the whole pane when the board holds nothing. It is a ROW rather than a body,
// the shape thinkingEmptyRow and inspectorEmptyRow use, so an empty pane scrolls, budgets and
// paints as every other report does.
const adviceEmptyRow = "no advice recorded yet"

// The two ReactionFiredEvent.Action labels the board folds. They are spelled here rather than
// imported because the TUI does not depend on the agent package: adviceActionAdvise is
// internal/agent/reactions.go's actionAdvise — an armed advise entry's injection at the
// tool-result seam — and adviceActionNotice is internal/agent/fillnotice.go's actionNotice, the
// context-fill notice's own label. TestAdviceBoardFoldsAdviseFiringsInOrder pins both spellings.
const (
	adviceActionAdvise = "advise"
	adviceActionNotice = "notice"
)

// maxAdviceRecords is how many firings the board keeps, newest last. Past it the OLDEST is
// dropped: the pane opens on the newest, and the oldest is what a reader would have scrolled off.
// Wider than the thinking board's 64 because a firing is one fenced text rather than a Turn's
// whole reasoning, and a user advise entry can fire on every tool call of a long session.
const maxAdviceRecords = 256

// adviceRecord is one advise firing as the view retains it: whose run it fired in and which Turn,
// the reaction that fired, its origin and Moment, and the Detail the engine booked — the capped
// fenced text for an advise entry, the rung for the notice, escape-stripped at this seam (doc.go).
//
// run is the {depth, spawn} run identity every fold in the view keys on (runRef — transcript.go),
// where the ZERO value is the human's own top-level conversation; turn is domain.EventBase.Turn off
// the firing's own event.
type adviceRecord struct {
	run      runRef
	turn     int
	reaction string
	origin   domain.Origin
	moment   domain.Moment
	detail   string
}

// foldAdvice appends one advise firing to the board (the eventMsg fold). Every Event that is not
// a ReactionFiredEvent with Action "advise" or "notice" leaves the Model untouched — the Floor
// guards' retries and intercepts are the transcript's debug view's business (transcript.addReaction),
// not this pane's. The board is rebuilt rather than appended into, so no two Model values share its
// backing array (ADR 0011), and it is trimmed to [maxAdviceRecords] from the FRONT before the new
// record lands, so the newest firing is always kept.
func (m Model) foldAdvice(e domain.Event) Model {
	fired, ok := e.(domain.ReactionFiredEvent)
	if !ok || (fired.Action != adviceActionAdvise && fired.Action != adviceActionNotice) {
		return m
	}
	keep := m.advice
	if len(keep) >= maxAdviceRecords {
		keep = keep[len(keep)-maxAdviceRecords+1:]
	}
	next := make([]adviceRecord, 0, len(keep)+1)
	next = append(next, keep...)
	m.advice = append(next, adviceRecord{
		run:      runOf(fired.EventBase),
		turn:     fired.Turn,
		reaction: fired.Reaction,
		origin:   fired.Origin,
		moment:   fired.Moment,
		detail:   stripEscapes(fired.Detail),
	})
	return m
}

// adviceHeading names one group of firings — the run and the Turn they fired in — in
// [Model.thinkingHeading]'s spelling exactly: `turn 4` for the main agent, `<run label> · turn 4`
// for a delegate's, the label spelled the way every surface that names a run spells it
// ([Model.runLabel]). The two headings are kept in step by TestAdvicePaneGroupsRowsByRunAndTurn,
// because a reader moving between the two panes reads the same owner off the same words.
func (m Model) adviceHeading(run runRef, turn int) string {
	heading := "turn " + strconv.Itoa(turn)
	if run.depth == 0 {
		return heading
	}
	return m.runLabel(run.spawn) + " · " + heading
}

// adviceFiringRow is the one row every firing opens with — `<reaction> (<origin> origin) @
// <moment>` — which is the ledger row the fence header carries to the model, in the pane's own
// words: the reaction's id as its config key, whose it was, and the seam it fired at. It is a PLAIN
// row under the group's heading, never a heading of its own: the heading names the Turn, and a Turn
// may hold several firings.
func adviceFiringRow(rec adviceRecord) string {
	return rec.reaction + " (" + string(rec.origin) + " origin) @ " + string(rec.moment)
}

// adviceRows composes the report at the given wrap column: for each run of firings sharing a run
// and a Turn, oldest first, one heading row, then per firing its firing row and its Detail as plain
// rows. An empty board is ONE row, and there is only one of those — nothing about this pane can be
// switched off, so an empty pane has exactly one thing to say.
//
// The grouping is CONSECUTIVE: a heading is emitted wherever the (run, Turn) key changes from the
// record before, which under a fan-out that interleaves two delegates' firings names each stretch
// by its own agent rather than re-sorting the arrival order the engine gave them.
//
// The Detail is split on its own newlines FIRST and each line wrapped separately, as the thinking
// rows are ([Model.thinkingRows]): a line the handler wrote starts flush against the marker column
// and only a continuation carries the two-space indent, so a wrap reads as a wrap. A firing with an
// empty Detail is its firing row alone.
func (m Model) adviceRows(column int) ([]popupRow, []popupRowKind) {
	if len(m.advice) == 0 {
		return []popupRow{{adviceEmptyRow}}, []popupRowKind{popupRowPlain}
	}
	rows := make([]popupRow, 0, len(m.advice)*3)
	kinds := make([]popupRowKind, 0, len(m.advice)*3)
	for i, rec := range m.advice {
		if i == 0 || rec.run != m.advice[i-1].run || rec.turn != m.advice[i-1].turn {
			rows = append(rows, popupRow{m.adviceHeading(rec.run, rec.turn)})
			kinds = append(kinds, popupRowHeading)
		}
		rows = append(rows, popupRow{adviceFiringRow(rec)})
		kinds = append(kinds, popupRowPlain)
		if rec.detail == "" {
			continue
		}
		for _, line := range strings.Split(rec.detail, "\n") {
			for _, row := range wrapReadable("", line, column) {
				rows = append(rows, popupRow{row})
				kinds = append(kinds, popupRowPlain)
			}
		}
	}
	return rows, kinds
}

// adviceContent is what the pane tells the shared report module about itself for one frame
// (reportpane.go): its name, the keys it spells, how tall it likes to be, and the rows with the
// kinds composed beside them. The wrap column is taken from the Model rather than from the caller
// for the reason thinkingContent's is: the rows and the box around them are two halves of ONE
// answer about a width.
func (m Model) adviceContent() reportContent {
	rows, kinds := m.adviceRows(m.thinkingWrapColumn())
	return reportContent{
		title:  adviceTitle,
		hint:   adviceHint,
		rowCap: maxInspectorRows,
		rows:   rows,
		kinds:  kinds,
	}
}

// runAdviceCommand drives the /advice verb: it opens the pane and does nothing else. Synchronous
// like /thinking, /usage and /inspect — no engine call, no worker, no I/O — and safe while a worker
// works for the same reason: every firing it shows was folded onto this Model when the engine booked
// it, and the advice a human wants to READ is advice the model is acting on right now.
//
// It does not toggle. A second /advice on an open pane RE-OPENS it — the top set past the last row
// and the follow re-armed — exactly as [Model.runThinkingCommand] does, so the verb is always "show
// me the newest" and never "hide it"; closing is esc or a click outside the box (reportKey,
// handleReportClick). The top is set past the last row and the pane opens FOLLOWING
// ([reportKind.follows]), so the window is the last full one on every paint ([Model.reportSpec])
// and a firing arriving while the pane is up lands under a pane that shows it.
func (m Model) runAdviceCommand() (tea.Model, tea.Cmd) {
	rows, _ := m.adviceRows(m.thinkingWrapColumn())
	m.advicePane = reportPane{open: true, top: len(rows), follow: true}
	m.layout()
	return m, nil
}

// The report module's functions under this pane's name (reportpane.go). Each one is the shared body
// with adviceReport filled in: naming them here is what lets the frame, the keyboard and the pointer
// go on addressing the /advice pane as itself while there is only one report left to maintain.

// renderAdvice paints the pane, or "" when it is closed or the frame cannot seat it.
func (m Model) renderAdvice() string { return m.renderReport(adviceReport) }

// adviceSpec composes the pane's [popupSpec] for THIS frame — its rows, the budget the frame
// granted and the window the scroll landed on ([Model.reportSpec]).
func (m Model) adviceSpec() (popupSpec, bool) {
	return m.reportSpec(adviceReport, m.adviceContent())
}

// adviceKey is the pane's whole key contract: esc closes it, ↑/↓ scroll a row at a time and
// pgup/pgdown a drawn window at a time (reportKey). There is no sixth key — one rendering.
func (m Model) adviceKey(msg tea.KeyPressMsg) (bool, tea.Model, tea.Cmd) {
	return m.reportKey(adviceReport, msg)
}

// dismissAdvice takes the pane off the frame and gives its rows back to the transcript. The scroll
// goes with it: the next open lands on the newest firing again.
func (m Model) dismissAdvice() Model { return m.dismissReport(adviceReport) }

// advicePaneRect is where the open pane is drawn: the screen row its top border lands on and how
// many rows it takes.
func (m Model) advicePaneRect() (y0, h int, ok bool) { return m.reportPaneRect(adviceReport) }

// adviceWindow is the row window the pane is showing as the frame DREW it.
func (m Model) adviceWindow() (reportWindow, bool) { return m.reportWindow(adviceReport) }
