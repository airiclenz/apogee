package agent

// The session briefing (sessionBriefing, briefing.go) tested across its interface. The module tests
// drive a bare briefing through its events alone — boundaryArmed, liveArmed, inputArrived, close,
// foldBegan, foldEnded, the two joins and the Exchange ends — with no Agent: a fake adviseCaller
// stands in for the handlers and a recording ObserveRunner (startRecordingRunner) for the Runner, and
// a join's landing is how a test reads what a lane holds. The Agent-driven tests after them read
// what only the module can tell: the timeout each of a boundary's runs carries, and that a
// boundary's run has answered before a cancelled opening joins it.

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/testtime"
)

// briefingCall is one call the fake handler took: the entry, the Turn and seam document it ran
// with, and the deadline its context carried.
type briefingCall struct {
	id       string
	turn     int
	doc      domain.SeamPayload
	deadline time.Time
}

// briefingHandler is the fake adviseCaller the module tests inject: it records every call and
// answers through answer — or, when answer is nil, at once with the entry's id as its advice.
type briefingHandler struct {
	mu     sync.Mutex
	calls  []briefingCall
	answer func(ctx context.Context, r domain.Reaction) (domain.Outcome, error)
}

// call is the adviseCaller.
func (h *briefingHandler) call(ctx context.Context, turn int, doc domain.SeamPayload, r domain.Reaction) (domain.Outcome, error) {
	deadline, _ := ctx.Deadline()
	h.mu.Lock()
	h.calls = append(h.calls, briefingCall{id: r.ID, turn: turn, doc: doc, deadline: deadline})
	h.mu.Unlock()
	if h.answer != nil {
		return h.answer(ctx, r)
	}
	return domain.Outcome{Inject: r.ID}, nil
}

// recorded returns a copy of the calls taken so far.
func (h *briefingHandler) recorded() []briefingCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.calls)
}

// newBriefing is a top-level Agent's briefing over handler, closed when the test ends.
func newBriefing(t *testing.T, handler *briefingHandler) *sessionBriefing {
	t.Helper()
	b := &sessionBriefing{call: handler.call}
	t.Cleanup(b.close)
	return b
}

// briefingEntries is one advise entry on m per id, in ladder order.
func briefingEntries(m domain.Moment, ids ...string) []domain.Reaction {
	entries := make([]domain.Reaction, 0, len(ids))
	for _, id := range ids {
		entries = append(entries, domain.Reaction{ID: id, Class: domain.ClassAdvise, On: []domain.Moment{m}})
	}
	return entries
}

// answeredRun is the verdict the Agent's landing reaches on run: it answered when it is done with
// no panic and no error. A run still going at its deadline, or one that failed, is one the landing
// reports, and the join then drops it.
func answeredRun(run *sessionAdviseRun) bool {
	select {
	case <-run.done:
		return run.panicked == nil && run.err == nil
	default:
		return false
	}
}

// joined runs join — a briefing's joinOpening or joinBridge — under a live context with a landing
// that judges each run as the Agent's does (answeredRun), and returns the ids it landed, in order.
func joined(join func(context.Context, adviceLanding)) []string {
	var landed []string
	join(context.Background(), func(run *sessionAdviseRun) bool {
		landed = append(landed, run.spec.ID)
		return answeredRun(run)
	})
	return landed
}

// TestBriefing_ALiveArmRunsOnlyWhatTheBoundaryDidNot — a boundary starts its advise entries and
// hands session-started to the Runner; a live swap inside its window runs only the entries the
// boundary did not, advise and observe alike, at the boundary's own start and Turn; and the opening
// joins the boundary's run ahead of the live one.
func TestBriefing_ALiveArmRunsOnlyWhatTheBoundaryDidNot(t *testing.T) {
	t.Parallel()
	handler := &briefingHandler{}
	runner := &startRecordingRunner{}
	b := newBriefing(t, handler)

	b.boundaryArmed(domain.SessionStartClear, 3, sessionArmed{
		advise: briefingEntries(domain.MomentSessionStarted, "brief"), observe: []string{"primer"}, runner: runner,
	})
	b.liveArmed(sessionArmed{
		advise:  briefingEntries(domain.MomentSessionStarted, "brief", "late"),
		observe: []string{"primer", "audit"},
		runner:  runner,
	})
	landed := joined(b.joinOpening)

	if !slices.Equal(landed, []string{"brief", "late"}) {
		t.Errorf("the opening landed %v, want [brief late]", landed)
	}
	if got := runner.recorded(); !slices.Equal(got, []domain.SessionStart{domain.SessionStartClear}) {
		t.Errorf("the Runner was handed session-started %v, want the boundary's once", got)
	}
	if got := runner.armedCalls(); len(got) != 1 || got[0].start != domain.SessionStartClear ||
		!slices.Equal(got[0].ids, []string{"audit"}) {
		t.Errorf("the Runner's narrowed firings = %+v, want audit alone at the boundary's start", got)
	}
	calls := handler.recorded()
	if len(calls) != 2 {
		t.Fatalf("the handler took %d calls, want one per entry", len(calls))
	}
	for _, c := range calls {
		if c.turn != 3 || c.doc.Event != domain.MomentSessionStarted || c.doc.Start != domain.SessionStartClear {
			t.Errorf("%s ran at Turn %d with %+v, want the boundary's Turn and start", c.id, c.turn, c.doc)
		}
	}
}

// TestBriefing_AnEndedWindowArmsNothing — an input or close ends the boundary's window, so a live
// swap after it runs neither an advise entry nor an observe one.
func TestBriefing_AnEndedWindowArmsNothing(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		end  func(*sessionBriefing)
	}{
		{name: "an input arrived", end: (*sessionBriefing).inputArrived},
		{name: "the Agent closed", end: (*sessionBriefing).close},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			handler := &briefingHandler{}
			runner := &startRecordingRunner{}
			b := newBriefing(t, handler)
			b.boundaryArmed(domain.SessionStartNew, 0, sessionArmed{runner: runner})

			tc.end(b)
			b.liveArmed(sessionArmed{
				advise: briefingEntries(domain.MomentSessionStarted, "late"), observe: []string{"audit"}, runner: runner,
			})

			if landed := joined(b.joinOpening); len(landed) != 0 {
				t.Errorf("the opening landed %v, want no run", landed)
			}
			if calls := handler.recorded(); len(calls) != 0 {
				t.Errorf("the handler took %d calls, want none", len(calls))
			}
			if armed := runner.armedCalls(); len(armed) != 0 {
				t.Errorf("the Runner was handed %+v, want no narrowed firing", armed)
			}
		})
	}
}

// TestBriefing_CloseCancelsBothLanesRuns — close cancels the runs a boundary started and the ones a
// fold that ran handed the compaction lane, so no handler outlives the Agent.
func TestBriefing_CloseCancelsBothLanesRuns(t *testing.T) {
	t.Parallel()
	ended := make(chan string, 2)
	handler := &briefingHandler{answer: func(ctx context.Context, r domain.Reaction) (domain.Outcome, error) {
		<-ctx.Done()
		ended <- r.ID
		return domain.Outcome{}, ctx.Err()
	}}
	b := newBriefing(t, handler)
	brief := briefingEntries(domain.MomentSessionStarted, "brief")
	brief[0].Timeout = time.Hour
	rebrief := briefingEntries(domain.MomentCompactionFinished, "rebrief")
	rebrief[0].Timeout = time.Hour
	b.boundaryArmed(domain.SessionStartNew, 0, sessionArmed{advise: brief})
	b.foldEnded(b.foldBegan(domain.CompactionFoldAuto, 1, rebrief, nil), true)

	b.close()

	var stopped []string
	for range 2 {
		select {
		case id := <-ended:
			stopped = append(stopped, id)
		case <-time.After(testtime.Budget(10 * time.Second)):
			t.Fatalf("close cancelled only %v, want both lanes' runs", stopped)
		}
	}
	if landed := joined(b.joinOpening); len(landed) != 0 {
		t.Errorf("the opening landed %v after close, want no run", landed)
	}
}

// TestBriefing_AnAbortPutsTheTakenRunsBackAheadOfPending — the runs an opening took that answered go
// back ahead of each lane's pending runs when its Exchange is aborted, so the next opening carries
// them first, the session-start lane still ahead of the compaction lane; an Exchange that closed
// keeps nothing it took.
func TestBriefing_AnAbortPutsTheTakenRunsBackAheadOfPending(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		end  func(*sessionBriefing)
		want []string
	}{
		{name: "aborted", end: (*sessionBriefing).exchangeAborted, want: []string{"first", "second", "rebrief"}},
		{name: "closed", end: (*sessionBriefing).exchangeClosed, want: []string{"second"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := newBriefing(t, &briefingHandler{})
			b.boundaryArmed(domain.SessionStartNew, 0, sessionArmed{advise: briefingEntries(domain.MomentSessionStarted, "first")})
			rebrief := briefingEntries(domain.MomentCompactionFinished, "rebrief")
			b.foldEnded(b.foldBegan(domain.CompactionFoldAuto, 0, rebrief, nil), true)
			if got := joined(b.joinOpening); !slices.Equal(got, []string{"first", "rebrief"}) {
				t.Fatalf("the first opening landed %v, want [first rebrief]", got)
			}
			b.liveArmed(sessionArmed{advise: briefingEntries(domain.MomentSessionStarted, "first", "second")})

			tc.end(b)
			got := joined(b.joinOpening)

			if !slices.Equal(got, tc.want) {
				t.Errorf("the next opening landed %v, want %v", got, tc.want)
			}
		})
	}
}

// TestBriefing_AReportedRunIsNeverReplayed — a run the landing reports — it errored, panicked or
// missed its deadline — is dropped at the join: an abort puts back only the runs that answered.
func TestBriefing_AReportedRunIsNeverReplayed(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		timeout time.Duration
		answer  func(ctx context.Context) (domain.Outcome, error)
	}{
		{name: "errored", answer: func(context.Context) (domain.Outcome, error) {
			return domain.Outcome{Inject: "ignored"}, errors.New("ci unreachable")
		}},
		{name: "panicked", answer: func(context.Context) (domain.Outcome, error) {
			panic("handler bug")
		}},
		{name: "timed out", timeout: testtime.Fixed(20 * time.Millisecond), answer: func(ctx context.Context) (domain.Outcome, error) {
			<-ctx.Done()
			return domain.Outcome{}, ctx.Err()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := newBriefing(t, &briefingHandler{answer: func(ctx context.Context, r domain.Reaction) (domain.Outcome, error) {
				if r.ID == "bad" {
					return tc.answer(ctx)
				}
				return domain.Outcome{Inject: r.ID}, nil
			}})
			entries := briefingEntries(domain.MomentSessionStarted, "ok", "bad")
			entries[1].Timeout = tc.timeout
			b.boundaryArmed(domain.SessionStartNew, 0, sessionArmed{advise: entries})
			if got := joined(b.joinOpening); !slices.Equal(got, []string{"ok", "bad"}) {
				t.Fatalf("the first opening landed %v, want [ok bad]", got)
			}

			b.exchangeAborted()
			got := joined(b.joinOpening)

			if !slices.Equal(got, []string{"ok"}) {
				t.Errorf("the next opening landed %v, want [ok] alone", got)
			}
		})
	}
}

// TestBriefing_AFoldThatRanAdoptsItsRunsAndOneThatDidNotDiscardsThem — a fold that ran hands its
// staged runs to the compaction lane, in place of an earlier fold's, and hands compaction-finished to
// the Runner with its trigger and Turn; a fold that did not run discards its runs, announces nothing
// and leaves an earlier fold's runs standing. The bridge joins the compaction lane alone.
func TestBriefing_AFoldThatRanAdoptsItsRunsAndOneThatDidNotDiscardsThem(t *testing.T) {
	t.Parallel()

	type fold struct {
		id  string
		ran bool
	}
	for _, tc := range []struct {
		name      string
		folds     []fold
		landed    []string
		announced []foldCall
	}{
		{
			name:      "a fold that ran",
			folds:     []fold{{"first", true}},
			landed:    []string{"first"},
			announced: []foldCall{{domain.CompactionFoldEmergency, 0}},
		},
		{
			name:  "a fold that did not run",
			folds: []fold{{"first", false}},
		},
		{
			name:      "a later fold that ran supersedes the earlier",
			folds:     []fold{{"first", true}, {"second", true}},
			landed:    []string{"second"},
			announced: []foldCall{{domain.CompactionFoldEmergency, 0}, {domain.CompactionFoldEmergency, 1}},
		},
		{
			name:      "a later fold that did not run leaves the earlier standing",
			folds:     []fold{{"first", true}, {"second", false}},
			landed:    []string{"first"},
			announced: []foldCall{{domain.CompactionFoldEmergency, 0}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runner := &startRecordingRunner{}
			b := newBriefing(t, &briefingHandler{})
			b.boundaryArmed(domain.SessionStartNew, 0, sessionArmed{advise: briefingEntries(domain.MomentSessionStarted, "brief")})

			for turn, f := range tc.folds {
				staged := b.foldBegan(domain.CompactionFoldEmergency, turn, briefingEntries(domain.MomentCompactionFinished, f.id), runner)
				b.foldEnded(staged, f.ran)
			}
			landed := joined(b.joinBridge)

			if !slices.Equal(landed, tc.landed) {
				t.Errorf("the bridge landed %v, want %v", landed, tc.landed)
			}
			if got := runner.foldCalls(); !slices.Equal(got, tc.announced) {
				t.Errorf("the Runner was handed compaction-finished %+v, want %+v", got, tc.announced)
			}
		})
	}
}

// TestBriefing_EachRunRunsUnderItsEntrysTimeoutElseTheAdviseDefault — a run's deadline is its
// entry's Timeout counted from the event that started it, or domain.DefaultAdviseTimeout for an
// entry that set none, on both lanes.
func TestBriefing_EachRunRunsUnderItsEntrysTimeoutElseTheAdviseDefault(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		moment domain.Moment
		start  func(b *sessionBriefing, entries []domain.Reaction)
		join   func(b *sessionBriefing) func(context.Context, adviceLanding)
	}{
		{
			name:   "session-start lane",
			moment: domain.MomentSessionStarted,
			start: func(b *sessionBriefing, entries []domain.Reaction) {
				b.boundaryArmed(domain.SessionStartNew, 0, sessionArmed{advise: entries})
			},
			join: func(b *sessionBriefing) func(context.Context, adviceLanding) { return b.joinOpening },
		},
		{
			name:   "compaction lane",
			moment: domain.MomentCompactionFinished,
			start: func(b *sessionBriefing, entries []domain.Reaction) {
				b.foldEnded(b.foldBegan(domain.CompactionFoldManual, 0, entries, nil), true)
			},
			join: func(b *sessionBriefing) func(context.Context, adviceLanding) { return b.joinBridge },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			handler := &briefingHandler{}
			b := newBriefing(t, handler)
			own := 3 * time.Second
			entries := briefingEntries(tc.moment, "own", "default")
			entries[0].Timeout = own
			want := map[string]time.Duration{"own": own, "default": domain.DefaultAdviseTimeout}

			before := time.Now()
			tc.start(b, entries)
			after := time.Now()
			joined(tc.join(b)) // waits for every run, so each call is recorded

			calls := handler.recorded()
			if len(calls) != len(want) {
				t.Fatalf("the handler took %d calls, want %d", len(calls), len(want))
			}
			for _, c := range calls {
				if c.deadline.Before(before.Add(want[c.id])) || c.deadline.After(after.Add(want[c.id])) {
					t.Errorf("%s ran under a deadline %s from its start, want %s",
						c.id, c.deadline.Sub(before).Round(time.Millisecond), want[c.id])
				}
			}
		})
	}
}

// TestSessionAdvise_AUserEntryTakesItsOwnTimeoutElseTheAdviseDefault — an entry's `timeout:` is its
// run's deadline, and an entry that set none runs under domain.DefaultAdviseTimeout, for a command
// and a webhook alike.
func TestSessionAdvise_AUserEntryTakesItsOwnTimeoutElseTheAdviseDefault(t *testing.T) {
	t.Parallel()

	server := replyWith(t, new(atomic.Int64), http.StatusNoContent, "")
	own := 3 * time.Second
	argvOwn := userAdvise("argv-own", sessionStarted, "/bin/sh", "-c", "true")
	argvOwn.Timeout = own
	hookOwn := userAdviseWebhook("hook-own", sessionStarted, domain.WebhookHandler{URL: server.URL})
	hookOwn.Timeout = own
	a := sessionAdviseAgent(t, baseConfig(&recordingSink{}), echoResponder(t, "ok"),
		userAdvise("argv-default", sessionStarted, "/bin/sh", "-c", "true"),
		argvOwn,
		userAdviseWebhook("hook-default", sessionStarted, domain.WebhookHandler{URL: server.URL}),
		hookOwn,
	)

	want := map[string]time.Duration{
		"argv-default": domain.DefaultAdviseTimeout,
		"argv-own":     own,
		"hook-default": domain.DefaultAdviseTimeout,
		"hook-own":     own,
	}
	landed := map[string]time.Duration{}
	a.briefing.joinOpening(context.Background(), func(run *sessionAdviseRun) bool {
		landed[run.spec.ID] = run.timeout
		return answeredRun(run)
	})

	if !maps.Equal(landed, want) {
		t.Errorf("the opening landed runs under %v, want %v", landed, want)
	}
}

// awaitBoundaryRunDone waits, under a testtime budget, for the one run a's boundary started to be
// done — answered or failed — so an opening joined under a cancelled context still lands it.
func awaitBoundaryRunDone(t *testing.T, a *Agent, what string) {
	t.Helper()
	testtime.Eventually(t, testtime.Budget(5*time.Second), func() bool {
		runs := a.briefing.lane.waiting()
		if len(runs) != 1 {
			return false
		}
		select {
		case <-runs[0].done:
			return true
		default:
			return false
		}
	}, what)
}

// TestSessionAdvise_AnAbortedFirstExchangeKeepsTheAdvice — an Exchange whose opening took the advice
// and was then aborted puts it back, so the next opening carries it.
func TestSessionAdvise_AnAbortedFirstExchangeKeepsTheAdvice(t *testing.T) {
	t.Parallel()

	probe := answering(sessionAdviceText)
	responder := echoResponder(t, "ok")
	a := sessionAdviseAgent(t, baseConfig(&recordingSink{}), responder, probe.reaction("ci", 0))
	awaitBoundaryRunDone(t, a, "the background run to answer")
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
	awaitBoundaryRunDone(t, a, "the background run to fail")

	abortAfterTheJoin(t, a, "aborted prompt")
	if landed := joined(a.briefing.joinOpening); len(landed) != 0 {
		t.Fatalf("the abort put back %v, want the reported run dropped", landed)
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

// briefingRaceRounds is how many times TestBriefing_AnInputRacingABoundarySeesItWhole races an input
// against a boundary: enough for the input to land between a boundary's lane replace and its
// window opening, were the two not one critical section.
const briefingRaceRounds = 2000

// briefingAdviseEntry is an advise entry on session-started, the shape a boundary or a live swap
// hands the briefing.
func briefingAdviseEntry(id string) domain.Reaction {
	return domain.Reaction{ID: id, Class: domain.ClassAdvise, On: []domain.Moment{domain.MomentSessionStarted}}
}

// silentCall is a handler call that answers nothing at once: the race test's runs only need to
// exist, not to say anything.
func silentCall(context.Context, int, domain.SeamPayload, domain.Reaction) (domain.Outcome, error) {
	return domain.Outcome{}, nil
}

// TestBriefing_AnInputRacingABoundarySeesItWhole — a boundary's lane replace and its window opening
// are one critical section, so an input racing the boundary sees it wholly before or wholly after.
// Each input records what it saw — the lane's pending runs, read as it arrives — and an input that
// saw the boundary's runs arrived after the boundary began, so it must leave the window shut: a
// live swap afterwards starts nothing. The window's state is read through liveArmed, the module's
// own door.
func TestBriefing_AnInputRacingABoundarySeesItWhole(t *testing.T) {
	t.Parallel()

	for round := range briefingRaceRounds {
		briefing := &sessionBriefing{call: silentCall}
		boundary := sessionArmed{advise: []domain.Reaction{briefingAdviseEntry("ci")}}
		var saw []*sessionAdviseRun
		var group sync.WaitGroup
		group.Add(2)
		go func() {
			defer group.Done()
			briefing.boundaryArmed(domain.SessionStartNew, 0, boundary)
		}()
		go func() {
			defer group.Done()
			saw = briefing.lane.waiting()
			briefing.inputArrived()
		}()
		group.Wait()

		before := len(briefing.lane.waiting())
		briefing.liveArmed(sessionArmed{advise: []domain.Reaction{briefingAdviseEntry("late")}})
		launched := len(briefing.lane.waiting()) - before
		briefing.close()

		if len(saw) > 0 && launched > 0 {
			t.Fatalf("round %d: the input saw the boundary's %d runs yet left the window open", round, len(saw))
		}
	}
}
