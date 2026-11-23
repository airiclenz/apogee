//go:build windows

package realpath

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/realpath/junctiontest"
)

// TestResolveCrossesJunction is the defect the resolver exists for: a path beneath an NTFS
// junction resolves to the junction target's spelling, with no \\?\ prefix, where
// filepath.EvalSymlinks fails outright. The expected path is built from tempDir — the long-name
// spelling — because CI's t.TempDir is the 8.3 RUNNER~1 form and the final path name never is.
func TestResolveCrossesJunction(t *testing.T) {
	t.Parallel()

	root := tempDir(t)
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("mkdir the junction target: %v", err)
	}
	inner := filepath.Join(target, "inner")
	if err := os.WriteFile(inner, []byte("x"), 0o600); err != nil {
		t.Fatalf("write a file beneath the target: %v", err)
	}
	link := filepath.Join(root, "link")
	junctiontest.Make(t, link, target)

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "the junction itself", path: link, want: target},
		{name: "a file beneath the junction", path: filepath.Join(link, "inner"), want: inner},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := Resolve(tc.path)

			if err != nil {
				t.Fatalf("Resolve(%q): %v", tc.path, err)
			}
			if strings.HasPrefix(got, `\\?\`) {
				t.Errorf("Resolve(%q) = %q; want no extended-length prefix", tc.path, got)
			}
			if !strings.EqualFold(got, tc.want) {
				t.Errorf("Resolve(%q) = %q; want the target spelling %q", tc.path, got, tc.want)
			}
		})
	}
}

// TestResolveLenientCrossesJunction is the path-safety half of the same defect: a file about to
// be created beneath a junction resolves to the junction target's spelling with the missing rest
// re-joined, and the junction-free root beside it comes back in the same final-name spelling.
func TestResolveLenientCrossesJunction(t *testing.T) {
	t.Parallel()

	root := tempDir(t)
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("mkdir the junction target: %v", err)
	}
	link := filepath.Join(root, "link")
	junctiontest.Make(t, link, target)

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "the junction-free root", path: root, want: root},
		{name: "the junction itself", path: link, want: target},
		{name: "a missing leaf beneath the junction", path: filepath.Join(link, "missing", "leaf"), want: filepath.Join(target, "missing", "leaf")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := ResolveLenient(tc.path)

			if strings.HasPrefix(got, `\\?\`) {
				t.Errorf("ResolveLenient(%q) = %q; want no extended-length prefix", tc.path, got)
			}
			if !strings.EqualFold(got, tc.want) {
				t.Errorf("ResolveLenient(%q) = %q; want the target spelling %q", tc.path, got, tc.want)
			}
		})
	}
}

// TestIsLinkSeesAJunction pins the Windows half of IsLink: a junction is a link, the directory it
// points at is not.
func TestIsLinkSeesAJunction(t *testing.T) {
	t.Parallel()

	root := tempDir(t)
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("mkdir the junction target: %v", err)
	}
	link := filepath.Join(root, "link")
	junctiontest.Make(t, link, target)

	for path, want := range map[string]bool{link: true, target: false} {
		got, err := IsLink(path)
		if err != nil {
			t.Fatalf("IsLink(%q): %v", path, err)
		}
		if got != want {
			t.Errorf("IsLink(%q) = %v; want %v", path, got, want)
		}
	}
}

// tempDir is t.TempDir in its final on-disk spelling. CI's t.TempDir is the 8.3 RUNNER~1 form,
// and the final path name the resolver answers with never is, so an expected path is built from
// this spelling.
func tempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if final, ok := FinalPathName(dir); ok {
		return final
	}
	return dir
}
