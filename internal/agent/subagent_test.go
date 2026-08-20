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
