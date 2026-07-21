package agent

import (
	"sync"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// TestAgentSetConfineToWorkspaceAffectsResolution proves a runtime SetConfineToWorkspace changes
// the per-call Resolution on the SAME Agent instance with no rebuild: in Auto, a third-party
// write gates while the workspace fence is on and auto-runs the moment the user turns it off.
// This is the load-bearing guarantee behind /confine — the toggle changes ACTUAL gating, not a
// label (the SetMode precedent, one column over).
func TestAgentSetConfineToWorkspaceAffectsResolution(t *testing.T) {
	sink := &recordingSink{}
	write := fakeTool{name: "w"} // readOnly:false, no markers ⇒ third-party write class
	a, err := newAgent(autoConfig(sink, eligibleConfiner{}, true, write), &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	call := domain.ToolCall{ID: "c1", Tool: "w"}

	if !a.ConfineToWorkspace() {
		t.Fatal("ConfineToWorkspace() = false at construction, want the cfg seed true")
	}
	if got := resolveLadder(a.resolutionInput(write, call, a.guards.PreExecute(call))).kind; got != resolveGate {
		t.Fatalf("confined Auto ladder = %s, want resolveGate", got)
	}

	a.SetConfineToWorkspace(false)

	if a.ConfineToWorkspace() {
		t.Fatal("ConfineToWorkspace() = true after SetConfineToWorkspace(false)")
	}
	if got := resolveLadder(a.resolutionInput(write, call, a.guards.PreExecute(call))).kind; got != resolveRun {
		t.Fatalf("after SetConfineToWorkspace(false) ladder = %s, want resolveRun", got)
	}

	a.SetConfineToWorkspace(true)

	if got := resolveLadder(a.resolutionInput(write, call, a.guards.PreExecute(call))).kind; got != resolveGate {
		t.Fatalf("after SetConfineToWorkspace(true) ladder = %s, want resolveGate again", got)
	}
}

// TestAgentSetConfineToWorkspaceObservedByNextToolCall drives the toggle through real dispatch:
// the first Exchange's write gates through the Approver (fence on), and after the toggle the
// NEXT Exchange's identical call runs with no Approval at all. It pins the wiring end to end —
// dispatch reads the LIVE flag, not cfg's construction seed.
func TestAgentSetConfineToWorkspaceObservedByNextToolCall(t *testing.T) {
	sink := &recordingSink{}
	ran := 0
	write := fakeTool{name: "w", ran: &ran}
	// ApprovalAllow (not allow-for-session) so the second call would gate again if the toggle
	// were not observed — no cache can mask a stale read.
	approver := &fakeApprover{decision: domain.ApprovalAllow}
	cfg := autoConfig(sink, eligibleConfiner{}, true, write)
	cfg.Approver = approver
	responder := &scriptedResponder{scripts: [][]provider.Delta{
		toolCallScript("c1", "w", `{}`),
		contentScript("done"),
		toolCallScript("c2", "w", `{}`),
		contentScript("done"),
	}}
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	_ = runExchange(t, a, "go")
	if approver.calls != 1 {
		t.Fatalf("confined Auto consulted the Approver %d times, want 1", approver.calls)
	}
	if ran != 1 {
		t.Fatalf("tool ran %d times after the approved gate, want 1", ran)
	}

	a.SetConfineToWorkspace(false)

	_ = runExchange(t, a, "again")
	if approver.calls != 1 {
		t.Fatalf("Approver consulted %d times in total, want 1 — the unconfined call must not gate", approver.calls)
	}
	if ran != 2 {
		t.Fatalf("tool ran %d times in total, want 2 — the unconfined call must still run", ran)
	}
}

// TestNewChildAgentInheritsLiveConfineToWorkspace proves a sub-agent spawned AFTER a toggle
// inherits the parent's LIVE blast radius (privileges = the parent's current ones, ADR
// 0005/0013), not the immutable construction seed — and that a child already spawned keeps what
// it was spawned with, so a later toggle can neither loosen nor tighten a running delegation.
func TestNewChildAgentInheritsLiveConfineToWorkspace(t *testing.T) {
	sink := &recordingSink{}
	write := fakeTool{name: "w"}
	a, err := newAgent(autoConfig(sink, eligibleConfiner{}, true, write), &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	call := domain.ToolCall{ID: "c1", Tool: "w"}

	spawnedConfined, err := a.newChildAgent()
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}
	if !spawnedConfined.ConfineToWorkspace() {
		t.Fatal("child spawned under a confined parent = unconfined, want confined")
	}

	a.SetConfineToWorkspace(false)

	child, err := a.newChildAgent()
	if err != nil {
		t.Fatalf("newChildAgent after the toggle: %v", err)
	}
	if child.ConfineToWorkspace() {
		t.Fatal("child confine-to-workspace = true, want the parent's live false at spawn")
	}
	if got := resolveLadder(child.resolutionInput(write, call, child.guards.PreExecute(call))).kind; got != resolveRun {
		t.Fatalf("child write ladder = %s, want resolveRun (the parent's live blast radius)", got)
	}
	if !spawnedConfined.ConfineToWorkspace() {
		t.Fatal("the earlier child lost its fence when the parent toggled; a spawned child keeps its own value")
	}
}

// TestAgentSetConfineToWorkspaceConcurrent runs SetConfineToWorkspace (the UI side) against the
// worker-side live reads (ConfineToWorkspace / the ladder) under the race detector, proving the
// lock covers them. It asserts nothing beyond "no data race" — that is the whole point of a
// toggle that lands mid-Step.
func TestAgentSetConfineToWorkspaceConcurrent(t *testing.T) {
	sink := &recordingSink{}
	write := fakeTool{name: "w"}
	a, err := newAgent(autoConfig(sink, eligibleConfiner{}, true, write), &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	call := domain.ToolCall{ID: "c1", Tool: "w"}

	const iters = 2000
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			a.SetConfineToWorkspace(i%2 == 0)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			_ = a.ConfineToWorkspace()
			_ = resolveLadder(a.resolutionInput(write, call, a.guards.PreExecute(call)))
			_, _ = a.newChildAgent() // the spawn seam reads the live flag too
		}
	}()
	wg.Wait()
}
