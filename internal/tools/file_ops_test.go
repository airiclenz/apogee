package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

// writeFixture writes one workspace fixture file with an explicit mode, failing the test rather
// than the tool. The chmod is separate because os.WriteFile's create is subject to the process
// umask, and these tests are about modes surviving a copy.
func writeFixture(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("setup %s: %v", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("setup chmod %s: %v", path, err)
	}
}

// runFileOp executes one file-operation call and fails on a Go error, which these tools reserve
// for ctx cancellation — every expected failure is an IsError result the model reads.
func runFileOp(t *testing.T, tool domain.Tool, args map[string]any) domain.ToolResult {
	t.Helper()

	result, err := tool.Execute(context.Background(), callWith(t, "c1", args))
	if err != nil {
		t.Fatalf("%s returned a Go error: %v", tool.Name(), err)
	}
	return result
}

// TestCopyFile_CopiesContentAndModeCreatingParents is the positive control for the whole tool:
// the destination gets the source's bytes AND the source's mode (the point of copying a 0755
// script is to end up with a 0755 script), missing parent directories are created as write_file
// creates them, and the source survives — that last part being the only difference from a move.
func TestCopyFile_CopiesContentAndModeCreatingParents(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "run.sh"), "#!/bin/sh\necho hi\n", 0o755)

	result := runFileOp(t, NewCopyFile(root, nil), map[string]any{
		"source": "run.sh", "destination": "bin/nested/run.sh",
	})
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}

	copied := filepath.Join(root, "bin", "nested", "run.sh")
	got, err := os.ReadFile(copied)
	if err != nil {
		t.Fatalf("destination was not created: %v", err)
	}
	if string(got) != "#!/bin/sh\necho hi\n" {
		t.Errorf("destination content = %q, want the source's", string(got))
	}
	info, err := os.Stat(copied)
	if err != nil {
		t.Fatalf("stat destination: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o755 {
		t.Errorf("destination mode = %v, want the source's 0755", perm)
	}
	if _, err := os.Stat(filepath.Join(root, "run.sh")); err != nil {
		t.Errorf("copy must leave the source in place: %v", err)
	}
}

// TestCopyFile_LeavesNoStagingFile: the copy stages its bytes beside the destination and renames
// them over it, so nothing of that mechanism may outlive a successful call — a stray
// .apogee-tmp-* in the workspace is a file the model would then see in a listing.
func TestCopyFile_LeavesNoStagingFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "a.txt"), "one", 0o644)

	if result := runFileOp(t, NewCopyFile(root, nil), map[string]any{
		"source": "a.txt", "destination": "b.txt",
	}); result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read workspace: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".apogee-tmp-") {
			t.Errorf("staging file %q survived a successful copy", entry.Name())
		}
	}
}

// TestCopyFile_RefusesOccupiedDestinationUnlessOverwrite pins ratified call 7 on both sides: an
// existing destination is a refusal that names the conflict and changes nothing, and the same
// call with overwrite:true replaces it. A tool that silently clobbered would make an
// unrecoverable mistake out of a typo'd destination.
func TestCopyFile_RefusesOccupiedDestinationUnlessOverwrite(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "src.txt"), "fresh", 0o644)
	writeFixture(t, filepath.Join(root, "dst.txt"), "existing", 0o644)

	result := runFileOp(t, NewCopyFile(root, nil), map[string]any{
		"source": "src.txt", "destination": "dst.txt",
	})
	if !result.IsError {
		t.Fatal("copying onto an existing destination must be refused without overwrite")
	}
	if !strings.Contains(result.Content, "dst.txt") {
		t.Errorf("the refusal must name the conflict: %q", result.Content)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "dst.txt")); string(got) != "existing" {
		t.Errorf("a refused copy must change nothing, destination now = %q", string(got))
	}

	if result := runFileOp(t, NewCopyFile(root, nil), map[string]any{
		"source": "src.txt", "destination": "dst.txt", "overwrite": true,
	}); result.IsError {
		t.Fatalf("overwrite:true must force the copy: %q", result.Content)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "dst.txt")); string(got) != "fresh" {
		t.Errorf("destination = %q, want the source's %q", string(got), "fresh")
	}
}

// TestCopyFile_RefusesDirectories: these tools are FILE operations. A directory source is refused
// (a recursive copy is a different tool with a different blast radius), and so is a directory
// destination — `cp foo bar/` habits would otherwise land a file named after the directory.
func TestCopyFile_RefusesDirectories(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "into"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	writeFixture(t, filepath.Join(root, "f.txt"), "x", 0o644)

	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "directory source",
			args: map[string]any{"source": "dir", "destination": "copy"},
			want: "not a file",
		},
		{
			name: "directory destination",
			args: map[string]any{"source": "f.txt", "destination": "into", "overwrite": true},
			want: "destination is a directory",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := runFileOp(t, NewCopyFile(root, nil), tc.args)
			if !result.IsError {
				t.Fatalf("%s must be refused", tc.name)
			}
			if !strings.Contains(result.Content, tc.want) {
				t.Errorf("refusal = %q, want it to contain %q", result.Content, tc.want)
			}
		})
	}
}

// TestCopyFile_MissingSourceIsAnErrorResult: a source that is not there is the model's mistake to
// see and correct, so it is an IsError result naming the path, never a Go error (ADR 0007).
func TestCopyFile_MissingSourceIsAnErrorResult(t *testing.T) {
	t.Parallel()

	result := runFileOp(t, NewCopyFile(t.TempDir(), nil), map[string]any{
		"source": "nope.txt", "destination": "b.txt",
	})
	if !result.IsError {
		t.Fatal("a missing source must be an error result")
	}
	if !strings.Contains(result.Content, "nope.txt") {
		t.Errorf("the refusal must name the missing source: %q", result.Content)
	}
}

// TestCopyFile_RefusesEscapes drives the fence from BOTH ends: a source outside the workspace may
// not be read out of it, and a destination outside it may not be written to — each refused with
// the uniform escape wording rather than disguised as an absence, and with nothing created
// outside the root. A tool that fenced only its target would be an exfiltration primitive.
func TestCopyFile_RefusesEscapes(t *testing.T) {
	t.Parallel()

	outside := t.TempDir()
	root := filepath.Join(outside, "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	writeFixture(t, filepath.Join(outside, "secret.txt"), "classified", 0o600)
	writeFixture(t, filepath.Join(root, "inside.txt"), "ours", 0o644)

	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{name: "escaping source", args: map[string]any{"source": "../secret.txt", "destination": "stolen.txt"}},
		{name: "escaping destination", args: map[string]any{"source": "inside.txt", "destination": "../leaked.txt"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := runFileOp(t, NewCopyFile(root, nil), tc.args)
			if !result.IsError {
				t.Fatalf("%s must be refused", tc.name)
			}
			if !strings.Contains(result.Content, "outside the workspace") {
				t.Errorf("refusal = %q, want the uniform escape wording", result.Content)
			}
		})
	}

	if _, err := os.Stat(filepath.Join(root, "stolen.txt")); err == nil {
		t.Error("a refused copy must not have written the outside file into the workspace")
	}
	if _, err := os.Stat(filepath.Join(outside, "leaked.txt")); err == nil {
		t.Error("a refused copy must not have written anything outside the workspace")
	}
}

// extraRootFixture builds the shape every extra-read-root case below shares: a workspace, a
// configured read-only root holding one 0755 file, and a third directory under no root at all. It
// returns the three dirs and the absolute path of the file in the read-only root.
func extraRootFixture(t *testing.T) (root, extra, outside, mounted string) {
	t.Helper()

	root, extra, outside = t.TempDir(), t.TempDir(), t.TempDir()
	mounted = filepath.Join(extra, "skill", "run.sh")
	if err := os.MkdirAll(filepath.Dir(mounted), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	writeFixture(t, mounted, "#!/bin/sh\necho skill\n", 0o755)
	writeFixture(t, filepath.Join(outside, "id_rsa"), "classified", 0o600)
	return root, extra, outside, mounted
}

// TestCopyFile_CopiesFromAnExtraReadRoot is the positive control for the mount half: a skill file
// living under a configured read-only root copies INTO the workspace by absolute source path,
// arriving with the source's bytes and the source's mode, and the source itself is untouched —
// which is the whole point of widening the source and nothing else.
func TestCopyFile_CopiesFromAnExtraReadRoot(t *testing.T) {
	t.Parallel()

	root, extra, _, mounted := extraRootFixture(t)

	result := runFileOp(t, NewCopyFile(root, func() []string { return []string{extra} }), map[string]any{
		"source": mounted, "destination": "docs/run.sh",
	})
	if result.IsError {
		t.Fatalf("copying from the read-only root was refused: %q", result.Content)
	}

	copied := filepath.Join(root, "docs", "run.sh")
	got, err := os.ReadFile(copied)
	if err != nil {
		t.Fatalf("destination was not created: %v", err)
	}
	if string(got) != "#!/bin/sh\necho skill\n" {
		t.Errorf("destination content = %q, want the mounted source's", string(got))
	}
	info, err := os.Stat(copied)
	if err != nil {
		t.Fatalf("stat destination: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o755 {
		t.Errorf("destination mode = %v, want the source's 0755", perm)
	}
	if _, err := os.Stat(mounted); err != nil {
		t.Errorf("the copy must leave the mounted source in place: %v", err)
	}
}

// TestCopyFile_ExtraReadRootRefusals pins the three edges the widening must NOT move: a RELATIVE
// name still resolves against the workspace alone (no one name may mean two files), the read-only
// root takes no writes as a destination, and a path under no root at all is refused exactly as it
// was before extra roots existed — with the one uniform escape message.
func TestCopyFile_ExtraReadRootRefusals(t *testing.T) {
	t.Parallel()

	root, extra, outside, mounted := extraRootFixture(t)
	writeFixture(t, filepath.Join(root, "inside.txt"), "ours", 0o644)
	tool := NewCopyFile(root, func() []string { return []string{extra} })

	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "relative name of an extra-root file",
			args: map[string]any{"source": filepath.Join("skill", "run.sh"), "destination": "copied.sh"},
			want: "file not found",
		},
		{
			name: "destination under the extra root",
			args: map[string]any{"source": "inside.txt", "destination": filepath.Join(extra, "planted.txt")},
			want: ErrPathEscape.Error(),
		},
		{
			name: "source under no root",
			args: map[string]any{"source": filepath.Join(outside, "id_rsa"), "destination": "stolen.txt"},
			want: ErrPathEscape.Error(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := runFileOp(t, tool, tc.args)
			if !result.IsError {
				t.Fatalf("%s must be refused", tc.name)
			}
			if !strings.Contains(result.Content, tc.want) {
				t.Errorf("refusal = %q, want it to contain %q", result.Content, tc.want)
			}
		})
	}

	if _, err := os.Stat(filepath.Join(extra, "planted.txt")); err == nil {
		t.Error("the read-only root took a write from copy_file's destination")
	}
	if _, err := os.Stat(filepath.Join(root, "stolen.txt")); err == nil {
		t.Error("a file under no root was copied into the workspace")
	}
	if _, err := os.Stat(filepath.Join(root, "copied.sh")); err == nil {
		t.Error("a relative name resolved against the extra root, which it may never do")
	}
	if data, err := os.ReadFile(mounted); err != nil || string(data) != "#!/bin/sh\necho skill\n" {
		t.Errorf("the mounted file = %q (err %v), want it untouched", data, err)
	}
}

// TestCopyFile_WriteTargetStaysTheDestinationForAMountedSource keeps the blast-radius fence honest
// about the widening: the marker still classifies the DESTINATION, so a call reading from a
// read-only root is dispatched on the workspace path it writes — not on the root it read.
func TestCopyFile_WriteTargetStaysTheDestinationForAMountedSource(t *testing.T) {
	t.Parallel()

	root, extra, _, mounted := extraRootFixture(t)

	target, ok := NewCopyFile(root, func() []string { return []string{extra} }).workspaceWriteTarget(
		callWith(t, "c1", map[string]any{"source": mounted, "destination": "run.sh"}))

	if !ok {
		t.Fatal("workspaceWriteTarget did not classify a call whose source is under an extra root")
	}
	if want := realPath(t, filepath.Join(root, "run.sh")); target != want {
		t.Errorf("write target = %q, want the workspace destination %q", target, want)
	}
}

// TestMoveFile_RefusesAMountedSource is the read-ONLY half where it is easiest to get wrong: a move
// REMOVES its source, which is a write, so move_file never receives the extra roots and the very
// file copy_file may read is refused to it — with the uniform escape message, and still there
// afterwards.
func TestMoveFile_RefusesAMountedSource(t *testing.T) {
	t.Parallel()

	root, _, _, mounted := extraRootFixture(t)

	result := runFileOp(t, NewMoveFile(root), map[string]any{
		"source": mounted, "destination": "run.sh",
	})
	if !result.IsError {
		t.Fatalf("move_file must refuse a source under a read-only root: %q", result.Content)
	}
	if !strings.Contains(result.Content, ErrPathEscape.Error()) {
		t.Errorf("refusal = %q, want the uniform escape wording", result.Content)
	}
	if _, err := os.Stat(mounted); err != nil {
		t.Errorf("a refused move must leave the mounted source in place: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "run.sh")); err == nil {
		t.Error("a refused move must not have written the mounted file into the workspace")
	}
}

// TestMoveFile_MovesFileAndRemovesSource is the move's positive control and its one difference
// from a copy: the bytes and the mode arrive, the parents are created, and the SOURCE IS GONE.
func TestMoveFile_MovesFileAndRemovesSource(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "old.sh"), "#!/bin/sh\n", 0o755)

	result := runFileOp(t, NewMoveFile(root), map[string]any{
		"source": "old.sh", "destination": "scripts/new.sh",
	})
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}

	moved := filepath.Join(root, "scripts", "new.sh")
	got, err := os.ReadFile(moved)
	if err != nil {
		t.Fatalf("destination was not created: %v", err)
	}
	if string(got) != "#!/bin/sh\n" {
		t.Errorf("destination content = %q, want the source's", string(got))
	}
	info, err := os.Stat(moved)
	if err != nil {
		t.Fatalf("stat destination: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o755 {
		t.Errorf("destination mode = %v, want the source's 0755", perm)
	}
	if _, err := os.Stat(filepath.Join(root, "old.sh")); !os.IsNotExist(err) {
		t.Errorf("the source must be gone after a move, stat error = %v", err)
	}
}

// TestMoveFile_RefusesOccupiedDestinationUnlessOverwrite: the same ratified-call-7 refusal
// copy_file gives, pinned on move because the consequence is worse — a silent clobber here
// destroys the destination AND removes the source.
func TestMoveFile_RefusesOccupiedDestinationUnlessOverwrite(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "src.txt"), "fresh", 0o644)
	writeFixture(t, filepath.Join(root, "dst.txt"), "existing", 0o644)

	result := runFileOp(t, NewMoveFile(root), map[string]any{
		"source": "src.txt", "destination": "dst.txt",
	})
	if !result.IsError {
		t.Fatal("moving onto an existing destination must be refused without overwrite")
	}
	if _, err := os.Stat(filepath.Join(root, "src.txt")); err != nil {
		t.Errorf("a refused move must leave the source alone: %v", err)
	}

	if result := runFileOp(t, NewMoveFile(root), map[string]any{
		"source": "src.txt", "destination": "dst.txt", "overwrite": true,
	}); result.IsError {
		t.Fatalf("overwrite:true must force the move: %q", result.Content)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "dst.txt")); string(got) != "fresh" {
		t.Errorf("destination = %q, want the source's %q", string(got), "fresh")
	}
	if _, err := os.Stat(filepath.Join(root, "src.txt")); !os.IsNotExist(err) {
		t.Errorf("a forced move must still remove the source, stat error = %v", err)
	}
}

// TestMoveFile_RefusesEscapes is copy's both-ends fence test for the move: an outside source may
// not be dragged in, an outside destination may not be written — and in neither case is the
// source removed, because a refused move must be a no-op on both ends.
func TestMoveFile_RefusesEscapes(t *testing.T) {
	t.Parallel()

	outside := t.TempDir()
	root := filepath.Join(outside, "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	writeFixture(t, filepath.Join(outside, "secret.txt"), "classified", 0o600)
	writeFixture(t, filepath.Join(root, "inside.txt"), "ours", 0o644)

	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{name: "escaping source", args: map[string]any{"source": "../secret.txt", "destination": "stolen.txt"}},
		{name: "escaping destination", args: map[string]any{"source": "inside.txt", "destination": "../leaked.txt"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := runFileOp(t, NewMoveFile(root), tc.args)
			if !result.IsError {
				t.Fatalf("%s must be refused", tc.name)
			}
			if !strings.Contains(result.Content, "outside the workspace") {
				t.Errorf("refusal = %q, want the uniform escape wording", result.Content)
			}
		})
	}

	if _, err := os.Stat(filepath.Join(outside, "secret.txt")); err != nil {
		t.Errorf("a refused move must not have removed the outside source: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "inside.txt")); err != nil {
		t.Errorf("a refused move must leave the in-workspace source in place: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "leaked.txt")); err == nil {
		t.Error("a refused move must not have written anything outside the workspace")
	}
}

// TestDeleteFile_RemovesTheNamedFile is the tool's positive control: the named file is gone, and
// nothing else in the workspace is — a delete that took a neighbour with it is the mistake this
// tool must never make.
func TestDeleteFile_RemovesTheNamedFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	writeFixture(t, filepath.Join(root, "sub", "gone.txt"), "bye", 0o644)
	writeFixture(t, filepath.Join(root, "sub", "kept.txt"), "stay", 0o644)

	result := runFileOp(t, NewDeleteFile(root), map[string]any{"path": "sub/gone.txt"})
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}
	if !strings.Contains(result.Content, "sub/gone.txt") {
		t.Errorf("the result must name what it deleted: %q", result.Content)
	}
	if _, err := os.Stat(filepath.Join(root, "sub", "gone.txt")); !os.IsNotExist(err) {
		t.Errorf("the file must be gone after a delete, stat error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "sub", "kept.txt")); err != nil {
		t.Errorf("a delete must touch nothing but its target: %v", err)
	}
}

// TestDeleteFile_RefusesADirectory pins ratified call 8: this is a FILE tool. The refusal is not
// only wording — os.Remove would silently unlink an EMPTY directory, so the check is what keeps a
// model that confuses the two from erasing a package it thought was a file.
func TestDeleteFile_RefusesADirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	result := runFileOp(t, NewDeleteFile(root), map[string]any{"path": "empty"})
	if !result.IsError {
		t.Fatal("deleting a directory must be refused")
	}
	if !strings.Contains(result.Content, "not a file") {
		t.Errorf("refusal = %q, want it to say the target is not a file", result.Content)
	}
	if _, err := os.Stat(filepath.Join(root, "empty")); err != nil {
		t.Errorf("a refused delete must leave the directory in place: %v", err)
	}
}

// TestDeleteFile_MissingFileIsAnErrorResult: a path that is not there is the model's mistake to see
// and correct, so it is an IsError result naming the path, never a Go error (ADR 0007). The empty
// path is the same class of mistake and gets its own refusal rather than a fence error.
func TestDeleteFile_MissingFileIsAnErrorResult(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{name: "missing file", args: map[string]any{"path": "nope.txt"}, want: "nope.txt"},
		{name: "empty path", args: map[string]any{"path": ""}, want: "path is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := runFileOp(t, NewDeleteFile(root), tc.args)
			if !result.IsError {
				t.Fatalf("%s must be an error result", tc.name)
			}
			if !strings.Contains(result.Content, tc.want) {
				t.Errorf("refusal = %q, want it to contain %q", result.Content, tc.want)
			}
		})
	}
}

// TestDeleteFile_RefusesEscape is the fence test that matters most for a destructive tool: a path
// pointing out of the workspace is refused with the uniform escape wording — never disguised as an
// absence — and the outside file is still there afterwards.
func TestDeleteFile_RefusesEscape(t *testing.T) {
	t.Parallel()

	outside := t.TempDir()
	root := filepath.Join(outside, "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	writeFixture(t, filepath.Join(outside, "secret.txt"), "classified", 0o600)

	result := runFileOp(t, NewDeleteFile(root), map[string]any{"path": "../secret.txt"})
	if !result.IsError {
		t.Fatal("deleting outside the workspace must be refused")
	}
	if !strings.Contains(result.Content, "outside the workspace") {
		t.Errorf("refusal = %q, want the uniform escape wording", result.Content)
	}
	if _, err := os.Stat(filepath.Join(outside, "secret.txt")); err != nil {
		t.Errorf("a refused delete must not have removed the outside file: %v", err)
	}
}

// TestDeleteFile_DangerousActionClassification pins where delete_file sits in ADR 0012's guard, and
// it is a two-sided claim because both sides are load-bearing.
//
// It adds NO default rule: that ruleset is precision-over-recall (almost-never-legitimate AND
// catastrophic), and deleting one file inside the workspace is an ordinary refactoring step — the
// very near-miss the shipped rules are written not to fire on. A rule here would gate normal work
// and teach a small model that the guard is noise.
//
// What it DOES inherit is the shipped credential/persistence coverage, for free and structurally:
// delete_file's argument is `path`, which is not a payloadKey, so the guard reads a delete target
// as inspectable text and hard-refuses one under ~/.ssh in every mode. Reclassify `path` as payload
// and this fails — which is the point, since that would blind the guard for every write tool at
// once.
func TestDeleteFile_DangerousActionClassification(t *testing.T) {
	t.Parallel()

	guard := security.DefaultDangerousActionGuard()
	for _, tc := range []struct {
		name string
		path string
		want security.Tier
	}{
		{name: "an ordinary workspace file is normal work", path: "internal/tools/old.go", want: security.TierNone},
		{name: "a build artefact is normal work", path: "build/main", want: security.TierNone},
		{name: "an SSH key is hard-refused", path: "~/.ssh/id_rsa", want: security.TierHardRefuse},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The call names itself with the tool's OWN name and its OWN argument key, so the
			// text the guard inspects is the text a real delete_file call produces.
			call := callWith(t, "c1", map[string]any{"path": tc.path})
			call.Tool = deleteFileSpec.name

			decision := guard.Inspect(call)
			if decision.Tier != tc.want {
				t.Errorf("guard tier for delete_file %q = %v (rule %q), want %v",
					tc.path, decision.Tier, decision.RuleID, tc.want)
			}
		})
	}
}

// TestDeleteFile_IsRegistered: an unregistered tool is not a tool. It must be in the default set,
// it must gate (it writes, and destructively), and it must carry the workspaceScopedWriter marker
// that path-bounds rather than confines it (ADR 0012 D1) — the marker being what dispatch reads to
// classify the file it would remove.
func TestDeleteFile_IsRegistered(t *testing.T) {
	t.Parallel()

	tool, ok := NewDefaultRegistry(t.TempDir()).Lookup("delete_file")
	if !ok {
		t.Fatal("default registry is missing \"delete_file\"")
	}
	if domain.IsReadOnly(tool) {
		t.Error("delete_file writes; it must not declare itself read-only")
	}
	if !IsWorkspaceScopedWriter(tool) {
		t.Error("delete_file must carry the workspaceScopedWriter marker")
	}
}

// TestCopyFileAndMoveFile_AreRegistered: an unregistered tool is not a tool. Both must be in the
// default set, both must gate (they write), and both must carry the workspaceScopedWriter marker
// that path-bounds rather than confines them (ADR 0012 D1) — the marker being what dispatch reads
// to classify their destination.
func TestCopyFileAndMoveFile_AreRegistered(t *testing.T) {
	t.Parallel()

	registry := NewDefaultRegistry(t.TempDir())
	for _, name := range []string{"copy_file", "move_file"} {
		tool, ok := registry.Lookup(name)
		if !ok {
			t.Fatalf("default registry is missing %q", name)
		}
		if domain.IsReadOnly(tool) {
			t.Errorf("%q writes; it must not declare itself read-only", name)
		}
		if !IsWorkspaceScopedWriter(tool) {
			t.Errorf("%q must carry the workspaceScopedWriter marker", name)
		}
	}
}
