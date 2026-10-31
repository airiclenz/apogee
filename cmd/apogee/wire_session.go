package main

// The session seam of the composition root, lifted out of wire.go by concern (ADR 0043).
//
// What a run persists between launches: the host that owns the active session's id and the metadata
// only the binary knows, and the resume resolution a --resume/--continue start goes through before
// that host exists. Prompt recall needs no host here: a recall.Store bound to the workspace is the
// TUI's recall seam itself (wire_options.go).

import (
	"bufio"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/eventjson"
	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/snapshot"
	"github.com/airiclenz/apogee/internal/title"
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
// The host also drives the session's live Event journal (ADR 0103), the `<id>.events.jsonl` a
// watcher follows: it is born just after the record's hold — at the first prompt (Prompt, through
// Live.Claim), at a Save that births the record without one, at an Activate, and at boot on a
// resumed record (attachEventJournal) — and Rotate, Activate and Close end and remove it BEFORE they
// hand the hold to Live to release. The journal is the host's own and not a Live follower, because
// Live's moves release the hold before they call a follower, and Close and Begin call none.
//
// model is mutex-guarded: Save runs on a Bubble Tea Cmd goroutine while SetModel is driven from the
// Update loop, so the two can race. Live guards its own state.
type sessionHost struct {
	store *session.Store
	// watched are the `sessions.watch-homes:` entries, in the configured order, each read only
	// through its read-only store (WatchedSessions); empty watches the own home alone.
	watched   []watchedHome
	workspace string
	// currency is the configured `currency:` label a Save writes onto Meta.Currency beside the
	// usage it labels (ADR 0093 decision 6) whenever the active session has no label of its own
	// (sessionLabel). The composition root sets it once, after construction and before the first
	// Save; "" (tests, a host built without one) writes no label.
	currency string
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
	// sessionLabel is the active session's OWN currency label — what session.Meta.EffectiveCurrency
	// keeps of a resumed or activated record (its label, once it carries a priced call), "" for a
	// fresh session or a record with nothing priced — which Save writes in place of the configured
	// currency, so a session priced under one label is never relabelled by a later Save (ADR 0093,
	// amendment 2026-10-04). Only the record's own half is held here because the configured label
	// arrives after construction; Save resolves the two. Set by newSessionHost and Activate, cleared
	// by Rotate, and guarded for the reason model is.
	sessionLabel string

	// events is the interactive chain's outermost sink the live Event journal records through, and
	// openEvents opens one record's journal (firingJournal's opener over the store, in production).
	// Both are set once by attachEventJournal, before the first prompt; nil — tests, and any host
	// built without one — keeps no journal at all.
	events     *liveJournalSink
	openEvents run.JournalOpener

	// evMu guards the open journal and serializes every step of its life — a Prompt on the worker
	// goroutine, a Save or a Rotate/Activate on the record-write Cmd, Close at teardown — so no
	// journal is born on an id between its end and the release of that id's hold. It is taken before
	// Live's own lock, never inside it.
	evMu sync.Mutex
	// journal is the open live Event journal, nil while none is; journalID names the record it
	// belongs to.
	journal   run.LiveJournal
	journalID string
	// failedID is the id whose journal would not open — the opener has said why already — so it is
	// not retried, and not said again, until the identity moves.
	failedID string
}

// liveJournalSink is the interactive chain's outermost Event sink (ADR 0103 decision 1): every Event
// is recorded into the session's open live Event journal, if one is open, and forwarded unchanged to
// inner — the stats recorder over the Reaction Runner, never inside it (ADR 0075 D9). The chain is
// built once at boot (wire_boot.go); the session host swaps the journal it records into as the
// session's identity and hold move, so nothing below it is ever rebuilt.
//
// The journal's own sink is read under the mutex and called outside it: a swap never waits on an
// Emit, and an Emit that read a journal a swap then ended writes nothing, since a closed journal
// writes no line (eventjson.Journal.Close).
type liveJournalSink struct {
	inner domain.EventSink

	mu      sync.Mutex
	journal domain.EventSink
}

// newLiveJournalSink builds the sink over inner with no journal open.
func newLiveJournalSink(inner domain.EventSink) *liveJournalSink {
	return &liveJournalSink{inner: inner}
}

// Emit implements domain.EventSink: the journal records ev first, then inner receives it.
func (s *liveJournalSink) Emit(ev domain.Event) {
	s.mu.Lock()
	journal := s.journal
	s.mu.Unlock()
	if journal != nil {
		journal.Emit(ev)
	}
	if s.inner != nil {
		s.inner.Emit(ev)
	}
}

// swap makes journal the sink every later Event is recorded into; nil records none.
func (s *liveJournalSink) swap(journal domain.EventSink) {
	s.mu.Lock()
	s.journal = journal
	s.mu.Unlock()
}

// sessionHost satisfies the persistence seam the TUI drives.
var _ tui.SessionHost = (*sessionHost)(nil)

// watchedHome is one `sessions.watch-homes:` entry as the binary reads it (ADR 0103 decision 7): the
// label its rows are tagged with and a read-only view of its sessions directory — the only door to
// a watched home this binary builds, so nothing there can be created, written, swept, truncated or
// held.
type watchedHome struct {
	label string
	store *session.ReadOnlyStore
}

// sessionsDirName is the directory under an apogee home its sessions live in (stateRoots.sessions).
const sessionsDirName = "sessions"

// watchedHomesOf builds the read-only views of the configured watched homes, in their order. It
// touches no disk: a home is read only when the browser lists it or --resume looks in it.
func watchedHomesOf(homes []config.WatchHome) []watchedHome {
	out := make([]watchedHome, 0, len(homes))
	for _, home := range homes {
		out = append(out, watchedHome{
			label: home.Label,
			store: session.NewReadOnlyStore(filepath.Join(home.Path, sessionsDirName)),
		})
	}
	return out
}

// sessions lists the home's sessions for the browser: every record it lists, newest first, each
// marked live when its owner runs it now, then every live session with no record yet. A directory
// that will not list — the record scan or the journal scan — is the home's error, and no rows.
func (w watchedHome) sessions() ([]tui.WatchedSession, error) {
	metas, err := w.store.List()
	if err != nil {
		return nil, err
	}
	entries, err := w.store.Live()
	if err != nil {
		return nil, err
	}
	live := make(map[string]tui.LiveSession, len(entries))
	for _, entry := range entries {
		if row, ok := liveSessionOf(w.store, entry, w.label); ok {
			live[entry.ID] = row
		}
	}
	rows := make([]tui.WatchedSession, 0, len(metas)+len(live))
	for _, meta := range metas {
		row := tui.WatchedSession{Home: w.label, Meta: meta, RecordPath: w.store.RecordPath(meta.ID)}
		if running, isLive := live[meta.ID]; isLive {
			row.Live = &running
			delete(live, meta.ID)
		}
		rows = append(rows, row)
	}
	for _, entry := range entries {
		if running, isLive := live[entry.ID]; isLive {
			rows = append(rows, tui.WatchedSession{Home: w.label, RecordPath: running.RecordPath, Live: &running})
		}
	}
	return rows, nil
}

// finished is the watch a --resume of id opens on a record this home lists, with no journal to
// follow: the record painted, shown finished, never resumable. ok is false when the home lists no
// such record, or will not list.
func (w watchedHome) finished(id string) (tui.LiveSession, bool) {
	metas, err := w.store.List()
	if err != nil {
		return tui.LiveSession{}, false
	}
	for _, meta := range metas {
		if meta.ID == id {
			return tui.LiveSession{
				ID:         id,
				Title:      meta.Title,
				Workspace:  meta.Workspace,
				At:         meta.UpdatedAt,
				RecordPath: w.store.RecordPath(id),
				Home:       w.label,
			}, true
		}
	}
	return tui.LiveSession{}, false
}

// sessionsReader is what the doors of a watch read a home's sessions through — the own home's
// *session.Store and a watched home's *session.ReadOnlyStore alike: the hold probe and the
// journal's name, neither of which holds, writes or creates anything.
type sessionsReader interface {
	ProbeHold(id string) (held bool, pid int, err error)
	JournalPath(id string) string
}

// newSessionHost builds the host over a store and the run's wiring facts, and the session.Live it
// runs on. When resumed is non-nil (a --resume/--continue start) Live begins ACTIVE on that record,
// so subsequent Saves update its file in place — its id, CreatedAt, Title and ParentID carried over
// rather than a new session forked — and HOLDS it from here, the record having been born before this
// run. The door that resolved the record (resolveResume) probed the hold and let go of it a moment
// ago, so this is the one real hold of the run; one Live cannot take is re-attempted, and reported,
// by the first Save. A fresh start instead PRE-MINTS the id its first Save will adopt, so the
// session's scratch dir can exist before the first tool call, and holds nothing until its first
// prompt (Prompt) or that Save. watched are the configured watched homes the browser mixes in
// (WatchedSessions); the host only ever reads them.
// scratchRoot and scratchMoved wire the scratch seam, snapshotsRoot and journalMoved the undo-store
// seam beside it ("" / nil disable either — see the fields); followScratch and followJournal are
// Live's followers, in that order, and construction calls neither (the composition root seeds its
// boot state from SessionScratchDir and SessionID).
func newSessionHost(store *session.Store, watched []watchedHome, workspace, model string,
	resumed *session.Record,
	scratchRoot string, scratchMoved func(dir string),
	snapshotsRoot string, journalMoved func(id string)) *sessionHost {
	h := &sessionHost{store: store, watched: watched, workspace: workspace, model: model, now: time.Now,
		scratchRoot: scratchRoot, scratchMoved: scratchMoved,
		snapshotsRoot: snapshotsRoot, journalMoved: journalMoved}
	if resumed != nil {
		h.sessionLabel = resumed.Meta.EffectiveCurrency("")
	}
	h.live = session.NewLive(store, func() time.Time { return h.now() }, resumed,
		h.followScratch, h.followJournal)
	return h
}

// Close releases the live-instance hold — and any hold Load parked that no Activate adopted — at the
// end of the run, after the engine has closed, so the record is free to resume elsewhere the moment
// this apogee is done writing it. The live Event journal ends and is removed first, under the hold
// still: the final save has landed by then, and a journal on disk only ever names a session still
// running or one a crash left for the startup sweep. Idempotent.
func (h *sessionHost) Close() {
	h.evMu.Lock()
	defer h.evMu.Unlock()
	h.endEventJournalLocked()
	h.live.Close()
}

// attachEventJournal hands the host the chain's journal sink and the opener it opens a record's
// journal with — the composition root's one call, after construction and before the first prompt.
// A host resumed on a record holds it from construction, and that record has had its prompt, so its
// journal is born here; a fresh host's waits for its first prompt.
func (h *sessionHost) attachEventJournal(events *liveJournalSink, open run.JournalOpener) {
	h.evMu.Lock()
	defer h.evMu.Unlock()
	h.events, h.openEvents = events, open
	if h.live.ActiveID() == "" {
		return
	}
	if ident, err := h.live.Claim(); err == nil {
		h.openEventJournalLocked(ident)
	}
}

// Prompt is the engine holder's word that a prompt was submitted (interjection false) or an
// Interjection committed (true): the record is held from here — the first prompt is the moment a
// fresh session's hold is taken (Live.Claim, ADR 0103 decision 10) — its live Event journal is born
// if it has none, and the prompt is written to it as a `prompt` line (decision 11). A hold that is
// refused or fails opens no journal: the Save that follows re-attempts it and reports it, refusing
// before it writes. A fresh record's journal is titled the way its record will be (title.Derive
// over the first prompt, the rule the TUI's own title follows).
func (h *sessionHost) Prompt(text string, interjection bool) {
	h.evMu.Lock()
	defer h.evMu.Unlock()
	if h.events == nil {
		return
	}
	if h.journal == nil {
		ident, err := h.live.Claim()
		if err != nil {
			return
		}
		if ident.Title == "" {
			ident.Title = title.Derive(text, title.MaxRunes, h.now())
		}
		h.openEventJournalLocked(ident)
	}
	if h.journal != nil {
		h.journal.Prompt(text, interjection)
	}
}

// openEventJournalLocked opens the live Event journal of ident's record — held by the caller's
// Claim, Begin or Activate — unless one is open already, the host keeps none, or that record's
// journal already failed to open. Entries is the record's transcript length at this birth. Callers
// hold evMu.
func (h *sessionHost) openEventJournalLocked(ident session.Identity) {
	if h.events == nil || h.openEvents == nil || h.journal != nil || ident.ID == h.failedID {
		return
	}
	journal := h.openEvents(run.JournalStart{
		RecordID:  ident.ID,
		Title:     ident.Title,
		Workspace: h.workspace,
		Entries:   h.recordEntries(ident.ID),
	})
	if journal == nil {
		h.failedID = ident.ID
		return
	}
	h.journal, h.journalID = journal, ident.ID
	h.events.swap(journal.Wrap(nil))
}

// endEventJournalLocked takes the open live Event journal down — no Event is recorded into it from
// here, its closing frame is written and its file closed and removed — and forgets a failed id, the
// identity being about to move. Callers hold evMu, and the hold of the journal's record still.
func (h *sessionHost) endEventJournalLocked() {
	h.failedID = ""
	if h.journal == nil {
		return
	}
	h.events.swap(nil)
	h.journal.End()
	h.journal, h.journalID = nil, ""
}

// recordEntries counts the transcript entries of id's stored record — the count a watcher paints
// from the record before it follows the journal (ADR 0103 decision 3). A record not written yet, the
// fresh session's at its first prompt, counts zero, as does one that will not load or decode.
func (h *sessionHost) recordEntries(id string) int {
	rec, err := h.store.Load(id)
	if err != nil {
		return 0
	}
	entries, err := session.DecodeTranscript(rec.Transcript)
	if err != nil {
		return 0
	}
	return len(entries)
}

// Save persists the active session, fixing its id, Title and CreatedAt on the first call and
// updating that same file thereafter. Title is set at create and never overwritten
// by a later Save — Rename is the only writer that changes it, so a user rename sticks — while
// UpdatedAt, the transcript blob, and the browsable counts refresh every Save. Workspace and Model
// come from the wiring, the facts the renderer cannot know. The two token accountings are stored
// exactly as they arrive — the main agent's and its delegates' — because the record keeps the halves
// of a session's spend apart (session.Meta), and servedModels — the ids the upstream actually
// answered with, which the renderer folds off the readings — is stored beside the bound Model
// rather than in place of it, so the record says both what was asked for and what answered. The
// currency label is the session's own when a resumed or activated record brought a priced one
// (sessionLabel), else the configured label (ADR 0093, amendment 2026-10-04).
//
// The first Save is the record's birth: Live.Begin adopts the pre-minted id with title and this
// Save's moment as its Title and CreatedAt, and takes its live-instance hold — or keeps the one the
// first prompt's Claim took (Prompt); every later Save of the same id finds the hold in place and
// keeps it. A hold that is refused — the record is open in another apogee — or fails is this Save's
// error, returned BEFORE anything is written: two instances never write one record.
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
	// A record born without a prompt — a background workflow the human started before typing any —
	// is held from this Begin, and its journal is born under it.
	h.evMu.Lock()
	h.openEventJournalLocked(a)
	h.evMu.Unlock()
	h.mu.Lock()
	model := h.model
	currency := cmp.Or(h.sessionLabel, h.currency)
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
			Currency:      currency,
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
// adopted; the fresh id holds nothing until its first prompt or Save. The closed session's currency label
// goes too: the fresh session saves under the configured label.
//
// The closed session's live Event journal ends and is removed before Live releases its hold — the
// record-write queue has landed the session's final save ahead of this Rotate — and the fresh id
// gets a journal of its own at its first prompt.
func (h *sessionHost) Rotate() {
	h.mu.Lock()
	h.sessionLabel = ""
	h.mu.Unlock()
	h.evMu.Lock()
	defer h.evMu.Unlock()
	h.endEventJournalLocked()
	h.live.Rotate()
}

// List returns every stored session's browsable metadata, newest first (the store's ordering).
func (h *sessionHost) List() ([]session.Meta, error) { return h.store.List() }

// LiveSessions lists the sessions another apogee is running now, for the /sessions browser to mark
// `● live` and watch (ADR 0103): every session Store.Live finds with an Event journal and a held
// lock, less every id this host holds itself — the active session's, the one a first prompt Claimed
// before any Save, the one Load parked — since the store's probe reads this process's own hold as
// held like any other. The host's own row is never live, so ⏎ on it still reloads it.
//
// It only reads (Store.Live, liveSessionOf): it holds, writes and creates nothing.
func (h *sessionHost) LiveSessions() ([]tui.LiveSession, error) {
	entries, err := h.store.Live()
	if err != nil {
		return nil, err
	}
	out := make([]tui.LiveSession, 0, len(entries))
	for _, entry := range entries {
		if h.live.Holds(entry.ID) {
			continue
		}
		if row, ok := liveSessionOf(h.store, entry, ""); ok {
			out = append(out, row)
		}
	}
	return out, nil
}

// WatchedSessions lists the sessions of every watched home, in the configured order, for the
// /sessions browser to mix in (ADR 0103 decision 7). A home whose sessions directory will not read
// adds one error naming its label to unreadable and no rows; a home with no sessions directory
// lists nothing and is no error.
//
// It only reads, through each home's read-only store: it creates, writes, sweeps, truncates and
// holds nothing there.
func (h *sessionHost) WatchedSessions() ([]tui.WatchedSession, []error) {
	var rows []tui.WatchedSession
	var unreadable []error
	for _, home := range h.watched {
		homeRows, err := home.sessions()
		if err != nil {
			unreadable = append(unreadable, fmt.Errorf("could not read watched home %s: %w", home.label, err))
			continue
		}
		rows = append(rows, homeRows...)
	}
	return rows, unreadable
}

// maxJournalFrameBytes bounds the read of a journal's opening frame: the frame carries a title, a
// workspace root and two small numbers, so a first line past this is not a frame worth reading.
const maxJournalFrameBytes = 64 << 10

// liveSessionOf is the browser's row of one running session: the facts its journal's opening frame
// names, the record path a watch paints from — beside the journal, where the owner's first Save
// puts it — and a probe over the store's read-only ProbeHold. A journal whose first line does not
// decode as a frame — torn, foreign, too long — leaves a session with a record its record's own
// facts; one with no record yet has nothing to show, and is skipped (ok false). home is the label of
// the watched home the session is in, "" for the own home's.
func liveSessionOf(store sessionsReader, entry session.LiveEntry, home string) (tui.LiveSession, bool) {
	frame, at, err := readJournalFrame(entry.JournalPath)
	if err != nil && !entry.HasRecord {
		return tui.LiveSession{}, false
	}
	id := entry.ID
	return tui.LiveSession{
		ID:          id,
		Title:       frame.Title,
		Workspace:   frame.Workspace,
		At:          at,
		RecordPath:  filepath.Join(filepath.Dir(entry.JournalPath), id+".json"),
		JournalPath: entry.JournalPath,
		Home:        home,
		Probe: func() (bool, error) {
			held, _, err := store.ProbeHold(id)
			return held, err
		},
	}, true
}

// readJournalFrame reads a journal's first line — complete, newline-terminated, within
// maxJournalFrameBytes — and returns its opening frame and the moment it was written. The file is
// opened read-only and closed before it returns.
func readJournalFrame(path string) (eventjson.JournalStarted, time.Time, error) {
	f, err := os.Open(path)
	if err != nil {
		return eventjson.JournalStarted{}, time.Time{}, err
	}
	defer func() { _ = f.Close() }()
	raw, err := bufio.NewReader(io.LimitReader(f, maxJournalFrameBytes)).ReadBytes('\n')
	if err != nil {
		return eventjson.JournalStarted{}, time.Time{}, fmt.Errorf("journal %q: no complete opening frame: %w", path, err)
	}
	line, err := eventjson.Decode(raw)
	if err != nil {
		return eventjson.JournalStarted{}, time.Time{}, err
	}
	if line.Started == nil {
		return eventjson.JournalStarted{}, time.Time{}, fmt.Errorf("journal %q: first line is %q, not its opening frame", path, line.Kind)
	}
	var frame struct {
		Data eventjson.JournalStarted `json:"data"`
	}
	if err := json.Unmarshal(raw, &frame); err != nil {
		return eventjson.JournalStarted{}, time.Time{}, err
	}
	return frame.Data, line.Time, nil
}

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
// just came back to rather than those of the one they left (ADR 0074 decision 10). meta's own
// currency label comes along on session.Meta.EffectiveCurrency's terms, so the resumed session's
// later Saves keep the label its priced amount was written under (ADR 0093, amendment 2026-10-04).
func (h *sessionHost) Activate(meta session.Meta) {
	h.mu.Lock()
	h.sessionLabel = meta.EffectiveCurrency("")
	h.mu.Unlock()
	h.evMu.Lock()
	defer h.evMu.Unlock()
	// The outgoing session's live Event journal ends and is removed under its hold, before Live
	// moves the hold — unless meta is the record it already belongs to, whose one hold, and so one
	// journal, goes on. The incoming record has had its prompt, so its journal is born as soon as it
	// is held.
	if meta.ID != h.journalID {
		h.endEventJournalLocked()
	}
	// Activate reports nothing (tui.SessionHost), so a hold Live could not take is left for the next
	// Save to re-attempt and report: the Save is what would write over the other instance's record,
	// and it refuses before writing. The followers below have already moved by then — a refused hold
	// still moves the scratch dir and the undo journal to meta's id, by design (owner call 2026-10-02):
	// the identity moved, so what fences the next tool call and what /undo reaches move with it.
	if err := h.live.Activate(meta); err != nil {
		return
	}
	h.openEventJournalLocked(session.Identity{ID: meta.ID, Title: meta.Title})
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
// It runs on every Activate, a refused hold included (owner call 2026-10-02): the scratch dir
// follows the identity, not the hold.
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
// Like followScratch it runs on every Activate, a refused hold included (owner call 2026-10-02).
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
//
// A --resume id another instance is running with an Event journal resolves to the live session to
// watch instead (resolveLive, ADR 0103 decision 5), and the record is nil: the start is a fresh one
// that opens watching. --continue never watches — it resumes this workspace's own newest record, and
// one another apogee holds is refused (resolveContinue). A --resume id this home does not have is
// looked for in the watched homes too (resolveResumeArg), and opens watching there.
func resolveResume(
	store *session.Store, watched []watchedHome, resume string, continueSession bool, workspace string,
) (*session.Record, *tui.LiveSession, error) {
	switch {
	case resume != "" && continueSession:
		return nil, nil, errors.New("apogee: --resume and --continue are mutually exclusive; pass one or the other")
	case resume != "":
		if live, isLive := resolveLive(store, resume, ""); isLive {
			return nil, &live, nil
		}
		return resolveResumeArg(store, watched, resume)
	case continueSession:
		rec, err := resolveContinue(store, workspace)
		if err != nil {
			return nil, nil, err
		}
		return &rec, nil, nil
	default:
		return nil, nil, nil
	}
}

// resolveLive reports the live session a --resume value names: a valid store id whose lock another
// instance holds (Store.ProbeHold) and whose Event journal is on disk — a record-less headless run
// included — as the row /sessions would list for it (liveSessionOf). Everything else reads as not
// live and is left to resolveResumeArg unchanged: an invalid id or a path (`./x.json`), a probe that
// errs, a held id without a journal (refused there with the friendly line), a free one with a
// journal (an orphan the startup sweep removes), and a journal-only session whose opening frame
// will not read.
//
// It only reads, as a watcher must: the probe never holds the lock, and the journal and record are
// only stat'ed here. store is the own home's store or a watched home's read-only one, whose label
// is home ("" for the own home).
func resolveLive(store sessionsReader, id, home string) (tui.LiveSession, bool) {
	held, pid, err := store.ProbeHold(id)
	if err != nil || !held {
		return tui.LiveSession{}, false
	}
	journal := store.JournalPath(id)
	if _, err := os.Stat(journal); err != nil {
		return tui.LiveSession{}, false
	}
	_, recordErr := os.Stat(filepath.Join(filepath.Dir(journal), id+".json"))
	return liveSessionOf(store, session.LiveEntry{
		ID:          id,
		PID:         pid,
		JournalPath: journal,
		HasRecord:   recordErr == nil,
	}, home)
}

// resolveWatched reports the session of a watched home a --resume id names, from the first home in
// the configured order that has it: live — held with a journal, as resolveLive finds it — or else
// finished, a record the home lists, which the watch paints with no journal to follow (JournalPath
// ""). Either carries its home's label, so it is never resumable. A value that is no valid id —
// a path — matches nothing in any home: the probe refuses it and no listed record declares it. It
// only reads.
func resolveWatched(homes []watchedHome, id string) (tui.LiveSession, bool) {
	for _, home := range homes {
		if live, isLive := resolveLive(home.store, id, home.label); isLive {
			return live, true
		}
		if finished, isFinished := home.finished(id); isFinished {
			return finished, true
		}
	}
	return tui.LiveSession{}, false
}

// resolveResumeArg resolves a --resume value: a store id first (the common case — the id shown in
// /sessions), else a file path (LoadPath, which also wraps a legacy bare envelope). A value that is
// neither a known id nor a readable file is a friendly error naming both interpretations and the
// sessions directory the id was looked for in, so a wrong --config or APOGEE_CONFIG home does not
// read as a wrong id.
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
//
// An id this store has no record of is looked for in the watched homes before it is read as a path
// (resolveWatched): one found there resolves to the watch it opens (the record nil), never to a
// record to resume — a watched home is never resumable.
func resolveResumeArg(
	store *session.Store, watched []watchedHome, arg string,
) (*session.Record, *tui.LiveSession, error) {
	if rec, err := store.Load(arg); err == nil {
		if err := probeHold(store, rec.Meta.ID); err != nil {
			return nil, nil, err
		}
		return &rec, nil, nil
	}
	if live, isWatched := resolveWatched(watched, arg); isWatched {
		return nil, &live, nil
	}
	rec, err := store.LoadPath(arg)
	if err != nil {
		return nil, nil, fmt.Errorf(
			"apogee: --resume %q: not a known session id under %s (see /sessions) nor a readable session file",
			arg, store.Dir())
	}
	rec.Meta.ID = session.NewID(time.Now())
	return &rec, nil, nil
}

// resolveContinue resumes the most recent session recorded for the resolved workspace — the
// --continue convenience that needs no id. List returns metas newest-first, so the first record
// whose Workspace matches is the newest; a workspace with none is a friendly error naming the
// sessions directory it searched and pointing at the alternatives. The newest record is the one --continue means: when another apogee holds it the
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
		"apogee: no saved sessions for this workspace (%s) under %s — start one, or resume another with "+
			"--resume <id> (see /sessions)", workspace, store.Dir())
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
// the interrupted note when the session died mid-task. currency is the configured `currency:`
// label; the payload carries the session's EFFECTIVE label (session.Meta.EffectiveCurrency) — the
// one the host's later Saves write — rather than the record's raw one.
func resumedSession(rec *session.Record, currency string, inExchange bool) *tui.ResumedSession {
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
		Currency:      rec.Meta.EffectiveCurrency(currency),
		UserMsgs:      rec.Meta.UserMsgs,
		InExchange:    inExchange,
	}
}
