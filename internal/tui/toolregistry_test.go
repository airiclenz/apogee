package tui

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestGrepTarget pins what a grep row LEADS with. The pattern alone answers "what was searched
// for" but never "where", and the two searches a reader has to tell apart in a group — the whole
// workspace and one file — differ in nothing else, so the path the call scoped itself to and the
// include glob that narrowed it ride the target as qualifiers, in that order. A path of "." is the
// search every grep is until it says otherwise: it is dropped rather than spelled, and dropping it
// must not leave the glob orphaned behind a stray separator.
func TestGrepTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "an unscoped search is the pattern alone",
			args: map[string]any{"pattern": "KeyMsg"},
			want: "KeyMsg",
		},
		{
			name: "a path-scoped search names the path",
			args: map[string]any{"pattern": "KeyMsg", "path": "internal/tui/model.go"},
			want: "KeyMsg · internal/tui/model.go",
		},
		{
			name: "an include glob qualifies on its own",
			args: map[string]any{"pattern": "KeyMsg", "include": "*.go"},
			want: "KeyMsg · *.go",
		},
		{
			name: "path and glob chain in that order",
			args: map[string]any{"pattern": "KeyMsg", "path": "internal/tui", "include": "*.go"},
			want: "KeyMsg · internal/tui · *.go",
		},
		{
			name: "the workspace root itself adds nothing",
			args: map[string]any{"pattern": "KeyMsg", "path": "."},
			want: "KeyMsg",
		},
		{
			name: "dropping the workspace root leaves no stray separator",
			args: map[string]any{"pattern": "KeyMsg", "path": ".", "include": "*.go"},
			want: "KeyMsg · *.go",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := grepTarget(tt.args); got != tt.want {
				t.Errorf("grepTarget(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

// TestFindFilesTarget pins the same shape for the other search tool: the name pattern leads, the
// path the walk was scoped to qualifies it, and "." — the walk the tool does by default — is left
// unsaid. find_files has no include glob; a call that gives only a path is the path alone rather
// than a row opening on a separator.
func TestFindFilesTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "an unscoped walk is the pattern alone",
			args: map[string]any{"pattern": "*.go"},
			want: "*.go",
		},
		{
			name: "a path-scoped walk names the path",
			args: map[string]any{"pattern": "*.go", "path": "internal/tui"},
			want: "*.go · internal/tui",
		},
		{
			name: "the workspace root itself adds nothing",
			args: map[string]any{"pattern": "*.go", "path": "."},
			want: "*.go",
		},
		{
			name: "a path with no pattern stands alone",
			args: map[string]any{"path": "internal/tui"},
			want: "internal/tui",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := findFilesTarget(tt.args); got != tt.want {
				t.Errorf("findFilesTarget(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

// The target extractor is only half the claim: what the human reads is the painted branch. This
// folds one scoped grep call into a transcript and asserts the scope survives the whole presenting
// path — registry lookup, sanitize and the display seam — onto the row itself.
func TestGrepBranchRowShowsTheSearchedPath(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "grep",
		Arguments: []byte(`{"pattern":"KeyMsg","path":"internal/tui/model.go"}`)}})

	got := renderPlain(tr, 80)

	if !strings.Contains(got, "KeyMsg · internal/tui/model.go") {
		t.Errorf("grep row does not name the searched path:\n--- got ---\n%s", got)
	}
}
