package agent

import (
	"context"
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
)

// Experimental-hook firing (ADR 0002). Each hook runs under a recover boundary so a
// panicking extension degrades to a clean quiescent boundary instead of unwinding the host
// (ADR 0007). A MechanismFiredEvent records each successful fire for bench attribution,
// under the synthetic experimentalMechanismID (a descriptor-less hook carries no real ID).
// The catalogued-Mechanism path — descriptors, self-regulation, deterministic total order —
// is Phase 4; Phase 1 drives only the experimental slots in registration order.

// runHistoryRewriteHooks lets each history-rewrite hook edit conversation state before the
// request is built (truncation, compaction). The hooks mutate a.conv directly — it is the
// history. A recovered panic returns errHookPanicked so the Turn degrades.
func (a *Agent) runHistoryRewriteHooks(ctx context.Context, turn int) error {
	for _, raw := range a.registry.Experimental(domain.HookHistoryRewrite) {
		hook, ok := raw.(domain.HistoryRewriter)
		if !ok {
			continue
		}
		if err := a.fireHistoryRewrite(ctx, hook, turn); err != nil {
			return err
		}
		a.fired(turn, domain.HookHistoryRewrite, "fired")
	}
	return nil
}

func (a *Agent) fireHistoryRewrite(ctx context.Context, hook domain.HistoryRewriter, turn int) (err error) {
	defer a.recoverHook(turn, &err)()
	return hook.RewriteHistory(ctx, &a.conv)
}

// runPreRequestHooks fires the pre-request hooks against the shared req — their mutations
// compose in registration order — so AppendToSystem / InjectContext / SetTools reach the
// Upstream request. A recovered panic returns errHookPanicked so the Turn degrades with no
// Upstream call (no assistant message).
func (a *Agent) runPreRequestHooks(ctx context.Context, turn int, req *domain.Request) error {
	for _, raw := range a.registry.Experimental(domain.HookPreRequest) {
		hook, ok := raw.(domain.PreRequestHook)
		if !ok {
			continue
		}
		if err := a.firePreRequest(ctx, hook, turn, req); err != nil {
			return err
		}
		a.fired(turn, domain.HookPreRequest, "fired")
	}
	return nil
}

func (a *Agent) firePreRequest(ctx context.Context, hook domain.PreRequestHook, turn int, req *domain.Request) (err error) {
	defer a.recoverHook(turn, &err)()
	return hook.PreRequest(ctx, req)
}

// runPostResponseHooks runs each post-response hook against resp in order. ActionIntercept
// is expressed by the hook mutating resp in place (SetText / SetToolCallArguments) — the
// loop reads resp back afterward. ActionDefer schedules its correction into the next request
// (held in conversation state so it survives a snapshot). ActionRetry asks the loop to
// re-call the Upstream and short-circuits the remaining hooks. retry reports whether a
// re-call was requested; err is non-nil only when a hook panicked (recovered).
func (a *Agent) runPostResponseHooks(ctx context.Context, turn int, resp *domain.Response) (retry bool, err error) {
	for _, raw := range a.registry.Experimental(domain.HookPostResponse) {
		hook, ok := raw.(domain.PostResponseHook)
		if !ok {
			continue
		}
		decision, fireErr := a.firePostResponse(ctx, hook, turn, resp)
		if fireErr != nil {
			return false, fireErr
		}
		a.fired(turn, domain.HookPostResponse, string(decision.Action))
		switch decision.Action {
		case domain.ActionRetry:
			return true, nil
		case domain.ActionDefer:
			if decision.Inject != "" {
				a.conv.Defer(decision.Inject)
			}
		}
		// ActionIntercept (and the zero action): the hook already mutated resp; continue.
	}
	return false, nil
}

func (a *Agent) firePostResponse(ctx context.Context, hook domain.PostResponseHook, turn int, resp *domain.Response) (decision domain.PostResponseDecision, err error) {
	defer a.recoverHook(turn, &err)()
	return hook.PostResponse(ctx, resp)
}

// runPreToolExecHooks fires the pre-tool-exec hooks against the pending call (which they may
// mutate) and the loop view. A recovered panic returns errHookPanicked so the caller skips
// the call rather than running it against a half-applied decision.
func (a *Agent) runPreToolExecHooks(ctx context.Context, turn int, call *domain.ToolCall) error {
	view := a.loopView(turn)
	for _, raw := range a.registry.Experimental(domain.HookPreToolExec) {
		hook, ok := raw.(domain.PreToolExecHook)
		if !ok {
			continue
		}
		if err := a.firePreToolExec(ctx, hook, turn, call, view); err != nil {
			return err
		}
		a.fired(turn, domain.HookPreToolExec, "fired")
	}
	return nil
}

func (a *Agent) firePreToolExec(ctx context.Context, hook domain.PreToolExecHook, turn int, call *domain.ToolCall, view domain.LoopView) (err error) {
	defer a.recoverHook(turn, &err)()
	return hook.PreToolExec(ctx, call, view)
}

// runPostToolResultHooks fires the post-tool-result hooks against the result (which they may
// mutate) before the model sees it, passing the originating call (the tool name + arguments
// live there, not on the result) and the loop view. A recovered panic stops the chain and
// the loop proceeds with the result as-is.
func (a *Agent) runPostToolResultHooks(ctx context.Context, turn int, call domain.ToolCall, result *domain.ToolResult) {
	view := a.loopView(turn)
	for _, raw := range a.registry.Experimental(domain.HookPostToolResult) {
		hook, ok := raw.(domain.PostToolResultHook)
		if !ok {
			continue
		}
		if err := a.firePostToolResult(ctx, hook, turn, call, result, view); err != nil {
			return
		}
		a.fired(turn, domain.HookPostToolResult, "fired")
	}
}

func (a *Agent) firePostToolResult(ctx context.Context, hook domain.PostToolResultHook, turn int, call domain.ToolCall, result *domain.ToolResult, view domain.LoopView) (err error) {
	defer a.recoverHook(turn, &err)()
	return hook.PostToolResult(ctx, call, result, view)
}

// recoverHook returns a deferred closure that converts a hook panic into an ErrorEvent and
// signals errHookPanicked through errp — the single recover-at-extension-boundary primitive
// every fire* helper shares (ADR 0007 / ADR 0002).
func (a *Agent) recoverHook(turn int, errp *error) func() {
	return func() {
		if r := recover(); r != nil {
			a.cfg.Events.Emit(domain.ErrorEvent{
				EventBase: a.base(turn),
				Source:    string(experimentalMechanismID),
				Err:       fmt.Sprintf("panic: %v", r),
			})
			*errp = errHookPanicked
		}
	}
}

// fired emits a MechanismFiredEvent for a successful experimental-hook fire.
func (a *Agent) fired(turn int, hook domain.HookPoint, action string) {
	a.cfg.Events.Emit(domain.MechanismFiredEvent{
		EventBase: a.base(turn),
		Mechanism: experimentalMechanismID,
		Hook:      hook,
		Action:    action,
	})
}
