package undo

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
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

	if err := security.WorkspaceFence(root).WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}

	journal.Record(Mutation{
		Fence:      security.WorkspaceFence(root),
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

	if err := security.WorkspaceFence(root).Remove(path); err != nil {
		t.Fatalf("remove %s: %v", name, err)
	}

	journal.Record(Mutation{
		Fence:      security.WorkspaceFence(root),
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

	if err := security.WorkspaceFence(root).Rename(source, destination); err != nil {
		t.Fatalf("move %s to %s: %v", from, to, err)
	}

	journal.Record(Mutation{
		Fence:      security.WorkspaceFence(root),
		Path:       source,
		Pre:        sourcePre,
		PreExisted: sourceExisted,
		PostExists: false,
	})
	journal.Record(Mutation{
		Fence:      security.WorkspaceFence(root),
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
				Fence:      security.WorkspaceFence(root),
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
		Fence:      security.WorkspaceFence(root),
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

// TestRevertThroughTheRecordedFence: a revert goes back through the very Fence the record
// carries. A permitted record (ADR 0049) reaches its out-of-workspace target through
// Fence{Root, Permit}; an ordinary record runs under the workspace fence alone, so the same
// path with no permit is refused — the record, not the path, decides how far a revert reaches.
func TestRevertThroughTheRecordedFence(t *testing.T) {
	root := t.TempDir()
	// The permit names the RESOLVED target (see Mutation), so the path is spelled by its real
	// name — on macOS t.TempDir() is reached through the /var → /private/var link.
	outside := filepath.Join(security.EvalRealPath(t.TempDir()), "outside.txt")
	inside := filepath.Join(root, "inside.txt")
	for _, path := range []string{outside, inside} {
		if err := os.WriteFile(path, []byte("after"), 0o644); err != nil {
			t.Fatalf("seed %s: %v", path, err)
		}
	}
	journal := New()
	journal.Record(Mutation{
		Fence:      security.Fence{Root: root, Permit: domain.WriteEscapePermit{Real: outside}},
		Path:       outside,
		Pre:        []byte("before"),
		PreExisted: true,
		Post:       []byte("after"),
		PostExists: true,
	})
	journal.Record(Mutation{
		Fence:      security.WorkspaceFence(root),
		Path:       inside,
		Pre:        []byte("before"),
		PreExisted: true,
		Post:       []byte("after"),
		PostExists: true,
	})

	report, err := journal.Revert()

	if err != nil {
		t.Fatalf("Revert: %v", err)
	}
	if len(report.Restored) != 2 || len(report.Skipped) != 0 {
		t.Fatalf("report = %+v, want both records restored through their own fence", report)
	}
	assertContent(t, outside, "before")
	assertContent(t, inside, "before")
}

// ----------------------------------------------------------------------------
// The lock scope of a step (apogee-m6k)
//
// A revert and a redo pop the group they are about to walk and then release the lock for
// the walk itself, so the journal keeps answering while the restores run. These cases bite
// on the LOCK, not on the race detector: each parks a step inside the image source and
// asserts that a call which would have queued behind a walk-long hold arrives anyway.
// ----------------------------------------------------------------------------

// lockWaitGrace is how long a call that must NOT be queued behind a walking step is given to
// arrive. It is generous because it is not a timing measurement: a queued call never arrives
// at all until the parked step is released, so anything short of the gate's own release is
// the same answer.
const lockWaitGrace = 10 * time.Second

// waitOn fails the test unless c delivers within the grace period, naming what was waited on.
func waitOn[T any](t *testing.T, c <-chan T, what string) T {
	t.Helper()

	select {
	case v := <-c:
		return v
	case <-time.After(lockWaitGrace):
		t.Fatalf("timed out waiting for %s", what)
		return *new(T)
	}
}

// parkableExchange records one exchange the walk of which must read an image: a diff-only
// path the revert restores from the pre-image tree (the Content call the gate parks on) and
// a funnel-recorded file beside it. It returns the two paths.
func parkableExchange(t *testing.T, journal *Journal, root string) (tracked, funnelled string) {
	t.Helper()

	tracked = seedFile(t, root, "tracked.txt", "human")
	exchange(t, journal, func() {
		subprocessWrite(t, root, "tracked.txt", "agent")
		funnelled = funnelWrite(t, journal, root, "doc.txt", "agent doc")
	})
	return tracked, funnelled
}

// recordInBackground writes name through the workspace fence and journals it, the way a
// delegated sub-agent's funnel does, and closes the returned channel when the Record returns.
// It touches no *testing.T, because it runs on a goroutine of its own.
func recordInBackground(journal *Journal, root, name, content string) (<-chan struct{}, string) {
	path := filepath.Join(root, name)
	done := make(chan struct{})
	go func() {
		defer close(done)
		fence := security.WorkspaceFence(root)
		_ = fence.WriteFile(path, []byte(content), 0o644)
		journal.Record(Mutation{
			Fence:      fence,
			Path:       path,
			Post:       []byte(content),
			PostExists: true,
		})
	}()
	return done, path
}

func TestRevert_WhileItWalks_AnswersRecordAndGeneration(t *testing.T) {
	journal, gate, root := gatedJournal(t)
	tracked, funnelled := parkableExchange(t, journal, root)

	entered := gate.arm()
	reverted := make(chan Report, 1)
	go func() {
		report, _ := journal.Revert()
		reverted <- report
	}()
	waitOn(t, entered, "the revert to park inside the image source")

	stamped := make(chan uint64, 1)
	go func() { stamped <- journal.Generation() }()
	generation := waitOn(t, stamped, "Generation() to answer while the revert walked")

	recorded, fresh := recordInBackground(journal, root, "fresh.txt", "fresh")
	waitOn(t, recorded, "Record to land while the revert walked")

	gate.unblock()
	report := waitOn(t, reverted, "the revert to finish")

	assertContent(t, tracked, "human")
	assertAbsent(t, funnelled)
	if len(report.Restored) != 1 || len(report.Deleted) != 1 {
		t.Errorf("revert reported %+v, want one restore and one removal", report)
	}

	// The mid-walk record belongs to an exchange of its own: the group the revert walked was
	// popped before the walk, so nothing could merge into it.
	step, ok := journal.Preview()
	if !ok {
		t.Fatal("the mid-walk record left nothing to undo, want its own group")
	}
	if step.Ordinal != 1 {
		t.Errorf("the journal holds %d groups after the revert, want the mid-walk one alone", step.Ordinal)
	}
	if got := onlyChange(t, step, ok); got.Path != fresh {
		t.Errorf("the top group holds %s, want the mid-walk record %s", got.Path, fresh)
	}
	if step.Generation <= generation {
		t.Errorf("generation %d did not advance past the %d read mid-walk", step.Generation, generation)
	}

	// A write during the walk clears the redo stack (ADR 0074 decision 6), so the reverted
	// group is dropped rather than offered back over work the human has just asked for.
	if _, ok := journal.RedoPreview(); ok {
		t.Error("a redo is offered after an exchange wrote during the revert, want none")
	}
}

func TestRevert_RecordArrivingMidWalk_DoesNotDisturbTheRestore(t *testing.T) {
	journal, gate, root := gatedJournal(t)
	tracked, funnelled := parkableExchange(t, journal, root)

	entered := gate.arm()
	reverted := make(chan Report, 1)
	go func() {
		report, _ := journal.Revert()
		reverted <- report
	}()
	waitOn(t, entered, "the revert to park inside the image source")

	// The racing write lands on a path the parked step has not reached yet. Merged into the
	// group being walked it would move that entry's post-state onto the new bytes, and the
	// step would then "restore" over a write the human has just asked for.
	recorded, _ := recordInBackground(journal, root, "doc.txt", "the human's own line")
	waitOn(t, recorded, "Record to land while the revert walked")

	gate.unblock()
	report := waitOn(t, reverted, "the revert to finish")

	assertContent(t, tracked, "human")
	assertContent(t, funnelled, "the human's own line")
	if len(report.Skipped) != 1 || report.Skipped[0].Path != funnelled {
		t.Fatalf("revert skipped %+v, want %s left alone", report.Skipped, funnelled)
	}
	if report.Skipped[0].Reason != changedReason(undoward) {
		t.Errorf("the skip reads %q, want %q", report.Skipped[0].Reason, changedReason(undoward))
	}
}

func TestRedo_WhileItWalks_ReappliesUnderAGroupOpenedMidWalk(t *testing.T) {
	journal, gate, root := gatedJournal(t)
	tracked, funnelled := parkableExchange(t, journal, root)

	if _, err := journal.Revert(); err != nil {
		t.Fatalf("Revert: %v", err)
	}
	assertContent(t, tracked, "human")
	assertAbsent(t, funnelled)

	step, ok := journal.RedoPreview()
	if !ok {
		t.Fatal("nothing to redo after a revert that raced nothing")
	}

	entered := gate.arm()
	redone := make(chan Report, 1)
	go func() {
		report, _ := journal.Redo(step.Generation)
		redone <- report
	}()
	waitOn(t, entered, "the redo to park inside the image source")

	stamped := make(chan uint64, 1)
	go func() { stamped <- journal.Generation() }()
	waitOn(t, stamped, "Generation() to answer while the redo walked")

	recorded, fresh := recordInBackground(journal, root, "fresh.txt", "fresh")
	waitOn(t, recorded, "Record to land while the redo walked")

	gate.unblock()
	waitOn(t, redone, "the redo to finish")

	assertContent(t, tracked, "agent")
	assertContent(t, funnelled, "agent doc")

	// The exchange that wrote during the walk is the NEWEST one, so the re-applied group goes
	// back underneath it and `/undo` still walks the stack newest-first.
	top, ok := journal.Preview()
	if !ok {
		t.Fatal("nothing to undo after the redo, want two groups")
	}
	if top.Ordinal != 2 {
		t.Fatalf("the journal holds %d groups after the redo, want 2", top.Ordinal)
	}
	if got := onlyChange(t, top, ok); got.Path != fresh {
		t.Errorf("the top group holds %s, want the mid-walk record %s", got.Path, fresh)
	}
}

func TestReportLines_OrdinalOfARevertAndARedo_CountsFromTheOldestGroup(t *testing.T) {
	root := t.TempDir()
	journal := New()
	for _, name := range []string{"one.txt", "two.txt", "three.txt"} {
		journal.BeginGroup()
		funnelWrite(t, journal, root, name, "agent")
	}

	report, err := journal.Revert()
	if err != nil {
		t.Fatalf("Revert: %v", err)
	}
	if report.Ordinal != 3 {
		t.Errorf("the revert reports exchange %d, want 3", report.Ordinal)
	}
	if line := ReportLines(report)[0]; !strings.HasPrefix(line, "exchange 3:") {
		t.Errorf("the revert's first line is %q, want it to head exchange 3", line)
	}

	step, ok := journal.RedoPreview()
	if !ok {
		t.Fatal("nothing to redo after a revert")
	}
	redone, err := journal.Redo(step.Generation)
	if err != nil {
		t.Fatalf("Redo: %v", err)
	}
	if redone.Ordinal != 1 {
		t.Errorf("the redo reports exchange %d, want 1", redone.Ordinal)
	}
	if line := ReportLines(redone)[0]; !strings.HasPrefix(line, "exchange 1:") {
		t.Errorf("the redo's first line is %q, want it to head exchange 1", line)
	}
}
