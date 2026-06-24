package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tools"
)

// ----------------------------------------------------------------------------
// Sub-agent orchestrator (ADR 0013, D2 — privileges ≤ parent, atomic within the Turn)
// ----------------------------------------------------------------------------
//
// A sub-agent IS the embeddable Agent (ADR 0001), one nesting level down. The orchestrator
// here constructs a nested Agent that inherits the parent's privileges VERBATIM OR STRICTER
// (ADR 0005): the same Mode, Approver, Confiner, and confine-to-workspace flag; a fresh
// guardrail bundle that ISOLATES live state but SHARES the dangerous-action floor read-only
// (Guards.ForSubAgent); and a tool set that is a SUBSET of the parent's, never an expansion.
// Its events re-emit into the parent's EventSink at Depth = parent+1 (the nested Agent stamps
// its own depth — base()), so the TUI and bench observe one nested stream.
//
// The sub-agent runs ATOMICALLY WITHIN the parent Turn (D2): the parent is mid-tool-dispatch
// while the nested loop runs to completion, so there is no quiescent boundary inside it. A
// cancel propagates to the nested loop's next boundary and unwinds the whole call (the parent
// rolls its Turn back from the pre-sub_agent boundary); no partial sub-agent result is
// surfaced and no snapshot lands mid-sub-agent. Nested STEPPING (suspend/resume a sub-agent at
// its own boundary) is deliberately out of scope for v1 — the driver below runs the nested
// Agent to its Exchange boundary in one shot, behind a seam a later snapshot-schema-additive
// change can swap for a suspendable driver.

// maxSubAgentDepth bounds sub-agent recursion so a model cannot spawn an unbounded tower of
// sub-agents (each level costs a full nested loop). The top-level agent is depth 0; a depth-0
// agent may spawn a depth-1 sub-agent and a depth-1 may spawn a depth-2, but a depth-2
// sub-agent is the deepest: at maxSubAgentDepth the sub_agent tool is withheld from the
// nested tool set AND the recursion point refuses defensively, so the bound holds even if the
// menu is bypassed. Three levels is ample for real delegation while making a runaway tower
// structurally impossible.
const maxSubAgentDepth = 2

// isSubAgentCall reports whether call targets the sub_agent recursion point — the signal
// resolveAndExecute drives a nested Agent for the call rather than executing a leaf tool.
func isSubAgentCall(call domain.ToolCall) bool {
	return call.Tool == tools.SubAgentToolName
}

// runSubAgent is the recursion point: it parses the delegated task, constructs a nested Agent
// bounded by this Agent's privileges (ADR 0005/0013), drives it to its Exchange boundary, and
// returns the sub-agent's final message as this call's tool result. A cancellation propagates
// out as dispatchCancelled so the parent rolls the whole Turn back (atomic-within-the-Turn).
//
// The nested loop's events already reached the parent's EventSink at Depth+1 as they ran; the
// returned ToolResult is what the PARENT model sees on its next Turn (the delegated work
// summarised back into the parent conversation).
func (a *Agent) runSubAgent(ctx context.Context, call domain.ToolCall) (domain.ToolResult, dispatchOutcome) {
	if a.depth >= maxSubAgentDepth {
		// Defensive floor: the tool is withheld from the menu at the bound, but refuse here
		// too so the bound holds even if a model emits the call anyway.
		return errorToolResult(call.ID, fmt.Sprintf(
			"sub-agent depth limit reached (max %d): cannot spawn a deeper sub-agent", maxSubAgentDepth)), dispatchDone
	}

	var args tools.SubAgentArgs
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return errorToolResult(call.ID, "invalid sub_agent arguments: "+err.Error()), dispatchDone
	}
	if args.Task == "" {
		return errorToolResult(call.ID, "sub_agent requires a non-empty task"), dispatchDone
	}

	sub, err := a.newChildAgent()
	if err != nil {
		return errorToolResult(call.ID, "could not construct sub-agent: "+err.Error()), dispatchDone
	}

	if err := sub.Submit(domain.UserInput{Text: args.Task}); err != nil {
		return errorToolResult(call.ID, "could not start sub-agent: "+err.Error()), dispatchDone
	}
	res, err := sub.Run(ctx)
	if err != nil {
		// Run returns a Go error only for a loop-level fault the nested Agent could not
		// localise — surface it as an error result to the parent model rather than failing
		// the parent Turn.
		return errorToolResult(call.ID, "sub-agent failed: "+err.Error()), dispatchDone
	}
	if res.Status == domain.StatusCancelled {
		// The cancel reached the nested loop's boundary and it returned resumably; the parent
		// Turn must now roll back wholesale (D2: the recovery point is the pre-sub_agent
		// boundary — the sub-agent's progress is discarded, no partial result surfaced).
		return domain.ToolResult{}, dispatchCancelled
	}

	return domain.ToolResult{CallID: call.ID, Content: sub.finalMessageText(), IsError: false}, dispatchDone
}

// newChildAgent constructs the nested Agent for a sub-agent, threading this Agent's privileges
// bounded (ADR 0005/0013): the parent Mode / Approver / Confiner / confine-to-workspace flag
// verbatim (never loosened), a Guards bundle that isolates live state but shares the dangerous
// floor read-only (Guards.ForSubAgent), a tool set that is a SUBSET of this Agent's tools
// (defaultSubAgentTools — never an expansion, and withholding sub_agent at the depth bound),
// the SAME Upstream responder and EventSink, and Depth = parent+1 so its events nest. The
// nested Agent is NOT given the parent's pending input, conversation, or approval cache — it
// starts fresh with only the delegated task (the ADR-0008 statelessness boundary).
func (a *Agent) newChildAgent() (*Agent, error) {
	childCfg := a.cfg
	childCfg.Tools = a.defaultSubAgentTools()
	// The sub-agent shares the parent's Mechanisms by default; an explicit per-sub-agent
	// catalogue is a later refinement (ADR 0013 leaves the default = the parent's).

	child, err := newAgent(childCfg, a.upstream)
	if err != nil {
		return nil, err
	}
	child.depth = a.depth + 1
	child.guards = a.guards.ForSubAgent()
	return child, nil
}

// defaultSubAgentTools returns the tool registry a sub-agent is constructed with: the parent's
// full tool set by default (ADR 0005 — the caller may narrow per task; the default is the
// parent's set), MINUS the sub_agent recursion point itself when spawning the child would put
// it AT the depth bound (a depth-(max) sub-agent is never offered sub_agent, so it cannot
// recurse further). A nil parent registry yields nil (a tool-less sub-agent — the parent had
// no tools to delegate).
//
// The result is always ≤ the parent's tools: it is built from the parent registry's own names
// via Subset, so it can never name a tool the parent lacks (a privilege expansion is
// structurally impossible — ADR 0005).
func (a *Agent) defaultSubAgentTools() *domain.ToolRegistry {
	if a.tools == nil {
		return nil
	}
	names := make([]string, 0, len(a.tools.All()))
	childDepth := a.depth + 1
	for _, t := range a.tools.All() {
		// Withhold sub_agent from a child that would itself be AT the depth bound: it must
		// not be able to recurse, so it never sees the tool. (The recursion point also
		// refuses defensively — defence in depth.)
		if t.Name() == tools.SubAgentToolName && childDepth >= maxSubAgentDepth {
			continue
		}
		names = append(names, t.Name())
	}
	return a.tools.Subset(names...)
}

// finalMessageText returns the text of the last assistant message in the sub-agent's
// conversation — the delegated result reported back to the parent. An empty conversation (or
// one with no assistant text) yields a neutral note rather than an empty string, so the parent
// model always receives an intelligible result.
func (a *Agent) finalMessageText() string {
	for _, m := range reverseMessages(a.conv.Messages()) {
		if m.Role == domain.RoleAssistant && m.Content != "" {
			return m.Content
		}
	}
	return "(sub-agent completed with no final message)"
}

// reverseMessages returns msgs in reverse order so finalMessageText can scan from the most
// recent assistant message backward without indexing gymnastics.
func reverseMessages(msgs []domain.Message) []domain.Message {
	out := make([]domain.Message, len(msgs))
	for i, m := range msgs {
		out[len(msgs)-1-i] = m
	}
	return out
}
