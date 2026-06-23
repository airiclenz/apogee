package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFile_Execute_CreatesFileAndParents(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	result, err := NewWriteFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "nested/dir/out.txt", "content": "hello"}))

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}

	got, readErr := os.ReadFile(filepath.Join(root, "nested", "dir", "out.txt"))
	if readErr != nil {
		t.Fatalf("file was not created: %v", readErr)
	}
	if string(got) != "hello" {
		t.Errorf("file content = %q, want %q", string(got), "hello")
	}
}

func TestWriteFile_Execute_OverwritesExisting(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "f.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, err := NewWriteFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "f.txt", "content": "new"}))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Errorf("content = %q, want %q", string(got), "new")
	}
}

func TestWriteFile_Execute_ToolErrors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	tool := NewWriteFile(root)

	cases := []struct {
		name        string
		args        map[string]any
		wantContain string
	}{
		{"path escape", map[string]any{"path": "../evil.txt", "content": "x"}, "outside the workspace"},
		{"missing path", map[string]any{"content": "x"}, "path is required"},
		{"oversized content", map[string]any{"path": "big.txt", "content": strings.Repeat("a", maxFileContentBytes+1)}, "content too large"},
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
