package main

// The session seam of the composition root, lifted out of wire.go by concern (ADR 0043).
//
// What a run persists between launches: the host that owns the active session's id and the metadata
// only the binary knows, and the resume resolution a --resume/--continue start goes through before
// that host exists. Prompt recall needs no host here: a recall.Store bound to the workspace is the
// TUI's recall seam itself (wire_options.go).

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/snapshot"
	"github.com/airiclenz/apogee/internal/tui"
)

// sessionHost adapts a session.Store to the TUI's [tui.SessionHost] seam: it stamps the wiring
// facts only the binary knows (workspace root, resolved model) onto every record, delegates listing,
// loading, deletion, and renaming to the store, and keeps the per-session state the binary names by
// the session's id — the scratch dirs and the undo stores — following that id. It is the composition
// root's door to session identity, keeping both it and the metadata policy out of the renderer
// (phase-2 detail plan §3 C5).
//
// The identity itself — the id minted at each boundary, the Title, CreatedAt and ParentID a later
// Save preserves, and the record's live-instance hold (session.Store.Hold) — is session.Live's, which
// this host runs on and holds no copy of (apogee-3b3: a second apogee asked to open a record this
// one holds is refused at its door). The host's two followers ride Live's onMove: followScratch
// creates the active session's scratch dir and tells the engine it moved (scratchMoved), and
// followJournal re-opens the undo journal under the new id (journalMoved).
//
// model is mutex-guarded: Save runs on a Bubble Tea Cmd goroutine while SetModel is driven from the
// Update loop, so the two can race. Live guards its own state.
type sessionHost struct {
	store     *session.Store
	workspace string
	// now stamps Save's CreatedAt and UpdatedAt and a Fork's moment, and is the clock Live mints ids
	// from (read through a closure, so a test that swaps it after construction moves both).
	now func() time.Time
	// live owns the identity Saves target and the hold that follows it; built by newSessionHost.
	live *session.Live

	// scratchRoot is the run's `~/.apogee/scratch` root (stateRoots.scratch): the host owns the
	// per-session scratch dirs because the dir is named by the session's id. "" (tests, and any host
	// built without one) disables the scratch seam entirely: no dir is created and the listener
	// below is never called.
	scratchRoot string
	// scratchMoved tells the engine the ACTIVE session's scratch dir moved (a /clear|/new rotate,
	// a /sessions resume), so the confinement box handed to the next tool call is fenced to the
	// new session's scratch rather than the old one's. nil ⇒ no listener.
	scratchMoved func(dir string)

	// snapshotsRoot is the run's `~/.apogee/snapshots` root (stateRoots.snapshots): the host owns
	// the per-session undo stores for the scratch dirs' reason — a store is named by the session's
	// id (ADR 0074). "" disables that half entirely: Delete removes the record alone, which is every
	// host built without one, tests included.
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
	model string
}

// sessionHost satisfies the persistence seam the TUI drives.
var _ tui.SessionHost = (*sessionHost)(nil)

// newSessionHost builds the host over a store and the run's wiring facts, and the session.Live it
// runs on. When resumed is non-nil (a --resume/--continue start) Live begins ACTIVE on that record,
// so subsequent Saves update its file in place — its id, CreatedAt, Title and ParentID carried over
// rather than a new session forked — and HOLDS it from here, the record having been born before this
// run. The door that resolved the record (resolveResume) probed the hold and let go of it a moment
// ago, so this is the one real hold of the run; one Live cannot take is re-attempted, and reported,
// by the first Save. A fresh start instead PRE-MINTS the id its first Save will adopt, so the
// session's scratch dir can exist before the first tool call, and holds nothing until that Save.
// scratchRoot and scratchMoved wire the scratch seam, snapshotsRoot and journalMoved the undo-store
// seam beside it ("" / nil disable either — see the fields); followScratch and followJournal are
// Live's followers, in that order, and construction calls neither (the composition root seeds its
// boot state from SessionScratchDir and SessionID).
func newSessionHost(store *session.Store, workspace, model string, resumed *session.Record,
	scratchRoot string, scratchMoved func(dir string),
	snapshotsRoot string, journalMoved func(id string)) *sessionHost {
	h := &sessionHost{store: store, workspace: workspace, model: model, now: time.Now,
		scratchRoot: scratchRoot, scratchMoved: scratchMoved,
		snapshotsRoot: snapshotsRoot, journalMoved: journalMoved}
	h.live = session.NewLive(store, func() time.Time { return h.now() }, resumed,
		h.followScratch, h.followJournal)
	return h
}

// Close releases the live-instance hold — and any hold Load parked that no Activate adopted — at the
// end of the run, after the engine has closed, so the record is free to resume elsewhere the moment
// this apogee is done writing it. Idempotent.
func (h *sessionHost) Close() { h.live.Close() }

// Save persists the active session, fixing its id, Title and CreatedAt on the first call and
// updating that same file thereafter. Title is set at create and never overwritten
// by a later Save — Rename is the only writer that changes it, so a user rename sticks — while
// UpdatedAt, the transcript blob, and the browsable counts refresh every Save. Workspace and Model
// come from the wiring, the facts the renderer cannot know. The two token accountings are stored
// exactly as they arrive — the main agent's and its delegates' — because the record keeps the halves
// of a session's spend apart (session.Meta), and servedModels — the ids the upstream actually
// answered with, which the renderer folds off the readings — is stored beside the bound Model
// rather than in place of it, so the record says both what was asked for and what answered.
//
// The first Save is the record's birth: Live.Begin adopts the pre-minted id with title and this
// Save's moment as its Title and CreatedAt, and takes its live-instance hold; every later Save of the
// same id finds the hold in place and keeps it. A hold that is refused — the record is open in
// another apogee — or fails is this Save's error, returned BEFORE anything is written: two instances
// never write one record.
func (h *sessionHost) Save(
	sess apogee.Session,
	transcript []byte,
	title string,
	userMsgs, ctxUsed int,
	usage, delegateUsage session.Usage,
	servedModels []string,
) error {
	now := h.now().UTC()
	a, err := h.live.Begin(title, now)
	if err != nil {
		return err
	}
	h.mu.Lock()
	model := h.model
	h.mu.Unlock()

	return h.store.Save(session.Record{
		Meta: session.Meta{
			ID:            a.ID,
			Title:         a.Title,
			CreatedAt:     a.CreatedAt,
			UpdatedAt:     now,
			Workspace:     h.workspace,
			Model:         model,
			ParentID:      a.ParentID,
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
// /clear|/new boundary) — Live.Rotate, whose followers then create the new session's scratch dir
// (pushed to the engine via scratchMoved) and re-open its undo journal, before the new session's
// first tool call. It is idempotent on an already-inactive host (each call simply re-mints). The
// closed session's live-instance hold goes with it, and so does a hold Load parked that no Activate
// adopted; the fresh id holds nothing until its first Save.
func (h *sessionHost) Rotate() { h.live.Rotate() }

// List returns every stored session's browsable metadata, newest first (the store's ordering).
func (h *sessionHost) List() ([]session.Meta, error) { return h.store.List() }

// Load returns a stored record; it does NOT change the active session. Activation is deferred to
// Activate so the /sessions resume flow switches which file Saves target only after the live
// RestoreSession has succeeded — a restore that then fails leaves the current session's file
// untouched (subsequent Saves keep updating it, not the loaded one).
//
// Load is where the /sessions door refuses a record another apogee holds: the live-instance hold
// is taken here and PARKED (Live.Park) for the Activate that follows a successful restore to adopt,
// so a held record is refused as this Load's error — the *session.HeldError whose Error() is the
// line the browser notes — before anything is restored, and the one real hold is never taken twice.
// A hold parked by an earlier Load that nothing adopted is released first; an id this host holds
// already — the active session's, or the one parked — is neither probed nor parked.
func (h *sessionHost) Load(id string) (session.Record, error) {
	rec, err := h.store.Load(id)
	if err != nil {
		return session.Record{}, err
	}
	if err := h.live.Park(id); err != nil {
		return session.Record{}, err
	}
	return rec, nil
}

// Activate makes meta's session the one subsequent Saves update, replacing the current active
// session rather than forking a new file — the /sessions resume flow calls it once RestoreSession
// has confirmed the switch. Live.Activate carries its id, Title, CreatedAt and ParentID over and
// moves the live-instance hold with it (adopting the one Load parked), then its followers move the
// scratch dir — the resumed session's own dir (re)exists and is what the engine fences the next
// tool call to — and the undo journal, so `/undo` reaches the exchanges of the session the human
// just came back to rather than those of the one they left (ADR 0074 decision 10).
func (h *sessionHost) Activate(meta session.Meta) {
	// Activate reports nothing (tui.SessionHost), so a hold Live could not take is left for the next
	// Save to re-attempt and report: the Save is what would write over the other instance's record,
	// and it refuses before writing.
	_ = h.live.Activate(meta)
}

// SessionScratchDir returns the scratch dir of the session Saves currently target — the active
// session's, or the pre-minted next id's — creating it on the way (ensureScratchDir). The
// composition root calls it once at boot to seed Config.ScratchDir, so the dir is fenced writable
// from the engine's very first tool call; every later move goes through followScratch at the
// session boundaries above. "" when the scratch seam is disabled or creation failed.
func (h *sessionHost) SessionScratchDir() string {
	return ensureScratchDir(h.scratchRoot, h.live.ID())
}

// followScratch creates the scratch dir for id and tells the listener the active scratch moved. It
// is Live's first onMove follower, so it runs outside Live's lock: scratchMoved reaches into the
// engine holder. A disabled seam (no root) does nothing; a creation failure pushes "", removing the old
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
// under the store that id names. It is Live's second onMove follower, outside Live's lock for
// followScratch's reason: the listener reaches into the engine holder. A host with no listener does nothing.
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
func (h *sessionHost) SessionID() string { return h.live.ID() }

// Delete removes a stored session's file — and, with it, the session's own undo snapshot store: the
// objects image a conversation that no longer exists, so keeping them would leave a store nothing
// can ever open again (ADR 0074 decision 13). The store goes only once the record actually did, and
// its removal is best-effort — a failed removal is not a failed delete, and the boot sweep
// (gcSnapshotDirs) collects whatever is left behind. A hold Load parked on this very id — a resume
// whose restore failed, now being deleted instead — is released first, so the store's own hold
// (Store.Delete) does not refuse this process; a record another apogee holds is refused by the store
// with the *session.HeldError the browser notes.
func (h *sessionHost) Delete(id string) error {
	h.live.DropParked(id)
	if err := h.store.Delete(id); err != nil {
		return err
	}
	if h.snapshotsRoot != "" && id != "" {
		_ = snapshot.Remove(filepath.Join(h.snapshotsRoot, id))
	}
	return nil
}

// Rename sets a stored session's title. When the renamed session is the active one, the new title
// is mirrored onto the active identity too (Live.Retitle), so the next Save preserves it rather
// than reverting to the create-time title.
func (h *sessionHost) Rename(id, title string) error {
	if err := h.store.Rename(id, title); err != nil {
		return err
	}
	h.live.Retitle(id, title)
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
// pre-minted for its first Save (SessionID's answer) — rather than the parent Meta the renderer
// passes: the renderer reads ActiveID, which is "" until the first queued Save has landed, so
// trusting what it carried would fork a child with no parent in exactly the window a quick /fork
// hits. The host always holds an identity (Live mints one at every boundary), so that Meta is
// never consulted. UserMsgs is counted from transcript by the rule Save's
// count follows (session.UserMessageCount), so the child's "N msgs" cell holds across its first
// Save; CreatedAt is the fork's moment, and usage and context fill start at zero — a child begins
// its own spend. The write is synchronous under the store's lock like Rename; the TUI reaches it
// only through its record write queue, behind the parent's own landed Save.
func (h *sessionHost) Fork(
	_ session.Meta,
	sess apogee.Session,
	transcript []session.Entry,
	title string,
) (session.Meta, error) {
	blob, err := session.EncodeTranscript(transcript)
	if err != nil {
		return session.Meta{}, err
	}
	now := h.now().UTC()
	parentID := h.live.ID()
	h.mu.Lock()
	model := h.model
	h.mu.Unlock()
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
func (h *sessionHost) ActiveID() string { return h.live.ActiveID() }

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
