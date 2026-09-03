package mechanisms

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// Deps are the construction-injected collaborators a catalogued Mechanism may need at BUILD
// time (D3 — a Mechanism's dependencies are injected once when it is constructed, never passed
// per hook call; hook signatures stay about conversation state). It is EMPTY: the only row that
// ever declared a need was `library`, and it retired in v0.20.0 with the store it read (ADR 0071),
// taking the DepNeeds derivation and row.needs with it.
//
// The type survives because it is the lab surface's shape (AGENTS.md: the hook API, the registry
// and `--bypass` stay for the bench). An experimental Mechanism that needs a collaborator adds a
// field here, populates it in the Driver that builds the row, and its constructor reads it — no
// caller has to construct anything today.
type Deps struct{}

// constructor builds one catalogued Mechanism's HOOK from the injected Deps (D3) — the behaviour
// only: a value implementing at least one of domain's five hook interfaces. Its metadata comes
// from the row it sits in, so the constructor returns `any` rather than a self-describing type
// (ADR 0003 as amended 2026-07-25). It returns an error so a Mechanism that cannot be built with
// the given Deps (a missing required collaborator, an invalid configuration) fails construction
// loudly rather than registering a half-built Mechanism.
type constructor func(Deps) (any, error)

// row is one catalogue entry — everything the engine needs to build and register one Mechanism.
// The row is the single source of a Mechanism's metadata; the ID is descriptor.ID, never a
// separate key, so a row can never be filed under an ID other than the one it describes.
type row struct {
	// descriptor is the Mechanism's static, harvestable metadata (ADR 0015 §3): its canonical ID,
	// what Bypass turns off, how it self-regulates, and what it may or must be stacked with.
	descriptor domain.MechanismDescriptor
	// ordering is the Mechanism's declared position relative to its peers at the same hook point
	// (ADR 0003). The zero value declares no edge, which is what most rows want, so it is omitted
	// from those rows' literals.
	ordering domain.OrderingConstraints
	// construct builds the Mechanism's hook — its behaviour — from the injected Deps (D3).
	construct constructor
}

// catalogue is the Mechanism table: canonical MechanismID → its row. It is the single registry of
// buildable Mechanisms — Build looks an ID up here, Descriptors() harvests the same rows' metadata
// without building anything, and the config surface validates an enabled `mechanisms:` key against
// its keys. The literal starts empty; each ported Mechanism adds its row with one
// `register(row{…})` call in its file's init(), beside the Mechanism's implementation. The wiring
// is also exercised independently of the real rows via buildFrom against a fake table
// (catalogue_test.go).
var catalogue = map[domain.MechanismID]row{}

// register files one Mechanism's row in the production catalogue under r.descriptor.ID — the call
// every Mechanism file makes once, from its own init().
//
// It PANICS on a row with an empty descriptor ID and on an ID that is already registered. Both are
// init()-time programming errors inside this package (a mis-written row), never runtime conditions
// a caller could handle, and the first test run catches them. register is deliberately unexported:
// the catalogue is curated (ADR 0002 / ADR 0015 §6), so there is no public way to add a Mechanism
// to it.
//
// The SHIPPED catalogue is empty since v0.20.0 (ADR 0071), so nothing calls register in this
// build; it stays as the lab surface's shape.
//
//nolint:unused // the door a Mechanism file's init() calls; the shipped catalogue is empty since v0.20.0.
func register(r row) { registerIn(catalogue, r) }

// registerIn is register over an explicit table, so a test can exercise the empty-ID and
// duplicate-ID panics without touching the production catalogue.
func registerIn(table map[domain.MechanismID]row, r row) {
	if r.descriptor.ID == "" {
		panic("mechanisms: register: row with an empty descriptor ID")
	}
	if _, dup := table[r.descriptor.ID]; dup {
		panic(fmt.Sprintf("mechanisms: register: duplicate Mechanism ID %q", r.descriptor.ID))
	}
	table[r.descriptor.ID] = r
}

// Build constructs the catalogued Mechanism identified by id, injecting deps (D3), and returns it
// as the registry holds it: the row's descriptor and ordering joined with the hook the row's
// constructor built. The metadata is copied with its slice fields cloned, exactly as Descriptors()
// clones what it harvests, so a caller cannot mutate the catalogue's rows through what it gets
// back. This is the SINGLE place a Mechanism's metadata and its behaviour are joined,
// so the two cannot drift. It is the seam the engine drives for each enabled `mechanisms:` ID. An
// id absent from the catalogue is a loud error naming the known IDs and wrapping
// domain.ErrUnknownMechanism (so callers can match it with errors.Is), so a typo'd config key
// fails startup rather than silently disabling a Mechanism.
func Build(id domain.MechanismID, deps Deps) (domain.RegisteredMechanism, error) {
	return buildFrom(catalogue, id, deps)
}

// KnownIDs returns the canonical IDs of every buildable Mechanism, sorted — the catalogue the
// config surface (and its unknown-ID error) reports as the valid `mechanisms:` keys.
func KnownIDs() []domain.MechanismID { return knownIDs(catalogue) }

// Descriptors returns every catalogued Mechanism's static descriptor, sorted by canonical ID and
// duplicate-free — the metadata the public surface (CataloguedMechanisms, ADR 0015 §3) exposes
// without building a Mechanism. Each returned descriptor is a copy with its slice fields cloned, so
// a caller cannot mutate the catalogue's rows.
func Descriptors() []domain.MechanismDescriptor {
	out := make([]domain.MechanismDescriptor, 0, len(catalogue))
	for _, r := range catalogue {
		out = append(out, cloneDescriptor(r.descriptor))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// cloneDescriptor returns a copy of d whose slice fields are independent of the source row, so a
// caller mutating a returned descriptor cannot reach back into the static catalogue.
func cloneDescriptor(d domain.MechanismDescriptor) domain.MechanismDescriptor {
	d.IncompatibleWith = slices.Clone(d.IncompatibleWith)
	d.Requires = slices.Clone(d.Requires)
	return d
}

// cloneOrdering returns a copy of o whose slice fields are independent of the source row — the
// Ordering half of the same guarantee cloneDescriptor gives, so a caller mutating a built
// Mechanism's Before/After cannot reach back into the static catalogue.
func cloneOrdering(o domain.OrderingConstraints) domain.OrderingConstraints {
	o.Before = slices.Clone(o.Before)
	o.After = slices.Clone(o.After)
	return o
}

// buildFrom is Build over an explicit table, so a test can exercise the lookup / unknown-id /
// inject path against a fake row while the production catalogue is still empty.
//
// The metadata it hands out is cloned, exactly as Descriptors() clones what it harvests: a built
// Mechanism's Descriptor and Ordering slices are independent of the table's row, so a caller
// mutating them cannot reach back into the static catalogue.
func buildFrom(table map[domain.MechanismID]row, id domain.MechanismID, deps Deps) (domain.RegisteredMechanism, error) {
	r, ok := table[id]
	if !ok {
		return domain.RegisteredMechanism{}, fmt.Errorf("%w %q; known: %s", domain.ErrUnknownMechanism, id, knownList(table))
	}
	hook, err := r.construct(deps)
	if err != nil {
		return domain.RegisteredMechanism{}, err
	}
	return domain.RegisteredMechanism{Descriptor: cloneDescriptor(r.descriptor), Ordering: cloneOrdering(r.ordering), Hook: hook}, nil
}

// knownIDs returns the table's IDs sorted by their canonical spelling (the stable order the
// dispatch tiebreak also keys on, D4), so error messages and listings are deterministic.
func knownIDs(table map[domain.MechanismID]row) []domain.MechanismID {
	ids := make([]domain.MechanismID, 0, len(table))
	for id := range table {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// knownList renders the table's IDs as a comma-separated string for an unknown-id error. An empty
// catalogue (no Mechanism ported yet) renders "(none)" rather than an empty tail.
func knownList(table map[domain.MechanismID]row) string {
	ids := knownIDs(table)
	if len(ids) == 0 {
		return "(none)"
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = string(id)
	}
	return strings.Join(parts, ", ")
}
