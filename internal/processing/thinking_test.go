package processing

import "testing"

// gemmaConfig and gptOSSConfig mirror the two thinking-token shapes the apogee-code
// oracle's vectors exercise (test/unit/thinking-stripper.test.ts).
var (
	gemmaConfig  = &ThinkingConfig{StartToken: "<think>", EndToken: "</think>"}
	gptOSSConfig = &ThinkingConfig{
		StartToken: "<|channel|>analysis<|message|>",
		EndToken:   "<|end|>",
	}
)

func TestStripThinking_PortedOracleVectors_MatchTypeScript(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		cfg          *ThinkingConfig
		raw          string
		wantVisible  string
		wantReason   string
		wantReasoned bool
	}{
		{
			name:        "no config returns raw content untouched",
			cfg:         nil,
			raw:         "Hello world",
			wantVisible: "Hello world",
		},
		{
			name:         "single block is stripped to visible answer",
			cfg:          gemmaConfig,
			raw:          "<think>I need to consider this carefully</think>Here is my answer.",
			wantVisible:  "Here is my answer.",
			wantReason:   "I need to consider this carefully",
			wantReasoned: true,
		},
		{
			name:         "multiple blocks join with a blank line",
			cfg:          gemmaConfig,
			raw:          "<think>First thought</think>Part 1 <think>Second thought</think>Part 2",
			wantVisible:  "Part 1 Part 2",
			wantReason:   "First thought\n\nSecond thought",
			wantReasoned: true,
		},
		{
			name:         "unclosed block (streaming) yields empty visible tail",
			cfg:          gemmaConfig,
			raw:          "<think>Still thinking about this...",
			wantVisible:  "",
			wantReason:   "Still thinking about this...",
			wantReasoned: true,
		},
		{
			name:         "gpt-oss harmony channel is stripped",
			cfg:          gptOSSConfig,
			raw:          "<|channel|>analysis<|message|>Let me analyze this<|end|>The answer is 42.",
			wantVisible:  "The answer is 42.",
			wantReason:   "Let me analyze this",
			wantReasoned: true,
		},
		{
			name:        "content with no thinking tokens is unchanged",
			cfg:         gemmaConfig,
			raw:         "Just a plain response with no thinking.",
			wantVisible: "Just a plain response with no thinking.",
		},
		{
			name:         "empty block is a present-but-empty reasoning span",
			cfg:          gemmaConfig,
			raw:          "<think></think>Answer here.",
			wantVisible:  "Answer here.",
			wantReason:   "",
			wantReasoned: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := StripThinking(tc.raw, tc.cfg)

			if got.Visible != tc.wantVisible {
				t.Errorf("Visible = %q, want %q", got.Visible, tc.wantVisible)
			}
			if got.Reasoning != tc.wantReason {
				t.Errorf("Reasoning = %q, want %q", got.Reasoning, tc.wantReason)
			}
			if got.HasReasoning != tc.wantReasoned {
				t.Errorf("HasReasoning = %v, want %v", got.HasReasoning, tc.wantReasoned)
			}
		})
	}
}

func TestStripThinking_EmptyTokenConfig_PassesThrough(t *testing.T) {
	t.Parallel()

	got := StripThinking("<think>x</think>y", &ThinkingConfig{StartToken: "<think>"})

	if got.Visible != "<think>x</think>y" {
		t.Errorf("Visible = %q, want pass-through with an empty EndToken", got.Visible)
	}
	if got.HasReasoning {
		t.Error("HasReasoning = true, want false when a token is empty")
	}
}

func TestIsThinking_PortedOracleVectors_MatchTypeScript(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		cfg  *ThinkingConfig
		raw  string
		want bool
	}{
		{"unclosed block is in-progress", gemmaConfig, "<think>in progress", true},
		{"closed block is not in-progress", gemmaConfig, "<think>done</think>Answer", false},
		{"no config is never thinking", nil, "<think>test", false},
		{"no opener is not thinking", gemmaConfig, "plain text", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := IsThinking(tc.raw, tc.cfg); got != tc.want {
				t.Errorf("IsThinking(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}
