package processing

import (
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestParserFor_SelectsParserByFormat pins that ParserFor maps the domain tool-call format onto
// the frozen factory's concrete parser — the boundary translation D2 relies on.
func TestParserFor_SelectsParserByFormat(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		profile domain.ModelProfile
		typeOf  string
	}{
		{"zero profile is native no-op", domain.ModelProfile{}, "processing.nativeTextParser"},
		{"explicit native is no-op", domain.ModelProfile{ToolCallFormat: domain.FormatNative}, "processing.nativeTextParser"},
		{"markdown-fenced", domain.ModelProfile{ToolCallFormat: domain.FormatMarkdownFenced}, "*processing.MarkdownFencedParser"},
		{
			"custom-regex carries the pattern",
			domain.ModelProfile{ToolCallFormat: domain.FormatCustomRegex, Pattern: toolCallPattern},
			"*processing.CustomRegexParser",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			parser, stripper, err := ParserFor(tc.profile)
			if err != nil {
				t.Fatalf("ParserFor returned error: %v", err)
			}
			if got := typeName(parser); got != tc.typeOf {
				t.Errorf("parser type = %s, want %s", got, tc.typeOf)
			}
			if stripper == nil {
				t.Error("stripper is nil")
			}
		})
	}
}

// TestParserFor_CustomRegexPatternReaches proves the domain Pattern threads through to the
// selected custom-regex parser so a configured model actually parses its calls.
func TestParserFor_CustomRegexPatternReaches(t *testing.T) {
	t.Parallel()

	parser, _, err := ParserFor(domain.ModelProfile{ToolCallFormat: domain.FormatCustomRegex, Pattern: toolCallPattern})
	if err != nil {
		t.Fatalf("ParserFor: %v", err)
	}
	call, ok := parser.ParseToolCall(`before <tool_call>read_file({"path":"a.go"})</tool_call>`)
	if !ok {
		t.Fatal("expected the pattern to parse a call")
	}
	if call.Tool != "read_file" {
		t.Errorf("Tool = %q, want read_file", call.Tool)
	}
}

// TestParserFor_UnknownFormatErrors proves a bad tool-call format fails construction loudly (D2).
func TestParserFor_UnknownFormatErrors(t *testing.T) {
	t.Parallel()

	if _, _, err := ParserFor(domain.ModelProfile{ToolCallFormat: "yaml-block"}); err == nil {
		t.Fatal("expected an error for an unknown tool-call format")
	}
}

// TestParserFor_UnknownThinkingStyleErrors proves a bad thinking style fails construction (D2).
func TestParserFor_UnknownThinkingStyleErrors(t *testing.T) {
	t.Parallel()

	profile := domain.ModelProfile{Thinking: domain.ThinkingProfile{Style: "brainwave"}}
	if _, _, err := ParserFor(profile); err == nil {
		t.Fatal("expected an error for an unknown thinking style")
	}
}

func TestParserFor_StripperByStyle(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		thinking      domain.ThinkingProfile
		raw           string
		wantVisible   string
		wantReasoning string
		wantMid       bool
	}{
		{
			name:        "none passes content through untouched",
			thinking:    domain.ThinkingProfile{},
			raw:         "<think>x</think>plain",
			wantVisible: "<think>x</think>plain",
		},
		{
			name:          "delimited strips the token pair",
			thinking:      domain.ThinkingProfile{Style: domain.ThinkingDelimited, Start: "<think>", End: "</think>"},
			raw:           "<think>reasoning here</think>Here is the answer.",
			wantVisible:   "Here is the answer.",
			wantReasoning: "reasoning here",
		},
		{
			name:          "delimited mid-channel guard on an unclosed span",
			thinking:      domain.ThinkingProfile{Style: domain.ThinkingDelimited, Start: "<think>", End: "</think>"},
			raw:           "<think>still going",
			wantVisible:   "",
			wantReasoning: "still going",
			wantMid:       true,
		},
		{
			name:          "harmony routes analysis to reasoning and final to visible",
			thinking:      domain.ThinkingProfile{Style: domain.ThinkingHarmony},
			raw:           "<|channel|>analysis<|message|>let me think<|end|><|channel|>final<|message|>the answer<|end|>",
			wantVisible:   "the answer",
			wantReasoning: "let me think",
		},
		{
			name:          "harmony folds commentary into reasoning after analysis",
			thinking:      domain.ThinkingProfile{Style: domain.ThinkingHarmony},
			raw:           "<|channel|>analysis<|message|>thinking<|end|><|channel|>commentary<|message|>planning a tool<|end|><|channel|>final<|message|>done<|end|>",
			wantVisible:   "done",
			wantReasoning: "thinking\n\nplanning a tool",
		},
		{
			name:          "harmony mid-channel guard on an open analysis tail",
			thinking:      domain.ThinkingProfile{Style: domain.ThinkingHarmony},
			raw:           "<|channel|>analysis<|message|>still working",
			wantVisible:   "",
			wantReasoning: "still working",
			wantMid:       true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, stripper, err := ParserFor(domain.ModelProfile{Thinking: tc.thinking})
			if err != nil {
				t.Fatalf("ParserFor: %v", err)
			}
			visible, reasoning := stripper.Strip(tc.raw)
			if visible != tc.wantVisible {
				t.Errorf("visible = %q, want %q", visible, tc.wantVisible)
			}
			if reasoning != tc.wantReasoning {
				t.Errorf("reasoning = %q, want %q", reasoning, tc.wantReasoning)
			}
			if got := stripper.IsMidChannel(tc.raw); got != tc.wantMid {
				t.Errorf("IsMidChannel = %v, want %v", got, tc.wantMid)
			}
		})
	}
}

// TestParserFor_NoneStripperIsByteIdentical pins the anchor: the no-op stripper never alters
// content and never reports mid-channel, so the native content path is unchanged.
func TestParserFor_NoneStripperIsByteIdentical(t *testing.T) {
	t.Parallel()

	_, stripper, err := ParserFor(domain.ModelProfile{})
	if err != nil {
		t.Fatalf("ParserFor: %v", err)
	}
	raw := "any content <|channel|>analysis<|message|>x with markup"
	visible, reasoning := stripper.Strip(raw)
	if visible != raw || reasoning != "" {
		t.Errorf("Strip = (%q, %q), want (%q, \"\")", visible, reasoning, raw)
	}
	if stripper.IsMidChannel(raw) {
		t.Error("IsMidChannel = true, want false for the no-op stripper")
	}
}
