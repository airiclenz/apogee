package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/floor"
	"github.com/airiclenz/apogee/internal/processing"
)

// The engine's builtin Reactions: the seven Floor guards (ADR 0071, ADR 0076 D1). Each is a
// domain.Reaction of engine origin and shape-view class, holding a thin handler that reads its
// own gate off the LIVE Floor config and then calls the unchanged internal/floor policy
// function. The policy stays in internal/floor byte-for-byte; what lives here is only the
// engine's half — when the guard is consulted, what its firing is called, and what it books.
//
// They are built into Agent.builtins at construction and are never entries of Config.Reactions:
// a builtin fires FIRST at its Moment and is never switched off by Bypass, which is the whole
// difference between the floor a model always gets and everything armed above it.
//
// Each handler reads a.floorConfig() at FIRE time rather than closing over a construction-time
// value, so SetFloor keeps switching a guard off and on mid-session with no rebuild: a guard
// switched off stops at the next Moment, one switched back on arms again, and nothing already
// corrected is undone.

// buildBuiltins returns this Agent's builtin Reactions in the order they fire. Within
// post-response the order is ratified (ADR 0071): tool-call salvage, then the tool-loop breaker,
// tool-call repair, empty-response recovery and the tool-use enforcer — the coarser "you are
// going in circles" judgment before the finer "this call is malformed" one, then the two
// recoveries for a Turn that produced no usable call at all.
//
// SALVAGE IS FIRST for a reason of its own: it does not correct the response, it COMPLETES it —
// a call the model wrote as JSON in its text is put back on the response as a call — so it asks
// for no retry and does not stop the leg. Running it first is what makes the four below judge
// the response the model MEANT, so a Turn whose only call was written into the text is answered
// by dispatching that call rather than by a retry the model did not need.
func (a *Agent) buildBuiltins() []armedReaction {
	return []armedReaction{
		engineBuiltin(guardToolCallSalvage, guardActionSalvage, domain.MomentPostResponse,
			domain.PostResponseFunc(a.salvageToolCall)),
		engineBuiltin(guardToolLoopBreaker, guardActionRetry, domain.MomentPostResponse,
			domain.PostResponseFunc(a.breakToolLoop)),
		engineBuiltin(guardToolCallRepair, guardActionRetry, domain.MomentPostResponse,
			domain.PostResponseFunc(a.repairToolCall)),
		engineBuiltin(guardEmptyResponseRecovery, guardActionRetry, domain.MomentPostResponse,
			domain.PostResponseFunc(a.recoverEmptyResponse)),
		engineBuiltin(guardToolUseEnforcer, guardActionRetry, domain.MomentPostResponse,
			domain.PostResponseFunc(a.enforceToolUse)),
		engineBuiltin(guardReadCache, guardActionIntercept, domain.MomentPreToolExec,
			domain.PreToolExecFunc(a.cacheRead)),
		engineBuiltin(guardToolResultCap, guardActionCap, domain.MomentPreRequest,
			domain.PreRequestFunc(a.capToolResults)),
	}
}

// engineBuiltin builds one builtin: an engine-origin, shape-view Reaction on the single Moment
// named, booking its firings under action. Every Floor guard is shape-view because that is what
// a guard does — it edits what the model SEES, the response it is about to be judged on or the
// request it is about to be sent.
//
// The Moment is passed rather than read off the handler because the handler's own seam is
// domain's seal, unexported outside it; Reaction.Validate re-checks the two agree, so a builtin
// wired to the wrong Moment fails the structural test rather than firing in the wrong place.
func engineBuiltin(id, action string, on domain.Moment, handler domain.Handler) armedReaction {
	return armedReaction{
		spec: domain.Reaction{
			ID:      id,
			Origin:  domain.OriginEngine,
			Class:   domain.ClassShapeView,
			On:      []domain.Moment{on},
			Handler: handler,
		},
		action: action,
	}
}

// salvageToolCall is the tool-call salvage guard (floor.SalvageToolCall): when the model wrote a
// call into its own text, it puts what it read back onto the response — the text stripped of the
// block that held the call, and one appended ToolCall per call written. It asks for no retry:
// salvage completes a response rather than correcting it.
//
// Two gates stand in front of it, both about whether reading the text as a call could be WRONG
// here rather than about whether the model stumbled:
//
//   - Only a NATIVE profile (processing.IsNative). A markdown-fenced or custom-regex profile
//     already extracts a call from the visible text at the parse seam (assembleResponse), so
//     salvaging the same text again would dispatch the call twice.
//   - Never on the WRAP-UP Turn (Agent.wrapUp). That Turn is offered no menu at all and its
//     reply is a closing report by construction: a call salvaged out of it would be a call the
//     delegation had already been told it could not make.
//
// The menu it salvages AGAINST is the REQUEST's — resp.View().Tools(), what this Turn was
// actually offered — and deliberately not registeredToolNames' whole registry, which is the
// repair guard's question. A name the menu withdrew is not a call to run: the mode that withdrew
// it owes the model its own answer, exactly as it does for a native call carrying that name.
//
// Each salvaged call is given a deterministic Turn-derived ID in the loop's own synthesized
// style (loop.go's text_call_<turn>), extended with the call's position because one Turn's text
// may hold several: snapshot, resume and tests stay stable across runs. The Detail names what
// was read back out of the text, the one fact the reaction's id cannot carry.
func (a *Agent) salvageToolCall(_ context.Context, resp *domain.Response) (domain.Outcome, error) {
	if a.floorConfig().DisableToolCallSalvage {
		return domain.Outcome{}, nil
	}
	if a.wrapUp || !processing.IsNative(a.textParser) {
		return domain.Outcome{}, nil
	}

	calls, text, fired := floor.SalvageToolCall(resp, offeredToolNames(resp.View()))
	if !fired {
		return domain.Outcome{}, nil
	}

	resp.SetText(text)
	names := make([]string, 0, len(calls))
	for i, call := range calls {
		call.ID = fmt.Sprintf("text_call_%d_%d", a.turns.index, i)
		resp.AppendToolCall(call)
		names = append(names, call.Tool)
	}
	return domain.Outcome{
		Edited: true,
		Detail: fmt.Sprintf("salvaged %s from content", strings.Join(names, ", ")),
	}, nil
}

// breakToolLoop is the tool-loop breaker (floor.ToolLoopBreak): a Turn repeating the previous
// Turn's tool calls verbatim is re-streamed with a directive naming what it has already tried.
func (a *Agent) breakToolLoop(_ context.Context, resp *domain.Response) (domain.Outcome, error) {
	if a.floorConfig().DisableToolLoopBreaker {
		return domain.Outcome{}, nil
	}
	directive, fired := floor.ToolLoopBreak(resp)
	if !fired {
		return domain.Outcome{}, nil
	}
	return domain.Outcome{Retry: true, Inject: directive}, nil
}

// repairToolCall is the tool-call repair guard (floor.ToolCallRepair): a malformed or
// hallucinated call is re-streamed with a correction naming what was wrong with it. It judges
// against the whole registry (registeredToolNames), not this Turn's menu, so a call the mode
// merely WITHDREW is left for that mode to answer.
func (a *Agent) repairToolCall(_ context.Context, resp *domain.Response) (domain.Outcome, error) {
	if a.floorConfig().DisableToolCallRepair {
		return domain.Outcome{}, nil
	}
	correction, fired := floor.ToolCallRepair(resp, a.registeredToolNames())
	if !fired {
		return domain.Outcome{}, nil
	}
	return domain.Outcome{Retry: true, Inject: correction}, nil
}

// recoverEmptyResponse is the empty-response recovery (floor.RecoverEmpty): a Turn that produced
// neither text nor a call is re-streamed with a nudge rather than faulting the Exchange.
func (a *Agent) recoverEmptyResponse(_ context.Context, resp *domain.Response) (domain.Outcome, error) {
	if a.floorConfig().DisableEmptyResponseRecovery {
		return domain.Outcome{}, nil
	}
	nudge, fired := floor.RecoverEmpty(resp)
	if !fired {
		return domain.Outcome{}, nil
	}
	return domain.Outcome{Retry: true, Inject: nudge}, nil
}

// enforceToolUse is the tool-use enforcer (floor.EnforceToolUse): a model narrating a second
// consecutive text-only Turn, having never called a tool, is re-streamed with a correction
// naming the tools it was offered.
func (a *Agent) enforceToolUse(_ context.Context, resp *domain.Response) (domain.Outcome, error) {
	if a.floorConfig().DisableToolUseEnforcer {
		return domain.Outcome{}, nil
	}
	correction, fired := floor.EnforceToolUse(resp)
	if !fired {
		return domain.Outcome{}, nil
	}
	return domain.Outcome{Retry: true, Inject: correction}, nil
}

// cacheRead is the read cache (floor.CacheRead): a re-read of a file this Exchange already read
// unchanged is narrowed in place, so the model spends its window on what it has not seen. It
// reshapes the pending call through the shared ToolCallEdit, the same wrapper every pre-tool-exec
// reaction writes through, so the loop executes what the cascade left behind.
func (a *Agent) cacheRead(_ context.Context, view domain.LoopView, call *domain.ToolCallEdit) (domain.Outcome, error) {
	if a.floorConfig().DisableReadCache {
		return domain.Outcome{}, nil
	}
	if !floor.CacheRead(view, call) {
		return domain.Outcome{}, nil
	}
	return domain.Outcome{Edited: true}, nil
}

// capToolResults is the tool-result cap (floor.CapToolResults): oversized tool results in the
// outgoing request are elided so the request fits. It edits only the PROJECTED REQUEST — the
// conversation keeps every result whole, so a later Turn, a session snapshot and the rendered
// transcript are unaffected by what the model was spared reading again.
func (a *Agent) capToolResults(_ context.Context, req *domain.Request) (domain.Outcome, error) {
	if a.floorConfig().DisableToolResultCap {
		return domain.Outcome{}, nil
	}
	if capped := floor.CapToolResults(req); capped == 0 {
		return domain.Outcome{}, nil
	}
	return domain.Outcome{Edited: true}, nil
}

// offeredToolNames lists the tool names THIS request's menu carried, in the order the model was
// shown them. It is the salvage guard's admissible-name set, for the reason its own doc gives, and
// it is read off the Response's LoopView rather than the registry so that a mode that narrowed the
// menu narrows what can be salvaged with it.
func offeredToolNames(view domain.LoopView) []string {
	tools := view.Tools()
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	return names
}

// registeredToolNames lists every tool name the resolved registry holds, WHATEVER the current
// menu shows. It is the fact the tool-call repair guard needs to tell a hallucinated tool from one
// this request's menu withdrew — Plan mode offers only what Plan can run (toolMenu), and a
// delegate's wrap-up Turn is offered nothing at all — because a withdrawn tool's call belongs to
// the mode that withdrew it: the Plan refusal (resolution.go) or the wrap-up drop (step) is the
// answer the model must get, not a correction retry that pre-empts it.
//
// It reads a.tools on the worker goroutine, exactly as toolMenu does, and needs no lock for the
// same reason: a tool-set swap is idle-only (SwapTools). A tool-less Agent lists nothing, which
// leaves the guard its pre-2026-09-03 behaviour — every off-menu name reads as unknown.
func (a *Agent) registeredToolNames() []string {
	if a.tools == nil {
		return nil
	}
	all := a.tools.All()
	names := make([]string, 0, len(all))
	for _, t := range all {
		names = append(names, t.Name())
	}
	return names
}
