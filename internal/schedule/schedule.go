package schedule

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/title"
)

// MinCycle is the floor under a Schedule's cycle. It guards the single-slot local server
// against a typo — `/schedule 5s ...` would hammer the one model slot the interactive
// session is also using — and it lives here rather than in the parser because it is policy,
// and policy belongs to the library (ADR 0033).
const MinCycle = 30 * time.Second

// The conditions a caller can branch on. Every error this package returns wraps one of
// them, so a surface can decide between "your input was wrong" and "this scheduler is gone"
// without matching on message text.
var (
	// ErrNoRunner reports a Config with no Fire seam: a Scheduler that cannot perform a
	// Firing is not a Scheduler.
	ErrNoRunner = errors.New("apogee: a scheduler needs a runner")
	// ErrPrompt reports a Spec with no prompt.
	ErrPrompt = errors.New("apogee: a schedule needs a prompt")
	// ErrCycle reports a cycle under MinCycle.
	ErrCycle = errors.New("apogee: a schedule's cycle is under the floor")
	// ErrMode reports a mode a Firing may not run in: a Schedule is Plan or Auto only,
	// because Ask-Before and Allow-Edits both exist to consult a human and a Firing has none
	// (ADR 0033, decision 2).
	ErrMode = errors.New("apogee: a schedule runs in plan or auto mode only")
	// ErrNotFound reports an id no live Schedule answers to — already stopped, or never
	// created.
	ErrNotFound = errors.New("apogee: no such schedule")
	// ErrClosed reports a Scheduler that has been closed; it accepts nothing further.
	ErrClosed = errors.New("apogee: the scheduler is closed")
)

// Spec is one Schedule's standing instruction: run this prompt, this often, in this mode.
type Spec struct {
	// Name is the display name a surface labels the Schedule and its Firings with. Empty ⇒
	// derived from the prompt, so a caller that has no name to offer still gets a usable one
	// on every record and every note.
	Name string
	// Cycle is how often the Schedule ticks. It must be at least MinCycle.
	Cycle time.Duration
	// Prompt is the single user message every Firing submits.
	Prompt string
	// Mode is the autonomy mode every Firing runs in: domain.ModePlan or domain.ModeAuto.
	// The Auto-eligibility ladder is the caller's gate (see the package doc).
	Mode domain.Mode
}

// Firing is what the runner seam is handed: everything one run needs and nothing about
// cycles, which are the Scheduler's business alone.
type Firing struct {
	// ScheduleID and ScheduleName are the identity the run stamps onto its session record.
	ScheduleID   string
	ScheduleName string
	// Prompt is the Schedule's prompt.
	Prompt string
	// Mode is the Schedule's mode.
	Mode domain.Mode
}

// Outcome is the runner's report about one Firing, passed through. The library reads none
// of these fields — it stays runner-agnostic (ADR 0033) and carries them only so a surface
// can show a human what the Firing did, and what it cost, without a seam of its own onto
// the runner.
type Outcome struct {
	// RecordID is the saved session record's id, empty when the runner persisted nothing.
	RecordID string
	// Title is the saved record's title.
	Title string
	// FinalText is the Firing's answer: the run's final assistant text, empty when it
	// produced none. It is RAW model output — a surface escape-strips it at its own render
	// seam, this library does not (ADR 0010: the answer crosses as plain data).
	FinalText string
	// Turns is how many Turns the Firing took.
	Turns int
	// Denied is how many gated actions the Firing's fail-safe denier refused.
	Denied int
	// Faulted reports that the Firing's final Turn was ABANDONED (run.Result.Faulted). The
	// Firing still RETURNED — the Exchange reached its boundary and this is an EventCompleted
	// carrying a faulted Outcome, not an EventFailed — but FinalText is then the run's last
	// words rather than its answer to the prompt.
	Faulted bool
	// Fault is why that final Turn was abandoned, empty when Faulted is false. Like FinalText it
	// is RAW upstream text — a surface escape-strips it at its own render seam, this library
	// does not.
	Fault string
	// TotalTokens is what the whole Firing spent: the run's OWN cumulative total plus every
	// delegated run's, summed by the runner — the same figure /sessions shows as a session's
	// spend. It is 0 both when the Upstream reported no usage at all and when a caller fills
	// nothing, which a surface treats alike: it omits the reading rather than printing a zero.
	TotalTokens int
	// SubAgents is how many runs the Firing delegated. It is a COUNT and nothing more —
	// per-delegation detail stays on the runner's own report and does not cross this seam. It
	// is 0 on a Firing that delegated nothing, omitted by a surface for the same reason
	// TotalTokens is.
	SubAgents int
}

// Status is one live Schedule as a surface displays it.
type Status struct {
	// ID is the Schedule's id, as returned by Add.
	ID string
	// Name is its display name.
	Name string
	// Cycle is how often it ticks.
	Cycle time.Duration
	// Mode is the mode its Firings run in.
	Mode domain.Mode
	// NextFire is when the next tick is due, on the Scheduler's clock.
	NextFire time.Time
	// Fired counts the Firings that have started; Skipped counts the ticks refused because a
	// Firing was still in flight.
	Fired   int
	Skipped int
	// InFlight reports whether a Firing is pending or running right now — the gate wait
	// counts, since that is precisely the window in which further ticks are skipped.
	InFlight bool
}

// EventKind names what happened to a Schedule.
type EventKind string

// The six things that happen to a Schedule. A surface renders them as notices; nothing in
// this package interprets them.
const (
	// EventCreated is emitted once, when Add accepts a Spec.
	EventCreated EventKind = "created"
	// EventFired is emitted when a Firing begins — after the Gate released it, so it reports
	// a run that is actually under way rather than one that is merely due.
	EventFired EventKind = "fired"
	// EventCompleted is emitted when a Firing returned, carrying its Outcome.
	EventCompleted EventKind = "completed"
	// EventSkipped is emitted for a tick refused because that Schedule's Firing was still in
	// flight (decision 1).
	EventSkipped EventKind = "skipped"
	// EventStopped is emitted when Stop takes a Schedule off the clock.
	EventStopped EventKind = "stopped"
	// EventFailed is emitted when a Firing returned an error, or when the Gate refused for a
	// reason other than the Schedule going away.
	EventFailed EventKind = "failed"
)

// EventKinds lists every EventKind, in the order declared above. It is the set a consumer's
// exhaustive switch is tested against — a drift guard that iterates it catches a kind the switch
// forgot. A new kind is added to both, on this screen: the const block and this slice.
var EventKinds = []EventKind{
	EventCreated,
	EventFired,
	EventCompleted,
	EventSkipped,
	EventStopped,
	EventFailed,
}

// Event is one thing that happened to one Schedule.
type Event struct {
	// Kind is what happened.
	Kind EventKind
	// ScheduleID and ScheduleName identify the Schedule it happened to.
	ScheduleID   string
	ScheduleName string
	// Prompt carries the Firing's prompt on EventFired, and is empty otherwise. It rides the
	// Event rather than being looked up because a surface must be able to show what was
	// submitted without a second seam read — and because the Schedule may have been stopped
	// by the time that surface looks.
	Prompt string
	// Outcome carries the saved run on EventCompleted, whatever the runner reported beside
	// its error on EventFailed, and is zero otherwise.
	Outcome Outcome
	// Elapsed is how long the Fire seam took, measured on the Scheduler's own Clock so every
	// Driver gets the same number and a fake clock pins it. It is set on EventCompleted and
	// EventFailed for a Firing that actually ran, and is zero otherwise — including on the
	// EventFailed of a Gate refusal, which never reached the runner.
	Elapsed time.Duration
	// Err carries the failure on EventFailed, and is nil otherwise.
	Err error
}

// Config carries the seams a Scheduler is built from.
type Config struct {
	// Fire performs one Firing. It is required — see ErrNoRunner. Its context is the
	// Scheduler's own: it is cancelled by Close and by nothing else, so a Firing already
	// under way survives Stop and dies with its Driver.
	Fire func(context.Context, Firing) (Outcome, error)
	// Gate blocks until the host is quiescent, and is consulted before every Firing. nil ⇒
	// no deferral, which is what a daemon with no interactive session in the way wants. Its
	// context is cancelled when the Schedule is stopped or the Scheduler closed, and a Gate
	// MUST honour it — a Gate that ignores cancellation holds Close open.
	Gate func(context.Context) error
	// Notify receives every Event. nil ⇒ events are discarded.
	//
	// It is NEVER called on the goroutine that called Add, Stop or Close: every Event is
	// emitted from a goroutine this Scheduler owns. That is a contract rather than an
	// implementation detail, because the Driver this library exists to serve routes its
	// Notify back into a single-threaded event loop (ADR 0031) — and a Notify called
	// re-entrantly from Add would have that loop waiting for itself.
	Notify func(Event)
	// Clock is the Scheduler's sense of time; nil ⇒ the wall clock and real tickers.
	Clock Clock
}

// Scheduler holds the live Schedules of one Driver and runs their cycles. The zero value is
// not usable; build one with New.
type Scheduler struct {
	fire   func(context.Context, Firing) (Outcome, error)
	gate   func(context.Context) error
	notify func(Event)
	clock  Clock

	// ctx is the root every Firing runs under and every per-Schedule context descends from;
	// cancel is Close's single lever.
	ctx    context.Context
	cancel context.CancelFunc

	// wg counts both the per-Schedule loops and the in-flight Firings, so Close joins
	// everything this Scheduler started. done is closed once that join has happened, which is
	// what makes a second Close wait rather than race past the first.
	wg        sync.WaitGroup
	closeOnce sync.Once
	done      chan struct{}

	// mu guards the live set and every counter on it. Seams are NEVER called while it is
	// held: a Notify that called List would deadlock its own Scheduler. Nor are they called
	// on a caller's goroutine — see Config.Notify.
	mu      sync.Mutex
	minted  int
	entries map[string]*entry
	order   []string
	closed  bool
}

// entry is one live Schedule: its spec, its cadence, its lever, and its counters.
type entry struct {
	id   string
	spec Spec

	ticker Ticker
	// stopCtx ends this Schedule's cycle and any pending Gate wait. It descends from the
	// Scheduler's root, so Close ends it too.
	stopCtx    context.Context
	stopCancel context.CancelFunc

	// stopped records that Stop — rather than Close — ended this Schedule. It is what tells the
	// loop goroutine, which sees only a cancelled stopCtx, whether to emit EventStopped on its
	// way out: Stop is announced and Close is silent (see Close).
	stopped atomic.Bool

	// notifyMu serializes this Schedule's own Events, so the loop goroutine and its Firing
	// goroutine never interleave inside a single Notify call.
	notifyMu sync.Mutex

	// Guarded by Scheduler.mu.
	nextFire time.Time
	fired    int
	skipped  int
	inFlight bool
}

// New builds a Scheduler over cfg's seams. It returns ErrNoRunner when cfg carries no Fire.
//
// The returned Scheduler owns an internal root context rather than taking a parent: its
// lifetime is exactly its Driver's, and Close is the one lever that ends it.
func New(cfg Config) (*Scheduler, error) {
	if cfg.Fire == nil {
		return nil, ErrNoRunner
	}
	clock := cfg.Clock
	if clock == nil {
		clock = systemClock{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		fire:    cfg.Fire,
		gate:    cfg.Gate,
		notify:  cfg.Notify,
		clock:   clock,
		ctx:     ctx,
		cancel:  cancel,
		done:    make(chan struct{}),
		entries: make(map[string]*entry),
	}, nil
}

// Add validates spec, puts the Schedule on the clock and returns its id. The first tick is
// one full cycle away — a Schedule created now does not fire now, because the human who
// typed the command is present and can simply ask.
//
// It returns ErrPrompt, ErrCycle or ErrMode for a Spec that cannot run, and ErrClosed once
// the Scheduler has been closed.
func (s *Scheduler) Add(spec Spec) (string, error) {
	spec, err := spec.normalized()
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return "", ErrClosed
	}
	s.minted++
	e := &entry{
		id:       mintID(s.minted),
		spec:     spec,
		ticker:   s.clock.NewTicker(spec.Cycle),
		nextFire: s.clock.Now().Add(spec.Cycle),
	}
	e.stopCtx, e.stopCancel = context.WithCancel(s.ctx)
	s.entries[e.id] = e
	s.order = append(s.order, e.id)
	s.mu.Unlock()

	// EventCreated is emitted by the loop rather than here, so that Add returns without ever
	// entering the Notify seam on its caller's goroutine (Config.Notify). It is still every
	// Schedule's first Event: the loop emits it before it can observe a tick.
	s.wg.Add(1)
	go s.loop(e)
	return e.id, nil
}

// Stop takes the Schedule off the clock: no further ticks, and any Gate wait it is sitting
// in is cancelled so the pending Firing is abandoned. A Firing that has already STARTED is
// left to finish and still reports its completion — Stop ends the cycle, not the run in
// flight, which is a fresh agent mid-task and belongs to Close's context (ADR 0033).
//
// It returns as soon as the Schedule is off the live set; EventStopped follows from the
// Schedule's own goroutine, so a caller is never inside Notify (Config.Notify). List agrees
// immediately either way — the entry is gone before Stop returns.
//
// It returns ErrNotFound for an id no live Schedule answers to.
func (s *Scheduler) Stop(id string) error {
	s.mu.Lock()
	e, ok := s.entries[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("%w (%q)", ErrNotFound, id)
	}
	delete(s.entries, id)
	for i, held := range s.order {
		if held == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	s.mu.Unlock()

	// The flag is set BEFORE the cancel it explains, so the loop cannot wake on a cancelled
	// stopCtx and read it as a Close. EventStopped is the loop's to emit, which keeps Stop off
	// the Notify seam for the same reason Add stays off it (Config.Notify).
	e.stopped.Store(true)
	e.stopCancel()
	return nil
}

// List reports every live Schedule in creation order — a stable order a surface can render
// without sorting, and the order a human created them in.
func (s *Scheduler) List() []Status {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Status, 0, len(s.order))
	for _, id := range s.order {
		e := s.entries[id]
		out = append(out, Status{
			ID:       e.id,
			Name:     e.spec.Name,
			Cycle:    e.spec.Cycle,
			Mode:     e.spec.Mode,
			NextFire: e.nextFire,
			Fired:    e.fired,
			Skipped:  e.skipped,
			InFlight: e.inFlight,
		})
	}
	return out
}

// Close stops every Schedule, cancels every Firing's context and returns once all of this
// Scheduler's goroutines have exited. It is idempotent, and a second call waits for the
// first to finish rather than racing past it.
//
// Nothing is persisted and no Event is emitted: the Driver is going away, and notices into a
// dying surface are noise. A closed Scheduler accepts nothing further (ErrClosed).
//
// Close must never be called from inside a seam — it waits for the goroutine that would be
// making that call.
func (s *Scheduler) Close() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.entries = make(map[string]*entry)
		s.order = nil
		s.mu.Unlock()

		s.cancel()
		s.wg.Wait()
		close(s.done)
	})
	<-s.done
}

// loop is one Schedule's cadence: every tick is either a Firing or a skip, until the
// Schedule is stopped or the Scheduler closed.
func (s *Scheduler) loop(e *entry) {
	defer s.wg.Done()
	defer e.ticker.Stop()

	s.emit(e, Event{Kind: EventCreated})

	ticks := e.ticker.C()
	for {
		select {
		case <-e.stopCtx.Done():
			// Stop ends a cycle with a notice; Close ends every cycle silently, because the
			// Driver is going away and notices into a dying surface are noise (see Close).
			if e.stopped.Load() {
				s.emit(e, Event{Kind: EventStopped})
			}
			return
		case <-ticks:
			s.tick(e)
		}
	}
}

// tick applies the overlap policy to one due tick: a Firing already in flight (waiting on
// the Gate counts) makes this tick a skip; otherwise the Schedule goes in flight and its
// Firing runs on its own goroutine, which is what lets the cadence keep ticking — and keep
// skipping — while the run takes its time.
func (s *Scheduler) tick(e *entry) {
	s.mu.Lock()
	e.nextFire = s.clock.Now().Add(e.spec.Cycle)
	if e.inFlight {
		e.skipped++
		s.mu.Unlock()
		s.emit(e, Event{Kind: EventSkipped})
		return
	}
	e.inFlight = true
	s.mu.Unlock()

	// Safe against Close's Wait: this runs on the loop goroutine, which the same WaitGroup
	// already counts, so the counter is never zero when the delta lands.
	s.wg.Add(1)
	go s.run(e)
}

// run waits for quiescence and then performs one Firing, reporting both ends of it.
func (s *Scheduler) run(e *entry) {
	defer s.wg.Done()
	defer s.landed(e)

	if s.gate != nil {
		if err := s.gate(e.stopCtx); err != nil {
			// A cancelled wait is Stop or Close taking the Schedule away, and both account for
			// themselves; anything else is the host refusing to release, which the surface has
			// to see.
			if e.stopCtx.Err() == nil {
				s.emit(e, Event{Kind: EventFailed, Err: err})
			}
			return
		}
	}
	if e.stopCtx.Err() != nil {
		// Stopped while the Gate was releasing: a Firing that has not started must not.
		return
	}

	s.mu.Lock()
	e.fired++
	s.mu.Unlock()
	s.emit(e, Event{Kind: EventFired, Prompt: e.spec.Prompt})

	started := s.clock.Now()
	out, err := s.fire(s.ctx, Firing{
		ScheduleID:   e.id,
		ScheduleName: e.spec.Name,
		Prompt:       e.spec.Prompt,
		Mode:         e.spec.Mode,
	})
	elapsed := s.clock.Now().Sub(started)
	if err != nil {
		// The Outcome rides the failure too: a runner that salvaged a partial record still
		// has something a surface can point a human at, and discarding it here would make the
		// partial save unreachable outside the error text.
		s.emit(e, Event{Kind: EventFailed, Outcome: out, Elapsed: elapsed, Err: err})
		return
	}
	s.emit(e, Event{Kind: EventCompleted, Outcome: out, Elapsed: elapsed})
}

// landed clears the in-flight flag, which reopens the Schedule to its next tick.
func (s *Scheduler) landed(e *entry) {
	s.mu.Lock()
	e.inFlight = false
	s.mu.Unlock()
}

// emit stamps the Schedule's identity onto ev and hands it to the Notify seam, serialized
// per Schedule so the loop's skips never interleave with its Firing's own Events.
func (s *Scheduler) emit(e *entry, ev Event) {
	if s.notify == nil {
		return
	}
	ev.ScheduleID = e.id
	ev.ScheduleName = e.spec.Name

	e.notifyMu.Lock()
	defer e.notifyMu.Unlock()
	s.notify(ev)
}

// normalized trims the Spec and applies the library's creation policy: a prompt is required,
// the cycle may not undercut MinCycle, the mode must be one a Firing can run in, and a
// missing name is derived rather than refused.
func (s Spec) normalized() (Spec, error) {
	s.Name = strings.TrimSpace(s.Name)
	s.Prompt = strings.TrimSpace(s.Prompt)

	if s.Prompt == "" {
		return Spec{}, ErrPrompt
	}
	if s.Cycle < MinCycle {
		return Spec{}, fmt.Errorf("%w (%s is under the %s floor)", ErrCycle, s.Cycle, MinCycle)
	}
	if s.Mode != domain.ModePlan && s.Mode != domain.ModeAuto {
		return Spec{}, fmt.Errorf("%w (got %q)", ErrMode, s.Mode)
	}
	if s.Name == "" {
		s.Name = deriveName(s.Prompt)
	}
	return s, nil
}

// nameMax bounds a derived name. It is shorter than a session title's 50 because the name
// is a LABEL — it rides every Firing's record title beside a clock, and it shares the status
// row with a cycle, a mode and two counters.
const nameMax = 40

// deriveName makes a display name out of a prompt: its first line, truncated at the last
// word boundary that leaves most of the budget intact. The prompt is already known
// non-empty, so every Schedule gets a name.
//
// The clipping itself is the shared rule in internal/title, at this package's own narrower cap:
// a name is not a session title, but it is cut the same way, and the copy that used to stand
// here said so at length.
func deriveName(prompt string) string {
	return title.Clip(prompt, nameMax)
}

// idRandomBytes is how much randomness a schedule id carries.
const idRandomBytes = 4

// mintID makes a schedule id from a per-Scheduler ordinal and hex randomness. The ordinal
// keeps ids unique within one process even if the randomness source fails; the randomness
// keeps them unique ACROSS processes, which matters because a Firing carries the id onto a
// session record that outlives the Scheduler (ADR 0033, decision 4).
func mintID(ordinal int) string { return mintIDFrom(ordinal, cryptorand.Reader) }

// mintIDFrom is mintID's seam: an explicit randomness source lets a test pin the suffix. A
// short read falls back to a zeroed suffix rather than failing — the ordinal already carries
// uniqueness where it is load-bearing.
func mintIDFrom(ordinal int, rand io.Reader) string {
	buf := make([]byte, idRandomBytes)
	_, _ = io.ReadFull(rand, buf)
	return fmt.Sprintf("sch-%d-%s", ordinal, hex.EncodeToString(buf))
}
