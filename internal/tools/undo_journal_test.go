package tools

// The tools' undo capture (ADR 0051), in two halves. Every content-writing verb reaches the
// filesystem through safeWriteFile, so the first half asks that ONE seam the three questions the
// journal's usefulness rests on: does each verb leave exactly one record for the path it
// touched, does that record hold the bytes that were there before (a revert puts them back),
// and does a write that never landed leave the journal untouched. The byte-moving verbs —
// copy_file, move_file, delete_file — do not reach that seam, so the second half asks the same
// questions of each of their own capture sites, plus the record SHAPES only they produce.

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/undo"
)

// runJournalledWrite executes one write tool under a context carrying journal, failing the
// test unless the call succeeded — a refused write journals nothing by design, so a case that
// meant to record something must not silently pass on a tool error.
func runJournalledWrite(t *testing.T, journal *undo.Journal, tool domain.Tool, args map[string]any) {
	t.Helper()

	result, err := tool.Execute(undo.WithJournal(context.Background(), journal), callWith(t, "c1", args))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}
}

// readOrAbsent returns the file's content, or ok=false when it does not exist.
func readOrAbsent(t *testing.T, path string) (string, bool) {
	t.Helper()

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false
	}
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data), true
}

// TestWriteFunnelJournalsEveryContentVerb walks the four tools that write content through
// safeWriteFile. Each records ONE change for its path; the previewed action being restore or
// delete (never skip) is the post-hash agreeing with what is on disk; and the revert putting
// the file back exactly as it was is the pre-image being the real one.
func TestWriteFunnelJournalsEveryContentVerb(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		file       string
		before     string // empty means the file does not exist before the write
		exists     bool
		newTool    func(root string) domain.Tool
		args       func(file string) map[string]any
		wantAction undo.Action
		wantAfter  string
	}{
		{
			name:    "write_file creating a file",
			file:    "made.txt",
			newTool: func(root string) domain.Tool { return NewWriteFile(root) },
			args: func(file string) map[string]any {
				return map[string]any{"path": file, "content": "fresh"}
			},
			wantAction: undo.ActionDelete,
			wantAfter:  "fresh",
		},
		{
			name:    "write_file overwriting a file",
			file:    "kept.txt",
			before:  "original",
			exists:  true,
			newTool: func(root string) domain.Tool { return NewWriteFile(root) },
			args: func(file string) map[string]any {
				return map[string]any{"path": file, "content": "replaced"}
			},
			wantAction: undo.ActionRestore,
			wantAfter:  "replaced",
		},
		{
			name:    "edit_existing_file",
			file:    "edited.txt",
			before:  "original",
			exists:  true,
			newTool: func(root string) domain.Tool { return NewEditExistingFile(root) },
			args: func(file string) map[string]any {
				return map[string]any{"path": file, "content": "edited"}
			},
			wantAction: undo.ActionRestore,
			wantAfter:  "edited",
		},
		{
			name:    "single_find_and_replace",
			file:    "single.txt",
			before:  "keep OLD keep",
			exists:  true,
			newTool: func(root string) domain.Tool { return NewSingleFindReplace(root) },
			args: func(file string) map[string]any {
				return map[string]any{"path": file, "oldText": "OLD", "newText": "NEW"}
			},
			wantAction: undo.ActionRestore,
			wantAfter:  "keep NEW keep",
		},
		{
			name:    "multi_find_and_replace",
			file:    "multi.txt",
			before:  "A and B",
			exists:  true,
			newTool: func(root string) domain.Tool { return NewMultiFindReplace(root) },
			args: func(file string) map[string]any {
				return map[string]any{"path": file, "replacements": []any{
					map[string]any{"oldText": "A", "newText": "X"},
					map[string]any{"oldText": "B", "newText": "Y"},
				}}
			},
			wantAction: undo.ActionRestore,
			wantAfter:  "X and Y",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := tempRoot(t)
			path := filepath.Join(root, tc.file)
			if tc.exists {
				writeFixtureFile(t, path, tc.before)
			}

			journal := undo.New()
			runJournalledWrite(t, journal, tc.newTool(root), tc.args(tc.file))

			if got, _ := readOrAbsent(t, path); got != tc.wantAfter {
				t.Fatalf("file after the write = %q, want %q", got, tc.wantAfter)
			}

			step, ok := journal.Preview()
			if !ok {
				t.Fatal("the write left no undo step")
			}
			if len(step.Changes) != 1 {
				t.Fatalf("journal holds %d changes, want exactly 1: %+v", len(step.Changes), step.Changes)
			}
			change := step.Changes[0]
			if change.Path != path {
				t.Errorf("recorded path = %q, want %q", change.Path, path)
			}
			if change.Action != tc.wantAction {
				t.Fatalf("previewed action = %v (%s), want %v", change.Action, change.Reason, tc.wantAction)
			}

			report, err := journal.Revert()
			if err != nil {
				t.Fatalf("Revert: %v", err)
			}
			if len(report.Skipped) != 0 {
				t.Fatalf("revert skipped %+v, want nothing skipped", report.Skipped)
			}

			got, exists := readOrAbsent(t, path)
			if exists != tc.exists {
				t.Fatalf("file exists after undo = %v, want %v", exists, tc.exists)
			}
			if got != tc.before {
				t.Errorf("file after undo = %q, want the pre-image %q", got, tc.before)
			}
		})
	}
}

// TestWriteFunnelKeepsOneRecordPerPath pins the merge the journal performs on the funnel's
// behalf: two writes to the same file inside one group are one undo step back to the state
// before the FIRST of them, not two steps that peel off one write each.
func TestWriteFunnelKeepsOneRecordPerPath(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	path := filepath.Join(root, "twice.txt")
	writeFixtureFile(t, path, "original")

	journal := undo.New()
	tool := NewWriteFile(root)
	runJournalledWrite(t, journal, tool, map[string]any{"path": "twice.txt", "content": "first"})
	runJournalledWrite(t, journal, tool, map[string]any{"path": "twice.txt", "content": "second"})

	step, ok := journal.Preview()
	if !ok {
		t.Fatal("the writes left no undo step")
	}
	if len(step.Changes) != 1 {
		t.Fatalf("journal holds %d changes, want exactly 1: %+v", len(step.Changes), step.Changes)
	}
	if step.Changes[0].Action != undo.ActionRestore {
		t.Fatalf("previewed action = %v (%s), want restore", step.Changes[0].Action, step.Changes[0].Reason)
	}

	if _, err := journal.Revert(); err != nil {
		t.Fatalf("Revert: %v", err)
	}
	if got, _ := readOrAbsent(t, path); got != "original" {
		t.Errorf("file after undo = %q, want the pre-image of the FIRST write %q", got, "original")
	}
}

// TestWriteFunnelJournalsNothingWhenTheWriteFails is the other half of the ordering contract:
// the record is committed only after the mutation succeeded, so a refused write leaves the
// journal exactly as empty as it found it — generation included, since a stale generation
// would invalidate a preview no write had actually invalidated.
func TestWriteFunnelJournalsNothingWhenTheWriteFails(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	journal := undo.New()

	result, err := NewWriteFile(root).Execute(undo.WithJournal(context.Background(), journal),
		callWith(t, "c1", map[string]any{"path": "../escaped.txt", "content": "nope"}))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("the escaping write was allowed: %q", result.Content)
	}

	if step, ok := journal.Preview(); ok {
		t.Fatalf("a refused write left an undo step: %+v", step.Changes)
	}
	if got := journal.Generation(); got != 0 {
		t.Errorf("generation = %d after a refused write, want 0", got)
	}
}

// TestWriteFunnelWritesWithoutAJournal is the floor: with nothing recording, the funnel
// behaves exactly as it did before the journal existed.
func TestWriteFunnelWritesWithoutAJournal(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	if undo.FromContext(context.Background()) != nil {
		t.Fatal("a bare context reports a journal")
	}

	result, err := NewWriteFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "plain.txt", "content": "written"}))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}
	if got, _ := readOrAbsent(t, filepath.Join(root, "plain.txt")); got != "written" {
		t.Errorf("file content = %q, want %q", got, "written")
	}
}

// ----------------------------------------------------------------------------
// The byte-moving verbs (item 3)
// ----------------------------------------------------------------------------
//
// copy_file, move_file and delete_file never reach safeWriteFile, so each captures its own
// pre-image at its own mutation site and each has a record SHAPE the funnel's tests cannot
// pin: a copy journals one end, a move journals two, and a delete journals the bytes that
// stop existing. These tests read those shapes off the preview and then prove them by
// reverting — the only check that cares whether the recorded bytes are the real ones.

// journalledChanges runs one tool under a fresh journal and returns the preview's changes,
// failing the test unless the call succeeded and the journal recorded something.
func journalledChanges(t *testing.T, tool domain.Tool, args map[string]any) (*undo.Journal, []undo.Change) {
	t.Helper()

	journal := undo.New()
	runJournalledWrite(t, journal, tool, args)

	step, ok := journal.Preview()
	if !ok {
		t.Fatal("the mutation left no undo step")
	}
	return journal, step.Changes
}

// journalledEscape is journalledChanges for an APPROVED out-of-workspace call (ADR 0049): the
// permit rides on the same context as the journal, because a record that must carry a permit can
// only be written by a call that actually ran under one.
func journalledEscape(t *testing.T, target string, tool domain.Tool, args map[string]any) (*undo.Journal, []undo.Change) {
	t.Helper()

	journal := undo.New()
	result := runWrite(t, undo.WithJournal(escapePermit(target), journal), tool, args)
	if result.IsError {
		t.Fatalf("the approved escape was refused: %q", result.Content)
	}

	step, ok := journal.Preview()
	if !ok {
		t.Fatal("the mutation left no undo step")
	}
	return journal, step.Changes
}

// lockDirectory makes dir readable and traversable but not writable, so an unlink INSIDE it is
// refused while every other half of the move still works. The permissions are put back before the
// temp tree is torn down, since a read-only directory would defeat that cleanup too.
func lockDirectory(t *testing.T, dir string) {
	t.Helper()

	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

// assertChange fails unless the change at index i names path and plans action.
func assertChange(t *testing.T, changes []undo.Change, i int, path string, action undo.Action) {
	t.Helper()

	if i >= len(changes) {
		t.Fatalf("journal holds %d changes, want at least %d: %+v", len(changes), i+1, changes)
	}
	if changes[i].Path != path {
		t.Errorf("change %d path = %q, want %q", i, changes[i].Path, path)
	}
	if changes[i].Action != action {
		t.Errorf("change %d action = %v (%s), want %v", i, changes[i].Action, changes[i].Reason, action)
	}
}

// revertCleanly reverts the top group and fails on any skip — a skip here would mean the
// recorded post-state disagreed with the file the tool actually left.
func revertCleanly(t *testing.T, journal *undo.Journal) undo.Report {
	t.Helper()

	report, err := journal.Revert()
	if err != nil {
		t.Fatalf("Revert: %v", err)
	}
	if len(report.Skipped) != 0 {
		t.Fatalf("revert skipped %+v, want nothing skipped", report.Skipped)
	}
	return report
}

// TestCopyFileJournalsTheDestinationOnly: a copy mutates one file, so it records one — the
// destination — and the undo of a copy that CREATED its destination removes it again while the
// undo of one that clobbered puts the clobbered bytes back. Either way the source is untouched
// and unrecorded, which is what "a copy's source is a read" means to the journal.
func TestCopyFileJournalsTheDestinationOnly(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		destBefore string // empty means the destination does not exist yet
		destExists bool
		wantAction undo.Action
		overwrite  bool
	}{
		{name: "creating the destination", wantAction: undo.ActionDelete},
		{
			name:       "clobbering the destination",
			destBefore: "displaced",
			destExists: true,
			overwrite:  true,
			wantAction: undo.ActionRestore,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := tempRoot(t)
			source := filepath.Join(root, "src.txt")
			destination := filepath.Join(root, "dst.txt")
			writeFixtureFile(t, source, "payload")
			if tc.destExists {
				writeFixtureFile(t, destination, tc.destBefore)
			}

			journal, changes := journalledChanges(t, NewCopyFile(root, nil), map[string]any{
				"source": "src.txt", "destination": "dst.txt", "overwrite": tc.overwrite,
			})
			if len(changes) != 1 {
				t.Fatalf("a copy recorded %d changes, want the destination alone: %+v", len(changes), changes)
			}
			assertChange(t, changes, 0, destination, tc.wantAction)

			revertCleanly(t, journal)

			got, exists := readOrAbsent(t, destination)
			if exists != tc.destExists {
				t.Fatalf("destination exists after undo = %v, want %v", exists, tc.destExists)
			}
			if got != tc.destBefore {
				t.Errorf("destination after undo = %q, want the pre-image %q", got, tc.destBefore)
			}
			if got, _ := readOrAbsent(t, source); got != "payload" {
				t.Errorf("source after undo = %q, want it untouched", got)
			}
		})
	}
}

// TestMoveFileJournalsBothEnds is the plan's move round-trip: a move changes two files, so it
// records two, and undoing it restores the source and removes the destination. The record order
// is the order the writes happened — source first — so the preview reads like the move did.
func TestMoveFileJournalsBothEnds(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	source := filepath.Join(root, "from.txt")
	destination := filepath.Join(root, "to.txt")
	writeFixtureFile(t, source, "moved bytes")

	journal, changes := journalledChanges(t, NewMoveFile(root), map[string]any{
		"source": "from.txt", "destination": "to.txt",
	})
	if len(changes) != 2 {
		t.Fatalf("a move recorded %d changes, want both ends: %+v", len(changes), changes)
	}
	assertChange(t, changes, 0, source, undo.ActionRestore)
	assertChange(t, changes, 1, destination, undo.ActionDelete)

	revertCleanly(t, journal)

	if got, exists := readOrAbsent(t, source); !exists || got != "moved bytes" {
		t.Errorf("source after undo = %q (exists %v), want the moved bytes back", got, exists)
	}
	if _, exists := readOrAbsent(t, destination); exists {
		t.Error("destination still exists after undo, want the move's own file removed")
	}
}

// TestMoveFileClobberingJournalsThePreImage: a move onto an occupied destination replaces
// bytes that were there first, so the destination's record carries THEM — the undo has to put
// two files back, not one.
func TestMoveFileClobberingJournalsThePreImage(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	source := filepath.Join(root, "from.txt")
	destination := filepath.Join(root, "to.txt")
	writeFixtureFile(t, source, "moved bytes")
	writeFixtureFile(t, destination, "displaced bytes")

	journal, changes := journalledChanges(t, NewMoveFile(root), map[string]any{
		"source": "from.txt", "destination": "to.txt", "overwrite": true,
	})
	if len(changes) != 2 {
		t.Fatalf("a move recorded %d changes, want both ends: %+v", len(changes), changes)
	}
	assertChange(t, changes, 0, source, undo.ActionRestore)
	assertChange(t, changes, 1, destination, undo.ActionRestore)

	revertCleanly(t, journal)

	if got, _ := readOrAbsent(t, source); got != "moved bytes" {
		t.Errorf("source after undo = %q, want the moved bytes back", got)
	}
	if got, _ := readOrAbsent(t, destination); got != "displaced bytes" {
		t.Errorf("destination after undo = %q, want the displaced bytes back", got)
	}
}

// TestMoveFileFallbackJournalsBothEndsLikeTheRename drives the route an approved escape
// (ADR 0049) makes the real one. One rename is one syscall through one pinned root, so it can
// never span the workspace fence and a permitted target outside it; the copy-then-remove pair is
// the only way that move can happen. The pair is a different pair of syscalls, not a different
// contract: it must leave the SAME two records the rename leaves — the source with the pre-image
// bytes and no post-image, the destination the move created — and the destination's record must
// carry the permit, or the undo could not reach back out to take it away.
func TestMoveFileFallbackJournalsBothEndsLikeTheRename(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	source := filepath.Join(root, "from.txt")
	destination := filepath.Join(tempRoot(t), "landed.txt")
	writeFixtureFile(t, source, "moved bytes")

	journal, changes := journalledEscape(t, destination, NewMoveFile(root), map[string]any{
		"source": "from.txt", "destination": destination,
	})
	if len(changes) != 2 {
		t.Fatalf("the fallback recorded %d changes, want both ends: %+v", len(changes), changes)
	}
	assertChange(t, changes, 0, source, undo.ActionRestore)
	assertChange(t, changes, 1, destination, undo.ActionDelete)

	revertCleanly(t, journal)

	if got, exists := readOrAbsent(t, source); !exists || got != "moved bytes" {
		t.Errorf("source after undo = %q (exists %v), want the moved bytes back", got, exists)
	}
	if _, exists := readOrAbsent(t, destination); exists {
		t.Error("destination still exists after undo, want the fallback's own file removed")
	}
}

// TestMoveFileFallbackSplitFailureJournalsTheDestinationAlone pins the fallback's own failure
// mode, the one the rename cannot have: its two halves can land apart. With the copy landed and
// the removal refused — here by a source directory the process may read but not write — the file
// really is at both ends, so the journal records the destination ALONE. A source record here
// would promise to put back a file that never left, and the undo would then write over the
// source it was meant to rescue.
func TestMoveFileFallbackSplitFailureJournalsTheDestinationAlone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a read-only directory does not refuse an unlink on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores a directory's missing write bit")
	}
	t.Parallel()

	root := tempRoot(t)
	locked := filepath.Join(root, "locked")
	source := filepath.Join(locked, "from.txt")
	destination := filepath.Join(tempRoot(t), "landed.txt")
	writeFixtureFile(t, source, "moved bytes")
	lockDirectory(t, locked)

	journal := undo.New()
	result := runWrite(t, undo.WithJournal(escapePermit(destination), journal), NewMoveFile(root),
		map[string]any{"source": "locked/from.txt", "destination": destination})

	if !result.IsError {
		t.Fatalf("a refused removal must be reported to the model, got %q", result.Content)
	}
	if !strings.Contains(result.Content, "could not remove the source") {
		t.Errorf("failure = %q, want it to name the half that did not happen", result.Content)
	}
	if got, _ := readOrAbsent(t, destination); got != "moved bytes" {
		t.Errorf("destination = %q, want the copy that did land", got)
	}
	if got, exists := readOrAbsent(t, source); !exists || got != "moved bytes" {
		t.Errorf("source = %q (exists %v), want it still in place", got, exists)
	}

	step, ok := journal.Preview()
	if !ok {
		t.Fatal("the half that landed left no undo step")
	}
	if len(step.Changes) != 1 {
		t.Fatalf("the split failure recorded %d changes, want the destination alone: %+v",
			len(step.Changes), step.Changes)
	}
	assertChange(t, step.Changes, 0, destination, undo.ActionDelete)

	revertCleanly(t, journal)

	if _, exists := readOrAbsent(t, destination); exists {
		t.Error("destination still exists after undo, want the copy that landed taken back")
	}
	if got, _ := readOrAbsent(t, source); got != "moved bytes" {
		t.Errorf("source after undo = %q, want it untouched throughout", got)
	}
}

// TestDeleteFileJournalsThePreImage: the journal's copy of a deleted file is the only one left,
// so this asserts the whole of it — the bytes AND the mode. A 0755 script restored 0644 is a
// broken restore, which is the same failure the family already refuses to make on a copy.
func TestDeleteFileJournalsThePreImage(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	path := filepath.Join(root, "script.sh")
	writeFixtureFile(t, path, "#!/bin/sh\necho hi\n")
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	journal, changes := journalledChanges(t, NewDeleteFile(root), map[string]any{"path": "script.sh"})
	if len(changes) != 1 {
		t.Fatalf("a delete recorded %d changes, want exactly 1: %+v", len(changes), changes)
	}
	assertChange(t, changes, 0, path, undo.ActionRestore)
	if _, exists := readOrAbsent(t, path); exists {
		t.Fatal("the file survived delete_file")
	}

	revertCleanly(t, journal)

	got, exists := readOrAbsent(t, path)
	if !exists {
		t.Fatal("the deleted file was not restored")
	}
	if got != "#!/bin/sh\necho hi\n" {
		t.Errorf("restored content = %q, want the pre-image", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the restored file: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o755 {
		t.Errorf("restored mode = %v, want the mode the file carried (0755)", info.Mode().Perm())
	}
}

// TestFileOpsJournalNothingWhenRefused closes the ordering contract for the three verbs the
// funnel's own test cannot reach: a refusal is not a mutation, so it leaves the journal — and
// its generation, which a pending preview is validated against — exactly as it found it.
func TestFileOpsJournalNothingWhenRefused(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		newTool func(root string) domain.Tool
		args    map[string]any
	}{
		{
			name:    "copy_file onto an occupied destination",
			newTool: func(root string) domain.Tool { return NewCopyFile(root, nil) },
			args:    map[string]any{"source": "src.txt", "destination": "taken.txt"},
		},
		{
			name:    "move_file onto an occupied destination",
			newTool: func(root string) domain.Tool { return NewMoveFile(root) },
			args:    map[string]any{"source": "src.txt", "destination": "taken.txt"},
		},
		{
			name:    "delete_file on a path outside the workspace",
			newTool: func(root string) domain.Tool { return NewDeleteFile(root) },
			args:    map[string]any{"path": "../escaped.txt"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := tempRoot(t)
			writeFixtureFile(t, filepath.Join(root, "src.txt"), "payload")
			writeFixtureFile(t, filepath.Join(root, "taken.txt"), "occupied")

			journal := undo.New()
			result, err := tc.newTool(root).Execute(
				undo.WithJournal(context.Background(), journal), callWith(t, "c1", tc.args))
			if err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}
			if !result.IsError {
				t.Fatalf("the refusal did not happen: %q", result.Content)
			}
			if step, ok := journal.Preview(); ok {
				t.Fatalf("a refused mutation left an undo step: %+v", step.Changes)
			}
			if got := journal.Generation(); got != 0 {
				t.Errorf("generation = %d after a refused mutation, want 0", got)
			}
		})
	}
}
