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
