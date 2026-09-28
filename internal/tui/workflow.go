package tui

import (
	"fmt"
	"maps"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// Background workflows in the TUI (ADR 0089). A background workflow runs beside the conversation,
// on goroutines the engine owns, and the TUI keeps it apart from the conversation by two rules.
//
// Its events stay out of the conversation's view. Every WorkflowPhaseEvent it emits is marked
// Background, and every event its item runs emit carries the synthetic call id
// domain.BackgroundWorkflowCallPrefix + its id — or, for a run one of those spawned in turn, a run
// id this file recorded off the spawning call. Those events fold into the workflow's own state
// (backgroundWorkflows) and the Inspector's rings, and never reach the transcript, the activity
// board or the stall clock: the transcript is the conversation's record, the board is what a stop
// key stops (so esc never reaches a background run), and the stall clock times the Exchange in
// front of the human, not work beside it.
//
// Its end reaches the conversation. The end phase — finished, stopped or failed — writes one finish
// line into the transcript, and the engine holds a note for the model (ADR 0089 D3) that one of
// three roads delivers: the running Exchange's worker commits it at the next between-Steps boundary
// (deliverWorkflowNotes, worker.go); an idle session is woken on it (wakeIfIdle), which opens an
// Exchange through Engine.Wake; or, under `workflow-wake: off`, it rides the human's next message.
// The wake's transcript row is a depth-0 prompt (entryUser) like a typed one, so it is a fork
// point and the prompt a cancelled first Turn marks aborted (transcript.forkPoints, markAborted) —
// the engine opened an Exchange on it, and the two counts must agree.

// The words the finish line, the wake's prompt row and the two failure notes are built from.
const (
	backgroundFinishFormat = "background workflow %s %s — items %d · ok %d · partial %d · blocked %d"
	backgroundFailedFormat = "background workflow %s failed — %s"
	wakePromptText         = "(background workflow report)"
	wakeFailedPrefix       = "could not wake the agent on a background workflow's report: "
	workflowNoteLostPrefix = "a background workflow's report did not reach the model: "
)

// The receipt statuses a finish line counts (domain.WorkflowReceipt.Status).
const (
	receiptOK      = "ok"
	receiptPartial = "partial"
	receiptBlocked = "blocked"
)

// backgroundWorkflows is the session's live background workflows as their events fold: each one's
// view by workflow id, and the run id of every run a background run spawned, mapped to its
// workflow. Both maps are replaced on write, never mutated in place, because the Model is copied by
// value on every Update and an earlier copy must not see a later fold. The zero value is empty.
type backgroundWorkflows struct {
	live map[string]backgroundWorkflow
	runs map[string]string
}

// backgroundWorkflow is one live background workflow: its name and its items so far, counted by the
// status their receipts ended on.
type backgroundWorkflow struct {
	name    string
	ok      int
	partial int
	blocked int
}

// workflowNoteLostMsg reports that the worker's drain took a finish note and the engine refused to
// commit it (deliverWorkflowNotes). The note cannot be put back, so the fold says so.
type workflowNoteLostMsg struct{ err error }

// owns reports whether e belongs to a background workflow: one of its phases, or an event of a run
// it spawned.
func (b backgroundWorkflows) owns(e domain.Event) bool {
	if phase, ok := e.(domain.WorkflowPhaseEvent); ok {
		return phase.Background
	}
	_, ok := b.workflowOf(e.Identity())
	return ok
}

// workflowOf names the live background workflow the run that stamped base belongs to: an item run
// by the synthetic call id it was spawned under, a run one of those spawned by the run id recorded
// off its spawning call. The top-level agent (depth 0) belongs to none.
func (b backgroundWorkflows) workflowOf(base domain.EventBase) (string, bool) {
	if base.Depth == 0 {
		return "", false
	}
	if base.RunID != "" {
		if id, ok := b.runs[base.RunID]; ok {
			return id, true
		}
	}
	id, ok := strings.CutPrefix(base.CallID, domain.BackgroundWorkflowCallPrefix)
	if !ok {
		return "", false
	}
	_, live := b.live[id]
	return id, live
}

// withWorkflow returns b with id's view replaced by view.
func (b backgroundWorkflows) withWorkflow(id string, view backgroundWorkflow) backgroundWorkflows {
	live := maps.Clone(b.live)
	if live == nil {
		live = make(map[string]backgroundWorkflow)
	}
	live[id] = view
	b.live = live
	return b
}

// withRun returns b with runID recorded as a run of workflow id.
func (b backgroundWorkflows) withRun(runID, id string) backgroundWorkflows {
	runs := maps.Clone(b.runs)
	if runs == nil {
		runs = make(map[string]string)
	}
	runs[runID] = id
	b.runs = runs
	return b
}

// without returns b with workflow id and every run recorded for it gone.
func (b backgroundWorkflows) without(id string) backgroundWorkflows {
	live := maps.Clone(b.live)
	delete(live, id)
	runs := maps.Clone(b.runs)
	maps.DeleteFunc(runs, func(_, workflow string) bool { return workflow == id })
	b.live, b.runs = live, runs
	return b
}

// view is id's live view, named from e when no started phase was folded for it.
func (b backgroundWorkflows) view(id, name string) backgroundWorkflow {
	view, ok := b.live[id]
	if !ok {
		view.name = name
	}
	return view
}

// count adds one finished item's receipt status to the tally.
func (v backgroundWorkflow) count(status string) backgroundWorkflow {
	switch status {
	case receiptOK:
		v.ok++
	case receiptPartial:
		v.partial++
	case receiptBlocked:
		v.blocked++
	}
	return v
}

// finishLine is the transcript line an end phase writes: how the workflow ended and its items by
// status, or the cause of a failure.
func (v backgroundWorkflow) finishLine(e domain.WorkflowPhaseEvent) string {
	if e.Phase == domain.WorkflowFailed {
		return fmt.Sprintf(backgroundFailedFormat, v.name, e.Detail)
	}
	total := v.ok + v.partial + v.blocked
	return fmt.Sprintf(backgroundFinishFormat, v.name, e.Phase, total, v.ok, v.partial, v.blocked)
}

// foldBackgroundEvent folds one event owns claimed: into the Inspector's rings, which record every
// wire exchange whatever run it belongs to, and into the workflow's own state — its phases, and the
// run a background run's delegation spawns, recorded so that run's events are claimed too.
func (m Model) foldBackgroundEvent(e domain.Event) Model {
	m = m.foldWire(e)
	m = m.foldAttempt(e)
	switch e := e.(type) {
	case domain.WorkflowPhaseEvent:
		return m.foldBackgroundPhase(e)
	case domain.ToolCallEvent:
		if id, ok := m.workflows.workflowOf(e.EventBase); ok && e.SpawnRunID != "" {
			m.workflows = m.workflows.withRun(e.SpawnRunID, id)
		}
	}
	return m
}

// foldBackgroundPhase folds one phase of a background workflow: a start opens its view, a finished
// item counts, and an end drops the view, writes the finish line and asks for a wake (wakePending).
// A stage start and a waiting question change nothing here.
func (m Model) foldBackgroundPhase(e domain.WorkflowPhaseEvent) Model {
	switch e.Phase {
	case domain.WorkflowStarted:
		m.workflows = m.workflows.withWorkflow(e.Workflow, backgroundWorkflow{name: e.Name})
	case domain.WorkflowItemFinished:
		view := m.workflows.view(e.Workflow, e.Name).count(e.Receipt.Status)
		m.workflows = m.workflows.withWorkflow(e.Workflow, view)
	case domain.WorkflowFinished, domain.WorkflowStopped, domain.WorkflowFailed:
		view := m.workflows.view(e.Workflow, e.Name)
		m.workflows = m.workflows.without(e.Workflow)
		m.transcript.addNote(view.finishLine(e))
		m.wakePending = true
	}
	return m
}

// wakeAfterFold is the Update tail's wake half: when a background workflow has ended since the last
// wake was tried, it tries one now (wakeIfIdle) and batches the worker it launches behind the fold's
// own Cmd. Anything but the Model, or a Model with no wake pending, passes through untouched.
func wakeAfterFold(next tea.Model, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	m, ok := next.(Model)
	if !ok || !m.wakePending {
		return next, cmd
	}
	m, wake := m.wakeIfIdle()
	if wake == nil {
		return m, cmd
	}
	return m, tea.Batch(cmd, wake)
}

// canWake reports whether the engine is the Update loop's to wake right now: idle, bound, not
// quitting, and with no idle-only operation in flight — a /sessions load (its restore takes the
// engine), a /bg launch (it reads the Agent off the loop), a queued record write or fork (it
// snapshots the engine at idle), a held message (the human's next ⏎ sends it, and the note rides
// with it), or a modal pane the human is answering.
func (m Model) canWake() bool {
	switch {
	case m.state != stateIdle, m.prebound(), m.quitting, m.sessionLoading, m.bgLaunching:
		return false
	case m.writeBusy, len(m.pendingWrites) > 0, len(m.pendingInterjections) > 0:
		return false
	}
	return !m.modalPaneOpen()
}

// modalPaneOpen reports whether a pane that owns the keyboard is up (paneSpecs' modal column).
func (m Model) modalPaneOpen() bool {
	for p := framePane(0); p < paneKinds; p++ {
		if paneSpecs[p].modal && paneSpecs[p].open(m) {
			return true
		}
	}
	return false
}

// wakeIfIdle tries the wake a background workflow's end asked for, once, when canWake allows it;
// until then the request is held and the next fold asks again. Engine.Wake queues the held finish
// notes as a new Exchange's opening message and reports whether it did — it declines, leaving them
// held, under `workflow-wake: off` (they ride the next message) or when a running Exchange's drain
// already took them — and on a wake the Exchange is launched exactly as a typed prompt's is: the
// tail followed, a depth-0 prompt row, a worker with a mailbox. The boundary a wake's progress saves
// pair with is the one before Wake queued its opening, as a typed prompt's is the one before its
// Submit, so a record saved mid-wake never carries the queued opening as pending input.
func (m Model) wakeIfIdle() (Model, tea.Cmd) {
	if !m.canWake() {
		return m, nil
	}
	m.wakePending = false
	boundary, boundaryErr := m.eng.Snapshot()
	woke, err := m.eng.Wake(m.parent)
	if err != nil {
		m.transcript.addNote(wakeFailedPrefix + err.Error())
		return m, nil
	}
	if !woke {
		return m, nil
	}
	m.detached = false
	m.transcript.addUser(wakePromptText, nil)
	box := newInterjectBox()
	cmd, cancel := startWake(m.parent, m.eng, box, m.notify, m.flushEvents)
	batch := m.enterRunning(cmd, cancel, box, actThinking)
	if boundaryErr == nil {
		m.cacheBoundary(boundary)
	}
	return m, batch
}
