package undo

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ----------------------------------------------------------------------------
// Fixture helpers — a snapshot-backed journal that keeps its index outside the
// workspace, exactly where the session's object store puts it.
// ----------------------------------------------------------------------------

// persistJournal returns a journal wired to a fake store over a fresh temp workspace, keeping
// its journal.json in a store directory beside it. The index is never inside the workspace: a
// capture would otherwise image it.
func persistJournal(t *testing.T) (journal *Journal, snap *fakeSnapshotter, root, path string) {
	t.Helper()

	root = t.TempDir()
	snap = newFakeSnapshotter(root)
	path = filepath.Join(t.TempDir(), "journal.json")
	journal = New(WithSnapshotter(snap), WithWorkspace(root), WithIndexPath(path))
	return journal, snap, root, path
}

// readIndex decodes the index file the journal wrote.
func readIndex(t *testing.T, path string) Index {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the index: %v", err)
	}
	var index Index
	if err := json.Unmarshal(data, &index); err != nil {
		t.Fatalf("decode the index: %v", err)
	}
	return index
}

// assertSameStep fails when two previews describe different steps.
func assertSameStep(t *testing.T, what string, got, want Step) {
	t.Helper()

	if got.Ordinal != want.Ordinal || got.Generation != want.Generation {
		t.Fatalf("%s: ordinal/generation %d/%d, want %d/%d",
			what, got.Ordinal, got.Generation, want.Ordinal, want.Generation)
	}
	if len(got.Changes) != len(want.Changes) {
		t.Fatalf("%s: %d changes, want %d: %+v", what, len(got.Changes), len(want.Changes), got.Changes)
	}
	for i, change := range got.Changes {
		if change.Path != want.Changes[i].Path || change.Action != want.Changes[i].Action {
			t.Fatalf("%s: change %d is %s %s, want %s %s",
				what, i, change.Action, change.Path, want.Changes[i].Action, want.Changes[i].Path)
		}
	}
}

// ----------------------------------------------------------------------------
// The round trip
// ----------------------------------------------------------------------------

func TestSaveLoad_RoundTrip_PreservesOrdinalsGenerationAndBothStacks(t *testing.T) {
	journal, snap, root, path := persistJournal(t)

	exchange(t, journal, func() { subprocessWrite(t, root, "one.txt", "first") })
	exchange(t, journal, func() { subprocessWrite(t, root, "two.txt", "second") })
	if _, err := journal.Revert(); err != nil {
		t.Fatalf("Revert: %v", err)
	}

	index := readIndex(t, path)
	if index.Version != indexVersion || index.Workspace != root {
		t.Fatalf("index header is version %d workspace %q, want %d %q",
			index.Version, index.Workspace, indexVersion, root)
	}
	if len(index.Groups) != 1 || len(index.Redo) != 1 {
		t.Fatalf("index holds %d groups and %d redo entries, want 1 and 1", len(index.Groups), len(index.Redo))
	}
	if index.Groups[0].Ordinal != 1 || index.Redo[0].Ordinal != 1 {
		t.Fatalf("ordinals are %d and %d, want 1 and 1", index.Groups[0].Ordinal, index.Redo[0].Ordinal)
	}
	if index.Groups[0].Generation == 0 || index.Groups[0].Pre == index.Groups[0].Post {
		t.Fatalf("the persisted group is %+v, want a stamped pair of differing trees", index.Groups[0])
	}
	if index.Generation != journal.Generation() {
		t.Fatalf("index generation %d, want the journal's %d", index.Generation, journal.Generation())
	}

	loaded, err := Load(path, snap, root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.Generation() != journal.Generation() {
		t.Fatalf("loaded generation %d, want %d", loaded.Generation(), journal.Generation())
	}
	wantUndo, ok := journal.Preview()
	if !ok {
		t.Fatal("the source journal previewed nothing to undo")
	}
	gotUndo, ok := loaded.Preview()
	if !ok {
		t.Fatal("the loaded journal previewed nothing to undo")
	}
	assertSameStep(t, "undo preview", gotUndo, wantUndo)

	wantRedo, ok := journal.RedoPreview()
	if !ok {
		t.Fatal("the source journal previewed nothing to redo")
	}
	gotRedo, ok := loaded.RedoPreview()
	if !ok {
		t.Fatal("the loaded journal previewed nothing to redo")
	}
	assertSameStep(t, "redo preview", gotRedo, wantRedo)
}

func TestSave_ExchangeWhoseTreesAreEqual_IsAbsentAndTakesNoOrdinal(t *testing.T) {
	journal, snap, root, path := persistJournal(t)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("before"), 0o644); err != nil {
		t.Fatalf("seed the escape target: %v", err)
	}

	exchange(t, journal, func() { subprocessWrite(t, root, "one.txt", "first") })
	exchange(t, journal, func() {
		if err := os.WriteFile(outside, []byte("after"), 0o644); err != nil {
			t.Fatalf("escape write: %v", err)
		}
		journal.Record(Mutation{
			Root: root, Path: outside, Permitted: outside,
			Pre: []byte("before"), PreExisted: true,
			Post: []byte("after"), PostExists: true,
		})
	})
	exchange(t, journal, func() { subprocessWrite(t, root, "two.txt", "second") })

	index := readIndex(t, path)
	if len(index.Groups) != 2 {
		t.Fatalf("index holds %d groups, want the two whose trees differ: %+v", len(index.Groups), index.Groups)
	}
	if index.Groups[0].Ordinal != 1 || index.Groups[1].Ordinal != 2 {
		t.Fatalf("ordinals are %d and %d, want 1 and 2", index.Groups[0].Ordinal, index.Groups[1].Ordinal)
	}

	loaded, err := Load(path, snap, root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	step, ok := loaded.Preview()
	if !ok {
		t.Fatal("the loaded journal previewed nothing to undo")
	}
	if step.Ordinal != 2 {
		t.Fatalf("the loaded top step is ordinal %d, want 2 — the escape exchange must not be a step", step.Ordinal)
	}
}

func TestLoad_ContinuesTheFile_SavingBackAndCountingOn(t *testing.T) {
	journal, snap, root, path := persistJournal(t)
	exchange(t, journal, func() { subprocessWrite(t, root, "one.txt", "first") })
	generationOnDisk := readIndex(t, path).Generation

	loaded, err := Load(path, snap, root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	exchange(t, loaded, func() { subprocessWrite(t, root, "two.txt", "second") })

	if loaded.Generation() <= generationOnDisk {
		t.Fatalf("the loaded journal is at generation %d, want it counting on from %d",
			loaded.Generation(), generationOnDisk)
	}
	index := readIndex(t, path)
	if len(index.Groups) != 2 {
		t.Fatalf("the loaded journal saved %d groups back, want 2: %+v", len(index.Groups), index.Groups)
	}
	if index.Groups[1].Ordinal != 2 || index.Generation != loaded.Generation() {
		t.Fatalf("saved back as ordinal %d at generation %d, want 2 at %d",
			index.Groups[1].Ordinal, index.Generation, loaded.Generation())
	}

	// The resumed session's write belongs to its own exchange, not to the loaded one.
	step, ok := loaded.Preview()
	if !ok {
		t.Fatal("the loaded journal previewed nothing to undo")
	}
	if len(step.Changes) != 1 || filepath.Base(step.Changes[0].Path) != "two.txt" {
		t.Fatalf("the top step is %+v, want two.txt alone", step.Changes)
	}
}

// ----------------------------------------------------------------------------
// Refusals
// ----------------------------------------------------------------------------

func TestLoad_MissingFile_IsAnEmptyJournalThatKeepsThePath(t *testing.T) {
	root := t.TempDir()
	snap := newFakeSnapshotter(root)
	path := filepath.Join(t.TempDir(), "journal.json")

	loaded, err := Load(path, snap, root)
	if err != nil {
		t.Fatalf("Load of a missing index: %v", err)
	}
	if _, ok := loaded.Preview(); ok {
		t.Fatal("a journal loaded from nothing previewed a step")
	}

	exchange(t, loaded, func() { subprocessWrite(t, root, "one.txt", "first") })
	if len(readIndex(t, path).Groups) != 1 {
		t.Fatal("the journal did not write the index path it was loaded from")
	}
}

func TestLoad_IndexOfAnotherWorkspace_IsRefusedWithErrWorkspaceMismatch(t *testing.T) {
	journal, snap, root, path := persistJournal(t)
	exchange(t, journal, func() { subprocessWrite(t, root, "one.txt", "first") })

	_, err := Load(path, snap, t.TempDir())
	if !errors.Is(err, ErrWorkspaceMismatch) {
		t.Fatalf("Load against another workspace returned %v, want ErrWorkspaceMismatch", err)
	}
}

func TestLoad_CorruptIndex_IsAnErrorNamingThePath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "journal.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("seed a corrupt index: %v", err)
	}

	_, err := Load(path, newFakeSnapshotter(root), root)
	if err == nil {
		t.Fatal("Load of a corrupt index succeeded")
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("the error does not name the file: %v", err)
	}
}

// ----------------------------------------------------------------------------
// The write itself
// ----------------------------------------------------------------------------

func TestSave_WritesTheIndexReadableByItsOwnerAlone(t *testing.T) {
	journal, _, root, path := persistJournal(t)
	exchange(t, journal, func() { subprocessWrite(t, root, "one.txt", "first") })

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the index: %v", err)
	}
	if info.Mode().Perm() != indexFilePerm {
		t.Fatalf("the index is mode %v, want %v", info.Mode().Perm(), indexFilePerm)
	}

	// Save writes the same index anywhere it is asked to.
	elsewhere := filepath.Join(t.TempDir(), "copy.json")
	if err := journal.Save(elsewhere); err != nil {
		t.Fatalf("Save: %v", err)
	}
	kept, copied := readIndex(t, path), readIndex(t, elsewhere)
	if len(copied.Groups) != len(kept.Groups) || copied.Generation != kept.Generation {
		t.Fatalf("Save wrote %+v, want the index the journal keeps: %+v", copied, kept)
	}
}

func TestLoad_RecordsWithNoSnapshotterToReadThem_IsRefused(t *testing.T) {
	journal, _, root, path := persistJournal(t)
	exchange(t, journal, func() { subprocessWrite(t, root, "one.txt", "first") })

	if _, err := Load(path, nil, root); err == nil {
		t.Fatal("Load materialised recorded exchanges with no snapshotter to read their images")
	}
}

func TestClose_SaveThatCannotComplete_IsReportedAndLeavesNoPartialFile(t *testing.T) {
	root := t.TempDir()
	snap := newFakeSnapshotter(root)
	store := t.TempDir()

	// An index path that is a non-empty directory: the temporary file is written and the
	// rename over it fails, which is the failure every partial-write worry reduces to.
	path := filepath.Join(store, "journal.json")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("seed the blocked index path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(path, "occupied"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed the blocked index path: %v", err)
	}

	journal := New(WithSnapshotter(snap), WithWorkspace(root), WithIndexPath(path))
	journal.BeginGroup()
	if err := journal.MarkPre(context.Background()); err != nil {
		t.Fatalf("MarkPre: %v", err)
	}
	subprocessWrite(t, root, "one.txt", "first")

	if err := journal.Close(context.Background()); err == nil {
		t.Fatal("Close reported no error where the index could not be written")
	}

	// The step still stands in memory, and nothing was left behind beside the index.
	if _, ok := journal.Preview(); !ok {
		t.Fatal("the failed save lost the in-memory step")
	}
	left, err := os.ReadDir(store)
	if err != nil {
		t.Fatalf("read the store directory: %v", err)
	}
	if len(left) != 1 || left[0].Name() != "journal.json" {
		t.Fatalf("the failed save left %d entries behind: %+v", len(left), left)
	}
}
