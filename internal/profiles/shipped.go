package profiles

import (
	"slices"

	"github.com/airiclenz/apogee/internal/domain"
)

// shippedTable is apogee's built-in shape table (ADR 0044): the known model shapes that
// auto-apply by name so an out-of-the-box run against a known family speaks correctly with
// an empty config.yaml. It grows one entry per sighting, never per guess — an unmatched model
// stays zero-profile pass-through.
//
// Every entry emits native tool calls (the zero ToolCallFormat), so thinking is the only
// wire-shape axis that is ever non-zero here. The ROSTER axis joined it with qwen3.8 (ADR 0059
// §3, amending ADR 0057 decision 6): a shipped entry may carry a roster where an ADR ratified
// that particular one, and only there. A Go literal rather than an embedded bundle: a handful of
// entries need no file format, and a compiled table cannot drift from the domain types it fills.
var shippedTable = []Entry{
	{
		Pattern: "gemma",
		Profile: domain.ModelProfile{
			Thinking: domain.ThinkingProfile{
				Style: domain.ThinkingDelimited,
				Start: "<think>",
				End:   "</think>",
			},
		},
		Note: "gemma family: delimited <think> reasoning (ported oracle vectors)",
	},
	{
		Pattern: "gpt-oss",
		Profile: domain.ModelProfile{
			Thinking: domain.ThinkingProfile{Style: domain.ThinkingHarmony},
		},
		Note: "gpt-oss: harmony channels (live runs)",
	},
	{
		Pattern: "minimax-m3",
		Profile: domain.ModelProfile{
			Thinking: domain.ThinkingProfile{
				Style: domain.ThinkingDelimited,
				Start: "<mm:think>",
				End:   "</mm:think>",
			},
		},
		Note: "minimax-m3: delimited <mm:think> reasoning, often pre-opened by the chat template (session 2026-08-11)",
	},
	{
		Pattern: "qwen3.8",
		Profile: domain.ModelProfile{
			Tools: domain.ToolRosterDelta{
				Enabled: []string{"console_open", "console_send", "console_read", "console_close"},
			},
		},
		SpellsTools: true,
		Note:        "qwen3.8: the Console family — the model that asked for it by name (ADR 0059 §3)",
	},
}

// Shipped returns the built-in shape table as the shipped tier for Resolve. The slice is a
// copy and so are the roster lists inside it: the table is a compiled constant of this binary
// and no caller may edit it. The lists are cloned by hand because they are the only fields a
// shallow copy would still SHARE with the table — every other axis is a string, and the roster
// put the first slice in there (ADR 0059 §3).
func Shipped() []Entry {
	table := make([]Entry, len(shippedTable))
	copy(table, shippedTable)
	for i := range table {
		table[i].Profile.Tools.Enabled = slices.Clone(table[i].Profile.Tools.Enabled)
		table[i].Profile.Tools.Disabled = slices.Clone(table[i].Profile.Tools.Disabled)
	}
	return table
}
