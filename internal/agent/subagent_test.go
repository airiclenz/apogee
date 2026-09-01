package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
// scriptedResponder (no Delegation target is latched, so the sub-agent reuses the parent's
// Upstream — the routed spawn has its own tests in routedspawn_test.go), so scripts[N] is consumed
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

// TestSubAgent_EventsCarryTheSpawningCallID proves the RUN IDENTITY every delegated event now
// carries (ADR 0039): a child stamps the id of the sub_agent call that spawned it on every Event
// it emits, the top-level agent stamps none, and two delegations — which Depth alone cannot tell
// apart, since both children run at Depth 1 — carry different ids. It is the attribution
// concurrent fan-out rests on, pinned here while delegation is still serial so the identity is in
// place before the pool exists.
func TestSubAgent_EventsCarryTheSpawningCallID(t *testing.T) {
	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore)

	responder := &scriptedResponder{scripts: [][]provider.Delta{
		subAgentCallScript("c1", "first task"),
		contentScript("first child reply"),
		subAgentCallScript("c2", "second task"),
		contentScript("second child reply"),
		contentScript("parent done"),
	}}
	a, _ := newAgent(cfg, responder)
	_ = a.Submit(domain.UserInput{Text: "go"})
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Every event, whatever its variant: a delegated one names its spawning call, a top-level
	// one names none.
	ids := map[string]bool{}
	for _, e := range sink.events {
		base, ok := eventBaseOf(e)
		if !ok {
			t.Fatalf("eventBaseOf does not know %T — teach it the new variant", e)
		}
		if base.Depth == 0 {
			if base.CallID != "" {
				t.Errorf("%T at Depth 0 carries CallID %q, want empty — the top-level agent was spawned by no call", e, base.CallID)
			}
			continue
		}
		if base.CallID == "" {
			t.Errorf("%T at Depth %d carries no CallID, want the spawning call's id", e, base.Depth)
			continue
		}
		ids[base.CallID] = true
	}
	if len(ids) != 2 || !ids["c1"] || !ids["c2"] {
		t.Errorf("the delegated events carried ids %v, want exactly c1 and c2", ids)
	}

	// And each child's own answer is stamped with the call that asked for it — not merely with
	// SOME id, which a single shared stamp would also satisfy.
	want := map[string]string{"first child reply": "c1", "second child reply": "c2"}
	for _, e := range sink.events {
		me, ok := e.(domain.MessageEvent)
		if !ok {
			continue
		}
		id, tracked := want[me.Text]
		if !tracked {
			continue
		}
		if me.CallID != id {
			t.Errorf("the child's %q message carries CallID %q, want %q", me.Text, me.CallID, id)
		}
		delete(want, me.Text)
	}
	if len(want) != 0 {
		t.Errorf("never saw the child messages %v", want)
	}
}

// eventBaseOf returns the EventBase a variant embeds, so a test can read Depth and CallID without
// knowing which variant it holds. domain seals the Event interface with an unexported method, so a
// switch over the variants is the only way to reach the base from here; ok=false means the set
// grew a variant this switch has not been taught.
func eventBaseOf(e domain.Event) (domain.EventBase, bool) {
	switch ev := e.(type) {
	case domain.TokenEvent:
		return ev.EventBase, true
	case domain.ReasoningEvent:
		return ev.EventBase, true
	case domain.StreamResetEvent:
		return ev.EventBase, true
	case domain.MessageEvent:
		return ev.EventBase, true
	case domain.ToolCallEvent:
		return ev.EventBase, true
	case domain.ToolResultEvent:
		return ev.EventBase, true
	case domain.SubAgentPhaseEvent:
		return ev.EventBase, true // the CHILD run's identity: the delegation's own depth and call id
	case domain.ChildInterjectionEvent:
		return ev.EventBase, true // likewise the CHILD run's identity: the run the message was addressed to
	case domain.ApprovalEvent:
		return ev.EventBase, true
	case domain.MechanismFiredEvent:
		return ev.EventBase, true
	case domain.ErrorEvent:
		return ev.EventBase, true
	case domain.UsageEvent:
		return ev.EventBase, true
	case domain.AuditEvent:
		return ev.EventBase, true // the base's CallID, not the audited call's shadowing member
	default:
		return domain.EventBase{}, false
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

// ---------------------------------------------------------------------------
// A faulted delegation is reported as a failure, never as a result
// ---------------------------------------------------------------------------

// staleChildText is the mid-task narration a child commits BEFORE the fault that abandons its
// Exchange — the text finalMessageText scans back to, and which must never stand in for a
// delegated result that was never produced.
const staleChildText = "starting on it — reading the entry point first"

// contentThenToolCallScript emits one content chunk AND one tool call in the same stream: the
// shape of a model that narrates before acting, so the committed assistant message carries
// mid-task text.
func contentThenToolCallScript(text, id, name, args string) []provider.Delta {
	return append([]provider.Delta{{Kind: provider.DeltaContent, Content: text}},
		toolCallScript(id, name, args)...)
}

// faultedDelegationScripts drives one delegation whose child narrates, calls a tool, and then
// hits an Upstream fault on its next Turn — the child's Exchange is ABANDONED, which closes on
// the same StatusExchangeComplete a real completion returns. The parent then finishes.
func faultedDelegationScripts() [][]provider.Delta {
	return [][]provider.Delta{
		subAgentCallScript("c1", "summarise the repo"),
		contentThenToolCallScript(staleChildText, "c2", "read_thing", `{}`),
		errorScript("upstream: connection reset by peer"), // the child's next Turn faults
		contentScript("parent done"),
	}
}

// TestSubAgent_FaultedDelegationReportsAsError proves a child Exchange abandoned by an Upstream
// fault reaches the parent model as an ERROR result naming the fault — not as a success carrying
// the child's stale mid-task text (which is what an abandoned Turn's StatusExchangeComplete,
// indistinguishable from a real completion, used to produce).
func TestSubAgent_FaultedDelegationReportsAsError(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, reader)

	a, err := newAgent(cfg, &scriptedResponder{scripts: faultedDelegationScripts()})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "please research"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The child's fault is localised to the delegation: the PARENT's Exchange still completes.
	if res.Status != domain.StatusExchangeComplete || res.Faulted {
		t.Errorf("parent result = %+v, want a clean exchange-complete (the child's fault must not fault the parent)", res)
	}

	sub, ok := lastSubAgentResult(sink.events)
	if !ok {
		t.Fatal("no sub_agent tool result emitted")
	}
	if !sub.IsError {
		t.Errorf("sub_agent result IsError = false for a faulted delegation; content = %q", sub.Content)
	}
	if strings.Contains(sub.Content, staleChildText) {
		t.Errorf("sub_agent result = %q — stale mid-task text passed off as the delegated result", sub.Content)
	}
	if !strings.Contains(strings.ToLower(sub.Content), "fault") {
		t.Errorf("sub_agent result = %q, want a message naming the child fault", sub.Content)
	}
	// The human still sees the cause: the child's own ErrorEvent reached the shared sink at Depth 1.
	if !hasErrorContaining(sink.events, 1, "connection reset") {
		t.Error("expected the child's Upstream fault to surface as an ErrorEvent at Depth 1")
	}
}

// TestSubAgent_TransientChildBlipStaysInsideTheDelegation proves the re-stream reaches a DELEGATED
// exchange, which is where a transient fault hurts most: the child's Turn is the parent's tool
// call, so the blip used to abandon the child's Exchange, set Faulted, and hand the parent model
// "sub-agent faulted" in place of the work. The child's own loop now recovers before Faulted is
// ever set (subagent.go is unchanged — it never learns a blip happened), so the parent receives
// the delegated RESULT and nothing surfaces to the human.
func TestSubAgent_TransientChildBlipStaysInsideTheDelegation(t *testing.T) {
	shortRestreamHoldoff(t)

	const childAnswer = "the repo is a Go TUI agent"
	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore)

	a, err := newAgent(cfg, &scriptedResponder{scripts: [][]provider.Delta{
		subAgentCallScript("c1", "summarise the repo"),
		retryableErrorScript(transientFaultMsg), // the child's only Turn hits a transient blip
		contentScript(childAnswer),              // ... and its one re-stream lands
		contentScript("parent done"),
	}})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "please research"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete || res.Faulted {
		t.Errorf("parent result = %+v, want a clean exchange-complete", res)
	}

	sub, ok := lastSubAgentResult(sink.events)
	if !ok {
		t.Fatal("no sub_agent tool result emitted")
	}
	if sub.IsError {
		t.Errorf("sub_agent result IsError = true after a recovered blip; content = %q", sub.Content)
	}
	if !strings.Contains(sub.Content, childAnswer) {
		t.Errorf("sub_agent result = %q, want the child's recovered answer %q", sub.Content, childAnswer)
	}
	if errs := errorEvents(sink.events); len(errs) != 0 {
		t.Errorf("ErrorEvents = %v, want none — a recovered blip is silent at every Depth", errs)
	}
	if !hasEvent[domain.StreamResetEvent](sink.events) {
		t.Error("no StreamResetEvent emitted; the child's superseded partial stream was never retracted")
	}
}

// TestSubAgent_FaultedDelegationBooksNoProductiveWrite proves the second half of the same defect:
// a failed delegation must not feed self-regulation the PRODUCTIVE signal (sub_agent is not
// read-only, so a non-error result booked noteWrite), which cleared every strike, re-opened every
// suppressed Mechanism and lifted the Turn Budget on the strength of a failure (R3).
func TestSubAgent_FaultedDelegationBooksNoProductiveWrite(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, reader)

	a, err := newAgent(cfg, &scriptedResponder{scripts: faultedDelegationScripts()})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	// Seed a Session that has been going badly: the Turn Budget is tripped and a Mechanism
	// carries strikes. Only a PRODUCTIVE Turn clears those (selfRegulator.endTurn).
	const probe = domain.MechanismID("probe")
	a.tracker.harmfulStreak = turnBudgetLimit
	a.tracker.budgetTripped = true
	a.tracker.strikes[probe] = adaptiveSuppressStrikes - 1

	_ = a.Submit(domain.UserInput{Text: "please research"})
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	view := a.tracker.observed()
	if !view.BudgetTripped {
		t.Error("the Turn Budget was lifted by a FAILED delegation — a fault is not a productive write")
	}
	if view.HarmfulStreak < turnBudgetLimit {
		t.Errorf("harmful streak = %d, want it held at or above %d (a fault never resets it)", view.HarmfulStreak, turnBudgetLimit)
	}
	if view.Strikes[probe] != adaptiveSuppressStrikes-1 {
		t.Errorf("strikes[probe] = %d, want %d (a failed delegation clears nothing)", view.Strikes[probe], adaptiveSuppressStrikes-1)
	}
}

// TestSubAgent_CancelledChildRollsTheParentTurnBack pins the neighbouring row the fault marker
// must not disturb: a CANCELLED child still unwinds the parent Turn wholesale (D2) — no tool
// result is surfaced at all, and the cancel is not reported as a fault.
func TestSubAgent_CancelledChildRollsTheParentTurnBack(t *testing.T) {
	sink := &recordingSink{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The human presses Esc while the child is working: the tool's ctx is cancelled mid-call.
	interrupted := fakeTool{name: "read_thing", readOnly: true, execute: func(c context.Context, _ domain.ToolCall) (domain.ToolResult, error) {
		cancel()
		return domain.ToolResult{}, c.Err()
	}}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, interrupted)

	a, err := newAgent(cfg, &scriptedResponder{scripts: [][]provider.Delta{
		subAgentCallScript("c1", "summarise the repo"),
		toolCallScript("c2", "read_thing", `{}`), // the child's call is cancelled mid-flight
	}})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	_ = a.Submit(domain.UserInput{Text: "please research"})
	res, err := a.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != domain.StatusCancelled {
		t.Errorf("parent status = %q, want %q (a cancelled child rolls the parent Turn back)", res.Status, domain.StatusCancelled)
	}
	if res.Faulted {
		t.Error("a cancelled delegation reported as a fault; a cancel is a re-attemptable rollback")
	}
	if sub, ok := lastSubAgentResult(sink.events); ok {
		t.Errorf("a cancelled delegation surfaced a tool result (%+v); no partial result may reach the parent", sub)
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

// lastErrorAtDepth returns the Err of the most recent ErrorEvent emitted at the given Depth — the
// cause the human read for that agent's run, and what a caller asserts a derived message against.
func lastErrorAtDepth(events []domain.Event, depth int) (string, bool) {
	var out string
	var found bool
	for _, e := range events {
		if ev, ok := e.(domain.ErrorEvent); ok && ev.Depth == depth {
			out, found = ev.Err, true
		}
	}
	return out, found
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

// TestSubAgentInheritsSystemPrompt: the configured system prompt (ADR 0023) reaches a
// sub-agent through newChildAgent's wholesale cfg copy — no carve-out, so a delegated task
// runs under the same persona and context as the parent.
func TestSubAgentInheritsSystemPrompt(t *testing.T) {
	cfg := subAgentConfig(&recordingSink{}, domain.ModeAskBefore)
	cfg.SystemPrompt = "You are apogee in {{workspace}} on {{datetime}} in {{mode}} mode."

	a, err := newAgent(cfg, &recordingResponder{reply: "unused"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	child, err := a.newChildAgent("call_sub", "the delegated task", "")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}
	if child.cfg.SystemPrompt != a.cfg.SystemPrompt {
		t.Errorf("child SystemPrompt = %q, want the parent's %q", child.cfg.SystemPrompt, a.cfg.SystemPrompt)
	}
}

// subAgentNamedArgs builds the sub_agent tool's JSON argument payload for a delegated task that
// also carries the optional short name.
func subAgentNamedArgs(task, name string) string {
	b, _ := json.Marshal(tools.SubAgentArgs{Task: task, Name: name})
	return string(b)
}

// TestDelegationNameNormalisesToATrimmedFirstLine pins the one normalisation the recursion point
// performs on a model-supplied name, so no display downstream has to defend itself: the first
// line only, trimmed. Anything that normalises to nothing is ABSENT — the signal every caller
// reads as "fall back to the task".
func TestDelegationNameNormalisesToATrimmedFirstLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"plain", "repo-scout", "repo-scout"},
		{"padded", "   repo-scout\t ", "repo-scout"},
		{"multi-line keeps the first line", "repo-scout\nand then some prose", "repo-scout"},
		{"padded multi-line", "  repo-scout  \n more prose\n", "repo-scout"},
		{"carriage return", "repo-scout\r\nprose", "repo-scout"},
		{"missing", "", ""},
		{"whitespace only", "   \n  ", ""},
		{"leading blank line is absent", "\nrepo-scout", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := delegationName(tc.raw); got != tc.want {
				t.Errorf("delegationName(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestSubAgent_ChildCarriesTheDelegationName proves the spawn seam stamps the name beside the
// child's other identity fields, and that an unnamed delegation leaves it empty so every display
// falls back to the task. The name is DISPLAY identity only (ADR 0005): the child's task and
// spawning call id must be untouched by it.
func TestSubAgent_ChildCarriesTheDelegationName(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	a, err := newAgent(subAgentConfig(sink, domain.ModeAskBefore), &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	named, err := a.newChildAgent("c1", "summarise the repo", "repo-scout")
	if err != nil {
		t.Fatalf("newChildAgent (named): %v", err)
	}
	if named.name != "repo-scout" {
		t.Errorf("named child name = %q, want %q", named.name, "repo-scout")
	}
	if named.task != "summarise the repo" {
		t.Errorf("named child task = %q, want the delegated task", named.task)
	}
	if named.callID != "c1" {
		t.Errorf("named child callID = %q, want c1", named.callID)
	}

	unnamed, err := a.newChildAgent("c2", "summarise the repo", "")
	if err != nil {
		t.Fatalf("newChildAgent (unnamed): %v", err)
	}
	if unnamed.name != "" {
		t.Errorf("unnamed child name = %q, want empty — the displays fall back to the task", unnamed.name)
	}
}

// TestSubAgent_NamedDelegationStillReportsBack drives the whole recursion point with a name in
// the arguments: the optional field must parse, normalise and delegate exactly as a bare task
// does, so adding a name can never cost a model its delegation.
func TestSubAgent_NamedDelegationStillReportsBack(t *testing.T) {
	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore)

	responder := &scriptedResponder{scripts: [][]provider.Delta{
		toolCallScript("c1", tools.SubAgentToolName, subAgentNamedArgs("summarise the repo", "  repo-scout\nignored prose")),
		contentScript("the repo is a Go TUI agent"),
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

	res, ok := lastSubAgentResult(sink.events)
	if !ok {
		t.Fatal("no sub_agent tool result emitted")
	}
	if res.IsError {
		t.Fatalf("named sub_agent result is an error: %q", res.Content)
	}
	if !strings.Contains(res.Content, "Go TUI agent") {
		t.Errorf("named sub_agent result = %q, want the child's final message", res.Content)
	}
}

// TestUnroutedChildNeverClosesTheParentsClient: an unrouted spawn BORROWS the session's Upstream,
// so the child owns nothing and its Close leaves the connection the parent is still speaking over
// exactly as it was. The parent remains the one owner, and its own Close is what finally tears the
// client down — the whole point of tracking ownership rather than closing whatever is in hand.
func TestUnroutedChildNeverClosesTheParentsClient(t *testing.T) {
	t.Parallel()

	shared := &closingResponder{}
	parent, err := newAgent(subAgentConfig(&recordingSink{}, domain.ModeAskBefore), shared)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	parent.ownsUpstream = true // what New does for a session that dialled its own client

	child, err := parent.newChildAgent("c1", "summarise the repo", "")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}
	if child.upstream != provider.Responder(shared) {
		t.Fatalf("unrouted child Upstream = %T, want the parent's shared responder", child.upstream)
	}
	if child.ownsUpstream {
		t.Error("unrouted child claims to own the parent's client")
	}

	if err := child.Close(); err != nil {
		t.Fatalf("child Close: %v", err)
	}
	if shared.closes != 0 {
		t.Fatalf("the child closed the parent's client %d times, want 0 — the session still speaks over it",
			shared.closes)
	}

	if err := parent.Close(); err != nil {
		t.Fatalf("parent Close: %v", err)
	}
	if shared.closes != 1 {
		t.Errorf("the owning parent closed its client %d times, want exactly 1", shared.closes)
	}
}

// ----------------------------------------------------------------------------
// The delegate step cap (plan 2026-08-26 - 00, item 2)
// ----------------------------------------------------------------------------
//
// A delegate that keeps asking for tools is bounded by Config.Delegation.MaxSteps: Agent.Run
// ends its Exchange at the cap, cleanly rather than faulted, and the parent receives a NON-error
// partial result. These tests drive the bound end to end through the same scripted responder the
// tests above share (scripts[N] is consumed in run order across BOTH loops).

// narratedToolCallScript is a stream that emits visible text AND a tool call in one reply — a
// delegate narrating as it works. It is what makes a step-capped child's "last visible text"
// non-empty, which is exactly what the partial result hands back to the parent.
func narratedToolCallScript(id, name, args, text string) []provider.Delta {
	return []provider.Delta{
		{Kind: provider.DeltaContent, Content: text},
		{Kind: provider.DeltaToolCall, ToolCall: &provider.ToolCall{
			ID:       id,
			Type:     "function",
			Function: provider.FunctionCall{Name: name, Arguments: args},
		}},
		{Kind: provider.DeltaDone, FinishReason: "tool_calls"},
	}
}

// cappedChildTurns returns n Turns of a child that reads a file and narrates each time — the
// shape the cap exists for (the 633-Turn lens delegation in the plan's evidence).
func cappedChildTurns(n int) [][]provider.Delta {
	out := make([][]provider.Delta, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, narratedToolCallScript(
			fmt.Sprintf("t%d", i), "read_thing", `{}`, fmt.Sprintf("reading file %d", i)))
	}
	return out
}

// subAgentArgsCapped builds the sub_agent argument payload for a delegation that asks for a
// LOWER step cap than the host configured.
func subAgentArgsCapped(task string, maxSteps int) string {
	b, _ := json.Marshal(tools.SubAgentArgs{Task: task, MaxSteps: maxSteps})
	return string(b)
}

// childClosingReport is what a capped delegate answers its tool-less wrap-up request with — the
// report the parent reads under the partial marker, distinct from every narration the child wrote
// during its capped Turns so a test can tell the authored text from the scavenged one.
const childClosingReport = "I read two files; the third is unread and the survey is unfinished."

// countCapErrors returns how many ErrorEvents at the given Depth name the step cap.
func countCapErrors(events []domain.Event, depth int) int {
	n := 0
	for _, e := range events {
		if ev, ok := e.(domain.ErrorEvent); ok && ev.Depth == depth &&
			strings.Contains(ev.Err, "step cap") {
			n++
		}
	}
	return n
}

// TestRunEndsTheExchangeAtTheStepCap pins the bound at its enforcement site: an Agent with a cap
// that keeps asking for tools has its Exchange ENDED by Run, on a clean StatusExchangeComplete
// boundary marked StepCapped and NOT Faulted, after exactly cap working Turns PLUS the one
// tool-less wrap-up Turn the cap spends on a closing report (finishAtStepCap).
func TestRunEndsTheExchangeAtTheStepCap(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, reader)

	responder := &requestLogResponder{scripts: cappedChildTurns(10)}
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.stepCap = 3 // what newChildAgent seeds on a delegate; a top-level Agent is left at 0

	if err := a.Submit(domain.UserInput{Text: "trawl the repo"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("Status = %q, want %q — the cap ends the Exchange", res.Status, domain.StatusExchangeComplete)
	}
	if !res.StepCapped {
		t.Error("StepCapped not set on a capped Exchange")
	}
	if res.Faulted {
		t.Error("Faulted set on a capped Exchange; the cap is not a failure")
	}
	// Three working Turns and then ONE more: the wrap-up Turn is EXTRA and uncounted, so the cap
	// still buys the three requests it names and the fourth is the closing report.
	if responder.calls != 4 {
		t.Errorf("upstream calls = %d, want 4 — three working Turns plus the wrap-up", responder.calls)
	}
	if got := len(responder.requests[3].Tools); got != 0 {
		t.Errorf("the wrap-up request carries %d tools, want 0 — the menu is withdrawn for it", got)
	}
	// The counter names the next Turn: the wrap-up ends through endExchangeDone, which advances
	// once for it, so a capped child ends at cap+1 — the index encodeState stores (state.go) and a
	// resume reads back must match the Turns actually taken, no more and no fewer.
	if a.turns.index != 4 {
		t.Errorf("turn index = %d, want 4 — cap Turns plus the wrap-up, advanced exactly once each", a.turns.index)
	}
	if got := countCapErrors(sink.events, 0); got != 1 {
		t.Errorf("step-cap ErrorEvents = %d, want exactly 1", got)
	}
	if !hasErrorContaining(sink.events, 0, "raise delegate-max-steps") {
		t.Error("the step-cap ErrorEvent does not name the key that raises the bound")
	}
	if !hasErrorContaining(sink.events, 0, "asking it to sum up") {
		t.Error("the step-cap ErrorEvent does not say the engine is asking the delegate to sum up")
	}
}

// TestSubAgent_StepCapReturnsAPartialResultToTheParent proves the parent's side of the bound: a
// delegation stopped at its cap is NOT an error result — it carries the marker line plus the
// closing report the tool-less wrap-up Turn authored — and the parent's own Turn continues to a
// normal Exchange end.
func TestSubAgent_StepCapReturnsAPartialResultToTheParent(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, reader)
	cfg.Delegation.MaxSteps = 3

	scripts := [][]provider.Delta{subAgentCallScript("c1", "trawl the repo")}
	scripts = append(scripts, cappedChildTurns(3)...)
	// The child's 4th request is the wrap-up: its menu is gone, so what it answers with is the
	// report the parent reads rather than narration scavenged from a tool round.
	scripts = append(scripts, contentScript(childClosingReport))
	scripts = append(scripts, contentScript("parent done"))
	responder := &requestLogResponder{scripts: scripts}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "please research"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The child's cap is localised to the delegation: the PARENT's Exchange still completes.
	if res.Status != domain.StatusExchangeComplete || res.Faulted || res.StepCapped {
		t.Errorf("parent result = %+v, want a clean uncapped exchange-complete", res)
	}
	if responder.calls != len(scripts) {
		t.Errorf("upstream calls = %d, want %d — the parent Turn must continue after the capped child",
			responder.calls, len(scripts))
	}

	sub, ok := lastSubAgentResult(sink.events)
	if !ok {
		t.Fatal("no sub_agent tool result emitted")
	}
	if sub.IsError {
		t.Errorf("sub_agent result IsError = true for a capped delegation; the partial work stands: %q", sub.Content)
	}
	marker := fmt.Sprintf(stepCapResultFormat, 3)
	if !strings.HasPrefix(sub.Content, marker+"\n") {
		t.Errorf("sub_agent result = %q, want it to open with %q", sub.Content, marker)
	}
	if !strings.HasSuffix(sub.Content, childClosingReport) {
		t.Errorf("sub_agent result = %q, want it to end with the wrap-up reply %q", sub.Content, childClosingReport)
	}
	// The wrap-up request itself: the child's last one, sent with the menu withdrawn — which is
	// what makes the report a report instead of a fourth tool call.
	if got := len(responder.requests[len(scripts)-2].Tools); got != 0 {
		t.Errorf("the wrap-up request carries %d tools, want 0", got)
	}
	// The human sees the cause on the child's own stream, once, and it says what happens next.
	if got := countCapErrors(sink.events, 1); got != 1 {
		t.Errorf("step-cap ErrorEvents at Depth 1 = %d, want exactly 1", got)
	}
	if !hasErrorContaining(sink.events, 1, "asking it to sum up") {
		t.Error("the step-cap ErrorEvent does not say the engine is asking the delegate to sum up")
	}
}

// TestSubAgent_StepCapFallsBackWhenTheWrapUpFaults drives the ratified fallback: the wrap-up Turn
// is a best effort, so an Upstream fault on it must never turn a capped delegation into a FAILED
// one. The parent still gets the non-error partial result, carrying the text the child managed
// before the cap.
func TestSubAgent_StepCapFallsBackWhenTheWrapUpFaults(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, reader)
	cfg.Delegation.MaxSteps = 3

	scripts := [][]provider.Delta{subAgentCallScript("c1", "trawl the repo")}
	scripts = append(scripts, cappedChildTurns(3)...)
	scripts = append(scripts, errorScript("upstream exploded on the wrap-up"))
	scripts = append(scripts, contentScript("parent done"))

	a, err := newAgent(cfg, &scriptedResponder{scripts: scripts})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	_ = a.Submit(domain.UserInput{Text: "please research"})
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.Status != domain.StatusExchangeComplete || res.Faulted {
		t.Errorf("parent result = %+v, want a clean exchange-complete — a failed wrap-up is the child's, not the parent's", res)
	}

	sub, ok := lastSubAgentResult(sink.events)
	if !ok {
		t.Fatal("no sub_agent tool result emitted")
	}
	if sub.IsError {
		t.Errorf("sub_agent result IsError = true after a faulted wrap-up; the cap is never reported as a failure: %q", sub.Content)
	}
	if strings.Contains(sub.Content, subAgentFaultPrefix) {
		t.Errorf("sub_agent result = %q, want the step-cap result, not the fault result", sub.Content)
	}
	marker := fmt.Sprintf(stepCapResultFormat, 3)
	if want := marker + "\n" + "reading file 2"; sub.Content != want {
		t.Errorf("sub_agent result = %q, want %q — the pre-cap last visible text", sub.Content, want)
	}
}

// TestSubAgent_StepCapMarksAWordlessDelegate covers the child that spent every capped Turn
// calling tools and never said anything — not even when its menu was withdrawn and it was asked to
// sum up: the parent still gets an intelligible result rather than a bare marker with an empty
// body, and never the "completed" note a finished child would get.
func TestSubAgent_StepCapMarksAWordlessDelegate(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, reader)
	cfg.Delegation.MaxSteps = 2

	scripts := [][]provider.Delta{
		subAgentCallScript("c1", "trawl the repo"),
		toolCallScript("t0", "read_thing", `{}`), // no visible text on either child Turn
		toolCallScript("t1", "read_thing", `{}`),
		// …and none on the wrap-up either: a child that answers its closing request with nothing
		// but another tool call commits no assistant message, so there is still nothing to show.
		toolCallScript("t2", "read_thing", `{}`),
		contentScript("parent done"),
	}
	a, err := newAgent(cfg, &scriptedResponder{scripts: scripts})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	_ = a.Submit(domain.UserInput{Text: "please research"})
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	sub, ok := lastSubAgentResult(sink.events)
	if !ok {
		t.Fatal("no sub_agent tool result emitted")
	}
	if want := fmt.Sprintf(stepCapResultFormat, 2) + "\n" + stepCapNoTextMarker; sub.Content != want {
		t.Errorf("sub_agent result = %q, want %q", sub.Content, want)
	}
	if strings.Contains(sub.Content, "completed") {
		t.Errorf("sub_agent result = %q — a capped delegation must never be reported as completed", sub.Content)
	}
}

// TestSubAgent_StepCapZeroIsUnbounded proves 0 means OFF: a child that takes more Turns than any
// cap in these tests still runs to its own final answer and reports it as a plain success.
func TestSubAgent_StepCapZeroIsUnbounded(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, reader)
	cfg.Delegation.MaxSteps = 0

	scripts := [][]provider.Delta{subAgentCallScript("c1", "trawl the repo")}
	scripts = append(scripts, cappedChildTurns(4)...)
	scripts = append(scripts, contentScript("the child's own final answer"), contentScript("parent done"))
	a, err := newAgent(cfg, &scriptedResponder{scripts: scripts})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	_ = a.Submit(domain.UserInput{Text: "please research"})
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	sub, ok := lastSubAgentResult(sink.events)
	if !ok {
		t.Fatal("no sub_agent tool result emitted")
	}
	if sub.IsError || sub.Content != "the child's own final answer" {
		t.Errorf("sub_agent result = %+v, want the child's own final answer (cap 0 = unbounded)", sub)
	}
	if got := countCapErrors(sink.events, 1); got != 0 {
		t.Errorf("step-cap ErrorEvents = %d with the cap switched off, want 0", got)
	}
}

// TestSubAgent_MaxStepsArgumentOnlyLowersTheCap pins the argument's one direction: a request
// BELOW the configured cap binds this delegation, a request ABOVE it changes nothing. The model
// may make a delegation cheaper, never longer than the host allows.
func TestSubAgent_MaxStepsArgumentOnlyLowersTheCap(t *testing.T) {
	cases := []struct {
		name       string
		configured int
		requested  int
		wantSteps  int
	}{
		{"a lower request binds", 3, 2, 2},
		{"a higher request is ignored", 3, 9, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
			cfg := subAgentConfig(sink, domain.ModeAskBefore, reader)
			cfg.Delegation.MaxSteps = tc.configured

			scripts := [][]provider.Delta{
				toolCallScript("c1", tools.SubAgentToolName, subAgentArgsCapped("trawl the repo", tc.requested)),
			}
			// wantSteps working Turns plus the one tool-less wrap-up Turn the cap spends: the
			// bound governs the WORK, and the closing report is extra however low it is set.
			scripts = append(scripts, cappedChildTurns(tc.wantSteps+1)...)
			scripts = append(scripts, contentScript("parent done"))
			responder := &scriptedResponder{scripts: scripts}

			a, err := newAgent(cfg, responder)
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			_ = a.Submit(domain.UserInput{Text: "please research"})
			if _, err := a.Run(context.Background()); err != nil {
				t.Fatalf("Run: %v", err)
			}

			if responder.calls != len(scripts) {
				t.Errorf("upstream calls = %d, want %d — the child ran a different number of Turns than the effective cap plus its wrap-up",
					responder.calls, len(scripts))
			}
			sub, ok := lastSubAgentResult(sink.events)
			if !ok {
				t.Fatal("no sub_agent tool result emitted")
			}
			if want := fmt.Sprintf(stepCapResultFormat, tc.wantSteps); !strings.HasPrefix(sub.Content, want+"\n") {
				t.Errorf("sub_agent result = %q, want it to open with %q", sub.Content, want)
			}
		})
	}
}

// TestStepCapNeverBoundsTheMainAgent holds the delegates-only line: the key is set, the top-level
// Agent takes more Turns than it, and nothing stops it — the main loop is the human's to stop.
func TestStepCapNeverBoundsTheMainAgent(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	cfg := baseConfig(sink)
	cfg.Mode = domain.ModeAskBefore
	reg := domain.NewToolRegistry()
	_ = reg.Register(reader)
	cfg.Tools = reg
	cfg.Delegation.MaxSteps = 1

	scripts := cappedChildTurns(3)
	scripts = append(scripts, contentScript("the main agent's own final answer"))
	a, err := newAgent(cfg, &scriptedResponder{scripts: scripts})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if a.stepCap != 0 {
		t.Fatalf("top-level Agent constructed with stepCap = %d, want 0 — the cap is a delegate bound", a.stepCap)
	}
	_ = a.Submit(domain.UserInput{Text: "do the work"})
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.StepCapped {
		t.Error("the top-level Agent was step-capped; the key bounds delegates only")
	}
	if got := countCapErrors(sink.events, 0); got != 0 {
		t.Errorf("step-cap ErrorEvents at Depth 0 = %d, want 0", got)
	}
	if !hasMessageAtDepth(sink.events, 0, "the main agent's own final answer") {
		t.Error("the main agent did not reach its own final answer")
	}
}

// foldedSummaryRequest returns the first recorded request carrying a message with the canned
// summary text — the request the model saw immediately AFTER an auto-fold, which is the one whose
// shape a fold can break.
func foldedSummaryRequest(reqs []provider.Request, summary string) (provider.Request, bool) {
	for _, req := range reqs {
		for _, m := range req.Messages {
			if strings.Contains(m.Content, summary) {
				return req, true
			}
		}
	}
	return provider.Request{}, false
}

// TestSubAgent_ChildFoldsMidDelegationAndFinishes is the delegate half of the child's mid-Exchange
// fold: a delegation whose tool result pushes its history past its Budget allocation folds DURING
// the delegation — there is no Exchange boundary to wait for, the whole delegation being one
// Exchange — and still reports its answer to the parent. The request the child sends after the
// fold is template-legal (no orphaned tool result, no unanswered tool call, strict alternation),
// which is what makes the quiescent Turn boundary a safe place to fold.
func TestSubAgent_ChildFoldsMidDelegationAndFinishes(t *testing.T) {
	sink := &recordingSink{}
	// ~25k chars ≈ 6.2k tokens, past the ~3.9k-token History allocation of the 8k window below.
	bulky := fakeTool{name: "read_thing", readOnly: true, result: strings.Repeat("x", 25000)}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, bulky)
	cfg.Context.MaxContextTokens = 8192
	cfg.Context.CompactionEnabled = true

	up := &scriptedCompactResponder{
		summaryReply: "CHILD-SUMMARY",
		scripts: [][]provider.Delta{
			subAgentCallScript("c1", "trawl the repo"),    // parent Turn 0: delegate
			toolCallScript("t1", "read_thing", `{}`),      // child Turn 0: the oversized read
			contentScript("the child's own final answer"), // child Turn 1: folds at its top, then answers
			contentScript("parent done"),                  // parent Turn 1: finish
		},
	}
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "please research"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if up.summaryCalls != 1 {
		t.Fatalf("folds during the delegation = %d, want exactly 1 — the child must fold mid-Exchange", up.summaryCalls)
	}
	sub, ok := lastSubAgentResult(sink.events)
	if !ok {
		t.Fatal("no sub_agent tool result emitted")
	}
	if sub.IsError || sub.Content != "the child's own final answer" {
		t.Errorf("sub_agent result = %+v, want the child's own final answer after its mid-run fold", sub)
	}
	if n := countCompactionErrors(sink.events); n != 0 {
		t.Errorf("the child's fold emitted %d compaction ErrorEvents, want 0", n)
	}

	req, ok := foldedSummaryRequest(up.requests, "CHILD-SUMMARY")
	if !ok {
		t.Fatal("no request carried the folded summary; the child's post-fold request was not observed")
	}
	assertRequestTemplateLegal(t, req)

	// The shape the trailing-role half of that check stands on: a delegation has no Exchange
	// opening for a user message to arrive at, so the fold owes the request its own bridge and the
	// request ends assistant-summary | user(bridge) rather than on the summary.
	if len(req.Messages) < 2 {
		t.Fatalf("the child's post-fold request carries %d messages, want at least the summary and its bridge", len(req.Messages))
	}
	summary, bridge := req.Messages[len(req.Messages)-2], req.Messages[len(req.Messages)-1]
	if summary.Role != string(domain.RoleAssistant) || !strings.Contains(summary.Content, "CHILD-SUMMARY") {
		t.Errorf("the request's second-to-last message is %q/%q, want the assistant's fold summary", summary.Role, summary.Content)
	}
	if bridge.Role != string(domain.RoleUser) || bridge.Content != overflowBridge {
		t.Errorf("the request's last message is %q/%q, want the user overflow bridge", bridge.Role, bridge.Content)
	}
}

// TestSubAgent_ChildNeverFoldsWithAutoCompactOff holds the one gate the child's mid-Exchange fold
// does NOT lift: `auto-compact: false` opts a delegation out exactly as it opts the main loop out.
// The same over-budget delegation runs to its answer with no summarizer call at all.
func TestSubAgent_ChildNeverFoldsWithAutoCompactOff(t *testing.T) {
	sink := &recordingSink{}
	bulky := fakeTool{name: "read_thing", readOnly: true, result: strings.Repeat("x", 25000)}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, bulky)
	cfg.Context.MaxContextTokens = 8192
	cfg.Context.CompactionEnabled = false

	up := &scriptedCompactResponder{
		summaryReply: "CHILD-SUMMARY",
		scripts: [][]provider.Delta{
			subAgentCallScript("c1", "trawl the repo"),
			toolCallScript("t1", "read_thing", `{}`),
			contentScript("the child's own final answer"),
			contentScript("parent done"),
		},
	}
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "please research"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if up.summaryCalls != 0 {
		t.Errorf("summarizer calls = %d with `auto-compact` off, want 0 — the child's fold obeys the same gate", up.summaryCalls)
	}
	sub, ok := lastSubAgentResult(sink.events)
	if !ok {
		t.Fatal("no sub_agent tool result emitted")
	}
	if sub.IsError || sub.Content != "the child's own final answer" {
		t.Errorf("sub_agent result = %+v, want the child's own final answer", sub)
	}
}

// TestNewChildAgent_CompactsMidExchange pins the seam itself: every child agent — the contract has
// no config key and no per-server override — is constructed folding at Turn boundaries, while the
// parent that spawned it keeps the Exchange-boundary-only trigger.
func TestNewChildAgent_CompactsMidExchange(t *testing.T) {
	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore)
	parent, err := newAgent(cfg, &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if parent.midExchangeCompaction {
		t.Error("the top-level Agent compacts mid-Exchange; the lifted guard is a delegate contract")
	}

	child, err := parent.newChildAgent("c1", "summarise the repo", "")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}
	defer child.Close()
	if !child.midExchangeCompaction {
		t.Error("a child agent does not compact mid-Exchange; its whole life is one Exchange, so it would never fold")
	}
}

// ---------------------------------------------------------------------------
// A child's output-capped reply is a fault, and the parent is told why
// ---------------------------------------------------------------------------

// truncatedChildText is the visible text a capped child reply carries — a real answer that simply
// stops mid-sentence, which is exactly why it must not be handed on as the delegated result.
const truncatedChildText = "the parser mishandles nested quotes; the second finding is that"

// cappedChildScripts drives one delegation whose child answers at LENGTH, with no tool call, and is
// cut off at the engine's own output cap (ADR 0046) — the 2026-08-25 shape. The parent then
// finishes.
func cappedChildScripts() [][]provider.Delta {
	return [][]provider.Delta{
		subAgentCallScript("c1", "audit the parser"),
		{
			{Kind: provider.DeltaContent, Content: truncatedChildText},
			{Kind: provider.DeltaDone, FinishReason: "length"},
		},
		contentScript("parent done"),
	}
}

// TestSubAgent_CappedChildReplyReportsAsErrorNamingTheCause proves both halves of the delegate rule
// end to end: the child's truncated answer faults instead of posing as the delegation's result, and
// the error result the parent MODEL receives carries the child's own cause sentence rather than
// pointing at an error only the human can see.
func TestSubAgent_CappedChildReplyReportsAsErrorNamingTheCause(t *testing.T) {
	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore)

	a, err := newAgent(cfg, &scriptedResponder{scripts: cappedChildScripts()})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "please research"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The child's fault is localised to the delegation: the PARENT's Exchange still completes.
	if res.Status != domain.StatusExchangeComplete || res.Faulted {
		t.Errorf("parent result = %+v, want a clean exchange-complete (the child's fault must not fault the parent)", res)
	}

	sub, ok := lastSubAgentResult(sink.events)
	if !ok {
		t.Fatal("no sub_agent tool result emitted")
	}
	if !sub.IsError {
		t.Errorf("sub_agent result IsError = false for a capped delegate reply; content = %q", sub.Content)
	}
	if strings.Contains(sub.Content, truncatedChildText) {
		t.Errorf("sub_agent result = %q — a truncated answer passed off as the delegated result", sub.Content)
	}

	childErr, ok := lastErrorAtDepth(sink.events, 1)
	if !ok {
		t.Fatal("expected the child's fault to surface as an ErrorEvent at Depth 1")
	}
	if !strings.Contains(childErr, "truncated answer is not a result") {
		t.Errorf("child ErrorEvent = %q, want the capped-delegate wording", childErr)
	}
	// The cause the human read at Depth 1 is the cause the parent model reads in the result.
	if want := subAgentFaultPrefix + childErr; sub.Content != want {
		t.Errorf("sub_agent result = %q, want %q", sub.Content, want)
	}
}

// TestSubAgent_CappedChildReplyWithToolCallContinues pins what the rule must NOT touch: a capped
// reply that still asked for a tool is not a truncated ANSWER — the loop has work to do — so the
// tool runs, the child answers on a later Turn, and the parent receives that answer as a success.
func TestSubAgent_CappedChildReplyWithToolCallContinues(t *testing.T) {
	sink := &recordingSink{}
	reads := 0
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main", ran: &reads}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, reader)

	scripts := [][]provider.Delta{
		subAgentCallScript("c1", "audit the parser"),
		{
			{Kind: provider.DeltaContent, Content: "reading the entry point first"},
			{Kind: provider.DeltaToolCall, ToolCall: &provider.ToolCall{
				ID:       "c2",
				Type:     "function",
				Function: provider.FunctionCall{Name: "read_thing", Arguments: `{}`},
			}},
			{Kind: provider.DeltaDone, FinishReason: "length"},
		},
		contentScript("the parser is fine"), // the child's next Turn answers normally
		contentScript("parent done"),
	}

	a, err := newAgent(cfg, &scriptedResponder{scripts: scripts})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "please research"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete || res.Faulted {
		t.Errorf("parent result = %+v, want a clean exchange-complete", res)
	}

	sub, ok := lastSubAgentResult(sink.events)
	if !ok {
		t.Fatal("no sub_agent tool result emitted")
	}
	if sub.IsError {
		t.Errorf("sub_agent result IsError = true; a capped reply carrying a tool call must not fault: %q", sub.Content)
	}
	if sub.Content != "the parser is fine" {
		t.Errorf("sub_agent result = %q, want the child's completed answer", sub.Content)
	}
	if reads != 1 {
		t.Errorf("read_thing ran %d times, want 1 — the tool on the capped reply must still run", reads)
	}
}

// ---------------------------------------------------------------------------
// The parent notice: a steered child says how many messages the human sent it
// ---------------------------------------------------------------------------
//
// The parent model never sees a message addressed to its delegate — it lands in the CHILD's
// conversation — so the result carries a count of what landed (ADR 0063 D3). These tests read it
// off the committed ToolResultEvent, which is where the parent model's copy actually comes from:
// the structural clamp runs on the way there.

// runSteeredDelegation drives ONE delegation whose child takes two Turns — a tool call, then its
// final answer, so there is exactly one between-Steps boundary for queued messages to land at —
// queues each remark for the child while its first Turn streams, and returns the sub_agent tool
// result the parent model saw. With no remarks it is the unsteered baseline.
func runSteeredDelegation(t *testing.T, answer string, remarks ...string) domain.ToolResult {
	t.Helper()

	sink := &recordingSink{}
	looked := 0
	cfg := subAgentConfig(sink, domain.ModeAskBefore, fakeTool{name: "look", readOnly: true, ran: &looked, result: "looked"})
	responder := &requestLogResponder{scripts: [][]provider.Delta{
		subAgentCallScript("c1", "survey the repo"), // [0] parent delegates
		toolCallScript("t1", "look", `{}`),          // [1] child Turn 1 — a tool call, so a Turn 2 follows
		contentScript(answer),                       // [2] child Turn 2 — its final answer
		contentScript("parent done"),                // [3] parent finishes
	}}
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	responder.before = func(call int) {
		if call != 1 {
			return
		}
		for _, remark := range remarks {
			if err := a.InterjectChild("c1", domain.UserInput{Text: remark}); err != nil {
				t.Errorf("InterjectChild while the child runs: %v", err)
			}
		}
	}

	if err := a.Submit(domain.UserInput{Text: "please research"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	res, ok := lastSubAgentResult(sink.events)
	if !ok {
		t.Fatal("no sub_agent tool result emitted")
	}
	return res
}

// TestSubAgent_SteeredChildResultCarriesTheParentNotice pins the notice the parent model reads,
// singular and plural, as the exact final line of the result.
func TestSubAgent_SteeredChildResultCarriesTheParentNotice(t *testing.T) {
	const answer = "child done"

	cases := []struct {
		name    string
		remarks []string
		want    string
	}{
		{"one message", []string{"focus on the tests"}, "(the user sent 1 message to this sub-agent while it ran)"},
		{"two messages", []string{"focus on the tests", "and the docs"}, "(the user sent 2 messages to this sub-agent while it ran)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := runSteeredDelegation(t, answer, tc.remarks...)

			if want := answer + "\n\n" + tc.want; res.Content != want {
				t.Errorf("sub_agent result = %q, want %q", res.Content, want)
			}
		})
	}
}

// TestSubAgent_UnsteeredChildResultIsUnchanged is the floor the notice must not move: a delegation
// nobody addressed reports exactly the child's final message, byte for byte, as it always did.
func TestSubAgent_UnsteeredChildResultIsUnchanged(t *testing.T) {
	const answer = "child done"

	res := runSteeredDelegation(t, answer)

	if res.Content != answer {
		t.Errorf("sub_agent result = %q, want the child's final message alone", res.Content)
	}
}

// TestSubAgent_ParentNoticeSurvivesTheStructuralClamp proves the notice reaches the parent MODEL,
// not just runSubAgent's return value: an oversized child answer is elided by the structural clamp
// (appendToolResult) on its way into the conversation, and because that elision is head/tail LINE
// based with the tail kept, the notice — the result's final line — comes through it.
func TestSubAgent_ParentNoticeSurvivesTheStructuralClamp(t *testing.T) {
	// Far past the structural floor at any window this harness can have, and many-lined, so the
	// clamp's head/tail rendering really does shrink it.
	answer := strings.TrimSuffix(strings.Repeat("the child has a great deal to say about the repo\n", 4000), "\n")

	res := runSteeredDelegation(t, answer, "focus on the tests")

	if len(res.Content) >= len(answer) {
		t.Fatalf("committed result is %d bytes for a %d-byte answer: the structural clamp never fired, so this proves nothing", len(res.Content), len(answer))
	}
	want := "\n\n" + userSteeredTrailerSingular
	if !strings.HasSuffix(res.Content, want) {
		t.Errorf("clamped result ends %q, want it to end with the parent notice %q", res.Content[max(0, len(res.Content)-120):], want)
	}
}

// TestSubAgent_ParentNoticeOnEveryOutcomeButCancelled pins the ONE-site rule where it lives: every
// outcome that produces a result carries the notice, and the cancelled one produces no result to
// carry it. It drives delegationResult directly because two of these outcomes cannot be scripted
// through a.Run — Run returns a Go error only for a loop-level fault it cannot localise, and a
// cancelled child surfaces no ToolResultEvent at all.
func TestSubAgent_ParentNoticeOnEveryOutcomeButCancelled(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		child    *Agent
		res      domain.StepResult
		err      error
		wantBody string
	}{
		{"run error", &Agent{steered: 2}, domain.StepResult{}, errors.New("boom"), "sub-agent failed: boom"},
		{"faulted", &Agent{steered: 2, lastFault: "the upstream died"}, domain.StepResult{Faulted: true}, nil, subAgentFaultPrefix + "the upstream died"},
		{"step capped", &Agent{steered: 2, stepCap: 3}, domain.StepResult{StepCapped: true}, nil, fmt.Sprintf(stepCapResultFormat, 3)},
		{"success", &Agent{steered: 2}, domain.StepResult{}, nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, outcome := tc.child.delegationResult("c1", tc.res, tc.err)

			if outcome != dispatchDone {
				t.Fatalf("outcome = %v, want dispatchDone", outcome)
			}
			if !strings.HasPrefix(got.Content, tc.wantBody) {
				t.Errorf("result = %q, want it to open with the outcome's own body %q", got.Content, tc.wantBody)
			}
			if want := "\n\n" + userSteeredTrailer(2); !strings.HasSuffix(got.Content, want) {
				t.Errorf("result = %q, want it to end with the parent notice %q", got.Content, want)
			}
		})
	}

	t.Run("cancelled", func(t *testing.T) {
		child := &Agent{steered: 2}

		got, outcome := child.delegationResult("c1", domain.StepResult{Status: domain.StatusCancelled}, nil)

		if outcome != dispatchCancelled {
			t.Fatalf("outcome = %v, want dispatchCancelled", outcome)
		}
		if got != (domain.ToolResult{}) {
			t.Errorf("cancelled result = %+v, want an empty result — a rolled-back delegation carries no notice either", got)
		}
	})
}

// TestUserSteeredTrailer_SingularAndPlural pins the two renderings the parent model reads.
func TestUserSteeredTrailer_SingularAndPlural(t *testing.T) {
	t.Parallel()

	cases := []struct {
		steered int
		want    string
	}{
		{1, "(the user sent 1 message to this sub-agent while it ran)"},
		{2, "(the user sent 2 messages to this sub-agent while it ran)"},
		{7, "(the user sent 7 messages to this sub-agent while it ran)"},
	}
	for _, tc := range cases {
		if got := userSteeredTrailer(tc.steered); got != tc.want {
			t.Errorf("userSteeredTrailer(%d) = %q, want %q", tc.steered, got, tc.want)
		}
	}
}

// ----------------------------------------------------------------------------
// The tool-less wrap-up Turn (Agent.wrapUp)
// ----------------------------------------------------------------------------
//
// These tests set the latch BY HAND — nothing in the engine writes it yet — because the three
// seams it moves (toolMenu, buildRequest, step) are its whole observable contract: one request
// with no tools and a directive saying why, and a reply that ends the Exchange whatever it asks
// for.

// wrapUpAgent builds a latched-or-clear single Agent over the given scripts with one read tool,
// returns it alongside the responder that logs what the loop actually sent and a counter the
// tool bumps if it is ever dispatched.
func wrapUpAgent(t *testing.T, latched bool, scripts ...[]provider.Delta) (*Agent, *requestLogResponder, *recordingSink, *int) {
	t.Helper()

	ran := 0
	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore,
		fakeTool{name: "read_thing", readOnly: true, ran: &ran, result: "package main"})

	responder := &requestLogResponder{scripts: scripts}
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.stepCap = 3 // what newChildAgent seeds on a delegate; the directive states this number
	a.wrapUp = latched
	return a, responder, sink, &ran
}

// runWrapUpAgent submits one task and runs the Agent to its boundary.
func runWrapUpAgent(t *testing.T, a *Agent) domain.StepResult {
	t.Helper()

	if err := a.Submit(domain.UserInput{Text: "trawl the repo"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

// requestSystemText returns the content of the first system message on a request the loop sent,
// or "" when it carries none — the native anchor a session with no configured prompt produces.
func requestSystemText(req provider.Request) string {
	for _, m := range req.Messages {
		if m.Role == string(domain.RoleSystem) {
			return m.Content
		}
	}
	return ""
}

// assistantMessages returns the assistant messages committed to an Agent's conversation.
func assistantMessages(a *Agent) []domain.Message {
	var out []domain.Message
	for _, m := range a.conv.Messages() {
		if m.Role == domain.RoleAssistant {
			out = append(out, m)
		}
	}
	return out
}

// TestWrapUpRequestWithdrawsToolsAndSaysWhy pins the shape of the one request the latch composes:
// zero tools on the wire and a system message carrying the directive with the cap's own number,
// in a session that configured no system prompt at all.
func TestWrapUpRequestWithdrawsToolsAndSaysWhy(t *testing.T) {
	a, responder, _, _ := wrapUpAgent(t, true, contentScript("here is what I found"))

	runWrapUpAgent(t, a)

	if len(responder.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(responder.requests))
	}
	req := responder.requests[0]
	if len(req.Tools) != 0 {
		t.Errorf("Tools = %d, want 0 — a latched Turn withdraws the whole menu", len(req.Tools))
	}
	want := fmt.Sprintf(wrapUpDirectiveFormat, 3)
	if got := requestSystemText(req); !strings.Contains(got, want) {
		t.Errorf("system text = %q, want it to contain the wrap-up directive %q", got, want)
	}
}

// TestToolMenuUnchangedWithoutTheWrapUpLatch is the other half: with the latch CLEAR the same
// session sends the full menu and no directive, so nothing about an ordinary Turn moved.
func TestToolMenuUnchangedWithoutTheWrapUpLatch(t *testing.T) {
	a, responder, _, _ := wrapUpAgent(t, false, contentScript("here is what I found"))

	runWrapUpAgent(t, a)

	req := responder.requests[0]
	if len(req.Tools) != len(a.tools.All()) {
		t.Errorf("Tools = %d, want the full menu of %d", len(req.Tools), len(a.tools.All()))
	}
	if got := requestSystemText(req); got != "" {
		t.Errorf("system text = %q, want none — an unlatched no-prompt session seeds no system message", got)
	}
}

// TestWrapUpDropsToolCallsAndKeepsTheText drives the reply the withdrawal exists to survive: a
// child that narrates AND asks for a tool anyway. The narration is committed, the call is never
// dispatched, and the Exchange ends there rather than taking another Turn.
func TestWrapUpDropsToolCallsAndKeepsTheText(t *testing.T) {
	a, responder, _, ran := wrapUpAgent(t, true,
		narratedToolCallScript("t0", "read_thing", `{}`, "partial findings"))

	res := runWrapUpAgent(t, a)

	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("Status = %q, want %q — the wrap-up reply ends the Exchange", res.Status, domain.StatusExchangeComplete)
	}
	if res.Faulted {
		t.Error("Faulted set; a dropped call is not a failure")
	}
	if *ran != 0 {
		t.Errorf("tool ran %d times, want 0 — a withdrawn menu must not be reachable", *ran)
	}
	if responder.calls != 1 {
		t.Errorf("upstream calls = %d, want 1 — the wrap-up Turn is the last one", responder.calls)
	}
	if got := a.lastVisibleText(); got != "partial findings" {
		t.Errorf("lastVisibleText = %q, want %q", got, "partial findings")
	}
	msgs := assistantMessages(a)
	if len(msgs) != 1 {
		t.Fatalf("assistant messages = %d, want 1", len(msgs))
	}
	if len(msgs[0].ToolCalls) != 0 {
		t.Errorf("committed assistant message carries %d tool calls, want 0", len(msgs[0].ToolCalls))
	}
}

// TestWrapUpWithNoTextCommitsNothing pins the empty case: a wrap-up reply that is nothing but a
// tool call commits no assistant message and emits no MessageEvent, so an empty final message can
// never become the child's last visible text and bury the partial result its capped Turns earned.
func TestWrapUpWithNoTextCommitsNothing(t *testing.T) {
	a, _, sink, ran := wrapUpAgent(t, true, toolCallScript("t0", "read_thing", `{}`))

	res := runWrapUpAgent(t, a)

	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("Status = %q, want %q", res.Status, domain.StatusExchangeComplete)
	}
	if *ran != 0 {
		t.Errorf("tool ran %d times, want 0 — a withdrawn menu must not be reachable", *ran)
	}
	if got := a.lastVisibleText(); got != "" {
		t.Errorf("lastVisibleText = %q, want empty — a text-less wrap-up commits nothing", got)
	}
	if got := len(assistantMessages(a)); got != 0 {
		t.Errorf("assistant messages = %d, want 0", got)
	}
	if hasEvent[domain.MessageEvent](sink.events) {
		t.Error("MessageEvent emitted for a text-less wrap-up reply")
	}
}
