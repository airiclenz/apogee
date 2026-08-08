package domain

// White-box tests for MechanismRegistry.Add's registration gates (phase-4-review-fixes
// item 5): a duplicate MechanismID and the reserved experimental sentinel are refused
// loudly at registration, never silently absorbed. The row builder (regd) lives in
// registry_ordered_test.go.

import (
	"context"
	"strings"
	"testing"
)

// TestAdd_RefusesDuplicateID proves a MechanismID can be registered only once: topoSort's
// byID map used to silently drop one of two same-ID Mechanisms; now the second Add fails
// loudly, naming the ID, and the registry keeps only the first.
func TestAdd_RefusesDuplicateID(t *testing.T) {
	t.Parallel()
	r := NewMechanismRegistry()
	if err := r.Add(regd("dup")); err != nil {
		t.Fatalf("first Add(dup): %v", err)
	}

	err := r.Add(regd("dup"))
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

	err := r.Add(regd(ExperimentalMechanismID))
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

// scopedHook is a hook that hands each sub-agent its OWN instance (SubAgentScoped) — the shape a
// Mechanism carrying per-run state takes. Each instance remembers the parent it came from, so a
// test can tell a fresh child instance from a shared one.
type scopedHook struct {
	from *scopedHook
}

func (h *scopedHook) PreRequest(context.Context, *Request) error { return nil }
func (h *scopedHook) ForSubAgent() any                           { return &scopedHook{from: h} }

// TestForSubAgent_ContainerIsAlwaysTheChildsOwn proves the delegation seam never hands a child the
// registry the parent is running: the container is fresh, so an Add into one is invisible to the
// other and two agents can never race through the registry itself.
func TestForSubAgent_ContainerIsAlwaysTheChildsOwn(t *testing.T) {
	t.Parallel()
	parent := NewMechanismRegistry()
	if err := parent.Add(regd("inherited")); err != nil {
		t.Fatalf("Add(inherited): %v", err)
	}

	child := parent.ForSubAgent()
	if child == parent {
		t.Fatal("ForSubAgent returned the parent's own registry; the child must get its own container")
	}
	if got := len(child.Ordered(HookPreRequest)); got != 1 {
		t.Fatalf("child registry holds %d Mechanisms, want the parent's 1", got)
	}
	if err := child.Add(regd("child_only")); err != nil {
		t.Fatalf("Add(child_only) into the child registry: %v", err)
	}
	if got := len(parent.Ordered(HookPreRequest)); got != 1 {
		t.Errorf("parent registry holds %d Mechanisms after the child registered one, want 1", got)
	}
}

// TestForSubAgent_ScopedHooksAreFreshAndOthersInherited is the seam's whole rule in one test: a hook
// declaring SubAgentScoped hands the child the instance IT chose (here a fresh one, so two siblings
// cannot share state), and a hook declaring nothing is inherited verbatim — which is what keeps
// today's value-hook catalogue byte-identical across the delegation boundary.
func TestForSubAgent_ScopedHooksAreFreshAndOthersInherited(t *testing.T) {
	t.Parallel()
	stateful := &scopedHook{}
	stateless := preReqMech{}
	parent := NewMechanismRegistry()
	if err := parent.Add(RegisteredMechanism{Descriptor: MechanismDescriptor{ID: "stateful"}, Hook: stateful}); err != nil {
		t.Fatalf("Add(stateful): %v", err)
	}
	if err := parent.Add(RegisteredMechanism{Descriptor: MechanismDescriptor{ID: "stateless"}, Hook: stateless}); err != nil {
		t.Fatalf("Add(stateless): %v", err)
	}
	if err := parent.AddExperimental(HookPreRequest, stateful); err != nil {
		t.Fatalf("AddExperimental(stateful): %v", err)
	}

	one, two := parent.ForSubAgent(), parent.ForSubAgent()
	hookOf := func(r *MechanismRegistry, id MechanismID) any {
		t.Helper()
		for _, m := range r.Ordered(HookPreRequest) {
			if m.Descriptor.ID == id {
				return m.Hook
			}
		}
		t.Fatalf("registry holds no Mechanism %q", id)
		return nil
	}

	childOne, childTwo := hookOf(one, "stateful"), hookOf(two, "stateful")
	if childOne == stateful || childTwo == stateful {
		t.Error("a SubAgentScoped hook was inherited by pointer; the child must run the instance ForSubAgent returned")
	}
	if childOne == childTwo {
		t.Error("two children got the SAME scoped instance; siblings would share state")
	}
	if got := childOne.(*scopedHook).from; got != stateful {
		t.Errorf("scoped instance came from %p, want the parent's %p", got, stateful)
	}
	if got := hookOf(one, "stateless"); got != any(stateless) {
		t.Errorf("a hook declaring no scope = %v, want the parent's instance inherited verbatim", got)
	}
	if exp := one.Experimental(HookPreRequest); len(exp) != 1 || exp[0] == stateful {
		t.Errorf("experimental hooks = %v, want the scoped instance the seam produced", exp)
	}
}
