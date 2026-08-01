package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
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

func TestListDir_Execute_ReportsEntryCounts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedTree(t, root)
	tool := NewListDir(root)

	cases := []struct {
		name        string
		args        map[string]any
		wantContent string
		wantSummary domain.ListedEntries
	}{
		{
			name:        "no pagination",
			args:        map[string]any{"path": "."},
			wantContent: "[2 entries total]\nsrc/\ntop.txt",
			wantSummary: domain.ListedEntries{Total: 2, Skipped: 0},
		},
		{
			name:        "offset skips entries",
			args:        map[string]any{"path": ".", "offset": 1},
			wantContent: "[2 entries total, skipped first 1]\ntop.txt",
			wantSummary: domain.ListedEntries{Total: 2, Skipped: 1},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := tool.Execute(context.Background(), callWith(t, "c1", tc.args))

			if err != nil {
				t.Fatalf("Execute returned a Go error: %v", err)
			}
			if result.Content != tc.wantContent {
				t.Errorf("Content = %q, want %q", result.Content, tc.wantContent)
			}
			listed, ok := result.Summary.(domain.ListedEntries)
			if !ok {
				t.Fatalf("Summary = %#v, want a domain.ListedEntries", result.Summary)
			}
			if listed != tc.wantSummary {
				t.Errorf("Summary = %+v, want %+v", listed, tc.wantSummary)
			}
		})
	}
}

// TestListDir_RefusesEscapingSymlink pins the workspace fence on every directory the walk
// OPENS, not just on the path argument it was given: a symlinked directory pointing out of
// the workspace is never listed through, whether the model names it directly or the
// recursion reaches it. list_dir is ReadOnly(), so it runs unapproved in every mode, and an
// inventory of `~/.ssh` is reconnaissance even when no file is read.
//
// The named-directly case is a boundary pin (resolveInRoot already refuses it, and the walk
// now ALSO opens through the pinned root); the recursive case additionally pins that
// descent happens by workspace-relative NAME through the fence — a link is never a
// directory the walk descends into.
func TestListDir_RefusesEscapingSymlink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "ssh"), 0o755); err != nil {
		t.Fatalf("setup outside dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "ssh", "id_rsa"), []byte("KEY"), 0o600); err != nil {
		t.Fatalf("setup outside file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "src", "inner"), 0o755); err != nil {
		t.Fatalf("setup workspace dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "inner", "a.go"), []byte("package a"), 0o644); err != nil {
		t.Fatalf("setup workspace file: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "ssh"), filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	tool := NewListDir(root)

	t.Run("named directly", func(t *testing.T) {
		t.Parallel()

		result, err := tool.Execute(context.Background(),
			callWith(t, "c1", map[string]any{"path": "escape"}))

		if err != nil {
			t.Fatalf("Execute returned a Go error: %v", err)
		}
		if !result.IsError {
			t.Fatalf("IsError = false, want true: a directory outside the workspace was listed (%q)", result.Content)
		}
		if !strings.Contains(result.Content, "outside the workspace") {
			t.Errorf("content %q does not carry the path-escape message", result.Content)
		}
		if strings.Contains(result.Content, "id_rsa") {
			t.Errorf("content leaked the directory outside the workspace: %q", result.Content)
		}
	})

	t.Run("recursive walk", func(t *testing.T) {
		t.Parallel()

		result, err := tool.Execute(context.Background(),
			callWith(t, "c1", map[string]any{"path": ".", "recursive": true}))

		if err != nil {
			t.Fatalf("Execute returned a Go error: %v", err)
		}
		if result.IsError {
			t.Fatalf("unexpected tool error: %q", result.Content)
		}
		if strings.Contains(result.Content, "id_rsa") {
			t.Errorf("the walk listed through a symlink out of the workspace: %q", result.Content)
		}
		if strings.Contains(result.Content, "escape/") {
			t.Errorf("the escaping symlink was descended into as a directory: %q", result.Content)
		}
		if !strings.Contains(result.Content, "inner/") || !strings.Contains(result.Content, "a.go") {
			t.Errorf("the ordinary in-workspace subtree stopped being listed: %q", result.Content)
		}
	})
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
