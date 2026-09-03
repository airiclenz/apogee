package validated

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
)

// shippedJSON is the bench-validated entries compiled into the binary — the ADR 0016
// "shipped entries for bench-validated models". The bundle is an array of entries in the
// same schema user-local files use (one decode path); shipped_test.go pins it against
// the live Mechanism catalogue so curation drift (a removed ID, a changed stacking
// relation) fails CI, never a user's startup.
//
// The bundle is EMPTY since v0.20.0. Its one entry, `gemma-4-e4b-it-qat`, named fifteen
// catalogued Mechanisms and every one of them retired in that wave (ADR 0071), so the entry
// had nothing left to enable; a roster row that arms nothing is not curation, it is a broken
// promise. The surface itself stays — a user's own measured entry in `~/.apogee/validated`
// applies exactly as before, and a bench build that registers experimental rows can ship its
// own roster here.
//
//go:embed shipped.json
var shippedJSON []byte

// retiredEntryRow is one entry key apogee once shipped and no longer does, with the release whose
// notes carry its removal. It is the Validated-set twin of the Mechanism roll in
// internal/mechanisms: a config written against the retired key is a config apogee itself offered,
// so the removal must cost the user a notice, never a start (AGENTS.md — a value apogee accepted
// yesterday and rejects today is a regression, never a deferral).
type retiredEntryRow struct {
	Key     string
	Release string
}

// retiredEntryKeys is the roll of retired SHIPPED entry keys. It is read only after the merged
// entries have been searched, so a user-local entry filed under a retired key still applies
// unchanged — the roll answers for the shipped row that went away, never for the user's own.
var retiredEntryKeys = []retiredEntryRow{
	{Key: "gemma-4-e4b-it-qat", Release: "v0.20.0"},
}

// RetiredEntryKeys returns the retired shipped entry keys, sorted, as a fresh slice the caller may
// keep. It exists for the pins that assert the roll and the roster agree.
func RetiredEntryKeys() []string {
	out := make([]string, 0, len(retiredEntryKeys))
	for _, r := range retiredEntryKeys {
		out = append(out, r.Key)
	}
	slices.Sort(out)
	return out
}

// RetiredEntryRelease returns the release whose notes carry key's retirement, or "" when key names
// no retired shipped entry. The empty answer is also the membership test: a caller asks this one
// question rather than a predicate and a lookup that could disagree.
func RetiredEntryRelease(key string) string {
	for _, r := range retiredEntryKeys {
		if r.Key == key {
			return r.Release
		}
	}
	return ""
}

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
