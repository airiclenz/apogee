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
	// the YAML (modelProfileConfig.spellsToolsAxis) and hands it in here; a shipped entry MAY
	// carry a roster, but only one an ADR ratified by name (ADR 0057 decision 6 as amended by
	// ADR 0059 §3 — the Console family on qwen3.8), so the table spells the flag on exactly
	// those entries and leaves it false everywhere else.
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

// spellsThinkingStyle reports whether this entry writes the channel-style half of the thinking
// axis — Style, together with the Start/End delimiter tokens that only mean something under it.
// The domain value answers it alone, for the same reason as the tool-call axis: `style: none` is
// the spelled zero and "" is the unwritten one. Start/End never make the entry speak by
// themselves — tokens without a style name no channel to read them — so they travel with Style
// and are never resolved on their own.
func (e Entry) spellsThinkingStyle() bool { return e.Profile.Thinking.Style != "" }

// spellsThinkingEffort reports whether this entry writes the effort half of the thinking axis.
// Self-describing as well, though differently: "" is the ABSENCE of the dial and the wire anchor
// (ADR 0050), and any of the four words is a spelled value — there is no spelled zero to tell
// apart from the unwritten one, so no config-layer field is needed the way SpellsTools is. The
// two halves resolve independently (ADR 0058): an entry that spells only `effort:` says nothing
// about how the reasoning ARRIVES, so the layer below keeps that word.
func (e Entry) spellsThinkingEffort() bool { return e.Profile.Thinking.Effort != "" }

// spellsTools reports whether this entry writes the roster axis, so all three axes are asked the
// same question the same way at the resolution seam.
func (e Entry) spellsTools() bool { return e.SpellsTools }
