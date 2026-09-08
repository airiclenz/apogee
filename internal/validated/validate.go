package validated

import (
	"fmt"
)

// DropRetired returns the entry with every RETIRED Mechanism ID removed from its set, plus the IDs
// it dropped in the order they were listed. It is the ADR 0016 amendment of 2026-08-29 in code: an
// entry naming an ID the roster no longer carries would otherwise fail Validate's unknown-ID
// check and lose the WHOLE set, punishing a user for a curation change they did not make. A retired
// ID is safe to shed where an unknown one is not, because a row only retires once it is inert by
// construction — dropping it leaves the measured stack, and therefore the entry's evidence,
// untouched. An UNKNOWN (non-retired) ID still disqualifies the entry whole, as ADR 0016 rules.
//
// An entry whose set is retired WHOLE sheds to an empty one. That is honest data, not an apply:
// the caller must read the empty set as "this entry no longer applies" rather than handing it to
// Validate, which an empty set passes vacuously. cmd/apogee's startup ladder makes that reading
// (its setRetired rung); no caller may treat an emptied entry as a validated stack.
//
// Retired IDs are a parameter for the same reason the known roster is: this package never imports
// the retired roll itself. The caller passes mechanisms.RetiredIDs(). The entry is
// copied, so the source entry (a shipped bundle member, a decoded user file) is never edited in
// place.
func DropRetired(e Entry, retired []string) (pruned Entry, dropped []string) {
	if len(retired) == 0 || len(e.Set) == 0 {
		return e, nil
	}
	roll := make(map[string]bool, len(retired))
	for _, id := range retired {
		roll[id] = true
	}

	keep := make([]string, 0, len(e.Set))
	for _, id := range e.Set {
		if roll[id] {
			dropped = append(dropped, id)
			continue
		}
		keep = append(keep, id)
	}
	if len(dropped) == 0 {
		return e, nil
	}

	pruned = e
	pruned.Set = keep
	return pruned, dropped
}

// Validate checks that the entry's enable set is whole against the roster this binary carries,
// run early so a defective entry degrades SOFT (skip + warn, floor still works) instead of
// blocking startup on data the user did not write. Whole-set-or-nothing: any defect
// disqualifies the entire entry, because enabling a subset would arm an unvalidated
// stack under the validated banner. A RETIRED ID is the one exception ADR 0016's
// 2026-08-29 amendment carves out, and it never reaches here: DropRetired sheds it
// first, so what this function sees is already the set the live roster must answer for.
//
// The known roster is a parameter (the caller passes the live one) so this package never
// imports the Mechanism catalogue; shipped_test.go runs the same check against the real
// roster as the CI drift pin. Since the Reaction core landed (ADR 0076 D11) that roster is
// permanently EMPTY, so every member is unknown and every non-empty set is skipped — the
// surface runs for its notices alone until stage 2 re-homes it.
//
// The unknown-ID and duplicate-ID checks are this package's own, and they are all that is
// left: the stacking rule used to be domain.CheckStack, shared with the registry's startup
// gates, and went with the registry.
func Validate(e Entry, known []string) error {
	roster := make(map[string]bool, len(known))
	for _, id := range known {
		roster[id] = true
	}

	members := make(map[string]bool, len(e.Set))
	for _, id := range e.Set {
		if !roster[id] {
			return fmt.Errorf("set names unknown mechanism %q (catalogue evolved since the entry was recorded)", id)
		}
		if members[id] {
			return fmt.Errorf("set lists mechanism %q twice", id)
		}
		members[id] = true
	}
	return nil
}
