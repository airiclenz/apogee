package undo

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// ----------------------------------------------------------------------------
// The fake Snapshotter — an in-memory stand-in for internal/snapshot's object store.
// It images the real temp workspace, so the tests exercise the same path arithmetic
// the git-backed store puts the journal through, and it computes real git blob ids so
// the conflict check is the one that will run in production.
// ----------------------------------------------------------------------------

type fakeSnapshotter struct {
	mu        sync.Mutex
	root      string
	trees     map[string]map[string][]byte
	ignore    map[string]bool // rel paths the "add pipeline" leaves out (ADR 0074 decision 12)
	fail      error           // when set, every Capture refuses
	failDiff  error           // when set, every Diff refuses
	failBlobs error           // when set, every ListBlobs refuses
}

func newFakeSnapshotter(root string) *fakeSnapshotter {
	return &fakeSnapshotter{root: root, trees: map[string]map[string][]byte{}, ignore: map[string]bool{}}
}

// Capture images every file under the workspace, keyed by its slash-separated relative path.
func (f *fakeSnapshotter) Capture(_ context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.fail != nil {
		return "", f.fail
	}
	image := map[string][]byte{}
	err := filepath.WalkDir(f.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(f.root, path)
		if err != nil {
			return err
		}
		slashed := filepath.ToSlash(rel)
		if f.ignore[slashed] {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		image[slashed] = data
		return nil
	})
	if err != nil {
		return "", err
	}

	id := fakeTreeID(image)
	f.trees[id] = image
	return id, nil
}

func (f *fakeSnapshotter) Diff(_ context.Context, a, b string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failDiff != nil {
		return nil, f.failDiff
	}
	before, after := f.trees[a], f.trees[b]
	seen := map[string]bool{}
	var changed []string
	for _, side := range []map[string][]byte{before, after} {
		for path := range side {
			if seen[path] {
				continue
			}
			seen[path] = true
			if string(before[path]) != string(after[path]) {
				changed = append(changed, path)
			}
		}
	}
	sort.Strings(changed)
	return changed, nil
}

func (f *fakeSnapshotter) ListBlobs(_ context.Context, tree string) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failBlobs != nil {
		return nil, f.failBlobs
	}
	image, ok := f.trees[tree]
	if !ok {
		return nil, fmt.Errorf("no such tree %q", tree)
	}
	blobs := make(map[string]string, len(image))
	for path, data := range image {
		blobs[path] = blobID(data, 2*sha1.Size)
	}
	return blobs, nil
}

func (f *fakeSnapshotter) Content(tree, path string) ([]byte, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	image, ok := f.trees[tree]
	if !ok {
		return nil, false, fmt.Errorf("no such tree %q", tree)
	}
	data, ok := image[path]
	return data, ok, nil
}

// fakeTreeID content-addresses an image, so two captures of an unchanged workspace answer
// the same id — the property item 11's "a pre==post group is not a step" rule reads.
func fakeTreeID(image map[string][]byte) string {
	paths := make([]string, 0, len(image))
	for path := range image {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	sum := sha1.New()
	for _, path := range paths {
		sum.Write(fmt.Appendf(nil, "%s\x00%s\n", path, blobID(image[path], 2*sha1.Size)))
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// ----------------------------------------------------------------------------
// Fixture helpers
// ----------------------------------------------------------------------------

// snapshotJournal returns a journal wired to a fake store over a fresh temp workspace.
func snapshotJournal(t *testing.T) (*Journal, *fakeSnapshotter, string) {
	t.Helper()

	root := t.TempDir()
	snap := newFakeSnapshotter(root)
	return New(WithSnapshotter(snap), WithWorkspace(root)), snap, root
}

// subprocessWrite changes a file the way a terminal command or an MCP server does: through
// nobody's funnel, so only the tree pair records it.
func subprocessWrite(t *testing.T, root, name, content string) string {
	t.Helper()

	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", name, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("subprocess write %s: %v", name, err)
	}
	return path
}

// exchange runs body between the journal's two capture points, as the loop does.
func exchange(t *testing.T, journal *Journal, body func()) {
	t.Helper()

	journal.BeginGroup()
	if err := journal.MarkPre(context.Background()); err != nil {
		t.Fatalf("MarkPre: %v", err)
	}
	body()
	if err := journal.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// onlyChange returns the single change a preview holds, failing when there is not exactly one.
func onlyChange(t *testing.T, step Step, ok bool) Change {
	t.Helper()

	if !ok {
		t.Fatal("preview reported nothing to do")
	}
	if len(step.Changes) != 1 {
		t.Fatalf("preview listed %d changes, want exactly one: %+v", len(step.Changes), step.Changes)
	}
	return step.Changes[0]
}

// ----------------------------------------------------------------------------
// Capture points and the shape of a group
// ----------------------------------------------------------------------------

func TestClose_ExchangeThatChangedNothing_LeavesNoStep(t *testing.T) {
	journal, _, root := snapshotJournal(t)
	seedFile(t, root, "notes.md", "unchanged")

	exchange(t, journal, func() {})

	if _, ok := journal.Preview(); ok {
		t.Error("Preview offered a step for an exchange that changed nothing")
	}
}

func TestClose_DroppedEmptyGroup_LeavesTheBoundaryForTheNextWrite(t *testing.T) {
	journal, _, root := snapshotJournal(t)
	seedFile(t, root, "first.md", "before")

	exchange(t, journal, func() { funnelWrite(t, journal, root, "first.md", "after") })
	exchange(t, journal, func() {}) // reached a write-capable call, wrote nothing: dropped again

	funnelWrite(t, journal, root, "second.md", "fresh") // no boundary call of its own

	step, ok := journal.Preview()
	change := onlyChange(t, step, ok)
	if change.Path != filepath.Join(root, "second.md") {
		t.Fatalf("Preview = %+v, want the later write in a group of its own", change)
	}
	if step.Ordinal != 2 {
		t.Errorf("Ordinal = %d, want 2 — the later write joined the earlier exchange's group", step.Ordinal)
	}
}

func TestClose_SubprocessWriteTheFunnelNeverSaw_BecomesADiffScopedStep(t *testing.T) {
	journal, _, root := snapshotJournal(t)
	var written string

	exchange(t, journal, func() { written = subprocessWrite(t, root, "build/out.txt", "made") })

	step, ok := journal.Preview()
	change := onlyChange(t, step, ok)
	if change.Path != written || change.Action != ActionDelete {
		t.Fatalf("Preview = %+v, want a delete of %s", change, written)
	}
	if _, err := journal.Revert(); err != nil {
		t.Fatalf("Revert: %v", err)
	}
	assertAbsent(t, written)
}

func TestRevert_ThreeSnapshotBackedExchanges_WalkBackOneAtATime(t *testing.T) {
	journal, _, root := snapshotJournal(t)
	path := seedFile(t, root, "notes.md", "v0")

	for _, content := range []string{"v1", "v2", "v3"} {
		exchange(t, journal, func() { subprocessWrite(t, root, "notes.md", content) })
	}

	for _, want := range []string{"v2", "v1", "v0"} {
		if _, err := journal.Revert(); err != nil {
			t.Fatalf("Revert back to %s: %v", want, err)
		}
		assertContent(t, path, want)
	}
	if _, err := journal.Revert(); !errors.Is(err, ErrNothingToUndo) {
		t.Errorf("fourth Revert = %v, want ErrNothingToUndo", err)
	}
}

func TestRevert_FunnelPathTheDiffOmits_RestoresFromItsPreImage(t *testing.T) {
	journal, snap, root := snapshotJournal(t)
	snap.ignore["secrets.env"] = true // the residue rule: git's add pipeline leaves it out
	path := seedFile(t, root, "secrets.env", "before")

	exchange(t, journal, func() { funnelWrite(t, journal, root, "secrets.env", "after") })

	step, ok := journal.Preview()
	change := onlyChange(t, step, ok)
	if change.Path != path || change.Action != ActionRestore {
		t.Fatalf("Preview = %+v, want a restore of the funnel's own record", change)
	}
	if _, err := journal.Revert(); err != nil {
		t.Fatalf("Revert: %v", err)
	}
	assertContent(t, path, "before")
}

func TestRevert_PathBothCapturePathsSaw_IsRevertedOnce_FromTheFunnelPreImage(t *testing.T) {
	journal, _, root := snapshotJournal(t)
	path := seedFile(t, root, "notes.md", "before")

	exchange(t, journal, func() { funnelWrite(t, journal, root, "notes.md", "after") })

	step, ok := journal.Preview()
	if !ok {
		t.Fatal("Preview reported nothing to undo")
	}
	if len(step.Changes) != 1 {
		t.Fatalf("Preview listed %d changes, want the one funnel entry: %+v", len(step.Changes), step.Changes)
	}
	if _, err := journal.Revert(); err != nil {
		t.Fatalf("Revert: %v", err)
	}
	assertContent(t, path, "before")
}

func TestClose_GroupWhoseCaptureFailed_IsKeptAsAFunnelOnlyStep(t *testing.T) {
	journal, snap, root := snapshotJournal(t)
	snap.fail = errors.New("git is wedged")
	path := seedFile(t, root, "notes.md", "before")

	journal.BeginGroup()
	if err := journal.MarkPre(context.Background()); err == nil {
		t.Fatal("MarkPre reported no error although the capture refused")
	}
	funnelWrite(t, journal, root, "notes.md", "after")
	if err := journal.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	step, ok := journal.Preview()
	change := onlyChange(t, step, ok)
	if change.Action != ActionRestore {
		t.Fatalf("Preview = %+v, want the funnel-only group to revert as it always did", change)
	}
	if _, err := journal.Revert(); err != nil {
		t.Fatalf("Revert: %v", err)
	}
	assertContent(t, path, "before")
}

func TestRevert_DiffPathEditedAfterTheAgentWroteIt_IsSkippedWithTheReason(t *testing.T) {
	journal, _, root := snapshotJournal(t)
	var written string

	exchange(t, journal, func() { written = subprocessWrite(t, root, "out.txt", "agent") })
	subprocessWrite(t, root, "out.txt", "the human's own edit")

	step, ok := journal.Preview()
	change := onlyChange(t, step, ok)
	if change.Action != ActionSkip || change.Reason != "changed since the agent wrote it" {
		t.Fatalf("Preview = %+v, want a skip naming the human's edit", change)
	}
	if _, err := journal.Revert(); err != nil {
		t.Fatalf("Revert: %v", err)
	}
	assertContent(t, written, "the human's own edit")
}

// ----------------------------------------------------------------------------
// The redo stack
// ----------------------------------------------------------------------------

func TestRedo_ReappliesThePostImageOfTheRevertedExchange(t *testing.T) {
	journal, _, root := snapshotJournal(t)
	path := seedFile(t, root, "notes.md", "before")

	exchange(t, journal, func() { subprocessWrite(t, root, "notes.md", "after") })
	if _, err := journal.Revert(); err != nil {
		t.Fatalf("Revert: %v", err)
	}
	assertContent(t, path, "before")

	step, ok := journal.RedoPreview()
	change := onlyChange(t, step, ok)
	if change.Path != path || change.Action != ActionRestore {
		t.Fatalf("RedoPreview = %+v, want a restore of the agent's own bytes", change)
	}
	if _, err := journal.Redo(step.Generation); err != nil {
		t.Fatalf("Redo: %v", err)
	}
	assertContent(t, path, "after")

	if _, ok := journal.Preview(); !ok {
		t.Error("the redone group did not return to the undo stack")
	}
}

func TestRedo_PathEditedAfterTheUndo_IsSkippedWithTheReason(t *testing.T) {
	journal, _, root := snapshotJournal(t)
	path := seedFile(t, root, "notes.md", "before")

	exchange(t, journal, func() { subprocessWrite(t, root, "notes.md", "after") })
	if _, err := journal.Revert(); err != nil {
		t.Fatalf("Revert: %v", err)
	}
	subprocessWrite(t, root, "notes.md", "the human's own edit")

	step, ok := journal.RedoPreview()
	change := onlyChange(t, step, ok)
	if change.Action != ActionSkip || change.Reason != "changed since the undo restored it" {
		t.Fatalf("RedoPreview = %+v, want a skip naming the edit made since the undo", change)
	}
	if _, err := journal.Redo(step.Generation); err != nil {
		t.Fatalf("Redo: %v", err)
	}
	assertContent(t, path, "the human's own edit")
}

func TestRedo_StaleGenerationOrEmptyStack_IsRefused(t *testing.T) {
	journal, _, root := snapshotJournal(t)
	seedFile(t, root, "notes.md", "before")

	if _, err := journal.Redo(0); !errors.Is(err, ErrNothingToRedo) {
		t.Fatalf("Redo on an empty stack = %v, want ErrNothingToRedo", err)
	}

	exchange(t, journal, func() { subprocessWrite(t, root, "notes.md", "after") })
	if _, err := journal.Revert(); err != nil {
		t.Fatalf("Revert: %v", err)
	}
	step, ok := journal.RedoPreview()
	if !ok {
		t.Fatal("RedoPreview reported nothing to redo")
	}

	if _, err := journal.Redo(step.Generation + 1); !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("Redo with a stale stamp = %v, want ErrStaleGeneration", err)
	}
	if _, ok := journal.RedoPreview(); !ok {
		t.Error("the refused Redo consumed the step anyway")
	}
}

func TestRedo_MaterialisedGroupClearsTheStack_BareBeginGroupDoesNot(t *testing.T) {
	t.Run("a bare BeginGroup leaves the stack", func(t *testing.T) {
		journal, _, root := snapshotJournal(t)
		seedFile(t, root, "notes.md", "before")
		exchange(t, journal, func() { subprocessWrite(t, root, "notes.md", "after") })
		if _, err := journal.Revert(); err != nil {
			t.Fatalf("Revert: %v", err)
		}

		journal.BeginGroup()
		if err := journal.MarkPre(context.Background()); err != nil {
			t.Fatalf("MarkPre: %v", err)
		}

		if _, ok := journal.RedoPreview(); !ok {
			t.Error("opening an exchange that wrote nothing cleared the redo stack")
		}
	})

	t.Run("a funnel write clears the stack", func(t *testing.T) {
		journal, _, root := snapshotJournal(t)
		seedFile(t, root, "notes.md", "before")
		exchange(t, journal, func() { subprocessWrite(t, root, "notes.md", "after") })
		if _, err := journal.Revert(); err != nil {
			t.Fatalf("Revert: %v", err)
		}

		exchange(t, journal, func() { funnelWrite(t, journal, root, "other.md", "fresh") })

		if _, ok := journal.RedoPreview(); ok {
			t.Error("a new write left the redo stack in place")
		}
	})

	t.Run("a diff-only write clears the stack", func(t *testing.T) {
		journal, _, root := snapshotJournal(t)
		seedFile(t, root, "notes.md", "before")
		exchange(t, journal, func() { subprocessWrite(t, root, "notes.md", "after") })
		if _, err := journal.Revert(); err != nil {
			t.Fatalf("Revert: %v", err)
		}

		exchange(t, journal, func() { subprocessWrite(t, root, "other.md", "fresh") })

		if _, ok := journal.RedoPreview(); ok {
			t.Error("a subprocess write left the redo stack in place")
		}
	})
}

// ----------------------------------------------------------------------------
// Approved out-of-workspace writes — funnel-journaled either way (ADR 0074 decision 9)
// ----------------------------------------------------------------------------

func TestEscape_RevertsAndRedoes_WithAndWithoutASnapshotter(t *testing.T) {
	for _, snapshots := range []bool{false, true} {
		name := "without a snapshotter"
		if snapshots {
			name = "with a snapshotter"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			outside := filepath.Join(t.TempDir(), "outside.txt")
			if err := os.WriteFile(outside, []byte("before"), 0o644); err != nil {
				t.Fatalf("seed the escape target: %v", err)
			}

			journal := New()
			if snapshots {
				journal = New(WithSnapshotter(newFakeSnapshotter(root)), WithWorkspace(root))
			}

			write := func() {
				if err := os.WriteFile(outside, []byte("after"), 0o644); err != nil {
					t.Fatalf("escape write: %v", err)
				}
				journal.Record(Mutation{
					Root: root, Path: outside, Permitted: outside,
					Pre: []byte("before"), PreExisted: true,
					Post: []byte("after"), PostExists: true,
				})
			}
			if snapshots {
				exchange(t, journal, write)
			} else {
				journal.BeginGroup()
				write()
			}

			if _, err := journal.Revert(); err != nil {
				t.Fatalf("Revert: %v", err)
			}
			assertContent(t, outside, "before")

			step, ok := journal.RedoPreview()
			if !ok {
				t.Fatal("RedoPreview reported nothing to redo")
			}
			if _, err := journal.Redo(step.Generation); err != nil {
				t.Fatalf("Redo: %v", err)
			}
			assertContent(t, outside, "after")
		})
	}
}

// ----------------------------------------------------------------------------
// Wrote — the run's whole account, across both capture paths
// ----------------------------------------------------------------------------

func TestWrote_CountsFunnelEntriesAndDiffOnlyPathsOnce(t *testing.T) {
	journal, _, root := snapshotJournal(t)

	exchange(t, journal, func() {
		funnelWrite(t, journal, root, "written.md", "funnel")
		subprocessWrite(t, root, "built.txt", "subprocess")
	})

	got := journal.Wrote()
	want := []string{filepath.Join(root, "written.md"), filepath.Join(root, "built.txt")}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("Wrote() = %v, want the funnel entry then the diff-only path: %v", got, want)
	}
}

// ----------------------------------------------------------------------------
// Concurrency — the snapshot surface takes the same lock as the rest
// ----------------------------------------------------------------------------

func TestSnapshotSurface_ConcurrentCaptureRecordAndPreview_IsRaceClean(t *testing.T) {
	journal, _, root := snapshotJournal(t)

	const exchanges = 8
	var work sync.WaitGroup
	stop := make(chan struct{})
	var readers sync.WaitGroup

	readers.Add(1)
	go func() {
		defer readers.Done()
		for {
			select {
			case <-stop:
				return
			default:
				journal.Preview()
				journal.RedoPreview()
				journal.Wrote()
			}
		}
	}()

	for i := range exchanges {
		work.Add(1)
		go func(n int) {
			defer work.Done()
			_ = journal.MarkPre(context.Background())
			journal.Record(Mutation{
				Root: root, Path: filepath.Join(root, fmt.Sprintf("f%d.txt", n)),
				Post: []byte("x"), PostExists: true,
			})
			_ = journal.Close(context.Background())
		}(i)
	}
	work.Wait()
	close(stop)
	readers.Wait()
}
