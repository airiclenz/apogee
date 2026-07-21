package agent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/tools"
)

// dispatchOutcome reports whether a Turn's tool dispatch ran to completion or was cut short
// by a ctx cancellation (which rolls the whole Turn back).
type dispatchOutcome int

const (
	dispatchDone dispatchOutcome = iota
	dispatchCancelled
	// dispatchConfinementUnavailable reports that a Confine subprocess call could not be
	// confined at run time (the Confiner returned ErrConfinementUnavailable). The call did NOT
	// run; the executor follows the verdict's precomputed fallback — a forced Approval gate
	// whose allow-continuation re-runs the call unconfined (Resolution D4;
	// confinement-execution-contract §4).
	dispatchConfinementUnavailable
)

// dispatchTools runs each requested tool call through the pre-tool-exec hooks, the Approval
// gate, execution, and the post-tool-result hooks — appending each result to the
// conversation as a tool message and emitting the observability events. Approval is
// consulted here, AFTER the stream has closed (the §6 #6 resolution: stream fully, then
// gate), so a blocking Approver never holds an open Upstream connection.
//
// It returns dispatchCancelled only if ctx was cancelled while a tool was approving or
// executing; the caller then rolls the Turn back. Every other failure — an unknown tool, a
// denied call, a tool error, a recovered tool panic — becomes an error tool-result the
// model sees on the next Turn, and dispatch continues to the next call (ADR 0007).
func (a *Agent) dispatchTools(ctx context.Context, turn int, calls []domain.ToolCall) dispatchOutcome {
	for _, call := range calls {
		a.cfg.Events.Emit(domain.ToolCallEvent{EventBase: a.base(turn), Call: call})

		if err := a.runPreToolExecHooks(ctx, turn, &call); err != nil {
			// A pre-tool-exec hook panicked (recovered into an ErrorEvent): skip the call
			// with an error result rather than running it against a half-applied decision.
			a.appendToolResult(turn, errorToolResult(call.ID, "pre-tool-exec hook failed"))
			continue
		}

		result, outcome := a.resolveAndExecute(ctx, turn, call)
		if outcome == dispatchCancelled {
			return dispatchCancelled
		}

		// Feed this call's outcome to self-regulation's proxy signals (R3): a novel read or a
		// successful write is the productive signal, an error result the harmful one. Ordering
		// relative to the post-tool-result hooks is immaterial to their judgment — fires are
		// judged by the NEXT Turn's outcome (next-Turn judgment), not this one's.
		a.noteToolProductivity(call, result)
		a.runPostToolResultHooks(ctx, turn, call, &result)
		a.appendToolResult(turn, result)
	}
	return dispatchDone
}

// resolveAndExecute gathers the facts one tool call is decided from — the registry lookup, the
// always-on guardrails, the effective mode, the caps probe, and the one on-disk write-target
// check — computes the call's complete Resolution once (resolve(), resolution.go), and then
// EXECUTES that verdict mechanically. It holds no ladder, guard-tier, or demote decision of its
// own: resolve() decides, the switch below carries it out (Resolution D6;
// confinement-execution-contract §4). It returns the tool result (or an error result) and
// whether ctx was cancelled mid-flight.
//
// An unknown tool is rejected here, before resolve(): the registry miss is a dispatch fact, and
// short-circuiting it keeps a withheld tool (e.g. sub_agent at the depth bound) resolving as an
// unknown tool exactly as before, un-audited (Resolution D8). resolve() has a matching
// unknown-tool row for its own test, but dispatch never reaches it.
func (a *Agent) resolveAndExecute(ctx context.Context, turn int, call domain.ToolCall) (domain.ToolResult, dispatchOutcome) {
	tool, ok := a.lookupTool(call.Tool)
	if !ok {
		return errorToolResult(call.ID, fmt.Sprintf("unknown tool %q", call.Tool)), dispatchDone
	}

	verdict := resolve(a.resolutionInput(tool, call, a.guards.PreExecute(call)))

	switch verdict.kind {
	case resolveRefuse:
		return a.executeRefuse(turn, call, verdict), dispatchDone
	case resolveDelegate:
		return a.executeDelegate(ctx, turn, call, verdict)
	case resolveGate:
		return a.executeGate(ctx, turn, tool, call, verdict)
	case resolveConfine:
		return a.executeConfine(ctx, turn, tool, call, verdict)
	default: // resolveRun
		return a.executeRun(ctx, turn, tool, call, verdict)
	}
}

// resolutionInput assembles the facts resolve() decides from for one call: the effective mode,
// the resolved tool, the guardrail verdict, the LIVE confine-to-workspace flag (read through
// ConfineToWorkspace() under its lock, so a /confine toggle from the UI lands on the next call
// exactly as a Shift+Tab mode change does), the backend caps probe, the precomputed on-disk
// write-target check (the one I/O-tainted fact — resolve() does
// none), the sub-agent depth bound, whether an Approver is configured, and the confinement box
// a Confine verdict would run inside. It is dispatch's fact-gathering; the verdict logic lives
// entirely in resolve().
func (a *Agent) resolutionInput(tool domain.Tool, call domain.ToolCall, guard security.PreCheck) resolutionInput {
	return resolutionInput{
		mode:                   a.effectiveMode(),
		call:                   call,
		tool:                   tool,
		guard:                  guard,
		confineToWorkspace:     a.ConfineToWorkspace(),
		fsConfineAvailable:     a.fsConfinementAvailable(),
		writeTargetInWorkspace: a.writeTargetInWorkspace(tool, call),
		atDepthBound:           a.depth >= maxSubAgentDepth,
		approverPresent:        a.cfg.Approver != nil,
		box: domain.ConfinementBox{
			WorkspaceRoot: a.cfg.WorkspaceDir,
			WritablePaths: a.cfg.ConfineWritablePaths,
			NetworkAllow:  a.cfg.ConfineNetworkAllow,
		},
	}
}

// executeRun runs a Run verdict directly — no Approval, no Confine — and records it. It is also
// the shared "run it now" tail for an approved Gate and an approved runtime-demote re-run, both
// of which run unconfined once the human has authorised the call.
func (a *Agent) executeRun(ctx context.Context, turn int, tool domain.Tool, call domain.ToolCall, verdict resolution) (domain.ToolResult, dispatchOutcome) {
	result, outcome := a.executeTool(ctx, turn, tool, call, nil /* no confinement box */)
	if outcome == dispatchCancelled {
		return result, dispatchCancelled
	}
	a.recordExecutedTrip(turn, call, verdict, result)
	return result, dispatchDone
}

// executeGate routes a Gate verdict through the Approver and, if allowed, runs it unconfined.
// The resolver guarantees an Approver is present for a Gate (a gate with none is folded to a
// Refuse — Resolution D5), so nothing runs unapproved here. A forced gate skips the
// allow-for-session cache; a deny (or a nil Approver defensively) refuses the call.
func (a *Agent) executeGate(ctx context.Context, turn int, tool domain.Tool, call domain.ToolCall, verdict resolution) (domain.ToolResult, dispatchOutcome) {
	allowed, outcome := a.approve(ctx, turn, call, verdict.force, verdict.cacheKey, verdict.reason)
	if outcome == dispatchCancelled {
		return domain.ToolResult{}, dispatchCancelled
	}
	if !allowed {
		result := errorToolResult(call.ID, "tool call denied by approver")
		a.recordBlocked(turn, call, verdict.auditDecision, verdict.auditReason, result)
		return result, dispatchDone
	}
	return a.executeRun(ctx, turn, tool, call, verdict)
}

// executeConfine runs a Confine verdict's subprocess inside the verdict's box. If the box
// cannot be established at run time (the subprocess tool returns ErrConfinementUnavailable
// rather than running unconfined), it follows the verdict's precomputed fallback instead of
// deciding anew — the runtime "confine if you can, gate if you can't" net (Resolution D4).
func (a *Agent) executeConfine(ctx context.Context, turn int, tool domain.Tool, call domain.ToolCall, verdict resolution) (domain.ToolResult, dispatchOutcome) {
	result, outcome := a.executeTool(ctx, turn, tool, call, &verdict.box)
	if outcome == dispatchCancelled {
		return result, dispatchCancelled
	}
	if outcome == dispatchConfinementUnavailable {
		return a.executeConfineFallback(ctx, turn, tool, call, verdict)
	}
	a.recordExecutedTrip(turn, call, verdict, result)
	return result, dispatchDone
}

// executeConfineFallback carries out a Confine verdict's precomputed runtime-demote fallback
// (Resolution D4) after the box could not be established: it surfaces the demote event, then
// executes the fallback the resolver already chose — a forced Approval gate whose
// allow-continuation re-runs the call UNCONFINED (Approval is now the bound), or, when no
// Approver is configured, a Refuse. The executor follows the plan; it never decides.
func (a *Agent) executeConfineFallback(ctx context.Context, turn int, tool domain.Tool, call domain.ToolCall, verdict resolution) (domain.ToolResult, dispatchOutcome) {
	a.cfg.Events.Emit(domain.ErrorEvent{
		EventBase: a.base(turn),
		Source:    call.Tool,
		Err:       "confinement unavailable at run time: demoting subprocess call to Approval",
	})

	fb := verdict.fallback
	if fb.kind == resolveRefuse {
		// No Approver: the subprocess could not be confined and no human could authorise the
		// unconfined run.
		result := errorToolResult(call.ID, fb.reason)
		a.recordBlocked(turn, call, fb.auditDecision, fb.auditReason, result)
		return result, dispatchDone
	}

	allowed, outcome := a.approve(ctx, turn, call, fb.force, fb.cacheKey, fb.reason)
	if outcome == dispatchCancelled {
		return domain.ToolResult{}, dispatchCancelled
	}
	if !allowed {
		result := errorToolResult(call.ID, confineDemoteRefuseReason)
		a.recordBlocked(turn, call, fb.auditDecision, fb.auditReason, result)
		return result, dispatchDone
	}
	// Approval granted: re-run with NO confinement handle installed (the call already failed to
	// confine, and Approval is the bound the human granted).
	return a.executeRun(ctx, turn, tool, call, verdict)
}

// executeDelegate drives the sub_agent recursion point (a nested Agent) and records the
// delegation. runSubAgent keeps its own defensive depth check — belt-and-braces with the
// resolver's depth-bound row and the withheld-tool floor (ADR 0013 defence in depth) — so the
// bound holds even if the call is reached by another route.
func (a *Agent) executeDelegate(ctx context.Context, turn int, call domain.ToolCall, verdict resolution) (domain.ToolResult, dispatchOutcome) {
	result, outcome := a.runSubAgent(ctx, call)
	if outcome == dispatchCancelled {
		return result, dispatchCancelled
	}
	a.recordExecuted(turn, call, verdict.auditDecision, verdict.auditReason, result)
	return result, dispatchDone
}

// executeRefuse carries out a Refuse verdict: an error result plus the exact audit/event trail
// its source produces today (Resolution D8). A guard hard-refuse and a nil-Approver refuse
// carry the guard's pass-through audit decision, so they are recorded and surfaced; an
// unknown-tool (rejected before resolve) and a Plan-mode write refuse carry none, so they are
// neither.
func (a *Agent) executeRefuse(turn int, call domain.ToolCall, verdict resolution) domain.ToolResult {
	result := errorToolResult(call.ID, verdict.reason)
	if verdict.auditDecision != "" {
		a.recordBlocked(turn, call, verdict.auditDecision, verdict.auditReason, result)
		a.cfg.Events.Emit(domain.ErrorEvent{EventBase: a.base(turn), Source: call.Tool, Err: verdict.reason})
	}
	return result
}

// guardRefusalMessage renders the model-facing reason a guardrail refused a call.
func guardRefusalMessage(guard security.PreCheck) string {
	switch guard.Audit {
	case security.AuditCircuitTripped:
		return "circuit-breaker open: this tool call has failed repeatedly with identical arguments and is refused"
	default:
		return "refused by the dangerous-action guard: " + guard.Reason
	}
}

// lookupTool resolves a tool name against the resolved registry (nil registry ⇒ not found).
func (a *Agent) lookupTool(name string) (domain.Tool, bool) {
	if a.tools == nil {
		return nil, false
	}
	return a.tools.Lookup(name)
}

// approve consults the Approver for a Gate verdict, returning whether the call may run. It
// honours allow-for-session — remembered for the rest of the Session under the verdict's
// cacheKey — unless force is set: a forced gate (a Tier-2 speed-bump or a runtime demote) is a
// per-call event, not a pre-allowable convenience. reason feeds the Approval prompt. It reports
// dispatchCancelled if ctx is cancelled while the human deliberates.
//
// The resolver only produces a Gate when an Approver is configured (a gate with none is a
// Refuse — Resolution D5), so the nil-Approver guard below is defensive: it refuses rather than
// dereferencing a nil Approver, never running unapproved.
func (a *Agent) approve(ctx context.Context, turn int, call domain.ToolCall, force bool, cacheKey, reason string) (bool, dispatchOutcome) {
	if !force && a.approved[cacheKey] {
		return true, dispatchDone
	}
	if a.cfg.Approver == nil {
		return false, dispatchDone
	}

	areq := domain.ApprovalRequest{Tool: call.Tool, Arguments: call.Arguments, Reason: reason}
	decision, err := a.cfg.Approver.Approve(ctx, areq)
	if err != nil {
		if ctx.Err() != nil {
			return false, dispatchCancelled
		}
		a.cfg.Events.Emit(domain.ErrorEvent{EventBase: a.base(turn), Source: "loop", Err: "approver: " + err.Error()})
		return false, dispatchDone
	}

	a.cfg.Events.Emit(domain.ApprovalEvent{EventBase: a.base(turn), Request: areq, Decision: decision})
	switch decision {
	case domain.ApprovalAllowForSession:
		if a.approved == nil {
			a.approved = make(map[string]bool)
		}
		a.approved[cacheKey] = true
		return true, dispatchDone
	case domain.ApprovalAllow:
		return true, dispatchDone
	default: // ApprovalDeny or any unknown verdict — refuse
		return false, dispatchDone
	}
}

// executeTool runs one tool under a recover boundary (ADR 0007): a panic becomes an ErrorEvent
// and an error tool-result so the loop survives; a ctx cancellation propagates as
// dispatchCancelled; any other Execute error is surfaced to the model as an error result rather
// than failing the Turn (a tool returns a Go error only for cancellation).
//
// When box is non-nil the call is a Confine verdict: the Confinement handle (Confiner + box) is
// installed in its context, so a subprocess tool confines the *exec.Cmd it builds
// (confinement-execution-contract §2.2). A subprocess tool that cannot establish the box at run
// time returns ErrConfinementUnavailable rather than running unconfined; executeTool surfaces
// that as dispatchConfinementUnavailable so the caller follows the verdict's demote fallback.
// An ExternalEffectTool routes through the injected ExternalEffects boundary (ADR 0008) when
// the host supplied one; else it runs live.
func (a *Agent) executeTool(ctx context.Context, turn int, tool domain.Tool, call domain.ToolCall, box *domain.ConfinementBox) (result domain.ToolResult, outcome dispatchOutcome) {
	outcome = dispatchDone
	defer func() {
		if r := recover(); r != nil {
			a.cfg.Events.Emit(domain.ErrorEvent{
				EventBase: a.base(turn),
				Source:    call.Tool,
				Err:       fmt.Sprintf("panic: %v", r),
			})
			result = errorToolResult(call.ID, fmt.Sprintf("tool %q panicked", call.Tool))
			outcome = dispatchDone
		}
	}()

	if box != nil {
		// Install the Confinement handle so the subprocess tool confines the command it
		// launches. resolve() chose Confine only after confirming caps (§4), so the Confiner is
		// non-nil and fs-confinement-capable here.
		ctx = domain.WithConfinement(ctx, domain.Confinement{
			Confiner: a.cfg.Confiner,
			Box:      *box,
		})
	}

	res, err := a.runTool(ctx, tool, call)
	if err != nil {
		if ctx.Err() != nil {
			return domain.ToolResult{}, dispatchCancelled
		}
		// A subprocess tool that could not confine its command (the backend returned
		// ErrConfinementUnavailable when asked to wrap the cmd) reports it as a Go error rather
		// than running unconfined. Surface it as the demote signal so the caller follows the
		// verdict's fallback (Resolution D4). This only arises on a Confine call (the only path
		// that installs a Confinement handle).
		if errors.Is(err, domain.ErrConfinementUnavailable) {
			return domain.ToolResult{}, dispatchConfinementUnavailable
		}
		a.cfg.Events.Emit(domain.ErrorEvent{EventBase: a.base(turn), Source: call.Tool, Err: err.Error()})
		return errorToolResult(call.ID, err.Error()), dispatchDone
	}
	return res, dispatchDone
}

// runTool routes the call to the injected ExternalEffects boundary for an external-effect
// tool when one is configured (ADR 0008 — the single non-forkable-effect seam, both network
// and MCP kinds), otherwise to the tool's live Execute. The gating decision keyed on the
// effect KIND (the Resolution); routing here is the SEPARATE concern of where the effect
// actually runs, so the two stay distinct (confinement-execution-contract §8 / task P3.4).
func (a *Agent) runTool(ctx context.Context, tool domain.Tool, call domain.ToolCall) (domain.ToolResult, error) {
	if _, isExternal := tool.(domain.ExternalEffectTool); isExternal && a.cfg.ExternalEffects != nil {
		return a.cfg.ExternalEffects.Do(ctx, call)
	}
	return tool.Execute(ctx, call)
}

// effectiveMode is the autonomy mode the per-call Resolution runs under. For a top-level Agent
// it is simply the Agent's own live mode. For a sub-agent (liveMode != nil) it is the TIGHTER of
// the child's spawn mode and the parent's live mode (ADR 0013), so a parent tightening
// mid-delegation (Shift+Tab from Auto down to Plan) gates/refuses the still-running child's next
// call, while a parent loosening never loosens the child. Both modes are read under their own
// modeMu locks (Mode() and the captured accessor), so a concurrent SetMode on either agent is
// observed race-free.
func (a *Agent) effectiveMode() domain.Mode {
	own := a.Mode()
	if a.liveMode == nil {
		return own
	}
	return domain.TighterMode(own, a.liveMode())
}

// writeTargetInWorkspace reports whether a workspace-scoped writer's call targets a path inside
// the workspace root. A call with no inspectable target (ok==false) is treated as in-bounds (the
// Resolution then runs it, path-safety bounding it at Execute). A tool that is not a
// workspace-scoped writer is never in-workspace by this seam. This is the one I/O-tainted fact
// dispatch precomputes for resolve() (EvalRealPath touches disk).
func (a *Agent) writeTargetInWorkspace(tool domain.Tool, call domain.ToolCall) bool {
	abs, ok := tools.WorkspaceWriteTarget(tool, call)
	if !ok {
		return true // nothing inspectable to classify ⇒ treat as in-bounds (Execute path-bounds it)
	}
	return pathWithin(abs, a.cfg.WorkspaceDir)
}

// fsConfinementAvailable reports whether the injected Confiner can enforce filesystem
// confinement on this host — the caps gate the Resolution checks before choosing to confine a
// subprocess tool (confinement-execution-contract §4/§5).
func (a *Agent) fsConfinementAvailable() bool {
	return a.cfg.Confiner != nil && a.cfg.Confiner.Capabilities().FSWrite
}

// pathWithin reports whether abs (an already-resolved real path) is the workspace root or lives
// beneath it, resolving the root through symlinks the same way the write tool's target resolver
// does so the two agree (e.g. macOS /tmp). An empty root cannot contain anything, so a write is
// treated as out-of-workspace — the safe default that gates.
func pathWithin(abs, root string) bool {
	if root == "" {
		return false
	}
	realRoot := security.EvalRealPath(filepath.Clean(root))
	if abs == realRoot {
		return true
	}
	return strings.HasPrefix(abs, realRoot+string(filepath.Separator))
}

// appendToolResult commits a tool result to the conversation as a tool message (linked to
// its call by ID) and emits the ToolResultEvent observers see, after clamping a pathologically
// oversized result to the structural floor (clampToolResult). The clamp lands here, at the ONE
// seam every tool result crosses on its way into history, so no route — a plain call, a Confine
// verdict's, an approved gate's, a sub-agent delegation's, an error result — can bypass it.
func (a *Agent) appendToolResult(turn int, result domain.ToolResult) {
	result.Content = a.clampToolResult(result.Content)
	a.conv.Append(domain.Message{Role: domain.RoleTool, Content: result.Content, ToolCallID: result.CallID})
	a.cfg.Events.Emit(domain.ToolResultEvent{EventBase: a.base(turn), Result: result})
}

// clampToolResult is the STRUCTURAL floor on a single tool result: content whose estimated tokens
// exceed the ENTIRE History allocation is replaced by the shared head/tail-plus-marker elision
// (context.TruncateToolResult — the same shape `tool_result_cap` renders) before it is committed.
// A result bigger than everything History may hold can never survive ANY reducer, so appending it
// whole buys nothing and can doom the Turn outright: the emergency fold's own summary call keeps
// the most recent message unconditionally (renderBudgetedTranscript), so a fresh giant result IS
// that message and overflows the fold that was supposed to rescue the Turn.
//
// It is structural, not a Mechanism (ADR 0006's floor): it consults no config, is never disabled
// under Bypass, and self-regulation cannot withdraw it. The `tool_result_cap` Mechanism stays the
// A/B-able tuning valve above it and cannot substitute for it — it is default-off, bypass-disabled,
// withdrawable, and caps only the turns BEFORE the most recent tool call, so the freshly appended
// result (the one that overflows) is exactly the one it never touches. The two thresholds are
// deliberately far apart: this floor fires only on the pathological — the whole History allocation
// (~60% of the working room, ~48% of the window at the default reserve), chosen because it sits
// BELOW the emergency fold's own transcript budget, which is the property that keeps the fold
// survivable — while the Mechanism's tighter 40%-of-working-room nudge shapes the ordinary case.
//
// Unlike the Mechanism, which edits only the projected request, this clamp edits the conversation
// itself: the raw result never reaches history, and so never reaches a snapshot or the rendered
// transcript. That is the price of a floor that must hold for every later reducer — and the model
// is told, in the marker, to re-read the omitted range.
//
// It is inert when the window is unknown (a zero History allocation — Allocate had no basis to
// allocate), matching every other Budget-gated path, and it never GROWS a result: a pathological
// few-very-long-lines body the head/tail form cannot shrink is left whole (the same guard
// tool_result_cap applies).
func (a *Agent) clampToolResult(content string) string {
	b := a.budget()
	if b.History <= 0 || b.EstimateTokens(len(content)) <= b.History {
		return content
	}
	clamped := apogeectx.TruncateToolResult(content, int(float64(b.History)*b.CharsPerToken))
	if len(clamped) >= len(content) {
		return content
	}
	return clamped
}

// errorToolResult builds a tool-level failure result surfaced to the model (IsError) rather
// than returned as a Go error, which the loop reserves for ctx cancellation (ADR 0007).
func errorToolResult(callID, message string) domain.ToolResult {
	return domain.ToolResult{CallID: callID, Content: message, IsError: true}
}

// recordExecutedTrip records an executed call's audit + circuit-breaker outcome and surfaces the
// single ErrorEvent on the breaker's trip edge (so a runaway identical-failure loop is halted,
// not crashed). It is the shared post-execution tail of a Run and a Confine verdict.
func (a *Agent) recordExecutedTrip(turn int, call domain.ToolCall, verdict resolution, result domain.ToolResult) {
	if tripped := a.recordExecuted(turn, call, verdict.auditDecision, verdict.auditReason, result); tripped {
		a.cfg.Events.Emit(domain.ErrorEvent{
			EventBase: a.base(turn),
			Source:    call.Tool,
			Err: fmt.Sprintf("circuit-breaker tripped: tool %q failed %d times with identical arguments; "+
				"further identical calls will be refused", call.Tool, a.guards.Breaker.Threshold()),
		})
	}
}

// recordExecuted appends the executed call's audit record (feeding the circuit-breaker) AND
// emits an AuditEvent so the trail is observable, not only held in the in-process ring
// (security-review M1). It returns whether the breaker tripped on this call. A sub-agent
// records through its own guards but emits through the SAME EventSink at Depth > 0, so a
// delegated call's audit reaches the parent's observer instead of vanishing with the child.
func (a *Agent) recordExecuted(turn int, call domain.ToolCall, decision security.AuditDecision, reason string, result domain.ToolResult) (tripped bool) {
	tripped = a.guards.RecordExecution(call, decision, reason, result)
	a.emitAudit(turn, call, decision, reason, result)
	return tripped
}

// recordBlocked appends a blocked/diverted call's audit record AND emits the matching
// AuditEvent (security-review M1), so a refused/denied call is observable, not silently
// dropped into a ring no observer reads.
func (a *Agent) recordBlocked(turn int, call domain.ToolCall, decision security.AuditDecision, reason string, result domain.ToolResult) {
	a.guards.RecordBlocked(call, decision, reason, result)
	a.emitAudit(turn, call, decision, reason, result)
}

// emitAudit surfaces one audit record to the EventSink as a domain.AuditEvent (M1). It is
// the single bridge from the security audit record onto the observable event stream; the
// agent layer constructs the domain-only event so domain keeps its no-upward-dependency
// property (ADR 0010).
func (a *Agent) emitAudit(turn int, call domain.ToolCall, decision security.AuditDecision, reason string, result domain.ToolResult) {
	a.cfg.Events.Emit(domain.AuditEvent{
		EventBase: a.base(turn),
		Tool:      call.Tool,
		CallID:    call.ID,
		Decision:  string(decision),
		Reason:    reason,
		IsError:   result.IsError,
	})
}
