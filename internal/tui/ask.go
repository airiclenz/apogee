package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The ask_user pane (P3.11; D5/D9)
// ----------------------------------------------------------------------------

// foldAskRequest records a pending ask_user question and hands the input box over to it: the state
// switches to stateAwaitingAsk, the offering's checked set is sized (multi-select only), and the
// (emptied) box is re-focused so the human types the answer. View renders the question through
// [Model.askPrompt] and [Model.submitAnswer] replies on the request's Reply channel — the C3
// rendezvous the [uiAsker] parked a Step on.
func (m Model) foldAskRequest(msg askReqMsg) (tea.Model, tea.Cmd) {
	m.state = stateAwaitingAsk
	m.pendingAsk = &msg
	m.askSel = listCursor{} // first choice pre-selected while the input is empty (D5); no-op when there are no choices
	// A multi-select question opens with nothing ticked and a checked set sized to its
	// offering; a single-select one carries no checked set at all.
	m.askChecked = nil
	if msg.Request.MultiSelect && len(msg.Request.Choices) > 0 {
		m.askChecked = make([]bool, len(msg.Request.Choices))
	}
	m.dismissAutocomplete() // a stale menu never shares the frame with a decision surface
	// The box is BORROWED, not repossessed: whatever the human was part-way through typing is
	// stashed and handed back when the question lets go of it (restoreAskDraft). Emptying it
	// without that stash discarded an unsent message outright.
	m.askDraft = m.input.Value()
	m.input.Reset()
	// The box is borrowed for the answer, so ⏎ sends rather than queues: the legend must say so
	// for as long as the question stands (submitAnswer swaps it back when the answer is away).
	m.setPlaceholder(m.legendFor(m.idleLegend()))
	m.sel = promptSel{} // the input was emptied for the answer; drop any stale selection
	m.dropRecall()      // and any walk in progress: the box now belongs to the question
	m.layout()
	return m, m.input.Focus()
}

// askChoiceKey is the ask prompt's keypress half, beside the pane that paints it: while a question
// OFFERING CHOICES is up, ↑/↓ move the choice highlight and ␣ ticks a multi-select row — but ONLY
// while the input box is empty (D5). The moment the human types, the guard fails and the arrows
// fall through to the textarea (cursor duty), so multi-line free-text editing is never stolen;
// deleting back to empty restores the highlight. Non-wrapping, clamped to the choice range.
//
// It reports whether it CLAIMED the key, the way the frame's other non-modal panes do
// ([Model.usageKey], [Model.inspectorKey]): the prompt is soft-modal, so every key it does not act
// on goes where it always went. ⏎ is not its key — the enter switch in handleKey sends the answer
// through [Model.submitAnswer] — and esc is the frame's own cancel.
//
// The WALK is the package's shared one ([listCursor.move] with listStopsAtEnds, listsurface.go), so
// this offering and the approval menu answer "what does ↓ do at the bottom" through the same code
// they always answered it the same way in. Which KEYS it claims stays this switch's, deliberately,
// rather than the cursor's full key contract ([listCursor.key]): that contract belongs to the MODAL
// list overlays, which swallow everything they do not claim, and this pane's whole point is that it
// claims as little as possible — the box below is still a text box the human may start typing an
// answer into at any moment.
func (m Model) askChoiceKey(msg tea.KeyPressMsg) (bool, tea.Model, tea.Cmd) {
	if m.state != stateAwaitingAsk || m.pendingAsk == nil ||
		len(m.pendingAsk.Request.Choices) == 0 || m.input.Value() != "" {
		return false, m, nil
	}
	switch msg.String() {
	case "up":
		m.askSel.move(-1, len(m.pendingAsk.Request.Choices), listStopsAtEnds)
		return true, m, nil
	case "down":
		m.askSel.move(1, len(m.pendingAsk.Request.Choices), listStopsAtEnds)
		return true, m, nil
	case "space":
		// ␣ ticks and un-ticks the highlighted row — but ONLY on a multi-select question,
		// which is the only kind that has a checked set. On a single-select one there is
		// nothing to toggle, so the key falls through to the textarea below and opens a
		// free-text answer with a space exactly as it always did.
		if m.pendingAsk.Request.MultiSelect {
			// A checked set shorter than the offering ticks nothing rather than panicking, which is
			// what the −1 [listCursor.highlight] answers for an empty one buys here.
			if sel := m.askSel.highlight(len(m.askChecked)); sel >= 0 {
				m.askChecked[sel] = !m.askChecked[sel]
			}
			return true, m, nil
		}
	}
	return false, m, nil
}

// submitAnswer sends the typed answer back to the blocked ask_user tool over the rendezvous
// reply channel (buffered cap 1, so the send never blocks — messages.go) and returns to
// running so the worker's blocked Step resumes; the spinner tick is re-armed because the
// chain died when the question went up. The input box is emptied (it was borrowed for the
// answer). An empty answer is allowed — the human may legitimately reply with nothing — so
// the answer round-trips whatever was typed. Only reachable from stateAwaitingAsk.
func (m Model) submitAnswer() (tea.Model, tea.Cmd) {
	if m.pendingAsk == nil {
		return m, nil
	}
	// With choices offered and an empty input the answer is the highlighted choice — the SAME
	// escape-stripped label the popup showed, not the raw domain string (D5/D9). A multi-select
	// question with rows ticked answers with those instead, one label per line; with NOTHING
	// ticked it falls back to the highlighted row, so the single-select fast path is its
	// degenerate case rather than a second rule. Otherwise the answer is the trimmed typed text
	// (an empty free-text answer stays allowed when no choices are offered).
	choices := m.pendingAsk.Request.Choices
	var answer string
	if len(choices) > 0 && m.input.Value() == "" {
		if ticked := m.checkedLabels(); len(ticked) > 0 {
			answer = strings.Join(ticked, "\n")
		} else {
			sel := m.askSel.highlight(len(choices)) // defensive clamp; routing keeps it in range
			answer = stripEscapes(choices[sel])
		}
	} else {
		answer = strings.TrimSpace(m.input.Value())
	}
	m.pendingAsk.Reply <- domain.AskAnswer{Text: answer}
	m.pendingAsk = nil
	m.askChecked = nil // the question is answered: no checked set outlives it
	m.input.Reset()
	m.restoreAskDraft() // the question has let go of the box: the message it interrupted comes back
	m.state = stateRunning
	m.setPlaceholder(m.legendFor(runningPlaceholder)) // the box is the human's own again — ⏎ queues from here
	m.layout()
	tick := m.spin.arm()
	return m, tick
}

// checkedLabels returns the escape-stripped labels ticked on a multi-select ask_user question, in
// the order the choices were OFFERED rather than the order they were ticked — the schema's array
// order is the one both sides of the wire agreed on, and it is what makes the reply reproducible.
// Labels, never indices (D9): what travels back is the human's own words, exactly as the popup
// painted them.
//
// It is empty for a single-select question and for a multi-select one with nothing ticked, which is
// precisely what lets submitAnswer fall back to the highlighted row on an empty checked set.
func (m Model) checkedLabels() []string {
	if m.pendingAsk == nil || !m.pendingAsk.Request.MultiSelect {
		return nil
	}
	choices := m.pendingAsk.Request.Choices
	ticked := make([]string, 0, len(m.askChecked))
	for i, on := range m.askChecked {
		if on && i < len(choices) {
			ticked = append(ticked, stripEscapes(choices[i]))
		}
	}
	return ticked
}

// restoreAskDraft hands the input box back the message an ask_user question borrowed it from: the
// unsent draft the human was typing when the question arrived, which the askReqMsg fold stashed
// before emptying the box for the answer. It runs on BOTH ways the box stops being borrowed — the
// answer going out (submitAnswer) and the Exchange ending under the question (finishWorker) — so no
// route leaves the stash behind, and it is a no-op on every path where nothing was borrowed.
//
// Whatever is in the box when it runs is the ANSWER. On the answered path that is already gone
// (submitAnswer resets first). On the abandoned path it is a half-typed answer to a question that no
// longer exists — the human's own keystrokes just the same — so it is KEPT rather than clobbered:
// the draft goes back above it, one line apart, with the caret at the end. Neither half of what was
// typed is discarded on either path, which is the whole point of the stash.
//
// The caller re-lays the frame out: both of them do it a few statements later, for their own
// reasons, and the box has to regrow around the restored draft exactly once.
func (m *Model) restoreAskDraft() {
	draft := m.askDraft
	m.askDraft = ""
	if draft == "" {
		return
	}
	if answer := m.input.Value(); answer != "" {
		draft += "\n" + answer
	}
	m.input.SetValue(draft)
	m.input.MoveToEnd()
}

// maxAskChoiceRows caps how many ask_user choice rows the popup shows at once (the
// maxAutocompleteItems convention); a longer set scrolls its window around the selection. The
// window itself is now budgeted in LINES rather than rows (popupRowWindow), so what the pane hands
// the budget is what this many rows and the blank lines between them COST (askPrompt) — the cap is
// on the offering, which is what a human counts it in.
const maxAskChoiceRows = 8

// askRowStyle is the shape this pane draws its offering in, stated once for the two places that
// have to agree about it: the spec the box is painted from, and the budget arithmetic that books
// what the offering will cost before a line of it is composed (askPrompt). It is the question
// surface's whole row shape — the blank line between two adjacent answers (whose own cost the line
// budget pays for as well as its rows), the blank line closing the block above the border, and the
// fallback that puts the question's lead on the top border where the window seats no line of it
// (popupRowStyle).
var askRowStyle = popupRowStyle{gap: true, padBelow: true, titleFromBody: true}

// askQuestionFloor is how many lines of the QUESTION this pane keeps before its answers claim the
// rest of the window (popupFloor.body). Rows-first is the budget's standing rule and the right one
// almost everywhere, but this pane's offering scales with what the model wrote: four answers of one
// line, the blanks between them and the ones around the block cost nine, and an
// eighty-by-twenty-four terminal — a window nobody would call short — grants the pane ten. The
// question was left with the one line every seated pane keeps, spent on "… (+2 more lines)", so the
// human was asked to choose between four answers with nothing on the screen saying what the choice
// was about.
//
// THREE because that is the shape of the surface rather than a round number: the mockup's own question
// takes two lines at eighty columns, and a third covers the questions that run longer without
// promising room a short window has not got. It is a CEILING on the claim, not a reservation — the
// pane asks for the lesser of it and the question's real wrapped height (popupBodyLineCount), so a
// one-line question leaves the answers every line they had — and it yields in turn to the lines the
// answers need to seat one row, so the give-way ladder at the bottom of the range is untouched.
const askQuestionFloor = 3

// askCheckedMarker and askUncheckedMarker are the checkbox glyphs a MULTI-SELECT question draws in
// front of every option — the mockup's own, pinned by the owner
// (docs/layout/user-questions-layout.md): a bracketed tick rather than ☑/☐, because the pane is painted
// in whatever font the terminal is set to and a box-drawing checkbox is exactly the kind of glyph
// that lands as a blank or a double-width tofu there; ✔ (U+2714) is a one-cell text-presentation
// glyph that stays one cell in every width table. They are the same width as each other, so
// ticking a row repaints three cells and moves nothing.
const (
	askCheckedMarker   = "[✔]"
	askUncheckedMarker = "[ ]"
)

// askChoiceRows composes the offering's popup rows. A single-select question hands over the plain
// one-cell labels it always did (singleCellRows), so its pane is byte-identical to the one drawn
// before multi-select existed. A multi-select question hands over TWO cells — the checkbox, then the
// label — and lets the popup's own column machinery align them: the markers line up in a column of
// their own down the pane, and a label too long for the pane wraps under the LABEL rather than under
// the box beside it (popupRowHangingIndent), so one option still reads as one block of text.
//
// The checked set is read defensively rather than indexed: it is nil for a single-select question
// and sized to the offering for a multi-select one (the askReqMsg fold), and a row it does not reach
// is drawn unticked — the rendering states what the state says, and never panics over what it does
// not.
func askChoiceRows(labels []string, multi bool, checked []bool) []popupRow {
	if !multi {
		return singleCellRows(labels)
	}
	rows := make([]popupRow, len(labels))
	for i, label := range labels {
		marker := askUncheckedMarker
		if i < len(checked) && checked[i] {
			marker = askCheckedMarker
		}
		rows[i] = popupRow{marker, label}
	}
	return rows
}

// askPrompt renders the pending ask_user question as a bordered popup pane above the input box
// (the shared popup module): the wrapped question body, then any offered choices as selectable
// menu rows, and a one-line key hint (P3.11; D5/D6/D8). While the input box is empty and
// choices are offered, ↑/↓ move the highlight and ⏎ sends the highlighted label; the moment the
// human types, the highlight drops (selected −1) and ⏎ sends the typed text — so the answer mode
// is always visible in the chrome. Every model-authored string (question, choices) is
// escape-stripped at this call site.
//
// The pane carries NO title (docs/layout/user-questions-layout.md): "the assistant is asking:" said
// what the question itself says better, and a heading over a single question is a row spent on
// nothing. So the top border is plain (popupSpec.titleInBorder with an empty title) and the question
// is the pane's own heading — which also means the pane's chrome is its two borders and its hint row
// (popupTitleBorderChrome), one row less than it used to draw. The choices are a MENU like the
// approval prompt's — ❯ on the answer the ⏎ would send, · on the rest — and they WRAP with a blank
// line between them, because an ask_user choice is prose written for this one question and a
// decision must not be taken against half a sentence (popupSpec.menuRows, wrapRows, askRowStyle's gap). One
// more blank line sets the offering off from the question above it and another closes it below
// (popupSpec.rowPadAbove and askRowStyle's padBelow, both, as the mockup draws this box): with wrapped prose on both
// sides of the join, the marker column alone is what distinguishes the first answer from the last
// line of the question, and an offering whose answers are a blank line apart would otherwise crowd
// its last one against the border.
//
// A question a SUB-AGENT raised leads its body with `Sub-agent: <its delegated task>`, the approval
// pane's line verbatim and under the same clip (approvalTaskClipRunes) — because it answers the same
// question that pane's does, and answering it two different ways on two decision surfaces would be a
// dialect rather than a design. Concurrent children's questions queue one at a time (ADR 0039), in an
// order nothing on the screen predicts, so the question's own words no longer say whose work it
// serves. The line is absent at depth 0 and the pane is then byte-identical to the one drawn before
// delegation existed.
//
// A MULTI-SELECT question (domain.AskRequest.MultiSelect) draws one thing more: a checkbox in front
// of every option, "[✔]" where the row is ticked and "[ ]" where it is not (askChoiceRows), and a
// hint that names ␣ among the live keys. The boxes are a COLUMN of the popup's own rather than three
// characters glued onto the label — so they align down the pane whatever the labels do, and a
// wrapped option hangs under its label instead of under its box — and the pointer, the dim rows and
// every blank line around them are the menu style's, untouched: the two questions differ by the
// column and the one key, which is exactly how much they differ to answer. A single-select question
// composes the plain labels it always did and paints byte-identically.
//
// The screen budget is derived from the live layout so a long question or a long choice set never
// pushes the input box off-screen: the question keeps its first askQuestionFloor lines ahead of
// everything (popupFloor.body, and never more than leaves the answers the row their window is
// anchored on), past that claim the rows get priority (they are what the human acts on), and the
// body takes what is left and overflows into the explicit "… (+N more lines)" marker. That claim is
// never less than the one line every seated pane keeps, so the shortest window is not a pane of
// pure chrome: the one content row states the question's own count, and the answers — granted no
// window at all there — have theirs counted onto the top BORDER now that no title row is drawn
// (D2, popupTitleLine), so a hint still offering ↑↓ is never the only trace of an offering the pane
// dropped.
//
// Where the shrinking goes one step further and leaves the question no line of its own — a body
// budget of one row, traded whole for the marker, which is the bottom of the ladder now that the
// question has a floor: the windows whose grant past this pane's chrome is the offering's anchor
// row and a single line, so the floor is clamped back to that line and a question longer than the
// pane is wide has nothing to put on it — the question falls back into the top border instead
// (askRowStyle's titleFromBody). Dropping the title was the right call because the question says what a
// heading would; a pane showing NEITHER says nothing, and a decision surface whose whole identity has
// become a count is the case the approval prompt does not have — its tool name is on the border at
// every height it is drawn at. The fallback costs no row (the border is drawn anyway) and applies
// nowhere else: with one line of the question on the screen the border is plain, as the mockup draws
// it.
func (m Model) askPrompt(req domain.AskRequest) string {
	choicesShown := len(req.Choices) > 0 && m.input.Value() == ""

	selected := -1
	hint := "type your answer below · ⏎ send · ⇧⏎/⌥⏎ newline · esc cancel"
	if choicesShown {
		selected = m.askSel.highlight(len(req.Choices)) // clamp: routing keeps it in range, this is defensive
		hint = "↑↓ select · ⏎ send · type for a custom answer · esc cancel"
		if req.MultiSelect {
			// The toggle is the one key this pane has that the single-select one has not, so it is
			// named where every other live key is named; the rest of the legend is word for word the
			// single-select hint, because the rest of the interaction is word for word the same.
			hint = "↑↓ select · ␣ toggle · ⏎ send · type for a custom answer · esc cancel"
		}
	}

	question := stripEscapes(req.Question)
	// A question raised by a sub-agent leads with the child's identity — its name when the delegation
	// was given one, and its delegated task either way — exactly as an approval prompt does
	// (subAgentPromptLine) and clipped by the same bound: with several children running at once their
	// questions QUEUE — one on the screen at a time, the asking child blocked and its siblings still
	// working — and the question's own words say nothing about which of them wrote them. Absent at
	// depth 0, so an undelegated session's pane is unchanged to the byte.
	if line := subAgentPromptLine(req.SubAgentName, req.SubAgentTask); line != "" {
		question = line + "\n" + question
	}

	// Budget against the live layout so a long question or choice set never pushes the input box
	// off-screen (D2); past the question's own floor the rows get priority and the body takes what is
	// left (see popupBudget).
	//
	// Both row figures are in the LINES the window will paint, not in choices: an option may now wrap
	// onto two or three lines, every adjacent pair is a blank line apart and the block itself is set
	// off by one more above and below (popupSpec.rowPadAbove and askRowStyle's padBelow), so a pane asking for one row
	// per choice would promise three answers and paint ten. maxAskChoiceRows stays a cap on the
	// OFFERING — in this budget, what that many options and their separators cost — because a cap read
	// as eight LINES would scroll five one-line choices, the top of the schema's own 2-5 range, on a
	// terminal with the room for all of them.
	// The SAME rows are measured and painted — a multi-select question's marker column is part of
	// what its options cost in lines, so the budget below is spent on the pane that is drawn rather
	// than on a narrower one composed for the arithmetic.
	rows := askChoiceRows(stripEscapesAll(req.Choices), req.MultiSelect, m.askChecked)
	heights := popupWrappedRowHeights(m.th, rows, m.width)
	askPad := popupRowPadLines(true, askRowStyle.padBelow)
	wanted := popupRowBlockLines(heights, askRowStyle.gapLines(), askPad)
	capped := popupRowBlockLines(heights[:min(len(heights), maxAskChoiceRows)], askRowStyle.gapLines(), askPad)
	floor := popupFloor{
		body: min(askQuestionFloor, popupBodyLineCount(m.th, question, m.width)),
		rows: askAnchorRowLines(selected, heights),
	}
	maxBodyRows, rowLines, seated := m.popupBudget(panePrompt, wanted, capped, popupTitleBorderChrome, floor)
	if !seated {
		return "" // the frame cannot seat this pane beside its siblings (frameRowPlan)
	}

	spec := popupSpec{
		titleInBorder: true, // no title at all: an empty one leaves the top border unbroken…
		body:          question,
		maxBodyRows:   maxBodyRows,
		rows:          rows,
		menuRows:      true,
		wrapRows:      true,
		// …and the question surface's own row shape: the blank line between two answers, the one
		// closing the offering above the border, and the question's lead on that unbroken border where
		// the window seats no line of it (askRowStyle).
		rowStyle:    askRowStyle,
		rowPadAbove: true, // the blank line under the question
		selected:    selected,
		hint:        hint,
		maxRows:     rowLines,
		scrollbar:   m.popupScrollbarOn(),
	}
	return renderPopup(m.th, spec, m.width)
}

// askAnchorRowLines is what the ask prompt's offering must keep to put ONE answer on the screen: the
// painted height of the row the window is anchored on (popupFloor.rows). A row is seated whole or not
// at all, so a two-line answer needs both of its lines — a budget of one seats nothing and counts all
// four onto the border, which is the state the question's floor may never buy itself.
//
// The anchor is popupRowWindow's own — the selection clamped into the list, which is row 0 for the
// −1 the pane carries once free text is typed — computed here rather than assumed to be the first
// row, so the floor and the window agree about which row has to fit. An empty offering claims nothing.
func askAnchorRowLines(selected int, heights []int) int {
	if len(heights) == 0 {
		return 0
	}
	return heights[clampInt(selected, 0, len(heights)-1)]
}
