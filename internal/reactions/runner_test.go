package reactions

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// awaitDeadline is how long a test waits for a goroutine it expects to make progress. It is
// generous on purpose: the assertions are about whether something happens at all, never about
// how fast, so a loaded CI machine must not be able to fail them.
const awaitDeadline = 5 * time.Second

// ----------------------------------------------------------------------------
// Doubles
// ----------------------------------------------------------------------------

// fakeExecutor records every firing it is handed and can be made to block, to fail, or to wait
// on its context — the three behaviours the Runner's queueing, reporting and shutdown are about.
type fakeExecutor struct {
	mu   sync.Mutex
	runs []Payload

	// started announces each Run as it begins, before any blocking, so a test can prove a worker
	// dequeued a firing without waiting for the firing to finish.
	started chan Payload

	// gate, when non-nil, holds every Run until it is closed (or the run's context is done).
	gate chan struct{}

	// errs is indexed by the order the runs arrive in, so a test can script a sequence of
	// outcomes across one Hook's firings; a run past the end of the slice succeeds.
	errs []error

	// cancelled is closed by the first Run whose context is cancelled under it.
	cancelled     chan struct{}
	cancelledOnce sync.Once
}

func newFakeExecutor() *fakeExecutor {
	return &fakeExecutor{started: make(chan Payload, 512), cancelled: make(chan struct{})}
}

func (f *fakeExecutor) Run(ctx context.Context, _ domain.Reaction, p Payload) error {
	f.mu.Lock()
	f.runs = append(f.runs, p)
	index := len(f.runs) - 1
	gate, errs := f.gate, f.errs
	f.mu.Unlock()

	select {
	case f.started <- p:
	default:
	}

	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			f.cancelledOnce.Do(func() { close(f.cancelled) })
			return ctx.Err()
		}
	}
	if index < len(errs) {
		return errs[index]
	}
	return nil
}

// recorded returns a copy of every firing the executor has run so far.
func (f *fakeExecutor) recorded() []Payload {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Payload(nil), f.runs...)
}

// recordingSink is the decorated sink: it is only ever emitted to from the test's own goroutine,
// which is the guarantee the engine itself gives a sink (domain.EventSink).
type recordingSink struct{ events []domain.Event }

func (s *recordingSink) Emit(e domain.Event) { s.events = append(s.events, e) }

// reportLog collects the Driver-facing failure and drop lines. The Runner serializes its calls,
// but the test reads them from its own goroutine, so the log locks anyway.
type reportLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *reportLog) add(line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, line)
}

func (l *reportLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

// commandHook is a valid entry whose action never matters — the fake Executor is what runs.
func commandHook(name string, events ...Event) domain.Reaction {
	return domain.Reaction{
		ID:      name,
		Origin:  domain.OriginUser,
		Class:   domain.ClassObserve,
		On:      events,
		Handler: domain.ArgvHandler{Argv: []string{"true"}},
		Timeout: time.Minute,
	}
}

// turnEvent is a Depth-0 Turn boundary carrying an index a test can recognise the firing by.
func turnEvent(turn int) domain.TurnEvent {
	return domain.TurnEvent{
		EventBase: domain.EventBase{Turn: turn},
		Status:    domain.StatusTurnComplete,
	}
}

// awaitStart waits for one Run to begin, failing the test rather than hanging forever.
func awaitStart(t *testing.T, f *fakeExecutor) Payload {
	t.Helper()
	select {
	case p := <-f.started:
		return p
	case <-time.After(awaitDeadline):
		t.Fatal("no hook run started within the deadline")
		return Payload{}
	}
}

// closeRunner drains the Runner with a grace long enough that a passing test never hits it.
func closeRunner(t *testing.T, r *Runner) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), awaitDeadline)
	defer cancel()
	if err := r.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// ----------------------------------------------------------------------------
// Tests
// ----------------------------------------------------------------------------

// TestRunnerForwardsEveryEventToInner — the decoration is invisible: whatever the Hook list is,
// the sink below the Runner sees the whole stream, matched events and unmatched ones alike.
func TestRunnerForwardsEveryEventToInner(t *testing.T) {
	t.Parallel()

	inner := &recordingSink{}
	exec := newFakeExecutor()
	runner, err := New([]domain.Reaction{commandHook("notify", TurnFinished)}, Options{
		Inner: inner, Workspace: t.TempDir(), Exec: exec,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeRunner(t, runner)

	events := []domain.Event{
		turnEvent(1),
		domain.MessageEvent{Text: "unmatched"},
		domain.TurnEvent{EventBase: domain.EventBase{Depth: 1, Turn: 2}, Status: domain.StatusTurnComplete},
		turnEvent(3),
	}
	for _, e := range events {
		runner.Emit(e)
	}

	if len(inner.events) != len(events) {
		t.Fatalf("inner received %d events, want %d", len(inner.events), len(events))
	}
	for i := range events {
		if inner.events[i] != events[i] {
			t.Errorf("inner event %d = %#v, want %#v", i, inner.events[i], events[i])
		}
	}
}

// TestRunnerKeepsOneHooksFiringsInOrder — a Hook sees its firings in the order the engine
// produced them, which is what a script appending to a log file depends on.
func TestRunnerKeepsOneHooksFiringsInOrder(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	runner, err := New([]domain.Reaction{commandHook("notify", TurnFinished)}, Options{
		Workspace: t.TempDir(), Exec: exec,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	const firings = 20
	for turn := 1; turn <= firings; turn++ {
		runner.Emit(turnEvent(turn))
	}
	closeRunner(t, runner)

	runs := exec.recorded()
	if len(runs) != firings {
		t.Fatalf("ran %d firings, want %d", len(runs), firings)
	}
	for i, run := range runs {
		if run.Turn != i+1 {
			t.Fatalf("firing %d carried turn %d, want %d — order was not preserved", i, run.Turn, i+1)
		}
		if run.Event != TurnFinished || run.Reaction != "notify" {
			t.Errorf("firing %d = %q/%q, want turn-finished/notify", i, run.Event, run.Reaction)
		}
	}
}

// TestRunnerStampsTheIdentityFields — the payload a Hook receives carries the facts only the
// Runner knows: which entry fired, when, in which workspace, and for which Schedule.
func TestRunnerStampsTheIdentityFields(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	resolved, err := ResolveWorkspace(workspace)
	if err != nil {
		t.Fatalf("ResolveWorkspace: %v", err)
	}
	schedule := ScheduleRef{ID: "sched-1", Name: "docs sweep"}
	fixed := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

	exec := newFakeExecutor()
	runner, err := New([]domain.Reaction{commandHook("notify", TurnFinished)}, Options{
		Workspace: workspace, Schedule: &schedule, Exec: exec,
		Now: func() time.Time { return fixed },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// A caller that reuses its ScheduleRef must not be able to rewrite a payload after the fact.
	schedule.Name = "rewritten"

	runner.Emit(turnEvent(7))
	closeRunner(t, runner)

	runs := exec.recorded()
	if len(runs) != 1 {
		t.Fatalf("ran %d firings, want 1", len(runs))
	}
	got := runs[0]
	if got.Reaction != "notify" || got.Workspace != resolved || got.Time != fixed.Format(time.RFC3339Nano) {
		t.Errorf("payload identity = %q/%q/%q, want notify/%q/%q",
			got.Reaction, got.Workspace, got.Time, resolved, fixed.Format(time.RFC3339Nano))
	}
	if got.Schedule == nil || got.Schedule.ID != "sched-1" || got.Schedule.Name != "docs sweep" {
		t.Errorf("payload schedule = %#v, want the value New copied", got.Schedule)
	}
}

// TestRunnerRunsHooksConcurrently — two Hooks fired by one event run at the same time, so a Hook
// with a slow script cannot hold up another Hook's fast one. Both block; neither can proceed
// until the gate opens, so a serial implementation never reaches the second start.
func TestRunnerRunsHooksConcurrently(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	exec.gate = make(chan struct{})
	runner, err := New([]domain.Reaction{
		commandHook("slow", TurnFinished),
		commandHook("fast", TurnFinished),
	}, Options{Workspace: t.TempDir(), Exec: exec})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	runner.Emit(turnEvent(1))

	names := map[string]bool{}
	names[awaitStart(t, exec).Reaction] = true
	names[awaitStart(t, exec).Reaction] = true
	if !names["slow"] || !names["fast"] {
		t.Fatalf("only %v started while both were blocked — the hooks are not concurrent", names)
	}

	close(exec.gate)
	closeRunner(t, runner)
}

// TestEmitReturnsWhileAHookIsBlocked — Emit runs on the engine's goroutine under the tree-wide
// sink mutex, so it must return whatever the Hook is doing. The executor never returns here.
func TestEmitReturnsWhileAHookIsBlocked(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	exec.gate = make(chan struct{})
	runner, err := New([]domain.Reaction{commandHook("wedged", TurnFinished)}, Options{
		Workspace: t.TempDir(), Exec: exec,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	returned := make(chan struct{})
	go func() {
		defer close(returned)
		for turn := 1; turn <= queueDepth*2; turn++ {
			runner.Emit(turnEvent(turn))
		}
	}()
	select {
	case <-returned:
	case <-time.After(awaitDeadline):
		t.Fatal("Emit did not return while the executor was blocked — the engine would be stalled")
	}

	close(exec.gate)
	ctx, cancel := context.WithTimeout(context.Background(), awaitDeadline)
	defer cancel()
	_ = runner.Close(ctx)
}

// TestRunnerDropsTheNewestFiringWhenAHooksQueueIsFull — the queue is a bound, not a promise. One
// firing is in the executor's hands and queueDepth more are waiting; the next is dropped, and the
// drop is reported once as it happens and once more with the total at Close.
func TestRunnerDropsTheNewestFiringWhenAHooksQueueIsFull(t *testing.T) {
	t.Parallel()

	log := &reportLog{}
	exec := newFakeExecutor()
	exec.gate = make(chan struct{})
	runner, err := New([]domain.Reaction{commandHook("wedged", TurnFinished)}, Options{
		Workspace: t.TempDir(), Exec: exec, Report: log.add,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Turn 1 is dequeued by the worker, which then blocks: the queue is empty again.
	runner.Emit(turnEvent(1))
	awaitStart(t, exec)
	// Turns 2..65 fill the queue exactly; turn 66 has nowhere to go.
	const overflow = queueDepth + 2
	for turn := 2; turn <= overflow; turn++ {
		runner.Emit(turnEvent(turn))
	}

	close(exec.gate)
	closeRunner(t, runner)

	runs := exec.recorded()
	if len(runs) != overflow-1 {
		t.Fatalf("ran %d firings, want %d — exactly one should have been dropped", len(runs), overflow-1)
	}
	for _, run := range runs {
		if run.Turn == overflow {
			t.Fatalf("the dropped firing (turn %d) reached the executor", overflow)
		}
	}

	lines := log.all()
	want := []string{"reaction wedged: dropped 1 event (queue full)", "reaction wedged: dropped 1 events"}
	if len(lines) != len(want) {
		t.Fatalf("reported %v, want exactly %v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("report line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

// TestRunnerReportsAFailureOnceUntilTheHookSucceeds — a Hook that fails every Turn is news once,
// not forever; a success in between makes the next failure news again.
func TestRunnerReportsAFailureOnceUntilTheHookSucceeds(t *testing.T) {
	t.Parallel()

	boom := errors.New("exit 1: no such file")
	log := &reportLog{}
	exec := newFakeExecutor()
	exec.errs = []error{boom, boom, nil, boom}
	runner, err := New([]domain.Reaction{commandHook("notify", TurnFinished)}, Options{
		Workspace: t.TempDir(), Exec: exec, Report: log.add,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for turn := 1; turn <= len(exec.errs); turn++ {
		runner.Emit(turnEvent(turn))
	}
	closeRunner(t, runner)

	lines := log.all()
	want := "reaction notify (turn-finished): exit 1: no such file"
	if len(lines) != 2 {
		t.Fatalf("reported %v, want exactly two lines — the repeat is suppressed, the post-success one is not", lines)
	}
	for i, line := range lines {
		if line != want {
			t.Errorf("report line %d = %q, want %q", i, line, want)
		}
	}
}

// TestRunnerIgnoresAHookScopedToAnotherWorkspace — a `workspace:` filter that does not name this
// root makes the entry inactive here, while one that does still fires (ratified call C).
func TestRunnerIgnoresAHookScopedToAnotherWorkspace(t *testing.T) {
	t.Parallel()

	here, elsewhere := t.TempDir(), t.TempDir()
	mine := commandHook("mine", TurnFinished)
	mine.Workspace = here
	theirs := commandHook("theirs", TurnFinished)
	theirs.Workspace = elsewhere

	exec := newFakeExecutor()
	runner, err := New([]domain.Reaction{mine, theirs}, Options{Workspace: here, Exec: exec})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	runner.Emit(turnEvent(1))
	closeRunner(t, runner)

	runs := exec.recorded()
	if len(runs) != 1 || runs[0].Reaction != "mine" {
		t.Fatalf("ran %#v, want the scoped-here hook alone", runs)
	}
}

// TestReplaceStopsTheOldHookAndStartsTheNew — a config reload swaps the list: the outgoing Hook
// finishes what it already holds and takes nothing more, and the incoming one takes over.
func TestReplaceStopsTheOldHookAndStartsTheNew(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	runner, err := New([]domain.Reaction{commandHook("old", TurnFinished)}, Options{
		Workspace: t.TempDir(), Exec: exec,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	runner.Emit(turnEvent(1))
	if started := awaitStart(t, exec); started.Reaction != "old" {
		t.Fatalf("first firing went to %q, want old", started.Reaction)
	}

	if err := runner.Replace([]domain.Reaction{commandHook("new", TurnFinished)}); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	runner.Emit(turnEvent(2))
	closeRunner(t, runner)

	runs := exec.recorded()
	if len(runs) != 2 {
		t.Fatalf("ran %#v, want one firing each", runs)
	}
	if runs[0].Reaction != "old" || runs[0].Turn != 1 {
		t.Errorf("first run = %q/turn %d, want old/turn 1", runs[0].Reaction, runs[0].Turn)
	}
	if runs[1].Reaction != "new" || runs[1].Turn != 2 {
		t.Errorf("second run = %q/turn %d, want new/turn 2", runs[1].Reaction, runs[1].Turn)
	}
}

// TestReplaceRefusesAMalformedListAndKeepsRunning — a broken edit to `hooks:` costs the live
// session nothing: the running set is untouched and still fires.
func TestReplaceRefusesAMalformedListAndKeepsRunning(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	runner, err := New([]domain.Reaction{commandHook("notify", TurnFinished)}, Options{
		Workspace: t.TempDir(), Exec: exec,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	broken := commandHook("notify", TurnFinished)
	broken.Handler = domain.ArgvHandler{}
	if err := runner.Replace([]domain.Reaction{broken}); err == nil {
		t.Fatal("Replace accepted an entry with an empty command")
	}

	runner.Emit(turnEvent(1))
	closeRunner(t, runner)

	if runs := exec.recorded(); len(runs) != 1 || runs[0].Reaction != "notify" {
		t.Fatalf("ran %#v, want the surviving hook to have fired", runs)
	}
}

// TestCloseWithAnExpiredContextCancelsTheRunningHook — shutdown is bounded: a wedged script does
// not hold the Driver open, and the executor's own context is cancelled under it.
func TestCloseWithAnExpiredContextCancelsTheRunningHook(t *testing.T) {
	t.Parallel()

	log := &reportLog{}
	exec := newFakeExecutor()
	exec.gate = make(chan struct{}) // never closed: the run ends only by cancellation
	runner, err := New([]domain.Reaction{commandHook("wedged", TurnFinished)}, Options{
		Workspace: t.TempDir(), Exec: exec, Report: log.add,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	runner.Emit(turnEvent(1))
	awaitStart(t, exec)

	expired, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() { done <- runner.Close(expired) }()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Close = %v, want the expired context's error", err)
		}
	case <-time.After(awaitDeadline):
		t.Fatal("Close did not return promptly with an expired context")
	}
	select {
	case <-exec.cancelled:
	case <-time.After(awaitDeadline):
		t.Fatal("the running hook's context was not cancelled")
	}

	// A job killed by our own shutdown is not a Hook failure, so it is not reported as one.
	for _, line := range log.all() {
		if strings.Contains(line, "wedged (turn-finished)") {
			t.Errorf("shutdown reported a failure line %q", line)
		}
	}
}

// TestCloseIsIdempotent — whoever closes the Runner first closes it; a second caller gets the
// same answer and no second drain.
func TestCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	runner, err := New([]domain.Reaction{commandHook("notify", TurnFinished)}, Options{
		Workspace: t.TempDir(), Exec: exec,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	closeRunner(t, runner)
	closeRunner(t, runner)

	if err := runner.Replace([]domain.Reaction{commandHook("late", TurnFinished)}); err == nil {
		t.Fatal("Replace on a closed runner was accepted")
	}
	// A closed Runner still forwards; it simply fires nothing.
	runner.Emit(turnEvent(1))
	if runs := exec.recorded(); len(runs) != 0 {
		t.Fatalf("a closed runner fired %#v", runs)
	}
}

// TestRunnerWithNoHooksTouchesNothing is the regression guard: with an empty active set the
// Runner forwards and returns, so the injected WriteTarget — the one call that reaches outside
// this package on the engine's own goroutine — is never made. Replacing a file-changed Hook in
// rebuilds the subscribed set, and the very next tool call asks for the target exactly once.
func TestRunnerWithNoHooksTouchesNothing(t *testing.T) {
	t.Parallel()

	var lookups int
	writeTarget := func(call domain.ToolCall) (string, bool) {
		lookups++
		return "/work/repo/" + call.ID + ".txt", true
	}

	inner := &recordingSink{}
	exec := newFakeExecutor()
	runner, err := New(nil, Options{
		Inner: inner, Workspace: t.TempDir(), Exec: exec, WriteTarget: writeTarget,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	call := domain.ToolCall{ID: "call-1", Tool: "write_file"}
	quiet := []domain.Event{
		turnEvent(1),
		domain.ToolCallEvent{Call: call},
		domain.ToolResultEvent{Result: domain.ToolResult{CallID: call.ID}},
	}
	for _, e := range quiet {
		runner.Emit(e)
	}
	if lookups != 0 {
		t.Fatalf("the write target was looked up %d times with no hooks configured", lookups)
	}
	if len(inner.events) != len(quiet) {
		t.Fatalf("inner received %d events, want %d", len(inner.events), len(quiet))
	}

	if err := runner.Replace([]domain.Reaction{commandHook("watch", FileChanged)}); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	second := domain.ToolCall{ID: "call-2", Tool: "write_file"}
	runner.Emit(domain.ToolCallEvent{Call: second})
	runner.Emit(domain.ToolResultEvent{Result: domain.ToolResult{CallID: second.ID}})
	closeRunner(t, runner)

	if lookups != 1 {
		t.Fatalf("the write target was looked up %d times after the replace, want exactly 1", lookups)
	}
	runs := exec.recorded()
	if len(runs) != 1 || runs[0].Event != FileChanged || runs[0].Path != "/work/repo/call-2.txt" {
		t.Fatalf("ran %#v, want one file-changed firing for the second call", runs)
	}
}

// TestNewRefusesAHookItCannotRun — a Runner that would have to fire a Hook with no executor is a
// composition mistake, refused at construction rather than discovered at the first Turn.
func TestNewRefusesAHookItCannotRun(t *testing.T) {
	t.Parallel()

	if _, err := New([]domain.Reaction{commandHook("notify", TurnFinished)}, Options{Workspace: t.TempDir()}); err == nil {
		t.Fatal("New accepted an active hook with no executor")
	}
	// With nothing to run, no executor is needed.
	runner, err := New(nil, Options{Workspace: t.TempDir()})
	if err != nil {
		t.Fatalf("New with no hooks: %v", err)
	}
	closeRunner(t, runner)
}

// TestNewRefusesAMalformedList — the Runner validates what it is handed, so a root that skipped
// the config layer's own check cannot start a Hook that could never fire correctly.
func TestNewRefusesAMalformedList(t *testing.T) {
	t.Parallel()

	nameless := commandHook("", TurnFinished)
	if _, err := New([]domain.Reaction{nameless}, Options{Workspace: t.TempDir(), Exec: newFakeExecutor()}); err == nil {
		t.Fatal("New accepted a nameless entry")
	}
}

// TestSeamClosedProjectionIsTakenBeforeEmitReturns — the value a SeamClosedEvent carries is the
// loop's own working value, valid only for the duration of Emit. The Runner therefore projects it
// while Emit is still running: the engine resumes mutating that value the instant Emit returns,
// and the firing may not run for minutes, so a document that tracked the mutation would report a
// pass that never happened.
func TestSeamClosedProjectionIsTakenBeforeEmitReturns(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	runner, err := New([]domain.Reaction{commandHook("watch", domain.MomentHistoryRewriteFinished)}, Options{
		Workspace: t.TempDir(), Exec: exec,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	conversation := domain.NewConversation([]domain.Message{{Role: domain.RoleUser, Content: "before the pass"}})
	fired := []string{"prune"}

	runner.Emit(domain.SeamClosedEvent{
		EventBase: domain.EventBase{Turn: 4},
		Seam:      domain.MomentHistoryRewrite,
		Fired:     fired,
		Value:     conversation,
	})
	conversation.SetMessageContent(0, "after the pass")
	conversation.Append(domain.Message{Role: domain.RoleAssistant, Content: "and one more"})
	fired[0] = "rewritten after the emit"
	closeRunner(t, runner)

	runs := exec.recorded()
	if len(runs) != 1 {
		t.Fatalf("ran %d firings, want 1", len(runs))
	}
	if runs[0].Event != domain.MomentHistoryRewriteFinished || runs[0].Seam != domain.MomentHistoryRewrite {
		t.Errorf("firing = %q/%q, want history-rewrite-finished/history-rewrite", runs[0].Event, runs[0].Seam)
	}
	if strings.Join(runs[0].Reactions, ",") != "prune" {
		t.Errorf("firing reactions = %v, want [prune] — the ids were referenced, not copied", runs[0].Reactions)
	}
	encoded, err := json.Marshal(runs[0].Value)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	const want = `{"messages":[{"role":"user","content":"before the pass"}]}`
	if string(encoded) != want {
		t.Errorf("firing value =\n  %s\nwant\n  %s", encoded, want)
	}
}
