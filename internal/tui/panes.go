package tui

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
// whether it owns the keyboard while it is up, the predicate that says it is open, and the renderer
// that paints it. `open` is the same predicate `render` returns "" on, so the frame-wide row
// allocation ([Model.frameRowPlan]) is divided between exactly the panes that will be drawn.
//
// modal says the pane swallows every key it does not act on while it is up — the /sessions browser,
// the picker and the /settings pane through their modalClaim rung of keyClaimOrder, the approval or
// ask prompt through the state-gated switches in handleKey — as opposed to a pane that says something
// rather than asking (the four reports, the dropdown), behind which the box stays live.
type paneSpec struct {
	name   string             // what the pane is called in a diagnostic
	slot   paneSlot           // which of the two slots stacks it
	modal  bool               // whether it owns the keyboard while it is up
	open   func(Model) bool   // whether the Model has it open
	render func(Model) string // its rendered block for one frame, "" when closed or unseated
}

// paneSpecs is the pane table: one row per framePane, indexed by it, so that "which panes are open"
// ([Model.openPanes]), "what does each one paint" ([Model.frameOverlays]) and "in what order does the
// slot stack them" (stackTranscriptSlot) are three walks over ONE table rather than three lists that
// have to agree. A pane that joined the frame without joining all three was a bug none of them could
// be read to find; now a pane that has no row fails TestEveryFramePaneHasASpec.
//
// The index IS the order: framePane's constants are the order the panes give way in AND the order the
// transcript-side slot stacks them in, top to bottom — one order, stated once on framePane (model.go).
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
		},
		paneBrowser: {
			// The /sessions browser takes the prompt's position in the slot because they never
			// co-occur: the browser is idle-only and modal, and the prompts belong to busy states.
			name:   "sessions browser",
			slot:   slotTranscript,
			modal:  true,
			open:   func(m Model) bool { return m.sessionBrowser.open },
			render: Model.renderSessionBrowser,
		},
		panePicker: {
			// The /model | /server picker shares that position on the same terms as the browser.
			name:   "picker",
			slot:   slotTranscript,
			modal:  true,
			open:   func(m Model) bool { return m.picker.open },
			render: Model.renderPicker,
		},
		paneSettings: {
			// The /settings pane is the frame's one FULL-HEIGHT pane (frameRowPlan): it is granted the
			// transcript's whole budget, and on every window it is seated in it is the only thing in
			// the slot — its verb is idle-only and it swallows every key, so no prompt, browser, picker
			// or dropdown can be up beside it.
			name:   "settings pane",
			slot:   slotTranscript,
			modal:  true,
			open:   func(m Model) bool { return m.settings.open },
			render: Model.renderSettings,
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
		},
	}
}

// reportPaneRow is the row of one report pane: open and render both resolve through the kind's row
// of reportRows (reportpane.go), so a fifth report is one row here and one there. A report is never
// modal — it says something rather than asking, and the box behind it stays live.
func reportPaneRow(name string, r reportKind) paneSpec {
	return paneSpec{
		name:   name,
		slot:   slotTranscript,
		modal:  false,
		open:   func(m Model) bool { return m.reportState(r).open },
		render: func(m Model) string { return m.renderReport(r) },
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
