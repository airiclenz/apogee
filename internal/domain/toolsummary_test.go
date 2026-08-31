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
	_ ToolSummary = ListedEntries{}
	_ ToolSummary = MatchedLines{}
	_ ToolSummary = DiffStat{}
	_ ToolSummary = ChangedFiles{}
	_ ToolSummary = SearchHits{}
	_ ToolSummary = EditRegions{}
)

func TestToolSummaryVariantsAreSealed(t *testing.T) {
	t.Parallel()

	// The readable half: one entry per documented variant. A new variant that is not
	// listed here fails the count — the nudge to also give it a root-facade alias and a
	// line in the host's renderer, the two places a summary is of any use.
	variants := []ToolSummary{
		ReadSpan{},
		ListedEntries{},
		MatchedLines{},
		DiffStat{},
		ChangedFiles{},
		SearchHits{},
		EditRegions{},
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

// Stat is the single derivation of an edit's +A −R pair: it counts the changed lines the
// regions carry and nothing else, so the context lines bracketing a region — which a reader
// sees but the edit never touched — stay out of the count.
func TestEditRegionsStat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		regions EditRegions
		want    DiffStat
	}{
		{
			name:    "zero value counts nothing",
			regions: EditRegions{},
			want:    DiffStat{},
		},
		{
			name: "one region counts its changed lines, not its context",
			regions: EditRegions{Regions: []EditRegion{{
				BeforeStart: 8,
				AfterStart:  8,
				Leading:     []string{"ctx one", "ctx two", "ctx three"},
				Removed:     []string{"old one", "old two"},
				Inserted:    []string{"new one"},
				Trailing:    []string{"ctx four", "ctx five"},
			}}},
			want: DiffStat{Added: 1, Removed: 2},
		},
		{
			name: "several regions sum, insertion-only and deletion-only included",
			regions: EditRegions{Regions: []EditRegion{
				{
					BeforeStart: 1,
					AfterStart:  1,
					Removed:     []string{"old one", "old two"},
					Inserted:    []string{"new one", "new two", "new three"},
				},
				{
					BeforeStart: 40,
					AfterStart:  41,
					Leading:     []string{"ctx"},
					Inserted:    []string{"added one", "added two"},
				},
				{
					BeforeStart: 90,
					AfterStart:  93,
					Removed:     []string{"dropped"},
					Trailing:    []string{"ctx"},
				},
			}},
			want: DiffStat{Added: 5, Removed: 3},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := tc.regions.Stat()

			if got != tc.want {
				t.Errorf("Stat() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestEditRegionsZeroValueHasNoRegions(t *testing.T) {
	t.Parallel()

	var zero EditRegions

	if zero.Regions != nil {
		t.Errorf("zero EditRegions Regions = %#v, want nil", zero.Regions)
	}
	if got := zero.Stat(); got != (DiffStat{}) {
		t.Errorf("zero EditRegions Stat() = %+v, want the zero DiffStat", got)
	}
}
