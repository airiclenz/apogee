package validated

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/mechanisms"
)

// TestShipped_PinnedAgainstCatalogue is the CI drift guard the ADR 0016 realisation
// names: shipped entries are curation data compiled into the binary, so a catalogue
// change that invalidates one (a removed ID, a changed Requires/IncompatibleWith
// relation) must fail HERE, at build time — never surface as a runtime skip-warning on
// a user's machine.
//
// RETIRED members are shed first, exactly as the runtime path does
// (cmd/apogee/validatedsets.go): a removal recorded on the roll is a curation change the
// entry survives, so failing on it here would fail the very case ADR 0016's 2026-08-29
// amendment exists to permit. A removal with NO roll entry still trips this pin —
// TestShipped_RemovalWithoutARollEntryStillTrips is the other half of that claim.
func TestShipped_PinnedAgainstCatalogue(t *testing.T) {
	entries, err := Shipped()
	if err != nil {
		t.Fatalf("Shipped: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("shipped bundle is empty — the gemma entry should exist")
	}

	descriptors := mechanisms.Descriptors()
	seen := map[string]bool{}
	for _, e := range entries {
		if seen[e.Key] {
			t.Fatalf("duplicate shipped key %q", e.Key)
		}
		seen[e.Key] = true
		if e.Source != SourceShipped {
			t.Fatalf("entry %q: Source not stamped shipped: %q", e.Key, e.Source)
		}
		live, _ := DropRetired(e, mechanisms.RetiredIDs())
		if err := Validate(live, descriptors); err != nil {
			t.Fatalf("shipped entry %q no longer validates against the catalogue: %v", e.Key, err)
		}
	}
}

// TestShipped_RemovalWithoutARollEntryStillTrips is the other half of the drift guard.
// TestShipped_PinnedAgainstCatalogue sheds retired members before validating, so a removal
// the roll records passes; a removal the roll does NOT record must still fail, or the
// relaxation would hide exactly the drift the pin exists to catch. A catalogue missing one
// live member of a shipped entry — which is what deleting a row without rolling it leaves
// behind — is simulated by thinning the descriptor list.
func TestShipped_RemovalWithoutARollEntryStillTrips(t *testing.T) {
	entries, err := Shipped()
	if err != nil {
		t.Fatalf("Shipped: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("shipped bundle is empty — the gemma entry should exist")
	}

	live, _ := DropRetired(entries[0], mechanisms.RetiredIDs())
	if len(live.Set) == 0 {
		t.Fatalf("shipped entry %q has no live members to remove", live.Key)
	}
	gone := live.Set[0]

	var thinned []domain.MechanismDescriptor
	for _, d := range mechanisms.Descriptors() {
		if d.ID != gone {
			thinned = append(thinned, d)
		}
	}

	err = Validate(live, thinned)
	if err == nil {
		t.Fatalf("entry %q validated against a catalogue missing %q; an un-rolled removal must trip the pin",
			live.Key, gone)
	}
	if !strings.Contains(err.Error(), string(gone)) {
		t.Errorf("Validate error = %v, want it to name the removed mechanism %q", err, gone)
	}
}

// TestShipped_GemmaEntryVerbatim pins the first Validated set (ADR 0016 §6) to the
// catalogue table's exact 15 IDs: the 16 recorded verbatim from the Probe manifest
// gemma-4-e4b-it-qat-20260714-minus-truncate-history, minus `grammar` — retired
// 2026-08-29 as inert on every backend, which is exactly the case ADR 0016's amendment
// lets a set shed without touching the evidence behind it. The remaining 15 are NOT
// derivable from the catalogue alone (the base stack's incompatibility picks). The
// shipped JSON, the catalogue table, and this pin must agree three ways.
func TestShipped_GemmaEntryVerbatim(t *testing.T) {
	want := []domain.MechanismID{
		"autofix", "cached_content_intercept", "decompose", "empty_response_recovery",
		"error_enrichment", "filehint", "library", "list_nudge", "syntax",
		"tool_loop_interceptor", "tool_result_cap", "tool_use_directive",
		"tool_use_enforcer", "toolfilter", "validate",
	}

	entries, err := Shipped()
	if err != nil {
		t.Fatalf("Shipped: %v", err)
	}
	var gemma *Entry
	for i := range entries {
		if entries[i].Key == "gemma-4-e4b-it-qat" {
			gemma = &entries[i]
			break
		}
	}
	if gemma == nil {
		t.Fatal("no shipped entry for gemma-4-e4b-it-qat")
	}
	if len(gemma.Set) != len(want) {
		t.Fatalf("gemma set: want %d IDs, got %d", len(want), len(gemma.Set))
	}
	for i, id := range want {
		if gemma.Set[i] != id {
			t.Fatalf("gemma set[%d]: want %q, got %q", i, id, gemma.Set[i])
		}
	}
	if gemma.Evidence.Campaign != "gemma-4-e4b-it-qat-20260714-minus-truncate-history" {
		t.Fatalf("gemma evidence campaign: got %q", gemma.Evidence.Campaign)
	}
}
