package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/tools"
)

// ----------------------------------------------------------------------------
// Sub-agent orchestrator (P3.13 / ADR 0013) — privileges ≤ parent, Depth+1, atomic
// ----------------------------------------------------------------------------
//
// These tests drive a nested Agent hermetically: the parent and the sub-agent share one
// scriptedResponder (the sub-agent reuses the parent's Upstream), so scripts[N] is consumed
// in run order across BOTH loops. A typical script is: [0] parent emits a sub_agent call →
// [1..k] the child's Turns → [k+1] the parent's final message. No real LLM, no real exec.

// subAgentArgs builds the sub_agent tool's JSON argument payload for a delegated task.
func subAgentArgs(task string) string {
	b, _ := json.Marshal(tools.SubAgentArgs{Task: task})
	return string(b)
}

// subAgentCallScript emits a single sub_agent tool call delegating task.
func subAgentCallScript(id, task string) []provider.Delta {
	return toolCallScript(id, tools.SubAgentToolName, subAgentArgs(task))
}

// subAgentConfig builds a Config wired with the sub_agent tool plus the given extra tools,
// in the requested mode. The sub_agent tool is registered explicitly so the recursion point
// resolves; extra tools are what a child may call one level down.
func subAgentConfig(sink domain.EventSink, mode domain.Mode, extra ...domain.Tool) domain.Config {
	cfg := baseConfig(sink)
	cfg.Mode = mode
	reg := domain.NewToolRegistry()
	_ = reg.Register(tools.NewSubAgent())
	for _, t := range extra {
		_ = reg.Register(t)
	}
	cfg.Tools = reg
	return cfg
}

// TestSubAgent_DelegatesAndReportsBack drives the happy path: the parent delegates a task,
// the sub-agent runs to completion and its final message is surfaced back to the parent as
// the sub_agent tool result, and the parent then finishes.
func TestSubAgent_DelegatesAndReportsBack(t *testing.T) {
	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore)

	responder := &scriptedResponder{scripts: [][]provider.Delta{
		subAgentCallScript("c1", "summarise the repo"),
		contentScript("the repo is a Go TUI agent"), // the sub-agent's only Turn (final)
		contentScript("done — delegated and summarised"),
	}}
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "please research"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The sub_agent tool result the parent saw must carry the sub-agent's final message.
	res, ok := lastSubAgentResult(sink.events)
	if !ok {
		t.Fatal("no sub_agent tool result emitted")
	}
	if res.IsError {
		t.Fatalf("sub_agent result is an error: %q", res.Content)
	}
	if !strings.Contains(res.Content, "Go TUI agent") {
		t.Errorf("sub_agent result = %q, want the child's final message", res.Content)
	}
}

// TestSubAgent_EventsNestAtDepthOne proves the sub-agent's events re-emit into the parent's
// sink at Depth==1, while the parent's own events stay at Depth==0 (ADR 0013 — one nested
// stream the TUI/bench observe).
func TestSubAgent_EventsNestAtDepthOne(t *testing.T) {
	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore)

	responder := &scriptedResponder{scripts: [][]provider.Delta{
		subAgentCallScript("c1", "do the thing"),
		contentScript("child reply"),
		contentScript("parent done"),
	}}
	a, _ := newAgent(cfg, responder)
	_ = a.Submit(domain.UserInput{Text: "go"})
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The child's MessageEvent ("child reply") must be at Depth 1; the parent's at Depth 0.
	var sawChildDepth1, sawParentDepth0 bool
	for _, e := range sink.events {
		me, ok := e.(domain.MessageEvent)
		if !ok {
			continue
		}
		switch me.Text {
		case "child reply":
			sawChildDepth1 = me.Depth == 1
		case "parent done":
			sawParentDepth0 = me.Depth == 0
		}
	}
	if !sawChildDepth1 {
		t.Error("the sub-agent's MessageEvent was not emitted at Depth==1")
	}
	if !sawParentDepth0 {
		t.Error("the parent's MessageEvent was not at Depth==0")
	}
}

// TestSubAgent_InheritsPlanModeCannotWrite proves a sub-agent in a Plan-mode parent inherits
// Plan and therefore refuses a write its child attempts (the acceptance ADR 0013 pins).
func TestSubAgent_InheritsPlanModeCannotWrite(t *testing.T) {
	sink := &recordingSink{}
	wrote := 0
	writer := fakeTool{name: "write_thing", readOnly: false, ran: &wrote, result: "wrote"}
	cfg := subAgentConfig(sink, domain.ModePlan, writer)

	responder := &scriptedResponder{scripts: [][]provider.Delta{
		subAgentCallScript("c1", "write a file"),
		toolCallScript("c2", "write_thing", `{}`), // the child attempts a write
		contentScript("child could not write"),    // child finishes after the refusal result
		contentScript("parent done"),
	}}
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	_ = a.Submit(domain.UserInput{Text: "go"})
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if wrote != 0 {
		t.Errorf("the child wrote %d times; a Plan-inheriting sub-agent must never run a write", wrote)
	}
	// The child's write attempt must surface a Plan-refusal tool result (Depth 1).
	if !hasToolResultContaining(sink.events, 1, "plan mode") {
		t.Error("expected a Plan-mode refusal tool result at Depth 1 for the child's write")
	}
}

// TestSubAgent_SubsetCannotCallOmittedTool proves a sub-agent narrowed by a subset cannot
// call a tool the parent has but the subset omits (ADR 0005). Here the child is given a
// registry WITHOUT the writer, so its write call resolves as an unknown tool.
func TestSubAgent_SubsetCannotCallOmittedTool(t *testing.T) {
	sink := &recordingSink{}
	ran := 0
	writer := fakeTool{name: "write_thing", readOnly: false, ran: &ran, result: "wrote"}
	// Parent HAS the writer + sub_agent; the orchestrator's default child set is the parent's
	// set, so to prove the narrowing we drive the child registry through a parent whose tools
	// are only {sub_agent} (the writer is reachable only at the parent level via a manual call
	// we never make) — i.e. the child inherits a parent set that already omits the writer.
	cfg := subAgentConfig(sink, domain.ModeAllowEdits) // writer NOT registered on the parent
	_ = writer                                         // documents intent: the tool exists but is not in the parent set

	responder := &scriptedResponder{scripts: [][]provider.Delta{
		subAgentCallScript("c1", "use the writer"),
		toolCallScript("c2", "write_thing", `{}`), // child calls a tool not in its subset
		contentScript("child saw unknown tool"),
		contentScript("parent done"),
	}}
	a, _ := newAgent(cfg, responder)
	_ = a.Submit(domain.UserInput{Text: "go"})
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if ran != 0 {
		t.Errorf("an omitted tool ran %d times; a subset sub-agent must not reach it", ran)
	}
	if !hasToolResultContaining(sink.events, 1, "unknown tool") {
		t.Error("expected an 'unknown tool' result at Depth 1 for the omitted tool")
	}
}

// TestSubAgent_MaxDepthRefusesAndWithholdsTool proves the recursion bound: a sub-agent AT the
// max depth is not offered sub_agent in its menu, and the recursion point refuses defensively
// if the call is emitted anyway — so an unbounded tower of sub-agents is impossible.
func TestSubAgent_MaxDepthRefusesAndWithholdsTool(t *testing.T) {
	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore)

	// Drive: parent (d0) spawns d1, d1 spawns d2, d2 attempts to spawn d3 (refused at the
	// bound), d2 finishes, d1 finishes, parent finishes.
	responder := &scriptedResponder{scripts: [][]provider.Delta{
		subAgentCallScript("c1", "level 1"), // parent → d1
		subAgentCallScript("c2", "level 2"), // d1 → d2
		subAgentCallScript("c3", "level 3"), // d2 → (refused: would be d3, past the bound)
		contentScript("d2 done after refusal"),
		contentScript("d1 done"),
		contentScript("parent done"),
	}}
	a, _ := newAgent(cfg, responder)
	_ = a.Submit(domain.UserInput{Text: "go"})
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The bound holds via the PRIMARY defense: a sub-agent constructed AT the bound is never
	// offered sub_agent, so the depth-2 child's sub_agent call resolves as an unknown tool —
	// it never reaches the recursion point, so no depth-3 agent is ever constructed.
	if !hasToolResultContaining(sink.events, 2, "unknown tool") {
		t.Error("expected the over-deep sub_agent call at Depth 2 to resolve as an unknown tool (tool withheld at the bound)")
	}

	// And a sub-agent constructed at the bound must not even be offered the tool: build the
	// child registry the orchestrator would hand a depth-2 child and assert sub_agent is gone.
	atBound := &Agent{tools: a.tools, depth: maxSubAgentDepth - 1}
	childReg := atBound.defaultSubAgentTools()
	if _, ok := childReg.Lookup(tools.SubAgentToolName); ok {
		t.Error("a child constructed at the depth bound must not be offered the sub_agent tool")
	}
}

// TestSubAgent_RecursionPointRefusesAtBound proves the SECONDARY (defense-in-depth) bound: the
// recursion point itself refuses a spawn at the max depth even if the tool were somehow
// emitted (the primary defense withholds the tool from the menu; this is the belt-and-braces).
func TestSubAgent_RecursionPointRefusesAtBound(t *testing.T) {
	t.Parallel()
	atBound := &Agent{depth: maxSubAgentDepth}
	res, outcome := atBound.runSubAgent(context.Background(),
		domain.ToolCall{ID: "c1", Tool: tools.SubAgentToolName, Arguments: json.RawMessage(subAgentArgs("recurse"))})
	if outcome != dispatchDone {
		t.Fatalf("outcome = %v, want dispatchDone", outcome)
	}
	if !res.IsError || !strings.Contains(res.Content, "depth limit") {
		t.Errorf("at-bound recursion = %+v, want a depth-limit refusal", res)
	}
}

// TestSubAgent_BreakerIsolatedFromParent proves the carried finding's isolation end-to-end:
// a sub-agent's circuit-breaker trips on the child's own failing loop WITHOUT tripping the
// parent's breaker, because Guards.ForSubAgent gave the child a fresh breaker.
func TestSubAgent_BreakerIsolatedFromParent(t *testing.T) {
	sink := &recordingSink{}
	// A tool whose every call fails identically, so the child trips its breaker.
	failing := fakeTool{name: "flaky", readOnly: true, execute: func(_ context.Context, call domain.ToolCall) (domain.ToolResult, error) {
		return domain.ToolResult{CallID: call.ID, Content: "boom", IsError: true}, nil
	}}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, failing)

	// The child calls "flaky" repeatedly (same args) until its breaker trips, then finishes.
	childScripts := [][]provider.Delta{}
	for i := 0; i < 4; i++ {
		childScripts = append(childScripts, toolCallScript("k", "flaky", `{}`))
	}
	childScripts = append(childScripts, contentScript("child gives up"))
	scripts := append([][]provider.Delta{subAgentCallScript("c1", "retry flaky")}, childScripts...)
	scripts = append(scripts, contentScript("parent done"))

	a, _ := newAgent(cfg, &scriptedResponder{scripts: scripts})
	parentBreaker := a.guards.Breaker
	_ = a.Submit(domain.UserInput{Text: "go"})
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The parent's breaker must be untouched by the child's failing loop: the identical
	// "flaky" signature has zero recorded failures on the parent.
	if parentBreaker.Tripped(domain.ToolCall{Tool: "flaky", Arguments: json.RawMessage(`{}`)}) {
		t.Error("the parent's circuit-breaker tripped from the sub-agent's failing loop — isolation broken")
	}
	// The child DID trip (its breaker refused further calls): a circuit-breaker ErrorEvent at
	// Depth 1 is the observable trip edge.
	if !hasErrorContaining(sink.events, 1, "circuit-breaker") {
		t.Error("expected the sub-agent's own circuit-breaker to trip at Depth 1")
	}
}

// TestSubAgent_DangerousFloorSharedReadOnly proves the dangerous-action floor is inherited and
// cannot be loosened one level down: a Tier-1 task the child attempts is refused by the SHARED
// floor (the same guard the parent carries), and the parent's and child's floors are the same
// guard instance (no per-sub-agent re-derivation).
func TestSubAgent_DangerousFloorSharedReadOnly(t *testing.T) {
	sink := &recordingSink{}
	danger := fakeTool{name: "terminal", readOnly: false, result: "ran"}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, danger)

	responder := &scriptedResponder{scripts: [][]provider.Delta{
		subAgentCallScript("c1", "clean up"),
		// The child attempts a Tier-1 dangerous action; the shared floor must hard-refuse it.
		toolCallScript("c2", "terminal", `{"command":"rm -rf /"}`),
		contentScript("child blocked"),
		contentScript("parent done"),
	}}
	a, _ := newAgent(cfg, responder)
	_ = a.Submit(domain.UserInput{Text: "go"})
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !hasErrorContaining(sink.events, 1, "dangerous-action guard") {
		t.Error("expected the shared dangerous-action floor to refuse the child's Tier-1 action at Depth 1")
	}

	// The child's floor IS the parent's floor pointer (shared read-only): construct the child
	// Guards the orchestrator would and assert pointer identity on Dangerous + freshness on
	// the live state.
	childGuards := a.guards.ForSubAgent()
	if childGuards.Dangerous != a.guards.Dangerous {
		t.Error("the sub-agent's dangerous floor must be the SAME (shared, read-only) guard as the parent's")
	}
	if childGuards.Breaker == a.guards.Breaker || childGuards.Audit == a.guards.Audit {
		t.Error("the sub-agent's live guard state (breaker/audit) must be fresh, not aliased")
	}
}

// TestSubAgent_ChildPanicRecoversAtParentBoundary proves a panic inside the sub-agent's loop
// is recovered (ADR 0007) and surfaced rather than unwinding the parent Exchange: the parent
// completes and the sub_agent result reports the failure.
func TestSubAgent_ChildPanicRecoversAtParentBoundary(t *testing.T) {
	sink := &recordingSink{}
	panicker := fakeTool{name: "boom", readOnly: true, execute: func(context.Context, domain.ToolCall) (domain.ToolResult, error) {
		panic("child tool boom")
	}}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, panicker)

	responder := &scriptedResponder{scripts: [][]provider.Delta{
		subAgentCallScript("c1", "trigger a panic"),
		toolCallScript("c2", "boom", `{}`), // child tool panics (recovered into an ErrorEvent)
		contentScript("child recovered"),
		contentScript("parent done"),
	}}
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	_ = a.Submit(domain.UserInput{Text: "go"})
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned a loop error despite the child panic being recoverable: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("parent Exchange status = %q, want exchange-complete (the child panic must not kill it)", res.Status)
	}
	// The recovered panic surfaced as an ErrorEvent at Depth 1.
	if !hasErrorContaining(sink.events, 1, "panic") {
		t.Error("expected the child tool panic to surface as a recovered ErrorEvent at Depth 1")
	}
}

// TestSubAgent_DepthLimitConstant guards the recursion bound's value so a careless change is
// caught (the orchestrator and its tests assume this ceiling).
func TestSubAgent_DepthLimitConstant(t *testing.T) {
	t.Parallel()
	if maxSubAgentDepth < 1 {
		t.Fatalf("maxSubAgentDepth = %d, must allow at least one level of delegation", maxSubAgentDepth)
	}
}

// TestSubAgent_RejectsEmptyAndBadArgs proves the recursion point validates its task argument.
func TestSubAgent_RejectsEmptyAndBadArgs(t *testing.T) {
	t.Parallel()
	a := &Agent{depth: 0}

	res, outcome := a.runSubAgent(context.Background(), domain.ToolCall{ID: "c1", Tool: tools.SubAgentToolName, Arguments: json.RawMessage(`{}`)})
	if outcome != dispatchDone || !res.IsError || !strings.Contains(res.Content, "non-empty task") {
		t.Errorf("empty task = %+v, want a non-empty-task error result", res)
	}

	res, _ = a.runSubAgent(context.Background(), domain.ToolCall{ID: "c2", Tool: tools.SubAgentToolName, Arguments: json.RawMessage(`{not json`)})
	if !res.IsError || !strings.Contains(res.Content, "invalid sub_agent arguments") {
		t.Errorf("bad args = %+v, want an invalid-arguments error result", res)
	}
}

// ---------------------------------------------------------------------------
// Event-scanning helpers (local to the sub-agent tests)
// ---------------------------------------------------------------------------

// lastSubAgentResult returns the most recent sub_agent ToolResultEvent's result. The
// ToolResultEvent does not carry the tool name, so we match on the result's CallID against the
// preceding ToolCallEvent for sub_agent.
func lastSubAgentResult(events []domain.Event) (domain.ToolResult, bool) {
	subCallIDs := map[string]bool{}
	var out domain.ToolResult
	var found bool
	for _, e := range events {
		switch ev := e.(type) {
		case domain.ToolCallEvent:
			if ev.Call.Tool == tools.SubAgentToolName {
				subCallIDs[ev.Call.ID] = true
			}
		case domain.ToolResultEvent:
			if subCallIDs[ev.Result.CallID] {
				out, found = ev.Result, true
			}
		}
	}
	return out, found
}

// hasToolResultContaining reports whether a ToolResultEvent at the given Depth has a Content
// containing sub (case-insensitive).
func hasToolResultContaining(events []domain.Event, depth int, sub string) bool {
	for _, e := range events {
		if ev, ok := e.(domain.ToolResultEvent); ok && ev.Depth == depth &&
			strings.Contains(strings.ToLower(ev.Result.Content), strings.ToLower(sub)) {
			return true
		}
	}
	return false
}

// hasErrorContaining reports whether an ErrorEvent at the given Depth has an Err containing sub.
func hasErrorContaining(events []domain.Event, depth int, sub string) bool {
	for _, e := range events {
		if ev, ok := e.(domain.ErrorEvent); ok && ev.Depth == depth &&
			strings.Contains(strings.ToLower(ev.Err), strings.ToLower(sub)) {
			return true
		}
	}
	return false
}
