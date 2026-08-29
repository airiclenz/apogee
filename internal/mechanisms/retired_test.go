package mechanisms

import (
	"slices"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// A retired ID is never also a catalogue row: the two lists partition the IDs this build recognises,
// so a caller that drops the retired ones and refuses the rest cannot silently drop a live Mechanism.
func TestRetiredIDsAreNotInTheCatalogue(t *testing.T) {
	t.Parallel()

	known := KnownIDs()
	for _, id := range RetiredIDs() {
		if slices.Contains(known, id) {
			t.Errorf("%q is both retired and a catalogue row; the roll and the catalogue must be disjoint", id)
		}
	}
}

// The roll names grammar (retired 2026-08-29) and answers IsRetired for it, while a live row and an
// invented ID both answer false — the distinction the tolerant config paths key on.
func TestIsRetiredNamesTheRolledIDsOnly(t *testing.T) {
	t.Parallel()

	if !IsRetired("grammar") {
		t.Errorf("IsRetired(%q) = false, want true — grammar was retired 2026-08-29", "grammar")
	}
	for _, id := range []domain.MechanismID{"validate", "not_a_mechanism", ""} {
		if IsRetired(id) {
			t.Errorf("IsRetired(%q) = true, want false", id)
		}
	}
}

// RetiredIDs hands out a copy, so a caller that sorts or truncates its answer cannot edit the roll
// every other caller reads.
func TestRetiredIDsIsACopy(t *testing.T) {
	t.Parallel()

	first := RetiredIDs()
	if len(first) == 0 {
		t.Fatal("RetiredIDs() is empty; grammar should be on the roll")
	}
	first[0] = "clobbered"

	if second := RetiredIDs(); slices.Contains(second, "clobbered") {
		t.Errorf("RetiredIDs() returned the roll itself: %v", second)
	}
}
