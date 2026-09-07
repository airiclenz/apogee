package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// ----------------------------------------------------------------------------
// Per-delegation lifecycle phases (domain.SubAgentPhaseEvent)
// ----------------------------------------------------------------------------
//
// A delegation group's ToolResultEvents deliberately burst together after the whole group joins,
// in emitted-call order (ADR 0039 decision 4), so they carry no per-child timing at all. These
// tests pin the timing the phase events add ON TOP of that burst without disturbing it: which
// child is running, which has finished, and with what result — while the history stays exactly as
// deterministic as it was.

// subAgentPhases returns every lifecycle phase event the sink saw, in emission order.
func subAgentPhases(events []domain.Event) []domain.SubAgentPhaseEvent {
	var out []domain.SubAgentPhaseEvent
	for _, e := range events {
		if pe, ok := e.(domain.SubAgentPhaseEvent); ok {
			out = append(out, pe)
		}
	}
	return out
}

// assertCancelledBracket pins ADR 0075 decision 12 for every named delegation: the child that was
// CANCELLED reports exactly one started and one finished phase, the finished one flagged Cancelled
// and carrying no result, stamped with the same run identity its started carried. A bracket a
// Driver cannot see close is a bracket left open forever in its log, which is the whole point of
// emitting a phase for a delegation the parent Turn is about to roll back.
func assertCancelledBracket(t *testing.T, events []domain.Event, callIDs ...string) {
	t.Helper()
	for _, id := range callIDs {
		var started, finished []domain.SubAgentPhaseEvent
		for _, pe := range subAgentPhases(events) {
			if pe.CallID != id {
				continue
			}
			if pe.Phase == domain.SubAgentStarted {
				started = append(started, pe)
				continue
			}
			finished = append(finished, pe)
		}
		if len(started) != 1 || len(finished) != 1 {
			t.Errorf("call %s: %d started / %d finished phases, want exactly one of each", id, len(started), len(finished))
			continue
		}
		if started[0].Cancelled {
			t.Errorf("call %s: started phase reported Cancelled; only a finished phase ever is", id)
		}
		if finished[0].Phase != domain.SubAgentFinished {
			t.Errorf("call %s: closing phase = %q, want %q", id, finished[0].Phase, domain.SubAgentFinished)
		}
		if !finished[0].Cancelled {
			t.Errorf("call %s: finished phase of a cancelled delegation was not flagged Cancelled", id)
		}
		if finished[0].Result != (domain.ToolResult{}) {
			t.Errorf("call %s: cancelled finished carried a result (%+v); a rolled-back delegation reports none", id, finished[0].Result)
		}
		if finished[0].Depth != started[0].Depth {
			t.Errorf("call %s: finished Depth = %d, want %d (its own started phase's child identity)", id, finished[0].Depth, started[0].Depth)
		}
	}
}

// phaseTripwireSink is a recording sink that also RELEASES a waiting child when a named
// delegation finishes. It is how these tests pin a completion ORDER instead of hoping for one:
// the child that waits on done cannot finish before the watched sibling has.
//
// It needs no lock of its own — the engine funnels every emitter through one serializing seam —
// and it tolerates a duplicate finished phase rather than panicking on a second close, so a
// double-emit bug fails as an assertion instead of as a dead test binary.
type phaseTripwireSink struct {
	events []domain.Event
	watch  string
	done   chan struct{}
	closed bool
}

func newPhaseTripwireSink(watch string) *phaseTripwireSink {
	return &phaseTripwireSink{watch: watch, done: make(chan struct{})}
}

func (s *phaseTripwireSink) Emit(e domain.Event) {
	s.events = append(s.events, e)
	pe, ok := e.(domain.SubAgentPhaseEvent)
	if !ok || pe.Phase != domain.SubAgentFinished || pe.CallID != s.watch || s.closed {
		return
	}
	s.closed = true
	close(s.done)
}

// waitFor blocks until ch closes, giving up after wait so a regression fails the assertions
// instead of hanging the suite.
func waitFor(ctx context.Context, ch <-chan struct{}, wait time.Duration) {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ch:
	case <-ctx.Done():
	case <-timer.C:
	}
}

// TestDelegationPhases_BracketEveryChildInAFanOut is the item's whole claim in one run: three
// delegations under a cap of two report started/finished per child, never more than two children
// are started-but-unfinished at once, every phase lands between the up-front ToolCallEvent burst
// and the trailing ToolResultEvent burst, and the results still commit in EMITTED order even
// though the tripwire forces the second child to complete before the first.
func TestDelegationPhases_BracketEveryChildInAFanOut(t *testing.T) {
	sink := newPhaseTripwireSink("c2")
	probe := newConcurrencyProbe(2, 3*time.Second)
	up := newRoutedResponder().
		route("delegate two things", nil, fanOutScript(
			[2]string{"c1", "task one"}, [2]string{"c2", "task two"}, [2]string{"c3", "task three"})).
		route("task one", func(ctx context.Context) {
			probe.enter(ctx)
			waitFor(ctx, sink.done, 3*time.Second)
		}, contentScript("child one done")).
		route("task two", probe.enter, contentScript("child two done")).
		route("task three", nil, contentScript("child three done")).
		route("delegate two things", nil, contentScript("parent done"))

	a := fanOutAgent(t, sink, 2, up)
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if peak := probe.peakInFlight(); peak != 2 {
		t.Fatalf("peak children in flight = %d, want 2 (nothing concurrent was exercised)", peak)
	}

	phases := subAgentPhases(sink.events)
	if len(phases) != 6 {
		t.Fatalf("phase events = %d, want 6 (a started and a finished per delegation): %+v", len(phases), phases)
	}

	// Per child: exactly one started, then exactly one finished. The counter that runs alongside
	// is the cap's proof — a third child can only start once a slot was freed by a finish.
	seen := map[string]domain.SubAgentPhase{}
	live, peakLive := 0, 0
	for _, p := range phases {
		if p.Depth != 1 {
			t.Errorf("phase %+v carried Depth %d, want the child's depth 1", p, p.Depth)
		}
		switch p.Phase {
		case domain.SubAgentStarted:
			if _, dup := seen[p.CallID]; dup {
				t.Fatalf("%s started twice (phases: %+v)", p.CallID, phases)
			}
			live++
			if live > peakLive {
				peakLive = live
			}
		case domain.SubAgentFinished:
			if seen[p.CallID] != domain.SubAgentStarted {
				t.Fatalf("%s finished without a preceding started (phases: %+v)", p.CallID, phases)
			}
			live--
		default:
			t.Fatalf("unknown phase %q on %+v", p.Phase, p)
		}
		seen[p.CallID] = p.Phase
	}
	if peakLive > 2 {
		t.Errorf("%d children were started-but-unfinished at once, want at most the cap of 2", peakLive)
	}
	for _, id := range []string{"c1", "c2", "c3"} {
		if seen[id] != domain.SubAgentFinished {
			t.Errorf("delegation %s ended on phase %q, want it finished", id, seen[id])
		}
	}

	// The tripwire pins the inversion: c2's child completed first, and the commit order ignored it.
	var finishOrder []string
	for _, p := range phases {
		if p.Phase == domain.SubAgentFinished {
			finishOrder = append(finishOrder, p.CallID)
		}
	}
	if finishOrder[0] != "c2" {
		t.Fatalf("finish order = %v, want c2 first — the completion inversion never happened", finishOrder)
	}
	results := subAgentResults(sink.events)
	if len(results) != 3 || results[0].CallID != "c1" || results[1].CallID != "c2" || results[2].CallID != "c3" {
		t.Errorf("results committed as %+v, want the emitted order c1,c2,c3", results)
	}

	assertPhasesSitBetweenTheBursts(t, sink.events)
}

// assertPhasesSitBetweenTheBursts pins the additive claim: the phase events changed neither burst.
// Every depth-0 sub_agent ToolCallEvent still precedes the first phase, and every depth-0
// ToolResultEvent still trails the last one.
func assertPhasesSitBetweenTheBursts(t *testing.T, events []domain.Event) {
	t.Helper()
	firstPhase, lastPhase := -1, -1
	for i, e := range events {
		if _, ok := e.(domain.SubAgentPhaseEvent); !ok {
			continue
		}
		if firstPhase < 0 {
			firstPhase = i
		}
		lastPhase = i
	}
	if firstPhase < 0 {
		t.Fatal("no phase events emitted")
	}
	for i, e := range events {
		switch ev := e.(type) {
		case domain.ToolCallEvent:
			if ev.Depth == 0 && i > firstPhase {
				t.Errorf("tool call %q was emitted after the first phase event; the up-front call burst moved", ev.Call.ID)
			}
		case domain.ToolResultEvent:
			if ev.Depth == 0 && i < lastPhase {
				t.Errorf("tool result %q was emitted before the last phase event; the trailing result burst moved", ev.Result.CallID)
			}
		}
	}
}

// TestDelegationPhases_SerialDelegationEmitsThePair pins the lone-run case: a single delegation
// never reaches the pool, and it must still report started and finished — a run that is executing
// may never look queued just because nothing else was delegated with it.
func TestDelegationPhases_SerialDelegationEmitsThePair(t *testing.T) {
	sink := &recordingSink{}
	responder := &scriptedResponder{scripts: [][]provider.Delta{
		subAgentCallScript("c1", "summarise the repo"),
		contentScript("the repo is a Go TUI agent"),
		contentScript("done — delegated and summarised"),
	}}
	a, err := newAgent(subAgentConfig(sink, domain.ModeAskBefore), responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "please research"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	phases := subAgentPhases(sink.events)
	if len(phases) != 2 {
		t.Fatalf("phase events = %d, want the started/finished pair: %+v", len(phases), phases)
	}
	if phases[0].Phase != domain.SubAgentStarted || phases[1].Phase != domain.SubAgentFinished {
		t.Fatalf("phases = %q,%q, want started then finished", phases[0].Phase, phases[1].Phase)
	}
	for _, p := range phases {
		if p.CallID != "c1" || p.Depth != 1 {
			t.Errorf("phase %+v, want the child's identity (CallID c1 at depth 1)", p)
		}
	}
	if phases[0].Result.Content != "" || phases[0].Result.CallID != "" {
		t.Errorf("started carried a result %+v, want the zero value", phases[0].Result)
	}
	if !strings.Contains(phases[1].Result.Content, "Go TUI agent") {
		t.Errorf("finished carried %q, want the child's report", phases[1].Result.Content)
	}
}

// TestDelegationPhases_FailedChildFinishesWithItsErrorResult pins the payload on the path that
// matters most to a watching human: a child that blew up still finishes, and its finished phase
// carries the error result the parent will commit — so the failure is readable the moment it
// happens rather than after the sibling finally joins.
func TestDelegationPhases_FailedChildFinishesWithItsErrorResult(t *testing.T) {
	sink := &recordingSink{}
	probe := newConcurrencyProbe(2, 3*time.Second)
	up := newRoutedResponder().
		route("delegate two things", nil, fanOutScript([2]string{"c1", "doomed task"}, [2]string{"c2", "healthy task"})).
		route("doomed task", probe.enter, []provider.Delta{{Kind: provider.DeltaError, Err: "upstream exploded"}}).
		route("healthy task", probe.enter, contentScript("healthy child done")).
		route("delegate two things", nil, contentScript("parent done"))

	a := fanOutAgent(t, sink, 2, up)
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	finished := map[string]domain.ToolResult{}
	for _, p := range subAgentPhases(sink.events) {
		if p.Phase == domain.SubAgentFinished {
			finished[p.CallID] = p.Result
		}
	}
	if len(finished) != 2 {
		t.Fatalf("finished phases = %d, want one per delegation: %+v", len(finished), finished)
	}
	if !finished["c1"].IsError {
		t.Errorf("failed child's finished phase carried %+v, want an error result", finished["c1"])
	}
	if finished["c2"].IsError || !strings.Contains(finished["c2"].Content, "healthy child done") {
		t.Errorf("healthy child's finished phase carried %+v, want its report intact", finished["c2"])
	}
}
