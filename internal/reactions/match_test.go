package reactions

import (
	"fmt"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// allSubscribed is the set a run with a Reaction on every event is built over.
func allSubscribed() map[Event]bool {
	return SubscribedEvents([]domain.Reaction{{On: Events()}})
}

// writeTargetFor answers like tools.WorkspaceWriteTarget over a registry holding only apogee's
// own write tools: a write reports its destination, a reader reports nothing.
func writeTargetFor(t *testing.T, calls *int) WriteTarget {
	t.Helper()
	writers := map[string]bool{"write_file": true, "edit_file": true, "delete_file": true, "move_file": true}
	return func(call domain.ToolCall) (string, bool) {
		*calls++
		if !writers[call.Tool] {
			return "", false
		}
		return "/work/repo/" + call.ID + ".txt", true
	}
}

// TestMatchTurnBoundary — a Depth-0 boundary fires turn-finished, and a Depth-0 boundary that
// CLOSED its Exchange fires both, boundary first. A sub-agent's Turn fires neither.
func TestMatchTurnBoundary(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		event domain.TurnEvent
		want  []Event
	}{
		{
			name:  "a depth-1 turn yields nothing",
			event: domain.TurnEvent{EventBase: domain.EventBase{Depth: 1, Turn: 2}, Status: domain.StatusExchangeComplete},
			want:  nil,
		},
		{
			name:  "a depth-0 turn-complete yields the boundary alone",
			event: domain.TurnEvent{EventBase: domain.EventBase{Turn: 2}, Status: domain.StatusTurnComplete},
			want:  []Event{TurnFinished},
		},
		{
			name: "a depth-0 exchange-complete yields both, boundary first",
			event: domain.TurnEvent{
				EventBase: domain.EventBase{Turn: 3}, Status: domain.StatusExchangeComplete,
				Faulted: true, StepCapped: true,
			},
			want: []Event{TurnFinished, ExchangeFinished},
		},
		{
			name:  "a cancelled depth-0 turn yields the boundary alone",
			event: domain.TurnEvent{EventBase: domain.EventBase{Turn: 4}, Status: domain.StatusCancelled},
			want:  []Event{TurnFinished},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got := newMatcher(allSubscribed(), nil).match(c.event)

			assertEvents(t, got, c.want)
			for _, f := range got {
				if f.Payload.Status != string(c.event.Status) {
					t.Errorf("payload status = %q, want %q", f.Payload.Status, c.event.Status)
				}
				if f.Payload.Faulted != c.event.Faulted || f.Payload.StepCapped != c.event.StepCapped {
					t.Errorf("payload faulted/step-capped = %v/%v, want %v/%v",
						f.Payload.Faulted, f.Payload.StepCapped, c.event.Faulted, c.event.StepCapped)
				}
				if f.Payload.Turn != c.event.Turn || f.Payload.Depth != c.event.Depth {
					t.Errorf("payload turn/depth = %d/%d, want %d/%d",
						f.Payload.Turn, f.Payload.Depth, c.event.Turn, c.event.Depth)
				}
			}
		})
	}
}

// TestMatchTurnBoundaryHonoursTheSubscribedSet — a run whose only Reaction wants the closure never
// hears about the plain boundary, and the reverse.
func TestMatchTurnBoundaryHonoursTheSubscribedSet(t *testing.T) {
	t.Parallel()

	closed := domain.TurnEvent{EventBase: domain.EventBase{Turn: 1}, Status: domain.StatusExchangeComplete}

	onlyExchange := newMatcher(map[Event]bool{ExchangeFinished: true}, nil).match(closed)
	onlyTurn := newMatcher(map[Event]bool{TurnFinished: true}, nil).match(closed)

	assertEvents(t, onlyExchange, []Event{ExchangeFinished})
	assertEvents(t, onlyTurn, []Event{TurnFinished})
}

// TestMatchApproval — both phases of one Approval are events: the raised gate, and the verdict it
// reached, which the decided payload carries as "decision" (ADR 0076 A6).
func TestMatchApproval(t *testing.T) {
	t.Parallel()

	request := domain.ApprovalRequest{
		Tool: "terminal", Reason: "write", Remedy: "run `apogee doctor`",
		SubAgentName: "docs sweep", Scope: "reads the package directory",
	}

	requested := newMatcher(allSubscribed(), nil).match(domain.ApprovalEvent{
		EventBase: domain.EventBase{Depth: 1, Turn: 4, CallID: "call-2"},
		Phase:     domain.ApprovalRequested, Request: request,
	})
	decided := newMatcher(allSubscribed(), nil).match(domain.ApprovalEvent{
		EventBase: domain.EventBase{Depth: 1, Turn: 4, CallID: "call-2"},
		Phase:     domain.ApprovalDecided, Request: request, Decision: domain.ApprovalAllow,
	})

	assertEvents(t, requested, []Event{ApprovalRequested})
	assertEvents(t, decided, []Event{ApprovalDecided})
	if verdict := decided[0].Payload.Decision; verdict != string(domain.ApprovalAllow) {
		t.Errorf("approval-decided payload decision = %q, want %q", verdict, domain.ApprovalAllow)
	}
	if verdict := requested[0].Payload.Decision; verdict != "" {
		t.Errorf("approval-requested payload decision = %q, want it empty — no verdict has been reached", verdict)
	}
	got := requested[0].Payload
	if got.Tool != "terminal" || got.Reason != "write" || got.Remedy != "run `apogee doctor`" ||
		got.SubAgentName != "docs sweep" || got.Scope != "reads the package directory" {
		t.Errorf("approval payload = %+v, want the request's fields carried through", got)
	}
	if got.Depth != 1 || got.Turn != 4 || got.CallID != "call-2" {
		t.Errorf("approval payload identity = %d/%d/%q, want 1/4/\"call-2\"", got.Depth, got.Turn, got.CallID)
	}
}

// TestMatchError — a recovered fault at any depth.
func TestMatchError(t *testing.T) {
	t.Parallel()

	got := newMatcher(allSubscribed(), nil).match(domain.ErrorEvent{
		EventBase: domain.EventBase{Depth: 2, Turn: 5, CallID: "call-9"},
		Source:    "terminal", Err: "exit status 1",
	})

	assertEvents(t, got, []Event{Error})
	if got[0].Payload.Source != "terminal" || got[0].Payload.Error != "exit status 1" {
		t.Errorf("error payload = %+v, want the source and message carried through", got[0].Payload)
	}
	if got[0].Payload.Depth != 2 {
		t.Errorf("error payload depth = %d, want 2 — an error fires at any depth", got[0].Payload.Depth)
	}
}

// TestMatchFileChanged walks a write from its call to its result, which is the only place the
// tool name, the destination and the success are all known.
func TestMatchFileChanged(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		tool    string
		isError bool
		want    []Event
	}{
		{name: "a successful write fires", tool: "write_file", want: []Event{FileChanged}},
		{name: "a delete reports its destination", tool: "delete_file", want: []Event{FileChanged}},
		{name: "an erroring write fires nothing", tool: "write_file", isError: true, want: nil},
		{name: "a read never fires", tool: "read_file", want: nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			calls := 0
			m := newMatcher(allSubscribed(), writeTargetFor(t, &calls))
			call := domain.ToolCall{ID: "call-7", Tool: c.tool}

			assertEvents(t, m.match(domain.ToolCallEvent{Call: call}), nil)
			got := m.match(domain.ToolResultEvent{
				EventBase: domain.EventBase{Depth: 1, Turn: 2},
				Result:    domain.ToolResult{CallID: call.ID, IsError: c.isError},
			})

			assertEvents(t, got, c.want)
			if len(got) == 1 {
				if got[0].Payload.Tool != c.tool || got[0].Payload.Path != "/work/repo/call-7.txt" {
					t.Errorf("file-changed payload = %+v, want the tool and its destination", got[0].Payload)
				}
			}
			if len(m.pending) != 0 {
				t.Errorf("%d pending entries remain, want the entry dropped on its result", len(m.pending))
			}
		})
	}
}

// TestMatchFileChangedIgnoresAnUnknownResult — a result whose call was never remembered (a read,
// a call from before the Reaction set was replaced) closes nothing and fires nothing.
func TestMatchFileChangedIgnoresAnUnknownResult(t *testing.T) {
	t.Parallel()

	calls := 0
	m := newMatcher(allSubscribed(), writeTargetFor(t, &calls))

	got := m.match(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "never-seen"}})

	assertEvents(t, got, nil)
}

// TestMatchPendingWritesAreBounded — a call whose result never arrives would otherwise leak an
// entry for the life of the session, so the map refuses to grow past the cap.
func TestMatchPendingWritesAreBounded(t *testing.T) {
	t.Parallel()

	calls := 0
	m := newMatcher(allSubscribed(), writeTargetFor(t, &calls))

	for i := 0; i < maxPendingWrites+50; i++ {
		m.match(domain.ToolCallEvent{Call: domain.ToolCall{ID: fmt.Sprintf("call-%d", i), Tool: "write_file"}})
	}

	if len(m.pending) != maxPendingWrites {
		t.Errorf("pending = %d entries, want it capped at %d", len(m.pending), maxPendingWrites)
	}
}

// TestMatchNeverAsksWriteTargetWhenFileChangedIsUnsubscribed is the item's regression guard: a
// matcher built over a set that does not hold file-changed does no work at all for a write —
// no WriteTarget call, no pending entry — so an unsubscribed run pays nothing for the feature.
func TestMatchNeverAsksWriteTargetWhenFileChangedIsUnsubscribed(t *testing.T) {
	t.Parallel()

	calls := 0
	m := newMatcher(map[Event]bool{TurnFinished: true, Error: true}, writeTargetFor(t, &calls))
	call := domain.ToolCall{ID: "call-7", Tool: "write_file"}

	assertEvents(t, m.match(domain.ToolCallEvent{Call: call}), nil)
	assertEvents(t, m.match(domain.ToolResultEvent{Result: domain.ToolResult{CallID: call.ID}}), nil)

	if calls != 0 {
		t.Errorf("WriteTarget was invoked %d times, want 0 when no reaction subscribes to file-changed", calls)
	}
	if len(m.pending) != 0 {
		t.Errorf("pending = %d entries, want none when no reaction subscribes to file-changed", len(m.pending))
	}
}

// TestMatchWithNoActiveHookIsANoOpForEveryEvent — the ordinary case for a user who configured no
// Reactions at all: the decorator is in the sink chain and costs one map length check per event.
func TestMatchWithNoActiveHookIsANoOpForEveryEvent(t *testing.T) {
	t.Parallel()

	calls := 0
	m := newMatcher(SubscribedEvents(nil), writeTargetFor(t, &calls))
	events := []domain.Event{
		domain.TurnEvent{Status: domain.StatusExchangeComplete},
		domain.ApprovalEvent{Phase: domain.ApprovalRequested},
		domain.ErrorEvent{Source: "loop", Err: "boom"},
		domain.ToolCallEvent{Call: domain.ToolCall{ID: "call-7", Tool: "write_file"}},
		domain.ToolResultEvent{Result: domain.ToolResult{CallID: "call-7"}},
		domain.MessageEvent{Text: "hello"},
	}

	for _, ev := range events {
		if got := m.match(ev); len(got) != 0 {
			t.Errorf("match(%T) = %v, want nothing with no active reaction", ev, got)
		}
	}

	if calls != 0 || len(m.pending) != 0 {
		t.Errorf("WriteTarget calls = %d, pending = %d, want 0/0", calls, len(m.pending))
	}
}

// TestMatchIgnoresEveryOtherVariant — the vocabulary is closed; an event outside it produces
// nothing even when every Reaction event is subscribed.
func TestMatchIgnoresEveryOtherVariant(t *testing.T) {
	t.Parallel()

	m := newMatcher(allSubscribed(), nil)

	for _, ev := range []domain.Event{
		domain.MessageEvent{Text: "hello"},
		domain.PruneEvent{},
		domain.ReactionFiredEvent{
			Reaction: "tool-call-repair",
			Origin:   domain.OriginEngine,
			Moment:   domain.MomentPostResponse,
			Action:   "retry",
		},
	} {
		if got := m.match(ev); len(got) != 0 {
			t.Errorf("match(%T) = %v, want nothing — it is not a reaction event", ev, got)
		}
	}
}

// assertEvents compares the events a match produced, in order.
func assertEvents(t *testing.T, got []firing, want []Event) {
	t.Helper()
	if len(got) != len(want) {
		names := make([]Event, 0, len(got))
		for _, f := range got {
			names = append(names, f.Event)
		}
		t.Fatalf("match produced %v, want %v", names, want)
	}
	for i := range want {
		if got[i].Event != want[i] {
			t.Errorf("match produced [%d] = %q, want %q", i, got[i].Event, want[i])
		}
		if got[i].Payload.Event != want[i] {
			t.Errorf("payload [%d] event = %q, want %q", i, got[i].Payload.Event, want[i])
		}
	}
}

// TestMatchSeamClosedMapsEverySeamToItsNotice — a seam that finished passing reaches the notice
// named after it, carrying the seam, the ids booked during the pass and the projected value.
func TestMatchSeamClosedMapsEverySeamToItsNotice(t *testing.T) {
	t.Parallel()

	seams := []domain.Moment{
		domain.MomentPreRequest,
		domain.MomentPostResponse,
		domain.MomentPreToolExec,
		domain.MomentPostToolResult,
		domain.MomentHistoryRewrite,
	}

	for _, seam := range seams {
		t.Run(string(seam), func(t *testing.T) {
			t.Parallel()

			fired := []string{"context-files", "tool-call-repair"}
			ev := domain.SeamClosedEvent{
				EventBase: domain.EventBase{Turn: 4},
				Seam:      seam,
				Fired:     fired,
			}

			got := newMatcher(allSubscribed(), nil).match(ev)

			assertEvents(t, got, []Event{seam.Closing()})
			payload := got[0].Payload
			if payload.Seam != seam {
				t.Errorf("payload seam = %q, want %q", payload.Seam, seam)
			}
			if strings.Join(payload.Reactions, ",") != strings.Join(fired, ",") {
				t.Errorf("payload reactions = %v, want %v", payload.Reactions, fired)
			}
			if payload.Turn != ev.Turn {
				t.Errorf("payload turn = %d, want %d", payload.Turn, ev.Turn)
			}
			fired[0] = "rewritten after the match"
			if payload.Reactions[0] != "context-files" {
				t.Errorf("payload reactions[0] = %q — the ids were referenced, not copied", payload.Reactions[0])
			}
		})
	}
}

// TestMatchSeamClosedFiresOnlyWhenSubscribed — the projection is the one expensive thing this
// matcher does, so a seam nothing subscribes to must cost a map lookup and nothing else.
func TestMatchSeamClosedFiresOnlyWhenSubscribed(t *testing.T) {
	t.Parallel()

	projections := 0
	m := newMatcher(map[Event]bool{domain.MomentPreRequestFinished: true}, nil)
	m.project = func(domain.Moment, any) any {
		projections++
		return "projected"
	}
	request := domain.NewRequest("qwen", nil, nil, domain.Budget{}, 1)

	got := m.match(domain.SeamClosedEvent{Seam: domain.MomentPreRequest, Value: request})

	assertEvents(t, got, []Event{domain.MomentPreRequestFinished})
	if got[0].Payload.Value != "projected" {
		t.Errorf("payload value = %v, want the projector's result", got[0].Payload.Value)
	}
	if projections != 1 {
		t.Fatalf("the subscribed seam projected %d times, want 1", projections)
	}

	for _, seam := range []domain.Moment{
		domain.MomentPostResponse,
		domain.MomentPreToolExec,
		domain.MomentPostToolResult,
		domain.MomentHistoryRewrite,
	} {
		assertEvents(t, m.match(domain.SeamClosedEvent{Seam: seam, Value: request}), nil)
	}

	if projections != 1 {
		t.Errorf("projections = %d, want 1 — an unsubscribed seam must not project its value", projections)
	}
}

// TestMatchSeamClosedIsTopLevelOnly — a sub-agent crosses the same five seams on every step of
// every delegation; a notice for each would bury the top-level pass a user asked to watch.
func TestMatchSeamClosedIsTopLevelOnly(t *testing.T) {
	t.Parallel()

	projections := 0
	m := newMatcher(allSubscribed(), nil)
	m.project = func(domain.Moment, any) any {
		projections++
		return nil
	}

	ev := domain.SeamClosedEvent{
		EventBase: domain.EventBase{Depth: 1, Turn: 2, CallID: "call-7"},
		Seam:      domain.MomentPreToolExec,
	}

	assertEvents(t, m.match(ev), nil)

	if projections != 0 {
		t.Errorf("projections = %d, want 0 — a sub-agent's seam is not matched at all", projections)
	}
}

// TestMatchSeamClosedIgnoresANoticeInTheSeamField — Closing answers the zero Moment for anything
// that is not one of the five seams, and a firing named by the empty string would be unroutable.
func TestMatchSeamClosedIgnoresANoticeInTheSeamField(t *testing.T) {
	t.Parallel()

	m := newMatcher(allSubscribed(), nil)

	for _, moment := range []domain.Moment{domain.MomentTurnFinished, domain.Moment("")} {
		assertEvents(t, m.match(domain.SeamClosedEvent{Seam: moment}), nil)
	}
}
