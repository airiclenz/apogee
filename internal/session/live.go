package session

import (
	"sync"
	"time"
)

// Identity is what a Live session's records carry from one Save to the next: the id minted once
// (or seeded by a resume), the Title fixed at the record's birth — rewritten only by Retitle —, the
// CreatedAt of that birth, and the ParentID a fork wrote. The parent pointer travels with the
// identity it was born with, through the Activate that adopts a forked child and the resume that
// starts on one alike, so no Save of a child writes ParentID "" over the pointer the fork wrote; a
// fresh identity (Begin after construction or Rotate) has none, being no fork.
type Identity struct {
	ID        string
	Title     string
	CreatedAt time.Time
	ParentID  string
}

// Live owns the identity of the session a Driver is running — which record its Saves target, when
// that identity is minted, moved and dropped — and the record's live-instance hold (Store.Hold) that
// follows it. It is the one home of the rules every Driver that persists a running conversation
// shares:
//
//   - The id is minted at the session BOUNDARY (NewLive, Rotate), not at the first Save: tool calls
//     run before a Turn is ever saved, and whatever is named by the id — a scratch dir, an undo
//     store — must exist before the first of them. An id is only a name; nothing reaches the store
//     until a Driver saves.
//   - The hold is taken at the record's BIRTH — Begin, or NewLive on a resumed record — never at the
//     mint, so a run that never saves touches no disk (ADR 0022). It is kept, not re-taken, across
//     every Begin and Activate of the id already held: flock refuses a second open of one path in one
//     process with this process's own pid, so re-acquiring would refuse ourselves.
//   - Park takes a hold ahead of the Activate that will adopt it — the /sessions resume door, which
//     must refuse a record another apogee holds before anything is restored. A parked hold nobody
//     adopts is released at the next identity boundary: Rotate, Close, DropParked of its id, or the
//     next Park.
//   - Followers (the onMove callbacks) hear every move of the active id — Rotate and Activate — and
//     only those: NewLive and Begin move nothing, so they call none, and a Driver seeds its boot
//     state from ID itself. Followers run in order, outside Live's mutex, since they reach into
//     engine state that may call back.
//
// Live resolves no resume argument and sweeps nothing (ADR 0083 §5): the door that found the record
// hands it to NewLive. The methods are safe for concurrent use — a Save runs on a Cmd goroutine
// while the browser verbs are driven from the Update loop.
type Live struct {
	store     *Store
	now       func() time.Time
	followers []func(id string)

	mu sync.Mutex
	// active is the identity Saves currently target; nil until Begin adopts nextID, and again after
	// a Rotate.
	active *Identity
	// nextID is the id minted at the last boundary for the next Begin to adopt while active is nil.
	nextID string
	// heldID / release is the live hold this Live owns; "" / nil when it holds none. The pair moves
	// together (holdLocked / releaseLocked).
	heldID  string
	release func() error
	// parkedID / parkedRelease is the hold Park took for an Activate of the same id to adopt; "" /
	// nil when nothing is parked.
	parkedID      string
	parkedRelease func() error
}

// NewLive builds the identity owner over store, minting ids from now (time.Now when nil) and calling
// each of onMove, in order, whenever the active id moves. With resumed non-nil (a --resume or
// --continue start) it begins ACTIVE on that record — its id, Title, CreatedAt and ParentID carried
// over, so later Saves update its file in place — and HOLDS it from here, the record having been
// born before this run. A hold it cannot take (a second instance won the gap since the door probed,
// or the lock file could not be opened) is not a construction failure: the first Begin re-attempts
// it and returns the refusal, Begin being what precedes a write over the other instance's record.
// A fresh start instead pre-mints the id its first Begin will adopt and holds nothing. No follower
// is called.
func NewLive(store *Store, now func() time.Time, resumed *Record, onMove ...func(id string)) *Live {
	if now == nil {
		now = time.Now
	}
	l := &Live{store: store, now: now, followers: onMove}
	if resumed == nil {
		l.nextID = NewID(now())
		return l
	}
	l.active = identityOf(resumed.Meta)
	// Refusal deferred by design: Begin re-attempts the hold and reports it (see the doc above).
	_ = l.holdLocked(resumed.Meta.ID)
	return l
}

// identityOf projects a record's Meta onto the identity a Live carries.
func identityOf(meta Meta) *Identity {
	return &Identity{ID: meta.ID, Title: meta.Title, CreatedAt: meta.CreatedAt, ParentID: meta.ParentID}
}

// Begin returns the identity the Save about to run writes under, making it the record's birth when
// no identity is active: the id pre-minted at the last boundary is adopted (NewLive and Rotate, the
// only ways to an inactive Live, both mint one), with title and at as its Title and CreatedAt. Both
// are ignored once an identity is active, so a later Save never rewrites them. It then holds that id
// (kept when already held). A refused hold — the record is open in another apogee, a *HeldError — or
// a failed one is returned with a zero Identity BEFORE the caller writes anything: two instances
// never write one record. The identity adopted stays active either way, so the next Begin retries
// the same record. No follower is called.
func (l *Live) Begin(title string, at time.Time) (Identity, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active == nil {
		l.active = &Identity{ID: l.nextID, Title: title, CreatedAt: at}
		l.nextID = ""
	}
	if err := l.holdLocked(l.active.ID); err != nil {
		return Identity{}, err
	}
	return *l.active, nil
}

// Rotate closes the active identity and pre-mints the fresh id the next Begin adopts (the /clear
// and /new boundary), then calls every follower with it. The closed record's hold goes with it — it
// is another run's to resume from here — and so does a parked hold nobody adopted: the boundary is
// the human moving on from that resume. The fresh identity carries no ParentID and holds nothing
// until its first Begin.
func (l *Live) Rotate() {
	l.mu.Lock()
	l.releaseParkedLocked()
	l.releaseLocked()
	l.active = nil
	l.nextID = NewID(l.now())
	id := l.nextID
	l.mu.Unlock()
	l.move(id)
}

// Park takes the hold on id ahead of the Activate that will adopt it, so a record another apogee
// holds is refused here — the *HeldError the /sessions browser notes — before the Driver restores
// anything. A hold parked earlier and never adopted is released first. An id this Live holds already
// (the active one, the one held or the one parked) is neither probed nor parked: the hold guards
// against OTHER instances, and a second flock on it would refuse this process's own pid.
func (l *Live) Park(id string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.holdsLocked(id) {
		return nil
	}
	l.releaseParkedLocked()
	release, err := l.store.Hold(id)
	if err != nil {
		return err
	}
	l.parkedID, l.parkedRelease = id, release
	return nil
}

// Activate makes meta's record the active identity — its id, Title, CreatedAt and ParentID carried
// over — and moves the hold with it: the outgoing hold is released and meta's record held, ADOPTED
// from Park's hold when it parked this id, acquired afresh when not (a fork's child reaches the
// resume flow without a Park), and kept when it is the id already held. A parked hold on some OTHER
// id is released: it was for an Activate that never came. Every follower is then called with the new
// id — even when the hold was refused, which is returned for the caller to report or leave to the
// next Begin (a *HeldError, or the failure to open the lock file).
func (l *Live) Activate(meta Meta) error {
	l.mu.Lock()
	l.active = identityOf(meta)
	var err error
	if l.parkedRelease != nil && l.parkedID == meta.ID {
		l.releaseLocked()
		l.heldID, l.release = l.parkedID, l.parkedRelease
		l.parkedID, l.parkedRelease = "", nil
	} else {
		l.releaseParkedLocked()
		err = l.holdLocked(meta.ID)
	}
	l.mu.Unlock()
	l.move(meta.ID)
	return err
}

// DropParked releases the hold Park took on id when nothing adopted it — the resume whose restore
// failed, now being deleted instead, so the store's own hold (Store.Delete) does not refuse this
// process. A hold parked on another id, or none, is left alone.
func (l *Live) DropParked(id string) {
	l.mu.Lock()
	if l.parkedRelease != nil && l.parkedID == id {
		l.releaseParkedLocked()
	}
	l.mu.Unlock()
}

// Retitle mirrors a rename of the record id onto the active identity when id is the active one, so
// the next Begin returns the new title rather than the birth one; any other id is not this Live's to
// change. The record on disk is the caller's to rename (Store.Rename).
func (l *Live) Retitle(id, title string) {
	l.mu.Lock()
	if l.active != nil && l.active.ID == id {
		l.active.Title = title
	}
	l.mu.Unlock()
}

// ID reports the id the next Save writes under — the active identity's, or the id pre-minted for the
// first Begin to adopt. Unlike ActiveID it answers before any Save has run, which is the window a
// Driver seeds its boot state (a scratch dir, an undo store) and names a fork's parent in.
func (l *Live) ID() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active != nil {
		return l.active.ID
	}
	return l.nextID
}

// ActiveID reports the active identity's id, or "" before the first Begin (and after a Rotate).
func (l *Live) ActiveID() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active == nil {
		return ""
	}
	return l.active.ID
}

// Close releases the live hold and any hold parked that no Activate adopted, so the record is free
// to resume elsewhere the moment this run is done writing it. The identity itself stays readable.
// Idempotent; no follower is called.
func (l *Live) Close() {
	l.mu.Lock()
	l.releaseParkedLocked()
	l.releaseLocked()
	l.mu.Unlock()
}

// move calls every follower with id, in order. Callers must not hold l.mu.
func (l *Live) move(id string) {
	for _, follow := range l.followers {
		follow(id)
	}
}

// holdLocked makes id the record this Live holds: a no-op when it is held already, otherwise the
// previous hold is released and id's taken through the store. A refused or failed hold leaves
// nothing held and is returned; the identity the caller adopted is unaffected. Callers hold l.mu.
func (l *Live) holdLocked(id string) error {
	if l.release != nil && l.heldID == id {
		return nil
	}
	l.releaseLocked()
	release, err := l.store.Hold(id)
	if err != nil {
		return err
	}
	l.heldID, l.release = id, release
	return nil
}

// releaseLocked lets go of the live hold, if any. A release cannot fail in a way Live could repair —
// the descriptor closes and the kernel drops the lock regardless — so its error is discarded.
// Callers hold l.mu.
func (l *Live) releaseLocked() {
	if l.release != nil {
		_ = l.release()
	}
	l.heldID, l.release = "", nil
}

// releaseParkedLocked lets go of the parked hold, if any; its error is discarded for releaseLocked's
// reason. Callers hold l.mu.
func (l *Live) releaseParkedLocked() {
	if l.parkedRelease != nil {
		_ = l.parkedRelease()
	}
	l.parkedID, l.parkedRelease = "", nil
}

// holdsLocked reports whether id is one this Live holds already — the live hold, the active
// identity's or the parked one — so Park neither probes nor parks it. Callers hold l.mu.
func (l *Live) holdsLocked(id string) bool {
	return (l.release != nil && l.heldID == id) || (l.active != nil && l.active.ID == id) ||
		(l.parkedRelease != nil && l.parkedID == id)
}
