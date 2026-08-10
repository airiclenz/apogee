package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
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

	result := runFileOp(t, NewCopyFile(root), map[string]any{
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

	if result := runFileOp(t, NewCopyFile(root), map[string]any{
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

	result := runFileOp(t, NewCopyFile(root), map[string]any{
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

	if result := runFileOp(t, NewCopyFile(root), map[string]any{
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

			result := runFileOp(t, NewCopyFile(root), tc.args)
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

	result := runFileOp(t, NewCopyFile(t.TempDir()), map[string]any{
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

			result := runFileOp(t, NewCopyFile(root), tc.args)
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
