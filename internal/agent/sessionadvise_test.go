package agent

// The session-start advise lane (ADR 0076 D6, 2026-10-19 amendment): an engine-origin Go advise
// handler on session-started runs in the background from each Session boundary, and the session's
// next Exchange opening joins it — waiting at most what is left of its deadline — and carries its
// answer fenced ahead of the user's text, booked as an advise firing at the join. A boundary never
// waits on it, a cancel ends the join's wait, a new boundary cancels the old run, an abort keeps the
// advice for the next opening but never replays a run the join reported, and Bypass, delegates and
// a failure attach nothing.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/testtime"
)

// sessionAdviceText is what the probe handlers answer.
const sessionAdviceText = "the build is red on main"

// sessionAdviseProbe is a SessionStartedFunc that records every start it is called with and answers
// through answer.
type sessionAdviseProbe struct {
	mu     sync.Mutex
	starts []domain.SessionStart
	answer func(ctx context.Context) (domain.Outcome, error)
}

// reaction arms the probe as engine-origin advise on session-started under timeout.
func (p *sessionAdviseProbe) reaction(id string, timeout time.Duration) domain.Reaction {
	return domain.Reaction{
		ID:      id,
		Origin:  domain.OriginEngine,
		Class:   domain.ClassAdvise,
		On:      []domain.Moment{domain.MomentSessionStarted},
		Timeout: timeout,
		Handler: domain.SessionStartedFunc(func(ctx context.Context, start domain.SessionStart) (domain.Outcome, error) {
			p.mu.Lock()
			p.starts = append(p.starts, start)
			p.mu.Unlock()
			return p.answer(ctx)
		}),
	}
}

// calls returns a copy of the starts the probe was called with.
func (p *sessionAdviseProbe) calls() []domain.SessionStart {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]domain.SessionStart(nil), p.starts...)
}

// answering is a probe that answers text at once.
func answering(text string) *sessionAdviseProbe {
	return &sessionAdviseProbe{answer: func(context.Context) (domain.Outcome, error) {
		return domain.Outcome{Inject: text}, nil
	}}
}

// blockedUntilCancel is a probe that answers nothing until its context ends, closing started when it
// is first called and ended when its context ends.
func blockedUntilCancel() (probe *sessionAdviseProbe, started, ended chan struct{}) {
	started, ended = make(chan struct{}), make(chan struct{})
	var once sync.Once
	probe = &sessionAdviseProbe{answer: func(ctx context.Context) (domain.Outcome, error) {
		once.Do(func() { close(started) })
		<-ctx.Done()
		close(ended)
		return domain.Outcome{}, ctx.Err()
	}}
	return probe, started, ended
}

// awaitStarted waits, under a testtime budget, for a blockedUntilCancel probe's handler to be called,
// failing the test with what did not start.
func awaitStarted(t *testing.T, started <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(testtime.Budget(10 * time.Second)):
		t.Fatalf("%s never started", what)
	}
}

// sessionAdviseAgent constructs a top-level Agent through New — so construction is its first Session
// boundary — with reactions armed, over responder.
func sessionAdviseAgent(
	t *testing.T,
	cfg domain.Config,
	responder *scriptedUpstream,
	reactions ...domain.Reaction,
) *Agent {
	t.Helper()
	cfg.Reactions = reactions
	a, err := New(cfg, WithDialer(dialerTo(responder).dial))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

// fenceFor is the header line the lane fences reaction id's advice under at session-started.
func fenceFor(id string) string {
	return domain.AdviceFencePrefix + id + " (engine origin) at session-started"
}

// wireFenceCount counts fence across every message req carries.
func wireFenceCount(req stubllm.Request, fence string) int {
	count := 0
	for _, m := range req.Messages {
		count += strings.Count(m.Content, fence)
	}
	return count
}

// sessionAdviseFirings returns the ReactionFiredEvents booked for reaction id.
func sessionAdviseFirings(sink *recordingSink, id string) []domain.ReactionFiredEvent {
	var fired []domain.ReactionFiredEvent
	for _, e := range sink.events {
		if f, ok := e.(domain.ReactionFiredEvent); ok && f.Reaction == id {
			fired = append(fired, f)
		}
	}
	return fired
}

// TestSessionAdvise_AGoHandlerLandsFencedInTheFirstUserMessageOnly — the advice rides ahead of the
// user's text in the session's first user message, and the second prompt carries none.
func TestSessionAdvise_AGoHandlerLandsFencedInTheFirstUserMessageOnly(t *testing.T) {
	t.Parallel()

	responder := echoResponder(t, "ok")
	a := sessionAdviseAgent(t, baseConfig(&recordingSink{}), responder, answering(sessionAdviceText).reaction("ci", 0))

	runExchange(t, a, "first prompt")
	runExchange(t, a, "second prompt")

	requests := responder.requests()
	if len(requests) != 2 {
		t.Fatalf("upstream saw %d requests, want 2", len(requests))
	}
	first := lastUserContent(t, requests[0])
	if !strings.HasPrefix(first, fenceFor("ci")) || !strings.Contains(first, sessionAdviceText) ||
		!strings.HasSuffix(first, "\n\nfirst prompt") {
		t.Errorf("first user message = %q, want the fenced advice ahead of the prompt", first)
	}
	if second := lastUserContent(t, requests[1]); second != "second prompt" {
		t.Errorf("second user message = %q, want the bare prompt", second)
	}
	if got := wireFenceCount(requests[1], fenceFor("ci")); got != 1 {
		t.Errorf("second request carries the session's first message with %d fences, want 1 (live conversation)", got)
	}
}

// TestSessionAdvise_TheFiringIsBookedAtTheJoin — nothing is booked at the boundary; the join books
// one advise firing on the loop goroutine.
func TestSessionAdvise_TheFiringIsBookedAtTheJoin(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	probe := answering(sessionAdviceText)
	a := sessionAdviseAgent(t, baseConfig(sink), echoResponder(t, "ok"), probe.reaction("ci", 0))
	testtime.Eventually(t, testtime.Budget(5*time.Second), func() bool { return len(probe.calls()) == 1 },
		"the background run to be called")

	if got := sessionAdviseFirings(sink, "ci"); len(got) != 0 {
		t.Fatalf("booked %+v before the join, want nothing", got)
	}
	runExchange(t, a, "first prompt")

	fired := sessionAdviseFirings(sink, "ci")
	if len(fired) != 1 || fired[0].Action != actionAdvise || fired[0].Moment != domain.MomentSessionStarted ||
		fired[0].Origin != domain.OriginEngine {
		t.Errorf("booked %+v, want one engine-origin advise firing at session-started", fired)
	}
}

// TestSessionAdvise_ASlowHandlerIsWaitedForUnderItsTimeout — the join waits for a handler still
// answering inside its deadline, and no longer than it takes.
func TestSessionAdvise_ASlowHandlerIsWaitedForUnderItsTimeout(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	probe := &sessionAdviseProbe{answer: func(context.Context) (domain.Outcome, error) {
		<-release
		return domain.Outcome{Inject: sessionAdviceText}, nil
	}}
	responder := echoResponder(t, "ok")
	a := sessionAdviseAgent(t, baseConfig(&recordingSink{}), responder, probe.reaction("slow", time.Minute))
	time.AfterFunc(testtime.Fixed(100*time.Millisecond), func() { close(release) })

	start := time.Now()
	runExchange(t, a, "first prompt")

	if elapsed := time.Since(start); elapsed > testtime.Budget(10*time.Second) {
		t.Errorf("the first Exchange took %s, want it bounded by the handler's answer, not its minute", elapsed)
	}
	if got := lastUserContent(t, responder.requests()[0]); !strings.Contains(got, sessionAdviceText) {
		t.Errorf("first user message = %q, want the slow handler's advice", got)
	}
}

// TestSessionAdvise_ATimeoutAddsNothingAndIsReported — a handler that misses its deadline delays the
// first request by no more than that deadline, adds no advice, and is reported as a failed firing.
func TestSessionAdvise_ATimeoutAddsNothingAndIsReported(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	probe := &sessionAdviseProbe{answer: func(context.Context) (domain.Outcome, error) {
		<-release // ignores its context: the join must still stop waiting at the deadline
		return domain.Outcome{Inject: "too late"}, nil
	}}
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	var mu sync.Mutex
	var reports []string
	cfg.Report = func(msg string) {
		mu.Lock()
		defer mu.Unlock()
		reports = append(reports, msg)
	}
	responder := echoResponder(t, "ok")
	timeout := testtime.Fixed(200 * time.Millisecond)
	a := sessionAdviseAgent(t, cfg, responder, probe.reaction("wedged", timeout))

	start := time.Now()
	runExchange(t, a, "first prompt")

	if elapsed := time.Since(start); elapsed > timeout+testtime.Budget(5*time.Second) {
		t.Errorf("the first Exchange took %s, want at most the %s deadline plus slack", elapsed, timeout)
	}
	if got := lastUserContent(t, responder.requests()[0]); got != "first prompt" {
		t.Errorf("first user message = %q, want the bare prompt", got)
	}
	fired := sessionAdviseFirings(sink, "wedged")
	if len(fired) != 1 || fired[0].Action != actionFailed {
		t.Errorf("booked %+v, want one failed firing", fired)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reports) != 1 || !strings.HasPrefix(reports[0], "reaction wedged (session-started): ") {
		t.Errorf("reported %q, want one line naming the reaction", reports)
	}
}

// TestSessionAdvise_AnErroringHandlerAddsNothing — a handler's error is fail-open: no advice, one
// failed firing.
func TestSessionAdvise_AnErroringHandlerAddsNothing(t *testing.T) {
	t.Parallel()

	probe := &sessionAdviseProbe{answer: func(context.Context) (domain.Outcome, error) {
		return domain.Outcome{Inject: "ignored"}, errors.New("ci unreachable")
	}}
	sink := &recordingSink{}
	responder := echoResponder(t, "ok")
	a := sessionAdviseAgent(t, baseConfig(sink), responder, probe.reaction("ci", 0))

	runExchange(t, a, "first prompt")

	if got := lastUserContent(t, responder.requests()[0]); got != "first prompt" {
		t.Errorf("first user message = %q, want the bare prompt", got)
	}
	if fired := sessionAdviseFirings(sink, "ci"); len(fired) != 1 || fired[0].Detail != "ci unreachable" {
		t.Errorf("booked %+v, want one failed firing carrying the error", fired)
	}
}

// TestSessionAdvise_TheDefaultDeadlineIsTheAdviseClassDefault — a handler whose reaction sets no
// Timeout runs under domain.DefaultAdviseTimeout.
func TestSessionAdvise_TheDefaultDeadlineIsTheAdviseClassDefault(t *testing.T) {
	t.Parallel()

	probe, _, _ := blockedUntilCancel()
	a := sessionAdviseAgent(t, baseConfig(&recordingSink{}), echoResponder(t, "ok"), probe.reaction("ci", 0))

	runs := a.sessionAdvice.waiting()
	if len(runs) != 1 || runs[0].timeout != domain.DefaultAdviseTimeout {
		t.Fatalf("runs = %+v, want one under the %s advise default", runs, domain.DefaultAdviseTimeout)
	}
}

// TestSessionAdvise_AClearFiresAgain — `/clear` is a boundary: the handler runs again with start
// "clear" and the cleared session's first prompt carries its advice.
func TestSessionAdvise_AClearFiresAgain(t *testing.T) {
	t.Parallel()

	probe := answering(sessionAdviceText)
	responder := echoResponder(t, "ok")
	a := sessionAdviseAgent(t, baseConfig(&recordingSink{}), responder, probe.reaction("ci", 0))
	runExchange(t, a, "first prompt")

	if err := a.ClearContext(); err != nil {
		t.Fatalf("ClearContext: %v", err)
	}
	runExchange(t, a, "after the clear")

	if got := lastUserContent(t, responder.requests()[1]); !strings.HasPrefix(got, fenceFor("ci")) {
		t.Errorf("cleared session's first user message = %q, want it advised", got)
	}
	if got := probe.calls(); len(got) != 2 || got[1] != domain.SessionStartClear {
		t.Errorf("handler called with %v, want [new clear]", got)
	}
}

// TestSessionAdvise_AResumeDropsTheOldSpan — the session record carries no advice, so a restore
// brings back the bare prompt, and the resumed session's next prompt is advised afresh.
func TestSessionAdvise_AResumeDropsTheOldSpan(t *testing.T) {
	t.Parallel()

	probe := answering(sessionAdviceText)
	responder := echoResponder(t, "ok")
	a := sessionAdviseAgent(t, baseConfig(&recordingSink{}), responder, probe.reaction("ci", 0))
	runExchange(t, a, "first prompt")
	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	if err := a.RestoreSession(snap); err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}

	if got := a.conv.At(0); got.Content != "first prompt" || len(got.Advice) != 0 {
		t.Errorf("restored first message = %q with %d spans, want the bare prompt and no spans", got.Content, len(got.Advice))
	}
	runExchange(t, a, "after the resume")
	last := responder.requests()[1]
	if got := lastUserContent(t, last); !strings.HasPrefix(got, fenceFor("ci")) {
		t.Errorf("resumed session's next user message = %q, want it advised", got)
	}
	if got := wireFenceCount(last, fenceFor("ci")); got != 1 {
		t.Errorf("resumed request carries %d fences, want 1 (the new prompt's alone)", got)
	}
}

// TestSessionAdvise_BypassAttachesNothing — Bypass switches advise off (D9): the handler is never
// started and the first prompt goes out bare.
func TestSessionAdvise_BypassAttachesNothing(t *testing.T) {
	t.Parallel()

	probe := answering(sessionAdviceText)
	cfg := baseConfig(&recordingSink{})
	cfg.Bypass = true
	responder := echoResponder(t, "ok")
	a := sessionAdviseAgent(t, cfg, responder, probe.reaction("ci", 0))

	runExchange(t, a, "first prompt")

	if got := lastUserContent(t, responder.requests()[0]); got != "first prompt" {
		t.Errorf("first user message = %q, want the bare prompt under Bypass", got)
	}
	if got := probe.calls(); len(got) != 0 {
		t.Errorf("handler called with %v under Bypass, want never", got)
	}
}

// TestSessionAdvise_ADelegateAttachesNothing — a delegate starts no session: the parent's opening is
// advised once, the child's task goes out bare, and the handler runs once.
func TestSessionAdvise_ADelegateAttachesNothing(t *testing.T) {
	t.Parallel()

	probe := answering(sessionAdviceText)
	responder := scriptedResponder(t,
		subAgentCallTurn("c1", "summarise the repo"),
		contentTurn("the repo is a Go TUI agent"),
		contentTurn("done — delegated and summarised"),
	)
	a := sessionAdviseAgent(t, subAgentConfig(&recordingSink{}, domain.ModeAskBefore), responder, probe.reaction("ci", 0))

	runExchange(t, a, "please research")

	requests := responder.requests()
	if len(requests) != 3 {
		t.Fatalf("upstream saw %d requests, want 3", len(requests))
	}
	if got := lastUserContent(t, requests[0]); !strings.HasPrefix(got, fenceFor("ci")) {
		t.Errorf("parent's opening = %q, want it advised", got)
	}
	if got := lastUserContent(t, requests[1]); strings.Contains(got, domain.AdviceFencePrefix) {
		t.Errorf("delegate's task = %q, want no advice", got)
	}
	if got := probe.calls(); len(got) != 1 {
		t.Errorf("handler called %d times, want once (the parent's construction)", len(got))
	}
}

// TestSessionAdvise_EscDuringTheJoinEndsTheWait — a cancel while the join waits on a handler ends
// the Turn at once, before any request is sent.
func TestSessionAdvise_EscDuringTheJoinEndsTheWait(t *testing.T) {
	t.Parallel()

	probe, started, _ := blockedUntilCancel()
	responder := echoResponder(t, "ok")
	a := sessionAdviseAgent(t, baseConfig(&recordingSink{}), responder, probe.reaction("ci", time.Hour))
	awaitStarted(t, started, "the construction boundary's advise run")
	if err := a.Submit(domain.UserInput{Text: "first prompt"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(testtime.Fixed(50*time.Millisecond), cancel)

	done := make(chan domain.StepResult, 1)
	go func() {
		res, _ := a.Step(ctx)
		done <- res
	}()

	select {
	case res := <-done:
		if res.Status != domain.StatusCancelled {
			t.Errorf("Step status = %q, want %q", res.Status, domain.StatusCancelled)
		}
	case <-time.After(testtime.Budget(10 * time.Second)):
		t.Fatal("Step still waiting on the handler after the cancel")
	}
	if got := responder.calls(); got != 0 {
		t.Errorf("upstream saw %d requests, want none", got)
	}
}

// TestSessionAdvise_AnAbortedFirstExchangeKeepsTheAdvice — an Exchange whose opening took the advice
// and was then aborted puts it back, so the next opening carries it.
func TestSessionAdvise_AnAbortedFirstExchangeKeepsTheAdvice(t *testing.T) {
	t.Parallel()

	probe := answering(sessionAdviceText)
	responder := echoResponder(t, "ok")
	a := sessionAdviseAgent(t, baseConfig(&recordingSink{}), responder, probe.reaction("ci", 0))
	testtime.Eventually(t, testtime.Budget(5*time.Second), func() bool {
		runs := a.sessionAdvice.waiting()
		if len(runs) != 1 {
			return false
		}
		select {
		case <-runs[0].done:
			return true
		default:
			return false
		}
	}, "the background run to answer")
	if err := a.Submit(domain.UserInput{Text: "aborted prompt"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if res, err := a.Step(cancelled); err != nil || res.Status != domain.StatusCancelled {
		t.Fatalf("Step = %+v, %v; want a cancelled Turn", res, err)
	}
	if !strings.HasPrefix(a.conv.At(0).Content, fenceFor("ci")) {
		t.Fatalf("aborted opening = %q, want the advice taken into it", a.conv.At(0).Content)
	}

	a.AbortExchange()
	runExchange(t, a, "next prompt")

	if got := lastUserContent(t, responder.requests()[0]); !strings.HasPrefix(got, fenceFor("ci")) ||
		!strings.HasSuffix(got, "next prompt") {
		t.Errorf("next opening = %q, want it to carry the advice the abort put back", got)
	}
}

// abortAfterTheJoin opens an Exchange on text under a context already cancelled, so the join runs
// over what the lane holds and the Turn takes the cancel exit before any request, then aborts it.
func abortAfterTheJoin(t *testing.T, a *Agent, text string) {
	t.Helper()
	if err := a.Submit(domain.UserInput{Text: text}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if res, err := a.Step(cancelled); err != nil || res.Status != domain.StatusCancelled {
		t.Fatalf("Step = %+v, %v; want a cancelled Turn", res, err)
	}
	a.AbortExchange()
}

// reportRecorder collects every line handed to Config.Report.
type reportRecorder struct {
	mu    sync.Mutex
	lines []string
}

// report is the Config.Report hook.
func (r *reportRecorder) report(msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, msg)
}

// all returns a copy of the lines reported so far.
func (r *reportRecorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.lines...)
}

// TestSessionAdvise_ATimedOutRunIsNeverReplayedAfterAnAbort — a run the join reported as timed out
// is dropped there: an abort of that Exchange does not put it back, so the next opening neither
// reports it again nor attaches its answer — even though the handler answered in between.
func TestSessionAdvise_ATimedOutRunIsNeverReplayedAfterAnAbort(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseHandler := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseHandler)
	probe := &sessionAdviseProbe{answer: func(context.Context) (domain.Outcome, error) {
		<-release // ignores its context, so it answers after its deadline once released
		return domain.Outcome{Inject: "too late"}, nil
	}}
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	reports := &reportRecorder{}
	cfg.Report = reports.report
	responder := echoResponder(t, "ok")
	timeout := testtime.Fixed(50 * time.Millisecond)
	a := sessionAdviseAgent(t, cfg, responder, probe.reaction("wedged", timeout))
	testtime.Eventually(t, testtime.Budget(5*time.Second), func() bool {
		return len(a.sessionAdvice.waiting()) == 1
	}, "the background run to be pending")
	run := a.sessionAdvice.waiting()[0]
	// The run's deadline was set before it was pending, so an expiry of the same timeout started
	// now ends no earlier than that deadline.
	testtime.Eventually(t, testtime.Budget(5*time.Second), testtime.Expiry(timeout), "the background run's deadline to pass")

	abortAfterTheJoin(t, a, "aborted prompt")
	if got := len(reports.all()); got != 1 {
		t.Fatalf("reported %d lines at the aborted join, want the one timeout", got)
	}
	releaseHandler()
	<-run.done // the handler answers after the join reported it, before the next opening
	runExchange(t, a, "next prompt")

	if got := lastUserContent(t, responder.requests()[0]); got != "next prompt" {
		t.Errorf("next opening = %q, want the bare prompt", got)
	}
	if got := wireFenceCount(responder.requests()[0], fenceFor("wedged")); got != 0 {
		t.Errorf("next request carries %d fences for the timed-out run, want none", got)
	}
	if got := reports.all(); len(got) != 1 || !strings.Contains(got[0], context.DeadlineExceeded.Error()) {
		t.Errorf("reported %q, want exactly the one timeout", got)
	}
	if fired := sessionAdviseFirings(sink, "wedged"); len(fired) != 1 || fired[0].Action != actionFailed {
		t.Errorf("booked %+v, want one failed firing", fired)
	}
}

// TestSessionAdvise_AnErroredRunIsNeverReplayedAfterAnAbort — a run the join reported as failed is
// dropped there: the aborted Exchange does not put it back, so the next opening reports nothing
// more and attaches nothing.
func TestSessionAdvise_AnErroredRunIsNeverReplayedAfterAnAbort(t *testing.T) {
	t.Parallel()

	probe := &sessionAdviseProbe{answer: func(context.Context) (domain.Outcome, error) {
		return domain.Outcome{Inject: "ignored"}, errors.New("ci unreachable")
	}}
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	reports := &reportRecorder{}
	cfg.Report = reports.report
	responder := echoResponder(t, "ok")
	a := sessionAdviseAgent(t, cfg, responder, probe.reaction("ci", 0))
	testtime.Eventually(t, testtime.Budget(5*time.Second), func() bool {
		runs := a.sessionAdvice.waiting()
		if len(runs) != 1 {
			return false
		}
		select {
		case <-runs[0].done:
			return true
		default:
			return false
		}
	}, "the background run to fail")

	abortAfterTheJoin(t, a, "aborted prompt")
	if got := a.sessionAdvice.waiting(); len(got) != 0 {
		t.Fatalf("pending after the abort = %d runs, want the reported run dropped", len(got))
	}
	runExchange(t, a, "next prompt")

	if got := lastUserContent(t, responder.requests()[0]); got != "next prompt" {
		t.Errorf("next opening = %q, want the bare prompt", got)
	}
	if got := reports.all(); len(got) != 1 || !strings.HasSuffix(got[0], "ci unreachable") {
		t.Errorf("reported %q, want exactly the one failure", got)
	}
	if fired := sessionAdviseFirings(sink, "ci"); len(fired) != 1 || fired[0].Detail != "ci unreachable" {
		t.Errorf("booked %+v, want one failed firing carrying the error", fired)
	}
}

// TestSessionAdvise_ABoundaryCancelsThePreviousRun — a new boundary cancels the run the previous one
// started, and discards it.
func TestSessionAdvise_ABoundaryCancelsThePreviousRun(t *testing.T) {
	t.Parallel()

	probe, started, ended := blockedUntilCancel()
	a := sessionAdviseAgent(t, baseConfig(&recordingSink{}), echoResponder(t, "ok"), probe.reaction("ci", time.Hour))
	awaitStarted(t, started, "the construction boundary's advise run")

	if err := a.ClearContext(); err != nil {
		t.Fatalf("ClearContext: %v", err)
	}

	select {
	case <-ended:
	case <-time.After(testtime.Budget(10 * time.Second)):
		t.Fatal("the previous boundary's run was not cancelled by the clear")
	}
}

// TestSessionAdvise_ABoundaryOverABlockedHandlerReturnsAtOnce — every TUI boundary runs on the Update
// goroutine, so construction and a clear return while the handler is still blocked.
func TestSessionAdvise_ABoundaryOverABlockedHandlerReturnsAtOnce(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	probe := &sessionAdviseProbe{answer: func(context.Context) (domain.Outcome, error) {
		<-release // ignores its context altogether
		return domain.Outcome{}, nil
	}}

	cfg := baseConfig(&recordingSink{})
	cfg.Reactions = []domain.Reaction{probe.reaction("ci", time.Hour)}
	dial := WithDialer(dialerTo(echoResponder(t, "ok")).dial)

	done := make(chan struct{})
	go func() {
		defer close(done)
		a, err := New(cfg, dial)
		if err != nil {
			t.Errorf("New: %v", err)
			return
		}
		defer func() { _ = a.Close() }()
		if err := a.ClearContext(); err != nil {
			t.Errorf("ClearContext: %v", err)
		}
	}()

	select {
	case <-done:
	case <-time.After(testtime.Budget(10 * time.Second)):
		t.Fatal("a boundary waited on a blocked session-start handler")
	}
}

// An advise entry a live swap arms inside the boundary's pre-first-prompt window (sessionWindow)
// starts for that session and is joined at its first opening, exactly as the boundary's own runs
// are; armed after the first prompt it waits for the next boundary, and an entry that already ran
// at the boundary never runs twice — with or without an observe Runner.

// liveAdviceEntry is a user `advise:` entry on session-started whose command prints
// sessionAdviceText, the shape a repo entry adopted, or a global one added, arms on the live lane.
func liveAdviceEntry(id string) domain.Reaction {
	return userAdvise(id, sessionStarted, "/bin/sh", "-c", `printf '%s\n' "`+sessionAdviceText+`"`)
}

// liveAdviceAgent constructs a top-level Agent through New over responder with no reaction armed —
// holding a recording observe Runner when withRunner is set — so construction opens its window over
// empty lanes.
func liveAdviceAgent(t *testing.T, sink *recordingSink, responder *scriptedUpstream, withRunner bool) *Agent {
	t.Helper()
	cfg := baseConfig(sink)
	if withRunner {
		cfg.ObserveRunner = &startRecordingRunner{}
	}
	return sessionAdviseAgent(t, cfg, responder)
}

// armSync swaps a's sync lane to list through SetReactions, every other field of the generation
// kept — what a settings reload hands the Agent.
func armSync(t *testing.T, a *Agent, list ...domain.Reaction) {
	t.Helper()
	gen := a.Generation()
	gen.Sync = list
	if err := a.SetReactions(gen); err != nil {
		t.Fatalf("SetReactions: %v", err)
	}
}

// TestSessionWindow_AnAdviseEntryArmedBeforeTheFirstPromptIsFencedAtTheOpening — the entry armed
// after construction and before the first prompt rides fenced ahead of that prompt, booked once,
// whether or not the Agent holds an observe Runner.
func TestSessionWindow_AnAdviseEntryArmedBeforeTheFirstPromptIsFencedAtTheOpening(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	for _, tc := range []struct {
		name       string
		withRunner bool
	}{
		{name: "with an observe Runner", withRunner: true},
		{name: "without an observe Runner", withRunner: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sink := &recordingSink{}
			responder := echoResponder(t, "ok")
			a := liveAdviceAgent(t, sink, responder, tc.withRunner)

			armSync(t, a, liveAdviceEntry("ci"))
			runExchange(t, a, "first prompt")

			first := lastUserContent(t, responder.requests()[0])
			if !strings.HasPrefix(first, userFenceFor("ci")) || strings.Count(first, userFenceFor("ci")) != 1 ||
				!strings.Contains(first, sessionAdviceText) || !strings.HasSuffix(first, "\n\nfirst prompt") {
				t.Errorf("first user message = %q, want the live-armed advice fenced once ahead of the prompt", first)
			}
			if fired := sessionAdviseFirings(sink, "ci"); len(fired) != 1 {
				t.Errorf("booked %d firings for the live-armed entry, want 1: %+v", len(fired), fired)
			}
		})
	}
}

// TestSessionWindow_AnAdviseEntryArmedAfterTheFirstPromptWaitsForTheNextBoundary — once the session
// has sent its first user message, a newly armed entry starts nothing for it: the second prompt goes
// out bare, and the clear that follows runs the entry as its own boundary's.
func TestSessionWindow_AnAdviseEntryArmedAfterTheFirstPromptWaitsForTheNextBoundary(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	responder := echoResponder(t, "ok")
	a := liveAdviceAgent(t, &recordingSink{}, responder, true)

	runExchange(t, a, "first prompt")
	armSync(t, a, liveAdviceEntry("ci"))
	runExchange(t, a, "second prompt")
	if err := a.ClearContext(); err != nil {
		t.Fatalf("ClearContext: %v", err)
	}
	runExchange(t, a, "third prompt")

	requests := responder.requests()
	if len(requests) != 3 {
		t.Fatalf("upstream saw %d requests, want 3", len(requests))
	}
	if second := lastUserContent(t, requests[1]); second != "second prompt" {
		t.Errorf("second user message = %q, want the bare prompt — an entry armed after the first prompt waits", second)
	}
	if third := lastUserContent(t, requests[2]); strings.Count(third, userFenceFor("ci")) != 1 {
		t.Errorf("third user message = %q, want the clear's own run fenced once", third)
	}
}

// TestSessionWindow_ReArmingAnAdviseEntryStartsNothing — "newly armed" is read against the
// boundary's fired-id set: an unchanged reload, and the entry removed and added back, start no
// second run, so the opening carries one fence and one firing.
func TestSessionWindow_ReArmingAnAdviseEntryStartsNothing(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	sink := &recordingSink{}
	responder := echoResponder(t, "ok")
	a := liveAdviceAgent(t, sink, responder, true)

	armSync(t, a, liveAdviceEntry("ci"))
	armSync(t, a, liveAdviceEntry("ci"))
	armSync(t, a)
	armSync(t, a, liveAdviceEntry("ci"))
	runExchange(t, a, "first prompt")

	if got := strings.Count(lastUserContent(t, responder.requests()[0]), userFenceFor("ci")); got != 1 {
		t.Errorf("the opening carries %d fences for the re-armed entry, want 1", got)
	}
	if fired := sessionAdviseFirings(sink, "ci"); len(fired) != 1 {
		t.Errorf("booked %d firings for the re-armed entry, want 1: %+v", len(fired), fired)
	}
}

// TestSessionWindow_AnAdviseEntryArmedAtTheBoundaryRunsOnce — the regression guard: an entry the
// generation armed before a deferred announcement (lateEngine.Bind's replay) is started by the
// boundary itself, and an unchanged SetReactions before the first prompt — a `/settings` toggle —
// starts no second run.
func TestSessionWindow_AnAdviseEntryArmedAtTheBoundaryRunsOnce(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	sink := &recordingSink{}
	responder := echoResponder(t, "ok")
	cfg := baseConfig(sink)
	cfg.ObserveRunner = &startRecordingRunner{}
	cfg.DeferSessionStart = true
	a := sessionAdviseAgent(t, cfg, responder)

	armSync(t, a, liveAdviceEntry("ci"))
	a.AnnounceSessionStart()
	armSync(t, a, liveAdviceEntry("ci"))
	runExchange(t, a, "first prompt")

	if got := strings.Count(lastUserContent(t, responder.requests()[0]), userFenceFor("ci")); got != 1 {
		t.Errorf("the opening carries %d fences for the entry armed at the boundary, want 1", got)
	}
	if fired := sessionAdviseFirings(sink, "ci"); len(fired) != 1 {
		t.Errorf("booked %d firings for the entry armed at the boundary, want 1: %+v", len(fired), fired)
	}
}

// TestSessionAdvise_ALiveArmedRunEndsAtTheNextBoundary — a run a live swap started on a boundary that
// started none of its own is still ended by the next boundary: the lane installed a cancel for it.
// The engine-origin Go handler is armed through installGeneration, as advise_test.go does, because
// the live lane refuses that shape at SetReactions.
func TestSessionAdvise_ALiveArmedRunEndsAtTheNextBoundary(t *testing.T) {
	t.Parallel()

	probe, started, ended := blockedUntilCancel()
	a := liveAdviceAgent(t, &recordingSink{}, echoResponder(t, "ok"), false)
	gen := a.Generation()
	gen.Sync = []domain.Reaction{probe.reaction("ci", time.Hour)}
	a.installGeneration(gen)
	a.fireNewlyArmed()
	awaitStarted(t, started, "the live-armed advise run")

	if err := a.ClearContext(); err != nil {
		t.Fatalf("ClearContext: %v", err)
	}

	select {
	case <-ended:
	case <-time.After(testtime.Budget(10 * time.Second)):
		t.Fatal("the live-armed run was not cancelled by the clear")
	}
}

// TestSessionAdvise_ALiveArmAfterCloseStartsNoRun — Close shuts the session window, so a live swap
// after it arms a session-started entry for neither lane: the advise lane holds no run and starts
// no handler, and the observe entry SetReactions arms never reaches the Runner. The engine-origin Go
// handler is armed through installGeneration, as TestSessionAdvise_ALiveArmedRunEndsAtTheNextBoundary
// does, because the live lane refuses that shape at SetReactions.
func TestSessionAdvise_ALiveArmAfterCloseStartsNoRun(t *testing.T) {
	t.Parallel()
	probe, _, _ := blockedUntilCancel()
	a := liveAdviceAgent(t, &recordingSink{}, echoResponder(t, "ok"), true)
	runner := a.cfg.ObserveRunner.(*startRecordingRunner)
	t.Cleanup(a.sessionAdvice.stop) // the base tree's leaked run must not outlive the test

	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	armObserve(t, a, sessionStartedEntry("primer", false))
	gen := a.Generation()
	gen.Sync = []domain.Reaction{probe.reaction("ci", time.Hour)}
	a.installGeneration(gen)
	a.fireNewlyArmed()

	if waiting := a.sessionAdvice.waiting(); len(waiting) != 0 {
		t.Errorf("the advise lane holds %d runs after Close, want none", len(waiting))
	}
	if calls := probe.calls(); len(calls) != 0 {
		t.Errorf("the advise handler ran %d times after Close, want never: %+v", len(calls), calls)
	}
	if armed := runner.armedCalls(); len(armed) != 0 {
		t.Errorf("the Runner was handed %d narrowed firings after Close, want none: %+v", len(armed), armed)
	}
}
