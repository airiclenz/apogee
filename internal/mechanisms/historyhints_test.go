package mechanisms

import (
	"errors"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// historyView builds a read-only LoopView over history — the window the post-tool-result and
// pre-tool-exec hooks in the Wave-3 history-aware family scan for cross-Turn evidence.
func historyView(history []domain.Message) domain.LoopView {
	return domain.NewRequest("m", history, nil, domain.Budget{}, 0, nil).View()
}

// Every member of the history-aware family is a strikes-3 Mechanism, NOT an exempt off-ramp
// (catalogue C1: apogee narrows exempt to the two true off-ramps), so all suppress normally and are
// disabled under Bypass — the item's "all suppress normally (non-exempt)" guarantee. Each resolves
// to its catalogued hook point.
func TestHistoryFamilyDescriptorsNonExempt(t *testing.T) {
	t.Parallel()
	cases := []struct {
		id   domain.MechanismID
		cap  domain.Capability
		hook func(domain.Mechanism) bool
	}{
		{errorEnrichmentID, domain.CapResponseRepair, func(m domain.Mechanism) bool { _, ok := m.(domain.PostToolResultHook); return ok }},
		{readLoopID, domain.CapProactiveNudge, func(m domain.Mechanism) bool { _, ok := m.(domain.PreRequestHook); return ok }},
		{readRepeatID, domain.CapResponseRepair, func(m domain.Mechanism) bool { _, ok := m.(domain.PostResponseHook); return ok }},
		{toolLoopInterceptorID, domain.CapResponseRepair, func(m domain.Mechanism) bool { _, ok := m.(domain.PostResponseHook); return ok }},
		{cachedContentInterceptID, domain.CapProactiveNudge, func(m domain.Mechanism) bool { _, ok := m.(domain.PreToolExecHook); return ok }},
	}
	for _, c := range cases {
		m := mustBuild(t, c.id)
		d := m.Descriptor()
		if d.ID != c.id {
			t.Errorf("Descriptor().ID = %q, want %q", d.ID, c.id)
		}
		if d.Capability != c.cap {
			t.Errorf("%q Capability = %q, want %q", c.id, d.Capability, c.cap)
		}
		if d.Suppression != domain.SuppressStrikesThree {
			t.Errorf("%q Suppression = %q, want strikes-3 (non-exempt)", c.id, d.Suppression)
		}
		if !c.hook(m) {
			t.Errorf("%q does not implement its catalogued hook interface", c.id)
		}
	}
}

// The re-read family (read_loop, read_repeat, cached_content_intercept) is pairwise-exclusive on the
// same wasted-read symptom (catalogue Table A / C2). In apogee IncompatibleWith is a startup gate, so
// any two co-registered fail ValidateIncompatibilities — at most one is enabled at a time.
func TestReReadFamilyPairwiseIncompatible(t *testing.T) {
	t.Parallel()
	pairs := [][2]domain.MechanismID{
		{readLoopID, readRepeatID},
		{readLoopID, cachedContentInterceptID},
		{readRepeatID, cachedContentInterceptID},
	}
	for _, p := range pairs {
		reg := domain.NewMechanismRegistry()
		if err := reg.Add(mustBuild(t, p[0])); err != nil {
			t.Fatalf("Add(%q): %v", p[0], err)
		}
		if err := reg.Add(mustBuild(t, p[1])); err != nil {
			t.Fatalf("Add(%q): %v", p[1], err)
		}
		if err := reg.ValidateIncompatibilities(); !errors.Is(err, domain.ErrIncompatibleMechanisms) {
			t.Errorf("ValidateIncompatibilities(%q,%q) = %v, want ErrIncompatibleMechanisms", p[0], p[1], err)
		}
	}
}

// error_enrichment and tool_loop_interceptor declare no incompatibility, so they co-register with a
// re-read-family member cleanly (they are not part of the exclusive symptom).
func TestHistoryFamilyCompatibleMembersCoRegister(t *testing.T) {
	t.Parallel()
	reg := domain.NewMechanismRegistry()
	for _, id := range []domain.MechanismID{errorEnrichmentID, toolLoopInterceptorID, readLoopID} {
		if err := reg.Add(mustBuild(t, id)); err != nil {
			t.Fatalf("Add(%q): %v", id, err)
		}
	}
	if err := reg.ValidateIncompatibilities(); err != nil {
		t.Fatalf("ValidateIncompatibilities: %v", err)
	}
	if err := reg.ValidateOrdering(); err != nil {
		t.Fatalf("ValidateOrdering: %v", err)
	}
}

// read_repeat and tool_loop_interceptor slot into the post-response cascade BEFORE validate, so the
// resolved dispatch order is read_repeat → tool_loop_interceptor → validate → autofix → syntax — the
// sim's response-side priority (response_analysis.go @pin: repeat-reads, then the tool loop, then
// validation, earliest match short-circuits). This is the concrete check that apogee follows the sim
// source over catalogue Table A's contradictory "read_repeat: After validate" cell (item-11 NOTES).
func TestPostResponseCascadeOrder(t *testing.T) {
	t.Parallel()
	reg := domain.NewMechanismRegistry()
	for _, id := range []domain.MechanismID{readRepeatID, toolLoopInterceptorID, validateID, autofixID, syntaxID} {
		if err := reg.Add(mustBuild(t, id)); err != nil {
			t.Fatalf("Add(%q): %v", id, err)
		}
	}
	if err := reg.ValidateOrdering(); err != nil {
		t.Fatalf("ValidateOrdering: %v", err)
	}
	want := []domain.MechanismID{readRepeatID, toolLoopInterceptorID, validateID, autofixID, syntaxID}
	got := reg.Ordered(domain.HookPostResponse)
	if len(got) != len(want) {
		t.Fatalf("Ordered(post-response) has %d mechanisms, want %d", len(got), len(want))
	}
	for i, m := range got {
		if m.Descriptor().ID != want[i] {
			t.Errorf("cascade[%d] = %q, want %q (full order: %v)", i, m.Descriptor().ID, want[i], want)
		}
	}
}
