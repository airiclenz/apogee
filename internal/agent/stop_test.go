package agent

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
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
		task:       retainedSurveyTask,
		name:       retainedSurveyName,
		tools:      tools.SubAgentRoster{Names: []string{"read_thing"}},
		outputPath: "notes/survey.md",
		rounds:     []delegateRound{{report: summaryReport(childFoldSummary, "reading file 0"), summary: true, spawnCallID: "c1"}},
	}
	if !ok || !reflect.DeepEqual(unstamped(retained), everyRoundAsTheEntry(wantRetained)) {
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
	wantReport := summaryReport(marker, "reading file 0")
	if retained, ok := a.retained.lookup(retainedSurveyName); !ok || len(retained.rounds) != 1 || retained.rounds[0].report != wantReport {
		t.Errorf("retained delegate = %+v (found %v), want one round reporting the unavailable marker %q", retained, ok, marker)
	}
}

// TestStopChild_AStoppedDelegationIsContinuable drives stop → continue → completion: the continued
// child opens on the retained task, the fold written at the stop and the new instructions, and its
// completed run is kept as the entry's second round.
func TestStopChild_AStoppedDelegationIsContinuable(t *testing.T) {
	scripts := stoppedSurveyScripts("c1")
	scripts = append(scripts,
		toolCallScript("c2", tools.SubAgentToolName, continueArgs(retainedSurveyName, continueInstructions, 0)),
		contentScript("the survey is now complete"),
		contentScript("parent done"))
	a, responder, sink := runStoppedSurveyParent(t, scripts, map[int]bool{2: true}, -1)

	// Call 5 is the continued child's opening request (4: the continue call).
	wantTask := firstRoundSeed(summaryReport(childFoldSummary, "reading file 0"), continueInstructions)
	if got := lastUserText(responder.requests[5]); got != wantTask {
		t.Errorf("the continued child opened on\n%s\nwant\n%s", got, wantTask)
	}
	if completed, ok := subAgentResultFor(sink.events, "c2"); !ok || completed.IsError || completed.Content != "the survey is now complete" {
		t.Errorf("the continued child's result = %+v, want its completed report", completed)
	}
	retained, ok := a.retained.lookup(retainedSurveyName)
	if !ok || len(retained.rounds) != 2 || !sameRoundText(retained.rounds[1], delegateRound{instructions: continueInstructions, report: "the survey is now complete", spawnCallID: "c2"}) {
		t.Errorf("retained delegate = %+v (found %v), want the stopped round and the completed continuation", retained, ok)
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

// TestStopChild_AQueuedPooledDelegationNeverStarts pins the stop on a pooled delegation still
// waiting for a worker: at width 2 a three-way group holds c3 queued while c1 and c2 run; the stop
// lands then, so c3 makes no request and folds nothing, commits the exact error-shaped unstarted
// result in call order with a finished phase alone, and its ledger row reads `stopped` — while
// both running siblings complete and the Turn goes on.
func TestStopChild_AQueuedPooledDelegationNeverStarts(t *testing.T) {
	sink := &recordingSink{}
	var requests atomic.Int32
	arrived := make(chan struct{}, 3)
	release := make(chan struct{})
	gate := func(ctx context.Context) {
		requests.Add(1)
		arrived <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
	}
	a := threeWayFanOutParent(t, sink, 2, gate)
	a.runIDs = newRunIDMinter(testRunIDPrefix)
	stopErr := make(chan error, 1)
	go func() {
		for i := 0; i < 2; i++ {
			<-arrived
		}
		stopErr <- a.StopChild(thirdRunID)
		close(release)
	}()

	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := <-stopErr; err != nil {
		t.Fatalf("StopChild on the queued delegation = %v, want nil", err)
	}
	if res.Status != domain.StatusExchangeComplete || res.Faulted {
		t.Fatalf("parent result = %+v, want a clean exchange-complete", res)
	}
	if got := requests.Load(); got != 2 {
		t.Errorf("child requests = %d, want 2: the stopped queued delegation makes none", got)
	}
	if folds := engineFoldUsage(sink.events, 1); len(folds) != 0 {
		t.Errorf("engine fold usage events at depth 1 = %d, want none: a queued stop folds nothing", len(folds))
	}

	results := subAgentResults(sink.events)
	if len(results) != 3 || results[0].CallID != "c1" || results[1].CallID != "c2" || results[2].CallID != "c3" {
		t.Fatalf("depth-0 results = %+v, want c1, c2, c3 in call order", results)
	}
	for i, want := range []string{"child one done", "child two done"} {
		if results[i].IsError || !strings.Contains(results[i].Content, want) {
			t.Errorf("%s result = %+v, want the running sibling's real result", results[i].CallID, results[i])
		}
	}
	if !results[2].IsError || results[2].Content != stoppedQueuedDelegationContent {
		t.Errorf("c3 result = %+v, want the exact queued-stop content as an error result", results[2])
	}
	const wantContent = "sub-agent not started: the user stopped it before it started; delegate again if the task is still needed"
	if stoppedQueuedDelegationContent != wantContent {
		t.Errorf("queued-stop content = %q, want %q", stoppedQueuedDelegationContent, wantContent)
	}
	phases := phasesFor(sink.events, "c3")
	if len(phases) != 1 || phases[0].Phase != domain.SubAgentFinished || phases[0].Cancelled ||
		phases[0].Result.Content != stoppedQueuedDelegationContent {
		t.Errorf("c3 phases = %+v, want one finished phase carrying the queued-stop result", phases)
	}
	for _, ae := range auditEvents(sink.events) {
		if ae.CallID == "c3" {
			t.Errorf("c3 was audit-recorded (%+v); a delegation that never ran books no record", ae)
		}
	}

	outcomes := map[string]delegationOutcome{}
	for _, row := range a.delegations.rows() {
		outcomes[row.callID] = row.outcome
	}
	want := map[string]delegationOutcome{"c1": delegationCompleted, "c2": delegationCompleted, "c3": delegationStopped}
	if !reflect.DeepEqual(outcomes, want) {
		t.Errorf("ledger outcomes = %v, want %v", outcomes, want)
	}
	if err := a.StopChild(thirdRunID); !errors.Is(err, domain.ErrNoSuchChild) {
		t.Errorf("StopChild on the settled queued delegation = %v, want ErrNoSuchChild", err)
	}
}

// TestChildRegistry_AStopAfterTheDequeueReachesTheArmedChild pins the window between the pool's
// dequeue and the child's arming: a stop marked there is carried into arm, which cancels the
// child's context as it is created, with the stop as its cause.
func TestChildRegistry_AStopAfterTheDequeueReachesTheArmedChild(t *testing.T) {
	var r childRegistry
	r.queue(firstRunID)
	if r.dequeue(firstRunID) {
		t.Fatal("dequeue of an unmarked delegation reported it stopped")
	}
	if !r.stop(firstRunID) {
		t.Fatal("stop between the dequeue and the arm did not reach the delegation")
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	r.arm(firstRunID, cancel)
	if !errors.Is(context.Cause(ctx), errDelegationStopped) {
		t.Errorf("child context cause = %v, want the stop carried into arm", context.Cause(ctx))
	}
	r.unregister(firstRunID)
	if r.stop(firstRunID) {
		t.Error("stop on an unregistered run still reached something")
	}
}

// TestStopChild_ReachesANestedDelegationAndItsParentGoesOn pins the nested reach: a stop on a
// grandchild's run id stops that grandchild alone — its parent child receives the stopped result
// like any tool result, runs on and completes, and the top-level Turn is untouched.
func TestStopChild_ReachesANestedDelegationAndItsParentGoesOn(t *testing.T) {
	sink := &recordingSink{}
	reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, reader)
	cfg.Delegation.MaxDepth = 2 // a grandchild exists only under a bound above the default
	scripts := [][]provider.Delta{
		subAgentCallScript("c1", "coordinate the survey"), // 0: the parent delegates
		subAgentCallScript("g1", "trawl the vendor tree"), // 1: the child delegates in turn
	}
	scripts = append(scripts, cappedChildTurns(1)...) // 2: the grandchild's working Turn
	scripts = append(scripts,
		nil,                          // 3: the grandchild's next Turn, blocked and stopped
		foldScript(700, 40),          // 4: the engine fold of the stopped grandchild
		contentScript("child done"),  // 5: the child goes on and completes
		contentScript("parent done")) // 6: the parent's reply
	responder := &stopResponder{scripts: scripts, block: map[int]bool{3: true}}
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.runIDs = newRunIDMinter(testRunIDPrefix)
	responder.before = func(call int) {
		if call != 3 {
			return
		}
		if err := a.StopChild(secondRunID); err != nil {
			t.Errorf("StopChild on the grandchild = %v, want nil", err)
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
		t.Fatalf("parent result = %+v, want a clean exchange-complete", res)
	}
	if len(responder.requests) != 7 {
		t.Fatalf("upstream calls = %d, want 7", len(responder.requests))
	}
	if !requestCarriesToolResult(responder.requests[5], "g1", stoppedResultHead+"\n") {
		t.Errorf("the child's next request does not carry the grandchild's stopped result: %+v", responder.requests[5].Messages)
	}
	if got, ok := subAgentResultFor(sink.events, "c1"); !ok || got.IsError || got.Content != "child done" {
		t.Errorf("child result = %+v (found %v), want the child's completed report", got, ok)
	}
	if rows := a.delegations.rows(); len(rows) != 1 || rows[0].outcome != delegationCompleted {
		t.Errorf("parent ledger rows = %+v, want the child recorded %q", rows, delegationCompleted)
	}
}

// sameRoundText reports whether got carries want's instructions, report, summary flag and spawning
// call id — the round's text, leaving out what its call resolved to and the use sequence retain stamps.
func sameRoundText(got, want delegateRound) bool {
	return got.instructions == want.instructions && got.report == want.report &&
		got.summary == want.summary && got.spawnCallID == want.spawnCallID
}
