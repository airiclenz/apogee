package tools

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
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

// TestOpenFile_ReportsWhatItOpened pins the structured half of open_file's outcome: the
// body's line count, the requested locate term, and the lines it was found on. Lines must
// equal what a reader of the rendered text counts — the text minus the "File: …" header and
// its blank separator — and that equality is asserted here, because it is the oracle for
// the summary replacing the prose.
func TestOpenFile_ReportsWhatItOpened(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTempFile(t, root, "f.txt", "first\nTODO here\nthird\nTODO again\n")
	tool := NewOpenFile(root)

	cases := []struct {
		name        string
		args        map[string]any
		wantContent string
		wantSummary domain.OpenedFile
	}{
		{
			name:        "no locate",
			args:        map[string]any{"path": "f.txt"},
			wantContent: "File: f.txt\n\nfirst\nTODO here\nthird\nTODO again\n",
			wantSummary: domain.OpenedFile{Lines: 5},
		},
		{
			name:        "locate matches",
			args:        map[string]any{"path": "f.txt", "locate": "TODO"},
			wantContent: "File: f.txt\nLocated \"TODO\" on lines: 2, 4\n\nfirst\nTODO here\nthird\nTODO again\n",
			wantSummary: domain.OpenedFile{Lines: 5, Locate: "TODO", LocatedOn: []int{2, 4}},
		},
		{
			// A set Locate with no LocatedOn is the fact the prose cannot state without a
			// prefix test: the term WAS requested and matched nothing.
			name:        "locate matches nothing",
			args:        map[string]any{"path": "f.txt", "locate": "absent"},
			wantContent: "File: f.txt\nLocated \"absent\" on no lines\n\nfirst\nTODO here\nthird\nTODO again\n",
			wantSummary: domain.OpenedFile{Lines: 5, Locate: "absent"},
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
			opened, ok := result.Summary.(domain.OpenedFile)
			if !ok {
				t.Fatalf("Summary = %#v, want a domain.OpenedFile", result.Summary)
			}
			if opened.Lines != tc.wantSummary.Lines || opened.Locate != tc.wantSummary.Locate {
				t.Errorf("Summary = %+v, want %+v", opened, tc.wantSummary)
			}
			if !slices.Equal(opened.LocatedOn, tc.wantSummary.LocatedOn) {
				t.Errorf("LocatedOn = %v, want %v", opened.LocatedOn, tc.wantSummary.LocatedOn)
			}
		})
	}
}

// TestOpenFile_LineCountMatchesTheRenderedBody: Lines is the body's own line count, which is
// the rendered text minus the header and the blank line beneath it — the number a reader of
// the prose derives by subtracting two.
func TestOpenFile_LineCountMatchesTheRenderedBody(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, body := range []string{"", "one", "one\ntwo\n", "one\ntwo\nthree"} {
		writeTempFile(t, root, "f.txt", body)

		result, err := NewOpenFile(root).Execute(context.Background(),
			callWith(t, "c1", map[string]any{"path": "f.txt"}))
		if err != nil {
			t.Fatalf("Execute returned a Go error: %v", err)
		}
		opened, ok := result.Summary.(domain.OpenedFile)
		if !ok {
			t.Fatalf("Summary = %#v, want a domain.OpenedFile", result.Summary)
		}
		if want := len(strings.Split(result.Content, "\n")) - 2; opened.Lines != want {
			t.Errorf("body %q: Lines = %d, want %d (rendered lines minus header and separator)",
				body, opened.Lines, want)
		}
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
