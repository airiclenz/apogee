package validated

import (
	"slices"
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
//
// The roster is EMPTY since v0.20.0, so this walks nothing today; it stays because the pin
// must already be standing on the day a bench-validated entry is added back, and because an
// entry that decodes but fails checkEntry would still fail Shipped() above.
func TestShipped_PinnedAgainstCatalogue(t *testing.T) {
	entries, err := Shipped()
	if err != nil {
		t.Fatalf("Shipped: %v", err)
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
		if len(live.Set) == 0 {
			t.Fatalf("shipped entry %q sheds to nothing: a roster row that arms no live Mechanism "+
				"promises what it cannot deliver — retire the entry key instead", e.Key)
		}
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
//
// The entry is SYNTHETIC because the shipped roster is empty since v0.20.0: what is under test
// is the pin's own logic (shed the rolled, still trip on the un-rolled), which must keep
// standing while there is no roster row to borrow.
func TestShipped_RemovalWithoutARollEntryStillTrips(t *testing.T) {
	t.Parallel()

	// One rolled member and one live member, in the shape a roster entry has.
	rolled := mechanisms.RetiredIDs()
	if len(rolled) == 0 {
		t.Fatal("the retired roll is empty; this pin needs one rolled ID to shed")
	}
	entry := Entry{Version: EntryVersion, Key: "synthetic-model", Set: []domain.MechanismID{rolled[0], "live_row"}}
	catalogue := []domain.MechanismDescriptor{{ID: "live_row"}}

	live, dropped := DropRetired(entry, rolled)
	if len(dropped) != 1 || dropped[0] != rolled[0] {
		t.Fatalf("dropped = %v, want the rolled member %q shed", dropped, rolled[0])
	}
	if err := Validate(live, catalogue); err != nil {
		t.Fatalf("the shed entry must validate against the catalogue that still carries its live member: %v", err)
	}

	// Now the live member disappears from the catalogue with no roll entry to license it.
	err := Validate(live, nil)
	if err == nil {
		t.Fatalf("entry %q validated against a catalogue missing %q; an un-rolled removal must trip the pin",
			live.Key, "live_row")
	}
	if !strings.Contains(err.Error(), "live_row") {
		t.Errorf("Validate error = %v, want it to name the removed mechanism %q", err, "live_row")
	}
}

// TestShipped_RosterIsEmptyAndTheGemmaEntryIsRolled pins what replaced the gemma set. Every one of
// the fifteen IDs it named retired in v0.20.0 (ADR 0071), so the entry had nothing left to enable
// and the roster ships empty; the key it was filed under is on the entry roll, which is what keeps
// a config carrying the alias apogee itself offered starting rather than refusing.
func TestShipped_RosterIsEmptyAndTheGemmaEntryIsRolled(t *testing.T) {
	t.Parallel()

	entries, err := Shipped()
	if err != nil {
		t.Fatalf("Shipped: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("shipped roster = %d entries, want none: the catalogue is empty since v0.20.0", len(entries))
	}

	// The fifteen the gemma entry named, verbatim from the retired shipped.json.
	gemmaSet := []domain.MechanismID{
		"autofix", "cached_content_intercept", "decompose", "empty_response_recovery",
		"error_enrichment", "filehint", "library", "list_nudge", "syntax",
		"tool_loop_interceptor", "tool_result_cap", "tool_use_directive",
		"tool_use_enforcer", "toolfilter", "validate",
	}
	roll := mechanisms.RetiredIDs()
	for _, id := range gemmaSet {
		if !slices.Contains(roll, id) {
			t.Errorf("gemma-set member %q is not on the retired roll; a set member that vanished "+
				"un-rolled would refuse a user's saved record instead of shedding from it", id)
		}
	}

	if got := RetiredEntryKeys(); !slices.Contains(got, "gemma-4-e4b-it-qat") {
		t.Errorf("RetiredEntryKeys() = %v, want the retired gemma entry key on the roll", got)
	}
	if got := RetiredEntryRelease("gemma-4-e4b-it-qat"); got != "v0.20.0" {
		t.Errorf("RetiredEntryRelease = %q, want the wave's release v0.20.0", got)
	}
	if got := RetiredEntryRelease("some-model-we-never-shipped"); got != "" {
		t.Errorf("RetiredEntryRelease of an unrolled key = %q, want the empty answer", got)
	}
}
