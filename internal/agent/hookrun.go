package agent

import (
	"context"
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
)

// Hook firing (ADR 0002/0003). At each hook point the loop dispatches the catalogued
// Mechanisms FIRST — in the registry's deterministic total order (Ordered: topo-sorted
// Before/After with a stable tiebreak by canonical ID, D4) and each under its real
// MechanismID — then the bench's experimental hooks in registration order (unchanged).
// This way the bench observes/perturbs the configured behaviour, not the other way round.
//
// All five points run that cascade through ONE runner (runHooks): ordered → Bypass gate →
// revision bracket → fire under the recover boundary → book outside it. What differs between
// the points — which working value they edit, which of them builds a LoopView, which installs
// the subprocess permit, which may short-circuit, which swallows a panic — lives in the five
// thin adapters below, each a hookPointRun the runner reads.
//
// Every hook runs under the same recover boundary, so a panicking extension degrades to a
// clean quiescent boundary instead of unwinding the host (ADR 0007); a MechanismFiredEvent
// records each ACTED fire for attribution, under the firing Mechanism's ID (a
// descriptor-less experimental hook carries the synthetic experimentalMechanismID).
//
// A catalogued Mechanism is skipped at dispatch when skipMechanism reports it off (selfreg.go):
// the Bypass gate (D5) or self-regulation (Adaptive Suppression / the Turn Budget, D2). Under
// cfg.Bypass every catalogued non-off-ramp Mechanism is skipped (proactive-nudge + response-repair
// off — ADR 0006), while the off-ramp recovery guarantees still run; self-regulation withdraws a
// Mechanism it has judged not-helpful (per-Session, exempt off-ramps bypass it). Experimental
// hooks are NEVER gated by either — they are the bench's own instruments.
//
// Fired means ACTED (R4, phase-4-review-fixes item 4): each catalogued fire is bracketed — the
// working value's Revision counter around the invocation, at all five points (Request, Response,
// Conversation, and the tool stage's ToolCallEdit / ToolResultEdit), plus a non-zero post-response
// Action — and recordFire + MechanismFiredEvent are booked only when the invocation intervened. An
// inspect-and-do-nothing invocation is not a fire (apogee-sim's FiredCounts: interventions, not
// invocations). Experimental hooks keep today's always-booked behaviour under the synthetic ID
// (bench observability). Booked fires feed the Session ledger LoopView.Fired reads and the
// next-Turn judgment (selfreg.go).

// firedAction is the Action a booked fire carries at the four hook points with no action
// vocabulary of their own; only post-response books the hook's own domain.Action.
const firedAction = "fired"

// skipUnderBypass reports whether a catalogued Mechanism is switched off by Bypass: a
// non-off-ramp catalogued Mechanism is skipped at dispatch, an off-ramp survives (D5). It
// governs only catalogued Mechanisms; experimental hooks never consult it. skipMechanism
// (selfreg.go) combines it with the self-regulation withdrawal.
//
// It reads the LIVE flag (bypassEnabled), not cfg's construction seed, so a mid-session SetBypass
// from the settings surface lands at this very next hook evaluation.
func (a *Agent) skipUnderBypass(m domain.RegisteredMechanism) bool {
	return a.bypassEnabled() && m.Descriptor.Capability != domain.CapOffRamp
}

// hookOutcome is what one hook invocation reports back to the runner, beyond the revision
// bracket the runner takes itself.
type hookOutcome struct {
	// action labels the booked fire on the MechanismFiredEvent — firedAction at the four
	// points with no action vocabulary, the hook's own domain.Action at post-response.
	action string
	// acted says the invocation intervened by its own account, whatever the revision bracket
	// saw: post-response's non-zero Action is an intervention even when resp is untouched.
	acted bool
	// stop ends the cascade after this hook, its remaining peers unfired — post-response's
	// ActionRetry, the one point where a hook may short-circuit.
	stop bool
}

// hookPointRun is one hook point's adapter: everything the shared cascade needs that differs
// between the five points. H is the point's hook interface — the runner dispatches exactly the
// registered hooks implementing it.
type hookPointRun[H any] struct {
	// at is the hook point being run, both the registry lookup key and the point a booked
	// fire is attributed to.
	at domain.HookPoint
	// revision reads the working value's Revision counter; the runner brackets every
	// invocation with it, and a changed counter IS the acted probe (R4).
	revision func() int
	// fire invokes one hook against the point's working value and reports what it did. It
	// runs inside the recover boundary, so it may also carry out the hook's decision (the
	// post-response adapter routes ActionDefer / ActionRetry there).
	fire func(ctx context.Context, hook H) (hookOutcome, error)
}

// runHooks dispatches one hook point's cascade: the catalogued Mechanisms in the registry's
// deterministic order, each past the Bypass/self-regulation gate, then the experimental hooks
// in registration order. A returned error is always errHookPanicked (a recovered panic) and
// ends the cascade; what the caller does with it is the hook point's own contract.
func runHooks[H any](a *Agent, ctx context.Context, turn int, run hookPointRun[H]) error {
	for _, m := range a.registry.Ordered(run.at) {
		if a.skipMechanism(m) {
			continue
		}
		hook, ok := m.Hook.(H)
		if !ok {
			continue
		}
		stop, err := runOneHook(a, ctx, turn, m.Descriptor.ID, hook, run)
		if err != nil {
			return err
		}
		if stop {
			return nil
		}
	}
	for _, raw := range a.registry.Experimental(run.at) {
		hook, ok := raw.(H)
		if !ok {
			continue
		}
		stop, err := runOneHook(a, ctx, turn, experimentalMechanismID, hook, run)
		if err != nil {
			return err
		}
		if stop {
			return nil
		}
	}
	return nil
}

// runOneHook fires one hook under the recover boundary and then books the fire OUTSIDE it: a
// catalogued invocation is booked only if it moved the working value's Revision or reported
// acting (R4), an experimental one always (bench observability). A recovered panic returns
// errHookPanicked and books nothing.
//
// The booking deliberately sits outside the boundary — the shape every hook point had before
// the five runners collapsed into this one. a.fired reaches the HOST's Events sink, and a sink
// that panics is the host's fault: recovered here it would surface as errHookPanicked
// attributed to the Mechanism whose hook had already returned cleanly, degrading the Turn in
// an innocent Mechanism's name. Outside the boundary it unwinds to the host untouched.
func runOneHook[H any](
	a *Agent,
	ctx context.Context,
	turn int,
	id domain.MechanismID,
	hook H,
	run hookPointRun[H],
) (bool, error) {
	out, book, err := fireOneHook(a, ctx, turn, id, hook, run)
	if err != nil {
		return false, err
	}

	if book {
		a.fired(turn, id, run.at, out.action)
	}
	return out.stop, nil
}

// fireOneHook invokes one hook inside the recover boundary and reports what it did, plus
// whether the invocation earns a booking: acted by its own account, or the revision bracket
// caught an in-place mutation (R4) — an experimental hook always. Everything the boundary must
// cover lives here, the invocation and the bracket reads around it, and nothing else does. A
// recovered panic returns errHookPanicked, an empty outcome and no booking.
func fireOneHook[H any](
	a *Agent,
	ctx context.Context,
	turn int,
	id domain.MechanismID,
	hook H,
	run hookPointRun[H],
) (out hookOutcome, book bool, err error) {
	defer a.recoverHook(turn, id, &err)()

	before := run.revision()
	out, err = run.fire(ctx, hook)
	if err != nil {
		return hookOutcome{}, false, err
	}
	return out, id == experimentalMechanismID || out.acted || run.revision() != before, nil
}

// runHistoryRewriteHooks lets each history-rewrite Mechanism/hook edit conversation state
// before the request is built (truncation, compaction). The hooks mutate a.conv directly — it
// is the history, and the point builds no LoopView. A recovered panic returns errHookPanicked
// so the Turn degrades.
func (a *Agent) runHistoryRewriteHooks(ctx context.Context, turn int) error {
	return runHooks(a, ctx, turn, hookPointRun[domain.HistoryRewriter]{
		at:       domain.HookHistoryRewrite,
		revision: a.conv.Revision,
		fire: func(ctx context.Context, hook domain.HistoryRewriter) (hookOutcome, error) {
			return hookOutcome{action: firedAction}, hook.RewriteHistory(ctx, &a.conv)
		},
	})
}

// runPreRequestHooks fires the pre-request Mechanisms/hooks against the shared req — their
// mutations compose in dispatch order — so AppendToSystem / InjectContext / SetTools reach the
// Upstream request. A recovered panic returns errHookPanicked so the Turn degrades with no
// Upstream call (no assistant message).
func (a *Agent) runPreRequestHooks(ctx context.Context, turn int, req *domain.Request) error {
	return runHooks(a, ctx, turn, hookPointRun[domain.PreRequestHook]{
		at:       domain.HookPreRequest,
		revision: req.Revision,
		fire: func(ctx context.Context, hook domain.PreRequestHook) (hookOutcome, error) {
			return hookOutcome{action: firedAction}, hook.PreRequest(ctx, req)
		},
	})
}

// runPostResponseHooks runs each post-response Mechanism/hook against resp in dispatch order.
// ActionIntercept is expressed by the hook mutating resp in place (SetText / SetToolCallArguments)
// — the loop reads resp back afterward. ActionDefer schedules its correction into the next request
// (held in conversation state so it survives a snapshot). ActionRetry asks the loop to re-call the
// Upstream and short-circuits the remaining hooks. retry reports whether a re-call was requested,
// and inject carries the retrying decision's correction text so respondAndReview can append the
// corrective exchange to the retried request (R1); err is non-nil only when a hook panicked
// (recovered).
//
// A catalogued invocation is booked when it acted (R4): a non-zero Action, or an in-place
// mutation of resp the Revision bracket catches.
func (a *Agent) runPostResponseHooks(ctx context.Context, turn int, resp *domain.Response) (retry bool, inject string, err error) {
	// Post-response is the ONE hook point whose Mechanisms may spawn a subprocess (autofix's
	// external formatters). The domain.SubprocessPermit is installed ONCE here, ahead of the
	// cascade, so every post-response hook — catalogued and experimental alike — sees the same
	// authorisation the ladder granted; outside Auto no permit is installed at all, which is the
	// refusal default (confinement-execution-contract §10).
	ctx = a.hookExecutionCtx(ctx)

	err = runHooks(a, ctx, turn, hookPointRun[domain.PostResponseHook]{
		at:       domain.HookPostResponse,
		revision: resp.Revision,
		fire: func(ctx context.Context, hook domain.PostResponseHook) (hookOutcome, error) {
			decision, fireErr := hook.PostResponse(ctx, resp)
			if fireErr != nil {
				return hookOutcome{}, fireErr
			}
			switch decision.Action {
			case domain.ActionRetry:
				// The loop re-calls the Upstream with this correction; the cascade stops here.
				retry, inject = true, decision.Inject
			case domain.ActionDefer:
				if decision.Inject != "" {
					a.conv.Defer(decision.Inject)
				}
			}
			// ActionIntercept (and the zero action): the hook already mutated resp; continue.
			return hookOutcome{
				action: string(decision.Action),
				acted:  decision.Action != "",
				stop:   decision.Action == domain.ActionRetry,
			}, nil
		},
	})
	if err != nil {
		return false, "", err
	}
	return retry, inject, nil
}

// runPreToolExecHooks fires the pre-tool-exec Mechanisms/hooks against the pending call (which
// they may reshape through the shared ToolCallEdit, so their mutations compose) and the loop
// view built once for this cascade. The edit writes through to call, so the caller executes what
// the cascade left behind. A recovered panic returns errHookPanicked so the caller skips the call
// rather than running it against a half-applied decision.
func (a *Agent) runPreToolExecHooks(ctx context.Context, turn int, call *domain.ToolCall) error {
	view := a.loopView(turn)
	edit := domain.NewToolCallEdit(call)
	return runHooks(a, ctx, turn, hookPointRun[domain.PreToolExecHook]{
		at:       domain.HookPreToolExec,
		revision: edit.Revision,
		fire: func(ctx context.Context, hook domain.PreToolExecHook) (hookOutcome, error) {
			return hookOutcome{action: firedAction}, hook.PreToolExec(ctx, edit, view)
		},
	})
}

// runPostToolResultHooks fires the post-tool-result Mechanisms/hooks against the result (which
// they may rewrite through the shared ToolResultEdit) before the model sees it, passing the
// originating call (the tool name + arguments live there, not on the result) and the loop view
// built once for this cascade. The edit writes through to result, so the loop commits what the
// cascade left behind.
func (a *Agent) runPostToolResultHooks(ctx context.Context, turn int, call domain.ToolCall, result *domain.ToolResult) {
	view := a.loopView(turn)
	edit := domain.NewToolResultEdit(result)
	// The one point that swallows the panic instead of reporting it: a recovered panic stops
	// the chain (already an ErrorEvent) and the loop proceeds with the result as-is, so there
	// is nothing for the caller to decide.
	_ = runHooks(a, ctx, turn, hookPointRun[domain.PostToolResultHook]{
		at:       domain.HookPostToolResult,
		revision: edit.Revision,
		fire: func(ctx context.Context, hook domain.PostToolResultHook) (hookOutcome, error) {
			return hookOutcome{action: firedAction}, hook.PostToolResult(ctx, call, edit, view)
		},
	})
}

// recoverHook returns a deferred closure that converts a hook panic into an ErrorEvent
// attributed to the firing Mechanism's id and signals errHookPanicked through errp — the single
// recover-at-extension-boundary primitive the runner shares across all five points (ADR 0007 /
// ADR 0002).
func (a *Agent) recoverHook(turn int, id domain.MechanismID, errp *error) func() {
	return func() {
		if r := recover(); r != nil {
			a.cfg.Events.Emit(domain.ErrorEvent{
				EventBase: a.base(turn),
				Source:    string(id),
				Err:       fmt.Sprintf("panic: %v", r),
			})
			*errp = errHookPanicked
		}
	}
}

// fired books one ACTED fire with self-regulation (the Session fire ledger LoopView.Fired
// reads, and the fired-this-Turn set the NEXT Turn's outcome judges — R3/R4) and emits a
// MechanismFiredEvent attributed to the firing Mechanism's id. The caller gates it: a
// catalogued invocation reaches here only when it intervened, while an experimental hook
// (experimentalMechanismID) is booked on every invocation.
func (a *Agent) fired(turn int, id domain.MechanismID, hook domain.HookPoint, action string) {
	a.tracker.recordFire(id)
	a.cfg.Events.Emit(domain.MechanismFiredEvent{
		EventBase: a.base(turn),
		Mechanism: id,
		Hook:      hook,
		Action:    action,
	})
}
