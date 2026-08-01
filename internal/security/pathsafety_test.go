package security

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveInRoot_StaysWithinRoot_Resolves(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	cases := []struct {
		name string
		in   string
	}{
		{"relative file", "file.txt"},
		{"relative nested not-yet-existing", "sub/dir/new.txt"},
		{"the root itself", "."},
		{"absolute inside root", filepath.Join(root, "file.txt")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ResolveInRoot(tc.in, root)

			if err != nil {
				t.Fatalf("ResolveInRoot(%q) returned error: %v", tc.in, err)
			}
			realRoot := EvalRealPath(root)
			if got != realRoot && !strings.HasPrefix(got, realRoot+string(filepath.Separator)) {
				t.Errorf("resolved %q outside root %q", got, realRoot)
			}
		})
	}
}

func TestResolveInRoot_EscapesRoot_ReturnsErrPathEscape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	cases := []struct {
		name string
		in   string
	}{
		{"parent traversal", "../escape.txt"},
		{"deep traversal", "a/b/../../../escape.txt"},
		{"absolute outside root", filepath.Dir(root)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := ResolveInRoot(tc.in, root)

			if !errors.Is(err, ErrPathEscape) {
				t.Fatalf("ResolveInRoot(%q) err = %v, want ErrPathEscape", tc.in, err)
			}
		})
	}
}

// WorkspaceRelative names an already-resolved path the way a fenced open wants it: relative to the
// root, measured against the root's REAL path (a root reached through a symlink is the case a
// plain Rel gets wrong), and falling back to the absolute path rather than to a "../.."-laden name
// that a fenced open would have to refuse.
func TestWorkspaceRelative_NamesPathsWithinTheRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	realRoot := EvalRealPath(root)
	outside := filepath.Join(filepath.Dir(realRoot), "elsewhere", "secret.txt")

	cases := []struct {
		name string
		path string
		want string
	}{
		{
			name: "a file in the root",
			path: filepath.Join(realRoot, "report.html"),
			want: "report.html",
		},
		{
			name: "a file in a subdirectory",
			path: filepath.Join(realRoot, "docs", "report.html"),
			want: filepath.Join("docs", "report.html"),
		},
		{
			name: "the root itself",
			path: realRoot,
			want: ".",
		},
		{
			name: "a path outside the root stays absolute rather than climbing out",
			path: outside,
			want: outside,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The configured root, not its resolved form: on a box where it is reached through a
			// symlink (macOS /tmp) these differ, and the resolved one is what must be measured.
			if got := WorkspaceRelative(tc.path, root); got != tc.want {
				t.Errorf("WorkspaceRelative(%q, %q) = %q, want %q", tc.path, root, got, tc.want)
			}
		})
	}
}

func TestResolveInRoot_SymlinkEscape_ReturnsErrPathEscape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	_, err := ResolveInRoot("link/secret.txt", root)

	if !errors.Is(err, ErrPathEscape) {
		t.Fatalf("ResolveInRoot through escaping symlink err = %v, want ErrPathEscape", err)
	}
}
