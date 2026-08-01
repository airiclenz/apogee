package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

func TestGrep_Execute_FindsMatchesWithLocation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedTree(t, root)

	result, err := NewGrep(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"pattern": "^package "}))

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}
	if !strings.Contains(result.Content, "src/a.go:1:package a") {
		t.Errorf("missing expected match for src/a.go: %q", result.Content)
	}
	if !strings.Contains(result.Content, "b.go:1:package b") {
		t.Errorf("missing expected match for nested b.go: %q", result.Content)
	}
}

func TestGrep_Execute_ExcludesNoiseDirs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedTree(t, root)

	result, err := NewGrep(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"pattern": "noise"}))

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(result.Content, "No matches found") {
		t.Errorf("node_modules match leaked through exclusion: %q", result.Content)
	}
}

func TestGrep_Execute_IncludeGlobNarrows(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedTree(t, root)

	result, err := NewGrep(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"pattern": "func", "include": "*.go"}))

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(result.Content, "a.go") {
		t.Errorf("include glob excluded a matching .go file: %q", result.Content)
	}
}

func TestGrep_Execute_InvalidRegexFallsBackToLiteral(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedTree(t, root)

	// "Alpha(" is not a valid regex (unclosed group); it must be matched literally
	// against "func Alpha() {}".
	result, err := NewGrep(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"pattern": "Alpha("}))

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(result.Content, "a.go") {
		t.Errorf("literal fallback failed to match: %q", result.Content)
	}
}

func TestGrep_Execute_SearchesSingleFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedTree(t, root)

	result, err := NewGrep(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"pattern": "Beta", "path": "src/inner/b.go"}))

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(result.Content, "b.go:2:") || !strings.Contains(result.Content, "Beta") {
		t.Errorf("single-file search missing match: %q", result.Content)
	}
}

func TestGrep_Execute_ReportsMatchCount(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedTree(t, root)
	tool := NewGrep(root)

	cases := []struct {
		name        string
		args        map[string]any
		wantContent string
		wantSummary domain.MatchedLines
	}{
		{
			name:        "matches",
			args:        map[string]any{"pattern": "^package "},
			wantContent: "[2 total matches, showing 1-2]\nsrc/a.go:1:package a\nsrc/inner/b.go:1:package b",
			wantSummary: domain.MatchedLines{Total: 2},
		},
		{
			// The sentinel path carries a summary too, so a host reads a zero rather
			// than testing this sentence for a "No matches" prefix.
			name:        "no matches",
			args:        map[string]any{"pattern": "zzz-nothing-matches-this"},
			wantContent: "No matches found",
			wantSummary: domain.MatchedLines{Total: 0},
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
			matched, ok := result.Summary.(domain.MatchedLines)
			if !ok {
				t.Fatalf("Summary = %#v, want a domain.MatchedLines", result.Summary)
			}
			if matched != tc.wantSummary {
				t.Errorf("Summary = %+v, want %+v", matched, tc.wantSummary)
			}
		})
	}
}

// TestGrep_Execute_SearchesSubdirectory covers the walk that does NOT start at the
// workspace root: the fence is anchored at the workspace (so a link out of the SUBTREE but
// still inside the workspace keeps matching), while the reported location stays relative to
// the searched directory, as before. It is the deterministic cover for the walk-relative →
// workspace-relative lift the fenced open needs.
func TestGrep_Execute_SearchesSubdirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedTree(t, root)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "id_rsa"), []byte(grepOutsideMarker), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "id_rsa"), filepath.Join(root, "src", "notes.go")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	// ../top.txt leaves the searched subtree but stays inside the workspace: still searched.
	if err := os.Symlink(filepath.Join("..", "top.txt"), filepath.Join(root, "src", "up.txt")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	result, err := NewGrep(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"pattern": "a|TOKEN", "path": "src"}))

	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}
	if !strings.Contains(result.Content, "a.go:1:package a") {
		t.Errorf("location is not relative to the searched directory: %q", result.Content)
	}
	if !strings.Contains(result.Content, "inner/b.go:2:func Beta() {}") {
		t.Errorf("nested location is not relative to the searched directory: %q", result.Content)
	}
	if !strings.Contains(result.Content, "up.txt:1:alpha") {
		t.Errorf("a symlink out of the subtree but inside the workspace stopped matching: %q", result.Content)
	}
	if strings.Contains(result.Content, grepOutsideMarker) {
		t.Errorf("the subdirectory walk read a file outside the workspace: %q", result.Content)
	}
}

// grepOutsideMarker is the body of the file OUTSIDE the workspace — the audit probe's own
// payload. A grep result carrying it read through the fence.
const grepOutsideMarker = "SUPERSECRET_TOKEN=abc123"

// TestGrep_RefusesEscapingSymlink pins the workspace fence on the file grep actually READS,
// not just on the path argument it was given. git stores symlinks verbatim, so a clone can
// plant `notes.txt -> ~/.ssh/id_rsa`; the walk yields it as an ordinary non-directory entry,
// and grep is ReadOnly() — it runs unapproved in every mode, Plan included. Before the fence
// reached the walk, an ordinary whole-workspace grep returned the outside file's matching
// lines as tool content (the audit's probe read SUPERSECRET_TOKEN through exactly this
// shape), so the "directory walk" case fails against the pre-change code.
//
// The "single file" case is a boundary pin rather than new behaviour: the top-level path
// argument was already fenced by resolveInRoot, and it now ALSO opens through the pinned
// root — this fails if either half is ever dropped.
func TestGrep_RefusesEscapingSymlink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "id_rsa"), []byte(grepOutsideMarker), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "inside.txt"), []byte("WORKSPACE_TOKEN=ok"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "id_rsa"), filepath.Join(root, "notes.txt")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	// A RELATIVE in-workspace symlink must still be searched: the fence narrows what leaves
	// the workspace, never what stays inside it.
	if err := os.Symlink("inside.txt", filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	tool := NewGrep(root)

	t.Run("directory walk", func(t *testing.T) {
		t.Parallel()

		result, err := tool.Execute(context.Background(),
			callWith(t, "c1", map[string]any{"pattern": "TOKEN="}))

		if err != nil {
			t.Fatalf("Execute returned a Go error: %v", err)
		}
		if result.IsError {
			t.Fatalf("unexpected tool error: %q", result.Content)
		}
		if strings.Contains(result.Content, grepOutsideMarker) {
			t.Errorf("the walk read a file outside the workspace: %q", result.Content)
		}
		if strings.Contains(result.Content, "notes.txt") {
			t.Errorf("the escaping symlink was reported as a match source: %q", result.Content)
		}
		if !strings.Contains(result.Content, "inside.txt:1:WORKSPACE_TOKEN=ok") {
			t.Errorf("the ordinary in-workspace file stopped matching: %q", result.Content)
		}
		if !strings.Contains(result.Content, "link.txt:1:WORKSPACE_TOKEN=ok") {
			t.Errorf("a relative in-workspace symlink stopped matching: %q", result.Content)
		}
		if matched, ok := result.Summary.(domain.MatchedLines); !ok || matched.Total != 2 {
			t.Errorf("Summary = %#v, want domain.MatchedLines{Total: 2}", result.Summary)
		}
	})

	t.Run("single file", func(t *testing.T) {
		t.Parallel()

		result, err := tool.Execute(context.Background(),
			callWith(t, "c1", map[string]any{"pattern": "TOKEN=", "path": "notes.txt"}))

		if err != nil {
			t.Fatalf("Execute returned a Go error: %v", err)
		}
		if !result.IsError {
			t.Fatalf("IsError = false, want true: the search followed a symlink out of the workspace (content: %q)", result.Content)
		}
		if !strings.Contains(result.Content, "outside the workspace") {
			t.Errorf("content %q does not carry the path-escape message", result.Content)
		}
		if strings.Contains(result.Content, grepOutsideMarker) {
			t.Errorf("content leaked the file outside the workspace: %q", result.Content)
		}
	})
}

func TestGrep_Execute_ToolErrors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedTree(t, root)
	tool := NewGrep(root)

	cases := []struct {
		name        string
		args        map[string]any
		wantContain string
	}{
		{"missing pattern", map[string]any{}, "pattern is required"},
		{"path escape", map[string]any{"pattern": "x", "path": "../"}, "outside the workspace"},
		{"missing path", map[string]any{"pattern": "x", "path": "absent"}, "path not found"},
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
