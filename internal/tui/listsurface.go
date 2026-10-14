package tui

import (
	"fmt"
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
//     have changed since the last one. A key that moved the FILTER RE-SEATS it instead: the
//     highlight stays on the row it stood on while that row still matches, else takes the first
//     match (pickerView.reseatPosition) — so typing never walks it off a row the human can still see.
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
//   - What the filter SAYS is the surface's, not the pane's. Its label, its caret and its stripping
//     are composed here (overlayFilterTail) and ride the pane's TITLE row (popupSpec.titleTail), never
//     a line of their own, so typing takes no row from the list and nothing moves on the first key
//     (ADR 0053, 2026-10-14 amendment rule 5). A pane with a body of its own — the /settings
//     sub-list's question — states it as body and takes the same budget→render call (renderList).
//     So is what the filter's STATE does to the pane (listContent.statingFilter): the legend that
//     swaps "type to filter" for "⌫ trim" once text is typed, and the one faint row that says a
//     filter matched nothing (rules 7 and 8) — each pane names only its noun and the keys of its
//     legend that change the row set.
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
	// row's matched text must carry for the row to survive (filterPopupRows, matchedText). It is a [lineEditor] — the package's one text FIELD
	// (lineeditor.go), so "what does backspace do here" is answered where every other field answers it
	// — held behind a [copyOnWriteField]: a pointer, so the two filtering panes do not each carry a
	// whole textarea in the value-copied Model, and replaced on every keystroke rather than written
	// through, so a Model copy keeps the filter it was taken with (ADR 0011).
	//
	// Its ZERO value is the empty, unbuilt field a whole-struct reset leaves behind — it answers ""
	// and drops every key — so the zeroing every close and accept already does still clears the
	// filter, and no path can carry a stale one into the next open. The real field is built on the
	// first key or paste that reaches it (typeIntoOverlayFilter) rather than at each of the sites that
	// open an overlay.
	filter copyOnWriteField
}

// view is the rows this surface SHOWS out of the ones it is given: the offering pruned by the filter,
// with each survivor's place in the unfiltered list carried beside it. Every consumer of a list —
// the painter, the count the selection is clamped against, and the accept — goes through this one
// derivation, which is what makes it impossible for an accept to take a row the pane did not paint.
// matched is the pane's answer to which of a row's cells the filter reads (matchedText); each pane
// wraps this in a view of its own that supplies it, so no caller has to know it.
func (l listSurface) view(rows []popupRow, matched matchedText) pickerView {
	return filterPopupRows(rows, matched, l.filter.value())
}

// clampSelection keeps selected inside a row list that moved under the open overlay — a beat carrying
// a shorter offering, a deleted session. (A keystroke that edits the filter re-seats the highlight on
// its row instead, pickerView.reseatPosition.) n is the count the pane
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

// seat puts the highlight ON row, clamped into a list of count painted rows — the SETTER beside
// highlight's getter, and the one way anything but a key moves a list's cursor. The pointer is what
// needs it: a click names a row outright instead of walking to it (mouse.go), and every pane's click
// handler goes through here rather than assigning the field, so a row the pane never painted can no
// more be highlighted by the mouse than by ↑/↓ (listCursor.key clamps for exactly this reason).
//
// An empty list seats nothing: there is no row to put the highlight on, and highlight answers −1 for
// it whatever the field holds.
func (l *listCursor) seat(row, count int) {
	if count == 0 {
		return
	}
	l.selected = clampInt(row, 0, count-1)
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
// and only what no list claims can be a letter of the filter. Both seatings of a keypress — the
// cursor's clamp, and the re-seat typeIntoOverlayFilter makes against the rows the edited filter
// leaves — read the SAME rows the caller composed for this keypress.
//
// There is no activation key, deliberately (ratified 2026-08-06): the overlay is modal, so no letter
// is a verb inside it and every one of them can be the filter's. That is also why esc CLOSES with a
// filter set rather than clearing it first — one key, one meaning, so a legend's "esc close" is never
// conditionally wrong, and backspace is the way back to a wider list.
func (m Model) listKey(
	l *listSurface,
	msg tea.KeyPressMsg,
	rows []popupRow,
	matched matchedText,
	wrap listWrap,
) (listVerdict, tea.Cmd) {
	if verdict := l.listCursor.key(msg, len(l.view(rows, matched).rows), wrap); verdict != listUnclaimed {
		return verdict, nil
	}
	// Text carries the key's rune(s) only for PRINTABLE input — a modifier chord carries none
	// (bubbletea's own contract) — so a chord the cursor had no use for is handed back to the pane
	// rather than typed into the filter.
	if msg.String() == "backspace" || msg.Text != "" {
		return listSwallowed, m.typeIntoOverlayFilter(l, rows, matched, func(f *lineEditor) tea.Cmd { return f.editKey(msg) })
	}
	return listUnclaimed, nil
}

// typeIntoOverlayFilter hands one edit to a filtering overlay's filter field and re-seats the
// highlight on the rows the field leaves standing (pickerView.reseatPosition), returning whatever Cmd
// the widget asked for. It is the door BOTH overlays that filter type through, so the two answer
// "what does backspace do to a filter" — and "where does the highlight go when it does" — with the
// same field and the same rule rather than each keeping a string and a clamp of its own.
//
// change is the edit itself, and only two kinds reach it: a keystroke the surface routes here (a
// printable keypress or backspace, listKey), and a bracketed paste (listPaste), which the field folds
// onto its one line ([lineEditor.editMsg]). Both land at the end of the value where the caret stands.
// Everything else is swallowed whole by the modal above (listKey) — the field is what EDITS, never
// what decides which keys are the overlay's.
//
// The field is BUILT here, on the first edit that reaches it, rather than at each of the sites that
// open an overlay: those assign the whole struct at once (`m.picker = picker{}`), and a filter nobody
// has typed into holds nothing, paints nothing (overlayFilterTail) and prunes nothing
// (filterPopupRows' identity view). The glyph is pickerFilterCursor for every surface, so every
// filtering pane's title row draws the same caret.
func (m Model) typeIntoOverlayFilter(
	l *listSurface,
	rows []popupRow,
	matched matchedText,
	change func(*lineEditor) tea.Cmd,
) tea.Cmd {
	if !l.filter.isBuilt() {
		l.filter = newCopyOnWriteField(newPopupField(m.opts.CursorShape, m.th.surface, pickerFilterCursor, ""))
	}
	// The Cmd is handed back rather than dropped, as the /settings buffer hands its own back
	// (settingsBufferKey): a single-line field asks for none today, and swallowing one silently is how
	// that stops being true unnoticed.
	//
	// The highlighted ROW is read before the edit, not its position: the edit moves every position
	// under it, and the row is what the human is looking at.
	underlying, held := l.view(rows, matched).offeringIndex(l.selected)
	cmd := l.filter.edit(change)
	l.selected = l.view(rows, matched).reseatPosition(underlying, held)
	return cmd
}

// listPaste routes a bracketed paste while a filtering list pane owns the keyboard — the paste is
// TYPING there (ADR 0053, 2026-10-14 amendment): into the open pane's filter, folded onto one line,
// with the highlight re-seated as a keystroke re-seats it. A pane that asks rather than lists (a
// question kind, pickerOffering.filters) drops it; the /sessions browser routes it by its own
// sub-step (sessionBrowserPaste). Either way it is CLAIMED, never
// handed on: the panes are modal, and a paste falling through would fill the prompt draft — or stage
// an interjection mid-run — behind a pane the human is looking at instead.
//
// Each pane is gated by its own key gate (paneSpecs keyOpen), not by its open flag, so the paste
// follows exactly the rule the keys follow: an approval or ask raised above a mid-run picker leaves
// the picker's gate shut, and the paste goes to the surface the keys go to.
func (m Model) listPaste(msg tea.PasteMsg) (Model, tea.Cmd, bool) {
	paste := func(f *lineEditor) tea.Cmd { return f.editMsg(msg) }
	switch {
	case paneSpecs[paneBrowser].keyOpen(m):
		return m.sessionBrowserPaste(paste)
	case paneSpecs[panePicker].keyOpen(m):
		offering := pickerOfferings[m.picker.kind]
		if !offering.filters {
			return m, nil, true // a question pane has no filter to type into
		}
		cmd := m.typeIntoOverlayFilter(&m.picker.listSurface, m.pickerOfferingRows(), offering.matched, paste)
		return m, cmd, true
	}
	return m, nil, false
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

// reseatPosition is where the highlight stands in this view after the filter changed under it — the
// ONE answer every filtering list gives (ADR 0053, 2026-10-14 amendment): on the row it stood on
// before the edit while that row still matches, else on the first match. underlying is that row's
// place in the FULL list (offeringIndex, read against the view the edit replaced), and held is
// offeringIndex's ok — false when the old view showed nothing to stand on, so the first match takes
// the highlight.
//
// It is stated over the view rather than over a pane's cursor so that a list whose selection indexes
// its full rows instead (the /settings key list) re-seats by the same rule: it takes the position
// back to the full list through offering. An emptied filter needs no case of its own — the identity
// view carries every row, so the highlight stays on the row it was on. An empty view answers 0, the
// position clampSelection pins an empty list at.
func (v pickerView) reseatPosition(underlying int, held bool) int {
	if !held {
		return 0
	}
	for pos, at := range v.offering {
		if at == underlying {
			return pos
		}
	}
	return 0
}

// filterPopupRows is the filter itself: the rows that survive it, and where each of them sat in the
// unfiltered list. An EMPTY filter is the IDENTITY view — every row, at its own index — which is
// what keeps every unfiltered behaviour (the clamp, the wrap, the accept target) exactly what it was
// before a filter existed.
func filterPopupRows(rows []popupRow, matched matchedText, filter string) pickerView {
	if filter == "" {
		offering := make([]int, len(rows))
		for i := range offering {
			offering[i] = i
		}
		return pickerView{rows: rows, offering: offering}
	}
	view := pickerView{rows: make([]popupRow, 0, len(rows)), offering: make([]int, 0, len(rows))}
	for i, row := range rows {
		if !rowMatchesFilter(row, matched, filter) {
			continue
		}
		view.rows = append(view.rows, row)
		view.offering = append(view.offering, i)
	}
	return view
}

// rowMatchesFilter reports whether one row survives filter: a case-insensitive substring test over
// the row's MATCHED TEXT (matchedText) — what the row says it is, never the live state painted beside
// it. Space is a filter character like any other, because the overlay is modal and space is no verb
// inside it. Substring rather than prefix, and no ranking of any kind: the filter PRUNES the offering
// and never reorders it, so no row can jump out from under the highlight mid-keystroke.
func rowMatchesFilter(row popupRow, matched matchedText, filter string) bool {
	if filter == "" {
		return true
	}
	return strings.Contains(strings.ToLower(matched(row)), strings.ToLower(filter))
}

// matchedText is a pane's answer to which part of a row the filter reads: the row's MATCHED TEXT
// (ADR 0053, 2026-10-14 amendment). It is what the row says it IS — a name, an id, a label and its
// gloss, a prompt, a title with its qualifiers — and never what it reports about the world: a value,
// a "· running" or "· current" mark, a context window, a measured summary, an age, a count, a spend.
// Those change under the open pane and say nothing about which row the human is looking for, so a
// filter that read them would find "32k" in a model list or "5m" in a session store and narrow to
// rows nobody named.
//
// It is a function of the row rather than a field of it because the panes already compose their
// rows as cells; each pane names the cells that count (matchCells) and the one matcher above does
// the rest. Like every function in this package it is looked up by the pane at the call — the
// picker's per-kind table, the browser's own constant — and never held on the value-copied Model
// (ADR 0011).
type matchedText func(row popupRow) string

// matchCells is the matchedText of a pane whose matched text is some of its cells: the named cells,
// in the order given, joined with a single space — so a filter may span the join between a label and
// its gloss exactly as the eye reads them. A cell a row does not carry (the effort picker's level
// rows have no gloss) is skipped rather than read past the row's end.
func matchCells(cells ...int) matchedText {
	return func(row popupRow) string {
		parts := make([]string, 0, len(cells))
		for _, c := range cells {
			if c < len(row) && row[c] != "" {
				parts = append(parts, row[c])
			}
		}
		return strings.Join(parts, " ")
	}
}

// The two shapes of matched text the panes share. matchLeadCell is a row whose first cell names it —
// a model id, a server name, a session title with its qualifiers; matchLabelAndGloss is a row that
// is a label and the "— …" gloss saying what the label means, both of which a human may type.
var (
	matchLeadCell      = matchCells(0)
	matchLabelAndGloss = matchCells(0, 1)
)

// ----------------------------------------------------------------------------
// The filter on the title row, and the one budget→render call behind every list pane
// ----------------------------------------------------------------------------

// pickerFilterLead labels the overlay's filter where the title row states it ("saved sessions ·
// filter: mo▌"): the eye finds the live text by its label and reads the human's own keystrokes after
// it, rather than taking them for more of the pane's name.
const pickerFilterLead = "filter: "

// pickerFilterCursor closes that text. A block where the next keystroke will land, because the real
// caret is in the prompt box below and stays there (a popup row has no seat for it — popup.go):
// without it the filter reads as a finished caption rather than as something being typed. U+258C LEFT
// HALF BLOCK — one cell in either width method, like every other glyph the frame measures (theme.go).
//
// It is the glyph the filter FIELD is built with (typeIntoOverlayFilter) rather than a string this
// file appends, so the caret stands where the caret stands ([lineEditor.textWithCaret]) — and every
// filtering overlay draws the same one.
const pickerFilterCursor = "▌"

// overlayFilterTail is what a filtering overlay shows of its filter on its title row
// (popupSpec.titleTail) — "filter: qwen▌" — or nothing at all while the filter is empty, which is
// what makes the tail's presence the fact that filtering is happening (ADR 0053, 2026-10-14
// amendment rule 5). The text is the human's own keystrokes rather than a foreign string, but it
// reaches the popup module as part of a title and is stripped here like every other cell a pane
// composes.
//
// typing is whether typing reaches the filter in the pane's current state: the caret shows only
// then. A /sessions rename or delete-confirm keeps the filter text on screen — the list it narrowed
// is still the list — but the next keystroke lands in the rename field or answers the confirm, so a
// caret on the filter would point at the wrong field.
//
// It is one composer for every overlay that filters, because the statement is one statement: two
// panes spelling it themselves would be two places for the label, the cursor and the stripping to
// drift apart. The cursor arrives INSIDE the text, where the field draws it
// ([lineEditor.textWithCaret]), so the strip covers it too — harmlessly, since a block glyph is no
// control character.
func overlayFilterTail(filter copyOnWriteField, typing bool) string {
	if filter.value() == "" {
		return ""
	}
	if !typing {
		return pickerFilterLead + stripEscapes(filter.value())
	}
	return pickerFilterLead + stripEscapes(filter.textWithCaret())
}

// The legend entries the filter's state adds and takes away (ADR 0053, 2026-10-14 amendment rule
// 8). listFilterEntry is the one every filtering pane's empty-filter legend LEADS with (pickerHint's
// reason: the letters announce themselves nowhere else); listTrimEntry is the one that replaces it
// once text is typed, because with a filter set the key nobody can guess is the way BACK — backspace
// trims, and esc still closes, filter and all. listCloseEntryLead is how the pane's own esc entry
// begins, so the trim entry can stand just before it.
const (
	listFilterEntry    = "type to filter"
	listTrimEntry      = "⌫ trim"
	listCloseEntryLead = "esc "
	listLegendJoin     = " · "
)

// listZeroMatchNote is the one row a filtering pane paints when its filter matched nothing (ADR
// 0053, 2026-10-14 amendment rule 7): "no model matches "zzz"", with the pane's own noun and the
// human's own keystrokes. An empty pane under a filter would say nothing about WHY it is empty; the
// row says it in the pane's words, and the legend beside it (filterLegend) names the way back.
const listZeroMatchNote = `no %s matches "%s"`

// filterLegend is the legend a filtering pane shows for its filter's state, composed from the one it
// shows with the filter EMPTY (hint) rather than kept per pane per state, so no pane can spell a
// state the others do not:
//
//   - empty filter — hint as it is, "type to filter · …" leading;
//   - text typed, rows matching — "type to filter" drops and "⌫ trim" stands just before the esc
//     entry, every other entry kept in its place;
//   - text typed, NOTHING matching — only "⌫ trim", the pane's rowSetEntries (the keys that change
//     which rows there are to match — /sessions' "^a this/all") and the esc entry: a verb with no
//     row to act on is a promise the pane cannot keep.
//
// An entry is matched whole, so a rowSetEntries value must be spelt exactly as the hint spells it.
func filterLegend(hint, filter string, matches int, rowSetEntries ...string) string {
	if filter == "" {
		return hint
	}
	entries := strings.Split(hint, listLegendJoin)
	kept := make([]string, 0, len(entries)+1)
	trimmed := false
	for _, entry := range entries {
		if entry == listFilterEntry {
			continue
		}
		isClose := strings.HasPrefix(entry, listCloseEntryLead)
		if isClose && !trimmed {
			kept = append(kept, listTrimEntry)
			trimmed = true
		}
		if matches == 0 && !isClose && !containsString(rowSetEntries, entry) {
			continue
		}
		kept = append(kept, entry)
	}
	if !trimmed {
		kept = append(kept, listTrimEntry)
	}
	return strings.Join(kept, listLegendJoin)
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
// body is the block between the title and the rows, for a pane that states one of its own — the
// /settings sub-list's question, which names the key being answered because the list where the human
// read that name is what the sub-list replaced.
//
// titleTail is what rides the title row after the name (popupSpec.titleTail): a filtering pane's
// filter, as overlayFilterTail composes it, and "" for every pane that does not filter or has
// nothing typed. It is part of the content rather than folded in by the paint so the paint and the
// height query ([Model.listHeight]) read one composition and cannot disagree.
type listContent struct {
	pane      framePane
	title     string
	titleTail string
	body      string
	hint      string
	rowCap    int
	rows      []popupRow
	menuRows  bool
	selected  int
}

// statingFilter folds a filtering pane's filter STATE into c, the last step of composing it: the
// legend for that state (filterLegend, from the empty-filter legend c already carries) and — when the
// filter matched nothing and the pane composed no row of its own — the one faint, unselectable
// listZeroMatchNote row, with the highlight off it (−1, the prose convention). filter is the field's
// value, escape-stripped here because the note is a row and a row reaches the popup module plain.
//
// matches is how many rows the filter LEFT, which the pane states rather than this method counting
// c.rows: a pane whose own empty-state note stands where its rows would (the /sessions browser over a
// workspace with no sessions) has painted rows and no matches. That note wins — it is a fact about
// the store, and the more useful one — so the zero-match row joins only an empty row list, while the
// legend still drops the verbs that would have nothing to act on.
func (c listContent) statingFilter(filter, noun string, matches int, rowSetEntries ...string) listContent {
	c.hint = filterLegend(c.hint, filter, matches, rowSetEntries...)
	if filter == "" || matches > 0 || len(c.rows) > 0 {
		return c
	}
	c.rows = singleCellRows([]string{fmt.Sprintf(listZeroMatchNote, noun, stripEscapes(filter))})
	c.selected = -1
	return c
}

// renderList paints an open list overlay through the shared popup module (renderPopup): a titled,
// bordered pane spanning the full window width (m.width, flush with the input box below) holding the
// body block, the rows and a key legend, the selected row highlighted. It is the one budget→render
// call behind every list pane — the picker, the /sessions browser, the /settings sub-lists and the
// "/" | "@" dropdown — and it returns "" when the frame cannot seat the pane beside its siblings, so
// View treats every list's slot alike.
//
// The BODY block is the pane's, and it is BUDGETED rather than merely drawn: the claim is the
// wrapped body (popupFloor.body), so the pane paints what it asked the frame for. The claim is also
// what decides the trade on a window too short for everything — it comes off the top of the grant,
// so the ROWS shrink and the body stays: the /settings sub-list's question is what says which key is
// being answered, while a list you cannot see all of is still a list. The row demand and the row cap
// are untouched by any of it: the pane's taste is its taste with a body exactly as without.
//
// A filter being typed claims nothing: it rides the title row the pane already draws
// (listContent.titleTail), so the first keystroke takes no row from the list.
func (m Model) renderList(c listContent) string {
	view, _, _ := m.renderListPlaced(c)
	return view
}

// renderListPlaced is that same paint with the painter's own PLACEMENT beside it — where the row
// block landed in the box and which of the spec's rows each of its lines belongs to
// (popupPlacement) — and whether the frame could seat the pane at all. It is the call a POINTER
// gesture goes through (popupPaneHit, mouse.go): the row under a click is read off the numbers the
// painter spent rather than re-derived from the title, the hint, the row cap and the filter a
// second time, so a click and the frame it was aimed at can never disagree about which row is
// where. renderList above is this call with the placement dropped, for every caller that only
// paints.
func (m Model) renderListPlaced(c listContent) (string, popupPlacement, bool) {
	spec, seated := m.listSpec(c)
	if !seated {
		return "", popupPlacement{}, false // the frame cannot seat this pane beside its siblings (frameRowPlan)
	}
	view, place := renderPopupPlaced(m.th, spec, m.width)
	return view, place, true
}

// listHeight is the rows the list pane c paints in, answered without painting it (popupHeight): 0
// where renderListPlaced draws nothing. It is the list surface's height query for the frame's
// transcript clamp ([Model.transcriptRows]).
func (m Model) listHeight(c listContent) int {
	spec, seated := m.listSpec(c)
	if !seated {
		return 0
	}
	return popupHeight(m.th, spec, m.width)
}

// listSpec composes the list pane c's [popupSpec] for THIS frame — the budget the frame granted it and
// the pads that budget booked — the one composition renderListPlaced paints and listHeight measures.
// ok is false when the frame cannot seat the pane at all.
func (m Model) listSpec(c listContent) (popupSpec, bool) {
	claim := popupFloor{}
	if c.body != "" {
		claim.body = popupBodyLineCount(m.th, c.body, m.width)
	}
	// The house blanks around the ROW block (popupSpec.rowPadAbove, popupRowStyle.padBelow): one under
	// whatever stands above the rows, one over the key legend. The upper one is spent on every list,
	// a body or none: under the title, or under the /settings sub-list's question, whose last line
	// would otherwise read as more of the prose the rows answer (the approval menu's shape,
	// approval.go). The lower blank follows the legend, because a blank above nothing is a blank above
	// the bottom border.
	//
	// Both are BOOKED here, beside the body's claim: the demand and the cap handed to popupBudget are
	// LINES, so a pane asking for its rows alone would draw the two
	// blanks out of the rows' own window and show one row fewer than its taste on a terminal with the
	// lines to spare. Reserving that window is NOT this call's business — the painter takes the pads
	// off the grant before it seats a row, and hands them back at the floor (popupRowLinesAt) — so a
	// list too tall for its window gives up its breathing room rather than a row of the list.
	padBelow := c.hint != ""
	pad := popupRowPadLines(len(c.rows) > 0, padBelow)
	// The pane's rowCap is the taste; popupBudget is the screen's answer to it, so a long offering on a
	// short terminal shrinks the pane instead of pushing the input box off the frame (D2).
	maxBody, shown, seated := m.popupBudget(c.pane, len(c.rows)+pad, c.rowCap+pad, popupChrome, claim)
	if !seated {
		return popupSpec{}, false
	}
	// A highlight the POINTER just put here keeps the window where the click found it (clickArm.pin), so
	// the row stays under the pointer for the second click; a key or a notch drops the arm and the
	// window re-centres on the cursor.
	rowTop, pinTop := m.clickArmed.pin(c.pane, c.selected)
	return popupSpec{
		title:       c.title,
		titleTail:   c.titleTail,
		body:        c.body,
		maxBodyRows: maxBody,
		rows:        c.rows,
		menuRows:    c.menuRows,
		rowPadAbove: true,
		rowStyle:    popupRowStyle{padBelow: padBelow},
		selected:    c.selected,
		rowTop:      rowTop,
		pinTop:      pinTop,
		hint:        c.hint,
		maxRows:     shown,
		scrollbar:   m.popupScrollbarOn(),
	}, true
}
