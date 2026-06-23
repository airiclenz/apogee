package tools

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

			got, err := resolveInRoot(tc.in, root)

			if err != nil {
				t.Fatalf("resolveInRoot(%q) returned error: %v", tc.in, err)
			}
			realRoot := evalRealPath(root)
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

			_, err := resolveInRoot(tc.in, root)

			if !errors.Is(err, ErrPathEscape) {
				t.Fatalf("resolveInRoot(%q) err = %v, want ErrPathEscape", tc.in, err)
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

	_, err := resolveInRoot("link/secret.txt", root)

	if !errors.Is(err, ErrPathEscape) {
		t.Fatalf("resolveInRoot through escaping symlink err = %v, want ErrPathEscape", err)
	}
}
