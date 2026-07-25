package domain

import (
	"fmt"
	"testing"
)

// The compile-time half of the sum's membership: every variant satisfies ToolSummary.
// The marker method isToolSummary is unexported, so this list can only ever grow from
// INSIDE this package — no external package can add a variant, exactly as with Event.
var (
	_ ToolSummary = ReadSpan{}
	_ ToolSummary = WroteBytes{}
	_ ToolSummary = ListedEntries{}
	_ ToolSummary = MatchedLines{}
	_ ToolSummary = DiffStat{}
	_ ToolSummary = SearchHits{}
	_ ToolSummary = OpenedFile{}
)

func TestToolSummaryVariantsAreSealed(t *testing.T) {
	t.Parallel()

	// The readable half: one entry per documented variant. A new variant that is not
	// listed here fails the count — the nudge to also give it a root-facade alias and a
	// line in the host's renderer, the two places a summary is of any use.
	variants := []ToolSummary{
		ReadSpan{},
		WroteBytes{},
		ListedEntries{},
		MatchedLines{},
		DiffStat{},
		SearchHits{},
		OpenedFile{},
	}

	const want = 7
	if len(variants) != want {
		t.Fatalf("ToolSummary variants = %d, want %d", len(variants), want)
	}

	seen := make(map[string]bool, len(variants))
	for _, v := range variants {
		if v == nil {
			t.Fatalf("variant list contains a nil ToolSummary")
		}
		name := fmt.Sprintf("%T", v)
		if seen[name] {
			t.Errorf("variant %s listed twice", name)
		}
		seen[name] = true
	}
}

func TestToolResultZeroValueHasNoSummary(t *testing.T) {
	t.Parallel()

	var zero ToolResult
	if zero.Summary != nil {
		t.Errorf("zero ToolResult Summary = %#v, want nil", zero.Summary)
	}

	// Summary is purely additive: a result built the way every tool builds one today —
	// the three pre-existing fields — is unchanged in each of them and carries no
	// summary, which is what makes a summary-less result render through the prose path.
	result := ToolResult{CallID: "call-1", Content: "wrote 12 bytes to notes.txt", IsError: false}

	if result.CallID != "call-1" {
		t.Errorf("CallID = %q, want %q", result.CallID, "call-1")
	}
	if result.Content != "wrote 12 bytes to notes.txt" {
		t.Errorf("Content = %q, want %q", result.Content, "wrote 12 bytes to notes.txt")
	}
	if result.IsError {
		t.Errorf("IsError = true, want false")
	}
	if result.Summary != nil {
		t.Errorf("Summary = %#v, want nil", result.Summary)
	}
}
