package domain

// White-box tests for MechanismRegistry.Add's registration gates (phase-4-review-fixes
// item 5): a duplicate MechanismID and the reserved experimental sentinel are refused
// loudly at registration, never silently absorbed. The fixtures (preReqMech) live in
// registry_ordered_test.go.

import (
	"strings"
	"testing"
)

// TestAdd_RefusesDuplicateID proves a MechanismID can be registered only once: topoSort's
// byID map used to silently drop one of two same-ID Mechanisms; now the second Add fails
// loudly, naming the ID, and the registry keeps only the first.
func TestAdd_RefusesDuplicateID(t *testing.T) {
	t.Parallel()
	r := NewMechanismRegistry()
	if err := r.Add(preReqMech{id: "dup"}); err != nil {
		t.Fatalf("first Add(dup): %v", err)
	}

	err := r.Add(preReqMech{id: "dup"})
	if err == nil {
		t.Fatal("second Add of the same ID: want an error, got nil")
	}
	if !strings.Contains(err.Error(), `"dup"`) {
		t.Errorf("duplicate-ID error = %q, want it to name the ID", err)
	}
	if got := len(r.Ordered(HookPreRequest)); got != 1 {
		t.Errorf("registry holds %d Mechanisms after the refused duplicate, want 1", got)
	}
}

// TestAdd_RefusesReservedExperimentalID proves a catalogued Mechanism cannot claim the
// synthetic ID experimental hooks fire under (R5) — it would masquerade as the bench's
// own instrument and inherit its always-booked fire accounting.
func TestAdd_RefusesReservedExperimentalID(t *testing.T) {
	t.Parallel()
	r := NewMechanismRegistry()

	err := r.Add(preReqMech{id: ExperimentalMechanismID})
	if err == nil {
		t.Fatal("Add of the reserved experimental ID: want an error, got nil")
	}
	if !strings.Contains(err.Error(), "reserved") {
		t.Errorf("reserved-ID error = %q, want it to say the ID is reserved", err)
	}
	if got := len(r.Ordered(HookPreRequest)); got != 0 {
		t.Errorf("registry holds %d Mechanisms after the refused reserved ID, want 0", got)
	}
}
