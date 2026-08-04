package schedule

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// testCycle is every test's cycle: a legal one (MinCycle is the floor) that no test ever
// waits out, because the fake clock delivers the ticks.
const testCycle = time.Minute

// testSpec is a valid Spec with the given name.
func testSpec(name string) Spec {
	return Spec{Name: name, Cycle: testCycle, Prompt: "check the build", Mode: domain.ModePlan}
}

// start builds a Scheduler over the fake clock and recorder and closes it with the test.
func start(t *testing.T, clk *fakeClock, rec *recorder,
	fire func(context.Context, Firing) (Outcome, error),
	gate func(context.Context) error,
) *Scheduler {
	t.Helper()
	s, err := New(Config{Fire: fire, Gate: gate, Notify: rec.notify, Clock: clk})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

// blockingFire returns a runner that reports it started and then waits for release (or for
// its context to be cancelled, so Close can always join it).
func blockingFire(started chan<- struct{}, release <-chan struct{}, out Outcome) func(context.Context, Firing) (Outcome, error) {
	return func(ctx context.Context, _ Firing) (Outcome, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-release:
			return out, nil
		case <-ctx.Done():
			return Outcome{}, ctx.Err()
		}
	}
}

// The overlap policy (decision 1)

// TestTickDuringAFiringIsSkippedAndTheNextTickFires is the headline policy: one Schedule
// never runs two Firings at once, the refused tick is REPORTED rather than swallowed, and
// the cadence recovers on its own.
func TestTickDuringAFiringIsSkippedAndTheNextTickFires(t *testing.T) {
	t.Parallel()

	clk, rec := newFakeClock(), newRecorder()
	release := make(chan struct{})
	var runs atomic.Int32
	fire := func(ctx context.Context, f Firing) (Outcome, error) {
		runs.Add(1)
		select {
		case <-release:
			return Outcome{RecordID: "rec-1", Title: f.ScheduleName + " — 09:01"}, nil
		case <-ctx.Done():
			return Outcome{}, ctx.Err()
		}
	}
	s := start(t, clk, rec, fire, nil)

	id, err := s.Add(testSpec("nightly"))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	rec.next(t, EventCreated)

	clk.tick(t, 0)
	rec.next(t, EventFired)

	// The tick that lands on the busy Schedule.
	clk.tick(t, 0)
	if got := rec.next(t, EventSkipped); got.ScheduleID != id {
		t.Errorf("skipped event carried schedule %q, want %q", got.ScheduleID, id)
	}
	if got := runs.Load(); got != 1 {
		t.Errorf("the skipped tick started %d firings, want the one already in flight", got-1)
	}

	close(release)
	done := rec.next(t, EventCompleted)
	if done.Outcome.RecordID != "rec-1" || done.Outcome.Title != "nightly — 09:01" {
		t.Errorf("completed event carried %+v, want the runner's outcome", done.Outcome)
	}

	// The cadence recovered: the next tick fires normally.
	clk.tick(t, 0)
	rec.next(t, EventFired)
	rec.next(t, EventCompleted)
	if got := runs.Load(); got != 2 {
		t.Errorf("firings = %d, want 2 (the skipped tick never ran)", got)
	}
	if got := rec.count(EventSkipped); got != 1 {
		t.Errorf("skipped events = %d, want exactly 1", got)
	}
}

// The quiescence gate (decision 7)

// TestGateHoldsADueFiringAndCollapsesTheTicksBehindIt proves the deferral obeys the same
// overlap rule as the run itself: at most one Firing is ever pending per Schedule, so ticks
// that pile up while the host is busy collapse into skips rather than a backlog.
func TestGateHoldsADueFiringAndCollapsesTheTicksBehindIt(t *testing.T) {
	t.Parallel()

	clk, rec := newFakeClock(), newRecorder()
	entered, hold := make(chan struct{}, 1), make(chan struct{})
	gate := func(ctx context.Context) error {
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-hold:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	fire := func(context.Context, Firing) (Outcome, error) { return Outcome{RecordID: "rec-1"}, nil }
	s := start(t, clk, rec, fire, gate)

	if _, err := s.Add(testSpec("nightly")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	rec.next(t, EventCreated)

	clk.tick(t, 0)
	signalled(t, entered, "the gate to be entered")

	// Two more ticks land while the firing waits for quiescence.
	clk.tick(t, 0)
	rec.next(t, EventSkipped)
	clk.tick(t, 0)
	rec.next(t, EventSkipped)
	if got := rec.count(EventFired); got != 0 {
		t.Fatalf("fired events = %d while the gate was still holding, want 0", got)
	}

	close(hold)
	rec.next(t, EventFired)
	rec.next(t, EventCompleted)
	if got := rec.count(EventFired); got != 1 {
		t.Errorf("fired events = %d, want 1 (the collapsed ticks never became firings)", got)
	}
	if got := rec.count(EventSkipped); got != 2 {
		t.Errorf("skipped events = %d, want 2", got)
	}
}

// TestABlockedGateOnOneScheduleDoesNotStarveAnother proves gating is PER SCHEDULE: the
// Scheduler holds one waiting Firing without holding the whole clock, which is why each
// Schedule gets its own loop rather than sharing one.
func TestABlockedGateOnOneScheduleDoesNotStarveAnother(t *testing.T) {
	t.Parallel()

	clk, rec := newFakeClock(), newRecorder()
	entered, hold := make(chan struct{}, 1), make(chan struct{})
	var calls atomic.Int32
	gate := func(ctx context.Context) error {
		if calls.Add(1) > 1 {
			return nil // only the first caller — the first Schedule — is held
		}
		entered <- struct{}{}
		select {
		case <-hold:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	fire := func(_ context.Context, f Firing) (Outcome, error) {
		return Outcome{RecordID: "rec-" + f.ScheduleName}, nil
	}
	s := start(t, clk, rec, fire, gate)

	held, err := s.Add(testSpec("held"))
	if err != nil {
		t.Fatalf("Add(held): %v", err)
	}
	rec.next(t, EventCreated)
	free, err := s.Add(testSpec("free"))
	if err != nil {
		t.Fatalf("Add(free): %v", err)
	}
	rec.next(t, EventCreated)

	clk.tick(t, 0)
	signalled(t, entered, "the first schedule's gate to be entered")

	// The second Schedule fires through while the first sits in the gate.
	clk.tick(t, 1)
	if got := rec.next(t, EventFired); got.ScheduleID != free {
		t.Fatalf("fired schedule = %q, want the un-gated one (%q)", got.ScheduleID, free)
	}
	if got := rec.next(t, EventCompleted); got.Outcome.RecordID != "rec-free" {
		t.Fatalf("completed outcome = %+v, want the un-gated schedule's", got.Outcome)
	}

	close(hold)
	if got := rec.next(t, EventFired); got.ScheduleID != held {
		t.Errorf("released schedule = %q, want %q", got.ScheduleID, held)
	}
	rec.next(t, EventCompleted)
}

// Lifecycle

// TestStopMidGateWaitEndsTheScheduleWithoutFiring proves Stop reaches a Firing that is still
// only PENDING: the wait is cancelled, nothing runs, and the Schedule leaves the live set.
func TestStopMidGateWaitEndsTheScheduleWithoutFiring(t *testing.T) {
	t.Parallel()

	clk, rec := newFakeClock(), newRecorder()
	entered := make(chan struct{}, 1)
	gate := func(ctx context.Context) error {
		entered <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}
	fire := func(context.Context, Firing) (Outcome, error) {
		t.Error("a stopped schedule fired")
		return Outcome{}, nil
	}
	s := start(t, clk, rec, fire, gate)

	id, err := s.Add(testSpec("nightly"))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	rec.next(t, EventCreated)

	clk.tick(t, 0)
	signalled(t, entered, "the gate to be entered")

	if err := s.Stop(id); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := rec.next(t, EventStopped); got.ScheduleID != id {
		t.Errorf("stopped event carried schedule %q, want %q", got.ScheduleID, id)
	}
	// The abandoned wait reports nothing of its own — Stop already accounted for it.
	rec.quiet(t)

	if got := s.List(); len(got) != 0 {
		t.Errorf("List after Stop = %+v, want empty", got)
	}
	// A tick after Stop reaches nobody.
	clk.tick(t, 0)
	rec.quiet(t)

	if err := s.Stop(id); !errors.Is(err, ErrNotFound) {
		t.Errorf("Stop twice = %v, want ErrNotFound", err)
	}
}

// TestStopLetsAFiringAlreadyUnderWayFinish pins the other half of Stop's promise: it ends the
// CYCLE, not the run in flight — that one is a fresh agent mid-task, and only Close's context
// takes it down.
func TestStopLetsAFiringAlreadyUnderWayFinish(t *testing.T) {
	t.Parallel()

	clk, rec := newFakeClock(), newRecorder()
	started, release := make(chan struct{}, 1), make(chan struct{})
	s := start(t, clk, rec, blockingFire(started, release, Outcome{RecordID: "rec-1"}), nil)

	id, err := s.Add(testSpec("nightly"))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	rec.next(t, EventCreated)

	clk.tick(t, 0)
	rec.next(t, EventFired)
	signalled(t, started, "the firing to start")

	if err := s.Stop(id); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	rec.next(t, EventStopped)

	close(release)
	if got := rec.next(t, EventCompleted); got.Outcome.RecordID != "rec-1" {
		t.Errorf("completed outcome = %+v, want the in-flight firing's", got.Outcome)
	}
}

// TestCloseIsIdempotentAndJoinsEveryGoroutine proves Close is the lever a Driver's quit path
// can trust: the Firing's context is cancelled, nothing is still running when it returns, and
// calling it twice neither panics nor races past the first call.
func TestCloseIsIdempotentAndJoinsEveryGoroutine(t *testing.T) {
	t.Parallel()

	clk, rec := newFakeClock(), newRecorder()
	started := make(chan struct{}, 1)
	var exited atomic.Bool
	fire := func(ctx context.Context, _ Firing) (Outcome, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		exited.Store(true)
		return Outcome{}, ctx.Err()
	}
	s := start(t, clk, rec, fire, nil)

	if _, err := s.Add(testSpec("nightly")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	rec.next(t, EventCreated)
	clk.tick(t, 0)
	rec.next(t, EventFired)
	signalled(t, started, "the firing to start")

	s.Close()
	if !exited.Load() {
		t.Error("Close returned while the firing goroutine was still running")
	}
	for i, ticker := range clk.allTickers() {
		if !ticker.isStopped() {
			t.Errorf("ticker %d was never stopped — its loop goroutine did not exit", i)
		}
	}

	closedAgain := make(chan struct{})
	go func() {
		s.Close()
		close(closedAgain)
	}()
	select {
	case <-closedAgain:
	case <-time.After(testWait):
		t.Fatal("a second Close never returned")
	}

	if _, err := s.Add(testSpec("late")); !errors.Is(err, ErrClosed) {
		t.Errorf("Add after Close = %v, want ErrClosed", err)
	}
}

// The status surface

// TestListReportsNextFireAndCounts pins what the bare `/schedule` surface reads: the cycle's
// next due time on the Scheduler's own clock, and the tally of what has fired and what was
// refused.
func TestListReportsNextFireAndCounts(t *testing.T) {
	t.Parallel()

	clk, rec := newFakeClock(), newRecorder()
	started, release := make(chan struct{}, 1), make(chan struct{})
	s := start(t, clk, rec, blockingFire(started, release, Outcome{RecordID: "rec-1"}), nil)

	created := clk.Now()
	id, err := s.Add(testSpec("nightly"))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	rec.next(t, EventCreated)

	got := s.List()
	if len(got) != 1 {
		t.Fatalf("List = %+v, want one schedule", got)
	}
	want := Status{
		ID: id, Name: "nightly", Cycle: testCycle, Mode: domain.ModePlan,
		NextFire: created.Add(testCycle),
	}
	if got[0] != want {
		t.Errorf("List()[0] = %+v, want %+v", got[0], want)
	}
	if every := clk.allTickers()[0].every; every != testCycle {
		t.Errorf("ticker cadence = %s, want the schedule's cycle %s", every, testCycle)
	}

	// A tick an hour and a half later: the next fire is measured from the tick that just
	// landed, not from creation.
	clk.advance(90 * time.Second)
	fired := clk.Now()
	clk.tick(t, 0)
	rec.next(t, EventFired)
	signalled(t, started, "the firing to start")

	got = s.List()
	if got[0].Fired != 1 || got[0].Skipped != 0 || !got[0].InFlight {
		t.Errorf("List during a firing = %+v, want fired=1 skipped=0 inFlight=true", got[0])
	}
	if got[0].NextFire != fired.Add(testCycle) {
		t.Errorf("NextFire = %s, want %s", got[0].NextFire, fired.Add(testCycle))
	}

	clk.tick(t, 0)
	rec.next(t, EventSkipped)
	if got = s.List(); got[0].Skipped != 1 || got[0].Fired != 1 {
		t.Errorf("List after a skip = %+v, want fired=1 skipped=1", got[0])
	}

	close(release)
	rec.next(t, EventCompleted)
	eventually(t, "the in-flight flag to clear", func() bool { return !s.List()[0].InFlight })
}

// TestEventsArriveInPerScheduleOrder pins the sequence a surface renders as notices: created
// opens a Schedule's account, a completion always follows its own firing, and stopped closes
// it.
func TestEventsArriveInPerScheduleOrder(t *testing.T) {
	t.Parallel()

	clk, rec := newFakeClock(), newRecorder()
	fire := func(context.Context, Firing) (Outcome, error) { return Outcome{RecordID: "rec-1"}, nil }
	s := start(t, clk, rec, fire, nil)

	id, err := s.Add(testSpec("nightly"))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	rec.next(t, EventCreated)
	clk.tick(t, 0)
	rec.next(t, EventFired)
	rec.next(t, EventCompleted)
	if err := s.Stop(id); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	rec.next(t, EventStopped)

	want := []EventKind{EventCreated, EventFired, EventCompleted, EventStopped}
	got := rec.kinds()
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events = %v, want %v", got, want)
		}
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, e := range rec.all {
		if e.ScheduleID != id || e.ScheduleName != "nightly" {
			t.Errorf("event %q carried (%q, %q), want the schedule's identity", e.Kind, e.ScheduleID, e.ScheduleName)
		}
	}
}

// TestAFailingRunnerIsReportedAndTheCadenceSurvives proves a failed Firing is an Event rather
// than the end of the Schedule: an unattended run that errors must not silently take its
// cycle down with it.
func TestAFailingRunnerIsReportedAndTheCadenceSurvives(t *testing.T) {
	t.Parallel()

	clk, rec := newFakeClock(), newRecorder()
	boom := errors.New("the upstream refused")
	var calls atomic.Int32
	fire := func(context.Context, Firing) (Outcome, error) {
		if calls.Add(1) == 1 {
			return Outcome{}, boom
		}
		return Outcome{RecordID: "rec-2"}, nil
	}
	s := start(t, clk, rec, fire, nil)

	if _, err := s.Add(testSpec("nightly")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	rec.next(t, EventCreated)

	clk.tick(t, 0)
	rec.next(t, EventFired)
	if got := rec.next(t, EventFailed); !errors.Is(got.Err, boom) {
		t.Errorf("failed event carried %v, want %v", got.Err, boom)
	}

	clk.tick(t, 0)
	rec.next(t, EventFired)
	rec.next(t, EventCompleted)
}

// Creation policy

// TestAddRefusesASpecNoFiringCanRun pins the library's creation policy — the floor under the
// cycle and the two modes a Firing may run in (ADR 0033, decision 2) are enforced HERE, not
// at whichever surface happened to collect the input.
func TestAddRefusesASpecNoFiringCanRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec Spec
		want error
	}{
		{"no prompt", Spec{Name: "n", Cycle: testCycle, Mode: domain.ModePlan}, ErrPrompt},
		{"blank prompt", Spec{Cycle: testCycle, Prompt: "   \n ", Mode: domain.ModePlan}, ErrPrompt},
		{"cycle under the floor", Spec{Cycle: 5 * time.Second, Prompt: "p", Mode: domain.ModePlan}, ErrCycle},
		{"cycle exactly zero", Spec{Prompt: "p", Mode: domain.ModePlan}, ErrCycle},
		{"ask-before", Spec{Cycle: testCycle, Prompt: "p", Mode: domain.ModeAskBefore}, ErrMode},
		{"allow-edits", Spec{Cycle: testCycle, Prompt: "p", Mode: domain.ModeAllowEdits}, ErrMode},
		{"no mode at all", Spec{Cycle: testCycle, Prompt: "p"}, ErrMode},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			clk, rec := newFakeClock(), newRecorder()
			s := start(t, clk, rec, func(context.Context, Firing) (Outcome, error) {
				t.Error("a refused spec fired")
				return Outcome{}, nil
			}, nil)

			id, err := s.Add(tc.spec)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Add = %v, want %v", err, tc.want)
			}
			if id != "" {
				t.Errorf("Add returned id %q for a refused spec", id)
			}
			if got := s.List(); len(got) != 0 {
				t.Errorf("List = %+v, want empty", got)
			}
			rec.quiet(t)
		})
	}
}

// TestMinCycleIsAccepted proves the floor is inclusive: 30s is a legal cycle, so the error
// message and the constant agree.
func TestMinCycleIsAccepted(t *testing.T) {
	t.Parallel()

	clk, rec := newFakeClock(), newRecorder()
	s := start(t, clk, rec, func(context.Context, Firing) (Outcome, error) { return Outcome{}, nil }, nil)

	spec := testSpec("floor")
	spec.Cycle = MinCycle
	if _, err := s.Add(spec); err != nil {
		t.Fatalf("Add at exactly MinCycle: %v", err)
	}
	rec.next(t, EventCreated)
}

// TestAddDerivesAMissingNameFromThePrompt proves a caller with no name to offer still gets a
// usable label on the Schedule, its Firings and its records.
func TestAddDerivesAMissingNameFromThePrompt(t *testing.T) {
	t.Parallel()

	clk, rec := newFakeClock(), newRecorder()
	fired := make(chan Firing, 1)
	fire := func(_ context.Context, f Firing) (Outcome, error) {
		fired <- f
		return Outcome{}, nil
	}
	s := start(t, clk, rec, fire, nil)

	spec := Spec{Cycle: testCycle, Prompt: "  review the open ISSUES\nand the TODO  ", Mode: domain.ModeAuto}
	id, err := s.Add(spec)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got := rec.next(t, EventCreated); got.ScheduleName != "review the open ISSUES" {
		t.Errorf("derived name = %q, want the prompt's first line", got.ScheduleName)
	}
	if got := s.List()[0]; got.Name != "review the open ISSUES" || got.Mode != domain.ModeAuto {
		t.Errorf("List()[0] = %+v, want the derived name and the auto mode", got)
	}

	clk.tick(t, 0)
	rec.next(t, EventFired)
	select {
	case f := <-fired:
		want := Firing{ScheduleID: id, ScheduleName: "review the open ISSUES", Prompt: "review the open ISSUES\nand the TODO", Mode: domain.ModeAuto}
		if f != want {
			t.Errorf("Firing = %+v, want %+v", f, want)
		}
	case <-time.After(testWait):
		t.Fatal("the runner was never handed a Firing")
	}
}

// TestDeriveName pins the label heuristic: a short prompt is its own name, a long one is cut
// at a word boundary and marked as cut.
func TestDeriveName(t *testing.T) {
	t.Parallel()

	long := "summarise every commit that landed today and note anything that looks odd"
	tests := []struct {
		name   string
		prompt string
		want   string
	}{
		{"short prompt is its own name", "check the build", "check the build"},
		{"first line only", "check the build\nthen report", "check the build"},
		{"long prompt is cut at a word boundary", long, "summarise every commit that landed…"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := deriveName(tc.prompt)
			if got != tc.want {
				t.Errorf("deriveName(%q) = %q, want %q", tc.prompt, got, tc.want)
			}
			if n := len([]rune(got)); n > nameMax+1 {
				t.Errorf("derived name is %d runes, want at most %d plus the ellipsis", n, nameMax)
			}
			if !strings.HasPrefix(tc.prompt, strings.TrimSuffix(got, "…")) {
				t.Errorf("derived name %q is not a prefix of the prompt", got)
			}
		})
	}
}

// Construction and identity

// TestNewWithoutARunnerIsRefused: a Scheduler that cannot perform a Firing is not one.
func TestNewWithoutARunnerIsRefused(t *testing.T) {
	t.Parallel()

	if _, err := New(Config{}); !errors.Is(err, ErrNoRunner) {
		t.Errorf("New without Fire = %v, want ErrNoRunner", err)
	}
}

// TestNilSeamsAreAccepted proves the optional seams really are optional: no Gate means no
// deferral (the daemon case) and no Notify means the Events are discarded, and neither turns
// into a nil dereference on the firing path.
func TestNilSeamsAreAccepted(t *testing.T) {
	t.Parallel()

	clk := newFakeClock()
	fired := make(chan struct{}, 1)
	s, err := New(Config{
		Clock: clk,
		Fire: func(context.Context, Firing) (Outcome, error) {
			fired <- struct{}{}
			return Outcome{}, nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.Close)

	if _, err := s.Add(testSpec("quiet")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	clk.tick(t, 0)
	signalled(t, fired, "the firing to run with no Gate and no Notify")
}

// TestMintedIDsAreUnique proves a schedule id is unique within the process even when the
// randomness source fails — the ordinal carries that half, because the id travels onto a
// session record that outlives the Scheduler.
func TestMintedIDsAreUnique(t *testing.T) {
	t.Parallel()

	clk, rec := newFakeClock(), newRecorder()
	s := start(t, clk, rec, func(context.Context, Firing) (Outcome, error) { return Outcome{}, nil }, nil)

	first, err := s.Add(testSpec("one"))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	second, err := s.Add(testSpec("two"))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if first == second {
		t.Errorf("two schedules share the id %q", first)
	}
	if !strings.HasPrefix(first, "sch-") {
		t.Errorf("id %q does not carry the schedule prefix", first)
	}

	// An exhausted randomness source degrades to a zeroed suffix; the ordinal keeps the ids apart.
	dry := strings.NewReader("")
	if a, b := mintIDFrom(1, dry), mintIDFrom(2, dry); a == b {
		t.Errorf("ids collide without randomness: %q and %q", a, b)
	}
}
