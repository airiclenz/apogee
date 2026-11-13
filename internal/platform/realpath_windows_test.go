//go:build windows

package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRealPathCrossesJunction is the defect the resolver exists for: a path beneath an NTFS
// junction resolves to the junction target's spelling, with no \\?\ prefix, where
// filepath.EvalSymlinks fails outright. The expected path is built from tempDir — the long-name
// spelling — because CI's t.TempDir is the 8.3 RUNNER~1 form and the final path name never is.
func TestRealPathCrossesJunction(t *testing.T) {
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
	makeJunction(t, link, target)

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

			got, err := RealPath(tc.path)

			if err != nil {
				t.Fatalf("RealPath(%q): %v", tc.path, err)
			}
			if strings.HasPrefix(got, `\\?\`) {
				t.Errorf("RealPath(%q) = %q; want no extended-length prefix", tc.path, got)
			}
			if !strings.EqualFold(got, tc.want) {
				t.Errorf("RealPath(%q) = %q; want the target spelling %q", tc.path, got, tc.want)
			}
		})
	}
}
