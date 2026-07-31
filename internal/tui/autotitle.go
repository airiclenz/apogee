package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/title"
)

// ----------------------------------------------------------------------------
// Automatic session naming (ADR 0022 addendum, 2026-07-31)
// ----------------------------------------------------------------------------
//
// A new Session record is named from the first thing the human asked, by one small out-of-band
// completion fired at that prompt's submit — in PARALLEL with the Exchange the prompt starts. On a
// single-slot server it therefore queues behind Turn 1 and answers between Turns 1 and 2, which is
// the cheapest KV-eviction point in the whole session: the context is at its smallest there.
//
// The call is COSMETIC. It is not a Turn and not a Mechanism: it fires at no Hook point, never
// shapes the primary call, never reaches the Engine (whose single-goroutine contract it would
// otherwise break — ADR 0011), emits no Token/Usage event, enters no transcript entry, and moves no
// gauge. Nothing in the conversation depends on it, which is exactly why every failure path here is
// silent: the heuristic title (sessionTitle) that the first Save already stamped simply stands, and
// a maintenance nicety must never nag.
//
// The renderer owns only WHEN it fires and whether the answer is applied. The call itself lives
// behind [Options.GenerateTitle], which the composition root backs with its own provider.Client over
// the server and model this session is bound to at the moment of the call.
//
// Three plain fields on the Model carry the state (see the Model's own declaration):
// autoTitleFired latches "this Session record has had its one call", titleTouched records that a
// HUMAN named the session (which drops a late-landing automatic title), and pendingTitle stashes a
// title that resolved before the first Save minted an id to rename.

// autoTitleMsg carries the AUTOMATIC naming call's result back to the Update loop. title is the
// model's raw reply (title.Sanitize turns it into a title, or reports that nothing usable came
// back) and err is whatever the seam reported. Both are folded silently: this Msg can only ever
// improve a title that already exists.
type autoTitleMsg struct {
	title string
	err   error
}

// Compile-time assertion that the naming Msg is a valid tea.Msg (mirroring messages.go).
var _ tea.Msg = autoTitleMsg{}

// maybeAutoTitle fires the automatic naming call for the prompt that just went out, returning the
// Cmd to batch alongside the Exchange (nil when nothing should fire). It latches autoTitleFired, so
// one Session record earns exactly one call however many prompts follow.
//
// Four things must hold. The config toggle must be on (`auto-title:`), the seam must be wired,
// there must be a SessionHost — with no persistence there is no Session record to name, so the call
// would buy nothing and still cost a queue slot — and this record must not have fired already. A
// resumed session starts latched (replayResumed, resumeLoaded), which is what keeps a record that
// already has a name from being renamed behind the human's back.
func (m *Model) maybeAutoTitle(firstUserText string) tea.Cmd {
	if m.autoTitleFired || !m.opts.AutoTitle || m.opts.GenerateTitle == nil || m.sessions == nil {
		return nil
	}
	m.autoTitleFired = true
	return m.autoTitleCmd(firstUserText)
}

// autoTitleCmd builds the Cmd that runs one naming call off the Update loop and reports it as an
// autoTitleMsg. It captures the seam and the program context by value, so the closure holds no
// pointer into the value-copied Model (the listSessions posture). The context is the PROGRAM's, not
// an Exchange's: the naming call outlives the Turn it was fired beside — that is the whole point of
// firing it in parallel — so only a shutdown should cut it short. Everything else about the
// deadline is the composition root's (it sets the client's queue-tolerant request timeout).
func (m Model) autoTitleCmd(firstUserText string) tea.Cmd {
	generate := m.opts.GenerateTitle
	parent := m.parent
	return func() tea.Msg {
		raw, err := generate(parent, firstUserText)
		return autoTitleMsg{title: raw, err: err}
	}
}

// foldAutoTitle folds the automatic naming call's result. It is silent in every direction that is
// not a usable title: a failed call, a reply nothing survives sanitizing, or a session the human has
// since named themselves all leave the heuristic title standing (Ratified design 9). Only the
// automatic path answers to titleTouched — a bare `/rename` is an explicit request and applies
// regardless.
func (m *Model) foldAutoTitle(msg autoTitleMsg) tea.Cmd {
	if msg.err != nil || m.titleTouched {
		return nil
	}
	name, ok := title.Sanitize(msg.title)
	if !ok {
		return nil
	}
	return m.applyTitle(name)
}

// applyTitle routes a resolved title at the Session record it names. Rename is the only correct
// writer of a stored title — SessionHost.Save fixes the title at create and ignores the argument
// thereafter (wire.go), so the heuristic stamped by the first Save can only ever be replaced this
// way. Before that first Save there is no id to rename, so the title is stashed and flushed at the
// save-complete that mints one (flushPendingTitle).
func (m *Model) applyTitle(name string) tea.Cmd {
	if m.sessions == nil {
		return nil
	}
	id := m.sessions.ActiveID()
	if id == "" {
		m.pendingTitle = name
		return nil
	}
	return m.setSessionTitle(id, name)
}

// flushPendingTitle applies a stashed title once a Save has put the record on disk, clearing the
// stash. It is called from the save-complete fold on a SUCCESSFUL save: the rename reads and
// rewrites a stored record, so a title applied before the file exists would simply be dropped by
// the store. A save that failed leaves the stash for the next one.
func (m *Model) flushPendingTitle() tea.Cmd {
	if m.pendingTitle == "" || m.sessions == nil {
		return nil
	}
	id := m.sessions.ActiveID()
	if id == "" {
		return nil
	}
	name := m.pendingTitle
	m.pendingTitle = ""
	return m.setSessionTitle(id, name)
}
