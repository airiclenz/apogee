package agent

// Sub-agent spawn under the PRODUCTION Config.EnableMechanisms arm (ADR 0015 Realisation; plan
// item 9). The existing coverage splits the two concerns: enable_mechanisms_test.go arms via
// EnableMechanisms but never delegates, and the delegation suites arm via a pre-built
// Config.Mechanisms (a synthetic row). Neither exercises the seam the ADR names —
// a spawned sub-agent inherits the parent's ALREADY-BUILT registry (subagent.go: childCfg.Mechanisms =
// a.registry.ForSubAgent(), which hands the child that catalogue in a container of its own) and
// CLEARS EnableMechanisms so the child does not rebuild those IDs into the inherited registry and
// trip the already-registered rejection. These tests arm the catalogued row by ID with
// Config.Mechanisms left nil (the engine BUILDS the stack), drive one real delegation, and prove the
// child ran the inherited stack — through New and through Resume, the one construction path the ADR
// names.

import (
	"context"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// The arm under test is ONE catalogued-shaped row that acts on every request, so a fire booked at
// Depth 1 proves the child ran the parent's registry. It is a synthetic row rather than a shipped
// one because the shipped catalogue has been EMPTY since v0.20.0 (ADR 0071): `library` was the last
// row a child could trip, and it retired with the store it read. A spawned sub-agent must inherit
// the parent's built registry, not rebuild one of its own. (The stack used to carry the fan-out row
// too, with the tool-result cap as its Required peer; the cap is the `tool-result-cap` Floor guard
// now — on for every agent at every Depth, and so no proof that a registry was inherited — and the
// row itself retired on the same verdict.)

// inheritedMechID names the synthetic row the parent arms and the child must inherit.
const inheritedMechID domain.MechanismID = "inherited_probe"

// gdWindow is the discovered context window these delegation tests run under: at 4 chars/token
// (uncalibrated) it allocates ~400 tokens to FileContext and ~960 to History, so a modest ask leaves
// the budget honest without any allocation being close to full.
const gdWindow = 2000

// enableMechanismsSubAgentConfig arms the stack, wires the sub_agent recursion point plus a
// write_file tool the child can call, and sets the discovered window the delegation is budgeted
// against.
func enableMechanismsSubAgentConfig(t *testing.T, sink domain.EventSink) domain.Config {
	t.Helper()
	cfg := subAgentConfig(sink, domain.ModeAskBefore,
		fakeTool{name: "write_file", result: "ok"})
	fired := 0
	cfg.Mechanisms = domain.NewMechanismRegistry()
	mustAddMech(t, cfg.Mechanisms, recordingMech{id: inheritedMechID, cap: domain.CapProactiveNudge, fired: &fired}.row())
	cfg.Context.MaxContextTokens = gdWindow
	return cfg
}

// enableMechanismsSubAgentScripts is the run-ordered script the shared responder replays across the
// parent AND its one child: the parent delegates unprompted on a modest opening ask, and the child
// writes a Go file before the child and then the parent each answer. Every request the child makes
// runs the inherited stack, so the armed row acts — and books a fire — at Depth 1.
func enableMechanismsSubAgentScripts() [][]provider.Delta {
	return [][]provider.Delta{
		subAgentCallScript("s1", "investigate the auth module and report the entry points"), // parent T0: unprompted delegation
		toolCallScript("w0", "write_file", `{"path":"auth.go","content":"package auth\n"}`), // child T0: a write
		contentScript("child: entry points catalogued"),                                     // child T1: final report
		contentScript("parent: synthesized the delegated investigation"),                    // parent T1: final answer
	}
}

// TestEnableMechanisms_SubAgentSpawnInheritsBuiltRegistry: a parent armed via Config.EnableMechanisms
// (registry nil ⇒ engine-built) delegates once; the spawn succeeds, the child nests at Depth 1, and
// the child fires a catalogued Mechanism from the registry it inherited (its OWN container over the
// parent's rows — ForSubAgent — not the parent's registry object).
func TestEnableMechanisms_SubAgentSpawnInheritsBuiltRegistry(t *testing.T) {
	sink := &recordingSink{}
	responder := &captureAllResponder{scripts: enableMechanismsSubAgentScripts()}

	a, err := newAgent(enableMechanismsSubAgentConfig(t, sink), responder)
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
	seed, err := newAgent(enableMechanismsSubAgentConfig(t, &recordingSink{}), echoResponder{reply: "seed"})
	if err != nil {
		t.Fatalf("newAgent (seed): %v", err)
	}
	snap, err := seed.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	sink := &recordingSink{}
	responder := &captureAllResponder{scripts: enableMechanismsSubAgentScripts()}
	b, err := resumeAgent(enableMechanismsSubAgentConfig(t, sink), snap, responder)
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
	// this — the child would rebuild the row into the registry it inherited and fail with the
	// already-registered rejection, surfacing "could not construct sub-agent" here.
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

	// The child ran the INHERITED stack: the armed row acted on the child's own request, booking a
	// fire at Depth 1. A child on an empty registry (the EnableMechanisms clear mis-applied to
	// Mechanisms, or the inheritance dropped) books no such fire.
	if !hasFireAtDepth(sink.events, inheritedMechID, 1) {
		t.Errorf("no %s fire at Depth 1; the child did not run the inherited stack. fires=%+v",
			inheritedMechID, mechanismFires(sink.events))
	}
}

// gdMessageEventDepth returns the Depth of the first MessageEvent whose Text equals text, or -1.
func gdMessageEventDepth(events []domain.Event, text string) int {
	for _, e := range events {
		if me, ok := e.(domain.MessageEvent); ok && me.Text == text {
			return me.Depth
		}
	}
	return -1
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
