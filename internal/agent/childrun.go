package agent

// The CHILD-RUN LIFECYCLE: the one sequence every nested Agent's run goes through once it has been
// built and handed its task — published in a registry under its run id (register), made stoppable
// on a context of its own (arm), Run, withdrawn from stopping the moment Run returns (disarm), the
// caller's settle hook, the stop verdict, and — for a caller that keeps what a stopped child did —
// the fold of its stopped work. reapChild is the matching teardown. A sub_agent delegation
// (runSubAgent) runs on it with fold on: a stopped delegate is folded and retained so the parent
// can continue it (ADR 0086 D4). A workflow item's child (workflowSpawner.Spawn) runs on it with
// fold off: a stopped item is re-run on resume, so nothing of it is kept.
//
// The helper owns no recover frame (each caller keeps its own, so a panic is classified where the
// caller can report it) and takes the registry explicitly: a background workflow's children are
// listed on the top-level Agent the Driver addresses, not on the launch-time snapshot that runs the
// workflow (backgroundHost).
//
// The CHILD BRACKET (childBracket) is what a delegation is booked under around that lifecycle: its
// run id, the started/finished phase pair a Driver reads, the recover boundary a child's panic stops
// at (ADR 0039 D4), the ending read once, and the one ledger row. A delegation settled before its
// child started takes a lone finished phase and its row (neverStarted); one that runs takes the
// whole pair around its body (run), the child inside it run through the bracket (runChild).

import (
	"context"
	"errors"
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
)

// childRun is one child run's lifecycle inputs. The child must already hold its task (Submit
// before register): a child that could not take it is never published. onArmed, when set, runs
// once the child is addressable and stoppable and before its Run starts. settled, when set, runs
// after Run returned and its stop handle is withdrawn, before the stop verdict is read — the place
// a caller joins anything that ran beside the child. foldStopped, when set, is called with the
// parent's ctx for a child the verdict reads as stopped, and the child's mailbox is closed after
// it, its leftover returned; nil is fold off.
type childRun struct {
	registry    *childRegistry
	runID       string
	sub         *Agent
	onArmed     func(childCtx context.Context)
	settled     func()
	foldStopped func(ctx context.Context)
}

// runChild runs c.sub to its end under ctx, the parent's context, and reports the run's result,
// whether it was stopped (childRunStopped), what its mailbox still held when a stop closed it (fold
// on only; nil otherwise) and the run's error. The child's own context is cancelled before
// runChild returns, so it is gone before any caller's reaping defer runs.
func runChild(ctx context.Context, c childRun) (res domain.StepResult, stopped bool, stopLeftover []domain.UserInput, err error) {
	c.registry.register(c.runID, c.sub)
	childCtx, stopRun := context.WithCancelCause(ctx)
	defer stopRun(nil)
	c.registry.arm(c.runID, childCtx, stopRun)
	if c.onArmed != nil {
		c.onArmed(childCtx)
	}
	res, err = c.sub.Run(childCtx)
	c.registry.disarm(c.runID)
	if c.settled != nil {
		c.settled()
	}
	stopped = childRunStopped(ctx, childCtx, res, err, c.sub.capFold)
	if stopped && c.foldStopped != nil {
		c.foldStopped(ctx)
		stopLeftover = c.sub.mailbox.close()
	}
	return res, stopped, stopLeftover, err
}

// childRunStopped is the stop verdict of a child run. A STOP is read where something cut the Run
// short — it returned cancelled, or faulted with finishAtFault's fold cancelled under it (capFold
// still empty) — and that something is either the human's stop (errDelegationStopped, the cause of
// the child's own context) or the cancel of the parent's ctx, which reaches every running child
// and is answered the same way (ADR 0088 D2). A stop or cancel that landed as the child finished
// leaves the finished result standing.
func childRunStopped(ctx, childCtx context.Context, res domain.StepResult, err error, capFold string) bool {
	return err == nil && (res.Status == domain.StatusCancelled || (res.Faulted && capFold == "")) &&
		(ctx.Err() != nil || errors.Is(context.Cause(childCtx), errDelegationStopped))
}

// reapChild withdraws a finished child run: it leaves the registry, so it is no longer
// addressable, and its mailbox closes, refusing everything after. What the mailbox still held is
// RETURNED, never reported — only the caller knows how the run ended and so why it did not land.
// With fold on a stop already closed the mailbox (runChild), so the second close yields nothing.
func reapChild(registry *childRegistry, runID string, sub *Agent) []domain.UserInput {
	registry.unregister(runID)
	return sub.mailbox.close()
}

// childBracket is ONE delegation's bracket, opened by its host — the Agent that spawned it and emits
// its phases (openBracket). It owns the delegation's run id, the started/finished phase pair, the
// recover boundary a panic in the child's life stops at (ADR 0039 D4: a recovered child is an
// error result its siblings and the parent Exchange survive), the ending, decided once, and the
// one ledger row. Its body runs on the calling goroutine and fills the rest through the bracket:
// the child's run (runChild), what its mailbox still held (reap) and the row's label and target
// (rowName, rowTarget).
//
// It is a value of the one frame that opened it, never a field on Agent (ADR 0083 part 3).
type childBracket struct {
	host *Agent
	turn int
	call domain.ToolCall
	// runID points at the delegation's run id — the dispatch slot's, where the call has one — so the
	// id the run path mints is written back where the slot's later readers find it.
	runID *string
	// ledger is the delegate ledger the row is booked in; nil books no row. spawnIndex is the index
	// the bracket took from it as it opened.
	ledger     *delegationLedger
	spawnIndex int
	// rowName is the label the row books — the child's display name once its run is read; "" falls
	// back to the call's own label (delegationLabel). rowTarget is the child's RESOLVED output
	// target, "" for none.
	rowName   string
	rowTarget string
	// hasStarted is set once the child's Run is reached, isStopped once the stop verdict reads the
	// run as stopped (runChild).
	hasStarted bool
	isStopped  bool
	// reportLeftover reports what the child's mailbox still held as undelivered under the ending's
	// reason (reap); nil while nothing was reaped.
	reportLeftover func(domain.UndeliveredReason)
}

// openBracket opens the bracket of one delegation answering call, its phases stamped with turn.
// The spawn index is taken from ledger at once (delegationLedger.open): the one a pooled group
// reserved for this call in call order, else the next — never reserved anew. runID is the slot's
// run id, read where it already holds one; a run id is MINTED only on the run path (mintRunID), so
// a delegation settled before it started never takes one. A nil ledger books no row.
func (a *Agent) openBracket(turn int, call domain.ToolCall, runID *string, ledger *delegationLedger) *childBracket {
	b := &childBracket{host: a, turn: turn, call: call, runID: runID, ledger: ledger}
	if ledger != nil {
		b.spawnIndex = ledger.open(call.ID)
	}
	return b
}

// mintRunID returns the delegation's run id, minting it first — and writing it back through runID —
// when the slot carries none. run reads it before its started phase; a caller that must name the
// run before that phase (an observer) may read it earlier, and the run keeps the same id.
func (b *childBracket) mintRunID() string {
	if *b.runID == "" {
		*b.runID = b.host.runIDs.mint()
	}
	return *b.runID
}

// neverStarted settles a delegation whose child never started with unstarted, one of the
// unstarted endings (unstartedSkipped, unstartedPastCeiling, unstartedCancelled,
// unstartedStopped): a lone finished phase carrying the ending's result — no started phase, since
// nothing ran — then the row. It returns that result for the caller to commit.
func (b *childBracket) neverStarted(unstarted childEnding) domain.ToolResult {
	result := errorToolResult(b.call.ID, unstarted.content)
	b.emitFinished(result)
	b.bookRow(unstarted, result.Content)
	return result
}

// run runs body inside the bracket: the started phase (started, its Phase set here) under the run
// id (mintRunID), then body, which returns the result the finished phase carries and the ending it
// read. Whatever way body leaves — returned or panicked — the bracket closes in this order:
//
//  1. a panic is recovered and reported (an ErrorEvent naming it), and onPanic gives the result
//     the path answers a panicked delegation with — each path keeps its own text;
//  2. the ending is decided (settle) — a delegation whose child never ran reads as refused;
//  3. the row is booked;
//  4. what the child's mailbox still held is reported undelivered, under the ending's reason;
//  5. the finished phase closes the pair.
//
// It returns the result and the decided ending.
func (b *childBracket) run(started domain.SubAgentPhaseEvent, onPanic func(recovered any) domain.ToolResult, body func() (domain.ToolResult, childEnding)) (result domain.ToolResult, ending childEnding) {
	started.Phase = domain.SubAgentStarted
	b.host.emitSubAgentPhase(b.turn, b.call, b.mintRunID(), started)
	defer func() {
		recovered := recover()
		isPanicked := recovered != nil
		if isPanicked {
			b.host.cfg.Events.Emit(domain.ErrorEvent{
				EventBase: b.host.base(b.turn),
				Source:    b.call.Tool,
				Err:       fmt.Sprintf("panic: %v", recovered),
			})
			result = onPanic(recovered)
		}
		ending = b.settle(ending, isPanicked)
		b.bookRow(ending, result.Content)
		if b.reportLeftover != nil {
			b.reportLeftover(ending.undelivered())
		}
		b.emitFinished(result)
	}()
	return body()
}

// settle decides the ending body read, against what the bracket saw: a cancel first — an
// unstarted one when the child never ran — then the stop verdict, which outranks every state the
// stop left the run in, then an error ending (a fault, a refusal or a panic), which is a refusal
// when the child never ran and a fault when it did, and otherwise the ending body read. A panic
// returned no ending, so it is never a cancel.
func (b *childBracket) settle(ending childEnding, isPanicked bool) childEnding {
	isCancelled := !isPanicked && ending.kind == delegationCancelled
	isError := isPanicked || ending.kind == delegationFaulted || ending.kind == delegationRefused
	switch {
	case isCancelled && !b.hasStarted:
		return unstartedCancelled()
	case isCancelled:
		return childEnding{kind: delegationCancelled, hasStarted: true}
	case b.isStopped:
		return runEnding(domain.StepResult{}, nil, true)
	case isError && !b.hasStarted:
		return refusedEnding()
	case isError:
		return childEnding{kind: delegationFaulted, hasStarted: true}
	default:
		return ending
	}
}

// runChild runs c through the child-run lifecycle (runChild) inside the bracket. The child counts
// as started once c.onArmed has run — the LAST act before Run, so a panic anywhere earlier reads
// as a refusal — and the stop verdict is recorded BEFORE c.foldStopped is called, so a stopped
// child whose fold panics still reads as stopped; with fold off it is recorded as runChild returns.
func (b *childBracket) runChild(ctx context.Context, c childRun) (res domain.StepResult, stopped bool, stopLeftover []domain.UserInput, err error) {
	onArmed := c.onArmed
	c.onArmed = func(childCtx context.Context) {
		if onArmed != nil {
			onArmed(childCtx)
		}
		b.hasStarted = true
	}
	if fold := c.foldStopped; fold != nil {
		c.foldStopped = func(ctx context.Context) {
			b.isStopped = true
			fold(ctx)
		}
	}
	res, stopped, stopLeftover, err = runChild(ctx, c)
	if stopped {
		b.isStopped = true
	}
	return res, stopped, stopLeftover, err
}

// reap withdraws the bracket's child from registry (reapChild) and holds what its mailbox still
// held — stopLeftover, what a fold-on stop already took, first — for the bracket to report
// undelivered once the ending is decided: the reap runs in body's own teardown, before the ending
// exists, and only the ending says why the mail never landed.
func (b *childBracket) reap(registry *childRegistry, sub *Agent, stopLeftover []domain.UserInput) {
	leftover, turn := append(stopLeftover, reapChild(registry, *b.runID, sub)...), sub.turns.index
	b.reportLeftover = func(reason domain.UndeliveredReason) {
		sub.reportUndelivered(turn, leftover, reason)
	}
}

// bookRow books the delegation's one row in the bracket's ledger, when it keeps one: ending's
// outcome and, for a refusal or a fault, the head line of resultContent as its cause.
func (b *childBracket) bookRow(ending childEnding, resultContent string) {
	if b.ledger == nil {
		return
	}
	b.ledger.record(ending.ledgerRecord(delegationRecord{
		spawnIndex: b.spawnIndex,
		callID:     b.call.ID,
		name:       delegationLabel(b.rowName, b.call),
		outputPath: b.rowTarget,
	}, resultContent))
}

// emitFinished emits the finished phase carrying result, under the delegation's run id as it
// stands — "" for a delegation that never started and never had one.
func (b *childBracket) emitFinished(result domain.ToolResult) {
	b.host.emitSubAgentPhase(b.turn, b.call, *b.runID, domain.SubAgentPhaseEvent{Phase: domain.SubAgentFinished, Result: result})
}
