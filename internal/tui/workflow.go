package tui

import (
	"fmt"
	"maps"
	"slices"
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
//
// Its approvals and questions wait for the human. A gate one of its runs reaches, and an `ask`
// stage's question, wait in the engine's own queue rather than on the conversation's prompt; each
// is reported by a waiting phase once it is queued. The status line counts the live workflows and
// the ones waiting (statusTrail), folded from the phases alone — it never reads the engine. The
// waiting prompt opens when the human is idle (offerWaitingPrompt, the Update tail): the engine's
// queue is read, and the oldest prompt not dismissed opens in the approval or the ask pane with its
// origin on the request. Its answer goes back through Engine.AnswerWorkflowPrompt and returns the
// TUI to idle (leaveWorkflowPrompt) — there is no worker to resume — and esc dismisses it back to
// the queue, where it waits, still counted, until the next Exchange ends and it is offered again, or
// until the human opens it from /workflows: ^a in the workflow's detail opens its oldest waiting
// prompt, a dismissed one included, at idle only (answerShownWorkflow). The idle offer is held while
// the /workflows pane is up, as it is behind any modal pane.
// A workflow that ends while its prompt is open takes the pane with it.
//
// Its spend reaches the session's accounting. The usage readings its runs report fold into its view,
// latest-wins per run (backgroundWorkflow.spend) — never into the gauge or the main agent's totals,
// though the model a reading names joins the session's served set. /usage lists the live view as a
// row named for the workflow, and the finish line carries the sum once it ends, so the record's
// delegate sum counts the workflow alive or finished (delegateUsageHeads). A session boundary
// rebases a view that runs across it (backgroundWorkflows.rebased): the closed session's record took
// what it had spent, and the fresh one counts only what comes after.

// The words the finish line, the wake's prompt row and the two failure notes are built from.
const (
	backgroundFinishFormat = "background workflow %s %s — items %d · ok %d · partial %d · blocked %d"
	backgroundFailedFormat = "background workflow %s failed — %s"
	wakePromptText         = "(background workflow report)"
	wakeFailedPrefix       = "could not wake the agent on a background workflow's report: "
	workflowNoteLostPrefix = "a background workflow's report did not reach the model: "
)

// The words the status line's workflow readout (statusTrail) and a waiting question's lead line
// are built from.
const (
	workflowRunningOne     = "1 workflow running"
	workflowRunningFormat  = "%d workflows running"
	workflowWaitingOne     = "1 workflow waiting for you"
	workflowWaitingFormat  = "%d workflows waiting for you"
	workflowQuestionFormat = "background workflow %s asks:"
)

// The receipt statuses a finish line counts (domain.WorkflowReceipt.Status).
const (
	receiptOK      = "ok"
	receiptPartial = "partial"
	receiptBlocked = "blocked"
)

// backgroundWorkflows is the session's live background workflows as their events fold: each one's
// view by workflow id, the run id of every run a background run spawned, mapped to its workflow,
// and the ids of the waiting prompts the human dismissed with esc, which the idle offer skips. The
// maps are replaced on write, never mutated in place, because the Model is copied by value on every
// Update and an earlier copy must not see a later fold. The zero value is empty.
type backgroundWorkflows struct {
	live      map[string]backgroundWorkflow
	runs      map[string]string
	dismissed map[uint64]bool
}

// backgroundWorkflow is one live background workflow: its name, its items so far, counted by the
// status their receipts ended on, how many of its prompts wait for the human, and what its runs
// have spent — each run's latest reading (spend), less what they had spent when this session began
// (base, the readings a session boundary rebased the view at).
type backgroundWorkflow struct {
	name    string
	ok      int
	partial int
	blocked int
	waiting int
	spend   runSpend
	base    runSpend
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

// synced returns b with every live workflow's waiting count taken from prompts — the engine's queue
// as it stands — and every dismissed id that is no longer queued forgotten.
func (b backgroundWorkflows) synced(prompts []domain.WorkflowPrompt) backgroundWorkflows {
	counts := make(map[string]int, len(prompts))
	queued := make(map[uint64]bool, len(prompts))
	for _, prompt := range prompts {
		counts[prompt.Workflow]++
		queued[prompt.ID] = true
	}
	live := make(map[string]backgroundWorkflow, len(b.live))
	for id, view := range b.live {
		view.waiting = counts[id]
		live[id] = view
	}
	dismissed := maps.Clone(b.dismissed)
	maps.DeleteFunc(dismissed, func(id uint64, _ bool) bool { return !queued[id] })
	b.live, b.dismissed = live, dismissed
	return b
}

// answered returns b with prompt counted off its workflow's waiting prompts.
func (b backgroundWorkflows) answered(prompt domain.WorkflowPrompt) backgroundWorkflows {
	view, ok := b.live[prompt.Workflow]
	if !ok || view.waiting == 0 {
		return b
	}
	view.waiting--
	return b.withWorkflow(prompt.Workflow, view)
}

// withReading returns b with reading as the latest of run, a run of workflow id; a workflow with no
// live view folds nothing.
func (b backgroundWorkflows) withReading(id string, run runRef, reading domain.Usage) backgroundWorkflows {
	view, ok := b.live[id]
	if !ok {
		return b
	}
	view.spend = view.spend.with(run, reading)
	return b.withWorkflow(id, view)
}

// rebased returns b with every live view's base moved to what its runs have spent so far: the
// session boundary a workflow runs across (resetSessionView, resumeLoaded). The closed session's
// record already counted that spend, so the view — its /usage row, and the finish line it ends on —
// counts only what its runs spend from here.
func (b backgroundWorkflows) rebased() backgroundWorkflows {
	if len(b.live) == 0 {
		return b
	}
	live := make(map[string]backgroundWorkflow, len(b.live))
	for id, view := range b.live {
		view.base = view.spend
		live[id] = view
	}
	b.live = live
	return b
}

// spendEntries are the live workflows that spent something this session, as the entries /usage
// reads a delegate from (delegateUsageHeads): each one's spend, named for it, in id order so the
// rows stand still between two paints.
func (b backgroundWorkflows) spendEntries() []entry {
	ids := slices.Sorted(maps.Keys(b.live))
	entries := make([]entry, 0, len(ids))
	for _, id := range ids {
		view := b.live[id]
		spent := view.spent()
		if spent.Calls <= 0 {
			continue
		}
		entries = append(entries, entry{kind: entryNote, tool: toolView{Target: stripEscapes(view.name)}, usage: spent})
	}
	return entries
}

// withoutDismissed returns b with the prompt id's dismissal forgotten.
func (b backgroundWorkflows) withoutDismissed(id uint64) backgroundWorkflows {
	if !b.dismissed[id] {
		return b
	}
	dismissed := maps.Clone(b.dismissed)
	delete(dismissed, id)
	b.dismissed = dismissed
	return b
}

// withDismissed returns b with the prompt id marked dismissed.
func (b backgroundWorkflows) withDismissed(id uint64) backgroundWorkflows {
	dismissed := maps.Clone(b.dismissed)
	if dismissed == nil {
		dismissed = make(map[uint64]bool)
	}
	dismissed[id] = true
	b.dismissed = dismissed
	return b
}

// waits reports whether live workflow id has a prompt waiting for the human.
func (b backgroundWorkflows) waits(id string) bool {
	return b.live[id].waiting > 0
}

// waitingCount is how many live workflows have a prompt waiting for the human.
func (b backgroundWorkflows) waitingCount() int {
	n := 0
	for _, view := range b.live {
		if view.waiting > 0 {
			n++
		}
	}
	return n
}

// readout is the status line's workflow segment: how many background workflows run and, when any
// waits on the human, how many do — "" when none runs.
func (b backgroundWorkflows) readout() string {
	running := len(b.live)
	if running == 0 {
		return ""
	}
	text := fmt.Sprintf(workflowRunningFormat, running)
	if running == 1 {
		text = workflowRunningOne
	}
	switch waiting := b.waitingCount(); {
	case waiting == 1:
		text += " · " + workflowWaitingOne
	case waiting > 1:
		text += " · " + fmt.Sprintf(workflowWaitingFormat, waiting)
	}
	return text
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

// spent is what the workflow's runs have spent since the last session boundary.
func (v backgroundWorkflow) spent() domain.Usage {
	return usageSince(v.spend.total(), v.base.total())
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
// wire exchange whatever run it belongs to, and into the workflow's own state — its phases, the run
// a background run's delegation spawns, recorded so that run's events are claimed too, and the
// usage its runs report. A reading's served model is a session fact before it is any run's, so it
// joins the session's set exactly as foldStats folds it ahead of its depth guard; nothing else of
// the reading reaches the gauge or the main agent's totals.
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
	case domain.UsageEvent:
		if e.ServedModel != "" && !slices.Contains(m.servedModels, e.ServedModel) {
			m.servedModels = append(slices.Clone(m.servedModels), e.ServedModel)
		}
		if id, ok := m.workflows.workflowOf(e.EventBase); ok {
			m.workflows = m.workflows.withReading(id, runOf(e.EventBase), usageReading(e))
		}
	}
	return m
}

// foldBackgroundPhase folds one phase of a background workflow: a start opens its view, a finished
// item counts, a waiting prompt counts and asks for the idle offer (promptPending), and an end
// drops the view — closing its prompt's pane if one is open — writes the finish line carrying what
// the workflow spent (transcript.addWorkflowNote) and asks for a wake (wakePending). A stage start
// changes nothing here.
func (m Model) foldBackgroundPhase(e domain.WorkflowPhaseEvent) Model {
	switch e.Phase {
	case domain.WorkflowStarted:
		m.workflows = m.workflows.withWorkflow(e.Workflow, backgroundWorkflow{name: e.Name})
	case domain.WorkflowItemFinished:
		view := m.workflows.view(e.Workflow, e.Name).count(e.Receipt.Status)
		m.workflows = m.workflows.withWorkflow(e.Workflow, view)
	case domain.WorkflowWaiting:
		view := m.workflows.view(e.Workflow, e.Name)
		view.waiting++
		m.workflows = m.workflows.withWorkflow(e.Workflow, view)
		m.promptPending = true
	case domain.WorkflowFinished, domain.WorkflowStopped, domain.WorkflowFailed:
		view := m.workflows.view(e.Workflow, e.Name)
		m.workflows = m.workflows.without(e.Workflow)
		if origin := m.workflowPromptOrigin(); origin != nil && origin.Workflow == e.Workflow {
			m.closeWorkflowPrompt()
		}
		m.transcript.addWorkflowNote(view.finishLine(e), view.name, view.spent())
		m.wakePending = true
	}
	return m
}

// statusTrail is what the status line's left slot carries after its phrase: the queued readout
// (queuedSegment), then the background workflows' (backgroundWorkflows.readout), each led by the
// separator when something stands before it. Folded state only — it never reads the engine.
func (m Model) statusTrail(afterPhrase bool) string {
	queued := m.queuedSegment(afterPhrase)
	text := m.workflows.readout()
	if text == "" {
		return queued
	}
	if afterPhrase || queued != "" {
		text = " · " + text
	}
	return queued + m.th.statusBar.Render(text)
}

// offerAfterFold is the Update tail's waiting-prompt half, run before the wake: when a background
// workflow has reported a prompt since the last offer, it tries one now (offerWaitingPrompt).
// Anything but the Model, or a Model with no offer pending, passes through untouched.
func offerAfterFold(next tea.Model, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	m, ok := next.(Model)
	if !ok || !m.promptPending {
		return next, cmd
	}
	m, open := m.offerWaitingPrompt()
	if open == nil {
		return m, cmd
	}
	return m, tea.Batch(cmd, open)
}

// offerWaitingPrompt opens the oldest waiting background prompt the human has not dismissed, when
// the human is idle — the same idle a wake waits for (canWake); until then the offer is held and
// the next fold asks again. The engine's queue is read once per try, and the waiting counts are
// synced to it, so a prompt its workflow's stop withdrew stops being counted.
func (m Model) offerWaitingPrompt() (Model, tea.Cmd) {
	if !m.canWake() {
		return m, nil
	}
	m.promptPending = false
	prompts := m.eng.WorkflowPrompts()
	m.workflows = m.workflows.synced(prompts)
	for _, prompt := range prompts {
		if !m.workflows.dismissed[prompt.ID] {
			return m.openWorkflowPrompt(prompt)
		}
	}
	return m, nil
}

// openWorkflowPrompt opens prompt in the pane its kind takes — an approval in the approval pane, a
// question in the ask pane under a line naming the workflow — with prompt as the request's origin
// and no Reply, through the very folds the conversation's own requests take.
func (m Model) openWorkflowPrompt(prompt domain.WorkflowPrompt) (Model, tea.Cmd) {
	origin := prompt
	var (
		next tea.Model
		cmd  tea.Cmd
	)
	switch {
	case prompt.Approval != nil:
		next, cmd = m.foldApprovalRequest(approvalReqMsg{Request: *prompt.Approval, Workflow: &origin})
	case prompt.Question != nil:
		request := *prompt.Question
		request.Question = fmt.Sprintf(workflowQuestionFormat, prompt.Name) + "\n" + request.Question
		next, cmd = m.foldAskRequest(askReqMsg{Request: request, Workflow: &origin})
	default:
		return m, nil
	}
	return next.(Model), cmd
}

// workflowPromptOrigin is the background prompt the open decision pane answers, or nil when no pane
// is open or the open one is the conversation's own.
func (m Model) workflowPromptOrigin() *domain.WorkflowPrompt {
	switch {
	case m.state == stateAwaitingApproval && m.pending != nil:
		return m.pending.Workflow
	case m.state == stateAwaitingAsk && m.pendingAsk != nil:
		return m.pendingAsk.Workflow
	}
	return nil
}

// answerWorkflowPrompt hands the human's answer to origin back to the engine and closes the pane.
// An answer the engine no longer takes — the workflow's stop withdrew the prompt first — is simply
// dropped: the workflow's end is folded on its own.
func (m *Model) answerWorkflowPrompt(origin domain.WorkflowPrompt, answer domain.WorkflowPromptAnswer) {
	m.eng.AnswerWorkflowPrompt(origin.ID, answer)
	m.workflows = m.workflows.answered(origin)
	m.closeWorkflowPrompt()
}

// dismissWorkflowPrompt is esc on a background prompt's pane: the prompt goes back to the engine's
// queue unanswered — its workflow still waits on it, and the status line still counts it — and is
// not offered again until the next Exchange ends (finishWorker), though ^a in /workflows opens it
// sooner (answerShownWorkflow). It never touches a worker.
func (m Model) dismissWorkflowPrompt() (Model, tea.Cmd) {
	origin := m.workflowPromptOrigin()
	if origin == nil {
		return m, nil
	}
	m.workflows = m.workflows.withDismissed(origin.ID)
	m.closeWorkflowPrompt()
	return m, nil
}

// closeWorkflowPrompt closes the open background prompt's pane and returns the TUI to idle — the
// state it was opened from, with no worker behind it, so nothing resumes (never resumeRunning).
// The box the ask pane borrowed is handed back, a rebind stashed while the pane stood is applied
// (the engine is the Update loop's, as at an Exchange's end), and the next waiting prompt is
// offered at the tail.
func (m *Model) closeWorkflowPrompt() {
	m.pendingDecision.reset()
	m.restoreAskDraft()
	m.state = stateIdle
	m.layout()
	if !m.bgLaunching && !m.actuation.inFlight {
		m.applyPendingRebind()
	}
	m.promptPending = true
}

// reofferDismissed forgets every dismissal, so a prompt the human sent back with esc is offered
// again at the next idle fold; finishWorker calls it as an Exchange ends.
func (m *Model) reofferDismissed() {
	if len(m.workflows.dismissed) == 0 {
		return
	}
	m.workflows.dismissed = nil
	m.promptPending = true
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
