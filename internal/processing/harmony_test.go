package processing

import "testing"

func TestStripHarmony_FullChannelSet(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		raw            string
		wantVisible    string
		wantReasoning  string
		wantCommentary string
		wantReasoned   bool
	}{
		{
			name:          "analysis then final",
			raw:           "<|channel|>analysis<|message|>Let me analyze this<|end|><|channel|>final<|message|>The answer is 42.<|end|>",
			wantVisible:   "The answer is 42.",
			wantReasoning: "Let me analyze this",
			wantReasoned:  true,
		},
		{
			name:           "analysis, commentary, final all routed",
			raw:            "<|channel|>analysis<|message|>thinking<|end|><|channel|>commentary<|message|>calling a tool<|end|><|channel|>final<|message|>done<|end|>",
			wantVisible:    "done",
			wantReasoning:  "thinking",
			wantCommentary: "calling a tool",
			wantReasoned:   true,
		},
		{
			name:          "call terminator closes a message",
			raw:           "<|channel|>analysis<|message|>I will call a tool<|call|><|channel|>final<|message|>result<|end|>",
			wantVisible:   "result",
			wantReasoning: "I will call a tool",
			wantReasoned:  true,
		},
		{
			name:          "return terminator closes the final message",
			raw:           "<|channel|>analysis<|message|>reasoning<|end|><|channel|>final<|message|>the answer<|return|>",
			wantVisible:   "the answer",
			wantReasoning: "reasoning",
			wantReasoned:  true,
		},
		{
			name:          "start role prefix is consumed",
			raw:           "<|start|>assistant<|channel|>final<|message|>hello<|end|>",
			wantVisible:   "hello",
			wantReasoning: "",
			wantReasoned:  false,
		},
		{
			name:          "unterminated analysis tail (streaming) does not leak into visible",
			raw:           "<|channel|>analysis<|message|>still thinking about",
			wantVisible:   "",
			wantReasoning: "still thinking about",
			wantReasoned:  true,
		},
		{
			name:          "plain non-harmony content passes through as visible",
			raw:           "just a normal answer",
			wantVisible:   "just a normal answer",
			wantReasoning: "",
			wantReasoned:  false,
		},
		{
			name:          "two analysis blocks join with a blank line",
			raw:           "<|channel|>analysis<|message|>first<|end|><|channel|>analysis<|message|>second<|end|><|channel|>final<|message|>answer<|end|>",
			wantVisible:   "answer",
			wantReasoning: "first\n\nsecond",
			wantReasoned:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := StripHarmony(tc.raw)
			if got.Visible != tc.wantVisible {
				t.Errorf("Visible = %q, want %q", got.Visible, tc.wantVisible)
			}
			if got.Reasoning != tc.wantReasoning {
				t.Errorf("Reasoning = %q, want %q", got.Reasoning, tc.wantReasoning)
			}
			if got.Commentary != tc.wantCommentary {
				t.Errorf("Commentary = %q, want %q", got.Commentary, tc.wantCommentary)
			}
			if got.HasReasoning != tc.wantReasoned {
				t.Errorf("HasReasoning = %v, want %v", got.HasReasoning, tc.wantReasoned)
			}
		})
	}
}

// TestStripHarmony_AgreesWithSinglePairAnalysis pins that the full-channel processor strips the
// analysis channel to the same visible content the existing single-pair StripThinking does for
// the gpt-oss vector — the two paths must not diverge on the channel they both handle.
func TestStripHarmony_AgreesWithSinglePairAnalysis(t *testing.T) {
	t.Parallel()

	raw := "<|channel|>analysis<|message|>Let me analyze this<|end|>The answer is 42."
	single := StripThinking(raw, gptOSSConfig)
	full := StripHarmony(raw)

	if single.Visible != full.Visible {
		t.Errorf("visible diverged: single=%q full=%q", single.Visible, full.Visible)
	}
	if single.Reasoning != full.Reasoning {
		t.Errorf("reasoning diverged: single=%q full=%q", single.Reasoning, full.Reasoning)
	}
}

func TestIsHarmonyThinking(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"open analysis is thinking", "<|channel|>analysis<|message|>working", true},
		{"open commentary is thinking", "<|channel|>commentary<|message|>planning a tool", true},
		{"open final is not thinking", "<|channel|>final<|message|>here is the answer", false},
		{"closed analysis is not thinking", "<|channel|>analysis<|message|>done<|end|>", false},
		{"no channel is not thinking", "plain text", false},
		{"closed analysis then open final is not thinking", "<|channel|>analysis<|message|>x<|end|><|channel|>final<|message|>y", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsHarmonyThinking(tc.raw); got != tc.want {
				t.Errorf("IsHarmonyThinking(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}
