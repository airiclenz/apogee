package tui

import (
	"bytes"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/session"
)

// ----------------------------------------------------------------------------
// The progress save: the cached boundary snapshot (delegation progress-save plan §1)
// ----------------------------------------------------------------------------
//
// A progress save re-persists the record MID-Turn, pairing the engine half from the last quiescent
// boundary with the LIVE transcript (sessionsave.go). These tests prove the cache that makes the
// pairing possible: what fills it (a completed Turn's snapshot, the idle capture each worker launch
// takes, a restored record's own payload), what empties it (/clear), and that a save built from it
// carries the boundary's engine state beside entries that landed after it.

// progressNote is the distinctive scrollback line these tests add AFTER a boundary snapshot, so a
// decoded blob proves the transcript half is the live one rather than the snapshot's contemporary.
const progressNote = "the delegation is still running"

// engineOps records the order in which a fake engine's doors were called, so a test can prove that
// the boundary snapshot is taken BEFORE the Submit that opens the Turn. It is concurrency-safe
// because Submit rides the worker Cmd's goroutine.
type engineOps struct {
	mu  sync.Mutex
	ops []string
}

// record appends one door name in call order.
func (o *engineOps) record(op string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.ops = append(o.ops, op)
}

// sequence returns the recorded call order.
func (o *engineOps) sequence() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.ops...)
}

// entriesHaveNote reports whether decoded entries hold a note with the given text.
func entriesHaveNote(entries []entry, want string) bool {
	for _, e := range entries {
		if e.kind == entryNote && e.text == want {
			return true
		}
	}
	return false
}

// Before any boundary has been cached there is nothing to pair a live transcript with, so a
// progress save schedules nothing at all rather than writing a record with a zero engine half — a
// zero domain.Session is a legal envelope, which is the whole reason the cache carries a presence
// flag beside the value.
func TestProgressSaveWithoutBoundarySchedulesNothing(t *testing.T) {
	host := &fakeSessionHost{}
	m := newBrowserModel(t, &fakeEngine{}, host, "/ws/a")
	seedConversation(&m)

	cmd := m.progressSave()

	if cmd != nil {
		t.Error("a progress save with no cached boundary dispatched a write")
	}
	if n := len(m.pendingWrites); n != 0 {
		t.Errorf("pending writes = %d, want 0 — nothing may be queued either", n)
	}
	if n := len(host.savedCalls()); n != 0 {
		t.Errorf("Save calls = %d, want 0", n)
	}
}

// The pairing itself: a completed Turn's snapshot becomes the cached boundary, and a progress save
// taken later in the NEXT Turn writes that engine half beside a transcript holding everything the
// scrollback has gained since — the record a second reader opens while a delegation runs.
func TestProgressSavePairsCachedBoundaryWithLiveTranscript(t *testing.T) {
	host := &fakeSessionHost{}
	m := newBrowserModel(t, &fakeEngine{}, host, "/ws/a")
	seedConversation(&m)
	boundary := domain.Session{Version: domain.SessionVersion, State: json.RawMessage(`{"turn":1}`)}

	m, saveCmd := stepCmd(t, m, turnSnapshotMsg{Sess: boundary})
	if saveCmd == nil {
		t.Fatal("the per-Turn snapshot scheduled no save")
	}
	m = runWrites(t, m, saveCmd) // let the per-Turn save land, so the progress save dispatches
	m.transcript.addNote(progressNote)
	cmd := m.progressSave()

	if cmd == nil {
		t.Fatal("a progress save with a cached boundary scheduled nothing")
	}
	m = runWrites(t, m, cmd)
	calls := host.savedCalls()
	if len(calls) != 2 {
		t.Fatalf("Save calls = %d, want 2 (the per-Turn save and the progress save)", len(calls))
	}
	progress := calls[1]
	if !bytes.Equal(progress.sess.State, boundary.State) {
		t.Errorf("progress save's engine half = %s, want the cached boundary %s", progress.sess.State, boundary.State)
	}
	entries, err := decodeTranscript(progress.transcript)
	if err != nil {
		t.Fatalf("decode the progress save's transcript: %v", err)
	}
	if !entriesHaveNote(entries, progressNote) {
		t.Error("the progress save's transcript half is not the live one: the entry added after the boundary is missing")
	}
}

// A launch caches the boundary it launches from, which is what gives a delegation in a session's
// FIRST Turn an engine half to pair with. The ordering is the point: the snapshot is taken while the
// Update loop still owns the engine and before the Submit that rides the worker Cmd, so what is
// cached can never carry pendingInput.
func TestLaunchCachesBoundarySnapshotBeforeSubmit(t *testing.T) {
	ops := &engineOps{}
	marker := domain.Session{Version: domain.SessionVersion, State: json.RawMessage(`{"boundary":"pre-submit"}`)}
	eng := &fakeEngine{
		snapshotFn: func() (domain.Session, error) {
			ops.record("snapshot")
			return marker, nil
		},
		submitFn: func(domain.UserInput) error {
			ops.record("submit")
			return nil
		},
	}
	m := newTestModelEng(t, eng, testOpts)

	m.input.SetValue("delegate this")
	m, cmd := stepCmd(t, m, keyEnter())

	if cmd == nil {
		t.Fatal("the prompt launched no worker Cmd")
	}
	if !m.hasBoundary {
		t.Fatal("the launch cached no boundary snapshot")
	}
	if !bytes.Equal(m.boundary.State, marker.State) {
		t.Errorf("cached boundary = %s, want the engine's idle snapshot %s", m.boundary.State, marker.State)
	}
	// The Submit rides the worker Cmd (not run here), so it has not happened yet — which is exactly
	// what proves the snapshot-then-submit ordering.
	if got := ops.sequence(); len(got) != 1 || got[0] != "snapshot" {
		t.Errorf("engine calls = %v, want the snapshot alone (the Submit comes later, on the worker)", got)
	}
}

// /clear closes one session and opens another, so the boundary cached for the outgoing conversation
// must not survive it: pairing it with the fresh session's transcript would file a record whose two
// halves describe different conversations.
func TestStartNewSessionClearsCachedBoundarySnapshot(t *testing.T) {
	host := &fakeSessionHost{}
	m := newBrowserModel(t, &fakeEngine{}, host, "/ws/a")
	seedConversation(&m)
	m = step(t, m, turnSnapshotMsg{Sess: domain.Session{Version: domain.SessionVersion, State: json.RawMessage(`{"turn":1}`)}})
	if !m.hasBoundary {
		t.Fatal("the per-Turn snapshot cached no boundary to clear")
	}

	m.input.SetValue("/clear")
	m = step(t, m, keyEnter())

	if m.hasBoundary {
		t.Error("the closed session's boundary survived /clear")
	}
	if len(m.boundary.State) != 0 {
		t.Errorf("cleared boundary still carries engine state %s", m.boundary.State)
	}
}

// A restore puts the record's own payload into the engine at the boundary it was stored at, so that
// payload IS the resumed session's boundary — cached from the record already in hand, so that a
// delegation in the resumed session's first Turn has an engine half to pair with.
func TestRestoreCachesBoundarySnapshot(t *testing.T) {
	var src transcript
	src.addUser("what is the capital of france", nil)
	blob, err := encodeTranscript(&src)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	stored := domain.Session{Version: domain.SessionVersion, State: json.RawMessage(`{"restored":true}`)}
	host := &fakeSessionHost{}
	host.seed(session.Record{
		Meta: session.Meta{
			ID: "sess-1", Title: "france question", Workspace: "/ws/a",
			UpdatedAt: time.Now(), UserMsgs: 1,
		},
		Transcript: blob,
		Session:    stored,
	})
	m := newBrowserModel(t, &fakeEngine{}, host, "/ws/a")
	seedConversation(&m)
	m = openBrowser(t, m)

	m, loadCmd := stepCmd(t, m, keyEnter())
	if loadCmd == nil {
		t.Fatal("enter dispatched no Load Cmd")
	}
	m = foldResume(t, m, loadCmd)

	if !m.hasBoundary {
		t.Fatal("the restore cached no boundary snapshot")
	}
	if !bytes.Equal(m.boundary.State, stored.State) {
		t.Errorf("cached boundary = %s, want the restored record's payload %s", m.boundary.State, stored.State)
	}
	if cmd := m.progressSave(); cmd == nil {
		t.Error("a progress save in the resumed session scheduled nothing")
	}
}
