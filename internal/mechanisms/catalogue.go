package mechanisms

import (
	"fmt"
	"sort"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// Deps are the construction-injected collaborators a catalogued Mechanism may need at BUILD
// time (D3 — a Mechanism's dependencies are injected once when it is constructed, never passed
// per hook call; hook signatures stay about conversation state). Every field is optional: a
// Mechanism that needs none ignores them. The set grows as the port waves land — a later wave
// adds a field here, populates it in cmd/apogee/wire.go, and its constructor reads it. Kept in
// internal/mechanisms (not domain) because these are host-supplied collaborators the catalogue
// wires, not part of the loop's construction surface.
type Deps struct {
	// Library is the confidence-tagged observation store the library observe/inject Mechanisms
	// read and write. It is nil until the Library store lands (Phase-4 item 13) and is narrowed
	// to that store's type when item 14 wires it; no constructor in the table consumes it yet, so
	// today it is an inert forward seam that keeps the injection shape stable across waves.
	Library any

	// LookPath resolves an executable name against the host PATH (exec.LookPath's contract). A
	// Mechanism that shells out probes its external commands ONCE at construction through this
	// seam and caches the resolved paths (D3 — autofix's formatter table), so fires never probe
	// and a test injects formatter availability without touching the real PATH. nil falls back
	// to exec.LookPath.
	LookPath func(string) (string, error)

	// GrammarConstraint is the D3-injected backend-capability gate for the grammar Mechanism
	// (catalogue Table A/C: grammar is backend-capability gated). It is true only when the
	// configured backend BOTH accepts a json_schema `response_format` constraint AND needs one
	// (the model does not emit native tool calls) — the apogee analog of apogee-sim's gate on
	// llama.cpp WITHOUT native tool-calls (`proxy.go:625-634` @pin). apogee has no such
	// backend-capability probe wired yet, and the provider wire itself carries no
	// `response_format` field yet (`internal/agent/loop.go` toProviderRequest drops SetExtra —
	// "response_format is a Phase-4 concern"), so cmd/apogee/wire.go leaves this false and grammar
	// no-ops on every current backend (catalogue Table B: "may no-op on all current apogee
	// backends"). It is an inert forward seam like Library: a future backend probe populates it,
	// and grammar's fire path is exercised today only by tests that inject it true.
	GrammarConstraint bool
}

// constructor builds one catalogued Mechanism from the injected Deps (D3). It returns an error so
// a Mechanism that cannot be built with the given Deps (a missing required collaborator, an
// invalid configuration) fails construction loudly rather than registering a half-built Mechanism.
type constructor func(Deps) (domain.Mechanism, error)

// catalogue is the constructor table: canonical MechanismID → its builder. It is the single
// registry of buildable Mechanisms — Build looks an ID up here, and the config surface validates
// an enabled `mechanisms:` key against its keys by driving Build. It starts EMPTY: waves 5–14 add
// one row per ported Mechanism (each row a `catalogue[id] = newFoo` line beside the Mechanism's
// implementation). The wiring is exercised while the table is empty via buildFrom against a fake
// row (catalogue_test.go), so the seam is proven before the first real Mechanism lands.
var catalogue = map[domain.MechanismID]constructor{}

// Build constructs the catalogued Mechanism identified by id, injecting deps (D3). It is the seam
// cmd/apogee/wire.go drives for each enabled `mechanisms:` ID. An id absent from the catalogue is
// a loud error naming the known IDs, so a typo'd config key fails startup rather than silently
// disabling a Mechanism.
func Build(id domain.MechanismID, deps Deps) (domain.Mechanism, error) {
	return buildFrom(catalogue, id, deps)
}

// KnownIDs returns the canonical IDs of every buildable Mechanism, sorted — the catalogue the
// config surface (and its unknown-ID error) reports as the valid `mechanisms:` keys.
func KnownIDs() []domain.MechanismID { return knownIDs(catalogue) }

// buildFrom is Build over an explicit table, so a test can exercise the lookup / unknown-id /
// inject path against a fake row while the production catalogue is still empty.
func buildFrom(table map[domain.MechanismID]constructor, id domain.MechanismID, deps Deps) (domain.Mechanism, error) {
	build, ok := table[id]
	if !ok {
		return nil, fmt.Errorf("apogee: unknown mechanism %q; known: %s", id, knownList(table))
	}
	return build(deps)
}

// knownIDs returns the table's IDs sorted by their canonical spelling (the stable order the
// dispatch tiebreak also keys on, D4), so error messages and listings are deterministic.
func knownIDs(table map[domain.MechanismID]constructor) []domain.MechanismID {
	ids := make([]domain.MechanismID, 0, len(table))
	for id := range table {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// knownList renders the table's IDs as a comma-separated string for an unknown-id error. An empty
// catalogue (no Mechanism ported yet) renders "(none)" rather than an empty tail.
func knownList(table map[domain.MechanismID]constructor) string {
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
