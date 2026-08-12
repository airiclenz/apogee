package profiles

import "github.com/airiclenz/apogee/internal/domain"

// Entry is one pattern-keyed Model profile: the shape a model whose name contains Pattern
// speaks the wire in. Both the shipped shape table and the user's `model-profiles:` map
// are lists of these, matched by the same rule (ADR 0044) — the only difference is which
// tier they sit in and whether a match announces itself.
type Entry struct {
	// Pattern is matched as a case-insensitive substring of the model name. An empty
	// Pattern never matches: it would otherwise profile every model.
	Pattern string

	// Profile is the WHOLE profile a match applies — both axes. A zero Profile is native
	// tool calls with no inline thinking, which is how a user entry turns a shipped match
	// back off.
	Profile domain.ModelProfile

	// Note is provenance for humans reading the table (which sighting licensed the entry).
	// The runtime never interprets it.
	Note string
}
