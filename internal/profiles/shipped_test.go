package profiles

import (
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestShippedTableIsTheVerifiedTrio pins curation: the table grows per sighting, so a new
// entry must arrive with this test updated and its evidence in the Note.
func TestShippedTableIsTheVerifiedTrio(t *testing.T) {
	t.Parallel()

	wantPatterns := []string{"gemma", "gpt-oss", "minimax-m3"}

	got := Shipped()

	if len(got) != len(wantPatterns) {
		t.Fatalf("shipped table has %d entries, want %d: %+v", len(got), len(wantPatterns), got)
	}
	for i, want := range wantPatterns {
		if got[i].Pattern != want {
			t.Errorf("shipped entry %d pattern = %q, want %q", i, got[i].Pattern, want)
		}
	}
}

// TestShippedEntriesAreWellFormed catches the drift a compiled table can still carry: an
// empty pattern (which would never match), a delimited style missing a token (which would
// strip nothing), or an entry with no provenance.
func TestShippedEntriesAreWellFormed(t *testing.T) {
	t.Parallel()

	for _, entry := range Shipped() {
		if entry.Pattern == "" {
			t.Errorf("shipped entry %+v has an empty pattern", entry)
		}
		if entry.Note == "" {
			t.Errorf("shipped entry %q has no note", entry.Pattern)
		}
		if entry.Profile.ToolCallFormat != "" && entry.Profile.ToolCallFormat != domain.FormatNative {
			t.Errorf("shipped entry %q claims tool-call format %q; the trio is all native",
				entry.Pattern, entry.Profile.ToolCallFormat)
		}
		if entry.Profile.Thinking.Style == domain.ThinkingDelimited {
			if entry.Profile.Thinking.Start == "" || entry.Profile.Thinking.End == "" {
				t.Errorf("shipped entry %q is delimited but missing a token: %+v",
					entry.Pattern, entry.Profile.Thinking)
			}
		}
	}
}

// TestShippedReturnsACopy guards the table against a caller that edits what it got back.
func TestShippedReturnsACopy(t *testing.T) {
	t.Parallel()

	first := Shipped()
	first[0].Pattern = "clobbered"

	if second := Shipped(); second[0].Pattern == "clobbered" {
		t.Fatal("Shipped() handed out the table itself; a caller's edit reached the built-in entries")
	}
}
