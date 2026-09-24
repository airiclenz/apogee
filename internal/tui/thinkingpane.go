package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// The /thinking pane — the model's reasoning as plain text
//
// This file holds the pane whole, the way usage.go and inspector.go each hold one: the rows it
// composes out of the thinking board (thinking.go, which retains and paints nothing) and the
// [reportContent] it hands the shared report module (reportpane.go). The row LIST keeps no state —
// it is derived for the frame that asks for it, so a chunk arriving between two paints can never
// leave the pane showing a list nobody folded. What is remembered between frames is only each
// record's wrapped text ([thinkingRowCache]), validated on the text itself, so a remembered wrap
// can never outlive the text it was wrapped from.
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
// defensive: the verb only ever opens the pane scoped as the view already is
// ([Model.runThinkingCommand]).
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
	return m.runLabel(rec.run) + " · " + turn
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
//
// The text rows come through the board's wrap memo ([thinkingRowCache]), so a frame re-wraps only
// the records whose text moved since the last one at this column and scope. The HEADING never does:
// it is composed fresh on every call, because [Model.runLabel] reads the transcript, and a
// delegate's record that arrives before its run's head entry must go on from the fallback label to
// the real one without its text changing by a byte.
func (m Model) thinkingRows(column int) ([]popupRow, []popupRowKind) {
	records := m.scopedThinking()
	if len(records) == 0 {
		return []popupRow{{thinkingEmptyRow}}, []popupRowKind{popupRowPlain}
	}
	wrapped := m.thinking.rows.wrapRecords(records, column, m.viewedRun())
	total := len(records)
	for _, text := range wrapped {
		total += len(text)
	}
	rows := make([]popupRow, 0, total)
	kinds := make([]popupRowKind, 0, total)
	for i, rec := range records {
		rows = append(rows, popupRow{m.thinkingHeading(rec)})
		kinds = append(kinds, popupRowHeading)
		for _, row := range wrapped[i] {
			rows = append(rows, popupRow{row})
			kinds = append(kinds, popupRowPlain)
		}
	}
	return rows, kinds
}

// wrapThinkingText is one record's text as the pane's plain rows at column: split on the text's own
// newlines first and each line wrapped on its own ([Model.thinkingRows] says why).
func wrapThinkingText(text string, column int) []string {
	var rows []string
	for _, line := range strings.Split(text, "\n") {
		rows = append(rows, wrapReadable("", line, column)...)
	}
	return rows
}

// thinkingRowKey names one record's place in the scoped list the memo can recognise across frames:
// whose it is, its Turn, and how many earlier records in the same list share both — an ordinal
// rather than a list index, so the board dropping its oldest record past [maxThinkingRecords] does
// not shift every key. The key is only where to LOOK; whether what is found still holds is decided
// by the text ([thinkingRowEntry]).
type thinkingRowKey struct {
	run     runRef
	turn    int
	ordinal int
}

// thinkingRowEntry is one memoised record: the text it was wrapped from and the rows that wrap
// made. The rows are handed out read-only — [Model.thinkingRows] copies them into its own list.
type thinkingRowEntry struct {
	text string
	rows []string
}

// thinkingRowCache is the /thinking pane's wrap memo: each scoped record's wrapped text rows, for
// the one column and scope the last render used. It lives behind a pointer on the board (thinking.go
// says why) and is nil-receiver-safe, so a board built without one renders uncached.
//
// It is a VALIDATION memo, like the transcript's paint cache (paintcache.go): an entry is served only
// when the record's text is == the text it was wrapped from. Length is not enough — at
// [thinkingRecordCap] an ASCII record holds exactly that many bytes however many more chunks land,
// so a length key would freeze the pane on the first full record. The comparison costs nothing in
// the steady state, since an unchanged record is the SAME string on both sides. A column or scope
// change drops every entry; each render keeps only the entries it used, so a dropped, trimmed-away
// or out-of-scope record's rows are released on the next frame.
//
// wraps counts records actually wrapped over the memo's life — a diagnostic the tests pin the reuse
// property on. Nothing in production reads it.
type thinkingRowCache struct {
	column  int
	scope   runRef
	entries map[thinkingRowKey]thinkingRowEntry
	wraps   int
}

// newThinkingRowCache returns an empty memo. Production builds exactly one, in newModel.
func newThinkingRowCache() *thinkingRowCache {
	return &thinkingRowCache{entries: make(map[thinkingRowKey]thinkingRowEntry)}
}

// wrapRecords returns each record's wrapped text rows at column, in the order given, re-wrapping
// only the records whose text the memo does not hold for this column and scope. A nil memo wraps
// every record and remembers nothing.
func (c *thinkingRowCache) wrapRecords(records []thinkingRecord, column int, scope runRef) [][]string {
	wrapped := make([][]string, len(records))
	if c == nil {
		for i, rec := range records {
			wrapped[i] = wrapThinkingText(rec.text, column)
		}
		return wrapped
	}
	if c.column != column || c.scope != scope {
		clear(c.entries)
		c.column, c.scope = column, scope
	}
	kept := make(map[thinkingRowKey]thinkingRowEntry, len(records))
	for i, rec := range records {
		key := thinkingRowKey{run: rec.run, turn: rec.turn}
		for _, seen := kept[key]; seen; _, seen = kept[key] {
			key.ordinal++
		}
		entry, ok := c.entries[key]
		if !ok || entry.text != rec.text {
			entry = thinkingRowEntry{text: rec.text, rows: wrapThinkingText(rec.text, column)}
			c.wraps++
		}
		kept[key] = entry
		wrapped[i] = entry.rows
	}
	c.entries = kept
	return wrapped
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
		title += " — " + m.runLabel(m.viewedRun())
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

// runThinkingCommand drives the /thinking verb: it opens the pane and does nothing else.
// Synchronous like /usage and /inspect — no engine call, no worker, no I/O — and safe while a
// worker works for the same reason: every chunk it shows was folded onto this Model when the engine
// revealed it, and thinking a human wants to READ is thinking the agent is doing right now.
//
// It opens at the END of the list it is going to show — the SCOPED one, the viewed run's alone
// under a run view ([Model.scopedThinking]). The records are newest-last, so the Turn worth reading
// is the last one, and a pane that opened on the oldest of a session's records would ask for a
// hundred page-downs before it said anything. The top is set past the last row and the pane opens
// FOLLOWING ([reportKind.follows]), so the window is the last full one on every paint
// ([Model.reportSpec]) — the frame's answer for this paint, not something this verb can know — and
// the reasoning the agent is streaming right now goes on arriving under a pane that shows it.
func (m Model) runThinkingCommand() (tea.Model, tea.Cmd) {
	rows, _ := m.thinkingRows(m.thinkingWrapColumn())
	m.thinkingPane = reportPane{open: true, top: len(rows), follow: true}
	m.layout()
	return m, nil
}
