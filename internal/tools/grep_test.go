package tools

import (
	"context"
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
