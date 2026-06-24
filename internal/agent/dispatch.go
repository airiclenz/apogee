package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

// dispatchOutcome reports whether a Turn's tool dispatch ran to completion or was cut short
// by a ctx cancellation (which rolls the whole Turn back).
type dispatchOutcome int

const (
	dispatchDone dispatchOutcome = iota
	dispatchCancelled
	// dispatchConfinementUnavailable reports that a dispoConfine subprocess call could not
	// be confined at run time (the Confiner returned ErrConfinementUnavailable). The call did
	// NOT run; resolveAndExecute demotes it to Approval — the runtime "confine if you can,
	// gate if you can't" net (confinement-execution-contract §2.2; carried finding #2).
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
		a.cfg.Events.Emit(domain.ToolCallEvent{EventBase: base(turn), Call: call})

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

		a.runPostToolResultHooks(ctx, turn, call, &result)
		a.appendToolResult(turn, result)
	}
	return dispatchDone
}

// resolveAndExecute resolves a tool call against the registry, applies the always-on
// security guardrails (D6) and the per-call blast-radius disposition (D5), and executes
// it — returning the result (or an error result) and whether ctx was cancelled mid-flight.
//
// The guardrails (security.Guards) run FIRST, in every mode and independent of the
// Confiner, and are tighten-only (ADR 0012): a Tier-1 dangerous action or a tripped
// circuit-breaker refuses the call outright; a Tier-2 dangerous action forces the
// Approver even in Auto. They run ahead of the mode disposition — they never loosen it.
//
// The disposition (D5; disposition.go) then decides the call's fate by blast radius:
// run directly, run OS-confined (subprocess surface in Auto), gate through Approval, or
// refuse (Plan-mode write). A Tier-2 force-approval upgrades any non-refuse disposition
// to a gate (the guardrail can only tighten).
func (a *Agent) resolveAndExecute(ctx context.Context, turn int, call domain.ToolCall) (domain.ToolResult, dispatchOutcome) {
	tool, ok := a.lookupTool(call.Tool)
	if !ok {
		return errorToolResult(call.ID, fmt.Sprintf("unknown tool %q", call.Tool)), dispatchDone
	}

	guard := a.guards.PreExecute(call)
	if guard.Outcome == security.GuardRefuse {
		result := errorToolResult(call.ID, guardRefusalMessage(guard))
		a.guards.RecordBlocked(call, guard.Audit, guard.Reason, result)
		a.cfg.Events.Emit(domain.ErrorEvent{EventBase: base(turn), Source: call.Tool, Err: guardRefusalMessage(guard)})
		return result, dispatchDone
	}

	dispo := a.dispose(tool, call)
	if dispo == dispoRefuse {
		// The Plan menu hides write tools; refuse one defensively if the model calls it.
		return errorToolResult(call.ID, "plan mode: write tools are not permitted"), dispatchDone
	}

	// A Tier-2 dangerous action forces the Approver even where the disposition would not
	// (e.g. an Auto confine/run): the guardrail can only tighten.
	forceApproval := guard.Outcome == security.GuardForceApproval
	needGate := dispo == dispoGate || forceApproval
	allowed, outcome := a.approve(ctx, turn, tool, call, needGate, forceApproval)
	if outcome == dispatchCancelled {
		return domain.ToolResult{}, dispatchCancelled
	}
	if !allowed {
		result := errorToolResult(call.ID, "tool call denied by approver")
		a.guards.RecordBlocked(call, guard.Audit, guard.Reason, result)
		return result, dispatchDone
	}

	result, execOutcome := a.executeTool(ctx, turn, tool, call, dispo)
	if execOutcome == dispatchCancelled {
		return result, dispatchCancelled
	}
	if execOutcome == dispatchConfinementUnavailable {
		// "Confine if you can, gate if you can't" at RUNTIME (carried finding #2): the
		// disposition chose dispoConfine (caps reported sufficient at construction), but the
		// backend could not establish the box when the subprocess tool tried to confine. The
		// call did NOT run unconfined — runSubprocess refused. Demote to Approval and, if the
		// human allows, re-run unconfined (Approval is now the bound, the §4 gate row).
		result, execOutcome = a.executeWithApprovalFallback(ctx, turn, tool, call, guard)
		if execOutcome == dispatchCancelled {
			return result, dispatchCancelled
		}
	}

	// Post-execution guardrails: feed the circuit-breaker the outcome (surfacing a
	// single ErrorEvent on the trip edge so a runaway loop is halted, not crashed) and
	// append the audit record (call / decision / result).
	if tripped := a.guards.RecordExecution(call, guard.Audit, guard.Reason, result); tripped {
		a.cfg.Events.Emit(domain.ErrorEvent{
			EventBase: base(turn),
			Source:    call.Tool,
			Err: fmt.Sprintf("circuit-breaker tripped: tool %q failed %d times with identical arguments; "+
				"further identical calls will be refused", call.Tool, a.guards.Breaker.Threshold()),
		})
	}
	return result, dispatchDone
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

// approve consults the Approver when the disposition (or a forced guardrail) requires a
// gate, returning whether the call may run. It honours allow-for-session (remembered for
// the rest of the Session) and reports dispatchCancelled if ctx is cancelled while the
// human deliberates.
//
// gate is the disposition's verdict that this call must clear the Approver (dispoGate, or
// a Tier-2 force-approval). force distinguishes the Tier-2 case: a forced gate ignores the
// allow-for-session cache — a force-approval is a per-call speed-bump, not a thing the user
// can pre-allow for the Session. A nil Approver while a gate is required ⇒ refuse rather
// than run unapproved.
func (a *Agent) approve(ctx context.Context, turn int, tool domain.Tool, call domain.ToolCall, gate, force bool) (bool, dispatchOutcome) {
	if !gate {
		return true, dispatchDone
	}
	if !force && a.approved[tool.Name()] {
		return true, dispatchDone
	}
	if a.cfg.Approver == nil {
		// A gate is required but the host supplied no Approver: refuse rather than run an
		// unapproved write / unconfinable / dangerous tool.
		a.cfg.Events.Emit(domain.ErrorEvent{
			EventBase: base(turn),
			Source:    "loop",
			Err:       "approval required but no Approver configured",
		})
		return false, dispatchDone
	}

	reason := a.approvalReason(tool)
	if force {
		reason = "dangerous-action guard forced approval"
	}
	areq := domain.ApprovalRequest{Tool: call.Tool, Arguments: call.Arguments, Reason: reason}
	decision, err := a.cfg.Approver.Approve(ctx, areq)
	if err != nil {
		if ctx.Err() != nil {
			return false, dispatchCancelled
		}
		a.cfg.Events.Emit(domain.ErrorEvent{EventBase: base(turn), Source: "loop", Err: "approver: " + err.Error()})
		return false, dispatchDone
	}

	a.cfg.Events.Emit(domain.ApprovalEvent{EventBase: base(turn), Request: areq, Decision: decision})
	switch decision {
	case domain.ApprovalAllowForSession:
		if a.approved == nil {
			a.approved = make(map[string]bool)
		}
		a.approved[tool.Name()] = true
		return true, dispatchDone
	case domain.ApprovalAllow:
		return true, dispatchDone
	default: // ApprovalDeny or any unknown verdict — refuse
		return false, dispatchDone
	}
}

// approvalReason is the human-facing why for the Approval prompt, derived from the tool's
// blast-radius class so the human sees what kind of reach they are authorising.
func (a *Agent) approvalReason(tool domain.Tool) string {
	switch classifyTool(tool) {
	case classNetwork:
		return "network reach"
	case classMCP:
		return "unconfinable MCP tool"
	case classSubprocess:
		return "subprocess execution (confinement unavailable on this host)"
	case classWorkspaceWrite:
		return "out-of-workspace write"
	default:
		return "write"
	}
}

// executeTool runs one tool under a recover boundary (ADR 0007): a panic becomes an
// ErrorEvent and an error tool-result so the loop survives; a ctx cancellation propagates as
// dispatchCancelled; any other Execute error is surfaced to the model as an error result
// rather than failing the Turn (a tool returns a Go error only for cancellation).
//
// dispo carries the blast-radius disposition: a dispoConfine subprocess call runs with the
// Confinement handle (Confiner + box) installed in its context, so the subprocess tool
// confines the *exec.Cmd it builds (confinement-execution-contract §2.2). An
// ExternalEffectTool routes through the injected ExternalEffects boundary (ADR 0008) when
// the host supplied one, so the bench can stub network/MCP deterministically; else it runs
// live.
func (a *Agent) executeTool(ctx context.Context, turn int, tool domain.Tool, call domain.ToolCall, dispo disposition) (result domain.ToolResult, outcome dispatchOutcome) {
	outcome = dispatchDone
	defer func() {
		if r := recover(); r != nil {
			a.cfg.Events.Emit(domain.ErrorEvent{
				EventBase: base(turn),
				Source:    call.Tool,
				Err:       fmt.Sprintf("panic: %v", r),
			})
			result = errorToolResult(call.ID, fmt.Sprintf("tool %q panicked", call.Tool))
			outcome = dispatchDone
		}
	}()

	if dispo == dispoConfine {
		// Install the Confinement handle so the subprocess tool confines the command it
		// launches. The disposition only chose dispoConfine after confirming caps (§4), so
		// the Confiner is non-nil and fs-confinement-capable here.
		ctx = domain.WithConfinement(ctx, domain.Confinement{
			Confiner: a.cfg.Confiner,
			Box:      a.confinementBox(),
		})
	}

	res, err := a.runTool(ctx, tool, call)
	if err != nil {
		if ctx.Err() != nil {
			return domain.ToolResult{}, dispatchCancelled
		}
		// A subprocess tool that could not confine its command (the backend returned
		// ErrConfinementUnavailable when asked to wrap the cmd) reports it as a Go error
		// rather than running unconfined. Surface it as the demote signal so dispatch routes
		// the call to Approval instead of failing it (carried finding #2). This only arises
		// on a dispoConfine call (the only path that installs a Confinement handle).
		if errors.Is(err, domain.ErrConfinementUnavailable) {
			return domain.ToolResult{}, dispatchConfinementUnavailable
		}
		a.cfg.Events.Emit(domain.ErrorEvent{EventBase: base(turn), Source: call.Tool, Err: err.Error()})
		return errorToolResult(call.ID, err.Error()), dispatchDone
	}
	return res, dispatchDone
}

// executeWithApprovalFallback is the runtime demote-to-Approval path for a subprocess call
// the disposition chose to confine but whose Confiner could not establish the box at run time
// (carried finding #2). It gates the call through the Approver (a forced gate — a runtime
// safety event, not a pre-allowable convenience) and, if allowed, re-runs the tool UNCONFINED
// (dispoRun): Approval is now the blast-radius bound, exactly the §4 "subproc, caps
// insufficient → gate" row applied at run time. A nil Approver or a deny refuses the call.
func (a *Agent) executeWithApprovalFallback(ctx context.Context, turn int, tool domain.Tool, call domain.ToolCall, guard security.PreCheck) (domain.ToolResult, dispatchOutcome) {
	a.cfg.Events.Emit(domain.ErrorEvent{
		EventBase: base(turn),
		Source:    call.Tool,
		Err:       "confinement unavailable at run time: demoting subprocess call to Approval",
	})

	allowed, outcome := a.approve(ctx, turn, tool, call, true /* gate */, true /* force */)
	if outcome == dispatchCancelled {
		return domain.ToolResult{}, dispatchCancelled
	}
	if !allowed {
		result := errorToolResult(call.ID, "subprocess could not be confined and approval was not granted")
		a.guards.RecordBlocked(call, guard.Audit, guard.Reason, result)
		return result, dispatchDone
	}
	// Re-run with NO confinement handle installed (dispoRun): the call already failed to
	// confine, and Approval is now the bound the human granted.
	return a.executeTool(ctx, turn, tool, call, dispoRun)
}

// runTool routes the call to the injected ExternalEffects boundary for an external-effect
// tool when one is configured (ADR 0008 — the single non-forkable-effect seam, both network
// and MCP kinds), otherwise to the tool's live Execute. The gating decision keyed on the
// effect KIND (the disposition); routing here is the SEPARATE concern of where the effect
// actually runs, so the two stay distinct (confinement-execution-contract §8 / task P3.4).
func (a *Agent) runTool(ctx context.Context, tool domain.Tool, call domain.ToolCall) (domain.ToolResult, error) {
	if _, isExternal := tool.(domain.ExternalEffectTool); isExternal && a.cfg.ExternalEffects != nil {
		return a.cfg.ExternalEffects.Do(ctx, call)
	}
	return tool.Execute(ctx, call)
}

// confinementBox builds the ConfinementBox a confined subprocess runs inside: the injected
// workspace as the writable root, plus the per-project writable/network allowlists the host
// folded into Config. Box construction (toolchain cache/temp dirs, etc.) is the host's
// concern (confinement-execution-contract §7); the loop confines to whatever box it builds
// from the injected roots.
func (a *Agent) confinementBox() domain.ConfinementBox {
	return domain.ConfinementBox{
		WorkspaceRoot: a.cfg.WorkspaceDir,
		WritablePaths: a.cfg.ConfineWritablePaths,
		NetworkAllow:  a.cfg.ConfineNetworkAllow,
	}
}

// appendToolResult commits a tool result to the conversation as a tool message (linked to
// its call by ID) and emits the ToolResultEvent observers see.
func (a *Agent) appendToolResult(turn int, result domain.ToolResult) {
	a.conv.Append(domain.Message{Role: domain.RoleTool, Content: result.Content, ToolCallID: result.CallID})
	a.cfg.Events.Emit(domain.ToolResultEvent{EventBase: base(turn), Result: result})
}

// errorToolResult builds a tool-level failure result surfaced to the model (IsError) rather
// than returned as a Go error, which the loop reserves for ctx cancellation (ADR 0007).
func errorToolResult(callID, message string) domain.ToolResult {
	return domain.ToolResult{CallID: callID, Content: message, IsError: true}
}
