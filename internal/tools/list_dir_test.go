package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedTree creates a small directory tree under root for the list_dir and grep tests.
func seedTree(t *testing.T, root string) {
	t.Helper()
	dirs := []string{"src", "src/inner", "node_modules", ".hidden"}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatalf("setup mkdir %s: %v", d, err)
		}
	}
	files := map[string]string{
		"top.txt":           "alpha",
		"src/a.go":          "package a\nfunc Alpha() {}",
		"src/inner/b.go":    "package b\nfunc Beta() {}",
		"node_modules/x.js": "noise",
		".hidden/secret":    "hidden",
		".dotfile":          "dot",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatalf("setup write %s: %v", name, err)
		}
	}
}

func TestListDir_Execute_ListsTopLevel(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedTree(t, root)

	result, err := NewListDir(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "."}))

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}
	if !strings.Contains(result.Content, "top.txt") || !strings.Contains(result.Content, "src/") {
		t.Errorf("listing missing expected entries: %q", result.Content)
	}
	if strings.Contains(result.Content, "node_modules") || strings.Contains(result.Content, ".dotfile") || strings.Contains(result.Content, ".hidden") {
		t.Errorf("listing leaked excluded/hidden entries: %q", result.Content)
	}
}

func TestListDir_Execute_RecursesWhenAsked(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedTree(t, root)

	result, err := NewListDir(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "src", "recursive": true}))

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(result.Content, "a.go") || !strings.Contains(result.Content, "b.go") {
		t.Errorf("recursive listing missing nested entries: %q", result.Content)
	}
}

func TestListDir_Execute_NonRecursiveStopsAtTop(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedTree(t, root)

	result, err := NewListDir(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "src"}))

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if strings.Contains(result.Content, "b.go") {
		t.Errorf("non-recursive listing leaked nested entry: %q", result.Content)
	}
}

func TestListDir_Execute_ToolErrors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	tool := NewListDir(root)

	cases := []struct {
		name        string
		args        map[string]any
		wantContain string
	}{
		{"not a directory", map[string]any{"path": "f.txt"}, "not a directory"},
		{"missing directory", map[string]any{"path": "absent"}, "directory not found"},
		{"path escape", map[string]any{"path": "../"}, "outside the workspace"},
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
