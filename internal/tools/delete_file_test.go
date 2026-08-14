package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

// TestDeleteFile_RemovesTheNamedFile is the tool's positive control: the named file is gone, and
// nothing else in the workspace is — a delete that took a neighbour with it is the mistake this
// tool must never make.
func TestDeleteFile_RemovesTheNamedFile(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
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

	root := tempRoot(t)
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

	root := tempRoot(t)
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

	outside := tempRoot(t)
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
	tool := NewDeleteFile(tempRoot(t))
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

			// The call names itself with the tool's OWN name and its OWN argument key, and the
			// guard sees the REAL tool, so the class it judges is the class dispatch hands it:
			// delete_file is write-capable, so the write-shaped rules stay in force for it.
			call := callWith(t, "c1", map[string]any{"path": tc.path})
			call.Tool = deleteFileSpec.name

			decision := guard.Inspect(call, tool)
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

	tool, ok := NewDefaultRegistry(tempRoot(t)).Lookup("delete_file")
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

// TestDeleteFile_DisclosesTheResolvedTarget covers the removal half. SafeRemove unlinks THE NAME,
// so the note here discloses more than the call touched — the file the link pointed at survives —
// which is the direction a security surface errs in: the operator asked to delete `docs/notes.md`
// and is told what that name stood for.
func TestDeleteFile_DisclosesTheResolvedTarget(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	config := symlinkedReadFixture(t, root, "docs", "notes.md")
	writeFixture(t, filepath.Join(root, "plain.txt"), "bytes\n", 0o644)

	tool := NewDeleteFile(root)
	redirected := runFileOp(t, tool, map[string]any{"path": "docs/notes.md"})
	if redirected.IsError {
		t.Fatalf("unexpected tool error: %q", redirected.Content)
	}
	if want := "deleted docs/notes.md → resolves to " + realPath(t, config); redirected.Content != want {
		t.Errorf("Content = %q, want %q", redirected.Content, want)
	}
	if got, err := os.ReadFile(config); err != nil || string(got) != gitConfigFixture {
		t.Errorf("redirect target = (%q, %v), want the link's target untouched", got, err)
	}
	if _, err := os.Lstat(filepath.Join(root, "docs", "notes.md")); !os.IsNotExist(err) {
		t.Errorf("the named link must be gone after a delete, lstat error = %v", err)
	}

	ordinary := runFileOp(t, tool, map[string]any{"path": "plain.txt"})
	if ordinary.IsError {
		t.Fatalf("unexpected tool error: %q", ordinary.Content)
	}
	if ordinary.Content != "deleted plain.txt" {
		t.Errorf("Content = %q, want the bare sentence for a path that resolves to itself", ordinary.Content)
	}
}
