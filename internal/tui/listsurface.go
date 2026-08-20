package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// ----------------------------------------------------------------------------
// The list surface every filtering overlay is built on
// ----------------------------------------------------------------------------
//
// A LIST SURFACE is what the /model | /server picker (picker.go) and the /sessions browser
// (sessions.go) both are underneath their own wording: a modal list of rows with one highlight, a
// filter the human types into it, and a row window the frame grants. Everything about that is
// written once here and named twice there, so a pane's whole remaining job is its ROWS, its ACCEPT
// and its HINT (ADR 0053).
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
//     an open list, which is what makes every letter the filter's.
//   - What ↑/↓ do at the ENDS is a parameter and not a rule (listWrap) — the panes already disagree
//     and each keeps the answer it had.
//   - The filter LINE is the surface's, not the painter's spec. Its label, its caret, its two
//     blank lines and the budget claim all three cost ride here (renderList), so a pane cannot
//     paint a line it did not claim room for.
//
// The surface decides nothing about what a key MEANS. Closing an overlay, resuming a session,
// rebinding a model: those are the pane's own acts with the pane's own consequences, and a
// listVerdict is what asks for one. That is the split that lets seven pickerKinds and three browser
// modes share one key contract without this file knowing that any of them exist.

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
// had (the deepening plan's merge policy, 2026-08-19): the picker and the /sessions browser wrap
// around, while the approval and ask selections stop at the ends.
type listWrap bool

const (
	listStopsAtEnds listWrap = false // ↓ on the last row stays on it
	listWrapsAround listWrap = true  // ↓ on the last row returns to the first
)

// listSurface is the state a filtering list overlay keeps between keypresses. It is a plain value
// with no lock and no self-pointer, so a pane EMBEDS it inline in the value-copied Model (ADR 0011,
// ADR 0053) and the fields below read as the pane's own; its zero value is "nothing typed, first row
// highlighted", which is the state a whole-struct reset (`m.picker = picker{}`) leaves behind.
type listSurface struct {
	// selected indexes the FILTERED rows — what the pane actually paints — and is clamped rather than
	// trusted, because the list underneath it can change while the overlay is open.
	selected int
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
// a shorter offering, a keystroke that narrowed the filter, a deleted session. n is the count of the
// FILTERED view, so the highlight can never point past the last row on the screen. An empty list pins
// the selection at zero (the pane paints no highlight for it — highlight).
func (l *listSurface) clampSelection(n int) {
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
func (l *listSurface) move(delta, n int, wrap listWrap) {
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

// highlight is which of rows the painter marks: the clamped selection, or −1 where there is nothing
// to choose — the popup module's own convention for a pane with no cursor (popupSpec.selected). A
// pane whose rows are PROSE rather than choices states that −1 itself; this answers for a list.
func (l listSurface) highlight(rows []popupRow) int {
	if len(rows) == 0 {
		return -1
	}
	return clampInt(l.selected, 0, len(rows)-1)
}

// listKey routes one keypress through the surface at l over the pane's UNFILTERED rows, and reports
// what it did (listVerdict). It is the whole key contract every filtering list shares: esc closes,
// ↑/↓ (and ^p/^n) move the highlight by this pane's wrap rule, ⏎ takes the highlighted row where
// there is one, and everything PRINTABLE types — the key's runes extend the filter that prunes the
// rows, with backspace as its undo.
//
// The count is re-derived and the selection re-clamped BEFORE anything else, because the rows
// underneath can have changed since the last key, and again after a key that moved the filter
// (typeIntoOverlayFilter). Both clamps read the SAME unfiltered rows the caller composed for this
// keypress, so the two counts one key spends can never come from two different compositions of the
// list.
//
// There is no activation key, deliberately (ratified 2026-08-06): the overlay is modal, so no letter
// is a verb inside it and every one of them can be the filter's. That is also why esc CLOSES with a
// filter set rather than clearing it first — one key, one meaning, so a legend's "esc close" is never
// conditionally wrong, and backspace is the way back to a wider list.
//
// l points into the CALLER's Model and the pointer never outlives the call: every caller is a
// value-receiver method that hands over the address of its own copy's surface and returns that copy,
// so nothing here puts a self-pointer on a Model that is copied on every Update (ADR 0011 — the
// [Model.reportState] posture).
func (m Model) listKey(
	l *listSurface,
	msg tea.KeyPressMsg,
	rows []popupRow,
	wrap listWrap,
) (listVerdict, tea.Cmd) {
	n := len(l.view(rows).rows)
	l.clampSelection(n)
	switch msg.String() {
	case "esc":
		return listCloses, nil
	case "up", "ctrl+p":
		l.move(-1, n, wrap)
		return listSwallowed, nil
	case "down", "ctrl+n":
		l.move(1, n, wrap)
		return listSwallowed, nil
	case "enter":
		if n == 0 {
			return listSwallowed, nil // nothing to take: the modal keeps the key and does nothing
		}
		return listAccepts, nil
	case "backspace":
		return listSwallowed, m.typeIntoOverlayFilter(l, msg, rows)
	}
	// Text carries the key's rune(s) only for PRINTABLE input — a modifier chord carries none
	// (bubbletea's own contract) — so a chord that is not one of the verbs above is handed back to the
	// pane rather than typed into the filter.
	if msg.Text != "" {
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
// open pane. Callers whose list can move between the two reads of one keypress (the Options.Servers
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
// to what the window can seat. selected is [listSurface.highlight] for a list of choices and −1 for a
// pane whose rows are prose (the /sessions browser's empty-workspace note), which is a fact about the
// rows and so the pane's to state.
type listContent struct {
	pane     framePane
	title    string
	hint     string
	rowCap   int
	rows     []popupRow
	selected int
}

// renderList paints an open list overlay through the shared popup module (renderPopup): a titled,
// bordered pane spanning the full window width (m.width, flush with the input box below) holding the
// filter line, the rows and a key legend, the selected row highlighted. It returns "" when the frame
// cannot seat the pane beside its siblings, so View treats every list's slot alike.
//
// While a filter is being typed the pane grows one line for it, set off by a blank line at each end
// (ratified 2026-08-06 — "not all bunched up"). All three lines are the module's BODY block, which
// sits between the title and the rows and drops away entirely when the filter is empty: the text is
// the body, and BOTH blanks are the body's own pads (popupSpec.bodyPadAbove, bodyPadBelow) rather
// than one pad per neighbouring block. The lower blank has to belong to the body because the row
// block's pads are spent out of the ROW window — and an offering longer than the pane's taste fills
// that window by definition, so a pad owned by the rows would be dropped exactly on the roomy
// terminals where the pane has lines to spare, and dropped by taking a row past the pane's own taste.
//
// The three flags are set HERE and nowhere else, which is the point of the line living with the
// surface that owns the filter: a pane states its rows and its wording, and cannot forget the pads or
// set them out of step with the claim below.
//
// All three lines are BUDGETED and not merely drawn: the claim is the wrapped filter line plus its
// two blanks (popupFloor.body), so the pane paints what it asked the frame for. The claim is also
// what decides the trade on a window too short for everything — it comes off the top of the grant, so
// the ROWS shrink and the filter line stays. That is the right way round for the one line the human
// is actively typing: a list you cannot see all of is still being narrowed, while a filter you cannot
// see is a pane that has stopped explaining itself. The row demand and the row cap are untouched by
// any of it — the pane's taste is its taste with a filter open exactly as without.
func (m Model) renderList(l listSurface, c listContent) string {
	filter := overlayFilterLine(l.filter)
	claim := popupFloor{}
	if filter != "" {
		claim.body = popupBodyLineCount(m.th, filter, m.width) + popupBodyPadLines(true, true)
	}
	// The pane's rowCap is the taste; popupBudget is the screen's answer to it, so a long offering on a
	// short terminal shrinks the pane instead of pushing the input box off the frame (D2).
	maxBody, shown, seated := m.popupBudget(c.pane, len(c.rows), c.rowCap, popupChrome, claim)
	if !seated {
		return "" // the frame cannot seat this pane beside its siblings (frameRowPlan)
	}
	return renderPopup(m.th, popupSpec{
		title:        c.title,
		body:         filter,
		bodyLead:     pickerFilterLead,
		maxBodyRows:  maxBody,
		bodyPadAbove: filter != "",
		bodyPadBelow: filter != "",
		rows:         c.rows,
		selected:     c.selected,
		hint:         c.hint,
		maxRows:      shown,
		scrollbar:    m.popupScrollbarOn(),
	}, m.width)
}
