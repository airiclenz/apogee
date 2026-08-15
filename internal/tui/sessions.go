package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/session"
)

// ----------------------------------------------------------------------------
// The /sessions history-browser overlay (session-system plan §7)
// ----------------------------------------------------------------------------
//
// A modal list of saved sessions the human opens with /sessions: browse, resume, rename, or
// delete. It renders as a bordered popup pane above the input through the shared popup module
// (renderPopup) and, while open, claims every keypress (handleKey routes to sessionBrowserKey
// first). All persistence I/O
// (List/Load/Delete/Rename) runs off the Update loop through a tea.Cmd against the [SessionHost],
// so a slow disk never stalls the UI; the results fold back as sessionListMsg / sessionLoadedMsg.
// The engine restore itself happens on the Update loop at idle, where the Model owns the Engine.

// sessionBrowser is the overlay's inline state on the Model (its zero value is "closed", so it is
// safe in the value-copied Model, like autocompleteState — ADR 0011). metas is the full list from
// List() (newest first); the rendered rows are metas filtered to the current workspace unless
// allWorkspaces is set, and then pruned by what the human has typed. selected indexes the FILTERED
// rows (browserView). confirming arms a "delete? y/n" confirm on the selected row; renaming arms an
// inline title edit whose in-progress text is renameBuf.
type sessionBrowser struct {
	open          bool
	metas         []session.Meta
	allWorkspaces bool
	selected      int
	confirming    bool
	renaming      bool
	renameBuf     string
	// filter is what the human has typed into the open browser: the case-insensitive substring every
	// row must carry to survive (browserView), composed after the workspace view rather than beside
	// it. A plain string, so the value-copied Model stays copyable (ADR 0011) — no strings.Builder can
	// ever live here — and part of the overlay's own state, so the whole-struct zeroing every close
	// already does (`m.sessionBrowser = sessionBrowser{}`) is what clears it: no path can carry a
	// stale filter into the next open. A re-LIST is deliberately not such a path — a delete or a
	// rename refreshes the pane the human is still standing in, and it stays as they left it.
	filter string
}

// maxSessionRows caps how many session rows the overlay shows at once; a longer list scrolls a
// window around the selection (popupRowWindow) so the pane never crowds the transcript off a
// short terminal.
const maxSessionRows = 8

// sessionBrowserHint is the one-line key legend shown at the foot of the overlay. It LEADS with
// "type to filter" for the picker's own reason (pickerHint): there is no activation key to name, so
// the letters announce themselves nowhere else the way "↑/↓" and "esc" do — and it is the legend
// that has to say the three verbs are chords now, because the letters they used to be are what the
// filter is typed with (ratified 2026-08-06).
const sessionBrowserHint = "type to filter · ↑/↓ select · ⏎ resume · ^r rename · ^d delete · ^a this/all · esc close"

// deleteConfirmCell is the inline "delete? y/n" an armed delete puts on the selected row (sessionRows)
// — a CELL of its own past the message counts rather than a suffix glued to the last one, so arming
// the confirm cannot stretch the count column, and the column it sits in collapses away entirely
// while nothing is armed.
const deleteConfirmCell = "delete? y/n"

// scheduleTagGlyph leads the tag a row carries when its record is one Firing of a Schedule
// (ADR 0033): a circling arrow, for the standing instruction that will run again. It is one
// terminal cell wide in EITHER width method — it carries no variation selector, so the two
// measures agree about it (ADR 0030) — which is what keeps a labelled row costing the title
// column exactly what it looks like it costs, whatever measure the painter is on.
const scheduleTagGlyph = "⟳"

// interruptedNote is the transcript note appended when a resumed session was interrupted
// mid-task (the engine reports InExchange after the restore). It tells the human how to pick the
// work back up; the step-only /continue drive that actually resumes it is item 8's work.
const interruptedNote = "this session was interrupted mid-task — /continue picks up where it left off; sending a new message discards the unfinished work"

// sessionListMsg carries the result of an off-loop Sessions.List() back to the Update loop: the
// metas to render (newest first) or the error that aborts the open (foldSessionList).
type sessionListMsg struct {
	metas []session.Meta
	err   error
}

// sessionLoadedMsg carries the result of an off-loop Sessions.Load(id) back to the Update loop:
// the record to restore into the live engine or the error that aborts the resume (resumeLoaded).
type sessionLoadedMsg struct {
	rec session.Record
	err error
}

// Compile-time assertions that the browser Msgs are valid tea.Msgs (mirroring messages.go).
var (
	_ tea.Msg = sessionListMsg{}
	_ tea.Msg = sessionLoadedMsg{}
)

// openSessionBrowser starts the /sessions overlay: it dispatches Sessions.List() off the Update
// loop (sessionListMsg opens the pane when it returns). Without a wired host there is nothing to
// browse, so it notes "no saved sessions" and opens nothing — the same degrade as an empty store.
func (m Model) openSessionBrowser() (tea.Model, tea.Cmd) {
	if m.sessions == nil {
		m.transcript.addNote("no saved sessions")
		m.layout()
		return m, nil
	}
	return m, m.listSessions()
}

// listSessions builds the Cmd that reads Sessions.List() off the Update loop and reports it as a
// sessionListMsg. It captures the host by value so the closure holds no pointer into the
// value-copied Model.
func (m Model) listSessions() tea.Cmd {
	sessions := m.sessions
	return func() tea.Msg {
		metas, err := sessions.List()
		return sessionListMsg{metas: metas, err: err}
	}
}

// foldSessionList folds a List() result: an error or a completely empty store notes the outcome
// and opens no overlay; otherwise the browser opens (or refreshes, after a delete/rename) over the
// metas with the selection clamped into range. The empty-store note fires on the raw List result,
// not the workspace-filtered view — a store with sessions only in OTHER workspaces still opens so
// the human can widen with `a`.
func (m *Model) foldSessionList(msg sessionListMsg) tea.Cmd {
	if msg.err != nil {
		m.sessionBrowser = sessionBrowser{}
		m.transcript.addNote("could not list sessions: " + msg.err.Error())
		m.layout()
		return nil
	}
	if len(msg.metas) == 0 {
		m.sessionBrowser = sessionBrowser{}
		m.transcript.addNote("no saved sessions")
		m.layout()
		return nil
	}
	m.sessionBrowser.metas = msg.metas
	m.sessionBrowser.open = true
	m.sessionBrowser.clampSelection(len(m.sessionBrowserView().metas))
	return nil
}

// visible returns the metas the current view shows: all of them when allWorkspaces is set, else
// only those recorded against the current workspace. Legacy workspace-less records (Workspace "")
// never match a real workspace root, so they appear only under the all-workspaces view — exactly
// as the plan specifies.
func (b sessionBrowser) visible(workspace string) []session.Meta {
	if b.allWorkspaces {
		return b.metas
	}
	out := make([]session.Meta, 0, len(b.metas))
	for _, meta := range b.metas {
		if meta.Workspace == workspace {
			out = append(out, meta)
		}
	}
	return out
}

// browserView is the overlay's FILTERED view of the sessions it lists, and the ONE seam its
// consumers share: the rows the pane paints, how many of them there are, and which record ⏎, ^r and
// ^d act on. Deriving them once is what makes it impossible for a verb to reach a record the pane
// did not paint — metas[i] is the record rows[i] describes — so a highlight is resolved against the
// same list the painter used rather than against the unfiltered store (the pickerView posture,
// picker.go).
type browserView struct {
	rows  []popupRow     // the surviving rows, in the store's own newest-first order
	metas []session.Meta // metas[i] is the record rows[i] states
}

// filteredView is the rows this browser shows RIGHT NOW: the workspace view (visible) pruned by the
// overlay's own filter, composed in that order — the workspace scope decides which records exist for
// this pane at all and the filter narrows what is left, so ^a widens the very list the typed text
// narrows. The match is the picker's (rowMatchesFilter): a case-insensitive substring of the row's
// display cells joined with one space, every cell participating.
//
// It is derived per frame and per keypress rather than captured at open, the picker's own posture:
// the store can be re-listed under the open pane (a delete, a rename) and the relative times in the
// cells move on their own.
func (b sessionBrowser) filteredView(workspace string, now time.Time) browserView {
	visible := b.visible(workspace)
	rows := make([]popupRow, 0, len(visible))
	for _, meta := range visible {
		rows = append(rows, sessionRowCells(meta, workspace, b.allWorkspaces, now))
	}
	pruned := filterPopupRows(rows, b.filter)
	view := browserView{rows: pruned.rows, metas: make([]session.Meta, 0, len(pruned.offering))}
	for _, i := range pruned.offering {
		view.metas = append(view.metas, visible[i])
	}
	return view
}

// sessionBrowserView is that view as of now — the one read every key route and the painter share, so
// a verb and the pane can never disagree about which record the highlight names.
func (m Model) sessionBrowserView() browserView {
	return m.sessionBrowser.filteredView(m.opts.Workspace, time.Now())
}

// clampSelection keeps selected within the filtered row range after the list or the view changed
// (a toggle, a delete, a keystroke that narrowed the filter). n is the count of the FILTERED view —
// what the pane actually paints — so the highlight can never point past the last row on the screen.
// An empty view pins the selection at zero.
func (b *sessionBrowser) clampSelection(n int) {
	switch {
	case n == 0:
		b.selected = 0
	case b.selected >= n:
		b.selected = n - 1
	case b.selected < 0:
		b.selected = 0
	}
}

// sessionBrowserKey routes a keypress while the overlay is open (idle only). A live rename edit or
// delete confirm claims the keys first (they are modes within the modal); otherwise ↑/↓ move the
// highlight, ⏎ resumes, esc closes, the three browse verbs are CHORDS (^r rename, ^d delete, ^a
// this/all), and everything PRINTABLE types — the key's runes extend the filter that prunes the rows
// (browserView), with backspace as its undo. It always fully consumes the key — the browser is modal.
//
// The verbs are chords for the filter's sake (ratified 2026-08-06): a modal list where any letter
// might be a verb is a list no letter can be typed into, and a session store is exactly the place a
// human wants to type a name. `d` is the reason it had to be all three at once — a letter that
// deletes is the one that must never be reachable by typing. The delete-confirm's y/n and the whole
// rename edit are untouched: they are modal surfaces of their own, and no filter is typed inside them.
//
// The count is re-derived and the selection re-clamped on every key — the rows underneath can have
// changed since the last one — and again after a key that MOVED the filter, because the rows it
// leaves standing can be fewer than the highlight was pointing at.
func (m Model) sessionBrowserKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.sessionBrowser.renaming {
		return m.sessionRenameKey(msg)
	}
	if m.sessionBrowser.confirming {
		return m.sessionConfirmKey(msg)
	}
	view := m.sessionBrowserView()
	n := len(view.metas)
	m.sessionBrowser.clampSelection(n)
	switch msg.String() {
	case "esc":
		m.sessionBrowser = sessionBrowser{}
		m.layout()
		return m, nil
	case "up", "ctrl+p":
		if n > 0 {
			m.sessionBrowser.selected = (m.sessionBrowser.selected - 1 + n) % n
		}
		return m, nil
	case "down", "ctrl+n":
		if n > 0 {
			m.sessionBrowser.selected = (m.sessionBrowser.selected + 1) % n
		}
		return m, nil
	case "ctrl+a":
		// Toggle the current-workspace ⇄ all-workspaces view; the row set changes, so re-anchor
		// the selection at the top rather than leave it pointing at a now-hidden row. The filter
		// stands: it is what the human is looking FOR, and the toggle changes where they are looking.
		m.sessionBrowser.allWorkspaces = !m.sessionBrowser.allWorkspaces
		m.sessionBrowser.selected = 0
		return m, nil
	case "ctrl+d":
		if n > 0 {
			m.sessionBrowser.confirming = true // arm the inline "delete? y/n" on the selected row
		}
		return m, nil
	case "ctrl+r":
		if n > 0 {
			m.sessionBrowser.renaming = true
			// The seed is a stored title, so it is escape-stripped on the way INTO the buffer: the
			// rename row paints the buffer verbatim, and the commit below strips anyway — seeding it
			// clean keeps what is being edited equal to what will be saved.
			m.sessionBrowser.renameBuf = stripEscapes(view.metas[m.sessionBrowser.selected].Title)
		}
		return m, nil
	case "enter":
		if n == 0 {
			return m, nil
		}
		id := view.metas[m.sessionBrowser.selected].ID
		m.sessionBrowser = sessionBrowser{} // close; the resume runs when the record loads (sessionLoadedMsg)
		m.layout()
		return m, m.loadSession(id)
	case "backspace":
		// By RUNE rather than by byte: the filter is the human's own text, and half a multi-byte
		// character is not a state any list can be filtered by.
		if runes := []rune(m.sessionBrowser.filter); len(runes) > 0 {
			m.sessionBrowser.filter = string(runes[:len(runes)-1])
			m.sessionBrowser.clampSelection(len(m.sessionBrowserView().metas))
		}
		return m, nil
	}
	// Text carries the key's rune(s) only for PRINTABLE input — a modifier chord carries none
	// (bubbletea's own contract) — so a chord that is not one of the verbs above is still swallowed
	// whole by the modal rather than typed into the filter.
	if msg.Text != "" {
		m.sessionBrowser.filter += msg.Text
		m.sessionBrowser.clampSelection(len(m.sessionBrowserView().metas))
		return m, nil
	}
	return m, nil // any other key is swallowed by the modal
}

// sessionConfirmKey resolves an armed delete confirm: y deletes the selected session, n or esc
// cancels back to the row. Deleting the ACTIVE session first rotates the host so the live
// conversation keeps saving under a fresh id, and notes that its old file is gone (the
// conversation itself lives on in memory).
//
// That rotate is QUEUED ahead of the delete rather than run on the spot (audit 2026-08-01 follow-up).
// Run synchronously it overtook whatever the queue was holding: a save already in flight or waiting
// reached an already-inactive host and minted a SECOND record for the live conversation — the same
// duplicate /clear used to file — and the delete then removed the wrong one of the two. Queued, the
// order the human asked for is the order the host sees: everything already pending, then the
// retirement, then the removal.
func (m Model) sessionConfirmKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		m.sessionBrowser.confirming = false
		// Through the FILTERED view, like every other verb: the highlight the confirm was armed on
		// indexes the rows the pane painted, and resolving it against the unfiltered list would delete
		// a session the human never saw (browserView).
		view := m.sessionBrowserView()
		if len(view.metas) == 0 {
			return m, nil
		}
		id := view.metas[m.sessionBrowser.selected].ID
		if m.sessions != nil && m.sessions.ActiveID() == id {
			// queueWrite, not scheduleWrite: the delete below pumps, so the two verbs leave this fold
			// as ONE dispatched Cmd (a fold may never batch two record writes — model.go).
			m.queueWrite(recordWrite{kind: writeRotate})
			m.transcript.addNote("current session's file deleted — it lives on in memory; the next turn saves it as a new session")
			m.refreshViewport()
		}
		cmd := m.deleteSession(id) // queued: it mutates m, so it is sequenced before the return
		return m, cmd
	case "n", "esc":
		m.sessionBrowser.confirming = false
		return m, nil
	}
	return m, nil // any other key leaves the confirm armed
}

// sessionRenameKey drives the inline rename edit on the selected row: printable text and backspace
// edit the buffer, enter commits through Sessions.Rename (an empty title is a no-op), esc cancels.
// A commit re-lists so the browser repaints with the new title.
func (m Model) sessionRenameKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.sessionBrowser.renaming = false
		m.sessionBrowser.renameBuf = ""
		return m, nil
	case "enter":
		// The filtered view again (browserView): the edit was opened on a painted row, so the commit
		// lands on the record that row named rather than on the same index into the whole store.
		view := m.sessionBrowserView()
		title := stripEscapes(strings.TrimSpace(m.sessionBrowser.renameBuf))
		m.sessionBrowser.renaming = false
		m.sessionBrowser.renameBuf = ""
		if len(view.metas) == 0 || title == "" {
			return m, nil
		}
		// A human just named a session, so a naming call still in flight must not overwrite what
		// they typed when it lands (autotitle.go, Ratified design 5). The flag is set on the
		// COMMIT, not on arming the edit: an abandoned rename changed nothing.
		m.titleTouched = true
		id := view.metas[m.sessionBrowser.selected].ID
		// The browser renames ANY row, so only a rename of the live session renames the frame with
		// it — naming a stored session from the browser must leave the frame naming this one.
		if m.sessions != nil && m.sessions.ActiveID() == id {
			m.nameSession(title)
		}
		cmd := m.renameSession(id, title) // queued: it mutates m
		return m, cmd
	case "backspace":
		r := []rune(m.sessionBrowser.renameBuf)
		if len(r) > 0 {
			m.sessionBrowser.renameBuf = string(r[:len(r)-1])
		}
		return m, nil
	}
	if msg.Text != "" { // a printable keypress carries its rune(s) in Text
		m.sessionBrowser.renameBuf += msg.Text
	}
	return m, nil
}

// deleteSession queues the removal of id and the re-list that follows it, so the browser refreshes
// without the deleted row. It goes through the record-write queue (model.go) rather than straight
// onto a Cmd goroutine: a delete racing the per-Turn save can otherwise remove a record the
// already-dispatched save then re-creates. The delete is best-effort — a failure just leaves the
// row on the re-list.
func (m *Model) deleteSession(id string) tea.Cmd {
	return m.scheduleWrite(recordWrite{kind: writeDelete, id: id, relist: true})
}

// renameSession queues the browser's rename of id and the re-list that follows it, so the browser
// refreshes with the new title. Same queue and the same reason as deleteSession: Store.Rename
// re-writes the whole record, so one running beside a save reverts a Turn.
func (m *Model) renameSession(id, title string) tea.Cmd {
	return m.scheduleWrite(recordWrite{kind: writeRename, id: id, title: title, relist: true})
}

// setSessionTitle is renameSession's QUIET twin: it queues id's title and reports nothing back to
// the human. It is the apply path for a title the model generated (autotitle.go), where the re-list
// would be actively wrong — foldSessionList opens the overlay over every list it folds, so reusing
// renameSession would make the /sessions browser appear unbidden partway through the first answer.
// Nothing is displayed either way: a title surfaces in the browser the next time it is opened, and
// in the resume note, and nowhere else.
//
// It is the one write whose FAILURE is not simply swallowed (retryTitle): the title is put back on
// the stash and applied at the next successful save, because the alternative — the behaviour this
// replaced — is that a title answering before the first record reaches disk is silently discarded.
// src rides along because the never-clobber rule is re-checked when the stash is remade.
func (m *Model) setSessionTitle(id, title string, src titleSource) tea.Cmd {
	return m.scheduleWrite(recordWrite{kind: writeRename, id: id, title: title, retryTitle: true, source: src})
}

// loadSession builds the Cmd that reads Sessions.Load(id) off the Update loop and reports it as a
// sessionLoadedMsg. Load does not activate the record — resumeLoaded switches the active session
// only once the live restore has succeeded, so a failed restore keeps saving the outgoing session.
func (m Model) loadSession(id string) tea.Cmd {
	sessions := m.sessions
	return func() tea.Msg {
		rec, err := sessions.Load(id)
		return sessionLoadedMsg{rec: rec, err: err}
	}
}

// resumeLoaded restores a loaded record into the live engine and repaints its scrollback. On a
// restore error the view AND the host's active session are left untouched (the locked "a fresh view
// must never lie about the engine" rule) and the failure is noted — because Load did not activate,
// the outgoing conversation keeps saving to its own file. Only on success does it QUEUE the
// activation of the loaded session (redirecting future saves, once everything already queued against
// the outgoing record has landed) and reset the view like startNewSession — reseed the start-up box, repaint the stored scrollback (a decode failure or a
// legacy empty blob degrades to an honest no-scrollback note), relight the gauge from the stored
// fill, and re-arm the same field set startNewSession resets. A session restored mid-task gets the
// interrupted note (item 8 supplies the step-only /continue drive that finishes it).
//
// The resume notices — "resumed: <title>", its no-scrollback degrade variant, and the interrupted
// note — are ephemeral (addEphemeralNote): they are re-derived from the loaded record every time it
// is opened, so the record itself must not carry them. The load/restore FAILURE notes above stay
// persistent: they belong to the session that stays live, and they record something that happened
// rather than something re-derived.
//
// The title those notices quote is untrusted disk input — no codec sanitizes a record's Meta on the
// way back in, which is why sessionRowCells strips it too — and it needs no wrapping here: both
// addNote and addEphemeralNote escape-strip at the seam.
func (m *Model) resumeLoaded(msg sessionLoadedMsg) tea.Cmd {
	if msg.err != nil {
		m.transcript.addNote("could not load session: " + msg.err.Error())
		m.refreshViewport()
		return nil
	}
	if err := m.eng.RestoreSession(msg.rec.Session); err != nil {
		m.transcript.addNote("could not restore session: " + err.Error())
		m.refreshViewport()
		return nil
	}
	// The restore succeeded, so it is now safe to redirect saves at the loaded session's file
	// (Load deliberately left the active session untouched — see resumeLoaded's doc). A failed
	// RestoreSession above returns before this, leaving the outgoing conversation's file active.
	//
	// The redirect is QUEUED, not applied here (audit 2026-08-01 follow-up). Applied on the Update
	// goroutine it jumped every write the queue was already holding, so the OUTGOING conversation's
	// coalesced save then went out against the record just resumed — the loaded session's transcript
	// and engine state overwritten by the conversation the human was leaving. Behind the queue the
	// outgoing save lands in its own file first, and only then does the loaded record become the one
	// saves resolve against. Nothing else in this fold waits on it: the view below repaints from the
	// record already in hand, not from the host.
	cmd := m.scheduleWrite(recordWrite{kind: writeActivate, meta: msg.rec.Meta})
	// Saves now target a record that already HAS a name, so the automatic naming call is latched off
	// for the rest of this session exactly as it is for a --resume start (replayResumed), and a title
	// still waiting for an id is dropped: it was stashed for the session this restore just replaced.
	m.autoTitleFired = true
	m.pendingTitle = ""
	title := msg.rec.Meta.Title
	m.nameSession(title) // the frame follows the restore, as it follows every other naming route
	m.transcript.reset()
	m.transcript.addStartup(newStartupView(m.opts))
	entries, decErr := decodeTranscript(msg.rec.Transcript)
	if decErr != nil || len(entries) == 0 {
		m.transcript.addEphemeralNote("resumed: " + title + " (no scrollback recorded — the model still remembers)")
	} else {
		m.transcript.replay(entries)
		m.transcript.addEphemeralNote("resumed: " + title)
	}
	if m.eng.InExchange() {
		m.transcript.addEphemeralNote(interruptedNote)
	}
	// A successful restore starts a new session, so the engine re-read the workspace context files
	// (the resolved-live posture: a resumed session speaks from the CURRENT files, not the ones its
	// snapshot was taken under) — the notice says what it is now carrying.
	m.noteContextFiles()
	m.ctxUsed = msg.rec.Meta.CtxUsed          // relight the gauge near the resumed session's last fill
	m.usage = usageTotals(msg.rec.Meta.Usage) // …and reopen its accounting where the record left it
	m.tokPerSec = 0
	m.genStart = time.Time{}
	m.detached = false // re-arm follow-the-tail: the resumed view opens at its tail like a launch
	m.flash = ""
	m.layout()
	return cmd // the queued Activate, when this fold's schedule found the queue idle
}

// ----------------------------------------------------------------------------
// Rendering
// ----------------------------------------------------------------------------

// renderSessionBrowser paints the /sessions overlay through the shared popup module (renderPopup):
// a titled, bordered pane spanning the full window width (m.width, flush with the input box below)
// holding the session rows and a key legend, the selected row highlighted. Row composition —
// which facts each row states, in which cells, including the inline delete-confirm or rename-edit
// decoration — stays caller-side in sessionRows, while the module owns the marker, highlight,
// column alignment, truncation, and scroll windowing. An empty view is a single unselectable note
// row, which has nothing to align and so stays one cell. It returns "" when the browser is closed,
// so View treats it like the approval-prompt slot.
//
// While a filter is being typed the pane grows one line for it, set off by a blank line at each end —
// the picker's own line, budget and trade (renderPicker): the three lines are the module's BODY
// block, both blanks are the body's own pads, and the whole claim comes off the top of the frame's
// grant so a short window gives up ROWS before it gives up the line the human is typing.
func (m Model) renderSessionBrowser() string {
	b := m.sessionBrowser
	if !b.open {
		return ""
	}
	scope := "this workspace"
	if b.allWorkspaces {
		scope = "all workspaces"
	}
	filter := overlayFilterLine(b.filter)
	spec := popupSpec{
		title:        "saved sessions  (" + scope + ")",
		body:         filter,
		bodyLead:     pickerFilterLead,
		bodyPadAbove: filter != "",
		bodyPadBelow: filter != "",
		hint:         sessionBrowserHint,
		selected:     -1, // no rows ⇒ no highlight (the popup module's own convention)
		scrollbar:    m.popupScrollbarOn(),
	}
	if len(b.visible(m.opts.Workspace)) == 0 {
		// An empty WORKSPACE view is a fact about the store, and a row of prose is how the pane states
		// it. A filter that matched nothing is not the same thing and gets no such row: the visible
		// filter line over an empty list already says why the pane is empty, and backspace is the way
		// back (the picker's zero-match pane).
		spec.rows = singleCellRows([]string{"no sessions in this workspace — press ^a to see all"})
	} else {
		spec.rows = sessionRows(b, m.opts.Workspace, time.Now())
		if len(spec.rows) > 0 {
			spec.selected = clampInt(b.selected, 0, len(spec.rows)-1)
		}
	}
	claim := popupFloor{}
	if filter != "" {
		claim.body = popupBodyLineCount(m.th, filter, m.width) + popupBodyPadLines(true, true)
	}
	// The row window is the SCREEN's to grant, not the browser's to assume: maxSessionRows is this
	// overlay's own taste, and popupBudget cuts it down to what the window can seat above the input
	// box (D2) — on a short terminal that is fewer rows, or none at all. A window of none is the one
	// case the pane has to speak up about: the module counts the dropped entries onto the title row
	// (popupTitleLine), because a browser showing no sessions at all would otherwise be
	// indistinguishable from a workspace that has none.
	maxBody, maxRows, seated := m.popupBudget(paneBrowser, len(spec.rows), maxSessionRows, popupChrome, claim)
	if !seated {
		return "" // the frame cannot seat this pane beside its siblings (frameRowPlan)
	}
	spec.maxBodyRows = maxBody
	spec.maxRows = maxRows
	return renderPopup(m.th, spec, m.width)
}

// sessionRows composes the row list the popup module paints: the filtered view's rows (browserView
// — the workspace scope narrowed by what the human has typed), newest first. On the selected row an
// armed rename replaces the whole row with a single cell holding the edit buffer — an edit is prose
// being typed, not a session being described, so it has no columns to keep — and an armed delete adds
// the confirm as a fourth cell; every other row is its plain cells. The module adds the marker, the
// highlight, the column padding, and the truncation.
//
// The decoration is applied AFTER the filter and so takes no part in it: "delete? y/n" is the pane
// answering a keypress rather than a fact about the session, and a row must not survive a filter on
// the strength of being armed.
func sessionRows(b sessionBrowser, workspace string, now time.Time) []popupRow {
	view := b.filteredView(workspace, now)
	rows := make([]popupRow, 0, len(view.rows))
	for i, row := range view.rows {
		switch {
		case i == b.selected && b.renaming:
			row = popupRow{"rename: " + b.renameBuf + "▏"}
		case i == b.selected && b.confirming:
			row = append(row, deleteConfirmCell)
		}
		rows = append(rows, row)
	}
	return rows
}

// sessionRowCells is one row's three cells — ["title", "· relative time", "· N msgs"] — rather than
// one concatenated label, so the times start at one column down the pane and the counts at another,
// whatever the titles beside them measure and however the list scrolls. Each separator leads the
// cell it introduces, so the "·" glyphs line up as well as the words after them.
//
// In the all-workspaces view a foreign session's workspace base joins the TITLE cell instead of
// claiming a tier of its own: it says WHICH "fix the parser" this is, so it belongs with the title
// it qualifies, and a fourth column carrying it would push the two facts every row states out of
// line behind an optional one only some rows fill.
//
// A record one Firing of a Schedule wrote (Meta.ScheduleName set — ADR 0033) carries that
// Schedule's name as a tag in the SAME title cell, for the same reason and at the same price: it
// says which standing instruction produced this run, so it belongs with the title it qualifies
// rather than in a tier of its own past the two facts every row states. It is worth stating even
// though a Firing is saved under a "<schedule> — <HH:MM>" title, because a title is the one fact of
// a record the human can rewrite (the r verb) and the tag is what survives that. Nothing else about
// the row moves: a Firing orders, resumes, renames and deletes like any other record, and a record
// with no schedule identity renders exactly as it did before there were Schedules.
//
// Every fact the Meta supplies — the title, the workspace path behind its base, and a Firing's
// schedule name — is escape-stripped here, exactly as the pickers strip every cell they build from
// launcher text (launchProfileRows). A Meta is untrusted DISK input: List() reads session files
// that no codec has sanitized (transcriptcodec strips the record's transcript on the way back in,
// never its Meta), so a title carrying "\x1bc" would otherwise reach the pane as a live RIS
// terminal reset — the popup module strips nothing and truncates ANSI-preservingly — and would also
// lie to the column math, since an ESC byte occupies no display cell but does occupy the string.
func sessionRowCells(meta session.Meta, currentWorkspace string, all bool, now time.Time) popupRow {
	title := stripEscapes(meta.Title)
	if all && meta.Workspace != currentWorkspace {
		title += " · " + stripEscapes(workspaceBase(meta.Workspace))
	}
	if meta.ScheduleName != "" {
		title += " · " + scheduleTagGlyph + " " + stripEscapes(meta.ScheduleName)
	}
	return popupRow{
		title,
		"· " + relativeTime(meta.UpdatedAt, now),
		"· " + msgsLabel(meta.UserMsgs),
	}
}

// msgsLabel renders the user-message count, singularising "1 msg".
func msgsLabel(n int) string {
	if n == 1 {
		return "1 msg"
	}
	return fmt.Sprintf("%d msgs", n)
}

// workspaceBase is the short name of a session's workspace root for a foreign-workspace row; a
// legacy record with no recorded workspace reads "unknown workspace" rather than filepath.Base's
// "." for an empty path.
func workspaceBase(ws string) string {
	if ws == "" {
		return "unknown workspace"
	}
	return filepath.Base(ws)
}

// relativeTime renders how long ago t was, coarsely: "just now", "5m ago", "3h ago", "2d ago",
// "4w ago". A zero time (a legacy record whose mtime could not be read) reads "unknown". A
// timestamp AHEAD of the wall clock — clock skew, an NTP step back, a restored snapshot — clamps
// to zero and reads "just now" by decision rather than by accident, mirroring formatElapsed's own
// clamp, so no rearrangement of the arms below can turn a negative duration into garbage.
func relativeTime(t, now time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d/(24*time.Hour)))
	default:
		return fmt.Sprintf("%dw ago", int(d/(7*24*time.Hour)))
	}
}
