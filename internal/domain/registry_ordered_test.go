package domain

// White-box tests for the deterministic dispatch order (Ordered) and the incompatibility
// gate (ValidateIncompatibilities) added in Phase-4 item 2. They live in package domain so a
// minimal stub Mechanism can satisfy the hook interfaces directly.

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// preReqMech is a minimal catalogued Mechanism hooking at pre-request, carrying just the
// descriptor + ordering fields the topo-sort and the gates read.
type preReqMech struct {
	id       MechanismID
	before   []MechanismID
	after    []MechanismID
	incompat []MechanismID
	requires []MechanismID
}

func (m preReqMech) Descriptor() MechanismDescriptor {
	return MechanismDescriptor{ID: m.id, IncompatibleWith: m.incompat, Requires: m.requires}
}
func (m preReqMech) Ordering() OrderingConstraints {
	return OrderingConstraints{Before: m.before, After: m.after}
}
func (preReqMech) PreRequest(context.Context, *Request) error { return nil }

// postRespMech hooks at post-response only — the fixture proving Ordered filters by hook point.
type postRespMech struct{ id MechanismID }

func (m postRespMech) Descriptor() MechanismDescriptor { return MechanismDescriptor{ID: m.id} }
func (postRespMech) Ordering() OrderingConstraints     { return OrderingConstraints{} }
func (postRespMech) PostResponse(context.Context, *Response) (PostResponseDecision, error) {
	return PostResponseDecision{}, nil
}

func orderedIDs(mechs []Mechanism) []MechanismID {
	out := make([]MechanismID, len(mechs))
	for i, m := range mechs {
		out[i] = m.Descriptor().ID
	}
	return out
}

func registerAll(t *testing.T, mechs ...Mechanism) *MechanismRegistry {
	t.Helper()
	r := NewMechanismRegistry()
	for _, m := range mechs {
		if err := r.Add(m); err != nil {
			t.Fatalf("Add(%s): %v", m.Descriptor().ID, err)
		}
	}
	return r
}

func TestOrdered_DeterministicUnderShuffle(t *testing.T) {
	// Constraints: b before d, a after b (⇒ b before a). c and d are free.
	// Expected Kahn order (lowest ready ID first): b, a, c, d.
	build := func(order ...Mechanism) []MechanismID {
		return orderedIDs(registerAll(t, order...).Ordered(HookPreRequest))
	}
	a := preReqMech{id: "a", after: []MechanismID{"b"}}
	b := preReqMech{id: "b", before: []MechanismID{"d"}}
	c := preReqMech{id: "c"}
	d := preReqMech{id: "d"}

	want := []MechanismID{"b", "a", "c", "d"}
	shuffles := [][]Mechanism{
		{a, b, c, d},
		{d, c, b, a},
		{c, a, d, b},
		{b, d, a, c},
	}
	for i, s := range shuffles {
		if got := build(s...); !reflect.DeepEqual(got, want) {
			t.Errorf("shuffle %d: Ordered = %v, want %v", i, got, want)
		}
	}
}

func TestOrdered_TiebreakByID(t *testing.T) {
	// No constraints at all ⇒ pure lexicographic order by canonical ID, regardless of
	// registration order.
	r := registerAll(t,
		preReqMech{id: "zebra"},
		preReqMech{id: "alpha"},
		preReqMech{id: "mike"},
	)
	want := []MechanismID{"alpha", "mike", "zebra"}
	if got := orderedIDs(r.Ordered(HookPreRequest)); !reflect.DeepEqual(got, want) {
		t.Errorf("Ordered = %v, want %v", got, want)
	}
}

func TestOrdered_FiltersByHookPoint(t *testing.T) {
	r := registerAll(t,
		preReqMech{id: "pre"},
		postRespMech{id: "post"},
	)
	if got := orderedIDs(r.Ordered(HookPreRequest)); !reflect.DeepEqual(got, []MechanismID{"pre"}) {
		t.Errorf("Ordered(pre-request) = %v, want [pre]", got)
	}
	if got := orderedIDs(r.Ordered(HookPostResponse)); !reflect.DeepEqual(got, []MechanismID{"post"}) {
		t.Errorf("Ordered(post-response) = %v, want [post]", got)
	}
	if got := r.Ordered(HookPreToolExec); len(got) != 0 {
		t.Errorf("Ordered(pre-tool-exec) = %v, want empty", orderedIDs(got))
	}
}

func TestOrdered_IgnoresConstraintOnAbsentMechanism(t *testing.T) {
	// b names a Before edge to an ID that is not registered at this hook point; it must be
	// ignored, leaving the pure ID tiebreak (a, b).
	r := registerAll(t,
		preReqMech{id: "b", before: []MechanismID{"not-here"}},
		preReqMech{id: "a"},
	)
	want := []MechanismID{"a", "b"}
	if got := orderedIDs(r.Ordered(HookPreRequest)); !reflect.DeepEqual(got, want) {
		t.Errorf("Ordered = %v, want %v", got, want)
	}
}

func TestValidateIncompatibilities(t *testing.T) {
	t.Run("both registered ⇒ error", func(t *testing.T) {
		r := registerAll(t,
			preReqMech{id: "read_loop", incompat: []MechanismID{"cached_content_intercept"}},
			preReqMech{id: "cached_content_intercept"},
		)
		if err := r.ValidateIncompatibilities(); !errors.Is(err, ErrIncompatibleMechanisms) {
			t.Errorf("ValidateIncompatibilities = %v, want ErrIncompatibleMechanisms", err)
		}
	})

	t.Run("declaration is symmetric in effect", func(t *testing.T) {
		// Only the SECOND mechanism declares the incompatibility; it must still trip.
		r := registerAll(t,
			preReqMech{id: "read_loop"},
			preReqMech{id: "cached_content_intercept", incompat: []MechanismID{"read_loop"}},
		)
		if err := r.ValidateIncompatibilities(); !errors.Is(err, ErrIncompatibleMechanisms) {
			t.Errorf("ValidateIncompatibilities = %v, want ErrIncompatibleMechanisms", err)
		}
	})

	t.Run("only one side registered ⇒ ok", func(t *testing.T) {
		r := registerAll(t,
			preReqMech{id: "read_loop", incompat: []MechanismID{"cached_content_intercept"}},
		)
		if err := r.ValidateIncompatibilities(); err != nil {
			t.Errorf("ValidateIncompatibilities = %v, want nil (the peer is not registered)", err)
		}
	})

	t.Run("compatible set ⇒ ok", func(t *testing.T) {
		r := registerAll(t,
			preReqMech{id: "a"},
			preReqMech{id: "b"},
		)
		if err := r.ValidateIncompatibilities(); err != nil {
			t.Errorf("ValidateIncompatibilities = %v, want nil", err)
		}
	})
}

func TestValidateRequirements(t *testing.T) {
	t.Run("required peer absent ⇒ error naming both IDs", func(t *testing.T) {
		r := registerAll(t,
			preReqMech{id: "guided_decomposition", requires: []MechanismID{"tool_result_cap"}},
		)
		err := r.ValidateRequirements()
		if !errors.Is(err, ErrMissingRequirement) {
			t.Fatalf("ValidateRequirements = %v, want ErrMissingRequirement", err)
		}
		msg := err.Error()
		if !strings.Contains(msg, "guided_decomposition") || !strings.Contains(msg, "tool_result_cap") {
			t.Errorf("error %q does not name both the requiring and required IDs", msg)
		}
	})

	t.Run("required peer present ⇒ ok", func(t *testing.T) {
		r := registerAll(t,
			preReqMech{id: "guided_decomposition", requires: []MechanismID{"tool_result_cap"}},
			preReqMech{id: "tool_result_cap"},
		)
		if err := r.ValidateRequirements(); err != nil {
			t.Errorf("ValidateRequirements = %v, want nil (the required peer is registered)", err)
		}
	})

	t.Run("empty Requires ⇒ ok", func(t *testing.T) {
		r := registerAll(t,
			preReqMech{id: "a"},
			preReqMech{id: "b"},
		)
		if err := r.ValidateRequirements(); err != nil {
			t.Errorf("ValidateRequirements = %v, want nil (no requirements declared)", err)
		}
	})

	t.Run("requirement chain A→B→C all present ⇒ ok (transitive by iteration)", func(t *testing.T) {
		r := registerAll(t,
			preReqMech{id: "a", requires: []MechanismID{"b"}},
			preReqMech{id: "b", requires: []MechanismID{"c"}},
			preReqMech{id: "c"},
		)
		if err := r.ValidateRequirements(); err != nil {
			t.Errorf("ValidateRequirements = %v, want nil (whole chain registered)", err)
		}
	})

	t.Run("requirement chain with a missing link ⇒ error (the broken link, not the head)", func(t *testing.T) {
		// A→B→C but C is absent: iterating every Mechanism's direct requirements catches the
		// B→C break independently of A, so no recursion is needed.
		r := registerAll(t,
			preReqMech{id: "a", requires: []MechanismID{"b"}},
			preReqMech{id: "b", requires: []MechanismID{"c"}},
		)
		err := r.ValidateRequirements()
		if !errors.Is(err, ErrMissingRequirement) {
			t.Fatalf("ValidateRequirements = %v, want ErrMissingRequirement", err)
		}
		if msg := err.Error(); !strings.Contains(msg, "\"b\"") || !strings.Contains(msg, "\"c\"") {
			t.Errorf("error %q should name the broken B→C link", msg)
		}
	})
}
