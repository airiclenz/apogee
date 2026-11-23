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
// leave the pane showing a list nobody folded. What is remembered between frames ([thinkingRowCache])
// is each record's wrapped text, line by line, and the composed list of the last frame, each
// validated on what it was built from — a line's text, a record's text, every record's heading — so
// a remembered row can never outlive the text it was wrapped from or the heading it was named by.
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
// (popupRowIndent), less the two the overflow bar takes (popupScrollbarGutter: the bar's column and
// the blank one that sets it off from the text).
//
// Those columns are reserved WHETHER OR NOT the bar is drawn. It appears exactly when the row
// list outgrows the window, so a column that counted it only then would re-wrap every row of the
// pane the moment one more row arrived — the text would reflow under the reader mid-scroll for a
// reason that has nothing to do with the text.
//
// This is what makes the row list width-dependent: a resize recomposes it and the scroll offset
// lands elsewhere in the text. That is the correction [Model.reportSpec]'s clamp already applies on
// every frame, and it is the price of never cutting a rune off a line the reader cannot get back.
func (m Model) thinkingWrapColumn() int {
	return max(popupInnerWidth(m.th, m.slotWidth())-popupRowIndent-popupScrollbarGutter, minThinkingWrapColumn)
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
// ratified sub-agent scoping). A stage level scopes as the run its workflow stands in does
// ([Model.viewedRun]): a stage is not a run. Top level is a depth test rather than an equality one, so a
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
// The rows come through the board's memo ([thinkingRowCache]) twice over. The composed list is
// served whole while every record's text and HEADING are what it was built from, so the frame's
// height query and its paint ([Model.reportHeight], [Model.renderReport]) share one build. The
// headings are computed fresh on every call and are part of that check — [Model.runLabel] reads the
// transcript, and a delegate's record that arrives before its run's head entry must go on from the
// fallback label to the real one without its text changing by a byte. Where the list is rebuilt,
// the text rows come through the wrap memo, which re-wraps only the lines that moved.
func (m Model) thinkingRows(column int) ([]popupRow, []popupRowKind) {
	records := m.scopedThinking()
	if len(records) == 0 {
		return []popupRow{{thinkingEmptyRow}}, []popupRowKind{popupRowPlain}
	}
	headings := make([]string, len(records))
	for i, rec := range records {
		headings[i] = m.thinkingHeading(rec)
	}
	return m.thinking.rows.compose(records, headings, column, m.viewedRun())
}

// composeThinkingRows lays out the pane's list: each record's heading row, then its wrapped text
// rows. Every row's one cell is cut out of a single backing array, capped to its own length, rather
// than allocated one by one — a board at its ceiling is tens of thousands of rows.
func composeThinkingRows(headings []string, wrapped [][]string) ([]popupRow, []popupRowKind) {
	total := len(headings)
	for _, text := range wrapped {
		total += len(text)
	}
	cells := make([]string, 0, total)
	kinds := make([]popupRowKind, 0, total)
	for i, heading := range headings {
		cells = append(cells, heading)
		kinds = append(kinds, popupRowHeading)
		cells = append(cells, wrapped[i]...)
		for range wrapped[i] {
			kinds = append(kinds, popupRowPlain)
		}
	}
	rows := make([]popupRow, len(cells))
	for i := range cells {
		rows[i] = popupRow(cells[i : i+1 : i+1])
	}
	return rows, kinds
}

// wrapThinkingText is one record's text as the pane's plain rows at column: split on the text's own
// newlines first and each line wrapped on its own ([Model.thinkingRows] says why). It is the cold
// reference the wrap memo's per-line reuse must agree with.
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

// thinkingRowEntry is one memoised record: the text it was wrapped from, that text's lines (split on
// its own newlines, as [wrapThinkingText] splits it), each line's wrapped rows, and those rows
// flattened in order. Everything is handed out read-only — a re-wrap builds a fresh entry and never
// writes into one a frame may still be reading.
type thinkingRowEntry struct {
	text     string
	lines    []string
	lineRows [][]string
	rows     []string
}

// rewrap is the entry for text at column, reusing this entry's rows for every line it already
// wrapped. A line's rows depend on nothing but the line and the column, so a line served from the
// memo is one whose text is == the line it was wrapped from — wherever it now sits. Where it sits is
// what [thinkingRowEntry.lineShift] works out, so that a chunk landing on the record costs only the
// lines it changed: the trailing one it grew (and any it added), and — once the record is at
// [thinkingRecordCap] and the board cut its front ([keepLastBytes]) — the first one, which the cut
// shortened. lineWraps on c counts every line actually wrapped.
func (e thinkingRowEntry) rewrap(c *thinkingRowCache, text string, column int) thinkingRowEntry {
	lines := strings.Split(text, "\n")
	shift := e.lineShift(text)
	lineRows := make([][]string, len(lines))
	total := 0
	for i, line := range lines {
		if j := i + shift; j < len(e.lines) && e.lines[j] == line {
			lineRows[i] = e.lineRows[j]
		} else {
			lineRows[i] = wrapReadable("", line, column)
			c.lineWraps++
		}
		total += len(lineRows[i])
	}
	rows := make([]string, 0, total)
	for _, wrapped := range lineRows {
		rows = append(rows, wrapped...)
	}
	return thinkingRowEntry{text: text, lines: lines, lineRows: lineRows, rows: rows}
}

// lineShift is how many of this entry's lines the new text dropped off its front: the board only
// ever GROWS a record at its tail and, at the cap, cuts bytes off its head, so the new text is this
// entry's text with some prefix gone and something appended. With nothing gone the shift is 0. With a
// prefix gone, the new text's second line is one of this entry's later lines, and the one whose
// alignment leaves the old text's remainder a prefix of the new is the shift — checked in full, so a
// shift is only ever reported for an alignment the bytes bear out. Where none does (a record the
// memo met under a different text altogether) the answer is 0 and the line comparison in
// [thinkingRowEntry.rewrap] decides alone: a wrong guess costs wraps, never rows.
func (e thinkingRowEntry) lineShift(text string) int {
	if strings.HasPrefix(text, e.text) {
		return 0
	}
	firstBreak := strings.IndexByte(text, '\n')
	if firstBreak < 0 || len(e.lines) == 0 {
		return 0
	}
	next := 0 // where this entry's line j+1 starts in its text
	for j, line := range e.lines[:len(e.lines)-1] {
		next += len(line) + 1
		cut := next - (firstBreak + 1) // the bytes dropped, if the new second line is line j+1
		if cut < 0 {
			continue
		}
		if strings.HasPrefix(text, e.text[cut:]) {
			return j
		}
	}
	return 0
}

// thinkingComposed is the composed list the last build made — rows and kinds — with the headings and
// texts it was built from, one per record in order. It is served whole only when every one of those
// is == what the frame asks for now ([thinkingComposed.holds]); the column and scope it was built at
// are the cache's own, and a change to either drops it.
type thinkingComposed struct {
	headings []string
	texts    []string
	rows     []popupRow
	kinds    []popupRowKind
}

// holds says whether the composed list was built from exactly these records under exactly these
// headings. A zero value holds nothing: there is always at least one record when this is asked.
func (p thinkingComposed) holds(records []thinkingRecord, headings []string) bool {
	if len(p.texts) != len(records) || len(p.headings) != len(headings) {
		return false
	}
	for i, rec := range records {
		if p.texts[i] != rec.text || p.headings[i] != headings[i] {
			return false
		}
	}
	return true
}

// thinkingRowCache is the /thinking pane's memo, for the one column and scope the last render used:
// each scoped record's wrapped text rows, kept line by line, and the composed list of the last build.
// It lives behind a pointer on the board (thinking.go says why) and is nil-receiver-safe, so a board
// built without one renders uncached.
//
// It is a VALIDATION memo, like the transcript's paint cache (paintcache.go): a record's entry is
// served only when the record's text is == the text it was wrapped from, a line's rows only when the
// line is == the line they were wrapped from, and the composed list only when every record's text
// and heading are == the ones it was built from. Length is not enough — at [thinkingRecordCap] an
// ASCII record holds exactly that many bytes however many more chunks land, so a length key would
// freeze the pane on the first full record. Nor is any generation counter: the memo is shared by
// every copy of the Model (ADR 0011), and a copy whose transcript moved without a word to the memo
// must still be served the heading its own transcript names. The comparisons cost nothing in the
// steady state, since an unchanged record is the SAME string on both sides. A column or scope change
// drops everything; each build keeps only the entries it used, so a dropped, trimmed-away or
// out-of-scope record's rows are released on the next build.
//
// wraps counts records re-wrapped over the memo's life, lineWraps the lines actually wrapped inside
// them, and composes the composed lists built — diagnostics the tests pin the reuse properties on.
// Nothing in production reads them.
type thinkingRowCache struct {
	column    int
	scope     runRef
	entries   map[thinkingRowKey]thinkingRowEntry
	composed  thinkingComposed
	wraps     int
	lineWraps int
	composes  int
}

// newThinkingRowCache returns an empty memo. Production builds exactly one, in newModel.
func newThinkingRowCache() *thinkingRowCache {
	return &thinkingRowCache{entries: make(map[thinkingRowKey]thinkingRowEntry)}
}

// at points the memo at column and scope, dropping everything it holds for any other.
func (c *thinkingRowCache) at(column int, scope runRef) {
	if c.column != column || c.scope != scope {
		clear(c.entries)
		c.composed = thinkingComposed{}
		c.column, c.scope = column, scope
	}
}

// compose returns the pane's composed list for records under headings at column: the last build's
// when it still holds ([thinkingComposed.holds]), a fresh build through the wrap memo otherwise. A nil
// memo builds every time and remembers nothing.
func (c *thinkingRowCache) compose(records []thinkingRecord, headings []string, column int, scope runRef) ([]popupRow, []popupRowKind) {
	if c == nil {
		return composeThinkingRows(headings, c.wrapRecords(records, column, scope))
	}
	c.at(column, scope)
	if c.composed.holds(records, headings) {
		return c.composed.rows, c.composed.kinds
	}
	rows, kinds := composeThinkingRows(headings, c.wrapRecords(records, column, scope))
	texts := make([]string, len(records))
	for i, rec := range records {
		texts[i] = rec.text
	}
	c.composed = thinkingComposed{headings: headings, texts: texts, rows: rows, kinds: kinds}
	c.composes++
	return rows, kinds
}

// wrapRecords returns each record's wrapped text rows at column, in the order given, re-wrapping
// only the records whose text the memo does not hold for this column and scope — and inside those,
// only the lines that moved ([thinkingRowEntry.rewrap]). A nil memo wraps every record and remembers
// nothing.
func (c *thinkingRowCache) wrapRecords(records []thinkingRecord, column int, scope runRef) [][]string {
	wrapped := make([][]string, len(records))
	if c == nil {
		for i, rec := range records {
			wrapped[i] = wrapThinkingText(rec.text, column)
		}
		return wrapped
	}
	c.at(column, scope)
	kept := make(map[thinkingRowKey]thinkingRowEntry, len(records))
	for i, rec := range records {
		key := thinkingRowKey{run: rec.run, turn: rec.turn}
		for _, seen := kept[key]; seen; _, seen = kept[key] {
			key.ordinal++
		}
		entry, ok := c.entries[key]
		if !ok || entry.text != rec.text {
			entry = entry.rewrap(c, rec.text, column)
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
	if m.inRunScope() {
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
	return m, nil
}
