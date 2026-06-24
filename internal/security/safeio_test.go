package security

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestSafeWriteFile_RefusesSwappedSymlinkComponent is the H1 regression: a workspace
// path component is a symlink pointing OUTSIDE the workspace (the swap a confined
// subprocess can perform after a check-time validation). The write must be REFUSED and
// land nothing outside the fence — not followed through the symlink. Before the fix,
// resolveInRoot validated at check time and os.WriteFile re-walked the path at use time,
// following the swapped symlink out of the workspace.
func TestSafeWriteFile_RefusesSwappedSymlinkComponent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()

	// Simulate the post-check swap: "build" is now a symlink to an outside directory.
	link := filepath.Join(root, "build")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	err := SafeWriteFile(root, "build/authorized_keys", []byte("pwned"), 0o644)
	if !errors.Is(err, ErrPathEscape) {
		t.Fatalf("SafeWriteFile through escaping symlink err = %v, want ErrPathEscape", err)
	}

	// The fence held: nothing was written into the outside directory.
	if _, statErr := os.Stat(filepath.Join(outside, "authorized_keys")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("write escaped the fence: file present outside the workspace (stat err = %v)", statErr)
	}
}

// TestSafeReadFile_RefusesSwappedSymlinkComponent is the read-side H1 regression: a read
// through a workspace component symlinked outside the workspace must be refused, so the
// read-modify-write tools cannot be steered to slurp a host file via a swapped component.
func TestSafeReadFile_RefusesSwappedSymlinkComponent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "id_rsa")
	if err := os.WriteFile(secret, []byte("PRIVATE KEY"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	link := filepath.Join(root, "ssh")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	_, err := SafeReadFile(root, "ssh/id_rsa")
	if !errors.Is(err, ErrPathEscape) {
		t.Fatalf("SafeReadFile through escaping symlink err = %v, want ErrPathEscape", err)
	}
}

// TestSafeWriteFile_RefusesFinalSymlinkToOutside covers the case where the FINAL
// component is itself a symlink to an outside path (the "leaf is a symlink" variant): the
// write must not follow it out of the fence.
func TestSafeWriteFile_RefusesFinalSymlinkToOutside(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "target.txt")
	link := filepath.Join(root, "leak")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	err := SafeWriteFile(root, "leak", []byte("data"), 0o644)
	if !errors.Is(err, ErrPathEscape) {
		t.Fatalf("SafeWriteFile through final-component symlink err = %v, want ErrPathEscape", err)
	}
	if _, statErr := os.Stat(target); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("write escaped via final-component symlink (stat err = %v)", statErr)
	}
}

// TestSafeWriteFile_WritesWithinRoot is the positive control: an ordinary in-workspace
// write (including a not-yet-existing nested path) succeeds and creates the file inside
// the fence.
func TestSafeWriteFile_WritesWithinRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := SafeWriteFile(root, "sub/dir/new.txt", []byte("hello"), 0o644); err != nil {
		t.Fatalf("SafeWriteFile within root: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "sub", "dir", "new.txt"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("content = %q, want %q", got, "hello")
	}
}

// TestSafeWriteFile_RejectsTraversal proves a "../" escape is refused with ErrPathEscape
// before any fd is opened (the containment check), matching ResolveInRoot's behaviour.
func TestSafeWriteFile_RejectsTraversal(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := SafeWriteFile(root, "../escape.txt", []byte("x"), 0o644); !errors.Is(err, ErrPathEscape) {
		t.Fatalf("traversal write err = %v, want ErrPathEscape", err)
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(root), "escape.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("traversal write escaped the fence (stat err = %v)", statErr)
	}
}

// TestSafeReadFile_ReadsWithinRoot is the read positive control.
func TestSafeReadFile_ReadsWithinRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("body"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	got, err := SafeReadFile(root, "f.txt")
	if err != nil {
		t.Fatalf("SafeReadFile within root: %v", err)
	}
	if string(got) != "body" {
		t.Errorf("content = %q, want %q", got, "body")
	}
}
