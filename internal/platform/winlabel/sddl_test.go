package winlabel

import (
	"errors"
	"testing"
)

func TestDescendantLabelDecision(t *testing.T) {
	t.Parallel()

	// The label walk's three-way decision for one descendant, proven on every OS. The rung
	// that matters most is the read ERROR: labelling a path whose prior could not be read
	// would destroy a possibly-foreign label with no journalled record to put it back, so the
	// path must be skipped entirely — no journal entry AND no label.
	tests := []struct {
		name              string
		prior             string
		readErr           error
		wantShouldJournal bool
		wantShouldLabel   bool
	}{
		{
			name:    "read_error_skips_journal_and_label",
			readErr: errors.New("access is denied"),
		},
		{
			name:    "read_error_wins_even_over_a_leftover_prior",
			prior:   "S:AI(ML;;NW;;;ME)",
			readErr: errors.New("access is denied"),
		},
		{
			name:              "foreign_prior_is_journalled_then_labelled",
			prior:             "S:AI(ML;;NW;;;ME)",
			wantShouldJournal: true,
			wantShouldLabel:   true,
		},
		{
			name:              "own_low_prior_still_passes_through_the_journal",
			prior:             FileSDDL, // journalLabelEntry decides what the entry may say
			wantShouldJournal: true,
			wantShouldLabel:   true,
		},
		{
			name:            "empty_prior_labels_only",
			wantShouldLabel: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			shouldJournal, shouldLabel := DescendantDecision(tt.prior, tt.readErr)
			if shouldJournal != tt.wantShouldJournal {
				t.Errorf("shouldJournal = %v, want %v", shouldJournal, tt.wantShouldJournal)
			}
			if shouldLabel != tt.wantShouldLabel {
				t.Errorf("shouldLabel = %v, want %v", shouldLabel, tt.wantShouldLabel)
			}
			if shouldJournal && !shouldLabel {
				t.Error("shouldJournal without shouldLabel; a journal entry would describe a mutation that never happens")
			}
		})
	}
}

func TestIsLowLabelSDDL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sddl string
		want bool
	}{
		{name: "empty_descriptor", sddl: ""},
		{name: "no_label_ace", sddl: "S:"},
		{name: "own_dir_label", sddl: DirSDDL, want: true},
		{name: "own_file_label", sddl: FileSDDL, want: true},
		{name: "inherited_own_label", sddl: "S:AI(ML;OICIID;NW;;;LW)", want: true},
		{name: "canonical_low_sid", sddl: "S:AI(ML;;NW;;;s-1-16-4096)", want: true},
		{name: "medium_label", sddl: "S:AI(ML;;NW;;;ME)"},
		{name: "high_label", sddl: "S:(ML;OICI;NW;;;HI)"},
		{name: "truncated_ace", sddl: "S:(ML;OICI;NW;;;LW"},
		{name: "audit_ace_then_low_label", sddl: "S:(AU;SA;WD;;;WD)(ML;;NW;;;LW)", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := IsLowLabel(tt.sddl); got != tt.want {
				t.Errorf("IsLowLabel(%q) = %v, want %v", tt.sddl, got, tt.want)
			}
		})
	}
}
