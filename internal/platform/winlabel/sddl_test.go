package winlabel

import (
	"errors"
	"testing"
)

func TestDescendantLabelDecision(t *testing.T) {
	t.Parallel()

	// The label walk's three-way decision for one descendant, proven on every OS. Two rungs
	// matter most, and both are skips. A prior-read ERROR: labelling a path whose prior could
	// not be read would destroy a possibly-foreign label with no journalled record to put it
	// back. A hard-linked file: every name of a file shares one NTFS security descriptor, so
	// labelling the in-box name would mark the file Low at names outside the box — pnpm's
	// node_modules is entirely hard links into a global store under %LOCALAPPDATA%. Either
	// way the path is skipped entirely — no journal entry AND no label.
	tests := []struct {
		name              string
		facts             descendantFacts
		wantShouldJournal bool
		wantShouldLabel   bool
	}{
		{
			name:  "read_error_skips_journal_and_label",
			facts: descendantFacts{priorErr: errors.New("access is denied"), links: 1},
		},
		{
			name: "read_error_wins_even_over_a_leftover_prior",
			facts: descendantFacts{
				prior:    "S:AI(ML;;NW;;;ME)",
				priorErr: errors.New("access is denied"),
				links:    1,
			},
		},
		{
			name:  "hard_linked_file_skips_journal_and_label",
			facts: descendantFacts{links: 2},
		},
		{
			name:  "hard_linked_file_is_skipped_however_many_names_it_has",
			facts: descendantFacts{links: 9},
		},
		{
			name: "hard_link_count_wins_over_a_journallable_prior",
			facts: descendantFacts{
				prior: "S:AI(ML;;NW;;;ME)", // a shared descriptor is not apogee's to rewrite
				links: 2,
			},
		},
		{
			name:  "unreadable_link_count_skips_journal_and_label",
			facts: descendantFacts{linksErr: errors.New("access is denied")},
		},
		{
			name: "foreign_prior_is_journalled_then_labelled",
			facts: descendantFacts{
				prior: "S:AI(ML;;NW;;;ME)",
				links: 1,
			},
			wantShouldJournal: true,
			wantShouldLabel:   true,
		},
		{
			name: "own_low_prior_still_passes_through_the_journal",
			facts: descendantFacts{
				prior: fileSDDL, // recordEntry decides what the entry may say
				links: 1,
			},
			wantShouldJournal: true,
			wantShouldLabel:   true,
		},
		{
			name:            "empty_prior_labels_only",
			facts:           descendantFacts{links: 1},
			wantShouldLabel: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			shouldJournal, shouldLabel := descendantDecision(tt.facts)
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
		{name: "own_dir_label", sddl: dirSDDL, want: true},
		{name: "own_file_label", sddl: fileSDDL, want: true},
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
