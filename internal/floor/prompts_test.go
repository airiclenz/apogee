package floor

import (
	"strings"
	"testing"
)

// Every asset the guards ship loads, carries text, and has had its single trailing newline
// stripped — the contract mustPrompt states and the one a re-worded asset could break silently
// (a trailing newline baked into a directive runs it into whatever the guard appends next).
func TestEveryPromptAssetLoadsWithoutItsTrailingNewline(t *testing.T) {
	t.Parallel()

	entries, err := promptFS.ReadDir("prompts")
	if err != nil {
		t.Fatalf("read the embedded prompts directory: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no prompt assets are embedded")
	}

	for _, entry := range entries {
		t.Run(entry.Name(), func(t *testing.T) {
			t.Parallel()

			text := mustPrompt(entry.Name())

			if strings.TrimSpace(text) == "" {
				t.Fatal("the asset loads as blank text")
			}
			if strings.HasSuffix(text, "\n") {
				t.Error("the loaded text still ends in a newline")
			}
			if strings.Contains(text, "\r") {
				t.Error("the loaded text carries a CR — CRLF endings must normalise away")
			}
		})
	}
}
