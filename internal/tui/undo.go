package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/undo"
)

// ----------------------------------------------------------------------------
// /undo — routing the revert verb (command.go owns its grammar)
// ----------------------------------------------------------------------------
//
// The command is the human end of the pre-image journal (ADR 0051): the engine records what every
// funnel write overwrote, and this is the only surface that puts any of it back. Two steps, no
// modal — a preview note that discloses the RESOLVED path of every file the revert would touch,
// and a `/undo confirm` that executes exactly the step the human just read. The generation stamped
// on that preview travels back with the confirmation, so a journal that moved in between earns a
// fresh preview instead of a revert nobody authorised (ratified call 7).
//
// The disclosure is the authorization surface, which is why the preview never abbreviates a path
// and never hides a skip: a file the human edited since the agent wrote it is left alone, and the
// note says which and why, because a silent skip reads exactly like a revert that worked.
//
// Routing is synchronous and idle-only (commandrun.go says why — this verb mutates the workspace),
// and the note builders below are pure so the wording is table-testable without a Model.

// undoMovedLead heads the re-preview a stale confirmation earns: the journal moved between the
// preview the human read and the confirmation they gave, so nothing was reverted and the fresh
// preview under this line is the step that is actually on offer now.
const undoMovedLead = "the journal moved since that preview — nothing was undone"

// undoConfirmHint closes every preview with the line that executes it. It names the whole grammar,
// because a preview the human cannot act on from what they are reading is a preview they will
// retype the wrong way.
const undoConfirmHint = "  /undo confirm applies this; anything else leaves the files alone"

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

// previewUndo reads the top un-undone group off the engine, stashes its generation as the stamp a
// following confirmation must quote, and records the note describing it. lead is the line printed
// above that note when the preview is a RE-preview (a stale confirmation), and empty for the
// ordinary bare form.
//
// Nothing is stashed when there is nothing to undo: a preview that describes no step authorises no
// revert, and leaving the old stamp standing would let a confirmation typed after it slip through.
func (m Model) previewUndo(lead string) (tea.Model, tea.Cmd) {
	step, ok := m.eng.UndoPreview()
	note := undoNothingNote()
	if ok {
		m.undoGeneration = step.Generation
		note = undoPreviewNote(step)
	} else {
		m.undoGeneration = 0
	}
	if lead != "" {
		note = lead + "\n" + note
	}
	m.transcript.addNote(note)
	m.layout()
	return m, nil
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
		m.transcript.addNote(undoNothingNote())
	case err != nil:
		m.transcript.addNote("undo failed: " + err.Error())
	default:
		m.undoGeneration = 0
		m.transcript.addNote(undoReportNote(report))
	}
	m.layout()
	return m, nil
}

// ----------------------------------------------------------------------------
// The rendered notes (pure)
// ----------------------------------------------------------------------------

// undoPreviewNote renders the preview: which exchange is on top of the stack, what reverting it
// would do to each recorded path, and the line that executes it. Every path is listed — restores,
// deletions and skips alike — because this note is what the human authorises the revert from, and
// a summary they cannot check is not a disclosure.
func undoPreviewNote(step undo.Step) string {
	lines := []string{fmt.Sprintf(
		"/undo — exchange %d, the most recent one that wrote files:", step.Ordinal)}
	for _, change := range step.Changes {
		lines = append(lines, undoPathLine(change.Action, change.Path, change.Reason))
	}
	return strings.Join(append(lines, undoConfirmHint), "\n")
}

// undoReportNote renders what a revert actually did: the counts, then every path it left alone
// with the reason. The skips are named individually and the successes are only counted, because a
// skip is the one outcome that leaves the human with work to do — the file still holds what the
// agent wrote, and only the reason says whether that was their own edit or a failure.
func undoReportNote(report undo.Report) string {
	lines := []string{fmt.Sprintf("undone — exchange %d: %d restored, %d removed, %d skipped",
		report.Ordinal, len(report.Restored), len(report.Deleted), len(report.Skipped))}
	for _, skipped := range report.Skipped {
		lines = append(lines, undoPathLine(undo.ActionSkip, skipped.Path, skipped.Reason))
	}
	return strings.Join(lines, "\n")
}

// undoPathLine renders one path row, shared by the preview and the report so the two read as one
// listing: the verb in a fixed column, the resolved path, and — for a skip — the reason after it.
func undoPathLine(action undo.Action, path, reason string) string {
	line := fmt.Sprintf("  %-7s %s", action, path)
	if reason != "" {
		line += " — " + reason
	}
	return line
}

// undoNothingNote answers both empty cases — a bare /undo with no group on the journal and a
// confirmation that found none. It states the journal's lifetime as well as its emptiness, because
// "nothing to undo" on a resumed session is otherwise indistinguishable from a journal that lost
// what it held: the writes of an earlier process were never recorded here at all.
func undoNothingNote() string {
	return "nothing to undo — no agent file writes are recorded for this session\n" +
		"  the undo journal is memory, not storage: it starts empty each run,\n" +
		"  so writes made before this process started cannot be put back"
}
