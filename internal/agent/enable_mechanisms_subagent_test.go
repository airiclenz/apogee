package agent

// Sub-agent spawn under the PRODUCTION Config.EnableMechanisms arm (ADR 0015 Realisation; plan
// item 9). The existing coverage splits the two concerns: enable_mechanisms_test.go arms via
// EnableMechanisms but never delegates, and guided_decomposition_test.go delegates but arms via a
// pre-built Config.Mechanisms (wave1Registry). Neither exercises the seam the ADR names — a spawned
// sub-agent inherits the parent's ALREADY-BUILT registry (subagent.go: childCfg.Mechanisms =
// a.registry) and CLEARS EnableMechanisms so the child does not rebuild those IDs into the shared
// registry and trip the already-registered rejection. These tests arm guided_decomposition +
// tool_result_cap by ID with Config.Mechanisms left nil (the engine BUILDS the stack), drive one
// real delegation, and prove the child ran the inherited stack — through New and through Resume, the
// one construction path the ADR names.

import (
	"context"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// gdEnableStack is the production arm under test: the guided_decomposition stack with its Required
// peer, left for the engine to BUILD (Config.Mechanisms nil). A spawned sub-agent must inherit this
// built registry, not rebuild it.
var gdEnableStack = []domain.MechanismID{"guided_decomposition", "tool_result_cap"}

// subAgentCapResult is a big, many-lined read_file result — comfortably over tool_result_cap's
// per-result ceiling at gdWindow (working budget 1600 tokens × 4 chars/token × 0.4 ≈ 2560 chars) and
// long enough that the head/tail trim strictly shrinks it. An older copy of it (once it falls out of
// tool_result_cap's protected most-recent-Turn window) is capped, firing the Mechanism once inside
// the child.
func subAgentCapResult() string {
	return strings.Repeat("scanned a source line wide enough to matter for the budget\n", 200)
}

// enableMechanismsSubAgentConfig arms the guided_decomposition stack by ID (Config.Mechanisms left
// nil so the engine builds it), wires the sub_agent recursion point plus a read_file tool the child
// can call, and sets the discovered window so tool_result_cap has a non-zero ceiling.
func enableMechanismsSubAgentConfig(sink domain.EventSink) domain.Config {
	cfg := subAgentConfig(sink, domain.ModeAskBefore,
		fakeTool{name: "read_file", readOnly: true, result: subAgentCapResult()})
	cfg.EnableMechanisms = gdEnableStack
	cfg.Context.MaxContextTokens = gdWindow
	return cfg
}

// enableMechanismsSubAgentScripts is the run-ordered script the shared responder replays across the
// parent AND its one child: the parent delegates unprompted (a modest opening ask, so signal A never
// fires; the committed delegation then keeps the once-per-Exchange gate quiet), and the child makes
// two read_file Turns so the FIRST (older) oversized result falls out of tool_result_cap's protected
// most-recent-Turn window and is capped on the child's third request — firing the inherited Mechanism
// at Depth 1 — before the child and then the parent each answer.
func enableMechanismsSubAgentScripts() [][]provider.Delta {
	return [][]provider.Delta{
		subAgentCallScript("s1", "investigate the auth module and report the entry points"), // parent T0: unprompted delegation
		toolCallScript("r0", "read_file", `{"path":"auth.go"}`),                             // child T0: first (soon-older) read
		toolCallScript("r1", "read_file", `{"path":"login.go"}`),                            // child T1: second read → the first result is now unprotected
		contentScript("child: entry points catalogued"),                                     // child T2: final report (tool_result_cap fires on this request)
		contentScript("parent: synthesized the delegated investigation"),                    // parent T1: final answer
	}
}

// TestEnableMechanisms_SubAgentSpawnInheritsBuiltRegistry: a parent armed via Config.EnableMechanisms
// (registry nil ⇒ engine-built) delegates once; the spawn succeeds, the child nests at Depth 1, and
// the child fires a catalogued Mechanism from the inherited shared registry.
func TestEnableMechanisms_SubAgentSpawnInheritsBuiltRegistry(t *testing.T) {
	sink := &recordingSink{}
	responder := &captureAllResponder{scripts: enableMechanismsSubAgentScripts()}

	a, err := newAgent(enableMechanismsSubAgentConfig(sink), responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "Please look into the login module for me."}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	assertSubAgentInheritedStack(t, res, sink)
}

// TestEnableMechanisms_SubAgentSpawnInheritsBuiltRegistryOnResume mirrors the arm through Resume: the
// ADR names New/Resume as one construction path (mechanisms are Config, not session state), so a
// resumed parent rebuilds the same stack and a spawned child inherits it identically. A fresh armed
// Agent seeds a snapshot; Resume rebuilds the registry from Config and drives the same delegation.
func TestEnableMechanisms_SubAgentSpawnInheritsBuiltRegistryOnResume(t *testing.T) {
	seed, err := newAgent(enableMechanismsSubAgentConfig(&recordingSink{}), echoResponder{reply: "seed"})
	if err != nil {
		t.Fatalf("newAgent (seed): %v", err)
	}
	snap, err := seed.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	sink := &recordingSink{}
	responder := &captureAllResponder{scripts: enableMechanismsSubAgentScripts()}
	b, err := resumeAgent(enableMechanismsSubAgentConfig(sink), snap, responder)
	if err != nil {
		t.Fatalf("resumeAgent: %v", err)
	}
	if err := b.Submit(domain.UserInput{Text: "Please look into the login module for me."}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := b.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	assertSubAgentInheritedStack(t, res, sink)
}

// assertSubAgentInheritedStack checks the three ADR 0015 guarantees on a completed delegation: the
// spawn returned no error, the child nested at Depth 1, and the child fired a catalogued Mechanism
// from the inherited registry.
func assertSubAgentInheritedStack(t *testing.T, res domain.StepResult, sink *recordingSink) {
	t.Helper()

	if res.Status != domain.StatusExchangeComplete {
		t.Fatalf("final status = %q, want the Exchange to complete", res.Status)
	}

	// The spawn succeeded: the sub_agent tool result the parent saw is the child's report, not a
	// construction error. Reverting subagent.go's `childCfg.EnableMechanisms = nil` breaks exactly
	// this — the child would rebuild guided_decomposition/tool_result_cap into the shared registry and
	// fail with the already-registered rejection, surfacing "could not construct sub-agent" here.
	subRes, ok := lastSubAgentResult(sink.events)
	if !ok {
		t.Fatal("no sub_agent tool result — the parent never delegated")
	}
	if subRes.IsError {
		t.Fatalf("sub_agent result is an error (the spawn failed under the EnableMechanisms arm): %q", subRes.Content)
	}
	if !strings.Contains(subRes.Content, "entry points catalogued") {
		t.Errorf("sub_agent result = %q, want the child's report back", subRes.Content)
	}

	// The child's events nest at Depth 1 while the parent's stay at Depth 0 (ADR 0013).
	if d := gdMessageEventDepth(sink.events, "child: entry points catalogued"); d != 1 {
		t.Errorf("child report event Depth = %d, want 1 (a real nested sub-agent)", d)
	}
	if d := gdMessageEventDepth(sink.events, "parent: synthesized the delegated investigation"); d != 0 {
		t.Errorf("parent answer event Depth = %d, want 0", d)
	}

	// The child ran the INHERITED stack: tool_result_cap capped the child's older oversized read
	// result, booking a fire at Depth 1. A child on an empty registry (the EnableMechanisms clear
	// mis-applied to Mechanisms, or the inheritance dropped) books no such fire.
	if !hasFireAtDepth(sink.events, "tool_result_cap", 1) {
		t.Errorf("no tool_result_cap fire at Depth 1; the child did not run the inherited EnableMechanisms stack. fires=%+v",
			mechanismFires(sink.events))
	}
}

// hasFireAtDepth reports whether a MechanismFiredEvent for id was emitted at the given nesting Depth.
func hasFireAtDepth(events []domain.Event, id domain.MechanismID, depth int) bool {
	for _, fe := range mechanismFires(events) {
		if fe.Mechanism == id && fe.Depth == depth {
			return true
		}
	}
	return false
}
