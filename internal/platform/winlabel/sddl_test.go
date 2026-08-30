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
				prior: lowSDDL, // recordEntry decides what the entry may say
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

func TestDescendantClearDecision(t *testing.T) {
	t.Parallel()

	// The teardown walk's decision for one descendant, the mirror of the label walk's above and
	// proven on every OS beside it. The clear writes a NULL SACL, so it must skip exactly the
	// paths the label pass skipped: a hard-linked file's descriptor is shared with every other
	// name of the file, including names outside the box, and clearing it would erase a label
	// apogee never wrote — the foreign label teardown exists to put back. An unreadable count
	// cannot rule that out, so it takes the same rung.
	tests := []struct {
		name            string
		links           uint32
		linksErr        error
		wantShouldClear bool
	}{
		{name: "hard_linked_file_is_not_cleared", links: 2},
		{name: "hard_linked_file_is_skipped_however_many_names_it_has", links: 9},
		{
			name:     "unreadable_link_count_is_not_cleared",
			linksErr: errors.New("access is denied"),
		},
		{
			name:     "unreadable_count_wins_over_a_single_reported_link",
			links:    1,
			linksErr: errors.New("access is denied"),
		},
		{name: "single_named_file_is_cleared", links: 1, wantShouldClear: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := clearDescendantDecision(tt.links, tt.linksErr); got != tt.wantShouldClear {
				t.Errorf("clearDescendantDecision(%d, %v) = %v, want %v",
					tt.links, tt.linksErr, got, tt.wantShouldClear)
			}
		})
	}
}

func TestLabelAndClearSkipTheSameDescendants(t *testing.T) {
	t.Parallel()

	// The invariant the two decisions exist to keep together: every path the label walk refuses
	// over a shared descriptor is a path teardown also refuses to write its NULL SACL over.
	// Were they to drift apart, teardown would clear a record LabelTree never labelled — the
	// foreign label on the far end of a hard link, destroyed by the revert of a box it was
	// never in.
	shared := []descendantFacts{
		{links: 2},
		{links: 9},
		{linksErr: errors.New("access is denied")},
		{prior: "S:AI(ML;;NW;;;ME)", links: 2},
	}

	for _, f := range shared {
		if _, shouldLabel := descendantDecision(f); shouldLabel {
			t.Errorf("facts %+v: shouldLabel = true over a possibly shared descriptor", f)
		}
		if clearDescendantDecision(f.links, f.linksErr) {
			t.Errorf("facts %+v: shouldClear = true over a possibly shared descriptor; teardown would write over a record the label pass refused",
				f)
		}
	}

	// The other direction: a single-named file the label walk labels is one teardown clears
	// back, so the revert stays complete for everything apogee actually touched.
	own := descendantFacts{prior: "S:AI(ML;;NW;;;ME)", links: 1}
	if _, shouldLabel := descendantDecision(own); !shouldLabel {
		t.Fatalf("facts %+v: shouldLabel = false; the fixture no longer exercises a labelled path", own)
	}
	if !clearDescendantDecision(own.links, own.linksErr) {
		t.Errorf("facts %+v: shouldClear = false over a path the label walk labels; the label would be stranded", own)
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
		{name: "own_label", sddl: lowSDDL, want: true},
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
