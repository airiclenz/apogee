package tui

import (
	tea "charm.land/bubbletea/v2"
)

// ----------------------------------------------------------------------------
// Prompt recall — the per-workspace list of sent inputs the box walks with ↑/↓
// ----------------------------------------------------------------------------
//
// The name is deliberate: this is prompt RECALL, not history. The /sessions browser owns the word
// "history" (CONTEXT.md), and the two are different things — one lists saved conversations, this
// one hands back the exact text the human typed into this workspace's box.
//
// The split across the seam is [SessionHost]'s: internal/recall owns the file (one JSONL file per
// workspace under the config home), cmd/apogee owns which directory and which workspace, and this
// package owns only WHEN — one load at start-up, one append per input sent, both off the Update
// loop. An unwired [Options.Recall] is the whole of the degrade: no load Cmd, no entries, no
// appends.

// promptRecall is the prompt box's recall state, carried by [promptEditor]. Its zero value is
// "nothing recalled and nothing to recall", which is what an unwired host leaves it at forever.
//
// entries is a slice inside the value-copied Model (ADR 0011), so it is REPLACED wholesale and
// never written through — every copy of the Model shares the backing array, and an in-place write
// would be seen by copies that have no business seeing it.
type promptRecall struct {
	// entries are this workspace's recorded inputs, oldest→newest — the order internal/recall
	// returns and the order the walk indexes into.
	entries []string

	// pos is where the walk stands: the index into entries of the text the box currently holds. It
	// is meaningful only while active, and is zeroed with it.
	pos int

	// active reports that the box holds a freshly recalled entry the human has taken no other
	// action in — the ONE state where ↑/↓ still belong to recall rather than to the caret
	// (ratified decision 1). Every other action drops it: handleKey's chokepoint clears it on any
	// keypress but the arrows recall itself claims, and the non-key edit sites (paste, resize, the
	// ask's borrow of the box, a click in it) clear it beside the drag-selection.
	active bool
}

// dropRecall ends recall mode WITHOUT touching the box: the walk stops owning ↑/↓ and whatever text
// stands stays exactly where it is, now an ordinary draft. The entries are untouched — they are the
// workspace's record, not the mode — so the next empty-box ↑ starts a fresh walk from the newest.
func (e *promptEditor) dropRecall() {
	e.recall.active, e.recall.pos = false, 0
}

// recallLoadedMsg carries the start-up LoadPrompts result back to the Update loop. It is a plain
// report and never an error case: a host that could not read its file yields no entries, and the
// box simply has nothing to recall (recall is a convenience — a session never fails over one).
type recallLoadedMsg struct {
	entries []string
}

// Compile-time assertion that the recall Msg is a valid tea.Msg (mirroring messages.go).
var _ tea.Msg = recallLoadedMsg{}

// loadRecallCmd builds the Cmd that reads the workspace's recorded prompts off the Update loop and
// reports them as a recallLoadedMsg. It captures the host by value so the closure holds no pointer
// into the value-copied Model (listSessions' posture). An unwired host returns a nil Cmd, so an
// unwired TUI issues no read at all.
//
// The error is dropped HERE rather than carried into the fold, because there is nothing the fold
// could do with it that differs from an empty file: recall is silent when it has nothing, and a
// start-up note about an unreadable recall file would be noise in front of the human's first prompt.
func (m Model) loadRecallCmd() tea.Cmd {
	host := m.opts.Recall
	if host == nil {
		return nil
	}
	return func() tea.Msg {
		entries, err := host.LoadPrompts()
		if err != nil {
			return recallLoadedMsg{}
		}
		return recallLoadedMsg{entries: entries}
	}
}

// appendRecallCmd builds the fire-and-forget Cmd that records one sent input. It runs off the
// Update loop (it writes a file) and reports nothing: the send has already happened, so a failed
// record costs the human one Up-arrow entry and nothing else — swallowing the error is what keeps
// the renderer wire-silent and the conversation uninterrupted (ADR 0031). An unwired host or empty
// text returns a nil Cmd, so nothing is scheduled that would do nothing.
func (m Model) appendRecallCmd(text string) tea.Cmd {
	host := m.opts.Recall
	if host == nil || text == "" {
		return nil
	}
	return func() tea.Msg {
		_ = host.AppendPrompt(text)
		return nil
	}
}

// foldRecallLoaded installs the loaded entries as the box's recall state. It REPLACES the slice
// rather than appending into it (ADR 0011), and it is total: an empty or failed load leaves the
// state empty, which is indistinguishable from a workspace that has never sent anything.
func (m *Model) foldRecallLoaded(msg recallLoadedMsg) {
	m.recall.entries = msg.entries
}

// recallKey is what ↑ and ↓ mean in the prompt box, and the only place recall mode is entered. It
// reports whether recall claimed the key; anything it does not claim falls through to the textarea's
// own cursor duty, which is the whole of the "arrows are the caret's again" rule.
//
// prev is the walk as it stood BEFORE handleKey's chokepoint deactivated it. The chokepoint clears
// recall on every keypress precisely so that no key can be forgotten here; the two keys that ARE
// recall read their position out of that stash instead, the same posture the selection-delete
// carve-out takes with the drag-selection span (model.go).
//
// Three rules, one per branch (ratified decision 1): ↑ on an EMPTY box opens the walk at the newest
// entry; ↑ while the walk is live steps older and stops at the oldest (a walk that has hit the
// bottom still owns the key — releasing it there would put a cursor move where the human expects
// nothing to happen); ↓ steps newer and, one past the newest, empties the box and hands the arrows
// back. ↓ with no walk live is never recall's: it is the caret's, exactly as ↑ is on a typed draft.
//
// Only the two states where the box is the human's own (idle, and running where ⏎ queues an
// interjection) are in scope. stateAwaitingAsk deliberately is not: there ↑/↓ move the choice
// highlight, and that guard runs earlier in handleKey.
func (m Model) recallKey(msg tea.KeyPressMsg, prev promptRecall) (Model, bool) {
	if m.state != stateIdle && m.state != stateRunning {
		return m, false
	}
	switch msg.String() {
	case "up":
		if prev.active {
			return m.showRecall(prev.pos - 1), true
		}
		if m.input.Value() == "" && len(prev.entries) > 0 {
			return m.showRecall(len(prev.entries) - 1), true
		}
	case "down":
		if !prev.active {
			return m, false
		}
		if prev.pos+1 >= len(prev.entries) {
			return m.recallPastNewest(), true
		}
		return m.showRecall(prev.pos + 1), true
	}
	return m, false
}

// showRecall puts entry i in the box and marks the walk live there. The index is clamped, so the
// callers may hand it one step past either end and let the clamp be the "stops at the oldest" rule.
//
// The caret goes to the END of what was loaded — the human's next act is almost always to edit the
// tail of the recalled line, and it is where the caret would stand had they just typed it. The
// autocomplete overlay is DISMISSED rather than re-derived, which is the one deliberate asymmetry
// with every other value change in this package: a recalled "/command" would otherwise pop the menu,
// and that menu claims ↑/↓ before recall ever sees them (handleKey) — the walk would be stolen by
// its own first entry. Nothing re-opens it until the human acts, and any action they take ends
// recall first, so the overlay comes back exactly when the arrows do.
func (m Model) showRecall(i int) Model {
	if len(m.recall.entries) == 0 {
		return m
	}
	i = clampInt(i, 0, len(m.recall.entries)-1)
	m.input.SetValue(m.recall.entries[i])
	m.input.MoveToEnd()
	m.recall.pos, m.recall.active = i, true
	m.dismissAutocomplete()
	m.layout() // the box grows or shrinks around the recalled text, which may be many rows
	return m
}

// recallPastNewest is ↓ off the end of the walk: the box returns to EMPTY and the arrows go back to
// the caret. That is what makes the walk reversible — the human can always get back to the blank box
// they started from without deleting the recalled text by hand — and it mirrors what a terminal's
// own history does at the newest entry.
func (m Model) recallPastNewest() Model {
	m.input.Reset()
	m.dropRecall()
	m.dismissAutocomplete()
	m.layout() // the emptied box shrinks back
	return m
}

// recordSend records one input the human sent: into the store off the Update loop (the fire-and-
// forget Cmd) and into the in-memory entries, so ↑ immediately after sending recalls it without
// waiting for a reload. Every send path calls it — a message, a whole-input /command, an
// interjection — and the ask-answer path deliberately does not (ratified decision 3).
//
// The entries slice is REPLACED, never appended into (ADR 0011): the Model this returns is a copy
// that will be folded back, and an in-place append would be seen through the shared backing array by
// copies that have no business seeing it. The newest entry goes at the END, because entries run
// oldest→newest — the order the store returns and the order the walk indexes into — so the ↑ that
// follows opens on exactly the line just sent.
//
// The consecutive-duplicate skip mirrors the store's own (internal/recall), which keeps the
// in-memory view identical to what a reload would hand back: re-sending a recalled prompt records
// again on both sides and grows neither. An unwired host records nothing at all, in memory or on
// disk — a nil [Options.Recall] is the whole feature off.
func (m Model) recordSend(text string) (Model, tea.Cmd) {
	if m.opts.Recall == nil || text == "" {
		return m, nil
	}
	if n := len(m.recall.entries); n == 0 || m.recall.entries[n-1] != text {
		next := make([]string, 0, n+1)
		next = append(next, m.recall.entries...)
		m.recall.entries = append(next, text)
	}
	return m, m.appendRecallCmd(text)
}
