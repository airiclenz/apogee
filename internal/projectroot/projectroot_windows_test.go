//go:build windows

package projectroot_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/projectroot"
)

// longTempDir is a fresh temporary folder in its long-name spelling. CI's t.TempDir is the 8.3
// RUNNER~1 form, and a path resolved through a junction never is, so every expected path is built
// from this one.
func longTempDir(t *testing.T) string {
	t.Helper()
	dir, err := platform.RealPath(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the temp dir: %v", err)
	}
	return dir
}

// makeJunction creates an NTFS junction at link pointing at target. A junction needs no privilege,
// unlike a symbolic link, so the test never skips.
func makeJunction(t *testing.T, link, target string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J %q %q: %v (%s)", link, target, err, out)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat the junction %q: %v", link, err)
	}
	if info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) == 0 {
		t.Fatalf("lstat %q mode = %v; the junction did not register as a reparse point", link, info.Mode())
	}
}

// junction-project-root: a workspace reached through a junction resolves like one reached through
// a symlink — the walk climbs from the junction target to the `.apogee/` above it, where it used to
// fail to resolve the workspace and silently drop the project layer.
func TestResolveThroughJunction(t *testing.T) {
	t.Parallel()

	base := longTempDir(t)
	repo := filepath.Join(base, "repo")
	mkdirs(t, repo, ".git", ".apogee", "pkg/sub")
	link := filepath.Join(base, "link")
	makeJunction(t, link, repo)

	tests := []struct {
		name      string
		workspace string
		want      string
	}{
		{
			name:      "a workspace below the junction walks up to the root above it",
			workspace: filepath.Join(link, "pkg", "sub"),
			want:      repo,
		},
		{
			name:      "a workspace that is the root keeps its own junction spelling",
			workspace: link,
			want:      link,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := projectroot.Resolve(tc.workspace, "")

			if strings.HasPrefix(got, `\\?\`) {
				t.Errorf("Resolve(%q) = %q; want no extended-length prefix", tc.workspace, got)
			}
			if !strings.EqualFold(got, tc.want) {
				t.Errorf("Resolve(%q) = %q, want %q", tc.workspace, got, tc.want)
			}
		})
	}
}
