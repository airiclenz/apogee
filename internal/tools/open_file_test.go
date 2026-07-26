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

// TestOpenFile_RefusesEscapingSymlink is open_file's half of the STATIC fence pin: a
// workspace path component that is a symlink pointing OUTSIDE the workspace is refused with
// the uniform ErrPathEscape message rather than "file not found", and nothing from outside
// reaches the result.
//
// As on the read_file side this is a boundary pin, not new behaviour — the former
// resolveInRoot → os.Stat → os.ReadFile trio also refused this static case at check time,
// so the test passes against the pre-change code. What this item changed for open_file is
// pinned by TestOpenFile_RefusesComponentSwappedMidRead and
// TestOpenFile_RefusesAbsoluteInRootSymlink.
func TestOpenFile_RefusesEscapingSymlink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "id_rsa"), []byte(outsideMarker), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "ssh")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	result, err := NewOpenFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "ssh/id_rsa"}))

	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("IsError = false, want true: the read followed a symlink out of the workspace (content: %q)", result.Content)
	}
	if !strings.Contains(result.Content, ErrPathEscape.Error()) {
		t.Errorf("content %q does not carry the ErrPathEscape message %q", result.Content, ErrPathEscape.Error())
	}
	if strings.Contains(result.Content, outsideMarker) {
		t.Errorf("content leaked the file outside the workspace: %q", result.Content)
	}
}

// TestOpenFile_RefusesComponentSwappedMidRead is the behaviour open_file gained (D9): a
// workspace component swapped to an outside-pointing symlink while the call is in flight no
// longer redirects the read, because the stat that bounds the file and the read that
// materialises it resolve against one pinned root. It fails against the pre-change code.
func TestOpenFile_RefusesComponentSwappedMidRead(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	escapes := escapesUnderComponentSwap(t, NewOpenFile(root), root, 2000)

	if escapes != 0 {
		t.Errorf("%d of 2000 reads returned the file outside the workspace, want 0", escapes)
	}
}

// TestOpenFile_RefusesAbsoluteInRootSymlink pins open_file's half of the narrowing: an
// in-workspace symlink whose target is spelled as an ABSOLUTE path is refused even when the
// target is inside the workspace, because the pinned root resolves relative components
// only. It read fine before; the tightening is recorded in the CHANGELOG, and relative
// in-workspace symlinks still read (TestOpenFile_ReadsRelativeInRootSymlink).
func TestOpenFile_RefusesAbsoluteInRootSymlink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "real.txt")
	if err := os.WriteFile(target, []byte("inside the workspace"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	result, err := NewOpenFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "link.txt"}))

	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("IsError = false, want true: the absolute-target symlink narrowing is gone (content: %q)", result.Content)
	}
	if !strings.Contains(result.Content, ErrPathEscape.Error()) {
		t.Errorf("content %q does not carry the ErrPathEscape message %q", result.Content, ErrPathEscape.Error())
	}
}

// TestOpenFile_ReadsRelativeInRootSymlink is the positive control bounding that narrowing: a
// RELATIVE in-workspace symlink, as the target file and as a directory component, still
// opens exactly as it did before.
func TestOpenFile_ReadsRelativeInRootSymlink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "real.txt"), []byte("inside the workspace"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(filepath.Join("sub", "real.txt"), filepath.Join(root, "file_link.txt")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if err := os.Symlink("sub", filepath.Join(root, "dir_link")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	for _, path := range []string{"file_link.txt", "dir_link/real.txt"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			result, err := NewOpenFile(root).Execute(context.Background(),
				callWith(t, "c1", map[string]any{"path": path}))

			if err != nil {
				t.Fatalf("Execute returned a Go error: %v", err)
			}
			if result.IsError {
				t.Fatalf("IsError = true on an in-workspace relative symlink (content: %q)", result.Content)
			}
			if !strings.Contains(result.Content, "inside the workspace") {
				t.Errorf("content %q does not carry the linked file's body", result.Content)
			}
		})
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
