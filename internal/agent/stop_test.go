package agent

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/floor"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/tools"
)

// stopResponder is requestLogResponder for the stop path (ADR 0086 D4): call N plays scripts[N],
// except a call listed in block, which BLOCKS until its ctx is cancelled and then surfaces the
// cancellation as a terminal stream error — the fake for a stop landing mid-request. before runs
// immediately before call N's stream is produced, on the goroutine making the call, which is where
// a test issues its stop. One responder serves the whole tree; the delegations here run one at a
// time, so the calls are sequential.
type stopResponder struct {
	scripts  [][]provider.Delta
	block    map[int]bool
	before   func(call int)
	requests []provider.Request
	calls    int
}

func (r *stopResponder) Stream(ctx context.Context, req provider.Request) iter.Seq[provider.Delta] {
	r.requests = append(r.requests, req)
	i := r.calls
	r.calls++
	if r.before != nil {
		r.before(i)
	}
	return func(yield func(provider.Delta) bool) {
		if r.block[i] {
			<-ctx.Done()
			yield(provider.Delta{Kind: provider.DeltaError, Err: ctx.Err().Error()})
			return
		}
		if i >= len(r.scripts) || r.scripts[i] == nil {
			yield(provider.Delta{Kind: provider.DeltaError, Err: "stopResponder: out of scripts"})
			return
		}
		for _, d := range r.scripts[i] {
			if !yield(d) {
				return
			}
		}
	}
}

// runningChildRunID returns the run id of the one sub-agent a is running.
func runningChildRunID(t *testing.T, a *Agent) string {
	t.Helper()
	children := a.children.all()
	if len(children) != 1 {
		t.Fatalf("running children = %d, want exactly one", len(children))
	}
	return children[0].runID
}

// stoppedSurveyNote is the human's remark to the delegate, queued while it runs and never
// delivered because the stop lands first.
const stoppedSurveyNote = "also check the vendor directory"

// stoppedSurveyScripts is the upstream of one named delegation stopped mid-run: the spawning call
// (0), one working Turn (1), the second Turn's request — which the caller blocks and stops the
// child in (2) — then the engine fold of the stopped work (3). The parent's own requests follow.
func stoppedSurveyScripts(callID string) [][]provider.Delta {
	scripts := [][]provider.Delta{toolCallScript(callID, tools.SubAgentToolName, cappedSurveyArgs(retainedSurveyTask, retainedSurveyName))}
	scripts = append(scripts, cappedChildTurns(1)...)
	return append(scripts, nil, foldScript(700, 40))
}

// stoppedResult renders the exact result a stopped delegation hands its parent: the stop head, the
// engine summary and the closing text under the capped result's sub-heads, then the undelivered
// messages and the continue line.
func stoppedResult(fold, closing string, undelivered []string, name string) string {
	head := closingReportHead
	if floor.ClosingShapeOf(closing).IsNonReport() {
		head = closingNarrationHead
	}
	out := stoppedResultHead + "\n" + engineSummaryHead + "\n" + fold + "\n\n" + head + "\n" + closing
	for _, text := range undelivered {
		out += "\n" + undeliveredInterjectionHead + "\n" + text
	}
	return out + "\n" + fmt.Sprintf(continueLineFormat, name)
}

// runStoppedSurveyParent drives a parent whose one delegation is stopped at call 2 — after a
// message is queued for it — over scripts, stops it again at call extraStopAt (-1: never), checks
// the parent's Exchange went on and the finished run can no longer be stopped, and returns the
// parent, the responder and the sink.
func runStoppedSurveyParent(t *testing.T, scripts [][]provider.Delta, block map[int]bool, extraStopAt int) (*Agent, *stopResponder, *recordingSink) {
	t.Helper()
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, reader)
	responder := &stopResponder{scripts: scripts, block: block}
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	var runID string
	responder.before = func(call int) {
		switch call {
		case 2:
			runID = runningChildRunID(t, a)
			if err := a.InterjectChild(runID, domain.UserInput{Text: stoppedSurveyNote}); err != nil {
				t.Errorf("InterjectChild: %v", err)
			}
			if err := a.StopChild(runID); err != nil {
				t.Errorf("StopChild while the child runs = %v, want nil", err)
			}
		case extraStopAt:
			if err := a.StopChild(runID); err != nil {
				t.Errorf("second StopChild during the fold = %v, want nil", err)
			}
		}
	}
	if err := a.Submit(domain.UserInput{Text: "please research"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete || res.Faulted {
		t.Fatalf("parent result = %+v, want a clean exchange-complete: a stop is not a cancel", res)
	}
	if err := a.StopChild(runID); !errors.Is(err, domain.ErrNoSuchChild) {
		t.Errorf("StopChild on the finished run = %v, want ErrNoSuchChild", err)
	}
	return a, responder, sink
}

// TestStopChild_StopsOneDelegationAndTheTurnGoesOn pins the stop end to end: the child is folded,
// the parent's next request carries the non-error stopped result with the exact head, the fold,
// the closing text, the undelivered message and the continue line; nothing rolls back; the ledger
// records `stopped`; the queued message is reported undelivered for the reason `stopped`; and the
// run is retained as a capped one is.
func TestStopChild_StopsOneDelegationAndTheTurnGoesOn(t *testing.T) {
	scripts := append(stoppedSurveyScripts("c1"), contentScript("parent done"))
	a, responder, sink := runStoppedSurveyParent(t, scripts, map[int]bool{2: true}, -1)

	if len(responder.requests) != 5 {
		t.Fatalf("upstream calls = %d, want 5 (spawn, one child Turn, the stopped Turn, the fold, the parent's reply)", len(responder.requests))
	}
	if folds := engineFoldUsage(sink.events, 1); len(folds) != 1 {
		t.Errorf("engine fold usage events at depth 1 = %d, want 1: the stopped work is folded once", len(folds))
	}
	want := stoppedResult(childFoldSummary, "reading file 0", []string{stoppedSurveyNote}, retainedSurveyName)
	got, ok := subAgentResultFor(sink.events, "c1")
	if !ok {
		t.Fatal("no result answered the stopped delegation c1")
	}
	if got.IsError || got.Content != want {
		t.Errorf("stopped result = %+v, want a non-error result\n%s", got, want)
	}
	// Nothing rolled back: the parent's next request and its own conversation carry the result.
	if !requestCarriesToolResult(responder.requests[4], "c1", want) {
		t.Errorf("the parent's next request does not carry the stopped result: %+v", responder.requests[4].Messages)
	}
	if !conversationCarriesToolResult(a.conv.Messages(), "c1", want) {
		t.Error("the parent's conversation lost the stopped result: the Turn rolled back")
	}
	rows := a.delegations.rows()
	if len(rows) != 1 || rows[0].outcome != delegationStopped {
		t.Errorf("ledger rows = %+v, want one row recorded %q", rows, delegationStopped)
	}
	undelivered := childInterjections(sink.events)
	if len(undelivered) != 1 || undelivered[0].Landed || undelivered[0].Reason != domain.UndeliveredStopped ||
		undelivered[0].Input.Text != stoppedSurveyNote {
		t.Errorf("ChildInterjectionEvents = %+v, want the queued message undelivered for %q", undelivered, domain.UndeliveredStopped)
	}
	retained, ok := a.retained.lookup(retainedSurveyName)
	wantRetained := retainedDelegate{
		task:          retainedSurveyTask,
		name:          retainedSurveyName,
		tools:         tools.SubAgentRoster{Names: []string{"read_thing"}},
		outputPath:    "notes/survey.md",
		fold:          childFoldSummary,
		closingReport: "reading file 0",
		spawnCallID:   "c1",
	}
	if !ok || !reflect.DeepEqual(retained, wantRetained) {
		t.Errorf("retained delegate = %+v (found %v), want %+v", retained, ok, wantRetained)
	}
}

// TestStopChild_ASecondStopSkipsTheFold pins the second stop: landing while the stopped run's fold
// is in flight, it cuts the summary call and the result — and the retention — carry the
// unavailable marker in the fold's place.
func TestStopChild_ASecondStopSkipsTheFold(t *testing.T) {
	scripts := append(stoppedSurveyScripts("c1"), contentScript("parent done"))
	a, responder, sink := runStoppedSurveyParent(t, scripts, map[int]bool{2: true, 3: true}, 3)

	if len(responder.requests) != 5 {
		t.Fatalf("upstream calls = %d, want 5 (spawn, one child Turn, the stopped Turn, the cut fold, the parent's reply)", len(responder.requests))
	}
	if folds := engineFoldUsage(sink.events, 1); len(folds) != 0 {
		t.Errorf("engine fold usage events at depth 1 = %d, want none: the second stop cut the fold", len(folds))
	}
	marker := fmt.Sprintf(engineFoldUnavailableFormat, secondStopFoldCause)
	want := stoppedResult(marker, "reading file 0", []string{stoppedSurveyNote}, retainedSurveyName)
	if got, ok := subAgentResultFor(sink.events, "c1"); !ok || got.IsError || got.Content != want {
		t.Errorf("stopped result = %+v, want a non-error result\n%s", got, want)
	}
	if retained, ok := a.retained.lookup(retainedSurveyName); !ok || retained.fold != marker {
		t.Errorf("retained fold = %q (found %v), want the unavailable marker %q", retained.fold, ok, marker)
	}
}

// TestStopChild_AStoppedDelegationIsContinuable drives stop → continue → completion: the continued
// child opens on the retained task, the fold written at the stop and the new instructions.
func TestStopChild_AStoppedDelegationIsContinuable(t *testing.T) {
	scripts := stoppedSurveyScripts("c1")
	scripts = append(scripts,
		toolCallScript("c2", tools.SubAgentToolName, continueArgs(retainedSurveyName, continueInstructions, 0)),
		contentScript("the survey is now complete"),
		contentScript("parent done"))
	a, responder, sink := runStoppedSurveyParent(t, scripts, map[int]bool{2: true}, -1)

	// Call 5 is the continued child's opening request (4: the continue call).
	wantTask := retainedSurveyTask + "\n\n" + previousAttemptHead + "\n" + childFoldSummary + "\n\n" + continuationInstructionsHead + "\n" + continueInstructions
	if got := lastUserText(responder.requests[5]); got != wantTask {
		t.Errorf("the continued child opened on\n%s\nwant\n%s", got, wantTask)
	}
	if completed, ok := subAgentResultFor(sink.events, "c2"); !ok || completed.IsError || completed.Content != "the survey is now complete" {
		t.Errorf("the continued child's result = %+v, want its completed report", completed)
	}
	if names := a.retained.names(); len(names) != 0 {
		t.Errorf("retained names = %v after the continuation completed, want none", names)
	}
}

// TestStopChild_UnknownRunIDIsRefused is the floor: an id naming no running delegation — empty,
// unknown, or on an agent with no children — is refused and changes nothing.
func TestStopChild_UnknownRunIDIsRefused(t *testing.T) {
	a, err := newAgent(baseConfig(&recordingSink{}), scriptedResponder(t))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	for _, runID := range []string{"", "nope.1"} {
		if err := a.StopChild(runID); !errors.Is(err, domain.ErrNoSuchChild) {
			t.Errorf("StopChild(%q) = %v, want ErrNoSuchChild", runID, err)
		}
	}
}

// stopOnCancelNamer never answers; once its context is cancelled — which runSubAgent does after
// the child's Run returned and before the result is rendered — it stops the delegation it was
// naming and records what StopChild said.
type stopOnCancelNamer struct {
	a      **Agent
	mu     sync.Mutex
	stopAt error
	tried  bool
}

func (n *stopOnCancelNamer) NameDelegation(ctx context.Context, _ domain.DelegationNaming) (string, error) {
	<-ctx.Done()
	children := (*n.a).children.all()
	n.mu.Lock()
	defer n.mu.Unlock()
	n.tried = len(children) == 1
	if n.tried {
		n.stopAt = (*n.a).StopChild(children[0].runID)
	}
	return "", ctx.Err()
}

// TestStopChild_AfterTheRunReturnedLeavesTheReportStanding pins the regression guard: a stop that
// lands once the child's Run has returned a completed result — while the namer is joined — finds
// the stop handle withdrawn, is refused, and the completed report reaches the parent unchanged.
func TestStopChild_AfterTheRunReturnedLeavesTheReportStanding(t *testing.T) {
	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore)
	var a *Agent
	namer := &stopOnCancelNamer{a: &a}
	cfg.Namer = namer
	responder := &stopResponder{scripts: [][]provider.Delta{
		toolCallScript("c1", tools.SubAgentToolName, `{"task":"summarise the repo"}`),
		contentScript("the repo is a Go TUI agent"),
		contentScript("parent done"),
	}}
	var err error
	a, err = newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "please summarise"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	namer.mu.Lock()
	tried, stopAt := namer.tried, namer.stopAt
	namer.mu.Unlock()
	if !tried {
		t.Fatal("the namer found no registered child to stop after the run returned")
	}
	if !errors.Is(stopAt, domain.ErrNoSuchChild) {
		t.Errorf("StopChild after the run returned = %v, want ErrNoSuchChild", stopAt)
	}
	got, ok := subAgentResultFor(sink.events, "c1")
	if !ok || got.IsError || got.Content != "the repo is a Go TUI agent" {
		t.Errorf("result = %+v, want the completed report standing", got)
	}
	if rows := a.delegations.rows(); len(rows) != 1 || rows[0].outcome != delegationCompleted {
		t.Errorf("ledger rows = %+v, want one row recorded %q", rows, delegationCompleted)
	}
}

// TestUndeliveredReason_Stopped pins the mapping a stopped delegation's leftover messages are
// reported under.
func TestUndeliveredReason_Stopped(t *testing.T) {
	if got := undeliveredReason(delegationStopped); got != domain.UndeliveredStopped {
		t.Errorf("undeliveredReason(stopped) = %q, want %q", got, domain.UndeliveredStopped)
	}
}

// requestCarriesToolResult reports whether req holds a tool message answering callID that opens on
// content — the request tail may carry an engine note after it.
func requestCarriesToolResult(req provider.Request, callID, content string) bool {
	for _, m := range req.Messages {
		if m.Role == string(domain.RoleTool) && m.ToolCallID == callID && strings.HasPrefix(m.Content, content) {
			return true
		}
	}
	return false
}

// conversationCarriesToolResult reports whether msgs hold a tool result answering callID with
// content.
func conversationCarriesToolResult(msgs []domain.Message, callID, content string) bool {
	for _, m := range msgs {
		if m.Role == domain.RoleTool && m.ToolCallID == callID && m.Content == content {
			return true
		}
	}
	return false
}
