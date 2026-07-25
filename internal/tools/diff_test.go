package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

func TestViewDiff_ReportsChanges(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTempFile(t, root, "f.txt", "line one\nline two\nline three\n")

	result, err := NewViewDiff(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "f.txt", "newContent": "line one\nline TWO\nline three\n"}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}

	// The middle line changed: it must show as a removal and an addition, with the
	// unchanged lines as context.
	if !strings.Contains(result.Content, "- line two") {
		t.Errorf("diff missing removal of the old line:\n%s", result.Content)
	}
	if !strings.Contains(result.Content, "+ line TWO") {
		t.Errorf("diff missing addition of the new line:\n%s", result.Content)
	}
	if !strings.Contains(result.Content, "  line one") {
		t.Errorf("diff missing the unchanged context line:\n%s", result.Content)
	}
}

func TestViewDiff_NoChanges(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTempFile(t, root, "f.txt", "same\ncontent\n")

	result, err := NewViewDiff(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "f.txt", "newContent": "same\ncontent\n"}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}
	if !strings.Contains(result.Content, "No changes") {
		t.Errorf("identical content should report no changes, got %q", result.Content)
	}
}

// TestViewDiff_ReportsDiffStat pins the structured half of view_diff's outcome. The stat is
// counted from the diff OPERATIONS, so the test also asserts the equality that makes it
// trustworthy: it matches what counting leading "+"/"-" over the rendered text yields, which
// is what a reader had to do before the summary existed.
func TestViewDiff_ReportsDiffStat(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTempFile(t, root, "f.txt", "one\ntwo\nthree\n")
	tool := NewViewDiff(root)

	cases := []struct {
		name        string
		newContent  string
		wantContent string
		wantSummary domain.DiffStat
	}{
		{
			name:        "line replaced",
			newContent:  "one\nTWO\nthree\n",
			wantContent: "  one\n- two\n+ TWO\n  three\n  ",
			wantSummary: domain.DiffStat{Added: 1, Removed: 1},
		},
		{
			name:        "pure addition",
			newContent:  "one\ntwo\nthree\nfour\n",
			wantContent: "  one\n  two\n  three\n+ four\n  ",
			wantSummary: domain.DiffStat{Added: 1},
		},
		{
			name:        "pure deletion",
			newContent:  "one\nthree\n",
			wantContent: "  one\n- two\n  three\n  ",
			wantSummary: domain.DiffStat{Removed: 1},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := tool.Execute(context.Background(),
				callWith(t, "c1", map[string]any{"path": "f.txt", "newContent": tc.newContent}))

			if err != nil {
				t.Fatalf("Execute returned a Go error: %v", err)
			}
			if result.Content != tc.wantContent {
				t.Errorf("Content = %q, want %q", result.Content, tc.wantContent)
			}
			stat, ok := result.Summary.(domain.DiffStat)
			if !ok {
				t.Fatalf("Summary = %#v, want a domain.DiffStat", result.Summary)
			}
			if stat != tc.wantSummary {
				t.Errorf("Summary = %+v, want %+v", stat, tc.wantSummary)
			}
			if added, removed := countDiffTags(result.Content); added != stat.Added || removed != stat.Removed {
				t.Errorf("stat %+v disagrees with the rendered text (+%d -%d)", stat, added, removed)
			}
		})
	}
}

// countDiffTags counts the added and removed lines of a rendered diff the way a reader of
// the text has to — by their leading "+"/"-".
func countDiffTags(rendered string) (added, removed int) {
	for _, line := range strings.Split(rendered, "\n") {
		switch {
		case strings.HasPrefix(line, "+"):
			added++
		case strings.HasPrefix(line, "-"):
			removed++
		}
	}
	return added, removed
}

// TestViewDiff_NoChangesCarriesNoSummary: identical content is not a diff, so the sentinel
// result carries no summary at all and a host renders its sentence as prose.
func TestViewDiff_NoChangesCarriesNoSummary(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTempFile(t, root, "f.txt", "same\ncontent\n")

	result, err := NewViewDiff(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "f.txt", "newContent": "same\ncontent\n"}))

	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.Content != "No changes detected" {
		t.Fatalf("Content = %q, want the no-changes sentinel", result.Content)
	}
	if result.Summary != nil {
		t.Errorf("Summary = %#v, want nil for the no-changes sentinel", result.Summary)
	}
}

// TestViewDiff_Deterministic proves the diff output is stable across repeated calls — the
// LCS table fully determines the ordering (no map iteration, no time-dependence).
func TestViewDiff_Deterministic(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTempFile(t, root, "f.txt", "a\nb\nc\nd\ne\n")
	tool := NewViewDiff(root)
	call := callWith(t, "c1", map[string]any{"path": "f.txt", "newContent": "a\nX\nc\nd\nY\n"})

	first, err := tool.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	for i := 0; i < 5; i++ {
		again, err := tool.Execute(context.Background(), call)
		if err != nil {
			t.Fatalf("Execute returned a Go error: %v", err)
		}
		if again.Content != first.Content {
			t.Fatalf("diff is not deterministic:\nfirst:\n%s\nlater:\n%s", first.Content, again.Content)
		}
	}
}

func TestUnifiedLineDiff_PureInsertionAndDeletion(t *testing.T) {
	t.Parallel()

	// Pure addition.
	if got, _ := unifiedLineDiff("a\nb", "a\nb\nc"); !strings.Contains(got, "+ c") {
		t.Errorf("addition diff = %q, want a + c line", got)
	}
	// Pure deletion.
	if got, _ := unifiedLineDiff("a\nb\nc", "a\nc"); !strings.Contains(got, "- b") {
		t.Errorf("deletion diff = %q, want a - b line", got)
	}
	// Identical → empty.
	if got, _ := unifiedLineDiff("same", "same"); got != "" {
		t.Errorf("identical diff = %q, want empty", got)
	}
}

func TestViewDiff_ToolErrors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	tool := NewViewDiff(root)

	cases := []struct {
		name        string
		args        map[string]any
		wantContain string
	}{
		{"missing path", map[string]any{"newContent": "x"}, "path is required"},
		{"file not found", map[string]any{"path": "nope.txt", "newContent": "x"}, "file not found"},
		{"path escape", map[string]any{"path": "../escape.txt", "newContent": "x"}, "outside the workspace"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := tool.Execute(context.Background(), callWith(t, "c1", tc.args))
			if err != nil {
				t.Fatalf("Execute returned a Go error: %v", err)
			}
			if !result.IsError {
				t.Fatalf("IsError = false, want true (content: %q)", result.Content)
			}
			if !strings.Contains(result.Content, tc.wantContain) {
				t.Errorf("content %q does not contain %q", result.Content, tc.wantContain)
			}
		})
	}
}
