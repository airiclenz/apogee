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
