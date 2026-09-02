package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// ----------------------------------------------------------------------------
// The read-only report panes — one module behind /usage, /inspect and /thinking
// ----------------------------------------------------------------------------
//
// A REPORT is the frame's lightest kind of overlay: a scrolled list of rows that answers a question
// the human asked and decides nothing. Three of them exist — the /usage token accounting (usage.go),
// the /inspect raw-protocol view (inspector.go) and the /thinking plain-reasoning pane
// (thinkingpane.go) — and everything about them except their ROWS is the same pane, so it is written
// once here and named three times there.
//
// What a report is, stated once:
//
//   - It is NOT modal. It says something rather than asking, so the box behind it stays live: esc,
//     the four scroll keys and — on /inspect alone — ctrl+r are its whole keyboard, every other key
//     goes exactly where it would go with no report up (handleKey), and a printable key still opens
//     a message. That is also why the rendering toggle is a CHORD and never a plain letter: a
//     printable key belongs to the box behind the pane, and a report may not take it.
//   - It has no selection. Nothing in it is chosen (popupSpec.selected is −1), which is what makes
//     the window open where the reader scrolled it rather than around a cursor.
//   - It scrolls off the PAINTER. The rows a key or a wheel notch moves are the ones the frame DREW
//     (reportWindow, reading renderPopupPlaced's own placement), so the two ways in can never
//     disagree about which rows are on the screen, and an offset left over from a taller window or a
//     shorter list corrects itself the first time it is moved instead of drifting.
//   - Its scroll is clamped to the last FULL window rather than to the last row, so a report scrolled
//     to its end shows a full pane of rows.
//   - It FOLLOWS the tail while it is left at the end — /inspect and /thinking do ([reportKind.follows]),
//     /usage keeps the clamp alone — so rows arriving under an open pane are shown rather than
//     landing below a frozen window, and scrolling up off the end is what stops it. It is the
//     transcript's own behaviour ([Model.detached], model.go) under a report's smaller contract.
//   - Closing it takes the scroll with it, and /inspect's choice of rendering with it too: a
//     reportPane's zero value is "closed at the top, not following, readable", so the next open
//     starts where the first one did.
//
// The POINTER does the two things the keyboard has no key for (handleReportClick, reportWheel): a
// click OUTSIDE the box dismisses the report — the gesture esc already is, made with the hand that is
// already on the mouse — and a click INSIDE is swallowed, so a press on a report cannot arm a
// drag-selection across the transcript lines it is drawn over. The wheel scrolls a row per notch and
// CLAMPS at both ends: a wheel is a scroll, so rolling past the last row must not land the reader
// back at the first.
//
// Dismissing does NOT swallow the click. The pane is not modal — the input box behind it stays live
// and every key goes where it always went (layout.md) — so a click that lands in the prompt seats the
// caret there as it would have with no report up, and one on the transcript starts its selection. The
// report going away is a side effect of acting elsewhere, not an act of its own.
//
// The reports are the ONLY panes of the transcript-side slot that can be up TOGETHER (View), and they
// are asked in the order the slot draws them (handleMouseClick), so a click on the /inspect pane
// dismisses the /usage report before it reaches it. What makes that order safe is the PRE-CLICK frame, not the geometry:
// the slot is bottom-anchored (frameOverlays.transcriptRows), so the lower pane's bottom edge is fixed
// and losing the one above it grows the box UPWARD — by MORE rows than that one was drawn on whenever
// it was drawn shorter than its grant (frameRowPlan), because the whole grant goes back into the
// division. Resolved against the already-dismissed model, the regrown box would swallow a click the
// human aimed at the gap row or the transcript above it. So the rect a handler tests is the one the
// frame drew when the button went down, and a click in the band the box grew into falls through to the
// dismissal it was aimed at.

// reportKind names one of the read-only report panes. It is this module's ONE parameter: every
// function below takes it, resolves the pane's state and its content through it, and is otherwise the
// same code for all of them — which is what keeps "what a report does" a single answer rather than
// three copies that drift a key at a time.
//
// The three functions that resolve something THROUGH a kind — [reportKind.pane],
// [Model.reportState] and [Model.reportContent] — are exhaustive switches with a panicking default
// rather than an `if` over the odd one out. Written as an `if`, a kind that missed a branch compiled
// and painted ANOTHER pane's state or content inside its own box: a wrong pane rather than a build
// error. The default is unreachable for every declared kind, and TestReportKindsResolveDistinctly
// walks them all to keep it so.
type reportKind int

const (
	usageReport    reportKind = iota // /usage — the session's token accounting (usage.go)
	inspectReport                    // /inspect — the raw wire traffic (inspector.go)
	thinkingReport                   // /thinking — the model's plain reasoning (thinkingpane.go)
	reportKinds                      // not a kind: the count, so a walk over the reports needs no hand-written list
)

// pane is the report's slot in the frame's row allocation — the one thing about a report the frame
// knows before the pane is composed ([Model.popupBudget], framePane).
func (r reportKind) pane() framePane {
	switch r {
	case usageReport:
		return paneUsage
	case inspectReport:
		return paneInspector
	case thinkingReport:
		return paneThinking
	default:
		panic(fmt.Sprintf("tui: no frame pane for report kind %d", r))
	}
}

// reportPane is a report overlay's whole state: whether it is up, how far its row list is scrolled,
// whether it is FOLLOWING the tail of that list, and — for /inspect, the one report whose records
// carry two renderings — which of them it is showing. The rows themselves are derived at render time
// from the folds or the ring, so there is nothing here to keep in step with them. Its zero value is
// "closed at the top, not following, readable", so it lives inline in the value-copied Model like the
// picker and the settings pane (ADR 0011).
type reportPane struct {
	open   bool
	top    int  // the first row the window shows (popupSpec.rowTop) — what the wheel and the scroll keys move
	follow bool // the window is pinned to the tail, so rows arriving under it are shown ([reportKind.follows])
	raw    bool // /inspect only: show the pretty-printed protocol rather than the readable rendering (ctrl+r)
}

// follows says whether the named report FOLLOWS the tail of its row list: whether a pane left at the
// end keeps showing the end as rows arrive under it, rather than standing still while the reader
// watches a frozen window. It is the report module's ONE statement of that scope, read by the three
// functions that arm it or honour it ([Model.reportKey], [Model.reportWheel], [Model.reportSpec]).
//
// /inspect and /thinking follow, because they are windows onto a stream a reader opens WHILE it is
// arriving — reasoning and wire traffic land under the pane as the agent works, and a pane that
// froze on the record it opened on would say nothing about the call the reader is watching. It is
// the transcript's own behaviour ([Model.detached], model.go) under a report's smaller contract.
//
// /usage does not. Its rows grow too — a delegate row per run that reports a count, plus the session
// total (usageRows) — so this is a scope and not a statement about a static pane: it is a reading of
// an accounting, asked and answered, rather than a stream a reader sits and watches, and its scroll
// stays the clamp-only one it has always had.
//
// It is an exhaustive switch with a panicking default for the reason the three resolvers above are
// (reportKind's doc): a fourth report has to state its own answer here rather than inherit /usage's
// by falling through.
func (r reportKind) follows() bool {
	switch r {
	case usageReport:
		return false
	case inspectReport, thinkingReport:
		return true
	default:
		panic(fmt.Sprintf("tui: no follow answer for report kind %d", r))
	}
}

// reportState points at the named report's state inside THIS Model value — the module's one statement
// of which field a report keeps its {open, top} in, read through and written through alike.
//
// The pointer never outlives the call: every caller below is a value-receiver method that reads or
// mutates through it and returns its own copy, so nothing here puts a self-pointer on a Model that is
// copied on every Update (ADR 0011).
func (m *Model) reportState(r reportKind) *reportPane {
	switch r {
	case usageReport:
		return &m.usagePane
	case inspectReport:
		return &m.inspector
	case thinkingReport:
		return &m.thinkingPane
	default:
		panic(fmt.Sprintf("tui: no pane state for report kind %d", r))
	}
}

// reportContent is everything ONE report says about itself that this module cannot know: what the box
// is called, the keys it spells, how many rows it likes to show at once, its rows, their kinds, and
// the prose it shows in place of rows where it has none. ALL of it is composed for THIS frame,
// title included — /inspect names the run its view scoped it to (inspector.go), so the box and the
// rows under it speak for the same thing on every paint.
//
// rowCap is the pane's own taste and not a limit the frame respects; [Model.popupBudget] cuts it down
// to what the window can seat. body is "" for a report that never words an empty state.
type reportContent struct {
	title  string
	hint   string
	rowCap int
	body   string
	rows   []popupRow
	kinds  []popupRowKind
}

// reportContent composes the named report's content for this frame. It is this module's ONE call into
// the panes' own files, and the only place a report's kind decides anything about what it holds.
func (m Model) reportContent(r reportKind) reportContent {
	switch r {
	case usageReport:
		return usageContent(m.usageRows())
	case inspectReport:
		return m.inspectContent()
	case thinkingReport:
		return m.thinkingContent()
	default:
		panic(fmt.Sprintf("tui: no content for report kind %d", r))
	}
}

// reportSpec composes a report's [popupSpec] for THIS frame — the content, the budget the frame
// granted and the window the scroll landed on. ok is false when the frame cannot seat the pane at all.
//
// The content is composed BEFORE the budget is asked for, because what the pane demands is what it has
// to show — the same order every other overlay composes in (renderPicker, renderSessionBrowser). It is
// a step of its own for the reason the /settings key list's is (settingsKeyListSpec): the painter is
// not the composition's only reader, and the KEYS and the WHEEL have to be told about the very window
// that was drawn (reportWindow) rather than a second derivation of it.
func (m Model) reportSpec(r reportKind, c reportContent) (popupSpec, bool) {
	maxBody, shown, seated := m.popupBudget(r.pane(), len(c.rows), c.rowCap, popupChrome, popupFloor{})
	if !seated {
		return popupSpec{}, false
	}
	// The scroll is clamped to the LAST full window rather than to the last row: a report scrolled to
	// its end shows a full pane of rows, and a stale offset — the grant shrank with the window, or the
	// rows dropped — is corrected here rather than painting one row over an empty pane. That clamp is
	// the NOT-following path, and it stays: a detached pane is exactly where a stale offset is kept.
	//
	// A FOLLOWING pane ([reportKind.follows]) does not read the offset at all. Its window is DERIVED
	// as the last full one on every paint, which is what keeps /inspect and /thinking on the tail
	// while records arrive under them, and what lands an opening pane on the newest record — the verbs
	// arm the follow and set the top past the last row (runInspectCommand, runThinkingCommand), so it
	// is the follow rather than the clamp that answers for where they open.
	last := max(0, len(c.rows)-shown)
	rowTop := clampInt(m.reportState(r).top, 0, last)
	if r.follows() && m.reportState(r).follow {
		rowTop = last
	}
	return popupSpec{
		title:       c.title,
		body:        c.body,
		maxBodyRows: maxBody,
		rows:        c.rows,
		rowKinds:    c.kinds,
		selected:    -1, // a report has no selection: nothing here is chosen (the popup module's convention)
		rowTop:      rowTop,
		hint:        c.hint,
		maxRows:     shown,
		scrollbar:   m.popupScrollbarOn(),
	}, true
}

// renderReport paints the named report, or "" when it is closed or the frame cannot seat it.
func (m Model) renderReport(r reportKind) string {
	if !m.reportState(r).open {
		return ""
	}
	spec, seated := m.reportSpec(r, m.reportContent(r))
	if !seated {
		return "" // the frame cannot seat this pane beside its siblings (frameRowPlan)
	}
	return renderPopup(m.th, spec, m.width)
}

// dismissReport takes the named report off the frame and gives its rows back to the transcript. Both
// ways of closing it spend this one — esc (reportKey) and a click outside the box (handleReportClick)
// — so the two can never come apart, and neither can leave the scroll behind for the next open.
func (m Model) dismissReport(r reportKind) Model {
	*m.reportState(r) = reportPane{}
	m.layout()
	return m
}

// reportScrollStep spells how far each scroll key moves a report: one row for the arrows, one window
// for the page keys — byPage rather than a number, because how big a page is is the frame's answer for
// this paint and not this table's. ok is false for every key a report does not scroll on.
//
// One table for both panes: they answer to the same four keys with the same two step sizes, and a
// second table would be a place for them to drift apart one key at a time.
func reportScrollStep(key string) (step int, byPage, ok bool) {
	switch key {
	case "up":
		return -1, false, true
	case "down":
		return 1, false, true
	case "pgup":
		return -1, true, true
	case "pgdown":
		return 1, true, true
	}
	return 0, false, false
}

// reportKey is a report's whole key contract: esc closes it, ↑/↓ scroll it a row at a time,
// pgup/pgdown a drawn window at a time, and ctrl+r — /inspect's alone — flips that pane between its
// readable and its raw rendering. handled is false for every other key, because a report is NOT
// modal (the doctrine above) — the box behind it stays live and a printable key opens a message
// exactly as it would with no report up (handleKey).
//
// The rows it moves are the ones the frame DREW (reportWindow), which is what makes a key and a wheel
// notch move the same list by the same arithmetic and stop at the same two ends: the first row, and
// the last FULL window. A pane the frame could not seat claims neither key — there is nothing on the
// screen for them to mean anything against, and swallowing pgup there would leave the transcript
// unscrollable behind a report that is not drawn.
func (m Model) reportKey(r reportKind, msg tea.KeyPressMsg) (bool, tea.Model, tea.Cmd) {
	if !m.reportState(r).open {
		return false, m, nil
	}
	key := msg.String()
	if key == "esc" {
		return true, m.dismissReport(r), nil
	}
	// The rendering toggle is /inspect's and not the module's: /usage has ONE rendering, and a key
	// that flipped nothing there would still swallow a keystroke the live box behind the pane was
	// owed. It is claimed whether or not the frame seated the pane, exactly as esc is — what it moves
	// is state the pane carries, not a window the painter has to have drawn.
	if key == "ctrl+r" && r == inspectReport {
		state := m.reportState(r)
		state.raw = !state.raw
		return true, m, nil
	}
	step, byPage, scrolls := reportScrollStep(key)
	if !scrolls {
		return false, m, nil
	}
	win, drawn := m.reportWindow(r)
	if !drawn {
		return false, m, nil
	}
	shown := win.end - win.start
	if byPage {
		step *= max(1, shown)
	}
	last := max(0, win.total-shown)
	state := m.reportState(r)
	state.top = clampInt(win.start+step, 0, last)
	// The follow is RE-DERIVED from where the key landed, never toggled: scrolling up off the end
	// detaches the pane, and scrolling back down onto the last full window re-arms it. That is what
	// keeps the invariant total in both directions — a pane is following exactly when its window sits
	// at the tail — rather than a latch some path could leave set the wrong way (the transcript keeps
	// its own the same way, every writer re-deriving it: [Model.detached], model.go).
	if r.follows() {
		state.follow = state.top >= last
	}
	return true, m, nil
}

// ----------------------------------------------------------------------------
// Where a report is drawn, and what the pointer does there
// ----------------------------------------------------------------------------

// block is the rendered block of one pane of the frame, "" when that pane is not on it. It is what
// lets the slot's order be WALKED (transcriptSlotPanes, model.go) instead of re-listed field by field
// at every rectangle in it.
func (o frameOverlays) block(p framePane) string {
	switch p {
	case panePrompt:
		return o.prompt
	case paneBrowser:
		return o.browser
	case panePicker:
		return o.picker
	case paneSettings:
		return o.settings
	case paneUsage:
		return o.usage
	case paneInspector:
		return o.inspector
	case paneThinking:
		return o.thinking
	case paneDropdown:
		return o.dropdown
	}
	return ""
}

// reportPaneRect is where the named report is drawn: the screen row its top border lands on and how
// many rows it takes. ok is false when it is not on the frame at all — closed, or given way to a
// window too short to seat it (frameRowPlan).
//
// It is settingsPaneRect (mouse.go) asking the frame's published geometry under a different pane name:
// the slot is walked ONCE, by the composer that draws it (stackTranscriptSlot, model.go), so the same
// body answers for every report and for every other tenant of the slot.
func (m Model) reportPaneRect(r reportKind) (y0, h int, ok bool) {
	if !m.reportState(r).open {
		// Asked before the frame is composed, because every click and every wheel notch asks: with no
		// report up there is nothing to place, and composing the frame's overlays to learn that would
		// put a render on the path of a click the pane has no part in.
		return 0, 0, false
	}
	return m.frameSpans().pane(r.pane())
}

// reportWindow is the row window a report is showing as the frame DREW it: which rows of its list the
// pane holds, and how many rows that list has in all. It is everything the keys and the wheel need —
// whether there is anything above the window to scroll back to, and anything below it to scroll on to.
//
// ok is false wherever there is nothing to scroll: the pane closed or given way, or a frame that
// cannot seat it.
type reportWindow struct {
	start, end int // the [start, end) rows of the report the pane is showing
	total      int // the rows it holds
}

// reportWindow composes the named report exactly as the frame does and reports the window it landed
// on. It renders the pane to get the answer, the price settingsPaint already pays: the painter is the
// authority on which rows are on the screen, and asking it costs less than an arithmetic that can
// disagree with it.
func (m Model) reportWindow(r reportKind) (reportWindow, bool) {
	if !m.reportState(r).open {
		return reportWindow{}, false
	}
	spec, seated := m.reportSpec(r, m.reportContent(r))
	if !seated {
		return reportWindow{}, false
	}
	_, place := renderPopupPlaced(m.th, spec, m.width)
	return reportWindow{start: place.start, end: place.end, total: len(spec.rows)}, true
}

// handleReportClick answers a left-click while the named report is up: inside the box it is claimed
// and nothing happens — a report has nothing to click ON, and swallowing the press is what keeps it
// from arming a drag across the transcript underneath — and outside it the report is dismissed and the
// click goes on to name whatever it landed on. claimed says only which of the two it was.
//
// The box is the one pre drew — the pre-click frame (handleMouseClick) — and the dismissal is applied
// to the live receiver. That is also what keeps a click in the band the LOWER report regrows into,
// once the one above it is dismissed, out of its rectangle (the doctrine above).
func (m Model) handleReportClick(r reportKind, pre Model, msg tea.MouseClickMsg) (Model, bool) {
	y0, h, ok := pre.reportPaneRect(r)
	if !ok {
		return m, false
	}
	if msg.Y >= y0 && msg.Y < y0+h {
		return m, true
	}
	return m.dismissReport(r), false
}

// reportWheel scrolls the named report one row per notch while the pointer is over it, and CLAMPS at
// both ends. handled is false anywhere else, which leaves the notch to the transcript scrolling above
// and behind the pane — and true over a report short enough to show every row it has, where the notch
// moves nothing because there is nothing off the screen to move to.
func (m Model) reportWheel(r reportKind, msg tea.MouseWheelMsg) (Model, bool) {
	y0, h, ok := m.reportPaneRect(r)
	if !ok || msg.Y < y0 || msg.Y >= y0+h {
		return m, false
	}
	win, ok := m.reportWindow(r)
	if !ok {
		return m, true
	}
	landed := win.start
	switch {
	case msg.Button == tea.MouseWheelUp && win.start > 0:
		landed = win.start - 1
		m.reportState(r).top = landed
	case msg.Button == tea.MouseWheelDown && win.end < win.total:
		landed = win.start + 1
		m.reportState(r).top = landed
	}
	// Re-derived ONCE, after the switch and never inside its two branches: neither of them fires on a
	// notch that has nowhere to go, and a pane detached by a wheel-up whose rows then dropped past
	// their cap would sit at the tail with nothing left to re-arm it. It is read off the window
	// ALREADY in hand — the branches write only the top, so that window is still the one drawn — and
	// a second [Model.reportWindow] here would recompose the whole pane on every notch the pointer
	// makes over it.
	if r.follows() {
		m.reportState(r).follow = landed >= max(0, win.total-(win.end-win.start))
	}
	return m, true
}
