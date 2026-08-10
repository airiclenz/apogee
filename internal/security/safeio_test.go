package security

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

// assertNoStagingLeftovers walks dir and fails if any of SafeWriteFile's staging files
// survived the call — the atomic write must leave the tree exactly as clean as the direct
// write it replaced, on success and on failure alike.
func assertNoStagingLeftovers(t *testing.T, dir string) {
	t.Helper()

	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), stagingPrefix) {
			t.Errorf("staging file left behind: %s", path)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", dir, walkErr)
	}
}

// TestSafeWriteFile_OverwritePreservesMode: the rename must land the target with the mode
// it already had, not with the caller's perm (which only ever governs a NEW file). An
// executable script rewritten by a tool has to stay executable.
func TestSafeWriteFile_OverwritePreservesMode(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "run.sh")
	if err := os.WriteFile(target, []byte("#!/bin/sh\necho old\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatalf("setup chmod: %v", err)
	}

	if err := SafeWriteFile(root, "run.sh", []byte("#!/bin/sh\necho new\n"), 0o600); err != nil {
		t.Fatalf("SafeWriteFile overwrite: %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat after overwrite: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Errorf("mode after overwrite = %v, want %v (the target's own mode, not perm)", got, os.FileMode(0o755))
	}
	assertNoStagingLeftovers(t, root)
}

// TestSafeWriteFile_NewFileTakesPermArgument: a target that does not exist yet is created
// with the caller's perm. The reference file written by os.WriteFile with the same perm
// pins the expectation umask-independently — the staged create must be no more permissive
// than an ordinary create.
func TestSafeWriteFile_NewFileTakesPermArgument(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "reference.txt"), []byte("x"), 0o640); err != nil {
		t.Fatalf("setup: %v", err)
	}
	reference, err := os.Stat(filepath.Join(root, "reference.txt"))
	if err != nil {
		t.Fatalf("stat reference: %v", err)
	}

	if err := SafeWriteFile(root, "fresh.txt", []byte("x"), 0o640); err != nil {
		t.Fatalf("SafeWriteFile new file: %v", err)
	}
	info, err := os.Stat(filepath.Join(root, "fresh.txt"))
	if err != nil {
		t.Fatalf("stat new file: %v", err)
	}
	if got, want := info.Mode().Perm(), reference.Mode().Perm(); got != want {
		t.Errorf("new-file mode = %v, want %v (same as an ordinary create with that perm)", got, want)
	}
	assertNoStagingLeftovers(t, root)
}

// TestSafeWriteFile_ReplacesContentWholesale: the new content fully replaces the old, with
// no tail of the longer previous file surviving — the failure mode a truncate-in-place
// write can produce and a rename cannot.
func TestSafeWriteFile_ReplacesContentWholesale(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := SafeWriteFile(root, "notes.txt", []byte(strings.Repeat("long original content\n", 50)), 0o644); err != nil {
		t.Fatalf("SafeWriteFile first: %v", err)
	}
	if err := SafeWriteFile(root, "notes.txt", []byte("short"), 0o644); err != nil {
		t.Fatalf("SafeWriteFile second: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(root, "notes.txt"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "short" {
		t.Errorf("content = %q, want %q", got, "short")
	}
	assertNoStagingLeftovers(t, root)
}

// TestSafeWriteFile_FailedRenameRemovesStagingFile: when the rename cannot land — here the
// target name is an existing DIRECTORY — the call must fail and clean up after itself,
// leaving no staging file for the next listing (or the next tool) to trip over.
func TestSafeWriteFile_FailedRenameRemovesStagingFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "occupied"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := SafeWriteFile(root, "occupied", []byte("data"), 0o644); err == nil {
		t.Fatal("SafeWriteFile over an existing directory succeeded, want an error")
	}
	assertNoStagingLeftovers(t, root)
}

// TestSafeWriteFile_EscapeStagesNothingOutsideRoot: the staging file is part of the write,
// so it is subject to the same fence. Neither a traversal nor a component symlinked
// outside the root may leave anything — target or staging file — beyond the workspace.
func TestSafeWriteFile_EscapeStagesNothingOutsideRoot(t *testing.T) {
	t.Parallel()

	t.Run("traversal", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		if err := SafeWriteFile(root, "../escape.txt", []byte("x"), 0o644); !errors.Is(err, ErrPathEscape) {
			t.Fatalf("traversal write err = %v, want ErrPathEscape", err)
		}
		assertNoStagingLeftovers(t, filepath.Dir(root))
	})

	t.Run("symlinked component", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(root, "build")); err != nil {
			t.Skipf("symlinks unsupported: %v", err)
		}

		if err := SafeWriteFile(root, "build/artifact.txt", []byte("x"), 0o644); !errors.Is(err, ErrPathEscape) {
			t.Fatalf("write through escaping symlink err = %v, want ErrPathEscape", err)
		}
		if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
			t.Fatalf("outside dir entries = %v (err %v), want none", entries, err)
		}
		assertNoStagingLeftovers(t, root)
	})
}

// TestSafeWriteFile_ReplacesInRootSymlinkName is the one deliberate semantic change of the
// atomic write: because the last step is a rename, a target NAME that is an in-root
// symlink is REPLACED by a regular file rather than written through to its link target.
// (A symlink pointing OUTSIDE the root stays refused — see
// TestSafeWriteFile_RefusesFinalSymlinkToOutside.)
func TestSafeWriteFile_ReplacesInRootSymlinkName(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "real.txt"), []byte("original"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink("real.txt", filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	if err := SafeWriteFile(root, "link.txt", []byte("replacement"), 0o644); err != nil {
		t.Fatalf("SafeWriteFile over in-root symlink: %v", err)
	}

	info, err := os.Lstat(filepath.Join(root, "link.txt"))
	if err != nil {
		t.Fatalf("lstat link.txt: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("link.txt is still a symlink, want a regular file (replace-the-name contract)")
	}
	got, err := os.ReadFile(filepath.Join(root, "link.txt"))
	if err != nil {
		t.Fatalf("read link.txt: %v", err)
	}
	if string(got) != "replacement" {
		t.Errorf("link.txt content = %q, want %q", got, "replacement")
	}
	through, err := os.ReadFile(filepath.Join(root, "real.txt"))
	if err != nil {
		t.Fatalf("read real.txt: %v", err)
	}
	if string(through) != "original" {
		t.Errorf("real.txt content = %q, want %q (the write must not go through the link)", through, "original")
	}
	assertNoStagingLeftovers(t, root)
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

// TestSafeCopyFile_CopiesContentAndSourceMode is the copy primitive's positive control and its
// one departure from SafeWriteFile's mode rule: the destination takes the SOURCE's mode even
// when it already existed with another, because copying a 0755 script that lands 0644 is a
// broken copy. Parents are created inside the fence and no staging file survives.
func TestSafeCopyFile_CopiesContentAndSourceMode(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := filepath.Join(root, "run.sh")
	if err := os.WriteFile(src, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Chmod(src, 0o755); err != nil {
		t.Fatalf("setup chmod: %v", err)
	}

	if err := SafeCopyFile(root, "run.sh", "bin/run.sh"); err != nil {
		t.Fatalf("SafeCopyFile: %v", err)
	}

	dst := filepath.Join(root, "bin", "run.sh")
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("destination missing: %v", err)
	}
	if string(got) != "#!/bin/sh\n" {
		t.Errorf("destination content = %q, want the source's", got)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat destination: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o755 {
		t.Errorf("destination mode = %v, want the source's 0755", perm)
	}
	assertNoStagingLeftovers(t, root)
}

// TestSafeCopyFile_RefusesEscapes drives the fence from both ends — a source outside the root may
// not be read out of it, a destination outside it may not be written — and proves the refusal is
// total: nothing lands inside the workspace, nothing (not even a staging file) outside it.
func TestSafeCopyFile_RefusesEscapes(t *testing.T) {
	t.Parallel()

	outside := t.TempDir()
	root := filepath.Join(outside, "workspace")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("classified"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "inside.txt"), []byte("ours"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := SafeCopyFile(root, "../secret.txt", "stolen.txt"); !errors.Is(err, ErrPathEscape) {
		t.Errorf("escaping source error = %v, want ErrPathEscape", err)
	}
	if err := SafeCopyFile(root, "inside.txt", "../leaked.txt"); !errors.Is(err, ErrPathEscape) {
		t.Errorf("escaping destination error = %v, want ErrPathEscape", err)
	}
	if _, err := os.Stat(filepath.Join(root, "stolen.txt")); err == nil {
		t.Error("a refused copy wrote the outside file into the workspace")
	}
	if _, err := os.Stat(filepath.Join(outside, "leaked.txt")); err == nil {
		t.Error("a refused copy wrote outside the workspace")
	}
	assertNoStagingLeftovers(t, outside)
}

// TestSafeCopyFile_RefusesNonRegularSource: the primitive copies FILES. A directory source is
// refused before anything is staged, rather than producing whatever io.Copy makes of a directory
// descriptor on the running platform.
func TestSafeCopyFile_RefusesNonRegularSource(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	err := SafeCopyFile(root, "dir", "copy")
	if err == nil {
		t.Fatal("copying a directory must fail")
	}
	if !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("error = %v, want it to name the non-regular source", err)
	}
	assertNoStagingLeftovers(t, root)
}

// TestSafeRename_MovesWithinRootAndRefusesEscapes: the rename primitive fences BOTH names — the
// half a one-path fence would miss is renaming an in-workspace file out of the workspace — and
// creates the destination's parents the way SafeWriteFile creates its target's.
func TestSafeRename_MovesWithinRootAndRefusesEscapes(t *testing.T) {
	t.Parallel()

	outside := t.TempDir()
	root := filepath.Join(outside, "workspace")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := SafeRename(root, "a.txt", "sub/b.txt"); err != nil {
		t.Fatalf("SafeRename: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(root, "sub", "b.txt")); err != nil || string(got) != "payload" {
		t.Fatalf("destination = (%q, %v), want the moved payload", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); !os.IsNotExist(err) {
		t.Errorf("source survived the rename, stat error = %v", err)
	}

	if err := SafeRename(root, "sub/b.txt", "../escaped.txt"); !errors.Is(err, ErrPathEscape) {
		t.Errorf("escaping destination error = %v, want ErrPathEscape", err)
	}
	if err := SafeRename(root, "../nothing.txt", "pulled.txt"); !errors.Is(err, ErrPathEscape) {
		t.Errorf("escaping source error = %v, want ErrPathEscape", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "escaped.txt")); err == nil {
		t.Error("a refused rename moved a file out of the workspace")
	}
}

// TestSafeRemove_RemovesWithinRootAndRefusesEscapes: removal is the one operation whose mistakes
// cannot be undone, so its fence gets the same both-ends proof — an in-root name goes, an
// out-of-root name is refused with the file left standing.
func TestSafeRemove_RemovesWithinRootAndRefusesEscapes(t *testing.T) {
	t.Parallel()

	outside := t.TempDir()
	root := filepath.Join(outside, "workspace")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "gone.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "kept.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := SafeRemove(root, "gone.txt"); err != nil {
		t.Fatalf("SafeRemove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "gone.txt")); !os.IsNotExist(err) {
		t.Errorf("file survived removal, stat error = %v", err)
	}

	if err := SafeRemove(root, "../kept.txt"); !errors.Is(err, ErrPathEscape) {
		t.Errorf("escaping removal error = %v, want ErrPathEscape", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "kept.txt")); err != nil {
		t.Errorf("a refused removal deleted a file outside the workspace: %v", err)
	}

	if err := SafeRemove(root, "never-existed.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("removing a missing name = %v, want an os.ErrNotExist error", err)
	}
}
