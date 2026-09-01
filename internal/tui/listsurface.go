package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// ----------------------------------------------------------------------------
// The list surface every modal list overlay is built on
// ----------------------------------------------------------------------------
//
// A LIST SURFACE is what the /model | /server picker (picker.go), the /sessions browser
// (sessions.go), the /settings key list with its two sub-lists (settings.go) and the "/" | "@"
// dropdown (autocomplete.go) all are underneath their own wording: a modal list of rows with one
// highlight, a row window the frame grants, and — for the two that filter — a field the human types
// into. Everything about that is written once here and named at each pane, so a pane's whole
// remaining job is its ROWS, its ACCEPT and its HINT (ADR 0053).
//
// It is TWO values, because the panes are two kinds. [listCursor] is where the highlight stands and
// the keys that walk it — the whole state a list that does not filter keeps. [listSurface] is that
// cursor plus the [lineEditor] a filter is typed into, and the keys that type. The split is what let
// the five lists above adopt the module without adopting a text widget none of them types into: a
// lineEditor is a textarea, thousands of bytes in a Model copied on every Update, and a surface
// handed to a pane that never filters would multiply exactly the field this module must not
// (ADR 0053 decision 9). The two DECISION panes — the approval menu and the ask_user offering —
// take less again: the cursor's walk and its clamp without its key contract, for the reason its own
// doc gives.
//
// What a list surface is, stated once:
//
//   - Its rows are DERIVED, never held. They are re-composed per frame and per keypress from the
//     state they describe — the models a beat advertised, the sessions the store holds — and handed
//     IN to the methods below. What the surface keeps is only the two things a frame cannot
//     re-derive: where the highlight stands and what has been typed.
//   - The highlight indexes the FILTERED rows, and every accept is resolved through the filter
//     (pickerView.offeringIndex) rather than against the list underneath it. With a filter set, "the
//     third painted row" and "the third model advertised" are different rows, and taking the second
//     would act on something the human never saw.
//   - The selection is CLAMPED rather than trusted, on every key, because the rows underneath can
//     have changed since the last one — and again after a key that moved the FILTER, because the
//     rows it leaves standing can be fewer than the highlight was pointing at.
//   - It is MODAL. Every key it is offered is either one of its own or handed back once
//     (listVerdict), and the pane swallows what neither of them wanted: no letter is a verb inside
//     an open list, which is what makes every letter the filter's. A list with NO filter hands its
//     letters back instead, and the pane says what that means: the /settings key list swallows them
//     (it is modal too), while the dropdown gives them to the chat box it is completing a token in.
//     That is a fact about the list OVERLAYS. The two decision panes that borrow the cursor alone
//     are SOFT-modal — the transcript stays scrollable under the approval prompt, the answer box
//     stays typeable under the question — so they claim their arrows and leave every other key to
//     the surface it belonged to (listCursor).
//   - What ↑/↓ do at the ENDS is a parameter and not a rule (listWrap) — the panes already disagree
//     and each keeps the answer it had. What the WHEEL does at the ends is NOT a parameter: it CLAMPS
//     in every one of them (listCursor.wheel), because a wheel is a scroll and rolling past the last
//     row onto the first would move the human somewhere they did not aim (ratified 2026-08-22).
//   - The filter LINE is the surface's, not the painter's spec. Its label, its caret, its two
//     blank lines and the budget claim all three cost ride here (renderFilterList), so a pane cannot
//     paint a line it did not claim room for. A pane with a body of its own instead — the /settings
//     sub-list's question — states it as body and takes the same budget→render call (renderList).
//
// The surface decides nothing about what a key MEANS. Closing an overlay, resuming a session,
// rebinding a model: those are the pane's own acts with the pane's own consequences, and a
// listVerdict is what asks for one. That is the split that lets eleven pickerKinds, three browser
// modes, three /settings steps and two dropdown kinds share one key contract without this file
// knowing that any of them exist.

// listVerdict is what a [listSurface] DID with one keypress, and the whole of what it tells its pane.
type listVerdict int

const (
	listSwallowed listVerdict = iota // the surface acted, or deliberately did nothing; the modal keeps the key
	listCloses                       // esc — the pane closes
	listAccepts                      // ⏎ on a seated row — the pane's own accept runs
	listUnclaimed                    // none of the surface's keys: the pane's own verbs get it
)

// listWrap says what ↑ on the first row and ↓ on the last one do. It is a PARAMETER rather than a
// rule because the panes already answer it differently and every one of them keeps the answer it
// had (the deepening plan's merge policy, 2026-08-19): the four list OVERLAYS wrap around — the
// picker, the /sessions browser, the /settings lists and the dropdown — while the two DECISION
// panes, the approval menu and the ask_user offering, stop at the ends. Stopping is the answer a
// surface a decision is taken on wants: ↑ on the first row jumping to the last one is the
// difference between a stray keypress and the Cancel row (approval.go).
type listWrap bool

const (
	listStopsAtEnds listWrap = false // ↓ on the last row stays on it
	listWrapsAround listWrap = true  // ↓ on the last row returns to the first
)

// listCursor is where a list overlay's highlight stands, and — for the lists that do not filter —
// the whole state such a list keeps between keypresses. It is a plain value with no lock and no
// self-pointer, so a pane EMBEDS it inline in the value-copied Model (ADR 0011, ADR 0053) and the
// field below reads as the pane's own; its zero value is "first row highlighted", which is the state
// a whole-struct reset (`m.settings = settingsPane{}`) leaves behind.
//
// It is eight bytes, deliberately: a pane that never filters adopts the clamp, the wrap rule and the
// key contract without adopting a text widget with them (ADR 0053 decision 9). A pane that DOES
// filter holds a [listSurface], which is this value plus the field.
//
// The two DECISION panes take it a third way. The approval menu (approval.go) and the ask_user
// offering (ask.go) have no pane struct to embed it in — their state IS the Model's own — so they
// NAME it (m.approvalSel, m.askSel) and take the walk, the clamp and the −1 WITHOUT the key
// contract below: they are soft-modal, so which keys they claim is a fact about the surface
// underneath them rather than about lists, and each says it at its own switch (the deepening plan's
// merge policy, 2026-08-19).
type listCursor struct {
	// selected indexes the rows the pane actually PAINTS — for a filtering list, the filtered view —
	// and is clamped rather than trusted, because the list underneath it can change while the overlay
	// is open.
	selected int
}

// listSurface is a list that also FILTERS: the cursor above, plus the field the human narrows the
// rows with. The picker and the /sessions browser are its two panes; every other list in the package
// holds the cursor alone. Embedded rather than named, so `m.picker.selected` and `m.picker.filter`
// both still read as the pane's own fields, and its zero value is "nothing typed, first row
// highlighted" — the state `m.picker = picker{}` leaves behind.
type listSurface struct {
	listCursor
	// filter is what the human has typed into the open overlay: the case-insensitive substring every
	// row must carry to survive (filterPopupRows). It is a [lineEditor] — the package's one text FIELD
	// (lineeditor.go), so "what does backspace do here" is answered where every other field answers it
	// — held BY VALUE as the Model holds the prompt's own (ADR 0011): the widget carries no
	// self-referential no-copy type, so the value-copied Model stays copyable.
	//
	// Its ZERO value is the inert widget a whole-struct reset leaves behind — a textarea nothing has
	// focused, which answers "" and drops every key — so the zeroing every close and accept already
	// does still clears the filter, and no path can carry a stale one into the next open. The real
	// field is built on the first key that reaches it (typeIntoOverlayFilter) rather than at each of
	// the sites that open an overlay.
	filter lineEditor
}

// view is the rows this surface SHOWS out of the ones it is given: the offering pruned by the filter,
// with each survivor's place in the unfiltered list carried beside it. Every consumer of a list —
// the painter, the count the selection is clamped against, and the accept — goes through this one
// derivation, which is what makes it impossible for an accept to take a row the pane did not paint.
func (l listSurface) view(rows []popupRow) pickerView {
	return filterPopupRows(rows, l.filter.value())
}

// clampSelection keeps selected inside a row list that moved under the open overlay — a beat carrying
// a shorter offering, a keystroke that narrowed the filter, a deleted session. n is the count the pane
// PAINTS (for a filtering list, its filtered view), so the highlight can never point past the last row
// on the screen. An empty list pins the selection at zero (the pane paints no highlight for it —
// highlight).
func (l *listCursor) clampSelection(n int) {
	switch {
	case n == 0:
		l.selected = 0
	case l.selected >= n:
		l.selected = n - 1
	case l.selected < 0:
		l.selected = 0
	}
}

// move walks the highlight delta rows through a list of n, by whichever answer to "what happens at
// the end" this pane keeps (listWrap). An empty list moves nowhere. The caller has already clamped,
// so a wrapping move is the modulo of a seated index and a stopping one cannot leave the range.
func (l *listCursor) move(delta, n int, wrap listWrap) {
	if n == 0 {
		return
	}
	next := l.selected + delta
	if wrap == listWrapsAround {
		l.selected = ((next % n) + n) % n
		return
	}
	l.selected = clampInt(next, 0, n-1)
}

// highlight is which of a pane's n painted rows the painter marks: the clamped selection, or −1
// where there is nothing to choose — the popup module's own convention for a pane with no cursor
// (popupSpec.selected). A pane whose rows are PROSE rather than choices states that −1 itself; this
// answers for a list.
//
// It takes the COUNT rather than the rows because the /settings key list counts something else than
// it paints (its display rows interleave unselectable section headers, settingsDisplayRows), and one
// clamp answering for both is what keeps a stale selection from reaching either slice.
func (l listCursor) highlight(n int) int {
	if n == 0 {
		return -1
	}
	return clampInt(l.selected, 0, n-1)
}

// key routes one keypress through the cursor at l over a list of n painted rows, and reports what it
// did (listVerdict). It is the key contract every list OVERLAY in the package shares — the whole of
// it for a list that does not filter, and the floor [Model.listKey] adds the typing keys to for one
// that does: esc closes, ↑/↓ (and ^p/^n) move the highlight by this pane's wrap rule, ⏎ takes the
// highlighted row where there is one, and every other key goes back to the pane (listUnclaimed).
// The two soft-modal decision panes take [listCursor.move] and [listCursor.highlight] without this
// contract, and the type's own doc says why.
//
// The selection is re-clamped BEFORE anything else, because the rows underneath can have changed
// since the last key — a beat carrying a shorter offering, a deleted session, a config key the
// provider stopped answering for. n is what the pane composed for THIS keypress, so the count a key
// is measured against and the count it leaves behind are one reading of the list.
//
// l points into the CALLER's Model and the pointer never outlives the call: every caller is a
// value-receiver method that hands over the address of its own copy's cursor and returns that copy,
// so nothing here puts a self-pointer on a Model that is copied on every Update (ADR 0011 — the
// [Model.reportState] posture).
func (l *listCursor) key(msg tea.KeyPressMsg, n int, wrap listWrap) listVerdict {
	l.clampSelection(n)
	switch msg.String() {
	case "esc":
		return listCloses
	case "up", "ctrl+p":
		l.move(-1, n, wrap)
		return listSwallowed
	case "down", "ctrl+n":
		l.move(1, n, wrap)
		return listSwallowed
	case "enter":
		if n == 0 {
			return listSwallowed // nothing to take: the modal keeps the key and does nothing
		}
		return listAccepts
	}
	return listUnclaimed
}

// wheel walks the cursor at l one row per wheel notch over a list of n painted rows, and is the
// package's one answer to what a WHEEL does to a list overlay: [tea.MouseWheelUp] a row back,
// [tea.MouseWheelDown] a row on, and every other button — the sideways notches a trackpad sends —
// nothing at all. The pane above it decides whether the notch is ITS notch (the pointer is inside its
// rectangle); this decides what the notch does once it is.
//
// It CLAMPS at the ends where the keys WRAP, and the difference is the gesture rather than an
// inconsistency: ↑/↓ walk a list as a cycle, while a wheel is a scroll — rolling past the last row and
// landing back on the first would move the human somewhere they did not aim (ratified 2026-08-22).
// That is why there is no [listWrap] parameter here: the clamp is structural, [listStopsAtEnds] passed
// from inside rather than by a caller who could get it wrong, and a wheel that wraps is not a scroll.
//
// n is the count the pane PAINTS for this notch — for a filtering list, its filtered view — so a notch
// and an ↑ can never disagree about which row is highlighted. A list with no rows to walk (n < 1) moves
// nothing, and a highlight the rows shrank out from under needs no clamp of its own first: a stopping
// [listCursor.move] cannot leave the range, so the notch lands it on the last row that is still there.
//
// settingsWheel (mouse.go) and reportWheel (reportpane.go) stated the clamp rule first and keep their
// own row arithmetic — they scroll a row WINDOW and a caret rather than a bare cursor — so this is the
// shared answer for the panes that hold a [listCursor], not for those two.
//
// l points into the CALLER's Model exactly as [listCursor.key]'s does and the pointer never outlives
// the call, so nothing here puts a self-pointer on a Model that is copied on every Update (ADR 0011).
func (l *listCursor) wheel(msg tea.MouseWheelMsg, n int) {
	if n < 1 {
		return // no rows: nothing to highlight, so nothing for a notch to walk
	}
	switch msg.Button {
	case tea.MouseWheelUp:
		l.move(-1, n, listStopsAtEnds)
	case tea.MouseWheelDown:
		l.move(1, n, listStopsAtEnds)
	}
}

// listKey routes one keypress through the FILTERING surface at l over the pane's UNFILTERED rows.
// It is the cursor's contract above, measured against the rows the filter leaves standing, plus the
// two keys only a filtering list has: everything PRINTABLE types — the key's runes extend the filter
// that prunes the rows — with backspace as its undo.
//
// The typing keys are asked LAST, of what the cursor handed back, which is what keeps the two
// contracts one contract: a key the cursor claims means the same thing in every list of the package,
// and only what no list claims can be a letter of the filter. Both clamps of a keypress — the
// cursor's, and the one typeIntoOverlayFilter makes against the rows the edited filter leaves —
// read the SAME rows the caller composed for this keypress.
//
// There is no activation key, deliberately (ratified 2026-08-06): the overlay is modal, so no letter
// is a verb inside it and every one of them can be the filter's. That is also why esc CLOSES with a
// filter set rather than clearing it first — one key, one meaning, so a legend's "esc close" is never
// conditionally wrong, and backspace is the way back to a wider list.
func (m Model) listKey(
	l *listSurface,
	msg tea.KeyPressMsg,
	rows []popupRow,
	wrap listWrap,
) (listVerdict, tea.Cmd) {
	if verdict := l.listCursor.key(msg, len(l.view(rows).rows), wrap); verdict != listUnclaimed {
		return verdict, nil
	}
	// Text carries the key's rune(s) only for PRINTABLE input — a modifier chord carries none
	// (bubbletea's own contract) — so a chord the cursor had no use for is handed back to the pane
	// rather than typed into the filter.
	if msg.String() == "backspace" || msg.Text != "" {
		return listSwallowed, m.typeIntoOverlayFilter(l, msg, rows)
	}
	return listUnclaimed, nil
}

// typeIntoOverlayFilter hands one keystroke to a filtering overlay's filter field and re-clamps the
// highlight to the rows the field leaves standing, returning whatever Cmd the widget asked for. It is
// the door BOTH overlays that filter type through, so the two answer "what does backspace do to a
// filter" with the same field rather than each trimming a rune off a string of its own.
//
// Only the two keys the surface routes here reach it: a printable keypress and backspace, both
// landing at the end of the value where the caret stands. Everything else is swallowed whole by the
// modal above (listKey) — the field is what EDITS, never what decides which keys are the overlay's.
//
// The field is BUILT here, on the first key that reaches it, rather than at each of the sites that
// open an overlay: those assign the whole struct at once (`m.picker = picker{}`), and a filter nobody
// has typed into holds nothing, paints nothing (overlayFilterLine) and prunes nothing
// (filterPopupRows' identity view). The glyph is pickerFilterCursor for every surface, which is the
// caret the shared filter line has always drawn.
func (m Model) typeIntoOverlayFilter(l *listSurface, msg tea.KeyPressMsg, rows []popupRow) tea.Cmd {
	if !l.filter.isBuilt() {
		l.filter = newPopupField(m.opts.CursorShape, m.th.surface, pickerFilterCursor, "")
	}
	// The Cmd is handed back rather than dropped, as the /settings buffer hands its own back
	// (settingsBufferKey): a single-line field asks for none today, and swallowing one silently is how
	// that stops being true unnoticed.
	cmd := l.filter.editKey(msg)
	l.clampSelection(len(l.view(rows).rows))
	return cmd
}

// pickerView is a list's FILTERED view of the rows it was given, and the ONE seam its consumers
// share: the rows the pane paints, how many there are, and which row an accept takes. Deriving them
// once is what makes it impossible for the accept to take a row the pane did not paint — offering
// carries, for each surviving row, the index it holds in the FULL list, and every accept resolves
// that index against its own offering rather than indexing it with a filtered position.
type pickerView struct {
	rows     []popupRow
	offering []int // offering[i] is where rows[i] sits in the unfiltered offering
}

// offeringIndex maps the highlighted FILTERED row back to its place in the full list, and reports
// false when there is nothing to take: a filter matching no row, or a list that emptied under the
// open pane. Callers whose list can move between the two reads of one keypress (the ServerHost.List
// provider, the live Schedules) still bounds-check what comes back against a fresh read — this
// answers where the human aimed, not what is still there.
func (v pickerView) offeringIndex(selected int) (int, bool) {
	if selected < 0 || selected >= len(v.offering) {
		return 0, false
	}
	return v.offering[selected], true
}

// filterPopupRows is the filter itself: the rows that survive it, and where each of them sat in the
// unfiltered list. An EMPTY filter is the IDENTITY view — every row, at its own index — which is
// what keeps every unfiltered behaviour (the clamp, the wrap, the accept target) exactly what it was
// before a filter existed.
func filterPopupRows(rows []popupRow, filter string) pickerView {
	if filter == "" {
		offering := make([]int, len(rows))
		for i := range offering {
			offering[i] = i
		}
		return pickerView{rows: rows, offering: offering}
	}
	view := pickerView{rows: make([]popupRow, 0, len(rows)), offering: make([]int, 0, len(rows))}
	for i, row := range rows {
		if !rowMatchesFilter(row, filter) {
			continue
		}
		view.rows = append(view.rows, row)
		view.offering = append(view.offering, i)
	}
	return view
}

// rowMatchesFilter reports whether one row survives filter: a case-insensitive substring test over
// the row's cells joined with a single space. EVERY cell participates, the marker cells included —
// filtering on "running" to find the live profiles is a legitimate thing to ask of a profile list —
// and space is a filter character like any other, because the overlay is modal and space is no verb
// inside it. Substring rather than prefix, and no ranking of any kind: the filter PRUNES the
// offering and never reorders it, so no row can jump out from under the highlight mid-keystroke.
func rowMatchesFilter(row popupRow, filter string) bool {
	if filter == "" {
		return true
	}
	return strings.Contains(strings.ToLower(strings.Join(row, " ")), strings.ToLower(filter))
}

// ----------------------------------------------------------------------------
// The filter line, and the one budget→render call behind every list pane
// ----------------------------------------------------------------------------

// pickerFilterLead labels the line the overlay's filter is typed on. It is a LABEL rather than
// prose, so the pane paints it as one (popupSpec.bodyLead, the /settings header's own idiom): the
// eye finds the live line by its label and reads the human's own text after it.
const pickerFilterLead = "filter: "

// pickerFilterCursor closes that line. A block where the next keystroke will land, because the real
// caret is in the prompt box below and stays there (a popup row has no seat for it — popup.go):
// without it the line reads as a finished caption rather than as something being typed. U+258C LEFT
// HALF BLOCK — one cell in either width method, like every other glyph the frame measures (theme.go).
//
// It is the glyph the filter FIELD is built with (typeIntoOverlayFilter) rather than a string this
// file appends, so the caret stands where the caret stands ([lineEditor.textWithCaret]) — and every
// filtering overlay draws the same one, which is what the shared line has always painted.
const pickerFilterCursor = "▌"

// overlayFilterLine is what a filtering overlay shows of its filter — "filter: qwen▌" — or nothing
// at all while the filter is empty, which is what makes the line's presence the fact that filtering
// is happening. The text is the human's own keystrokes rather than a foreign string, but it reaches
// the popup module as BODY and that contract takes body escape-stripped (popup.go), so it is
// stripped here like every other cell a pane composes.
//
// It is one composer for every overlay that filters, because the line is one line: two panes
// spelling it themselves would be two places for the label, the cursor and the stripping to drift
// apart. The cursor arrives INSIDE the text, where the field draws it ([lineEditor.textWithCaret]),
// so the strip covers it too — harmlessly, since a block glyph is no control character.
func overlayFilterLine(filter lineEditor) string {
	if filter.value() == "" {
		return ""
	}
	return pickerFilterLead + stripEscapes(filter.textWithCaret())
}

// listContent is everything ONE list overlay says about itself that this module cannot know: its slot
// in the frame's row allocation, what the box is called, the keys it spells, how many rows it likes
// to show at once, and — composed for THIS frame — the rows it paints and which of them the highlight
// is on.
//
// rowCap is the pane's own taste and not a limit the frame respects; [Model.popupBudget] cuts it down
// to what the window can seat. selected is [listCursor.highlight] for a list of choices and −1 for a
// pane whose rows are prose (the /sessions browser's empty-workspace note), which is a fact about the
// rows and so the pane's to state.
//
// body is the block between the title and the rows, and a pane either states one of its own — the
// /settings sub-list's question, which names the key being answered because the list where the human
// read that name is what the sub-list replaced — or leaves it to [Model.renderFilterList], which
// fills all three body fields from the filter being typed. The two cannot both be true of one pane:
// a filtering list's body IS its filter line.
type listContent struct {
	pane     framePane
	title    string
	body     string
	bodyLead string
	bodyPad  bool
	hint     string
	rowCap   int
	rows     []popupRow
	menuRows bool
	selected int
}

// renderList paints an open list overlay through the shared popup module (renderPopup): a titled,
// bordered pane spanning the full window width (m.width, flush with the input box below) holding the
// body block, the rows and a key legend, the selected row highlighted. It is the one budget→render
// call behind every list pane — the picker, the /sessions browser, the /settings sub-lists and the
// "/" | "@" dropdown — and it returns "" when the frame cannot seat the pane beside its siblings, so
// View treats every list's slot alike.
//
// The BODY block is the pane's, and it is BUDGETED rather than merely drawn: the claim is the
// wrapped body plus whichever blanks it asked to be set off by (popupFloor.body), so the pane paints
// what it asked the frame for. The claim is also what decides the trade on a window too short for
// everything — it comes off the top of the grant, so the ROWS shrink and the body stays. That is the
// right way round for both bodies a list has: the one line the human is actively typing
// (renderFilterList) is what says the list is being narrowed, and the /settings sub-list's question
// is what says which key is being answered — while a list you cannot see all of is still a list. The
// row demand and the row cap are untouched by any of it: the pane's taste is its taste with a body
// exactly as without.
//
// A body set off by blanks pays for BOTH of them out of its own claim (popupSpec.bodyPadAbove,
// bodyPadBelow) rather than one pad per neighbouring block. The lower blank has to belong to the body
// because the row block's pads are spent out of the ROW window — and an offering longer than the
// pane's taste fills that window by definition, so a pad owned by the rows would be dropped exactly
// on the roomy terminals where the pane has lines to spare, and dropped by taking a row past the
// pane's own taste.
func (m Model) renderList(c listContent) string {
	claim := popupFloor{}
	if c.body != "" {
		claim.body = popupBodyLineCount(m.th, c.body, m.width) + popupBodyPadLines(c.bodyPad, c.bodyPad)
	}
	// The pane's rowCap is the taste; popupBudget is the screen's answer to it, so a long offering on a
	// short terminal shrinks the pane instead of pushing the input box off the frame (D2).
	maxBody, shown, seated := m.popupBudget(c.pane, len(c.rows), c.rowCap, popupChrome, claim)
	if !seated {
		return "" // the frame cannot seat this pane beside its siblings (frameRowPlan)
	}
	return renderPopup(m.th, popupSpec{
		title:        c.title,
		body:         c.body,
		bodyLead:     c.bodyLead,
		maxBodyRows:  maxBody,
		bodyPadAbove: c.bodyPad,
		bodyPadBelow: c.bodyPad,
		rows:         c.rows,
		menuRows:     c.menuRows,
		selected:     c.selected,
		hint:         c.hint,
		maxRows:      shown,
		scrollbar:    m.popupScrollbarOn(),
	}, m.width)
}

// renderFilterList paints an open FILTERING list overlay: the call above, with the filter line as
// the pane's body. It is the one place the line, its label, its two blanks and the claim for all
// three are stated (renderList's own doc says what that claim buys), so a pane states its rows and
// its wording and cannot forget the pads or set them out of step with what it asked the frame for.
//
// An empty filter leaves the body block exactly as the pane left it — nothing at all, for both panes
// that filter — so a list nobody has typed into is the list it was before a filter existed.
func (m Model) renderFilterList(filter lineEditor, c listContent) string {
	if line := overlayFilterLine(filter); line != "" {
		c.body, c.bodyLead, c.bodyPad = line, pickerFilterLead, true
	}
	return m.renderList(c)
}
