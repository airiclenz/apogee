package agent

// The engine surface a Driver's `/undo` drives (ADR 0051): UndoPreview reads the journal's top
// group, UndoRevert executes exactly the step the human was shown, and the generation the preview
// carried is what proves those are the same step. These tests hold the journal at arm's length —
// they record into it directly rather than driving the loop (undo_group_test.go does that) — so
// what they pin is the SURFACE: the delegation, the stale-generation refusal, and the promise that
// a refusal leaves both the disk and the journal exactly as they were.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/airiclenz/apogee/internal/undo"
)

// journalledAgent builds an Agent and records one overwrite of a real file into its journal,
// returning the agent and the recorded file's path. The file on disk holds the post-image, so the
// recorded mutation is the one an undo can actually reverse.
func journalledAgent(t *testing.T, pre, post string) (*Agent, string) {
	t.Helper()

	// Symlink-resolved for the reason undo_group_test.go resolves it: the journal fences its
	// read-back and its restore against this root, and macOS reaches a temp dir through a symlink.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the temp root: %v", err)
	}
	target := filepath.Join(root, "note.txt")
	if err := os.WriteFile(target, []byte(post), 0o644); err != nil {
		t.Fatalf("seed the written file: %v", err)
	}

	cfg := configWithTools(&recordingSink{})
	cfg.WorkspaceDir = root
	a, err := newAgent(cfg, &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.journal.Record(undo.Mutation{
		Root:       root,
		Path:       target,
		Perm:       0o644,
		Pre:        []byte(pre),
		PreExisted: true,
		Post:       []byte(post),
		PostExists: true,
	})
	return a, target
}

// TestUndoPreviewAndRevertDelegateToTheJournal: the two methods are the journal's top group as the
// Driver sees it — the preview names the path and stamps the generation, and the revert quoting
// that stamp puts the pre-image back and pops the group.
func TestUndoPreviewAndRevertDelegateToTheJournal(t *testing.T) {
	a, target := journalledAgent(t, "before", "after")

	step, ok := a.UndoPreview()
	if !ok {
		t.Fatal("UndoPreview reported nothing to undo after a record")
	}
	if step.Ordinal != 1 {
		t.Errorf("previewed ordinal = %d, want 1 (one recorded group)", step.Ordinal)
	}
	if len(step.Changes) != 1 || step.Changes[0].Path != target {
		t.Fatalf("previewed changes = %+v, want one restore of %q", step.Changes, target)
	}
	if step.Changes[0].Action != undo.ActionRestore {
		t.Errorf("previewed action = %s, want restore (the file still holds what was written)", step.Changes[0].Action)
	}

	report, err := a.UndoRevert(step.Generation)
	if err != nil {
		t.Fatalf("UndoRevert at the previewed generation: %v", err)
	}
	if len(report.Restored) != 1 || report.Restored[0] != target {
		t.Fatalf("restored = %v, want exactly [%s]", report.Restored, target)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "before" {
		t.Fatalf("file after the revert = %q (err %v), want the pre-image %q", got, err, "before")
	}
	if _, ok := a.UndoPreview(); ok {
		t.Error("UndoPreview still reports a step after the only group was reverted")
	}
}

// TestUndoRevertRefusesAStaleGeneration: a journal that moved between the preview and the confirm
// means the human would be confirming a step they never read, so the revert refuses with the typed
// sentinel — and refuses INERTLY: the file is untouched and the group is still there to preview
// again, which is what lets a Driver re-preview and ask once more.
func TestUndoRevertRefusesAStaleGeneration(t *testing.T) {
	a, target := journalledAgent(t, "before", "after")

	step, ok := a.UndoPreview()
	if !ok {
		t.Fatal("UndoPreview reported nothing to undo after a record")
	}

	// What a write landing between the preview and the confirm does to the journal.
	a.journal.Record(undo.Mutation{
		Root:       a.cfg.WorkspaceDir,
		Path:       filepath.Join(filepath.Dir(target), "other.txt"),
		Post:       []byte("later"),
		PostExists: true,
	})

	_, err := a.UndoRevert(step.Generation)
	if !errors.Is(err, undo.ErrStaleGeneration) {
		t.Fatalf("UndoRevert at a stale generation = %v, want undo.ErrStaleGeneration", err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "after" {
		t.Fatalf("file after the refusal = %q (err %v), want the written %q untouched", got, err, "after")
	}
	fresh, ok := a.UndoPreview()
	if !ok {
		t.Fatal("the refusal consumed the group: UndoPreview reports nothing to undo")
	}
	if fresh.Generation == step.Generation {
		t.Error("the fresh preview carries the stale generation, so a re-confirm would refuse forever")
	}
	if _, err := a.UndoRevert(fresh.Generation); err != nil {
		t.Fatalf("UndoRevert at the FRESH generation: %v, want the re-confirm to go through", err)
	}
}

// TestUndoSurfaceToleratesNoJournal: a hand-built Agent has no journal, and both methods answer
// that plainly instead of panicking — the same nil tolerance the loop's BeginGroup site carries.
func TestUndoSurfaceToleratesNoJournal(t *testing.T) {
	var a Agent

	if _, ok := a.UndoPreview(); ok {
		t.Error("UndoPreview on a journal-less Agent reported a step")
	}
	if _, err := a.UndoRevert(0); !errors.Is(err, undo.ErrNothingToUndo) {
		t.Errorf("UndoRevert on a journal-less Agent = %v, want undo.ErrNothingToUndo", err)
	}
}
