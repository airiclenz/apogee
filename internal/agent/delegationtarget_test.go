package agent

import (
	"sync"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestDelegationTargetUnsetSnapshotIsNil proves the latch starts empty: a freshly constructed
// Agent reports no Delegation target, which is what every spawn reads as "routing is off, take
// today's parent-inheriting path" (ADR 0045 §4).
func TestDelegationTargetUnsetSnapshotIsNil(t *testing.T) {
	t.Parallel()

	a, err := newAgent(baseConfig(&recordingSink{}), &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	if got := a.delegationTarget(); got != nil {
		t.Fatalf("delegationTarget() on a fresh Agent = %+v, want nil", got)
	}
}

// TestSetDelegationTargetStoresAndClears proves the setter round-trips a target and that nil
// clears it — the two halves of the host's beat loop, a usable observation and an unusable one.
func TestSetDelegationTargetStoresAndClears(t *testing.T) {
	t.Parallel()

	a, err := newAgent(baseConfig(&recordingSink{}), &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	bypass := true
	want := &DelegationTarget{
		Endpoint:       "http://grunt.local:1111",
		APIKey:         "grunt-key",
		Model:          "cheap-4b",
		ContextWindow:  32768,
		ParallelAgents: 3,
		Profile:        domain.ModelProfile{Thinking: domain.ThinkingProfile{Style: domain.ThinkingDelimited}},
		Bypass:         &bypass,
		Mechanisms:     domain.NewMechanismRegistry,
	}
	a.SetDelegationTarget(want)

	got := a.delegationTarget()
	if got == nil {
		t.Fatalf("delegationTarget() after SetDelegationTarget = nil, want the latched target")
	}
	if got.Endpoint != want.Endpoint || got.APIKey != want.APIKey || got.Model != want.Model {
		t.Errorf("latched dial facts = %q/%q/%q, want %q/%q/%q",
			got.Endpoint, got.APIKey, got.Model, want.Endpoint, want.APIKey, want.Model)
	}
	if got.ContextWindow != want.ContextWindow || got.ParallelAgents != want.ParallelAgents {
		t.Errorf("latched window/cap = %d/%d, want %d/%d",
			got.ContextWindow, got.ParallelAgents, want.ContextWindow, want.ParallelAgents)
	}
	if got.Profile.Thinking.Style != domain.ThinkingDelimited {
		t.Errorf("latched profile thinking style = %v, want delimited", got.Profile.Thinking.Style)
	}
	if got.Bypass == nil || !*got.Bypass {
		t.Errorf("latched Bypass = %v, want a non-nil true", got.Bypass)
	}
	if got.Mechanisms == nil || got.Mechanisms() == nil {
		t.Error("latched Mechanisms factory is nil or builds no registry, want one that builds a fresh registry per child")
	}

	a.SetDelegationTarget(nil)

	if got := a.delegationTarget(); got != nil {
		t.Fatalf("delegationTarget() after SetDelegationTarget(nil) = %+v, want nil (fallback)", got)
	}
}

// TestDelegationTargetConcurrentSetAndSnapshot drives the latch the way a live session does — the
// host's heartbeat goroutine pushing beats while several spawning goroutines snapshot (a depth-0
// fan-out spawns at once, ADR 0039) — so `-race` proves the RWMutex covers the shared field.
func TestDelegationTargetConcurrentSetAndSnapshot(t *testing.T) {
	t.Parallel()

	a, err := newAgent(baseConfig(&recordingSink{}), &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	const beats = 200
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range beats {
			if i%2 == 0 {
				a.SetDelegationTarget(&DelegationTarget{Endpoint: "http://grunt.local:1111", Model: "cheap-4b"})
				continue
			}
			a.SetDelegationTarget(nil) // an unusable beat: the fallback push
		}
	}()

	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range beats {
				if got := a.delegationTarget(); got != nil && got.Model != "cheap-4b" {
					t.Errorf("snapshot model = %q, want the latched %q", got.Model, "cheap-4b")
				}
			}
		}()
	}

	wg.Wait()
}

// TestChildSharesTheParentsDelegationLatch is the shared-handle acceptance: a child built BEFORE
// the host latched anything still reads the target the parent is given afterwards, and so does a
// grandchild — one latch serves the whole tree, so routing reaches every depth from the one place
// a host pushes to (ADR 0045).
func TestChildSharesTheParentsDelegationLatch(t *testing.T) {
	t.Parallel()

	cfg := configWithTools(&recordingSink{}, fakeTool{name: "w"})
	parent, err := newAgent(cfg, &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	child, err := parent.newChildAgent("call_sub", "the delegated task", "")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}
	grandchild, err := child.newChildAgent("call_sub_sub", "the nested task", "")
	if err != nil {
		t.Fatalf("newChildAgent (depth 2): %v", err)
	}
	if got := child.delegationTarget(); got != nil {
		t.Fatalf("child target before any push = %+v, want nil", got)
	}

	parent.SetDelegationTarget(&DelegationTarget{Endpoint: "http://grunt.local:1111", Model: "cheap-4b"})

	if got := child.delegationTarget(); got == nil || got.Model != "cheap-4b" {
		t.Errorf("child target after the parent's push = %+v, want the latched cheap-4b", got)
	}
	if got := grandchild.delegationTarget(); got == nil || got.Model != "cheap-4b" {
		t.Errorf("grandchild target after the parent's push = %+v, want the latched cheap-4b", got)
	}

	parent.SetDelegationTarget(nil)

	if got := grandchild.delegationTarget(); got != nil {
		t.Errorf("grandchild target after the clearing push = %+v, want nil", got)
	}
}
