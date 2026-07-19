package validated

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// shippedJSON is the bench-validated entries compiled into the binary — the ADR 0016
// "shipped entries for bench-validated models". The bundle is an array of entries in the
// same schema user-local files use (one decode path); shipped_test.go pins it against
// the live Mechanism catalogue so curation drift (a removed ID, a changed stacking
// relation) fails CI, never a user's startup.
//
//go:embed shipped.json
var shippedJSON []byte

// Shipped decodes the embedded bundle. A decode failure is a build defect (the pin test
// catches it before release); the caller treats it like any other defective source —
// warn and run at the floor — rather than refusing to start.
func Shipped() ([]Entry, error) {
	var entries []Entry
	if err := json.Unmarshal(shippedJSON, &entries); err != nil {
		return nil, fmt.Errorf("embedded shipped.json: malformed JSON: %w", err)
	}
	for i := range entries {
		if err := checkEntry(entries[i]); err != nil {
			return nil, fmt.Errorf("embedded shipped.json entry %d (%q): %w", i, entries[i].Key, err)
		}
		entries[i].Source = SourceShipped
	}
	return entries, nil
}
