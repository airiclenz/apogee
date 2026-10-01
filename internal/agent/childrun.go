package agent

// The CHILD-RUN LIFECYCLE: the one sequence every nested Agent's run goes through once it has been
// built and handed its task — published in a registry under its run id (register), made stoppable
// on a context of its own (arm), Run, withdrawn from stopping the moment Run returns (disarm), the
// caller's settle hook, the stop verdict, and — for a caller that keeps what a stopped child did —
// the fold of its stopped work. reapChild is the matching teardown. A workflow item's child
// (workflowSpawner.Spawn) runs on it with fold off: a stopped item is re-run on resume, so nothing
// of it is kept.
//
// The helper owns no recover frame (each caller keeps its own, so a panic is classified where the
// caller can report it) and takes the registry explicitly: a background workflow's children are
// listed on the top-level Agent the Driver addresses, not on the launch-time snapshot that runs the
// workflow (backgroundHost).

import (
	"context"
	"errors"

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
