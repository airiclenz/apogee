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
// Every hook runs under the same recover boundary, so a panicking extension degrades to a
// clean quiescent boundary instead of unwinding the host (ADR 0007); a MechanismFiredEvent
// records each successful fire for attribution, under the firing Mechanism's ID (a
// descriptor-less experimental hook carries the synthetic experimentalMechanismID).
//
// A catalogued Mechanism is skipped at dispatch when skipMechanism reports it off (selfreg.go):
// the Bypass gate (D5) or self-regulation (Adaptive Suppression / the Turn Budget, D2). Under
// cfg.Bypass every catalogued non-off-ramp Mechanism is skipped (proactive-nudge + response-repair
// off — ADR 0006), while the off-ramp recovery guarantees still run; self-regulation withdraws a
// Mechanism it has judged not-helpful (per-Session, exempt off-ramps bypass it). Experimental
// hooks are NEVER gated by either — they are the bench's own instruments. Each successful fire is
// booked through fired (the Session ledger LoopView.Fired reads + the productivity judgment input).

// skipUnderBypass reports whether a catalogued Mechanism is switched off by Bypass: a
// non-off-ramp catalogued Mechanism is skipped at dispatch, an off-ramp survives (D5). It
// governs only catalogued Mechanisms; experimental hooks never consult it. skipMechanism
// (selfreg.go) combines it with the self-regulation withdrawal.
func (a *Agent) skipUnderBypass(m domain.Mechanism) bool {
	return a.cfg.Bypass && m.Descriptor().Capability != domain.CapOffRamp
}

// runHistoryRewriteHooks lets each history-rewrite Mechanism/hook edit conversation state
// before the request is built (truncation, compaction). The hooks mutate a.conv directly — it
// is the history. A recovered panic returns errHookPanicked so the Turn degrades.
func (a *Agent) runHistoryRewriteHooks(ctx context.Context, turn int) error {
	for _, m := range a.registry.Ordered(domain.HookHistoryRewrite) {
		if a.skipMechanism(m) {
			continue
		}
		hook, ok := m.(domain.HistoryRewriter)
		if !ok {
			continue
		}
		id := m.Descriptor().ID
		if err := a.fireHistoryRewrite(ctx, id, hook, turn); err != nil {
			return err
		}
		a.fired(turn, id, domain.HookHistoryRewrite, "fired")
	}
	for _, raw := range a.registry.Experimental(domain.HookHistoryRewrite) {
		hook, ok := raw.(domain.HistoryRewriter)
		if !ok {
			continue
		}
		if err := a.fireHistoryRewrite(ctx, experimentalMechanismID, hook, turn); err != nil {
			return err
		}
		a.fired(turn, experimentalMechanismID, domain.HookHistoryRewrite, "fired")
	}
	return nil
}

func (a *Agent) fireHistoryRewrite(ctx context.Context, id domain.MechanismID, hook domain.HistoryRewriter, turn int) (err error) {
	defer a.recoverHook(turn, id, &err)()
	return hook.RewriteHistory(ctx, &a.conv)
}

// runPreRequestHooks fires the pre-request Mechanisms/hooks against the shared req — their
// mutations compose in dispatch order — so AppendToSystem / InjectContext / SetTools reach the
// Upstream request. A recovered panic returns errHookPanicked so the Turn degrades with no
// Upstream call (no assistant message).
func (a *Agent) runPreRequestHooks(ctx context.Context, turn int, req *domain.Request) error {
	for _, m := range a.registry.Ordered(domain.HookPreRequest) {
		if a.skipMechanism(m) {
			continue
		}
		hook, ok := m.(domain.PreRequestHook)
		if !ok {
			continue
		}
		id := m.Descriptor().ID
		if err := a.firePreRequest(ctx, id, hook, turn, req); err != nil {
			return err
		}
		a.fired(turn, id, domain.HookPreRequest, "fired")
	}
	for _, raw := range a.registry.Experimental(domain.HookPreRequest) {
		hook, ok := raw.(domain.PreRequestHook)
		if !ok {
			continue
		}
		if err := a.firePreRequest(ctx, experimentalMechanismID, hook, turn, req); err != nil {
			return err
		}
		a.fired(turn, experimentalMechanismID, domain.HookPreRequest, "fired")
	}
	return nil
}

func (a *Agent) firePreRequest(ctx context.Context, id domain.MechanismID, hook domain.PreRequestHook, turn int, req *domain.Request) (err error) {
	defer a.recoverHook(turn, id, &err)()
	return hook.PreRequest(ctx, req)
}

// runPostResponseHooks runs each post-response Mechanism/hook against resp in dispatch order.
// ActionIntercept is expressed by the hook mutating resp in place (SetText / SetToolCallArguments)
// — the loop reads resp back afterward. ActionDefer schedules its correction into the next request
// (held in conversation state so it survives a snapshot). ActionRetry asks the loop to re-call the
// Upstream and short-circuits the remaining hooks. retry reports whether a re-call was requested;
// err is non-nil only when a hook panicked (recovered).
func (a *Agent) runPostResponseHooks(ctx context.Context, turn int, resp *domain.Response) (retry bool, err error) {
	for _, m := range a.registry.Ordered(domain.HookPostResponse) {
		if a.skipMechanism(m) {
			continue
		}
		hook, ok := m.(domain.PostResponseHook)
		if !ok {
			continue
		}
		wantRetry, fireErr := a.applyPostResponse(ctx, turn, m.Descriptor().ID, hook, resp)
		if fireErr != nil {
			return false, fireErr
		}
		if wantRetry {
			return true, nil
		}
	}
	for _, raw := range a.registry.Experimental(domain.HookPostResponse) {
		hook, ok := raw.(domain.PostResponseHook)
		if !ok {
			continue
		}
		wantRetry, fireErr := a.applyPostResponse(ctx, turn, experimentalMechanismID, hook, resp)
		if fireErr != nil {
			return false, fireErr
		}
		if wantRetry {
			return true, nil
		}
	}
	return false, nil
}

// applyPostResponse fires one post-response hook, emits its MechanismFiredEvent, and carries out
// its decision: ActionRetry short-circuits the cascade (retry=true), ActionDefer schedules its
// correction into the next request, and ActionIntercept (and the zero action) leaves the hook's
// in-place mutation of resp untouched. err is non-nil only when the hook panicked (recovered).
func (a *Agent) applyPostResponse(ctx context.Context, turn int, id domain.MechanismID, hook domain.PostResponseHook, resp *domain.Response) (retry bool, err error) {
	decision, fireErr := a.firePostResponse(ctx, id, hook, turn, resp)
	if fireErr != nil {
		return false, fireErr
	}
	a.fired(turn, id, domain.HookPostResponse, string(decision.Action))
	switch decision.Action {
	case domain.ActionRetry:
		return true, nil
	case domain.ActionDefer:
		if decision.Inject != "" {
			a.conv.Defer(decision.Inject)
		}
	}
	// ActionIntercept (and the zero action): the hook already mutated resp; continue.
	return false, nil
}

func (a *Agent) firePostResponse(ctx context.Context, id domain.MechanismID, hook domain.PostResponseHook, turn int, resp *domain.Response) (decision domain.PostResponseDecision, err error) {
	defer a.recoverHook(turn, id, &err)()
	return hook.PostResponse(ctx, resp)
}

// runPreToolExecHooks fires the pre-tool-exec Mechanisms/hooks against the pending call (which
// they may mutate) and the loop view. A recovered panic returns errHookPanicked so the caller
// skips the call rather than running it against a half-applied decision.
func (a *Agent) runPreToolExecHooks(ctx context.Context, turn int, call *domain.ToolCall) error {
	view := a.loopView(turn)
	for _, m := range a.registry.Ordered(domain.HookPreToolExec) {
		if a.skipMechanism(m) {
			continue
		}
		hook, ok := m.(domain.PreToolExecHook)
		if !ok {
			continue
		}
		id := m.Descriptor().ID
		if err := a.firePreToolExec(ctx, id, hook, turn, call, view); err != nil {
			return err
		}
		a.fired(turn, id, domain.HookPreToolExec, "fired")
	}
	for _, raw := range a.registry.Experimental(domain.HookPreToolExec) {
		hook, ok := raw.(domain.PreToolExecHook)
		if !ok {
			continue
		}
		if err := a.firePreToolExec(ctx, experimentalMechanismID, hook, turn, call, view); err != nil {
			return err
		}
		a.fired(turn, experimentalMechanismID, domain.HookPreToolExec, "fired")
	}
	return nil
}

func (a *Agent) firePreToolExec(ctx context.Context, id domain.MechanismID, hook domain.PreToolExecHook, turn int, call *domain.ToolCall, view domain.LoopView) (err error) {
	defer a.recoverHook(turn, id, &err)()
	return hook.PreToolExec(ctx, call, view)
}

// runPostToolResultHooks fires the post-tool-result Mechanisms/hooks against the result (which
// they may mutate) before the model sees it, passing the originating call (the tool name +
// arguments live there, not on the result) and the loop view. A recovered panic stops the chain
// and the loop proceeds with the result as-is.
func (a *Agent) runPostToolResultHooks(ctx context.Context, turn int, call domain.ToolCall, result *domain.ToolResult) {
	view := a.loopView(turn)
	for _, m := range a.registry.Ordered(domain.HookPostToolResult) {
		if a.skipMechanism(m) {
			continue
		}
		hook, ok := m.(domain.PostToolResultHook)
		if !ok {
			continue
		}
		id := m.Descriptor().ID
		if err := a.firePostToolResult(ctx, id, hook, turn, call, result, view); err != nil {
			return
		}
		a.fired(turn, id, domain.HookPostToolResult, "fired")
	}
	for _, raw := range a.registry.Experimental(domain.HookPostToolResult) {
		hook, ok := raw.(domain.PostToolResultHook)
		if !ok {
			continue
		}
		if err := a.firePostToolResult(ctx, experimentalMechanismID, hook, turn, call, result, view); err != nil {
			return
		}
		a.fired(turn, experimentalMechanismID, domain.HookPostToolResult, "fired")
	}
}

func (a *Agent) firePostToolResult(ctx context.Context, id domain.MechanismID, hook domain.PostToolResultHook, turn int, call domain.ToolCall, result *domain.ToolResult, view domain.LoopView) (err error) {
	defer a.recoverHook(turn, id, &err)()
	return hook.PostToolResult(ctx, call, result, view)
}

// recoverHook returns a deferred closure that converts a hook panic into an ErrorEvent
// attributed to the firing Mechanism's id and signals errHookPanicked through errp — the single
// recover-at-extension-boundary primitive every fire* helper shares (ADR 0007 / ADR 0002).
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

// fired books a successful fire with self-regulation (the Session fire ledger LoopView.Fired
// reads, and the fired-this-Turn set the Turn's productivity judges — plan item 3) and emits a
// MechanismFiredEvent attributed to the firing Mechanism's id (experimentalMechanismID for a
// descriptor-less experimental hook).
func (a *Agent) fired(turn int, id domain.MechanismID, hook domain.HookPoint, action string) {
	a.tracker.recordFire(id)
	a.cfg.Events.Emit(domain.MechanismFiredEvent{
		EventBase: a.base(turn),
		Mechanism: id,
		Hook:      hook,
		Action:    action,
	})
}
