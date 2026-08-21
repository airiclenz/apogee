package domain

// White-box tests for the deterministic dispatch order (Ordered) and the incompatibility
// gate (ValidateIncompatibilities) added in Phase-4 item 2. They live in package domain so a
// minimal stub hook can satisfy the hook interfaces directly.

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// preReqMech is a minimal pre-request hook — behaviour only. Its descriptor and ordering are
// supplied by the row regd wraps it in, exactly as the catalogue supplies a real Mechanism's.
type preReqMech struct{}

func (preReqMech) PreRequest(context.Context, *Request) error { return nil }

// postRespMech hooks at post-response only — the fixture proving Ordered filters by hook point.
type postRespMech struct{}

func (postRespMech) PostResponse(context.Context, *Response) (PostResponseDecision, error) {
	return PostResponseDecision{}, nil
}

// regd builds the row the registry stores: a descriptor keyed by id, no ordering edges, and the
// pre-request hook fixture. The options below set whatever a case needs, so each fixture stays
// the one-liner it was when the metadata lived on the stub type.
func regd(id MechanismID, opts ...func(*RegisteredMechanism)) RegisteredMechanism {
	m := RegisteredMechanism{Descriptor: MechanismDescriptor{ID: id}, Hook: preReqMech{}}
	for _, opt := range opts {
		opt(&m)
	}
	return m
}

// before / after declare the row's ordering edges (the topo-sort's input).
func before(ids ...MechanismID) func(*RegisteredMechanism) {
	return func(m *RegisteredMechanism) { m.Ordering.Before = ids }
}

func after(ids ...MechanismID) func(*RegisteredMechanism) {
	return func(m *RegisteredMechanism) { m.Ordering.After = ids }
}

// incompatibleWith / requires declare the descriptor's stacking relations (the gates' input).
func incompatibleWith(ids ...MechanismID) func(*RegisteredMechanism) {
	return func(m *RegisteredMechanism) { m.Descriptor.IncompatibleWith = ids }
}

func requires(ids ...MechanismID) func(*RegisteredMechanism) {
	return func(m *RegisteredMechanism) { m.Descriptor.Requires = ids }
}

// atPostResponse swaps in the post-response hook, so Ordered's hook-point filter has something
// to filter out.
func atPostResponse(m *RegisteredMechanism) { m.Hook = postRespMech{} }

func orderedIDs(mechs []RegisteredMechanism) []MechanismID {
	out := make([]MechanismID, len(mechs))
	for i, m := range mechs {
		out[i] = m.Descriptor.ID
	}
	return out
}

func registerAll(t *testing.T, mechs ...RegisteredMechanism) *MechanismRegistry {
	t.Helper()
	r := NewMechanismRegistry()
	for _, m := range mechs {
		if err := r.Add(m); err != nil {
			t.Fatalf("Add(%s): %v", m.Descriptor.ID, err)
		}
	}
	return r
}

func TestOrdered_DeterministicUnderShuffle(t *testing.T) {
	// Constraints: b before d, a after b (⇒ b before a). c and d are free.
	// Expected Kahn order (lowest ready ID first): b, a, c, d.
	build := func(order ...RegisteredMechanism) []MechanismID {
		return orderedIDs(registerAll(t, order...).Ordered(HookPreRequest))
	}
	a := regd("a", after("b"))
	b := regd("b", before("d"))
	c := regd("c")
	d := regd("d")

	want := []MechanismID{"b", "a", "c", "d"}
	shuffles := [][]RegisteredMechanism{
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
		regd("zebra"),
		regd("alpha"),
		regd("mike"),
	)
	want := []MechanismID{"alpha", "mike", "zebra"}
	if got := orderedIDs(r.Ordered(HookPreRequest)); !reflect.DeepEqual(got, want) {
		t.Errorf("Ordered = %v, want %v", got, want)
	}
}

func TestOrdered_FiltersByHookPoint(t *testing.T) {
	r := registerAll(t,
		regd("pre"),
		regd("post", atPostResponse),
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
		regd("b", before("not-here")),
		regd("a"),
	)
	want := []MechanismID{"a", "b"}
	if got := orderedIDs(r.Ordered(HookPreRequest)); !reflect.DeepEqual(got, want) {
		t.Errorf("Ordered = %v, want %v", got, want)
	}
}

// TestOrdered_FrozenOrderMatchesOnTheFly proves the order the validate gates precompute is the
// same order Ordered used to compute on every call: the same fixtures as
// TestOrdered_DeterministicUnderShuffle above, plus a post-response row so both sides run the
// hook-point filter, compared across all five hook points.
func TestOrdered_FrozenOrderMatchesOnTheFly(t *testing.T) {
	build := func() *MechanismRegistry {
		// b before d, a after b, c and d free — the shuffle test's constraint set.
		return registerAll(t, regd("a", after("b")), regd("b", before("d")), regd("c"), regd("d"),
			regd("post", atPostResponse))
	}
	onTheFly := build() // never validated: computes per call, as a bench caller's registry does
	frozen := build()
	if err := frozen.ValidateOrdering(); err != nil {
		t.Fatalf("ValidateOrdering: %v", err)
	}

	for _, at := range []HookPoint{HookPreRequest, HookPostResponse, HookPreToolExec, HookPostToolResult, HookHistoryRewrite} {
		want := orderedIDs(onTheFly.Ordered(at))
		if got := orderedIDs(frozen.Ordered(at)); !reflect.DeepEqual(got, want) {
			t.Errorf("Ordered(%s): frozen = %v, computed on the fly = %v", at, got, want)
		}
	}
}

// TestOrdered_AddAfterValidationDropsTheFrozenOrder covers the invalidation half: a registry that
// gains a Mechanism after a gate froze its order must never serve the stale one, and the next gate
// freezes the new order.
func TestOrdered_AddAfterValidationDropsTheFrozenOrder(t *testing.T) {
	r := registerAll(t, regd("b"), regd("d"))
	if err := r.ValidateOrdering(); err != nil {
		t.Fatalf("ValidateOrdering: %v", err)
	}
	if got := orderedIDs(r.Ordered(HookPreRequest)); !reflect.DeepEqual(got, []MechanismID{"b", "d"}) {
		t.Fatalf("Ordered after validation = %v, want [b d]", got)
	}

	if err := r.Add(regd("c")); err != nil {
		t.Fatalf("Add(c): %v", err)
	}
	want := []MechanismID{"b", "c", "d"}
	if got := orderedIDs(r.Ordered(HookPreRequest)); !reflect.DeepEqual(got, want) {
		t.Errorf("Ordered after a post-validation Add = %v, want %v", got, want)
	}
	if err := r.ValidateRequirements(); err != nil {
		t.Fatalf("ValidateRequirements: %v", err)
	}
	if got := orderedIDs(r.Ordered(HookPreRequest)); !reflect.DeepEqual(got, want) {
		t.Errorf("Ordered after re-validation = %v, want %v", got, want)
	}

	// AddExperimental drops the frozen order too — it cannot change the catalogued order, so
	// what this pins is that recomputing it yields the same answer.
	if err := r.AddExperimental(HookPreRequest, preReqMech{}); err != nil {
		t.Fatalf("AddExperimental: %v", err)
	}
	if got := orderedIDs(r.Ordered(HookPreRequest)); !reflect.DeepEqual(got, want) {
		t.Errorf("Ordered after AddExperimental = %v, want %v", got, want)
	}
}

func TestValidateIncompatibilities(t *testing.T) {
	t.Run("both registered ⇒ error", func(t *testing.T) {
		r := registerAll(t,
			regd("read_loop", incompatibleWith("cached_content_intercept")),
			regd("cached_content_intercept"),
		)
		if err := r.ValidateIncompatibilities(); !errors.Is(err, ErrIncompatibleMechanisms) {
			t.Errorf("ValidateIncompatibilities = %v, want ErrIncompatibleMechanisms", err)
		}
	})

	t.Run("declaration is symmetric in effect", func(t *testing.T) {
		// Only the SECOND mechanism declares the incompatibility; it must still trip.
		r := registerAll(t,
			regd("read_loop"),
			regd("cached_content_intercept", incompatibleWith("read_loop")),
		)
		if err := r.ValidateIncompatibilities(); !errors.Is(err, ErrIncompatibleMechanisms) {
			t.Errorf("ValidateIncompatibilities = %v, want ErrIncompatibleMechanisms", err)
		}
	})

	t.Run("only one side registered ⇒ ok", func(t *testing.T) {
		r := registerAll(t,
			regd("read_loop", incompatibleWith("cached_content_intercept")),
		)
		if err := r.ValidateIncompatibilities(); err != nil {
			t.Errorf("ValidateIncompatibilities = %v, want nil (the peer is not registered)", err)
		}
	})

	t.Run("compatible set ⇒ ok", func(t *testing.T) {
		r := registerAll(t,
			regd("a"),
			regd("b"),
		)
		if err := r.ValidateIncompatibilities(); err != nil {
			t.Errorf("ValidateIncompatibilities = %v, want nil", err)
		}
	})
}

func TestValidateRequirements(t *testing.T) {
	t.Run("required peer absent ⇒ error naming both IDs", func(t *testing.T) {
		r := registerAll(t,
			regd("guided_decomposition", requires("tool_result_cap")),
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
			regd("guided_decomposition", requires("tool_result_cap")),
			regd("tool_result_cap"),
		)
		if err := r.ValidateRequirements(); err != nil {
			t.Errorf("ValidateRequirements = %v, want nil (the required peer is registered)", err)
		}
	})

	t.Run("empty Requires ⇒ ok", func(t *testing.T) {
		r := registerAll(t,
			regd("a"),
			regd("b"),
		)
		if err := r.ValidateRequirements(); err != nil {
			t.Errorf("ValidateRequirements = %v, want nil (no requirements declared)", err)
		}
	})

	t.Run("requirement chain A→B→C all present ⇒ ok (transitive by iteration)", func(t *testing.T) {
		r := registerAll(t,
			regd("a", requires("b")),
			regd("b", requires("c")),
			regd("c"),
		)
		if err := r.ValidateRequirements(); err != nil {
			t.Errorf("ValidateRequirements = %v, want nil (whole chain registered)", err)
		}
	})

	t.Run("requirement chain with a missing link ⇒ error (the broken link, not the head)", func(t *testing.T) {
		// A→B→C but C is absent: iterating every Mechanism's direct requirements catches the
		// B→C break independently of A, so no recursion is needed.
		r := registerAll(t,
			regd("a", requires("b")),
			regd("b", requires("c")),
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
