package tui

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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

// bgEnd is the background workflow's finished or stopped end phase carrying the engine's tally.
func bgEnd(phase domain.WorkflowPhase, tally domain.WorkflowTally) domain.WorkflowPhaseEvent {
	e := bgPhase(phase)
	e.Tally = &tally
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
	return foldEvents(t, m, bgPhase(domain.WorkflowStarted), bgItemOK(), bgEnd(domain.WorkflowFinished, domain.WorkflowTally{OK: 1}))
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
	m.holds.hold(holdSessionLoad)

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
	if view := m.workflows.live[bgWorkflowID]; view.name != bgWorkflowName {
		t.Errorf("workflow view = %+v, want %q", view, bgWorkflowName)
	}
}

// A stopped background workflow's finish line reads its end phase's tally: neither a verify step's
// ok receipt nor an earlier round's item is counted, and its unfinished items are left out.
func TestBackgroundWorkflow_AStoppedRunsFinishLineReadsItsTally(t *testing.T) {
	t.Parallel()
	const want = "background workflow sweep stopped — items 1 · ok 1 · partial 0 · blocked 0"
	verify := bgItemOK()
	verify.Stage = "verify"
	secondRound := bgItemOK()
	secondRound.Round = 2
	for name, items := range map[string][]domain.Event{
		"a verify item":  {bgItemOK(), verify},
		"a second round": {bgItemOK(), secondRound},
		"no item events": nil,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := newTestModelEng(t, &fakeEngine{}, testOpts)
			startStubWorker(t, &m)

			m = foldEvents(t, m, bgPhase(domain.WorkflowStarted))
			m = foldEvents(t, m, items...)
			m = foldEvents(t, m, bgEnd(domain.WorkflowStopped, domain.WorkflowTally{OK: 1, Unfinished: 2}))

			if !slices.Contains(noteTexts(m), want) {
				t.Errorf("notes = %q, want the finish line %q", noteTexts(m), want)
			}
		})
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

// bgQuestion is a question the background workflow waits on, as the engine's queue lists it.
func bgQuestion(id uint64) domain.WorkflowPrompt {
	return domain.WorkflowPrompt{ID: id, Workflow: bgWorkflowID, Name: bgWorkflowName,
		Question: &domain.AskRequest{Question: "Sweep deep?", Choices: []string{"yes", "no"}}}
}

// bgApproval is a gate the background workflow waits on, as the engine's queue lists it.
func bgApproval(id uint64) domain.WorkflowPrompt {
	return domain.WorkflowPrompt{ID: id, Workflow: bgWorkflowID, Name: bgWorkflowName,
		Approval: &domain.ApprovalRequest{Tool: "write_file", Arguments: []byte(`{}`)}}
}

// bgWaiting is the waiting phase the background workflow reports once a prompt is queued.
func bgWaiting() domain.WorkflowPhaseEvent {
	e := bgPhase(domain.WorkflowWaiting)
	e.Stage, e.Detail = "scope", "Sweep deep?"
	return e
}

// statusText is the status line's left slot without its styling.
func leftStatus(m Model) string { return ansiPattern.ReplaceAllString(m.statusLeft(), "") }

// armed returns m with the open decision pane's latch open, as the terminal's drain answer would.
func armed(m Model) Model {
	m.approvalArmed = true
	return m
}

func TestBackgroundWorkflow_StatusLineCountsRunningAndWaitingWorkflows(t *testing.T) {
	t.Parallel()
	m := newTestModelEng(t, &fakeEngine{}, testOpts)
	startStubWorker(t, &m) // busy, so a waiting prompt is counted but not opened

	m = foldEvents(t, m, bgPhase(domain.WorkflowStarted))
	if got := leftStatus(m); !strings.Contains(got, "1 workflow running") || strings.Contains(got, "waiting") {
		t.Errorf("status = %q, want `1 workflow running` and nothing waiting", got)
	}
	second := bgPhase(domain.WorkflowStarted)
	second.Workflow, second.Name = "sweep-2", "other"
	m = foldEvents(t, m, second, bgWaiting())
	if got := leftStatus(m); !strings.Contains(got, "2 workflows running · 1 workflow waiting for you") {
		t.Errorf("status = %q, want `2 workflows running · 1 workflow waiting for you`", got)
	}

	m = step(t, m, exchangeDoneMsg{Result: domain.StepResult{Status: domain.StatusExchangeComplete}})
	if got := leftStatus(m); !strings.Contains(got, "2 workflows running") || strings.Contains(got, "waiting") {
		t.Errorf("idle status = %q, want the waiting count synced to the engine's empty queue", got)
	}
	m = foldEvents(t, m, bgPhase(domain.WorkflowStopped), func() domain.WorkflowPhaseEvent {
		e := bgPhase(domain.WorkflowFinished)
		e.Workflow, e.Name = "sweep-2", "other"
		return e
	}())
	if got := leftStatus(m); strings.Contains(got, "workflow") {
		t.Errorf("status = %q after both ended, want no workflow readout", got)
	}
}

func TestBackgroundWorkflow_AWaitingQuestionOpensOnlyWhenIdle(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{workflowPrompts: []domain.WorkflowPrompt{bgQuestion(7)}}
	m := newTestModelEng(t, eng, testOpts)
	m.transcript.addUser("first", nil)
	startStubWorker(t, &m)

	m = foldEvents(t, m, bgPhase(domain.WorkflowStarted), bgWaiting())
	if eng.listings() != 0 || m.pendingAsk != nil || m.state != stateRunning {
		t.Fatalf("listings %d, pane %v, state %v while an Exchange runs; want the question held", eng.listings(), m.pendingAsk != nil, m.state)
	}
	m = step(t, m, exchangeDoneMsg{Result: domain.StepResult{Status: domain.StatusExchangeComplete}})

	if m.state != stateAwaitingAsk || m.pendingAsk == nil || m.pendingAsk.Workflow == nil || m.pendingAsk.Workflow.ID != 7 {
		t.Fatalf("state %v, pane %+v; want the waiting question open once idle", m.state, m.pendingAsk)
	}
	if q := m.pendingAsk.Request.Question; !strings.HasPrefix(q, "background workflow sweep asks:\n") || !strings.HasSuffix(q, "Sweep deep?") {
		t.Errorf("question = %q, want it led by the workflow's name", q)
	}
	if m.worker.cancel != nil {
		t.Error("opening a background question started a worker")
	}
}

func TestBackgroundWorkflow_AnAnsweredQuestionGoesToTheEngineAndReturnsToIdle(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{workflowPrompts: []domain.WorkflowPrompt{bgQuestion(7)}}
	m := newTestModelEng(t, eng, testOpts)
	m.input.SetValue("half a message")
	m = foldEvents(t, m, bgPhase(domain.WorkflowStarted), bgWaiting())
	if m.state != stateAwaitingAsk {
		t.Fatalf("state = %v, want the question open at idle", m.state)
	}

	m = step(t, armed(m), keyEnter()) // the first choice, highlighted, is the answer

	want := []workflowPromptAnswer{{id: 7, answer: domain.WorkflowPromptAnswer{Text: "yes"}}}
	if got := eng.answers(); !reflect.DeepEqual(got, want) {
		t.Errorf("answers = %+v, want %+v", got, want)
	}
	if m.state != stateIdle || m.pendingAsk != nil || m.worker.cancel != nil {
		t.Errorf("state %v, pane %v, worker %v; want idle with no worker", m.state, m.pendingAsk != nil, m.worker.cancel != nil)
	}
	if got := m.input.Value(); got != "half a message" {
		t.Errorf("input = %q, want the borrowed draft handed back", got)
	}
	if got := leftStatus(m); strings.Contains(got, "waiting") {
		t.Errorf("status = %q, want nothing waiting once answered", got)
	}
}

func TestBackgroundWorkflow_EscSendsTheQuestionBackToTheQueue(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{workflowPrompts: []domain.WorkflowPrompt{bgQuestion(7)}}
	m := newTestModelEng(t, eng, testOpts)
	m = foldEvents(t, m, bgPhase(domain.WorkflowStarted), bgWaiting())

	m = step(t, m, keyEsc())

	if m.state != stateIdle || m.pendingAsk != nil || len(eng.answers()) != 0 || m.worker.cancel != nil {
		t.Fatalf("state %v, pane %v, answers %v; want idle with the question unanswered", m.state, m.pendingAsk != nil, eng.answers())
	}
	if got := leftStatus(m); !strings.Contains(got, "1 workflow waiting for you") {
		t.Errorf("status = %q, want the dismissed question still counted", got)
	}
	m = step(t, m, bgItemOK())
	if m.pendingAsk != nil {
		t.Fatal("a dismissed question reopened on the next fold")
	}

	m.transcript.addUser("next", nil)
	startStubWorker(t, &m)
	m = step(t, m, exchangeDoneMsg{Result: domain.StepResult{Status: domain.StatusExchangeComplete}})
	if m.state != stateAwaitingAsk || m.pendingAsk == nil || m.pendingAsk.Workflow.ID != 7 {
		t.Errorf("state %v after the next Exchange, want the dismissed question offered again", m.state)
	}
}

func TestBackgroundWorkflow_AQueuedApprovalOpensAtIdleAndIsAnsweredThroughTheEngine(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{workflowPrompts: []domain.WorkflowPrompt{bgApproval(3)}}
	m := newTestModelEng(t, eng, testOpts)

	m = foldEvents(t, m, bgPhase(domain.WorkflowStarted), bgWaiting())
	if m.state != stateAwaitingApproval || m.pending == nil || m.pending.Workflow == nil || m.pending.Request.Tool != "write_file" {
		t.Fatalf("state %v, pane %+v; want the queued approval open at idle", m.state, m.pending)
	}
	m = step(t, armed(m), keyRune('a'))

	want := []workflowPromptAnswer{{id: 3, answer: domain.WorkflowPromptAnswer{Decision: domain.ApprovalAllow}}}
	if got := eng.answers(); !reflect.DeepEqual(got, want) {
		t.Errorf("answers = %+v, want %+v", got, want)
	}
	if m.state != stateIdle || m.pending != nil || m.worker.cancel != nil {
		t.Errorf("state %v, pane %v; want idle with no worker", m.state, m.pending != nil)
	}
}

func TestBackgroundWorkflow_EndingTheWorkflowClosesItsOpenPrompt(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{workflowPrompts: []domain.WorkflowPrompt{bgApproval(3)}}
	m := newTestModelEng(t, eng, testOpts)
	m = foldEvents(t, m, bgPhase(domain.WorkflowStarted), bgWaiting())
	eng.mu.Lock()
	eng.workflowPrompts = nil // the stop withdrew it
	eng.mu.Unlock()

	m = foldEvents(t, m, bgPhase(domain.WorkflowStopped))

	if m.state != stateIdle || m.pending != nil || len(eng.answers()) != 0 {
		t.Errorf("state %v, pane %v, answers %v; want the pane closed unanswered", m.state, m.pending != nil, eng.answers())
	}
}

func TestBackgroundWorkflow_QuitWithAPromptOpenQuitsAtOnce(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{workflowPrompts: []domain.WorkflowPrompt{bgQuestion(7)}}
	m := newTestModelEng(t, eng, testOpts)
	m = foldEvents(t, m, bgPhase(domain.WorkflowStarted), bgWaiting())

	next, cmd := m.quit()

	if got := next.(Model); got.state != stateIdle || got.quitting && cmd == nil {
		t.Errorf("state %v, quitting %v; want the prompt dismissed and the idle quit taken", got.state, got.quitting)
	}
	if len(eng.answers()) != 0 {
		t.Errorf("answers = %v, want the question left unanswered", eng.answers())
	}
}

// A claimed background reading names the model that answered it: that model joins the session's
// served set, which /usage paints and the record keeps, while the gauge and the main agent's totals
// stay where they were.
func TestBackgroundWorkflow_AReadingsServedModelJoinsTheSession(t *testing.T) {
	t.Parallel()
	m := newTestModelEng(t, &fakeEngine{}, testOpts)
	reading := servedUsage("routed-model", 1)
	reading.EventBase = bgChildBase("bg.1")

	m = foldEvents(t, m, bgPhase(domain.WorkflowStarted), reading)

	if !slices.Equal(m.servedModels, []string{"routed-model"}) {
		t.Errorf("servedModels = %q, want the background run's own model", m.servedModels)
	}
	if !strings.Contains(usageContent(m.usageRows(), m.servedModels).body, "routed-model") {
		t.Errorf("the /usage pane does not name the background run's model on its served: line")
	}
	if m.usage != (domain.Usage{}) || m.ctxUsed != 0 {
		t.Errorf("usage = %+v, fill = %d, want both untouched by a background reading", m.usage, m.ctxUsed)
	}
}

// A session boundary a background workflow runs across rebases its spend: the closed session's
// record took what it had spent, so the fresh session's delegate sum starts at zero and the
// workflow's stop or finish line carries only what it spent after the clear — nothing after a `y`
// stopped it, the later readings' growth after an `n` kept it.
func TestBackgroundWorkflow_SpendAcrossAClearCountsOnlyWhatFollows(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		key   tea.KeyPressMsg
		after []domain.Event
		end   domain.WorkflowPhase
		want  domain.Usage
	}{
		{"y stops it", keyRune('y'), nil, domain.WorkflowStopped, domain.Usage{}},
		{
			"n keeps it", keyRune('n'),
			[]domain.Event{workflowRunUsage(bgChildBase("bg.1"), 3, 3200, 330)},
			domain.WorkflowFinished,
			domain.Usage{Calls: 1, PromptTokens: 700, CompletionTokens: 30, TotalTokens: 730},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := runningWorkflowModel(t, &fakeEngine{})
			m = foldEvents(t, m, workflowRunUsage(bgChildBase("bg.1"), 2, 2500, 300))
			if got := m.delegateUsageTotal(); got.Calls != 2 {
				t.Fatalf("precondition: the delegate total = %+v, want the workflow's two calls", got)
			}

			m, _ = typeCommand(t, m, "/clear")
			m = step(t, m, tc.key)

			if got := m.delegateUsageTotal(); got != (domain.Usage{}) {
				t.Errorf("delegate total after /clear = %+v, want zero — the closed session's record took it", got)
			}
			m = foldEvents(t, m, tc.after...)
			m = foldEvents(t, m, bgPhase(tc.end))
			var line entry
			for _, e := range m.transcript.entries {
				if e.kind == entryNote && strings.HasPrefix(e.text, "background workflow "+bgWorkflowName) {
					line = e
				}
			}
			if line.text == "" {
				t.Fatalf("no end line for the workflow among the notes %q", noteTexts(m))
			}
			if line.usage != tc.want {
				t.Errorf("end line spend = %+v, want %+v — only what followed the clear", line.usage, tc.want)
			}
			if got := m.delegateUsageTotal(); got != tc.want {
				t.Errorf("delegate total = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// Closing a background prompt's pane hands the box back and returns to idle: the answer, esc, and the
// workflow ending under the open pane are each laid out by Update's tail (doc.go, "an arm mutates").
func TestBackgroundPromptCloseIsSettledByTheTail(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		close func(t *testing.T, m Model, eng *fakeEngine) Model
	}{
		{"answered", func(t *testing.T, m Model, _ *fakeEngine) Model { return step(t, armed(m), keyEnter()) }},
		{"dismissed", func(t *testing.T, m Model, _ *fakeEngine) Model { return step(t, m, keyEsc()) }},
		{"its workflow ended", func(t *testing.T, m Model, eng *fakeEngine) Model {
			eng.workflowPrompts = nil // the stop withdrew it
			return foldEvents(t, m, bgPhase(domain.WorkflowStopped))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			eng := &fakeEngine{workflowPrompts: []domain.WorkflowPrompt{bgQuestion(7)}}
			m := newTestModelEng(t, eng, testOpts)
			m.input.SetValue("half a message")
			m = foldEvents(t, m, bgPhase(domain.WorkflowStarted), bgWaiting())
			if m.state != stateAwaitingAsk {
				t.Fatalf("precondition: state = %v, want the question open at idle", m.state)
			}

			m = tc.close(t, m, eng)
			if m.state != stateIdle || m.pendingAsk != nil {
				t.Fatalf("precondition: state %v, pane %v; want idle with the pane closed", m.state, m.pendingAsk != nil)
			}
			assertSettled(t, m)
		})
	}
}

// ----------------------------------------------------------------------------
// A background sub_agent (ADR 0094)
// ----------------------------------------------------------------------------

// bgSubAgentStarted is the immediate answer a background sub_agent call gets (internal/agent's
// subAgentBackgroundStarted over the workflow's id and the delegation's name).
const bgSubAgentStarted = "sub_agent started in the background as workflow " + bgWorkflowID + ": scout"

// bgSubAgentCall is a depth-0 sub_agent call asking for the background, with the run id the engine
// spawned for it — "" for a call that runs as a background workflow.
func bgSubAgentCall(id, spawnRunID string) domain.ToolCallEvent {
	return domain.ToolCallEvent{
		Call: domain.ToolCall{
			ID: id, Tool: subAgentToolName,
			Arguments: []byte(`{"task":"Scan the scout area of the repo","name":"scout","background":true}`),
		},
		SpawnRunID: spawnRunID,
	}
}

// bgSubAgentAnswer is the immediate answer to the call id names.
func bgSubAgentAnswer(id string) domain.ToolResultEvent {
	return domain.ToolResultEvent{Result: domain.ToolResult{
		CallID:  id,
		Content: bgSubAgentStarted + "\nYou are woken with its report when it ends.",
	}}
}

// A background sub_agent's call paints as a background-workflow call — its name as the target,
// the immediate answer's first line in the slot — and never as a delegation block: it heads no
// run, wears no `done`, two of them never fold into one "Sub-Agent (2)" group, and its call fires
// no delegation progress save.
func TestBackgroundSubAgent_CallPaintsAsABackgroundWorkflowCall(t *testing.T) {
	t.Parallel()
	tr := feed(bgSubAgentCall("s1", ""), bgSubAgentAnswer("s1"), bgSubAgentCall("s2", ""), bgSubAgentAnswer("s2"))

	for i, e := range tr.entries {
		if e.kind != entryToolCall {
			continue
		}
		if e.headsRun() || !e.tool.background {
			t.Errorf("entry %d: headsRun %v, background %v; want a background call that heads no run", i, e.headsRun(), e.tool.background)
		}
		if group := subAgentGroup(tr.entries, i); group != nil {
			t.Errorf("entry %d groups as a delegation: %+v", i, group)
		}
	}
	painted := plainRender(tr)
	if !strings.Contains(painted, "scout "+glyphLeaderDot+" "+bgSubAgentStarted) {
		t.Errorf("no row reads the delegation's name over its immediate answer:\n%s", painted)
	}
	for _, refused := range []string{"done", "Sub-Agent (2)"} {
		if strings.Contains(painted, refused) {
			t.Errorf("a background sub_agent's call reads %q:\n%s", refused, painted)
		}
	}
	if progressSaveTrigger(bgSubAgentCall("s3", "")) {
		t.Error("a background sub_agent's call fired the delegation progress save")
	}
}

// A call that asked for the background where the switch is not offered runs blocking, with a run
// id of its own, and stays a delegation's head.
func TestBackgroundSubAgent_ABlockingFallbackStaysADelegation(t *testing.T) {
	t.Parallel()
	tr := feed(bgSubAgentCall("s1", "run.1"))
	if e := tr.entries[0]; !e.headsRun() || e.tool.background {
		t.Errorf("headsRun %v, background %v; want a blocking delegation's head", e.headsRun(), e.tool.background)
	}
	if !progressSaveTrigger(bgSubAgentCall("s1", "run.1")) {
		t.Error("a blocking delegation's call fired no progress save")
	}
}

// A background sub_agent's call replays from the record as the background call it was: the record
// keeps its arguments and its (empty) spawned run id, and the decode re-derives the rest.
func TestBackgroundSubAgent_CallReplaysAsABackgroundCall(t *testing.T) {
	t.Parallel()
	data, err := encodeTranscript(feed(bgSubAgentCall("s1", ""), bgSubAgentAnswer("s1")))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := decodeTranscript(data)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(entries, func(e entry) bool { return e.kind == entryToolCall })
	if i < 0 {
		t.Fatal("the replayed record holds no call")
	}
	if e := entries[i]; e.headsRun() || !e.tool.background {
		t.Errorf("replayed: headsRun %v, background %v; want the background call", e.headsRun(), e.tool.background)
	}
}

// A background sub_agent's finish line names it by its delegation's name and counts its one item
// on the receipt the engine built for it.
func TestBackgroundSubAgent_FinishLineNamesTheDelegation(t *testing.T) {
	t.Parallel()
	const want = "background workflow scout finished — items 1 · ok 1 · partial 0 · blocked 0"
	m := newTestModelEng(t, &fakeEngine{}, testOpts)
	startStubWorker(t, &m)

	started := bgPhase(domain.WorkflowStarted)
	started.Name = "scout"
	end := bgEnd(domain.WorkflowFinished, domain.WorkflowTally{OK: 1})
	end.Name = "scout"
	m = foldEvents(t, m, started, end)

	if !slices.Contains(noteTexts(m), want) {
		t.Errorf("notes = %q, want the finish line %q", noteTexts(m), want)
	}
}
