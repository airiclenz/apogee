package agent

import (
	"context"
	"errors"
	"iter"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// ----------------------------------------------------------------------------
// Child addressing (ADR 0063) — InterjectChild, the mailbox drain, the delivery event
// ----------------------------------------------------------------------------
//
// A delegation runs synchronously inside the parent's Turn, so the ONLY window a test has to act
// "while the child runs" is inside the responder's Stream for one of the child's own Turns — the
// child is registered before its Run starts, so it is addressable from there.

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
	a, err := newAgent(baseConfig(&recordingSink{}), &scriptedResponder{})
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
	responder.before = func(call int) {
		if call != 1 {
			return
		}
		var ok bool
		if child, ok = a.children.lookup("c1"); !ok {
			t.Error("the running child is not registered under its spawn call-ID")
		}
		if err := a.InterjectChild("c1", domain.UserInput{Text: remark}); err != nil {
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

// TestInterjectChild_TopLevelRunNeverDrains pins the depth > 0 scope of the drain: ADR 0063
// supersedes ADR 0025's rejected Run-side drain for CHILDREN only, so a top-level Run leaves its
// mailbox alone and emits no delivery event — an embedder's interjection stays its own Interject
// call between the Steps it drives.
func TestInterjectChild_TopLevelRunNeverDrains(t *testing.T) {
	sink := &recordingSink{}
	responder := &scriptedResponder{scripts: [][]provider.Delta{
		toolCallScript("t1", "look", `{}`),
		contentScript("done"),
	}}
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
	responder.before = func(call int) {
		if call != 1 {
			return
		}
		if err := a.InterjectChild("c1", domain.UserInput{Text: remark}); err != nil {
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

	// The child is gone, so the same id is refused from here on.
	if err := a.InterjectChild("c1", domain.UserInput{Text: remark}); !errors.Is(err, domain.ErrNoSuchChild) {
		t.Errorf("InterjectChild after the child finished = %v, want ErrNoSuchChild", err)
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
	responder.before = func(call int) {
		if call != 2 {
			return
		}
		if err := a.InterjectChild("c2", domain.UserInput{Text: remark}); err != nil {
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
