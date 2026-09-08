package agent

import (
	"context"
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
)

// The registry BRIDGE (ADR 0002/0003; ADR 0076 D1, stage 1). The Reaction dispatcher's third leg:
// after the engine's builtins and the Reactions armed on Config.Reactions, every seam Moment runs
// the catalogued Mechanisms still held in the registry — in its deterministic total order (Ordered:
// topo-sorted Before/After with a stable tiebreak by canonical ID, D4) and each under its real
// MechanismID — then the bench's experimental hooks in registration order. This way the bench
// observes/perturbs the configured behaviour, not the other way round.
//
// It is a BRIDGE and not a home: the lab layer it serves is retired, and this leg dies with it
// (stage 1 deletes the registry once the seams no longer need it). Until then the cascade below is
// unchanged in substance — ordered → Bypass/self-regulation gate → revision bracket → fire under
// the recover boundary → book outside it — and the dispatcher supplies what differs between the
// five Moments (the working value, the LoopView, the subprocess permit) from the payload it already
// resolved, so a registry row sees exactly what it saw when the seams called it directly.
//
// Every hook runs under the same recover boundary, so a panicking extension degrades to a
// clean quiescent boundary instead of unwinding the host (ADR 0007); a ReactionFiredEvent
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
// Action — and recordFire + the firing event are booked only when the invocation intervened. An
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
	// action labels the booked fire on the ReactionFiredEvent — firedAction at the four
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
// between the five points. The dispatcher builds it per Moment from the seam payload it resolved
// (reactions.go), so the runner stays seam-blind. H is the point's hook interface — the runner
// dispatches exactly the registered hooks implementing it.
type hookPointRun[H any] struct {
	// at is the hook point being run, both the registry lookup key and the point a booked
	// fire is attributed to.
	at domain.HookPoint
	// revision reads the working value's Revision counter; the runner brackets every
	// invocation with it, and a changed counter IS the acted probe (R4).
	revision func() int
	// fire invokes one hook against the point's working value and reports what it did. It
	// runs inside the recover boundary, so it may also carry out the hook's decision (the
	// post-response bridge routes ActionDefer / ActionRetry there).
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
// The booking deliberately sits outside the boundary — the shape every hook point has always had. a.fired reaches the HOST's Events sink, and a sink
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

// fired books one ACTED fire on the bridge leg: the self-regulation ledger (the Session fire
// ledger LoopView.Fired reads, and the fired-this-Turn set the NEXT Turn's outcome judges — R3/R4)
// and one ReactionFiredEvent attributed to the firing Mechanism's id. The caller gates it: a
// catalogued invocation reaches here only when it intervened, while an experimental hook
// (experimentalMechanismID) is booked on every invocation.
//
// The event is the Reaction core's ONE firing event (ADR 0076 D1), the same variant a builtin and
// an armed Reaction book (reactions.go): a registry row arrives at the same Moment as everything
// else on the ladder, so an observer should not have to know which leg it rode to read what it did.
// The Origin is engine — a catalogued Mechanism and a bench hook are both the engine's own — and
// the hook point converts to its Moment directly, the two vocabularies being the same five
// spellings.
func (a *Agent) fired(turn int, id domain.MechanismID, hook domain.HookPoint, action string) {
	a.tracker.recordFire(id)
	a.cfg.Events.Emit(domain.ReactionFiredEvent{
		EventBase: a.base(turn),
		Reaction:  string(id),
		Origin:    domain.OriginEngine,
		Moment:    domain.Moment(hook),
		Action:    action,
	})
}
