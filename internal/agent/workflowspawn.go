package agent

// The WORKFLOW CHILD SPAWNER (ADR 0087): the agent's implementation of workflow.Spawner. Every
// item child of a workflow is a nested Agent built through the recursion point exactly as a
// sub_agent child is (ADR 0014) — newChildAgentOn, a run id minted from the tree's minter, the
// started/finished phase pair stamped with the child's identity under the spawning call's id —
// with its privileges the parent's or stricter (ADR 0005/0013) and the same depth bound.
//
// What differs is how the child reports and what the parent keeps. The child reports through a
// `finish` tool (tools.Finish) whose schema is its stage's ReceiptSpec: a malformed receipt is
// refused with an error naming each problem and the child keeps running; an accepted one ends
// its Exchange (step, loop.go). The finish instructions ride in place of the delegate report
// block (delegateReportBlock), and a capped child's closing Turn offers finish alone
// (toolMenu, wrapUpCalls, wrapUpDirective). And the workflow folder is the record: an item child
// books no delegate-ledger row, no retention entry, and runs no out-of-band namer — the item's
// own label is its name.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/floor"
	"github.com/airiclenz/apogee/internal/tools"
	"github.com/airiclenz/apogee/internal/workflow"
)

// workflowFinishBlock is the standing block a workflow item's child carries in place of
// DelegateReportBlock (delegateReportBlock): the same first sentence — so the one forgery fence,
// delegateReportFence, guards both — then what the child's reply is for here: nothing but the
// receipt it hands to finish and the output file it writes. A var only because it is composed
// from that fence; its text never changes, so it is prefix-cache-stable like the block it replaces.
var workflowFinishBlock = delegateReportFence + " It cannot see this conversation or read your" +
	" replies: it receives only the receipt you hand back through the finish tool and the output" +
	" file your task names, so anything in neither is lost." +
	"\n\nDo the task, write the detail to that output file, then call finish once: status ok when" +
	" the item is done, partial when you got part of the way, blocked when you could not proceed," +
	" with a one-line summary and the fields finish asks for. If finish refuses your receipt, fix" +
	" what it names and call it again. The call ends your run."

// wrapUpFinishDirectiveFormat is the closing directive a CAPPED workflow item's child is handed
// in place of wrapUpDirectiveFormat and its two siblings: its closing Turn keeps finish alone, so
// the ask is the receipt, never a report. %s names the bound that tripped (wrapUpFinishCause). It
// carries wrapUpMarker verbatim, because the system-prompt fallback's idempotency keys on it.
const wrapUpFinishDirectiveFormat = "You have reached %s for this item: " + wrapUpMarker +
	" but one call to finish." +
	"\n\nCall finish now with your receipt — status partial when the work is unfinished, blocked" +
	" when you could not proceed — and a summary saying where you stopped. Do not continue the task."

// The bound phrases wrapUpFinishDirectiveFormat opens with, one per delegate bound (capHit).
const (
	wrapUpFinishStepCause  = "the step limit (%d steps)"
	wrapUpFinishTokenCause = "the token budget (%d tokens)"
	wrapUpFinishTimeCause  = "the time limit (%s)"
)

// workflowOutputLineFormat is the line the spawner adds to an item child's task when the rendered
// brief does not already name the item's output file, so the child always knows where its detail
// goes. %s is the path.
const workflowOutputLineFormat = "Write your detail output to %s."

// workflowContinueInstructions is what a continued item child (ItemSpec.Prior) is asked under the
// continuation head, after the brief and the earlier rounds (continuationTask).
const workflowContinueInstructions = "Continue this item from where the earlier round stopped —" +
	" the output file may already hold its partial work — and call finish when it is done."

// workflowReceiptLineFormat leads a continued round's report with the receipt that round left.
const workflowReceiptLineFormat = "receipt left: %s — %s"

// The finished-phase results an item child's block closes on, for a Driver to paint.
const (
	workflowReceiptResultFormat = "%s — %s"
	workflowNoReceiptResult     = "ended without calling finish"
	workflowCappedNoReceipt     = "reached its bound without a receipt"
	workflowStoppedResult       = "stopped before finishing"
	workflowFaultResultPrefix   = "faulted: "
)

// workflowChild is a workflow item child's own state (Agent.workflowItem): the receipt its finish
// tool accepted. The tool may run on a pool worker, so the receipt is guarded.
type workflowChild struct {
	mu      sync.Mutex
	receipt *workflow.Receipt
}

// accept keeps the first well-formed receipt and reports whether this one was it.
func (w *workflowChild) accept(receipt workflow.Receipt) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.receipt != nil {
		return false
	}
	w.receipt = &receipt
	return true
}

// taken returns the accepted receipt, or nil.
func (w *workflowChild) taken() *workflow.Receipt {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.receipt
}

// workflowFinished reports whether this Agent is a workflow item's child whose finish call has
// been accepted — the fact step ends the Exchange on.
func (a *Agent) workflowFinished() bool {
	return a.workflowItem != nil && a.workflowItem.taken() != nil
}

// finishMenu is the capped closing Turn's menu for a workflow item's child: finish alone.
func (a *Agent) finishMenu() []domain.ToolDef {
	finish, ok := a.lookupTool(tools.FinishToolName)
	if !ok {
		return nil
	}
	return []domain.ToolDef{{
		Name:        finish.Name(),
		Description: finish.Description(),
		Schema:      finish.Schema(),
		ReadOnly:    domain.IsReadOnly(finish),
	}}
}

// finishCalls is the capped closing Turn's call filter for a workflow item's child: the finish
// calls, and nothing else.
func finishCalls(calls []domain.ToolCall) []domain.ToolCall {
	var kept []domain.ToolCall
	for _, call := range calls {
		if call.Tool == tools.FinishToolName {
			kept = append(kept, call)
		}
	}
	return kept
}

// wrapUpFinishDirective renders the capped closing directive for a workflow item's child, naming
// the bound capHit records with its applied value, as wrapUpDirective does for a delegate.
func (a *Agent) wrapUpFinishDirective() string {
	var cause string
	switch a.capHit {
	case boundTokens:
		cause = fmt.Sprintf(wrapUpFinishTokenCause, a.tokenCap)
	case boundTime:
		cause = fmt.Sprintf(wrapUpFinishTimeCause, boundDurationText(a.timeCap))
	default:
		cause = fmt.Sprintf(wrapUpFinishStepCause, a.stepCap)
	}
	return fmt.Sprintf(wrapUpFinishDirectiveFormat, cause)
}

// workflowSpawner runs one workflow's item children for the Agent that runs the workflow, under
// the call that asked for it: turn and call stamp every child's phase events and head its block,
// so a Driver brackets the children under that one call as it brackets a sub_agent's. prompts is
// where a stage's `prompt:` file is read from; nil reads it from the workspace. children is the
// registry a running child is addressable and stoppable through — the parent's own, except for a
// background workflow, whose parent is a launch-time snapshot (backgroundHost) while its children
// stay listed on the top-level Agent the Driver addresses. seat is the Delegation seat every child
// is built on (newChildAgentOn): the one the fan_out call's `run_on` named, seatConfigured — the
// zero — when it named none. observer, when set (observeWorkflow), is told of every item child's
// run id as it is minted.
type workflowSpawner struct {
	parent   *Agent
	turn     int
	call     domain.ToolCall
	prompts  fs.FS
	children *childRegistry
	seat     delegationSeat
	observer *workflowObserver
	// fellBack is set once any item child this spawner built asked for the Sub-agent server and was
	// built on the session server instead (Agent.seatFallback, ADR 0069 decision 9): the fact the
	// workflow's answer — or, run in the background, its finish note — adds its one
	// SeatFallbackNote line on (workflowAnswer, finishNote). Items spawn concurrently, so it is
	// atomic.
	fellBack atomic.Bool
}

// newWorkflowSpawner returns the Spawner for one workflow this Agent runs under call, in turn, on
// seatConfigured; a caller whose call named a seat sets it on the returned spawner.
func (a *Agent) newWorkflowSpawner(turn int, call domain.ToolCall, prompts fs.FS) *workflowSpawner {
	return &workflowSpawner{parent: a, turn: turn, call: call, prompts: prompts, children: &a.children}
}

var _ workflow.Spawner = (*workflowSpawner)(nil)

// Spawn runs one item child to its end and reports how it ended (workflow.Spawner). It is the
// workflow's recursion point and its one recover boundary: a panic anywhere in the child's life
// is reported as a fault. An error is a child that could not run at all — the depth bound, an
// unreadable prompt file, an unknown tool name, a failed construction — which the Runner treats
// as a fault; a cancel of ctx before the child starts is EndStopped with no child built.
func (s *workflowSpawner) Spawn(ctx context.Context, spec workflow.ItemSpec) (outcome workflow.Outcome, err error) {
	a := s.parent
	if a.depth >= a.maxDepth() {
		return workflow.Outcome{}, errors.New(depthLimitReason(a.maxDepth()))
	}
	task, err := s.task(spec)
	if err != nil {
		return workflow.Outcome{}, err
	}
	var narrowed []string
	if len(spec.Stage.Tools) > 0 {
		if narrowed, err = a.requestedChildTools(tools.SubAgentRoster{Names: spec.Stage.Tools}); err != nil {
			return workflow.Outcome{}, err
		}
	}
	if ctx.Err() != nil {
		return workflow.Outcome{Ending: workflow.EndStopped}, nil
	}

	runID := a.runIDs.mint()
	if s.observer != nil {
		s.observer.itemStarted(spec, runID)
	}
	stepCap, _ := resolveStepCap(a.cfg.Delegation.MaxSteps, 0)
	a.emitSubAgentPhase(s.turn, s.call, runID, domain.SubAgentPhaseEvent{
		Phase:   domain.SubAgentStarted,
		StepCap: stepCap,
	})
	// Whether the child below fell back from the Sub-agent server; read by the finished phase.
	seatFallback := false
	// Registered FIRST so it runs LAST: the finished phase closes the block on whatever outcome
	// the frame ends with, a recovered panic's included.
	defer func() {
		if r := recover(); r != nil {
			a.cfg.Events.Emit(domain.ErrorEvent{
				EventBase: a.base(s.turn),
				Source:    s.call.Tool,
				Err:       fmt.Sprintf("panic: %v", r),
			})
			outcome, err = workflow.Outcome{Ending: workflow.EndFaulted, Report: fmt.Sprintf("panic: %v", r)}, nil
		}
		a.emitSubAgentPhase(s.turn, s.call, runID, domain.SubAgentPhaseEvent{
			Phase:  domain.SubAgentFinished,
			Result: workflowPhaseResult(s.call.ID, outcome, err, seatFallback),
		})
	}()

	sub, err := a.newChildAgentOn(s.seat, s.call.ID, runID, task, delegationName(spec.Item.Label))
	if err != nil {
		return workflow.Outcome{}, fmt.Errorf("could not construct the item's child: %w", err)
	}
	// newChildAgentOn decided the fallback at the one place that saw both the ask and the latch.
	if seatFallback = sub.seatFallback; seatFallback {
		s.fellBack.Store(true)
	}
	item := &workflowChild{}
	sub.workflowItem = item
	sub.tools = withFinish(sub.tools, narrowed, tools.NewFinish(spec.Stage.Returns, item.accept))
	var ending workflow.Ending
	defer func() {
		s.children.unregister(runID)
		sub.reportUndelivered(sub.turns.index, sub.mailbox.close(), undeliveredWorkflowReason(ending))
		_ = sub.Close()
	}()

	if err := sub.Submit(domain.UserInput{Text: task, FileRefs: spec.Stage.Context}); err != nil {
		return workflow.Outcome{}, fmt.Errorf("could not start the item's child: %w", err)
	}
	// Addressable and stoppable while it runs, as a sub_agent child is (ADR 0063, ADR 0086 D4): a
	// human's stop of one item ends that child as stopped, and the Runner restarts it on resume.
	s.children.register(runID, sub)
	childCtx, stopRun := context.WithCancelCause(ctx)
	defer stopRun(nil)
	s.children.arm(runID, childCtx, stopRun)
	res, runErr := sub.Run(childCtx)
	s.children.disarm(runID)

	stopped := runErr == nil && (res.Status == domain.StatusCancelled || (res.Faulted && sub.capFold == "")) &&
		(ctx.Err() != nil || errors.Is(context.Cause(childCtx), errDelegationStopped))
	outcome = sub.workflowOutcome(res, runErr, stopped)
	outcome.Transcript = sub.conv.Messages()
	ending = outcome.Ending
	return outcome, nil
}

// task composes an item child's opening task: the spec's brief, the stage's prompt file rendered
// after it when the stage names one, a line naming the output file when neither does, and — for
// a continuation — the earlier rounds and the continuation ask laid in the ADR 0086 way
// (continuationTask).
func (s *workflowSpawner) task(spec workflow.ItemSpec) (string, error) {
	brief := spec.Brief
	if spec.Stage.Prompt != "" {
		body, err := s.readPrompt(spec.Stage.Prompt)
		if err != nil {
			return "", err
		}
		// The placeholders a stage's task renders with (workflow's renderBrief): {item} is the
		// item's entries, {out} its output path.
		rendered := strings.NewReplacer(
			"{item}", strings.Join(spec.Item.Units, ", "),
			"{out}", spec.Output,
		).Replace(body)
		brief = joinParagraphs(brief, rendered)
	}
	if spec.Output != "" && !strings.Contains(brief, spec.Output) {
		brief = joinParagraphs(brief, fmt.Sprintf(workflowOutputLineFormat, spec.Output))
	}
	if len(spec.Prior) == 0 {
		return brief, nil
	}
	prior := retainedDelegate{task: brief}
	for i, round := range spec.Prior {
		report := round.Report
		if round.Receipt != nil {
			report = joinParagraphs(
				fmt.Sprintf(workflowReceiptLineFormat, round.Receipt.Status, round.Receipt.Summary), report)
		}
		kept := delegateRound{report: report, summary: true}
		if i > 0 {
			kept.instructions = workflowContinueInstructions
		}
		prior.rounds = append(prior.rounds, kept)
	}
	return continuationTask(prior, workflowContinueInstructions), nil
}

// readPrompt reads a stage's prompt file from the spawner's prompt source, else from the
// workspace. The path must name a file inside that source: fs.FS refuses an absolute path and
// every `..` climb.
func (s *workflowSpawner) readPrompt(name string) (string, error) {
	source := s.prompts
	if source == nil {
		root := s.parent.cfg.WorkspaceDir
		if root == "" {
			return "", fmt.Errorf("prompt file %q: no workspace to read it from", name)
		}
		source = os.DirFS(root)
	}
	clean := path.Clean(filepath.ToSlash(name))
	if !fs.ValidPath(clean) {
		return "", fmt.Errorf("prompt file %q is not a path inside the workspace", name)
	}
	body, err := fs.ReadFile(source, clean)
	if err != nil {
		return "", fmt.Errorf("prompt file %q: %w", name, err)
	}
	return string(body), nil
}

// joinParagraphs joins the non-empty parts with a blank line between them.
func joinParagraphs(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, "\n\n")
}

// withFinish returns the child's roster narrowed to names (nil keeps it whole) with finish added —
// on a fresh registry, so the registry the child was built with is never written. A tool the
// parent holds under the name finish is replaced: the item's receipt tool is the one that name
// means to its child.
func withFinish(roster *domain.ToolRegistry, names []string, finish domain.Tool) *domain.ToolRegistry {
	if roster == nil {
		roster = domain.NewToolRegistry()
	}
	if names == nil {
		for _, t := range roster.All() {
			names = append(names, t.Name())
		}
	}
	kept := make([]string, 0, len(names))
	for _, name := range names {
		if name != tools.FinishToolName {
			kept = append(kept, name)
		}
	}
	out := roster.Subset(kept...)
	_ = out.Register(finish) // cannot fail: the name is non-empty and was just left out
	return out
}

// workflowOutcome reads how an item child's run ended — the receiver is the CHILD. A capped run
// keeps the engine fold and its closing text as the report a continuation is seeded from; a
// faulted one keeps its cause; a completed one keeps its last words.
func (a *Agent) workflowOutcome(res domain.StepResult, err error, stopped bool) workflow.Outcome {
	receipt := a.workflowItem.taken()
	switch {
	case err != nil:
		return workflow.Outcome{Ending: workflow.EndFaulted, Report: err.Error()}
	case stopped:
		return workflow.Outcome{Ending: workflow.EndStopped}
	case res.StepCapped:
		text := a.lastVisibleText()
		return workflow.Outcome{
			Ending:  workflow.EndCapped,
			Receipt: receipt,
			Report:  a.foldWithClosing(text, floor.ClosingShapeOf(text)),
		}
	case res.Faulted:
		cause := a.turns.fault()
		if cause == "" {
			cause = subAgentFaultNoCause
		}
		return workflow.Outcome{Ending: workflow.EndFaulted, Report: cause}
	case res.Status == domain.StatusCancelled:
		return workflow.Outcome{Ending: workflow.EndStopped}
	default:
		return workflow.Outcome{Ending: workflow.EndCompleted, Receipt: receipt, Report: a.lastVisibleText()}
	}
}

// workflowPhaseResult is the result an item child's finished phase carries: its receipt's status
// and summary, or why it has none — and, for a child that asked for the Sub-agent server and ran
// on the session one (seatFallback), SeatFallbackNote as its last body line, the slot
// delegationResult gives it. The note rides this result, which Drivers read, and never the
// Receipt: receipt.json replays on resume and feeds merge manifests and `when:`.
func workflowPhaseResult(callID string, outcome workflow.Outcome, err error, seatFallback bool) domain.ToolResult {
	result := workflowPhaseBody(callID, outcome, err)
	if seatFallback {
		result.Content += "\n" + SeatFallbackNote
	}
	return result
}

// workflowPhaseBody is workflowPhaseResult before its note: the receipt's status and summary, or
// why there is none.
func workflowPhaseBody(callID string, outcome workflow.Outcome, err error) domain.ToolResult {
	switch {
	case err != nil:
		return errorToolResult(callID, workflowFaultResultPrefix+err.Error())
	case outcome.Ending == workflow.EndFaulted:
		return errorToolResult(callID, workflowFaultResultPrefix+outcome.Report)
	case outcome.Ending == workflow.EndStopped:
		return domain.ToolResult{CallID: callID, Content: workflowStoppedResult}
	case outcome.Receipt != nil:
		return domain.ToolResult{CallID: callID, Content: fmt.Sprintf(workflowReceiptResultFormat,
			outcome.Receipt.Status, outcome.Receipt.Summary)}
	case outcome.Ending == workflow.EndCapped:
		return domain.ToolResult{CallID: callID, Content: workflowCappedNoReceipt}
	default:
		return domain.ToolResult{CallID: callID, Content: workflowNoReceiptResult}
	}
}

// undeliveredWorkflowReason is why a message queued for an item child never landed, by how the
// child ended; a child that never ran reads as faulted.
func undeliveredWorkflowReason(ending workflow.Ending) domain.UndeliveredReason {
	switch ending {
	case workflow.EndCompleted:
		return domain.UndeliveredCompleted
	case workflow.EndCapped:
		return domain.UndeliveredCapped
	case workflow.EndStopped:
		return domain.UndeliveredCancelled
	default:
		return domain.UndeliveredFaulted
	}
}
