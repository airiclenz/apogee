package tools

import (
	"context"
	"strings"
	"testing"
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
	if got := unifiedLineDiff("a\nb", "a\nb\nc"); !strings.Contains(got, "+ c") {
		t.Errorf("addition diff = %q, want a + c line", got)
	}
	// Pure deletion.
	if got := unifiedLineDiff("a\nb\nc", "a\nc"); !strings.Contains(got, "- b") {
		t.Errorf("deletion diff = %q, want a - b line", got)
	}
	// Identical → empty.
	if got := unifiedLineDiff("same", "same"); got != "" {
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
