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
