package validated

import (
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/mechanisms"
)

// TestShipped_PinnedAgainstCatalogue is the CI drift guard the ADR 0016 realisation
// names: shipped entries are curation data compiled into the binary, so a catalogue
// change that invalidates one (a removed ID, a changed Requires/IncompatibleWith
// relation) must fail HERE, at build time — never surface as a runtime skip-warning on
// a user's machine.
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
		if err := Validate(e, descriptors); err != nil {
			t.Fatalf("shipped entry %q no longer validates against the catalogue: %v", e.Key, err)
		}
	}
}

// TestShipped_GemmaEntryVerbatim pins the first Validated set (ADR 0016 §6) to the
// catalogue table's exact 16 IDs — recorded verbatim from the Probe manifest
// gemma-4-e4b-it-qat-20260714-minus-truncate-history, NOT derivable from the catalogue
// alone (the base stack's incompatibility picks). The shipped JSON, the catalogue
// table, and this pin must agree three ways.
func TestShipped_GemmaEntryVerbatim(t *testing.T) {
	want := []domain.MechanismID{
		"autofix", "cached_content_intercept", "decompose", "empty_response_recovery",
		"error_enrichment", "filehint", "grammar", "library", "list_nudge", "syntax",
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
