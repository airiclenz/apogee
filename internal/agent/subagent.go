package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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

// delegationName normalises the OPTIONAL name a sub_agent call may carry into the one form
// every display can paint on a single line: the first line only, trimmed of surrounding
// whitespace. A name that is empty after normalisation is ABSENT — the callers fall back to the
// delegated task's first line, exactly as they did before names existed. It runs once here at
// the recursion point rather than at each display, so a model that pads or newlines its name
// cannot break a status line or a prompt body downstream.
func delegationName(raw string) string {
	first, _, _ := strings.Cut(raw, "\n")
	return strings.TrimSpace(first)
}

// runSubAgent is the recursion point: it parses the delegated task, constructs a nested Agent
// bounded by this Agent's privileges (ADR 0005/0013), drives it to its Exchange boundary, and
// returns the sub-agent's final message as this call's tool result. A cancellation propagates
// out as dispatchCancelled so the parent rolls the whole Turn back (atomic-within-the-Turn);
// a FAULTED child Exchange — abandoned rather than completed, which closes on the same
// StatusExchangeComplete a real completion does — returns an ERROR result naming the fault
// instead of the child's last assistant text (StepResult.Faulted).
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

	sub, err := a.newChildAgent(call.ID, args.Task, delegationName(args.Name))
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
	if res.Faulted {
		// The nested Exchange was ABANDONED, not completed — an Upstream fault, a recovered
		// extension panic, or an overflow the child's one fold could not rescue. It closes on
		// StatusExchangeComplete exactly as a real completion does, so the fault marker is the
		// only thing that tells them apart, and reporting it as a success would hand the parent
		// model a placeholder — or, worse, stale mid-task text from an earlier child Turn
		// (finalMessageText scans backwards for the last assistant message) — as the delegated
		// result. An error result also books the delegation as HARMFUL rather than as a
		// productive write for self-regulation (noteToolProductivity, R3), so a failure can no
		// longer clear the parent's strikes and Turn Budget. The child's own ErrorEvent already
		// reached the shared EventSink at Depth+1, so the human sees the cause.
		return errorToolResult(call.ID, "sub-agent faulted before finishing the delegated task: "+
			"its exchange was abandoned (see the preceding error), so no result was produced"), dispatchDone
	}

	return domain.ToolResult{CallID: call.ID, Content: sub.finalMessageText(), IsError: false}, dispatchDone
}

// newChildAgent constructs the nested Agent for a sub-agent, threading this Agent's privileges
// bounded (ADR 0005/0013): the parent's LIVE Mode, LIVE confine-to-workspace flag, LIVE Bypass and
// LIVE auto-Compaction gate at spawn (Shift+Tab, /confine and the settings surface can move any of
// them mid-session — the child inherits what the parent
// actually has NOW, never a stale construction seed) / Approver / Confiner verbatim (never
// loosened beyond the parent's current privileges), PLUS a tighten-only
// live view of the parent's EFFECTIVE mode (child.liveMode) so a mid-delegation tightening
// reaches the still-running child at ANY depth, a Guards bundle that isolates live state but shares the dangerous
// floor read-only (Guards.ForSubAgent), a tool set that is a SUBSET of this Agent's tools
// (defaultSubAgentTools — never an expansion, and withholding sub_agent at the depth bound),
// the SAME Upstream responder and EventSink, the parent session's context-file content
// verbatim (copied, never re-read — a sub-agent is not a session boundary), and Depth =
// parent+1 so its events nest. The
// nested Agent is NOT given the parent's pending input or conversation — it starts fresh with only
// the delegated task (the ADR-0008 statelessness boundary). The allow-for-session approval memory is
// deliberately NOT on that withheld list: it is scoped to the SESSION rather than to an Agent, and
// it reaches the child through the very Approver threaded above — the shared queueing seam holds it
// (approvalCache in approvalcache.go), so a gate the human already cleared anywhere in the tree does
// not ask the child again, and an allow the child earns outlives it for the parent and its siblings.
//
// spawnCallID is the id of the sub_agent tool call being served — the child's RUN IDENTITY,
// stamped on every Event it emits (domain.EventBase.CallID). It is what tells one delegated
// stream from another once siblings share a depth (ADR 0039), so it is threaded at
// construction rather than at each emission: the child's own tools, Mechanisms and nested
// delegations all emit through its base() and inherit it for free.
//
// task is that same call's delegated task — the child's identity in WORDS rather than in ids, and
// the only one a human can read. It rides every Approval the child raises
// (domain.ApprovalRequest.SubAgentTask), so a prompt that queued behind a sibling's still says
// which agent is asking. It is threaded here for the same reason the id is: the child carries it
// for its whole run, and every gate it reaches is one it asks for itself.
//
// name is the OPTIONAL short name the same call may have supplied, already normalised by
// delegationName — the child's identity in a FEW words where the task is a sentence, so a
// display too narrow for the task can still say which delegation it is showing. Empty means the
// model named no delegation, and every display falls back to the task's first line. Like the
// task it is display identity only: it is never consulted for privilege (ADR 0005).
func (a *Agent) newChildAgent(spawnCallID, task, name string) (*Agent, error) {
	childCfg := a.cfg
	childCfg.Mode = a.Mode() // inherit the parent's LIVE mode at spawn (Shift+Tab may have changed it),
	//                          read under the lock since this runs on the worker goroutine during dispatch
	childCfg.ConfineToWorkspace = a.ConfineToWorkspace() // likewise the parent's LIVE blast radius at spawn
	//                                                     (/confine may have moved it since construction)
	childCfg.Bypass = a.bypassEnabled()                        // and the parent's LIVE Bypass + auto-Compaction gates, which the
	childCfg.Context.CompactionEnabled = a.compactionEnabled() // settings surface may have swapped since construction
	// The context-file NAMES are deliberately NOT re-read from the live list: the child copies the
	// parent's context-file CONTENT verbatim below, because a sub-agent is not a session boundary.
	childCfg.Tools = a.defaultSubAgentTools()
	// The sub-agent inherits the parent's ALREADY-BUILT catalogue (a.registry — the parent's
	// Config.Mechanisms merged with whatever Config.EnableMechanisms armed), so it fires the same
	// catalogued + experimental Mechanisms; an explicit per-sub-agent catalogue is a later refinement
	// (ADR 0013 leaves the default = the parent's). It inherits it through ForSubAgent rather than by
	// pointer: siblings in a depth-0 fan-out run AT ONCE (ADR 0039), so the child gets a registry of
	// its own — and a hook carrying live state gets a per-child instance, the same isolate-the-live-
	// state / share-the-read-only-floor answer Guards.ForSubAgent gives one line down. A hook with
	// nothing live to isolate is still inherited verbatim, so the delegation is unchanged for every
	// Mechanism in today's catalogue. EnableMechanisms is cleared because those IDs are already built
	// into the inherited registry — re-building them into it would trip the already-registered
	// rejection and fail every sub-agent spawn.
	childCfg.Mechanisms = a.registry.ForSubAgent()
	childCfg.EnableMechanisms = nil

	child, err := newAgent(childCfg, a.upstream)
	if err != nil {
		return nil, err
	}
	child.depth = a.depth + 1
	child.callID = spawnCallID
	child.task = task
	child.name = name
	child.guards = a.guards.ForSubAgent()
	// The child belongs to the PARENT's session, so it speaks from the parent's context-file
	// bytes: copy the cache over the one its own construction just read. A sub-agent is not a
	// session boundary, so an AGENTS.md edited (or deleted) mid-delegation must not reach it.
	child.contextFiles = a.contextFiles
	// A tighten-only view of the parent's EFFECTIVE mode (ADR 0013): the child's disposition takes
	// TighterMode(parentEffective, spawnMode), so a parent tightening mid-delegation reaches the
	// child while a parent loosening cannot loosen it. It is the parent's effectiveMode accessor —
	// not its own Mode — so the view COMPOSES down the chain: a depth-2 grandchild reads its
	// parent's effective mode, which already folds in the top-level agent's live mode, and a
	// top-level tightening therefore reaches every descendant rather than stopping at depth 1.
	// Capturing an accessor (not the raw field/mutex) keeps every read modeMu-guarded at the agent
	// that owns the field, so the child reads race-free but has no seam to mutate anything.
	child.liveMode = a.effectiveMode
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
// model always receives an intelligible result. It is reached only for a COMPLETED Exchange:
// runSubAgent answers a faulted one with an error result before it gets here, so neither that
// note nor a stale mid-task message can stand in for a delegation that never finished.
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
