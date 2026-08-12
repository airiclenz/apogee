package profiles

import "github.com/airiclenz/apogee/internal/domain"

// shippedTable is apogee's built-in shape table (ADR 0044): the known model shapes that
// auto-apply by name so an out-of-the-box run against a known family speaks correctly with
// an empty config.yaml. It launches as the VERIFIED TRIO only and grows one entry per
// sighting, never per guess — an unmatched model stays zero-profile pass-through.
//
// All three emit native tool calls (the zero ToolCallFormat), so only the thinking axis is
// non-zero today. A Go literal rather than an embedded bundle: three entries need no file
// format, and a compiled table cannot drift from the domain types it fills.
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
}

// Shipped returns the built-in shape table as the shipped tier for Resolve. The slice is a
// copy: the table is a compiled constant of this binary and no caller may edit it.
func Shipped() []Entry {
	table := make([]Entry, len(shippedTable))
	copy(table, shippedTable)
	return table
}
