package profiles

import (
	"slices"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestShippedTableHoldsOnlyTheRatifiedEntries pins curation: the table grows per sighting (a
// wire shape) or per ADR (a roster), so a new entry must arrive with this test updated and its
// evidence in the Note.
func TestShippedTableHoldsOnlyTheRatifiedEntries(t *testing.T) {
	t.Parallel()

	wantPatterns := []string{"gemma", "gpt-oss", "minimax-m3", "qwen3.8"}

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

// TestShippedReturnsACopy guards the table against a caller that edits what it got back — the
// entries themselves and, since the roster axis put slices in the table, the lists inside them:
// a shallow copy shares those with the compiled constant.
func TestShippedReturnsACopy(t *testing.T) {
	t.Parallel()

	first := Shipped()
	first[0].Pattern = "clobbered"
	for i := range first {
		for j := range first[i].Profile.Tools.Enabled {
			first[i].Profile.Tools.Enabled[j] = "clobbered"
		}
	}

	second := Shipped()
	if second[0].Pattern == "clobbered" {
		t.Fatal("Shipped() handed out the table itself; a caller's edit reached the built-in entries")
	}
	for _, entry := range second {
		if slices.Contains(entry.Profile.Tools.Enabled, "clobbered") {
			t.Fatalf("shipped entry %q shares its roster list with the table: %+v",
				entry.Pattern, entry.Profile.Tools)
		}
	}
}

// consoleFamily is the roster ADR 0059 §3 ratified for qwen3.8 — the first tools axis any shipped
// entry carries, and the reason ADR 0057 decision 6 gained an amendment.
var consoleFamily = []string{"console_open", "console_send", "console_read", "console_close"}

// TestShippedQwen38CarriesTheConsoleRoster is the ratified entry read end to end: a real advertised
// name resolves to the four Console tools and to NOTHING on the wire-shape axes, because the model
// speaks native tool calls with no inline channel and the table must not invent one for it.
func TestShippedQwen38CarriesTheConsoleRoster(t *testing.T) {
	t.Parallel()

	decision := Resolve("Qwen3.8-27B-Instruct", nil, Shipped())

	if decision.Source != SourceShipped {
		t.Fatalf("source = %s, want shipped (the built-in table supplied the roster)", decision.Source)
	}
	if decision.Entry.Pattern != "qwen3.8" {
		t.Errorf("matched pattern = %q, want qwen3.8", decision.Entry.Pattern)
	}
	if got := decision.Profile.Tools.Enabled; !slices.Equal(got, consoleFamily) {
		t.Errorf("enabled roster = %v, want %v", got, consoleFamily)
	}
	if got := decision.Profile.Tools.Disabled; len(got) != 0 {
		t.Errorf("disabled roster = %v, want none: the entry only lifts", got)
	}
	if got := decision.Profile.ToolCallFormat; got != "" {
		t.Errorf("tool-call format = %q, want the unwritten zero", got)
	}
	if got := decision.Profile.Thinking; got != (domain.ThinkingProfile{}) {
		t.Errorf("thinking axis = %+v, want the unwritten zero", got)
	}
}

// TestAnotherQwenGetsNoRoster pins the pattern's reach: the entry is keyed to the build that asked
// for the Console family, so a sibling Qwen the table does not name stays zero-profile — no roster,
// no notice, exactly as every unmatched model behaves.
func TestAnotherQwenGetsNoRoster(t *testing.T) {
	t.Parallel()

	decision := Resolve("qwen3-32b", nil, Shipped())

	if decision.Source != SourceNone {
		t.Fatalf("source = %s, want none: no pattern covers qwen3-32b", decision.Source)
	}
	if got := decision.Profile.Tools; len(got.Enabled) != 0 || len(got.Disabled) != 0 {
		t.Errorf("roster = %+v, want empty", got)
	}
}

// TestAUserRosterReplacesTheShippedOneWhole is the axis-wise rule applied to the first shipped
// roster: an entry of the user's that spells tools: is the last word on the WHOLE axis, so naming
// one console tool under disabled: does not trim the built-in list — it replaces it, and the
// family is off. That is what the config template has to promise, because it is what a user
// switching the family back off will get.
func TestAUserRosterReplacesTheShippedOneWhole(t *testing.T) {
	t.Parallel()

	user := []Entry{{
		Pattern:     "qwen3.8",
		Profile:     domain.ModelProfile{Tools: domain.ToolRosterDelta{Disabled: []string{"console_open"}}},
		SpellsTools: true,
	}}

	decision := Resolve("Qwen3.8-27B-Instruct", user, Shipped())

	if decision.Source != SourceUser {
		t.Fatalf("source = %s, want user: the user's entry answered the only axis the table spells",
			decision.Source)
	}
	if got := decision.Profile.Tools.Disabled; !slices.Equal(got, []string{"console_open"}) {
		t.Errorf("disabled roster = %v, want [console_open]", got)
	}
	if got := decision.Profile.Tools.Enabled; len(got) != 0 {
		t.Errorf("enabled roster = %v, want none: a spelled tools axis replaces the shipped one whole",
			got)
	}
}
