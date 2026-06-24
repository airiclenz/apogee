package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// writeTempFile creates name under root with the given content and returns its path.
func writeTempFile(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("setup: write %s: %v", name, err)
	}
	return path
}

func TestCountOccurrences(t *testing.T) {
	t.Parallel()

	cases := []struct {
		haystack, needle string
		want             int
	}{
		{"aaa bbb aaa", "aaa", 2},
		{"line one\nline two\nline three\n", "line two", 1},
		{"abcabc", "abc", 2},
		{"aaaa", "aa", 2}, // non-overlapping
		{"nothing here", "xyz", 0},
		{"anything", "", 0}, // empty needle never matches
	}
	for _, tc := range cases {
		if got := countOccurrences(tc.haystack, tc.needle); got != tc.want {
			t.Errorf("countOccurrences(%q, %q) = %d, want %d", tc.haystack, tc.needle, got, tc.want)
		}
	}
}

func TestSingleFindReplace_Execute(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := writeTempFile(t, root, "test.txt", "line one\nline two\nline three\n")

	result, err := NewSingleFindReplace(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "test.txt", "oldText": "line two", "newText": "line TWO"}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}
	if !strings.Contains(result.Content, "replaced text") {
		t.Errorf("output %q does not confirm the replacement", result.Content)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "line one\nline TWO\nline three\n" {
		t.Errorf("file content = %q, want the single span replaced", string(got))
	}
}

func TestSingleFindReplace_ToolErrors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTempFile(t, root, "dup.txt", "aaa bbb aaa")
	writeTempFile(t, root, "ok.txt", "hello world")
	tool := NewSingleFindReplace(root)

	cases := []struct {
		name        string
		args        map[string]any
		wantContain string
		// unchanged names the file whose content must be byte-identical after the call.
		unchanged string
		want      string
	}{
		{"old text not found", map[string]any{"path": "ok.txt", "oldText": "absent", "newText": "x"}, "not found", "ok.txt", "hello world"},
		{"old text found twice", map[string]any{"path": "dup.txt", "oldText": "aaa", "newText": "ccc"}, "found 2 times", "dup.txt", "aaa bbb aaa"},
		{"missing path", map[string]any{"oldText": "a", "newText": "b"}, "path is required", "", ""},
		{"missing oldText", map[string]any{"path": "ok.txt", "newText": "b"}, "oldText is required", "ok.txt", "hello world"},
		{"file not found", map[string]any{"path": "nope.txt", "oldText": "a", "newText": "b"}, "file not found", "", ""},
		{"path traversal", map[string]any{"path": "../../../etc/passwd", "oldText": "root", "newText": "x"}, "outside the workspace", "", ""},
		{"oversized newText", map[string]any{"path": "ok.txt", "oldText": "hello", "newText": strings.Repeat("x", maxFileContentBytes+1)}, "exceeds maximum size", "ok.txt", "hello world"},
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
			if tc.unchanged != "" {
				got, _ := os.ReadFile(filepath.Join(root, tc.unchanged))
				if string(got) != tc.want {
					t.Errorf("file %s changed on a failing call: %q", tc.unchanged, string(got))
				}
			}
		})
	}
}

func TestMultiFindReplace_AppliesSequentially(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := writeTempFile(t, root, "test.txt", "line one\nline two\nline three\n")

	result, err := NewMultiFindReplace(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{
			"path": "test.txt",
			"replacements": []map[string]any{
				{"oldText": "line one", "newText": "first line"},
				{"oldText": "line two", "newText": "second line"},
				{"oldText": "line three", "newText": "third line"},
			},
		}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}
	if !strings.Contains(result.Content, "3 replacements") {
		t.Errorf("output %q does not report 3 replacements", result.Content)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "first line\nsecond line\nthird line\n" {
		t.Errorf("file content = %q, want all three replaced", string(got))
	}
}

func TestMultiFindReplace_SequentialDependentEdit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := writeTempFile(t, root, "test.txt", "hello world")

	// Edit #1 introduces "MARKER world" that edit #2 then targets (oracle vector).
	_, err := NewMultiFindReplace(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{
			"path": "test.txt",
			"replacements": []map[string]any{
				{"oldText": "hello", "newText": "MARKER"},
				{"oldText": "MARKER world", "newText": "goodbye universe"},
			},
		}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "goodbye universe" {
		t.Errorf("file content = %q, want %q", string(got), "goodbye universe")
	}
}

func TestMultiFindReplace_FailsAtomically(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	original := "line one\nline two\nline three\n"
	path := writeTempFile(t, root, "test.txt", original)

	// Replacement #2 cannot be found; the whole call must fail and leave the file intact.
	result, err := NewMultiFindReplace(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{
			"path": "test.txt",
			"replacements": []map[string]any{
				{"oldText": "line one", "newText": "FIRST"},
				{"oldText": "DOES NOT EXIST", "newText": "x"},
			},
		}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("IsError = false, want true")
	}
	if !strings.Contains(result.Content, "replacement #2") || !strings.Contains(result.Content, "not found") {
		t.Errorf("error %q does not name replacement #2 as not found", result.Content)
	}

	got, _ := os.ReadFile(path)
	if string(got) != original {
		t.Errorf("file changed on an atomic failure: %q", string(got))
	}
}

func TestMultiFindReplace_DuplicateCreatedBySequentialEdit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	original := "alpha beta"
	path := writeTempFile(t, root, "test.txt", original)

	// Edit #1 turns "alpha" into "beta", so "beta" then appears twice for edit #2 (oracle vector).
	result, err := NewMultiFindReplace(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{
			"path": "test.txt",
			"replacements": []map[string]any{
				{"oldText": "alpha", "newText": "beta"},
				{"oldText": "beta", "newText": "gamma"},
			},
		}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("IsError = false, want true")
	}
	if !strings.Contains(result.Content, "replacement #2") || !strings.Contains(result.Content, "found 2 times") {
		t.Errorf("error %q does not report replacement #2 found twice", result.Content)
	}

	got, _ := os.ReadFile(path)
	if string(got) != original {
		t.Errorf("file changed despite the failure: %q", string(got))
	}
}

func TestMultiFindReplace_DeletionWithEmptyNewText(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := writeTempFile(t, root, "test.txt", "line one\nline two\nline three\n")

	result, err := NewMultiFindReplace(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{
			"path":         "test.txt",
			"replacements": []map[string]any{{"oldText": "line two\n", "newText": ""}},
		}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "line one\nline three\n" {
		t.Errorf("file content = %q, want the deleted line gone", string(got))
	}
}

func TestMultiFindReplace_ValidationErrors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTempFile(t, root, "f.txt", "x")
	tool := NewMultiFindReplace(root)

	cases := []struct {
		name        string
		args        map[string]any
		wantContain string
	}{
		{"missing path", map[string]any{"replacements": []map[string]any{{"oldText": "a", "newText": "b"}}}, "path is required"},
		{"empty replacements", map[string]any{"path": "f.txt", "replacements": []map[string]any{}}, "non-empty array"},
		{"empty oldText", map[string]any{"path": "f.txt", "replacements": []map[string]any{{"oldText": "", "newText": "b"}}}, "replacements[0].oldText"},
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

// TestFindReplace_CarryTheMarker proves both find-replace writers carry the
// workspaceScopedWriter marker and that view_diff/open_file do not.
func TestFindReplace_CarryTheMarker(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writers := []domain.Tool{NewSingleFindReplace(root), NewMultiFindReplace(root)}
	for _, w := range writers {
		if !IsWorkspaceScopedWriter(w) {
			t.Errorf("%s does not carry the workspaceScopedWriter marker", w.Name())
		}
	}
	readers := []domain.Tool{NewViewDiff(root), NewOpenFile(root)}
	for _, r := range readers {
		if IsWorkspaceScopedWriter(r) {
			t.Errorf("%s wrongly carries the writer marker (it is read-only)", r.Name())
		}
	}
}
