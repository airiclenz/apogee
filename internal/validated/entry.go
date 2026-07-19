package validated

import (
	"encoding/json"
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
)

// EntryVersion is the current on-disk schema version. A file claiming a NEWER version
// was written by a newer apogee and is skipped (soft — the session runs at the floor)
// rather than half-understood; the same posture as the Library store's version envelope.
const EntryVersion = 1

// Entry sources, stamped at load so the per-session notice can name where an applied
// set came from. User-local wins a key collision with shipped: it is the user's own
// evidence, measured on their exact quant.
const (
	SourceShipped = "shipped"
	SourceUser    = "user-local"
)

// Evidence cites the campaign that licensed the entry (CONTEXT "Validated set": a
// completed, pre-registered aggregate Campaign passing the non-inferiority gate with
// engagement verified). It is carried for the notice and for humans reading the file —
// the runtime never interprets it.
type Evidence struct {
	Campaign string `json:"campaign"`
	Note     string `json:"note,omitempty"`
}

// Entry is one Validated set: a per-model enable set keyed by the fingerprint label of
// the model it was measured on (ADR 0016 §3). The set is the exact enable set that
// passed the gate — it applies verbatim or not at all.
type Entry struct {
	Version  int                  `json:"version"`
	Key      string               `json:"key"`
	Set      []domain.MechanismID `json:"set"`
	Evidence Evidence             `json:"evidence"`
	Entered  string               `json:"entered,omitempty"`

	// Source is stamped by the loader (SourceShipped / SourceUser), never read from disk.
	Source string `json:"-"`
}

// decodeEntry parses and shape-checks one entry. The checks here are the always-fatal
// defects a file cannot recover from (bad JSON, wrong version, no key, empty set);
// catalogue-dependent validity (unknown IDs, stacking) is Validate's job, because it
// depends on the binary the entry meets, not on the file.
func decodeEntry(data []byte) (Entry, error) {
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return Entry{}, fmt.Errorf("malformed JSON: %w", err)
	}
	if err := checkEntry(e); err != nil {
		return Entry{}, err
	}
	return e, nil
}

// checkEntry enforces the schema invariants shared by both sources.
func checkEntry(e Entry) error {
	switch {
	case e.Version == 0:
		return fmt.Errorf("missing version (want %d)", EntryVersion)
	case e.Version > EntryVersion:
		return fmt.Errorf("version %d is newer than this apogee understands (%d)", e.Version, EntryVersion)
	case e.Key == "":
		return fmt.Errorf("missing key")
	case len(e.Set) == 0:
		return fmt.Errorf("empty set")
	}
	return nil
}
