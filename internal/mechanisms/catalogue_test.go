package mechanisms

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/library"
)

// fakeMechanism is a minimal Mechanism HOOK for exercising the catalogue table independently of
// the real rows. It implements one hook interface (pre-request), so a row carrying it is one the
// registry would accept; its metadata comes from the row it sits in, never from the value.
type fakeMechanism struct {
	id   domain.MechanismID
	deps Deps
}

func (f fakeMechanism) PreRequest(context.Context, *domain.Request) error { return nil }

// A fake row in an explicit table builds and receives the injected Deps — the seam every real
// wave row will use.
func TestBuildFromKnownIDInjectsDeps(t *testing.T) {
	t.Parallel()
	const id domain.MechanismID = "fake"
	marker := library.NewStore(t.TempDir())
	table := map[domain.MechanismID]row{
		id: {
			descriptor: domain.MechanismDescriptor{ID: id},
			construct:  func(d Deps) (any, error) { return fakeMechanism{id: id, deps: d}, nil },
		},
	}

	m, err := buildFrom(table, id, Deps{Library: marker})
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
	if fake.deps.Library != marker {
		t.Error("Deps were not injected into the constructor")
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
func TestBuildFromClonesDescriptorAndOrderingSlices(t *testing.T) {
	t.Parallel()
	const id domain.MechanismID = "fake"
	table := map[domain.MechanismID]row{
		id: {
			descriptor: domain.MechanismDescriptor{
				ID:               id,
				IncompatibleWith: []domain.MechanismID{"rival"},
				Requires:         []domain.MechanismID{"prereq"},
			},
			ordering:  domain.OrderingConstraints{Before: []domain.MechanismID{"later"}, After: []domain.MechanismID{"earlier"}},
			construct: func(Deps) (any, error) { return fakeMechanism{id: id}, nil },
		},
	}

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

// The production catalogue carries the ported Mechanisms and only those: Wave 1 registered
// syntax/autofix (item 5), Wave 2 added the truncate_history history-rewrite (item 7), item 9 added the
// tool_result_cap pre-request capping Mechanism, Wave 3 added the toolfilter/filehint
// request shapers (item 10) and the error_enrichment/read_loop/read_repeat/
// cached_content_intercept history-aware family (item 11), Wave 4 added the decompose request
// shaper plus the stall_nudge/list_nudge/tool_use_directive completion nudges (item 12), and item 14
// added the library observe/inject Mechanism, so each is buildable and KnownIDs reports it, while a
// deferred / un-ported ID is still an unknown-ID error. The tool-call validator, the identical-repeat
// detector and the two Wave-1 recoveries (item 6) are NOT here: they were promoted to Floor guards
// (ADR 0071) and are on the retired roll, so a `mechanisms:` key naming one is tolerated, never built.
func TestProductionCatalogueHasPortedWaves(t *testing.T) {
	t.Parallel()
	known := make(map[domain.MechanismID]bool)
	for _, id := range KnownIDs() {
		known[id] = true
	}
	// Every ported Mechanism that builds with no injected Deps.
	for _, want := range []domain.MechanismID{"syntax", "autofix", "truncate_history", "tool_result_cap", "toolfilter", "filehint", "error_enrichment", "read_loop", "read_repeat", "cached_content_intercept", "decompose", "stall_nudge", "list_nudge", "tool_use_directive"} {
		if !known[want] {
			t.Errorf("KnownIDs() missing the ported Mechanism %q; got %v", want, KnownIDs())
		}
		if _, err := Build(want, Deps{}); err != nil {
			t.Errorf("Build(%q): %v", want, err)
		}
	}
	// library (item 14) is ported and known, but it needs the Library store injected (D3): Build with
	// no store is a loud construction error, Build WITH a store succeeds.
	if !known["library"] {
		t.Errorf("KnownIDs() missing the ported Mechanism %q; got %v", "library", KnownIDs())
	}
	if _, err := Build("library", Deps{}); err == nil {
		t.Error(`Build("library", Deps{}): want a construction error for the missing Library store, got nil`)
	}
	if _, err := Build("library", Deps{Library: library.NewStore(t.TempDir())}); err != nil {
		t.Errorf(`Build("library", store): %v`, err)
	}
	// correct_tool_result is DEFERRED (owner-ratified) — never a catalogue row — so it is still an
	// unknown-ID error, proving a deferred / un-ported ID does not silently build.
	if _, err := Build("correct_tool_result", Deps{}); err == nil {
		t.Error("Build of a deferred/un-ported ID: want an unknown-ID error, got nil")
	}
}

// TestPreRequestOrderingSeeds pins the pre-request dispatch order the §Ordering seeds declare
// (review-fixes item 11 / option A, ratified into Table A 2026-07-04): the cot nudges and library
// inject before toolfilter, toolfilter before decompose, and tool_result_cap runs last among the
// pre-request shapers. It builds the REAL Mechanisms and topo-sorts them through the registry, so a
// future rename or a dropped Before/After edge fails loudly here — the finding this item closes was
// that the seeds lived only in catalogue prose, not in the code.
func TestPreRequestOrderingSeeds(t *testing.T) {
	t.Parallel()
	deps := Deps{Library: library.NewStore(t.TempDir())}
	// Every pre-request Mechanism, including the unordered request-prep injectors (filehint/read_loop),
	// so the pin reflects the production registry. stall_nudge and list_nudge are
	// IncompatibleWith each other and never co-enabled in production, but Ordered is a pure topo-sort
	// that does not gate on incompatibility, so registering both here only exercises their shared
	// Before edge.
	ids := []domain.MechanismID{
		"toolfilter", "decompose", "tool_result_cap", "guided_decomposition",
		"stall_nudge", "list_nudge", "tool_use_directive", "library",
		"filehint", "read_loop",
	}
	reg := domain.NewMechanismRegistry()
	built := make(map[domain.MechanismID]domain.RegisteredMechanism, len(ids))
	for _, id := range ids {
		m, err := Build(id, deps)
		if err != nil {
			t.Fatalf("Build(%q): %v", id, err)
		}
		built[id] = m
		if err := reg.Add(m); err != nil {
			t.Fatalf("Add(%q): %v", id, err)
		}
	}

	ordered := reg.Ordered(domain.HookPreRequest)
	if len(ordered) != len(ids) {
		t.Fatalf("Ordered(pre-request) returned %d Mechanisms, want %d", len(ordered), len(ids))
	}
	pos := make(map[domain.MechanismID]int, len(ordered))
	for i, m := range ordered {
		pos[m.Descriptor.ID] = i
	}

	// Every cot nudge and library injects before toolfilter narrows the menu — assert each one
	// DECLARES its Before-toolfilter edge, not merely that it sorts ahead of toolfilter. Under the
	// D4 stable-ID tiebreak these four canonical IDs already sort before "toolfilter" even with the
	// edge dropped, so an emergent-position check passes vacuously and would not catch an
	// accidentally-deleted edge; inspecting the declared Ordering guards each edge independently.
	for _, before := range []domain.MechanismID{"stall_nudge", "list_nudge", "tool_use_directive", "library"} {
		if !slices.Contains(built[before].Ordering.Before, "toolfilter") {
			t.Errorf("%s does not declare Before toolfilter (Ordering = %+v)", before, built[before].Ordering)
		}
	}
	// The transform chain: toolfilter before decompose before tool_result_cap.
	if !(pos["toolfilter"] < pos["decompose"] && pos["decompose"] < pos["tool_result_cap"]) {
		t.Errorf("want toolfilter@%d < decompose@%d < tool_result_cap@%d",
			pos["toolfilter"], pos["decompose"], pos["tool_result_cap"])
	}
	// guided_decomposition declares After toolfilter (its sub_agent-presence gate must read the final,
	// post-toolfilter menu) — assert the DECLARED edge, not merely that it sorts after toolfilter, and
	// that it lands after the narrowing yet before the trailing tool_result_cap.
	if !slices.Contains(built["guided_decomposition"].Ordering.After, "toolfilter") {
		t.Errorf("guided_decomposition does not declare After toolfilter (Ordering = %+v)",
			built["guided_decomposition"].Ordering)
	}
	if !(pos["toolfilter"] < pos["guided_decomposition"] && pos["guided_decomposition"] < pos["tool_result_cap"]) {
		t.Errorf("want toolfilter@%d < guided_decomposition@%d < tool_result_cap@%d",
			pos["toolfilter"], pos["guided_decomposition"], pos["tool_result_cap"])
	}
	// tool_result_cap runs last among the pre-request shapers (§Ordering: it trims after context is
	// assembled), which here means the final position overall — the injectors are in-degree-0 and
	// emit early, so nothing sorts after tool_result_cap.
	if last := ordered[len(ordered)-1].Descriptor.ID; last != "tool_result_cap" {
		t.Errorf("last pre-request Mechanism = %q, want tool_result_cap (runs last among shapers)", last)
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
		// pre-build metadata query and the post-build registry agree. library needs its store
		// injected (D3, catalogue_test fake-Deps pattern); every other Mechanism builds with benign
		// zero Deps.
		deps := Deps{}
		if id == libraryID {
			deps = Deps{Library: library.NewStore(t.TempDir())}
		}
		m, err := Build(id, deps)
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
	// syntax is catalogued, so the error still names the known IDs.
	if got := err.Error(); !strings.Contains(got, "syntax") {
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

// DepsNeeded answers the engine's "what must I derive for this arm?" from the rows themselves, so
// the build path carries no Mechanism ID literal: an arm of rows that declare nothing needs nothing
// derived, and one containing `library` — the single row declaring needs — asks for the store (and
// the Fingerprint it keys on). An ID absent from the catalogue contributes nothing rather than
// failing here: Build is the one place an unknown ID is reported, loudly, a moment later.
func TestDepsNeeded(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		ids  []domain.MechanismID
		want DepNeeds
	}{
		"no mechanisms enabled": {ids: nil, want: DepNeeds{}},
		"a row declaring no needs": {
			ids:  []domain.MechanismID{"syntax"},
			want: DepNeeds{},
		},
		"library declares the store": {
			ids:  []domain.MechanismID{"syntax", "library"},
			want: DepNeeds{Library: true},
		},
		"an uncatalogued ID is skipped": {
			ids:  []domain.MechanismID{"not_a_real_mechanism"},
			want: DepNeeds{},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := DepsNeeded(tc.ids); got != tc.want {
				t.Errorf("DepsNeeded(%v) = %+v; want %+v", tc.ids, got, tc.want)
			}
		})
	}
}

// Every catalogued Mechanism held by POINTER declares how it scopes to a delegated sub-agent
// (domain.SubAgentScoped). This is the catalogue's half of the fan-out safety rule (ADR 0039):
// siblings in a depth-0 fan-out run at once, so a hook instance reached from two children at once
// must either carry no per-run state or hand each child its own. A VALUE hook is exempt by
// construction — its methods take value receivers, so a fire cannot mutate anything a sibling can
// observe, and what a value hook does hold (autofix's resolved formatter table) is read-only after
// construction, the same standing the dangerous-action floor has when
// Guards.ForSubAgent shares IT by pointer. Holding the hook by pointer is exactly the shape
// per-instance state requires, so that is where the declaration is demanded: a new stateful
// Mechanism fails this test on the day it is written rather than racing on the day it is armed
// beside a fan-out.
func TestCatalogueHooksDeclareTheirSubAgentScope(t *testing.T) {
	t.Parallel()
	for _, id := range KnownIDs() {
		// library needs its store injected (D3); every other Mechanism builds with zero Deps.
		deps := Deps{}
		if id == libraryID {
			deps = Deps{Library: library.NewStore(t.TempDir())}
		}
		m, err := Build(id, deps)
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
