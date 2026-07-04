package mechanisms

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/library"
)

// fakeMechanism is a minimal catalogued Mechanism for exercising the constructor table while the
// production catalogue is still empty (waves 5–14 fill it). It implements one hook interface
// (pre-request) so it is a valid Mechanism the registry would accept.
type fakeMechanism struct {
	id   domain.MechanismID
	deps Deps
}

func (f fakeMechanism) Descriptor() domain.MechanismDescriptor {
	return domain.MechanismDescriptor{ID: f.id, Capability: domain.CapProactiveNudge, Suppression: domain.SuppressStrikesThree}
}
func (f fakeMechanism) Ordering() domain.OrderingConstraints              { return domain.OrderingConstraints{} }
func (f fakeMechanism) PreRequest(context.Context, *domain.Request) error { return nil }

// A fake row in an explicit table builds and receives the injected Deps — the seam every real
// wave row will use.
func TestBuildFromKnownIDInjectsDeps(t *testing.T) {
	t.Parallel()
	const id domain.MechanismID = "fake"
	marker := library.NewStore(t.TempDir())
	table := map[domain.MechanismID]constructor{
		id: func(d Deps) (domain.Mechanism, error) { return fakeMechanism{id: id, deps: d}, nil },
	}

	m, err := buildFrom(table, id, Deps{Library: marker})
	if err != nil {
		t.Fatalf("buildFrom(%q): %v", id, err)
	}
	fake, ok := m.(fakeMechanism)
	if !ok {
		t.Fatalf("built mechanism is %T; want fakeMechanism", m)
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
	table := map[domain.MechanismID]constructor{
		"beta":  func(Deps) (domain.Mechanism, error) { return fakeMechanism{id: "beta"}, nil },
		"alpha": func(Deps) (domain.Mechanism, error) { return fakeMechanism{id: "alpha"}, nil },
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
	table := map[domain.MechanismID]constructor{
		"needs-deps": func(Deps) (domain.Mechanism, error) { return nil, boom },
	}
	_, err := buildFrom(table, "needs-deps", Deps{})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v; want it to wrap the constructor error", err)
	}
}

// The production catalogue carries the ported Mechanisms and only those: Wave 1 registered
// validate/syntax/autofix (item 5) and the empty_response_recovery/tool_use_enforcer off-ramps
// (item 6), Wave 2 added the truncate_history history-rewrite (item 7), item 9 added the
// tool_result_cap pre-request capping Mechanism, Wave 3 added the toolfilter/filehint/grammar
// request shapers (item 10) and the error_enrichment/read_loop/read_repeat/tool_loop_interceptor/
// cached_content_intercept history-aware family (item 11), Wave 4 added the decompose request
// shaper plus the stall_nudge/list_nudge/tool_use_directive completion nudges (item 12), and item 14
// added the library observe/inject Mechanism, so each is buildable and KnownIDs reports it, while a
// deferred / un-ported ID is still an unknown-ID error. Later waves add rows the same way.
func TestProductionCatalogueHasPortedWaves(t *testing.T) {
	t.Parallel()
	known := make(map[domain.MechanismID]bool)
	for _, id := range KnownIDs() {
		known[id] = true
	}
	// Every ported Mechanism that builds with no injected Deps.
	for _, want := range []domain.MechanismID{"validate", "syntax", "autofix", "empty_response_recovery", "tool_use_enforcer", "truncate_history", "tool_result_cap", "toolfilter", "filehint", "grammar", "error_enrichment", "read_loop", "read_repeat", "tool_loop_interceptor", "cached_content_intercept", "decompose", "stall_nudge", "list_nudge", "tool_use_directive"} {
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

// knownList renders "(none)" for the empty catalogue rather than a dangling tail.
func TestKnownListEmptyRendersNone(t *testing.T) {
	t.Parallel()
	if got := knownList(map[domain.MechanismID]constructor{}); got != "(none)" {
		t.Errorf("knownList(empty) = %q; want %q", got, "(none)")
	}
}
