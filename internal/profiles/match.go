package profiles

import (
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// Source names which layer supplied a resolved profile — the caller's cue for the notice
// (ADR 0044: a shipped match announces itself, a user match is silent because the user
// wrote it).
type Source int

const (
	// SourceNone — no pattern matched: the model runs the zero profile, silently. This is
	// the pass-through an unknown model has always had.
	SourceNone Source = iota

	// SourceUser — an entry from the user's `model-profiles:` map matched. It applies
	// silently and outranks every shipped entry.
	SourceUser

	// SourceShipped — an entry from the built-in shape table matched. The caller emits the
	// one-line notice so a wrong match has a first debugging clue.
	SourceShipped
)

// String names the source for notices and test output.
func (s Source) String() string {
	switch s {
	case SourceUser:
		return "user"
	case SourceShipped:
		return "shipped"
	default:
		return "none"
	}
}

// Decision is the outcome of resolving one model name: the profile to apply, which layer
// supplied it, and the winning entry (zero when Source is SourceNone) so the caller can
// name the pattern in its notice.
type Decision struct {
	Profile domain.ModelProfile
	Source  Source
	Entry   Entry
}

// Resolve picks the Model profile for model out of the two tiers — the user's entries
// first, the shipped shape table second — and falls back to the zero profile when neither
// matches. Any user match beats any shipped match, so the tiers are consulted in order
// rather than pooled.
func Resolve(model string, user, shipped []Entry) Decision {
	if entry, ok := best(model, user); ok {
		return Decision{Profile: entry.Profile, Source: SourceUser, Entry: entry}
	}
	if entry, ok := best(model, shipped); ok {
		return Decision{Profile: entry.Profile, Source: SourceShipped, Entry: entry}
	}
	return Decision{}
}

// best returns the winning entry within ONE tier: every entry whose pattern is a
// case-insensitive substring of the model name competes, and outranks decides.
func best(model string, entries []Entry) (Entry, bool) {
	name := strings.ToLower(model)

	var winner Entry
	found := false
	for _, entry := range entries {
		if entry.Pattern == "" || !strings.Contains(name, strings.ToLower(entry.Pattern)) {
			continue
		}
		if !found || outranks(entry.Pattern, winner.Pattern) {
			winner, found = entry, true
		}
	}
	return winner, found
}

// outranks reports whether candidate beats incumbent within a tier: the longer pattern is
// the more specific one and wins; equal lengths break lexicographically (smaller wins) so
// the outcome never depends on the order the entries arrived in.
func outranks(candidate, incumbent string) bool {
	if len(candidate) != len(incumbent) {
		return len(candidate) > len(incumbent)
	}
	return candidate < incumbent
}
