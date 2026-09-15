package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/floor"
	"github.com/airiclenz/apogee/internal/processing"
)

// The engine's builtin Reactions: the seven Floor guards (ADR 0071, ADR 0076 D1) and, each behind
// its own switch, the two engine notices — the context-fill notice (ADR 0077) and the step-budget
// notice (its 2026-09-15 addendum). Each guard is a domain.Reaction of engine
// origin and shape-view class, holding a thin handler that calls the unchanged internal/floor
// policy function. The policy stays in internal/floor byte-for-byte; what lives here is only the
// engine's half — when the guard is consulted, what its firing is called, and what it books.
//
// They are built into Agent.builtins at construction and are never entries of Config.Reactions:
// a builtin fires FIRST at its Moment. A Floor guard is never switched off by Bypass, which is
// the whole difference between the floor a model always gets and everything armed above it; the
// two notices are the builtins Bypass does switch off, because they are class advise — each
// steers the model rather than correcting what it sees — and Bypass is the promise that nothing
// of that class speaks (ADR 0077 D1, bypassSkips).
//
// The ladder is an ENABLE SET (ADR 0076 A8): a guard whose gate is off — or a notice while its
// switch is off — is ABSENT from the slice buildBuiltins returns rather than
// present-and-self-skipping, so no handler consults a Floor gate at fire time. The firing
// sequence is identical either way — a disabled guard booked nothing before — and the ladder is
// rebuilt from the live Generation whenever its Floor or a notice switch moves (SetReactions),
// so a guard switched off stops at the next Moment, one switched back on arms again, and nothing
// already corrected is undone.

// floorGuard is one row of the Floor-guard table: the guard's key (its config key and the id its
// builtin fires under, floorguards.go), the one Moment it is consulted at, the action its firing is
// booked under, the gate that reads its opt-out off the live Floor, and the handler that binds it to
// an Agent. The gate and the handler are funcs of the Floor and of the Agent rather than values
// because a package-level table can hold neither a live Generation nor a bound *Agent method: each
// row is resolved against both when the ladder is built (buildBuiltins).
type floorGuard struct {
	key     string
	moment  domain.Moment
	action  string
	gate    func(domain.FloorConfig) bool
	handler func(*Agent) domain.Handler
}

// floorGuards is the ONE table of the seven Floor guards, in the order they fire. guardIDs
// (floorguards.go) and buildBuiltins are both derived from it, and cmd/apogee's settings switch is
// pinned to its key set through the facade (FloorGuardKeys), so a guard added or renamed here is
// added or renamed everywhere it is known by name.
//
// Within post-response the order is ratified (ADR 0071): tool-call salvage, then the tool-loop
// breaker, tool-call repair, empty-response recovery and the tool-use enforcer — the coarser "you
// are going in circles" judgment before the finer "this call is malformed" one, then the two
// recoveries for a Turn that produced no usable call at all.
//
// SALVAGE IS FIRST for a reason of its own: it does not correct the response, it COMPLETES it —
// a call the model wrote as JSON in its text is put back on the response as a call — so it asks
// for no retry and does not stop the leg. Running it first is what makes the four below judge
// the response the model MEANT, so a Turn whose only call was written into the text is answered
// by dispatching that call rather than by a retry the model did not need.
//
// The four retry guards share one adapter (retryGuard) around their internal/floor policy:
//
//   - The tool-loop breaker (floor.ToolLoopBreak): a Turn repeating the previous Turn's tool calls
//     verbatim, or closing an exact A-B-A-B alternation on identical results, is re-streamed with
//     a directive naming what it has already tried.
//   - Tool-call repair (floor.ToolCallRepair): a malformed or hallucinated call is re-streamed with
//     a correction naming what was wrong with it. It judges against the whole registry
//     (registeredToolNames), not this Turn's menu, so a call the mode merely WITHDREW is left for
//     that mode to answer — which is why its row closes over the Agent where the other three take
//     the policy function bare.
//   - Empty-response recovery (floor.RecoverEmpty): a Turn that produced neither text nor a call is
//     re-streamed with a nudge rather than faulting the Exchange.
//   - The tool-use enforcer (floor.EnforceToolUse): a model narrating a second consecutive
//     text-only Turn, having never called a tool, is re-streamed with a correction naming the
//     tools it was offered.
var floorGuards = []floorGuard{
	{
		key:    guardToolCallSalvage,
		moment: domain.MomentPostResponse,
		action: guardActionSalvage,
		gate:   func(f domain.FloorConfig) bool { return f.DisableToolCallSalvage },
		handler: func(a *Agent) domain.Handler {
			return domain.PostResponseFunc(a.salvageToolCall)
		},
	},
	{
		key:    guardToolLoopBreaker,
		moment: domain.MomentPostResponse,
		action: guardActionRetry,
		gate:   func(f domain.FloorConfig) bool { return f.DisableToolLoopBreaker },
		handler: func(*Agent) domain.Handler {
			return retryGuard(floor.ToolLoopBreak)
		},
	},
	{
		key:    guardToolCallRepair,
		moment: domain.MomentPostResponse,
		action: guardActionRetry,
		gate:   func(f domain.FloorConfig) bool { return f.DisableToolCallRepair },
		handler: func(a *Agent) domain.Handler {
			return retryGuard(func(resp *domain.Response) (string, bool) {
				return floor.ToolCallRepair(resp, a.registeredToolNames())
			})
		},
	},
	{
		key:    guardEmptyResponseRecovery,
		moment: domain.MomentPostResponse,
		action: guardActionRetry,
		gate:   func(f domain.FloorConfig) bool { return f.DisableEmptyResponseRecovery },
		handler: func(*Agent) domain.Handler {
			return retryGuard(floor.RecoverEmpty)
		},
	},
	{
		key:    guardToolUseEnforcer,
		moment: domain.MomentPostResponse,
		action: guardActionRetry,
		gate:   func(f domain.FloorConfig) bool { return f.DisableToolUseEnforcer },
		handler: func(*Agent) domain.Handler {
			return retryGuard(floor.EnforceToolUse)
		},
	},
	{
		key:    guardReadCache,
		moment: domain.MomentPreToolExec,
		action: guardActionIntercept,
		gate:   func(f domain.FloorConfig) bool { return f.DisableReadCache },
		handler: func(a *Agent) domain.Handler {
			return domain.PreToolExecFunc(a.cacheRead)
		},
	},
	{
		key:    guardToolResultCap,
		moment: domain.MomentPreRequest,
		action: guardActionCap,
		gate:   func(f domain.FloorConfig) bool { return f.DisableToolResultCap },
		handler: func(a *Agent) domain.Handler {
			return domain.PreRequestFunc(a.capToolResults)
		},
	},
}

// FloorGuardKeys lists the seven Floor guards' keys in the order they fire — the table's key set,
// exported so the composition root can pin its own key list (the settings switch, the config
// keys) to it without importing the table. The slice is a fresh copy every call.
func FloorGuardKeys() []string {
	keys := make([]string, 0, len(floorGuards))
	for _, g := range floorGuards {
		keys = append(keys, g.key)
	}
	return keys
}

// buildBuiltins returns this Agent's builtin Reactions — the guards gates leaves ON and, each when
// its switch is, the context-fill notice and the step-budget notice — in the order they fire: the
// floorGuards table's order, whose doc gives the reasons, then the two notices.
//
// gates is the enable set's input, read ONCE here: a guard whose opt-out is set is skipped over,
// which is the whole of how a Floor gate is honoured now. The relative order of the guards that
// survive is untouched, so switching one off never reshuffles the rest. notice is the
// context-fill notice's switch (Generation.ContextFillNotice, ADR 0077) and stepNotice the
// step-budget notice's (Generation.StepBudgetNotice), read the same way: a notice belongs after
// the guards when it is on and is absent otherwise. The notices are the LAST builtins because they
// are the ones that speak rather than correct: what each says is measured over the tool result as
// the guards ahead of it left it — the fill notice first, since its line measures the result's
// size, and the step notice after it, whose count the fill line does not move.
func (a *Agent) buildBuiltins(gates domain.FloorConfig, notice, stepNotice bool) []armedReaction {
	ladder := make([]armedReaction, 0, len(floorGuards)+2)
	enabled := func(off bool, r armedReaction) {
		if !off {
			ladder = append(ladder, r)
		}
	}
	for _, g := range floorGuards {
		enabled(g.gate(gates), engineBuiltin(g.key, g.action, g.moment, g.handler(a)))
	}
	enabled(!notice,
		classedBuiltin(contextFillNoticeID, actionNotice, domain.ClassAdvise, domain.MomentPostToolResult,
			domain.PostToolResultFunc(a.contextFillNotice)))
	enabled(!stepNotice,
		classedBuiltin(stepBudgetNoticeID, actionNotice, domain.ClassAdvise, domain.MomentPostToolResult,
			domain.PostToolResultFunc(a.stepBudgetNotice)))
	return ladder
}

// retryGuard adapts one retry-shaped Floor policy — given the response, a correction and whether
// it fired — into the post-response handler the four retry guards share: a policy that fired asks
// for the Turn to be re-streamed with its correction injected, and one that did not books nothing.
func retryGuard(policy func(*domain.Response) (string, bool)) domain.Handler {
	return domain.PostResponseFunc(func(_ context.Context, resp *domain.Response) (domain.Outcome, error) {
		correction, fired := policy(resp)
		if !fired {
			return domain.Outcome{}, nil
		}
		return domain.Outcome{Retry: true, Inject: correction}, nil
	})
}

// engineBuiltin builds one Floor guard: an engine-origin, shape-view Reaction on the single Moment
// named, booking its firings under action. Every Floor guard is shape-view because that is what
// a guard does — it edits what the model SEES, the response it is about to be judged on or the
// request it is about to be sent.
func engineBuiltin(id, action string, on domain.Moment, handler domain.Handler) armedReaction {
	return classedBuiltin(id, action, domain.ClassShapeView, on, handler)
}

// classedBuiltin builds one builtin of the class named: engineBuiltin's general form, which the
// two notices — engine origin, class advise (ADR 0077) — are the callers of beside it.
//
// The Moment is passed rather than read off the handler because the handler's own seam is
// domain's seal, unexported outside it; Reaction.Validate re-checks the two agree, so a builtin
// wired to the wrong Moment fails the structural test rather than firing in the wrong place.
func classedBuiltin(id, action string, class domain.Class, on domain.Moment, handler domain.Handler) armedReaction {
	return armedReaction{
		spec: domain.Reaction{
			ID:      id,
			Origin:  domain.OriginEngine,
			Class:   class,
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
//   - Never on a tool-less WRAP-UP Turn (turnLifecycle.wrapUp). That Turn is offered no menu at all and
//     its reply is a closing report by construction: a call salvaged out of it would be a call
//     the delegation had already been told it could not make. A wrap-up that kept write_file for
//     the delegation's `output_path` (wrapUpWriter) is offered exactly that menu, and salvages
//     against it like any other Turn — the one call it may still make is the one worth reading
//     out of its text.
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
	if (a.turns.wrappingUp() && a.wrapUpOutput() == "") || !processing.IsNative(a.textParser) {
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

// cacheRead is the read cache (floor.CacheRead): a re-read of a file this Exchange already read
// unchanged is narrowed in place, so the model spends its window on what it has not seen. It
// reshapes the pending call through the shared ToolCallEdit, the same wrapper every pre-tool-exec
// reaction writes through, so the loop executes what the cascade left behind.
func (a *Agent) cacheRead(_ context.Context, view domain.LoopView, call *domain.ToolCallEdit) (domain.Outcome, error) {
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
