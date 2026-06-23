package agent

import (
	"context"
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
)

// dispatchOutcome reports whether a Turn's tool dispatch ran to completion or was cut short
// by a ctx cancellation (which rolls the whole Turn back).
type dispatchOutcome int

const (
	dispatchDone dispatchOutcome = iota
	dispatchCancelled
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

// resolveAndExecute resolves a tool call against the registry, applies the Plan-mode and
// Approval gates, and executes it — returning the result (or an error result) and whether
// ctx was cancelled mid-flight.
func (a *Agent) resolveAndExecute(ctx context.Context, turn int, call domain.ToolCall) (domain.ToolResult, dispatchOutcome) {
	tool, ok := a.lookupTool(call.Tool)
	if !ok {
		return errorToolResult(call.ID, fmt.Sprintf("unknown tool %q", call.Tool)), dispatchDone
	}
	if a.cfg.Mode == domain.ModePlan && !domain.IsReadOnly(tool) {
		// The Plan menu hides write tools; refuse one defensively if the model calls it.
		return errorToolResult(call.ID, "plan mode: write tools are not permitted"), dispatchDone
	}

	allowed, outcome := a.approve(ctx, turn, tool, call)
	if outcome == dispatchCancelled {
		return domain.ToolResult{}, dispatchCancelled
	}
	if !allowed {
		return errorToolResult(call.ID, "tool call denied by approver"), dispatchDone
	}

	return a.executeTool(ctx, turn, tool, call)
}

// lookupTool resolves a tool name against the resolved registry (nil registry ⇒ not found).
func (a *Agent) lookupTool(name string) (domain.Tool, bool) {
	if a.tools == nil {
		return nil, false
	}
	return a.tools.Lookup(name)
}

// approve consults the Approver when the Agent's mode and the tool require it, returning
// whether the call may run. It honours allow-for-session (remembered for the rest of the
// Session) and reports dispatchCancelled if ctx is cancelled while the human deliberates.
func (a *Agent) approve(ctx context.Context, turn int, tool domain.Tool, call domain.ToolCall) (bool, dispatchOutcome) {
	if !a.needsApproval(tool) || a.approved[tool.Name()] {
		return true, dispatchDone
	}
	if a.cfg.Approver == nil {
		// A gate is required but the host supplied no Approver: refuse rather than run an
		// unapproved write / unconfinable tool.
		a.cfg.Events.Emit(domain.ErrorEvent{
			EventBase: base(turn),
			Source:    "loop",
			Err:       "approval required but no Approver configured",
		})
		return false, dispatchDone
	}

	areq := domain.ApprovalRequest{Tool: call.Tool, Arguments: call.Arguments, Reason: a.approvalReason(tool)}
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

// needsApproval reports whether a tool call must clear the Approver before running, per the
// Agent's mode: Plan runs only read-only tools (no gate); Ask-Before gates every write (a
// read-only tool is harmless and skips it); Auto trusts OS confinement and gates only tools
// that reach unconfinable external state (ADR 0004). An empty mode is treated as Ask-Before
// — the safe default that gates writes.
func (a *Agent) needsApproval(tool domain.Tool) bool {
	switch a.cfg.Mode {
	case domain.ModePlan:
		return false
	case domain.ModeAuto:
		return isExternalEffect(tool)
	default:
		return !domain.IsReadOnly(tool)
	}
}

// approvalReason is the human-facing why for the Approval prompt.
func (a *Agent) approvalReason(tool domain.Tool) string {
	if isExternalEffect(tool) {
		return "unconfinable external-effect tool"
	}
	return "write"
}

// isExternalEffect reports whether tool reaches state Apogee cannot confine (network, MCP)
// — the tools that gate through Approval even in Auto (ADR 0004).
func isExternalEffect(tool domain.Tool) bool {
	_, ok := tool.(domain.ExternalEffectTool)
	return ok
}

// executeTool runs one tool under a recover boundary (ADR 0007): a panic becomes an
// ErrorEvent and an error tool-result so the loop survives; a ctx cancellation propagates as
// dispatchCancelled; any other Execute error is surfaced to the model as an error result
// rather than failing the Turn (a tool returns a Go error only for cancellation).
func (a *Agent) executeTool(ctx context.Context, turn int, tool domain.Tool, call domain.ToolCall) (result domain.ToolResult, outcome dispatchOutcome) {
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

	res, err := tool.Execute(ctx, call)
	if err != nil {
		if ctx.Err() != nil {
			return domain.ToolResult{}, dispatchCancelled
		}
		a.cfg.Events.Emit(domain.ErrorEvent{EventBase: base(turn), Source: call.Tool, Err: err.Error()})
		return errorToolResult(call.ID, err.Error()), dispatchDone
	}
	return res, dispatchDone
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
