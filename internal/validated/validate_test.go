package validated

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

func TestValidate(t *testing.T) {
	descriptors := []domain.MechanismDescriptor{
		{ID: "a"},
		{ID: "b", Requires: []domain.MechanismID{"a"}},
		{ID: "c", IncompatibleWith: []domain.MechanismID{"a"}},
		{ID: "d"},
	}

	tests := []struct {
		name    string
		set     []domain.MechanismID
		wantErr string // substring; "" = valid
	}{
		{"whole valid set", ids("a", "b", "d"), ""},
		{"single member", ids("d"), ""},
		{"unknown id", ids("a", "ghost"), `unknown mechanism "ghost"`},
		{"duplicate id", ids("a", "a"), `lists mechanism "a" twice`},
		{"requirement outside the set", ids("b", "d"), `requires "a"`},
		{"incompatible pair inside the set", ids("a", "c"), "declared incompatible"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(Entry{Key: "k", Set: tt.set}, descriptors)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("want valid, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

// A retired ID is shed and named; everything else in the set survives in order, and the source entry
// is left as it was found. This is ADR 0016's 2026-08-29 amendment: a saved record naming a Mechanism
// this build retired keeps the rest of its set instead of being skipped whole.
func TestDropRetiredShedsOnlyTheRetiredIDs(t *testing.T) {
	t.Parallel()

	source := Entry{Key: "k", Set: ids("a", "gone", "b", "also_gone", "d")}

	pruned, dropped := DropRetired(source, ids("gone", "also_gone"))

	if got, want := pruned.Set, ids("a", "b", "d"); !equalIDs(got, want) {
		t.Errorf("pruned set = %v, want %v", got, want)
	}
	if got, want := dropped, ids("gone", "also_gone"); !equalIDs(got, want) {
		t.Errorf("dropped = %v, want %v (listed order)", got, want)
	}
	if got, want := source.Set, ids("a", "gone", "b", "also_gone", "d"); !equalIDs(got, want) {
		t.Errorf("the source entry was edited in place: %v, want %v", got, want)
	}
	if pruned.Key != source.Key {
		t.Errorf("pruned key = %q, want %q — only the set may change", pruned.Key, source.Key)
	}
}

// An entry naming no retired ID comes back untouched, and so does one measured against an empty roll:
// the shed is a no-op on every build that has retired nothing.
func TestDropRetiredLeavesACleanEntryAlone(t *testing.T) {
	t.Parallel()

	source := Entry{Key: "k", Set: ids("a", "b", "d")}

	for _, tt := range []struct {
		name    string
		retired []domain.MechanismID
	}{
		{"nothing on the roll matches", ids("gone")},
		{"the roll is empty", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pruned, dropped := DropRetired(source, tt.retired)
			if len(dropped) != 0 {
				t.Errorf("dropped = %v, want none", dropped)
			}
			if !equalIDs(pruned.Set, source.Set) {
				t.Errorf("pruned set = %v, want %v", pruned.Set, source.Set)
			}
		})
	}
}

// A set that is ONLY retired IDs sheds to empty rather than to the whole entry surviving — the caller
// then sees an entry with nothing left to apply, which is honest data rather than a silent floor.
func TestDropRetiredCanEmptyASet(t *testing.T) {
	t.Parallel()

	pruned, dropped := DropRetired(Entry{Key: "k", Set: ids("gone")}, ids("gone"))

	if len(pruned.Set) != 0 {
		t.Errorf("pruned set = %v, want empty", pruned.Set)
	}
	if !equalIDs(dropped, ids("gone")) {
		t.Errorf("dropped = %v, want [gone]", dropped)
	}
}

// equalIDs compares two ID lists element-wise, treating nil and empty as equal.
func equalIDs(got, want []domain.MechanismID) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
