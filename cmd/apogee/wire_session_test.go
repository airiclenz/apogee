package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/eventjson"
	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/snapshot"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/testtime"
	"github.com/airiclenz/apogee/internal/tui"
	"github.com/airiclenz/apogee/internal/tuitest"
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
	host := newSessionHost(store, nil, t.TempDir(), "fake", nil, "", nil, "", nil)
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

	rec, _, err := resolveResume(store, nil, id, false, "")
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
	_, _, err := resolveResume(store, nil, filepath.Join(t.TempDir(), "absent.json"), false, "")
	if err == nil {
		t.Fatal("resolveResume of a value that is neither an id nor a file: want error, got nil")
	}
}

// runRootForResumeMiss drives the root command over a fresh `--config` home with args appended,
// and returns that home's sessions directory and the start's refusal. A miss is refused before the
// launch, so a launch that does run fails the test.
func runRootForResumeMiss(t *testing.T, workspace string, args ...string) (string, error) {
	t.Helper()
	home := testConfigHome(t, "")
	rec := &recordingLauncher{}
	cmd := newRootCommand(rec.launch)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{"--config", home, "--workspace", workspace}, args...))

	err := cmd.ExecuteContext(context.Background())

	if rec.called {
		t.Fatalf("the launcher ran for %v; a miss must refuse the start\n%s", args, out.String())
	}
	return filepath.Join(home, "sessions"), err
}

// A --resume value that is neither an id nor a readable file names the sessions directory it was
// looked for in, so a start pointed at the wrong home does not read as a wrong id.
func TestRootResumeMissNamesTheSessionsDir(t *testing.T) {
	t.Parallel()
	const missingID = "no-such-session"

	sessions, err := runRootForResumeMiss(t, t.TempDir(), "--resume", missingID)

	want := `apogee: --resume "` + missingID + `": not a known session id under ` + sessions +
		` (see /sessions) nor a readable session file`
	if err == nil || err.Error() != want {
		t.Fatalf("--resume miss: err = %v\nwant %s", err, want)
	}
}

// --continue in a workspace with nothing saved names the workspace and the sessions directory it
// searched, for the same reason.
func TestRootContinueMissNamesTheSessionsDir(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()

	sessions, err := runRootForResumeMiss(t, workspace, "--continue")

	want := "apogee: no saved sessions for this workspace (" + workspace + ") under " + sessions +
		" — start one, or resume another with --resume <id> (see /sessions)"
	if err == nil || err.Error() != want {
		t.Fatalf("--continue miss: err = %v\nwant %s", err, want)
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
	rec, _, err := resolveResume(store, nil, path, false, "")
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
	host := newSessionHost(store, nil, "/ws", "model-x", nil, "", nil, "", nil)

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
	host := newSessionHost(store, nil, "/ws", "", nil, "", nil, "", nil) // a cold start: nothing bound yet

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
	host := newSessionHost(store, nil, "/ws", "m", nil, "", nil, "", nil)

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
	host := newSessionHost(store, nil, "/ws", "m", nil, "", nil, "", nil)
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
	host := newSessionHost(store, nil, "/ws", "m", seed, "", nil, "", nil)

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
	host := newSessionHost(store, nil, "/ws", "m", nil, "", nil, "", nil)
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
	host := newSessionHost(store, nil, "/ws", "m", nil, "", nil, "", nil)
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
	host := newSessionHost(store, nil, "/ws", "m", nil, "", nil, "", nil)
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
	host := newSessionHost(store, nil, "/ws", "m", child, "", nil, "", nil)

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

// pricedEURRecord is a stored session whose amount was priced under EUR, and unpricedEURRecord one
// that names EUR but never priced a call — the two sides of the session-label rule.
func pricedEURRecord() session.Record {
	return session.Record{Meta: session.Meta{
		ID: "20261004T120000Z-eeee", Title: "priced in euro", Currency: "EUR",
		Usage: session.Usage{Calls: 2, CostMicros: 40_000, PricedCalls: 2},
	}}
}

func unpricedEURRecord() session.Record {
	return session.Record{Meta: session.Meta{
		ID: "20261004T130000Z-ffff", Title: "never priced", Currency: "EUR",
		Usage: session.Usage{Calls: 2, UnpricedCalls: 2},
	}}
}

// savedCurrency saves once through host and returns the label the record landed with.
func savedCurrency(t *testing.T, store *session.Store, host *sessionHost) string {
	t.Helper()
	if err := host.Save(apogee.Session{}, nil, "t", 1, 0, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	rec, err := store.Load(host.ActiveID())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return rec.Meta.Currency
}

// A session keeps the label its record priced a call under on every later Save, whether it came
// back through --resume (newSessionHost) or /sessions (Activate) — a USD host never relabels a
// EUR amount — while a record with nothing priced adopts the configured label (ADR 0093,
// amendment 2026-10-04).
func TestResumedSessionKeepsItsPricedCurrency(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		record session.Record
		want   string
	}{
		{"a priced EUR record stays EUR", pricedEURRecord(), "EUR"},
		{"an unpriced EUR record adopts USD", unpricedEURRecord(), "USD"},
	}
	for _, tc := range tests {
		t.Run(tc.name+" on resume", func(t *testing.T) {
			t.Parallel()
			store := session.NewStore(t.TempDir())
			rec := tc.record
			host := newSessionHost(store, nil, "/ws", "m", &rec, "", nil, "", nil)
			host.currency = "USD"
			defer host.Close()

			got := savedCurrency(t, store, host)

			if got != tc.want {
				t.Errorf("Currency after a resumed Save = %q; want %q", got, tc.want)
			}
		})
		t.Run(tc.name+" on Activate", func(t *testing.T) {
			t.Parallel()
			store := session.NewStore(t.TempDir())
			if err := store.Save(tc.record); err != nil {
				t.Fatalf("seed the record: %v", err)
			}
			host := newSessionHost(store, nil, "/ws", "m", nil, "", nil, "", nil)
			host.currency = "USD"
			defer host.Close()

			host.Activate(tc.record.Meta)
			got := savedCurrency(t, store, host)

			if got != tc.want {
				t.Errorf("Currency after an activated Save = %q; want %q", got, tc.want)
			}
		})
	}
}

// Rotate leaves the resumed session's label behind with the rest of its identity: the fresh session
// the next Save mints is priced under the configured label.
func TestRotateReturnsToTheConfiguredCurrency(t *testing.T) {
	t.Parallel()
	store := session.NewStore(t.TempDir())
	rec := pricedEURRecord()
	host := newSessionHost(store, nil, "/ws", "m", &rec, "", nil, "", nil)
	host.currency = "USD"
	defer host.Close()

	host.Rotate()
	got := savedCurrency(t, store, host)

	if got != "USD" {
		t.Errorf("Currency after Rotate = %q; want the configured USD", got)
	}
}

// The replay payload carries the label the resumed session counts under — the effective one the
// host's Saves write — not the record's raw label.
func TestResumedSessionPayloadCarriesTheEffectiveCurrency(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		record session.Record
		want   string
	}{
		{"a priced EUR record stays EUR", pricedEURRecord(), "EUR"},
		{"an unpriced EUR record adopts USD", unpricedEURRecord(), "USD"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rs := resumedSession(&tc.record, "USD", false)

			if rs == nil || rs.Currency != tc.want {
				t.Errorf("resumedSession currency = %+v; want %q", rs, tc.want)
			}
		})
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
	host := newSessionHost(store, nil, "/ws", "m", child, "", nil, "", nil)

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
	rec, _, err := resolveResume(store, nil, legacyPath, false, "")
	if err != nil {
		t.Fatalf("resolveResume by path: %v", err)
	}
	if rec == nil {
		t.Fatal("resolveResume returned nil for a readable legacy file")
	}
	if len(rec.Transcript) != 0 {
		t.Errorf("legacy Transcript = %s; want empty (no scrollback recorded)", rec.Transcript)
	}
	if rs := resumedSession(rec, "", false); rs == nil || len(rs.Transcript) != 0 {
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

	rec, _, err := resolveResume(store, nil, plantedPath, false, "/ws")
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
	host := newSessionHost(store, nil, "/ws", "m", rec, "", nil, "", nil)
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
	if _, _, err := resolveResume(store, nil, plantedPath, false, "/ws"); err == nil {
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
	h := newSessionHost(store, nil, ws, "m", nil, "", nil, "", nil)
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
	_, _, err := resolveResume(store, nil, "some-id", true, "/ws")
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
	host := newSessionHost(store, nil, "/ws", "model-x", nil, "", nil, "", nil)

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
	rs := resumedSession(&rec, "", false)
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
	rec, _, err := resolveResume(store, nil, "", false, "/ws")
	if err != nil {
		t.Fatalf("resolveResume fresh: %v", err)
	}
	if rec != nil {
		t.Errorf("resolveResume with neither flag = %+v; want nil", rec)
	}
	if got := resumedSession(nil, "", false); got != nil {
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
	host := newSessionHost(store, nil, "/ws", "m", nil, root, func(dir string) { moved = append(moved, dir) }, "", nil)

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
	host := newSessionHost(store, nil, "/ws", "m", nil, "", func(string) { called = true }, "", nil)

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

	t.Run("orphan journals go under the unconfigured default", func(t *testing.T) {
		t.Parallel()
		store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
		live := saveAt(t, store, "/ws", now.Add(-time.Hour), "live")
		crashed := saveAt(t, store, "/ws", now.Add(-2*time.Hour), "crashed")
		openJournal := func(id string) func() error {
			t.Helper()
			release, err := store.Hold(id)
			if err != nil {
				t.Fatalf("Hold %s: %v", id, err)
			}
			t.Cleanup(func() { _ = release() })
			journal, err := store.OpenJournal(id)
			if err != nil {
				t.Fatalf("OpenJournal %s: %v", id, err)
			}
			if err := journal.Close(); err != nil {
				t.Fatalf("close the journal: %v", err)
			}
			return release
		}
		openJournal(live)
		if err := openJournal(crashed)(); err != nil {
			t.Fatalf("release %s: %v", crashed, err)
		}

		gcSessions(store, config.SessionSettings{})

		if _, err := os.Stat(store.JournalPath(live)); err != nil {
			t.Errorf("the sweep removed a journal its owner holds: %v", err)
		}
		if _, err := os.Stat(store.JournalPath(crashed)); !os.IsNotExist(err) {
			t.Errorf("the sweep kept a journal whose lock is free (stat err = %v)", err)
		}
		if got := titles(t, store); len(got) != 2 {
			t.Errorf("the journal sweep left records %v; want both kept under an unconfigured policy", got)
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

	host := newSessionHost(store, nil, "/ws", "m", nil, "", nil, root, nil)
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
	host := newSessionHost(store, nil, "/ws", "m", nil, "", nil,
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
	host := newSessionHost(store, nil, "/ws", "m", nil, "", nil, "", nil)

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
	host := newSessionHost(store, nil, "/ws", "m", nil, "", nil, "", nil)
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

	if _, _, err := resolveResume(store, nil, id, false, "/ws"); err == nil || err.Error() != want {
		t.Errorf("resolveResume(--resume %s) err = %v; want %q", id, err, want)
	}
	if _, _, err := resolveResume(store, nil, "", true, "/ws"); err == nil || err.Error() != want {
		t.Errorf("resolveResume(--continue) err = %v; want %q", err, want)
	}
}

// The doors probe and release: after a successful resolve nothing holds the record, so the host
// built on it takes the one real hold at construction.
func TestResolveResumeProbesAndReleasesForTheHostsHold(t *testing.T) {
	t.Parallel()
	store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
	id := saveAt(t, store, "/ws", time.Now(), "free")

	rec, _, err := resolveResume(store, nil, id, false, "/ws")
	if err != nil {
		t.Fatalf("resolveResume: %v", err)
	}
	assertHeld(t, store, id, false)

	host := newSessionHost(store, nil, "/ws", "m", rec, "", nil, "", nil)
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
	host := newSessionHost(store, nil, "/ws", "m", nil, "", nil, "", nil)
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

// eventRecorder is an inner sink that keeps every Event it was handed, in order.
type eventRecorder struct {
	mu     sync.Mutex
	events []domain.Event
}

// Emit implements domain.EventSink.
func (r *eventRecorder) Emit(ev domain.Event) {
	r.mu.Lock()
	r.events = append(r.events, ev)
	r.mu.Unlock()
}

// got returns a copy of the Events recorded so far.
func (r *eventRecorder) got() []domain.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.events)
}

// journalEnd is one End a journalLog saw: the record the journal belonged to, and whether that
// record was still held when its journal ended.
type journalEnd struct {
	id   string
	held bool
}

// journalLog is a run.JournalOpener stand-in that records every journal the host opened and every
// one it ended — probing, at each End, whether the record is still held, which is the order the host
// promises: the journal goes BEFORE the hold.
type journalLog struct {
	store *session.Store

	mu     sync.Mutex
	starts []run.JournalStart
	ends   []journalEnd
}

// open is the opener the host is attached with.
func (l *journalLog) open(start run.JournalStart) run.LiveJournal {
	l.mu.Lock()
	l.starts = append(l.starts, start)
	l.mu.Unlock()
	return &loggedJournal{log: l, id: start.RecordID}
}

// snapshot returns copies of the starts and ends recorded so far.
func (l *journalLog) snapshot() ([]run.JournalStart, []journalEnd) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.starts), slices.Clone(l.ends)
}

// loggedJournal is one journal journalLog opened.
type loggedJournal struct {
	log *journalLog
	id  string
}

// Wrap implements run.LiveJournal; the stand-in records no Event.
func (j *loggedJournal) Wrap(domain.EventSink) domain.EventSink { return discardEvents{} }

// Prompt implements run.LiveJournal.
func (j *loggedJournal) Prompt(string, bool) {}

// End implements run.LiveJournal, probing the record's hold as the journal goes.
func (j *loggedJournal) End() {
	held, _, err := j.log.store.ProbeHold(j.id)
	j.log.mu.Lock()
	j.log.ends = append(j.log.ends, journalEnd{id: j.id, held: held && err == nil})
	j.log.mu.Unlock()
}

// journalFilesIn lists the live Event journals a sessions directory holds; a directory that does not
// exist holds none.
func journalFilesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("ReadDir: %v", err)
	}
	var journals []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".events.jsonl") {
			journals = append(journals, e.Name())
		}
	}
	return journals
}

// readJournal decodes every line of the journal at path, and the opening frame's journal-only data.
func readJournal(t *testing.T, path string) ([]eventjson.Line, eventjson.JournalStarted) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the journal: %v", err)
	}
	var lines []eventjson.Line
	var started eventjson.JournalStarted
	for i, raw := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		line, err := eventjson.Decode(raw)
		if err != nil {
			t.Fatalf("journal line %d %q: %v", i+1, raw, err)
		}
		if i == 0 {
			var frame struct {
				Data eventjson.JournalStarted `json:"data"`
			}
			if err := json.Unmarshal(raw, &frame); err != nil {
				t.Fatalf("the opening frame %q: %v", raw, err)
			}
			started = frame.Data
		}
		lines = append(lines, line)
	}
	return lines, started
}

// promptLines returns the journal's prompt lines, in order.
func promptLines(lines []eventjson.Line) []eventjson.Prompt {
	var prompts []eventjson.Prompt
	for _, l := range lines {
		if l.Prompt != nil {
			prompts = append(prompts, *l.Prompt)
		}
	}
	return prompts
}

// tokenText concatenates the journal's top-level token lines — the answer the session streamed.
func tokenText(lines []eventjson.Line) string {
	var text strings.Builder
	for _, l := range lines {
		if tok, ok := l.Event.(domain.TokenEvent); ok && l.Depth == 0 {
			text.WriteString(tok.Text)
		}
	}
	return text.String()
}

// recordWithEntries stores a record whose transcript holds entries, and returns it.
func recordWithEntries(t *testing.T, store *session.Store, title string, entries []session.Entry) session.Record {
	t.Helper()
	blob, err := session.EncodeTranscript(entries)
	if err != nil {
		t.Fatalf("EncodeTranscript: %v", err)
	}
	now := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	rec := session.Record{
		Meta:       session.Meta{ID: session.NewID(now), Title: title, CreatedAt: now, UpdatedAt: now, Workspace: "/ws"},
		Transcript: blob,
	}
	if err := store.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}
	return rec
}

// The interactive session holds its record at the first prompt — before Turn 1 and before any Save —
// and its live Event journal is born just after that hold: the prompt and interjection lines and the
// session's Events land in it, in order, while every Event still reaches the chain beneath unchanged.
// Close removes the journal and then lets the hold go.
func TestSessionHostEventJournalRecordsPromptsAndEvents(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "sessions")
	store := session.NewStore(dir)
	host := newSessionHost(store, nil, "/ws", "m", nil, "", nil, "", nil)
	inner := &eventRecorder{}
	sink := newLiveJournalSink(inner)
	host.attachEventJournal(sink, firingJournal(store, nil))
	id := host.SessionID()

	before := domain.TurnEvent{Status: domain.StatusTurnComplete}
	sink.Emit(before)
	if journals := journalFilesIn(t, dir); len(journals) != 0 {
		t.Fatalf("journals before the first prompt = %v, want none", journals)
	}
	assertHeld(t, store, id, false)

	host.Prompt("Say hello", false)
	assertHeld(t, store, id, true)
	if host.ActiveID() != "" {
		t.Errorf("the first prompt adopted the identity %q; the first Save is the record's birth", host.ActiveID())
	}
	sink.Emit(domain.TokenEvent{Text: "Hel"})
	sink.Emit(domain.TokenEvent{Text: "lo."})
	turn := domain.TurnEvent{Status: domain.StatusExchangeComplete}
	sink.Emit(turn)
	if err := host.Save(apogee.Session{}, nil, "Say hello", 1, 0, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	host.Prompt("and this", true)

	lines, started := readJournal(t, store.JournalPath(id))
	kinds := make([]string, 0, len(lines))
	for _, l := range lines {
		kinds = append(kinds, l.Kind)
	}
	if want := []string{"run_started", "prompt", "token", "turn", "prompt"}; !slices.Equal(kinds, want) {
		t.Errorf("journal kinds = %v, want %v", kinds, want)
	}
	if want := (eventjson.JournalStarted{Session: id, Workspace: "/ws", Title: "Say hello", Entries: 0}); started != want {
		t.Errorf("opening frame = %+v, want %+v", started, want)
	}
	if want := []eventjson.Prompt{{Text: "Say hello"}, {Text: "and this", Interjection: true}}; !slices.Equal(promptLines(lines), want) {
		t.Errorf("prompt lines = %+v, want %+v", promptLines(lines), want)
	}
	if got := tokenText(lines); got != "Hello." {
		t.Errorf("journalled answer = %q, want %q", got, "Hello.")
	}
	if got := inner.got(); len(got) != 4 {
		t.Errorf("the chain beneath received %d Events, want all 4, unmerged", len(got))
	}

	host.Close()
	if journals := journalFilesIn(t, dir); len(journals) != 0 {
		t.Errorf("journals after Close = %v, want none", journals)
	}
	assertHeld(t, store, id, false)
}

// On /clear (Rotate), a /sessions switch (Load + Activate), /fork (Fork + Activate of the child) and
// exit (Close), the open journal ends while its record is still held — the journal goes, then the
// hold. Each new id gets a journal of its own, born with the record's transcript length.
func TestSessionHostEventJournalEndsBeforeTheHoldIsReleased(t *testing.T) {
	t.Parallel()
	store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
	stored := recordWithEntries(t, store, "stored", forkPrefix()[:2])
	host := newSessionHost(store, nil, "/ws", "m", nil, "", nil, "", nil)
	log := &journalLog{store: store}
	host.attachEventJournal(newLiveJournalSink(nil), log.open)

	host.Prompt("first", false)
	first := host.SessionID()
	if err := host.Save(apogee.Session{}, nil, "first", 1, 0, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	host.Prompt("again", false) // the open journal takes it; no second birth

	host.Rotate() // /clear
	assertHeld(t, store, first, false)
	host.Prompt("cleared", false)
	cleared := host.SessionID()

	rec, err := host.Load(stored.Meta.ID) // a /sessions switch
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	host.Activate(rec.Meta)
	assertHeld(t, store, cleared, false)

	child, err := host.Fork(session.Meta{}, apogee.Session{}, forkPrefix(), "child") // /fork
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	host.Activate(child)
	assertHeld(t, store, stored.Meta.ID, false)

	host.Close() // exit
	assertHeld(t, store, child.ID, false)

	starts, ends := log.snapshot()
	wantStarts := []run.JournalStart{
		{RecordID: first, Title: "first", Workspace: "/ws", Entries: 0},
		{RecordID: cleared, Title: "cleared", Workspace: "/ws", Entries: 0},
		{RecordID: stored.Meta.ID, Title: "stored", Workspace: "/ws", Entries: 2},
		{RecordID: child.ID, Title: "child", Workspace: "/ws", Entries: len(forkPrefix())},
	}
	if !slices.Equal(starts, wantStarts) {
		t.Errorf("journals born = %+v, want %+v", starts, wantStarts)
	}
	wantEnds := []journalEnd{{first, true}, {cleared, true}, {stored.Meta.ID, true}, {child.ID, true}}
	if !slices.Equal(ends, wantEnds) {
		t.Errorf("journals ended = %+v, want each ended under its record's hold %+v", ends, wantEnds)
	}
}

// A run resumed on a record holds it from construction, and that record has had its prompt: its
// journal is born at attach, with the record's title and transcript length.
func TestSessionHostEventJournalBornAtAttachOnAResumedRecord(t *testing.T) {
	t.Parallel()
	store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
	rec := recordWithEntries(t, store, "resumed", forkPrefix())
	host := newSessionHost(store, nil, "/ws", "m", &rec, "", nil, "", nil)
	log := &journalLog{store: store}
	host.attachEventJournal(newLiveJournalSink(nil), log.open)

	starts, _ := log.snapshot()
	want := []run.JournalStart{{RecordID: rec.Meta.ID, Title: "resumed", Workspace: "/ws", Entries: len(forkPrefix())}}
	if !slices.Equal(starts, want) {
		t.Errorf("journals born at attach = %+v, want %+v", starts, want)
	}
	host.Close()
	if _, ends := log.snapshot(); !slices.Equal(ends, []journalEnd{{rec.Meta.ID, true}}) {
		t.Errorf("journals ended at Close = %+v, want the resumed record's, under its hold", ends)
	}
}

// A driven TUI session writes its live Event journal: none before the first prompt, one after it
// holding the prompts and the answers the session streamed, in order; /clear removes it and the next
// prompt starts a new one under the new id; exit removes that one too.
func TestSessionE2EWritesALiveEventJournal(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, loadScript(t, "queued-command"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUI(t, drv, stub)
	waitIdle(drv)
	dir := filepath.Join(sess.Home(), "sessions")
	if journals := journalFilesIn(t, dir); len(journals) != 0 {
		t.Fatalf("journals before the first prompt = %v, want none", journals)
	}

	// journalled waits for exactly one journal whose decoded lines satisfy done, and returns its id.
	journalled := func(what string, done func([]eventjson.Line) bool) string {
		t.Helper()
		var id string
		testtime.Eventually(t, testtime.Budget(10*time.Second), func() bool {
			journals := journalFilesIn(t, dir)
			if len(journals) != 1 {
				return false
			}
			id = strings.TrimSuffix(journals[0], ".events.jsonl")
			lines, _ := readJournal(t, filepath.Join(dir, journals[0]))
			return done(lines)
		}, what)
		return id
	}

	submit(drv, "Say hello")
	drv.WaitText("Hello there.")
	waitIdle(drv)
	submit(drv, "And once more")
	drv.WaitText("Thought about it.")
	waitIdle(drv)
	first := journalled("the first session's journal", func(lines []eventjson.Line) bool {
		prompts := promptLines(lines)
		return len(prompts) == 2 && prompts[0].Text == "Say hello" && prompts[1].Text == "And once more" &&
			tokenText(lines) == "Hello there.Thought about it."
	})

	submit(drv, "/clear")
	testtime.Eventually(t, testtime.Budget(10*time.Second), func() bool {
		return len(journalFilesIn(t, dir)) == 0
	}, "/clear to remove the journal")

	submit(drv, "After the clear")
	drv.WaitText("Thought about it.")
	waitIdle(drv)
	second := journalled("the cleared session's journal", func(lines []eventjson.Line) bool {
		prompts := promptLines(lines)
		return len(prompts) == 1 && prompts[0].Text == "After the clear"
	})
	if second == first {
		t.Errorf("the cleared session journals under the old id %q", first)
	}

	if err := sess.Quit(); err != nil {
		t.Fatalf("quit: %v", err)
	}
	if journals := journalFilesIn(t, dir); len(journals) != 0 {
		t.Errorf("journals after exit = %v, want none", journals)
	}
}

// runElsewhere stands in for another apogee running session id: it holds the record and writes
// the journal's opening frame (raw "" — the real Journal's), or raw bytes in its place. The hold
// and the file are released at cleanup.
func runElsewhere(t *testing.T, store *session.Store, id, title, workspace, raw string) {
	t.Helper()
	release, err := store.Hold(id)
	if err != nil {
		t.Fatalf("Hold %s: %v", id, err)
	}
	t.Cleanup(func() { _ = release() })
	file, err := store.OpenJournal(id)
	if err != nil {
		t.Fatalf("OpenJournal %s: %v", id, err)
	}
	t.Cleanup(func() { _ = file.Close() })
	if raw != "" {
		if _, err := file.WriteString(raw); err != nil {
			t.Fatalf("write journal %s: %v", id, err)
		}
		return
	}
	eventjson.NewJournal(file, eventjson.JournalOptions{Session: id, Title: title, Workspace: workspace})
}

// The host lists every session another instance runs with a journal: a held record with one, and a
// held journal-only run (a headless or daemon Firing before its first save) whose row takes its
// title, workspace and time from the journal's frame. A record-less run whose frame does not decode
// has nothing to show and is skipped; a record keeps its row whatever its journal holds. Each row's
// probe follows the owner's hold.
func TestSessionHostListsLiveSessionsOfOtherInstances(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "sessions")
	store := session.NewStore(dir)
	recorded := saveAt(t, store, "/ws", time.Now(), "a recorded run")
	runElsewhere(t, store, recorded, "a recorded run", "/ws", "")
	tornRecorded := saveAt(t, store, "/ws", time.Now(), "torn but recorded")
	runElsewhere(t, store, tornRecorded, "", "", `{"kind":"run_sta`)
	journalOnly := session.NewID(time.Now())
	runElsewhere(t, store, journalOnly, "a headless run", "/ws/b", "")
	torn := session.NewID(time.Now().Add(time.Second))
	runElsewhere(t, store, torn, "", "", `{"kind":"run_sta`)
	host := newSessionHost(store, nil, "/ws", "m", nil, "", nil, "", nil)
	defer host.Close()

	rows, err := host.LiveSessions()
	if err != nil {
		t.Fatalf("LiveSessions: %v", err)
	}
	byID := make(map[string]tui.LiveSession, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	if len(rows) != 3 || byID[recorded].ID == "" || byID[tornRecorded].ID == "" || byID[journalOnly].ID == "" {
		t.Fatalf("live rows = %+v; want the recorded run, the torn recorded run and the journal-only run", rows)
	}
	row := byID[journalOnly]
	if row.Title != "a headless run" || row.Workspace != "/ws/b" || row.At.IsZero() {
		t.Errorf("journal-only row = %+v; want the frame's title, workspace and time", row)
	}
	if row.JournalPath != store.JournalPath(journalOnly) || row.RecordPath != filepath.Join(dir, journalOnly+".json") {
		t.Errorf("journal-only row paths = %q, %q; want the journal and the record beside it", row.JournalPath, row.RecordPath)
	}
	if held, err := row.Probe(); err != nil || !held {
		t.Errorf("probe of a running session = %v, %v; want held", held, err)
	}
	if got := byID[recorded].Title; got != "a recorded run" {
		t.Errorf("recorded row title = %q; want the journal frame's", got)
	}
}

// The host's own session is never live, though its journal and its hold are on disk: not once the
// first prompt has Claimed it before any Save, not once it is saved, and not a record Load parked.
func TestSessionHostLiveLeavesOutItsOwnSessions(t *testing.T) {
	t.Parallel()
	store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
	parked := saveAt(t, store, "/ws", time.Now(), "parked")
	host := newSessionHost(store, nil, "/ws", "m", nil, "", nil, "", nil)
	defer host.Close()
	host.attachEventJournal(newLiveJournalSink(&eventRecorder{}), firingJournal(store, nil))
	assertNoLive := func(when string) {
		t.Helper()
		rows, err := host.LiveSessions()
		if err != nil {
			t.Fatalf("LiveSessions %s: %v", when, err)
		}
		if len(rows) != 0 {
			t.Errorf("live rows %s = %+v; want none — every one is this host's own", when, rows)
		}
	}

	host.Prompt("Say hello", false)
	if _, err := os.Stat(store.JournalPath(host.SessionID())); err != nil {
		t.Fatalf("setup: the first prompt opened no journal: %v", err)
	}
	assertNoLive("after the first prompt")

	if err := host.Save(apogee.Session{}, nil, "Say hello", 1, 0, session.Usage{}, session.Usage{}, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	assertNoLive("after the first save")

	if _, err := host.Load(parked); err != nil {
		t.Fatalf("Load: %v", err)
	}
	file, err := store.OpenJournal(parked) // a journal a crashed owner left, not yet swept
	if err != nil {
		t.Fatalf("OpenJournal: %v", err)
	}
	defer func() { _ = file.Close() }()
	eventjson.NewJournal(file, eventjson.JournalOptions{Session: parked, Title: "parked"})
	assertNoLive("with a parked record")
}

// heldElsewhereLine is the refusal a held session without a journal still meets at the door: the
// friendly line of the *session.HeldError another apogee's hold raises (this test process holds it,
// so the pid is its own).
func heldElsewhereLine(id string) string {
	return fmt.Sprintf("session %s is open in another apogee (pid %d) — fork it to work alongside", id, os.Getpid())
}

// assertWatches checks that --resume id resolved to a watch of the running session — no record to
// restore, and the live session's row with its journal, record path, frame title and a probe that
// finds the owner holding it.
func assertWatches(t *testing.T, store *session.Store, rec *session.Record, live *tui.LiveSession, id, title string) {
	t.Helper()
	if rec != nil {
		t.Errorf("resolved record = %+v; want nil — a watch restores nothing", rec.Meta)
	}
	if live == nil {
		t.Fatal("resolved no live session; want a watch source")
	}
	journal := store.JournalPath(id)
	if live.ID != id || live.JournalPath != journal ||
		live.RecordPath != filepath.Join(filepath.Dir(journal), id+".json") || live.Title != title {
		t.Errorf("live session = %+v; want id %s, journal %s, title %q", live, id, journal, title)
	}
	if live.Probe == nil {
		t.Fatal("live session has no probe")
	}
	if held, err := live.Probe(); err != nil || !held {
		t.Errorf("probe = %v, %v; want held by its owner", held, err)
	}
}

// A record-less run another instance holds with a journal — a headless Firing before its first save
// — is watched by --resume, where it was "not a known session id" before journals existed.
func TestResolveResumeWatchesAJournalOnlyRun(t *testing.T) {
	t.Parallel()
	store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
	id := session.NewID(time.Now())
	runElsewhere(t, store, id, "a headless run", "/ws", "")

	rec, live, err := resolveResume(store, nil, id, false, "/ws")

	if err != nil {
		t.Fatalf("resolveResume(--resume %s): %v", id, err)
	}
	assertWatches(t, store, rec, live, id, "a headless run")
}

// A recorded session another instance holds with a journal is watched too, rather than refused.
func TestResolveResumeWatchesAHeldRecordWithAJournal(t *testing.T) {
	t.Parallel()
	store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
	id := saveAt(t, store, "/ws", time.Now(), "a recorded run")
	runElsewhere(t, store, id, "a recorded run", "/ws", "")

	rec, live, err := resolveResume(store, nil, id, false, "/ws")

	if err != nil {
		t.Fatalf("resolveResume(--resume %s): %v", id, err)
	}
	assertWatches(t, store, rec, live, id, "a recorded run")
}

// Only a held id with a journal is watched: a held one without a journal keeps the friendly
// refusal, a free one with a journal — an orphan the sweep has yet to remove — resumes as before,
// and --continue refuses its newest record whenever another apogee holds it, journal or not.
func TestResolveResumeWatchNeedsAHoldAndAJournal(t *testing.T) {
	t.Parallel()
	t.Run("held without a journal", func(t *testing.T) {
		t.Parallel()
		store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
		id := saveAt(t, store, "/ws", time.Now(), "held elsewhere")
		release, err := store.Hold(id)
		if err != nil {
			t.Fatalf("Hold: %v", err)
		}
		defer func() { _ = release() }()

		rec, live, err := resolveResume(store, nil, id, false, "/ws")

		if err == nil || err.Error() != heldElsewhereLine(id) || rec != nil || live != nil {
			t.Errorf("resolveResume = %v, %v, %v; want only the refusal %q", rec, live, err, heldElsewhereLine(id))
		}
	})
	t.Run("free with an orphan journal", func(t *testing.T) {
		t.Parallel()
		store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
		id := saveAt(t, store, "/ws", time.Now(), "crashed")
		if err := os.WriteFile(store.JournalPath(id), []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("write orphan journal: %v", err)
		}

		rec, live, err := resolveResume(store, nil, id, false, "/ws")

		if err != nil || live != nil || rec == nil || rec.Meta.ID != id {
			t.Errorf("resolveResume = %v, %v, %v; want the record %s resumed", rec, live, err, id)
		}
	})
	t.Run("continue", func(t *testing.T) {
		t.Parallel()
		store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
		id := saveAt(t, store, "/ws", time.Now(), "running elsewhere")
		runElsewhere(t, store, id, "running elsewhere", "/ws", "")

		rec, live, err := resolveResume(store, nil, "", true, "/ws")

		if err == nil || err.Error() != heldElsewhereLine(id) || rec != nil || live != nil {
			t.Errorf("resolveResume(--continue) = %v, %v, %v; want only the refusal %q", rec, live, err, heldElsewhereLine(id))
		}
	})
}

// A --resume start of a running session is a fresh start that opens watching: the wiring keeps no
// record (so the sweeps keep nothing, the host mints its own session, and the Agent restores none),
// the host neither holds nor adopts the watched id, and the TUI's options carry the live session in
// place of a resume payload.
func TestResumeWatchWiresAFreshStart(t *testing.T) {
	t.Parallel()
	id := session.NewID(time.Now())
	w := urlGuardWiring(t, config.Options{Resume: id})
	store := session.NewStore(w.roots.sessions)
	runElsewhere(t, store, id, "theirs", w.roots.workspace, "")

	if err := w.wireSession(context.Background()); err != nil {
		t.Fatalf("wireSession: %v", err)
	}

	if w.resumed != nil {
		t.Errorf("w.resumed = %+v; want nil on a watch start", w.resumed.Meta)
	}
	if w.watch == nil || w.watch.ID != id {
		t.Fatalf("w.watch = %+v; want the live session %s", w.watch, id)
	}
	if w.host.ActiveID() != "" || w.host.live.Holds(id) {
		t.Errorf("host active %q, holds %s = %v; want a fresh host that never touches the watched id",
			w.host.ActiveID(), id, w.host.live.Holds(id))
	}
	opts := w.options()
	if opts.Resumed != nil || opts.Watch != w.watch {
		t.Errorf("options Resumed = %+v, Watch = %+v; want no resume payload and the live session", opts.Resumed, opts.Watch)
	}
}

// watchedHomeLabel is the label the watched-home fixtures are configured under.
const watchedHomeLabel = "bob"

// watchedHomeFixture is a watched home on disk: the home directory, the store another apogee writes
// its sessions with, and the watched homes this apogee is configured with to read it.
func watchedHomeFixture(t *testing.T) (home string, writer *session.Store, watched []watchedHome) {
	t.Helper()
	home = t.TempDir()
	writer = session.NewStore(filepath.Join(home, sessionsDirName))
	watched = watchedHomesOf([]config.WatchHome{{Label: watchedHomeLabel, Path: home}})
	return home, writer, watched
}

// The host lists a watched home's sessions, each tagged with its label: a finished record, a record
// its owner runs now (marked live, following the owner's hold), and a live run with no record yet
// (the zero Meta, its row from the journal frame). The own home's List stays the own home's.
func TestWatchedHomeSessionsAreListedAndTagged(t *testing.T) {
	t.Parallel()
	home, writer, watched := watchedHomeFixture(t)
	now := time.Now()
	finished := saveAt(t, writer, "/ws", now.Add(-time.Hour), "bobs finished run")
	running := saveAt(t, writer, "/ws", now.Add(-time.Minute), "bobs live run")
	runElsewhere(t, writer, running, "bobs live run", "/ws", "")
	journalOnly := session.NewID(now)
	runElsewhere(t, writer, journalOnly, "bobs headless run", "/ws/b", "")
	own := session.NewStore(filepath.Join(t.TempDir(), sessionsDirName))
	mine := saveAt(t, own, "/ws", now, "my own run")
	host := newSessionHost(own, watched, "/ws", "m", nil, "", nil, "", nil)
	defer host.Close()

	rows, unreadable := host.WatchedSessions()

	if len(unreadable) != 0 {
		t.Fatalf("unreadable = %v; want none", unreadable)
	}
	byID := map[string]tui.WatchedSession{}
	for _, row := range rows {
		if row.Home != watchedHomeLabel {
			t.Errorf("row %+v; want it tagged %q", row, watchedHomeLabel)
		}
		id := row.Meta.ID
		if id == "" && row.Live != nil {
			id = row.Live.ID
		}
		byID[id] = row
	}
	if len(rows) != 3 {
		t.Fatalf("watched rows = %+v; want the finished, the running and the journal-only session", rows)
	}
	dir := filepath.Join(home, sessionsDirName)
	if row := byID[finished]; row.Live != nil || row.RecordPath != filepath.Join(dir, finished+".json") {
		t.Errorf("finished row = %+v; want not live, with its record path", row)
	}
	if row := byID[running]; row.Live == nil || row.Live.Home != watchedHomeLabel ||
		row.Live.JournalPath != filepath.Join(dir, running+".events.jsonl") {
		t.Errorf("running row = %+v; want it live in the watched home with its journal", row)
	} else if held, err := row.Live.Probe(); err != nil || !held {
		t.Errorf("probe of the running session = %v, %v; want held", held, err)
	}
	if row := byID[journalOnly]; row.Meta.ID != "" || row.Live == nil || row.Live.Title != "bobs headless run" {
		t.Errorf("journal-only row = %+v; want the zero Meta and the frame's title", row)
	}
	metas, err := host.List()
	if err != nil || len(metas) != 1 || metas[0].ID != mine {
		t.Errorf("own List = %v, %v; want the own home's one record", metas, err)
	}
}

// A watched home whose sessions directory will not read is one notice naming its label, and no
// rows; a readable home beside it still lists, and a home with no sessions directory is no notice.
func TestWatchedHomeUnreadableIsOneNotice(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory mode that denies this process")
	}
	shut := t.TempDir()
	shutDir := filepath.Join(shut, sessionsDirName)
	saveAt(t, session.NewStore(shutDir), "/ws", time.Now(), "unreachable")
	if err := os.Chmod(shutDir, 0); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(shutDir, 0o700) })
	open, writer, _ := watchedHomeFixture(t)
	saveAt(t, writer, "/ws", time.Now(), "readable")
	watched := watchedHomesOf([]config.WatchHome{
		{Label: "shut", Path: shut},
		{Label: watchedHomeLabel, Path: open},
		{Label: "empty", Path: t.TempDir()},
	})
	host := newSessionHost(session.NewStore(t.TempDir()), watched, "/ws", "m", nil, "", nil, "", nil)
	defer host.Close()

	rows, unreadable := host.WatchedSessions()

	if len(unreadable) != 1 || !strings.HasPrefix(unreadable[0].Error(), "could not read watched home shut: ") {
		t.Errorf("unreadable = %v; want one notice naming the shut home", unreadable)
	}
	if len(rows) != 1 || rows[0].Home != watchedHomeLabel {
		t.Errorf("rows = %+v; want the readable home's one session", rows)
	}
}

// --resume resolves an id found in a watched home into watch mode, never into a record to resume:
// a live one to its journal, a finished one to its record with no journal. An id the own home has
// stays the own home's record.
func TestWatchedHomeResumeResolvesAnID(t *testing.T) {
	t.Parallel()
	home, writer, watched := watchedHomeFixture(t)
	finished := saveAt(t, writer, "/ws", time.Now().Add(-time.Hour), "bobs finished run")
	running := saveAt(t, writer, "/ws", time.Now(), "bobs live run")
	runElsewhere(t, writer, running, "bobs live run", "/ws", "")
	own := session.NewStore(t.TempDir())
	mine := saveAt(t, own, "/ws", time.Now(), "my own run")
	dir := filepath.Join(home, sessionsDirName)

	for _, tc := range []struct {
		name, id, wantJournal string
	}{
		{"finished", finished, ""},
		{"live", running, filepath.Join(dir, running+".events.jsonl")},
	} {
		rec, live, err := resolveResume(own, watched, tc.id, false, "/ws")
		if err != nil || rec != nil || live == nil {
			t.Fatalf("%s: resolveResume = %v, %v, %v; want a watch and no record", tc.name, rec, live, err)
		}
		if live.Home != watchedHomeLabel || live.JournalPath != tc.wantJournal ||
			live.RecordPath != filepath.Join(dir, tc.id+".json") {
			t.Errorf("%s: watch = %+v; want home %q, journal %q and the record path", tc.name, live, watchedHomeLabel, tc.wantJournal)
		}
	}
	rec, live, err := resolveResume(own, watched, mine, false, "/ws")
	if err != nil || live != nil || rec == nil || rec.Meta.ID != mine {
		t.Errorf("own id: resolveResume = %v, %v, %v; want the own record", rec, live, err)
	}
	if _, _, err := resolveResume(own, watched, session.NewID(time.Now()), false, "/ws"); err == nil {
		t.Error("an id no home has resolved; want the not-known error")
	}
}

// watchedFileFacts is what a tree snapshot records of one file: nothing a reader may change.
type watchedFileFacts struct {
	size    int64
	mode    os.FileMode
	modTime time.Time
}

// watchedTreeSnapshot records every name under root with its size, mode and modification time.
func watchedTreeSnapshot(t *testing.T, root string) map[string]watchedFileFacts {
	t.Helper()
	tree := map[string]watchedFileFacts{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		tree[path] = watchedFileFacts{size: info.Size(), mode: info.Mode(), modTime: info.ModTime()}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return tree
}

// Listing a watched home, resolving its ids, probing a live one until its owner finishes and
// probing it free afterwards leaves the home's tree exactly as it was: the same names, sizes,
// modes and modification times — nothing created, written, swept, truncated or locked into being.
func TestWatchedHomeTreeIsUntouchedByListingWatchingAndFinishing(t *testing.T) {
	t.Parallel()
	home, writer, watched := watchedHomeFixture(t)
	finished := saveAt(t, writer, "/ws", time.Now().Add(-time.Hour), "bobs finished run")
	running := saveAt(t, writer, "/ws", time.Now(), "bobs live run")
	release, err := writer.Hold(running)
	if err != nil {
		t.Fatalf("Hold: %v", err)
	}
	journal, err := writer.OpenJournal(running)
	if err != nil {
		t.Fatalf("OpenJournal: %v", err)
	}
	eventjson.NewJournal(journal, eventjson.JournalOptions{Session: running, Title: "bobs live run"}).Close()
	if err := journal.Close(); err != nil {
		t.Fatalf("close journal: %v", err)
	}
	host := newSessionHost(session.NewStore(t.TempDir()), watched, "/ws", "m", nil, "", nil, "", nil)
	defer host.Close()
	before := watchedTreeSnapshot(t, home)

	rows, _ := host.WatchedSessions()
	for _, id := range []string{finished, running} {
		if _, _, err := resolveResume(host.store, watched, id, false, "/ws"); err != nil {
			t.Fatalf("resolveResume %s: %v", id, err)
		}
	}
	var probe func() (bool, error)
	for _, row := range rows {
		if row.Live != nil {
			probe = row.Live.Probe
		}
	}
	if probe == nil {
		t.Fatal("setup: the running session was not listed live")
	}
	if held, err := probe(); err != nil || !held {
		t.Fatalf("probe while running = %v, %v; want held", held, err)
	}
	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if held, err := probe(); err != nil || held {
		t.Fatalf("probe after the finish = %v, %v; want free", held, err)
	}
	host.WatchedSessions()

	if after := watchedTreeSnapshot(t, home); !reflect.DeepEqual(after, before) {
		t.Errorf("the watched home's tree changed:\nbefore %v\nafter  %v", before, after)
	}
}
