package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

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

	// The successful resume activated the loaded record: a subsequent per-Turn save targets the
	// loaded id, not a fresh one.
	m = driveOneSave(t, m, domain.Session{})
	calls := host.savedCalls()
	if len(calls) == 0 || calls[len(calls)-1].id != "sess-1" {
		t.Errorf("post-resume save id = %v, want the loaded 'sess-1'", calls)
	}
	// …and that save carries the conversation, not the resume notice: the notice is display-only,
	// re-derived from the record every time it is opened.
	saved := calls[len(calls)-1].transcript
	if strings.Contains(string(saved), "resumed:") {
		t.Errorf("the saved record carries the resume notice: %s", saved)
	}
	if !strings.Contains(string(saved), "what is the capital of france") {
		t.Errorf("the saved record lost the replayed conversation: %s", saved)
	}
}

// Resume-time notices never accumulate in the record. Opening the SAME session twice — each time
// resuming, then saving what the view holds back over the record — leaves the stored scrollback
// exactly as the conversation left it, while the human still sees the notices on every reopen. This
// is the ISSUES.md defect: they used to persist, so a record collected one more "resumed:" line per
// resume, forever. The workspace loads a context file here too, because the "context: …" notice the
// restore reprints is re-derived exactly as the resume line is and must compound no more than it.
func TestSessionBrowserResumeNotesDoNotAccumulate(t *testing.T) {
	var src transcript
	src.addUser("what is the capital of france", nil)
	src.apply(domain.MessageEvent{Text: "Paris."})
	blob, err := encodeTranscript(&src)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	host := &fakeSessionHost{}
	storeMeta(host, "sess-1", "france question", "/ws/a", time.Now(), 0, blob)

	for round := 1; round <= 2; round++ {
		m := newBrowserModel(t, &fakeEngine{contextReport: loadedReport()}, host, "/ws/a")
		m = openBrowser(t, m)
		m, cmd := stepCmd(t, m, keyEnter())
		if cmd == nil {
			t.Fatalf("round %d: enter dispatched no Load Cmd", round)
		}
		m = step(t, m, cmdMsg(cmd)) // fold sessionLoadedMsg → resumeLoaded
		if !hasEntry(m, entryNote, "resumed: france question") {
			t.Fatalf("round %d: the human was not shown the resume notice", round)
		}
		if !hasEntry(m, entryNote, "context: AGENTS.md (3.1 KiB)") {
			t.Fatalf("round %d: the human was not shown the context-files notice", round)
		}
		// A per-Turn save writes the resumed view back over the record — which is exactly what the
		// next round loads, so anything persisted here compounds.
		m = driveOneSave(t, m, domain.Session{})
		calls := host.savedCalls()
		storeMeta(host, "sess-1", "france question", "/ws/a", time.Now(), 0, calls[len(calls)-1].transcript)
	}

	rec, err := host.Load("sess-1")
	if err != nil {
		t.Fatalf("load after two resumes: %v", err)
	}
	entries, err := decodeTranscript(rec.Transcript)
	if err != nil {
		t.Fatalf("decode after two resumes: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("record holds %d entries after two resumes; want the original 2 (user + assistant): %+v", len(entries), entries)
	}
	for _, e := range entries {
		if e.kind == entryNote {
			t.Errorf("record accumulated a note entry: %q", e.text)
		}
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

// A browser resume whose RestoreSession fails must leave the host's ACTIVE session untouched, so
// subsequent per-Turn saves keep targeting the original session's file rather than the just-loaded
// one. Load reads the record without activating; only a confirmed restore activates it.
func TestSessionBrowserResumeErrorLeavesActiveSessionUntouched(t *testing.T) {
	host := &fakeSessionHost{}
	host.activeID = "current" // the live conversation's file, the one saves must keep targeting
	storeMeta(host, "sess-1", "corrupt one", "/ws/a", time.Now(), 0, nil)
	eng := &fakeEngine{restoreFn: func(domain.Session) error { return errors.New("bad snapshot") }}
	m := newBrowserModel(t, eng, host, "/ws/a")
	seedConversation(&m)

	m = openBrowser(t, m)
	m, cmd := stepCmd(t, m, keyEnter())
	m = step(t, m, cmdMsg(cmd)) // fold sessionLoadedMsg → resumeLoaded, RestoreSession fails

	if host.ActiveID() != "current" {
		t.Errorf("active session after a failed restore = %q, want the untouched %q", host.ActiveID(), "current")
	}

	// A per-Turn save now still writes to the original session, not the loaded sess-1.
	m = driveOneSave(t, m, domain.Session{})
	calls := host.savedCalls()
	if len(calls) == 0 || calls[len(calls)-1].id != "current" {
		t.Errorf("post-failed-resume save id = %v, want the original 'current'", calls)
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
// The overlay pane spans the full window width (selector-popup plan §2)
// ----------------------------------------------------------------------------

// Every physical line of the open browser pane is exactly the full window width (m.width, 100 at
// the 100×30 harness window) — flush with the input box below it — so the box spans the whole
// terminal rather than stopping at the transcript's right edge or the old 72-column ceiling.
func TestSessionBrowserPaneSpansFullWidth(t *testing.T) {
	host := &fakeSessionHost{}
	storeMeta(host, "sess-1", "render me", "/ws/a", time.Now().Add(-5*time.Minute), 0, nil)
	m := newBrowserModel(t, &fakeEngine{}, host, "/ws/a")
	m = openBrowser(t, m)

	wantWidth := m.width // the full window width, matching the input box
	for i, ln := range popupLines(m.renderSessionBrowser()) {
		if w := lipgloss.Width(ln); w != wantWidth {
			t.Errorf("pane line %d is %d cells, want %d: %q", i, w, wantWidth, strip(ln))
		}
	}
}

// A session title wider than the box is truncated, not wrapped: the pane keeps exactly
// 2 borders + title + one session row + hint physical lines, and the over-wide row ends in an
// ellipsis.
func TestSessionBrowserLongTitleDoesNotWrap(t *testing.T) {
	host := &fakeSessionHost{}
	storeMeta(host, "sess-1", strings.Repeat("verylongtitle ", 12), "/ws/a", time.Now(), 0, nil)
	m := newBrowserModel(t, &fakeEngine{}, host, "/ws/a")
	m = openBrowser(t, m)

	pane := m.renderSessionBrowser()
	const wantLines = 2 + 1 + 1 + 1 // borders + title + one session row + hint
	if got := len(popupLines(pane)); got != wantLines {
		t.Fatalf("pane has %d lines, want %d (a wide title must truncate, not wrap):\n%s",
			got, wantLines, strip(pane))
	}
	if !strings.Contains(strip(pane), "…") {
		t.Errorf("the over-wide title row was not truncated to an ellipsis:\n%s", strip(pane))
	}
}

// With more than maxSessionRows sessions the pane still scrolls a window around the selection: its
// physical line count is capped at 2 borders + title + maxSessionRows + hint, never the full list.
func TestSessionBrowserWindowsLongList(t *testing.T) {
	host := &fakeSessionHost{}
	now := time.Now()
	for i := 0; i < maxSessionRows+5; i++ {
		storeMeta(host, fmt.Sprintf("sess-%02d", i), fmt.Sprintf("session %d", i), "/ws/a",
			now.Add(-time.Duration(i)*time.Minute), 0, nil)
	}
	m := newBrowserModel(t, &fakeEngine{}, host, "/ws/a")
	m = openBrowser(t, m)

	pane := m.renderSessionBrowser()
	const wantLines = 2 + 1 + maxSessionRows + 1 // borders + title + capped rows + hint
	if got := len(popupLines(pane)); got != wantLines {
		t.Errorf("pane has %d lines, want %d (a long list must window to maxSessionRows):\n%s",
			got, wantLines, strip(pane))
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

func TestSessionRowCells(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	meta := session.Meta{Title: "a task", UpdatedAt: now.Add(-5 * time.Minute), UserMsgs: 3, Workspace: "/home/me/proj"}

	// Same-workspace row in the current view: three cells, each separator leading its own cell.
	want := popupRow{"a task", "· 5m ago", "· 3 msgs"}
	if got := sessionRowCells(meta, "/home/me/proj", false, now); !reflect.DeepEqual(got, want) {
		t.Errorf("current-view cells = %v, want %v", got, want)
	}
	// Foreign row in the all view: the workspace base qualifies the TITLE, inside the title cell, so
	// the time and count columns stay where every other row put them.
	want = popupRow{"a task · proj", "· 5m ago", "· 3 msgs"}
	if got := sessionRowCells(meta, "/home/me/other", true, now); !reflect.DeepEqual(got, want) {
		t.Errorf("foreign all-view cells = %v, want the workspace base inside the title cell (%v)", got, want)
	}
	// A legacy record with no workspace reads a friendly base rather than filepath.Base's ".".
	legacy := session.Meta{Title: "old", UpdatedAt: now.Add(-time.Hour), UserMsgs: 1}
	want = popupRow{"old · unknown workspace", "· 1h ago", "· 1 msg"}
	if got := sessionRowCells(legacy, "/home/me/proj", true, now); !reflect.DeepEqual(got, want) {
		t.Errorf("legacy all-view cells = %v, want %v", got, want)
	}
}

// A stored title is untrusted DISK input — List() hands the browser whatever bytes are in the
// session file, and no codec sanitizes a Meta on the way back in — so every cell the browser builds
// from one is escape-stripped, exactly as the pickers strip the launcher's text. A title carrying
// "\x1bc" would otherwise reach the terminal as a live RIS (a full reset), the popup module
// stripping nothing and truncating ANSI-preservingly; it would also lie to the column math, an ESC
// byte taking string length but no display cell. The all-workspaces view's workspace base comes off
// the same untrusted record, so it is stripped with it.
func TestSessionRowCellsStripEscapes(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	b := sessionBrowser{
		open:          true,
		allWorkspaces: true,
		metas: []session.Meta{
			{Title: "reset \x1bc me", UpdatedAt: now.Add(-5 * time.Minute), UserMsgs: 3, Workspace: "/ws/\x1bcother"},
		},
	}

	rows := sessionRows(b, "/ws/a", now)
	want := []popupRow{{"reset c me · cother", "· 5m ago", "· 3 msgs"}}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("rows = %v, want the ESC bytes gone from the title cell (%v)", rows, want)
	}
	// The laid-out line is what the pane paints, before any styling of its own: no ESC survives into
	// it from any cell.
	for i, ln := range layoutPopupRows(rows) {
		if strings.ContainsRune(ln, 0x1b) {
			t.Errorf("rendered row %d = %q carries a raw ESC into the pane", i, ln)
		}
	}
}

// The inline rename edit is seeded from a stored title, so the seed is stripped on the way INTO the
// buffer: the rename row paints that buffer verbatim, and an ESC that vanished only at commit would
// show the human one title while saving another.
func TestSessionBrowserRenameSeedStripsEscapes(t *testing.T) {
	host := &fakeSessionHost{}
	storeMeta(host, "sess-1", "reset \x1bc me", "/ws/a", time.Now(), 0, nil)
	m := newBrowserModel(t, &fakeEngine{}, host, "/ws/a")
	m = openBrowser(t, m)

	m = step(t, m, keyRune('r'))
	if got, want := m.sessionBrowser.renameBuf, "reset c me"; got != want {
		t.Errorf("rename buffer = %q, want the seed escape-stripped (%q)", got, want)
	}
	for i, ln := range layoutPopupRows(sessionRows(m.sessionBrowser, m.opts.Workspace, time.Now())) {
		if strings.ContainsRune(ln, 0x1b) {
			t.Errorf("rendered row %d = %q carries a raw ESC into the pane", i, ln)
		}
	}
}

// Titles of different lengths do not stagger the facts beside them: every row starts its relative
// time at one shared display column and its message count at another, so the browser reads as a
// table rather than as a ragged list.
func TestSessionRowsAlignTheColumns(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	b := sessionBrowser{ // nothing armed, so every row is its plain three cells
		open: true,
		metas: []session.Meta{
			{Title: "a much longer session title", UpdatedAt: now.Add(-5 * time.Minute), UserMsgs: 3, Workspace: "/ws/a"},
			{Title: "short", UpdatedAt: now.Add(-2 * time.Hour), UserMsgs: 1, Workspace: "/ws/a"},
		},
	}

	lines := layoutPopupRows(sessionRows(b, "/ws/a", now))
	if len(lines) != 2 {
		t.Fatalf("rows = %v, want one per visible session", lines)
	}
	// The first "· " on a line opens the time cell (no title here carries one). The counts differ per
	// row — "3 msgs" against "1 msg" — so each is looked for whole; a short one is padded, not shifted.
	wantTime := ansi.StringWidth("a much longer session title") + len(popupGutter)
	wantCount := wantTime + ansi.StringWidth("· 2h ago") + len(popupGutter)
	counts := []string{"· 3 msgs", "· 1 msg"}
	for i, ln := range lines {
		if got := popupCellOffset(t, ln, "· "); got != wantTime {
			t.Errorf("row %d starts its time column at %d, want %d: %q", i, got, wantTime, ln)
		}
		if got := popupCellOffset(t, ln, counts[i]); got != wantCount {
			t.Errorf("row %d starts its count column at %d, want %d: %q", i, got, wantCount, ln)
		}
	}
}

// An armed delete confirm is a CELL past the counts, not a suffix on the last one: it lands in its
// own column and leaves the three columns before it exactly where they were unarmed.
func TestSessionRowsConfirmIsItsOwnCell(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	b := sessionBrowser{
		open:  true,
		metas: []session.Meta{{Title: "doomed", UpdatedAt: now, UserMsgs: 1, Workspace: "/ws/a"}},
	}

	unarmed := sessionRows(b, "/ws/a", now)
	b.confirming = true
	armed := sessionRows(b, "/ws/a", now)

	if len(armed) != 1 || len(armed[0]) != 4 || armed[0][3] != deleteConfirmCell {
		t.Fatalf("armed row = %v, want the confirm as a fourth cell", armed)
	}
	if !reflect.DeepEqual(popupRow(armed[0][:3]), unarmed[0]) {
		t.Errorf("arming the confirm changed the row's own cells: %v, want %v", armed[0][:3], unarmed[0])
	}
}

// An armed rename replaces the whole row with the single cell holding the edit buffer — the row
// stops describing a session, so it keeps none of its columns.
func TestSessionRowsRenameIsOneCell(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	b := sessionBrowser{
		open:      true,
		renaming:  true,
		renameBuf: "new title",
		metas:     []session.Meta{{Title: "old title", UpdatedAt: now, UserMsgs: 1, Workspace: "/ws/a"}},
	}

	rows := sessionRows(b, "/ws/a", now)
	want := popupRow{"rename: new title▏"}
	if len(rows) != 1 || !reflect.DeepEqual(rows[0], want) {
		t.Errorf("renaming row = %v, want %v", rows, want)
	}
}

// ----------------------------------------------------------------------------
// The overlay's screen budget: D2 on a short window (item 7)
// ----------------------------------------------------------------------------

// browserWithSessions builds an open /sessions overlay listing n sessions, all workspaces, so the
// pane has more rows on offer than any short window can seat.
func browserWithSessions(n int) sessionBrowser {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	metas := make([]session.Meta, 0, n)
	for i := range n {
		metas = append(metas, session.Meta{
			ID:        fmt.Sprintf("sess-%02d", i),
			Title:     fmt.Sprintf("session number %02d", i),
			Workspace: "/ws/a",
			UpdatedAt: now.Add(-time.Duration(i) * time.Minute),
			UserMsgs:  i + 1,
		})
	}
	return sessionBrowser{open: true, allWorkspaces: true, metas: metas}
}

// smallestOverlayWindow is the shortest terminal on which a boxed overlay and the frame's fixed
// chrome can both fit: the gap row, the ▔ hairline, the status line, the input box (a border row
// and one text row) and the three footer rows come to 8, and the pane's own irreducible chrome —
// two borders, the title, the hint — to 4. Below that no arrangement fits, so the frame-height
// property is asserted from here up; the transcript is what has already given way at this size.
const smallestOverlayWindow = 12

// TestFrameNeverExceedsTheTerminalHeight is the D2 property the audit measured broken: with eight
// stored sessions the browser composed a 21-row frame on a 20-row terminal, and +5 / +9 rows on 16-
// and 12-row ones — pushing the input box and the footer clean off the alternate screen, which is
// plausible in a half-height tmux pane. The pane's row budget now comes from popupBudget, whose
// floor shrinks to nothing rather than promising rows the frame has no space for, so the composed
// frame fits every window a boxed overlay can be drawn in at all.
func TestFrameNeverExceedsTheTerminalHeight(t *testing.T) {
	for _, height := range []int{smallestOverlayWindow, 13, 16, 20, 24, 30} {
		t.Run(fmt.Sprintf("%d rows", height), func(t *testing.T) {
			m := newModel(context.Background(), &fakeEngine{}, Options{Workspace: "/ws/a"}, nil)
			m = step(t, m, tea.WindowSizeMsg{Width: 80, Height: height})
			for i := range 40 {
				m.transcript.commitAssistant(fmt.Sprintf("reply line %02d", i), 0)
			}
			m.refreshViewport()
			m.sessionBrowser = browserWithSessions(8)

			frame := strings.Split(m.View().Content, "\n")
			if len(frame) > height {
				t.Errorf("composed frame is %d rows on a %d-row terminal (+%d): the input box is off-screen\n%s",
					len(frame), height, len(frame)-height, ansiPattern.ReplaceAllString(m.View().Content, ""))
			}
			// The footer is the last thing View stacks, so its presence is the proof nothing was
			// pushed past the last row.
			if last := ansiPattern.ReplaceAllString(frame[len(frame)-1], ""); strings.TrimSpace(last) == "" {
				t.Errorf("last frame row is blank, want the footer's bottom rule:\n%s",
					ansiPattern.ReplaceAllString(m.View().Content, ""))
			}
		})
	}
}

// The same property for the /model | /server picker, which shares the browser's slot and now
// shares its budget.
func TestFrameWithPickerNeverExceedsTheTerminalHeight(t *testing.T) {
	for _, height := range []int{smallestOverlayWindow, 16, 20, 24} {
		t.Run(fmt.Sprintf("%d rows", height), func(t *testing.T) {
			servers := make([]ServerChoice, 0, 12)
			for i := range 12 {
				servers = append(servers, ServerChoice{
					Name:     fmt.Sprintf("host-%02d", i),
					Endpoint: fmt.Sprintf("http://192.168.64.%d:1111", i+1),
				})
			}
			m := newModel(context.Background(), &fakeEngine{}, Options{Workspace: "/ws/a", Servers: servers}, nil)
			m = step(t, m, tea.WindowSizeMsg{Width: 80, Height: height})
			m.picker = picker{open: true, kind: pickerServer}

			if frame := strings.Split(m.View().Content, "\n"); len(frame) > height {
				t.Errorf("composed frame is %d rows on a %d-row terminal (+%d)",
					len(frame), height, len(frame)-height)
			}
		})
	}
}
