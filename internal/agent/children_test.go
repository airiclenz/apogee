package agent

import (
	"context"
	"errors"
	"iter"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// ----------------------------------------------------------------------------
// Child addressing (ADR 0063, ADR 0086) — InterjectChild, the mailbox drain, the delivery event
// ----------------------------------------------------------------------------
//
// A delegation runs synchronously inside the parent's Turn, so the ONLY window a test has to act
// "while the child runs" is inside the responder's Stream for one of the child's own Turns — the
// child is registered before its Run starts, so it is addressable from there. A child is addressed
// by its run id, so every test here that addresses one pins the tree's minter to testRunIDPrefix
// and names the Nth delegation the tree spawned by firstRunID / secondRunID.

// testRunIDPrefix is the fixed run-id prefix these tests inject (newRunIDMinter), so the run ids the
// tree mints — in spawn order, across every depth — are known ahead of the run.
const testRunIDPrefix = "0badc0de"

const (
	firstRunID  = testRunIDPrefix + ".1" // the run id of the first delegation the tree spawns
	secondRunID = testRunIDPrefix + ".2" // the run id of the second
	thirdRunID  = testRunIDPrefix + ".3" // the run id of the third
)

// requestLogResponder is scriptedResponder plus the two seams these tests need: it keeps every
// request the loop sent, in order, so an assertion can read what the model actually saw, and it
// runs an optional hook on the loop's own goroutine before a given call's stream is produced —
// the window in which a running child can be addressed.
type requestLogResponder struct {
	scripts  [][]provider.Delta
	requests []provider.Request
	// before runs immediately before call N's stream is produced, N counted from 0 across BOTH
	// the parent's and the children's Turns (one responder serves the whole tree).
	before func(call int)
	calls  int
}

func (r *requestLogResponder) Stream(_ context.Context, req provider.Request) iter.Seq[provider.Delta] {
	r.requests = append(r.requests, req)
	i := r.calls
	r.calls++
	if r.before != nil {
		r.before(i)
	}
	return func(yield func(provider.Delta) bool) {
		if i >= len(r.scripts) {
			yield(provider.Delta{Kind: provider.DeltaError, Err: "requestLogResponder: out of scripts"})
			return
		}
		for _, d := range r.scripts[i] {
			if !yield(d) {
				return
			}
		}
	}
}

// childInterjections returns every ChildInterjectionEvent on the sink, in emission order.
func childInterjections(events []domain.Event) []domain.ChildInterjectionEvent {
	var found []domain.ChildInterjectionEvent
	for _, e := range events {
		if ev, ok := e.(domain.ChildInterjectionEvent); ok {
			found = append(found, ev)
		}
	}
	return found
}

// TestInterjectChild_UnknownCallIDIsRefused proves the refusal an addressing race always has to
// have: an id naming no running sub-agent is answered with ErrNoSuchChild rather than queued
// somewhere nothing drains.
func TestInterjectChild_UnknownCallIDIsRefused(t *testing.T) {
	a, err := newAgent(baseConfig(&recordingSink{}), scriptedResponder(t))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	for _, id := range []string{"nobody", ""} {
		if err := a.InterjectChild(id, domain.UserInput{Text: "hello"}); !errors.Is(err, domain.ErrNoSuchChild) {
			t.Errorf("InterjectChild(%q) = %v, want ErrNoSuchChild", id, err)
		}
	}
}

// TestInterjectChild_LandsAtTheChildsNextStep is the delivery guarantee: a message queued for a
// running child while its first Turn streams is committed at the child's next between-Steps
// boundary, so the child's SECOND request carries it as a user message after the tool results —
// and one Landed event, stamped with the child's own depth and spawn id, accounts for it.
func TestInterjectChild_LandsAtTheChildsNextStep(t *testing.T) {
	const remark = "focus on the tests"

	sink := &recordingSink{}
	looked := 0
	cfg := subAgentConfig(sink, domain.ModeAskBefore, fakeTool{name: "look", readOnly: true, ran: &looked, result: "looked"})

	responder := &requestLogResponder{scripts: [][]provider.Delta{
		subAgentCallScript("c1", "survey the repo"), // [0] parent delegates
		toolCallScript("t1", "look", `{}`),          // [1] child Turn 1 — a tool call, so a Turn 2 follows
		contentScript("child done"),                 // [2] child Turn 2 — carries the remark
		contentScript("parent done"),                // [3] parent finishes
	}}
	var child *Agent
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.runIDs = newRunIDMinter(testRunIDPrefix)
	responder.before = func(call int) {
		if call != 1 {
			return
		}
		var ok bool
		if child, ok = a.children.lookup(firstRunID); !ok {
			t.Error("the running child is not registered under its run id")
		}
		if err := a.InterjectChild(firstRunID, domain.UserInput{Text: remark}); err != nil {
			t.Errorf("InterjectChild while the child runs: %v", err)
		}
	}

	if err := a.Submit(domain.UserInput{Text: "go"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// What the child's model saw on its next request: the remark last, after the tool results.
	if len(responder.requests) < 3 {
		t.Fatalf("requests = %d, want at least 3 (parent, child Turn 1, child Turn 2)", len(responder.requests))
	}
	msgs := responder.requests[2].Messages
	last := msgs[len(msgs)-1]
	if last.Role != string(domain.RoleUser) || last.Content != remark {
		t.Errorf("child's second request ends with %+v, want the queued user message %q", last, remark)
	}
	if before := msgs[len(msgs)-2]; before.Role != string(domain.RoleTool) {
		t.Errorf("the message before the remark has role %q, want a tool result — the remark must land AFTER the tool results", before.Role)
	}

	// And on the child's own history it is marked an interjection, so the derived Exchange
	// opening does not move (domain.CurrentExchange).
	if child == nil {
		t.Fatal("never captured the child agent")
	}
	interjected := false
	for _, m := range child.conv.Messages() {
		if m.Role == domain.RoleUser && m.Content == remark {
			interjected = m.Interjected
		}
	}
	if !interjected {
		t.Error("the delivered remark is not marked Interjected on the child's history")
	}

	// One event, Landed, carrying the CHILD run's identity.
	events := childInterjections(sink.events)
	if len(events) != 1 {
		t.Fatalf("ChildInterjectionEvents = %d, want exactly 1", len(events))
	}
	got := events[0]
	if !got.Landed || got.Depth != 1 || got.CallID != "c1" || got.Input.Text != remark {
		t.Errorf("event = %+v, want Landed at Depth 1 for c1 carrying %q", got, remark)
	}
}

// TestInterjectChild_SharedCallIDsAddressTwoRuns is the case a spawn-call-id key got wrong (ADR
// 0086 D5): two concurrent delegations whose calls share one id "c1" are two running children, and
// an interjection by each one's run id lands on that child alone. Keyed by call id, the second
// registration replaced the first, so both remarks reached one child and the other never saw its
// own.
func TestInterjectChild_SharedCallIDsAddressTwoRuns(t *testing.T) {
	const (
		remarkOne = "remark for one"
		remarkTwo = "remark for two"
	)

	sink := newLockedSink() // two children emit from two pool workers at once
	// Both children are held inside their first Turn until each is registered and addressed, so
	// the two interjections go out while both runs are live at once.
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	gate := func(ctx context.Context) {
		arrived <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
	}
	up := newRoutedResponder().
		route("delegate two things", nil, fanOutScript([2]string{"c1", "task one"}, [2]string{"c1", "task two"})).
		route("task one", gate, toolCallScript("t1", "look", `{}`)).
		route("task two", gate, toolCallScript("t1", "look", `{}`)).
		route(remarkOne, nil, contentScript("child one done")).
		route(remarkTwo, nil, contentScript("child two done")).
		route("delegate two things", nil, contentScript("parent done"))
	cfg := subAgentConfig(sink, domain.ModeAskBefore, fakeTool{name: "look", readOnly: true, result: "looked"})
	cfg.ParallelAgents = 2
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.runIDs = newRunIDMinter(testRunIDPrefix)
	go func() {
		<-arrived
		<-arrived
		if err := a.InterjectChild(firstRunID, domain.UserInput{Text: remarkOne}); err != nil {
			t.Errorf("InterjectChild(%s): %v", firstRunID, err)
		}
		if err := a.InterjectChild(secondRunID, domain.UserInput{Text: remarkTwo}); err != nil {
			t.Errorf("InterjectChild(%s): %v", secondRunID, err)
		}
		close(release)
	}()
	if err := a.Submit(domain.UserInput{Text: "delegate two things"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	landedOn := map[string]string{}
	sink.mu.Lock()
	events := childInterjections(sink.events)
	sink.mu.Unlock()
	for _, ev := range events {
		if !ev.Landed {
			t.Errorf("%q did not land (reason %q); each remark had a running child to reach", ev.Input.Text, ev.Reason)
			continue
		}
		landedOn[ev.Input.Text] = ev.RunID
	}
	want := map[string]string{remarkOne: firstRunID, remarkTwo: secondRunID}
	if !maps.Equal(landedOn, want) {
		t.Errorf("remarks landed on runs %v, want each on its own run %v", landedOn, want)
	}
}

// TestInterjectChild_TopLevelRunNeverDrains pins the depth > 0 scope of the drain: ADR 0063
// supersedes ADR 0025's rejected Run-side drain for CHILDREN only, so a top-level Run leaves its
// mailbox alone and emits no delivery event — an embedder's interjection stays its own Interject
// call between the Steps it drives.
func TestInterjectChild_TopLevelRunNeverDrains(t *testing.T) {
	sink := &recordingSink{}
	responder := scriptedResponder(t,
		toolCallTurn("t1", "look", `{}`),
		contentTurn("done"),
	)
	looked := 0
	cfg := baseConfig(sink)
	reg := domain.NewToolRegistry()
	_ = reg.Register(fakeTool{name: "look", readOnly: true, ran: &looked, result: "looked"})
	cfg.Tools = reg
	cfg.Mode = domain.ModeAskBefore

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.mailbox.add(domain.UserInput{Text: "never delivered"})
	if err := a.Submit(domain.UserInput{Text: "go"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if events := childInterjections(sink.events); len(events) != 0 {
		t.Errorf("a top-level Run emitted %d ChildInterjectionEvents, want none", len(events))
	}
	for _, m := range a.conv.Messages() {
		if m.Interjected {
			t.Error("a top-level Run drained its mailbox; only a child's Run may")
		}
	}
}

// TestInterjectChild_QueuedAfterTheLastStepIsReportedUndelivered proves the other half of the
// accounting contract: a message queued while the child's LAST Turn streams has no boundary left
// to land at, and is reported undelivered rather than silently dropped.
func TestInterjectChild_QueuedAfterTheLastStepIsReportedUndelivered(t *testing.T) {
	const remark = "one more thing"

	sink := &recordingSink{}
	responder := &requestLogResponder{scripts: [][]provider.Delta{
		subAgentCallScript("c1", "survey the repo"), // [0] parent delegates
		contentScript("child done"),                 // [1] the child's ONLY Turn
		contentScript("parent done"),                // [2] parent finishes
	}}
	a, err := newAgent(subAgentConfig(sink, domain.ModeAskBefore), responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.runIDs = newRunIDMinter(testRunIDPrefix)
	responder.before = func(call int) {
		if call != 1 {
			return
		}
		if err := a.InterjectChild(firstRunID, domain.UserInput{Text: remark}); err != nil {
			t.Errorf("InterjectChild while the child runs: %v", err)
		}
	}
	if err := a.Submit(domain.UserInput{Text: "go"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	events := childInterjections(sink.events)
	if len(events) != 1 {
		t.Fatalf("ChildInterjectionEvents = %d, want exactly 1", len(events))
	}
	if got := events[0]; got.Landed || got.CallID != "c1" || got.Input.Text != remark {
		t.Errorf("event = %+v, want Landed:false for c1 carrying %q", got, remark)
	}
	// The child ran to its own reply, so the reason is that it finished.
	if got := events[0].Reason; got != domain.UndeliveredCompleted {
		t.Errorf("Reason = %q, want %q — the child completed before the message could land",
			got, domain.UndeliveredCompleted)
	}

	// The child is gone, so the same id is refused from here on.
	if err := a.InterjectChild(firstRunID, domain.UserInput{Text: remark}); !errors.Is(err, domain.ErrNoSuchChild) {
		t.Errorf("InterjectChild after the child finished = %v, want ErrNoSuchChild", err)
	}
}

// TestInterjectChild_QueuedBeforeAPanicIsReportedFaulted pins the reason's source on the one path
// where the mailbox closes before the run is classified: a child that panics with a message queued
// unwinds through the reaping defer first, while no outcome exists, and the message is reported
// only once the recover has classified the delegation — as faulted, never as finished.
func TestInterjectChild_QueuedBeforeAPanicIsReportedFaulted(t *testing.T) {
	const remark = "one more thing"

	sink := &recordingSink{}
	responder := &requestLogResponder{scripts: [][]provider.Delta{
		subAgentCallScript("c1", "survey the repo"), // [0] parent delegates
		contentScript("never streamed"),             // [1] the child's Turn — panics first
		contentScript("parent done"),                // [2] parent finishes
	}}
	a, err := newAgent(subAgentConfig(sink, domain.ModeAskBefore), responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.runIDs = newRunIDMinter(testRunIDPrefix)
	responder.before = func(call int) {
		if call != 1 {
			return
		}
		if err := a.InterjectChild(firstRunID, domain.UserInput{Text: remark}); err != nil {
			t.Errorf("InterjectChild while the child runs: %v", err)
		}
		panic("child boom")
	}
	if err := a.Submit(domain.UserInput{Text: "go"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	events := childInterjections(sink.events)
	if len(events) != 1 {
		t.Fatalf("ChildInterjectionEvents = %d, want exactly 1", len(events))
	}
	if got := events[0]; got.Landed || got.CallID != "c1" || got.Reason != domain.UndeliveredFaulted {
		t.Errorf("event = %+v, want Landed:false for c1 with Reason %q", got, domain.UndeliveredFaulted)
	}
}

// TestInterjectChild_ReachesAGrandchild proves the recursion: a host holding only the top-level
// Agent addresses a child two levels down, because the lookup walks every registered child's own
// registry when the id is not its own.
func TestInterjectChild_ReachesAGrandchild(t *testing.T) {
	const remark = "check the goldens too"

	sink := &recordingSink{}
	looked := 0
	cfg := subAgentConfig(sink, domain.ModeAskBefore, fakeTool{name: "look", readOnly: true, ran: &looked, result: "looked"})
	cfg.Delegation.MaxDepth = 2 // a grandchild exists only under a bound above the default

	responder := &requestLogResponder{scripts: [][]provider.Delta{
		subAgentCallScript("c1", "level 1"), // [0] parent → child
		subAgentCallScript("c2", "level 2"), // [1] child → grandchild
		toolCallScript("t1", "look", `{}`),  // [2] grandchild Turn 1
		contentScript("grandchild done"),    // [3] grandchild Turn 2 — carries the remark
		contentScript("child done"),         // [4] child finishes
		contentScript("parent done"),        // [5] parent finishes
	}}
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.runIDs = newRunIDMinter(testRunIDPrefix)
	responder.before = func(call int) {
		if call != 2 {
			return
		}
		if err := a.InterjectChild(secondRunID, domain.UserInput{Text: remark}); err != nil {
			t.Errorf("InterjectChild for a grandchild through the top-level agent: %v", err)
		}
	}
	if err := a.Submit(domain.UserInput{Text: "go"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	events := childInterjections(sink.events)
	if len(events) != 1 {
		t.Fatalf("ChildInterjectionEvents = %d, want exactly 1", len(events))
	}
	if got := events[0]; !got.Landed || got.Depth != 2 || got.CallID != "c2" {
		t.Errorf("event = %+v, want Landed at Depth 2 for c2", got)
	}
}

// TestInterjectChild_PendingMailboxSkipsAGrandchild pins the depth > 0 half of the pre-emption
// rule: a message queued for a running child (its mailbox) waits on that child's grandchildren
// exactly as the human's queued message waits on its children. The child's reply delegates while
// the remark is queued, so the grandchild is never started — the child commits the skip result for
// it, reaches its boundary, and the remark lands there as an ordinary interjection.
func TestInterjectChild_PendingMailboxSkipsAGrandchild(t *testing.T) {
	const remark = "stop, change of plan"

	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore)

	responder := &requestLogResponder{scripts: [][]provider.Delta{
		subAgentCallScript("c1", "level 1"), // [0] parent → child
		subAgentCallScript("c2", "level 2"), // [1] child Turn 1 delegates — with the remark already queued
		contentScript("child done"),         // [2] child Turn 2 — carries the remark; no grandchild ever asked
		contentScript("parent done"),        // [3] parent finishes
	}}
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.runIDs = newRunIDMinter(testRunIDPrefix)
	responder.before = func(call int) {
		if call != 1 {
			return
		}
		if err := a.InterjectChild(firstRunID, domain.UserInput{Text: remark}); err != nil {
			t.Errorf("InterjectChild while the child runs: %v", err)
		}
	}
	if err := a.Submit(domain.UserInput{Text: "go"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The grandchild never asked the model: four requests, none of them from "level 2".
	if responder.calls != 4 {
		t.Errorf("model calls = %d, want 4 (parent, child ×2, parent) — the grandchild must not run", responder.calls)
	}
	for _, req := range responder.requests {
		if lastUserText(req) == "level 2" {
			t.Error("the grandchild reached the Upstream; a pending mailbox message must skip it")
		}
	}

	// The child's history holds the skip result for c2, and its bracket is one finished phase.
	var skipped *domain.ToolResult
	for _, e := range sink.events {
		if re, ok := e.(domain.ToolResultEvent); ok && re.Depth == 1 && re.Result.CallID == "c2" {
			r := re.Result
			skipped = &r
		}
	}
	if skipped == nil {
		t.Fatal("no depth-1 tool result for c2; the skipped grandchild must still commit")
	}
	if !skipped.IsError || skipped.Content != skippedDelegationContent {
		t.Errorf("c2 result = %+v, want the exact skip content as an error result", *skipped)
	}
	var phases []domain.SubAgentPhaseEvent
	for _, e := range sink.events {
		if pe, ok := e.(domain.SubAgentPhaseEvent); ok && pe.CallID == "c2" {
			phases = append(phases, pe)
		}
	}
	if len(phases) != 1 || phases[0].Phase != domain.SubAgentFinished || phases[0].Depth != 2 || phases[0].Cancelled {
		t.Errorf("c2 phases = %+v, want exactly one finished phase at Depth 2, not Cancelled", phases)
	}

	// And the remark still lands at the child's next boundary, exactly as without a delegation.
	events := childInterjections(sink.events)
	if len(events) != 1 || !events[0].Landed || events[0].CallID != "c1" {
		t.Errorf("ChildInterjectionEvents = %+v, want exactly one, Landed, for c1", events)
	}
	msgs := responder.requests[2].Messages
	if last := msgs[len(msgs)-1]; last.Role != string(domain.RoleUser) || last.Content != remark {
		t.Errorf("child's second request ends with %+v, want the queued remark %q", last, remark)
	}
}

// TestRetainedDelegates_KeepsTheLatestUnderEachName pins the set's contract: a name maps to the
// latest capped delegation retained under it, an unnamed one is never kept, names come back sorted,
// and clear forgets everything.
func TestRetainedDelegates_KeepsTheLatestUnderEachName(t *testing.T) {
	var r retainedDelegates

	r.retain(retainedDelegate{name: "Beta", spawnCallID: "c1"})
	r.retain(retainedDelegate{name: "Alpha", spawnCallID: "c2"})
	r.retain(retainedDelegate{name: "Beta", spawnCallID: "c3"})
	r.retain(retainedDelegate{name: "", spawnCallID: "c4"})

	if names := r.names(); !slices.Equal(names, []string{"Alpha", "Beta"}) {
		t.Errorf("names = %v, want the two named entries, sorted", names)
	}
	if got, ok := r.lookup("Beta"); !ok || got.spawnCallID != "c3" {
		t.Errorf("lookup(Beta) = %+v, %v; want the latest entry c3", got, ok)
	}
	if _, ok := r.lookup(""); ok {
		t.Error("an unnamed delegation was retained; it has no handle a continuation could name")
	}

	r.clear()

	if names := r.names(); len(names) != 0 {
		t.Errorf("names after clear = %v, want none", names)
	}
}

// TestDelegationLedger_RowsReadInSpawnOrderAndTheNoteNeedsTwoOrAFailure pins the ledger's own
// contract: rows come back by spawn index whatever order they were recorded in, one completed
// delegation alone renders no note, a second one or any non-completed one does, and clear forgets
// everything including the spawn count.
func TestDelegationLedger_RowsReadInSpawnOrderAndTheNoteNeedsTwoOrAFailure(t *testing.T) {
	var l delegationLedger

	l.reserve("c1")
	l.reserve("c2")
	first, second := l.open("c1"), l.open("c2")
	if first != 1 || second != 2 {
		t.Fatalf("open(c1), open(c2) = %d, %d; want the reserved indices 1 and 2", first, second)
	}
	l.record(delegationRecord{spawnIndex: second, callID: "c2", name: "Beta", outcome: delegationCompleted})
	if _, ok := l.note(); ok {
		t.Error("one completed delegation rendered a note; the coordinator gets that case right unaided")
	}
	l.record(delegationRecord{spawnIndex: first, callID: "c1", name: "Alpha", outcome: delegationFaulted, cause: "the upstream died"})

	rows := l.rows()
	if len(rows) != 2 || rows[0].callID != "c1" || rows[1].callID != "c2" {
		t.Fatalf("rows = %+v, want c1 then c2 in spawn order, not recording order", rows)
	}
	note, ok := l.note()
	want := delegationsNoteHead + "\n#1 Alpha — faulted: the upstream died — output none\n#2 Beta — completed — output none"
	if !ok || note != want {
		t.Errorf("note = %q, %v; want %q", note, ok, want)
	}

	l.clear()

	if rows := l.rows(); len(rows) != 0 {
		t.Errorf("rows after clear = %+v, want none", rows)
	}
	if got := l.open("c9"); got != 1 {
		t.Errorf("first spawn index after clear = %d, want the count to restart at 1 for an unreserved call", got)
	}
}

// TestDelegationLedger_OneFailureAloneIsNotable pins the other half of the trigger: a single
// delegation that did not complete renders the note on its own.
func TestDelegationLedger_OneFailureAloneIsNotable(t *testing.T) {
	for _, outcome := range []delegationOutcome{delegationCapped, delegationFaulted, delegationCancelled, delegationRefused} {
		var l delegationLedger
		l.record(delegationRecord{spawnIndex: l.open("c1"), name: "Solo", outcome: outcome})
		if note, ok := l.note(); !ok || !strings.Contains(note, "#1 Solo — "+string(outcome)) {
			t.Errorf("%s: note = %q, %v; want the one row rendered", outcome, note, ok)
		}
	}
}

// TestDelegationLedger_CauseIsTheHeadLineClamped pins what a row quotes of a failure: its first
// line only, trimmed, and cut at delegationCauseMaxRunes with an ellipsis.
func TestDelegationLedger_CauseIsTheHeadLineClamped(t *testing.T) {
	if got := delegationCause("first line \nsecond line"); got != "first line" {
		t.Errorf("cause = %q, want the trimmed head line", got)
	}
	long := strings.Repeat("x", delegationCauseMaxRunes+5)
	if got := delegationCause(long); got != strings.Repeat("x", delegationCauseMaxRunes)+"…" {
		t.Errorf("cause = %q, want %d runes and the ellipsis", got, delegationCauseMaxRunes)
	}
}

// TestUndeliveredReason_FollowsTheDelegationOutcome pins the mapping the reaping path reports
// through: each way a delegation ends names its own reason, so a message left in the mailbox is
// never told it missed a child that "finished" when the child was capped, failed or cancelled.
func TestUndeliveredReason_FollowsTheDelegationOutcome(t *testing.T) {
	t.Parallel()
	cases := []struct {
		ended delegationOutcome
		want  domain.UndeliveredReason
	}{
		{delegationCompleted, domain.UndeliveredCompleted},
		{delegationCapped, domain.UndeliveredCapped},
		{delegationFaulted, domain.UndeliveredFaulted},
		{delegationCancelled, domain.UndeliveredCancelled},
		{delegationRefused, domain.UndeliveredRefused},
	}
	for _, tc := range cases {
		if got := undeliveredReason(tc.ended); got != tc.want {
			t.Errorf("undeliveredReason(%q) = %q, want %q", tc.ended, got, tc.want)
		}
	}
}
