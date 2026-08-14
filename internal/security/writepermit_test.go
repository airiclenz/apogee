package security

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// permitFor renders the permit an approval would carry for target: the RESOLVED absolute path,
// exactly as dispatch classifies it (security.EvalRealPath over the root-joined argument), so the
// tests pin the same string the approval pane discloses rather than a hand-built one. On macOS the
// difference is real — a t.TempDir() lives under a symlinked /var — and a test that skipped this
// would pass for the wrong reason.
func permitFor(t *testing.T, target string) string {
	t.Helper()

	return EvalRealPath(target)
}

// TestSafeWriteFile_PermittedTargetWritesOutsideTheWorkspace is ADR 0049's whole promise at the
// fence: the approved out-of-workspace write actually writes, to exactly the path the approval
// disclosed and to nothing else. The table walks the four answers the permit can give — the
// target as it stands, the target whose parents do not exist yet, a target the permit does not
// name, and the same write with no permit at all, which must refuse exactly as it always did.
func TestSafeWriteFile_PermittedTargetWritesOutsideTheWorkspace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		target    string // path under the outside directory the write names
		permit    string // path under the outside directory the permit authorises ("" = no permit)
		wantWrite bool
	}{
		{name: "an approved target is written", target: "notes.md", permit: "notes.md", wantWrite: true},
		{name: "an approved target's missing parents are created", target: "deep/nested/notes.md", permit: "deep/nested/notes.md", wantWrite: true},
		{name: "a target the permit does not name is refused", target: "notes.md", permit: "elsewhere.md"},
		{name: "no permit refuses as it always did", target: "notes.md"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			outside := t.TempDir()
			target := filepath.Join(outside, tc.target)
			permit := ""
			if tc.permit != "" {
				permit = permitFor(t, filepath.Join(outside, tc.permit))
			}

			err := SafeWriteFile(root, target, []byte("approved"), 0o644, permit)

			if !tc.wantWrite {
				if !errors.Is(err, ErrPathEscape) {
					t.Fatalf("SafeWriteFile err = %v, want ErrPathEscape", err)
				}
				if _, statErr := os.Stat(target); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("refused write still landed at %s (stat err = %v)", target, statErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("SafeWriteFile on the approved target: %v", err)
			}
			data, readErr := os.ReadFile(target)
			if readErr != nil {
				t.Fatalf("read back the approved target: %v", readErr)
			}
			if string(data) != "approved" {
				t.Fatalf("approved target holds %q, want %q", data, "approved")
			}
		})
	}
}

// TestSafeWriteFile_PermitRefusesADivergedTarget covers the two ways the disclosed path and the
// filesystem can part company after the human said yes. A DANGLING symlink at the target name is
// the case resolution cannot see through — the permit and the argument agree, and the write must
// still be refused, because the bytes would land on a name the pane never showed. A resolving
// symlink is refused one step earlier, as a mismatch, and the file it points at must be untouched:
// that is the redirect an attacker would want.
func TestSafeWriteFile_PermitRefusesADivergedTarget(t *testing.T) {
	t.Parallel()

	t.Run("a dangling symlink at the approved name is refused", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		outside := t.TempDir()
		link := filepath.Join(outside, "notes.md")
		if err := os.Symlink(filepath.Join(outside, "absent.md"), link); err != nil {
			t.Skipf("symlinks unsupported: %v", err)
		}

		err := SafeWriteFile(root, link, []byte("approved"), 0o644, permitFor(t, link))

		if !errors.Is(err, ErrPathEscape) {
			t.Fatalf("SafeWriteFile onto a symlinked target err = %v, want ErrPathEscape", err)
		}
		info, statErr := os.Lstat(link)
		if statErr != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("the symlink was replaced: lstat mode = %v, err = %v", info.Mode(), statErr)
		}
	})

	t.Run("a symlink that resolves elsewhere is refused as a mismatch", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		outside := t.TempDir()
		elsewhere := filepath.Join(outside, "elsewhere.md")
		if err := os.WriteFile(elsewhere, []byte("untouched"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
		link := filepath.Join(outside, "notes.md")
		if err := os.Symlink(elsewhere, link); err != nil {
			t.Skipf("symlinks unsupported: %v", err)
		}

		// The permit names the link the operator approved; resolution follows it to elsewhere.md.
		err := SafeWriteFile(root, link, []byte("approved"), 0o644, filepath.Clean(link))

		if !errors.Is(err, ErrPathEscape) {
			t.Fatalf("SafeWriteFile through a redirecting symlink err = %v, want ErrPathEscape", err)
		}
		data, readErr := os.ReadFile(elsewhere)
		if readErr != nil || string(data) != "untouched" {
			t.Fatalf("the link's target was written: %q (err = %v)", data, readErr)
		}
	})

	t.Run("an approved target under a regular file is refused", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		outside := t.TempDir()
		blocker := filepath.Join(outside, "notes.md")
		if err := os.WriteFile(blocker, []byte("a file, not a directory"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
		target := filepath.Join(blocker, "child.md")

		err := SafeWriteFile(root, target, []byte("approved"), 0o644, permitFor(t, target))

		if !errors.Is(err, ErrPathEscape) {
			t.Fatalf("SafeWriteFile under a regular file err = %v, want ErrPathEscape", err)
		}
	})
}

// TestSafeWriteFile_PermitLeavesWorkspaceWritesUnchanged pins the never-worse floor from the other
// side: a permit is an EXCEPTION for one path, never a mode. An in-workspace write still lands in
// the workspace while a permit rides along, and — the case that matters — a workspace path that
// escapes through a symlink is still refused, because the fence answers the in-workspace question
// first and the permit is never consulted for a path the workspace root already contains.
func TestSafeWriteFile_PermitLeavesWorkspaceWritesUnchanged(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	approved := filepath.Join(outside, "approved.md")
	permit := permitFor(t, approved)

	if err := SafeWriteFile(root, "notes.md", []byte("in workspace"), 0o644, permit); err != nil {
		t.Fatalf("in-workspace write with a permit present: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "notes.md"))
	if err != nil || string(data) != "in workspace" {
		t.Fatalf("in-workspace write landed wrong: %q (err = %v)", data, err)
	}
	if _, statErr := os.Stat(approved); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("the permitted target was written by an in-workspace call (stat err = %v)", statErr)
	}

	// A workspace-named path that leaves the fence through a symlink stays refused, permit or not:
	// the permit authorises the disclosed path, not the argument that points at it.
	if err := os.Symlink(outside, filepath.Join(root, "hop")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	err = SafeWriteFile(root, filepath.Join("hop", "approved.md"), []byte("smuggled"), 0o644, permit)
	if !errors.Is(err, ErrPathEscape) {
		t.Fatalf("write through a workspace symlink err = %v, want ErrPathEscape", err)
	}
	if _, statErr := os.Stat(approved); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("the symlinked hop reached the permitted target (stat err = %v)", statErr)
	}
}

// TestPermittedTarget_NeverWidensARead states the asymmetry ADR 0049 is careful about: an approval
// to WRITE one path is not permission to READ the host. The read primitives take no permit and
// none can be handed to them, so the very file the permit just authorised is still outside the
// read fence.
func TestPermittedTarget_NeverWidensARead(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "approved.md")
	permit := permitFor(t, target)

	if err := SafeWriteFile(root, target, []byte("approved"), 0o644, permit); err != nil {
		t.Fatalf("SafeWriteFile on the approved target: %v", err)
	}

	if _, err := SafeReadFile(root, target); !errors.Is(err, ErrPathEscape) {
		t.Fatalf("SafeReadFile of the permitted target err = %v, want ErrPathEscape", err)
	}
	if _, err := SafeOpen(root, target); !errors.Is(err, ErrPathEscape) {
		t.Fatalf("SafeOpen of the permitted target err = %v, want ErrPathEscape", err)
	}
}

// TestSafeRemove_PermittedTarget covers the most destructive member of the approved family. A
// delete is included on purpose (ADR 0049 §5): its Gate discloses the same resolved path a write's
// does, so it honours the same permit — and refuses just as narrowly, leaving a neighbour the
// permit does not name exactly where it was.
func TestSafeRemove_PermittedTarget(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	doomed := filepath.Join(outside, "doomed.md")
	kept := filepath.Join(outside, "kept.md")
	for _, path := range []string{doomed, kept} {
		if err := os.WriteFile(path, []byte("content"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	permit := permitFor(t, doomed)

	if err := SafeRemove(root, kept, permit); !errors.Is(err, ErrPathEscape) {
		t.Fatalf("SafeRemove of an unapproved neighbour err = %v, want ErrPathEscape", err)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("the unapproved neighbour was removed: %v", err)
	}

	if err := SafeRemove(root, doomed, permit); err != nil {
		t.Fatalf("SafeRemove of the approved target: %v", err)
	}
	if _, err := os.Stat(doomed); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the approved target survived its delete (stat err = %v)", err)
	}
}

// TestSafeCopyFile_PermittedDestination pins the copy half of the family: the permit bounds the
// DESTINATION, which is the write, and leaves the source where it was — a read the workspace fence
// still owns. The last case is the one an approval must never buy: an approved out-of-workspace
// destination does not make an out-of-workspace SOURCE readable.
func TestSafeCopyFile_PermittedDestination(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "src.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	approved := filepath.Join(outside, "copied", "src.txt")
	permit := permitFor(t, approved)

	if err := SafeCopyFile(root, "src.txt", filepath.Join(outside, "other.txt"), permit); !errors.Is(err, ErrPathEscape) {
		t.Fatalf("copy to an unapproved destination err = %v, want ErrPathEscape", err)
	}

	if err := SafeCopyFile(root, "src.txt", approved, permit); err != nil {
		t.Fatalf("copy to the approved destination: %v", err)
	}
	data, err := os.ReadFile(approved)
	if err != nil || string(data) != "payload" {
		t.Fatalf("approved destination holds %q (err = %v)", data, err)
	}

	outsideSource := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideSource, []byte("PRIVATE"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	err = SafeCopyFile(root, outsideSource, "stolen.txt", permitFor(t, outsideSource))
	if !errors.Is(err, ErrPathEscape) {
		t.Fatalf("copy FROM a permitted path err = %v, want ErrPathEscape", err)
	}
}
