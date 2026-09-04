package undo

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/airiclenz/apogee/internal/security"
)

// ----------------------------------------------------------------------------
// Funnel stand-ins — the shape the write funnel will call Record with (item 2/3).
// Each captures the pre-image, mutates through the same fenced primitive the tools
// use, and records only after the mutation succeeded.
// ----------------------------------------------------------------------------

// funnelWrite creates or overwrites name under root and journals it.
func funnelWrite(t *testing.T, journal *Journal, root, name, content string) string {
	t.Helper()

	path := filepath.Join(root, name)
	pre, existed := preImage(t, path)

	if err := security.SafeWriteFile(root, path, []byte(content), 0o644, ""); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}

	journal.Record(Mutation{
		Root:       root,
		Path:       path,
		Pre:        pre,
		PreExisted: existed,
		Post:       []byte(content),
		PostExists: true,
	})
	return path
}

// funnelDelete removes name under root and journals it.
func funnelDelete(t *testing.T, journal *Journal, root, name string) string {
	t.Helper()

	path := filepath.Join(root, name)
	pre, existed := preImage(t, path)

	if err := security.SafeRemove(root, path, ""); err != nil {
		t.Fatalf("remove %s: %v", name, err)
	}

	journal.Record(Mutation{
		Root:       root,
		Path:       path,
		Pre:        pre,
		PreExisted: existed,
		PostExists: false,
	})
	return path
}

// funnelMove renames from to to under root and journals it as the two records the move
// verb produces: the source ends absent, the destination ends holding the moved bytes.
func funnelMove(t *testing.T, journal *Journal, root, from, to string) (string, string) {
	t.Helper()

	source := filepath.Join(root, from)
	destination := filepath.Join(root, to)
	sourcePre, sourceExisted := preImage(t, source)
	destinationPre, destinationExisted := preImage(t, destination)

	if err := security.SafeRename(root, source, destination); err != nil {
		t.Fatalf("move %s to %s: %v", from, to, err)
	}

	journal.Record(Mutation{
		Root:       root,
		Path:       source,
		Pre:        sourcePre,
		PreExisted: sourceExisted,
		PostExists: false,
	})
	journal.Record(Mutation{
		Root:       root,
		Path:       destination,
		Pre:        destinationPre,
		PreExisted: destinationExisted,
		Post:       sourcePre,
		PostExists: true,
	})
	return source, destination
}

// ----------------------------------------------------------------------------
// Fixture helpers
// ----------------------------------------------------------------------------

// preImage reads path's current bytes, reporting absence rather than failing on it.
func preImage(t *testing.T, path string) ([]byte, bool) {
	t.Helper()

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false
	}
	if err != nil {
		t.Fatalf("read pre-image of %s: %v", path, err)
	}
	return data, true
}

// seedFile writes a file that exists before the agent ever runs.
func seedFile(t *testing.T, root, name, content string) string {
	t.Helper()

	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("seed %s: %v", name, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("seed %s: %v", name, err)
	}
	return path
}

// assertContent fails unless path holds want.
func assertContent(t *testing.T, path, want string) {
	t.Helper()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Errorf("%s holds %q, want %q", filepath.Base(path), got, want)
	}
}

// assertAbsent fails unless path does not exist.
func assertAbsent(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s still exists (stat error: %v), want it gone", filepath.Base(path), err)
	}
}

// ----------------------------------------------------------------------------
// The four mutation shapes
// ----------------------------------------------------------------------------

func TestRevert_CreatedFile_IsDeleted(t *testing.T) {
	root := t.TempDir()
	journal := New()
	journal.BeginGroup()
	created := funnelWrite(t, journal, root, "new.txt", "fresh")

	report, err := journal.Revert()

	if err != nil {
		t.Fatalf("Revert: %v", err)
	}
	assertAbsent(t, created)
	if len(report.Deleted) != 1 || report.Deleted[0] != created {
		t.Errorf("Deleted = %v, want [%s]", report.Deleted, created)
	}
	if len(report.Restored) != 0 || len(report.Skipped) != 0 {
		t.Errorf("Restored = %v, Skipped = %v, want both empty", report.Restored, report.Skipped)
	}
}

func TestRevert_DeletedFile_RestoresTheBytes(t *testing.T) {
	root := t.TempDir()
	seedFile(t, root, "sub/gone.txt", "original bytes")
	journal := New()
	journal.BeginGroup()
	removed := funnelDelete(t, journal, root, "sub/gone.txt")

	report, err := journal.Revert()

	if err != nil {
		t.Fatalf("Revert: %v", err)
	}
	assertContent(t, removed, "original bytes")
	if len(report.Restored) != 1 || report.Restored[0] != removed {
		t.Errorf("Restored = %v, want [%s]", report.Restored, removed)
	}
}

func TestRevert_OverwrittenFile_RestoresThePreImage(t *testing.T) {
	root := t.TempDir()
	overwritten := seedFile(t, root, "notes.md", "before")
	journal := New()
	journal.BeginGroup()
	funnelWrite(t, journal, root, "notes.md", "after")

	if _, err := journal.Revert(); err != nil {
		t.Fatalf("Revert: %v", err)
	}

	assertContent(t, overwritten, "before")
}

func TestRevert_MoveRecordedAsTwoRecords_RoundTrips(t *testing.T) {
	root := t.TempDir()
	seedFile(t, root, "src.txt", "moving bytes")
	journal := New()
	journal.BeginGroup()
	source, destination := funnelMove(t, journal, root, "src.txt", "nested/dst.txt")

	report, err := journal.Revert()

	if err != nil {
		t.Fatalf("Revert: %v", err)
	}
	assertContent(t, source, "moving bytes")
	assertAbsent(t, destination)
	if len(report.Skipped) != 0 {
		t.Errorf("Skipped = %v, want none", report.Skipped)
	}
}

// ----------------------------------------------------------------------------
// Grouping, merging and the conflict rule
// ----------------------------------------------------------------------------

func TestRecord_SamePathTwiceInOneGroup_KeepsFirstPreImageAndLastPostState(t *testing.T) {
	root := t.TempDir()
	edited := seedFile(t, root, "doc.txt", "v0")
	journal := New()
	journal.BeginGroup()
	funnelWrite(t, journal, root, "doc.txt", "v1")
	funnelWrite(t, journal, root, "doc.txt", "v2")

	step, ok := journal.Preview()
	if !ok {
		t.Fatal("Preview reported nothing to undo")
	}
	if len(step.Changes) != 1 {
		t.Fatalf("Preview listed %d changes, want 1 entry per path", len(step.Changes))
	}
	if step.Changes[0].Action != ActionRestore {
		t.Errorf("action = %v, want restore (the last post-state must still match on disk)", step.Changes[0].Action)
	}

	if _, err := journal.Revert(); err != nil {
		t.Fatalf("Revert: %v", err)
	}

	assertContent(t, edited, "v0")
}

func TestRevert_FileHandEditedAfterTheAgentWroteIt_IsSkippedWhileSiblingsRestore(t *testing.T) {
	root := t.TempDir()
	handEdited := seedFile(t, root, "touched.txt", "agent-start")
	sibling := seedFile(t, root, "untouched.txt", "sibling-start")
	journal := New()
	journal.BeginGroup()
	funnelWrite(t, journal, root, "touched.txt", "agent-wrote")
	funnelWrite(t, journal, root, "untouched.txt", "agent-wrote-too")

	if err := os.WriteFile(handEdited, []byte("the human's own edit"), 0o644); err != nil {
		t.Fatalf("hand edit: %v", err)
	}
	report, err := journal.Revert()

	if err != nil {
		t.Fatalf("Revert: %v", err)
	}
	assertContent(t, handEdited, "the human's own edit")
	assertContent(t, sibling, "sibling-start")
	if len(report.Skipped) != 1 || report.Skipped[0].Path != handEdited {
		t.Fatalf("Skipped = %v, want just %s", report.Skipped, handEdited)
	}
	if report.Skipped[0].Reason == "" {
		t.Error("the skipped path carries no reason; the report has to say why it was left alone")
	}
	if len(report.Restored) != 1 || report.Restored[0] != sibling {
		t.Errorf("Restored = %v, want [%s]", report.Restored, sibling)
	}
}

func TestRevert_SameFileAcrossThreeExchanges_WalksBackOneExchangeAtATime(t *testing.T) {
	root := t.TempDir()
	journal := New()
	var walked string
	for _, content := range []string{"v1", "v2", "v3"} {
		journal.BeginGroup()
		walked = funnelWrite(t, journal, root, "walked.txt", content)
	}

	for _, want := range []string{"v2", "v1"} {
		report, err := journal.Revert()
		if err != nil {
			t.Fatalf("Revert to %s: %v", want, err)
		}
		if len(report.Skipped) != 0 {
			t.Fatalf("Revert to %s skipped %v; each pre-image must equal the previous exchange's post-state", want, report.Skipped)
		}
		assertContent(t, walked, want)
	}

	if _, err := journal.Revert(); err != nil {
		t.Fatalf("final Revert: %v", err)
	}
	assertAbsent(t, walked)

	if _, err := journal.Revert(); !errors.Is(err, ErrNothingToUndo) {
		t.Errorf("a fourth Revert returned %v, want ErrNothingToUndo", err)
	}
}

func TestBeginGroup_WithNoWritesAfterIt_AddsNoStep(t *testing.T) {
	root := t.TempDir()
	journal := New()
	journal.BeginGroup()
	funnelWrite(t, journal, root, "written.txt", "content")
	journal.BeginGroup()
	journal.BeginGroup()

	step, ok := journal.Preview()

	if !ok {
		t.Fatal("Preview reported nothing to undo, want the one group that has writes")
	}
	if step.Ordinal != 1 {
		t.Errorf("Ordinal = %d, want 1; an exchange that wrote nothing must not become a step", step.Ordinal)
	}
	if len(step.Changes) != 1 {
		t.Errorf("Preview listed %d changes, want 1", len(step.Changes))
	}
}

func TestPreview_EmptyJournal_ReportsNothingToUndo(t *testing.T) {
	journal := New()

	step, ok := journal.Preview()

	if ok {
		t.Errorf("Preview reported %+v, want nothing to undo", step)
	}
}

// ----------------------------------------------------------------------------
// The whole-run written-files account
// ----------------------------------------------------------------------------

func TestWrote_PathsAcrossTwoGroups_AreListedOnceInFirstWriteOrder(t *testing.T) {
	root := t.TempDir()
	journal := New()

	journal.BeginGroup()
	first := funnelWrite(t, journal, root, "first.txt", "a")
	second := funnelWrite(t, journal, root, "second.txt", "b")
	journal.BeginGroup()
	funnelWrite(t, journal, root, "second.txt", "b again")
	third := funnelWrite(t, journal, root, "third.txt", "c")

	wrote := journal.Wrote()

	want := []string{first, second, third}
	if len(wrote) != len(want) {
		t.Fatalf("Wrote listed %d paths (%v), want %d", len(wrote), wrote, len(want))
	}
	for i := range want {
		if wrote[i] != want[i] {
			t.Errorf("Wrote[%d] = %q, want %q; the order is first write per path", i, wrote[i], want[i])
		}
	}
}

func TestWrote_PathDeletedSinceItWasRecorded_IsStillReportedWithoutReadingIt(t *testing.T) {
	root := t.TempDir()
	journal := New()
	journal.BeginGroup()
	written := funnelWrite(t, journal, root, "gone.txt", "content")
	if err := os.Remove(written); err != nil {
		t.Fatalf("remove the file behind the record: %v", err)
	}

	wrote := journal.Wrote()

	if len(wrote) != 1 || wrote[0] != written {
		t.Errorf("Wrote = %v, want [%s]; the account reads no file, so a vanished path still counts", wrote, written)
	}
}

func TestWrote_EmptyJournal_ReportsNothing(t *testing.T) {
	journal := New()

	wrote := journal.Wrote()

	if len(wrote) != 0 {
		t.Errorf("Wrote = %v, want empty", wrote)
	}
}

// ----------------------------------------------------------------------------
// The staleness stamp and concurrency
// ----------------------------------------------------------------------------

func TestGeneration_RecordAndRevert_BothAdvanceTheStamp(t *testing.T) {
	root := t.TempDir()
	journal := New()

	atStart := journal.Generation()
	journal.BeginGroup()
	afterBoundary := journal.Generation()
	funnelWrite(t, journal, root, "one.txt", "a")
	afterFirstRecord := journal.Generation()
	funnelWrite(t, journal, root, "two.txt", "b")
	afterSecondRecord := journal.Generation()
	if _, err := journal.Revert(); err != nil {
		t.Fatalf("Revert: %v", err)
	}
	afterRevert := journal.Generation()

	if afterBoundary != atStart {
		t.Errorf("BeginGroup moved the stamp from %d to %d; a boundary alone changes nothing", atStart, afterBoundary)
	}
	if afterFirstRecord <= atStart {
		t.Errorf("first Record left the stamp at %d, want it advanced past %d", afterFirstRecord, atStart)
	}
	if afterSecondRecord <= afterFirstRecord {
		t.Errorf("second Record left the stamp at %d, want it advanced past %d", afterSecondRecord, afterFirstRecord)
	}
	if afterRevert <= afterSecondRecord {
		t.Errorf("Revert left the stamp at %d, want it advanced past %d", afterRevert, afterSecondRecord)
	}
}

func TestPreview_TopGroup_StampsTheCurrentGeneration(t *testing.T) {
	root := t.TempDir()
	journal := New()
	journal.BeginGroup()
	funnelWrite(t, journal, root, "stamped.txt", "content")

	step, ok := journal.Preview()

	if !ok {
		t.Fatal("Preview reported nothing to undo")
	}
	if step.Generation != journal.Generation() {
		t.Errorf("Step.Generation = %d, journal is at %d", step.Generation, journal.Generation())
	}
	if !filepath.IsAbs(step.Changes[0].Path) {
		t.Errorf("previewed path %q is not absolute; the preview is the disclosure surface", step.Changes[0].Path)
	}
}

func TestRecord_ConcurrentWritersAndReaders_IsRaceClean(t *testing.T) {
	const writers = 24

	root := t.TempDir()
	journal := New()
	journal.BeginGroup()

	paths := make([]string, writers)
	for i := range paths {
		paths[i] = seedFile(t, root, "concurrent-"+string(rune('a'+i))+".txt", "seed")
	}

	stop := make(chan struct{})
	var readers sync.WaitGroup
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
					journal.Preview()
					journal.Generation()
				}
			}
		}()
	}

	var writes sync.WaitGroup
	for _, path := range paths {
		writes.Add(1)
		go func(path string) {
			defer writes.Done()
			journal.Record(Mutation{
				Root:       root,
				Path:       path,
				Pre:        []byte("seed"),
				PreExisted: true,
				Post:       []byte("seed"),
				PostExists: true,
			})
		}(path)
	}
	writes.Wait()
	close(stop)
	readers.Wait()

	if got := journal.Generation(); got != writers {
		t.Errorf("Generation = %d after %d concurrent records, want %d", got, writers, writers)
	}
	step, ok := journal.Preview()
	if !ok {
		t.Fatal("Preview reported nothing to undo")
	}
	if len(step.Changes) != writers {
		t.Errorf("Preview listed %d changes, want %d — one per path", len(step.Changes), writers)
	}
}

// ----------------------------------------------------------------------------
// Revert's failure reporting
// ----------------------------------------------------------------------------

func TestRevert_PathThatEscapedTheFence_IsSkippedWithTheRefusal(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	journal := New()
	journal.Record(Mutation{
		Root:       root,
		Path:       outside,
		Pre:        []byte("before"),
		PreExisted: true,
		PostExists: false,
	})

	report, err := journal.Revert()

	if err != nil {
		t.Fatalf("Revert: %v", err)
	}
	if len(report.Skipped) != 1 {
		t.Fatalf("Skipped = %v, want the unpermitted out-of-fence path", report.Skipped)
	}
	if !strings.Contains(report.Skipped[0].Reason, "restore failed") {
		t.Errorf("reason = %q, want it to name the failed restore", report.Skipped[0].Reason)
	}
	assertAbsent(t, outside)
}
