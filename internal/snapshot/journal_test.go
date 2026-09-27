package snapshot

// Tests for OpenJournal: the one call a Driver makes for a session's snapshot-backed undo. What
// matters here is that it NEVER costs a start — every way snapshots cannot be had comes back as
// the in-memory journal plus a reason a human can be told — and that the one it opens is wired to
// the store beside its own index.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/undo"
)

// newHomeAndWorkspace returns an apogee home and a workspace, siblings of one temp root, both
// symlink-resolved: the store records paths relative to the work-tree git resolves, and a temp dir
// reached through a symlink (macOS /var) would otherwise make the index disagree with the run.
func newHomeAndWorkspace(t *testing.T) (home, workspace string) {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the temp root: %v", err)
	}
	home, workspace = filepath.Join(root, "home"), filepath.Join(root, "workspace")
	for _, dir := range []string{home, workspace} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}
	return home, workspace
}

// The four ways a call cannot open a store at all: each answers a usable journal and a reason,
// and none of them is an error — a start is never lost over undo.
func TestOpenJournalFallsBackWithAReasonRatherThanFailing(t *testing.T) {
	t.Parallel()
	home, workspace := newHomeAndWorkspace(t)

	tests := []struct {
		name                       string
		home, sessionID, workspace string
		enabled                    bool
		want                       string
	}{
		{name: "the key is off", home: home, sessionID: "s1", workspace: workspace, enabled: false, want: reasonDisabled},
		{name: "no home", home: "", sessionID: "s1", workspace: workspace, enabled: true, want: reasonNoHome},
		{name: "no session id", home: home, sessionID: "", workspace: workspace, enabled: true, want: reasonNoHome},
		{name: "no workspace", home: home, sessionID: "s1", workspace: "", enabled: true, want: reasonNoWorkDir},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			journal, reason, err := OpenJournal(context.Background(), tt.home, tt.sessionID, tt.workspace, tt.enabled)
			if err != nil {
				t.Fatalf("OpenJournal: %v; a store that cannot be opened is a reason, not a failed start", err)
			}
			if journal == nil {
				t.Fatal("OpenJournal returned no journal; the fallback is ADR 0051's in-memory one, never nil")
			}
			if reason != tt.want {
				t.Errorf("reason = %q, want %q", reason, tt.want)
			}
			if _, err := os.Stat(Dir(tt.home, tt.sessionID)); err == nil {
				t.Errorf("a fallback created the store directory %s; nothing should have been opened",
					Dir(tt.home, tt.sessionID))
			}
		})
	}
}

// git off the PATH is the reason ADR 0074 decision 2 is written for: the same fallback, the same
// sentence, and no error. It cannot run in parallel — it edits the environment.
func TestOpenJournalReportsAnAbsentGit(t *testing.T) {
	home, workspace := newHomeAndWorkspace(t)
	t.Setenv("PATH", "")

	journal, reason, err := OpenJournal(context.Background(), home, "s1", workspace, true)
	if err != nil {
		t.Fatalf("OpenJournal: %v; a machine without git is a supported configuration", err)
	}
	if journal == nil {
		t.Fatal("OpenJournal returned no journal")
	}
	if reason != reasonNoGit {
		t.Errorf("reason = %q, want %q", reason, reasonNoGit)
	}
}

// The whole point, end to end: the journal it opens captures through the session's own store, its
// index lands beside the objects rather than in the workspace, and a second call in the same
// session reads that index back — which is what makes `/undo` survive a relaunch.
func TestOpenJournalOpensAStoreAndReloadsItsIndex(t *testing.T) {
	t.Parallel()
	requireGit(t)
	home, workspace := newHomeAndWorkspace(t)
	ctx := context.Background()

	journal, reason, err := OpenJournal(ctx, home, "s1", workspace, true)
	if err != nil {
		t.Fatalf("OpenJournal: %v", err)
	}
	if reason != "" {
		t.Fatalf("reason = %q, want none: snapshots are in force", reason)
	}

	journal.BeginGroup()
	if err := journal.MarkPre(ctx); err != nil {
		t.Fatalf("MarkPre: %v", err)
	}
	writeFile(t, workspace, "wrote.txt", "by a subprocess the funnel never saw\n")
	if err := journal.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	index := filepath.Join(Dir(home, "s1"), journalFileName)
	if _, err := os.Stat(index); err != nil {
		t.Fatalf("no index at %s: %v", index, err)
	}
	if _, err := os.Stat(filepath.Join(workspace, storeRootName)); err == nil {
		t.Error("the store wrote inside the workspace; ADR 0074 decision 1 forbids it")
	}

	// A second process, same session: the step the first one recorded is still there.
	reopened, reason, err := OpenJournal(ctx, home, "s1", workspace, true)
	if err != nil {
		t.Fatalf("re-OpenJournal: %v", err)
	}
	if reason != "" {
		t.Fatalf("reason on reopen = %q, want none", reason)
	}
	step, ok := reopened.Preview()
	if !ok {
		t.Fatal("the reopened journal has no step; the index did not survive the process boundary")
	}
	if len(step.Changes) != 1 || !strings.HasSuffix(step.Changes[0].Path, "wrote.txt") {
		t.Errorf("reopened step changes = %+v, want the one written file", step.Changes)
	}
}

// An index taken of ANOTHER workspace is a reason, not an error and not a revert: the recorded
// trees spell their paths relative to a root this run does not have.
func TestOpenJournalReportsAnIndexOfAnotherWorkspace(t *testing.T) {
	t.Parallel()
	requireGit(t)
	home, workspace := newHomeAndWorkspace(t)

	dir := Dir(home, "s1")
	if err := os.MkdirAll(dir, storeDirPerm); err != nil {
		t.Fatalf("create the store directory: %v", err)
	}
	elsewhere, err := json.Marshal(undo.Index{Version: 1, Workspace: filepath.Join(workspace, "..", "other")})
	if err != nil {
		t.Fatalf("encode the index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, journalFileName), elsewhere, 0o600); err != nil {
		t.Fatalf("write the index: %v", err)
	}

	journal, reason, err := OpenJournal(context.Background(), home, "s1", workspace, true)
	if err != nil {
		t.Fatalf("OpenJournal: %v; a moved session is allowed, not an error", err)
	}
	if journal == nil {
		t.Fatal("OpenJournal returned no journal")
	}
	if reason != reasonMismatch {
		t.Errorf("reason = %q, want %q", reason, reasonMismatch)
	}
}

// An index this apogee cannot read at all IS an error — the caller decides what to do about it,
// and ADR 0074 decision 2's fallback is its answer. The distinction matters: a mismatch is a
// human moving a session, a bad version is a file nothing here should overwrite silently.
func TestOpenJournalErrorsOnAnUnreadableIndex(t *testing.T) {
	t.Parallel()
	requireGit(t)
	home, workspace := newHomeAndWorkspace(t)

	dir := Dir(home, "s1")
	if err := os.MkdirAll(dir, storeDirPerm); err != nil {
		t.Fatalf("create the store directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, journalFileName), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write the index: %v", err)
	}

	journal, reason, err := OpenJournal(context.Background(), home, "s1", workspace, true)
	if err == nil {
		t.Fatalf("OpenJournal accepted a corrupt index: journal=%v reason=%q", journal != nil, reason)
	}
	if journal != nil {
		t.Error("an errored OpenJournal handed back a journal; the caller's fallback is its own")
	}
}

// OpenStored is the verb's opener: home and id alone, the workspace read from the index the session
// left beside its objects. The reopened journal holds the step the session recorded, which pins
// both facts the verb used to compose for itself — the index file's name at Dir, and the workspace
// field inside it — so a rename on either side cannot turn every saved session into "nothing to
// undo" silently.
func TestOpenStoredReadsTheWorkspaceFromTheIndex(t *testing.T) {
	t.Parallel()
	requireGit(t)
	home, workspace := newHomeAndWorkspace(t)
	ctx := context.Background()

	journal, reason, err := OpenJournal(ctx, home, "s1", workspace, true)
	if err != nil || reason != "" {
		t.Fatalf("OpenJournal: err=%v reason=%q", err, reason)
	}
	journal.BeginGroup()
	if err := journal.MarkPre(ctx); err != nil {
		t.Fatalf("MarkPre: %v", err)
	}
	writeFile(t, workspace, "note.txt", "written\n")
	if err := journal.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	stored, reason, err := OpenStored(ctx, home, "s1")

	if err != nil {
		t.Fatalf("OpenStored: %v", err)
	}
	if reason != "" {
		t.Fatalf("reason = %q, want none: the index names the workspace", reason)
	}
	step, ok := stored.Preview()
	if !ok {
		t.Fatal("the stored journal has no step; the index's workspace did not reach the store")
	}
	if len(step.Changes) != 1 || step.Changes[0].Path != filepath.Join(workspace, "note.txt") {
		t.Errorf("stored step changes = %+v, want the one file under %s", step.Changes, workspace)
	}
}

// The missing-index contract: a session that left no index is ErrNoIndex — before git is even
// looked for, and without a store being initialised under the home for an id that names nothing.
func TestOpenStoredAnswersErrNoIndexBeforeLookingForGit(t *testing.T) {
	home, _ := newHomeAndWorkspace(t)
	t.Setenv("PATH", "")

	journal, reason, err := OpenStored(context.Background(), home, "s-never")

	if !errors.Is(err, ErrNoIndex) {
		t.Fatalf("OpenStored: journal=%v reason=%q err=%v, want ErrNoIndex", journal != nil, reason, err)
	}
	if _, err := os.Stat(Dir(home, "s-never")); !os.IsNotExist(err) {
		t.Errorf("an unknown id still opened a store (err %v)", err)
	}
	if _, _, err := OpenStored(context.Background(), "", "s-never"); !errors.Is(err, ErrNoIndex) {
		t.Errorf("OpenStored with no home: %v, want ErrNoIndex", err)
	}
}

// With an index present and git absent, the answer is OpenJournal's own reason — the same
// sentence `/undo` names in the session — never ErrNoIndex.
func TestOpenStoredReportsAnAbsentGit(t *testing.T) {
	home, workspace := newHomeAndWorkspace(t)
	writeIndex(t, Dir(home, "s1"), undo.Index{Version: 1, Workspace: workspace})
	t.Setenv("PATH", "")

	journal, reason, err := OpenStored(context.Background(), home, "s1")

	if err != nil {
		t.Fatalf("OpenStored: %v", err)
	}
	if journal == nil || reason != reasonNoGit {
		t.Errorf("journal=%v reason=%q, want a fallback journal with %q", journal != nil, reason, reasonNoGit)
	}
}

// An index that is there and wrong is an error in the verb's own words, which the verb wraps
// unchanged: corrupt bytes, and a file that names no workspace.
func TestOpenStoredErrorsOnAnIndexItCannotUse(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		index []byte
		want  string
	}{
		{"corrupt", []byte("{not json"), "decode the session's undo index"},
		{"no workspace", []byte(`{"version":1}`), "the session's undo index names no workspace"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			home, _ := newHomeAndWorkspace(t)
			dir := Dir(home, "s1")
			if err := os.MkdirAll(dir, storeDirPerm); err != nil {
				t.Fatalf("create the store directory: %v", err)
			}
			if err := os.WriteFile(filepath.Join(dir, journalFileName), tc.index, 0o600); err != nil {
				t.Fatalf("write the index: %v", err)
			}

			journal, _, err := OpenStored(context.Background(), home, "s1")

			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("OpenStored: journal=%v err=%v, want %q", journal != nil, err, tc.want)
			}
		})
	}
}

// writeIndex encodes index as the session's journal.json under dir, creating the store directory.
func writeIndex(t *testing.T, dir string, index undo.Index) {
	t.Helper()

	if err := os.MkdirAll(dir, storeDirPerm); err != nil {
		t.Fatalf("create the store directory: %v", err)
	}
	data, err := json.Marshal(index)
	if err != nil {
		t.Fatalf("encode the index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, journalFileName), data, 0o600); err != nil {
		t.Fatalf("write the index: %v", err)
	}
}

// Dir is the one spelling of the store path, and it refuses to name one it cannot.
func TestDirNamesTheSessionStoreAndNothingElse(t *testing.T) {
	t.Parallel()

	if got, want := Dir("/home/.apogee", "s1"), filepath.Join("/home/.apogee", "snapshots", "s1"); got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
	if got := Dir("", "s1"); got != "" {
		t.Errorf("Dir with no home = %q, want empty", got)
	}
	if got := Dir("/home/.apogee", ""); got != "" {
		t.Errorf("Dir with no session = %q, want empty", got)
	}
}

// requireUnreadableFiles skips where a mode-000 file cannot make `git add` fail: Windows ignores
// the POSIX permission bits, and root reads the file regardless of them.
func requireUnreadableFiles(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits cannot make a file unreadable on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads a mode-000 file, so git add cannot be made to fail")
	}
}

// A file that EXISTED when the exchange began but could not be read by the pre-image capture is
// missing from that image, and a diff against a post-image that could read it calls it added.
// `/undo` must not take that for a file the exchange created and delete it — in this process or
// in the next one, which rebuilds the diff from the recorded trees. The exchange is the second
// of the session, so the persistent index already staged the file once (the case item 25's fresh
// index widened this to).
func TestUndoNeverDeletesAFileTheCaptureCouldNotReadAtTheStart(t *testing.T) {
	requireGit(t)
	requireUnreadableFiles(t)
	home, workspace := newHomeAndWorkspace(t)
	writeFile(t, workspace, "locked.txt", "the human's own file\n")
	requireUndoKeepsWhatTheCaptureCouldNotRead(t, home, workspace,
		[]string{filepath.Join(workspace, "locked.txt")},
		map[string]string{"locked.txt": "the human's own file\n"})
}

// A directory the pre-image capture could not open is worse than a file it could not read: git
// only warns about it, so nothing — not the failed add, not `ls-files --others` — names what is
// inside. With an erroring file beside it forcing the fresh index, every file inside the
// directory is missing from the pre-image, and `/undo` must leave each of them alone once the
// exchange made the directory readable again.
func TestUndoNeverDeletesTheFilesOfADirectoryTheCaptureCouldNotOpen(t *testing.T) {
	requireGit(t)
	requireUnreadableFiles(t)
	home, workspace := newHomeAndWorkspace(t)
	writeFile(t, workspace, "locked.txt", "the human's own file\n")
	writeFile(t, workspace, "dir/inner.txt", "inside the closed directory\n")
	writeFile(t, workspace, "dir/sub/deep.txt", "deeper inside it\n")
	requireUndoKeepsWhatTheCaptureCouldNotRead(t, home, workspace,
		[]string{filepath.Join(workspace, "locked.txt"), filepath.Join(workspace, "dir")},
		map[string]string{
			"locked.txt":       "the human's own file\n",
			"dir/inner.txt":    "inside the closed directory\n",
			"dir/sub/deep.txt": "deeper inside it\n",
		})
}

// requireUndoKeepsWhatTheCaptureCouldNotRead runs two exchanges over workspace: the first
// captures every path readable, the second begins with each of locked at mode 000 and gives
// each its mode back while it writes a file of its own. The step `/undo` would take — live and
// from a reopened journal — must delete none of survivors, the revert must leave each with its
// content, and the exchange's own file must go.
func requireUndoKeepsWhatTheCaptureCouldNotRead(t *testing.T, home, workspace string, locked []string, survivors map[string]string) {
	t.Helper()
	ctx := context.Background()
	modes := make(map[string]os.FileMode, len(locked))
	for _, path := range locked {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		modes[path] = info.Mode().Perm()
		t.Cleanup(func() { _ = os.Chmod(path, modes[path]) })
	}

	journal, reason, err := OpenJournal(ctx, home, "s1", workspace, true)
	if err != nil || reason != "" {
		t.Fatalf("OpenJournal: reason %q, err %v", reason, err)
	}
	exchange := func(during func()) {
		t.Helper()
		journal.BeginGroup()
		if err := journal.MarkPre(ctx); err != nil {
			t.Fatalf("MarkPre: %v", err)
		}
		during()
		if err := journal.Close(ctx); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}
	exchange(func() { writeFile(t, workspace, "first.txt", "exchange one\n") })

	for _, path := range locked {
		if err := os.Chmod(path, 0); err != nil {
			t.Fatal(err)
		}
	}
	exchange(func() {
		for _, path := range locked {
			if err := os.Chmod(path, modes[path]); err != nil {
				t.Fatal(err)
			}
		}
		writeFile(t, workspace, "second.txt", "exchange two\n")
	})

	requireNoDelete := func(label string, step undo.Step) {
		t.Helper()
		for _, change := range step.Changes {
			rel, err := filepath.Rel(workspace, change.Path)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := survivors[filepath.ToSlash(rel)]; ok && change.Action == undo.ActionDelete {
				t.Fatalf("%s: step %+v deletes %s, a file that existed before the exchange", label, step.Changes, rel)
			}
		}
	}
	step, ok := journal.Preview()
	if !ok {
		t.Fatal("no step to preview after the second exchange")
	}
	requireNoDelete("live preview", step)

	reopened, reason, err := OpenJournal(ctx, home, "s1", workspace, true)
	if err != nil || reason != "" {
		t.Fatalf("re-OpenJournal: reason %q, err %v", reason, err)
	}
	step, ok = reopened.Preview()
	if !ok {
		t.Fatal("no step to preview in the reopened journal")
	}
	requireNoDelete("reopened preview", step)

	if _, err := reopened.Revert(); err != nil {
		t.Fatalf("Revert: %v", err)
	}
	for rel, want := range survivors {
		data, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s after /undo: %v", rel, err)
		}
		if string(data) != want {
			t.Errorf("%s after /undo = %q, want the human's content untouched", rel, data)
		}
	}
	if _, err := os.Stat(filepath.Join(workspace, "second.txt")); !os.IsNotExist(err) {
		t.Errorf("second.txt after /undo: stat err %v, want it removed with the exchange that wrote it", err)
	}
}
