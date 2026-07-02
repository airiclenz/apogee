package agent

import (
	"sync"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestAgentSetModeAffectsDispatch proves a runtime SetMode changes the per-call disposition on
// the SAME Agent instance with no rebuild: a write tool is refused under Plan and gated under
// Allow-Edits, flipped live between the two dispose() calls. This is the load-bearing guarantee
// behind Shift+Tab — cycling the mode changes ACTUAL gating, not just a label.
func TestAgentSetModeAffectsDispatch(t *testing.T) {
	sink := &recordingSink{}
	write := fakeTool{name: "w"} // readOnly:false, no markers ⇒ third-party write class
	cfg := configWithTools(sink, write)
	cfg.Mode = domain.ModePlan
	a, err := newAgent(cfg, &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	call := domain.ToolCall{ID: "c1", Tool: "w"}

	if got := a.dispose(write, call); got != dispoRefuse {
		t.Fatalf("Plan dispose = %d, want dispoRefuse (%d)", got, dispoRefuse)
	}

	a.SetMode(domain.ModeAllowEdits)

	if got := a.dispose(write, call); got != dispoGate {
		t.Fatalf("after SetMode(allow-edits) dispose = %d, want dispoGate (%d)", got, dispoGate)
	}
	if a.Mode() != domain.ModeAllowEdits {
		t.Fatalf("Mode() = %q, want allow-edits", a.Mode())
	}
}

// TestNewChildAgentInheritsLiveMode proves a sub-agent inherits the parent's LIVE mode at spawn
// (Shift+Tab may have moved it), not the immutable construction seed.
func TestNewChildAgentInheritsLiveMode(t *testing.T) {
	sink := &recordingSink{}
	cfg := configWithTools(sink, fakeTool{name: "w"})
	cfg.Mode = domain.ModeAskBefore
	a, err := newAgent(cfg, &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	a.SetMode(domain.ModeAllowEdits)

	child, err := a.newChildAgent()
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}
	if child.Mode() != domain.ModeAllowEdits {
		t.Fatalf("child mode = %q, want allow-edits (parent's live mode at spawn)", child.Mode())
	}
}

// TestSubAgentSeesParentTighteningMidRun is the ADR-0013 realisation acceptance: a sub-agent's
// disposition tracks the parent's LIVE mode tighten-only. A child spawned in Auto auto-runs a
// write; the moment the parent tightens to Plan mid-delegation (Shift+Tab down), the child's
// NEXT write refuses — the child no longer keeps auto-approving on its frozen spawn mode.
func TestSubAgentSeesParentTighteningMidRun(t *testing.T) {
	sink := &recordingSink{}
	write := fakeTool{name: "w"} // readOnly:false, no markers ⇒ third-party write class
	cfg := configWithTools(sink, write)
	cfg.Mode = domain.ModeAuto
	cfg.Confiner = eligibleConfiner{} // Auto needs a Confiner at construction (ADR 0012)
	cfg.ConfineToWorkspace = false    // "I am the sandbox": Auto auto-runs the write (dispoRun)
	parent, err := newAgent(cfg, &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	child, err := parent.newChildAgent()
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}
	call := domain.ToolCall{ID: "c1", Tool: "w"}

	// Spawned in Auto, the child auto-runs the write — no refusal yet.
	if got := child.dispose(write, call); got == dispoRefuse {
		t.Fatalf("child spawned in Auto refused a write before any tightening (got %d)", got)
	}

	// The parent tightens to Plan MID-delegation; the still-running child must now refuse.
	parent.SetMode(domain.ModePlan)
	if got := child.dispose(write, call); got != dispoRefuse {
		t.Fatalf("after the parent tightened to Plan, child write disposition = %d, want dispoRefuse (%d)", got, dispoRefuse)
	}
}

// TestSubAgentParentLooseningCannotLoosenChild proves the other half of tighten-only: a parent
// LOOSENING mid-delegation never loosens a child spawned tighter. A child spawned in Plan keeps
// refusing writes even after the parent cycles up to Auto — loosening mid-flight stays impossible.
func TestSubAgentParentLooseningCannotLoosenChild(t *testing.T) {
	sink := &recordingSink{}
	write := fakeTool{name: "w"}
	cfg := configWithTools(sink, write)
	cfg.Mode = domain.ModePlan
	parent, err := newAgent(cfg, &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	child, err := parent.newChildAgent()
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}
	call := domain.ToolCall{ID: "c1", Tool: "w"}

	if got := child.dispose(write, call); got != dispoRefuse {
		t.Fatalf("child spawned in Plan write disposition = %d, want dispoRefuse (%d)", got, dispoRefuse)
	}

	// The parent loosens all the way to Auto; the child, spawned in Plan, must NOT loosen.
	parent.SetMode(domain.ModeAuto)
	if got := child.dispose(write, call); got != dispoRefuse {
		t.Fatalf("after the parent loosened to Auto, child (spawned Plan) write disposition = %d, want dispoRefuse (%d) — loosening must stay impossible", got, dispoRefuse)
	}
}

// TestSubAgentEffectiveModeConcurrent runs the parent's SetMode (the UI side) against the child's
// worker-side effectiveMode/dispose, proving the parent's modeMu covers the child's cross-agent
// read of the live mode through the captured accessor. It asserts nothing beyond "no data race" —
// the tighten-only view must be observed race-free while the parent's mode is being cycled.
func TestSubAgentEffectiveModeConcurrent(t *testing.T) {
	sink := &recordingSink{}
	write := fakeTool{name: "w"}
	cfg := configWithTools(sink, write)
	cfg.Mode = domain.ModeAskBefore
	parent, err := newAgent(cfg, &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	child, err := parent.newChildAgent()
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}
	call := domain.ToolCall{ID: "c1", Tool: "w"}
	ladder := []domain.Mode{domain.ModePlan, domain.ModeAskBefore, domain.ModeAllowEdits, domain.ModeAuto}

	const iters = 2000
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			parent.SetMode(ladder[i%len(ladder)])
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			_ = child.effectiveMode()
			_ = child.dispose(write, call)
		}
	}()
	wg.Wait()
}

// TestAgentSetModeConcurrent runs SetMode (the UI side) against every worker-side live read
// (Mode / dispose / toolMenu) under the race detector, proving the lock covers all of them. It
// asserts nothing beyond "no data race" — that is the whole point of the mid-turn-switch design.
func TestAgentSetModeConcurrent(t *testing.T) {
	sink := &recordingSink{}
	write := fakeTool{name: "w"}
	cfg := configWithTools(sink, write)
	cfg.Mode = domain.ModeAskBefore
	a, err := newAgent(cfg, &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	call := domain.ToolCall{ID: "c1", Tool: "w"}
	ladder := []domain.Mode{domain.ModePlan, domain.ModeAskBefore, domain.ModeAllowEdits, domain.ModeAuto}

	const iters = 2000
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			a.SetMode(ladder[i%len(ladder)])
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			_ = a.Mode()
			_ = a.dispose(write, call)
			_ = a.toolMenu()
		}
	}()
	wg.Wait()
}
