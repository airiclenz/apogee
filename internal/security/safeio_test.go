package security

import (
	"errors"
	"io"
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

// TestSafeOpen_ReadsWithinRoot is the open positive control: the returned handle reads the
// in-root file's content, and stays readable after SafeOpen's internal pinning root has
// been closed (a file opened through an os.Root outlives the root).
func TestSafeOpen_ReadsWithinRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("body"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	f, err := SafeOpen(root, "f.txt")
	if err != nil {
		t.Fatalf("SafeOpen within root: %v", err)
	}
	defer f.Close()

	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read through the handle: %v", err)
	}
	if string(got) != "body" {
		t.Errorf("content = %q, want %q", got, "body")
	}
}

// TestSafeOpen_RejectsTraversal proves a "../" escape and an absolute path outside the
// root are refused with ErrPathEscape before any fd is opened (the containment check),
// matching the other Safe helpers.
func TestSafeOpen_RejectsTraversal(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, input := range []string{"../escape.txt", filepath.Join(filepath.Dir(root), "escape.txt")} {
		if _, err := SafeOpen(root, input); !errors.Is(err, ErrPathEscape) {
			t.Errorf("SafeOpen(%q) err = %v, want ErrPathEscape", input, err)
		}
	}
}

// TestSafeOpen_RefusesEscapingSymlink: a workspace component symlinked outside the root is
// refused at open time rather than followed, so no handle onto an outside file ever exists.
func TestSafeOpen_RefusesEscapingSymlink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "id_rsa"), []byte("PRIVATE KEY"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "ssh")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	if _, err := SafeOpen(root, "ssh/id_rsa"); !errors.Is(err, ErrPathEscape) {
		t.Fatalf("SafeOpen through escaping symlink err = %v, want ErrPathEscape", err)
	}
}

// TestSafeOpen_HandleSurvivesRename is the identity pin — the deterministic proof of what
// the racing swap probes could only show statistically: the handle SafeOpen returns is
// bound to the file that was opened, not to its name. A rename over that name while the
// handle is held (the adversarial flip the retired stat-first pattern lost to) changes
// nothing the handle sees — fstat and read through it still describe the originally-opened
// file, so a size bound decided from this descriptor cannot be bait-and-switched.
func TestSafeOpen_HandleSurvivesRename(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("original A"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("impostor B, rather longer"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	f, err := SafeOpen(root, "a.txt")
	if err != nil {
		t.Fatalf("SafeOpen: %v", err)
	}
	defer f.Close()

	// The flip: b.txt takes over a.txt's NAME while the handle is held.
	if err := os.Rename(filepath.Join(root, "b.txt"), filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("rename: %v", err)
	}

	info, err := f.Stat()
	if err != nil {
		t.Fatalf("fstat after rename: %v", err)
	}
	if want := int64(len("original A")); info.Size() != want {
		t.Errorf("fstat size after rename = %d, want %d (the originally-opened file)", info.Size(), want)
	}
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read after rename: %v", err)
	}
	if string(got) != "original A" {
		t.Errorf("content after rename = %q, want %q (the originally-opened file)", got, "original A")
	}
}
