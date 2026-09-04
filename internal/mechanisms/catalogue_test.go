package mechanisms

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// fakeMechanism is a minimal Mechanism HOOK for exercising the catalogue table independently of
// the real rows. It implements one hook interface (pre-request), so a row carrying it is one the
// registry would accept; its metadata comes from the row it sits in, never from the value.
type fakeMechanism struct {
	id   domain.MechanismID
	deps Deps
}

func (f fakeMechanism) PreRequest(context.Context, *domain.Request) error { return nil }

// A fake row in an explicit table builds and receives the injected Deps — the seam an
// experimental Mechanism uses. Deps is EMPTY since `library` retired with the store it read
// (v0.20.0, ADR 0071), so what this pins today is that the constructor is handed whatever the
// caller passed: a Deps field added for a lab row reaches the row that declared it.
func TestBuildFromKnownIDInjectsDeps(t *testing.T) {
	t.Parallel()
	const id domain.MechanismID = "fake"
	table := map[domain.MechanismID]row{
		id: {
			descriptor: domain.MechanismDescriptor{ID: id},
			construct:  func(d Deps) (any, error) { return fakeMechanism{id: id, deps: d}, nil },
		},
	}

	m, err := buildFrom(table, id, Deps{})
	if err != nil {
		t.Fatalf("buildFrom(%q): %v", id, err)
	}
	if m.Descriptor.ID != id {
		t.Errorf("built row descriptor ID = %q; want %q", m.Descriptor.ID, id)
	}
	fake, ok := m.Hook.(fakeMechanism)
	if !ok {
		t.Fatalf("built hook is %T; want fakeMechanism", m.Hook)
	}
	if fake.id != id {
		t.Errorf("built ID = %q; want %q", fake.id, id)
	}
	if fake.deps != (Deps{}) {
		t.Error("the constructor was handed Deps other than the ones passed")
	}
}

// An unknown ID is a loud error that names the known catalogue, so a typo'd config key fails
// startup rather than silently disabling a Mechanism.
func TestBuildFromUnknownIDErrorsListingKnown(t *testing.T) {
	t.Parallel()
	table := map[domain.MechanismID]row{
		"beta": {
			descriptor: domain.MechanismDescriptor{ID: "beta"},
			construct:  func(Deps) (any, error) { return fakeMechanism{id: "beta"}, nil },
		},
		"alpha": {
			descriptor: domain.MechanismDescriptor{ID: "alpha"},
			construct:  func(Deps) (any, error) { return fakeMechanism{id: "alpha"}, nil },
		},
	}

	_, err := buildFrom(table, "nope", Deps{})
	if err == nil {
		t.Fatal("unknown mechanism ID: want an error, got nil")
	}
	// The message lists the known IDs sorted by canonical spelling (deterministic).
	if got := err.Error(); !strings.Contains(got, "alpha, beta") {
		t.Errorf("error %q; want it to list the known IDs %q", got, "alpha, beta")
	}
}

// A constructor that fails propagates its error (a Mechanism that cannot be built with the given
// Deps fails loudly, not half-built).
func TestBuildFromConstructorErrorPropagates(t *testing.T) {
	t.Parallel()
	boom := errors.New("missing collaborator")
	table := map[domain.MechanismID]row{
		"needs-deps": {
			descriptor: domain.MechanismDescriptor{ID: "needs-deps"},
			construct:  func(Deps) (any, error) { return nil, boom },
		},
	}
	_, err := buildFrom(table, "needs-deps", Deps{})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v; want it to wrap the constructor error", err)
	}
}

// What buildFrom hands out carries CLONED metadata slices, exactly as Descriptors() does: a caller
// that mutates a built Mechanism's IncompatibleWith/Requires or Before/After cannot reach back
// through the aliased slice header into the static catalogue row. Without the clone the two builds
// below would see each other's writes.
//
// It is registered through registerIn over a table of its own, so the row it asserts on carries both
// Requires and IncompatibleWith non-empty whatever the shipped catalogue holds. That is why the
// clone contract lives HERE rather than at the root: apogee.CataloguedMechanisms() is
// Descriptors() over the production catalogue, which returns no synthetic row, so the root's old
// assertion could only ever be as strong as whichever catalogued row happened to declare an edge.
func TestBuildFromClonesDescriptorAndOrderingSlices(t *testing.T) {
	t.Parallel()
	const id domain.MechanismID = "fake"
	table := map[domain.MechanismID]row{}
	registerIn(table, row{
		descriptor: domain.MechanismDescriptor{
			ID:               id,
			IncompatibleWith: []domain.MechanismID{"rival"},
			Requires:         []domain.MechanismID{"prereq"},
		},
		ordering:  domain.OrderingConstraints{Before: []domain.MechanismID{"later"}, After: []domain.MechanismID{"earlier"}},
		construct: func(Deps) (any, error) { return fakeMechanism{id: id}, nil },
	})

	built, err := buildFrom(table, id, Deps{})
	if err != nil {
		t.Fatalf("buildFrom(%q): %v", id, err)
	}
	built.Descriptor.IncompatibleWith[0] = "tampered"
	built.Descriptor.Requires[0] = "tampered"
	built.Ordering.Before[0] = "tampered"
	built.Ordering.After[0] = "tampered"

	stored := table[id]
	if got := stored.descriptor.IncompatibleWith; !slices.Equal(got, []domain.MechanismID{"rival"}) {
		t.Errorf("catalogue row IncompatibleWith = %v; want it untouched by the caller's mutation", got)
	}
	if got := stored.descriptor.Requires; !slices.Equal(got, []domain.MechanismID{"prereq"}) {
		t.Errorf("catalogue row Requires = %v; want it untouched by the caller's mutation", got)
	}
	if got := stored.ordering.Before; !slices.Equal(got, []domain.MechanismID{"later"}) {
		t.Errorf("catalogue row Ordering.Before = %v; want it untouched by the caller's mutation", got)
	}
	if got := stored.ordering.After; !slices.Equal(got, []domain.MechanismID{"earlier"}) {
		t.Errorf("catalogue row Ordering.After = %v; want it untouched by the caller's mutation", got)
	}

	// A second build is unaffected too — the row is still pristine, so every caller gets the
	// declared metadata rather than the previous caller's edits.
	second, err := buildFrom(table, id, Deps{})
	if err != nil {
		t.Fatalf("buildFrom(%q) second call: %v", id, err)
	}
	if got := second.Descriptor.IncompatibleWith; !slices.Equal(got, []domain.MechanismID{"rival"}) {
		t.Errorf("second build IncompatibleWith = %v; want %v", got, []domain.MechanismID{"rival"})
	}
	if got := second.Ordering.Before; !slices.Equal(got, []domain.MechanismID{"later"}) {
		t.Errorf("second build Ordering.Before = %v; want %v", got, []domain.MechanismID{"later"})
	}
}

// The SHIPPED catalogue is EMPTY, and that is the end state rather than a stage on the way to one:
// on ADR 0071's ratified verdict every row this build ever carried left in v0.20.0 — six PROMOTED to
// Floor guards, plain engine behaviour with config keys of their own, and fourteen retired outright
// — so the whole of the former roster is on the retired roll and none of it is buildable. That makes
// this the invariant every empty-catalogue restatement elsewhere rests on: a `mechanisms:` key
// naming a former row is tolerated and arms nothing, an ID on neither list is still a loud unknown-ID
// error, and a row that reappears here is a LAB row a Driver registered rather than a shipped one.
//
// KnownIDs() answers empty but NON-NIL, which is the shape every caller composing a list off it
// depends on (the `/settings` sub-list, the config surface's valid-keys tail): a nil would be the
// same length and a different thing to a caller that ranges, appends and reports what it built.
func TestProductionCatalogueIsEmpty(t *testing.T) {
	t.Parallel()

	known := KnownIDs()
	if known == nil {
		t.Error("KnownIDs() = nil; an empty catalogue is an empty SLICE, not the absence of one")
	}
	if len(known) != 0 {
		t.Errorf("KnownIDs() = %v; the shipped catalogue emptied in v0.20.0 (ADR 0071)", known)
	}
	if rows := Descriptors(); len(rows) != 0 {
		t.Errorf("Descriptors() = %+v; an empty catalogue harvests no descriptor", rows)
	}

	// Nothing that left is buildable, and nothing buildable is on the roll: the two lists partition
	// every ID this build recognises, so a row can never be tolerated AND armed.
	roll := RetiredIDs()
	if len(roll) == 0 {
		t.Fatal("the retired roll is empty; every ID the catalogue ever carried has to stay on it so a saved config still starts")
	}
	for _, id := range roll {
		if _, err := Build(id, Deps{}); err == nil {
			t.Errorf("Build(%q) succeeded; a retired ID is tolerated by the resolver, never built", id)
		}
	}
	for _, id := range known {
		if IsRetired(id) {
			t.Errorf("%q is both catalogued and retired; the two lists must not overlap", id)
		}
	}

	// correct_tool_result is DEFERRED (owner-ratified) — never a catalogue row and never on the roll —
	// so it is still an unknown-ID error, proving a deferred / un-ported ID does not silently build.
	if _, err := Build("correct_tool_result", Deps{}); err == nil {
		t.Error("Build of a deferred/un-ported ID: want an unknown-ID error, got nil")
	}
}

// Every catalogued Mechanism has a static descriptor row keyed by its own ID, and Descriptors() is
// sorted and duplicate-free — the contract the public CataloguedMechanisms() query (ADR 0015 §3)
// rests on. The drift this test once guarded — a built instance's Descriptor() disagreeing with its
// static row — is now UNREPRESENTABLE: the row registered in the Mechanism's init() is the single
// source of its metadata, and Build joins it to the hook once, so there is no second copy to
// disagree.
func TestDescriptorsMatchCatalogue(t *testing.T) {
	t.Parallel()

	rows := Descriptors()

	// Descriptors() is sorted by canonical ID and duplicate-free.
	seen := make(map[domain.MechanismID]bool, len(rows))
	for i, d := range rows {
		if seen[d.ID] {
			t.Errorf("Descriptors() lists %q more than once", d.ID)
		}
		seen[d.ID] = true
		if i > 0 && rows[i-1].ID >= d.ID {
			t.Errorf("Descriptors() not sorted: %q appears before %q", rows[i-1].ID, d.ID)
		}
	}

	// Exactly one row per KnownIDs() entry, each keyed by its own ID.
	byID := make(map[domain.MechanismID]domain.MechanismDescriptor, len(rows))
	for _, d := range rows {
		byID[d.ID] = d
	}
	known := KnownIDs()
	if len(rows) != len(known) {
		t.Errorf("Descriptors() has %d rows; KnownIDs() has %d", len(rows), len(known))
	}
	for _, id := range known {
		row, ok := byID[id]
		if !ok {
			t.Errorf("KnownIDs() entry %q has no descriptor row", id)
			continue
		}
		if row.ID != id {
			t.Errorf("descriptor row for catalogue key %q has ID %q", id, row.ID)
		}
		// What Build hands the registry carries the same descriptor Descriptors() harvested, so the
		// pre-build metadata query and the post-build registry agree. Every Mechanism builds with
		// zero Deps since `library` — the one row that ever declared a need — retired (v0.20.0).
		m, err := Build(id, Deps{})
		if err != nil {
			t.Errorf("Build(%q): %v", id, err)
			continue
		}
		if got := m.Descriptor; !reflect.DeepEqual(got, row) {
			t.Errorf("Build(%q).Descriptor = %+v; want its static row %+v", id, got, row)
		}
	}
}

// A bogus Build ID wraps domain.ErrUnknownMechanism (matchable with errors.Is) while its message
// still names the known catalogue IDs — a typo'd config key fails loudly AND programmatically.
func TestBuildUnknownIDWrapsSentinel(t *testing.T) {
	t.Parallel()
	_, err := Build("definitely_not_a_mechanism", Deps{})
	if !errors.Is(err, domain.ErrUnknownMechanism) {
		t.Fatalf("Build(bogus) err = %v; want it to wrap domain.ErrUnknownMechanism", err)
	}
	// The shipped catalogue is empty (v0.20.0), so the known-IDs tail renders "(none)" rather than
	// dangling — the message still tells the user what the valid keys are.
	if got := err.Error(); !strings.Contains(got, "(none)") {
		t.Errorf("error %q; want it to name the known IDs", got)
	}
}

// knownList renders "(none)" for the empty catalogue rather than a dangling tail.
func TestKnownListEmptyRendersNone(t *testing.T) {
	t.Parallel()
	if got := knownList(map[domain.MechanismID]row{}); got != "(none)" {
		t.Errorf("knownList(empty) = %q; want %q", got, "(none)")
	}
}

// register is the only way a row enters the catalogue, and it refuses the two ways a row can be
// mis-written: an empty descriptor ID (the row would be filed under "", and Build could never find
// it) and an ID that is already registered (the second row would silently displace the first).
// Both are init()-time programming errors inside this package, so register PANICS rather than
// returning an error no init() could act on. Driven against a local table so the production
// catalogue is untouched.
func TestRegisterRejectsDuplicateAndEmptyID(t *testing.T) {
	t.Parallel()

	fakeRow := func(id domain.MechanismID) row {
		return row{
			descriptor: domain.MechanismDescriptor{ID: id},
			construct:  func(Deps) (any, error) { return fakeMechanism{id: id}, nil },
		}
	}
	panics := func(f func()) (panicked bool) {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		f()
		return false
	}

	table := map[domain.MechanismID]row{}
	if panics(func() { registerIn(table, fakeRow("alpha")) }) {
		t.Fatal("registerIn(fresh ID) panicked; want the row registered")
	}
	if _, ok := table["alpha"]; !ok {
		t.Fatal(`registerIn did not file the row under its descriptor ID "alpha"`)
	}
	if !panics(func() { registerIn(table, fakeRow("alpha")) }) {
		t.Error("registerIn(duplicate ID): want a panic, got none")
	}
	if !panics(func() { registerIn(table, fakeRow("")) }) {
		t.Error("registerIn(empty descriptor ID): want a panic, got none")
	}
	if len(table) != 1 {
		t.Errorf("table holds %d rows; want only the first accepted row", len(table))
	}
}

// Every catalogued Mechanism held by POINTER declares how it scopes to a delegated sub-agent
// (domain.SubAgentScoped). This is the catalogue's half of the fan-out safety rule (ADR 0039):
// siblings in a depth-0 fan-out run at once, so a hook instance reached from two children at once
// must either carry no per-run state or hand each child its own. A VALUE hook is exempt by
// construction — its methods take value receivers, so a fire cannot mutate anything a sibling can
// observe, and what a value hook does hold is read-only after
// construction, the same standing the dangerous-action floor has when
// Guards.ForSubAgent shares IT by pointer. Holding the hook by pointer is exactly the shape
// per-instance state requires, so that is where the declaration is demanded: a new stateful
// Mechanism fails this test on the day it is written rather than racing on the day it is armed
// beside a fan-out.
func TestCatalogueHooksDeclareTheirSubAgentScope(t *testing.T) {
	t.Parallel()
	for _, id := range KnownIDs() {
		m, err := Build(id, Deps{})
		if err != nil {
			t.Errorf("Build(%q): %v", id, err)
			continue
		}
		if reflect.ValueOf(m.Hook).Kind() != reflect.Pointer {
			continue // a value hook cannot mutate shared state through a value receiver
		}
		scoped, ok := m.Hook.(domain.SubAgentScoped)
		if !ok {
			t.Errorf("Mechanism %q is held by pointer but declares no domain.SubAgentScoped: "+
				"a pointer hook is shared with every sub-agent, and depth-0 siblings run it at once — "+
				"say whether the child gets its own instance or shares this one", id)
			continue
		}
		if child := scoped.ForSubAgent(); child == nil {
			t.Errorf("Mechanism %q: ForSubAgent() returned nil; a child must get a runnable hook", id)
		}
	}
}

// SwapCatalogue is the one seam a caller outside this package has on the curated table, and what it
// buys is that a swapped row is visible through the SHIPPED reading surface — KnownIDs(),
// Descriptors() and Build() all read the package var, so a Driver's lab row reaches the config ->
// EnableMechanisms -> engine-build path exactly as a catalogued row would. The restore closure puts
// the empty shipped catalogue (ADR 0071) back, so the swap cannot leak into another test.
//
// No t.Parallel(): this test assigns a package-level variable, and the seam is deliberately not
// concurrency-safe. Go runs the sequential tests to completion while the parallel ones are paused,
// which is what keeps a swapped table invisible to TestProductionCatalogueIsEmpty and its peers.
func TestSwapCatalogueExposesTheRowAndRestores(t *testing.T) {
	const id domain.MechanismID = "lab_row"

	restore := SwapCatalogue([]Row{{
		Descriptor: domain.MechanismDescriptor{ID: id, Capability: domain.CapProactiveNudge},
		Ordering:   domain.OrderingConstraints{After: []domain.MechanismID{"earlier"}},
		Construct:  func(d Deps) (any, error) { return fakeMechanism{id: id, deps: d}, nil },
	}})

	if known := KnownIDs(); !slices.Equal(known, []domain.MechanismID{id}) {
		t.Errorf("KnownIDs() = %v; want the swapped row %q", known, id)
	}
	descs := Descriptors()
	if len(descs) != 1 || descs[0].ID != id {
		t.Fatalf("Descriptors() = %+v; want the swapped row's descriptor", descs)
	}
	if descs[0].Capability != domain.CapProactiveNudge {
		t.Errorf("the swapped row's descriptor Capability = %q; Descriptors() harvests the row it was given", descs[0].Capability)
	}
	m, err := Build(id, Deps{})
	if err != nil {
		t.Fatalf("Build(%q) against the swapped catalogue: %v", id, err)
	}
	if _, ok := m.Hook.(fakeMechanism); !ok {
		t.Errorf("Build(%q) hook is %T; want the swapped row's fakeMechanism", id, m.Hook)
	}
	if !slices.Equal(m.Ordering.After, []domain.MechanismID{"earlier"}) {
		t.Errorf("Build(%q) Ordering.After = %v; want the swapped row's edge", id, m.Ordering.After)
	}

	restore()

	if known := KnownIDs(); len(known) != 0 {
		t.Errorf("after restore KnownIDs() = %v; want the empty shipped catalogue back", known)
	}
	if rows := Descriptors(); len(rows) != 0 {
		t.Errorf("after restore Descriptors() = %+v; want the empty shipped catalogue back", rows)
	}
	if _, err := Build(id, Deps{}); !errors.Is(err, domain.ErrUnknownMechanism) {
		t.Errorf("after restore Build(%q) error = %v; want the unknown-ID sentinel", id, err)
	}
}

// Nested swaps restore in order: each closure puts back the table its own call displaced, so a test
// that swaps inside a swap (a suite-wide lab table with a per-case row on top) unwinds through
// deferred restores to exactly the state it started from.
func TestSwapCatalogueNestedRestoresInOrder(t *testing.T) {
	const outer domain.MechanismID = "outer_row"
	const inner domain.MechanismID = "inner_row"
	build := func(id domain.MechanismID) Row {
		return Row{
			Descriptor: domain.MechanismDescriptor{ID: id},
			Construct:  func(d Deps) (any, error) { return fakeMechanism{id: id, deps: d}, nil },
		}
	}

	restoreOuter := SwapCatalogue([]Row{build(outer)})
	restoreInner := SwapCatalogue([]Row{build(inner)})

	if known := KnownIDs(); !slices.Equal(known, []domain.MechanismID{inner}) {
		t.Errorf("inside the nested swap KnownIDs() = %v; want only %q", known, inner)
	}

	restoreInner()
	if known := KnownIDs(); !slices.Equal(known, []domain.MechanismID{outer}) {
		t.Errorf("after the inner restore KnownIDs() = %v; want the outer table's %q back", known, outer)
	}

	restoreOuter()
	if known := KnownIDs(); len(known) != 0 {
		t.Errorf("after the outer restore KnownIDs() = %v; want the empty shipped catalogue back", known)
	}
}

// A swapped table is registered through the same door the curated one uses, so the two programming
// errors registerIn refuses stay refused: a row with no descriptor ID, and two rows filed under the
// same ID. Both PANIC — they are mistakes in the test's own table, not runtime conditions.
func TestSwapCatalogueRejectsEmptyAndDuplicateIDs(t *testing.T) {
	good := Row{
		Descriptor: domain.MechanismDescriptor{ID: "dup_row"},
		Construct:  func(Deps) (any, error) { return fakeMechanism{id: "dup_row"}, nil },
	}
	cases := []struct {
		name string
		rows []Row
	}{
		{name: "empty ID", rows: []Row{{Construct: func(Deps) (any, error) { return fakeMechanism{}, nil }}}},
		{name: "duplicate ID", rows: []Row{good, good}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("SwapCatalogue with a %s row did not panic", tc.name)
				}
				if known := KnownIDs(); len(known) != 0 {
					t.Errorf("after the panic KnownIDs() = %v; the shipped catalogue must not have been swapped", known)
				}
			}()
			SwapCatalogue(tc.rows)
		})
	}
}
