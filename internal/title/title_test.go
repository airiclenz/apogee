package title

import (
	"strings"
	"testing"
	"time"
)

// promptDate is the fixed date every Prompt test builds against, so the rendered context line is
// deterministic.
var promptDate = time.Date(2026, 7, 31, 14, 30, 0, 0, time.UTC)

// userContent returns the naming request's user message, failing the test if the request is not
// the expected system+user pair.
func userContent(t *testing.T, firstPrompt, workspace string) string {
	t.Helper()
	req := Prompt(firstPrompt, workspace, promptDate)
	if len(req.Messages) != 2 {
		t.Fatalf("Prompt built %d messages, want 2 (system + user)", len(req.Messages))
	}
	if req.Messages[1].Role != "user" {
		t.Fatalf("second message role = %q, want user", req.Messages[1].Role)
	}
	return req.Messages[1].Content
}

func TestPromptCarriesWorkspaceDateAndInstruction(t *testing.T) {
	t.Parallel()

	req := Prompt("add a retry to the uploader", "apogee", promptDate)

	if req.Messages[0].Role != "system" {
		t.Fatalf("first message role = %q, want system", req.Messages[0].Role)
	}
	if !strings.Contains(req.Messages[0].Content, "3 to 8 words") {
		t.Errorf("system prompt does not state the length instruction: %q", req.Messages[0].Content)
	}

	user := req.Messages[1].Content
	for _, want := range []string{"apogee", "2026-07-31", "add a retry to the uploader", userInstruction} {
		if !strings.Contains(user, want) {
			t.Errorf("user message missing %q:\n%s", want, user)
		}
	}
}

func TestPromptLeavesModelToTheClient(t *testing.T) {
	t.Parallel()

	req := Prompt("hello", "apogee", promptDate)
	if req.Model != "" {
		t.Errorf("Model = %q, want empty so the Client's current binding wins", req.Model)
	}
	if req.Stream {
		t.Error("Stream = true, want a single non-streaming round-trip")
	}
	if len(req.Tools) != 0 {
		t.Errorf("Tools = %d, want none on a naming call", len(req.Tools))
	}
}

func TestPromptSetsSamplingConstants(t *testing.T) {
	t.Parallel()

	req := Prompt("hello", "apogee", promptDate)
	if req.Sampling.Temperature == nil || *req.Sampling.Temperature != titleTemperature {
		t.Errorf("Temperature = %v, want %v", req.Sampling.Temperature, titleTemperature)
	}
	if req.Sampling.MaxTokens == nil {
		t.Fatal("MaxTokens is unset, want a generous cap for a model that thinks inline")
	}
	if *req.Sampling.MaxTokens < 512 {
		t.Errorf("MaxTokens = %d, want at least 512", *req.Sampling.MaxTokens)
	}
}

func TestPromptCapsTheExcerpt(t *testing.T) {
	t.Parallel()

	// "z" appears nowhere in the message template, so counting it counts excerpt body only.
	long := strings.Repeat("z", promptExcerptRunes*3) + " SENTINEL_TAIL"
	user := userContent(t, long, "apogee")

	if strings.Contains(user, "SENTINEL_TAIL") {
		t.Error("user message carries the tail of an over-long prompt; the excerpt cap did not apply")
	}
	if got := strings.Count(user, "z"); got > promptExcerptRunes {
		t.Errorf("excerpt carried %d body runes, want at most %d", got, promptExcerptRunes)
	}
	if !strings.Contains(user, "…") {
		t.Error("a truncated excerpt should be marked with an ellipsis")
	}
}

func TestPromptKeepsAShortPromptWhole(t *testing.T) {
	t.Parallel()

	user := userContent(t, "  fix the parser  ", "apogee")
	if !strings.Contains(user, "fix the parser") {
		t.Errorf("short prompt not carried verbatim:\n%s", user)
	}
	if strings.Contains(user, "…") {
		t.Error("a prompt under the cap must not be marked as truncated")
	}
}

func TestSanitize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
		ok   bool
	}{
		{
			name: "plain title passes through",
			raw:  "Add retry to the uploader",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "leading think block stripped",
			raw:  "<think>The user wants a retry.\nSo the title is about retries.</think>\nAdd retry to the uploader",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "think block on one line stripped",
			raw:  "<think>hmm</think> Add retry to the uploader",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "unterminated think block leaves nothing",
			raw:  "<think>still reasoning about the answer",
			ok:   false,
		},
		{
			name: "multiline reply takes the first non-empty line",
			raw:  "\n\nAdd retry to the uploader\nHere is why I chose that.",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "bare fence markers skipped as noise",
			raw:  "```\nAdd retry to the uploader\n```",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "language-tagged fence marker skipped as noise",
			raw:  "```text\nAdd retry to the uploader\n```",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "tilde fence marker skipped as noise",
			raw:  "~~~\nAdd retry to the uploader\n~~~",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "fence pair with nothing inside fails",
			raw:  "```\n```",
			ok:   false,
		},
		{
			name: "fence glued to prose keeps the prose",
			raw:  "```Add retry to the uploader",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "ansi colour sequences stripped",
			raw:  "\x1b[1;32mAdd retry to the uploader\x1b[0m",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "osc sequence stripped",
			raw:  "\x1b]0;pwned\x07Add retry to the uploader",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "bare control characters stripped",
			raw:  "Add\x07 retry\x00 to the uploader",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "surrounding double quotes stripped",
			raw:  `"Add retry to the uploader"`,
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "surrounding backticks stripped",
			raw:  "`Add retry to the uploader`",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "curly quotes stripped",
			raw:  "“Add retry to the uploader”",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "unbalanced apostrophe kept",
			raw:  "Fix the user's uploader",
			want: "Fix the user's uploader",
			ok:   true,
		},
		{
			name: "title label stripped",
			raw:  "Title: Add retry to the uploader",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "title label stripped case-insensitively",
			raw:  "TITLE:Add retry to the uploader",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "quoted label stripped in either order",
			raw:  `"Title: Add retry to the uploader"`,
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "label then quotes stripped",
			raw:  `Title: "Add retry to the uploader"`,
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "comment marker and label stripped",
			raw:  "// title: Add retry to the uploader",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "heading marker stripped",
			raw:  "# Add retry to the uploader",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "multi-hash heading marker stripped",
			raw:  "### Add retry to the uploader",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "trailing period trimmed",
			raw:  "Add retry to the uploader.",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "inner whitespace collapsed",
			raw:  "Add   retry\tto  the uploader",
			want: "Add retry to the uploader",
			ok:   true,
		},
		{
			name: "empty input fails",
			raw:  "",
			ok:   false,
		},
		{
			name: "whitespace-only input fails",
			raw:  "   \n\t \n ",
			ok:   false,
		},
		{
			name: "all-noise input fails",
			raw:  "<think>reasoning</think>\n```\n\"Title:\"\n```",
			ok:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := Sanitize(tc.raw)
			if ok != tc.ok {
				t.Fatalf("Sanitize(%q) ok = %v, want %v (got %q)", tc.raw, ok, tc.ok, got)
			}
			if ok && got != tc.want {
				t.Errorf("Sanitize(%q) = %q, want %q", tc.raw, got, tc.want)
			}
			if !ok && got != "" {
				t.Errorf("Sanitize(%q) returned %q alongside ok=false, want an empty title", tc.raw, got)
			}
		})
	}
}

func TestSanitizeTruncatesOnAWordBoundary(t *testing.T) {
	t.Parallel()

	// 11 words of 5 runes plus separators runs well past the 50-rune cap.
	raw := strings.TrimSpace(strings.Repeat("alpha ", 11))
	got, ok := Sanitize(raw)
	if !ok {
		t.Fatal("Sanitize reported failure on an over-long but valid title")
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated title %q does not end in an ellipsis", got)
	}
	if n := len([]rune(got)); n > titleMaxRunes+1 {
		t.Errorf("title is %d runes (%q), want at most %d plus the ellipsis", n, got, titleMaxRunes)
	}
	if strings.HasSuffix(strings.TrimSuffix(got, "…"), " ") {
		t.Errorf("truncated title %q keeps the boundary space", got)
	}
	if !strings.HasPrefix(raw, strings.TrimSuffix(got, "…")) {
		t.Errorf("truncated title %q is not a prefix of the input", got)
	}
}

func TestSanitizeHardCutsWhenThereIsNoLateWordBoundary(t *testing.T) {
	t.Parallel()

	// One long word: no boundary past the 60% floor, so the cut is hard at the rune cap.
	got, ok := Sanitize(strings.Repeat("x", 120))
	if !ok {
		t.Fatal("Sanitize reported failure on a single long word")
	}
	if want := strings.Repeat("x", titleMaxRunes) + "…"; got != want {
		t.Errorf("Sanitize hard cut = %q, want %q", got, want)
	}
}

func TestSanitizeCountsMultibyteRunesNotBytes(t *testing.T) {
	t.Parallel()

	// 60 CJK runes: 180 bytes, but only 60 runes, so the cap must bite at 50 runes.
	raw := strings.Repeat("日", 60)
	got, ok := Sanitize(raw)
	if !ok {
		t.Fatal("Sanitize reported failure on a CJK title")
	}
	if want := strings.Repeat("日", titleMaxRunes) + "…"; got != want {
		t.Errorf("Sanitize(CJK) = %q (%d runes), want %d runes plus the ellipsis",
			got, len([]rune(got)), titleMaxRunes)
	}
}

func TestSanitizeKeepsAMultibyteTitleUnderTheCapWhole(t *testing.T) {
	t.Parallel()

	raw := "修复上传器的重试逻辑"
	got, ok := Sanitize(raw)
	if !ok || got != raw {
		t.Errorf("Sanitize(%q) = %q, %v; want the title unchanged", raw, got, ok)
	}
}
