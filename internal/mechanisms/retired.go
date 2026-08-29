package mechanisms

import (
	"slices"

	"github.com/airiclenz/apogee/internal/domain"
)

// retiredIDs is the roll of catalogue IDs apogee once carried and no longer does. It exists so a
// user's saved configuration survives a deletion: a `mechanisms:` key, a per-server `sub-agents:`
// posture or a Validated-set record naming a retired ID was valid at the release before the
// removal, and the removal must not turn it into a refusal (AGENTS.md: a value apogee itself
// accepted yesterday and rejects today is a regression, never a deferral).
//
// A row only ever joins this roll when it was INERT BY CONSTRUCTION at the moment it was retired —
// it could not fire on any backend the release supported — because that is what makes dropping it
// from a set behaviour-preserving rather than a silent re-tuning of a validated stack (ADR 0016's
// whole-set-or-nothing amendment, 2026-08-29).
//
//   - grammar (retired 2026-08-29) — the json_schema `response_format` shaper. Its only gate was a
//     backend-capability field on Deps that nothing ever populated, over a provider wire that
//     carries no response_format field, so it no-op'd on every backend from the port onwards.
var retiredIDs = []domain.MechanismID{"grammar"}

// RetiredIDs returns the retired catalogue IDs, sorted, as a fresh slice the caller may keep. It is
// the complement of KnownIDs: an ID in neither list is unknown, and unknown stays a loud error
// everywhere a retired ID is quietly dropped.
func RetiredIDs() []domain.MechanismID {
	out := slices.Clone(retiredIDs)
	slices.Sort(out)
	return out
}

// IsRetired reports whether id names a Mechanism this build retired. A retired ID is never also a
// known one — retired_test.go pins that — so a caller may ask the two questions in either order.
func IsRetired(id domain.MechanismID) bool { return slices.Contains(retiredIDs, id) }
