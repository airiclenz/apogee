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
