package hooks

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

const (
	// queueDepth is how many pending firings one Hook may hold. It is a BOUND, not a promise:
	// a Hook slower than the events it subscribes to loses the newest firings rather than
	// growing without limit, because the alternative — an unbounded queue behind an engine that
	// emits under its own mutex — turns a wedged script into a memory leak (ADR 0073 §7).
	queueDepth = 64

	// drainGrace is how long a set of workers replaced by a config reload is given to finish
	// what it is holding before the running jobs are cancelled. It matches the grace every root
	// passes to Close, so a Hook takes the same worst case whether the run ended or the list did.
	drainGrace = 5 * time.Second
)

// Executor runs one Hook's action for one firing: an argv command, a webhook POST, or — in a
// test — a recording fake. It is the package's ONE seam over the outside world, so everything
// above it (matching, queueing, reporting, shutdown) is provable without a process or a socket.
//
// Run is called from the Hook's own worker goroutine, one firing at a time per Hook but
// concurrently across Hooks, so an implementation shared by several Hooks must be safe for
// concurrent use. It is handed a context already bounded by the Hook's Timeout and cancelled
// when the Runner is closing; it must return promptly once that context is done. A returned
// error is REPORTED to the Driver and otherwise discarded — nothing an executor produces
// reaches the model, the conversation or the Session record.
type Executor interface {
	Run(ctx context.Context, h Hook, p Payload) error
}

// Options are the facts a Runner cannot derive: what it decorates, where it is rooted, who it
// reports to, and how it reaches the outside world. Every field but Exec is optional, and the
// zero value of each is the sensible absence rather than a fault.
type Options struct {
	// Inner is the sink this Runner decorates. Every Event is forwarded to it FIRST, before any
	// matching, so installing a Runner cannot change what the Driver below it sees. nil ⇒ the
	// events are discarded after matching, which is what a root with no other observer wants.
	Inner domain.EventSink

	// Workspace is the run's workspace root. It is resolved through ResolveWorkspace once, and
	// the result is both the payload's "workspace" field and the value a Hook's `workspace:`
	// filter is compared against — so the two readings can never disagree (ratified call C).
	Workspace string

	// Schedule names the Schedule this root runs for, present on a daemon or `/schedule` Firing
	// and absent everywhere else. A defensive copy is taken, so the caller may reuse its value.
	Schedule *ScheduleRef

	// Report receives one line per Hook failure and per drop, worded for a human. It is the ONLY
	// way a Hook's trouble reaches anyone: a failing Hook never becomes an ErrorEvent, because a
	// Hook subscribed to `error` would then fire on its own failure and loop (ADR 0073 §8).
	//
	// It is called under the Runner's own mutex, so it need not be safe for concurrent use, and
	// it is called from a worker goroutine and — on a queue drop — from the goroutine that called
	// Emit. It must therefore NOT block and must not re-enter the Runner. nil ⇒ failures are
	// silent, which is the bench's case.
	Report func(string)

	// WriteTarget answers whether a tool call wrote a file, and where. nil ⇒ no file-changed
	// event is ever derived (see newMatcher).
	WriteTarget WriteTarget

	// Exec runs a Hook's action. Required whenever at least one Hook is active at this root;
	// a Runner with nothing to run needs none.
	Exec Executor

	// Now is the clock the payload's "time" field is read from. nil ⇒ time.Now.
	Now func() time.Time
}

// Runner is the observe-only decorator that turns the engine's Event stream into fired Hooks. It
// is what every Driver installs as Config.Events, wrapping whatever sink it already had.
//
// The contract that matters is that Emit NEVER BLOCKS and never fails: it runs on the engine's
// own goroutine, under the tree-wide sink mutex (internal/agent's serialEventSink), so any wait
// here is a wait for the whole agent tree. Matching is pure and bounded; delivery is a
// non-blocking send onto a bounded per-Hook queue; everything slow — the command, the POST, the
// timeout — happens on that Hook's own worker goroutine.
//
// One worker per active Hook is deliberate: each Hook sees its events in order and cannot be
// delayed by another Hook's slow script, which a single shared worker could not promise.
type Runner struct {
	inner       domain.EventSink
	workspace   string
	schedule    *ScheduleRef
	report      func(string)
	writeTarget WriteTarget
	exec        Executor
	now         func() time.Time

	// active is the live set of workers and the matcher built over their subscribed events.
	// Emit loads it without a lock; Replace and Close swap it, so a reload never blocks the
	// engine and an in-flight Emit finishes against a consistent set.
	active atomic.Pointer[hookSet]

	// swapMu serializes the set swaps themselves — two concurrent Replaces, or a Replace racing
	// Close — so an old set is drained exactly once and never after the Runner is closed.
	swapMu sync.Mutex
	closed atomic.Bool

	// reportMu makes Options.Report a single-caller-at-a-time seam: several workers can fail at
	// the same instant, and a Driver's notifier should not have to grow its own lock.
	reportMu sync.Mutex

	closeOnce sync.Once
	closeErr  error
}

// hookSet is one generation of active Hooks: the workers that run them, the matcher built over
// exactly their subscribed events, and the context every job of theirs runs under. A Replace
// builds a whole new generation rather than editing this one, so Emit never sees a half-swapped
// list and the old generation's cancellation cannot reach the new one's jobs.
type hookSet struct {
	matcher *matcher
	workers []*worker
	ctx     context.Context
	cancel  context.CancelFunc
}

// worker is one active Hook and its queue. lastFailure is touched only by the worker's own
// goroutine; the counters are touched from Emit's goroutine as well and are therefore atomic.
type worker struct {
	hook   Hook
	events map[Event]bool
	queue  chan Payload
	done   chan struct{}

	dropped      atomic.Int64
	dropReported atomic.Bool

	// lastFailure is the failure line most recently reported for this Hook, cleared by a
	// success. It is how a Hook that fails every single Turn reports once rather than forever.
	lastFailure string
}

// New builds a Runner over the given Hook list. The list is validated and reduced to the ones
// ACTIVE at this root — a Hook whose `workspace:` resolves to a different directory is simply not
// here — and one worker goroutine is started per survivor. A Runner with no active Hook is a
// legitimate, cheap result: Emit then forwards and does nothing else.
//
// It fails when an entry is malformed (ValidateAll's message names the entry), when a workspace
// path cannot be resolved, or when a Hook would have to run with no Options.Exec to run it.
func New(list []Hook, o Options) (*Runner, error) {
	if err := ValidateAll(list); err != nil {
		return nil, err
	}
	workspace, err := ResolveWorkspace(o.Workspace)
	if err != nil {
		return nil, err
	}
	r := &Runner{
		inner:       o.Inner,
		workspace:   workspace,
		report:      o.Report,
		writeTarget: o.WriteTarget,
		exec:        o.Exec,
		now:         o.Now,
	}
	if r.now == nil {
		r.now = time.Now
	}
	if o.Schedule != nil {
		schedule := *o.Schedule
		r.schedule = &schedule
	}
	set, err := r.buildSet(list)
	if err != nil {
		return nil, err
	}
	r.active.Store(set)
	return r, nil
}

// Emit forwards the Event to the decorated sink and then fires whatever Hooks it produced. The
// forward comes FIRST and unconditionally: a Hook that panicked the matcher would otherwise be
// able to swallow the Driver's own stream, and the decoration is meant to be invisible.
//
// It returns without matching at all when no Hook is active — the ordinary case for a user who
// configured none — so an unhooked run pays one atomic load per Event and nothing more.
func (r *Runner) Emit(e domain.Event) {
	if r.inner != nil {
		r.inner.Emit(e)
	}
	set := r.active.Load()
	if set == nil || len(set.workers) == 0 {
		return
	}
	firings := set.matcher.match(e)
	if len(firings) == 0 {
		return
	}
	// One reading of the clock per Event: two Hooks fired by the same event report the same
	// instant, which is what a human correlating two notifications expects.
	now := r.now().Format(time.RFC3339Nano)
	for _, f := range firings {
		r.fanOut(set, f, now)
	}
}

// fanOut hands one firing to every worker subscribing to its event, stamping the identity fields
// only the Runner knows. The send is non-blocking by construction: a full queue drops the NEWEST
// firing, because the older ones are already the ones a script is working through.
func (r *Runner) fanOut(set *hookSet, f firing, now string) {
	for _, w := range set.workers {
		if !w.events[f.Event] {
			continue
		}
		payload := f.Payload
		payload.Hook = w.hook.Name
		payload.Time = now
		payload.Workspace = r.workspace
		if r.schedule != nil {
			schedule := *r.schedule
			payload.Schedule = &schedule
		}
		select {
		case w.queue <- payload:
		default:
			r.noteDrop(w)
		}
	}
}

// noteDrop counts a dropped firing and reports the FIRST one for this Hook. Only the first: a
// Hook that is being outrun drops in bursts, and one line per dropped event would bury the
// failure it is a symptom of. The total is reported once more when the set is drained.
func (r *Runner) noteDrop(w *worker) {
	w.dropped.Add(1)
	if w.dropReported.CompareAndSwap(false, true) {
		r.emitReport(fmt.Sprintf("hook %s: dropped 1 event (queue full)", w.hook.Name))
	}
}

// Replace swaps the active Hook list — the config file changed under a live session — and drains
// the previous generation in the BACKGROUND, so a reload never blocks the goroutine that noticed
// the change. The new list is validated and re-filtered by workspace exactly as New's was, and a
// failure leaves the running set untouched: a broken edit to `hooks:` costs the user nothing.
//
// The previous generation finishes what it already holds (its in-flight job and its queue) under
// the same grace Close gives, then stops. Firings the old matcher was still correlating — a write
// whose tool result has not arrived — are forgotten, because the new generation starts with a
// clean correlation map; a reload is a rare, human-initiated event and the alternative would be to
// read one generation's map from another generation's goroutine.
func (r *Runner) Replace(list []Hook) error {
	if err := ValidateAll(list); err != nil {
		return err
	}
	r.swapMu.Lock()
	defer r.swapMu.Unlock()
	if r.closed.Load() {
		return errors.New("hooks: the runner is closed and cannot take a new hook list")
	}
	set, err := r.buildSet(list)
	if err != nil {
		return err
	}
	previous := r.active.Swap(set)
	if previous != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), drainGrace)
			defer cancel()
			_ = r.drainSet(previous, ctx)
		}()
	}
	return nil
}

// Close stops intake, waits for every worker to finish what it holds until ctx expires, then
// cancels whatever is still running. It is idempotent — whoever gets there first closes the
// Runner, and a later caller gets the same answer — and it reports each Hook's drop total on the
// way out, which is the only place the full count is ever stated.
//
// It returns ctx's error, wrapped, when a Hook was still running at the deadline: the run is over
// either way, but a root that wants to say "a hook was killed" needs to be told.
func (r *Runner) Close(ctx context.Context) error {
	r.closeOnce.Do(func() {
		r.swapMu.Lock()
		r.closed.Store(true)
		// An empty generation stops intake without disturbing an Emit already in flight: it
		// loads a set and finds no workers, exactly as an unhooked run does.
		previous := r.active.Swap(&hookSet{})
		r.swapMu.Unlock()
		if previous != nil {
			r.closeErr = r.drainSet(previous, ctx)
		}
	})
	return r.closeErr
}

// buildSet reduces the list to the Hooks active at this root and starts a worker for each.
func (r *Runner) buildSet(list []Hook) (*hookSet, error) {
	active := make([]Hook, 0, len(list))
	for _, h := range list {
		scope, err := ResolveWorkspace(h.Workspace)
		if err != nil {
			return nil, h.errorf("%v", err)
		}
		if scope != "" && scope != r.workspace {
			continue
		}
		active = append(active, h)
	}
	if len(active) > 0 && r.exec == nil {
		return nil, fmt.Errorf("hooks: %d hook(s) are active here but no executor was supplied to run them", len(active))
	}

	ctx, cancel := context.WithCancel(context.Background())
	set := &hookSet{
		matcher: newMatcher(SubscribedEvents(active), r.writeTarget),
		workers: make([]*worker, 0, len(active)),
		ctx:     ctx,
		cancel:  cancel,
	}
	for _, h := range active {
		w := &worker{
			hook:   h,
			events: eventSet(h.Events),
			queue:  make(chan Payload, queueDepth),
			done:   make(chan struct{}),
		}
		set.workers = append(set.workers, w)
		go r.serve(set, w)
	}
	return set, nil
}

// serve is one Hook's worker: it runs the Hook's firings in the order they were queued and exits
// once the queue is closed AND emptied, so a drained generation still finishes what it holds.
func (r *Runner) serve(set *hookSet, w *worker) {
	defer close(w.done)
	for payload := range w.queue {
		r.runOne(set, w, payload)
	}
}

// runOne runs one firing under the Hook's own timeout and reports a failure the Driver has not
// already been told about.
func (r *Runner) runOne(set *hookSet, w *worker, payload Payload) {
	ctx, cancel := context.WithTimeout(set.ctx, w.hook.Timeout)
	defer cancel()

	err := r.exec.Run(ctx, w.hook, payload)
	if err == nil {
		w.lastFailure = ""
		return
	}
	if set.ctx.Err() != nil {
		// The generation was cancelled out from under this job — by a Close past its grace or a
		// reload's drain. That is our own doing, not the Hook's, and reporting it would put a
		// failure line on every shutdown that killed a slow script.
		return
	}
	line := fmt.Sprintf("hook %s (%s): %v", w.hook.Name, payload.Event, err)
	if line == w.lastFailure {
		return
	}
	w.lastFailure = line
	r.emitReport(line)
}

// drainSet closes every queue, waits for the workers until ctx expires, then cancels what is
// still running and states each Hook's drop total.
func (r *Runner) drainSet(set *hookSet, ctx context.Context) error {
	for _, w := range set.workers {
		close(w.queue)
	}
	var expired bool
	for _, w := range set.workers {
		select {
		case <-w.done:
		case <-ctx.Done():
			expired = true
		}
	}
	set.cancel()
	r.reportDrops(set)
	if expired {
		return fmt.Errorf("hooks: gave up waiting for a hook to finish, cancelling it: %w", ctx.Err())
	}
	return nil
}

// reportDrops states the final count for every Hook that lost a firing.
func (r *Runner) reportDrops(set *hookSet) {
	for _, w := range set.workers {
		if dropped := w.dropped.Load(); dropped > 0 {
			r.emitReport(fmt.Sprintf("hook %s: dropped %d events", w.hook.Name, dropped))
		}
	}
}

// emitReport hands one line to the Driver's reporter, one caller at a time.
func (r *Runner) emitReport(line string) {
	if r.report == nil {
		return
	}
	r.reportMu.Lock()
	defer r.reportMu.Unlock()
	r.report(line)
}

// eventSet indexes one Hook's events for the per-firing membership test.
func eventSet(events []Event) map[Event]bool {
	set := make(map[Event]bool, len(events))
	for _, e := range events {
		set[e] = true
	}
	return set
}
