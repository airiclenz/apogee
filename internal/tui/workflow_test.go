package tui

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// bgWorkflowID and bgWorkflowName are the one background workflow these tests fold.
const (
	bgWorkflowID   = "sweep-1"
	bgWorkflowName = "sweep"
)

// bgFinishLine is the finish line a one-item background workflow that ended ok writes.
const bgFinishLine = "background workflow sweep finished — items 1 · ok 1 · partial 0 · blocked 0"

// bgPhase is one phase of the background workflow.
func bgPhase(phase domain.WorkflowPhase) domain.WorkflowPhaseEvent {
	return domain.WorkflowPhaseEvent{Phase: phase, Workflow: bgWorkflowID, Name: bgWorkflowName, Background: true}
}

// bgItemOK is the background workflow's one item ending on an ok receipt.
func bgItemOK() domain.WorkflowPhaseEvent {
	e := bgPhase(domain.WorkflowItemFinished)
	e.Stage, e.Item, e.Receipt = "sweep", "alpha", domain.WorkflowReceipt{Status: receiptOK, Summary: "fine"}
	return e
}

// bgChildBase is the stamp of an item run of the background workflow.
func bgChildBase(runID string) domain.EventBase {
	return domain.EventBase{Depth: 1, Turn: 1, CallID: domain.BackgroundWorkflowCallPrefix + bgWorkflowID, RunID: runID}
}

// wakingEngine is a fake whose Wake opens an Exchange.
func wakingEngine() *fakeEngine {
	return &fakeEngine{wakeFn: func(context.Context) (bool, error) { return true, nil }}
}

// foldEvents folds each event through Update in order.
func foldEvents(t *testing.T, m Model, events ...domain.Event) Model {
	t.Helper()
	for _, e := range events {
		m = step(t, m, eventMsg{Event: e})
	}
	return m
}

// finishBackground folds the background workflow's whole life: started, one ok item, finished.
func finishBackground(t *testing.T, m Model) Model {
	t.Helper()
	return foldEvents(t, m, bgPhase(domain.WorkflowStarted), bgItemOK(), bgPhase(domain.WorkflowFinished))
}

// lastPrompt is the last depth-0 prompt row of the scrollback.
func lastPrompt(t *testing.T, m Model) entry {
	t.Helper()
	for i := len(m.transcript.entries) - 1; i >= 0; i-- {
		if e := m.transcript.entries[i]; e.kind == entryUser && e.depth == 0 {
			return e
		}
	}
	t.Fatal("the scrollback holds no depth-0 prompt")
	return entry{}
}

func TestBackgroundWorkflow_FinishWhileIdleWakesTheAgent(t *testing.T) {
	t.Parallel()
	eng := wakingEngine()
	m := newTestModelEng(t, eng, testOpts)

	m = finishBackground(t, m)

	if eng.wakes() != 1 {
		t.Fatalf("Wake calls = %d, want 1 — an idle session is woken on the finish", eng.wakes())
	}
	if !slices.Contains(noteTexts(m), bgFinishLine) {
		t.Errorf("notes = %q, want the finish line %q", noteTexts(m), bgFinishLine)
	}
	if m.state != stateRunning || m.worker.box == nil {
		t.Errorf("state = %v, mailbox %v; want a running Exchange with a mailbox", m.state, m.worker.box != nil)
	}
	if prompt := lastPrompt(t, m); prompt.text != wakePromptText {
		t.Errorf("last prompt = %q, want the wake's row %q", prompt.text, wakePromptText)
	}
	if m.wakePending {
		t.Error("the wake is still pending after it was tried")
	}
}

func TestBackgroundWorkflow_WakeDeclinedOpensNothing(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{} // Wake answers false: `workflow-wake: off`, or the note was already taken
	m := newTestModelEng(t, eng, testOpts)

	m = finishBackground(t, m)

	if eng.wakes() != 1 || m.state != stateIdle || m.wakePending {
		t.Errorf("Wake calls %d, state %v, pending %v; want one try, idle, nothing pending", eng.wakes(), m.state, m.wakePending)
	}
	for _, e := range m.transcript.entries {
		if e.kind == entryUser {
			t.Errorf("a declined wake wrote the prompt row %q", e.text)
		}
	}
}

func TestBackgroundWorkflow_FinishWhileRunningWakesAtTheNextIdleFold(t *testing.T) {
	t.Parallel()
	eng := wakingEngine()
	m := newTestModelEng(t, eng, testOpts)
	m.transcript.addUser("first", nil)
	startStubWorker(t, &m)

	m = finishBackground(t, m)

	if eng.wakes() != 0 || !m.wakePending {
		t.Fatalf("Wake calls %d, pending %v while an Exchange runs; want none, held", eng.wakes(), m.wakePending)
	}
	m = step(t, m, exchangeDoneMsg{Result: domain.StepResult{Status: domain.StatusExchangeComplete}})
	if eng.wakes() != 1 || m.state != stateRunning {
		t.Errorf("after the Exchange ended: Wake calls %d, state %v; want the held wake launched", eng.wakes(), m.state)
	}
}

func TestBackgroundWorkflow_WakeIsHeldWhileASessionLoads(t *testing.T) {
	t.Parallel()
	eng := wakingEngine()
	m := newTestModelEng(t, eng, testOpts)
	m.sessionLoading = true

	m = finishBackground(t, m)
	if eng.wakes() != 0 {
		t.Fatalf("Wake calls = %d while a session load is in flight, want 0", eng.wakes())
	}
	m = step(t, m, sessionLoadedMsg{err: errors.New("record gone")})

	if eng.wakes() != 1 || m.state != stateRunning {
		t.Errorf("after the load landed: Wake calls %d, state %v; want the held wake launched", eng.wakes(), m.state)
	}
}

func TestBackgroundWorkflow_EventsReachNeitherTranscriptBoardNorStallClock(t *testing.T) {
	t.Parallel()
	m := newTestModelEng(t, &fakeEngine{}, testOpts)
	startStubWorker(t, &m)
	entries, heard := len(m.transcript.entries), m.lastEvent

	m = foldEvents(t, m,
		bgPhase(domain.WorkflowStarted),
		bgPhase(domain.WorkflowStageStarted),
		domain.SubAgentPhaseEvent{EventBase: bgChildBase("bg.1"), Phase: domain.SubAgentStarted},
		domain.TokenEvent{EventBase: bgChildBase("bg.1"), Text: "looking"},
		domain.ToolCallEvent{EventBase: bgChildBase("bg.1"), Call: domain.ToolCall{ID: "c1", Tool: "sub_agent"}, SpawnRunID: "bg.2"},
		domain.TokenEvent{EventBase: domain.EventBase{Depth: 2, Turn: 1, CallID: "c1", RunID: "bg.2"}, Text: "deeper"},
		bgItemOK(),
	)

	if got := len(m.transcript.entries); got != entries {
		t.Errorf("transcript entries = %d, want %d — a background workflow's events write nothing", got, entries)
	}
	for run := range m.acts {
		if run.id == "bg.1" || run.id == "bg.2" {
			t.Errorf("activity board holds background run %q", run.id)
		}
	}
	if !m.lastEvent.Equal(heard) {
		t.Error("a background workflow's events moved the stall clock")
	}
	if view := m.workflows.live[bgWorkflowID]; view.name != bgWorkflowName || view.ok != 1 {
		t.Errorf("workflow view = %+v, want %q with one ok item", view, bgWorkflowName)
	}
}

func TestBackgroundWorkflow_EscNeverStopsIt(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{}
	m := newTestModelEng(t, eng, testOpts)
	startStubWorker(t, &m)
	conversationChild := domain.EventBase{Depth: 1, Turn: 1, CallID: "call_1", RunID: "conv.1"}
	m = foldEvents(t, m,
		bgPhase(domain.WorkflowStarted),
		domain.SubAgentPhaseEvent{EventBase: bgChildBase("bg.1"), Phase: domain.SubAgentStarted},
		domain.TokenEvent{EventBase: bgChildBase("bg.1"), Text: "sweeping"}, // a first event opens a slot
		domain.SubAgentPhaseEvent{EventBase: conversationChild, Phase: domain.SubAgentStarted},
		domain.TokenEvent{EventBase: conversationChild, Text: "reading"},
	)

	for range 4 { // esc×2 stops the worker; a second esc×2 stops every delegation on the board
		m = step(t, m, keyEsc())
	}

	if got := eng.childStops; slices.Contains(got, "bg.1") {
		t.Errorf("StopChild calls = %v; esc reached the background workflow's run", got)
	}
	if !reflect.DeepEqual(eng.childStops, []string{"conv.1"}) {
		t.Errorf("StopChild calls = %v, want the conversation's own delegation only", eng.childStops)
	}
	if _, live := m.workflows.live[bgWorkflowID]; !live {
		t.Error("the background workflow is gone from its view after esc")
	}
}

func TestBackgroundWorkflow_ForkAfterAWakeCountsTheWakeExchange(t *testing.T) {
	t.Parallel()
	eng := wakingEngine()
	host := &fakeSessionHost{}
	m := newForkModel(t, eng, host)

	m = finishBackground(t, m)
	m = step(t, m, exchangeDoneMsg{Result: domain.StepResult{Status: domain.StatusExchangeComplete}})
	m = openForkPicker(t, m)
	rows := m.pickerOfferingRows()
	if len(rows) != len(forkPrompts)+1 || rows[len(rows)-1][1] != wakePromptText {
		t.Fatalf("rows = %v, want the typed prompts and then the wake's", rows)
	}
	m = step(t, m, keyDown())
	m = step(t, m, keyDown())
	_ = step(t, m, keyEnter()) // the last typed prompt: one Exchange — the wake's — lies after it

	if got := eng.cutCalls; len(got) != 1 || got[0] != 1 {
		t.Errorf("CutSnapshot drop counts = %v, want [1] — the wake opened an Exchange the cut drops", got)
	}
}

func TestBackgroundWorkflow_EscOnTheWakesFirstTurnMarksItsRow(t *testing.T) {
	t.Parallel()
	eng := wakingEngine()
	eng.settleDrops = true // the cancel found no finished Turn: the engine dropped the wake's opening
	m := newTestModelEng(t, eng, testOpts)
	m.transcript.addUser("first", nil)
	m.transcript.commitAssistant("done", runRef{})

	m = finishBackground(t, m)
	m = step(t, m, cancelledMsg{Result: domain.StepResult{Status: domain.StatusCancelled}})

	if prompt := lastPrompt(t, m); prompt.text != wakePromptText || !prompt.aborted {
		t.Errorf("last prompt = %q aborted %v; want the wake's row marked aborted", prompt.text, prompt.aborted)
	}
	if got := forkDrops(m.transcript.forkPoints()); len(got) != 1 {
		t.Errorf("fork points = %v, want the typed prompt alone — the dropped wake is none", got)
	}
}

func TestDeliverWorkflowNotes_TakesTheNoteAtTheBoundaryAndInterjectsIt(t *testing.T) {
	t.Parallel()
	note := domain.UserInput{Text: "Background workflow report (a note from apogee, not from the user):\nworkflow sweep finished"}
	eng := &fakeEngine{workflowNotes: []domain.UserInput{note}}
	var takenBeforeFirstStep int
	eng.stepFn = func(_ context.Context, call int) (domain.StepResult, error) {
		if call == 0 {
			takenBeforeFirstStep = eng.notesTaken
			return domain.StepResult{Status: domain.StatusTurnComplete}, nil
		}
		return domain.StepResult{Status: domain.StatusExchangeComplete}, nil
	}

	msg := driveWake(context.Background(), eng, newInterjectBox(), nil, nil)

	if _, done := msg.(exchangeDoneMsg); !done {
		t.Fatalf("terminal Msg = %T, want exchangeDoneMsg", msg)
	}
	if takenBeforeFirstStep != 0 {
		t.Error("a note was taken before the first Step opened the Exchange")
	}
	if got := eng.interjections(); len(got) != 1 || !strings.HasPrefix(got[0].Text, "Background workflow report") {
		t.Errorf("Interject calls = %v, want the note committed at the boundary", got)
	}
}
