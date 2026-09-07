package tui

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/undo"
)

// ----------------------------------------------------------------------------
// /undo and /redo — routing the revert verbs (command.go owns their grammar)
// ----------------------------------------------------------------------------
//
// The commands are the human end of the session's undo journal (ADR 0051, widened to whole-tree
// snapshots by ADR 0074): the engine records what every Exchange wrote, and these are the only
// surfaces that put any of it back — or put it back again. Two steps, no modal — a preview note
// that discloses the journal's recorded absolute path for every file the revert would touch, and a
// `confirm` that executes exactly the step the human just read. The generation stamped on that
// preview travels back with the confirmation, so a journal that moved in between earns a fresh
// preview instead of a revert nobody authorised (ratified call 7).
//
// The disclosure is the authorization surface, which is why the preview never abbreviates a path
// and never hides a skip: a file the human edited since the agent wrote it is left alone, and the
// note says which and why, because a silent skip reads exactly like a revert that worked.
//
// `/redo` is `/undo`'s mirror in every respect — same grammar, same two steps, same stamp, its own
// stack — and the two are written as mirrors rather than folded into one parameterised routine,
// because the pair the human reads about is two commands over two stacks and the shared half is
// the listing, which lives in internal/undo where every Driver reaches it.
//
// Routing is synchronous and idle-only (commandrun.go says why — these verbs mutate the
// workspace), and the notes below are built from that shared listing so the TUI words only the
// verb and the line that applies it.

// undoMovedLead and redoMovedLead head the re-preview a stale confirmation earns: the journal moved
// between the preview the human read and the confirmation they gave, so nothing was put back and
// the fresh preview under this line is the step that is actually on offer now.
const (
	undoMovedLead = "the journal moved since that preview — nothing was undone"
	redoMovedLead = "the journal moved since that preview — nothing was redone"
)

// undoConfirmHint and redoConfirmHint close every preview with the line that executes it. Each
// names the whole grammar, because a preview the human cannot act on from what they are reading is
// a preview they will retype the wrong way.
const (
	undoConfirmHint = "  /undo confirm applies this; anything else leaves the files alone"
	redoConfirmHint = "  /redo confirm applies this; anything else leaves the files alone"
)

// redoNothingNote answers both empty redo cases — a bare /redo with an empty stack and a
// confirmation that found one. It names what fills that stack, because "nothing to redo" is
// otherwise indistinguishable from a redo the engine lost: the stack holds exactly what `/undo`
// took away, and the next exchange that writes clears it (ADR 0074 decision 6).
const redoNothingNote = "nothing to redo — /undo has put nothing back since the last exchange that wrote files"

// runUndo routes a parsed /undo line from the idle state. The bare form previews the top exchange
// group and stashes the generation that preview quoted; `confirm` hands that stamp back to the
// engine, which reverts the step or refuses it as stale. It never launches a worker, so it always
// returns a nil Cmd.
func (m Model) runUndo(action undoAction) (tea.Model, tea.Cmd) {
	if action == undoConfirm {
		return m.confirmUndo()
	}
	return m.previewUndo("")
}

// runRedo routes a parsed /redo line, /undo's mirror over the redo stack: bare previews what the
// last `/undo confirm` took away, `confirm` puts it back. Synchronous and worker-free like runUndo.
func (m Model) runRedo(action undoAction) (tea.Model, tea.Cmd) {
	if action == undoConfirm {
		return m.confirmRedo()
	}
	return m.previewRedo("")
}

// previewUndo reads the top un-undone group off the engine, stashes its generation as the stamp a
// following confirmation must quote, and records the note describing it. lead is the line printed
// above that note when the preview is a RE-preview (a stale confirmation), and empty for the
// ordinary bare form.
//
// Nothing is stashed when there is nothing to undo: a preview that describes no step authorises no
// revert, and leaving the old stamp standing would let a confirmation typed after it slip through.
func (m Model) previewUndo(lead string) (tea.Model, tea.Cmd) {
	step, ok := m.eng.UndoPreview()
	note := undoNothingNote(m.eng.UndoNote())
	if ok {
		m.undoGeneration = step.Generation
		note = undoPreviewNote(step)
	} else {
		m.undoGeneration = 0
	}
	return m.noteRevert(lead, note)
}

// previewRedo reads the top group off the redo stack under previewUndo's rule, stamp for stamp: a
// preview that describes no step leaves no stamp behind for a later confirmation to spend.
func (m Model) previewRedo(lead string) (tea.Model, tea.Cmd) {
	step, ok := m.eng.RedoPreview()
	note := redoNothingNote
	if ok {
		m.redoGeneration = step.Generation
		note = redoPreviewNote(step)
	} else {
		m.redoGeneration = 0
	}
	return m.noteRevert(lead, note)
}

// confirmUndo executes the previewed step, quoting the stashed generation as proof of which step
// was read. The three refusals each get their own answer: an empty journal says so plainly, a
// journal that moved re-previews rather than reverting something else, and any other failure is
// reported verbatim — never swallowed, because a confirmation that appears to do nothing is
// indistinguishable from one that reverted nothing.
//
// A spent stamp is dropped on success: the group it named is popped, so a second confirmation must
// go through a fresh preview like the first one did.
func (m Model) confirmUndo() (tea.Model, tea.Cmd) {
	report, err := m.eng.UndoRevert(m.undoGeneration)
	switch {
	case errors.Is(err, undo.ErrStaleGeneration):
		return m.previewUndo(undoMovedLead)
	case errors.Is(err, undo.ErrNothingToUndo):
		m.undoGeneration = 0
		return m.noteRevert("", undoNothingNote(m.eng.UndoNote()))
	case err != nil:
		return m.noteRevert("", "undo failed: "+err.Error())
	default:
		m.undoGeneration = 0
		return m.noteRevert("", undoReportNote(report))
	}
}

// confirmRedo re-applies the previewed step, confirmUndo's mirror down to the four answers: an
// empty stack says so, a moved journal re-previews rather than putting back something else, any
// other failure is reported verbatim, and a spent stamp is dropped on success.
func (m Model) confirmRedo() (tea.Model, tea.Cmd) {
	report, err := m.eng.RedoRevert(m.redoGeneration)
	switch {
	case errors.Is(err, undo.ErrStaleGeneration):
		return m.previewRedo(redoMovedLead)
	case errors.Is(err, undo.ErrNothingToRedo):
		m.redoGeneration = 0
		return m.noteRevert("", redoNothingNote)
	case err != nil:
		return m.noteRevert("", "redo failed: "+err.Error())
	default:
		m.redoGeneration = 0
		return m.noteRevert("", redoReportNote(report))
	}
}

// noteRevert records one revert note in the transcript, under lead when the note is a re-preview a
// stale confirmation earned, and re-lays the frame. It is the single exit both verbs take, so a
// path that answers without saying anything is a path that does not compile.
func (m Model) noteRevert(lead, note string) (tea.Model, tea.Cmd) {
	if lead != "" {
		note = lead + "\n" + note
	}
	m.transcript.addNote(note)
	m.layout()
	return m, nil
}

// ----------------------------------------------------------------------------
// The rendered notes (pure)
// ----------------------------------------------------------------------------

// undoPreviewNote and redoPreviewNote render a preview: which exchange is on top of the stack, what
// the step would do to each recorded path, and the line that executes it.
func undoPreviewNote(step undo.Step) string {
	return revertNote("/undo", undo.PreviewLines(step), undoConfirmHint)
}

func redoPreviewNote(step undo.Step) string {
	return revertNote("/redo", undo.PreviewLines(step), redoConfirmHint)
}

// undoReportNote and redoReportNote render what a revert actually did: the counts, then every path
// it left alone with the reason. No hint follows — the step is spent.
func undoReportNote(report undo.Report) string {
	return revertNote("undone", undo.ReportLines(report), "")
}

func redoReportNote(report undo.Report) string {
	return revertNote("redone", undo.ReportLines(report), "")
}

// revertNote turns the Driver-neutral listing internal/undo renders into one transcript note: the
// verb heads its first line, the rest of the listing follows, and hint — when there is one — closes
// it. The verb is the TUI's half precisely because the listing is not: `apogee undo` shows the same
// rows under a different command (ADR 0074).
func revertNote(verb string, lines []string, hint string) string {
	noted := make([]string, 0, len(lines)+1)
	for i, line := range lines {
		if i == 0 {
			line = verb + " — " + line
		}
		noted = append(noted, line)
	}
	if hint != "" {
		noted = append(noted, hint)
	}
	return strings.Join(noted, "\n")
}

// undoNothingNote answers both empty cases — a bare /undo with no group on the journal and a
// confirmation that found none. reason is the engine's [Engine.UndoNote]: it states why undo covers
// what it covers when snapshots are not in force, because a journal that only ever saw this
// process's funnel writes is a narrower answer than a snapshot-backed one and must not read as a
// broken one (ADR 0074 decision 2).
func undoNothingNote(reason string) string {
	return strings.Join(undo.NothingLines(reason), "\n")
}
