package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/snapshot"
)

func TestBuildAgentResumeRoundTrip(t *testing.T) {
	t.Parallel()
	// Snapshot a fresh Agent and resume off the record's Session (buildAgent no longer reads
	// files — resolveResume owns the id-or-path lookup, exercised separately below).
	original, err := apogee.New(validCfg(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = original.Close() })

	snap, err := original.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	resumed, err := buildAgent(validCfg(t), &session.Record{Session: snap})
	if err != nil {
		t.Fatalf("buildAgent resume: %v", err)
	}
	if resumed == nil {
		t.Fatal("buildAgent resume returned a nil Agent")
	}
	t.Cleanup(func() { _ = resumed.Close() })
}

// The TUI-side save round-trips through --resume: a record persisted by the same host the binary
// installs (sessionHost over a session.Store) resolves back by its minted id and reconstructs an
// Agent via buildAgent — the save↔resume acceptance, exercised without a terminal (P2.6 drives it
// live).
func TestSessionHostRoundTripsThroughResume(t *testing.T) {
	t.Parallel()
	original, err := apogee.New(validCfg(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = original.Close() })

	snap, err := original.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
	host := newSessionHost(store, t.TempDir(), "fake", nil, "", nil, "", nil)
	if err := host.Save(snap, nil, "hi", 1, 0, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	id := host.ActiveID()
	if id == "" {
		t.Fatal("host minted no id after a successful Save")
	}
	// The first run ends before the second resumes its record: a live host holds the session, and
	// the resume door would refuse it (TestResolveResumeRefusesAHeldSessionWithTheFriendlyLine).
	host.Close()

	rec, err := resolveResume(store, id, false, "")
	if err != nil {
		t.Fatalf("resolveResume by id: %v", err)
	}
	resumed, err := buildAgent(validCfg(t), rec)
	if err != nil {
		t.Fatalf("buildAgent resume of the saved session: %v", err)
	}
	if resumed == nil {
		t.Fatal("buildAgent resume returned a nil Agent")
	}
	t.Cleanup(func() { _ = resumed.Close() })
}

func TestResolveResumeMissingArg(t *testing.T) {
	t.Parallel()
	store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
	_, err := resolveResume(store, filepath.Join(t.TempDir(), "absent.json"), false, "")
	if err == nil {
		t.Fatal("resolveResume of a value that is neither an id nor a file: want error, got nil")
	}
}

func TestBuildAgentResumeFutureVersion(t *testing.T) {
	t.Parallel()
	// A session stamped with a version newer than this build understands must surface
	// ErrSessionVersion (a clear message), not panic. resolveResume wraps the legacy bare
	// envelope happily; the version check bites at Resume, inside buildAgent.
	path := filepath.Join(t.TempDir(), "future.json")
	const futureVersionPayload = `{"Version":9999,"State":null}`
	if err := os.WriteFile(path, []byte(futureVersionPayload), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
	rec, err := resolveResume(store, path, false, "")
	if err != nil {
		t.Fatalf("resolveResume of a future-version file: %v", err)
	}
	_, err = buildAgent(validCfg(t), rec)
	if !errors.Is(err, apogee.ErrSessionVersion) {
		t.Fatalf("buildAgent resume of a future version: err = %v; want ErrSessionVersion", err)
	}
}

// ----------------------------------------------------------------------------
// The store-backed session host and the resume resolution (item 5)
// ----------------------------------------------------------------------------

// The host mints an id on the first Save and updates that same file thereafter, never overwriting
// the create-time title, and stamps the wiring facts (workspace, model) the renderer cannot know.
func TestSessionHostMintsIDOnceAndUpdatesInPlace(t *testing.T) {
	t.Parallel()
	store := session.NewStore(t.TempDir())
	host := newSessionHost(store, "/ws", "model-x", nil, "", nil, "", nil)

	if host.ActiveID() != "" {
		t.Errorf("ActiveID before any Save = %q; want empty", host.ActiveID())
	}
	if err := host.Save(apogee.Session{}, nil, "first title", 1, 100, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("Save #1: %v", err)
	}
	id := host.ActiveID()
	if id == "" {
		t.Fatal("Save minted no id")
	}
	// A second Save keeps the same id (update-in-place) and never overwrites the create-time title.
	if err := host.Save(apogee.Session{}, nil, "SECOND title", 2, 200, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("Save #2: %v", err)
	}
	if host.ActiveID() != id {
		t.Errorf("ActiveID after the second Save = %q; want the same minted id %q", host.ActiveID(), id)
	}
	metas, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("two Saves produced %d files; want 1 (update-in-place)", len(metas))
	}
	m := metas[0]
	if m.Title != "first title" {
		t.Errorf("Title = %q; want the create-time title (a later Save must not overwrite it)", m.Title)
	}
	if m.Workspace != "/ws" || m.Model != "model-x" {
		t.Errorf("Meta workspace/model = %q/%q; want /ws / model-x from the wiring", m.Workspace, m.Model)
	}
	if m.UserMsgs != 2 || m.CtxUsed != 200 {
		t.Errorf("Meta counts = msgs %d, ctx %d; want the latest Save's 2 / 200", m.UserMsgs, m.CtxUsed)
	}
}

// A heartbeat rebind moves the session's model mid-conversation, and the stored metadata has to
// follow it: a session that started model-less (the async cold start) or switched models upstream
// must be listed under what its Turns actually ran against, not under a launch-time value that was
// never true. SetModel restamps subsequent Saves in place — it does not rewrite history, because
// the record IS the session and its current model is the session's current truth.
func TestSessionHostSetModelStampsSaves(t *testing.T) {
	t.Parallel()
	store := session.NewStore(t.TempDir())
	host := newSessionHost(store, "/ws", "", nil, "", nil, "", nil) // a cold start: nothing bound yet

	if err := host.Save(apogee.Session{}, nil, "cold", 1, 0, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("Save before the bind: %v", err)
	}
	host.SetModel("bound-model")
	if err := host.Save(apogee.Session{}, nil, "cold", 2, 0, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("Save after the bind: %v", err)
	}

	metas, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("two Saves produced %d files; want 1 (update-in-place)", len(metas))
	}
	if metas[0].Model != "bound-model" {
		t.Errorf("Meta.Model = %q; want %q — the save after SetModel must carry the rebound model",
			metas[0].Model, "bound-model")
	}
}

// Rotate closes the active session so the next Save mints a fresh id; Load reads a stored session
// without touching the active one, and Activate then makes it the target of subsequent Saves so
// they update ITS file rather than forking a new one.
func TestSessionHostRotateAndLoadActivate(t *testing.T) {
	t.Parallel()
	store := session.NewStore(t.TempDir())
	host := newSessionHost(store, "/ws", "m", nil, "", nil, "", nil)

	if err := host.Save(apogee.Session{}, nil, "A", 1, 0, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("Save A: %v", err)
	}
	first := host.ActiveID()

	host.Rotate()
	if host.ActiveID() != "" {
		t.Errorf("ActiveID after Rotate = %q; want empty", host.ActiveID())
	}
	if err := host.Save(apogee.Session{}, nil, "B", 1, 0, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("Save B: %v", err)
	}
	second := host.ActiveID()
	if second == first || second == "" {
		t.Errorf("Save after Rotate minted %q; want a fresh id different from %q", second, first)
	}

	// Loading the first session reads it without activating; Activate then makes it current again,
	// so the next Save updates its file, not B's.
	rec, err := host.Load(first)
	if err != nil {
		t.Fatalf("Load(first): %v", err)
	}
	if rec.Meta.ID != first {
		t.Errorf("Load returned rec id %q, want %q", rec.Meta.ID, first)
	}
	if host.ActiveID() != second {
		t.Errorf("Load changed the active session to %q; it must leave %q active until Activate", host.ActiveID(), second)
	}
	host.Activate(rec.Meta)
	if host.ActiveID() != first {
		t.Errorf("Activate did not make %q current (active %q)", first, host.ActiveID())
	}
	if err := host.Save(apogee.Session{}, nil, "ignored", 3, 0, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("Save after Load: %v", err)
	}
	if metas, _ := store.List(); len(metas) != 2 {
		t.Fatalf("after Save/Rotate/Save/Load/Save there are %d sessions; want 2", len(metas))
	}
}

// A rename of the ACTIVE session sticks: the next Save preserves the new title rather than
// reverting to the create-time one.
func TestSessionHostRenameActiveSticks(t *testing.T) {
	t.Parallel()
	store := session.NewStore(t.TempDir())
	host := newSessionHost(store, "/ws", "m", nil, "", nil, "", nil)
	if err := host.Save(apogee.Session{}, nil, "original", 1, 0, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	id := host.ActiveID()
	if err := host.Rename(id, "renamed"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if err := host.Save(apogee.Session{}, nil, "original", 2, 0, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("Save after Rename: %v", err)
	}
	rec, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if rec.Meta.Title != "renamed" {
		t.Errorf("Title after rename+Save = %q; want the renamed title to stick", rec.Meta.Title)
	}
}

// A host seeded from a resumed record begins ACTIVE on it — same id, preserved title — so the run
// continues that file rather than forking a new session.
func TestSessionHostResumeBeginsActive(t *testing.T) {
	t.Parallel()
	store := session.NewStore(t.TempDir())
	seed := &session.Record{Meta: session.Meta{ID: "20260724T120000Z-abcd", Title: "kept"}}
	host := newSessionHost(store, "/ws", "m", seed, "", nil, "", nil)

	if host.ActiveID() != seed.Meta.ID {
		t.Errorf("ActiveID of a resumed host = %q; want the resumed id %q", host.ActiveID(), seed.Meta.ID)
	}
	if err := host.Save(apogee.Session{}, nil, "derived", 1, 0, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	rec, err := store.Load(seed.Meta.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if rec.Meta.Title != "kept" {
		t.Errorf("Title after a resumed Save = %q; want the resumed title preserved", rec.Meta.Title)
	}
}

// ----------------------------------------------------------------------------
// Fork: the child record and the parent id it carries through every save
// ----------------------------------------------------------------------------

// forkPrefix is the scrollback prefix the fork tests hand the host: two top-level prompts and one a
// delegate asked at depth 1, so a count by Save's rule (every user entry, depth included) is 3
// where a depth-0 count would be 2.
func forkPrefix() []session.Entry {
	return []session.Entry{
		{Kind: session.EntryKindUser, Text: "first"},
		{Kind: session.EntryKindAssistant, Text: "one", Done: true},
		{Kind: session.EntryKindUser, Text: "second"},
		{Kind: session.EntryKindUser, Text: "a delegate's brief", Depth: 1},
		{Kind: session.EntryKindAssistant, Text: "two", Done: true},
	}
}

// Fork writes a NEW record — the child loads back under its own id with ParentID naming the active
// session, the transcript and Session it was handed, the wiring facts, and UserMsgs counted by
// Save's rule — and leaves the parent's file byte-for-byte as it was: a fork is a write beside the
// active record, never of it.
func TestSessionHostForkWritesAChildRecord(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := session.NewStore(dir)
	host := newSessionHost(store, "/ws", "m", nil, "", nil, "", nil)
	if err := host.Save(apogee.Session{}, nil, "parent", 2, 0, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("Save parent: %v", err)
	}
	parentID := host.ActiveID()
	parentPath := filepath.Join(dir, parentID+".json")
	before, err := os.ReadFile(parentPath)
	if err != nil {
		t.Fatalf("read the parent's file: %v", err)
	}
	cut := apogee.Session{Version: domain.SessionVersion, State: json.RawMessage(`{"cut":true}`)}

	child, err := host.Fork(session.Meta{ID: parentID}, cut, forkPrefix(), "parent")
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}

	if child.ID == "" || child.ID == parentID {
		t.Fatalf("child id = %q; want a fresh id distinct from the parent's %q", child.ID, parentID)
	}
	if child.ParentID != parentID {
		t.Errorf("returned ParentID = %q; want the parent's id %q", child.ParentID, parentID)
	}
	if host.ActiveID() != parentID {
		t.Errorf("Fork activated %q; the parent %q must stay the active session", host.ActiveID(), parentID)
	}
	rec, err := store.Load(child.ID)
	if err != nil {
		t.Fatalf("Load the child: %v", err)
	}
	if rec.Meta.ParentID != parentID {
		t.Errorf("stored ParentID = %q; want %q", rec.Meta.ParentID, parentID)
	}
	if rec.Meta.Title != "parent" || rec.Meta.Workspace != "/ws" || rec.Meta.Model != "m" {
		t.Errorf("stored child Meta = %+v; want title \"parent\", workspace /ws, model m", rec.Meta)
	}
	if rec.Meta.UserMsgs != 3 {
		t.Errorf("stored UserMsgs = %d; want 3 (every user entry, the depth-1 one included)", rec.Meta.UserMsgs)
	}
	if rec.Meta.CreatedAt.IsZero() || !rec.Meta.UpdatedAt.Equal(rec.Meta.CreatedAt) {
		t.Errorf("stored CreatedAt/UpdatedAt = %v/%v; want both set to the fork's moment", rec.Meta.CreatedAt, rec.Meta.UpdatedAt)
	}
	if !bytes.Equal(rec.Session.State, cut.State) {
		t.Errorf("stored Session.State = %s; want the cut state %s", rec.Session.State, cut.State)
	}
	entries, err := session.DecodeTranscript(rec.Transcript)
	if err != nil {
		t.Fatalf("decode the child's transcript: %v", err)
	}
	if len(entries) != len(forkPrefix()) || entries[3].Depth != 1 {
		t.Errorf("stored transcript = %+v; want the prefix as handed over", entries)
	}
	after, err := os.ReadFile(parentPath)
	if err != nil {
		t.Fatalf("re-read the parent's file: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("the fork rewrote the parent's file:\nbefore: %s\nafter:  %s", before, after)
	}
}

// The parent id is the host's OWN identity: a fresh host whose first Save has not landed still
// forks a child whose ParentID is the id it pre-minted for that Save — not the "" the renderer
// reads from ActiveID in that window.
func TestForkStampsParentIDFromTheHostIdentity(t *testing.T) {
	t.Parallel()
	store := session.NewStore(t.TempDir())
	host := newSessionHost(store, "/ws", "m", nil, "", nil, "", nil)
	preMinted := host.SessionID()
	if preMinted == "" || host.ActiveID() != "" {
		t.Fatalf("pre-minted id %q, active %q; want a pre-minted id and no active session", preMinted, host.ActiveID())
	}

	child, err := host.Fork(session.Meta{ID: ""}, apogee.Session{}, forkPrefix(), "t")
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}

	if child.ParentID != preMinted {
		t.Errorf("ParentID = %q; want the pre-minted id %q, not what the parent Meta carried", child.ParentID, preMinted)
	}
	if host.SessionID() != preMinted {
		t.Errorf("Fork moved the host's own identity to %q; want %q unchanged", host.SessionID(), preMinted)
	}
}

// Activating a forked child carries its ParentID into the identity later Saves rebuild Meta from,
// so the first Save after a resume of the child does not write "" over the pointer the fork wrote.
func TestActivateCarriesParentIDIntoLaterSaves(t *testing.T) {
	t.Parallel()
	store := session.NewStore(t.TempDir())
	host := newSessionHost(store, "/ws", "m", nil, "", nil, "", nil)
	if err := host.Save(apogee.Session{}, nil, "parent", 1, 0, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("Save parent: %v", err)
	}
	parentID := host.ActiveID()
	child, err := host.Fork(session.Meta{ID: parentID}, apogee.Session{}, forkPrefix(), "parent")
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}

	host.Activate(child)
	if err := host.Save(apogee.Session{}, nil, "parent", 4, 0, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("Save the child: %v", err)
	}

	rec, err := store.Load(child.ID)
	if err != nil {
		t.Fatalf("Load the child: %v", err)
	}
	if rec.Meta.ParentID != parentID {
		t.Errorf("ParentID after Activate+Save = %q; want %q kept", rec.Meta.ParentID, parentID)
	}
	if rec.Meta.UserMsgs != 4 {
		t.Errorf("UserMsgs after the child's Save = %d; want 4 (the Save's own count)", rec.Meta.UserMsgs)
	}
}

// A --resume start on a forked child (newSessionHost's resumed branch) carries its ParentID too:
// the next Save keeps the pointer rather than forgetting the fork.
func TestResumeCarriesParentIDIntoLaterSaves(t *testing.T) {
	t.Parallel()
	store := session.NewStore(t.TempDir())
	child := &session.Record{Meta: session.Meta{
		ID: "20260916T120000Z-abcd", Title: "kept", ParentID: "20260916T110000Z-0000",
	}}
	host := newSessionHost(store, "/ws", "m", child, "", nil, "", nil)

	if err := host.Save(apogee.Session{}, nil, "derived", 1, 0, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}

	rec, err := store.Load(child.Meta.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if rec.Meta.ParentID != child.Meta.ParentID {
		t.Errorf("ParentID after a resumed Save = %q; want the resumed %q", rec.Meta.ParentID, child.Meta.ParentID)
	}
}

// Rotate closes the forked child with everything else about its identity: the session the next
// Save mints is not a fork, so it carries no ParentID.
func TestRotateClearsParentID(t *testing.T) {
	t.Parallel()
	store := session.NewStore(t.TempDir())
	child := &session.Record{Meta: session.Meta{
		ID: "20260916T120000Z-abcd", Title: "kept", ParentID: "20260916T110000Z-0000",
	}}
	host := newSessionHost(store, "/ws", "m", child, "", nil, "", nil)

	host.Rotate()
	if err := host.Save(apogee.Session{}, nil, "fresh", 1, 0, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("Save after Rotate: %v", err)
	}

	fresh := host.ActiveID()
	if fresh == child.Meta.ID {
		t.Fatalf("Save after Rotate updated the child %q; want a fresh session", fresh)
	}
	rec, err := store.Load(fresh)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if rec.Meta.ParentID != "" {
		t.Errorf("ParentID of the session after Rotate = %q; want none", rec.Meta.ParentID)
	}
}

// --resume accepts a raw file path (not only a store id), including a pre-plan bare envelope, which
// resumes with no recorded scrollback — the replay payload carries the empty blob through so the
// TUI degrades to an honest note.
func TestResolveResumeLegacyPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "old.json")
	if err := os.WriteFile(legacyPath, []byte(`{"Version":1,"State":null}`), 0o600); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	store := session.NewStore(filepath.Join(dir, "sessions"))
	rec, err := resolveResume(store, legacyPath, false, "")
	if err != nil {
		t.Fatalf("resolveResume by path: %v", err)
	}
	if rec == nil {
		t.Fatal("resolveResume returned nil for a readable legacy file")
	}
	if len(rec.Transcript) != 0 {
		t.Errorf("legacy Transcript = %s; want empty (no scrollback recorded)", rec.Transcript)
	}
	if rs := resumedSession(rec, false); rs == nil || len(rs.Transcript) != 0 {
		t.Errorf("resumedSession(legacy) = %+v; want a non-nil payload with an empty transcript", rs)
	}
}

// A record resumed from an explicit PATH is adopted with a FRESH id: the file's declared id is
// content, not identity, so a planted record claiming another session's id must not make the run's
// autosaves overwrite that session. Resuming by id (the /sessions handle) still continues in place
// — TestSessionHostRoundTripsThroughResume and TestSessionHostResumeBeginsActive pin that half.
func TestResolveResumeByPathRemintsID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := session.NewStore(filepath.Join(dir, "sessions"))

	// The victim: a real session of this store, with its own transcript.
	victimID := saveAt(t, store, "/ws", time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC), "victim")

	// The planted file: a readable record that CLAIMS the victim's id.
	planted := session.Record{
		RecordVersion: session.RecordVersion,
		Meta:          session.Meta{ID: victimID, Title: "planted", Workspace: "/elsewhere"},
	}
	data, err := json.Marshal(planted)
	if err != nil {
		t.Fatalf("marshal planted: %v", err)
	}
	plantedPath := filepath.Join(dir, "planted.json")
	if err := os.WriteFile(plantedPath, data, 0o600); err != nil {
		t.Fatalf("write planted: %v", err)
	}

	rec, err := resolveResume(store, plantedPath, false, "/ws")
	if err != nil {
		t.Fatalf("resolveResume by path: %v", err)
	}
	if rec.Meta.ID == victimID {
		t.Fatalf("path resume adopted the file's declared id %q; want a freshly minted one", victimID)
	}
	if rec.Meta.Title != "planted" {
		t.Errorf("path resume Title = %q; want the record's own title carried over", rec.Meta.Title)
	}

	// The run continues as a NEW session: its autosave lands on its own file and the victim's
	// record is untouched.
	host := newSessionHost(store, "/ws", "m", rec, "", nil, "", nil)
	if host.ActiveID() != rec.Meta.ID {
		t.Errorf("host active id = %q; want the re-minted %q", host.ActiveID(), rec.Meta.ID)
	}
	if err := host.Save(apogee.Session{}, nil, "continued", 1, 0, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("Save after a path resume: %v", err)
	}
	got, err := store.Load(victimID)
	if err != nil {
		t.Fatalf("Load victim: %v", err)
	}
	if got.Meta.Title != "victim" {
		t.Errorf("the victim record was overwritten by the path-resumed session: title = %q", got.Meta.Title)
	}
	if metas, err := store.List(); err != nil || len(metas) != 2 {
		t.Errorf("store holds %d sessions (err %v); want 2 — the victim plus the re-minted one", len(metas), err)
	}
}

// A record whose declared id is not a filename — a traversal planted in a repo's session file — is
// refused outright at load, so --resume of it never starts a run whose autosaves write there.
func TestResolveResumeRejectsTraversalRecordID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	plantedPath := filepath.Join(dir, "planted.json")
	planted := fmt.Sprintf(
		`{"recordVersion":%d,"meta":{"id":"../../.claude/settings"},"session":{"Version":1,"State":null}}`,
		session.RecordVersion)
	if err := os.WriteFile(plantedPath, []byte(planted), 0o600); err != nil {
		t.Fatalf("write planted: %v", err)
	}
	store := session.NewStore(filepath.Join(dir, "sessions"))
	if _, err := resolveResume(store, plantedPath, false, "/ws"); err == nil {
		t.Fatal("resolveResume of a record declaring a traversal id: want an error, got nil")
	}
}

// --continue resumes this workspace's most recent session (skipping newer sessions in other
// workspaces) and errors helpfully when the workspace has none.
func TestResolveContinuePicksWorkspaceNewest(t *testing.T) {
	t.Parallel()
	store := session.NewStore(t.TempDir())
	base := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	saveAt(t, store, "/a", base, "a-old")
	newestA := saveAt(t, store, "/a", base.Add(2*time.Hour), "a-new")
	saveAt(t, store, "/b", base.Add(3*time.Hour), "b-newest") // newer overall, but wrong workspace

	rec, err := resolveContinue(store, "/a")
	if err != nil {
		t.Fatalf("resolveContinue(/a): %v", err)
	}
	if rec.Meta.ID != newestA {
		t.Errorf("continue picked %q (%q); want /a's newest %q", rec.Meta.Title, rec.Meta.ID, newestA)
	}

	// A workspace with no sessions of its own is a friendly error, even though the store is non-empty.
	if _, err := resolveContinue(store, "/c"); err == nil {
		t.Error("resolveContinue(/c) with no sessions for that workspace: want an error")
	}
}

// saveAt persists one fresh session in workspace ws stamped at when (controlling both its id and
// UpdatedAt), returning the minted id. Each call uses its own host so it mints a distinct session, and
// closes that host once the record is down: the record stands for a run that has ENDED, free for the
// test's own resume, prune or delete to take — a live host would hold it against them.
func saveAt(t *testing.T, store *session.Store, ws string, when time.Time, title string) string {
	t.Helper()
	h := newSessionHost(store, ws, "m", nil, "", nil, "", nil)
	h.now = func() time.Time { return when }
	if err := h.Save(apogee.Session{}, nil, title, 1, 0, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("saveAt %q: %v", title, err)
	}
	h.Close()
	return h.ActiveID()
}

// --resume and --continue are mutually exclusive at the resolution seam (the runRoot-testable
// guard mirroring the cobra flag marker).
func TestResolveResumeMutuallyExclusive(t *testing.T) {
	t.Parallel()
	store := session.NewStore(t.TempDir())
	_, err := resolveResume(store, "some-id", true, "/ws")
	if err == nil {
		t.Fatal("resolveResume with both --resume and --continue: want a flag error")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error = %q; want it to mention mutual exclusion", err)
	}
}

// The host stores the two token accountings apart, exactly as the renderer hands them over, and the
// resume projection carries both back: what the main agent spent and what its delegates did. The
// halves stay separate on disk because the session total is their sum, and a store that folded them
// together could never say which was which again (session.Meta). The models that answered ride the
// same save, beside the bound Model rather than in its place — the record keeps what was asked for
// and what answered as two facts — and come back in the replay payload so the reopened session's
// first save does not drop them.
func TestSessionHostStoresBothTokenAccountings(t *testing.T) {
	t.Parallel()
	store := session.NewStore(t.TempDir())
	host := newSessionHost(store, "/ws", "model-x", nil, "", nil, "", nil)

	main := session.Usage{Calls: 4, PromptTokens: 60000, CachedPromptTokens: 12000, TotalTokens: 64000}
	delegates := session.Usage{Calls: 300, PromptTokens: 900000, TotalTokens: 936000}
	served := []string{"model-x-q4", "grunt-8b"}
	if err := host.Save(apogee.Session{}, nil, "delegating run", 1, 100, main, delegates, served); err != nil {
		t.Fatalf("Save: %v", err)
	}

	rec, err := store.Load(host.ActiveID())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if rec.Meta.Usage != main {
		t.Errorf("stored usage = %+v; want the main agent's own %+v", rec.Meta.Usage, main)
	}
	if rec.Meta.DelegateUsage != delegates {
		t.Errorf("stored delegate usage = %+v; want %+v", rec.Meta.DelegateUsage, delegates)
	}
	if !slices.Equal(rec.Meta.ServedModels, served) {
		t.Errorf("stored served models = %q; want %q, in the order the renderer handed them over", rec.Meta.ServedModels, served)
	}
	if rec.Meta.Model != "model-x" {
		t.Errorf("stored model = %q; want the bound profile model-x — the served set does not displace it", rec.Meta.Model)
	}
	rs := resumedSession(&rec, false)
	if rs == nil || rs.Usage != main || rs.DelegateUsage != delegates {
		t.Errorf("resumedSession = %+v; want both accountings carried into the replay payload", rs)
	}
	if rs == nil || !slices.Equal(rs.ServedModels, served) {
		t.Errorf("resumedSession served models = %v; want %q carried into the replay payload", rs, served)
	}
}

// A fresh start (neither flag set) resolves to no record and projects to a nil replay payload.
func TestResolveResumeFreshStart(t *testing.T) {
	t.Parallel()
	store := session.NewStore(t.TempDir())
	rec, err := resolveResume(store, "", false, "/ws")
	if err != nil {
		t.Fatalf("resolveResume fresh: %v", err)
	}
	if rec != nil {
		t.Errorf("resolveResume with neither flag = %+v; want nil", rec)
	}
	if got := resumedSession(nil, false); got != nil {
		t.Errorf("resumedSession(nil) = %+v; want nil (a fresh start replays nothing)", got)
	}
}

// ----------------------------------------------------------------------------
// Session scratch dirs (workspace-clobber hardening, 2026-08-22)
// ----------------------------------------------------------------------------

// TestGCScratchDirsRemovesOldKeepsFresh pins the startup sweep's one rule: an entry whose mtime
// has aged past scratchMaxAge goes, one inside the window stays — and a root that does not exist
// is silently nothing to do.
func TestGCScratchDirsRemovesOldKeepsFresh(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	now := time.Now()

	old := filepath.Join(root, "2026-01-01T00-00-00-old1")
	fresh := filepath.Join(root, "2026-08-22T00-00-00-new1")
	for _, dir := range []string{old, fresh} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("MkdirAll(%s): %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(old, "scratch.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	backdated := now.Add(-scratchMaxAge - time.Hour)
	if err := os.Chtimes(old, backdated, backdated); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	gcScratchDirs(root, now)

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("stale scratch dir survived the sweep (stat err = %v)", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh scratch dir did not survive the sweep: %v", err)
	}

	gcScratchDirs(filepath.Join(root, "does-not-exist"), now) // must not panic or create anything
}

// TestSessionHostScratchFollowsTheActiveSession proves the scratch seam tracks session identity
// end to end: the boot dir exists before any Save (the pre-minted id), a Rotate mints a NEW
// session and moves the engine's scratch to its dir, an Activate moves it to the resumed
// session's, and the id the first Save adopts is the one the boot scratch dir was named by — so
// dir and record never disagree.
func TestSessionHostScratchFollowsTheActiveSession(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
	var moved []string
	host := newSessionHost(store, "/ws", "m", nil, root, func(dir string) { moved = append(moved, dir) }, "", nil)

	bootDir := host.SessionScratchDir()
	if bootDir == "" {
		t.Fatal("SessionScratchDir answered \"\" on a scratch-enabled host")
	}
	if info, err := os.Stat(bootDir); err != nil || !info.IsDir() {
		t.Fatalf("boot scratch dir %s not created: %v", bootDir, err)
	}

	// The first Save adopts the pre-minted id — the name the boot dir already carries.
	if err := host.Save(apogee.Session{}, nil, "t", 1, 0, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got, want := host.ActiveID(), filepath.Base(bootDir); got != want {
		t.Errorf("first Save minted id %q, want the boot scratch dir's name %q", got, want)
	}

	host.Rotate()
	if len(moved) != 1 {
		t.Fatalf("Rotate pushed %d scratch moves, want 1", len(moved))
	}
	if moved[0] == bootDir || filepath.Dir(moved[0]) != root {
		t.Errorf("Rotate moved scratch to %q, want a NEW dir under %q", moved[0], root)
	}
	if info, err := os.Stat(moved[0]); err != nil || !info.IsDir() {
		t.Errorf("rotated scratch dir %s not created: %v", moved[0], err)
	}

	// A /sessions resume: scratch follows the ACTIVATED session's own id.
	host.Activate(session.Meta{ID: filepath.Base(bootDir)})
	if len(moved) != 2 || moved[1] != bootDir {
		t.Fatalf("Activate pushed moves %v, want the resumed session's dir %q last", moved, bootDir)
	}
}

// TestSessionHostWithoutScratchRootIsInert pins the disabled seam: no root means no dirs, no
// listener calls, and the pre-scratch behaviour everywhere else.
func TestSessionHostWithoutScratchRootIsInert(t *testing.T) {
	t.Parallel()
	store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
	called := false
	host := newSessionHost(store, "/ws", "m", nil, "", func(string) { called = true }, "", nil)

	if dir := host.SessionScratchDir(); dir != "" {
		t.Errorf("SessionScratchDir = %q on a disabled seam, want \"\"", dir)
	}
	host.Rotate()
	host.Activate(session.Meta{ID: "some-id"})
	if called {
		t.Error("scratchMoved called on a host with no scratch root")
	}
}

// ----------------------------------------------------------------------------
// The boot sweep (session retention)
// ----------------------------------------------------------------------------

// The configured policy, applied to the store the way a boot applies it: an age rule discards what
// is older than the cut, a count rule keeps the newest N, and a config that names neither knob
// leaves every record exactly where it was — retention is opt-in, so the unconfigured default is
// still the store apogee has always kept.
func TestGCSessionsAppliesTheConfiguredPolicy(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()

	titles := func(t *testing.T, store *session.Store) []string {
		t.Helper()
		metas, err := store.List()
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		var out []string
		for _, m := range metas {
			out = append(out, m.Title)
		}
		return out
	}

	t.Run("age", func(t *testing.T) {
		t.Parallel()
		store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
		saveAt(t, store, "/ws", now.Add(-100*time.Hour), "stale")
		saveAt(t, store, "/ws", now.Add(-time.Hour), "recent")

		gcSessions(store, config.SessionSettings{MaxAge: 48 * time.Hour})

		if got := titles(t, store); !slices.Equal(got, []string{"recent"}) {
			t.Errorf("after a 48h sweep the store holds %v; want only the recent record", got)
		}
	})

	t.Run("count", func(t *testing.T) {
		t.Parallel()
		store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
		saveAt(t, store, "/ws", now.Add(-3*time.Hour), "oldest")
		saveAt(t, store, "/ws", now.Add(-2*time.Hour), "middle")
		saveAt(t, store, "/ws", now.Add(-time.Hour), "newest")

		gcSessions(store, config.SessionSettings{MaxCount: 2})

		if got := titles(t, store); !slices.Equal(got, []string{"newest", "middle"}) {
			t.Errorf("after a max-count 2 sweep the store holds %v; want the two newest", got)
		}
	})

	t.Run("both knobs absent", func(t *testing.T) {
		t.Parallel()
		store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
		saveAt(t, store, "/ws", now.Add(-10000*time.Hour), "ancient")
		saveAt(t, store, "/ws", now.Add(-time.Hour), "recent")

		gcSessions(store, config.SessionSettings{})

		if got := titles(t, store); len(got) != 2 {
			t.Errorf("an unconfigured sweep left %v; want every record kept", got)
		}
	})

	t.Run("the kept id survives", func(t *testing.T) {
		t.Parallel()
		store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
		stale := saveAt(t, store, "/ws", now.Add(-100*time.Hour), "stale but open")
		saveAt(t, store, "/ws", now.Add(-time.Hour), "recent")

		gcSessions(store, config.SessionSettings{MaxAge: 48 * time.Hour, MaxCount: 1}, stale)

		if _, err := store.Load(stale); err != nil {
			t.Errorf("the id passed as active was swept out from under the run: %v", err)
		}
	})

	t.Run("no store directory", func(t *testing.T) {
		t.Parallel()
		root := filepath.Join(t.TempDir(), "sessions")
		// A first-ever start sweeps a directory that does not exist yet: silent, and it creates
		// nothing on the way past.
		gcSessions(session.NewStore(root), config.SessionSettings{MaxAge: time.Hour, MaxCount: 1})
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Errorf("the sweep touched a missing sessions root (stat err = %v)", err)
		}
	})
}

// The ORDERING, on the real boot path: a `--continue` target that does not rank inside `max-count`
// store-wide still resumes, because the sweep runs after resolveResume and is handed the resolved
// record's id to keep. Driven through wireSession rather than through resolveResume and the sweep
// called in hand-picked order — the wrong ordering passes that composition and fails only here.
func TestWireSessionSweepsAfterResolvingContinue(t *testing.T) {
	t.Parallel()
	w := urlGuardWiring(t, config.Options{
		ContinueSession: true,
		Sessions:        config.SessionSettings{MaxCount: 1},
	})

	// The store as the boot will find it: this workspace's only session is the OLDEST record in
	// it, so a max-count of 1 applied before the resume is resolved would delete exactly the
	// record --continue is about to ask for.
	now := time.Now().UTC()
	store := session.NewStore(w.roots.sessions)
	target := saveAt(t, store, w.roots.workspace, now.Add(-72*time.Hour), "the one to continue")
	saveAt(t, store, "/elsewhere", now.Add(-2*time.Hour), "another workspace")
	saveAt(t, store, "/elsewhere", now.Add(-time.Hour), "another workspace, newer")

	if err := w.wireSession(context.Background()); err != nil {
		t.Fatalf("wireSession --continue under a max-count sweep: %v", err)
	}

	if w.resumed == nil || w.resumed.Meta.ID != target {
		t.Fatalf("--continue resumed %+v; want the record %q the workspace's only session is", w.resumed, target)
	}
	if _, err := store.Load(target); err != nil {
		t.Errorf("the continued record was swept: %v", err)
	}
	// And the sweep did run: the two out-of-budget records from the other workspace are gone.
	metas, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 1 || metas[0].ID != target {
		t.Errorf("after the boot sweep the store holds %d records (%v); want only the continued one", len(metas), metas)
	}
}

// ---------------------------------------------------------------------------
// The per-session undo snapshot stores (ADR 0074)
// ---------------------------------------------------------------------------

// The root the sweep walks and the directory [snapshot.Dir] names must be the SAME place or
// persistent undo quietly does nothing: the Driver would open a store under one path while the
// sweep and the session delete removed another. The two are spelled in different packages, so the
// agreement is pinned here rather than left to a comment.
func TestSnapshotRootMatchesStoreDir(t *testing.T) {
	t.Parallel()
	home := t.TempDir()

	roots, err := resolveRoots(home, t.TempDir())
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}

	if got, want := snapshot.Dir(home, "sess-1"), filepath.Join(roots.snapshots, "sess-1"); got != want {
		t.Errorf("snapshot.Dir = %q; want the store under the swept root %q", got, want)
	}
}

// TestGCSnapshotDirsSweepsWhatNothingCanReach pins both of the sweep's rules and both of its
// refusals: an untouched store goes on age alone, a store whose session record is gone goes after a
// day, and neither a young record-less store (the session whose first Turn has not landed yet) nor
// the id this run is resuming is ever taken.
func TestGCSnapshotDirsSweepsWhatNothingCanReach(t *testing.T) {
	t.Parallel()
	now := time.Now()
	root := t.TempDir()
	store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))

	recorded := saveAt(t, store, "/ws", now, "still open")
	resumed := saveAt(t, store, "/ws", now, "the one being resumed")

	aged := snapshotDirAt(t, root, "aged", now.Add(-scratchMaxAge-time.Hour))
	orphan := snapshotDirAt(t, root, "orphan", now.Add(-snapshotOrphanMaxAge-time.Hour))
	youngOrphan := snapshotDirAt(t, root, "young-orphan", now.Add(-time.Hour))
	kept := snapshotDirAt(t, root, recorded, now.Add(-snapshotOrphanMaxAge-time.Hour))
	// The resumed session's own store, backdated past BOTH rules: the keep list is what saves it.
	resuming := snapshotDirAt(t, root, resumed, now.Add(-scratchMaxAge-time.Hour))

	gcSnapshotDirs(root, store, now, resumed)

	for _, dir := range []string{aged, orphan} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("unreachable store %s survived the sweep (stat err = %v)", filepath.Base(dir), err)
		}
	}
	for _, dir := range []string{youngOrphan, kept, resuming} {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("reachable store %s did not survive the sweep: %v", filepath.Base(dir), err)
		}
	}

	// And the sweep's posture: a root that was never created is not an error and creates nothing.
	gcSnapshotDirs(filepath.Join(root, "does-not-exist"), store, now)
	if _, err := os.Stat(filepath.Join(root, "does-not-exist")); !os.IsNotExist(err) {
		t.Errorf("the sweep created the root it could not read (stat err = %v)", err)
	}
}

// A deleted session's snapshot store images a conversation nobody can open again, so it goes with
// the record — and only once the record actually went, which is what keeps a failed delete from
// destroying the undo history of a session that is still there.
func TestSessionHostDeleteRemovesTheSnapshotStore(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
	id := saveAt(t, store, "/ws", time.Now(), "done with")
	dir := snapshotDirAt(t, root, id, time.Now())

	host := newSessionHost(store, "/ws", "m", nil, "", nil, root, nil)
	if err := host.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the deleted session's snapshot store survived (stat err = %v)", err)
	}

	// A record that was never there fails, and the store of an unrelated session stays put.
	survivor := saveAt(t, store, "/ws", time.Now(), "still here")
	survivorDir := snapshotDirAt(t, root, survivor, time.Now())
	if err := host.Delete("no-such-session"); err == nil {
		t.Error("Delete of a missing record: want an error, got none")
	}
	if _, err := os.Stat(survivorDir); err != nil {
		t.Errorf("an unrelated session's store went with a failed delete: %v", err)
	}
}

// The journal follows session identity exactly as the scratch dir does: a /clear|/new rotate and a
// /sessions resume each report the id the session now runs under, so the composition root re-opens
// the journal against that session's own store rather than leaving the previous session's open.
func TestSessionHostJournalFollowsTheActiveSession(t *testing.T) {
	t.Parallel()
	store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
	var reopened []string
	host := newSessionHost(store, "/ws", "m", nil, "", nil,
		t.TempDir(), func(id string) { reopened = append(reopened, id) })

	boot := host.SessionID()
	if boot == "" {
		t.Fatal("SessionID answered \"\" before the first Save: the boot journal has no store to open")
	}

	host.Rotate()
	rotated := host.SessionID()
	host.Activate(session.Meta{ID: "resumed-session"})

	want := []string{rotated, "resumed-session"}
	if !slices.Equal(reopened, want) {
		t.Errorf("the journal was re-opened under %v; want %v", reopened, want)
	}
	if rotated == boot {
		t.Error("Rotate re-used the boot session's id: the new session would record into the old store")
	}
	if got := host.SessionID(); got != "resumed-session" {
		t.Errorf("SessionID after Activate = %q; want the resumed session's id", got)
	}
}

// snapshotDirAt creates one session's store directory under root with a file in it and backdates
// it, so a sweep sees a store of a known age with content worth losing.
func snapshotDirAt(t *testing.T, root, id string, mtime time.Time) string {
	t.Helper()

	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll(%s): %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "journal.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chtimes(dir, mtime, mtime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	return dir
}

// ----------------------------------------------------------------------------
// The live-instance hold: one apogee per session (apogee-3b3)
// ----------------------------------------------------------------------------

// assertHeld reports whether the store's hold on id is live — a Hold of it is refused with this
// process's own pid — or free, in which case the probe's own hold is released at once.
func assertHeld(t *testing.T, store *session.Store, id string, want bool) {
	t.Helper()
	release, err := store.Hold(id)
	if err == nil {
		_ = release()
	}
	var held *session.HeldError
	got := errors.As(err, &held)
	if got != want {
		t.Fatalf("hold on %q live = %v (Hold err %v), want %v", id, got, err, want)
	}
	if got && held.PID != os.Getpid() {
		t.Errorf("the holder of %q is pid %d, want this process (%d)", id, held.PID, os.Getpid())
	}
}

// lockFilesIn lists the `.lock` files a sessions directory holds; a directory that does not exist
// holds none.
func lockFilesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("ReadDir: %v", err)
	}
	var locks []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".lock") {
			locks = append(locks, e.Name())
		}
	}
	return locks
}

// The hold is taken at the record's birth — the first Save — never at the mint, so a run that never
// saves touches no disk; Close lets it go.
func TestSessionHostHoldsAtTheFirstSaveAndReleasesOnClose(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "sessions")
	store := session.NewStore(dir)
	host := newSessionHost(store, "/ws", "m", nil, "", nil, "", nil)

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("a host that has not saved created the sessions dir (stat err = %v)", err)
	}
	minted := host.SessionID()
	assertHeld(t, store, minted, false)

	if err := host.Save(apogee.Session{}, nil, "first", 1, 0, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	id := host.ActiveID()
	assertHeld(t, store, id, true)
	if locks := lockFilesIn(t, dir); !slices.Equal(locks, []string{id + ".lock"}) {
		t.Errorf("lock files after the first Save = %v, want [%s.lock]", locks, id)
	}

	// A second Save keeps the live hold rather than refusing itself.
	if err := host.Save(apogee.Session{}, nil, "first", 2, 0, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("second Save under the live hold: %v", err)
	}

	host.Close()
	assertHeld(t, store, id, false)
	host.Close() // idempotent
	if _, err := os.Stat(filepath.Join(dir, id+".lock")); err != nil {
		t.Errorf("Close removed the lock file (stat err = %v); only Delete may unlink it", err)
	}
}

// Rotate releases the closed session's hold and holds nothing for the fresh id until its first Save;
// Activate re-holds the adopted record, releasing the outgoing one — and Activate or Save of the id
// already held keeps the live hold, never releasing and re-acquiring it.
func TestSessionHostRotateReleasesAndActivateReHolds(t *testing.T) {
	t.Parallel()
	store := session.NewStore(t.TempDir())
	host := newSessionHost(store, "/ws", "m", nil, "", nil, "", nil)
	if err := host.Save(apogee.Session{}, nil, "A", 1, 0, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("Save A: %v", err)
	}
	first := host.ActiveID()
	assertHeld(t, store, first, true)

	host.Rotate()
	assertHeld(t, store, first, false)
	assertHeld(t, store, host.SessionID(), false)

	if err := host.Save(apogee.Session{}, nil, "B", 1, 0, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("Save B: %v", err)
	}
	second := host.ActiveID()
	assertHeld(t, store, second, true)

	rec, err := host.Load(first)
	if err != nil {
		t.Fatalf("Load(first): %v", err)
	}
	host.Activate(rec.Meta)
	assertHeld(t, store, first, true)
	assertHeld(t, store, second, false)

	// The held id, adopted and saved again: the hold stays live throughout.
	host.Activate(rec.Meta)
	assertHeld(t, store, first, true)
	if err := host.Save(apogee.Session{}, nil, "A", 2, 0, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("Save of the held id: %v", err)
	}
	assertHeld(t, store, first, true)
	host.Close()
	assertHeld(t, store, first, false)
}

// --resume of a record another live apogee holds is refused with exactly the ratified line — by id
// and by --continue alike — and the record is left as it was.
func TestResolveResumeRefusesAHeldSessionWithTheFriendlyLine(t *testing.T) {
	t.Parallel()
	store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
	id := saveAt(t, store, "/ws", time.Now(), "held elsewhere")
	release, err := store.Hold(id) // the other instance
	if err != nil {
		t.Fatalf("Hold: %v", err)
	}
	defer func() { _ = release() }()
	want := fmt.Sprintf("session %s is open in another apogee (pid %d) — fork it to work alongside", id, os.Getpid())

	if _, err := resolveResume(store, id, false, "/ws"); err == nil || err.Error() != want {
		t.Errorf("resolveResume(--resume %s) err = %v; want %q", id, err, want)
	}
	if _, err := resolveResume(store, "", true, "/ws"); err == nil || err.Error() != want {
		t.Errorf("resolveResume(--continue) err = %v; want %q", err, want)
	}
}

// The doors probe and release: after a successful resolve nothing holds the record, so the host
// built on it takes the one real hold at construction.
func TestResolveResumeProbesAndReleasesForTheHostsHold(t *testing.T) {
	t.Parallel()
	store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
	id := saveAt(t, store, "/ws", time.Now(), "free")

	rec, err := resolveResume(store, id, false, "/ws")
	if err != nil {
		t.Fatalf("resolveResume: %v", err)
	}
	assertHeld(t, store, id, false)

	host := newSessionHost(store, "/ws", "m", rec, "", nil, "", nil)
	assertHeld(t, store, id, true)
	if err := host.Save(apogee.Session{}, nil, "free", 2, 0, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("Save on the resumed host: %v", err)
	}
	host.Close()
	assertHeld(t, store, id, false)
}

// --continue means the workspace's NEWEST session: when that one is held it is refused, never
// skipped for the next record of the workspace.
func TestResolveContinueRefusesRatherThanSkips(t *testing.T) {
	t.Parallel()
	store := session.NewStore(t.TempDir())
	base := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	older := saveAt(t, store, "/ws", base, "older")
	newest := saveAt(t, store, "/ws", base.Add(time.Hour), "newest")
	release, err := store.Hold(newest)
	if err != nil {
		t.Fatalf("Hold: %v", err)
	}
	defer func() { _ = release() }()

	rec, err := resolveContinue(store, "/ws")
	var held *session.HeldError
	if !errors.As(err, &held) || held.ID != newest {
		t.Fatalf("resolveContinue = (%q, %v); want the newest record's HeldError, not a skip to %q", rec.Meta.ID, err, older)
	}
}

// The /sessions doors: Load takes the record's hold and PARKS it for the Activate a successful
// restore queues, which adopts it rather than acquiring afresh; a parked hold nobody adopts is
// released by Rotate, Close, Delete of that id or the next Load; a fork's child reaches Activate
// with no Load and is held afresh; and the hold guards only against OTHER instances — a Load of the
// active id, or of the id already parked, neither probes nor parks, and Activate of it keeps the
// live hold. A record another apogee holds is refused as Load's error, with the ratified line.
func TestSessionHostLoadParksTheHoldForActivate(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "sessions")
	store := session.NewStore(dir)
	first := saveAt(t, store, "/ws", time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC), "first")
	third := saveAt(t, store, "/ws", time.Date(2026, 9, 19, 13, 0, 0, 0, time.UTC), "third")
	host := newSessionHost(store, "/ws", "m", nil, "", nil, "", nil)
	if err := host.Save(apogee.Session{}, nil, "second", 1, 0, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	second := host.ActiveID()

	t.Run("Load parks and Activate adopts", func(t *testing.T) {
		rec, err := host.Load(first)
		if err != nil {
			t.Fatalf("Load(first): %v", err)
		}
		assertHeld(t, store, first, true)
		assertHeld(t, store, second, true) // parked beside the live hold, not in place of it
		if host.ActiveID() != second {
			t.Fatalf("Load moved the active session to %q", host.ActiveID())
		}
		host.Activate(rec.Meta)
		assertHeld(t, store, first, true)
		assertHeld(t, store, second, false)
		// Adopted as the LIVE hold, not left parked: a Delete releases only a parked hold of its id,
		// so a live one makes the store refuse this process's own Delete and the record survives.
		var held *session.HeldError
		if err := host.Delete(first); !errors.As(err, &held) {
			t.Errorf("Delete of the adopted id = %v; want the HeldError of the live hold Activate adopted", err)
		}
		if _, err := store.Load(first); err != nil {
			t.Errorf("the adopted record did not survive the refused Delete: %v", err)
		}
	})

	t.Run("Rotate, the next Load and Close release an unadopted hold", func(t *testing.T) {
		if _, err := host.Load(third); err != nil {
			t.Fatalf("Load(third): %v", err)
		}
		assertHeld(t, store, third, true)
		host.Rotate()
		assertHeld(t, store, third, false)
		assertHeld(t, store, first, false)

		if _, err := host.Load(third); err != nil {
			t.Fatalf("Load(third) again: %v", err)
		}
		if _, err := host.Load(first); err != nil {
			t.Fatalf("Load(first): %v", err)
		}
		assertHeld(t, store, third, false) // the next Load let the earlier parked hold go
		assertHeld(t, store, first, true)

		host.Close()
		assertHeld(t, store, first, false)
	})

	t.Run("a fork's child is held afresh at Activate", func(t *testing.T) {
		if err := host.Save(apogee.Session{}, nil, "parent", 1, 0, session.Usage{}, session.Usage{}, nil); err != nil {
			t.Fatalf("Save: %v", err)
		}
		parent := host.ActiveID()
		child, err := host.Fork(session.Meta{}, apogee.Session{}, nil, "child")
		if err != nil {
			t.Fatalf("Fork: %v", err)
		}
		host.Activate(child) // no Load preceded it: the resume fold took the fork's own record
		assertHeld(t, store, child.ID, true)
		assertHeld(t, store, parent, false)
	})

	t.Run("self-resume neither probes nor parks", func(t *testing.T) {
		active := host.ActiveID()
		// A probe or a park of the active id would be a second flock this process's own pid refuses.
		rec, err := host.Load(active)
		if err != nil {
			t.Fatalf("Load of the active id was refused: %v", err)
		}
		host.Activate(rec.Meta)
		assertHeld(t, store, active, true)

		// An id already parked is not probed a second time either — that probe would refuse ourselves.
		if _, err := host.Load(first); err != nil {
			t.Fatalf("Load(first): %v", err)
		}
		if _, err := host.Load(first); err != nil {
			t.Errorf("a second Load of the parked id was refused: %v", err)
		}
		assertHeld(t, store, first, true)
	})

	t.Run("Delete after a failed restore releases the parked hold", func(t *testing.T) {
		// first is parked from the arm above and was never adopted: the restore "failed".
		if err := host.Delete(first); err != nil {
			t.Fatalf("Delete of the parked id: %v", err)
		}
		if _, err := store.Load(first); err == nil {
			t.Error("the record survived its Delete")
		}
		if locks := lockFilesIn(t, dir); slices.Contains(locks, first+".lock") {
			t.Errorf("Delete left the parked hold's lock file behind: %v", locks)
		}
	})

	t.Run("a record another apogee holds is refused at Load", func(t *testing.T) {
		release, err := store.Hold(third) // the other instance
		if err != nil {
			t.Fatalf("Hold: %v", err)
		}
		defer func() { _ = release() }()
		want := fmt.Sprintf("session %s is open in another apogee (pid %d) — fork it to work alongside", third, os.Getpid())
		_, err = host.Load(third)
		var held *session.HeldError
		if !errors.As(err, &held) || err.Error() != want {
			t.Errorf("Load(held) err = %v; want the HeldError %q", err, want)
		}
	})
	host.Close()
}
