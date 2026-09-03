package agent

// Sub-agent spawn under the PRODUCTION Config.EnableMechanisms arm (ADR 0015 Realisation; plan
// item 9). The existing coverage splits the two concerns: enable_mechanisms_test.go arms via
// EnableMechanisms but never delegates, and guided_decomposition_test.go delegates but arms via a
// pre-built Config.Mechanisms (wave1Registry). Neither exercises the seam the ADR names — a spawned
// sub-agent inherits the parent's ALREADY-BUILT registry (subagent.go: childCfg.Mechanisms =
// a.registry.ForSubAgent(), which hands the child that catalogue in a container of its own) and
// CLEARS EnableMechanisms so the child does not rebuild those IDs into the inherited registry and
// trip the already-registered rejection. These tests arm guided_decomposition +
// syntax by ID with Config.Mechanisms left nil (the engine BUILDS the stack), drive one
// real delegation, and prove the child ran the inherited stack — through New and through Resume, the
// one construction path the ADR names.

import (
	"context"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// gdEnableStack is the production arm under test: two catalogued rows left for the engine to BUILD
// (Config.Mechanisms nil), one of which — syntax — is a response-repair row a CHILD can trip. A
// spawned sub-agent must inherit this built registry, not rebuild it. (The stack used to name the
// tool-result cap as guided_decomposition's Required peer; that is the `tool-result-cap` Floor guard
// now, on for every agent at every Depth and so no proof that a registry was inherited.)
var gdEnableStack = []domain.MechanismID{"guided_decomposition", "syntax"}

// enableMechanismsSubAgentConfig arms the stack by ID (Config.Mechanisms left nil so the engine
// builds it), wires the sub_agent recursion point plus a write_file tool the child can call, and
// sets the discovered window the delegation is budgeted against.
func enableMechanismsSubAgentConfig(sink domain.EventSink) domain.Config {
	cfg := subAgentConfig(sink, domain.ModeAskBefore,
		fakeTool{name: "write_file", result: "ok"})
	cfg.EnableMechanisms = gdEnableStack
	cfg.Context.MaxContextTokens = gdWindow
	return cfg
}

// enableMechanismsSubAgentScripts is the run-ordered script the shared responder replays across the
// parent AND its one child: the parent delegates unprompted (a modest opening ask, so signal A never
// fires; the committed delegation then keeps the once-per-Exchange gate quiet), and the child writes
// a Go file whose payload is unbalanced — which the inherited syntax row retries in place, firing at
// Depth 1 — before the child and then the parent each answer. The call itself is well formed, so the
// tool-call repair Floor guard ahead of every hook stands down and leaves the seam to the Mechanism.
func enableMechanismsSubAgentScripts() [][]provider.Delta {
	return [][]provider.Delta{
		subAgentCallScript("s1", "investigate the auth module and report the entry points"),               // parent T0: unprompted delegation
		toolCallScript("w0", "write_file", `{"path":"auth.go","content":"package auth\nfunc Login() {"}`), // child T0: unbalanced write → syntax retries
		contentScript("child: entry points catalogued"),                                                   // child T0 (re-streamed): final report
		contentScript("parent: synthesized the delegated investigation"),                                  // parent T1: final answer
	}
}

// TestEnableMechanisms_SubAgentSpawnInheritsBuiltRegistry: a parent armed via Config.EnableMechanisms
// (registry nil ⇒ engine-built) delegates once; the spawn succeeds, the child nests at Depth 1, and
// the child fires a catalogued Mechanism from the registry it inherited (its OWN container over the
// parent's rows — ForSubAgent — not the parent's registry object).
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
	// this — the child would rebuild guided_decomposition/syntax into the registry it
	// inherited and fail with the already-registered rejection, surfacing "could not construct
	// sub-agent" here.
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

	// The child ran the INHERITED stack: syntax retried the child's unbalanced write in place,
	// booking a fire at Depth 1. A child on an empty registry (the EnableMechanisms clear
	// mis-applied to Mechanisms, or the inheritance dropped) books no such fire.
	if !hasFireAtDepth(sink.events, "syntax", 1) {
		t.Errorf("no syntax fire at Depth 1; the child did not run the inherited EnableMechanisms stack. fires=%+v",
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
