package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/session"
)

// ----------------------------------------------------------------------------
// /fork — branch a new session from one of this session's prompts
// ----------------------------------------------------------------------------
//
// /fork answers one question through the shared picker overlay (picker.go, the /schedule-stop
// shape): WHICH prompt should the new session keep the history through. The rows are the
// transcript's own fork points ([transcript.forkPoints] — every top-level prompt after the last
// fold whose Exchange the engine opened), numbered in transcript order, and ⏎ on one cuts a child
// record off the active session at that prompt and switches the TUI to it.
//
// The cut is two halves made from the same moment: the engine's state with the later Exchanges cut
// off from the END ([Engine.CutSnapshot] with the row's drop count), and the scrollback prefix
// through the chosen Exchange cut from the FRONT ([transcript.prefixThrough]). Both ride the record
// write queue as ONE fork write (sessionsave.go, writeFork) behind the parent's own idle Save —
// never a host call from this command path — so the parent's record is on disk, and its id minted,
// before the child that names it as parent is written (ADR 0031's one-queue rule, sessionsave.go).
//
// The switch happens on the fork write's completion message, not on ⏎: the child record the queue
// assembled is adopted through the /sessions browser's own resume fold (resumeLoaded), so a forked
// session opens exactly as a resumed one does — engine restored, scrollback replayed, saves
// redirected behind the queue, the naming call latched off. The human may send a prompt between ⏎
// and that fold; the record is already written, so the switch is skipped with a note naming the
// child rather than refusing a restore the open Exchange would refuse anyway.

// forkPickerTitle names the overlay: what is being made and what the row chooses.
const forkPickerTitle = "fork this session — keep the history through which prompt"

// forkPickerHint is the legend the fork picker shows — ⏎ forks, because it neither switches the
// session to something that exists nor chooses a setting; it makes a session.
const forkPickerHint = "type to filter · ↑/↓ select · ⏎ fork · esc close"

// The three notes /fork answers with instead of a picker or a switch. The first two are the
// "one honest line, no overlay" degrade every picker verb takes (pickerNote); the third is the
// completion fold's answer when the switch cannot run — the record is written and the browser can
// reach it.
const (
	nothingToForkNote = "nothing to fork yet"
	forkFoldedNote    = "that stretch was folded — fork at a later block"
	// noSessionHostNote is the /sessions posture for a build with no session host
	// (openSessionBrowser): there is no record to cut a child from, so /fork says what /sessions
	// says.
	noSessionHostNote = "no saved sessions"
)

// runFork drives bare /fork. Without a session host there is nothing to write the child to, and
// the queue would drop the fork silently (queueWrite), so it is refused up front with the /sessions
// posture. A session that has not spoken has nothing to fork; one whose every prompt lies before
// the last fold has no State to stand at (forkPoints). Otherwise the picker opens over the
// eligible prompts — the rows are derived per frame like every other kind's, so the offering is
// read again at accept.
func (m Model) runFork() (tea.Model, tea.Cmd) {
	if m.sessions == nil {
		return m.pickerNote(noSessionHostNote)
	}
	if !m.transcript.hasPrompt() {
		return m.pickerNote(nothingToForkNote)
	}
	if len(m.transcript.forkPoints()) == 0 {
		return m.pickerNote(forkFoldedNote)
	}
	m.picker = picker{open: true, kind: pickerFork}
	m.layout()
	return m, nil
}

// forkRows is one row per fork point, in transcript order: its ordinal and the prompt's text on one
// line — whitespace runs collapsed so a multi-line prompt reads as one row, escape-stripped as the
// popup module's contract requires (the text is the human's, but it came through the transcript
// unsanitized), and left for the module to clip to the pane's width.
func forkRows(points []forkPoint) []popupRow {
	rows := make([]popupRow, 0, len(points))
	for i, p := range points {
		text := strings.Join(strings.Fields(stripEscapes(p.text)), " ")
		rows = append(rows, popupRow{fmt.Sprintf("%d.", i+1), text})
	}
	return rows
}

// acceptFork takes the fork picker's highlighted row, named by its index into the fork points
// rather than among the painted rows (acceptPicker resolves the filter first). The points are
// re-derived rather than trusted from the frame that drew them, the acceptScheduleStop posture: the
// scrollback cannot change under an idle picker, but an index naming no point closes the overlay
// and forks nothing rather than indexing past the list.
func (m Model) acceptFork(offered int) (tea.Model, tea.Cmd) {
	points := m.transcript.forkPoints()
	m.picker = picker{}
	m.layout()
	if offered < 0 || offered >= len(points) {
		return m, nil
	}
	return m.forkAt(points[offered])
}

// forkAt cuts the child at point and queues its write behind the parent's idle Save. The engine
// half is cut first, on the Update goroutine that owns the engine at idle (C1): a Snapshot that
// fails forks nothing and says so. Then the parent is saved exactly as /clear saves the outgoing
// session (saveAtIdle), and the fork is queued after it — scheduleWrite dispatches the Save and
// parks the fork behind its latch, so the host sees the parent's record land, and its id minted,
// before it is asked to stamp that id on the child. The child starts under the parent's title as
// the frame wears it (sessionRuleName), which is the title the browser lists it by and the one the
// completion note names as the parent's.
//
// The parent Meta rides along as the host's FALLBACK identity only (SessionHost.Fork): a renderer
// may read ActiveID as "" before the first Save lands, which is exactly why the host stamps
// ParentID from its own identity once that Save has.
func (m Model) forkAt(point forkPoint) (tea.Model, tea.Cmd) {
	sess, err := m.eng.CutSnapshot(point.drop)
	if err != nil {
		return m.pickerNote("could not fork: " + err.Error())
	}
	title := m.sessionRuleName()
	saveCmd := m.saveAtIdle()
	forkCmd := m.scheduleWrite(recordWrite{kind: writeFork, fork: forkPayload{
		sess:    sess,
		entries: entriesToRecords(m.transcript.prefixThrough(point.index)),
		title:   title,
		parent:  session.Meta{ID: m.sessions.ActiveID(), Title: title},
	}})
	return m, tea.Batch(saveCmd, forkCmd)
}

// foldFork folds a landed fork write: the child record the queue assembled becomes the live session
// through the /sessions browser's own resume fold (resumeLoaded, with the record as a
// sessionLoadedMsg), followed by the note that names the parent it was cut from. It runs inside
// foldRecordWrite while the write latch is still held, so the Activate the resume queues waits its
// turn behind it and goes out with the pump that releases the latch — one queue, in order.
//
// The switch is skipped, with the record already written, in the two cases the resume's
// RestoreSession would refuse (agent.go): a worker is running because the human sent a prompt
// between ⏎ and this fold (busy), or the parent is a session resumed mid-task and still inside its
// interrupted Exchange (InExchange — idle to this Model, open to the engine). Both earn the same
// note, naming the child so /sessions can reach it. A restore that fails for any other reason is a
// State the live engine produced a moment ago failing to decode, which cannot happen; resumeLoaded
// notes it if it does.
//
// A fork the host refused is noted rather than swallowed with the other best-effort writes: the
// human asked for it by name and is waiting to see the session change.
func (m *Model) foldFork(msg recordWriteDoneMsg) tea.Cmd {
	if msg.err != nil {
		m.transcript.addNote("could not fork: " + msg.err.Error())
		return nil
	}
	child := msg.fork.child
	if m.busy() || m.eng.InExchange() {
		m.transcript.addNote(fmt.Sprintf("forked as %s — resume it from /sessions", child.ID))
		return nil
	}
	cmd := m.resumeLoaded(sessionLoadedMsg{rec: msg.fork.record})
	m.transcript.addNote(fmt.Sprintf("forked from %s — %s", child.ParentID, msg.write.fork.title))
	return cmd
}
