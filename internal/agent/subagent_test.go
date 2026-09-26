package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/floor"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/stubllm"
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

// subAgentCallScript emits a single sub_agent tool call delegating task — the Delta script the
// surviving hand-written fakes play; subAgentCallTurn is its stubllm twin.
func subAgentCallScript(id, task string) []provider.Delta {
	return toolCallScript(id, tools.SubAgentToolName, subAgentArgs(task))
}

// subAgentCallTurn is a turn that emits a single sub_agent tool call delegating task.
func subAgentCallTurn(id, task string) stubllm.Turn {
	return toolCallTurn(id, tools.SubAgentToolName, subAgentArgs(task))
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

	responder := scriptedResponder(t,
		subAgentCallTurn("c1", "summarise the repo"),
		contentTurn("the repo is a Go TUI agent"), // the sub-agent's only Turn (final)
		contentTurn("done — delegated and summarised"),
	)
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

	responder := scriptedResponder(t,
		subAgentCallTurn("c1", "do the thing"),
		contentTurn("child reply"),
		contentTurn("parent done"),
	)
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

	responder := scriptedResponder(t,
		subAgentCallTurn("c1", "first task"),
		contentTurn("first child reply"),
		subAgentCallTurn("c2", "second task"),
		contentTurn("second child reply"),
		contentTurn("parent done"),
	)
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
		return ev.EventBase, true // the CHILD run's identity: the delegation's own depth, run id and call id
	case domain.ChildInterjectionEvent:
		return ev.EventBase, true // likewise the CHILD run's identity: the run the message was addressed to
	case domain.ApprovalEvent:
		return ev.EventBase, true
	case domain.TurnEvent:
		return ev.EventBase, true
	case domain.ReactionFiredEvent:
		return ev.EventBase, true
	case domain.SeamClosedEvent:
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

// TestSubAgent_RunIDIdentifiesEachDelegation pins the engine-minted run identity (plan
// 2026-09-24 - 01, item 1) through a nested delegation whose two levels REUSE one call id — the
// collision a call id cannot rule out. The top-level agent stamps no run id; the head ToolCallEvent
// of each delegation carries a fresh SpawnRunID; every event its child emits, its lifecycle phases
// included, carries that id as RunID; its ToolResultEvent pairs back by the same id; and the
// grandchild's id differs from the child's although both were spawned by a call named "c1".
func TestSubAgent_RunIDIdentifiesEachDelegation(t *testing.T) {
	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore)
	cfg.Delegation.MaxDepth = 2 // a grandchild exists only under a bound above the default

	responder := &requestLogResponder{scripts: [][]provider.Delta{
		subAgentCallScript("c1", "level 1"), // [0] parent → child
		subAgentCallScript("c1", "level 2"), // [1] child → grandchild, under the SAME call id
		contentScript("grandchild done"),    // [2] grandchild finishes
		contentScript("child done"),         // [3] child finishes
		contentScript("parent done"),        // [4] parent finishes
	}}
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "go"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The run id of the delegation opened at each depth, read off its head ToolCallEvent: the
	// parent's head (depth 0) opens the child's run (depth 1), the child's head the grandchild's.
	runAt := map[int]string{}
	for _, e := range sink.events {
		call, ok := e.(domain.ToolCallEvent)
		if !ok || call.Call.Tool != tools.SubAgentToolName {
			continue
		}
		if call.SpawnRunID == "" {
			t.Fatalf("the sub_agent call at depth %d carries no SpawnRunID", call.Depth)
		}
		runAt[call.Depth+1] = call.SpawnRunID
	}
	if runAt[1] == "" || runAt[2] == "" {
		t.Fatalf("delegation heads = %v, want one at depth 0 and one at depth 1", runAt)
	}
	if runAt[1] == runAt[2] {
		t.Errorf("child and grandchild share run id %q; the grandchild's must differ although both calls are c1", runAt[1])
	}

	for _, e := range sink.events {
		base, ok := eventBaseOf(e)
		if !ok {
			t.Fatalf("eventBaseOf does not know %T — teach it the new variant", e)
		}
		if base.RunID != runAt[base.Depth] {
			t.Errorf("%T at Depth %d carries RunID %q, want %q", e, base.Depth, base.RunID, runAt[base.Depth])
		}
		if result, isResult := e.(domain.ToolResultEvent); isResult && result.Tool == tools.SubAgentToolName {
			if want := runAt[result.Depth+1]; result.SpawnRunID != want {
				t.Errorf("the sub_agent result at depth %d carries SpawnRunID %q, want its run's %q", result.Depth, result.SpawnRunID, want)
			}
		}
	}
}

// TestSubAgent_NamedEventCarriesTheChildsRunID pins that a generated name lands on the run it
// names: the rename is stamped with the CHILD's run id — the SpawnRunID its head call carried —
// and not merely with a call id another delegation could share.
func TestSubAgent_NamedEventCarriesTheChildsRunID(t *testing.T) {
	sink := newLockedSink()
	namer := &stubNamer{reply: "Generated Name"}
	runNamingParent(t, namingParent(t, sink, namer, "", func(context.Context) { sink.awaitRename() }))

	named := sink.namings()
	if len(named) != 1 {
		t.Fatalf("SubAgentNamedEvents = %d, want exactly one rename", len(named))
	}
	var head string
	sink.mu.Lock()
	for _, e := range sink.events {
		if call, ok := e.(domain.ToolCallEvent); ok && call.Call.Tool == tools.SubAgentToolName {
			head = call.SpawnRunID
		}
	}
	sink.mu.Unlock()
	if head == "" {
		t.Fatal("the delegation's head ToolCallEvent carries no SpawnRunID")
	}
	if named[0].RunID != head {
		t.Errorf("SubAgentNamedEvent.RunID = %q, want the child's run id %q", named[0].RunID, head)
	}
}

// TestRunIDMinter_MintsDistinctIDs pins the minter's shape: `<prefix>.<n>` from a 1-based counter
// under an injected prefix, a random eight-hex-character prefix per root otherwise — so two roots'
// first ids differ — and "" from a nil minter, the id a reader falls back from.
func TestRunIDMinter_MintsDistinctIDs(t *testing.T) {
	t.Parallel()

	m := newRunIDMinter("0badc0de")
	if first, second := m.mint(), m.mint(); first != "0badc0de.1" || second != "0badc0de.2" {
		t.Errorf("injected-prefix ids = %q, %q; want 0badc0de.1, 0badc0de.2", first, second)
	}

	one, two := newRunIDMinter(randomRunIDPrefix()), newRunIDMinter(randomRunIDPrefix())
	if len(one.prefix) != 2*runIDPrefixBytes {
		t.Errorf("random prefix %q has %d characters, want %d", one.prefix, len(one.prefix), 2*runIDPrefixBytes)
	}
	if a, b := one.mint(), two.mint(); a == b {
		t.Errorf("two roots minted the same first id %q; the per-root prefix must separate them", a)
	}

	var none *runIDMinter
	if got := none.mint(); got != "" {
		t.Errorf("nil minter minted %q, want empty", got)
	}
}

// TestSubAgent_InheritsPlanModeCannotWrite proves a sub-agent in a Plan-mode parent inherits
// Plan and therefore refuses a write its child attempts (the acceptance ADR 0013 pins).
func TestSubAgent_InheritsPlanModeCannotWrite(t *testing.T) {
	sink := &recordingSink{}
	wrote := 0
	writer := fakeTool{name: "write_thing", readOnly: false, ran: &wrote, result: "wrote"}
	cfg := subAgentConfig(sink, domain.ModePlan, writer)

	// Plan withdraws the writer from the child's MENU, so the tool-call repair Floor guard would
	// answer the call as "not in the tool set" before dispatch saw it. This test is about the
	// Plan-mode refusal at dispatch, so the guard is off for it (its own proofs: floorguards_test.go).
	cfg.Floor.DisableToolCallRepair = true
	responder := scriptedResponder(t,
		subAgentCallTurn("c1", "write a file"),
		toolCallTurn("c2", "write_thing", `{}`), // the child attempts a write
		contentTurn("child could not write"),    // child finishes after the refusal result
		contentTurn("parent done"),
	)
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

	// The omitted tool is absent from the child's MENU too, so the tool-call repair Floor guard
	// would answer it before the unknown-tool result this test is about; the guard is off here.
	cfg.Floor.DisableToolCallRepair = true
	responder := scriptedResponder(t,
		subAgentCallTurn("c1", "use the writer"),
		toolCallTurn("c2", "write_thing", `{}`), // child calls a tool not in its subset
		contentTurn("child saw unknown tool"),
		contentTurn("parent done"),
	)
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
// if the call is emitted anyway — so an unbounded tower of sub-agents is impossible. The bound is
// raised to 2 (`delegate-max-depth: 2`) so the tower has a middle level to prove it with.
func TestSubAgent_MaxDepthRefusesAndWithholdsTool(t *testing.T) {
	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore)
	cfg.Delegation.MaxDepth = 2

	// Drive: parent (d0) spawns d1, d1 spawns d2, d2 attempts to spawn d3 (refused at the
	// bound), d2 finishes, d1 finishes, parent finishes.
	responder := scriptedResponder(t,
		subAgentCallTurn("c1", "level 1"), // parent → d1
		subAgentCallTurn("c2", "level 2"), // d1 → d2
		subAgentCallTurn("c3", "level 3"), // d2 → (refused: would be d3, past the bound)
		contentTurn("d2 done after refusal"),
		contentTurn("d1 done"),
		contentTurn("parent done"),
	)
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
	atBound := &Agent{cfg: cfg, tools: a.tools, depth: cfg.Delegation.MaxDepth - 1}
	childReg := atBound.defaultSubAgentTools()
	if _, ok := childReg.Lookup(tools.SubAgentToolName); ok {
		t.Error("a child constructed at the depth bound must not be offered the sub_agent tool")
	}
}

// TestSubAgent_RecursionPointRefusesAtBound proves the SECONDARY (defense-in-depth) bound: the
// recursion point itself refuses a spawn at the max depth even if the tool were somehow
// emitted (the primary defense withholds the tool from the menu; this is the belt-and-braces).
// The refusal names the configured bound, so the model reads the number it ran into.
func TestSubAgent_RecursionPointRefusesAtBound(t *testing.T) {
	t.Parallel()
	cfg := domain.Config{Delegation: domain.DelegationConfig{MaxDepth: 2}}
	atBound := &Agent{cfg: cfg, depth: cfg.Delegation.MaxDepth}
	res, outcome := atBound.runSubAgent(context.Background(),
		domain.ToolCall{ID: "c1", Tool: tools.SubAgentToolName, Arguments: json.RawMessage(subAgentArgs("recurse"))}, "")
	if outcome != dispatchDone {
		t.Fatalf("outcome = %v, want dispatchDone", outcome)
	}
	if !res.IsError || !strings.Contains(res.Content, "depth limit reached (max 2)") {
		t.Errorf("at-bound recursion = %+v, want a depth-limit refusal naming the bound", res)
	}
}

// TestSubAgent_DepthBoundFollowsTheKey pins where the bound is read from — Config.Delegation.MaxDepth,
// the `delegate-max-depth` key — and its default: a depth-0 parent under the default (1) hands its
// child no sub_agent, while under 2 the depth-0 parent's child keeps it and the depth-1 parent's
// child loses it. The menu is the PRIMARY defence, so it is the menu that is asserted.
func TestSubAgent_DepthBoundFollowsTheKey(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		maxDepth    int
		parentDepth int
		wantOffered bool
	}{
		{"the default withholds sub_agent from a depth-0 parent's child", 0, 0, false},
		{"an explicit 1 is the default spelled out", 1, 0, false},
		{"under 2 a depth-0 parent's child is offered sub_agent", 2, 0, true},
		{"under 2 a depth-1 parent's child is not", 2, 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := subAgentConfig(&recordingSink{}, domain.ModeAskBefore, fakeTool{name: "w"})
			cfg.Delegation.MaxDepth = tc.maxDepth
			parent := &Agent{cfg: cfg, tools: cfg.Tools, depth: tc.parentDepth}

			roster := parent.defaultSubAgentTools()

			if _, offered := roster.Lookup(tools.SubAgentToolName); offered != tc.wantOffered {
				t.Errorf("child of a depth-%d parent under MaxDepth %d is offered sub_agent = %v, want %v",
					tc.parentDepth, tc.maxDepth, offered, tc.wantOffered)
			}
			if _, hasLeaf := roster.Lookup("w"); !hasLeaf {
				t.Error("the child's roster lost a leaf tool while sub_agent was decided")
			}
		})
	}
}

// ----------------------------------------------------------------------------
// The child's roster: no human seat, and a `tools` argument that only narrows (plan 2026-09-14 - 03, item 5)
// ----------------------------------------------------------------------------

// menuRecorder wraps a responder and keeps the tool names every request offered, keyed by the
// routing key routedResponder uses (the asking agent's last user message) — so a test reads the
// menu a CHILD was actually sent rather than the registry it was built with.
type menuRecorder struct {
	inner provider.Responder
	mu    sync.Mutex
	menus map[string][][]string
}

func newMenuRecorder(inner provider.Responder) *menuRecorder {
	return &menuRecorder{inner: inner, menus: map[string][][]string{}}
}

func (m *menuRecorder) Stream(ctx context.Context, req provider.Request) iter.Seq[provider.Delta] {
	names := make([]string, 0, len(req.Tools))
	for _, spec := range req.Tools {
		names = append(names, spec.Name)
	}
	asker := lastUserText(req)
	m.mu.Lock()
	m.menus[asker] = append(m.menus[asker], names)
	m.mu.Unlock()
	return m.inner.Stream(ctx, req)
}

// firstMenu returns the tool names the first request keyed by asker offered.
func (m *menuRecorder) firstMenu(asker string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.menus[asker]) == 0 {
		return nil
	}
	return m.menus[asker][0]
}

// humanSeatRegistry builds a parent registry holding the two human-seat tools beside sub_agent and
// a leaf, with the host delegates stubbed so both tools register.
func humanSeatRegistry(extra ...domain.Tool) *domain.ToolRegistry {
	reg := domain.NewToolRegistry()
	_ = reg.Register(tools.NewSubAgent())
	_ = reg.Register(tools.NewAskUser(&askProbeAsker{answer: func(domain.AskRequest) string { return "" }}))
	_ = reg.Register(tools.NewPresentDocument("", tools.ReadMounts{}, &recordingPresenter{}))
	for _, t := range extra {
		_ = reg.Register(t)
	}
	return reg
}

// TestSubAgent_ChildNeverHoldsTheHumanSeatTools pins the unconditional withholding at BOTH places
// it is visible: the roster the orchestrator builds a child with, and the menu the child actually
// sends upstream in a run. ask_user and present_document are the parent's whichever mode it runs
// in and whatever the call asked for; the leaf tool beside them reaches the child untouched.
func TestSubAgent_ChildNeverHoldsTheHumanSeatTools(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.Tools = humanSeatRegistry(fakeTool{name: "read_thing", readOnly: true, result: "read"})
	parent, err := newAgent(cfg, scriptedResponder(t))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	roster := parent.defaultSubAgentTools()

	for _, withheld := range []string{tools.AskUserToolName, tools.PresentDocumentToolName} {
		if _, offered := roster.Lookup(withheld); offered {
			t.Errorf("the child's roster holds %s; a sub-agent has no seat at the human's prompt", withheld)
		}
	}
	if _, hasLeaf := roster.Lookup("read_thing"); !hasLeaf {
		t.Error("the child's roster lost the leaf tool while the human seat was withheld")
	}

	// And on the wire: the menu the child's first request carried.
	const parentInput, childTask = "delegate the reading", "read the thing over there"
	up := newMenuRecorder(newRoutedResponder().
		route(parentInput, nil, subAgentCallScript("c1", childTask)).
		route(childTask, nil, contentScript("child done")).
		route(parentInput, nil, contentScript("parent done")))
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	_ = a.Submit(domain.UserInput{Text: parentInput})
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := up.firstMenu(childTask); !slices.Equal(got, []string{"read_thing"}) {
		t.Errorf("the child's menu = %v, want [read_thing] — no human seat, no sub_agent at the default bound", got)
	}
	if got := up.firstMenu(parentInput); !slices.Contains(got, tools.AskUserToolName) || !slices.Contains(got, tools.PresentDocumentToolName) {
		t.Errorf("the parent's menu = %v, want it to keep both human-seat tools", got)
	}
}

// TestSubAgent_ReadOnlyRosterIsThePlanFloorMinusTheHumanSeat defines the read-only set the
// `tools: "read-only"` keyword yields, against the real default registry: every parent tool
// planAdmits — the class Plan mode runs on every target, never the bare ReadOnly() declaration —
// minus the two withheld tools, with sub_agent following the depth rule (withheld under the default
// bound, offered under 2). The menu the child sends is compared to that set, so the definition is
// pinned where the model reads it.
func TestSubAgent_ReadOnlyRosterIsThePlanFloorMinusTheHumanSeat(t *testing.T) {
	for _, tc := range []struct {
		name         string
		maxDepth     int
		wantSubAgent bool
	}{
		{"under the default bound the child is not offered sub_agent", 0, false},
		{"under delegate-max-depth 2 the child keeps sub_agent", 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseConfig(&recordingSink{})
			cfg.Tools = tools.NewDefaultRegistryWithHost(t.TempDir(), tools.HostTools{
				Asker:     &askProbeAsker{answer: func(domain.AskRequest) string { return "" }},
				Presenter: &recordingPresenter{},
			})
			cfg.Delegation.MaxDepth = tc.maxDepth

			want := []string{}
			for _, tool := range cfg.Tools.All() {
				switch tool.Name() {
				case tools.AskUserToolName, tools.PresentDocumentToolName:
					continue
				case tools.SubAgentToolName:
					if tc.wantSubAgent {
						want = append(want, tool.Name())
					}
					continue
				}
				if planAdmits(tool) {
					want = append(want, tool.Name())
				}
			}
			if len(want) < 3 || slices.Contains(want, "write_file") || slices.Contains(want, "terminal") {
				t.Fatalf("the expected read-only set %v is not a credible floor", want)
			}

			const parentInput, childTask = "delegate a read-only survey", "survey the tree without touching it"
			up := newMenuRecorder(newRoutedResponder().
				route(parentInput, nil, toolCallScript("c1", tools.SubAgentToolName,
					`{"task":"`+childTask+`","tools":"read-only"}`)).
				route(childTask, nil, contentScript("child done")).
				route(parentInput, nil, contentScript("parent done")))
			a, err := newAgent(cfg, up)
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			_ = a.Submit(domain.UserInput{Text: parentInput})
			if _, err := a.Run(context.Background()); err != nil {
				t.Fatalf("Run: %v", err)
			}

			if got := up.firstMenu(childTask); !slices.Equal(got, want) {
				t.Errorf("the read-only child's menu = %v, want %v", got, want)
			}
		})
	}
}

// TestSubAgent_ToolsListNarrowsToExactlyThoseTools drives the array spelling: the child's menu is
// the named tools and nothing else, in the order named; a tool the parent holds but the list omits
// resolves as unknown inside the delegation; and a name that is withheld from every child
// (ask_user here) passes the unknown-name check — it is not a misspelling — and is dropped by the
// intersection rather than smuggled back in.
func TestSubAgent_ToolsListNarrowsToExactlyThoseTools(t *testing.T) {
	sink := &recordingSink{}
	ran := 0
	cfg := baseConfig(sink)
	cfg.Tools = humanSeatRegistry(
		fakeTool{name: "read_thing", readOnly: true, result: "read"},
		fakeTool{name: "grep_thing", readOnly: true, result: "found"},
		fakeTool{name: "write_thing", ran: &ran, result: "wrote"},
	)
	cfg.Mode = domain.ModeAllowEdits
	// The tool-call repair Floor guard would answer the off-menu write before the unknown-tool
	// result this test is about; off, as in TestSubAgent_SubsetCannotCallOmittedTool.
	cfg.Floor.DisableToolCallRepair = true

	const parentInput, childTask = "delegate a narrowed read", "read and grep, never write"
	up := newMenuRecorder(newRoutedResponder().
		route(parentInput, nil, toolCallScript("c1", tools.SubAgentToolName,
			`{"task":"`+childTask+`","tools":["grep_thing","read_thing","ask_user"]}`)).
		route(childTask, nil, toolCallScript("t1", "write_thing", `{}`)).
		route(childTask, nil, contentScript("child done")).
		route(parentInput, nil, contentScript("parent done")))
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	_ = a.Submit(domain.UserInput{Text: parentInput})
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := up.firstMenu(childTask); !slices.Equal(got, []string{"grep_thing", "read_thing"}) {
		t.Errorf("the narrowed child's menu = %v, want exactly [grep_thing read_thing]", got)
	}
	if ran != 0 {
		t.Errorf("the omitted writer ran %d times; a narrowed child must not reach it", ran)
	}
	if !hasToolResultContaining(sink.events, 1, "unknown tool") {
		t.Error("expected the omitted write_thing call to resolve as an unknown tool at Depth 1")
	}
	if res, ok := lastSubAgentResult(sink.events); !ok || res.IsError {
		t.Errorf("the delegation's result = %+v, want the child's own completion", res)
	}
}

// TestSubAgent_UnknownToolNameIsRefusedBeforeAnyChildRuns pins the refusal: a list naming a tool
// the parent does not hold is answered with an error result naming every unknown name — before
// ToolRegistry.Subset could drop it silently and before a child is built — so the parent spends
// nothing on the delegation and reads the spelling it got wrong. Known names beside the unknown
// ones are not enough to let the call through.
func TestSubAgent_UnknownToolNameIsRefusedBeforeAnyChildRuns(t *testing.T) {
	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, fakeTool{name: "read_thing", readOnly: true, result: "read"})
	parent, err := newAgent(cfg, scriptedResponder(t))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	res, outcome := parent.runSubAgent(context.Background(), domain.ToolCall{
		ID: "c1", Tool: tools.SubAgentToolName,
		Arguments: json.RawMessage(`{"task":"do the narrowed thing","tools":["read_thing","frobnicate","zap"]}`),
	}, "")

	if outcome != dispatchDone {
		t.Fatalf("outcome = %v, want dispatchDone", outcome)
	}
	if !res.IsError || !strings.Contains(res.Content, "unknown tool frobnicate, zap") {
		t.Errorf("result = %+v, want an error naming the unknown tools in order", res)
	}
	if len(sink.events) != 0 {
		t.Errorf("%d events reached the sink; a refused roster must build no child", len(sink.events))
	}
	// The wrong SHAPE is refused the same way, by the argument decoder's own message.
	res, _ = parent.runSubAgent(context.Background(), domain.ToolCall{
		ID: "c2", Tool: tools.SubAgentToolName,
		Arguments: json.RawMessage(`{"task":"do the narrowed thing","tools":"readonly"}`),
	}, "")
	if !res.IsError || !strings.Contains(res.Content, `unknown keyword "readonly"`) {
		t.Errorf("result = %+v, want the decoder's keyword correction", res)
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

	// The child calls "flaky" repeatedly (same args) until its breaker trips, then finishes. The
	// tool-loop breaker Floor guard answers that identical repeat first, so it is off for this
	// test — the subject is the child's own circuit-breaker and its isolation from the parent's.
	cfg.Floor.DisableToolLoopBreaker = true
	childScripts := []stubllm.Turn{}
	for i := 0; i < 4; i++ {
		childScripts = append(childScripts, toolCallTurn("k", "flaky", `{}`))
	}
	childScripts = append(childScripts, contentTurn("child gives up"))
	scripts := append([]stubllm.Turn{subAgentCallTurn("c1", "retry flaky")}, childScripts...)
	scripts = append(scripts, contentTurn("parent done"))

	a, _ := newAgent(cfg, scriptedResponder(t, scripts...))
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

	responder := scriptedResponder(t,
		subAgentCallTurn("c1", "clean up"),
		// The child attempts a Tier-1 dangerous action; the shared floor must hard-refuse it.
		toolCallTurn("c2", "terminal", `{"command":"rm -rf /"}`),
		contentTurn("child blocked"),
		contentTurn("parent done"),
	)
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

	responder := scriptedResponder(t,
		subAgentCallTurn("c1", "trigger a panic"),
		toolCallTurn("c2", "boom", `{}`), // child tool panics (recovered into an ErrorEvent)
		contentTurn("child recovered"),
		contentTurn("parent done"),
	)
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

// faultedDelegationScripts drives one delegation whose child narrates, calls a tool, and then
// hits an Upstream fault on its next Turn — the child's Exchange is ABANDONED, which closes on
// the same StatusExchangeComplete a real completion returns. The child completed a Turn before
// the fault, so the engine folds its conversation for the retention (Agent.finishAtFault) before
// the parent finishes.
func faultedDelegationScripts() []stubllm.Turn {
	return []stubllm.Turn{
		subAgentCallTurn("c1", "summarise the repo"),
		narratedToolCallTurn("c2", "read_thing", `{}`, staleChildText), // a model that narrates before acting
		errorScript("upstream: connection reset by peer"),              // the child's next Turn faults
		contentTurn(childFoldSummary),                                  // the engine fold of the faulted child
		contentTurn("parent done"),
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

	a, err := newAgent(cfg, scriptedResponder(t, faultedDelegationScripts()...))
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

	a, err := newAgent(cfg, scriptedResponder(t,
		subAgentCallTurn("c1", "summarise the repo"),
		retryableErrorTurn(transientFaultMsg), // the child's only Turn hits a transient blip
		contentTurn(childAnswer),              // ... and its re-stream lands, well inside the budget
		contentTurn("parent done"),
	))
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

// TestSubAgent_CancelledChildRollsTheParentTurnBack pins the neighbouring row the fault marker
// must not disturb: a CANCELLED child still unwinds the parent Turn wholesale (D2) — no tool
// result is surfaced at all, and the cancel is not reported as a fault. runDelegation closes the
// cancelled delegation's bracket at width 1 exactly as it does on a pool worker (ADR 0075
// decision 12).
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

	a, err := newAgent(cfg, scriptedResponder(t,
		subAgentCallTurn("c1", "summarise the repo"),
		toolCallTurn("c2", "read_thing", `{}`), // the child's call is cancelled mid-flight
	))
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
	assertCancelledBracket(t, sink.events, "c1")
}

// TestSubAgent_ChildInheritsTheLiveNoticeSwitch proves the context-fill notice switch
// (Generation.ContextFillNotice, ADR 0077 D1) reaches a child the way Bypass and the Floor do:
// read from the parent's LIVE Generation at spawn, so a switch flipped through SetReactions after
// construction — the construction Config has it off — is what a child spawned after it carries,
// and one flipped back off is not.
func TestSubAgent_ChildInheritsTheLiveNoticeSwitch(t *testing.T) {
	sink := &recordingSink{}
	parent, err := newAgent(subAgentConfig(sink, domain.ModeAllowEdits), scriptedResponder(t, contentTurn("done")))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if parent.Generation().ContextFillNotice {
		t.Fatal("a bare Config seeds the notice switch on, want it off (ADR 0077: off by default)")
	}

	gen := parent.Generation()
	gen.ContextFillNotice = true
	mustSetReactions(t, parent, gen)
	child, err := parent.newChildAgent("spawn-1", "survey the tree", "surveyor")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}
	if !child.Generation().ContextFillNotice {
		t.Error("a child spawned after the switch went ON does not carry it")
	}

	gen.ContextFillNotice = false
	mustSetReactions(t, parent, gen)
	child, err = parent.newChildAgent("spawn-2", "survey the tree again", "surveyor")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}
	if child.Generation().ContextFillNotice {
		t.Error("a child spawned after the switch went OFF still carries it")
	}
}

// TestSubAgent_DepthZeroReadsAsOne guards the engine's floor on the recursion bound: an embedder's
// untouched Config (MaxDepth 0) delegates ONCE — the zero is the built-in default, never "no
// delegation" — and a stated bound is read verbatim. Driven end to end: the depth-0 parent's
// delegation runs, and the child's own sub_agent call resolves as an unknown tool because the
// menu withheld it.
func TestSubAgent_DepthZeroReadsAsOne(t *testing.T) {
	t.Parallel()
	if got := (&Agent{}).maxDepth(); got != 1 {
		t.Fatalf("maxDepth() on a zero Config = %d, want 1 — 0 is the default, not \"no delegation\"", got)
	}
	if got := (&Agent{cfg: domain.Config{Delegation: domain.DelegationConfig{MaxDepth: 3}}}).maxDepth(); got != 3 {
		t.Fatalf("maxDepth() under MaxDepth 3 = %d, want 3", got)
	}

	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore)
	cfg.Delegation.MaxDepth = 0
	responder := scriptedResponder(t,
		subAgentCallTurn("c1", "level 1"), // parent → d1
		subAgentCallTurn("c2", "level 2"), // d1 → (withheld: d2 would be past the default bound)
		contentTurn("d1 done after refusal"),
		contentTurn("parent done"),
	)
	a, _ := newAgent(cfg, responder)
	_ = a.Submit(domain.UserInput{Text: "go"})
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !hasToolResultContaining(sink.events, 0, "d1 done after refusal") {
		t.Error("the depth-0 parent's delegation never ran — a zero MaxDepth must still delegate once")
	}
	if !hasToolResultContaining(sink.events, 1, "unknown tool") {
		t.Error("expected the depth-1 child's sub_agent call to resolve as an unknown tool (withheld at the default bound)")
	}
}

// TestSubAgent_RejectsEmptyAndBadArgs proves the recursion point validates its task argument.
func TestSubAgent_RejectsEmptyAndBadArgs(t *testing.T) {
	t.Parallel()
	a := &Agent{depth: 0}

	res, outcome := a.runSubAgent(context.Background(), domain.ToolCall{ID: "c1", Tool: tools.SubAgentToolName, Arguments: json.RawMessage(`{}`)}, "")
	if outcome != dispatchDone || !res.IsError || !strings.Contains(res.Content, "non-empty task") {
		t.Errorf("empty task = %+v, want a non-empty-task error result", res)
	}

	res, _ = a.runSubAgent(context.Background(), domain.ToolCall{ID: "c2", Tool: tools.SubAgentToolName, Arguments: json.RawMessage(`{not json`)}, "")
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

	a, err := newAgent(cfg, echoResponder(t, "unused"))
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
	a, err := newAgent(subAgentConfig(sink, domain.ModeAskBefore), scriptedResponder(t))
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

	responder := scriptedResponder(t,
		toolCallTurn("c1", tools.SubAgentToolName, subAgentNamedArgs("summarise the repo", "  repo-scout\nignored prose")),
		contentTurn("the repo is a Go TUI agent"),
		contentTurn("done — delegated and summarised"),
	)
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

// narratedToolCallTurn is a turn that emits visible text AND a tool call in one reply — a
// delegate narrating as it works, the shape narratedToolCallScript plays for the surviving
// hand-written fakes.
func narratedToolCallTurn(id, name, args, text string) stubllm.Turn {
	turn := toolCallTurn(id, name, args)
	turn.Text = text
	return turn
}

// narratedChildTurns returns n turns of a child that reads a file and narrates each time — the
// shape the step cap exists for; it is cappedChildTurns for a scripted upstream.
func narratedChildTurns(n int) []stubllm.Turn {
	out := make([]stubllm.Turn, 0, n)
	for i := 0; i < n; i++ {
		// The arguments carry the Turn index so consecutive child Turns are not an identical
		// repeat, which the tool-loop breaker Floor guard would answer instead of spending a step.
		out = append(out, narratedToolCallTurn(
			fmt.Sprintf("t%d", i), "read_thing", fmt.Sprintf(`{"n":%d}`, i), fmt.Sprintf("reading file %d", i)))
	}
	return out
}

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
		// The arguments carry the Turn index so consecutive child Turns are not an identical
		// repeat, which the tool-loop breaker Floor guard would answer instead of spending a step.
		out = append(out, narratedToolCallScript(
			fmt.Sprintf("t%d", i), "read_thing", fmt.Sprintf(`{"n":%d}`, i), fmt.Sprintf("reading file %d", i)))
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
// report the parent reads under the closing-report sub-head, distinct from every narration the
// child wrote during its capped Turns so a test can tell the authored text from the scavenged one.
const childClosingReport = "I read two files; the third is unread and the survey is unfinished."

// childFoldSummary is what the scripted upstream answers the ENGINE FOLD with — the summary call
// finishAtStepCap makes over the child's conversation before the wrap-up Turn (foldForParent). Every
// capped child in this package answers it FIRST, then the wrap-up: a bound costs the upstream one
// fold request plus one wrap-up request beyond the working Turns. Its wording is distinct from the
// closing report so a test can tell which sub-head each landed under.
const childFoldSummary = "Engine fold: the delegate read files 0 and 1; file 2 is unread."

// cappedResult renders the exact result a capped delegation hands its parent: the head, the engine
// summary under its sub-head, a blank line, then the closing text under its own.
func cappedResult(head, fold, closing string) string {
	return head + "\n" + engineSummaryHead + "\n" + fold + "\n\n" + closingReportHead + "\n" + closing
}

// foldScript is the fold reply with a server-reported usage, so the fold's one UsageEvent is emitted
// and a test can read the flags it carries.
func foldScript(prompt, completion int) []provider.Delta {
	script := contentScript(childFoldSummary)
	script[len(script)-1].Usage = &provider.Usage{PromptTokens: prompt, CompletionTokens: completion, TotalTokens: prompt + completion}
	return script
}

// engineFoldUsage returns the Maintenance UsageEvents at depth that carry the DelegateFold flag —
// the engine fold's own accounting, told apart from a Compaction's by that flag.
func engineFoldUsage(events []domain.Event, depth int) []domain.UsageEvent {
	var out []domain.UsageEvent
	for _, e := range events {
		if ev, ok := e.(domain.UsageEvent); ok && ev.Depth == depth && ev.Maintenance && ev.DelegateFold {
			out = append(out, ev)
		}
	}
	return out
}

// requestSystemContains reports whether req's system text (requestSystemText) carries phrase.
func requestSystemContains(req provider.Request, phrase string) bool {
	return strings.Contains(requestSystemText(req), phrase)
}

// foldInstructionPhrase is a phrase of internal/context's delegate-fold-instruction.txt, the system
// prompt the engine fold's request carries and nothing else does.
const foldInstructionPhrase = "summarizing the work of a sub-agent for the agent that delegated"

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
// boundary marked StepCapped and NOT Faulted, after exactly cap working Turns PLUS the engine fold's
// summary call and the one tool-less wrap-up Turn the cap spends on a closing report
// (finishAtStepCap).
func TestRunEndsTheExchangeAtTheStepCap(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, reader)

	// Three working Turns, then the fold's reply and the wrap-up's: a child that would keep asking
	// for tools is what the cap ends, so the fourth working Turn is never scripted.
	scripts := append(cappedChildTurns(3), contentScript(childFoldSummary), contentScript(childClosingReport))
	responder := &requestLogResponder{scripts: scripts}
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
	// Three working Turns and then TWO more requests: the engine fold's summary call, then the
	// wrap-up Turn — both EXTRA and uncounted, so the cap still buys the three requests it names.
	if responder.calls != 5 {
		t.Errorf("upstream calls = %d, want 5 — three working Turns, the fold and the wrap-up", responder.calls)
	}
	if fold := responder.requests[3]; len(fold.Tools) != 0 || !requestSystemContains(fold, foldInstructionPhrase) {
		t.Errorf("the fourth request = %+v, want the engine fold: no tools and the delegate-fold instruction as its system prompt", fold.Messages)
	}
	if got := len(responder.requests[4].Tools); got != 0 {
		t.Errorf("the wrap-up request carries %d tools, want 0 — the menu is withdrawn for it", got)
	}
	// The counter names the next Turn: the wrap-up ends through endExchangeDone, which advances
	// once for it, so a capped child ends at cap+1 — the index encodeState stores (state.go) and a
	// resume reads back must match the Turns actually taken, no more and no fewer. The fold is not a
	// Turn and advances nothing.
	if a.turns.index != 4 {
		t.Errorf("turn index = %d, want 4 — cap Turns plus the wrap-up, advanced exactly once each; the fold is no Turn", a.turns.index)
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
// delegation stopped at its cap is NOT an error result — it carries the marker line, the engine's
// own fold of the child's conversation under `[engine summary]`, and the closing report the
// tool-less wrap-up Turn authored under `[delegate's closing report]` — and the parent's own Turn
// continues to a normal Exchange end. The fold's summary call is accounted as Maintenance with the
// DelegateFold flag and is no Turn: the child's TurnEvents are its working Turns plus the wrap-up.
func TestSubAgent_StepCapReturnsAPartialResultToTheParent(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, reader)
	cfg.Delegation.MaxSteps = 3

	scripts := [][]provider.Delta{subAgentCallScript("c1", "trawl the repo")}
	scripts = append(scripts, cappedChildTurns(3)...)
	// The child's 4th request is the engine fold — the summary the parent reads whatever the
	// wrap-up produces — and its 5th the wrap-up: its menu is gone, so what it answers with is the
	// report the parent reads rather than narration scavenged from a tool round.
	scripts = append(scripts, foldScript(700, 40))
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
	if want := cappedResult(fmt.Sprintf(stepCapResultFormat, 3), childFoldSummary, childClosingReport); sub.Content != want {
		t.Errorf("sub_agent result = %q, want %q — head, engine summary, closing report", sub.Content, want)
	}
	// The fold request: tool-less, under the delegate-fold instruction, and answered before the
	// wrap-up — so the summary is in hand whatever the wrap-up then does.
	if fold := responder.requests[len(scripts)-3]; len(fold.Tools) != 0 || !requestSystemContains(fold, foldInstructionPhrase) {
		t.Errorf("the fold request = %+v, want no tools and the delegate-fold instruction as its system prompt", fold.Messages)
	}
	// The wrap-up request itself: the child's last one, sent with the menu withdrawn — which is
	// what makes the report a report instead of a fourth tool call.
	if got := len(responder.requests[len(scripts)-2].Tools); got != 0 {
		t.Errorf("the wrap-up request carries %d tools, want 0", got)
	}
	// The fold's accounting: one Maintenance reading flagged as the engine fold on the child's
	// stream, and no Turn for it — the child's TurnEvents are its three working Turns plus the
	// wrap-up, exactly as before the fold existed.
	if folds := engineFoldUsage(sink.events, 1); len(folds) != 1 || folds[0].PromptTokens != 700 {
		t.Errorf("engine-fold UsageEvents at Depth 1 = %+v, want exactly one Maintenance reading flagged DelegateFold with the fold's 700 prompt tokens", folds)
	}
	childTurns := 0
	for _, te := range turnEvents(sink.events) {
		if te.Depth == 1 {
			childTurns++
		}
	}
	if childTurns != 4 {
		t.Errorf("child TurnEvents = %d, want 4 — three working Turns and the wrap-up; the fold is no Turn", childTurns)
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

	scripts := []stubllm.Turn{subAgentCallTurn("c1", "trawl the repo")}
	scripts = append(scripts, narratedChildTurns(3)...)
	scripts = append(scripts, contentTurn(childFoldSummary))
	scripts = append(scripts, errorScript("upstream exploded on the wrap-up"))
	scripts = append(scripts, contentTurn("parent done"))

	a, err := newAgent(cfg, scriptedResponder(t, scripts...))
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
	if want := cappedResult(fmt.Sprintf(stepCapResultFormat, 3), childFoldSummary, "reading file 2"); sub.Content != want {
		t.Errorf("sub_agent result = %q, want %q — the engine summary, then the pre-cap last visible text as the closing report", sub.Content, want)
	}
}

// TestSubAgent_StepCapReportsAnUnavailableEngineFold drives the fold's own fallback: the summary
// call is a best effort like the wrap-up, so a fault on it must neither fail the delegation nor
// cost the parent the closing report — the engine-summary sub-head carries the unavailable marker
// naming the cause, and the report the wrap-up then authors lands under its own sub-head as ever.
func TestSubAgent_StepCapReportsAnUnavailableEngineFold(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, reader)
	cfg.Delegation.MaxSteps = 3

	scripts := []stubllm.Turn{subAgentCallTurn("c1", "trawl the repo")}
	scripts = append(scripts, narratedChildTurns(3)...)
	scripts = append(scripts, errorScript("summarizer exploded"))
	scripts = append(scripts, contentTurn(childClosingReport))
	scripts = append(scripts, contentTurn("parent done"))

	a, err := newAgent(cfg, scriptedResponder(t, scripts...))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	_ = a.Submit(domain.UserInput{Text: "please research"})
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.Status != domain.StatusExchangeComplete || res.Faulted {
		t.Errorf("parent result = %+v, want a clean exchange-complete — a failed fold is the child's, not the parent's", res)
	}
	sub, ok := lastSubAgentResult(sink.events)
	if !ok {
		t.Fatal("no sub_agent tool result emitted")
	}
	if sub.IsError {
		t.Errorf("sub_agent result IsError = true after a faulted fold; the cap is never reported as a failure: %q", sub.Content)
	}
	head := fmt.Sprintf(stepCapResultFormat, 3) + "\n" + engineSummaryHead + "\n[engine summary unavailable — "
	if !strings.HasPrefix(sub.Content, head) || !strings.Contains(sub.Content, "summarizer exploded") {
		t.Errorf("sub_agent result = %q, want it to open with %q and name the fold's cause", sub.Content, head)
	}
	if !strings.HasSuffix(sub.Content, "\n\n"+closingReportHead+"\n"+childClosingReport) {
		t.Errorf("sub_agent result = %q, want the closing report under its sub-head after the unavailable marker", sub.Content)
	}
	if !hasErrorContaining(sink.events, 1, "step cap") {
		t.Error("no ErrorEvent at Depth 1 names the cap; the fold's fault must not displace the cap's own notice")
	}
	if got := countCapErrors(sink.events, 1); got != 1 {
		t.Errorf("step-cap ErrorEvents at Depth 1 = %d, want exactly 1", got)
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

	scripts := []stubllm.Turn{
		subAgentCallTurn("c1", "trawl the repo"),
		toolCallTurn("t0", "read_thing", `{"n":0}`), // no visible text on either child Turn
		toolCallTurn("t1", "read_thing", `{"n":1}`), // (arguments differ per Turn so the tool-loop
		//                                                breaker guard reads no identical repeat)
		contentTurn(childFoldSummary), // the engine fold, answered whatever the child said
		// …and none on the wrap-up either: a child that answers its closing request with nothing
		// but another tool call commits no assistant message, so there is still nothing to show.
		toolCallTurn("t2", "read_thing", `{"n":2}`),
		contentTurn("parent done"),
	}
	a, err := newAgent(cfg, scriptedResponder(t, scripts...))
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
	if want := cappedResult(fmt.Sprintf(stepCapResultFormat, 2), childFoldSummary, stepCapNoTextMarker); sub.Content != want {
		t.Errorf("sub_agent result = %q, want %q — the engine summary, then the no-text marker under the closing-report sub-head", sub.Content, want)
	}
	if strings.Contains(sub.Content, "completed") {
		t.Errorf("sub_agent result = %q — a capped delegation must never be reported as completed", sub.Content)
	}
}

// closingShapeFixture reads one of the session-mining fixtures under testdata/closingshape: the
// closing texts capped delegates actually handed their parents on 2026-09-18 (session
// 20260918T143011Z-9788d447, transcript entries 277, 442, 580, 892 and 1106), copied verbatim
// rather than paraphrased, because the shapes they carry — a header on the SECOND line, an intent
// MID-line — are exactly what a one-line quote would have flattened away.
func closingShapeFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "closingshape", name+".txt"))
	if err != nil {
		t.Fatalf("closing-shape fixture %s: %v", name, err)
	}
	return string(data)
}

// TestCapResultHead_NonReportVariantsKeepTheBoundPrefix pins the twelve head lines the parent
// model can read on a bounded delegation — three bounds × (a report, the four non-report shapes)
// — each keeping the `[delegate stopped at its <bound>;` prefix the TUI's recogniser anchors on,
// and each non-report variant naming its shape. The receiver is a bare child Agent, as in
// delegationResult's own tests.
func TestCapResultHead_NonReportVariantsKeepTheBoundPrefix(t *testing.T) {
	t.Parallel()

	bounds := []struct {
		name   string
		child  *Agent
		prefix string
	}{
		{"step cap", &Agent{stepCap: 3}, "[delegate stopped at its step cap (3 steps);"},
		{"token budget", &Agent{capHit: boundTokens, tokenCap: 20000000}, "[delegate stopped at its token budget (20000000 tokens);"},
		{"time limit", &Agent{capHit: boundTime, timeCap: 2 * time.Hour}, "[delegate stopped at its time limit (2h0m);"},
	}
	shapes := []floor.ClosingShape{floor.ShapeReport, floor.ShapeToolCallMarkup, floor.ShapeFileDump, floor.ShapeGrepDump, floor.ShapeNarration}
	for _, b := range bounds {
		for _, shape := range shapes {
			t.Run(b.name+"/"+string(shape), func(t *testing.T) {
				t.Parallel()

				got := b.child.capResultHead(shape)

				if !strings.HasPrefix(got, b.prefix) {
					t.Errorf("head = %q, want the prefix %q", got, b.prefix)
				}
				wantTail := " partial result — engine summary and closing report follow]"
				if shape.IsNonReport() {
					wantTail = " no closing report — the delegate's last reply reads as " + string(shape) + ", not a finding; engine summary follows]"
				}
				if !strings.HasSuffix(got, wantTail) {
					t.Errorf("head = %q, want it to end %q", got, wantTail)
				}
			})
		}
	}
}

// TestSubAgent_StepCapNamesANarratingChildsNonReport drives the non-report path end to end: a
// capped child whose wrap-up reply is [580]'s text — a thinking fence and "Let me look at …" —
// hands its parent the narration variant of the step-cap head, the engine summary, and then the
// text WHOLE under the narration sub-head. Nothing is dropped and nothing is an error: the head
// and the sub-head correct the parent's reading of the text, the fold stands as the finding.
func TestSubAgent_StepCapNamesANarratingChildsNonReport(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, reader)
	cfg.Delegation.MaxSteps = 2
	narration := closingShapeFixture(t, "580-trailing-intent")

	scripts := []stubllm.Turn{
		subAgentCallTurn("c1", "trawl the repo"),
		toolCallTurn("t0", "read_thing", `{"n":0}`),
		toolCallTurn("t1", "read_thing", `{"n":1}`),
		contentTurn(childFoldSummary),
		contentTurn(narration), // the wrap-up reply: narration, not a report
		contentTurn("parent done"),
	}
	a, err := newAgent(cfg, scriptedResponder(t, scripts...))
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
	if sub.IsError {
		t.Errorf("sub_agent result IsError = true; a non-report is named, never faulted: %q", sub.Content)
	}
	want := fmt.Sprintf(stepCapNonReportFormat, 2, floor.ShapeNarration) + "\n" +
		engineSummaryHead + "\n" + childFoldSummary + "\n\n" + closingNarrationHead + "\n" + narration
	if sub.Content != want {
		t.Errorf("sub_agent result = %q, want %q — variant head, engine summary, the text whole under the narration sub-head", sub.Content, want)
	}
}

// TestSubAgent_StepCapZeroIsUnbounded proves 0 means OFF: a child that takes more Turns than any
// cap in these tests still runs to its own final answer and reports it as a plain success.
func TestSubAgent_StepCapZeroIsUnbounded(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, reader)
	cfg.Delegation.MaxSteps = 0

	// The call asks for a cap of its own: against an UNBOUNDED cap the ask is ignored and — the
	// exact Content below is the pin — no clamp note is written for it either.
	scripts := []stubllm.Turn{
		toolCallTurn("c1", tools.SubAgentToolName, subAgentArgsCapped("trawl the repo", 2)),
	}
	scripts = append(scripts, narratedChildTurns(4)...)
	scripts = append(scripts, contentTurn("the child's own final answer"), contentTurn("parent done"))
	a, err := newAgent(cfg, scriptedResponder(t, scripts...))
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
		t.Errorf("sub_agent result = %+v, want the child's own final answer alone (cap 0 = unbounded; no clamp note)", sub)
	}
	if got := countCapErrors(sink.events, 1); got != 0 {
		t.Errorf("step-cap ErrorEvents = %d with the cap switched off, want 0", got)
	}
}

// spentChildTurns is cappedChildTurns with a server-reported usage on every Turn: prompts[i] is
// the prompt-token count Turn i reports, which the child's own tally sums — the reading the token
// bound (Agent.tokenCap) is enforced against.
func spentChildTurns(prompts ...int) [][]provider.Delta {
	out := make([][]provider.Delta, 0, len(prompts))
	for i, prompt := range prompts {
		script := narratedToolCallScript(
			fmt.Sprintf("t%d", i), "read_thing", fmt.Sprintf(`{"n":%d}`, i), fmt.Sprintf("reading file %d", i))
		script[len(script)-1].Usage = &provider.Usage{PromptTokens: prompt, TotalTokens: prompt}
		out = append(out, script)
	}
	return out
}

// steppingClock is a pinned `now` that moves by step on every reading after the first, so a bound
// measured against it trips deterministically and never waits on the wall.
func steppingClock(start time.Time, step time.Duration) func() time.Time {
	calls := 0
	return func() time.Time {
		now := start.Add(time.Duration(calls) * step)
		calls++
		return now
	}
}

// runBoundedDelegation drives one delegation of a parent built on cfg whose child spends the
// scripted Turns, then answers the engine fold with childFoldSummary and the wrap-up with
// childClosingReport, and lets the parent finish. It returns the recorded events, the sub_agent
// result and the responder, for the bound tests below to read their own marker off.
func runBoundedDelegation(t *testing.T, cfg domain.Config, sink *recordingSink, now func() time.Time,
	childTurns [][]provider.Delta) (domain.ToolResult, *requestLogResponder) {
	t.Helper()
	scripts := [][]provider.Delta{subAgentCallScript("c1", "trawl the repo")}
	scripts = append(scripts, childTurns...)
	scripts = append(scripts, contentScript(childFoldSummary), contentScript(childClosingReport), contentScript("parent done"))
	responder := &requestLogResponder{scripts: scripts}
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if now != nil {
		a.now = now
	}
	if err := a.Submit(domain.UserInput{Text: "please research"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete || res.Faulted || res.StepCapped {
		t.Errorf("parent result = %+v, want a clean uncapped exchange-complete", res)
	}
	if responder.calls != len(scripts) {
		t.Errorf("upstream calls = %d, want %d — the bound ends the child after its scripted Turns, one fold and one wrap-up",
			responder.calls, len(scripts))
	}
	sub, ok := lastSubAgentResult(sink.events)
	if !ok {
		t.Fatal("no sub_agent tool result emitted")
	}
	return sub, responder
}

// TestSubAgent_TokenBudgetEndsTheChildThroughTheWrapUp pins the token bound (`delegate-max-tokens`,
// Config.Delegation.MaxTokens): a child whose usage tally passes the budget after its second Turn
// is ended exactly as the step cap ends one — one wrap-up request with the menu withdrawn and the
// budget named in its directive, a non-error result opening with the token marker, one ErrorEvent
// on the child's stream naming the key — while the parent's Exchange completes.
func TestSubAgent_TokenBudgetEndsTheChildThroughTheWrapUp(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, reader)
	cfg.Delegation.MaxSteps = 80
	cfg.Delegation.MaxTokens = 20_000_000

	// 10M after Turn 1 (under), 25M after Turn 2 (over): the bound trips at the second boundary.
	sub, responder := runBoundedDelegation(t, cfg, sink, nil, spentChildTurns(10_000_000, 15_000_000))

	if sub.IsError {
		t.Errorf("sub_agent result IsError = true for a delegation stopped at its token budget: %q", sub.Content)
	}
	if want := cappedResult(fmt.Sprintf(tokenCapResultFormat, 20_000_000), childFoldSummary, childClosingReport); sub.Content != want {
		t.Errorf("sub_agent result = %q, want %q — the token head, the engine summary, the wrap-up reply", sub.Content, want)
	}
	wrapUp := responder.requests[len(responder.requests)-2]
	if got := len(wrapUp.Tools); got != 0 {
		t.Errorf("the wrap-up request carries %d tools, want 0", got)
	}
	if directive := fmt.Sprintf(wrapUpTokenDirectiveFormat, 20_000_000); !strings.HasSuffix(requestTail(t, wrapUp).Content, domain.RenderEngineNote(wrapUpNoteTopic, directive)) {
		t.Errorf("wrap-up tail = %q, want the closing tool result carrying the token directive %q", requestTail(t, wrapUp).Content, directive)
	}
	if !hasErrorContaining(sink.events, 1, "token budget (20000000 tokens)") ||
		!hasErrorContaining(sink.events, 1, "raise delegate-max-tokens") {
		t.Error("no ErrorEvent at Depth 1 names the token budget and the key that raises it")
	}
	if got := countCapErrors(sink.events, 1); got != 0 {
		t.Errorf("step-cap ErrorEvents = %d, want 0 — the step cap was never reached", got)
	}
}

// TestSubAgent_TimeLimitEndsTheChildThroughTheWrapUp pins the time bound (`delegate-timeout`,
// Config.Delegation.Timeout) on a pinned clock threaded from the parent: the child's first request
// starts the clock, and a reading past the limit at the next boundary ends it through the same
// wrap-up path with the time marker — spelled `2h0m`, the duration's own text without its idle
// seconds.
func TestSubAgent_TimeLimitEndsTheChildThroughTheWrapUp(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, reader)
	cfg.Delegation.MaxSteps = 80
	cfg.Delegation.Timeout = 2 * time.Hour

	// Every reading after the first is three hours on, so the child's first boundary is past 2h.
	clock := steppingClock(time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC), 3*time.Hour)
	sub, responder := runBoundedDelegation(t, cfg, sink, clock, cappedChildTurns(1))

	if sub.IsError {
		t.Errorf("sub_agent result IsError = true for a delegation stopped at its time limit: %q", sub.Content)
	}
	if want := cappedResult(fmt.Sprintf(timeCapResultFormat, "2h0m"), childFoldSummary, childClosingReport); sub.Content != want {
		t.Errorf("sub_agent result = %q, want %q — the time head, the engine summary, the wrap-up reply", sub.Content, want)
	}
	wrapUp := responder.requests[len(responder.requests)-2]
	if got := len(wrapUp.Tools); got != 0 {
		t.Errorf("the wrap-up request carries %d tools, want 0", got)
	}
	if directive := fmt.Sprintf(wrapUpTimeDirectiveFormat, "2h0m"); !strings.HasSuffix(requestTail(t, wrapUp).Content, domain.RenderEngineNote(wrapUpNoteTopic, directive)) {
		t.Errorf("wrap-up tail = %q, want the closing tool result carrying the time directive %q", requestTail(t, wrapUp).Content, directive)
	}
	if !hasErrorContaining(sink.events, 1, "time limit (2h0m)") ||
		!hasErrorContaining(sink.events, 1, "raise delegate-timeout") {
		t.Error("no ErrorEvent at Depth 1 names the time limit and the key that raises it")
	}
}

// TestSubAgent_TokenAndTimeBoundsAtZeroLeaveTheStepCapAlone pins the off spelling of both keys: at
// 0 neither trips however much the child spends or however far the clock moves, and the 80-step
// cap — lowered to 3 here so the test can reach it — is the only thing that ends the child.
func TestSubAgent_TokenAndTimeBoundsAtZeroLeaveTheStepCapAlone(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, reader)
	cfg.Delegation.MaxSteps = 3
	cfg.Delegation.MaxTokens = 0
	cfg.Delegation.Timeout = 0

	clock := steppingClock(time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC), 3*time.Hour)
	sub, _ := runBoundedDelegation(t, cfg, sink, clock, spentChildTurns(30_000_000, 30_000_000, 30_000_000))

	marker := fmt.Sprintf(stepCapResultFormat, 3)
	if !strings.HasPrefix(sub.Content, marker+"\n") {
		t.Errorf("sub_agent result = %q, want it to open with the step-cap marker %q", sub.Content, marker)
	}
	if got := countCapErrors(sink.events, 1); got != 1 {
		t.Errorf("step-cap ErrorEvents at Depth 1 = %d, want exactly 1", got)
	}
	if hasErrorContaining(sink.events, 1, "token budget") || hasErrorContaining(sink.events, 1, "time limit") {
		t.Error("a bound at 0 wrote its ErrorEvent; 0 is off")
	}
}

// TestBoundDurationText pins the spelling the time markers quote: idle trailing seconds are dropped
// after a minutes field and nothing else is touched.
func TestBoundDurationText(t *testing.T) {
	for _, tc := range []struct {
		in   time.Duration
		want string
	}{
		{2 * time.Hour, "2h0m"},
		{90 * time.Minute, "1h30m"},
		{90 * time.Second, "1m30s"},
		{45 * time.Second, "45s"},
	} {
		if got := boundDurationText(tc.in); got != tc.want {
			t.Errorf("boundDurationText(%s) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestSubAgent_MaxStepsArgumentOnlyLowersTheCap pins the argument's one direction: a request
// BELOW the configured cap binds this delegation, a request ABOVE it is applied as the cap — and
// SAID SO, as a note appended to the result body below the partial marker, which stays the first
// line. The model may make a delegation cheaper, never longer than the host allows, and it is told
// when it tried.
func TestSubAgent_MaxStepsArgumentOnlyLowersTheCap(t *testing.T) {
	cases := []struct {
		name       string
		configured int
		requested  int
		wantSteps  int
		wantNote   string
	}{
		{"a lower request binds", 3, 2, 2, ""},
		{"a higher request is clamped and announced", 3, 9, 3,
			"[max_steps 9 requested; the configured cap is 3 — 3 applied]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
			cfg := subAgentConfig(sink, domain.ModeAskBefore, reader)
			cfg.Delegation.MaxSteps = tc.configured

			scripts := []stubllm.Turn{
				toolCallTurn("c1", tools.SubAgentToolName, subAgentArgsCapped("trawl the repo", tc.requested)),
			}
			// wantSteps working Turns, then the engine fold and the one tool-less wrap-up Turn the
			// cap spends: the bound governs the WORK, and both are extra however low it is set.
			scripts = append(scripts, narratedChildTurns(tc.wantSteps)...)
			scripts = append(scripts, contentTurn(childFoldSummary), contentTurn(childClosingReport))
			scripts = append(scripts, contentTurn("parent done"))
			responder := scriptedResponder(t, scripts...)

			a, err := newAgent(cfg, responder)
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			_ = a.Submit(domain.UserInput{Text: "please research"})
			if _, err := a.Run(context.Background()); err != nil {
				t.Fatalf("Run: %v", err)
			}

			if responder.calls() != len(scripts) {
				t.Errorf("upstream calls = %d, want %d — the child ran a different number of Turns than the effective cap plus its fold and wrap-up",
					responder.calls(), len(scripts))
			}
			sub, ok := lastSubAgentResult(sink.events)
			if !ok {
				t.Fatal("no sub_agent tool result emitted")
			}
			if want := fmt.Sprintf(stepCapResultFormat, tc.wantSteps); !strings.HasPrefix(sub.Content, want+"\n") {
				t.Errorf("sub_agent result = %q, want it to open with %q", sub.Content, want)
			}
			if tc.wantNote == "" {
				if strings.Contains(sub.Content, "requested; the configured cap") {
					t.Errorf("sub_agent result = %q, carries a clamp note for a request that bound", sub.Content)
				}
				return
			}
			// Appended, never prefixed: the note is the last line of the body, under the marker.
			if !strings.HasSuffix(sub.Content, "\n"+tc.wantNote) {
				t.Errorf("sub_agent result = %q, want it to end with the clamp note %q", sub.Content, tc.wantNote)
			}
		})
	}
}

// TestResolveStepCap pins the one clamp rule both readers share — runSubAgent seeding the child
// and runDelegation stamping the started phase: an ask only ever lowers a positive configured
// cap, an ask above it is applied as the cap and reported back as the ask, and an ask against an
// unbounded cap (0) or no ask at all leaves the configured value alone and reports nothing.
func TestResolveStepCap(t *testing.T) {
	cases := []struct {
		name                     string
		configured, asked        int
		wantApplied, wantRequest int
	}{
		{"below the cap binds", 80, 40, 40, 0},
		{"at the cap is the cap", 80, 80, 80, 0},
		{"above the cap is clamped and remembered", 80, 120, 80, 120},
		{"an ask against an unbounded cap is ignored", 0, 40, 0, 0},
		{"no ask keeps the configured cap", 80, 0, 80, 0},
		{"no ask against an unbounded cap stays unbounded", 0, 0, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			applied, requested := resolveStepCap(tc.configured, tc.asked)
			if applied != tc.wantApplied || requested != tc.wantRequest {
				t.Errorf("resolveStepCap(%d, %d) = (%d, %d), want (%d, %d)",
					tc.configured, tc.asked, applied, requested, tc.wantApplied, tc.wantRequest)
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

	scripts := narratedChildTurns(3)
	scripts = append(scripts, contentTurn("the main agent's own final answer"))
	a, err := newAgent(cfg, scriptedResponder(t, scripts...))
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
func foldedSummaryRequest(reqs []stubllm.Request, summary string) (stubllm.Request, bool) {
	for _, req := range reqs {
		for _, m := range req.Messages {
			if strings.Contains(m.Content, summary) {
				return req, true
			}
		}
	}
	return stubllm.Request{}, false
}

// TestSubAgent_ChildFoldsMidDelegationAndFinishes is the delegate half of the child's mid-Exchange
// fold: a delegation whose tool result pushes its history past its Budget allocation folds DURING
// the delegation — there is no Exchange boundary to wait for, the whole delegation being one
// Exchange — and still reports its answer to the parent. The request the child sends after the
// fold is template-legal (no orphaned tool result, no unanswered tool call, strict alternation),
// which is what makes the quiescent Turn boundary a safe place to fold.
func TestSubAgent_ChildFoldsMidDelegationAndFinishes(t *testing.T) {
	sink := &recordingSink{}
	// ~25k chars ≈ 6.2k tokens, past the ~3.6k-token History allocation of the 8k window below.
	bulky := fakeTool{name: "read_thing", readOnly: true, result: strings.Repeat("x", 25000)}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, bulky)
	cfg.Context.MaxContextTokens = 8192
	cfg.Context.CompactionEnabled = true

	up := scriptedCompactResponder(t, "CHILD-SUMMARY",
		subAgentCallTurn("c1", "trawl the repo"),    // parent Turn 0: delegate
		toolCallTurn("t1", "read_thing", `{}`),      // child Turn 0: the oversized read
		contentTurn("the child's own final answer"), // child Turn 1: folds at its top, then answers
		contentTurn("parent done"),                  // parent Turn 1: finish
	)
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

	if up.summaryCalls() != 1 {
		t.Fatalf("folds during the delegation = %d, want exactly 1 — the child must fold mid-Exchange", up.summaryCalls())
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

	req, ok := foldedSummaryRequest(up.mains(), "CHILD-SUMMARY")
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

	up := scriptedCompactResponder(t, "CHILD-SUMMARY",
		subAgentCallTurn("c1", "trawl the repo"),
		toolCallTurn("t1", "read_thing", `{}`),
		contentTurn("the child's own final answer"),
		contentTurn("parent done"),
	)
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

	if up.summaryCalls() != 0 {
		t.Errorf("summarizer calls = %d with `auto-compact` off, want 0 — the child's fold obeys the same gate", up.summaryCalls())
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
	parent, err := newAgent(cfg, scriptedResponder(t))
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
	defer func() { _ = child.Close() }()
	if !child.midExchangeCompaction {
		t.Error("a child agent does not compact mid-Exchange; its whole life is one Exchange, so it would never fold")
	}
}

// TestNewChildAgent_IsBuiltFromOneDelegationValue pins the construction contract behind every
// spawn: newChildAgentOn composes ONE delegation value and newDelegateAgent copies it into the child
// once — the child leaves construction complete, with its identity and bounds stamped, the parent's
// handles shared by reference where the parent's session owns the state (journal, Console registry,
// Delegation-target latch, context-file cache, clock) and fresh instances where the run is its own
// (task list, isolated guards). The LIVE parent facts are read at spawn: the effort dialect is the
// parent's field, not the Config copy, and the mode view is the parent's tighten-only accessor.
func TestNewChildAgent_IsBuiltFromOneDelegationValue(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore)
	cfg.Delegation.MaxSteps = 7
	cfg.Delegation.MaxTokens = 9000
	cfg.Delegation.Timeout = 3 * time.Minute
	parent, err := newAgent(cfg, scriptedResponder(t))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	pinned := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	parent.now = func() time.Time { return pinned }
	parent.effortDialect = provider.EffortDialectReasoning // the LIVE field, deliberately apart from the Config copy
	parent.contextFiles = []contextFile{{name: "AGENTS.md", content: "parent bytes", size: 12}}

	child, err := parent.newChildAgent("c1", "summarise the repo", "summariser")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}
	defer func() { _ = child.Close() }()

	// Identity and bounds, stamped once.
	if child.depth != parent.depth+1 {
		t.Errorf("child.depth = %d, want %d", child.depth, parent.depth+1)
	}
	if child.callID != "c1" || child.task != "summarise the repo" || child.displayName() != "summariser" {
		t.Errorf("child identity = (%q, %q, %q), want (c1, summarise the repo, summariser)", child.callID, child.task, child.displayName())
	}
	if child.stepCap != 7 || child.tokenCap != 9000 || child.timeCap != 3*time.Minute {
		t.Errorf("child bounds = (%d, %d, %v), want (7, 9000, 3m)", child.stepCap, child.tokenCap, child.timeCap)
	}
	if !child.midExchangeCompaction {
		t.Error("a delegate folds mid-Exchange; the contract is set at construction")
	}
	if child.ownsUpstream || child.seatFallback {
		t.Errorf("unrouted default-seat child: ownsUpstream = %v, seatFallback = %v, want both false", child.ownsUpstream, child.seatFallback)
	}
	if child.consoleOwner == "" {
		t.Error("child.consoleOwner is empty; a delegate carries the engine-minted Console privilege key")
	}
	// Live parent facts, read at spawn.
	if child.effortDialect != provider.EffortDialectReasoning {
		t.Errorf("child.effortDialect = %q, want the parent's LIVE %q", child.effortDialect, provider.EffortDialectReasoning)
	}
	if child.liveMode == nil {
		t.Error("child.liveMode is nil; a delegate reads its parent's effective mode through the tighten-only view")
	}
	if got := child.now(); !got.Equal(pinned) {
		t.Errorf("child.now() = %v, want the parent's pinned clock %v", got, pinned)
	}
	// Handles shared by reference…
	if child.journal != parent.journal {
		t.Error("child.journal is not the parent's; delegated writes belong to the current Exchange's undo step (ADR 0051)")
	}
	if child.consoles != parent.consoles {
		t.Error("child.consoles is not the parent's; one engine has one Console registry (ADR 0059 §6)")
	}
	if child.delegation != parent.delegation {
		t.Error("child.delegation latch is not the parent's; routing reaches every depth from the one place a host pushes to (ADR 0045)")
	}
	if got := cachedContent(child.contextFiles, "AGENTS.md"); got != "parent bytes" {
		t.Errorf("child context-file cache = %q, want the parent's %q", got, "parent bytes")
	}
	// …and the ones each run owns for itself.
	if child.tasks == parent.tasks || child.tasks == nil {
		t.Error("child.tasks must be a fresh list of its own (ADR 0072)")
	}
	if child.guards.Breaker == parent.guards.Breaker {
		t.Error("child.guards shares the parent's live breaker; ForSubAgent isolates live state")
	}
}

// TestNewChildAgentOn_SessionSeatGetsAnEmptyLatch: the one place the composed value departs from
// sharing — a session-seated child holds an empty Delegation-target latch of its own, never the
// parent's, so its grandchildren stay on the session server (ADR 0069 decision 3).
func TestNewChildAgentOn_SessionSeatGetsAnEmptyLatch(t *testing.T) {
	t.Parallel()

	parent, err := newAgent(subAgentConfig(&recordingSink{}, domain.ModeAskBefore), scriptedResponder(t))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	child, err := parent.newChildAgentOn(seatSession, "c1", "", "stay here", "")
	if err != nil {
		t.Fatalf("newChildAgentOn: %v", err)
	}
	defer func() { _ = child.Close() }()

	if child.delegation == parent.delegation {
		t.Error("a session-seated child shares the parent's latch; it must hold an empty one of its own")
	}
	if child.delegation == nil || child.delegation.snapshot() != nil {
		t.Error("a session-seated child's latch must be present and empty")
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
func cappedChildScripts() []stubllm.Turn {
	return []stubllm.Turn{
		subAgentCallTurn("c1", "audit the parser"),
		{Text: truncatedChildText, FinishReason: "length"},
		contentTurn("parent done"),
	}
}

// TestSubAgent_CappedChildReplyReportsAsErrorNamingTheCause proves both halves of the delegate rule
// end to end: the child's truncated answer faults instead of posing as the delegation's result, and
// the error result the parent MODEL receives carries the child's own cause sentence rather than
// pointing at an error only the human can see.
func TestSubAgent_CappedChildReplyReportsAsErrorNamingTheCause(t *testing.T) {
	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore)

	a, err := newAgent(cfg, scriptedResponder(t, cappedChildScripts()...))
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

	cutOffCall := narratedToolCallTurn("c2", "read_thing", `{}`, "reading the entry point first")
	cutOffCall.FinishReason = "length"
	scripts := []stubllm.Turn{
		subAgentCallTurn("c1", "audit the parser"),
		cutOffCall,
		contentTurn("the parser is fine"), // the child's next Turn answers normally
		contentTurn("parent done"),
	}

	a, err := newAgent(cfg, scriptedResponder(t, scripts...))
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
	a.runIDs = newRunIDMinter(testRunIDPrefix)
	responder.before = func(call int) {
		if call != 1 {
			return
		}
		for _, remark := range remarks {
			if err := a.InterjectChild(firstRunID, domain.UserInput{Text: remark}); err != nil {
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
// not just runSubAgent's return value: an oversized child answer is cut twice on its way into the
// conversation — by the absolute cap in delegationResult and then by the structural clamp
// (appendToolResult) — and because both keep the tail, the notice comes through as the result's
// final line, directly after the answer's own last line. The lines are distinct and numbered:
// one line repeated is degenerate narration and would be faulted before either cut.
func TestSubAgent_ParentNoticeSurvivesTheStructuralClamp(t *testing.T) {
	// Past the absolute cap, and far past the structural floor at any window this harness can
	// have, so both cuts really do fire.
	answer := numberedReport(4000)

	res := runSteeredDelegation(t, answer, "focus on the tests")

	if len(res.Content) >= len(answer) {
		t.Fatalf("committed result is %d bytes for a %d-byte answer: neither cut fired, so this proves nothing", len(res.Content), len(answer))
	}
	want := "the child has a great deal to say about the repo, line 4000\n\n" + userSteeredTrailerSingular
	if !strings.HasSuffix(res.Content, want) {
		t.Errorf("clamped result ends %q, want it to end with the answer's last line then the parent notice %q", res.Content[max(0, len(res.Content)-160):], want)
	}
}

// numberedReport is a child answer of n distinct, numbered lines — the fixture for the two cuts,
// shaped so no line repeats and the degenerate check stays out of the way.
func numberedReport(n int) string {
	lines := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		lines = append(lines, fmt.Sprintf("the child has a great deal to say about the repo, line %d", i))
	}
	return strings.Join(lines, "\n")
}

// completedChild is a child Agent whose conversation closes on answer, for driving
// delegationResult's completed outcome directly — the committed ToolResultEvent runs through the
// structural clamp as well, which would hide the absolute cap's own shape.
func completedChild(answer string, steered int) *Agent {
	return &Agent{
		conv:    *domain.NewConversation([]domain.Message{{Role: domain.RoleAssistant, Content: answer}}),
		steered: steered,
	}
}

// TestSubAgent_DegenerateNarrationIsAFault pins the fourth shape: a closing text whose most
// frequent line occurs fifty times or more is an error result naming the count and carrying the
// first twenty lines only, while a long report of distinct lines is the report it always was.
func TestSubAgent_DegenerateNarrationIsAFault(t *testing.T) {
	t.Parallel()

	recital := strings.TrimSuffix(strings.Repeat("Emit.\nwrite_file.\nGO.\n", 2000), "\n")

	got, _ := completedChild(recital, 0).delegationResult("c1", domain.StepResult{}, nil)

	want := fmt.Sprintf(degenerateResultFormat, 2000) + "\n" + strings.Join(strings.Split(recital, "\n")[:degenerateResultHeadLines], "\n")
	if !got.IsError || got.Content != want {
		t.Errorf("sub_agent result = %+v, want the error %q", got, want)
	}

	t.Run("a distinct-lined report is not degenerate", func(t *testing.T) {
		t.Parallel()
		report := numberedReport(200)

		got, _ := completedChild(report, 0).delegationResult("c1", domain.StepResult{}, nil)

		if got.IsError || got.Content != report {
			t.Errorf("sub_agent result = %+v, want the report byte for byte", got)
		}
	})
}

// TestSubAgent_ResultIsCappedAtSixtyFourKiB pins the absolute cap on a completed report: a 200 KB
// report of distinct lines comes back within delegateResultMaxBytes, its first and last lines
// intact around the shared elision marker, with the steered trailer after the capped tail — and a
// 30 KB report comes back byte for byte.
func TestSubAgent_ResultIsCappedAtSixtyFourKiB(t *testing.T) {
	t.Parallel()

	t.Run("a 200 KB report is elided to the cap", func(t *testing.T) {
		t.Parallel()
		report := numberedReport(3500)
		if len(report) < 200*1000 {
			t.Fatalf("fixture is %d bytes, want at least 200 KB", len(report))
		}

		got, _ := completedChild(report, 1).delegationResult("c1", domain.StepResult{}, nil)

		body := strings.TrimSuffix(got.Content, "\n\n"+userSteeredTrailerSingular)
		if body == got.Content {
			t.Fatalf("result = %q, want the steered trailer after the capped tail", got.Content[max(0, len(got.Content)-160):])
		}
		if got.IsError || len(body) > delegateResultMaxBytes {
			t.Errorf("capped body is %d bytes (IsError %v), want at most %d", len(body), got.IsError, delegateResultMaxBytes)
		}
		if !strings.HasPrefix(body, "the child has a great deal to say about the repo, line 1\n") {
			t.Errorf("capped body opens %q, want the report's first line", body[:min(len(body), 80)])
		}
		if !strings.HasSuffix(body, "\nthe child has a great deal to say about the repo, line 3500") {
			t.Errorf("capped body ends %q, want the report's last line", body[max(0, len(body)-80):])
		}
		if !strings.Contains(body, "[truncated to fit the context budget") {
			t.Errorf("capped body carries no elision marker")
		}
		if strings.Contains(body, "line 1750\n") {
			t.Errorf("capped body still carries the report's middle")
		}
	})

	t.Run("a 30 KB report is untouched", func(t *testing.T) {
		t.Parallel()
		report := numberedReport(520)
		if len(report) < 30*1000 || len(report) > delegateResultMaxBytes {
			t.Fatalf("fixture is %d bytes, want about 30 KB under the cap", len(report))
		}

		got, _ := completedChild(report, 0).delegationResult("c1", domain.StepResult{}, nil)

		if got.IsError || got.Content != report {
			t.Errorf("sub_agent result = %+v, want the report byte for byte", got)
		}
	})
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
		{"faulted", &Agent{steered: 2, turns: &turnLifecycle{lastFault: "the upstream died"}}, domain.StepResult{Faulted: true}, nil, subAgentFaultPrefix + "the upstream died"},
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
// The wrap-up Turn keeps one write_file — narrowed to a spawn-named output_path where there is one
// (plan 2026-09-14 - 03, item 8; plan 2026-09-18 - 00, item 5)
// ----------------------------------------------------------------------------

// outputPathAgent builds a parent in mode over a real workspace with a real write_file tool and
// the fake reader, capped at two child Turns, whose one delegation names outputPath (or none, for
// ""). The scripts are the child's two capped Turns, the engine fold's reply, then the wrap-up
// reply given, then the parent's close. It returns the parent, the responder that logs every
// request the tree sent, the sink, and the workspace root the output path resolves against.
func outputPathAgent(t *testing.T, mode domain.Mode, outputPath string, wrapUp []provider.Delta) (*Agent, *requestLogResponder, *recordingSink, string) {
	t.Helper()

	// The workspace root is resolved, not as t.TempDir() spells it. The ledger records the
	// RESOLVED output target, so an expectation joined against an unresolved root would compare
	// two spellings of the same directory — which is exactly what macOS hands back, where
	// t.TempDir() returns /var/folders/... and /var is a symlink to /private/var.
	ws, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks(t.TempDir()): %v", err)
	}
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	cfg := subAgentConfig(sink, mode, reader, tools.NewWriteFile(ws))
	cfg.WorkspaceDir = ws
	cfg.Delegation.MaxSteps = 2

	call, err := json.Marshal(tools.SubAgentArgs{Task: "trawl the repo", OutputPath: outputPath})
	if err != nil {
		t.Fatalf("marshal sub_agent args: %v", err)
	}
	scripts := [][]provider.Delta{toolCallScript("c1", tools.SubAgentToolName, string(call))}
	scripts = append(scripts, cappedChildTurns(2)...)
	scripts = append(scripts, contentScript(childFoldSummary), wrapUp, contentScript("parent done"))
	responder := &requestLogResponder{scripts: scripts}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	return a, responder, sink, ws
}

// wrapUpRequest is the child's wrap-up request in a two-Turn capped delegation: the parent's
// opening request, two child Turns, the engine fold's request, then this one.
func wrapUpRequest(t *testing.T, responder *requestLogResponder) provider.Request {
	t.Helper()

	const wrapUpIndex = 4
	if len(responder.requests) <= wrapUpIndex {
		t.Fatalf("requests = %d, want the wrap-up at index %d", len(responder.requests), wrapUpIndex)
	}
	return responder.requests[wrapUpIndex]
}

// requestTail returns the last message of a request the loop sent — where the wrap-up directive
// rides when that message is the capping Turn's closing tool result (Request.NoteOnTail).
func requestTail(t *testing.T, req provider.Request) provider.Message {
	t.Helper()

	if len(req.Messages) == 0 {
		t.Fatal("request carries no messages")
	}
	return req.Messages[len(req.Messages)-1]
}

// TestWrapUpDirectiveRidesTheClosingToolResult pins the placement on a REAL capped run, whose
// wrap-up request ends on the capping Turn's tool result: the directive is fenced onto that tail
// as the engine note — under the engine's own header, after the tool's output — and the system
// prompt carries no copy of it. The one-message-per-Turn shape of the request is untouched: no
// message is added after the tool result. The child holds write_file under Allow-Edits, so the
// directive carries the one-write clause (item 5); the clause rides inside the same fence. At a
// cap of 2 the step-budget notice (stepnotice.go) rides the same result — ceil(0.75 × 2) is the
// capping Turn — as the engine note committed with it, so the tail stacks the two notes in the
// order they landed: the tool's output, the step note, then the wrap-up on the very end.
func TestWrapUpDirectiveRidesTheClosingToolResult(t *testing.T) {
	a, responder, _, _ := outputPathAgent(t, domain.ModeAllowEdits, "", contentScript(childClosingReport))

	runExchange(t, a, "please research")

	req := wrapUpRequest(t, responder)
	tail := requestTail(t, req)
	if tail.Role != string(domain.RoleTool) || tail.ToolCallID == "" {
		t.Fatalf("tail = %+v, want the capping Turn's tool result", tail)
	}
	directive := fmt.Sprintf(wrapUpDirectiveFormat, 2) + wrapUpWriteClause
	body, fence, cut := strings.Cut(tail.Content, "\n\n"+domain.EngineNoteFencePrefix)
	if !cut || body != "package main" {
		t.Fatalf("tail = %q, want the tool's own output %q followed by the engine fence", tail.Content, "package main")
	}
	stepNote := strings.TrimPrefix(domain.RenderEngineNote(stepNoticeTopic, "steps: 2 of 2 used — 0 left before the wrap-up Turn; write your output now"), "\n\n"+domain.EngineNoteFencePrefix)
	if want := stepNote + domain.RenderEngineNote(wrapUpNoteTopic, directive); fence != want {
		t.Errorf("fence = %q, want %q", fence, want)
	}
	if strings.Contains(tail.Content, domain.AdviceFencePrefix) {
		t.Errorf("tail = %q wears the advice fence; an engine note has its own header", tail.Content)
	}
	if got := requestSystemText(req); strings.Contains(got, wrapUpMarker) {
		t.Errorf("system text = %q, want no directive there — it rides the tail", got)
	}
	// The one request before it — the engine fold's — and every working Turn's carry no
	// wrap-up note: the directive is the wrap-up request's alone.
	for i, earlier := range responder.requests[:4] {
		for _, m := range earlier.Messages {
			if strings.Contains(m.Content, domain.EngineNoteFencePrefix+wrapUpNoteTopic) {
				t.Errorf("request %d carries the wrap-up note before the wrap-up: %q", i, m.Content)
			}
		}
	}
}

// TestWrapUpDirectiveFallsBackToTheSystemPromptOnAnAssistantTail pins the other leg of the
// placement: a wrap-up request whose tail is NOT a tool result — here the faulted-then-retried
// shape, where the conversation ends on the assistant's half-reply — cannot take the note, so the
// directive lands in the system prompt through AppendToSystem, and nothing is fenced anywhere.
func TestWrapUpDirectiveFallsBackToTheSystemPromptOnAnAssistantTail(t *testing.T) {
	a, _, _, _ := wrapUpAgent(t, true, contentScript("here is what I found"))
	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: "trawl the repo"})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, Content: "half a reply, then a fault"})

	req, _ := a.buildRequest(1)

	msgs := req.State().Messages
	want := fmt.Sprintf(wrapUpDirectiveFormat, 3)
	if got := requestSystemText(a.toProviderRequest(req)); !strings.Contains(got, want) {
		t.Errorf("system text = %q, want the directive %q — the fallback for a tail that is not a tool result", got, want)
	}
	if tail := msgs[len(msgs)-1]; tail.Role != domain.RoleAssistant || strings.Contains(tail.Content, domain.EngineNoteFencePrefix) {
		t.Errorf("tail = %q (%s), want the assistant message untouched", tail.Content, tail.Role)
	}
	for _, m := range msgs {
		if strings.Contains(m.Content, domain.EngineNoteFencePrefix) {
			t.Errorf("a message carries the engine fence on the fallback path: %q", m.Content)
		}
	}
}

// wrapUpWriteResults returns every tool result a child (Depth 1) committed for the named tool.
func wrapUpWriteResults(events []domain.Event, tool string) []domain.ToolResult {
	ids := map[string]bool{}
	var out []domain.ToolResult
	for _, e := range events {
		switch ev := e.(type) {
		case domain.ToolCallEvent:
			if ev.Depth == 1 && ev.Call.Tool == tool {
				ids[ev.Call.ID] = true
			}
		case domain.ToolResultEvent:
			if ids[ev.Result.CallID] {
				out = append(out, ev.Result)
			}
		}
	}
	return out
}

// TestSubAgent_OutputPathKeepsWriteFileInTheWrapUp drives the exception whole: a delegation
// spawned with an `output_path` reaches its cap, its wrap-up request offers exactly write_file
// and says so after the untouched directive, the scripted write to that path LANDS, and the
// parent still reads the capped result with the closing report the same reply carried.
func TestSubAgent_OutputPathKeepsWriteFileInTheWrapUp(t *testing.T) {
	a, responder, sink, ws := outputPathAgent(t, domain.ModeAllowEdits, "out/report.md",
		narratedToolCallScript("w0", tools.WriteFileToolName,
			`{"path":"out/report.md","content":"the survey so far"}`, childClosingReport))

	runExchange(t, a, "please research")

	req := wrapUpRequest(t, responder)
	if len(req.Tools) != 1 || req.Tools[0].Name != tools.WriteFileToolName {
		t.Fatalf("wrap-up menu = %+v, want exactly write_file", req.Tools)
	}
	want := fmt.Sprintf(wrapUpDirectiveFormat, 2) + fmt.Sprintf(wrapUpOutputClauseFormat, "out/report.md")
	tail := requestTail(t, req)
	if tail.Role != string(domain.RoleTool) || !strings.HasSuffix(tail.Content, domain.RenderEngineNote(wrapUpNoteTopic, want)) {
		t.Errorf("tail = %q (%s), want the closing tool result carrying the directive followed by the output clause %q as its engine note", tail.Content, tail.Role, want)
	}
	if got := requestSystemText(req); strings.Contains(got, wrapUpMarker) {
		t.Errorf("system text = %q carries the directive; with a tool-result tail it rides the tail, not the system prompt", got)
	}

	body, err := os.ReadFile(filepath.Join(ws, "out", "report.md"))
	if err != nil {
		t.Fatalf("the wrap-up write did not land: %v", err)
	}
	if string(body) != "the survey so far" {
		t.Errorf("output file = %q, want the scripted content", body)
	}
	results := wrapUpWriteResults(sink.events, tools.WriteFileToolName)
	if len(results) != 1 || results[0].IsError {
		t.Errorf("write_file results = %+v, want one non-error result", results)
	}

	sub, ok := lastSubAgentResult(sink.events)
	if !ok {
		t.Fatal("no sub_agent tool result emitted")
	}
	head := fmt.Sprintf(stepCapResultFormat, 2)
	if sub.IsError || !strings.HasPrefix(sub.Content, head+"\n") || !strings.HasSuffix(sub.Content, childClosingReport) {
		t.Errorf("sub_agent result = %q, want a non-error capped result carrying the closing report", sub.Content)
	}
	// The fold and the wrap-up bought one request each, never a further working Turn: the child
	// asked four times.
	if got := len(responder.requests); got != 6 {
		t.Errorf("requests = %d, want 6 — parent, two capped child Turns, the fold, the wrap-up, the parent's close", got)
	}
}

// TestWrapUpCallsKeepsTheFirstOutputWrite pins the filter alone, with an output path: of a
// wrap-up reply's three write_file calls — one aimed elsewhere, two at the output path — the filter
// keeps the elsewhere-write (so resolve's refusal can reach the transcript) and the FIRST
// output-path write, and drops the second, which the clause's "once" already withdrew.
func TestWrapUpCallsKeepsTheFirstOutputWrite(t *testing.T) {
	ws := t.TempDir()
	cfg := subAgentConfig(&recordingSink{}, domain.ModeAllowEdits, tools.NewWriteFile(ws))
	cfg.WorkspaceDir = ws
	a, err := newAgent(cfg, &requestLogResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.resolveOutputPath("out/report.md")
	if _, ok := a.wrapUpWriter(); !ok {
		t.Fatal("wrapUpWriter = false, want write_file on the wrap-up menu")
	}
	writeCall := func(id, path string) domain.ToolCall {
		return domain.ToolCall{ID: id, Tool: tools.WriteFileToolName,
			Arguments: json.RawMessage(fmt.Sprintf(`{"path":%q,"content":%q}`, path, id))}
	}

	kept := a.wrapUpCalls([]domain.ToolCall{
		writeCall("elsewhere", "elsewhere.md"),
		writeCall("first", "out/report.md"),
		writeCall("second", "out/report.md"),
	})

	var ids []string
	for _, call := range kept {
		ids = append(ids, call.ID)
	}
	if want := []string{"elsewhere", "first"}; !slices.Equal(ids, want) {
		t.Errorf("kept call ids = %v, want %v — the elsewhere-write and the first output write only", ids, want)
	}
}

// TestSubAgent_WrapUpRunsTheOutputWriteOnce drives the "once" whole: a wrap-up reply that asks
// for two write_file calls to the output path with different content lands the FIRST content and
// commits exactly one write_file result — the second call is dropped undispatched, never run
// over the first.
func TestSubAgent_WrapUpRunsTheOutputWriteOnce(t *testing.T) {
	wrapUp := []provider.Delta{
		{Kind: provider.DeltaContent, Content: childClosingReport},
		{Kind: provider.DeltaToolCall, ToolCall: &provider.ToolCall{ID: "w0", Type: "function",
			Function: provider.FunctionCall{Name: tools.WriteFileToolName,
				Arguments: `{"path":"out/report.md","content":"the first draft"}`}}},
		{Kind: provider.DeltaToolCall, ToolCall: &provider.ToolCall{ID: "w1", Type: "function",
			Function: provider.FunctionCall{Name: tools.WriteFileToolName,
				Arguments: `{"path":"out/report.md","content":"the second draft"}`}}},
		{Kind: provider.DeltaDone, FinishReason: "tool_calls"},
	}
	a, _, sink, ws := outputPathAgent(t, domain.ModeAllowEdits, "out/report.md", wrapUp)

	runExchange(t, a, "please research")

	body, err := os.ReadFile(filepath.Join(ws, "out", "report.md"))
	if err != nil {
		t.Fatalf("the wrap-up write did not land: %v", err)
	}
	if string(body) != "the first draft" {
		t.Errorf("output file = %q, want the FIRST write's content", body)
	}
	results := wrapUpWriteResults(sink.events, tools.WriteFileToolName)
	if len(results) != 1 || results[0].IsError {
		t.Errorf("write_file results = %+v, want exactly one non-error result — the second output write is dropped", results)
	}
}

// TestSubAgent_WrapUpRefusesAWriteElsewhere pins the refusal: a wrap-up write_file aimed anywhere
// but the output path gets exactly the wrap-up refusal naming that path, and writes nothing — so
// the output path is still absent when the run ends, and the capped result says so in its body
// note (item 9) beneath the closing report the same reply carried.
func TestSubAgent_WrapUpRefusesAWriteElsewhere(t *testing.T) {
	a, _, sink, ws := outputPathAgent(t, domain.ModeAllowEdits, "out/report.md",
		narratedToolCallScript("w0", tools.WriteFileToolName,
			`{"path":"elsewhere.md","content":"not the output"}`, childClosingReport))

	runExchange(t, a, "please research")

	results := wrapUpWriteResults(sink.events, tools.WriteFileToolName)
	if len(results) != 1 {
		t.Fatalf("write_file results = %d, want exactly one — the refusal", len(results))
	}
	if !results[0].IsError || results[0].Content != "wrap-up: only out/report.md may be written" {
		t.Errorf("write_file result = %+v, want the exact wrap-up refusal naming the output path", results[0])
	}
	if _, err := os.Stat(filepath.Join(ws, "elsewhere.md")); !os.IsNotExist(err) {
		t.Errorf("elsewhere.md exists (stat err = %v); a refused wrap-up write must not land", err)
	}
	sub, ok := lastSubAgentResult(sink.events)
	want := childClosingReport + "\n" + fmt.Sprintf(missingOutputNoteFormat, "out/report.md")
	if !ok || sub.IsError || !strings.HasSuffix(sub.Content, want) {
		t.Errorf("sub_agent result = %+v, want the non-error capped result with the closing report and the missing-output note", sub)
	}
}

// TestWrapUpCallsKeepsTheFirstWriteWithoutAnOutputPath is the filter without an output path: of a
// wrap-up reply's calls — a read, then three write_file calls to three different paths — the
// filter keeps the FIRST write_file wherever it is aimed and drops every other call, the two later
// writes included: the clause promised one write, not one per path.
func TestWrapUpCallsKeepsTheFirstWriteWithoutAnOutputPath(t *testing.T) {
	ws := t.TempDir()
	cfg := subAgentConfig(&recordingSink{}, domain.ModeAllowEdits, tools.NewWriteFile(ws))
	cfg.WorkspaceDir = ws
	a, err := newAgent(cfg, &requestLogResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if _, ok := a.wrapUpWriter(); !ok {
		t.Fatal("wrapUpWriter = false, want write_file on the wrap-up menu without an output path")
	}
	writeCall := func(id, path string) domain.ToolCall {
		return domain.ToolCall{ID: id, Tool: tools.WriteFileToolName,
			Arguments: json.RawMessage(fmt.Sprintf(`{"path":%q,"content":%q}`, path, id))}
	}

	kept := a.wrapUpCalls([]domain.ToolCall{
		{ID: "read", Tool: "read_thing", Arguments: json.RawMessage(`{}`)},
		writeCall("first", "notes.md"),
		writeCall("second", "out/report.md"),
		writeCall("third", "notes.md"),
	})

	var ids []string
	for _, call := range kept {
		ids = append(ids, call.ID)
	}
	if want := []string{"first"}; !slices.Equal(ids, want) {
		t.Errorf("kept call ids = %v, want %v — the first write_file alone", ids, want)
	}
}

// TestSubAgent_WrapUpOffersOneWriteWithoutAnOutputPath drives the plain one-write wrap-up whole
// (plan 2026-09-18 - 00, item 5): the same workspace and the same write_file in the child's
// registry, a spawn that named no output path, and a child whose Mode admits the write
// (Allow-Edits — the one config this test needs, as the output_path runs above have) — the wrap-up
// menu is exactly write_file, the directive carries the plain one-write clause and not the
// output-path one, the FIRST write the reply asks for lands, and the second is dropped
// undispatched.
func TestSubAgent_WrapUpOffersOneWriteWithoutAnOutputPath(t *testing.T) {
	wrapUp := []provider.Delta{
		{Kind: provider.DeltaContent, Content: childClosingReport},
		{Kind: provider.DeltaToolCall, ToolCall: &provider.ToolCall{ID: "w0", Type: "function",
			Function: provider.FunctionCall{Name: tools.WriteFileToolName,
				Arguments: `{"path":"out/report.md","content":"the survey so far"}`}}},
		{Kind: provider.DeltaToolCall, ToolCall: &provider.ToolCall{ID: "w1", Type: "function",
			Function: provider.FunctionCall{Name: tools.WriteFileToolName,
				Arguments: `{"path":"notes.md","content":"a second file"}`}}},
		{Kind: provider.DeltaDone, FinishReason: "tool_calls"},
	}
	a, responder, sink, ws := outputPathAgent(t, domain.ModeAllowEdits, "", wrapUp)

	runExchange(t, a, "please research")

	req := wrapUpRequest(t, responder)
	if len(req.Tools) != 1 || req.Tools[0].Name != tools.WriteFileToolName {
		t.Fatalf("wrap-up menu = %+v, want exactly write_file without an output path", req.Tools)
	}
	tail := requestTail(t, req)
	if want := fmt.Sprintf(wrapUpDirectiveFormat, 2) + wrapUpWriteClause; tail.Role != string(domain.RoleTool) ||
		!strings.HasSuffix(tail.Content, domain.RenderEngineNote(wrapUpNoteTopic, want)) {
		t.Errorf("tail = %q (%s), want the closing tool result carrying the directive followed by the one-write clause %q as its engine note", tail.Content, tail.Role, want)
	}
	if strings.Contains(tail.Content, "write_file once, for ") {
		t.Errorf("tail = %q carries the output-path clause without an output path", tail.Content)
	}
	if got := requestSystemText(req); strings.Contains(got, wrapUpMarker) {
		t.Errorf("system text = %q carries the directive; with a tool-result tail it rides the tail, not the system prompt", got)
	}

	body, err := os.ReadFile(filepath.Join(ws, "out", "report.md"))
	if err != nil {
		t.Fatalf("the wrap-up write did not land: %v", err)
	}
	if string(body) != "the survey so far" {
		t.Errorf("output file = %q, want the FIRST write's content", body)
	}
	if _, err := os.Stat(filepath.Join(ws, "notes.md")); !os.IsNotExist(err) {
		t.Errorf("notes.md exists (stat err = %v); the second wrap-up write is dropped", err)
	}
	results := wrapUpWriteResults(sink.events, tools.WriteFileToolName)
	if len(results) != 1 || results[0].IsError {
		t.Errorf("write_file results = %+v, want exactly one non-error result — the second write is dropped", results)
	}
	sub, ok := lastSubAgentResult(sink.events)
	if !ok || sub.IsError || strings.Contains(sub.Content, "without writing") {
		t.Errorf("sub_agent result = %+v, want the plain non-error capped result — no output path, no missing-output note", sub)
	}
}

// TestSubAgent_PlanModeWrapUpOffersNoWriter pins the announced-then-refused guard: a Plan-mode
// child inherits Plan, whose ladder refuses a workspace write, so its wrap-up keeps no writer and
// says nothing about one whether or not the spawn named an output path — and with one named its
// result carries no missing-output note either (item 9): a file the ladder forbade is not the
// child's to have written. Both legs complete without error.
func TestSubAgent_PlanModeWrapUpOffersNoWriter(t *testing.T) {
	for _, outputPath := range []string{"out/report.md", ""} {
		t.Run("output_path="+outputPath, func(t *testing.T) {
			a, responder, sink, ws := outputPathAgent(t, domain.ModePlan, outputPath,
				narratedToolCallScript("w0", tools.WriteFileToolName,
					`{"path":"out/report.md","content":"the survey so far"}`, childClosingReport))

			runExchange(t, a, "please research")

			req := wrapUpRequest(t, responder)
			if len(req.Tools) != 0 {
				t.Errorf("wrap-up menu = %+v, want none in Plan mode", req.Tools)
			}
			if tail := requestTail(t, req); strings.Contains(tail.Content, "You may still call write_file") {
				t.Errorf("tail = %q carries a write clause for a Plan-mode child", tail.Content)
			}
			if results := wrapUpWriteResults(sink.events, tools.WriteFileToolName); len(results) != 0 {
				t.Errorf("write_file results = %+v, want none in Plan mode", results)
			}
			if _, err := os.Stat(filepath.Join(ws, "out", "report.md")); !os.IsNotExist(err) {
				t.Errorf("out/report.md exists (stat err = %v); Plan mode writes nothing", err)
			}
			sub, ok := lastSubAgentResult(sink.events)
			if !ok || sub.IsError || strings.Contains(sub.Content, "without writing") {
				t.Errorf("sub_agent result = %+v, want the plain non-error capped result — a withheld writer earns no missing-output note", sub)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// A child's result is validated: missing output, vendor markup, acknowledgements
// (plan 2026-09-14 - 03, item 9)
// ----------------------------------------------------------------------------

// TestSubAgent_AcknowledgementIsNoReport pins rule (c) on both sides of its closed set: each
// acknowledgement, case-insensitive with trailing punctuation, is answered with the no-report
// marker alone, while every other reply — "Yes." included — reaches the parent byte for byte.
func TestSubAgent_AcknowledgementIsNoReport(t *testing.T) {
	cases := []struct {
		answer string
		want   string
	}{
		{"Done.", noReportMarker},
		{"Understood", noReportMarker},
		{"ok", noReportMarker},
		{"Noted!", noReportMarker},
		{"  Acknowledged.  ", noReportMarker},
		{"Yes.", "Yes."},
		{"child done", "child done"},
		{"child one done", "child one done"},
		{"Done, I read the file.", "Done, I read the file."},
		{"OK — the repo has four packages.", "OK — the repo has four packages."},
	}
	for _, tc := range cases {
		t.Run(tc.answer, func(t *testing.T) {
			res := runSteeredDelegation(t, tc.answer)

			if res.IsError || res.Content != tc.want {
				t.Errorf("sub_agent result = %+v, want the non-error %q", res, tc.want)
			}
		})
	}
}

// TestSubAgent_MarkupReplyIsAFault pins rule (b): a closing text that is a tool call written in a
// vendor container — a <tool_call> pair with a JSON body, or a reply that begins with the DSML
// container — is an error result heading the text, while a report that merely quotes a tag pair
// in prose is the report it always was. The written call names a tool the child was never offered
// so the tool-call salvage Floor guard cannot read it back as a call first.
func TestSubAgent_MarkupReplyIsAFault(t *testing.T) {
	const jsonPair = `<tool_call>{"name": "shell", "arguments": {"cmd": "ls"}}</tool_call>`
	const dsml = "<｜DSML｜tool_calls><｜DSML｜invoke name=\"shell\"></｜DSML｜invoke></｜DSML｜tool_calls>"
	const quoted = "The child wrote `<tool_call>list the files</tool_call>` and then stopped."

	cases := []struct {
		name      string
		answer    string
		wantError bool
	}{
		{"a JSON-bodied tool_call pair", jsonPair, true},
		{"a JSON-bodied pair inside prose", "Next I would run " + jsonPair + " to list them.", true},
		{"a reply that begins with the DSML container", dsml, true},
		{"a report that quotes a tag pair in prose", quoted, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := runSteeredDelegation(t, tc.answer)

			want := tc.answer
			if tc.wantError {
				want = markupResultHead + "\n" + tc.answer
			}
			if res.IsError != tc.wantError || res.Content != want {
				t.Errorf("sub_agent result = %+v, want IsError %v with content %q", res, tc.wantError, want)
			}
		})
	}
}

// TestSubAgent_CappedChildWithoutItsOutputCarriesTheNote pins rule (a) on the capped path: a
// delegation spawned with an `output_path` whose wrap-up reply writes nothing keeps the partial
// marker first and its non-error shape, and the missing-output note is the body's last line —
// the SeatFallbackNote slot — beneath the closing report.
func TestSubAgent_CappedChildWithoutItsOutputCarriesTheNote(t *testing.T) {
	a, _, sink, ws := outputPathAgent(t, domain.ModeAllowEdits, "out/report.md", contentScript(childClosingReport))

	runExchange(t, a, "please research")

	if _, err := os.Stat(filepath.Join(ws, "out", "report.md")); !os.IsNotExist(err) {
		t.Fatalf("out/report.md exists (stat err = %v); the wrap-up reply wrote nothing", err)
	}
	sub, ok := lastSubAgentResult(sink.events)
	if !ok {
		t.Fatal("no sub_agent tool result emitted")
	}
	want := cappedResult(fmt.Sprintf(stepCapResultFormat, 2), childFoldSummary, childClosingReport) + "\n" + fmt.Sprintf(missingOutputNoteFormat, "out/report.md")
	if sub.IsError || sub.Content != want {
		t.Errorf("sub_agent result = %+v, want the non-error capped result %q", sub, want)
	}
}

// draftOutputPath is the `output_path` the faulted-draft tests spawn their delegation with, in the
// call's own spelling — the spelling the draft note must quote back.
const draftOutputPath = "out/report.md"

// faultedOutputPathAgent builds a parent in Allow-Edits over a real workspace with a real
// write_file tool, whose one NAMED delegation names draftOutputPath and whose child plays the
// given scripts — its working Turns, the fault and, where it completed a Turn, the engine fold —
// before the parent closes. It returns the parent, the sink and the workspace root.
func faultedOutputPathAgent(t *testing.T, child ...[]provider.Delta) (*Agent, *recordingSink, string) {
	t.Helper()

	ws := t.TempDir()
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	cfg := subAgentConfig(sink, domain.ModeAllowEdits, reader, tools.NewWriteFile(ws))
	cfg.WorkspaceDir = ws

	call, err := json.Marshal(tools.SubAgentArgs{Task: retainedSurveyTask, Name: retainedSurveyName, OutputPath: draftOutputPath})
	if err != nil {
		t.Fatalf("marshal sub_agent args: %v", err)
	}
	scripts := [][]provider.Delta{toolCallScript("c1", tools.SubAgentToolName, string(call))}
	scripts = append(scripts, child...)
	scripts = append(scripts, contentScript("parent done"))

	a, err := newAgent(cfg, &requestLogResponder{scripts: scripts})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	return a, sink, ws
}

// childFault is the non-retryable upstream fault that abandons a child's Exchange mid-task.
func childFault() []provider.Delta {
	return []provider.Delta{{Kind: provider.DeltaError, Err: "the upstream died"}}
}

// TestSubAgent_FaultResultNamesTheSurvivingDraft drives the draft note whole: a child that writes
// its `output_path` through the real write_file tool and then faults hands the parent an error
// result that still names the fault first, and whose body notes end on the draft note — in the
// call's spelling of the path — followed by the continue line.
func TestSubAgent_FaultResultNamesTheSurvivingDraft(t *testing.T) {
	a, sink, ws := faultedOutputPathAgent(t,
		narratedToolCallScript("w0", tools.WriteFileToolName,
			`{"path":"out/report.md","content":"the survey so far"}`, "writing the draft"),
		childFault(),
		contentScript(childFoldSummary))

	runExchange(t, a, "please research")

	if _, err := os.Stat(filepath.Join(ws, "out", "report.md")); err != nil {
		t.Fatalf("the child's write did not land: %v", err)
	}
	faulted, ok := subAgentResultFor(sink.events, "c1")
	if !ok {
		t.Fatal("no result answered the faulted delegation c1")
	}
	assertFaultedResultContinuable(t, faulted, retainedSurveyName)
	want := "\n" + fmt.Sprintf(draftOutputNoteFormat, draftOutputPath) + "\n" + fmt.Sprintf(continueLineFormat, retainedSurveyName)
	if !strings.HasSuffix(faulted.Content, want) {
		t.Errorf("faulted result =\n%s\nwant its body notes to end on the draft note then the continue line %q", faulted.Content, want)
	}
}

// TestSubAgent_FaultResultWithoutADraftHasNoNote is the control: the same fault after a Turn
// that wrote nothing carries no draft note, only the fault and the continue line.
func TestSubAgent_FaultResultWithoutADraftHasNoNote(t *testing.T) {
	a, sink, _ := faultedOutputPathAgent(t,
		narratedToolCallScript("t0", "read_thing", `{"n":0}`, "reading file 0"),
		childFault(),
		contentScript(childFoldSummary))

	runExchange(t, a, "please research")

	faulted, ok := subAgentResultFor(sink.events, "c1")
	if !ok {
		t.Fatal("no result answered the faulted delegation c1")
	}
	assertFaultedResultContinuable(t, faulted, retainedSurveyName)
	if strings.Contains(faulted.Content, "draft output") {
		t.Errorf("faulted result =\n%s\ncarries a draft note, but the child wrote nothing", faulted.Content)
	}
}

// TestSubAgent_FaultResultIgnoresAPreExistingFile pins the baseline: a file that was already at
// the `output_path` before the spawn is not a draft of the child's, so a child that faults on its
// first request — having written nothing — earns no draft note although the file is there.
func TestSubAgent_FaultResultIgnoresAPreExistingFile(t *testing.T) {
	a, sink, ws := faultedOutputPathAgent(t, childFault()) // zero Turns: no fold request follows
	target := filepath.Join(ws, "out", "report.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("left here before the spawn"), 0o644); err != nil {
		t.Fatal(err)
	}

	runExchange(t, a, "please research")

	faulted, ok := subAgentResultFor(sink.events, "c1")
	if !ok {
		t.Fatal("no result answered the faulted delegation c1")
	}
	assertFaultedResultContinuable(t, faulted, retainedSurveyName)
	if strings.Contains(faulted.Content, "draft output") {
		t.Errorf("faulted result =\n%s\ncarries a draft note for a file that predates the spawn", faulted.Content)
	}
}

// TestSubAgent_CappedResultKeepsTheMissingOutputNote pins the cap path against the draft note: a
// capped child's result carries the missing-output note when its file is absent and no note at
// all when the wrap-up wrote it — the draft note is the fault result's alone.
func TestSubAgent_CappedResultKeepsTheMissingOutputNote(t *testing.T) {
	head := fmt.Sprintf(stepCapResultFormat, 2)
	cases := []struct {
		name   string
		wrapUp []provider.Delta
		want   string
	}{
		{
			name:   "the output is absent",
			wrapUp: contentScript(childClosingReport),
			want:   cappedResult(head, childFoldSummary, childClosingReport) + "\n" + fmt.Sprintf(missingOutputNoteFormat, draftOutputPath),
		},
		{
			name: "the output was written",
			wrapUp: narratedToolCallScript("w0", tools.WriteFileToolName,
				`{"path":"out/report.md","content":"the survey so far"}`, childClosingReport),
			want: cappedResult(head, childFoldSummary, childClosingReport),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, _, sink, _ := outputPathAgent(t, domain.ModeAllowEdits, draftOutputPath, tc.wrapUp)

			runExchange(t, a, "please research")

			sub, ok := lastSubAgentResult(sink.events)
			if !ok {
				t.Fatal("no sub_agent tool result emitted")
			}
			if sub.IsError || sub.Content != tc.want {
				t.Errorf("sub_agent result = %+v, want the non-error capped result %q", sub, tc.want)
			}
		})
	}
}

// completedOutputPathAgent builds a parent in Allow-Edits over a real workspace with a real
// write_file tool, whose one delegation names out/report.md and whose child runs to COMPLETION
// (no cap) through the given scripts before the parent closes. It returns the parent, the sink
// and the workspace root.
func completedOutputPathAgent(t *testing.T, child ...stubllm.Turn) (*Agent, *recordingSink, string) {
	t.Helper()

	ws := t.TempDir()
	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAllowEdits, tools.NewWriteFile(ws))
	cfg.WorkspaceDir = ws

	call, err := json.Marshal(tools.SubAgentArgs{Task: "trawl the repo", OutputPath: "out/report.md"})
	if err != nil {
		t.Fatalf("marshal sub_agent args: %v", err)
	}
	scripts := []stubllm.Turn{toolCallTurn("c1", tools.SubAgentToolName, string(call))}
	scripts = append(scripts, child...)
	scripts = append(scripts, contentTurn("parent done"))

	a, err := newAgent(cfg, scriptedResponder(t, scripts...))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	return a, sink, ws
}

// TestSubAgent_CompletedChildWithoutItsOutputIsAFault pins rule (a) on the completed path: a child
// that answers without ever writing the file its spawn named gets an error result heading its
// text with the missing-output line, and the control — the same child writing the file first —
// reports byte for byte.
func TestSubAgent_CompletedChildWithoutItsOutputIsAFault(t *testing.T) {
	const answer = "I surveyed the repo; the findings are in the report."

	t.Run("the output is absent", func(t *testing.T) {
		a, sink, _ := completedOutputPathAgent(t, contentTurn(answer))

		runExchange(t, a, "please research")

		sub, ok := lastSubAgentResult(sink.events)
		want := fmt.Sprintf(missingOutputResultFormat, "out/report.md") + "\n" + answer
		if !ok || !sub.IsError || sub.Content != want {
			t.Errorf("sub_agent result = %+v, want the error result %q", sub, want)
		}
	})

	t.Run("the output was written", func(t *testing.T) {
		a, sink, ws := completedOutputPathAgent(t,
			narratedToolCallTurn("w0", tools.WriteFileToolName, `{"path":"out/report.md","content":"the survey"}`, "writing"),
			contentTurn(answer))

		runExchange(t, a, "please research")

		if _, err := os.Stat(filepath.Join(ws, "out", "report.md")); err != nil {
			t.Fatalf("the child's write did not land: %v", err)
		}
		sub, ok := lastSubAgentResult(sink.events)
		if !ok || sub.IsError || sub.Content != answer {
			t.Errorf("sub_agent result = %+v, want the child's answer %q alone", sub, answer)
		}
	})
}

// ----------------------------------------------------------------------------
// The tool-less wrap-up Turn (turnLifecycle.wrapUp)
// ----------------------------------------------------------------------------
//
// These tests set the latch BY HAND — nothing in the engine writes it yet — because the three
// seams it moves (toolMenu, buildRequest, step) are its whole observable contract: one request
// with no tools and a directive saying why, and a reply that ends the Exchange whatever it asks
// for. Their child holds no write_file, so the menu is withdrawn wholesale; the one-write
// exception has its own block above.

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
	if latched {
		a.turns.capped()
	}
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
// zero tools on the wire and the directive with the cap's own number, in a session that configured
// no system prompt at all. This run Submits, so its one request ends on the USER message — a tail
// that takes no engine note — which makes it the SYSTEM-FALLBACK pin: AppendToSystem creates the
// system message for the directive. The tool-result-tail placement is pinned on a real capped run
// (TestWrapUpDirectiveRidesTheClosingToolResult).
func TestWrapUpRequestWithdrawsToolsAndSaysWhy(t *testing.T) {
	a, responder, _, _ := wrapUpAgent(t, true, contentScript("here is what I found"))

	runWrapUpAgent(t, a)

	if len(responder.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(responder.requests))
	}
	req := responder.requests[0]
	if len(req.Tools) != 0 {
		t.Errorf("Tools = %d, want 0 — a latched Turn withdraws the whole menu of a child with no write_file", len(req.Tools))
	}
	want := fmt.Sprintf(wrapUpDirectiveFormat, 3)
	if got := requestSystemText(req); !strings.Contains(got, want) {
		t.Errorf("system text = %q, want it to contain the wrap-up directive %q", got, want)
	}
	if tail := requestTail(t, req); tail.Role != string(domain.RoleUser) || strings.Contains(tail.Content, domain.EngineNoteFencePrefix) {
		t.Errorf("tail = %q (%s), want the user message untouched — a user tail takes no engine note", tail.Content, tail.Role)
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

// TestWrapUpWithBlankTextCommitsNothing is the whitespace sibling: a wrap-up reply whose text is
// nothing but spaces, and which asks for a tool anyway, is as empty as no text at all. The latched
// exit measures emptiness the way replyFault does (strings.TrimSpace), so a blank final message
// never becomes the child's last visible text and never buries the partial result. It also pins
// the boundary finishAtStepCap relies on: the latched exit ends the EXCHANGE, never a bare Turn.
func TestWrapUpWithBlankTextCommitsNothing(t *testing.T) {
	a, _, sink, ran := wrapUpAgent(t, true,
		narratedToolCallScript("t0", "read_thing", `{}`, "  \n\t "))

	res := runWrapUpAgent(t, a)

	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("Status = %q, want %q — the latched exit always closes the Exchange", res.Status, domain.StatusExchangeComplete)
	}
	if *ran != 0 {
		t.Errorf("tool ran %d times, want 0 — a withdrawn menu must not be reachable", *ran)
	}
	if got := a.lastVisibleText(); got != "" {
		t.Errorf("lastVisibleText = %q, want empty — a whitespace-only wrap-up commits nothing", got)
	}
	if got := len(assistantMessages(a)); got != 0 {
		t.Errorf("assistant messages = %d, want 0", got)
	}
	if hasEvent[domain.MessageEvent](sink.events) {
		t.Error("MessageEvent emitted for a whitespace-only wrap-up reply")
	}
}

// ----------------------------------------------------------------------------
// Retention of a capped delegation (plan 2026-09-18 - 00, item 8 / P6a)
// ----------------------------------------------------------------------------
//
// A child the engine stopped at a bound is kept by its parent — task, roster, output path, fold,
// closing text and the bound — under the name it ended its run wearing, for the rest of the
// parent's Exchange. These tests drive runSubAgent and read the retained set through the seam a
// continuation will read it by (Agent.retained).

// retainedSurveyTask and retainedSurveyName are the delegation the retention tests spawn.
const (
	retainedSurveyTask = "trawl the repo"
	retainedSurveyName = "Repo Survey"
)

// cappedSurveyArgs is the sub_agent payload of a delegation that names everything a continuation
// needs to carry over: a name, a roster and an output path beside the task.
func cappedSurveyArgs(task, name string) string {
	b, _ := json.Marshal(tools.SubAgentArgs{
		Task:       task,
		Name:       name,
		Tools:      tools.SubAgentRoster{Names: []string{"read_thing"}},
		OutputPath: "notes/survey.md",
	})
	return string(b)
}

// cappedSurveyScripts is the upstream script of one delegation capped at three steps: the spawning
// call, three working Turns, the engine fold, the wrap-up — the parent's own closing reply is the
// caller's to append.
func cappedSurveyScripts(callID, task, name string) [][]provider.Delta {
	scripts := [][]provider.Delta{toolCallScript(callID, tools.SubAgentToolName, cappedSurveyArgs(task, name))}
	scripts = append(scripts, cappedChildTurns(3)...)
	scripts = append(scripts, foldScript(700, 40))
	return append(scripts, contentScript(childClosingReport))
}

// runCappedSurveyParent builds a parent at a three-step delegate cap over scripts, runs it to its
// boundary and fails on anything but a clean parent Exchange.
func runCappedSurveyParent(t *testing.T, cfg domain.Config, responder provider.Responder) *Agent {
	t.Helper()
	cfg.Delegation.MaxSteps = 3
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
	if res.Status != domain.StatusExchangeComplete || res.Faulted || res.StepCapped {
		t.Fatalf("parent result = %+v, want a clean uncapped exchange-complete", res)
	}
	return a
}

// TestSubAgent_CappedChildIsRetainedWithItsFold pins what the parent keeps of a capped delegation:
// every field a continuation is spawned from, copied from the call and the child's capped result,
// under the name the call gave it.
func TestSubAgent_CappedChildIsRetainedWithItsFold(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	scripts := append(cappedSurveyScripts("c1", retainedSurveyTask, retainedSurveyName), contentScript("parent done"))

	a := runCappedSurveyParent(t, subAgentConfig(sink, domain.ModeAskBefore, reader), &requestLogResponder{scripts: scripts})

	got, ok := a.retained.lookup(retainedSurveyName)
	if !ok {
		t.Fatalf("no delegation retained under %q; retained names = %v", retainedSurveyName, a.retained.names())
	}
	want := retainedDelegate{
		task:          retainedSurveyTask,
		name:          retainedSurveyName,
		tools:         tools.SubAgentRoster{Names: []string{"read_thing"}},
		outputPath:    "notes/survey.md",
		fold:          childFoldSummary,
		closingReport: childClosingReport,
		bound:         boundSteps,
		spawnCallID:   "c1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("retained delegate = %+v, want %+v", got, want)
	}
	if names := a.retained.names(); !slices.Equal(names, []string{retainedSurveyName}) {
		t.Errorf("retained names = %v, want exactly %q", names, retainedSurveyName)
	}
}

// TestSubAgent_CompletedChildIsNotRetained is the floor: a delegation that ran to completion has
// nothing to continue from, so the parent keeps nothing of it.
func TestSubAgent_CompletedChildIsNotRetained(t *testing.T) {
	sink := &recordingSink{}
	scripts := [][]provider.Delta{
		toolCallScript("c1", tools.SubAgentToolName, cappedSurveyArgs(retainedSurveyTask, retainedSurveyName)),
		contentScript("the survey is complete"),
		contentScript("parent done"),
	}

	a := runCappedSurveyParent(t, subAgentConfig(sink, domain.ModeAskBefore), &requestLogResponder{scripts: scripts})

	if names := a.retained.names(); len(names) != 0 {
		t.Errorf("retained names = %v after a completed delegation, want none", names)
	}
}

// TestSubAgent_LatestCappedChildIsRetainedUnderTheSharedName pins the replacement rule: two capped delegations the
// parent named alike are two attempts at one piece of work, and the later one is what a
// continuation picks up from.
func TestSubAgent_LatestCappedChildIsRetainedUnderTheSharedName(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	scripts := cappedSurveyScripts("c1", "survey part one", retainedSurveyName)
	scripts = append(scripts, cappedSurveyScripts("c2", "survey part two", retainedSurveyName)...)
	scripts = append(scripts, contentScript("parent done"))

	a := runCappedSurveyParent(t, subAgentConfig(sink, domain.ModeAskBefore, reader), &requestLogResponder{scripts: scripts})

	got, ok := a.retained.lookup(retainedSurveyName)
	if !ok {
		t.Fatalf("no delegation retained under %q", retainedSurveyName)
	}
	if got.spawnCallID != "c2" || got.task != "survey part two" {
		t.Errorf("retained under %q = call %q task %q, want the latest capped child c2 / %q",
			retainedSurveyName, got.spawnCallID, got.task, "survey part two")
	}
	if names := a.retained.names(); len(names) != 1 {
		t.Errorf("retained names = %v, want the one name both children shared", names)
	}
}

// TestSubAgent_RetainedChildIsForgottenAsTheNextExchangeOpens pins the lifetime: a capped delegation is held for the
// rest of the Exchange that spawned it and forgotten as the next one opens.
func TestSubAgent_RetainedChildIsForgottenAsTheNextExchangeOpens(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	scripts := append(cappedSurveyScripts("c1", retainedSurveyTask, retainedSurveyName), contentScript("parent done"))
	scripts = append(scripts, contentScript("second done"))

	a := runCappedSurveyParent(t, subAgentConfig(sink, domain.ModeAskBefore, reader), &requestLogResponder{scripts: scripts})
	if _, ok := a.retained.lookup(retainedSurveyName); !ok {
		t.Fatalf("the capped delegation is not retained at the end of its own Exchange")
	}

	if err := a.Submit(domain.UserInput{Text: "something else"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if names := a.retained.names(); len(names) != 0 {
		t.Errorf("retained names = %v after a new Exchange opened, want none", names)
	}
}

// gatedNamer is a DelegationNamer that answers only once its gate opens — the namer whose reply
// arrives late in the run, after the child has already hit its bound — and gives up on its
// context like a real host would.
type gatedNamer struct {
	open  chan struct{}
	reply string
}

func (n gatedNamer) NameDelegation(ctx context.Context, _ domain.DelegationNaming) (string, error) {
	select {
	case <-n.open:
		return n.reply, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// TestSubAgent_ANamerThatAnswersAfterTheCapStillNamesTheRetainedChild pins the join: a delegation
// the model left unnamed is named out of band (ADR 0068), and a name that lands after the cap but
// before the run ends is the name the parent retains it under — the namer is joined before the
// retention reads the child's name, so the entry never wears a stale empty name while the parent
// model has been told the generated one.
func TestSubAgent_ANamerThatAnswersAfterTheCapStillNamesTheRetainedChild(t *testing.T) {
	sink := newLockedSink()
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	capped := make(chan struct{})
	cfg := subAgentConfig(sink, domain.ModeAskBefore, reader)
	cfg.Namer = gatedNamer{open: capped, reply: retainedSurveyName}
	scripts := append(cappedSurveyScripts("c1", retainedSurveyTask, ""), contentScript("parent done"))
	responder := &requestLogResponder{scripts: scripts}
	// Call 4 is the engine fold — the bound has been hit — which is when the namer is let answer;
	// call 5 is the wrap-up, held until the rename has landed so the run ends wearing the name.
	responder.before = func(call int) {
		switch call {
		case 4:
			close(capped)
		case 5:
			sink.awaitRename()
		}
	}

	a := runCappedSurveyParent(t, cfg, responder)

	if named := sink.namings(); len(named) != 1 || named[0].Name != retainedSurveyName {
		t.Fatalf("SubAgentNamedEvents = %+v, want the one rename to %q", named, retainedSurveyName)
	}
	got, ok := a.retained.lookup(retainedSurveyName)
	if !ok {
		t.Fatalf("no delegation retained under the generated name %q; retained names = %v", retainedSurveyName, a.retained.names())
	}
	if got.task != retainedSurveyTask || got.fold != childFoldSummary {
		t.Errorf("retained delegate = %+v, want the capped child's task and fold", got)
	}
}

// taskRoutedResponder answers each request from the queue registered for the key its last user
// message equals or CONTAINS — the engine fold's request carries the child's transcript as its user
// message, so a capped child's fold is routed to the same queue as its Turns. It is the fan-out
// twin of routedResponder for children that hit a bound.
type taskRoutedResponder struct {
	mu     sync.Mutex
	keys   []string
	routes map[string][][]provider.Delta
}

func newTaskRoutedResponder() *taskRoutedResponder {
	return &taskRoutedResponder{routes: map[string][][]provider.Delta{}}
}

// route appends the scripted Turns for the agent whose last user message is, or contains, key.
func (r *taskRoutedResponder) route(key string, scripts ...[]provider.Delta) *taskRoutedResponder {
	if _, seen := r.routes[key]; !seen {
		r.keys = append(r.keys, key)
	}
	r.routes[key] = append(r.routes[key], scripts...)
	return r
}

func (r *taskRoutedResponder) Stream(_ context.Context, req provider.Request) iter.Seq[provider.Delta] {
	asker := lastUserText(req)
	r.mu.Lock()
	script := []provider.Delta{{Kind: provider.DeltaError, Err: "taskRoutedResponder: no script for " + asker}}
	for _, key := range r.keys {
		if asker != key && !strings.Contains(asker, key) {
			continue
		}
		if queue := r.routes[key]; len(queue) > 0 {
			script, r.routes[key] = queue[0], queue[1:]
		}
		break
	}
	r.mu.Unlock()
	return func(yield func(provider.Delta) bool) {
		for _, d := range script {
			if !yield(d) {
				return
			}
		}
	}
}

// TestFanOut_CappedChildrenAreRetainedFromThePool drives two capped delegations through the depth-0
// pool at once (ADR 0039): both are retained under their own names, from two pool workers writing
// the parent's set concurrently — which -race judges.
func TestFanOut_CappedChildrenAreRetainedFromThePool(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, reader)
	cfg.ParallelAgents = 2
	cfg.Delegation.MaxSteps = 2
	spawn := []provider.Delta{
		{Kind: provider.DeltaToolCall, ToolCall: &provider.ToolCall{
			ID: "c1", Type: "function",
			Function: provider.FunctionCall{Name: tools.SubAgentToolName, Arguments: subAgentNamedArgs("task one", "Alpha")},
		}},
		{Kind: provider.DeltaToolCall, ToolCall: &provider.ToolCall{
			ID: "c2", Type: "function",
			Function: provider.FunctionCall{Name: tools.SubAgentToolName, Arguments: subAgentNamedArgs("task two", "Beta")},
		}},
		{Kind: provider.DeltaDone, FinishReason: "tool_calls"},
	}
	childScripts := append(cappedChildTurns(2), foldScript(700, 40), contentScript(childClosingReport))
	up := newTaskRoutedResponder().
		route("delegate two things", spawn, contentScript("parent done")).
		route("task one", childScripts...).
		route("task two", childScripts...)

	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "delegate two things"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete || res.Faulted {
		t.Fatalf("parent result = %+v, want a clean exchange-complete", res)
	}

	if names := a.retained.names(); !slices.Equal(names, []string{"Alpha", "Beta"}) {
		t.Fatalf("retained names = %v, want both capped children, Alpha and Beta", names)
	}
	for name, want := range map[string]string{"Alpha": "c1", "Beta": "c2"} {
		if got, _ := a.retained.lookup(name); got.spawnCallID != want || got.fold != childFoldSummary {
			t.Errorf("retained %q = %+v, want call %q with the engine fold", name, got, want)
		}
	}
}

// ----------------------------------------------------------------------------
// Continuing a capped delegation (plan 2026-09-18 - 00, item 9 / P6b)
// ----------------------------------------------------------------------------
//
// `sub_agent` with `continue: "<name>"` spawns a FRESH child from the retained entry: its opening
// task is the retained task, the engine fold of the capped attempt and the call's own task as the
// continuation instructions; name, roster and output path are inherited where the call leaves them
// unset; the entry is consumed; and the capped result itself carries the line that tells the
// parent model the handle to spell back.

// continueInstructions is what the parent asks the continued child to do next.
const continueInstructions = "read the remaining file and finish the survey"

// continueArgs is the sub_agent payload of a continuation that sets nothing but the handle and the
// instructions — the shape that inherits everything else from the retained entry.
func continueArgs(name, instructions string, maxSteps int) string {
	b, _ := json.Marshal(tools.SubAgentArgs{Task: instructions, Continue: name, MaxSteps: maxSteps})
	return string(b)
}

// subAgentResultFor returns the ToolResultEvent answering the sub_agent call callID, at any depth.
func subAgentResultFor(events []domain.Event, callID string) (domain.ToolResult, bool) {
	for _, e := range events {
		if ev, ok := e.(domain.ToolResultEvent); ok && ev.Result.CallID == callID {
			return ev.Result, true
		}
	}
	return domain.ToolResult{}, false
}

// namingsOf returns every SubAgentNamedEvent on the sink, in emission order.
func namingsOf(events []domain.Event) []domain.SubAgentNamedEvent {
	var named []domain.SubAgentNamedEvent
	for _, e := range events {
		if ne, ok := e.(domain.SubAgentNamedEvent); ok {
			named = append(named, ne)
		}
	}
	return named
}

// requestToolNames returns the names of the tools req offered, in menu order.
func requestToolNames(req provider.Request) []string {
	names := make([]string, 0, len(req.Tools))
	for _, tool := range req.Tools {
		names = append(names, tool.Name)
	}
	return names
}

// TestSubAgent_ContinueSpawnsAChildFromTheFoldWithAFreshCap drives cap → continue → completion:
// the continued child's first request opens on the retained task, the fold and the instructions;
// the call that named no `name` inherits the retained one and announces it for the new spawn id;
// the roster inherited from the entry narrows the child's menu; the entry is consumed; and the
// child, on a fresh cap, runs to completion where the first attempt could not.
func TestSubAgent_ContinueSpawnsAChildFromTheFoldWithAFreshCap(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	scripts := cappedSurveyScripts("c1", retainedSurveyTask, retainedSurveyName)
	scripts = append(scripts, toolCallScript("c2", tools.SubAgentToolName, continueArgs(retainedSurveyName, continueInstructions, 0)))
	scripts = append(scripts, cappedChildTurns(2)...)
	scripts = append(scripts, contentScript("the survey is now complete"), contentScript("parent done"))
	responder := &requestLogResponder{scripts: scripts}

	a := runCappedSurveyParent(t, subAgentConfig(sink, domain.ModeAskBefore, reader), responder)

	// The capped result told the parent the handle, as the last body note.
	capped, ok := subAgentResultFor(sink.events, "c1")
	if !ok {
		t.Fatal("no result answered the capped delegation c1")
	}
	wantCapped := cappedResult(fmt.Sprintf(stepCapResultFormat, 3), childFoldSummary, childClosingReport) +
		"\n" + fmt.Sprintf(continueLineFormat, retainedSurveyName)
	if capped.Content != wantCapped {
		t.Errorf("capped result =\n%s\nwant\n%s", capped.Content, wantCapped)
	}
	// The continued child's opening request: call 6 (0: spawn, 1–3: turns, 4: fold, 5: wrap-up,
	// 6: the continue call, 7: the child's first Turn).
	opening := responder.requests[7]
	wantTask := retainedSurveyTask + "\n\n" + previousAttemptHead + "\n" + childFoldSummary + "\n\n" + continuationInstructionsHead + "\n" + continueInstructions
	if got := lastUserText(opening); got != wantTask {
		t.Errorf("the continued child opened on\n%s\nwant\n%s", got, wantTask)
	}
	if got := requestToolNames(opening); !slices.Equal(got, []string{"read_thing"}) {
		t.Errorf("the continued child's menu = %v, want the inherited roster [read_thing]", got)
	}
	if named := namingsOf(sink.events); len(named) != 1 || named[0].CallID != "c2" || named[0].Name != retainedSurveyName || named[0].Depth != 1 {
		t.Errorf("SubAgentNamedEvents = %+v, want one announcing %q for the new spawn c2 at depth 1", named, retainedSurveyName)
	}
	completed, ok := subAgentResultFor(sink.events, "c2")
	if !ok || completed.IsError || completed.Content != "the survey is now complete" {
		t.Errorf("the continued child's result = %+v, want its completed report on a fresh cap", completed)
	}
	if names := a.retained.names(); len(names) != 0 {
		t.Errorf("retained names = %v after the continuation completed, want none — the entry is consumed", names)
	}
}

// TestSubAgent_ContinueOfAnUnknownNameIsRefused pins the refusal, exact text: the asked name and
// the names retained — or "none" — because the list is the only correction the model can act on.
func TestSubAgent_ContinueOfAnUnknownNameIsRefused(t *testing.T) {
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	cases := []struct {
		name    string
		scripts [][]provider.Delta
		want    string
	}{
		{
			name: "one retained",
			scripts: append(cappedSurveyScripts("c1", retainedSurveyTask, retainedSurveyName),
				toolCallScript("c2", tools.SubAgentToolName, continueArgs("Nope", continueInstructions, 0)),
				contentScript("parent done")),
			want: `[no delegate named "Nope" to continue — retained: Repo Survey]`,
		},
		{
			name: "nothing retained",
			scripts: [][]provider.Delta{
				toolCallScript("c2", tools.SubAgentToolName, continueArgs("Nope", continueInstructions, 0)),
				contentScript("parent done"),
			},
			want: `[no delegate named "Nope" to continue — retained: none]`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}

			runCappedSurveyParent(t, subAgentConfig(sink, domain.ModeAskBefore, reader), &requestLogResponder{scripts: tc.scripts})

			got, ok := subAgentResultFor(sink.events, "c2")
			if !ok || !got.IsError || got.Content != tc.want {
				t.Errorf("continue result = %+v, want the error result %q", got, tc.want)
			}
		})
	}
}

// refusedContinueArgs is the sub_agent payload of a continuation the engine refuses on the call
// alone — a `run_on` that names no seat, or a `tools` roster naming a tool the parent's menu lacks
// — marshalled from the same struct continueArgs uses, so the refusal is provoked by the exact
// argument the model would spell.
func refusedContinueArgs(name, instructions, runOn string, roster tools.SubAgentRoster) string {
	b, _ := json.Marshal(tools.SubAgentArgs{Task: instructions, Continue: name, RunOn: runOn, Tools: roster})
	return string(b)
}

// seatChoiceSubAgentConfig is subAgentConfig with the sub_agent tool that PUBLISHES `run_on`
// (seatChoiceRegistry's idiom): the only parent whose seat refusal fires at all — the plain tool
// ignores an unpublished run_on rather than refusing it.
func seatChoiceSubAgentConfig(t *testing.T, sink domain.EventSink, extra ...domain.Tool) domain.Config {
	t.Helper()
	cfg := baseConfig(sink)
	cfg.Mode = domain.ModeAskBefore
	reg := domain.NewToolRegistry()
	if err := reg.Register(tools.NewSubAgentWith(tools.SubAgentOptions{SeatChoice: true})); err != nil {
		t.Fatalf("register the seat-choice sub_agent: %v", err)
	}
	for _, tool := range extra {
		if err := reg.Register(tool); err != nil {
			t.Fatalf("register %q: %v", tool.Name(), err)
		}
	}
	cfg.Tools = reg
	return cfg
}

// TestSubAgent_ARefusedContinueKeepsTheRetainedDelegate is apogee-if9: a continuation the engine
// refuses on the call alone — an invalid `run_on`, an unknown tool name — costs no fold. The entry
// stays retained with its fold after the refusal, and a corrected continue spawns from it where the
// pre-fix tree refused it as unknown.
func TestSubAgent_ARefusedContinueKeepsTheRetainedDelegate(t *testing.T) {
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	cases := []struct {
		name    string
		config  func(sink domain.EventSink) domain.Config
		refused string
		want    string
	}{
		{
			name:    "invalid run_on",
			config:  func(sink domain.EventSink) domain.Config { return seatChoiceSubAgentConfig(t, sink, reader) },
			refused: refusedContinueArgs(retainedSurveyName, continueInstructions, "banana", tools.SubAgentRoster{}),
			want:    `invalid run_on "banana": want "session" or "sub-agents-server"`,
		},
		{
			name:    "unknown tool",
			config:  func(sink domain.EventSink) domain.Config { return subAgentConfig(sink, domain.ModeAskBefore, reader) },
			refused: refusedContinueArgs(retainedSurveyName, continueInstructions, "", tools.SubAgentRoster{Names: []string{"no_such_tool"}}),
			want:    "sub_agent tools: unknown tool no_such_tool — name tools from your own menu",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The refusal alone: the parent closes its Exchange right after it, so the retained
			// set can be read as the refusal left it.
			sink := &recordingSink{}
			scripts := cappedSurveyScripts("c1", retainedSurveyTask, retainedSurveyName)
			scripts = append(scripts, toolCallScript("c2", tools.SubAgentToolName, tc.refused), contentScript("parent done"))

			a := runCappedSurveyParent(t, tc.config(sink), &requestLogResponder{scripts: scripts})

			got, ok := subAgentResultFor(sink.events, "c2")
			if !ok || !got.IsError || got.Content != tc.want {
				t.Fatalf("refused continue result = %+v, want the error result %q", got, tc.want)
			}
			kept, ok := a.retained.lookup(retainedSurveyName)
			if !ok {
				t.Fatalf("the refusal forgot the delegate: nothing retained under %q; retained names = %v", retainedSurveyName, a.retained.names())
			}
			if kept.fold != childFoldSummary || kept.spawnCallID != "c1" {
				t.Errorf("retained delegate after the refusal = %+v, want the capped entry with fold %q from c1", kept, childFoldSummary)
			}

			// The corrected retry, in the same Exchange as the refusal: it spawns from the fold and
			// runs to completion on a fresh cap.
			sink = &recordingSink{}
			scripts = cappedSurveyScripts("c1", retainedSurveyTask, retainedSurveyName)
			scripts = append(scripts, toolCallScript("c2", tools.SubAgentToolName, tc.refused))
			scripts = append(scripts, toolCallScript("c3", tools.SubAgentToolName, continueArgs(retainedSurveyName, continueInstructions, 0)))
			scripts = append(scripts, cappedChildTurns(2)...)
			scripts = append(scripts, contentScript("the survey is now complete"), contentScript("parent done"))
			responder := &requestLogResponder{scripts: scripts}

			a = runCappedSurveyParent(t, tc.config(sink), responder)

			completed, ok := subAgentResultFor(sink.events, "c3")
			if !ok || completed.IsError || completed.Content != "the survey is now complete" {
				t.Errorf("the corrected continue's result = %+v, want the child's completed report", completed)
			}
			// Request 8 is the continued child's opening (0: spawn, 1–3: turns, 4: fold, 5: wrap-up,
			// 6: the refused continue, 7: the corrected continue, 8: the child's first Turn).
			wantTask := retainedSurveyTask + "\n\n" + previousAttemptHead + "\n" + childFoldSummary + "\n\n" + continuationInstructionsHead + "\n" + continueInstructions
			if len(responder.requests) < 9 {
				t.Fatalf("%d upstream requests, want the corrected continue to have spawned a child (9+)", len(responder.requests))
			}
			if got := lastUserText(responder.requests[8]); got != wantTask {
				t.Errorf("the continued child opened on\n%s\nwant\n%s", got, wantTask)
			}
			if names := a.retained.names(); len(names) != 0 {
				t.Errorf("retained names = %v after the corrected continue completed, want none — the entry is consumed", names)
			}
		})
	}
}

// TestSubAgent_AContinuedChildThatCapsAgainIsRetainedAnew pins the second cap: the continued child
// is retained under the inherited name, with the ORIGINAL task (never the composed one, so a third
// attempt composes over one fold), the inherited roster and output path, and the new spawn id; its
// result carries the clamp note — max_steps is clamped exactly as on a fresh spawn — and then the
// continue line, in that order, so the notes stay ahead of the handle.
func TestSubAgent_AContinuedChildThatCapsAgainIsRetainedAnew(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	scripts := cappedSurveyScripts("c1", retainedSurveyTask, retainedSurveyName)
	scripts = append(scripts, toolCallScript("c2", tools.SubAgentToolName, continueArgs(retainedSurveyName, continueInstructions, 50)))
	scripts = append(scripts, cappedChildTurns(3)...)
	scripts = append(scripts, foldScript(700, 40), contentScript(childClosingReport), contentScript("parent done"))

	a := runCappedSurveyParent(t, subAgentConfig(sink, domain.ModeAskBefore, reader), &requestLogResponder{scripts: scripts})

	got, ok := a.retained.lookup(retainedSurveyName)
	if !ok {
		t.Fatalf("the continued child is not retained under %q; retained names = %v", retainedSurveyName, a.retained.names())
	}
	want := retainedDelegate{
		task:          retainedSurveyTask,
		name:          retainedSurveyName,
		tools:         tools.SubAgentRoster{Names: []string{"read_thing"}},
		outputPath:    "notes/survey.md",
		fold:          childFoldSummary,
		closingReport: childClosingReport,
		bound:         boundSteps,
		spawnCallID:   "c2",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("retained delegate = %+v, want %+v", got, want)
	}
	second, ok := subAgentResultFor(sink.events, "c2")
	if !ok {
		t.Fatal("no result answered the continued delegation c2")
	}
	wantSecond := cappedResult(fmt.Sprintf(stepCapResultFormat, 3), childFoldSummary, childClosingReport) +
		"\n" + fmt.Sprintf(stepCapClampNoteFormat, 50, 3, 3) +
		"\n" + fmt.Sprintf(continueLineFormat, retainedSurveyName)
	if second.Content != wantSecond {
		t.Errorf("the continued child's capped result =\n%s\nwant\n%s", second.Content, wantSecond)
	}
}

// TestSubAgent_CompletedResultCarriesNoContinueLine is the floor: a named delegation that ran to
// completion is not retained, so its result offers no handle.
func TestSubAgent_CompletedResultCarriesNoContinueLine(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	scripts := [][]provider.Delta{
		toolCallScript("c1", tools.SubAgentToolName, cappedSurveyArgs(retainedSurveyTask, retainedSurveyName)),
		contentScript("the survey is complete"),
		contentScript("parent done"),
	}

	runCappedSurveyParent(t, subAgentConfig(sink, domain.ModeAskBefore, reader), &requestLogResponder{scripts: scripts})

	got, ok := subAgentResultFor(sink.events, "c1")
	if !ok || got.Content != "the survey is complete" {
		t.Errorf("completed result = %+v, want the child's report alone, no continue line", got)
	}
}

// ----------------------------------------------------------------------------
// Retention of a FAULTED delegation (plan 2026-09-20 - 04, item 5; ADR 0082)
// ----------------------------------------------------------------------------
//
// A child whose Exchange faulted with the parent still live is retained exactly as a capped one:
// the engine folds its conversation at the fault (Agent.finishAtFault), the error result gains the
// continue line, and a `continue` spawns a fresh child from that fold. A cancel still unwinds the
// whole delegation and retains nothing; an unnamed delegation still has no handle.

// faultedSurveyScripts is the upstream script of one named delegation that spends two working
// Turns and then faults on its third request: the spawning call, the two Turns, the fault, then
// the fold reply the engine's summary call is answered with — the parent's own turns follow.
func faultedSurveyScripts(callID, task, name string, fold []provider.Delta) [][]provider.Delta {
	scripts := [][]provider.Delta{toolCallScript(callID, tools.SubAgentToolName, cappedSurveyArgs(task, name))}
	scripts = append(scripts, cappedChildTurns(2)...)
	scripts = append(scripts, []provider.Delta{{Kind: provider.DeltaError, Err: "the upstream died"}})
	return append(scripts, fold)
}

// assertFaultedResultContinuable fails unless result is the ERROR result of a faulted delegation
// named name whose last body line is the continue line.
func assertFaultedResultContinuable(t *testing.T, result domain.ToolResult, name string) {
	t.Helper()
	if !result.IsError || !strings.HasPrefix(result.Content, subAgentFaultPrefix) {
		t.Fatalf("faulted result = %+v, want an error result opening on %q", result, subAgentFaultPrefix)
	}
	if want := "\n" + fmt.Sprintf(continueLineFormat, name); !strings.HasSuffix(result.Content, want) {
		t.Errorf("faulted result =\n%s\nwant it to end on %q", result.Content, want)
	}
}

// TestSubAgent_FaultedDelegateIsRetainedAndContinuable drives fault → continue → completion: the
// error result ends on the continue line, the child is retained with the fold the engine wrote at
// the fault, and the continued child's first request opens on the retained task, that fold and the
// instructions — the same opening a capped child's continuation reads.
func TestSubAgent_FaultedDelegateIsRetainedAndContinuable(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	scripts := faultedSurveyScripts("c1", retainedSurveyTask, retainedSurveyName, foldScript(700, 40))
	scripts = append(scripts, toolCallScript("c2", tools.SubAgentToolName, continueArgs(retainedSurveyName, continueInstructions, 0)))
	scripts = append(scripts, contentScript("the survey is now complete"), contentScript("parent done"))
	responder := &requestLogResponder{scripts: scripts}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, reader)
	cfg.Delegation.MaxSteps = 3
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	// The retention is read as the continue call is about to be answered — after the faulted
	// child was retained, before the continuation consumes the entry.
	var retained retainedDelegate
	var wasRetained bool
	responder.before = func(call int) {
		if call == 5 {
			retained, wasRetained = a.retained.lookup(retainedSurveyName)
		}
	}
	if err := a.Submit(domain.UserInput{Text: "please research"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	res, err := a.Run(context.Background())

	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete || res.Faulted {
		t.Fatalf("parent result = %+v, want a clean exchange-complete (the child's fault must not fault the parent)", res)
	}
	faulted, ok := subAgentResultFor(sink.events, "c1")
	if !ok {
		t.Fatal("no result answered the faulted delegation c1")
	}
	assertFaultedResultContinuable(t, faulted, retainedSurveyName)
	if !strings.Contains(faulted.Content, "the upstream died") {
		t.Errorf("faulted result = %q, want the fault's cause in it", faulted.Content)
	}
	if !wasRetained {
		t.Fatal("the faulted delegation was not retained before the continue call")
	}
	want := retainedDelegate{
		task:          retainedSurveyTask,
		name:          retainedSurveyName,
		tools:         tools.SubAgentRoster{Names: []string{"read_thing"}},
		outputPath:    "notes/survey.md",
		fold:          childFoldSummary,
		closingReport: "reading file 1",
		spawnCallID:   "c1",
	}
	if !reflect.DeepEqual(retained, want) {
		t.Errorf("retained delegate = %+v, want %+v", retained, want)
	}
	// The continued child's opening request: call 5 (0: spawn, 1–2: turns, 3: the fault, 4: the
	// fold, 5: the continue call, 6: the child's first Turn).
	opening := responder.requests[6]
	wantTask := retainedSurveyTask + "\n\n" + previousAttemptHead + "\n" + childFoldSummary + "\n\n" + continuationInstructionsHead + "\n" + continueInstructions
	if got := lastUserText(opening); got != wantTask {
		t.Errorf("the continued child opened on\n%s\nwant\n%s", got, wantTask)
	}
	completed, ok := subAgentResultFor(sink.events, "c2")
	if !ok || completed.IsError || completed.Content != "the survey is now complete" {
		t.Errorf("the continued child's result = %+v, want its completed report", completed)
	}
	if names := a.retained.names(); len(names) != 0 {
		t.Errorf("retained names = %v after the continuation completed, want none — the entry is consumed", names)
	}
}

// TestSubAgent_FaultedDelegateFoldFailureRetainsTheUnavailableMarker keeps the cap path's fold
// contract on the fault path: a summary call that faults too retains the unavailable marker naming
// its cause, the error result still carries the continue line, and the continuation's task shows
// that marker under the previous-attempt head rather than an empty body.
func TestSubAgent_FaultedDelegateFoldFailureRetainsTheUnavailableMarker(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	scripts := faultedSurveyScripts("c1", retainedSurveyTask, retainedSurveyName, []provider.Delta{{Kind: provider.DeltaError, Err: "summarizer exploded"}})
	scripts = append(scripts, toolCallScript("c2", tools.SubAgentToolName, continueArgs(retainedSurveyName, continueInstructions, 0)))
	scripts = append(scripts, contentScript("the survey is now complete"), contentScript("parent done"))
	responder := &requestLogResponder{scripts: scripts}

	runCappedSurveyParent(t, subAgentConfig(sink, domain.ModeAskBefore, reader), responder)

	faulted, ok := subAgentResultFor(sink.events, "c1")
	if !ok {
		t.Fatal("no result answered the faulted delegation c1")
	}
	assertFaultedResultContinuable(t, faulted, retainedSurveyName)
	opening := lastUserText(responder.requests[6])
	head := previousAttemptHead + "\n[engine summary unavailable — "
	if !strings.Contains(opening, head) || !strings.Contains(opening, "summarizer exploded") {
		t.Errorf("the continued child opened on\n%s\nwant the unavailable marker naming the summarizer's fault under %q", opening, previousAttemptHead)
	}
}

// TestSubAgent_ZeroTurnFaultedDelegateSkipsTheFold pins the zero-Turn gate: a child that faults on
// its FIRST request has no history worth a summary call, so none is made; it is still retained
// with its closing text (none) and the continue line, and the continuation's task carries the
// unavailable marker naming the reason under the previous-attempt head — never an empty body.
func TestSubAgent_ZeroTurnFaultedDelegateSkipsTheFold(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	scripts := [][]provider.Delta{
		toolCallScript("c1", tools.SubAgentToolName, cappedSurveyArgs(retainedSurveyTask, retainedSurveyName)),
		{{Kind: provider.DeltaError, Err: "the upstream died"}}, // the child's first request faults
		toolCallScript("c2", tools.SubAgentToolName, continueArgs(retainedSurveyName, continueInstructions, 0)),
		contentScript("the survey is now complete"),
		contentScript("parent done"),
	}
	responder := &requestLogResponder{scripts: scripts}

	runCappedSurveyParent(t, subAgentConfig(sink, domain.ModeAskBefore, reader), responder)

	if got := len(responder.requests); got != 5 {
		t.Fatalf("the upstream saw %d requests, want 5 — no fold request for a child that completed no Turn", got)
	}
	faulted, ok := subAgentResultFor(sink.events, "c1")
	if !ok {
		t.Fatal("no result answered the faulted delegation c1")
	}
	assertFaultedResultContinuable(t, faulted, retainedSurveyName)
	opening := lastUserText(responder.requests[3])
	wantTask := retainedSurveyTask + "\n\n" + previousAttemptHead + "\n" + fmt.Sprintf(engineFoldUnavailableFormat, zeroTurnFoldCause) + "\n\n" + continuationInstructionsHead + "\n" + continueInstructions
	if opening != wantTask {
		t.Errorf("the continued child opened on\n%s\nwant\n%s", opening, wantTask)
	}
}

// TestSubAgent_FaultedDelegateFoldIsBounded pins the fold's own deadline: a summary call that
// stalls past Config.StreamIdleTimeout while the parent ctx stays live is cut, the marker names the
// bound, and the error result lands with the continue line — not "" and not a cancel.
func TestSubAgent_FaultedDelegateFoldIsBounded(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, reader)
	cfg.StreamIdleTimeout = 20 * time.Millisecond
	scripts := faultedSurveyScripts("c1", retainedSurveyTask, retainedSurveyName, nil)
	scripts = append(scripts, contentScript("parent done"))
	responder := &blockAtResponder{scripts: scripts, blockAt: 4, started: make(chan struct{})} // call 4 is the fold

	a := runCappedSurveyParent(t, cfg, responder)

	faulted, ok := subAgentResultFor(sink.events, "c1")
	if !ok {
		t.Fatal("no result answered the faulted delegation c1")
	}
	assertFaultedResultContinuable(t, faulted, retainedSurveyName)
	retained, ok := a.retained.lookup(retainedSurveyName)
	if !ok {
		t.Fatalf("no delegation retained under %q", retainedSurveyName)
	}
	want := fmt.Sprintf(engineFoldUnavailableFormat, fmt.Sprintf(foldBoundExceededFormat, "20ms"))
	if retained.fold != want {
		t.Errorf("retained fold = %q, want %q", retained.fold, want)
	}
}

// TestSubAgent_CancelledDelegateIsNotRetained keeps D2: a cancel while the child runs unwinds the
// whole delegation — no fold, no result, no retained name, so nothing to continue.
func TestSubAgent_CancelledDelegateIsNotRetained(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, reader)
	scripts := [][]provider.Delta{toolCallScript("c1", tools.SubAgentToolName, cappedSurveyArgs(retainedSurveyTask, retainedSurveyName))}
	scripts = append(scripts, cappedChildTurns(1)...)
	responder := &blockAtResponder{scripts: scripts, blockAt: 2, started: make(chan struct{})} // the child's second Turn blocks
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "please research"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-responder.started
		cancel()
	}()

	res, err := a.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.Status != domain.StatusCancelled {
		t.Fatalf("parent result = %+v, want a cancel", res)
	}
	if names := a.retained.names(); len(names) != 0 {
		t.Errorf("retained names = %v after a cancel, want none", names)
	}
	if got := responder.calls; got != 3 {
		t.Errorf("the upstream saw %d calls, want 3 — no fold request after a cancel", got)
	}
	if _, ok := subAgentResultFor(sink.events, "c1"); ok {
		t.Error("a cancelled delegation surfaced a result")
	}
}

// TestSubAgent_UnnamedFaultedDelegateIsNotRetained is the floor the cap path shares: a faulted
// delegation that ended its run without a name has no handle, so it is not retained and its error
// result carries no continue line.
func TestSubAgent_UnnamedFaultedDelegateIsNotRetained(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	scripts := [][]provider.Delta{subAgentCallScript("c1", retainedSurveyTask)}
	scripts = append(scripts, cappedChildTurns(2)...)
	scripts = append(scripts, []provider.Delta{{Kind: provider.DeltaError, Err: "the upstream died"}}, foldScript(700, 40), contentScript("parent done"))

	a := runCappedSurveyParent(t, subAgentConfig(sink, domain.ModeAskBefore, reader), &requestLogResponder{scripts: scripts})

	faulted, ok := subAgentResultFor(sink.events, "c1")
	if !ok || !faulted.IsError {
		t.Fatalf("faulted result = %+v, want an error result", faulted)
	}
	if strings.Contains(faulted.Content, "to continue this delegate") {
		t.Errorf("faulted result = %q, carries a continue line for a delegation nothing is retained under", faulted.Content)
	}
	if names := a.retained.names(); len(names) != 0 {
		t.Errorf("retained names = %v, want none — an unnamed delegation has no handle", names)
	}
}

// TestSplitUserSteeredTrailer_AfterTheContinueLine pins that the continue line, the last body note,
// leaves the trailer recognisable: the split still lands between the two.
func TestSplitUserSteeredTrailer_AfterTheContinueLine(t *testing.T) {
	t.Parallel()
	bodyText := "report\n" + fmt.Sprintf(continueLineFormat, retainedSurveyName)
	content := bodyText + userSteeredTrailerSeparator + userSteeredTrailerSingular

	body, trailer := splitUserSteeredTrailer(content)

	if body != bodyText || trailer != userSteeredTrailerSeparator+userSteeredTrailerSingular {
		t.Errorf("splitUserSteeredTrailer = %q, %q; want %q and the trailer", body, trailer, bodyText)
	}
}

// ----------------------------------------------------------------------------
// The delegate ledger (apogee-clb) — `[engine — delegations]` on the coordinator's request tail
// ----------------------------------------------------------------------------
//
// runSubAgent records one row per delegation from its closing defer (children.go,
// delegationLedger); buildRequest renders the rows as an engine note on every request tail once the
// Exchange holds two delegations or one that did not complete, ahead of the wrap-up directive, with
// the system prompt as the fallback for a tail that is not a tool result. Output presence is read
// at render time from the child's RESOLVED target.

// loggingResponder records every request it forwards to inner — a routedResponder has no log of
// its own, and the ledger note is read off the parent's request tail.
type loggingResponder struct {
	inner    provider.Responder
	mu       sync.Mutex
	requests []provider.Request
}

func (l *loggingResponder) Stream(ctx context.Context, req provider.Request) iter.Seq[provider.Delta] {
	l.mu.Lock()
	l.requests = append(l.requests, req)
	l.mu.Unlock()
	return l.inner.Stream(ctx, req)
}

// ledgerFence returns the delegations engine note fenced on the tail of req, or "" when the tail
// carries none.
func ledgerFence(req provider.Request) string {
	if len(req.Messages) == 0 {
		return ""
	}
	tail := req.Messages[len(req.Messages)-1].Content
	open := domain.EngineNoteFencePrefix + delegationsNoteTopic + "]"
	start := strings.Index(tail, open)
	if start < 0 {
		return ""
	}
	end := strings.Index(tail[start:], domain.EngineNoteFenceClosePrefix+delegationsNoteTopic+"]")
	if end < 0 {
		return tail[start:]
	}
	return tail[start : start+end]
}

// ledgerParentRequest returns the parent's request that follows its delegations: the last one the
// responder logged whose asker is the human's text.
func ledgerParentRequest(t *testing.T, requests []provider.Request, asker string) provider.Request {
	t.Helper()
	for i := len(requests) - 1; i >= 0; i-- {
		if lastUserText(requests[i]) == asker {
			return requests[i]
		}
	}
	t.Fatalf("no request logged for %q", asker)
	return provider.Request{}
}

// TestDelegationLedgerRecordsEveryOutcomeInSpawnOrder drives a depth-0 pool of three: Alpha is
// held until a sibling has already been recorded, Beta faults on its first request, Gamma
// completes. The rows come back #1 Alpha, #2 Beta, #3 Gamma — spawn order, not completion order —
// with the fault's head line as Beta's cause, and the parent's next request carries them in that
// order on its tail.
func TestDelegationLedgerRecordsEveryOutcomeInSpawnOrder(t *testing.T) {
	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore)
	cfg.ParallelAgents = 3
	spawn := []provider.Delta{
		{Kind: provider.DeltaToolCall, ToolCall: &provider.ToolCall{
			ID: "c1", Type: "function",
			Function: provider.FunctionCall{Name: tools.SubAgentToolName, Arguments: subAgentNamedArgs("task one", "Alpha")},
		}},
		{Kind: provider.DeltaToolCall, ToolCall: &provider.ToolCall{
			ID: "c2", Type: "function",
			Function: provider.FunctionCall{Name: tools.SubAgentToolName, Arguments: subAgentNamedArgs("task two", "Beta")},
		}},
		{Kind: provider.DeltaToolCall, ToolCall: &provider.ToolCall{
			ID: "c3", Type: "function",
			Function: provider.FunctionCall{Name: tools.SubAgentToolName, Arguments: subAgentNamedArgs("task three", "Gamma")},
		}},
		{Kind: provider.DeltaDone, FinishReason: "tool_calls"},
	}
	var a *Agent
	// Alpha's gate: its reply streams only once ANOTHER sibling's row is already in the ledger, so
	// the first-spawned delegation is certainly not the first recorded.
	holdAlpha := func(ctx context.Context) {
		deadline := time.After(5 * time.Second)
		for len(a.delegations.rows()) == 0 {
			select {
			case <-ctx.Done():
				return
			case <-deadline:
				return
			default:
				time.Sleep(time.Millisecond)
			}
		}
	}
	routed := newRoutedResponder().
		route("delegate three things", nil, spawn).
		route("delegate three things", nil, contentScript("parent done")).
		route("task one", holdAlpha, contentScript("alpha found it")).
		route("task two", nil, []provider.Delta{{Kind: provider.DeltaError, Err: "beta's upstream died"}}).
		route("task three", nil, contentScript("gamma found it"))
	up := &loggingResponder{inner: routed}

	var err error
	a, err = newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "delegate three things"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete || res.Faulted {
		t.Fatalf("parent result = %+v, want a clean exchange-complete", res)
	}

	rows := a.delegations.rows()
	if len(rows) != 3 {
		t.Fatalf("rows = %+v, want three", rows)
	}
	wantNames := []string{"Alpha", "Beta", "Gamma"}
	wantOutcomes := []delegationOutcome{delegationCompleted, delegationFaulted, delegationCompleted}
	for i, r := range rows {
		if r.spawnIndex != i+1 || r.name != wantNames[i] || r.outcome != wantOutcomes[i] {
			t.Errorf("row %d = %+v, want #%d %s %s", i, r, i+1, wantNames[i], wantOutcomes[i])
		}
	}
	if !strings.HasPrefix(rows[1].cause, subAgentFaultPrefix) {
		t.Errorf("Beta's cause = %q, want the fault result's head line", rows[1].cause)
	}
	fence := ledgerFence(ledgerParentRequest(t, up.requests, "delegate three things"))
	if fence == "" {
		t.Fatal("the parent's request after the fan-out carries no delegations note on its tail")
	}
	alpha, beta, gamma := strings.Index(fence, "#1 Alpha — completed"), strings.Index(fence, "#2 Beta — faulted: "), strings.Index(fence, "#3 Gamma — completed")
	if alpha < 0 || beta < 0 || gamma < 0 || !(alpha < beta && beta < gamma) {
		t.Errorf("fence = %q, want the three rows in spawn order", fence)
	}
}

// TestDelegationLedgerRecordsARefusedSibling pins the refusal row: a sub_agent call no child was
// built for — here an empty task — is recorded as refused with the refusal's text as its cause,
// labelled by its call id since it has neither a name nor a task, and one refusal alone puts the
// note on the parent's next request.
func TestDelegationLedgerRecordsARefusedSibling(t *testing.T) {
	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore)
	responder := &requestLogResponder{scripts: [][]provider.Delta{
		toolCallScript("c1", tools.SubAgentToolName, `{"task":""}`),
		contentScript("parent done"),
	}}
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	runExchange(t, a, "delegate nothing")

	rows := a.delegations.rows()
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want the one refused row", rows)
	}
	want := delegationRecord{spawnIndex: 1, callID: "c1", name: "call c1", outcome: delegationRefused, cause: "sub_agent requires a non-empty task"}
	if rows[0] != want {
		t.Errorf("row = %+v, want %+v", rows[0], want)
	}
	fence := ledgerFence(responder.requests[1])
	if !strings.Contains(fence, "#1 call c1 — refused: sub_agent requires a non-empty task — output none") {
		t.Errorf("fence = %q, want the refused row", fence)
	}
}

// TestDelegationLedgerNoteIsAbsentForOneCompletedDelegation pins the trigger's quiet case: one
// delegation that completed is recorded but renders no note anywhere on the parent's next request.
func TestDelegationLedgerNoteIsAbsentForOneCompletedDelegation(t *testing.T) {
	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore)
	responder := &requestLogResponder{scripts: [][]provider.Delta{
		subAgentCallScript("c1", "summarise the repo"),
		contentScript("the repo is a Go TUI agent"),
		contentScript("parent done"),
	}}
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	runExchange(t, a, "please summarise")

	if rows := a.delegations.rows(); len(rows) != 1 || rows[0].outcome != delegationCompleted || rows[0].name != "summarise the repo" {
		t.Fatalf("rows = %+v, want one completed row labelled by the task's first line", rows)
	}
	for i, req := range responder.requests {
		for _, m := range req.Messages {
			if strings.Contains(m.Content, delegationsNoteHead) || strings.Contains(m.Content, domain.EngineNoteFencePrefix+delegationsNoteTopic) {
				t.Errorf("request %d carries the delegations note for one completed delegation: %q", i, m.Content)
			}
		}
	}
}

// TestDelegationLedgerNoteRidesTheTailAfterAFault pins the placement on a real run: after one
// faulted delegation the parent's next request ends on the sub_agent tool result, the note is
// fenced onto that tail under the engine's own header with the fault's head line as the cause,
// and the system prompt carries no copy of it.
func TestDelegationLedgerNoteRidesTheTailAfterAFault(t *testing.T) {
	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore)
	responder := &requestLogResponder{scripts: [][]provider.Delta{
		subAgentCallScript("c1", "summarise the repo"),
		{{Kind: provider.DeltaError, Err: "the upstream died"}},
		contentScript("parent done"),
	}}
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	runExchange(t, a, "please summarise")

	req := responder.requests[2]
	tail := requestTail(t, req)
	if tail.Role != string(domain.RoleTool) || tail.ToolCallID != "c1" {
		t.Fatalf("tail = %+v, want the sub_agent tool result", tail)
	}
	sub, ok := lastSubAgentResult(sink.events)
	if !ok || !sub.IsError {
		t.Fatalf("sub_agent result = %+v, want the fault", sub)
	}
	row := "#1 summarise the repo — faulted: " + delegationCause(sub.Content) + " — output none"
	if want := domain.RenderEngineNote(delegationsNoteTopic, delegationsNoteHead+"\n"+row); !strings.HasSuffix(tail.Content, want) {
		t.Errorf("tail = %q, want it to end on the fence %q", tail.Content, want)
	}
	if strings.Contains(tail.Content, domain.AdviceFencePrefix) {
		t.Errorf("tail = %q wears the advice fence; an engine note has its own header", tail.Content)
	}
	if got := requestSystemText(req); strings.Contains(got, delegationsNoteHead) {
		t.Errorf("system text = %q, want no note there — it rides the tail", got)
	}
}

// TestDelegationLedgerNoteRidesAheadOfTheWrapUp pins the order of the two engine notes on one
// tail: a wrapping-up coordinator with a ledger to report stamps the delegations note first, so
// the wrap-up directive stays on the very end where a capped child reads last.
func TestDelegationLedgerNoteRidesAheadOfTheWrapUp(t *testing.T) {
	a, _, _, _ := wrapUpAgent(t, true, contentScript("unused"))
	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: "coordinate the survey"})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c2", Tool: tools.SubAgentToolName}}})
	a.conv.Append(domain.Message{Role: domain.RoleTool, ToolCallID: "c2", Content: "beta's report"})
	a.delegations.record(delegationRecord{spawnIndex: a.delegations.open("c1"), callID: "c1", name: "Alpha", outcome: delegationCompleted})
	a.delegations.record(delegationRecord{spawnIndex: a.delegations.open("c2"), callID: "c2", name: "Beta", outcome: delegationCompleted})

	req, _ := a.buildRequest(2)

	msgs := req.State().Messages
	tail := msgs[len(msgs)-1]
	ledger := strings.Index(tail.Content, domain.EngineNoteFencePrefix+delegationsNoteTopic+"]")
	wrapUp := strings.Index(tail.Content, domain.EngineNoteFencePrefix+wrapUpNoteTopic+"]")
	if ledger < 0 || wrapUp < 0 || ledger > wrapUp {
		t.Fatalf("tail = %q, want the delegations note before the wrap-up directive", tail.Content)
	}
	if !strings.HasSuffix(tail.Content, domain.EngineNoteFenceClosePrefix+wrapUpNoteTopic+"]") {
		t.Errorf("tail = %q, want the wrap-up fence on the very end", tail.Content)
	}
	if !strings.Contains(tail.Content, "#1 Alpha — completed — output none\n#2 Beta — completed — output none") {
		t.Errorf("tail = %q, want both rows in the delegations fence", tail.Content)
	}
}

// TestDelegationLedgerNoteFallsBackToTheSystemPromptOnAnAssistantTail pins the other leg of the
// placement: a request whose tail is not a tool result — the faulted-then-retried shape — cannot
// take the note, so it lands in the system prompt through AppendToSystem and nothing is fenced.
func TestDelegationLedgerNoteFallsBackToTheSystemPromptOnAnAssistantTail(t *testing.T) {
	a, _, _, _ := wrapUpAgent(t, false, contentScript("unused"))
	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: "coordinate the survey"})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, Content: "half a reply, then a fault"})
	a.delegations.record(delegationRecord{spawnIndex: a.delegations.open("c1"), callID: "c1", name: "Alpha", outcome: delegationFaulted, cause: "died"})

	req, _ := a.buildRequest(1)

	msgs := req.State().Messages
	want := delegationsNoteHead + "\n#1 Alpha — faulted: died — output none"
	if got := requestSystemText(a.toProviderRequest(req)); !strings.Contains(got, want) {
		t.Errorf("system text = %q, want the note %q — the fallback for a tail that is not a tool result", got, want)
	}
	if tail := msgs[len(msgs)-1]; tail.Role != domain.RoleAssistant || strings.Contains(tail.Content, domain.EngineNoteFencePrefix) {
		t.Errorf("tail = %q (%s), want the assistant message untouched", tail.Content, tail.Role)
	}
	for _, m := range msgs {
		if strings.Contains(m.Content, domain.EngineNoteFencePrefix) {
			t.Errorf("a message carries the engine fence on the fallback path: %q", m.Content)
		}
	}
}

// TestDelegationLedgerReportsOutputPresenceAtRenderTime pins the output column: a capped
// delegation spawned with an `output_path` it never wrote records the RESOLVED workspace target
// (never the argument as spelled, which a stat against the process cwd would misreport) and reads
// `missing` on the parent's next request; once the file exists the same ledger reads `present`,
// because presence is read when the note is rendered, not when the row was recorded.
func TestDelegationLedgerReportsOutputPresenceAtRenderTime(t *testing.T) {
	a, responder, _, ws := outputPathAgent(t, domain.ModeAllowEdits, "out/report.md", contentScript(childClosingReport))

	runExchange(t, a, "please research")

	rows := a.delegations.rows()
	if len(rows) != 1 || rows[0].outcome != delegationCapped {
		t.Fatalf("rows = %+v, want one capped row", rows)
	}
	if want := filepath.Join(ws, "out", "report.md"); rows[0].outputPath != want {
		t.Errorf("outputPath = %q, want the resolved target %q", rows[0].outputPath, want)
	}
	fence := ledgerFence(responder.requests[len(responder.requests)-1])
	if !strings.Contains(fence, "#1 trawl the repo — capped — output missing") {
		t.Errorf("fence = %q, want the capped row reading `output missing`", fence)
	}

	if err := os.MkdirAll(filepath.Join(ws, "out"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "out", "report.md"), []byte("late"), 0o644); err != nil {
		t.Fatal(err)
	}

	if note, ok := a.delegations.note(); !ok || !strings.Contains(note, "#1 trawl the repo — capped — output present") {
		t.Errorf("note = %q, %v; want `output present` once the file exists", note, ok)
	}
}

// TestDelegationLedgerIsForgottenAsTheNextExchangeOpens pins the lifetime: the rows a faulted
// delegation left are cleared as the human's next message opens an Exchange, so that Exchange's
// requests carry no note and a fresh delegation counts from #1 again.
func TestDelegationLedgerIsForgottenAsTheNextExchangeOpens(t *testing.T) {
	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore)
	responder := &requestLogResponder{scripts: [][]provider.Delta{
		subAgentCallScript("c1", "summarise the repo"),
		{{Kind: provider.DeltaError, Err: "the upstream died"}},
		contentScript("parent done"),
		contentScript("second exchange done"),
	}}
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "please summarise")
	if rows := a.delegations.rows(); len(rows) != 1 {
		t.Fatalf("rows after the first exchange = %+v, want one", rows)
	}

	runExchange(t, a, "and now something else")

	if rows := a.delegations.rows(); len(rows) != 0 {
		t.Errorf("rows after the next exchange opened = %+v, want none", rows)
	}
	for _, m := range responder.requests[3].Messages {
		if strings.Contains(m.Content, delegationsNoteHead) {
			t.Errorf("the next exchange's request carries the old ledger: %q", m.Content)
		}
	}
	if got := a.delegations.open("c9"); got != 1 {
		t.Errorf("next spawn index = %d, want 1 — the count restarts with the exchange", got)
	}
}

// TestDelegationLedgerNoteNeverReachesTheRecord pins the note's ephemerality: it is a projection
// onto the request alone, so neither the conversation's messages nor the session snapshot carry
// the head line or the fence.
func TestDelegationLedgerNoteNeverReachesTheRecord(t *testing.T) {
	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore)
	responder := &requestLogResponder{scripts: [][]provider.Delta{
		subAgentCallScript("c1", "summarise the repo"),
		{{Kind: provider.DeltaError, Err: "the upstream died"}},
		contentScript("parent done"),
	}}
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	runExchange(t, a, "please summarise")

	if fence := ledgerFence(responder.requests[2]); fence == "" {
		t.Fatal("the request carried no delegations note; nothing to check against the record")
	}
	for _, m := range a.conv.Messages() {
		if strings.Contains(m.Content, delegationsNoteHead) || len(m.Advice) != 0 {
			t.Errorf("conversation message carries the note or a ledger row: %+v", m)
		}
	}
	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if strings.Contains(string(snap.State), delegationsNoteHead) || strings.Contains(string(snap.State), delegationsNoteTopic+"]") {
		t.Errorf("snapshot carries the delegations note: %s", snap.State)
	}
}
