package validated

import (
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
)

// Validate checks that the entry's enable set is whole and buildable against the
// catalogue this binary carries — the checks apogee.New would otherwise fail loudly on,
// run early so a defective entry degrades SOFT (skip + warn, floor still works) instead
// of blocking startup on data the user did not write. Whole-set-or-nothing: any defect
// disqualifies the entire entry, because enabling a subset would arm an unvalidated
// stack under the validated banner.
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
