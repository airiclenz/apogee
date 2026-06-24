package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenFile_ReadsContent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTempFile(t, root, "f.txt", "alpha\nbeta\ngamma\n")

	result, err := NewOpenFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "f.txt"}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}
	if !strings.Contains(result.Content, "File: f.txt") {
		t.Errorf("output %q missing the file header", result.Content)
	}
	if !strings.Contains(result.Content, "alpha\nbeta\ngamma") {
		t.Errorf("output %q missing the file content", result.Content)
	}
}

func TestOpenFile_LocatesSubstring(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTempFile(t, root, "f.txt", "first\nTODO here\nthird\nTODO again\n")

	result, err := NewOpenFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "f.txt", "locate": "TODO"}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}
	// "TODO" is on lines 2 and 4 (1-based).
	if !strings.Contains(result.Content, "lines: 2, 4") {
		t.Errorf("output %q does not report the located lines 2, 4", result.Content)
	}
}

func TestOpenFile_LocateNoMatch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTempFile(t, root, "f.txt", "nothing to find here\n")

	result, err := NewOpenFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "f.txt", "locate": "absent"}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}
	if !strings.Contains(result.Content, "on no lines") {
		t.Errorf("output %q should report no matching lines", result.Content)
	}
}

func TestOpenFile_ToolErrors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "adir"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	tool := NewOpenFile(root)

	cases := []struct {
		name        string
		args        map[string]any
		wantContain string
	}{
		{"missing path", map[string]any{}, "path is required"},
		{"file not found", map[string]any{"path": "nope.txt"}, "file not found"},
		{"is a directory", map[string]any{"path": "adir"}, "not a file"},
		{"path escape", map[string]any{"path": "../../../etc/passwd"}, "outside the workspace"},
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
