package main

// The session and prompt-recall seams of the composition root, lifted out of wire.go by concern
// (ADR 0043).
//
// What a run persists between launches: the host that owns the active session's id and the metadata
// only the binary knows, the workspace-bound host behind prompt recall, and the resume resolution a
// --resume/--continue start goes through before either of them exists.

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/recall"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/snapshot"
	"github.com/airiclenz/apogee/internal/tui"
)

// sessionHost adapts a session.Store to the TUI's [tui.SessionHost] seam: it owns the active
// session's id and the metadata policy the renderer must not, stamps the wiring facts only the
// binary knows (workspace root, resolved model) onto every record, and delegates listing, loading,
// deletion, and renaming to the store. It is the composition root's single owner of id minting and
// metadata, keeping both out of the renderer (phase-2 detail plan §3 C5) — and, because the
// per-session scratch dir is named by that id, of the scratch dirs too: it creates the active
// session's dir at each identity boundary and tells the engine when it moves (scratchMoved).
//
// The host is also the session's live-instance holder (session.Store.Hold): it holds the record
// Saves target from the record's birth — the first Save, or construction on a resumed record — for as
// long as that identity is the active one, moves the hold at every identity boundary (Rotate releases,
// Activate re-holds) and lets go of it at Close. A run that never saves therefore touches no disk,
// and a second apogee asked to open the same record is refused at its door for as long as this one
// lives (apogee-3b3).
//
// The mutable fields are mutex-guarded: Save runs on a Bubble Tea Cmd goroutine while ActiveID, the
// browser verbs, and SetModel are driven from the Update loop, so the two can race.
type sessionHost struct {
	store     *session.Store
	workspace string
	now       func() time.Time

	// scratchRoot is the run's `~/.apogee/scratch` root (stateRoots.scratch): the host owns the
	// per-session scratch dirs because it is the single owner of session identity — the dir is
	// named by the id it mints. "" (tests, and any host built without one) disables the scratch
	// seam entirely: no dir is created and the listener below is never called.
	scratchRoot string
	// scratchMoved tells the engine the ACTIVE session's scratch dir moved (a /clear|/new rotate,
	// a /sessions resume), so the confinement box handed to the next tool call is fenced to the
	// new session's scratch rather than the old one's. nil ⇒ no listener.
	scratchMoved func(dir string)

	// snapshotsRoot is the run's `~/.apogee/snapshots` root (stateRoots.snapshots): the host owns
	// the per-session undo stores for the scratch dirs' reason — a store is named by the id this
	// host mints, and identity is the host's (ADR 0074). "" disables that half entirely: Delete
	// removes the record alone, which is every host built without one, tests included.
	snapshotsRoot string
	// journalMoved tells the composition root that the ACTIVE session's id moved (a /clear|/new
	// rotate, a /sessions resume), so the undo journal is re-opened under the new id's own store
	// and handed to the engine. It carries the ID rather than a path, unlike scratchMoved above,
	// because opening a journal takes the apogee home and the workspace as well and those are the
	// composition root's to know, not this host's. nil ⇒ no listener.
	journalMoved func(id string)

	mu sync.Mutex
	// model is the model id stamped on saved metadata. It MOVES: a heartbeat rebind switches the
	// session's model mid-conversation (ADR 0024), and SetModel is how the composition root's
	// rebind closure keeps the record's metadata describing what the conversation actually ran
	// against. Guarded because that closure runs on the Update goroutine while Save runs on a Cmd.
	model  string
	active *activeSession // nil ⇒ no active session; the next Save adopts nextID (or mints one)

	// nextID is the PRE-MINTED id the next Save adopts while active is nil. It exists so a
	// session's id — and with it the scratch dir named by that id — is minted at the session
	// BOUNDARY (construction, Rotate) rather than at the first Save: tool calls run before a
	// Turn is ever saved, and the scratch dir must be created and fenced writable before the
	// first of them. An id is only a name; nothing reaches the store until a Save. "" falls
	// back to Save minting on the spot, exactly the pre-scratch behaviour.
	nextID string

	// heldID is the id whose live-instance hold this host currently owns, "" when it holds none;
	// release lets that hold go. The pair moves together (holdLocked / releaseLocked) and is
	// guarded by mu like the identity it follows. The hold is taken at the record's BIRTH — the
	// first Save, or construction on a resumed record — never at the mint, so a run that never
	// saves creates no lock file, and it is kept, not re-taken, across every Save and Activate of
	// the id already held: flock refuses a second open of the same path in one process with this
	// process's own pid, so re-acquiring would refuse ourselves.
	heldID  string
	release func() error

	// pendingID / pendingRelease is the hold Load PARKED for the Activate the /sessions resume flow
	// queues once the live restore has succeeded: Load takes it — so a record another apogee holds
	// is refused at the browser as a Load error, before anything is restored — and Activate of the
	// same id ADOPTS it rather than acquiring afresh (a second flock on the path would refuse this
	// process's own pid). A parked hold nobody adopts — the restore failed, the human moved on — is
	// released at the next identity boundary: Rotate, Close, Delete of that id, or the next Load.
	// "" ⇒ nothing parked. Guarded by mu like the hold it precedes.
	pendingID      string
	pendingRelease func() error
}

// activeSession is the identity of the session Saves currently target: the id minted once (or
// seeded by a resume), plus the CreatedAt, Title and ParentID a later Save must preserve — only
// Rename rewrites the title, only Rotate/Load changes the id, and the parent pointer is fixed at
// the fork that minted the record (Fork) and travels with its identity from there: through the
// Activate that adopts a forked child and the --resume that starts on one alike, so no Save of a
// child ever writes ParentID "" over the pointer the fork wrote. Every site that builds one
// carries it (Save's fresh identity legitimately has none: a session that is not a fork).
type activeSession struct {
	id        string
	title     string
	createdAt time.Time
	parentID  string
}

// sessionHost satisfies the persistence seam the TUI drives.
var _ tui.SessionHost = (*sessionHost)(nil)

// newSessionHost builds the host over a store and the run's wiring facts. When resumed is non-nil
// (a --resume/--continue start) the host begins ACTIVE on that record, so subsequent Saves update
// its file in place — its id, CreatedAt, and Title carried over rather than a new session forked —
// and HOLDS it from here (holdLocked), the record having been born before this run. The door that
// resolved the record (resolveResume) probed the hold and let go of it a moment ago, so this is the
// one real hold of the run; the hold it cannot take — a second instance won that gap, or the lock
// file could not be opened — is not a construction failure: the first Save re-attempts it and
// reports the refusal, since Save is the first act that would write over the other instance's
// record and the one with an error to return. A fresh start instead PRE-MINTS the id its first Save
// will adopt (nextID), so the session's scratch dir can exist before the first tool call, and holds
// nothing until that Save. scratchRoot and scratchMoved wire the scratch seam, snapshotsRoot and
// journalMoved the undo-store seam beside it ("" / nil disable either — see the fields).
func newSessionHost(store *session.Store, workspace, model string, resumed *session.Record,
	scratchRoot string, scratchMoved func(dir string),
	snapshotsRoot string, journalMoved func(id string)) *sessionHost {
	h := &sessionHost{store: store, workspace: workspace, model: model, now: time.Now,
		scratchRoot: scratchRoot, scratchMoved: scratchMoved,
		snapshotsRoot: snapshotsRoot, journalMoved: journalMoved}
	if resumed != nil {
		h.active = &activeSession{
			id:        resumed.Meta.ID,
			title:     resumed.Meta.Title,
			createdAt: resumed.Meta.CreatedAt,
			parentID:  resumed.Meta.ParentID,
		}
		_ = h.holdLocked(resumed.Meta.ID)
		return h
	}
	h.nextID = session.NewID(h.now())
	return h
}

// holdLocked makes id the session this host holds: a no-op when it is held already (the live hold
// is kept, never re-taken — see heldID), otherwise the previous hold is released and id's taken
// through the store. A refused or failed hold leaves the host holding nothing and is returned for the
// caller to report or retry; the identity the caller adopted is unaffected. Callers hold h.mu.
func (h *sessionHost) holdLocked(id string) error {
	if h.release != nil && h.heldID == id {
		return nil
	}
	h.releaseLocked()
	release, err := h.store.Hold(id)
	if err != nil {
		return err
	}
	h.heldID, h.release = id, release
	return nil
}

// releaseLocked lets go of whatever hold this host owns; a host holding nothing does nothing. The
// release cannot fail in a way the host could repair — the descriptor closes and the kernel drops the
// lock regardless — so its error is discarded. Callers hold h.mu.
func (h *sessionHost) releaseLocked() {
	if h.release != nil {
		_ = h.release()
	}
	h.heldID, h.release = "", nil
}

// releasePendingLocked lets go of the hold Load parked and nobody adopted (see pendingID); a host
// with nothing parked does nothing. Its error is discarded for releaseLocked's reason. Callers hold
// h.mu.
func (h *sessionHost) releasePendingLocked() {
	if h.pendingRelease != nil {
		_ = h.pendingRelease()
	}
	h.pendingID, h.pendingRelease = "", nil
}

// holdsLocked reports whether id is one this host holds already — the live hold or the parked one —
// so a Load of it neither probes nor parks: the hold guards against OTHER instances, and a second
// flock on a path this process holds would refuse with this process's own pid. Callers hold h.mu.
func (h *sessionHost) holdsLocked(id string) bool {
	return (h.release != nil && h.heldID == id) || (h.active != nil && h.active.id == id) ||
		(h.pendingRelease != nil && h.pendingID == id)
}

// Close releases the host's live-instance hold — and any hold Load parked that no Activate adopted —
// at the end of the run, after the engine has closed, so the record is free to resume elsewhere the
// moment this apogee is done writing it. Idempotent.
func (h *sessionHost) Close() {
	h.mu.Lock()
	h.releasePendingLocked()
	h.releaseLocked()
	h.mu.Unlock()
}

// Save persists the active session, minting its id (and fixing its Title and CreatedAt) on the
// first call and updating that same file thereafter. Title is set at create and never overwritten
// by a later Save — Rename is the only writer that changes it, so a user rename sticks — while
// UpdatedAt, the transcript blob, and the browsable counts refresh every Save. Workspace and Model
// come from the wiring, the facts the renderer cannot know. The two token accountings are stored
// exactly as they arrive — the main agent's and its delegates' — because the record keeps the halves
// of a session's spend apart (session.Meta), and servedModels — the ids the upstream actually
// answered with, which the renderer folds off the readings — is stored beside the bound Model
// rather than in place of it, so the record says both what was asked for and what answered.
//
// The first Save is the record's birth and takes its live-instance hold (holdLocked); every later
// Save of the same id finds the hold in place and keeps it. A hold that is refused — the record is
// open in another apogee — or fails is this Save's error, returned BEFORE anything is written: two
// instances never write one record.
func (h *sessionHost) Save(
	sess apogee.Session,
	transcript []byte,
	title string,
	userMsgs, ctxUsed int,
	usage, delegateUsage session.Usage,
	servedModels []string,
) error {
	now := h.now().UTC()
	h.mu.Lock()
	if h.active == nil {
		// Adopt the id the session boundary pre-minted (construction / Rotate — the name the
		// scratch dir already carries); "" is the defensive fallback, minting on the spot.
		id := h.nextID
		if id == "" {
			id = session.NewID(now)
		}
		h.nextID = ""
		h.active = &activeSession{id: id, title: title, createdAt: now}
	}
	a := *h.active
	model := h.model
	if err := h.holdLocked(a.id); err != nil {
		h.mu.Unlock()
		return err
	}
	h.mu.Unlock()

	return h.store.Save(session.Record{
		Meta: session.Meta{
			ID:            a.id,
			Title:         a.title,
			CreatedAt:     a.createdAt,
			UpdatedAt:     now,
			Workspace:     h.workspace,
			Model:         model,
			ParentID:      a.parentID,
			UserMsgs:      userMsgs,
			CtxUsed:       ctxUsed,
			Usage:         usage,
			DelegateUsage: delegateUsage,
			ServedModels:  servedModels,
		},
		Transcript: transcript,
		Session:    sess,
	})
}

// SetModel restamps the model recorded on subsequent Saves. The composition root's rebind closure
// calls it once the engine has actually been rebound, so the stored metadata names the model the
// conversation is running against rather than the one it launched with — including on a cold start,
// where the session began with no model bound at all. It does not rewrite already-saved records:
// the next Save updates the same file with the new id, which is the session's current truth.
func (h *sessionHost) SetModel(model string) {
	h.mu.Lock()
	h.model = model
	h.mu.Unlock()
}

// Rotate closes the active session and pre-mints the fresh id the next Save adopts (the
// /clear|/new boundary). Minting HERE rather than at that Save is what lets the new session's
// scratch dir exist — created and pushed to the engine via scratchMoved — before the new
// session's first tool call. It is idempotent on an already-inactive host (each call simply
// re-mints). The closed session's live-instance hold goes with it: the record is another run's to
// resume from here, and the fresh id holds nothing until its first Save. A hold Load parked that no
// Activate adopted goes too — the boundary is the human moving on from that resume.
func (h *sessionHost) Rotate() {
	h.mu.Lock()
	h.releasePendingLocked()
	h.releaseLocked()
	h.active = nil
	h.nextID = session.NewID(h.now())
	id := h.nextID
	h.mu.Unlock()
	h.followScratch(id)
	h.followJournal(id)
}

// List returns every stored session's browsable metadata, newest first (the store's ordering).
func (h *sessionHost) List() ([]session.Meta, error) { return h.store.List() }

// Load returns a stored record; it does NOT change the active session. Activation is deferred to
// Activate so the /sessions resume flow switches which file Saves target only after the live
// RestoreSession has succeeded — a restore that then fails leaves the current session's file
// untouched (subsequent Saves keep updating it, not the loaded one).
//
// Load is where the /sessions door refuses a record another apogee holds: the live-instance hold
// is taken here and PARKED (pendingID) for the Activate that follows a successful restore to adopt,
// so a held record is refused as this Load's error — the *session.HeldError whose Error() is the
// line the browser notes — before anything is restored, and the one real hold is never taken twice.
// A hold parked by an earlier Load that nothing adopted is released first. An id this host holds
// already — the active session's, or the one parked — is neither probed nor parked (holdsLocked):
// the hold guards against other instances, and re-taking it would refuse this process's own pid.
func (h *sessionHost) Load(id string) (session.Record, error) {
	rec, err := h.store.Load(id)
	if err != nil {
		return session.Record{}, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.holdsLocked(id) {
		return rec, nil
	}
	h.releasePendingLocked()
	release, err := h.store.Hold(id)
	if err != nil {
		return session.Record{}, err
	}
	h.pendingID, h.pendingRelease = id, release
	return rec, nil
}

// Activate makes meta's session the one subsequent Saves update, replacing the current active
// session rather than forking a new file — the /sessions resume flow calls it once RestoreSession
// has confirmed the switch. Its id, Title, CreatedAt and ParentID carry over so a later Save
// preserves them. The live-instance hold follows the identity: the outgoing session's hold is
// released and the adopted record's taken — ADOPTED from the hold Load parked for this Activate
// when it is there (pendingID), acquired afresh when it is not (a fork's child reaches the resume
// flow without a Load), and kept as it is when the adopted id is the one already held (a resume of
// the active session). A parked hold on some OTHER id is released: it was for an Activate that never
// came. Activate reports nothing (tui.SessionHost), so a hold it cannot take is left for the next
// Save to re-attempt and report — the Save is what would write over the other instance's record,
// and it refuses before writing.
func (h *sessionHost) Activate(meta session.Meta) {
	h.mu.Lock()
	h.active = &activeSession{id: meta.ID, title: meta.Title, createdAt: meta.CreatedAt, parentID: meta.ParentID}
	if h.pendingRelease != nil && h.pendingID == meta.ID {
		h.releaseLocked()
		h.heldID, h.release = h.pendingID, h.pendingRelease
		h.pendingID, h.pendingRelease = "", nil
	} else {
		h.releasePendingLocked()
		_ = h.holdLocked(meta.ID)
	}
	h.mu.Unlock()
	// The scratch dir follows the activation: the resumed session's own dir (re)exists and is
	// what the engine fences the next tool call to.
	h.followScratch(meta.ID)
	// And the undo journal follows it too: the resumed session's own store is re-opened, so
	// `/undo` reaches the exchanges of the session the human just came back to rather than those
	// of the one they left (ADR 0074 decision 10).
	h.followJournal(meta.ID)
}

// SessionScratchDir returns the scratch dir of the session Saves currently target — the active
// session's, or the pre-minted next id's — creating it on the way (ensureScratchDir). The
// composition root calls it once at boot to seed Config.ScratchDir, so the dir is fenced writable
// from the engine's very first tool call; every later move goes through followScratch at the
// session boundaries above. "" when the scratch seam is disabled or creation failed.
func (h *sessionHost) SessionScratchDir() string {
	h.mu.Lock()
	id := h.nextID
	if h.active != nil {
		id = h.active.id
	}
	root := h.scratchRoot
	h.mu.Unlock()
	return ensureScratchDir(root, id)
}

// followScratch creates the scratch dir for id and tells the listener the active scratch moved.
// Outside the lock: scratchMoved reaches into the engine holder, and nothing here reads host
// state. A disabled seam (no root) does nothing; a creation failure pushes "", removing the old
// session's dir from the box rather than leaving a stale — or nonexistent — path fenced writable.
func (h *sessionHost) followScratch(id string) {
	if h.scratchRoot == "" {
		return
	}
	dir := ensureScratchDir(h.scratchRoot, id)
	if h.scratchMoved != nil {
		h.scratchMoved(dir)
	}
}

// followJournal tells the listener the active session's id moved, so the undo journal is re-opened
// under the store that id names. Outside the lock for followScratch's reason: the listener reaches
// into the engine holder, and nothing here reads host state. A host with no listener does nothing.
func (h *sessionHost) followJournal(id string) {
	if h.journalMoved == nil {
		return
	}
	h.journalMoved(id)
}

// SessionID reports the id the session Saves currently target — the active session's, or the id a
// fresh start pre-minted for its first Save to adopt. It is SessionScratchDir's answer without the
// directory, and it is deliberately not ActiveID: the composition root opens the boot session's
// undo store before any Save has run, which is exactly the window ActiveID answers "" in.
func (h *sessionHost) SessionID() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.active != nil {
		return h.active.id
	}
	return h.nextID
}

// Delete removes a stored session's file — and, with it, the session's own undo snapshot store: the
// objects image a conversation that no longer exists, so keeping them would leave a store nothing
// can ever open again (ADR 0074 decision 13). The store goes only once the record actually did, and
// its removal is best-effort — a failed removal is not a failed delete, and the boot sweep
// (gcSnapshotDirs) collects whatever is left behind. A hold Load parked on this very id — a resume
// whose restore failed, now being deleted instead — is released first, so the store's own hold
// (Store.Delete) does not refuse this process; a record another apogee holds is refused by the store
// with the *session.HeldError the browser notes.
func (h *sessionHost) Delete(id string) error {
	h.mu.Lock()
	if h.pendingRelease != nil && h.pendingID == id {
		h.releasePendingLocked()
	}
	h.mu.Unlock()
	if err := h.store.Delete(id); err != nil {
		return err
	}
	if h.snapshotsRoot != "" && id != "" {
		_ = snapshot.Remove(filepath.Join(h.snapshotsRoot, id))
	}
	return nil
}

// Rename sets a stored session's title. When the renamed session is the active one, the new title
// is mirrored onto the active identity too, so the next Save preserves it rather than reverting to
// the create-time title.
func (h *sessionHost) Rename(id, title string) error {
	if err := h.store.Rename(id, title); err != nil {
		return err
	}
	h.mu.Lock()
	if h.active != nil && h.active.id == id {
		h.active.title = title
	}
	h.mu.Unlock()
	return nil
}

// Fork writes the child record a /fork cuts off the session Saves currently target: a NEW file
// under a freshly minted id holding sess (the engine state cut at the fork point) and transcript
// (the scrollback prefix through it) under title, stamped with the wiring facts every record gets
// (Workspace, Model) and with ParentID — the pointer the session browser and a resume read the fork
// relationship from. It returns the child's Meta and activates nothing: the parent stays the record
// later Saves update until the resume flow Activates the child.
//
// The parent's id is the host's OWN identity — the active session's id, else the id a fresh start
// pre-minted for its first Save (SessionID's answer) — rather than parent.ID: the renderer reads
// ActiveID, which is "" until the first queued Save has landed, so trusting what it carried would
// fork a child with no parent in exactly the window a quick /fork hits. parent.ID is the fallback
// for a host holding no identity at all. UserMsgs is counted from transcript by the rule Save's
// count follows (session.UserMessageCount), so the child's "N msgs" cell holds across its first
// Save; CreatedAt is the fork's moment, and usage and context fill start at zero — a child begins
// its own spend. The write is synchronous under the store's lock like Rename; the TUI reaches it
// only through its record write queue, behind the parent's own landed Save.
func (h *sessionHost) Fork(
	parent session.Meta,
	sess apogee.Session,
	transcript []session.Entry,
	title string,
) (session.Meta, error) {
	blob, err := session.EncodeTranscript(transcript)
	if err != nil {
		return session.Meta{}, err
	}
	now := h.now().UTC()
	h.mu.Lock()
	parentID := h.nextID
	if h.active != nil {
		parentID = h.active.id
	}
	model := h.model
	h.mu.Unlock()
	if parentID == "" {
		parentID = parent.ID
	}
	meta := session.Meta{
		ID:        session.NewID(now),
		Title:     title,
		CreatedAt: now,
		UpdatedAt: now,
		Workspace: h.workspace,
		Model:     model,
		ParentID:  parentID,
		UserMsgs:  session.UserMessageCount(transcript),
	}
	if err := h.store.Save(session.Record{Meta: meta, Transcript: blob, Session: sess}); err != nil {
		return session.Meta{}, err
	}
	return meta, nil
}

// ActiveID reports the active session's id, or "" before the first Save has minted one (and after
// a Rotate). The composition root reads it to decide whether to print the resume hint.
func (h *sessionHost) ActiveID() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.active == nil {
		return ""
	}
	return h.active.id
}

// ----------------------------------------------------------------------------
// The prompt-recall host (the composition root's half of the recall seam)
// ----------------------------------------------------------------------------

// recallHost adapts a recall.Store to the TUI's [tui.RecallHost] seam by BINDING the workspace this
// run resolved. That binding is the whole of the adapter's reason to exist: the store is
// workspace-keyed (one JSONL file per project) while the renderer knows only "this box", and
// resolving a workspace path is the composition root's job (ADR 0001), never the renderer's.
//
// Both methods forward whatever the store reports; deciding that a recall failure is survivable is
// the TUI's call, made once at its own seam, so nothing is swallowed on this side.
type recallHost struct {
	store     *recall.Store
	workspace string
}

// recallHost satisfies the prompt-recall seam the TUI drives.
var _ tui.RecallHost = (*recallHost)(nil)

// newRecallHost builds the host over the recall directory dir, bound to the absolute workspace
// path. It touches no disk: recall.New creates the directory on the first recorded prompt.
func newRecallHost(dir, workspace string) *recallHost {
	return &recallHost{store: recall.New(dir), workspace: workspace}
}

// AppendPrompt records text as this workspace's newest sent input.
func (h *recallHost) AppendPrompt(text string) error { return h.store.Append(h.workspace, text) }

// LoadPrompts returns this workspace's recorded inputs, oldest→newest.
func (h *recallHost) LoadPrompts() ([]string, error) { return h.store.Load(h.workspace) }

// resolveResume loads the session a start restores from, or returns nil when neither --resume nor
// --continue is set. --resume tries its value as a store id first (the handle /sessions lists) and
// falls back to a file path (which still reads a pre-plan bare envelope); --continue resumes this
// workspace's most recent session. The two flags are mutually exclusive.
func resolveResume(store *session.Store, resume string, continueSession bool, workspace string) (*session.Record, error) {
	switch {
	case resume != "" && continueSession:
		return nil, errors.New("apogee: --resume and --continue are mutually exclusive; pass one or the other")
	case resume != "":
		rec, err := resolveResumeArg(store, resume)
		if err != nil {
			return nil, err
		}
		return &rec, nil
	case continueSession:
		rec, err := resolveContinue(store, workspace)
		if err != nil {
			return nil, err
		}
		return &rec, nil
	default:
		return nil, nil
	}
}

// resolveResumeArg resolves a --resume value: a store id first (the common case — the id shown in
// /sessions), else a file path (LoadPath, which also wraps a legacy bare envelope). A value that is
// neither a known id nor a readable file is a friendly error naming both interpretations.
//
// A record loaded by PATH keeps its conversation but not its identity: its id is content the file
// declares rather than a name this store minted, so adopting it would point every later autosave at
// whatever record that id names — another session's file, silently overwritten, and (before the
// store's id validation) any path the id spelled out. Re-minting makes the path-resumed
// conversation a NEW session of this store, which is also what makes resuming a file from outside
// the store — a repo-shipped session, a copied record — safe.
//
// A record resolved by id is probed for its live-instance hold (probeHold): one another apogee is
// running is refused with the friendly line rather than opened twice. A path-resumed record is not —
// its re-minted id is this run's own, held by no one.
func resolveResumeArg(store *session.Store, arg string) (session.Record, error) {
	if rec, err := store.Load(arg); err == nil {
		if err := probeHold(store, rec.Meta.ID); err != nil {
			return session.Record{}, err
		}
		return rec, nil
	}
	rec, err := store.LoadPath(arg)
	if err != nil {
		return session.Record{}, fmt.Errorf(
			"apogee: --resume %q: not a known session id (see /sessions) nor a readable session file", arg)
	}
	rec.Meta.ID = session.NewID(time.Now())
	return rec, nil
}

// resolveContinue resumes the most recent session recorded for the resolved workspace — the
// --continue convenience that needs no id. List returns metas newest-first, so the first record
// whose Workspace matches is the newest; a workspace with none is a friendly error pointing at the
// alternatives. The newest record is the one --continue means: when another apogee holds it the
// start is refused with the friendly line (probeHold), never skipped to the workspace's next record.
func resolveContinue(store *session.Store, workspace string) (session.Record, error) {
	metas, err := store.List()
	if err != nil {
		return session.Record{}, err
	}
	for _, m := range metas {
		if m.Workspace == workspace {
			rec, err := store.Load(m.ID)
			if err != nil {
				return session.Record{}, err
			}
			if err := probeHold(store, m.ID); err != nil {
				return session.Record{}, err
			}
			return rec, nil
		}
	}
	return session.Record{}, fmt.Errorf(
		"apogee: no saved sessions for this workspace (%s) — start one, or resume another with "+
			"--resume <id> (see /sessions)", workspace)
}

// probeHold asks whether the record id may be opened by this run: the live-instance hold is taken
// and released AT ONCE, so a refusal — the record is open in another apogee — surfaces at the door
// as the *session.HeldError whose Error() is the line the start prints, while a record nobody holds
// is left free for the host to take the one real hold at construction (newSessionHost). Probing
// rather than holding here is what keeps the run to a single hold: a door that kept its lock would
// refuse the host's own flock with this process's pid.
func probeHold(store *session.Store, id string) error {
	release, err := store.Hold(id)
	if err != nil {
		return err
	}
	return release()
}

// resumedSession projects a resolved store record onto the TUI's startup-replay payload, or nil for
// a fresh start. The renderer decodes the opaque transcript blob itself; the binary only carries it
// across with the title, context fill, and message count the resume note and gauge need, plus
// inExchange — the resumed Agent's open-Exchange state (agent.InExchange()) — so newModel can append
// the interrupted note when the session died mid-task.
func resumedSession(rec *session.Record, inExchange bool) *tui.ResumedSession {
	if rec == nil {
		return nil
	}
	return &tui.ResumedSession{
		Transcript:    rec.Transcript,
		Title:         rec.Meta.Title,
		CtxUsed:       rec.Meta.CtxUsed,
		Usage:         rec.Meta.Usage,
		DelegateUsage: rec.Meta.DelegateUsage,
		ServedModels:  rec.Meta.ServedModels,
		UserMsgs:      rec.Meta.UserMsgs,
		InExchange:    inExchange,
	}
}
