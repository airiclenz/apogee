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

	// SourceUser — the user's `model-profiles:` map had the last word on every axis the
	// shipped table also speaks to. It applies silently: the user wrote it.
	SourceUser

	// SourceShipped — the built-in shape table supplied at least one axis of the result (or
	// matched with nothing above it). The caller emits the one-line notice so a shape the
	// human never asked for has a first debugging clue.
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
// gets the credit for it, and that layer's winning entry (zero when Source is SourceNone) so
// the caller can name the pattern in its notice.
//
// Because resolution is axis-wise, the profile can carry axes from BOTH tiers. Source names the
// SHIPPED tier whenever the shipped table still got a word in — that is exactly the case the
// notice exists for, a shape the human never asked for — and the user tier when the user's own
// entry answered everything the shipped one had to say.
type Decision struct {
	Profile domain.ModelProfile
	Source  Source
	Entry   Entry
}

// Resolve picks the Model profile for model AXIS BY AXIS out of the two tiers — the user's
// entries first, the shipped shape table second — with the zero profile as the third layer
// (ADR 0057 decision 5, amending ADR 0044's whole-entry replacement). Each axis independently
// takes the nearest tier whose matching entry SPELLS it; an axis neither spells keeps its zero
// value, and an explicitly spelled zero (`tool-call-format: native`, `thinking: {style: none}`,
// an empty `tools:`) is a word like any other and overrides the tier below.
//
// The match itself is unchanged: within a tier the longest pattern wins, and any user match
// beats any shipped match — so a user entry still outranks the table on every axis it writes.
// What it no longer does is silence the axes it says nothing about: a tools-only entry for a
// gpt-oss build keeps the table's harmony parsing, which whole-entry replacement would have
// wiped without a word.
func Resolve(model string, user, shipped []Entry) Decision {
	userEntry, matchedUser := best(model, user)
	shippedEntry, matchedShipped := best(model, shipped)

	// shippedSpoke records whether the shipped tier supplied any axis of the result, which is
	// what the caller's notice is about — not merely whether the table matched.
	shippedSpoke := false
	supplier := func(spells func(Entry) bool) Entry {
		switch {
		case matchedUser && spells(userEntry):
			return userEntry
		case matchedShipped && spells(shippedEntry):
			shippedSpoke = true
			return shippedEntry
		default:
			// The zero Entry carries the zero profile, so an unspelled axis reads its third layer
			// straight off it.
			return Entry{}
		}
	}

	toolCall := supplier(Entry.spellsToolCall)
	profile := domain.ModelProfile{
		ToolCallFormat: toolCall.Profile.ToolCallFormat,
		Pattern:        toolCall.Profile.Pattern,
		Thinking:       supplier(Entry.spellsThinking).Profile.Thinking,
		Tools:          supplier(Entry.spellsTools).Profile.Tools,
	}

	switch {
	case matchedShipped && (shippedSpoke || !matchedUser):
		return Decision{Profile: profile, Source: SourceShipped, Entry: shippedEntry}
	case matchedUser:
		return Decision{Profile: profile, Source: SourceUser, Entry: userEntry}
	default:
		return Decision{}
	}
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
