package tui

import (
	"cmp"
	"slices"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
)

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
// height is render's height query: the screen rows render's block takes, answered from the pane's
// spec without painting it (popupHeight), and 0 wherever render returns "" — a closed pane, one the
// frame cannot seat, a width with no room for a box. It is what the frame's transcript clamp measures
// the panes by ([Model.transcriptRows]), so the repaint tail of an Update ([Model.settle]) renders no
// pane and View renders each open one once. It must agree with render to the row — the widget's scroll
// clamp and the click map rest on it — and TestOverlayHeightQueryMatchesItsRender pins that for every row.
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
// answers false to anyway.
//
// click is what [Model.handleMouseClick] asks of the pane, in that chain's three-part currency —
// the Model, a tea.Cmd and whether the pane CLAIMED the click — and wheel is what
// [Model.foldMouseWheel] asks, in the wheel's two-part one (no Cmd exists on that side: a notch moves
// a highlight and hands back no work). Every click func takes the live m, which is what mutates, and
// the pre-click frame pre, which every geometry question is put to (handleMouseClick's rule); the
// walk composes pre once and hands the same value to every pane.
//
// rank is WHEN the pane is asked: its rung of [keyClaimOrder] and its place in [pointerPanes], the two
// gesture chains. Both lists are derived from the rows' ranks in init(), so where a pane rises and
// falls in either chain is stated on its row beside what it does there ([paneRank]).
type paneSpec struct {
	name    string                                                           // what the pane is called in a diagnostic
	slot    paneSlot                                                         // which of the two slots stacks it
	modal   bool                                                             // whether it owns the keyboard while it is up
	open    func(Model) bool                                                 // whether the Model has it open
	render  func(Model) string                                               // its rendered block for one frame, "" when closed or unseated
	height  func(Model) int                                                  // the rows render's block takes, without rendering it; 0 where render is ""
	key     func(Model, tea.KeyPressMsg) (Model, tea.Cmd, bool)              // its key claim, in claimant currency; nil only for the prompt
	keyOpen func(Model) bool                                                 // the key claim's gate; nil = always ask
	click   func(m, pre Model, msg tea.MouseClickMsg) (Model, tea.Cmd, bool) // its answer to a left-click on the frame
	wheel   func(Model, tea.MouseWheelMsg) (Model, bool)                     // its answer to a wheel notch on the frame
	rank    paneRank                                                         // where the two gesture chains ask it
}

// paneRank is where one pane stands in the two gesture chains, 1 for the first rung. key is its rung
// of the overlay precedence ([keyClaimOrder]) and 0 for the prompt alone, which has no rung — its keys
// are handleKey's state switches. pointer is its place in the one order the click and the wheel ask the
// panes in ([pointerPanes], whose doc states why the order is what it is), and every pane has one. The
// ranks of one chain are distinct, so each chain is ONE order; TestKeyClaimOrderMatchesTheDocumentedPrecedence
// and TestPointerPanesWalkInTheClickChainOrder pin the orders they derive.
type paneRank struct {
	key     int // rung of keyClaimOrder, 0 = none
	pointer int // place in pointerPanes
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
// The two gesture chains keep their own orders — the key precedence, the click-chain order — because
// walking this table in index order would change which pane answers first; each row carries its rank
// in both ([paneRank]) beside what the pane DOES with a key, a click or a notch, and init() derives the
// ordered lists ([keyClaimOrder], [pointerPanes]) from those ranks, so a pane's place in a chain is
// stated once, on its row.
//
// It is a package value rather than a field (the Model is copied on every Update — ADR 0011 — and a
// table of funcs is nothing a frame needs to carry) and it is filled in init() rather than by its
// declaration: a render func reaches the painter, which asks the row allocation, which asks openPanes,
// which reads this table — a reference loop a declaration-time initializer would be refused for
// (render → renderReport → popupBudget → frameRowPlan → openPanes → paneSpecs). Anything that wants a
// row must therefore read it at CALL time, through a closure — a package var that copied a row's
// field at its own declaration would copy the zero value, since every init function runs after every
// variable initializer. That is why the two gesture chains are assigned at the end of the same init(),
// once the rows they are derived from exist.
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
			height: Model.promptHeight,
			click:  promptPointerClick,
			wheel:  Model.promptWheel,
			// No key rung, for the reason above; its place in the pointer chain is the one pointerPanes
			// states.
			rank: paneRank{pointer: 9},
		},
		paneBrowser: {
			// The /sessions browser takes the prompt's position in the slot because they never
			// co-occur: the browser is idle-only and modal, and the prompts belong to busy states.
			name:    "sessions browser",
			slot:    slotTranscript,
			modal:   true,
			open:    func(m Model) bool { return m.sessionBrowser.open },
			render:  Model.renderSessionBrowser,
			height:  Model.sessionBrowserHeight,
			key:     modalClaim(Model.sessionBrowserKey),
			keyOpen: func(m Model) bool { return m.state == stateIdle && m.sessionBrowser.open },
			click:   Model.handleBrowserClick,
			wheel:   Model.browserWheel,
			// The top rung of the key precedence: a modal overlay (idle only), so while open it claims
			// every keypress — selection, resume, delete-confirm, rename edit, and esc to close
			// (sessions.go) — before the normal input routing, exactly as the autocomplete dropdown claims
			// its keys first.
			rank: paneRank{key: 1, pointer: 6},
		},
		panePicker: {
			// The /model | /server picker shares that position on the same terms as the browser.
			name:   "picker",
			slot:   slotTranscript,
			modal:  true,
			open:   func(m Model) bool { return m.picker.open },
			render: Model.renderPicker,
			height: Model.pickerHeight,
			key:    modalClaim(Model.pickerKey),
			// Asked in BOTH live states, because /schedule opens its cycle and mode popups mid-Exchange
			// as well, and /sub-agents-server the one it retargets delegations from — those verbs are
			// whileRunning (commandSpecs), and an overlay that renders without taking keys would be a
			// modal the human cannot answer. The older kinds are unaffected: their verbs are idle-only,
			// and a picker cannot be open when a worker STARTS, since it owns ⏎ for as long as it is up.
			keyOpen: func(m Model) bool { return m.state.live() && m.picker.open },
			click:   Model.handlePickerClick,
			wheel:   Model.pickerWheel,
			// The browser's simpler sibling, and it claims keys the same way: while it is open the
			// selection, the accept and esc are all its own (picker.go) — in BOTH live states, for the
			// reason keyOpen states. Its rung sits under the /settings pane's.
			rank: paneRank{key: 3, pointer: 7},
		},
		paneWorkflows: {
			// The /workflows view shares that position on the picker's terms: modal, and asked in
			// BOTH live states, because its verb is whileRunning (commandSpecs) — the human reads a
			// workflow's items while the conversation runs, and a pane that rendered without taking
			// keys would be a modal the human cannot close. It cannot be open when a worker STARTS,
			// since it owns ⏎ for as long as it is up (workflows.go).
			name:    "workflows view",
			slot:    slotTranscript,
			modal:   true,
			open:    func(m Model) bool { return m.workflowsPane.open },
			render:  Model.renderWorkflows,
			height:  Model.workflowsHeight,
			key:     modalClaim(Model.workflowsKey),
			keyOpen: func(m Model) bool { return m.state.live() && m.workflowsPane.open },
			click:   Model.handleWorkflowsClick,
			wheel:   Model.workflowsWheel,
			// The picker's sibling in the key precedence as well: modal while it is up, in both live
			// states, for the reason keyOpen states (workflows.go). The two are never open together —
			// each owns ⏎ for as long as it is up, so neither verb can be accepted over the other — so
			// the order between them decides nothing.
			rank: paneRank{key: 4, pointer: 8},
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
			height:  Model.settingsHeight,
			key:     modalClaim(Model.settingsKey),
			keyOpen: Model.settingsOwnsInput,
			click:   settingsPointerClick,
			wheel:   Model.settingsWheel,
			// Modal in the key precedence as the browser is, and for a stronger reason: as the frame's one
			// full-height pane it hides the input box behind it, a box the human cannot read — a keystroke
			// falling through to it would edit an invisible draft. Idle-only, like its verb (commandSpecs),
			// and it swallows every key it does not act on (settings.go). It is asked FIRST of the pointer
			// chain, for the reason pointerPanes states.
			rank: paneRank{key: 2, pointer: 1},
		},
		// The four reports close the transcript-side slot, nearest the chrome, because they are the
		// panes that CAN be up beside another: their verbs are whileRunning, so a report opens over an
		// approval or ask prompt the run is blocked on — and stacking it under the prompt keeps the
		// surface the human is answering in the position it has when no report is up.
		//
		// In the key precedence the four sit under the dropdown and above the run view.
		//
		// The /usage report claims esc and the four keys that scroll it, and nothing else (usage.go). It
		// is not modal — it says something rather than asking, so the box behind it stays live and every
		// other key goes where it always went — and its rung sits below the overlays above precisely
		// because they ARE modal: a pane that owns the keyboard answers its own esc first. Below the
		// dropdown for the same reason one rung down: a menu a keystroke opened is dismissed by the esc
		// the human means for it.
		//
		// It must stay ABOVE the transcript's PgUp/PgDn interception in handleKey, which claims those two
		// keys in every state: the pane is what the human is reading, and a page key that scrolled the
		// conversation hidden BEHIND the report would move the one list they cannot see.
		paneUsage: reportPaneRow("usage report", usageReport, paneRank{key: 6, pointer: 2}),
		// The /inspect pane claims the same five keys, plus a ctrl+r of its own that flips its rendering,
		// on the same terms (inspector.go): not modal, so every key it does not act on goes where it
		// always went, and below the modal overlays because a pane that owns the keyboard answers its own
		// esc first. It sits beside the report it is shaped after — the two are never open together in
		// practice and their claims are disjoint while one of them is closed, so the order between THEM
		// decides nothing.
		paneInspector: reportPaneRow("inspector pane", inspectReport, paneRank{key: 7, pointer: 3}),
		// The /thinking pane claims the report's five keys and no sixth — it has ONE rendering, so there
		// is no ctrl+r here (thinkingpane.go) — on the same non-modal terms as the two panes above it:
		// every key it does not act on goes where it always went, and it sits below the modal overlays
		// because a pane that owns the keyboard answers its own esc first.
		//
		// It sits beside its two siblings, and ABOVE the run view on purpose: opened inside a view the
		// pane shows that run's thinking, so the esc that closes it is the esc the human means for it,
		// and the NEXT esc goes on up the view exactly as it would have with no pane open.
		paneThinking: reportPaneRow("thinking pane", thinkingReport, paneRank{key: 8, pointer: 4}),
		// The /advice pane claims the report's five keys and no sixth — one rendering, no ctrl+r
		// (advicepane.go) — on the same non-modal terms as the three panes above it, and sits beside
		// them for the same reason: a pane that owns the keyboard answers its own esc first, and the
		// esc that closes a report opened inside a run view is the esc the human means for it.
		paneAdvice: reportPaneRow("advice pane", adviceReport, paneRank{key: 9, pointer: 5}),
		paneDropdown: {
			// The command / @file / skill autocomplete is the input slot's own tenant, drawn flush over
			// the box rather than over the transcript, and the one list that is not modal at all: any
			// key it does not act on falls through to edit the input, which re-derives it.
			name:   "autocomplete dropdown",
			slot:   slotInput,
			modal:  false,
			open:   func(m Model) bool { return m.autocomplete.active && len(m.autocomplete.items) > 0 },
			render: Model.renderAutocomplete,
			height: Model.autocompleteHeight,
			key:    paneClaim(Model.autocompleteKey),
			// Every region opens in BOTH states (computeAutocomplete), so an interjection reaches a
			// file, a skill and a reporting command as easily as a submitted message does; the
			// per-command while-running policy is applied at accept (commandRunnable), not by hiding
			// the menu.
			keyOpen: func(m Model) bool { return m.state.live() && m.autocomplete.active },
			click:   Model.handleDropdownClick,
			wheel:   Model.dropdownWheel,
			// While it is open it claims the navigation, accept and dismiss keys — enter and tab among
			// them — under the four modal overlays' rungs and before the normal routing; any other key
			// falls through to edit the input. It closes the pointer chain, for the reason pointerPanes
			// states.
			rank: paneRank{key: 5, pointer: 10},
		},
	}
	keyClaimOrder = keyClaimRungs()
	pointerPanes = panesRankedBy(func(r paneRank) int { return r.pointer })
}

// panesRankedBy is every pane whose rank under by is non-zero, lowest rank first: one gesture chain
// read off the rows of paneSpecs. It reads the table, so it runs only once init() has filled it.
func panesRankedBy(by func(paneRank) int) []framePane {
	var ps []framePane
	for p := framePane(0); p < paneKinds; p++ {
		if by(paneSpecs[p].rank) > 0 {
			ps = append(ps, p)
		}
	}
	slices.SortStableFunc(ps, func(a, b framePane) int {
		return cmp.Compare(by(paneSpecs[a].rank), by(paneSpecs[b].rank))
	})
	return ps
}

// keyClaimRungs is [keyClaimOrder] derived: the panes that have a key rung, in rank order, each read
// off its row ([paneClaimant]), followed by the surfaces that are not panes ([transcriptClaimants]) —
// the run view, then the block cursor.
func keyClaimRungs() []keyClaimant {
	panes := panesRankedBy(func(r paneRank) int { return r.key })
	order := make([]keyClaimant, 0, len(panes)+len(transcriptClaimants))
	for _, p := range panes {
		order = append(order, paneClaimant(p))
	}
	return append(order, transcriptClaimants...)
}

// reportPaneRow is the row of one report pane: open, render, key, click and wheel all resolve through
// the kind's row of reportRows (reportpane.go), so a fifth report is one row here and one there. A
// report is never modal — it says something rather than asking, and the box behind it stays live —
// so its key claim is the soft one ([paneClaim] over [Model.reportKey]) with no gate: the claim
// itself answers false while the report is closed. The click is [Model.handleReportClick] — inside
// the box it is claimed and nothing happens, outside it the report is dismissed and the click goes on
// — and the wheel is [Model.reportWheel], one row per notch while the pointer is over it. rank is the
// kind's place in the two gesture chains, which the table states beside each report's row.
func reportPaneRow(name string, r reportKind, rank paneRank) paneSpec {
	return paneSpec{
		name:   name,
		slot:   slotTranscript,
		modal:  false,
		open:   func(m Model) bool { return m.reportState(r).open },
		render: func(m Model) string { return m.renderReport(r) },
		height: func(m Model) int { return m.reportHeight(r) },
		key:    paneClaim(reportClaim(r)),
		click: func(m, pre Model, msg tea.MouseClickMsg) (Model, tea.Cmd, bool) {
			return m.handleReportClick(r, pre, msg)
		},
		wheel: func(m Model, msg tea.MouseWheelMsg) (Model, bool) {
			return m.reportWheel(r, msg)
		},
		rank: rank,
	}
}

// paneClaimant is one pane's rung of [keyClaimOrder], read off its row of paneSpecs: the name is the
// row's, the gate is the row's keyOpen (nil there means "always ask", as [keyClaimant.open] does) and
// the claim is the row's key. It copies the row's fields, so it is called only once init() has filled
// the table — which is where keyClaimOrder is built ([keyClaimRungs]).
func paneClaimant(p framePane) keyClaimant {
	row := paneSpecs[p]
	return keyClaimant{name: row.name, open: row.keyOpen, claim: row.key}
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

// promptHeight is the rows renderPrompt paints the decision prompt in, answered without painting it
// (popupHeight); 0 where it paints nothing.
func (m Model) promptHeight() int {
	var (
		spec   popupSpec
		seated bool
	)
	switch {
	case m.state == stateAwaitingApproval && m.pending != nil:
		spec, seated = m.approvalPromptSpec(m.pending.Request)
	case m.state == stateAwaitingAsk && m.pendingAsk != nil:
		spec, seated = m.askPromptSpec(m.pendingAsk.Request)
	}
	if !seated {
		return 0
	}
	return popupHeight(m.th, spec, m.width)
}

// sessionBrowserHeight is the rows renderSessionBrowser paints the browser in, answered without
// painting it (listHeight); 0 where it paints nothing.
func (m Model) sessionBrowserHeight() int {
	c, ok := m.browserListContent()
	if !ok {
		return 0
	}
	return m.listHeight(filteredListContent(m.sessionBrowser.filter, c))
}

// pickerHeight is the rows renderPicker paints the picker in, answered without painting it
// (listHeight); 0 where it paints nothing.
func (m Model) pickerHeight() int {
	c, ok := m.pickerListContent()
	if !ok {
		return 0
	}
	return m.listHeight(filteredListContent(m.picker.filter, c))
}

// autocompleteHeight is the rows renderAutocomplete paints the dropdown in, answered without painting
// it (listHeight); 0 where it paints nothing.
func (m Model) autocompleteHeight() int {
	c, ok := m.autocompleteListContent()
	if !ok {
		return 0
	}
	return m.listHeight(c)
}

// paneRenders counts, per framePane, how many times the frame rendered that pane through its row of
// the pane table ([Model.frameOverlays], the table's one render walk). It exists for the tests that
// pin how often an Update and its View render each open pane (paintcache_test.go) and is read by
// nothing else; it is atomic because panes render from parallel tests, and those tests read it only
// when running alone.
var paneRenders [paneKinds]atomic.Int64

// block is the rendered block of one pane of the frame, "" when that pane is not on it. It is what
// lets the slot's order be WALKED (stackTranscriptSlot, model.go) instead of re-listed field by field
// at every rectangle in it.
func (o frameOverlays) block(p framePane) string { return o.panes[p] }
