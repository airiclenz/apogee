package session

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

// liveClock is the fixed instant the Live tests mint ids and stamp births at.
var liveClock = time.Date(2026, 9, 24, 9, 30, 0, 0, time.UTC)

// followerLog records the ids a Live's onMove followers were called with.
type followerLog struct {
	mu  sync.Mutex
	ids []string
}

// follow is the onMove callback that appends to the log.
func (f *followerLog) follow(id string) {
	f.mu.Lock()
	f.ids = append(f.ids, id)
	f.mu.Unlock()
}

// moves returns a copy of the ids recorded so far.
func (f *followerLog) moves() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.ids)
}

// newTestLive builds a Live over a fresh store with the fixed clock and one logged follower.
func newTestLive(t *testing.T, resumed *Record) (*Live, *Store, *followerLog) {
	t.Helper()
	store := NewStore(filepath.Join(t.TempDir(), "sessions"))
	log := &followerLog{}
	live := NewLive(store, func() time.Time { return liveClock }, resumed, log.follow)
	t.Cleanup(live.Close)
	return live, store, log
}

// assertLiveHeld fails unless the live-instance hold on id is (want) owned by this process: a Hold
// through the store is refused with a *HeldError exactly while something in this process holds it.
func assertLiveHeld(t *testing.T, store *Store, id string, want bool) {
	t.Helper()
	release, err := store.Hold(id)
	if err == nil {
		_ = release()
	}
	var held *HeldError
	got := errors.As(err, &held)
	if got != want {
		t.Fatalf("hold on %q live = %v (Hold err %v), want %v", id, got, err, want)
	}
	if got && held.PID != os.Getpid() {
		t.Errorf("the holder of %q is pid %d, want this process (%d)", id, held.PID, os.Getpid())
	}
}

// begin runs Begin at the fixed clock and fails the test on a refusal.
func begin(t *testing.T, live *Live, title string) Identity {
	t.Helper()
	identity, err := live.Begin(title, liveClock)
	if err != nil {
		t.Fatalf("Begin(%q): %v", title, err)
	}
	return identity
}

// A fresh Live pre-mints the id at construction; the first Begin adopts it as the record's birth and
// later Begins keep the birth title and instant.
func TestLiveMintsAtConstructionAndBeginAdopts(t *testing.T) {
	t.Parallel()
	live, _, _ := newTestLive(t, nil)
	minted := live.ID()

	if minted == "" || live.ActiveID() != "" {
		t.Fatalf("construction: ID %q, ActiveID %q; want a pre-minted id and no active one", minted, live.ActiveID())
	}
	first := begin(t, live, "first title")
	later, err := live.Begin("second title", liveClock.Add(time.Hour))
	if err != nil {
		t.Fatalf("second Begin: %v", err)
	}

	want := Identity{ID: minted, Title: "first title", CreatedAt: liveClock}
	if first != want || later != want {
		t.Errorf("Begin identities = %+v then %+v, want both %+v", first, later, want)
	}
	if live.ActiveID() != minted {
		t.Errorf("ActiveID = %q after Begin, want %q", live.ActiveID(), minted)
	}
}

// A run that never saves holds nothing; the first Begin takes the hold and Close lets it go.
func TestLiveHoldsAtBeginNotAtTheMint(t *testing.T) {
	t.Parallel()
	live, store, _ := newTestLive(t, nil)
	id := live.ID()

	assertLiveHeld(t, store, id, false)
	begin(t, live, "t")
	assertLiveHeld(t, store, id, true)
	live.Close()
	live.Close() // idempotent

	assertLiveHeld(t, store, id, false)
}

// NewLive on a resumed record begins active on it, carrying its identity, and holds it from there.
func TestLiveResumedBeginsActiveAndHeld(t *testing.T) {
	t.Parallel()
	meta := Meta{ID: NewID(liveClock), Title: "resumed", CreatedAt: liveClock.Add(-time.Hour), ParentID: "parent"}
	live, store, log := newTestLive(t, &Record{Meta: meta})

	assertLiveHeld(t, store, meta.ID, true)
	got := begin(t, live, "ignored")

	want := Identity{ID: meta.ID, Title: "resumed", CreatedAt: meta.CreatedAt, ParentID: "parent"}
	if got != want {
		t.Errorf("Begin on a resumed Live = %+v, want %+v", got, want)
	}
	if moves := log.moves(); len(moves) != 0 {
		t.Errorf("NewLive/Begin called the followers with %v, want no call", moves)
	}
}

// Neither NewLive nor Begin moves the active id, so neither calls a follower.
func TestLiveNewLiveAndBeginFireNoOnMove(t *testing.T) {
	t.Parallel()
	live, _, log := newTestLive(t, nil)

	begin(t, live, "t")
	begin(t, live, "t")

	if moves := log.moves(); len(moves) != 0 {
		t.Errorf("followers called with %v, want no call", moves)
	}
}

// Rotate releases the closed record's hold, mints a fresh id with no parent pointer, and tells every
// follower, in order, of the new id.
func TestLiveRotateClearsParentIDAndMovesFollowers(t *testing.T) {
	t.Parallel()
	meta := Meta{ID: NewID(liveClock), Title: "child", CreatedAt: liveClock, ParentID: "parent"}
	store := NewStore(filepath.Join(t.TempDir(), "sessions"))
	var order []string
	live := NewLive(store, func() time.Time { return liveClock.Add(time.Minute) }, &Record{Meta: meta},
		func(id string) { order = append(order, "first:"+id) },
		func(id string) { order = append(order, "second:"+id) })
	t.Cleanup(live.Close)

	live.Rotate()
	next := live.ID()
	fresh := begin(t, live, "fresh")

	if next == meta.ID || live.ActiveID() != next {
		t.Fatalf("Rotate kept the id: ID %q, ActiveID %q, rotated from %q", next, live.ActiveID(), meta.ID)
	}
	if fresh.ParentID != "" || fresh.Title != "fresh" {
		t.Errorf("identity after Rotate = %+v, want title %q and no ParentID", fresh, "fresh")
	}
	assertLiveHeld(t, store, meta.ID, false)
	if want := []string{"first:" + next, "second:" + next}; !slices.Equal(order, want) {
		t.Errorf("follower calls = %v, want %v", order, want)
	}
}

// Park holds a record ahead of its Activate, which adopts that hold in place of the live one and
// moves the followers; the identity Activate carries (title, birth, parent) is what Begin returns.
func TestLiveParkThenActivateAdoptsTheHold(t *testing.T) {
	t.Parallel()
	live, store, log := newTestLive(t, nil)
	current := begin(t, live, "current").ID
	other := Meta{ID: NewID(liveClock.Add(time.Second)), Title: "other", CreatedAt: liveClock.Add(-time.Hour), ParentID: "p"}

	if err := live.Park(other.ID); err != nil {
		t.Fatalf("Park: %v", err)
	}
	assertLiveHeld(t, store, other.ID, true)
	assertLiveHeld(t, store, current, true) // parked beside the live hold, not in place of it
	if live.ActiveID() != current {
		t.Fatalf("Park moved the active id to %q", live.ActiveID())
	}
	if err := live.Activate(other); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	assertLiveHeld(t, store, other.ID, true)
	assertLiveHeld(t, store, current, false)
	want := Identity{ID: other.ID, Title: "other", CreatedAt: other.CreatedAt, ParentID: "p"}
	if got := begin(t, live, "ignored"); got != want {
		t.Errorf("Begin after Activate = %+v, want %+v", got, want)
	}
	if moves := log.moves(); !slices.Equal(moves, []string{other.ID}) {
		t.Errorf("follower calls = %v, want [%s]", moves, other.ID)
	}
}

// Activate of an id nothing parked (a fork's child) holds it afresh and releases a hold parked on
// some other id.
func TestLiveActivateWithoutParkHoldsAfresh(t *testing.T) {
	t.Parallel()
	live, store, _ := newTestLive(t, nil)
	current := begin(t, live, "current").ID
	parked := NewID(liveClock.Add(time.Second))
	child := Meta{ID: NewID(liveClock.Add(2 * time.Second)), Title: "child", ParentID: current}
	if err := live.Park(parked); err != nil {
		t.Fatalf("Park: %v", err)
	}

	if err := live.Activate(child); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	assertLiveHeld(t, store, child.ID, true)
	assertLiveHeld(t, store, current, false)
	assertLiveHeld(t, store, parked, false)
}

// A hold Activate cannot take is returned, and the followers still move: the identity moved.
func TestLiveActivateRefusedStillMovesFollowers(t *testing.T) {
	t.Parallel()
	live, store, log := newTestLive(t, nil)
	target := Meta{ID: NewID(liveClock.Add(time.Second)), Title: "held elsewhere"}
	release, err := store.Hold(target.ID) // the other instance
	if err != nil {
		t.Fatalf("Hold: %v", err)
	}
	defer func() { _ = release() }()

	err = live.Activate(target)

	var held *HeldError
	if !errors.As(err, &held) {
		t.Errorf("Activate of a held record err = %v, want a *HeldError", err)
	}
	if live.ActiveID() != target.ID {
		t.Errorf("ActiveID = %q, want the activated %q", live.ActiveID(), target.ID)
	}
	if moves := log.moves(); !slices.Equal(moves, []string{target.ID}) {
		t.Errorf("follower calls = %v, want [%s]", moves, target.ID)
	}
	if _, err := live.Begin("t", liveClock); !errors.As(err, &held) {
		t.Errorf("Begin while another instance holds the record err = %v, want a *HeldError", err)
	}
}

// Park refuses a record another apogee holds, and releases an earlier parked hold only when it
// parks a new one.
func TestLiveParkRefusesAHeldRecord(t *testing.T) {
	t.Parallel()
	live, store, _ := newTestLive(t, nil)
	earlier := NewID(liveClock.Add(time.Second))
	target := NewID(liveClock.Add(2 * time.Second))
	later := NewID(liveClock.Add(3 * time.Second))
	release, err := store.Hold(target) // the other instance
	if err != nil {
		t.Fatalf("Hold: %v", err)
	}
	defer func() { _ = release() }()
	if err := live.Park(earlier); err != nil {
		t.Fatalf("Park(earlier): %v", err)
	}

	err = live.Park(target)

	var held *HeldError
	if !errors.As(err, &held) || held.ID != target {
		t.Errorf("Park of a held record err = %v, want a *HeldError on %q", err, target)
	}
	if err := live.Park(later); err != nil {
		t.Fatalf("Park(later): %v", err)
	}
	assertLiveHeld(t, store, earlier, false)
	assertLiveHeld(t, store, later, true)
}

// Park of an id this Live already holds — the active one, the one held, the one parked — neither
// probes nor parks, since a second flock would refuse this process's own pid.
func TestLiveParkOfItsOwnHoldIsNoRefusal(t *testing.T) {
	t.Parallel()
	live, store, _ := newTestLive(t, nil)
	active := begin(t, live, "active").ID
	parked := NewID(liveClock.Add(time.Second))

	if err := live.Park(active); err != nil {
		t.Errorf("Park of the active id was refused: %v", err)
	}
	if err := live.Park(parked); err != nil {
		t.Fatalf("Park: %v", err)
	}
	if err := live.Park(parked); err != nil {
		t.Errorf("a second Park of the parked id was refused: %v", err)
	}

	assertLiveHeld(t, store, active, true)
	assertLiveHeld(t, store, parked, true)
	if err := live.Activate(Meta{ID: active, Title: "active"}); err != nil {
		t.Errorf("Activate of the id already held: %v", err)
	}
	assertLiveHeld(t, store, active, true)
}

// An unadopted parked hold is released by DropParked of its id, by Rotate and by Close; DropParked of
// another id leaves it.
func TestLiveDropParkedRotateAndCloseReleaseAParkedHold(t *testing.T) {
	t.Parallel()
	for name, drop := range map[string]func(live *Live, parked string){
		"DropParked": func(live *Live, parked string) { live.DropParked(parked) },
		"Rotate":     func(live *Live, _ string) { live.Rotate() },
		"Close":      func(live *Live, _ string) { live.Close() },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			live, store, _ := newTestLive(t, nil)
			parked := NewID(liveClock.Add(time.Second))
			if err := live.Park(parked); err != nil {
				t.Fatalf("Park: %v", err)
			}
			live.DropParked(NewID(liveClock.Add(time.Minute)))
			assertLiveHeld(t, store, parked, true)

			drop(live, parked)

			assertLiveHeld(t, store, parked, false)
		})
	}
}

// Retitle renames the active identity in place, so the next Begin carries the new title; a rename
// of any other id leaves the active identity alone.
func TestLiveRetitleRenamesOnlyTheActiveIdentity(t *testing.T) {
	t.Parallel()
	live, _, _ := newTestLive(t, nil)
	active := begin(t, live, "birth title").ID

	live.Retitle(NewID(liveClock.Add(time.Second)), "not mine")
	unchanged := begin(t, live, "ignored")
	live.Retitle(active, "renamed")
	renamed := begin(t, live, "ignored")

	if unchanged.Title != "birth title" {
		t.Errorf("a rename of another id changed the active title to %q", unchanged.Title)
	}
	if renamed.Title != "renamed" || renamed.ID != active {
		t.Errorf("after Retitle the identity is %+v, want id %q titled %q", renamed, active, "renamed")
	}
}
