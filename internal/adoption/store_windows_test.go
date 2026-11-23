//go:build windows

package adoption

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/realpath"
	"github.com/airiclenz/apogee/internal/realpath/junctiontest"
)

// longTempDir is a fresh temporary folder in its long-name spelling. CI's t.TempDir is the 8.3
// RUNNER~1 form, and a path resolved through a junction never is, so every expected path is built
// from this one.
func longTempDir(t *testing.T) string {
	t.Helper()
	dir, err := realpath.Resolve(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the temp dir: %v", err)
	}
	return dir
}

// junction-project-root: a Project root reached through a junction opens a store — where adopt used
// to hard-error "adoption store: resolve Project root" — and shares one record and one config lock
// with the root's own spelling, as a symlinked spelling does.
func TestStoreResolvesARootThroughAJunction(t *testing.T) {
	t.Parallel()

	base := longTempDir(t)
	dir := filepath.Join(base, "workspaces")
	root := filepath.Join(base, "repo")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("mkdir the root: %v", err)
	}
	link := filepath.Join(base, "link")
	junctiontest.Make(t, link, root)

	direct, err := New(dir, root)
	if err != nil {
		t.Fatalf("New(%q): %v", root, err)
	}
	viaJunction, err := New(dir, link)
	if err != nil {
		t.Fatalf("New through the junction %q: %v", link, err)
	}
	if strings.HasPrefix(viaJunction.Root(), `\\?\`) || !strings.EqualFold(viaJunction.Root(), root) {
		t.Errorf("New(%q).Root() = %q, want the target spelling %q", link, viaJunction.Root(), root)
	}
	if viaJunction.Path() != direct.Path() {
		t.Errorf("the junction spelling records at %q, want the root's own record %q", viaJunction.Path(), direct.Path())
	}

	lock, err := ConfigLockPath(dir, link)
	if err != nil {
		t.Fatalf("ConfigLockPath through the junction %q: %v", link, err)
	}
	again, err := ConfigLockPath(dir, link)
	if err != nil {
		t.Fatalf("ConfigLockPath again: %v", err)
	}
	directLock, err := ConfigLockPath(dir, root)
	if err != nil {
		t.Fatalf("ConfigLockPath(%q): %v", root, err)
	}
	if lock != again || lock != directLock {
		t.Errorf("ConfigLockPath through the junction = %q then %q, want both %q (the root's own lock)", lock, again, directLock)
	}
}
