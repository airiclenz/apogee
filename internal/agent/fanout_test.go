package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/tools"
)

// ----------------------------------------------------------------------------
// Depth-0 sub-agent fan-out (ADR 0039 — Parallel agents)
// ----------------------------------------------------------------------------
//
// These tests drive several nested Agents at once, so the call-ORDER responder the rest of the
// package uses (scriptedResponder) cannot serve them: which child reaches the Upstream first is
// exactly the thing under test. routedResponder answers by WHO is asking instead — the last user
// message in the request, which is the delegated task for a child and the human's input for the
// parent — so each agent's script is deterministic however the goroutines interleave.

// routedResponder answers each request from the queue registered for the asker, identified by the
// last user message in the request. Every agent in a fan-out therefore has its own script, and the
// scripts are consumed in that agent's own Turn order rather than in global call order.
type routedResponder struct {
	mu     sync.Mutex
	routes map[string][]turnScript
}

// turnScript is one scripted reply plus an optional gate — a hook that runs on the asking agent's
// goroutine before the reply streams, which is where a test observes or synchronises concurrency.
type turnScript struct {
	gate   func(context.Context)
	deltas []provider.Delta
}

func newRoutedResponder() *routedResponder {
	return &routedResponder{routes: map[string][]turnScript{}}
}

// route appends one scripted Turn for the agent whose last user message is asker.
func (r *routedResponder) route(asker string, gate func(context.Context), deltas []provider.Delta) *routedResponder {
	r.routes[asker] = append(r.routes[asker], turnScript{gate: gate, deltas: deltas})
	return r
}

func (r *routedResponder) Stream(ctx context.Context, req provider.Request) iter.Seq[provider.Delta] {
	asker := lastUserText(req)
	r.mu.Lock()
	queue := r.routes[asker]
	script := turnScript{deltas: []provider.Delta{{
		Kind: provider.DeltaError,
		Err:  "routedResponder: no script left for " + asker,
	}}}
	if len(queue) > 0 {
		script, r.routes[asker] = queue[0], queue[1:]
	}
	r.mu.Unlock()

	return func(yield func(provider.Delta) bool) {
		if script.gate != nil {
			script.gate(ctx)
		}
		for _, d := range script.deltas {
			if !yield(d) {
				return
			}
		}
	}
}

// lastUserText is the routing key: the most recent user message in a request. It identifies the
// agent that built the request, because a child's only user message is its delegated task and the
// parent's is the human's input.
func lastUserText(req provider.Request) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == string(domain.RoleUser) {
			return req.Messages[i].Content
		}
	}
	return ""
}

// concurrencyProbe measures how many agents are inside their Upstream call AT ONCE. Each caller
// records its arrival, then waits for `want` arrivals so a genuinely concurrent fan-out
// rendezvouses immediately; `wait` bounds a serial run to one short pause instead of a hang.
//
// peak is the whole verdict: peak == want proves the children overlapped, peak == 1 proves they
// did not. Counting ARRIVALS would not — a serial run reaches `want` arrivals too, just never at
// the same time.
type concurrencyProbe struct {
	mu       sync.Mutex
	inFlight int
	peak     int
	arrivals int
	want     int
	wait     time.Duration
	open     chan struct{}
}

func newConcurrencyProbe(want int, wait time.Duration) *concurrencyProbe {
	return &concurrencyProbe{want: want, wait: wait, open: make(chan struct{})}
}

func (p *concurrencyProbe) enter(ctx context.Context) {
	p.mu.Lock()
	p.inFlight++
	if p.inFlight > p.peak {
		p.peak = p.inFlight
	}
	p.arrivals++
	if p.arrivals == p.want {
		close(p.open)
	}
	p.mu.Unlock()

	timer := time.NewTimer(p.wait)
	defer timer.Stop()
	select {
	case <-p.open:
	case <-ctx.Done():
	case <-timer.C:
	}

	p.mu.Lock()
	p.inFlight--
	p.mu.Unlock()
}

func (p *concurrencyProbe) peakInFlight() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.peak
}

// fanOutScript emits one assistant reply carrying a sub_agent call per (id, task) pair — the
// multi-delegation reply that triggers the fan-out.
func fanOutScript(pairs ...[2]string) []provider.Delta {
	deltas := make([]provider.Delta, 0, len(pairs)+1)
	for _, p := range pairs {
		deltas = append(deltas, provider.Delta{Kind: provider.DeltaToolCall, ToolCall: &provider.ToolCall{
			ID:       p[0],
			Type:     "function",
			Function: provider.FunctionCall{Name: tools.SubAgentToolName, Arguments: subAgentArgs(p[1])},
		}})
	}
	return append(deltas, provider.Delta{Kind: provider.DeltaDone, FinishReason: "tool_calls"})
}

// subAgentResults returns every sub_agent tool result the sink saw at depth 0, in emission order —
// which, because dispatch commits the group serially after the pool joins, is the order the
// results entered the parent's history.
func subAgentResults(events []domain.Event) []domain.ToolResult {
	var out []domain.ToolResult
	for _, e := range events {
		re, ok := e.(domain.ToolResultEvent)
		if !ok || re.Depth != 0 {
			continue
		}
		out = append(out, re.Result)
	}
	return out
}

// fanOutAgent builds a parent wired for a two-way delegation at the given cap, with the given
// responder, and runs it to its boundary. It returns the parent's StepResult.
func fanOutAgent(t *testing.T, sink domain.EventSink, parallelAgents int, up provider.Responder) *Agent {
	t.Helper()
	cfg := subAgentConfig(sink, domain.ModeAskBefore)
	cfg.ParallelAgents = parallelAgents
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "delegate two things"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	return a
}

// TestFanOut_ChildrenOverlapAndCommitInCallOrder is the heart of the item: with a cap of 2 a reply
// carrying two sub_agent calls runs both children AT ONCE (the probe's peak proves the overlap),
// and their results still land in the parent's history in EMITTED-call order — not in completion
// order — because the pool only widens the run, never the commit.
func TestFanOut_ChildrenOverlapAndCommitInCallOrder(t *testing.T) {
	sink := &recordingSink{}
	probe := newConcurrencyProbe(2, 3*time.Second)
	// The second child answers first if it can: the gate releases both at the same instant, so
	// completion order is genuinely unconstrained and only the commit phase fixes the history.
	up := newRoutedResponder().
		route("delegate two things", nil, fanOutScript([2]string{"c1", "task one"}, [2]string{"c2", "task two"})).
		route("task one", probe.enter, contentScript("child one done")).
		route("task two", probe.enter, contentScript("child two done")).
		route("delegate two things", nil, contentScript("parent done"))

	a := fanOutAgent(t, sink, 2, up)
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Fatalf("parent status = %q, want the Exchange to complete", res.Status)
	}

	if peak := probe.peakInFlight(); peak != 2 {
		t.Errorf("peak children in flight = %d, want 2 (the group ran serially, not through the pool)", peak)
	}

	results := subAgentResults(sink.events)
	if len(results) != 2 {
		t.Fatalf("depth-0 tool results = %d, want 2", len(results))
	}
	if results[0].CallID != "c1" || results[1].CallID != "c2" {
		t.Errorf("results committed as %q,%q; want the emitted call order c1,c2", results[0].CallID, results[1].CallID)
	}
	if !strings.Contains(results[0].Content, "child one done") {
		t.Errorf("c1 result = %q, want child one's final message", results[0].Content)
	}
	if !strings.Contains(results[1].Content, "child two done") {
		t.Errorf("c2 result = %q, want child two's final message", results[1].Content)
	}
}

// TestFanOut_CapOneKeepsTheGroupSerial pins the floor Bypass-style: a cap below 2 must leave the
// loop exactly where it was, running the delegations one after another.
func TestFanOut_CapOneKeepsTheGroupSerial(t *testing.T) {
	sink := &recordingSink{}
	probe := newConcurrencyProbe(2, 150*time.Millisecond)
	up := newRoutedResponder().
		route("delegate two things", nil, fanOutScript([2]string{"c1", "task one"}, [2]string{"c2", "task two"})).
		route("task one", probe.enter, contentScript("child one done")).
		route("task two", probe.enter, contentScript("child two done")).
		route("delegate two things", nil, contentScript("parent done"))

	a := fanOutAgent(t, sink, 1, up)
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if peak := probe.peakInFlight(); peak != 1 {
		t.Errorf("peak children in flight = %d at cap 1, want 1 (the serial floor was widened)", peak)
	}
	results := subAgentResults(sink.events)
	if len(results) != 2 || results[0].CallID != "c1" || results[1].CallID != "c2" {
		t.Errorf("serial results = %+v, want c1 then c2", results)
	}
}

// TestFanOut_DepthOneStaysSerial pins decision 3: only the top-level agent fans out. A depth-1
// child that itself emits two delegations runs its grandchildren one at a time even though the
// cap it inherited says 4. The depth bound is raised to 2 so the grandchildren can exist at all.
func TestFanOut_DepthOneStaysSerial(t *testing.T) {
	sink := &recordingSink{}
	probe := newConcurrencyProbe(2, 150*time.Millisecond)
	up := newRoutedResponder().
		route("delegate two things", nil, fanOutScript([2]string{"c1", "the middle task"})).
		route("the middle task", nil, fanOutScript([2]string{"g1", "leaf one"}, [2]string{"g2", "leaf two"})).
		route("leaf one", probe.enter, contentScript("leaf one done")).
		route("leaf two", probe.enter, contentScript("leaf two done")).
		route("the middle task", nil, contentScript("middle done")).
		route("delegate two things", nil, contentScript("parent done"))

	a := fanOutAgent(t, sink, 4, up)
	a.cfg.Delegation.MaxDepth = 2
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if peak := probe.peakInFlight(); peak != 1 {
		t.Errorf("peak grandchildren in flight = %d, want 1 (a depth-1 fan-out must stay serial)", peak)
	}
}

// TestFanOut_SiblingSurvivesAFailedChild pins decision 4: a child's failure becomes THAT child's
// tool result and nothing else — no sibling is cancelled, and the parent Exchange completes.
func TestFanOut_SiblingSurvivesAFailedChild(t *testing.T) {
	sink := &recordingSink{}
	probe := newConcurrencyProbe(2, 3*time.Second)
	up := newRoutedResponder().
		route("delegate two things", nil, fanOutScript([2]string{"c1", "doomed task"}, [2]string{"c2", "healthy task"})).
		route("doomed task", probe.enter, []provider.Delta{{Kind: provider.DeltaError, Err: "upstream exploded"}}).
		route("healthy task", probe.enter, contentScript("healthy child done")).
		route("delegate two things", nil, contentScript("parent done"))

	a := fanOutAgent(t, sink, 2, up)
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Fatalf("parent status = %q, want the Exchange to complete despite one failed child", res.Status)
	}

	results := subAgentResults(sink.events)
	if len(results) != 2 {
		t.Fatalf("depth-0 tool results = %d, want 2 (both delegations must report back)", len(results))
	}
	if !results[0].IsError {
		t.Errorf("failed child's result = %+v, want an error result", results[0])
	}
	if results[1].IsError || !strings.Contains(results[1].Content, "healthy child done") {
		t.Errorf("sibling's result = %+v, want it intact", results[1])
	}
}

// TestFanOut_CancelRollsTheWholeTurnBack pins decision 10 at N children: Esc reaches every
// in-flight child, the pool waits for all of them, and the parent Turn rolls back with NO partial
// delegation in history — while every child's lifecycle bracket still CLOSES (ADR 0075 decision
// 12), so a log reader sees the rollback rather than a delegation that never ends.
func TestFanOut_CancelRollsTheWholeTurnBack(t *testing.T) {
	sink := &recordingSink{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	probe := newConcurrencyProbe(2, 3*time.Second)
	// Both children reach the Upstream, then the human presses Esc: each stream surfaces the
	// cancellation the way the real provider does.
	interrupted := func(c context.Context) {
		probe.enter(c)
		cancel()
		<-c.Done()
	}
	up := newRoutedResponder().
		route("delegate two things", nil, fanOutScript([2]string{"c1", "task one"}, [2]string{"c2", "task two"})).
		route("task one", interrupted, []provider.Delta{{Kind: provider.DeltaError, Err: "context canceled"}}).
		route("task two", interrupted, []provider.Delta{{Kind: provider.DeltaError, Err: "context canceled"}})

	a := fanOutAgent(t, sink, 2, up)
	res, err := a.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != domain.StatusCancelled {
		t.Fatalf("parent status = %q, want %q", res.Status, domain.StatusCancelled)
	}
	if res.Faulted {
		t.Error("a cancelled fan-out reported as a fault; a cancel is a re-attemptable rollback")
	}
	if peak := probe.peakInFlight(); peak != 2 {
		t.Errorf("peak children in flight = %d, want 2 (both children must have been in flight)", peak)
	}
	if got := subAgentResults(sink.events); len(got) != 0 {
		t.Errorf("cancelled fan-out surfaced %d tool results, want none", len(got))
	}
	assertCancelledBracket(t, sink.events, "c1", "c2")
	for _, m := range a.conv.Messages() {
		if m.Role == domain.RoleTool {
			t.Fatalf("a tool message survived the rollback: %+v", m)
		}
	}
}

// TestFanOut_ChildPanicRecoversWithoutKillingTheSibling pins the per-child fault boundary (ADR
// 0007) on the pooled path — runSubAgent's frame, inside each worker goroutine's call chain: a
// panic raised inside one worker's nested Agent becomes that call's error result, and the sibling
// and the parent Exchange carry on. TestDispatch_SerialDelegationPanicRecoversAtTheChildBoundary
// is its serial-path mirror.
func TestFanOut_ChildPanicRecoversWithoutKillingTheSibling(t *testing.T) {
	sink := &recordingSink{}
	probe := newConcurrencyProbe(2, 3*time.Second)
	up := newRoutedResponder().
		route("delegate two things", nil, fanOutScript([2]string{"c1", "exploding task"}, [2]string{"c2", "calm task"})).
		route("exploding task", func(c context.Context) { probe.enter(c); panic("child boom") }, nil).
		route("calm task", probe.enter, contentScript("calm child done")).
		route("delegate two things", nil, contentScript("parent done"))

	a := fanOutAgent(t, sink, 2, up)
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Fatalf("parent status = %q, want the Exchange to survive a child panic", res.Status)
	}

	results := subAgentResults(sink.events)
	if len(results) != 2 {
		t.Fatalf("depth-0 tool results = %d, want 2", len(results))
	}
	if !results[0].IsError || !strings.Contains(results[0].Content, "panicked") {
		t.Errorf("panicking child's result = %+v, want a recovered-panic error result", results[0])
	}
	if results[1].IsError || !strings.Contains(results[1].Content, "calm child done") {
		t.Errorf("sibling's result = %+v, want it intact", results[1])
	}
	if !hasEvent[domain.ErrorEvent](sink.events) {
		t.Error("no ErrorEvent surfaced for the recovered child panic")
	}
}

// linearSink is recordingSink with a claim attached: it appends WITHOUT a lock of its own, so
// `go test -race` fails the instant the engine lets two emitters overlap, and it counts overlaps
// explicitly so the failure reads as a broken guarantee rather than a raw race dump.
type linearSink struct {
	events   []domain.Event
	inFlight atomic.Int32
	overlaps atomic.Int32
}

func (s *linearSink) Emit(e domain.Event) {
	if s.inFlight.Add(1) != 1 {
		s.overlaps.Add(1)
	}
	s.events = append(s.events, e)
	s.inFlight.Add(-1)
}

// TestFanOut_SinkReceivesALinearStampedStream pins the EventSink guarantee: concurrent children
// emit through one serializing seam, so an unguarded host sink is still safe, and each child's
// events carry ITS spawning call-ID at depth 1 (item 3's stamp) — the identity that separates two
// braided sibling streams.
func TestFanOut_SinkReceivesALinearStampedStream(t *testing.T) {
	sink := &linearSink{}
	probe := newConcurrencyProbe(2, 3*time.Second)
	up := newRoutedResponder().
		route("delegate two things", nil, fanOutScript([2]string{"c1", "task one"}, [2]string{"c2", "task two"})).
		route("task one", probe.enter, contentScript("child one done")).
		route("task two", probe.enter, contentScript("child two done")).
		route("delegate two things", nil, contentScript("parent done"))

	a := fanOutAgent(t, sink, 2, up)
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if peak := probe.peakInFlight(); peak != 2 {
		t.Fatalf("peak children in flight = %d, want 2 (nothing concurrent was exercised)", peak)
	}
	if n := sink.overlaps.Load(); n != 0 {
		t.Errorf("%d overlapping Emit calls reached the sink; the engine must serialize emission", n)
	}

	spawns := map[string]string{}
	for _, e := range sink.events {
		me, ok := e.(domain.MessageEvent)
		if !ok || me.Depth != 1 {
			continue
		}
		spawns[me.Text] = me.CallID
	}
	if spawns["child one done"] != "c1" {
		t.Errorf("child one's message carried CallID %q, want c1", spawns["child one done"])
	}
	if spawns["child two done"] != "c2" {
		t.Errorf("child two's message carried CallID %q, want c2", spawns["child two done"])
	}
}

// TestPartitionDispatch_LeafToolsRunBeforeDelegations pins decision 11's ordering rule on its own:
// the split is a pure function of the reply, so the same reply orders the same way whatever the
// bound server's cap turns out to be.
func TestPartitionDispatch_LeafToolsRunBeforeDelegations(t *testing.T) {
	t.Parallel()
	calls := []domain.ToolCall{
		{ID: "s1", Tool: tools.SubAgentToolName},
		{ID: "w1", Tool: "write_file"},
		{ID: "s2", Tool: tools.SubAgentToolName},
		{ID: "r1", Tool: "read_file"},
	}
	leaves, delegations := partitionDispatch(calls)
	if len(leaves) != 2 || leaves[0].ID != "w1" || leaves[1].ID != "r1" {
		t.Errorf("leaves = %+v, want w1 then r1 in emitted order", leaves)
	}
	if len(delegations) != 2 || delegations[0].ID != "s1" || delegations[1].ID != "s2" {
		t.Errorf("delegations = %+v, want s1 then s2 in emitted order", delegations)
	}
}

// TestFanOutWidth_BoundsTheGroup pins the width rule: min(cap, group) at depth 0, and 1 —
// "serial" — for every other row.
func TestFanOutWidth_BoundsTheGroup(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		depth, cap  int
		delegations int
		want        int
	}{
		{"cap below the group size bounds it", 0, 2, 5, 2},
		{"group below the cap bounds it", 0, 4, 3, 3},
		{"cap 1 is serial", 0, 1, 4, 1},
		{"cap 0 (nothing resolved) is serial", 0, 0, 4, 1},
		{"a single delegation never pools", 0, 4, 1, 1},
		{"depth 1 is serial whatever the cap", 1, 4, 4, 1},
		{"depth 2 is serial whatever the cap", 2, 4, 4, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The latch is part of an Agent's construction (construct.go), so a hand-built one
			// gets an empty holder: nothing routed, the session cap above governs.
			a := &Agent{depth: tc.depth, parallelAgents: tc.cap, delegation: &delegationLatch{}}
			if got := a.fanOutWidth(tc.delegations); got != tc.want {
				t.Errorf("fanOutWidth(%d) at depth %d cap %d = %d, want %d",
					tc.delegations, tc.depth, tc.cap, got, tc.want)
			}
		})
	}
}

// seatCalls builds one sub_agent call per named seat — "" for a call that names none — so a row
// below can state a reply's SHAPE ("one here, two there") and nothing else about it.
func seatCalls(runOn ...string) []domain.ToolCall {
	calls := make([]domain.ToolCall, 0, len(runOn))
	for i, seat := range runOn {
		args := `{"task":"scout"}`
		if seat != "" {
			args = fmt.Sprintf(`{"task":"scout","run_on":%q}`, seat)
		}
		calls = append(calls, domain.ToolCall{
			ID:        fmt.Sprintf("c%d", i),
			Tool:      tools.SubAgentToolName,
			Arguments: json.RawMessage(args),
		})
	}
	return calls
}

// TestFanOutWidth_MixedSeatsTakeTheSmallerCap pins the seat-aware width (ADR 0069): a reply the
// model SPLIT across both Delegation seats is sized by the smaller of the two servers' caps,
// because one pool runs the whole group and a wider one would overrun whichever server the children
// in flight happen to be on. A reply that is not split keeps its own seat's cap: an all-session
// reply is sized by the session server even with a target latched (ADR 0069 decision 7), and every
// other unsplit reply — no target to split onto, no `run_on` on the menu, below depth 0 — falls
// through to fanOutWidth unchanged, which is what the rows without a mixed reply are here to prove.
func TestFanOutWidth_MixedSeatsTakeTheSmallerCap(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		depth      int
		sessionCap int
		target     *DelegationTarget
		seatChoice bool
		runOn      []string
		want       int
	}{
		{
			name:       "split: the target's narrower cap bounds the whole group",
			sessionCap: 4, target: &DelegationTarget{ParallelAgents: 3}, seatChoice: true,
			runOn: []string{"session", "sub-agents-server", "", ""}, want: 3,
		},
		{
			name:       "split: the session's narrower cap bounds it just the same",
			sessionCap: 2, target: &DelegationTarget{ParallelAgents: 5}, seatChoice: true,
			runOn: []string{"session", "sub-agents-server", "", ""}, want: 2,
		},
		{
			name:       "split: the group size still bounds the smaller cap",
			sessionCap: 5, target: &DelegationTarget{ParallelAgents: 4}, seatChoice: true,
			runOn: []string{"session", "sub-agents-server"}, want: 2,
		},
		{
			name:       "split: a smaller cap of 1 is serial",
			sessionCap: 1, target: &DelegationTarget{ParallelAgents: 3}, seatChoice: true,
			runOn: []string{"session", "sub-agents-server", ""}, want: 1,
		},
		{
			name:       "one seat: every call routed keeps the target's cap, not the smaller one",
			sessionCap: 2, target: &DelegationTarget{ParallelAgents: 3}, seatChoice: true,
			runOn: []string{"sub-agents-server", "sub-agents-server", "", ""}, want: 3,
		},
		{
			name:       "one seat: every call on the session seat keeps the SESSION's cap (ADR 0069 decision 7)",
			sessionCap: 2, target: &DelegationTarget{ParallelAgents: 3}, seatChoice: true,
			runOn: []string{"session", "session", "session"}, want: 2,
		},
		{
			name:       "one seat: every call explicitly on the target seat keeps the target's cap",
			sessionCap: 2, target: &DelegationTarget{ParallelAgents: 3}, seatChoice: true,
			runOn: []string{"sub-agents-server", "sub-agents-server", "sub-agents-server"}, want: 3,
		},
		{
			name:       "one seat: all-session with nothing latched is the session cap it always was",
			sessionCap: 2, target: nil, seatChoice: true,
			runOn: []string{"session", "session", "session"}, want: 2,
		},
		{
			name:       "one seat: all-session with seat choice disarmed runs on the target and keeps its cap",
			sessionCap: 2, target: &DelegationTarget{ParallelAgents: 3}, seatChoice: false,
			runOn: []string{"session", "session", "session"}, want: 3,
		},
		{
			name:       "nothing latched: there is no second seat to split onto",
			sessionCap: 3, target: nil, seatChoice: true,
			runOn: []string{"session", "sub-agents-server", "", ""}, want: 3,
		},
		{
			name:       "the plain tool never reads run_on, so no reply is ever split",
			sessionCap: 2, target: &DelegationTarget{ParallelAgents: 3}, seatChoice: false,
			runOn: []string{"session", "sub-agents-server", "", ""}, want: 3,
		},
		{
			name:       "a single delegation never pools, whatever it named",
			sessionCap: 4, target: &DelegationTarget{ParallelAgents: 3}, seatChoice: true,
			runOn: []string{"session"}, want: 1,
		},
		{
			name:  "depth 1 is serial, split or not",
			depth: 1, sessionCap: 4, target: &DelegationTarget{ParallelAgents: 3}, seatChoice: true,
			runOn: []string{"session", "sub-agents-server", ""}, want: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &Agent{depth: tc.depth, parallelAgents: tc.sessionCap, delegation: &delegationLatch{}}
			if tc.seatChoice {
				a.tools = seatChoiceRegistry(t)
			}
			a.SetDelegationTarget(tc.target)

			if got := a.fanOutWidthFor(seatCalls(tc.runOn...)); got != tc.want {
				t.Errorf("fanOutWidthFor(%v) at depth %d, session cap %d = %d, want %d",
					tc.runOn, tc.depth, tc.sessionCap, got, tc.want)
			}
		})
	}
}

// TestFanOutWidth_UnparseableSeatIsNotASplit: a call whose arguments do not parse, or which names a
// seat outside the enum, is refused by runSubAgent before it spawns — so it must not be able to make
// a group look split and shrink the pool the calls beside it actually use.
func TestFanOutWidth_UnparseableSeatIsNotASplit(t *testing.T) {
	t.Parallel()

	a := &Agent{parallelAgents: 4, delegation: &delegationLatch{}, tools: seatChoiceRegistry(t)}
	a.SetDelegationTarget(&DelegationTarget{ParallelAgents: 3})

	calls := seatCalls("banana", "", "")
	calls = append(calls, domain.ToolCall{
		ID:        "c3",
		Tool:      tools.SubAgentToolName,
		Arguments: json.RawMessage(`{"task":`),
	})

	if got := a.fanOutWidthFor(calls); got != 3 {
		t.Errorf("fanOutWidthFor = %d, want the routed group's 3 — a refused call split the reply", got)
	}
}

// ----------------------------------------------------------------------------
// Routed fan-out width (ADR 0045 §5 — the receiving server's cap)
// ----------------------------------------------------------------------------
//
// A routed delegation spends the SUB-AGENT server's slots, so that server's cap is the one that
// bounds the group. These tests pin the choice (which cap governs), the behaviour it buys (a
// session server pinned to serial still fans out three-wide onto a three-slot grunt box), the
// second reader that follows for free (the hook view a delegation-synthesizing Mechanism batches by), and the
// once-per-reply snapshot that keeps a group at one width while the target moves under it.

// TestDelegationCapPicksTheGoverningServer is the resolution rule alone: routed ⇒ the target's
// cap, unrouted ⇒ the session server's live cap, and the depth-0 eligibility rule ahead of both.
func TestDelegationCapPicksTheGoverningServer(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		depth      int
		sessionCap int
		target     *DelegationTarget
		want       int
	}{
		{"nothing latched: the session server's cap governs", 0, 3, nil, 3},
		{"routed: the target's cap governs", 0, 1, &DelegationTarget{ParallelAgents: 3}, 3},
		{"routed narrow: the target REPLACES a wider session cap", 0, 4, &DelegationTarget{ParallelAgents: 1}, 1},
		{"routed with nothing resolved: the serial floor, not the session's 4", 0, 4, &DelegationTarget{}, 1},
		{"depth 1 is serial whatever is latched", 1, 4, &DelegationTarget{ParallelAgents: 3}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &Agent{depth: tc.depth, parallelAgents: tc.sessionCap, delegation: &delegationLatch{}}
			a.SetDelegationTarget(tc.target)
			if got := a.delegationWidth(); got != tc.want {
				t.Errorf("delegationWidth() = %d, want %d", got, tc.want)
			}
		})
	}
}

// gruntUpstream is the Sub-agent server these tests route to: an OpenAI-compatible httptest
// endpoint answering every child with one canned reply. It is a real HTTP server because a routed
// child dials a provider client of its own (ADR 0045, subagent.go) rather than borrowing the
// parent's responder — so the only place to observe routed children at once is the wire.
//
// gate runs on net/http's goroutine before the reply streams, which is where these tests measure
// concurrency, exactly as routedResponder's gate does for an unrouted child.
func gruntUpstream(t *testing.T, gate func(context.Context), reply string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gate != nil {
			gate(r.Context())
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q},\"finish_reason\":null}]}\n\n", reply)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv
}

// gruntTarget is a usable Delegation target pointing at endpoint with the given fan-out width.
func gruntTarget(endpoint string, parallelAgents int) *DelegationTarget {
	return &DelegationTarget{
		Endpoint:       endpoint,
		Model:          "cheap-4b",
		ContextWindow:  32768,
		ParallelAgents: parallelAgents,
	}
}

// threeWayFanOutParent builds a parent whose reply delegates three tasks, at the given SESSION
// cap, and gates each unrouted child on gate. It returns the agent, submitted and ready to Run.
func threeWayFanOutParent(t *testing.T, sink domain.EventSink, sessionCap int, gate func(context.Context)) *Agent {
	t.Helper()
	return threeWayFanOutParentSeamed(t, sink, sessionCap, gate, nil)
}

// threeWayFanOutParentSeamed is threeWayFanOutParent with the host's Config.InterjectionPending
// seam wired to pending; nil leaves the seam absent, exactly as every fixture before it existed.
func threeWayFanOutParentSeamed(
	t *testing.T,
	sink domain.EventSink,
	sessionCap int,
	gate func(context.Context),
	pending func() bool,
) *Agent {
	t.Helper()
	up := newRoutedResponder().
		route("delegate three things", nil, fanOutScript(
			[2]string{"c1", "task one"}, [2]string{"c2", "task two"}, [2]string{"c3", "task three"})).
		route("task one", gate, contentScript("child one done")).
		route("task two", gate, contentScript("child two done")).
		route("task three", gate, contentScript("child three done")).
		route("delegate three things", nil, contentScript("parent done"))

	cfg := subAgentConfig(sink, domain.ModeAskBefore)
	cfg.ParallelAgents = sessionCap
	cfg.InterjectionPending = pending
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "delegate three things"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	return a
}

// TestFanOut_RoutedWidthComesFromTheTargetCap is the item's core acceptance, and the contrast is
// the whole point: the SESSION server is pinned to 1, which TestFanOut_CapOneKeepsTheGroupSerial
// proves is serial with nothing latched — yet a three-slot Sub-agent server runs all three
// children at once, because the slots being spent are its own (ADR 0045 §5).
func TestFanOut_RoutedWidthComesFromTheTargetCap(t *testing.T) {
	sink := &recordingSink{}
	probe := newConcurrencyProbe(3, 3*time.Second)
	srv := gruntUpstream(t, probe.enter, "grunt child done")

	a := threeWayFanOutParent(t, sink, 1 /* the session server is serial */, nil)
	a.SetDelegationTarget(gruntTarget(srv.URL, 3))

	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Fatalf("parent status = %q, want the Exchange to complete", res.Status)
	}

	if peak := probe.peakInFlight(); peak != 3 {
		t.Errorf("peak children in flight = %d, want 3 (the session server's cap of 1 governed a routed group)", peak)
	}
	results := subAgentResults(sink.events)
	if len(results) != 3 {
		t.Fatalf("depth-0 tool results = %d, want 3", len(results))
	}
	if results[0].CallID != "c1" || results[1].CallID != "c2" || results[2].CallID != "c3" {
		t.Errorf("results committed as %q,%q,%q; want the emitted call order c1,c2,c3",
			results[0].CallID, results[1].CallID, results[2].CallID)
	}
	for i, r := range results {
		if !strings.Contains(r.Content, "grunt child done") {
			t.Errorf("result %d = %q, want the Sub-agent server's reply (the child ran on the parent's Upstream)", i, r.Content)
		}
	}
}

// TestFanOut_LatchClearedMidGroupKeepsTheGroupWidth pins the once-per-reply snapshot: the width is
// resolved before the first child spawns and travels as an argument, so a target cleared while the
// group is in flight cannot narrow the running pool. The children AFTER the clear spawn unrouted
// and answer on the parent's own Upstream — which is exactly why the probe is shared by both
// servers: where each child lands is a race, that all three ran AT ONCE is not.
func TestFanOut_LatchClearedMidGroupKeepsTheGroupWidth(t *testing.T) {
	sink := &recordingSink{}
	probe := newConcurrencyProbe(3, 3*time.Second)

	var parent atomic.Pointer[Agent]
	var once sync.Once
	// The first routed child to reach the Sub-agent server drops the target — a beat observing the
	// grunt box gone, landing squarely mid-group — and only then joins the rendezvous.
	srv := gruntUpstream(t, func(ctx context.Context) {
		once.Do(func() {
			if p := parent.Load(); p != nil {
				p.SetDelegationTarget(nil)
			}
		})
		probe.enter(ctx)
	}, "grunt child done")

	a := threeWayFanOutParent(t, sink, 1 /* falling back to this would be serial */, probe.enter)
	parent.Store(a)
	a.SetDelegationTarget(gruntTarget(srv.URL, 3))

	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if peak := probe.peakInFlight(); peak != 3 {
		t.Errorf("peak children in flight = %d, want 3 (the group re-read the latch and narrowed mid-flight)", peak)
	}
	if results := subAgentResults(sink.events); len(results) != 3 {
		t.Errorf("depth-0 tool results = %d, want 3", len(results))
	}
	if a.delegationTarget() != nil {
		t.Error("target still latched after the mid-group clear")
	}
}

// TestRoutedWidthReachesTheHookView pins the second reader (ADR 0039's one width everywhere): the
// width stamped onto the hook-facing view is the ROUTED one, so a delegation-synthesizing
// Mechanism's min(cap, remaining) batch follows the Sub-agent server's cap with no rule of its own — and falls
// back to the session server's the moment the target is gone.
func TestRoutedWidthReachesTheHookView(t *testing.T) {
	t.Parallel()

	cfg := subAgentConfig(&recordingSink{}, domain.ModeAskBefore)
	cfg.ParallelAgents = 1
	a, err := newAgent(cfg, &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	a.SetDelegationTarget(gruntTarget("http://grunt.local:1111", 3))
	req, _ := a.buildRequest(0)
	if got := req.View().ParallelAgents(); got != 3 {
		t.Errorf("routed request view width = %d, want the target's 3", got)
	}
	if got := a.loopView(0).ParallelAgents(); got != 3 {
		t.Errorf("routed tool-stage view width = %d, want the target's 3", got)
	}

	a.SetDelegationTarget(nil)
	req, _ = a.buildRequest(0)
	if got := req.View().ParallelAgents(); got != 1 {
		t.Errorf("unrouted request view width = %d, want the session server's 1", got)
	}
}

// ----------------------------------------------------------------------------
// A pending queued message pre-empts the delegations not yet started
// ----------------------------------------------------------------------------
//
// The host answers Config.InterjectionPending; the pool reads it once per slot at dequeue and
// dispatchSerially once per delegation before it resolves. Nothing here drains a message: the
// tests flip a flag and read what the group committed.

// phasesFor returns the lifecycle phases the sink saw for one delegation, in emission order.
func phasesFor(events []domain.Event, callID string) []domain.SubAgentPhaseEvent {
	var out []domain.SubAgentPhaseEvent
	for _, pe := range subAgentPhases(events) {
		if pe.CallID == callID {
			out = append(out, pe)
		}
	}
	return out
}

// assertSkippedDelegation pins the whole account of a pre-empted delegation: its committed
// result is the exact skip content as an error, its bracket is ONE finished phase carrying that
// result and no started phase, and no audit record was booked for a child that never ran.
func assertSkippedDelegation(t *testing.T, events []domain.Event, result domain.ToolResult) {
	t.Helper()
	if !result.IsError || result.Content != skippedDelegationContent {
		t.Errorf("%s result = %+v, want the exact skip content as an error result", result.CallID, result)
	}
	phases := phasesFor(events, result.CallID)
	if len(phases) != 1 || phases[0].Phase != domain.SubAgentFinished {
		t.Fatalf("%s phases = %+v, want exactly one finished phase and no started one", result.CallID, phases)
	}
	if phases[0].Cancelled || phases[0].Result.Content != skippedDelegationContent {
		t.Errorf("%s finished phase = %+v, want the skip result and not Cancelled", result.CallID, phases[0])
	}
	for _, ae := range auditEvents(events) {
		if ae.CallID == result.CallID {
			t.Errorf("%s was audit-recorded (%+v); a child that never ran books no record", result.CallID, ae)
		}
	}
}

// TestFanOut_PendingInterjectionSkipsTheQueuedChildren is the item's core: under a cap of 2 a
// three-way group has one child waiting for a worker; the message arrives while the first two run,
// so the third is never started — it commits the skip result in call order, with a finished phase
// alone — while both running children finish and commit their real results, and the Turn completes.
func TestFanOut_PendingInterjectionSkipsTheQueuedChildren(t *testing.T) {
	sink := &recordingSink{}
	var pending atomic.Bool
	arrived := make(chan struct{}, 3)
	release := make(chan struct{})
	// The gate holds the two running children until the test has staged the message, so the
	// third slot can only be dequeued once the seam already answers true.
	gate := func(ctx context.Context) {
		arrived <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
	}
	go func() {
		for i := 0; i < 2; i++ {
			<-arrived
		}
		pending.Store(true)
		close(release)
	}()

	a := threeWayFanOutParentSeamed(t, sink, 2, gate, pending.Load)
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Fatalf("parent status = %q, want the Exchange to complete", res.Status)
	}

	results := subAgentResults(sink.events)
	if len(results) != 3 {
		t.Fatalf("depth-0 tool results = %d, want 3 (the skipped slot still commits)", len(results))
	}
	if results[0].CallID != "c1" || results[1].CallID != "c2" || results[2].CallID != "c3" {
		t.Errorf("results committed as %q,%q,%q; want the emitted call order c1,c2,c3",
			results[0].CallID, results[1].CallID, results[2].CallID)
	}
	if results[0].IsError || !strings.Contains(results[0].Content, "child one done") {
		t.Errorf("c1 result = %+v, want the running child's real result", results[0])
	}
	if results[1].IsError || !strings.Contains(results[1].Content, "child two done") {
		t.Errorf("c2 result = %+v, want the running child's real result", results[1])
	}
	assertSkippedDelegation(t, sink.events, results[2])
	for _, id := range []string{"c1", "c2"} {
		if phases := phasesFor(sink.events, id); len(phases) != 2 || phases[0].Phase != domain.SubAgentStarted {
			t.Errorf("%s phases = %+v, want a started/finished pair for a child that ran", id, phases)
		}
	}
}

// TestFanOut_PendingInterjectionNeverTouchesARunningChild pins the boundary of the rule: the seam
// flips only after every slot has been dequeued and started, so every child runs to its real
// result — a message never cancels a child, it only pre-empts one that has not begun.
func TestFanOut_PendingInterjectionNeverTouchesARunningChild(t *testing.T) {
	sink := &recordingSink{}
	var pending atomic.Bool
	probe := newConcurrencyProbe(3, 3*time.Second)
	// enter rendezvouses all three children, so by the time any of them flips the seam all three
	// slots have been dequeued and checked.
	gate := func(ctx context.Context) {
		probe.enter(ctx)
		pending.Store(true)
	}

	a := threeWayFanOutParentSeamed(t, sink, 3, gate, pending.Load)
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if peak := probe.peakInFlight(); peak != 3 {
		t.Fatalf("peak children in flight = %d, want 3 (the fixture must start all three before the flip)", peak)
	}

	results := subAgentResults(sink.events)
	if len(results) != 3 {
		t.Fatalf("depth-0 tool results = %d, want 3", len(results))
	}
	for i, want := range []string{"child one done", "child two done", "child three done"} {
		if results[i].IsError || !strings.Contains(results[i].Content, want) {
			t.Errorf("%s result = %+v, want the real result %q (a running child was pre-empted)", results[i].CallID, results[i], want)
		}
	}
}

// TestFanOut_NilSeamRunsEverySlot pins the default: a Config without the delegate never pre-empts,
// so a three-way group under a cap of 2 runs and commits all three children exactly as it did
// before the seam existed.
func TestFanOut_NilSeamRunsEverySlot(t *testing.T) {
	sink := &recordingSink{}
	a := threeWayFanOutParent(t, sink, 2, nil)
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	results := subAgentResults(sink.events)
	if len(results) != 3 {
		t.Fatalf("depth-0 tool results = %d, want 3", len(results))
	}
	for i, want := range []string{"child one done", "child two done", "child three done"} {
		if results[i].IsError || !strings.Contains(results[i].Content, want) {
			t.Errorf("%s result = %+v, want the real result %q", results[i].CallID, results[i], want)
		}
		if phases := phasesFor(sink.events, results[i].CallID); len(phases) != 2 {
			t.Errorf("%s phases = %+v, want a started/finished pair", results[i].CallID, phases)
		}
	}
}

// TestDispatchSerially_PendingInterjectionSkipsTheNextDelegation covers the serial path, which a
// cap of 1 keeps every group on: the message arrives while the first child runs, the second is
// skipped with the same result and phase the pool gives, and a leaf tool the parent asks for next —
// with the seam still true — runs regardless: only delegations are ever pre-empted.
func TestDispatchSerially_PendingInterjectionSkipsTheNextDelegation(t *testing.T) {
	sink := &recordingSink{}
	var pending atomic.Bool
	looked := 0
	up := newRoutedResponder().
		route("delegate two things", nil, fanOutScript([2]string{"c1", "task one"}, [2]string{"c2", "task two"})).
		route("task one", func(context.Context) { pending.Store(true) }, contentScript("child one done")).
		route("task two", nil, contentScript("child two done")).
		route("delegate two things", nil, toolCallScript("t1", "look", `{}`)).
		route("delegate two things", nil, contentScript("parent done"))

	cfg := subAgentConfig(sink, domain.ModeAskBefore, fakeTool{name: "look", readOnly: true, ran: &looked, result: "looked"})
	cfg.ParallelAgents = 1
	cfg.InterjectionPending = pending.Load
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "delegate two things"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Fatalf("parent status = %q, want the Exchange to complete", res.Status)
	}

	results := subAgentResults(sink.events)
	if len(results) != 3 {
		t.Fatalf("depth-0 tool results = %d, want 3 (two delegations, then the leaf)", len(results))
	}
	if results[0].CallID != "c1" || results[0].IsError || !strings.Contains(results[0].Content, "child one done") {
		t.Errorf("c1 result = %+v, want the running child's real result", results[0])
	}
	if results[1].CallID != "c2" {
		t.Fatalf("second result = %+v, want c2", results[1])
	}
	assertSkippedDelegation(t, sink.events, results[1])
	if looked != 1 || results[2].CallID != "t1" || results[2].IsError {
		t.Errorf("leaf ran %d times, result %+v; want it to run once with the seam still true", looked, results[2])
	}
}

// ----------------------------------------------------------------------------
// The delegation-width line (plan 2026-09-14 - 04 item 4)
// ----------------------------------------------------------------------------
//
// A group wider than the width it ran under states that width ONCE, as the last line of its last
// committed result's body. The tests below read the committed results off the sink exactly as the
// parent model reads them off its history: the line is a model-facing fact, so its placement is
// asserted byte for byte.

// assertNoWidthNote fails when any of results carries a delegation-width line.
func assertNoWidthNote(t *testing.T, results []domain.ToolResult) {
	t.Helper()
	for _, r := range results {
		if strings.Contains(r.Content, "of this group's") {
			t.Errorf("%s result = %q, want no delegation-width line", r.CallID, r.Content)
		}
	}
}

// TestFanOut_LastResultStatesTheWidth is the item's core: three delegations under a width of 2 put
// the exact width line at the end of the THIRD result's body and nowhere else.
func TestFanOut_LastResultStatesTheWidth(t *testing.T) {
	sink := &recordingSink{}

	a := threeWayFanOutParent(t, sink, 2, nil)
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	results := subAgentResults(sink.events)
	if len(results) != 3 {
		t.Fatalf("depth-0 tool results = %d, want 3", len(results))
	}
	const want = "[1 of this group's 3 delegations ran after the others finished — the width is 2]"
	if got := results[2].Content; got != "child three done\n"+want {
		t.Errorf("last result = %q, want the child's answer then the exact width line %q", got, want)
	}
	assertNoWidthNote(t, results[:2])
}

// TestFanOut_GroupInsideTheWidthStatesNothing is the floor: a group no wider than its width had
// nothing wait, so no result carries the line and each one is the child's answer alone.
func TestFanOut_GroupInsideTheWidthStatesNothing(t *testing.T) {
	sink := &recordingSink{}

	a := threeWayFanOutParent(t, sink, 3, nil)
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	results := subAgentResults(sink.events)
	if len(results) != 3 {
		t.Fatalf("depth-0 tool results = %d, want 3", len(results))
	}
	assertNoWidthNote(t, results)
}

// TestFanOut_WidthLineStatesTheRoutedWidth pins W as the width the group ACTUALLY ran under: the
// session server is pinned to 4, which alone would fit the group, but a latched two-slot Sub-agent
// server replaces that cap (delegationCap), so the line says 2.
func TestFanOut_WidthLineStatesTheRoutedWidth(t *testing.T) {
	sink := &recordingSink{}
	srv := gruntUpstream(t, nil, "grunt child done")

	a := threeWayFanOutParent(t, sink, 4 /* would fit all three on the session server */, nil)
	a.SetDelegationTarget(gruntTarget(srv.URL, 2))
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	results := subAgentResults(sink.events)
	if len(results) != 3 {
		t.Fatalf("depth-0 tool results = %d, want 3", len(results))
	}
	const want = "[1 of this group's 3 delegations ran after the others finished — the width is 2]"
	if last := lastLine(results[2].Content); last != want {
		t.Errorf("last result's last line = %q, want the routed width stated: %q", last, want)
	}
	assertNoWidthNote(t, results[:2])
}

// TestFanOut_WidthLineSitsAboveTheSteeredTrailer is the collision ADR 0063 D3 settles: the human
// steers the LAST child while it runs, so its result carries the parent notice — which stays the
// result's final line — and the width line is the last line of the body, immediately above it.
func TestFanOut_WidthLineSitsAboveTheSteeredTrailer(t *testing.T) {
	sink := &recordingSink{}
	var parent atomic.Pointer[Agent]
	looked := 0
	// The third child's first Turn is a tool call so a second Turn follows, and the remark is
	// interjected before that first Turn streams — the shape runSteeredDelegation uses — so it
	// LANDS at the child's next boundary. Its answer is then routed by the landed remark, which is
	// the child's last user message from there on.
	steer := func(context.Context) {
		if err := parent.Load().InterjectChild("c3", domain.UserInput{Text: "focus on the tests"}); err != nil {
			t.Errorf("InterjectChild while the child runs: %v", err)
		}
	}
	up := newRoutedResponder().
		route("delegate three things", nil, fanOutScript(
			[2]string{"c1", "task one"}, [2]string{"c2", "task two"}, [2]string{"c3", "task three"})).
		route("task one", nil, contentScript("child one done")).
		route("task two", nil, contentScript("child two done")).
		route("task three", steer, toolCallScript("t3", "look", `{}`)).
		route("focus on the tests", nil, contentScript("child three done")).
		route("delegate three things", nil, contentScript("parent done"))
	cfg := subAgentConfig(sink, domain.ModeAskBefore, fakeTool{name: "look", readOnly: true, ran: &looked, result: "looked"})
	cfg.ParallelAgents = 2
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	parent.Store(a)
	if err := a.Submit(domain.UserInput{Text: "delegate three things"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	results := subAgentResults(sink.events)
	if len(results) != 3 {
		t.Fatalf("depth-0 tool results = %d, want 3", len(results))
	}
	const width = "[1 of this group's 3 delegations ran after the others finished — the width is 2]"
	want := "child three done\n" + width + "\n\n" + userSteeredTrailerSingular
	if got := results[2].Content; got != want {
		t.Errorf("steered last result = %q, want the body ending with the width line under the parent notice: %q", got, want)
	}
	assertNoWidthNote(t, results[:2])
}

// TestFanOut_PreemptedGroupStatesNoWidth pins the gate on the line: a group whose third slot was
// skipped for a pending interjection did not run three delegations, so NO result carries the
// width line — the skipped slot commits exactly the skip result, as it always has.
func TestFanOut_PreemptedGroupStatesNoWidth(t *testing.T) {
	sink := &recordingSink{}
	var pending atomic.Bool
	arrived := make(chan struct{}, 3)
	release := make(chan struct{})
	gate := func(ctx context.Context) {
		arrived <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
	}
	go func() {
		for i := 0; i < 2; i++ {
			<-arrived
		}
		pending.Store(true)
		close(release)
	}()

	a := threeWayFanOutParentSeamed(t, sink, 2, gate, pending.Load)
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	results := subAgentResults(sink.events)
	if len(results) != 3 {
		t.Fatalf("depth-0 tool results = %d, want 3 (the skipped slot still commits)", len(results))
	}
	assertSkippedDelegation(t, sink.events, results[2])
	assertNoWidthNote(t, results)
}

// TestSplitUserSteeredTrailer pins the recognition withBodyNote rests on: only the exact notice,
// set apart by its blank line, is split off — a body that merely mentions the words stays whole.
func TestSplitUserSteeredTrailer(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		content string
		body    string
		trailer string
	}{
		{"no trailer", "child done", "child done", ""},
		{"singular", "child done\n\n" + userSteeredTrailerSingular, "child done", "\n\n" + userSteeredTrailerSingular},
		{"plural", "child done\n\n" + userSteeredTrailer(12), "child done", "\n\n" + userSteeredTrailer(12)},
		{"mentioned in the body", "(the user sent 2 messages to this sub-agent while it ran) is what I read", "(the user sent 2 messages to this sub-agent while it ran) is what I read", ""},
		{"not set apart", "child done\n" + userSteeredTrailerSingular, "child done\n" + userSteeredTrailerSingular, ""},
		{"no count", "child done\n\n(the user sent  messages to this sub-agent while it ran)", "child done\n\n(the user sent  messages to this sub-agent while it ran)", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body, trailer := splitUserSteeredTrailer(tc.content)

			if body != tc.body || trailer != tc.trailer {
				t.Errorf("splitUserSteeredTrailer(%q) = %q, %q; want %q, %q", tc.content, body, trailer, tc.body, tc.trailer)
			}
		})
	}
}
