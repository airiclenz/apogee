package agent

import (
	"context"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// The child bracket (childBracket) alone: the phase pair, the recover boundary, the ending decided
// once and the one ledger row, over a host Agent that emits the phases and a real Agent standing
// in for the child wherever a body runs one.

// bracketCallID is the call every bracket test's delegation answers.
const bracketCallID = "call-1"

// bracketPanicContent is the result a bracket test's onPanic answers a panicked delegation with.
const bracketPanicContent = "tool \"sub_agent\" panicked"

// bracketCall is the sub_agent call every bracket test's delegation answers.
func bracketCall() domain.ToolCall {
	return domain.ToolCall{ID: bracketCallID, Tool: "sub_agent", Arguments: []byte(`{"task":"survey the module"}`)}
}

// bracketRequest is bracketCall's arguments as a dispatch slot parses them for the bracket.
func bracketRequest() delegationRequest {
	return parseDelegationRequest(bracketCall(), delegationOffer{})
}

// newBracketHost builds the Agent that opens a test's bracket, emitting into sink.
func newBracketHost(t *testing.T, sink domain.EventSink) *Agent {
	t.Helper()
	host, err := newAgent(baseConfig(sink), &stopResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	t.Cleanup(func() { _ = host.Close() })
	return host
}

// newBracketChild builds the Agent a bracket test's body runs as its child, over responder,
// holding a task and emitting into sink.
func newBracketChild(t *testing.T, sink domain.EventSink, responder provider.Responder) *Agent {
	t.Helper()
	sub, err := newAgent(baseConfig(sink), responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	t.Cleanup(func() { _ = sub.Close() })
	if err := sub.Submit(domain.UserInput{Text: "survey the module"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	return sub
}

// panicResult is the onPanic every bracket test passes: the path's own panicked result.
func panicResult(any) domain.ToolResult {
	return errorToolResult(bracketCallID, bracketPanicContent)
}

// bracketPhases is every delegation phase the sink saw, in order.
func bracketPhases(sink *recordingSink) []domain.SubAgentPhaseEvent {
	var phases []domain.SubAgentPhaseEvent
	for _, e := range sink.events {
		if ev, ok := e.(domain.SubAgentPhaseEvent); ok {
			phases = append(phases, ev)
		}
	}
	return phases
}

// assertThePair fails unless the sink saw exactly a started then a finished phase, both under
// runID, the finished one carrying result.
func assertThePair(t *testing.T, sink *recordingSink, runID string, result domain.ToolResult) {
	t.Helper()
	phases := bracketPhases(sink)
	if len(phases) != 2 || phases[0].Phase != domain.SubAgentStarted || phases[1].Phase != domain.SubAgentFinished {
		t.Fatalf("phases = %+v, want started then finished", phases)
	}
	if runID == "" || phases[0].RunID != runID || phases[1].RunID != runID {
		t.Errorf("phase run ids = %q, %q, want both %q (non-empty)", phases[0].RunID, phases[1].RunID, runID)
	}
	if phases[1].Result != result {
		t.Errorf("finished phase result = %+v, want %+v", phases[1].Result, result)
	}
}

// assertOneRow fails unless the host's ledger holds exactly one row for the bracket's call,
// booked as outcome with cause.
func assertOneRow(t *testing.T, host *Agent, outcome delegationOutcome, cause string) {
	t.Helper()
	rows := host.delegations.rows()
	if len(rows) != 1 {
		t.Fatalf("ledger rows = %+v, want one", rows)
	}
	if rows[0].callID != bracketCallID || rows[0].outcome != outcome || rows[0].cause != cause {
		t.Errorf("row = %+v, want call %q, outcome %q, cause %q", rows[0], bracketCallID, outcome, cause)
	}
}

func TestChildBracket_PanicBeforeRunBooksRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       func() (domain.ToolResult, childEnding)
		wantResult domain.ToolResult
		wantPanic  bool
	}{
		{
			name:       "a panic before the child's run",
			body:       func() (domain.ToolResult, childEnding) { panic("boom") },
			wantResult: errorToolResult(bracketCallID, bracketPanicContent),
			wantPanic:  true,
		},
		{
			name: "a fault returned before the child's run",
			body: func() (domain.ToolResult, childEnding) {
				return errorToolResult(bracketCallID, "could not start sub-agent: closed"), childEnding{kind: delegationFaulted, hasStarted: true}
			},
			wantResult: errorToolResult(bracketCallID, "could not start sub-agent: closed"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sink := &recordingSink{}
			host := newBracketHost(t, sink)
			var runID string

			result, ending := host.openBracket(1, bracketCall(), bracketRequest(), &runID, &host.delegations).run(domain.SubAgentPhaseEvent{}, panicResult, tt.body)

			if result != tt.wantResult || ending != refusedEnding() {
				t.Errorf("run = %+v, %+v; want %+v, refused", result, ending, tt.wantResult)
			}
			assertThePair(t, sink, runID, tt.wantResult)
			assertOneRow(t, host, delegationRefused, delegationCause(tt.wantResult.Content))
			if hasError := hasErrorEvent(sink); hasError != tt.wantPanic {
				t.Errorf("ErrorEvent emitted %v, want %v", hasError, tt.wantPanic)
			}
		})
	}
}

// hasErrorEvent reports whether the sink saw an ErrorEvent.
func hasErrorEvent(sink *recordingSink) bool {
	for _, e := range sink.events {
		if _, ok := e.(domain.ErrorEvent); ok {
			return true
		}
	}
	return false
}

func TestChildBracket_PanicAfterRunBooksFaulted(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	host := newBracketHost(t, sink)
	sub := newBracketChild(t, &recordingSink{}, &stopResponder{scripts: [][]provider.Delta{contentScript("all done")}})
	var (
		registry childRegistry
		runID    string
	)
	b := host.openBracket(1, bracketCall(), bracketRequest(), &runID, &host.delegations)

	result, ending := b.run(domain.SubAgentPhaseEvent{}, panicResult, func() (domain.ToolResult, childEnding) {
		_, _, _, _ = b.runChild(context.Background(), childRun{registry: &registry, runID: runID, sub: sub})
		panic("rendering the result")
	})

	want := errorToolResult(bracketCallID, bracketPanicContent)
	if result != want || ending != (childEnding{kind: delegationFaulted, hasStarted: true}) {
		t.Errorf("run = %+v, %+v; want %+v, a started fault", result, ending, want)
	}
	assertThePair(t, sink, runID, want)
	assertOneRow(t, host, delegationFaulted, delegationCause(bracketPanicContent))
}

func TestChildBracket_CancelledBodyClosesThePair(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	host := newBracketHost(t, sink)
	sub := newBracketChild(t, &recordingSink{}, &stopResponder{scripts: [][]provider.Delta{contentScript("all done")}})
	var (
		registry childRegistry
		runID    string
	)
	b := host.openBracket(1, bracketCall(), bracketRequest(), &runID, &host.delegations)
	answer := errorToolResult(bracketCallID, cancelledQueuedDelegationContent)

	result, ending := b.run(domain.SubAgentPhaseEvent{}, panicResult, func() (domain.ToolResult, childEnding) {
		_, _, _, _ = b.runChild(context.Background(), childRun{registry: &registry, runID: runID, sub: sub})
		return answer, runEnding(domain.StepResult{Status: domain.StatusCancelled}, nil, false)
	})

	if result != answer || ending != (childEnding{kind: delegationCancelled, hasStarted: true}) {
		t.Errorf("run = %+v, %+v; want %+v, a started cancel", result, ending, answer)
	}
	assertThePair(t, sink, runID, answer)
	assertOneRow(t, host, delegationCancelled, "")
}

func TestChildBracket_CancelledBeforeRunBooksCancelled(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	host := newBracketHost(t, sink)
	var runID string
	answer := errorToolResult(bracketCallID, cancelledQueuedDelegationContent)

	result, ending := host.openBracket(1, bracketCall(), bracketRequest(), &runID, &host.delegations).run(domain.SubAgentPhaseEvent{}, panicResult, func() (domain.ToolResult, childEnding) {
		return answer, childEnding{kind: delegationCancelled}
	})

	if result != answer || ending != unstartedCancelled() {
		t.Errorf("run = %+v, %+v; want %+v, the unstarted cancel", result, ending, answer)
	}
	assertThePair(t, sink, runID, answer)
	assertOneRow(t, host, delegationCancelled, "")
}

func TestChildBracket_AStopWhoseFoldPanicsBooksStopped(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	host := newBracketHost(t, sink)
	responder := &stopResponder{block: map[int]bool{0: true}}
	sub := newBracketChild(t, &recordingSink{}, responder)
	var (
		registry childRegistry
		runID    string
	)
	responder.before = func(int) {
		if !registry.stop(runID) {
			t.Error("stop found no armed run while the child ran")
		}
	}
	b := host.openBracket(1, bracketCall(), bracketRequest(), &runID, &host.delegations)

	result, ending := b.run(domain.SubAgentPhaseEvent{}, panicResult, func() (domain.ToolResult, childEnding) {
		_, _, _, _ = b.runChild(context.Background(), childRun{
			registry:    &registry,
			runID:       runID,
			sub:         sub,
			foldStopped: func(context.Context) { panic("folding the stopped child") },
		})
		return domain.ToolResult{}, childEnding{}
	})

	want := errorToolResult(bracketCallID, bracketPanicContent)
	if result != want || ending != runEnding(domain.StepResult{}, nil, true) {
		t.Errorf("run = %+v, %+v; want %+v, stopped", result, ending, want)
	}
	assertThePair(t, sink, runID, want)
	assertOneRow(t, host, delegationStopped, "")
}

func TestChildBracket_NeverStartedBooksOneRowAndALoneFinish(t *testing.T) {
	t.Parallel()

	ceiling := fanOutCeilingResult(bracketCallID, 5, 1, 2, false)
	tests := []struct {
		name        string
		unstarted   childEnding
		wantOutcome delegationOutcome
		wantCause   string
	}{
		{name: "pre-empted by a queued message", unstarted: unstartedSkipped(), wantOutcome: delegationSkipped},
		{name: "refused past the fan-out ceiling", unstarted: unstartedPastCeiling(ceiling), wantOutcome: delegationRefused, wantCause: delegationCause(ceiling.Content)},
		{name: "reached by the turn's cancel", unstarted: unstartedCancelled(), wantOutcome: delegationCancelled},
		{name: "stopped while queued", unstarted: unstartedStopped(), wantOutcome: delegationStopped},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sink := &recordingSink{}
			host := newBracketHost(t, sink)
			var runID string

			result := host.openBracket(1, bracketCall(), bracketRequest(), &runID, &host.delegations).neverStarted(tt.unstarted)

			if want := errorToolResult(bracketCallID, tt.unstarted.content); result != want {
				t.Errorf("neverStarted = %+v, want %+v", result, want)
			}
			phases := bracketPhases(sink)
			if len(phases) != 1 || phases[0].Phase != domain.SubAgentFinished || phases[0].Result != result {
				t.Errorf("phases = %+v, want one finished phase carrying the result", phases)
			}
			if runID != "" {
				t.Errorf("run id = %q, want none minted for a delegation that never started", runID)
			}
			assertOneRow(t, host, tt.wantOutcome, tt.wantCause)
		})
	}
}

func TestChildBracket_OpensOnTheReservedIndexAndMintsOnlyOnTheRunPath(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	host := newBracketHost(t, sink)
	host.delegations.reserve("earlier")
	host.delegations.reserve(bracketCallID)
	host.delegations.reserve("later")
	var runID string

	b := host.openBracket(1, bracketCall(), bracketRequest(), &runID, &host.delegations)
	minted := b.mintRunID()
	b.run(domain.SubAgentPhaseEvent{}, panicResult, func() (domain.ToolResult, childEnding) {
		return domain.ToolResult{CallID: bracketCallID, Content: "done"}, childEnding{kind: delegationCompleted, hasStarted: true}
	})

	if rows := host.delegations.rows(); len(rows) != 1 || rows[0].spawnIndex != 2 {
		t.Errorf("rows = %+v, want one row on the index reserved for the call (2)", rows)
	}
	if minted == "" || runID != minted {
		t.Errorf("slot run id = %q, minted %q; want the minted id written back", runID, minted)
	}
	assertThePair(t, sink, minted, domain.ToolResult{CallID: bracketCallID, Content: "done"})
}

// orderSink records every event and, at the finished phase, how many ledger rows and undelivered
// reports had already landed.
type orderSink struct {
	recordingSink
	ledger              *delegationLedger
	rowsAtFinish        int
	undeliveredAtFinish int
}

func (s *orderSink) Emit(e domain.Event) {
	if ev, ok := e.(domain.SubAgentPhaseEvent); ok && ev.Phase == domain.SubAgentFinished {
		s.rowsAtFinish = len(s.ledger.rows())
		s.undeliveredAtFinish = undeliveredCount(&s.recordingSink)
	}
	s.recordingSink.Emit(e)
}

func TestChildBracket_RowAndUndeliveredPrecedeFinished(t *testing.T) {
	t.Parallel()

	sink := &orderSink{}
	host := newBracketHost(t, sink)
	sink.ledger = &host.delegations
	sub := newBracketChild(t, sink, &stopResponder{scripts: [][]provider.Delta{contentScript("all done")}})
	var (
		registry childRegistry
		runID    string
	)
	b := host.openBracket(1, bracketCall(), bracketRequest(), &runID, &host.delegations)

	_, ending := b.run(domain.SubAgentPhaseEvent{}, panicResult, func() (domain.ToolResult, childEnding) {
		defer b.reap(&registry, sub, nil)
		res, stopped, _, err := b.runChild(context.Background(), childRun{registry: &registry, runID: runID, sub: sub})
		if !sub.mailbox.add(domain.UserInput{Text: childRunNote}, false) {
			t.Error("the finished child's mailbox refused a message before its reap")
		}
		return domain.ToolResult{CallID: bracketCallID, Content: "all done"}, runEnding(res, err, stopped)
	})

	if ending.kind != delegationCompleted {
		t.Fatalf("ending = %+v, want completed", ending)
	}
	if sink.rowsAtFinish != 1 || sink.undeliveredAtFinish != 1 {
		t.Errorf("at the finished phase: %d rows, %d undelivered; want the row and the one leftover first", sink.rowsAtFinish, sink.undeliveredAtFinish)
	}
	for _, e := range sink.events {
		if ev, ok := e.(domain.ChildInterjectionEvent); ok && ev.Reason != domain.UndeliveredCompleted {
			t.Errorf("undelivered reason = %q, want the ending's (%q)", ev.Reason, domain.UndeliveredCompleted)
		}
	}
}

func TestChildBracket_NilLedgerBooksNothing(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	host := newBracketHost(t, sink)
	var runID string
	answer := errorToolResult(bracketCallID, "could not start sub-agent: closed")

	runResult, runEnded := host.openBracket(1, bracketCall(), bracketRequest(), &runID, nil).run(domain.SubAgentPhaseEvent{}, panicResult, func() (domain.ToolResult, childEnding) {
		return answer, refusedEnding()
	})
	host.openBracket(1, bracketCall(), bracketRequest(), &runID, nil).neverStarted(unstartedSkipped())

	if runResult != answer || runEnded != refusedEnding() {
		t.Errorf("run = %+v, %+v; want %+v, refused", runResult, runEnded, answer)
	}
	if rows := host.delegations.rows(); len(rows) != 0 {
		t.Errorf("rows = %+v, want none booked without a ledger", rows)
	}
	if phases := bracketPhases(sink); len(phases) != 3 {
		t.Errorf("phases = %+v, want the run's pair and the never-started finish", phases)
	}
}
