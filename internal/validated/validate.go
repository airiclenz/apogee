package validated

import (
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
)

// DropRetired returns the entry with every RETIRED Mechanism ID removed from its set, plus the IDs
// it dropped in the order they were listed. It is the ADR 0016 amendment of 2026-08-29 in code: an
// entry naming an ID the catalogue no longer carries would otherwise fail Validate's unknown-ID
// check and lose the WHOLE set, punishing a user for a curation change they did not make. A retired
// ID is safe to shed where an unknown one is not, because a row only retires once it is inert by
// construction — dropping it leaves the measured stack, and therefore the entry's evidence,
// untouched. An UNKNOWN (non-retired) ID still disqualifies the entry whole, as ADR 0016 rules.
//
// Retired IDs are a parameter for the same reason descriptors are: this package never imports the
// Mechanism catalogue. The caller passes mechanisms.RetiredIDs(). The entry is copied, so the
// source entry (a shipped bundle member, a decoded user file) is never edited in place.
func DropRetired(e Entry, retired []domain.MechanismID) (pruned Entry, dropped []domain.MechanismID) {
	if len(retired) == 0 || len(e.Set) == 0 {
		return e, nil
	}
	roll := make(map[domain.MechanismID]bool, len(retired))
	for _, id := range retired {
		roll[id] = true
	}

	keep := make([]domain.MechanismID, 0, len(e.Set))
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

// Validate checks that the entry's enable set is whole and buildable against the
// catalogue this binary carries — the checks apogee.New would otherwise fail loudly on,
// run early so a defective entry degrades SOFT (skip + warn, floor still works) instead
// of blocking startup on data the user did not write. Whole-set-or-nothing: any defect
// disqualifies the entire entry, because enabling a subset would arm an unvalidated
// stack under the validated banner. A RETIRED ID is the one exception ADR 0016's
// 2026-08-29 amendment carves out, and it never reaches here: DropRetired sheds it
// first, so what this function sees is already the set the live catalogue must answer for.
//
// Descriptors are a parameter (the caller passes the live catalogue) so this package
// never imports the Mechanism constructors; shipped_test.go runs the same check against
// the real catalogue as the CI drift pin.
//
// The unknown-ID and duplicate-ID checks are this package's own — the registry can have
// neither. The stacking rule is not: it is domain.CheckStack, shared with the registry's
// startup gates so the two cannot drift. Only the RENDERING stays here, because a defect
// found pre-build is a soft skip-and-warn, not the loud startup failure it is post-build.
func Validate(e Entry, descriptors []domain.MechanismDescriptor) error {
	byID := make(map[domain.MechanismID]domain.MechanismDescriptor, len(descriptors))
	for _, d := range descriptors {
		byID[d.ID] = d
	}

	members := make(map[domain.MechanismID]bool, len(e.Set))
	set := make([]domain.MechanismDescriptor, 0, len(e.Set))
	for _, id := range e.Set {
		d, ok := byID[id]
		if !ok {
			return fmt.Errorf("set names unknown mechanism %q (catalogue evolved since the entry was recorded)", id)
		}
		if members[id] {
			return fmt.Errorf("set lists mechanism %q twice", id)
		}
		members[id] = true
		set = append(set, d)
	}

	for _, defect := range domain.CheckStack(set) {
		switch defect.Kind {
		case domain.StackMissingRequirement:
			return fmt.Errorf("mechanism %q requires %q, which is not in the set", defect.Mechanism, defect.Peer)
		case domain.StackIncompatible:
			return fmt.Errorf("mechanisms %q and %q are declared incompatible", defect.Mechanism, defect.Peer)
		}
	}
	return nil
}
