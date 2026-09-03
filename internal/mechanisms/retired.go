package mechanisms

import (
	"fmt"
	"slices"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// retiredRow is one entry on the roll: the catalogue ID, the release whose notes carry its
// retirement, and — for a PROMOTED row, one retired because its behaviour became engine behaviour —
// the top-level configuration key that governs that behaviour now. Successor is empty for a row
// retired outright, which is what tells the two apart everywhere the roll is read.
type retiredRow struct {
	ID        domain.MechanismID
	Release   string
	Successor string
}

// retired is the roll of catalogue IDs apogee once carried and no longer does. It exists so a
// user's saved configuration survives a deletion: a `mechanisms:` key, a per-server delegation
// posture or a Validated-set record naming a retired ID was valid at the release before the
// removal, and the removal must not turn it into a refusal (AGENTS.md: a value apogee itself
// accepted yesterday and rejects today is a regression, never a deferral).
//
// A row joins this roll on A RATIFIED VERDICT (ADR 0071) or when it was INERT BY CONSTRUCTION at
// the moment it was retired — it could not fire on any backend the release supported. Inertness is
// what makes dropping a row from a Validated set behaviour-preserving rather than a silent
// re-tuning of a validated stack (ADR 0016's whole-set-or-nothing amendment, 2026-08-29); a
// ratified verdict is the wider door ADR 0071 opened, where the owner decides a row's evidence no
// longer earns it a place and the set's evidence is void without it either way.
//
// Each row carries its OWN release because the roll now spans more than one wave: a single const
// could only ever be right for whichever removal went last, and the notices below name the release
// a user would look up in the changelog.
//
//   - grammar (retired 2026-08-29) — the json_schema `response_format` shaper. Its only gate was a
//     backend-capability field on Deps that nothing ever populated, over a provider wire that
//     carries no response_format field, so it no-op'd on every backend from the port onwards.
var retired = []retiredRow{
	{ID: "grammar", Release: "v0.18.7"},
}

// RetiredIDs returns the retired catalogue IDs, sorted, as a fresh slice the caller may keep. It is
// the complement of KnownIDs: an ID in neither list is unknown, and unknown stays a loud error
// everywhere a retired ID is quietly dropped.
func RetiredIDs() []domain.MechanismID {
	out := make([]domain.MechanismID, 0, len(retired))
	for _, r := range retired {
		out = append(out, r.ID)
	}
	slices.Sort(out)
	return out
}

// IsRetired reports whether id names a Mechanism this build retired. A retired ID is never also a
// known one — retired_test.go pins that — so a caller may ask the two questions in either order.
func IsRetired(id domain.MechanismID) bool { return rowFor(id) != nil }

// RetiredRelease returns the version whose notes carry id's retirement, or "" when id is not on the
// roll. It is a per-ID lookup rather than one const because the roll spans several waves, and every
// caller that words a notice about a removed ID — this package's own, a Validated-set shed line —
// must name the release that ID actually went in.
func RetiredRelease(id domain.MechanismID) string {
	if r := rowFor(id); r != nil {
		return r.Release
	}
	return ""
}

// Successor returns the top-level configuration key that now governs the behaviour id used to
// provide, or "" when id retired outright (and for an ID that is not on the roll at all). A
// non-empty answer is what turns a retired-ID notice from "this does nothing, delete it" into
// "this is engine behaviour now, and here is the key that switches it".
func Successor(id domain.MechanismID) string {
	if r := rowFor(id); r != nil {
		return r.Successor
	}
	return ""
}

// rowFor finds id's roll entry, or nil. The roll is a handful of rows walked linearly rather than a
// map, so the source order stays the documentation it is.
func rowFor(id domain.MechanismID) *retiredRow {
	for i := range retired {
		if retired[i].ID == id {
			return &retired[i]
		}
	}
	return nil
}

// OffRampFloor returns the catalogued off-ramp IDs a `mechanisms:` block leaves standing — every
// registered row whose Capability is domain.CapOffRamp and whose key is not explicitly false in
// block — sorted canonically, as a fresh slice the caller may keep.
//
// It is the machinery behind the one exception to D1 (ADR 0070): every other Capability defaults
// OFF and is armed only by being named, but the two off-ramps — the empty-reply and the
// narrated-instead-of-acted recoveries — are recovery guarantees rather than small-model tuning.
// They survive Bypass already (ADR 0006), so a run that had them off was the one posture where the
// floor and Bypass disagreed; defaulting them on closes that gap. An absent key means ON, which is
// why the block is read for an explicit false rather than for a true.
//
// The floor is harvested from the production catalogue's Capability column rather than from a
// hand-kept ID list, so a row that later joins (or leaves) CapOffRamp moves the floor with it and
// no second list can drift from the first. A block naming a RETIRED id is irrelevant here: a
// retired row is not in the catalogue, so it can neither be floored nor un-floor anything.
func OffRampFloor(block map[string]bool) []domain.MechanismID {
	var out []domain.MechanismID
	for _, d := range Descriptors() { // already sorted by canonical ID
		if d.Capability != domain.CapOffRamp {
			continue
		}
		if on, named := block[string(d.ID)]; named && !on {
			continue
		}
		out = append(out, d.ID)
	}
	return out
}

// ResolveEnabled validates every `mechanisms:` configuration key against the known catalogue and
// returns the enabled IDs in sorted canonical order for Config.EnableMechanisms — the engine
// (apogee.New/Resume) builds them, derives their Deps, and runs the stacking gates (ADR 0015 §1: a
// Driver's wiring collapses to a YAML→ID-list producer). EVERY key is validated here, enabled AND
// disabled: the engine only ever sees the enabled IDs, so a typo'd DISABLED key — never
// constructed — must still fail loudly at this startup boundary (phase-4-review-fixes item 5). An
// unknown key, whether true or false, is a loud error naming the known catalogue. Keys are walked
// in sorted spelling so the returned list (and any engine-side build error over it) is
// deterministic; the dispatch order is the registry's own topo-sort (ADR 0003), independent of this
// order.
//
// The OFF-RAMP FLOOR (OffRampFloor, ADR 0070) is unioned into the answer: the catalogued off-ramps
// are enabled unless the block names one explicitly `false`, so an absent or empty block resolves to
// the two of them rather than to nothing, `{"tool_use_enforcer": false}` resolves to
// empty_response_recovery alone, and a block enabling other rows gets them BESIDE the floor. Every
// other Capability is still armed only by being named (D1). The union is deduplicated and re-sorted,
// so a block that spells an off-ramp out as `true` yields it once, in canonical position. The floor
// is read from the production catalogue rather than from the `known` argument: `known` is the list a
// caller validates spelling against (a test may hand a fake one), while the floor is about which
// rows this build actually ships.
//
// A RETIRED ID (RetiredIDs — a row this build removed) is DROPPED from ids, silently and whatever
// its value: the key was valid at the release before the removal, so refusing it would break a
// configuration the user never edited. The roll is read here rather than injected beside known
// because every path that resolves the block must tolerate it identically — a Driver's startup, a
// live `/settings` apply, each delegate's per-server posture, a headless run, a daemon Firing —
// and a path that forgot to pass it would refuse where its siblings tolerate. The silence in ids is
// the same requirement: several of those paths run with the alt screen up, where a stderr line is
// painted over the TUI.
//
// notices is the user-facing half of that tolerance, handed back WITH the ids so a caller cannot
// take the ids and forget the lines: one line per retired ID the block speaks about, sorted by ID,
// so a configuration still asking for a removed Mechanism says so once instead of arming nothing
// without explanation. Which values earn a line depends on whether the row was PROMOTED (Successor
// non-empty):
//
//   - Retired OUTRIGHT and set true: the plain line naming the release and asking for the key's
//     removal. Set FALSE it earns no line — the user is not asking for it, and telling them to
//     delete a key that already disables nothing is noise.
//   - PROMOTED and set true: the behaviour is still there, so the line says so and names the
//     top-level key that governs it now.
//   - PROMOTED and set FALSE: this is the one case where silence would MISLEAD. The user wrote a
//     key to switch the behaviour off, and it no longer does — so the line says the old spelling
//     stopped working and names the top-level key that does it, rather than leaving them believing
//     a guard is off when it is on.
//
// An unknown key returns the error and no lines: a refused block never reached the point of
// tolerating anything. ResolveEnabled PRINTS nothing itself — the caller decides where the lines go, because only a pre-TUI path may write to
// stderr, a live apply folds them into the answer its pane renders, and a delegate's posture
// discards them.
func ResolveEnabled(
	enabled map[string]bool,
	known []domain.MechanismID,
) (ids []domain.MechanismID, notices []string, err error) {
	knownSet := make(map[string]bool, len(known))
	for _, id := range known {
		knownSet[string(id)] = true
	}

	keys := make([]string, 0, len(enabled))
	for key := range enabled {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	resolved := make([]domain.MechanismID, 0, len(keys))
	var retiredNoticed []string
	for _, key := range keys {
		if IsRetired(domain.MechanismID(key)) {
			if enabled[key] || Successor(domain.MechanismID(key)) != "" {
				retiredNoticed = append(retiredNoticed, key)
			}
			continue
		}
		if !knownSet[key] {
			return nil, nil, fmt.Errorf("apogee: unknown mechanism %q; known: %s", key, knownIDList(known))
		}
		if enabled[key] {
			resolved = append(resolved, domain.MechanismID(key))
		}
	}
	for _, id := range OffRampFloor(enabled) {
		if !slices.Contains(resolved, id) {
			resolved = append(resolved, id)
		}
	}
	slices.Sort(resolved)
	if len(resolved) == 0 {
		resolved = nil
	}

	for _, key := range retiredNoticed {
		id := domain.MechanismID(key)
		switch successor := Successor(id); {
		case successor == "":
			notices = append(notices, fmt.Sprintf(
				"apogee: mechanism %q was retired in %s and is ignored; remove it from mechanisms:",
				key, RetiredRelease(id)))
		case enabled[key]:
			notices = append(notices, fmt.Sprintf(
				"apogee: mechanism %q is the %q floor guard since %s and is on by default; remove it from mechanisms:",
				key, successor, RetiredRelease(id)))
		default:
			notices = append(notices, fmt.Sprintf(
				"apogee: mechanism %q is the %q floor guard since %s; \"%s: false\" under mechanisms: "+
					"no longer turns it off — set %s: false at the top level",
				key, successor, RetiredRelease(id), key, successor))
		}
	}
	return resolved, notices, nil
}

// knownIDList renders the catalogue ResolveEnabled was handed as a comma-separated string for its
// unknown-key error, matching the engine's own unknown-ID error tail (an empty catalogue renders
// "(none)" rather than a dangling tail). It takes the ID slice the caller passed, where knownList
// takes the registry table.
func knownIDList(known []domain.MechanismID) string {
	if len(known) == 0 {
		return "(none)"
	}
	parts := make([]string, len(known))
	for i, id := range known {
		parts[i] = string(id)
	}
	return strings.Join(parts, ", ")
}
