package profiles

import "github.com/airiclenz/apogee/internal/domain"

// Entry is one pattern-keyed Model profile: the shape a model whose name contains Pattern
// speaks the wire in, and the roster it is equipped with. Both the shipped shape table and
// the user's `model-profiles:` map are lists of these, matched by the same rule (ADR 0044)
// — the only difference is which tier they sit in and whether a match announces itself.
type Entry struct {
	// Pattern is matched as a case-insensitive substring of the model name. An empty
	// Pattern never matches: it would otherwise profile every model.
	Pattern string

	// Profile is what this entry has to say about the three axes. Only the axes it SPELLS
	// are its word (see the spells* predicates below): resolution is axis-wise, so an axis
	// this entry leaves out defers to the next tier rather than being turned off here
	// (ADR 0057 decision 5, amending ADR 0044 decision 4).
	Profile domain.ModelProfile

	// SpellsTools reports whether the entry WRITES the roster axis — `tools:` present, empty
	// lists included. It is a field rather than a reading of Profile because the roster is the
	// one axis whose domain value cannot answer the question: an absent axis and an explicitly
	// empty one both project to the zero ToolRosterDelta, while the other two axes have a
	// spelled zero distinct from their unwritten one (`tool-call-format: native`,
	// `thinking: {style: none}`). The fact belongs to the FILE, so the config layer reads it off
	// the YAML (modelProfileConfig.spellsToolsAxis) and hands it in here; a shipped entry carries
	// no roster at all (ADR 0057 decision 6), so the table leaves it false.
	SpellsTools bool

	// Note is provenance for humans reading the table (which sighting licensed the entry).
	// The runtime never interprets it.
	Note string
}

// spellsToolCall reports whether this entry writes the tool-call axis. The domain value answers
// it alone: "" is the unwritten format and `native` is the spelled zero, so an entry pinning a
// model back to native tool calls overrides a deeper layer while one that omits the key defers.
// The pattern is part of this axis rather than one of its own — it is read only under
// custom-regex, so a pattern from one layer under a format from another could never fire.
func (e Entry) spellsToolCall() bool { return e.Profile.ToolCallFormat != "" }

// spellsThinking reports whether this entry writes the thinking axis. Self-describing for the
// same reason: `style: none` is the spelled zero and "" is the unwritten one, and an entry that
// sets only `effort:` still has a word to say about the axis.
func (e Entry) spellsThinking() bool { return e.Profile.Thinking != (domain.ThinkingProfile{}) }

// spellsTools reports whether this entry writes the roster axis, so all three axes are asked the
// same question the same way at the resolution seam.
func (e Entry) spellsTools() bool { return e.SpellsTools }
