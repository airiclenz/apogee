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

	root := tempRoot(t)
	writeFixture(t, filepath.Join(root, "run.sh"), "#!/bin/sh\necho hi\n", 0o755)

	result := runFileOp(t, NewCopyFile(root, ReadMounts{}), map[string]any{
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

	root := tempRoot(t)
	writeFixture(t, filepath.Join(root, "a.txt"), "one", 0o644)

	if result := runFileOp(t, NewCopyFile(root, ReadMounts{}), map[string]any{
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

	root := tempRoot(t)
	writeFixture(t, filepath.Join(root, "src.txt"), "fresh", 0o644)
	writeFixture(t, filepath.Join(root, "dst.txt"), "existing", 0o644)

	result := runFileOp(t, NewCopyFile(root, ReadMounts{}), map[string]any{
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

	if result := runFileOp(t, NewCopyFile(root, ReadMounts{}), map[string]any{
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

	root := tempRoot(t)
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

			result := runFileOp(t, NewCopyFile(root, ReadMounts{}), tc.args)
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

	result := runFileOp(t, NewCopyFile(tempRoot(t), ReadMounts{}), map[string]any{
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

	outside := tempRoot(t)
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

			result := runFileOp(t, NewCopyFile(root, ReadMounts{}), tc.args)
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

	root, extra, outside = tempRoot(t), tempRoot(t), tempRoot(t)
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

	result := runFileOp(t, NewCopyFile(root, ReadMounts{Roots: func() []string { return []string{extra} }}), map[string]any{
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

// TestCopyFile_CopiesFromAnExtraRootBySymlinkSpelling is copy_file's half of the F-13 fix (audit
// 2026-08-28): the mounted root stays its own real path and only the SPELLING the model was handed
// runs through a symlink — the shape a dotfiles-managed ~/.apogee/skills gives every path in a
// skill's `files:` line. The copy lands the mounted source's bytes and mode because the source
// PATH is now chosen with its root (readScope.locate) rather than handed on as the argument
// spelled it, which is what made the fence's lexical half refuse what its real half had accepted.
func TestCopyFile_CopiesFromAnExtraRootBySymlinkSpelling(t *testing.T) {
	t.Parallel()

	root, extra, _, mounted := extraRootFixture(t)
	link := filepath.Join(tempRoot(t), "lib")
	if err := os.Symlink(extra, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	result := runFileOp(t, NewCopyFile(root, ReadMounts{Roots: func() []string { return []string{extra} }}), map[string]any{
		"source": filepath.Join(link, "skill", "run.sh"), "destination": "prompts/x.md",
	})
	if result.IsError {
		t.Fatalf("copying an extra-root file by its symlink spelling was refused: %q", result.Content)
	}

	copied := filepath.Join(root, "prompts", "x.md")
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
	tool := NewCopyFile(root, ReadMounts{Roots: func() []string { return []string{extra} }})

	type refusalCase struct {
		name string
		args map[string]any
		want string
	}
	cases := []refusalCase{
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
	}
	// Accepting a symlink SPELLING widens nothing: a spelling that RESOLVES outside every root is
	// refused with the same uniform escape message its real spelling gets, one case up.
	elsewhere := filepath.Join(tempRoot(t), "elsewhere")
	if err := os.Symlink(outside, elsewhere); err != nil {
		t.Logf("symlinks unsupported, leaving the symlink-spelling case out: %v", err)
	} else {
		cases = append(cases, refusalCase{
			name: "symlink spelling of a source under no root",
			args: map[string]any{"source": filepath.Join(elsewhere, "id_rsa"), "destination": "linked.txt"},
			want: ErrPathEscape.Error(),
		})
	}

	for _, tc := range cases {
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

	target, ok := NewCopyFile(root, ReadMounts{Roots: func() []string { return []string{extra} }}).workspaceWriteTarget(
		callWith(t, "c1", map[string]any{"source": mounted, "destination": "run.sh"}))

	if !ok {
		t.Fatal("workspaceWriteTarget did not classify a call whose source is under an extra root")
	}
	if want := realPath(t, filepath.Join(root, "run.sh")); target.Real != want {
		t.Errorf("write target = %q, want the workspace destination %q", target.Real, want)
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

	root := tempRoot(t)
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

	root := tempRoot(t)
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

	outside := tempRoot(t)
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

	registry := NewDefaultRegistry(tempRoot(t))
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

// TestCopyFile_GuardJudgesOnlyTheDestination pins copy_file's ReadSourceTool declaration
// end-to-end against the shipped floor: the guard judges the write-shaped rules on the
// DESTINATION alone, because `source` is declared a read-only source path. The first case
// is the skill-materialization step every skill run performs — copying a resource OUT of
// the home skill library (an extra read root under ~/.apogee). The other two hold the
// floor where it belongs — as the Tier-2 forced look `~/.apogee` is (ADR 0049 §4): copy_file
// writing INTO the control plane, and move_file naming the same source — move_file deliberately makes NO ReadSourceTool declaration, because
// its source is deleted, a write by another name (the var block's missing assertion is
// that deliberateness; this test is its behavioural pin).
func TestCopyFile_GuardJudgesOnlyTheDestination(t *testing.T) {
	t.Parallel()

	guard := security.DefaultDangerousActionGuard()
	root := tempRoot(t)
	copier := NewCopyFile(root, ReadMounts{})
	mover := NewMoveFile(root)

	if got := domain.ReadSourceArgKeys(mover); got != nil {
		t.Fatalf("move_file declares read-source keys %v, want none — its source is a delete target", got)
	}

	materialize := callWith(t, "c1", map[string]any{
		"source":      "/root/.apogee/skills/security-audit/resources/methodology.md",
		"destination": "docs/skill-runs/resources/methodology.md",
	})
	materialize.Tool = copyFileSpec.name
	if d := guard.Inspect(materialize, copier, nil); d.Triggered() {
		t.Errorf("copy FROM the skill library triggered rule %q (tier %d), want no trigger", d.RuleID, d.Tier)
	}

	poison := callWith(t, "c2", map[string]any{
		"source":      "docs/x.md",
		"destination": "/root/.apogee/skills/evil/SKILL.md",
	})
	poison.Tool = copyFileSpec.name
	if d := guard.Inspect(poison, copier, nil); d.Tier != security.TierForceApproval {
		t.Errorf("copy INTO the control plane tier = %d, want TierForceApproval", d.Tier)
	}

	drain := callWith(t, "c3", map[string]any{
		"source":      "/root/.apogee/skills/security-audit/SKILL.md",
		"destination": "docs/x.md",
	})
	drain.Tool = moveFileSpec.name
	if d := guard.Inspect(drain, mover, nil); d.Tier != security.TierForceApproval {
		t.Errorf("move OUT of the control plane tier = %d, want TierForceApproval", d.Tier)
	}
}

// TestMoveFile_RefusesASymlinkedDestinationParent is the move's half of the mutated-chain policy,
// and the reason the refusal must be TERMINAL rather than retried: `docs → .git` redirects the
// destination into the control plane without leaving the workspace, and move_file's copy-then-
// remove fallback would answer that refusal by copying the file through the link and then failing
// to remove the source — a half-completed move that also landed the very write the policy refused.
// So the rename's own wording must reach the model, the source must still be there, and nothing
// may exist on the far side of the link. A clean chain in the same workspace still moves.
func TestMoveFile_RefusesASymlinkedDestinationParent(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	gitDir := filepath.Join(root, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(".git", filepath.Join(root, "docs")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	writeFixture(t, filepath.Join(root, "hook.sh"), "#!/bin/sh\n", 0o755)

	tool := NewMoveFile(root)
	result := runFileOp(t, tool, map[string]any{
		"source": "hook.sh", "destination": "docs/pre-commit",
	})
	if !result.IsError {
		t.Fatalf("move through a symlinked destination parent was allowed: %q", result.Content)
	}
	if !strings.Contains(result.Content, security.ErrSymlinkedParent.Error()) {
		t.Errorf("refusal %q does not carry the rename's symlinked-parent wording", result.Content)
	}
	if !strings.Contains(result.Content, "docs") {
		t.Errorf("refusal %q does not name the symlinked component", result.Content)
	}
	if strings.Contains(result.Content, "copied") {
		t.Errorf("refusal %q reports a copy — the fallback must not run for this refusal", result.Content)
	}
	if _, err := os.Stat(filepath.Join(root, "hook.sh")); err != nil {
		t.Errorf("a refused move removed its source: %v", err)
	}
	entries, err := os.ReadDir(gitDir)
	if err != nil || len(entries) != 0 {
		t.Errorf(".git = (%v, %v), want nothing created through the link", entries, err)
	}

	moved := runFileOp(t, tool, map[string]any{
		"source": "hook.sh", "destination": "scripts/pre-commit",
	})
	if moved.IsError {
		t.Fatalf("a cross-directory move through real directories was refused: %q", moved.Content)
	}
	if got, err := os.ReadFile(filepath.Join(root, "scripts", "pre-commit")); err != nil || string(got) != "#!/bin/sh\n" {
		t.Errorf("clean-chain move = (%q, %v), want the moved bytes", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, "hook.sh")); !os.IsNotExist(err) {
		t.Errorf("the source must be gone after a clean move, stat error = %v", err)
	}
}

// TestCopyFile_ExtraReadRootSourceFollowsSymlinksIntoTheWorkspace is the standing skills-access
// guard against the mutated-chain refusals: they gate chains a call WRITES, never one it reads, so
// a resource under a configured read-only root still copies into the workspace — including when
// the path reaches it THROUGH a symlinked directory, which is what a skills tree assembled from
// linked source dirs looks like. A refusal here would break skill materialisation outright.
func TestCopyFile_ExtraReadRootSourceFollowsSymlinksIntoTheWorkspace(t *testing.T) {
	t.Parallel()

	root, extra, _, mounted := extraRootFixture(t)
	if err := os.Symlink("skill", filepath.Join(extra, "linked")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	tool := NewCopyFile(root, ReadMounts{Roots: func() []string { return []string{extra} }})
	direct := runFileOp(t, tool, map[string]any{
		"source": mounted, "destination": "docs/run.sh",
	})
	if direct.IsError {
		t.Fatalf("copying from the read-only root was refused: %q", direct.Content)
	}

	linked := runFileOp(t, tool, map[string]any{
		"source": filepath.Join(extra, "linked", "run.sh"), "destination": "docs/linked.sh",
	})
	if linked.IsError {
		t.Fatalf("copying through a symlinked SOURCE parent was refused: %q", linked.Content)
	}
	if got, err := os.ReadFile(filepath.Join(root, "docs", "linked.sh")); err != nil || string(got) != "#!/bin/sh\necho skill\n" {
		t.Errorf("destination = (%q, %v), want the mounted source's bytes", got, err)
	}
}

// TestCopyFile_DisclosesTheResolvedDestination is this family's half of the result-string
// disclosure the other writers already make. A symlinked destination LEAF still reaches success —
// the mutated-chain refusals gate parents — and the rename replaces that name rather than writing
// through it, so the sentence the model reads and the transcript prints quoted an argument that
// pointed somewhere else entirely. The success sentence must name what the argument resolved to,
// while an ordinary destination keeps the bare sentence: a note that fired on every copy would
// disclose nothing.
func TestCopyFile_DisclosesTheResolvedDestination(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	config := symlinkedReadFixture(t, root, "docs", "notes.md")
	writeFixture(t, filepath.Join(root, "payload.txt"), "payload\n", 0o644)

	tool := NewCopyFile(root, ReadMounts{})
	redirected := runFileOp(t, tool, map[string]any{
		"source": "payload.txt", "destination": "docs/notes.md", "overwrite": true,
	})
	if redirected.IsError {
		t.Fatalf("unexpected tool error: %q", redirected.Content)
	}
	if want := "copied payload.txt to docs/notes.md → resolves to " + realPath(t, config); redirected.Content != want {
		t.Errorf("Content = %q, want %q", redirected.Content, want)
	}
	// The copy replaced the NAME rather than going through the link, so the redirect target is
	// still the file it was — the note is read before the copy for exactly that reason.
	if got, err := os.ReadFile(config); err != nil || string(got) != gitConfigFixture {
		t.Errorf("redirect target = (%q, %v), want it untouched", got, err)
	}

	ordinary := runFileOp(t, tool, map[string]any{
		"source": "payload.txt", "destination": "copy.txt",
	})
	if ordinary.IsError {
		t.Fatalf("unexpected tool error: %q", ordinary.Content)
	}
	if ordinary.Content != "copied payload.txt to copy.txt" {
		t.Errorf("Content = %q, want the bare sentence for a destination that resolves to itself", ordinary.Content)
	}
}

// TestMoveFile_DisclosesTheResolvedDestination is the move's half of the same disclosure: the
// rename lands on the destination NAME, so a symlinked leaf is replaced and the operator is
// otherwise told only the name the model wrote.
func TestMoveFile_DisclosesTheResolvedDestination(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	config := symlinkedReadFixture(t, root, "docs", "notes.md")
	writeFixture(t, filepath.Join(root, "payload.txt"), "payload\n", 0o644)

	tool := NewMoveFile(root)
	redirected := runFileOp(t, tool, map[string]any{
		"source": "payload.txt", "destination": "docs/notes.md", "overwrite": true,
	})
	if redirected.IsError {
		t.Fatalf("unexpected tool error: %q", redirected.Content)
	}
	if want := "moved payload.txt to docs/notes.md → resolves to " + realPath(t, config); redirected.Content != want {
		t.Errorf("Content = %q, want %q", redirected.Content, want)
	}
	if got, err := os.ReadFile(config); err != nil || string(got) != gitConfigFixture {
		t.Errorf("redirect target = (%q, %v), want it untouched", got, err)
	}

	writeFixture(t, filepath.Join(root, "payload.txt"), "payload\n", 0o644)
	ordinary := runFileOp(t, tool, map[string]any{
		"source": "payload.txt", "destination": "moved.txt",
	})
	if ordinary.IsError {
		t.Fatalf("unexpected tool error: %q", ordinary.Content)
	}
	if ordinary.Content != "moved payload.txt to moved.txt" {
		t.Errorf("Content = %q, want the bare sentence for a destination that resolves to itself", ordinary.Content)
	}
}

// gitWorkspace returns a temp git repository usable as a workspace ROOT. It is gitRepo's temp dir
// with its symlinks resolved: on hosts where the temp dir sits behind a link (macOS /var), an
// unresolved root makes every destination "resolve to" somewhere else and the disclosure note
// fires, which would drown out what these tests are actually reading — the staging note.
func gitWorkspace(t *testing.T) string {
	t.Helper()

	return realPath(t, gitRepo(t))
}

// TestMoveFile_StagesATrackedRename is the tool half of the git-mv contract: moving a file that
// git tracks leaves the rename in the INDEX, not just on disk, and says so in the result — the
// model must not have to discover the index change through a later git_status.
func TestMoveFile_StagesATrackedRename(t *testing.T) {
	root := gitWorkspace(t)

	result := runFileOp(t, NewMoveFile(root), map[string]any{
		"source": "README.md", "destination": "GUIDE.md",
	})
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}

	if want := "moved README.md to GUIDE.md (rename staged in git)"; result.Content != want {
		t.Errorf("Content = %q, want %q", result.Content, want)
	}
	if status := strings.TrimSpace(gitStatusPorcelain(t, root)); status != "R  README.md -> GUIDE.md" {
		t.Errorf("status = %q, want the staged rename alone", status)
	}
}

// TestMoveFile_UntrackedSourceIsUnchanged pins the no-surprise half: a file git never tracked is
// content the operator did not commit, so moving it must stage nothing AND read exactly as it did
// before staging existed — the note is the signal that something happened to the index.
func TestMoveFile_UntrackedSourceIsUnchanged(t *testing.T) {
	root := gitWorkspace(t)
	writeFixture(t, filepath.Join(root, "scratch.txt"), "draft\n", 0o644)

	result := runFileOp(t, NewMoveFile(root), map[string]any{
		"source": "scratch.txt", "destination": "notes.txt",
	})
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}

	if result.Content != "moved scratch.txt to notes.txt" {
		t.Errorf("Content = %q, want the bare sentence for an untracked source", result.Content)
	}
	if status := strings.TrimSpace(gitStatusPorcelain(t, root)); status != "?? notes.txt" {
		t.Errorf("status = %q, want the moved file still merely untracked", status)
	}
}

// TestMoveFile_OutsideARepositoryIsUnchanged covers the workspace that is no git worktree at all,
// which is the case the whole feature must be invisible in: same sentence, no note, no error.
func TestMoveFile_OutsideARepositoryIsUnchanged(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	writeFixture(t, filepath.Join(root, "old.txt"), "payload\n", 0o644)

	result := runFileOp(t, NewMoveFile(root), map[string]any{
		"source": "old.txt", "destination": "new.txt",
	})
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}

	if result.Content != "moved old.txt to new.txt" {
		t.Errorf("Content = %q, want the bare sentence outside a repository", result.Content)
	}
}

// TestMoveFile_OverwriteStagesBothPaths drives the pathspec PAIR. Overwriting a tracked
// destination changes two tracked paths at once — the source goes, the destination's bytes are
// replaced — and both halves must land in the index, or the next commit would carry a rename the
// operator never reviewed alongside a worktree change they thought they had staged. Git reports
// the pair as a deletion plus a modification rather than a rename: the destination is modified,
// not added, so there is no addition for rename detection to pair the deletion with.
func TestMoveFile_OverwriteStagesBothPaths(t *testing.T) {
	root := gitWorkspace(t)
	commitInRepo(t, root, "old.txt", "source bytes")
	commitInRepo(t, root, "dest.txt", "destination bytes")

	result := runFileOp(t, NewMoveFile(root), map[string]any{
		"source": "old.txt", "destination": "dest.txt", "overwrite": true,
	})
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}

	if want := "moved old.txt to dest.txt (rename staged in git)"; result.Content != want {
		t.Errorf("Content = %q, want %q", result.Content, want)
	}
	// An exact match is the assertion: any leftover for either path would show as a second
	// column, and any untouched third path as an extra record.
	if status := strings.TrimSpace(gitStatusPorcelain(t, root)); status != "M  dest.txt\nD  old.txt" {
		t.Errorf("status = %q, want both paths fully staged with no leftovers", status)
	}
}
