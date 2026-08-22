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
	"sync"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/recall"
	"github.com/airiclenz/apogee/internal/session"
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
}

// activeSession is the identity of the session Saves currently target: the id minted once (or
// seeded by a resume), plus the CreatedAt and Title a later Save must preserve — only Rename
// rewrites the title, and only Rotate/Load changes the id.
type activeSession struct {
	id        string
	title     string
	createdAt time.Time
}

// sessionHost satisfies the persistence seam the TUI drives.
var _ tui.SessionHost = (*sessionHost)(nil)

// newSessionHost builds the host over a store and the run's wiring facts. When resumed is non-nil
// (a --resume/--continue start) the host begins ACTIVE on that record, so subsequent Saves update
// its file in place — its id, CreatedAt, and Title carried over rather than a new session forked.
// A fresh start instead PRE-MINTS the id its first Save will adopt (nextID), so the session's
// scratch dir can exist before the first tool call. scratchRoot and scratchMoved wire the scratch
// seam ("" / nil disable it — see the fields).
func newSessionHost(store *session.Store, workspace, model string, resumed *session.Record,
	scratchRoot string, scratchMoved func(dir string)) *sessionHost {
	h := &sessionHost{store: store, workspace: workspace, model: model, now: time.Now,
		scratchRoot: scratchRoot, scratchMoved: scratchMoved}
	if resumed != nil {
		h.active = &activeSession{
			id:        resumed.Meta.ID,
			title:     resumed.Meta.Title,
			createdAt: resumed.Meta.CreatedAt,
		}
		return h
	}
	h.nextID = session.NewID(h.now())
	return h
}

// Save persists the active session, minting its id (and fixing its Title and CreatedAt) on the
// first call and updating that same file thereafter. Title is set at create and never overwritten
// by a later Save — Rename is the only writer that changes it, so a user rename sticks — while
// UpdatedAt, the transcript blob, and the browsable counts refresh every Save. Workspace and Model
// come from the wiring, the facts the renderer cannot know.
func (h *sessionHost) Save(sess apogee.Session, transcript []byte, title string, userMsgs, ctxUsed int, usage session.Usage) error {
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
	h.mu.Unlock()

	return h.store.Save(session.Record{
		Meta: session.Meta{
			ID:        a.id,
			Title:     a.title,
			CreatedAt: a.createdAt,
			UpdatedAt: now,
			Workspace: h.workspace,
			Model:     model,
			UserMsgs:  userMsgs,
			CtxUsed:   ctxUsed,
			Usage:     usage,
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
// re-mints).
func (h *sessionHost) Rotate() {
	h.mu.Lock()
	h.active = nil
	h.nextID = session.NewID(h.now())
	id := h.nextID
	h.mu.Unlock()
	h.followScratch(id)
}

// List returns every stored session's browsable metadata, newest first (the store's ordering).
func (h *sessionHost) List() ([]session.Meta, error) { return h.store.List() }

// Load returns a stored record; it does NOT change the active session. Activation is deferred to
// Activate so the /sessions resume flow switches which file Saves target only after the live
// RestoreSession has succeeded — a restore that then fails leaves the current session's file
// untouched (subsequent Saves keep updating it, not the loaded one).
func (h *sessionHost) Load(id string) (session.Record, error) {
	return h.store.Load(id)
}

// Activate makes meta's session the one subsequent Saves update, replacing the current active
// session rather than forking a new file — the /sessions resume flow calls it once RestoreSession
// has confirmed the switch. Its id, Title, and CreatedAt carry over so a later Save preserves them.
func (h *sessionHost) Activate(meta session.Meta) {
	h.mu.Lock()
	h.active = &activeSession{id: meta.ID, title: meta.Title, createdAt: meta.CreatedAt}
	h.mu.Unlock()
	// The scratch dir follows the activation: the resumed session's own dir (re)exists and is
	// what the engine fences the next tool call to.
	h.followScratch(meta.ID)
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

// Delete removes a stored session's file.
func (h *sessionHost) Delete(id string) error { return h.store.Delete(id) }

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
func resolveResumeArg(store *session.Store, arg string) (session.Record, error) {
	if rec, err := store.Load(arg); err == nil {
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
// alternatives.
func resolveContinue(store *session.Store, workspace string) (session.Record, error) {
	metas, err := store.List()
	if err != nil {
		return session.Record{}, err
	}
	for _, m := range metas {
		if m.Workspace == workspace {
			return store.Load(m.ID)
		}
	}
	return session.Record{}, fmt.Errorf(
		"apogee: no saved sessions for this workspace (%s) — start one, or resume another with "+
			"--resume <id> (see /sessions)", workspace)
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
		Transcript: rec.Transcript,
		Title:      rec.Meta.Title,
		CtxUsed:    rec.Meta.CtxUsed,
		Usage:      rec.Meta.Usage,
		UserMsgs:   rec.Meta.UserMsgs,
		InExchange: inExchange,
	}
}
