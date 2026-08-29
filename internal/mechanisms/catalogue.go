package mechanisms

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/library"
)

// Deps are the construction-injected collaborators a catalogued Mechanism may need at BUILD
// time (D3 — a Mechanism's dependencies are injected once when it is constructed, never passed
// per hook call; hook signatures stay about conversation state). Every field is optional: a
// Mechanism that needs none ignores them. The set grows as the port waves land — a later wave
// adds a field here, populates it in cmd/apogee/wire.go, and its constructor reads it. Kept in
// internal/mechanisms (not domain) because these are host-supplied collaborators the catalogue
// wires, not part of the loop's construction surface.
type Deps struct {
	// Library is the confidence-tagged observation store the library observe/inject Mechanism reads
	// and writes (Phase-4 item 14; the store type landed in item 13). It is nil unless a row that
	// declares needs.Library is enabled — the engine's deriveDeps (internal/agent/construct.go, the
	// single Deps-deriving path since the ADR 0015 wire.go collapse) constructs and Loads the store
	// under Config.LibraryDir and injects it here only then, so a config without `library` builds no
	// store. newLibrary refuses a nil store (errLibraryStoreRequired).
	Library *library.Store

	// Fingerprint is the resolved model identity the library Mechanism keys its store reads and writes
	// on (D3 — resolved once at wire time from the configured model id via library.ResolveFingerprint,
	// so the inject and observe halves share one identity rather than re-resolving per call). The zero
	// fingerprint (an unidentified model) leaves the Library inert. Only the library Mechanism reads it.
	Fingerprint domain.ModelFingerprint

	// LookPath resolves an executable name against the host PATH (exec.LookPath's contract). A
	// Mechanism that shells out probes its external commands ONCE at construction through this
	// seam and caches the resolved paths (D3 — autofix's formatter table), so fires never probe
	// and a test injects formatter availability without touching the real PATH. nil falls back
	// to exec.LookPath.
	LookPath func(string) (string, error)

	// WritableBox is the fence a Mechanism that resolves an executable refuses to resolve one
	// INSIDE: the workspace root plus any configured extra writable paths. Autofix's formatter
	// probe is its only reader today (D3 — resolved once at construction, like the paths it
	// guards), because bytes a confined call was allowed to write must never become the argv[0]
	// of a later spawn. deriveDeps (internal/agent/construct.go) populates it from Config
	// UNCONDITIONALLY rather than behind a DepNeeds flag: the refusal has to hold on a host with
	// no confinement backend too, so it cannot be derived from a fire-time permit's box, and a
	// zero value simply names no fence (a Deps built by a test that has no workspace).
	//
	// It is the box as it stood at CONSTRUCTION, and that is the correct instant rather than a
	// stale one: autofix probes PATH exactly once, in newAutofix, before the model has written
	// a byte, and caches the resolved paths for the run's lifetime. The only box field that
	// moves later is the session scratch dir (the engine's Agent.SetScratchDir), and a
	// moved-to scratch dir is a freshly created ~/.apogee/scratch/<id>/ which cannot contain a
	// path that was already resolved before it existed — so a live box would measure the very
	// same formatter paths against a fence that cannot include them. The tools' per-call box
	// (Agent.confinementBox) does follow the live scratch dir, because a tool resolves and
	// spawns PER CALL against a tree the model has been writing to; a Mechanism that caches
	// its resolutions at construction needs no such freshness.
	WritableBox domain.ConfinementBox

	// SecretEnvVars names the operator-declared credential variables a Mechanism that SPAWNS must
	// drop from the child's environment beside apogee's own — the `api-key-env:` names (ADR 0047)
	// the execution tools receive as tools.HostTools.SecretEnvVars. Autofix's formatter is its only
	// reader today: it hands them to tools.RunHookSubprocess, so a hook's child scrubs exactly what
	// terminal/python_exec/run_tests scrub instead of inheriting a key the operator exported into
	// the shell apogee was started from. deriveDeps (internal/agent/construct.go) populates it from
	// Config.SecretEnvVars UNCONDITIONALLY, like WritableBox and for the same reason: the scrub has
	// to hold whichever rows a run enabled, so it cannot hang off a DepNeeds flag. Nil names none,
	// leaving apogee's own fixed half — the scrub before this field existed.
	SecretEnvVars []string
}

// DepNeeds is which construction-injected collaborators a set of enabled rows requires, so the
// engine derives exactly those and nothing else. It is the catalogue's half of the ADR 0015 §2
// split: the engine still derives Deps from Config, but WHICH Deps a run needs is declared by the
// rows themselves (row.needs, ORed by DepsNeeded) rather than by an engine-side branch naming a
// Mechanism ID.
//
// Library means BOTH Deps.Library (the observation store) and Deps.Fingerprint (the model identity
// it keys on): they are resolved together, and only the library Mechanism reads either, so one flag
// covers the pair.
//
// The struct grows with Deps: a future Deps field that must be DERIVED — as opposed to one that
// defaults itself, like LookPath — adds a flag here, and the row that needs it declares it.
type DepNeeds struct {
	Library bool
}

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
	// needs declares which derived Deps this row's constructor reads (D4). The zero value needs
	// nothing — which is what all but the library row want, so those literals omit the field — and the
	// engine derives only what the enabled rows between them ask for (DepsNeeded).
	needs DepNeeds
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

// DepsNeeded returns the union of the needs declared by every catalogued row named in ids — the
// exact set of collaborators the engine must derive to build that Mechanism set, and nothing more.
// It answers the engine's question ("what does this arm need?") from the catalogue, so no caller has
// to know that `library` is the one row wanting a store.
//
// An ID absent from the catalogue is skipped silently: Build is the single place an unknown ID is
// reported, and it reports it loudly a moment later, so failing here would only duplicate that.
func DepsNeeded(ids []domain.MechanismID) DepNeeds {
	var needs DepNeeds
	for _, id := range ids {
		r, ok := catalogue[id]
		if !ok {
			continue
		}
		needs.Library = needs.Library || r.needs.Library
	}
	return needs
}

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
