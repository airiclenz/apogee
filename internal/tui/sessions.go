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
// delete. It renders like the approval/ask prompts — a boxed pane above the input — and, while
// open, claims every keypress (handleKey routes to sessionBrowserKey first). All persistence I/O
// (List/Load/Delete/Rename) runs off the Update loop through a tea.Cmd against the [SessionHost],
// so a slow disk never stalls the UI; the results fold back as sessionListMsg / sessionLoadedMsg.
// The engine restore itself happens on the Update loop at idle, where the Model owns the Engine.

// sessionBrowser is the overlay's inline state on the Model (its zero value is "closed", so it is
// safe in the value-copied Model, like autocompleteState — ADR 0011). metas is the full list from
// List() (newest first); the rendered rows are metas filtered to the current workspace unless
// allWorkspaces is set. selected indexes the FILTERED rows. confirming arms a "delete? y/n"
// confirm on the selected row; renaming arms an inline title edit whose in-progress text is
// renameBuf.
type sessionBrowser struct {
	open          bool
	metas         []session.Meta
	allWorkspaces bool
	selected      int
	confirming    bool
	renaming      bool
	renameBuf     string
}

// maxSessionRows caps how many session rows the overlay shows at once; a longer list scrolls a
// window around the selection (popupRowWindow) so the pane never crowds the transcript off a
// short terminal.
const maxSessionRows = 8

// sessionBrowserHint is the one-line key legend shown at the foot of the overlay.
const sessionBrowserHint = "↑/↓ select · ⏎ resume · r rename · d delete · a this/all · esc close"

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
	m.sessionBrowser.clampSelection(m.opts.Workspace)
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

// clampSelection keeps selected within the filtered row range after the list or the view changed
// (a toggle, a delete). An empty view pins the selection at zero.
func (b *sessionBrowser) clampSelection(workspace string) {
	n := len(b.visible(workspace))
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
// delete confirm claims the keys first (they are modes within the modal); otherwise the keys are
// the browse verbs. It always fully consumes the key — the browser is modal.
func (m Model) sessionBrowserKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.sessionBrowser.renaming {
		return m.sessionRenameKey(msg)
	}
	if m.sessionBrowser.confirming {
		return m.sessionConfirmKey(msg)
	}
	visible := m.sessionBrowser.visible(m.opts.Workspace)
	n := len(visible)
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
	case "a":
		// Toggle the current-workspace ⇄ all-workspaces view; the row set changes, so re-anchor
		// the selection at the top rather than leave it pointing at a now-hidden row.
		m.sessionBrowser.allWorkspaces = !m.sessionBrowser.allWorkspaces
		m.sessionBrowser.selected = 0
		return m, nil
	case "d":
		if n > 0 {
			m.sessionBrowser.confirming = true // arm the inline "delete? y/n" on the selected row
		}
		return m, nil
	case "r":
		if n > 0 {
			m.sessionBrowser.renaming = true
			m.sessionBrowser.renameBuf = visible[m.sessionBrowser.selected].Title
		}
		return m, nil
	case "enter":
		if n == 0 {
			return m, nil
		}
		id := visible[m.sessionBrowser.selected].ID
		m.sessionBrowser = sessionBrowser{} // close; the resume runs when the record loads (sessionLoadedMsg)
		m.layout()
		return m, m.loadSession(id)
	}
	return m, nil // any other key is swallowed by the modal
}

// sessionConfirmKey resolves an armed delete confirm: y deletes the selected session, n or esc
// cancels back to the row. Deleting the ACTIVE session first rotates the host so the live
// conversation keeps saving under a fresh id, and notes that its old file is gone (the
// conversation itself lives on in memory).
func (m Model) sessionConfirmKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		m.sessionBrowser.confirming = false
		visible := m.sessionBrowser.visible(m.opts.Workspace)
		if len(visible) == 0 {
			return m, nil
		}
		id := visible[m.sessionBrowser.selected].ID
		if m.sessions != nil && m.sessions.ActiveID() == id {
			m.sessions.Rotate()
			m.transcript.addNote("current session's file deleted — it lives on in memory; the next turn saves it as a new session")
			m.refreshViewport()
		}
		return m, m.deleteSession(id)
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
		visible := m.sessionBrowser.visible(m.opts.Workspace)
		title := stripEscapes(strings.TrimSpace(m.sessionBrowser.renameBuf))
		m.sessionBrowser.renaming = false
		m.sessionBrowser.renameBuf = ""
		if len(visible) == 0 || title == "" {
			return m, nil
		}
		return m, m.renameSession(visible[m.sessionBrowser.selected].ID, title)
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

// deleteSession builds the Cmd that removes id off the Update loop and re-lists, so the browser
// refreshes without the deleted row.
func (m Model) deleteSession(id string) tea.Cmd {
	sessions := m.sessions
	return func() tea.Msg {
		_ = sessions.Delete(id) // best-effort: a delete failure just leaves the row on the re-list
		metas, err := sessions.List()
		return sessionListMsg{metas: metas, err: err}
	}
}

// renameSession builds the Cmd that sets id's title off the Update loop and re-lists, so the
// browser refreshes with the new title.
func (m Model) renameSession(id, title string) tea.Cmd {
	sessions := m.sessions
	return func() tea.Msg {
		_ = sessions.Rename(id, title) // best-effort: a rename failure leaves the old title on the re-list
		metas, err := sessions.List()
		return sessionListMsg{metas: metas, err: err}
	}
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
// the outgoing conversation keeps saving to its own file. Only on success does it activate the
// loaded session (redirecting future saves) and reset the view like startNewSession — reseed the start-up box, repaint the stored scrollback (a decode failure or a
// legacy empty blob degrades to an honest no-scrollback note), relight the gauge from the stored
// fill, and re-arm the same field set startNewSession resets. A session restored mid-task gets the
// interrupted note (item 8 supplies the step-only /continue drive that finishes it).
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
	if m.sessions != nil {
		m.sessions.Activate(msg.rec.Meta)
	}
	title := msg.rec.Meta.Title
	m.transcript.reset()
	m.transcript.addStartup(newStartupView(m.opts))
	entries, decErr := decodeTranscript(msg.rec.Transcript)
	if decErr != nil || len(entries) == 0 {
		m.transcript.addNote("resumed: " + title + " (no scrollback recorded — the model still remembers)")
	} else {
		m.transcript.replay(entries)
		m.transcript.addNote("resumed: " + title)
	}
	if m.eng.InExchange() {
		m.transcript.addNote(interruptedNote)
	}
	m.ctxUsed = msg.rec.Meta.CtxUsed // relight the gauge near the resumed session's last fill
	m.tokPerSec = 0
	m.genStart = time.Time{}
	m.pendingSkills = nil  // the staged chips belonged to the conversation being left behind
	m.userScrolled = false // re-arm sticky-to-top: the resumed view pins its first prompt like a launch
	m.flash = ""
	m.layout()
	return nil
}

// ----------------------------------------------------------------------------
// Rendering
// ----------------------------------------------------------------------------

// renderSessionBrowser paints the /sessions overlay through the shared popup module (renderPopup):
// a titled, bordered pane spanning the chat-area width (transcriptWidth, the startup card's right
// edge) holding the session rows and a key legend, the selected row highlighted. Row composition —
// including the inline delete-confirm or rename-edit decoration — stays caller-side in sessionRows,
// while the module owns the marker, highlight, truncation, and scroll windowing. An empty view is a
// single unselectable note row. It returns "" when the browser is closed, so View treats it like
// the approval-prompt slot.
func (m Model) renderSessionBrowser() string {
	b := m.sessionBrowser
	if !b.open {
		return ""
	}
	scope := "this workspace"
	if b.allWorkspaces {
		scope = "all workspaces"
	}
	spec := popupSpec{
		title:   "saved sessions  (" + scope + ")",
		hint:    sessionBrowserHint,
		maxRows: maxSessionRows,
	}
	if len(b.visible(m.opts.Workspace)) == 0 {
		spec.rows = []string{"no sessions in this workspace — press a to see all"}
		spec.selected = -1
	} else {
		spec.rows = sessionRows(b, m.opts.Workspace, time.Now())
		spec.selected = b.selected
	}
	return renderPopup(m.th, spec, m.transcriptWidth())
}

// sessionRows composes the FULL filtered row list the popup module paints: the plain
// sessionRowLabel ("title · relative · N msgs") for every visible session, newest first. On the
// selected row an armed rename replaces the label with the edit buffer and an armed delete appends
// the "delete? y/n" confirm; every other row is its plain label. The module adds the marker,
// highlight, and truncation.
func sessionRows(b sessionBrowser, workspace string, now time.Time) []string {
	visible := b.visible(workspace)
	rows := make([]string, 0, len(visible))
	for i, meta := range visible {
		label := sessionRowLabel(meta, workspace, b.allWorkspaces, now)
		switch {
		case i == b.selected && b.renaming:
			label = "rename: " + b.renameBuf + "▏"
		case i == b.selected && b.confirming:
			label += "   delete? y/n"
		}
		rows = append(rows, label)
	}
	return rows
}

// sessionRowLabel is one row's plain text: "title · relative time · N msgs", with "· <workspace
// base>" appended for a foreign-workspace row in the all-workspaces view (so the human can tell
// which project a session belongs to).
func sessionRowLabel(meta session.Meta, currentWorkspace string, all bool, now time.Time) string {
	label := strings.Join([]string{
		meta.Title,
		relativeTime(meta.UpdatedAt, now),
		msgsLabel(meta.UserMsgs),
	}, " · ")
	if all && meta.Workspace != currentWorkspace {
		label += " · " + workspaceBase(meta.Workspace)
	}
	return label
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
// "4w ago". A zero time (a legacy record whose mtime could not be read) reads "unknown".
func relativeTime(t, now time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := now.Sub(t)
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
