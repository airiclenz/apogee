package main

import (
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
	if err := host.Save(snap, nil, "hi", 1, 0, session.Usage{}, session.Usage{}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	id := host.ActiveID()
	if id == "" {
		t.Fatal("host minted no id after a successful Save")
	}

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
	if err := host.Save(apogee.Session{}, nil, "first title", 1, 100, session.Usage{}, session.Usage{}); err != nil {
		t.Fatalf("Save #1: %v", err)
	}
	id := host.ActiveID()
	if id == "" {
		t.Fatal("Save minted no id")
	}
	// A second Save keeps the same id (update-in-place) and never overwrites the create-time title.
	if err := host.Save(apogee.Session{}, nil, "SECOND title", 2, 200, session.Usage{}, session.Usage{}); err != nil {
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

	if err := host.Save(apogee.Session{}, nil, "cold", 1, 0, session.Usage{}, session.Usage{}); err != nil {
		t.Fatalf("Save before the bind: %v", err)
	}
	host.SetModel("bound-model")
	if err := host.Save(apogee.Session{}, nil, "cold", 2, 0, session.Usage{}, session.Usage{}); err != nil {
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

	if err := host.Save(apogee.Session{}, nil, "A", 1, 0, session.Usage{}, session.Usage{}); err != nil {
		t.Fatalf("Save A: %v", err)
	}
	first := host.ActiveID()

	host.Rotate()
	if host.ActiveID() != "" {
		t.Errorf("ActiveID after Rotate = %q; want empty", host.ActiveID())
	}
	if err := host.Save(apogee.Session{}, nil, "B", 1, 0, session.Usage{}, session.Usage{}); err != nil {
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
	if err := host.Save(apogee.Session{}, nil, "ignored", 3, 0, session.Usage{}, session.Usage{}); err != nil {
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
	if err := host.Save(apogee.Session{}, nil, "original", 1, 0, session.Usage{}, session.Usage{}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	id := host.ActiveID()
	if err := host.Rename(id, "renamed"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if err := host.Save(apogee.Session{}, nil, "original", 2, 0, session.Usage{}, session.Usage{}); err != nil {
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
	if err := host.Save(apogee.Session{}, nil, "derived", 1, 0, session.Usage{}, session.Usage{}); err != nil {
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
	if err := host.Save(apogee.Session{}, nil, "continued", 1, 0, session.Usage{}, session.Usage{}); err != nil {
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
// UpdatedAt), returning the minted id. Each call uses its own host so it mints a distinct session.
func saveAt(t *testing.T, store *session.Store, ws string, when time.Time, title string) string {
	t.Helper()
	h := newSessionHost(store, ws, "m", nil, "", nil, "", nil)
	h.now = func() time.Time { return when }
	if err := h.Save(apogee.Session{}, nil, title, 1, 0, session.Usage{}, session.Usage{}); err != nil {
		t.Fatalf("saveAt %q: %v", title, err)
	}
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
// together could never say which was which again (session.Meta).
func TestSessionHostStoresBothTokenAccountings(t *testing.T) {
	t.Parallel()
	store := session.NewStore(t.TempDir())
	host := newSessionHost(store, "/ws", "model-x", nil, "", nil, "", nil)

	main := session.Usage{Calls: 4, PromptTokens: 60000, CachedPromptTokens: 12000, TotalTokens: 64000}
	delegates := session.Usage{Calls: 300, PromptTokens: 900000, TotalTokens: 936000}
	if err := host.Save(apogee.Session{}, nil, "delegating run", 1, 100, main, delegates); err != nil {
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
	rs := resumedSession(&rec, false)
	if rs == nil || rs.Usage != main || rs.DelegateUsage != delegates {
		t.Errorf("resumedSession = %+v; want both accountings carried into the replay payload", rs)
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
	if err := host.Save(apogee.Session{}, nil, "t", 1, 0, session.Usage{}, session.Usage{}); err != nil {
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
