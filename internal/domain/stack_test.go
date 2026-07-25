package domain

// White-box tests for the shared Mechanism-stack validity rule (stack.go). The two
// callers' renderings are pinned by their own suites — registry_ordered_test.go for the
// registry's loud startup gates, internal/validated/validate_test.go for the soft
// skip-and-warn — so what these cover is the rule itself and its load-bearing defect
// order, which is what keeps both callers reporting the first defect they always did.

import (
	"reflect"
	"testing"
)

func TestCheckStack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		descs []MechanismDescriptor
		want  []StackDefect
	}{
		{
			name:  "empty stack is valid",
			descs: nil,
			want:  nil,
		},
		{
			name: "whole stack present ⇒ no defects",
			descs: []MechanismDescriptor{
				{ID: "a"},
				{ID: "b", Requires: []MechanismID{"a"}},
			},
			want: nil,
		},
		{
			name: "required peer absent",
			descs: []MechanismDescriptor{
				{ID: "b", Requires: []MechanismID{"a"}},
			},
			want: []StackDefect{
				{Kind: StackMissingRequirement, Mechanism: "b", Peer: "a"},
			},
		},
		{
			name: "incompatible peer present — named from the declaring side",
			descs: []MechanismDescriptor{
				{ID: "a"},
				{ID: "c", IncompatibleWith: []MechanismID{"a"}},
			},
			want: []StackDefect{
				{Kind: StackIncompatible, Mechanism: "c", Peer: "a"},
			},
		},
		{
			name: "incompatible peer absent ⇒ no defect",
			descs: []MechanismDescriptor{
				{ID: "c", IncompatibleWith: []MechanismID{"ghost"}},
			},
			want: nil,
		},
		{
			name: "both kinds ⇒ the missing requirement is emitted first",
			descs: []MechanismDescriptor{
				{ID: "x", Requires: []MechanismID{"ghost"}, IncompatibleWith: []MechanismID{"y"}},
				{ID: "y"},
			},
			want: []StackDefect{
				{Kind: StackMissingRequirement, Mechanism: "x", Peer: "ghost"},
				{Kind: StackIncompatible, Mechanism: "x", Peer: "y"},
			},
		},
		{
			name: "requirement chain with a broken far link trips on the middle member",
			descs: []MechanismDescriptor{
				{ID: "a", Requires: []MechanismID{"b"}},
				{ID: "b", Requires: []MechanismID{"c"}},
			},
			want: []StackDefect{
				{Kind: StackMissingRequirement, Mechanism: "b", Peer: "c"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CheckStack(tt.descs)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("CheckStack() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
