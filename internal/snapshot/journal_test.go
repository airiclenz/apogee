package snapshot

// Tests for OpenJournal: the one call a Driver makes for a session's snapshot-backed undo. What
// matters here is that it NEVER costs a start — every way snapshots cannot be had comes back as
// the in-memory journal plus a reason a human can be told — and that the one it opens is wired to
// the store beside its own index.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
