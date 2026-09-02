package tui

import (
	"strconv"
	"strings"
)

// The /thinking pane — the model's reasoning as plain text
//
// This file holds the pane whole, the way usage.go and inspector.go each hold one: the rows it
// composes out of the thinking board (thinking.go, which retains and paints nothing) and the
// [reportContent] it hands the shared report module (reportpane.go). Nothing here keeps state —
// the rows are derived for the frame that asks for them, so a chunk arriving between two paints
// can never leave the pane showing a list nobody folded.
//
// What the pane is FOR is what its rendering rules follow from: reading the model's own reasoning
// as prose. So the rows carry NO prefixes, no JSON, no tool-call passages, no per-record elision
// and no turn metadata inside the body — the byte cap on the board is the only bound, and the text
// IS the content. That is the deliberate opposite of /inspect, whose readable view is passages of
// the wire payload dressed with the kind-naming prefixes that view needs; a reader who wants the
// bytes has /inspect for them, which is also why this pane has no ctrl+r rendering toggle.
//
// The one structural row is the HEADING that opens each record — `turn 4`, or
// `<run label> · turn 4` for a delegate's — because a scrolled list of unattributed paragraphs
// cannot be read back to a Turn, and the board's records are per agent per Turn.
//
// The rows are composed at the pane's REAL width and not at a constant, which is the rule the rest
// of this file exists to keep. Report rows are TRUNCATED to the pane's inner width by the popup
// module (popup.go, truncateToWidth) and never re-wrapped, so text composed wider than the pane is
// text CUT OFF — and in a pane with no raw toggle and no horizontal scroll, cut off unrecoverably.
// /inspect survives its fixed column because its rows are wire records with ctrl+r behind them; a
// pane built for reading prose does not, so the wrap column is derived per frame below.

// thinkingTitle names the pane, and thinkingHint spells the keys it owns — the four scroll keys and
// esc, with no ctrl+r: the pane has ONE rendering, and a hint naming a key the pane does not answer
// would be the box lying about itself.
const (
	thinkingTitle = "thinking"
	thinkingHint  = "↑/↓ scroll · esc close"
)

// thinkingEmptyRow is the whole pane when the scope holds nothing: no capture switch to name (the
// board is unconditional — thinking.go) and nothing to fix, so it states the fact and stops. It is
// a ROW rather than a body, the shape inspectorEmptyRow already uses, so an empty pane scrolls,
// budgets and paints as every other report does.
const thinkingEmptyRow = "no thinking recorded yet"

// minThinkingWrapColumn is the floor under the derived wrap column. Below roughly this the frame
// will not seat the pane at all ([Model.popupBudget]), so the floor exists to keep the arithmetic
// from handing [wrapReadable] a zero or negative budget on a window mid-resize rather than to make
// a two-column pane readable.
const minThinkingWrapColumn = 20

// thinkingWrapColumn is the pane's REAL row budget for THIS frame, in runes: the width inside the
// border (popup.go, popupInnerWidth), less the two-cell marker column every row leads with
// (popupRowIndent), less the column the overflow bar draws down (scrollbarWidth).
//
// The bar's column is reserved WHETHER OR NOT the bar is drawn. It appears exactly when the row
// list outgrows the window, so a column that counted it only then would re-wrap every row of the
// pane the moment one more row arrived — the text would reflow under the reader mid-scroll for a
// reason that has nothing to do with the text.
//
// This is what makes the row list width-dependent: a resize recomposes it and the scroll offset
// lands elsewhere in the text. That is the correction [Model.reportSpec]'s clamp already applies on
// every frame, and it is the price of never cutting a rune off a line the reader cannot get back.
func (m Model) thinkingWrapColumn() int {
	return max(popupInnerWidth(m.th, m.width)-popupRowIndent-scrollbarWidth, minThinkingWrapColumn)
}

// scopedThinking is the record list the pane speaks for in THIS frame, oldest first, with the
// scoped run's IN-FLIGHT record — where one is in flight — at the tail.
//
// The tail is the live record's own arrival position and not a special slot: the board is
// newest-last, and an in-flight Turn is the newest thing the scoped agent has thought. Under a
// fan-out that matters, because a rule stated against an agent's LAST COMMITTED record would seat a
// delegate's partial text between two records that completed after it started (ADR 0039).
//
// The scope filter is [Model.inThinkingScope], applied to the committed records and the in-flight
// ones alike so the two halves of the list can never disagree about whose thinking this is. It is
// defensive: item 3's claim route only opens the pane scoped as the view already is.
//
// The slice is FRESH (ADR 0011): the Model is copied by value on every Update, so a slice handed
// back over the board's own backing array would let a later fold write into rows a frame is still
// drawing.
func (m Model) scopedThinking() []thinkingRecord {
	scoped := make([]thinkingRecord, 0, len(m.thinking.done)+len(m.thinking.live))
	for _, rec := range m.thinking.done {
		if m.inThinkingScope(rec.run) {
			scoped = append(scoped, rec)
		}
	}
	for _, rec := range m.thinking.live {
		if m.inThinkingScope(rec.run) {
			scoped = append(scoped, rec)
		}
	}
	return scoped
}

// inThinkingScope says whether one run's thinking belongs in the pane as the human has it open: the
// MAIN agent's alone at the top level, and the viewed delegation's alone under a run view (the
// ratified sub-agent scoping). Top level is a depth test rather than an equality one, so a
// top-level record that carried a call id is still the human's own conversation rather than a
// record the pane silently drops.
func (m Model) inThinkingScope(run runRef) bool {
	viewed := m.viewedRun()
	if viewed == (runRef{}) {
		return run.depth == 0
	}
	return run == viewed
}

// thinkingHeading names one record: `turn 4` for the main agent, `<run label> · turn 4` for a
// delegate's — the label spelled the way every other surface that names a run spells it
// ([Model.runLabel]), which is the wording /inspect's scoped title already uses. A delegate's
// records are named even under a run view, where every record is that run's: the heading is what a
// reader scrolling back reads a paragraph's owner off, and a list of `turn 4` alone says nothing
// about which agent thought it.
func (m Model) thinkingHeading(rec thinkingRecord) string {
	turn := "turn " + strconv.Itoa(rec.turn)
	if rec.run.depth == 0 {
		return turn
	}
	return m.runLabel(rec.run.spawn) + " · " + turn
}

// thinkingRows composes the report at the given wrap column: for each record the pane speaks for
// ([Model.scopedThinking]), oldest first, one heading row and then the record's text as plain rows.
// An empty scope is ONE row, and there is only one of those — unlike /inspect, nothing about this
// pane can be switched off, so an empty pane has exactly one thing to say.
//
// The text is split on its own newlines FIRST and each line wrapped separately, rather than handed
// to [wrapReadable] as one passage. That is what keeps a WRAP visibly a wrap: a line the model
// wrote starts flush against the marker column and only a continuation carries the two-space
// indent, so the model's own paragraphing survives a pane narrower than the prose it holds. A blank
// line stays a blank row for the same reason.
//
// The kinds are composed in the same pass rather than derived from the rows afterwards: a heading
// is a heading because of where it was put, and a line of reasoning that happened to read like one
// would be styled as a section label by any rule read back off the text.
func (m Model) thinkingRows(column int) ([]popupRow, []popupRowKind) {
	records := m.scopedThinking()
	if len(records) == 0 {
		return []popupRow{{thinkingEmptyRow}}, []popupRowKind{popupRowPlain}
	}
	rows := make([]popupRow, 0, len(records)*2)
	kinds := make([]popupRowKind, 0, len(records)*2)
	for _, rec := range records {
		rows = append(rows, popupRow{m.thinkingHeading(rec)})
		kinds = append(kinds, popupRowHeading)
		for _, line := range strings.Split(rec.text, "\n") {
			for _, row := range wrapReadable("", line, column) {
				rows = append(rows, popupRow{row})
				kinds = append(kinds, popupRowPlain)
			}
		}
	}
	return rows, kinds
}

// thinkingContent is what the pane tells the shared report module about itself for one frame
// (reportpane.go): its name, the keys it spells, how tall it likes to be, and the rows with the
// kinds composed beside them.
//
// It is a METHOD, and it takes the wrap column from the Model rather than from its caller, because
// the rows and the box around them are two halves of ONE answer about a width: composed apart, a
// pane could wrap its rows to a column the frame is not drawing them at. The TITLE joins them for
// the reason /inspect's does — a box called "thinking" over one delegation's records would misname
// what is under it, so the run's name is composed here, beside the rows it belongs to.
func (m Model) thinkingContent() reportContent {
	title := thinkingTitle
	if m.inRunView() {
		title += " — " + m.runLabel(m.viewedRun().spawn)
	}
	rows, kinds := m.thinkingRows(m.thinkingWrapColumn())
	return reportContent{
		title:  title,
		hint:   thinkingHint,
		rowCap: maxInspectorRows,
		rows:   rows,
		kinds:  kinds,
	}
}
