package tui

import tea "charm.land/bubbletea/v2"

// ----------------------------------------------------------------------------
// The pane table: one row per framePane
// ----------------------------------------------------------------------------

// paneSlot names which of the frame's two overlay slots a pane is stacked in. Every pane is FLUSH on
// the chrome its slot abuts: a transcript-side pane goes directly above the ▔ hairline (the frame's
// blank gap row falls above it, not between it and the chrome), an input-side pane directly above
// the input box.
type paneSlot uint8

const (
	slotTranscript paneSlot = iota // above the ▔ hairline, between the transcript and the chrome
	slotInput                      // below the status line, hugging the input box
)

// paneSpec is ONE boxed overlay of the frame as a row: what it is called, which slot stacks it,
// whether it owns the keyboard while it is up, the predicate that says it is open, the renderer
// that paints it, and its three answers to the human's gestures — the key claim, the click and the
// wheel. `open` is the same predicate `render` returns "" on, so the frame-wide row allocation
// ([Model.frameRowPlan]) is divided between exactly the panes that will be drawn.
//
// modal says the pane swallows every key it does not act on while it is up — the /sessions browser,
// the picker and the /settings pane through their modalClaim key, the approval or ask prompt through
// the state-gated switches in handleKey — as opposed to a pane that says something rather than
// asking (the four reports, the dropdown), behind which the box stays live.
//
// key is the pane's rung of the overlay precedence in the claimant currency [keyClaimant.claim]
// speaks — the row applies [modalClaim] or [paneClaim] to the pane's own handler itself, so the
// rung of [keyClaimOrder] that reads it ([paneClaimant]) adapts nothing. It is nil for the prompt
// alone: the approval and the ask prompt are modal by STATE, and their keys are handleKey's own
// switches. keyOpen is that rung's gate — whether the pane is up and entitled to be asked at all;
// nil means "always ask", the pane's own claim then being the whole test, which a closed pane
// answers false to anyway. Which rung the pane is — where it rises and falls — is keyClaimOrder's
// decision, not the row's (ADR 0053 D3).
//
// click is what [Model.handleMouseClick] asks of the pane, in that chain's three-part currency —
// the Model, a tea.Cmd and whether the pane CLAIMED the click — and wheel is what
// [Model.foldMouseWheel] asks, in the wheel's two-part one (no Cmd exists on that side: a notch moves
// a highlight and hands back no work). Every click func takes the live m, which is what mutates, and
// the pre-click frame pre, which every geometry question is put to (handleMouseClick's rule); the
// walk composes pre once and hands the same value to every pane. Which order the two gestures ask
// the panes in is [pointerPanes]' decision (mouse.go), never the row's.
type paneSpec struct {
	name    string                                                           // what the pane is called in a diagnostic
	slot    paneSlot                                                         // which of the two slots stacks it
	modal   bool                                                             // whether it owns the keyboard while it is up
	open    func(Model) bool                                                 // whether the Model has it open
	render  func(Model) string                                               // its rendered block for one frame, "" when closed or unseated
	key     func(Model, tea.KeyPressMsg) (Model, tea.Cmd, bool)              // its key claim, in claimant currency; nil only for the prompt
	keyOpen func(Model) bool                                                 // the key claim's gate; nil = always ask
	click   func(m, pre Model, msg tea.MouseClickMsg) (Model, tea.Cmd, bool) // its answer to a left-click on the frame
	wheel   func(Model, tea.MouseWheelMsg) (Model, bool)                     // its answer to a wheel notch on the frame
}

// paneSpecs is the pane table: one row per framePane, indexed by it, so that "which panes are open"
// ([Model.openPanes]), "what does each one paint" ([Model.frameOverlays]), "in what order does the
// slot stack them" (stackTranscriptSlot), "is this key yours" ([keyClaimOrder], through
// [paneClaimant]) and "is this click or notch yours" ([Model.handleMouseClick] and
// [Model.foldMouseWheel], through [pointerPanes]) are five walks over ONE table rather than five
// lists that have to agree. A pane that joined the frame without joining all of them was a bug none
// of them could be read to find; now a pane that has no row fails TestEveryFramePaneHasASpec.
//
// The index IS the order: framePane's constants are the order the panes give way in AND the order the
// transcript-side slot stacks them in, top to bottom — one order, stated once on framePane (model.go).
// The two gesture chains keep their own orders — the key precedence on keyClaimOrder, the click-chain
// order on pointerPanes — because walking this table in index order would change which pane answers
// first; the table holds what each pane DOES with a key, a click or a notch, and the ordered lists
// hold when it is asked.
//
// It is a package value rather than a field (the Model is copied on every Update — ADR 0011 — and a
// table of funcs is nothing a frame needs to carry) and it is filled in init() rather than by its
// declaration: a render func reaches the painter, which asks the row allocation, which asks openPanes,
// which reads this table — a reference loop a declaration-time initializer would be refused for
// (render → renderReport → popupBudget → frameRowPlan → openPanes → paneSpecs). Anything that wants a
// row must therefore read it at CALL time, through a closure — a package var that copied a row's
// field at its own declaration would copy the zero value, since every init function runs after every
// variable initializer.
var paneSpecs [paneKinds]paneSpec

func init() {
	paneSpecs = [paneKinds]paneSpec{
		panePrompt: {
			// The approval and the ask prompt belong to different states, so they are never both up:
			// the one pane covers both. It is modal by STATE rather than by a keyClaimOrder rung — at
			// awaitingApproval the keyboard belongs to a/d/s and the Enter-dismiss, at awaitingAsk to
			// the borrowed answer box (handleKey).
			name:   "prompt",
			slot:   slotTranscript,
			modal:  true,
			open:   Model.promptOpen,
			render: Model.renderPrompt,
			click:  promptPointerClick,
			wheel:  Model.promptWheel,
		},
		paneBrowser: {
			// The /sessions browser takes the prompt's position in the slot because they never
			// co-occur: the browser is idle-only and modal, and the prompts belong to busy states.
			name:    "sessions browser",
			slot:    slotTranscript,
			modal:   true,
			open:    func(m Model) bool { return m.sessionBrowser.open },
			render:  Model.renderSessionBrowser,
			key:     modalClaim(Model.sessionBrowserKey),
			keyOpen: func(m Model) bool { return m.state == stateIdle && m.sessionBrowser.open },
			click:   Model.handleBrowserClick,
			wheel:   Model.browserWheel,
		},
		panePicker: {
			// The /model | /server picker shares that position on the same terms as the browser.
			name:   "picker",
			slot:   slotTranscript,
			modal:  true,
			open:   func(m Model) bool { return m.picker.open },
			render: Model.renderPicker,
			key:    modalClaim(Model.pickerKey),
			// Asked in BOTH live states, because /schedule opens its cycle and mode popups mid-Exchange
			// as well, and /sub-agents-server the one it retargets delegations from — those verbs are
			// whileRunning (commandSpecs), and an overlay that renders without taking keys would be a
			// modal the human cannot answer. The older kinds are unaffected: their verbs are idle-only,
			// and a picker cannot be open when a worker STARTS, since it owns ⏎ for as long as it is up.
			keyOpen: func(m Model) bool { return m.state.live() && m.picker.open },
			click:   Model.handlePickerClick,
			wheel:   Model.pickerWheel,
		},
		paneSettings: {
			// The /settings pane is the frame's one FULL-HEIGHT pane (frameRowPlan): it is granted the
			// transcript's whole budget, and on every window it is seated in it is the only thing in
			// the slot — its verb is idle-only and it swallows every key, so no prompt, browser, picker
			// or dropdown can be up beside it.
			name:    "settings pane",
			slot:    slotTranscript,
			modal:   true,
			open:    func(m Model) bool { return m.settings.open },
			render:  Model.renderSettings,
			key:     modalClaim(Model.settingsKey),
			keyOpen: Model.settingsOwnsInput,
			click:   settingsPointerClick,
			wheel:   Model.settingsWheel,
		},
		// The four reports close the transcript-side slot, nearest the chrome, because they are the
		// panes that CAN be up beside another: their verbs are whileRunning, so a report opens over an
		// approval or ask prompt the run is blocked on — and stacking it under the prompt keeps the
		// surface the human is answering in the position it has when no report is up.
		paneUsage:     reportPaneRow("usage report", usageReport),
		paneInspector: reportPaneRow("inspector pane", inspectReport),
		paneThinking:  reportPaneRow("thinking pane", thinkingReport),
		paneAdvice:    reportPaneRow("advice pane", adviceReport),
		paneDropdown: {
			// The command / @file / skill autocomplete is the input slot's own tenant, drawn flush over
			// the box rather than over the transcript, and the one list that is not modal at all: any
			// key it does not act on falls through to edit the input, which re-derives it.
			name:   "autocomplete dropdown",
			slot:   slotInput,
			modal:  false,
			open:   func(m Model) bool { return m.autocomplete.active && len(m.autocomplete.items) > 0 },
			render: Model.renderAutocomplete,
			key:    paneClaim(Model.autocompleteKey),
			// Every region opens in BOTH states (computeAutocomplete), so an interjection reaches a
			// file, a skill and a reporting command as easily as a submitted message does; the
			// per-command while-running policy is applied at accept (commandRunnable), not by hiding
			// the menu.
			keyOpen: func(m Model) bool { return m.state.live() && m.autocomplete.active },
			click:   Model.handleDropdownClick,
			wheel:   Model.dropdownWheel,
		},
	}
}

// reportPaneRow is the row of one report pane: open, render, key, click and wheel all resolve through
// the kind's row of reportRows (reportpane.go), so a fifth report is one row here and one there. A
// report is never modal — it says something rather than asking, and the box behind it stays live —
// so its key claim is the soft one ([paneClaim] over [Model.reportKey]) with no gate: the claim
// itself answers false while the report is closed. The click is [Model.handleReportClick] — inside
// the box it is claimed and nothing happens, outside it the report is dismissed and the click goes on
// — and the wheel is [Model.reportWheel], one row per notch while the pointer is over it.
func reportPaneRow(name string, r reportKind) paneSpec {
	return paneSpec{
		name:   name,
		slot:   slotTranscript,
		modal:  false,
		open:   func(m Model) bool { return m.reportState(r).open },
		render: func(m Model) string { return m.renderReport(r) },
		key:    paneClaim(reportClaim(r)),
		click: func(m, pre Model, msg tea.MouseClickMsg) (Model, tea.Cmd, bool) {
			return m.handleReportClick(r, pre, msg)
		},
		wheel: func(m Model, msg tea.MouseWheelMsg) (Model, bool) {
			return m.reportWheel(r, msg)
		},
	}
}

// paneClaimant is one pane's rung of [keyClaimOrder], read off its row of paneSpecs: the gate is the
// row's keyOpen (nil there means "always ask") and the claim is the row's key. Both are closures that
// index the table at CALL time rather than copies taken here — keyClaimOrder is a package var, and
// every variable initializer runs before the init function that fills paneSpecs, so a value copied at
// declaration would be the zero row. The name is a literal beside each rung for the same reason: the
// precedence names TestKeyClaimOrderMatchesTheDocumentedPrecedence reads must be there before init()
// has run, and the rung's name is the precedence documentation's word for the surface, which is not
// always the pane table's (the "autocomplete overlay" rung reads the "autocomplete dropdown" row).
func paneClaimant(p framePane, name string) keyClaimant {
	return keyClaimant{
		name: name,
		open: func(m Model) bool {
			gate := paneSpecs[p].keyOpen
			return gate == nil || gate(m)
		},
		claim: func(m Model, msg tea.KeyPressMsg) (Model, tea.Cmd, bool) {
			return paneSpecs[p].key(m, msg)
		},
	}
}

// promptOpen reports whether the frame has a decision prompt up — the approval or the ask pane, each
// only in its own state and only while the request it answers is held.
func (m Model) promptOpen() bool {
	return (m.state == stateAwaitingApproval && m.pending != nil) ||
		(m.state == stateAwaitingAsk && m.pendingAsk != nil)
}

// renderPrompt paints the decision prompt the frame has up, or "" when it has none (promptOpen).
func (m Model) renderPrompt() string {
	if m.state == stateAwaitingApproval && m.pending != nil {
		return m.approvalPrompt(m.pending.Request)
	}
	if m.state == stateAwaitingAsk && m.pendingAsk != nil {
		return m.askPrompt(m.pendingAsk.Request)
	}
	return ""
}

// block is the rendered block of one pane of the frame, "" when that pane is not on it. It is what
// lets the slot's order be WALKED (stackTranscriptSlot, model.go) instead of re-listed field by field
// at every rectangle in it.
func (o frameOverlays) block(p framePane) string { return o.panes[p] }
