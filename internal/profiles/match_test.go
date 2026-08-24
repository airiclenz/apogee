package profiles

import (
	"reflect"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// delimited builds a thinking-only profile with the given tokens — the shape most entries
// carry (native tool calls, one delimiter pair).
func delimited(start, end string) domain.ModelProfile {
	return domain.ModelProfile{
		Thinking: domain.ThinkingProfile{Style: domain.ThinkingDelimited, Start: start, End: end},
	}
}

// Resolution is AXIS-WISE (ADR 0057 decision 5): every case below fixes which tier supplies which
// axis, and the Source it reports is the tier the caller narrates — the shipped one whenever the
// table still got a word in.
func TestResolve(t *testing.T) {
	t.Parallel()

	harmony := domain.ModelProfile{Thinking: domain.ThinkingProfile{Style: domain.ThinkingHarmony}}

	tests := []struct {
		name        string
		model       string
		user        []Entry
		shipped     []Entry
		wantSource  Source
		wantPattern string
		wantProfile domain.ModelProfile
	}{
		{
			name: "live minimax spelling matches the shipped entry", model: "minimax/minimax-m3:exacto",
			shipped: Shipped(), wantSource: SourceShipped, wantPattern: "minimax-m3",
			wantProfile: delimited("<mm:think>", "</mm:think>"),
		},
		{
			name: "gguf-ish minimax spelling matches the shipped entry", model: "minimax-m3-Q4_K_M",
			shipped: Shipped(), wantSource: SourceShipped, wantPattern: "minimax-m3",
			wantProfile: delimited("<mm:think>", "</mm:think>"),
		},
		{
			name: "gpt-oss-20b matches harmony", model: "gpt-oss-20b",
			shipped: Shipped(), wantSource: SourceShipped, wantPattern: "gpt-oss", wantProfile: harmony,
		},
		{
			name: "gemma quant spelling matches the delimited entry", model: "gemma-4-e4b-it-qat",
			shipped: Shipped(), wantSource: SourceShipped, wantPattern: "gemma",
			wantProfile: delimited("<think>", "</think>"),
		},
		{
			name: "matching is case-insensitive both ways", model: "MiniMax/MiniMax-M3:Exacto",
			shipped:    []Entry{{Pattern: "MINIMAX-M3", Profile: delimited("<mm:think>", "</mm:think>")}},
			wantSource: SourceShipped, wantPattern: "MINIMAX-M3", wantProfile: delimited("<mm:think>", "</mm:think>"),
		},
		{
			name: "longest pattern wins within a tier", model: "minimax-m3-Q4_K_M",
			user: []Entry{
				{Pattern: "minimax", Profile: delimited("<short>", "</short>")},
				{Pattern: "minimax-m3", Profile: delimited("<long>", "</long>")},
			},
			wantSource: SourceUser, wantPattern: "minimax-m3", wantProfile: delimited("<long>", "</long>"),
		},
		{
			name: "equal-length patterns break lexicographically", model: "gpt-oss-abc-xyz",
			user: []Entry{
				{Pattern: "xyz", Profile: delimited("<xyz>", "</xyz>")},
				{Pattern: "abc", Profile: delimited("<abc>", "</abc>")},
			},
			wantSource: SourceUser, wantPattern: "abc", wantProfile: delimited("<abc>", "</abc>"),
		},
		{
			name: "a shorter user pattern beats a longer shipped one", model: "minimax-m3-Q4_K_M",
			user:    []Entry{{Pattern: "m3", Profile: delimited("<mine>", "</mine>")}},
			shipped: Shipped(), wantSource: SourceUser, wantPattern: "m3",
			wantProfile: delimited("<mine>", "</mine>"),
		},
		{
			name: "a user entry with no thinking turns a shipped match off", model: "minimax-m3-Q4_K_M",
			user:       []Entry{{Pattern: "minimax-m3", Profile: domain.ModelProfile{Thinking: domain.ThinkingProfile{Style: domain.ThinkingNone}}}},
			shipped:    Shipped(),
			wantSource: SourceUser, wantPattern: "minimax-m3",
			wantProfile: domain.ModelProfile{Thinking: domain.ThinkingProfile{Style: domain.ThinkingNone}},
		},
		{
			name: "an unknown model resolves to the zero profile", model: "qwen3.6-27b",
			user: []Entry{{Pattern: "llama", Profile: harmony}}, shipped: Shipped(),
			wantSource: SourceNone,
		},
		{
			name: "an empty pattern never matches", model: "qwen3.6-27b",
			user: []Entry{{Pattern: "", Profile: harmony}}, wantSource: SourceNone,
		},
		{
			name: "an empty model name matches nothing", model: "",
			user: []Entry{{Pattern: "gemma", Profile: harmony}}, shipped: Shipped(), wantSource: SourceNone,
		},
		{
			name: "a non-native tool-call format rides the same match", model: "custom-caller-7b",
			user:       []Entry{{Pattern: "custom-caller", Profile: domain.ModelProfile{ToolCallFormat: domain.FormatMarkdownFenced}}},
			wantSource: SourceUser, wantPattern: "custom-caller",
			wantProfile: domain.ModelProfile{ToolCallFormat: domain.FormatMarkdownFenced},
		},
		{
			// The trap axis-wise resolution exists to close (ADR 0057 decision 5): the LIKELY user
			// entry is a tools-only line for a model whose wire shape the table already carries.
			// Whole-entry replacement would have wiped the harmony parsing without a word; the
			// shipped tier still supplied that axis, so it still gets the notice.
			name: "a tools-only user entry keeps the shipped thinking axis", model: "gpt-oss-20b",
			user: []Entry{{
				Pattern:     "gpt-oss-20b",
				Profile:     domain.ModelProfile{Tools: domain.ToolRosterDelta{Enabled: []string{"web_search"}}},
				SpellsTools: true,
			}},
			shipped: Shipped(), wantSource: SourceShipped, wantPattern: "gpt-oss",
			wantProfile: domain.ModelProfile{
				Thinking: domain.ThinkingProfile{Style: domain.ThinkingHarmony},
				Tools:    domain.ToolRosterDelta{Enabled: []string{"web_search"}},
			},
		},
		{
			// The other half of the same rule: an entry that spells NOTHING says nothing about any
			// axis, so every one of them defers to the table (superseding ADR 0044 decision 4, under
			// which this same entry wiped the shipped profile).
			name: "a user entry that spells no axis defers all three", model: "minimax-m3-Q4_K_M",
			user: []Entry{{Pattern: "minimax-m3"}}, shipped: Shipped(),
			wantSource: SourceShipped, wantPattern: "minimax-m3",
			wantProfile: delimited("<mm:think>", "</mm:think>"),
		},
		{
			// Axes travel independently: the user's tool-call format and the table's thinking style
			// resolve into ONE profile, each from the nearest tier that spells it.
			name: "each axis takes its own nearest tier", model: "gemma-4-e4b-it-qat",
			user: []Entry{{
				Pattern: "gemma",
				Profile: domain.ModelProfile{ToolCallFormat: domain.FormatMarkdownFenced},
			}},
			shipped: Shipped(), wantSource: SourceShipped, wantPattern: "gemma",
			wantProfile: domain.ModelProfile{
				ToolCallFormat: domain.FormatMarkdownFenced,
				Thinking:       domain.ThinkingProfile{Style: domain.ThinkingDelimited, Start: "<think>", End: "</think>"},
			},
		},
		{
			// The tool-call pattern is part of the format's axis rather than an axis of its own: a
			// regex from one tier under a format from another could never fire, so the pair moves
			// together — here from the shipped tier, under a user entry that spells only thinking.
			name: "the tool-call pattern travels with the format it belongs to", model: "regex-caller-7b",
			user: []Entry{{
				Pattern: "regex-caller",
				Profile: domain.ModelProfile{Thinking: domain.ThinkingProfile{Style: domain.ThinkingNone}},
			}},
			shipped: []Entry{{Pattern: "regex-caller", Profile: domain.ModelProfile{
				ToolCallFormat: domain.FormatCustomRegex, Pattern: `(?P<tool>\w+)`,
			}}},
			wantSource: SourceShipped, wantPattern: "regex-caller",
			wantProfile: domain.ModelProfile{
				ToolCallFormat: domain.FormatCustomRegex,
				Pattern:        `(?P<tool>\w+)`,
				Thinking:       domain.ThinkingProfile{Style: domain.ThinkingNone},
			},
		},
		{
			// An explicitly spelled zero is a word like any other and overrides the tier below —
			// which for the roster axis is `tools:` written with empty lists, the one axis whose
			// presence the domain value cannot carry.
			name: "an empty tools axis overrides a deeper roster", model: "profiled-7b",
			user: []Entry{{Pattern: "profiled", SpellsTools: true}},
			shipped: []Entry{{
				Pattern:     "profiled",
				Profile:     domain.ModelProfile{Tools: domain.ToolRosterDelta{Disabled: []string{"web_search"}}},
				SpellsTools: true,
			}},
			wantSource: SourceUser, wantPattern: "profiled",
		},
		{
			// The thinking axis resolves as TWO sub-axes (ADR 0058) — this is the ratifying case.
			// An `effort:`-only entry is the taught idiom, and under a whole-axis rule it wiped the
			// table's harmony parsing: the user spells only how MUCH to think, so the shipped tier
			// keeps its word on how the reasoning arrives, and it still gets the notice.
			name: "an effort-only user entry keeps the shipped channel style", model: "gpt-oss-20b",
			user: []Entry{{
				Pattern: "gpt-oss-20b",
				Profile: domain.ModelProfile{Thinking: domain.ThinkingProfile{Effort: domain.EffortLow}},
			}},
			shipped: Shipped(), wantSource: SourceShipped, wantPattern: "gpt-oss",
			wantProfile: domain.ModelProfile{
				Thinking: domain.ThinkingProfile{Style: domain.ThinkingHarmony, Effort: domain.EffortLow},
			},
		},
		{
			// The mirror: a style-only entry says nothing about the dial, so a shipped effort
			// survives underneath the user's channel style.
			name: "a style-only user entry keeps a shipped effort", model: "dialled-7b",
			user: []Entry{{
				Pattern: "dialled",
				Profile: domain.ModelProfile{Thinking: domain.ThinkingProfile{Style: domain.ThinkingHarmony}},
			}},
			shipped: []Entry{{
				Pattern: "dialled",
				Profile: domain.ModelProfile{Thinking: domain.ThinkingProfile{Effort: domain.EffortHigh}},
			}},
			wantSource: SourceShipped, wantPattern: "dialled",
			wantProfile: domain.ModelProfile{
				Thinking: domain.ThinkingProfile{Style: domain.ThinkingHarmony, Effort: domain.EffortHigh},
			},
		},
		{
			// The spelled zero is still a word on ITS half only: `style: none` turns the shipped
			// channel off and leaves the shipped dial where it was.
			name: "a spelled none style overrides the channel and not the effort", model: "gpt-oss-20b",
			user: []Entry{{
				Pattern: "gpt-oss-20b",
				Profile: domain.ModelProfile{Thinking: domain.ThinkingProfile{Style: domain.ThinkingNone}},
			}},
			shipped: []Entry{{
				Pattern: "gpt-oss",
				Profile: domain.ModelProfile{Thinking: domain.ThinkingProfile{Style: domain.ThinkingHarmony, Effort: domain.EffortMedium}},
			}},
			wantSource: SourceShipped, wantPattern: "gpt-oss",
			wantProfile: domain.ModelProfile{
				Thinking: domain.ThinkingProfile{Style: domain.ThinkingNone, Effort: domain.EffortMedium},
			},
		},
		{
			// Start/End travel with Style: a user style brings its own tokens and replaces all three,
			// while the shipped dial underneath is untouched.
			name: "a user style replaces the shipped tokens with its own", model: "minimax-m3-Q4_K_M",
			user: []Entry{{Pattern: "minimax-m3", Profile: delimited("<mine>", "</mine>")}},
			shipped: []Entry{{
				Pattern: "minimax-m3",
				Profile: domain.ModelProfile{Thinking: domain.ThinkingProfile{
					Style: domain.ThinkingDelimited, Start: "<mm:think>", End: "</mm:think>", Effort: domain.EffortHigh,
				}},
			}},
			wantSource: SourceShipped, wantPattern: "minimax-m3",
			wantProfile: domain.ModelProfile{Thinking: domain.ThinkingProfile{
				Style: domain.ThinkingDelimited, Start: "<mine>", End: "</mine>", Effort: domain.EffortHigh,
			}},
		},
		{
			// ... and never alone: an effort-only entry leaves Style AND both tokens at the shipped
			// values, because tokens are meaningless without the style that reads them.
			name: "an effort-only user entry keeps the shipped tokens", model: "minimax-m3-Q4_K_M",
			user: []Entry{{
				Pattern: "minimax-m3",
				Profile: domain.ModelProfile{Thinking: domain.ThinkingProfile{Effort: domain.EffortOff}},
			}},
			shipped: Shipped(), wantSource: SourceShipped, wantPattern: "minimax-m3",
			wantProfile: domain.ModelProfile{Thinking: domain.ThinkingProfile{
				Style: domain.ThinkingDelimited, Start: "<mm:think>", End: "</mm:think>", Effort: domain.EffortOff,
			}},
		},
		{
			// Tokens without a style do not spell the channel half at all: the shipped style wins
			// and the orphaned tokens are dropped rather than grafted under it.
			name: "tokens without a style never resolve on their own", model: "gpt-oss-20b",
			user: []Entry{{
				Pattern: "gpt-oss-20b",
				Profile: domain.ModelProfile{Thinking: domain.ThinkingProfile{Start: "<orphan>", End: "</orphan>"}},
			}},
			shipped: Shipped(), wantSource: SourceShipped, wantPattern: "gpt-oss", wantProfile: harmony,
		},
		{
			// An entry spelling BOTH halves is still that entry's word on both — the whole-axis
			// outcome is unchanged when the entry actually writes the whole axis.
			name: "a user entry spelling both halves owns the whole axis", model: "gpt-oss-20b",
			user: []Entry{{
				Pattern: "gpt-oss-20b",
				Profile: domain.ModelProfile{Thinking: domain.ThinkingProfile{Style: domain.ThinkingNone, Effort: domain.EffortLow}},
			}},
			shipped: Shipped(), wantSource: SourceUser, wantPattern: "gpt-oss-20b",
			wantProfile: domain.ModelProfile{
				Thinking: domain.ThinkingProfile{Style: domain.ThinkingNone, Effort: domain.EffortLow},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := Resolve(tc.model, tc.user, tc.shipped)

			if got.Source != tc.wantSource {
				t.Fatalf("Resolve(%q) source = %s, want %s", tc.model, got.Source, tc.wantSource)
			}
			if got.Entry.Pattern != tc.wantPattern {
				t.Errorf("Resolve(%q) entry pattern = %q, want %q", tc.model, got.Entry.Pattern, tc.wantPattern)
			}
			if !reflect.DeepEqual(got.Profile, tc.wantProfile) {
				t.Errorf("Resolve(%q) profile = %+v, want %+v", tc.model, got.Profile, tc.wantProfile)
			}
		})
	}
}

func TestResolveNoMatchYieldsTheZeroProfile(t *testing.T) {
	t.Parallel()

	got := Resolve("unheard-of-model", nil, Shipped())

	if !reflect.DeepEqual(got, Decision{}) {
		t.Fatalf("Resolve of an unmatched model = %+v, want the zero Decision", got)
	}
}

func TestSourceString(t *testing.T) {
	t.Parallel()

	tests := map[Source]string{SourceNone: "none", SourceUser: "user", SourceShipped: "shipped"}

	for source, want := range tests {
		if got := source.String(); got != want {
			t.Errorf("Source(%d).String() = %q, want %q", int(source), got, want)
		}
	}
}
