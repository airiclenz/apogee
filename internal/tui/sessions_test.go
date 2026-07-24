package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/session"
)

// ----------------------------------------------------------------------------
// The /sessions history-browser overlay (session-system plan §7)
// ----------------------------------------------------------------------------

// newBrowserModel builds a ready, idle model wired to a persistence host and a workspace root, so
// the /sessions overlay's workspace filtering is exercisable.
func newBrowserModel(t *testing.T, eng Engine, host *fakeSessionHost, workspace string) Model {
	t.Helper()
	m := newModel(context.Background(), eng, Options{Sessions: host, Workspace: workspace}, nil)
	return step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
}

// openBrowser drives /sessions to completion: it submits the command, runs the List Cmd off the
// loop, and folds the resulting sessionListMsg — so the returned model has the overlay open (or
// the empty/error note folded, when nothing opened).
func openBrowser(t *testing.T, m Model) Model {
	t.Helper()
	m.input.SetValue("/sessions")
	m, cmd := stepCmd(t, m, keyEnter())
	if cmd == nil {
		t.Fatal("/sessions dispatched no List Cmd")
	}
	return step(t, m, cmdMsg(cmd))
}

// storeMeta seeds one record into the fake host with the given browsable metadata and transcript
// blob, so the browser can list, load, delete, and rename it.
func storeMeta(h *fakeSessionHost, id, title, workspace string, updated time.Time, ctxUsed int, blob []byte) {
	h.seed(session.Record{
		Meta: session.Meta{
			ID:        id,
			Title:     title,
			Workspace: workspace,
			UpdatedAt: updated,
			CtxUsed:   ctxUsed,
			UserMsgs:  1,
		},
		Transcript: blob,
		Session:    domain.Session{},
	})
}

// Opening /sessions lists the stored metas filtered to the current workspace, newest first; the
// `a` toggle widens the view to every workspace.
func TestSessionBrowserListsAndTogglesWorkspace(t *testing.T) {
	host := &fakeSessionHost{}
	now := time.Now()
	storeMeta(host, "a-old", "older here", "/ws/a", now.Add(-2*time.Hour), 0, nil)
	storeMeta(host, "a-new", "newer here", "/ws/a", now.Add(-1*time.Minute), 0, nil)
	storeMeta(host, "b-one", "other project", "/ws/b", now.Add(-30*time.Minute), 0, nil)

	m := newBrowserModel(t, &fakeEngine{}, host, "/ws/a")
	m = openBrowser(t, m)

	if !m.sessionBrowser.open {
		t.Fatal("browser did not open over a non-empty store")
	}
	vis := m.sessionBrowser.visible(m.opts.Workspace)
	if len(vis) != 2 {
		t.Fatalf("current-workspace view = %d rows, want 2 (the /ws/a sessions)", len(vis))
	}
	if vis[0].ID != "a-new" || vis[1].ID != "a-old" {
		t.Errorf("current-workspace order = [%s %s], want newest-first [a-new a-old]", vis[0].ID, vis[1].ID)
	}

	// `a` widens to all workspaces — the /ws/b session now appears too.
	m = step(t, m, keyRune('a'))
	if !m.sessionBrowser.allWorkspaces {
		t.Fatal("`a` did not toggle to the all-workspaces view")
	}
	if got := len(m.sessionBrowser.visible(m.opts.Workspace)); got != 3 {
		t.Errorf("all-workspaces view = %d rows, want 3", got)
	}
}

// The open overlay renders above the input: its title, the scope, a session row, and the key
// legend all appear in the View (a smoke test that the pane composes without panicking).
func TestSessionBrowserRendersPane(t *testing.T) {
	host := &fakeSessionHost{}
	storeMeta(host, "sess-1", "render me", "/ws/a", time.Now().Add(-5*time.Minute), 0, nil)
	m := newBrowserModel(t, &fakeEngine{}, host, "/ws/a")
	m = openBrowser(t, m)

	got := plain(m.View())
	for _, want := range []string{"saved sessions", "this workspace", "render me", "5m ago", "1 msg", "esc close"} {
		if !strings.Contains(got, want) {
			t.Errorf("open browser View missing %q:\n%s", want, got)
		}
	}
}

// A completely empty store opens no overlay and notes it instead.
func TestSessionBrowserEmptyStoreNotesNoOverlay(t *testing.T) {
	host := &fakeSessionHost{}
	m := newBrowserModel(t, &fakeEngine{}, host, "/ws/a")
	m = openBrowser(t, m)

	if m.sessionBrowser.open {
		t.Error("browser opened over an empty store; want no overlay")
	}
	if !hasEntry(m, entryNote, "no saved sessions") {
		t.Error("empty store did not note 'no saved sessions'")
	}
}

// The resume happy path: enter loads the selected record, restores it into the live engine,
// repaints its scrollback closed by a "resumed:" note, relights the gauge, and — because Load
// activates the session — routes subsequent saves to the loaded id.
func TestSessionBrowserResumeHappyPath(t *testing.T) {
	var src transcript
	src.addUser("what is the capital of france", nil)
	src.apply(domain.MessageEvent{Text: "Paris."})
	blob, err := encodeTranscript(&src)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	marker := domain.Session{Version: domain.SessionVersion, State: []byte(`{"k":1}`)}

	host := &fakeSessionHost{}
	host.seed(session.Record{
		Meta:       session.Meta{ID: "sess-1", Title: "france question", Workspace: "/ws/a", UpdatedAt: time.Now(), CtxUsed: 8192, UserMsgs: 1},
		Transcript: blob,
		Session:    marker,
	})
	eng := &fakeEngine{}
	m := newBrowserModel(t, eng, host, "/ws/a")
	m = openBrowser(t, m)

	// enter dispatches the Load Cmd (which activates the record); folding it restores + repaints.
	m, cmd := stepCmd(t, m, keyEnter())
	if cmd == nil {
		t.Fatal("enter dispatched no Load Cmd")
	}
	if m.sessionBrowser.open {
		t.Error("browser stayed open after enter; want it closed while the record loads")
	}
	m = step(t, m, cmdMsg(cmd)) // fold sessionLoadedMsg → resumeLoaded

	if got := eng.restores(); len(got) != 1 || string(got[0].State) != `{"k":1}` {
		t.Fatalf("RestoreSession calls = %v, want exactly the loaded record's Session", got)
	}
	if !hasEntry(m, entryUser, "what is the capital of france") {
		t.Error("resumed transcript did not repaint the stored user message")
	}
	if !hasEntry(m, entryNote, "resumed: france question") {
		t.Error("resume did not close with a 'resumed: <title>' note")
	}
	if m.ctxUsed != 8192 {
		t.Errorf("ctxUsed after resume = %d, want the stored 8192 (gauge relight)", m.ctxUsed)
	}
	if m.transcript.entries[0].kind != entryStartup {
		t.Error("the start-up box was not re-seeded at the head of the resumed view")
	}

	// Load activated the record: a subsequent per-Turn save targets the loaded id, not a fresh one.
	m = driveOneSave(t, m, domain.Session{})
	calls := host.savedCalls()
	if len(calls) == 0 || calls[len(calls)-1].id != "sess-1" {
		t.Errorf("post-resume save id = %v, want the loaded 'sess-1'", calls)
	}
}

// A resumed record whose stored snapshot is a mid-Exchange session (InExchange true after the
// restore) gets the interrupted note appended.
func TestSessionBrowserResumeInterruptedNote(t *testing.T) {
	var src transcript
	src.addUser("start a long task", nil)
	blob, err := encodeTranscript(&src)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	host := &fakeSessionHost{}
	storeMeta(host, "sess-1", "long task", "/ws/a", time.Now(), 0, blob)
	eng := &fakeEngine{inExchange: true} // the restored snapshot is mid-Exchange
	m := newBrowserModel(t, eng, host, "/ws/a")
	m = openBrowser(t, m)

	m, cmd := stepCmd(t, m, keyEnter())
	m = step(t, m, cmdMsg(cmd))

	if !hasEntry(m, entryNote, interruptedNote) {
		t.Error("a mid-Exchange resume did not append the interrupted note")
	}
}

// A RestoreSession failure leaves the view and the live engine untouched (the locked error rule):
// the outgoing conversation stays painted, no start-up box is re-seeded, and only an honest
// failure note is added.
func TestSessionBrowserResumeErrorLeavesViewUntouched(t *testing.T) {
	host := &fakeSessionHost{}
	storeMeta(host, "sess-1", "corrupt one", "/ws/a", time.Now(), 0, nil)
	eng := &fakeEngine{restoreFn: func(domain.Session) error { return errors.New("bad snapshot") }}
	m := newBrowserModel(t, eng, host, "/ws/a")
	seedConversation(&m) // a live conversation the failed resume must not disturb
	before := len(m.transcript.entries)

	m = openBrowser(t, m)
	m, cmd := stepCmd(t, m, keyEnter())
	m = step(t, m, cmdMsg(cmd))

	if !hasEntry(m, entryUser, seededUserText) {
		t.Error("the failed resume wiped the outgoing conversation; the view must be left untouched")
	}
	if hasEntry(m, entryNote, "resumed: corrupt one") {
		t.Error("a failed restore still claimed to resume")
	}
	if !hasEntry(m, entryNote, "could not restore session: bad snapshot") {
		t.Error("a failed restore did not note the failure")
	}
	// The view only grew by the failure note — the scrollback was not reset and re-seeded.
	if len(m.transcript.entries) != before+1 {
		t.Errorf("transcript entries = %d, want %d (+1 failure note only)", len(m.transcript.entries), before+1)
	}
}

// d arms an inline confirm and y deletes; deleting the ACTIVE session rotates the host and notes
// that the live conversation lives on.
func TestSessionBrowserDeleteActiveRotates(t *testing.T) {
	host := &fakeSessionHost{}
	now := time.Now()
	storeMeta(host, "sess-1", "the active one", "/ws/a", now, 0, nil)
	storeMeta(host, "sess-2", "another one", "/ws/a", now.Add(-time.Hour), 0, nil)
	host.activeID = "sess-1" // sess-1 is the live conversation's file

	m := newBrowserModel(t, &fakeEngine{}, host, "/ws/a")
	m = openBrowser(t, m) // sess-1 is newest, so it is selected first

	m = step(t, m, keyRune('d'))
	if !m.sessionBrowser.confirming {
		t.Fatal("d did not arm the delete confirm")
	}
	m, cmd := stepCmd(t, m, keyRune('y'))
	if cmd == nil {
		t.Fatal("y dispatched no delete Cmd")
	}
	if host.rotateCount() != 1 {
		t.Errorf("Rotate calls after deleting the active session = %d, want 1", host.rotateCount())
	}
	if !hasEntry(m, entryNote, "current session's file deleted — it lives on in memory; the next turn saves it as a new session") {
		t.Error("deleting the active session did not note that the conversation lives on")
	}
	m = step(t, m, cmdMsg(cmd)) // fold the re-list
	if _, gone := host.stored["sess-1"]; gone {
		t.Error("the deleted session still exists in the store")
	}
	if !m.sessionBrowser.open {
		t.Error("the browser closed even though sess-2 remains")
	}
}

// r opens an inline rename edit; typing then enter commits the new title through Sessions.Rename
// and the re-list repaints it.
func TestSessionBrowserRenameCommits(t *testing.T) {
	host := &fakeSessionHost{}
	storeMeta(host, "sess-1", "old title", "/ws/a", time.Now(), 0, nil)
	m := newBrowserModel(t, &fakeEngine{}, host, "/ws/a")
	m = openBrowser(t, m)

	m = step(t, m, keyRune('r'))
	if !m.sessionBrowser.renaming {
		t.Fatal("r did not open the rename edit")
	}
	// The edit pre-fills the current title so the human tweaks rather than retypes; append " v2".
	if m.sessionBrowser.renameBuf != "old title" {
		t.Fatalf("rename buffer = %q, want it pre-filled with the current title", m.sessionBrowser.renameBuf)
	}
	for _, r := range " v2" {
		m = step(t, m, keyRune(r))
	}
	m, cmd := stepCmd(t, m, keyEnter())
	if cmd == nil {
		t.Fatal("committing the rename dispatched no Cmd")
	}
	m = step(t, m, cmdMsg(cmd)) // fold the re-list

	if got := host.stored["sess-1"].Meta.Title; got != "old title v2" {
		t.Errorf("stored title after rename = %q, want %q", got, "old title v2")
	}
	if vis := m.sessionBrowser.visible(m.opts.Workspace); len(vis) != 1 || vis[0].Title != "old title v2" {
		t.Errorf("browser row title after rename = %v, want it to show 'old title v2'", vis)
	}
}

// esc peels the modal one layer at a time: a live rename edit → the plain row → the overlay closed.
func TestSessionBrowserEscLayers(t *testing.T) {
	host := &fakeSessionHost{}
	storeMeta(host, "sess-1", "a session", "/ws/a", time.Now(), 0, nil)
	m := newBrowserModel(t, &fakeEngine{}, host, "/ws/a")
	m = openBrowser(t, m)

	m = step(t, m, keyRune('r')) // into rename edit
	m = step(t, m, keyEsc())
	if m.sessionBrowser.renaming {
		t.Fatal("esc did not cancel the rename edit")
	}
	if !m.sessionBrowser.open {
		t.Fatal("esc closed the whole overlay from the rename edit; want it to peel one layer")
	}
	m = step(t, m, keyEsc())
	if m.sessionBrowser.open {
		t.Error("a second esc did not close the overlay")
	}
}

// The overlay is idle-only: an enter while a worker runs never opens it (submit is unreachable, so
// no List Cmd is dispatched).
func TestSessionBrowserRefusesToOpenWhileBusy(t *testing.T) {
	host := &fakeSessionHost{}
	storeMeta(host, "sess-1", "a session", "/ws/a", time.Now(), 0, nil)
	m := newBrowserModel(t, &fakeEngine{}, host, "/ws/a")
	m.state = stateRunning
	m.input.SetValue("/sessions")

	m, cmd := stepCmd(t, m, keyEnter())
	if m.sessionBrowser.open {
		t.Error("the browser opened while a worker was running")
	}
	if cmd != nil {
		t.Error("a busy /sessions dispatched a Cmd; enter must be a no-op while running")
	}
}

// ----------------------------------------------------------------------------
// Pure row-formatting helpers
// ----------------------------------------------------------------------------

func TestRelativeTime(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		when time.Time
		want string
	}{
		{"zero is unknown", time.Time{}, "unknown"},
		{"seconds ago is just now", now.Add(-30 * time.Second), "just now"},
		{"minutes", now.Add(-5 * time.Minute), "5m ago"},
		{"hours", now.Add(-3 * time.Hour), "3h ago"},
		{"days", now.Add(-2 * 24 * time.Hour), "2d ago"},
		{"weeks", now.Add(-3 * 7 * 24 * time.Hour), "3w ago"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := relativeTime(c.when, now); got != c.want {
				t.Errorf("relativeTime = %q, want %q", got, c.want)
			}
		})
	}
}

func TestSessionRowLabel(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	meta := session.Meta{Title: "a task", UpdatedAt: now.Add(-5 * time.Minute), UserMsgs: 3, Workspace: "/home/me/proj"}

	// Same-workspace row in the current view: no workspace suffix.
	if got := sessionRowLabel(meta, "/home/me/proj", false, now); got != "a task · 5m ago · 3 msgs" {
		t.Errorf("current-view label = %q", got)
	}
	// Foreign row in the all view: the workspace base is appended.
	if got := sessionRowLabel(meta, "/home/me/other", true, now); got != "a task · 5m ago · 3 msgs · proj" {
		t.Errorf("foreign all-view label = %q, want the workspace base appended", got)
	}
	// A legacy record with no workspace reads a friendly base rather than filepath.Base's ".".
	legacy := session.Meta{Title: "old", UpdatedAt: now.Add(-time.Hour), UserMsgs: 1}
	if got := sessionRowLabel(legacy, "/home/me/proj", true, now); got != "old · 1h ago · 1 msg · unknown workspace" {
		t.Errorf("legacy all-view label = %q", got)
	}
}
