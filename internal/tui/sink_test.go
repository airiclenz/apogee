package tui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// newTestSink builds a bound sink whose coalescing window is short enough that a timer-driven
// flush lands inside a test's patience.
func newTestSink(t *testing.T) (*teaSink, *stubProgram) {
	t.Helper()
	prog := newStubProgram()
	ref := &programRef{}
	ref.bind(prog)
	return &teaSink{prog: ref, window: time.Millisecond}, prog
}

// newBufferingSink builds a bound sink whose window outlives any test, so nothing it buffers can
// reach the program by timer: whatever the program received got there because something flushed
// the buffer explicitly. It is what the Step-boundary flush is proved with — and what every test
// that asserts an exact merge uses, because under a millisecond window a loaded runner can let
// the timer fire between two adjacent Emits and split the pair (CI, 2026-09-18).
func newBufferingSink(t *testing.T) (*teaSink, *stubProgram) {
	t.Helper()
	prog := newStubProgram()
	ref := &programRef{}
	ref.bind(prog)
	return &teaSink{prog: ref, window: time.Hour}, prog
}

// stillBuffering reports whether s is holding tokens back, taking the sink's own lock so the read
// is safe beside a window timer.
func stillBuffering(s *teaSink) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buffering
}

// waitForEvents polls prog until it has captured at least n Events, or fails. It is the only
// way to observe a timer-driven flush without sleeping for the window and hoping.
func waitForEvents(t *testing.T, prog *stubProgram, n int) []domain.Event {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		got := prog.events()
		if len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d events; captured %d: %#v", n, len(got), got)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestTeaSinkEmitsEventsInOrder proves the C2 contract as coalescing amends it: nothing is
// dropped and nothing is reordered, but adjacent TokenEvents arrive merged — the "he"+"llo"
// pair as one "hello", ahead of the StreamResetEvent that follows them.
func TestTeaSinkEmitsEventsInOrder(t *testing.T) {
	t.Parallel()
	sink, prog := newBufferingSink(t)

	emitted := []domain.Event{
		domain.TokenEvent{Text: "he"},
		domain.TokenEvent{Text: "llo"},
		domain.StreamResetEvent{},
		domain.TokenEvent{Text: "hi"},
		domain.ErrorEvent{Source: "tool", Err: "boom"},
		domain.MessageEvent{Text: "hi there"},
	}
	for _, e := range emitted {
		sink.Emit(e)
	}

	want := []domain.Event{
		domain.TokenEvent{Text: "hello"},
		domain.StreamResetEvent{},
		domain.TokenEvent{Text: "hi"},
		domain.ErrorEvent{Source: "tool", Err: "boom"},
		domain.MessageEvent{Text: "hi there"},
	}
	got := prog.events()
	if len(got) != len(want) {
		t.Fatalf("captured %d events; want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("event[%d] = %#v; want %#v", i, got[i], want[i])
		}
	}
}

// TestTeaSinkCoalescesOnlyWithinOneStream proves the merge boundary: tokens sharing a
// (Depth, Turn) accumulate into one Event, while a sub-agent's token (Depth 1) or the next
// Turn flushes what was buffered and starts its own run — text never crosses a boundary.
func TestTeaSinkCoalescesOnlyWithinOneStream(t *testing.T) {
	t.Parallel()
	sink, prog := newBufferingSink(t)

	for _, e := range []domain.Event{
		domain.TokenEvent{EventBase: domain.EventBase{Depth: 0, Turn: 1}, Text: "pa"},
		domain.TokenEvent{EventBase: domain.EventBase{Depth: 0, Turn: 1}, Text: "rent"},
		domain.TokenEvent{EventBase: domain.EventBase{Depth: 1, Turn: 1}, Text: "sub"},
		domain.TokenEvent{EventBase: domain.EventBase{Depth: 1, Turn: 1}, Text: "agent"},
		domain.TokenEvent{EventBase: domain.EventBase{Depth: 0, Turn: 2}, Text: "next"},
		domain.MessageEvent{EventBase: domain.EventBase{Depth: 0, Turn: 2}, Text: "done"},
	} {
		sink.Emit(e)
	}

	want := []domain.Event{
		domain.TokenEvent{EventBase: domain.EventBase{Depth: 0, Turn: 1}, Text: "parent"},
		domain.TokenEvent{EventBase: domain.EventBase{Depth: 1, Turn: 1}, Text: "subagent"},
		domain.TokenEvent{EventBase: domain.EventBase{Depth: 0, Turn: 2}, Text: "next"},
		domain.MessageEvent{EventBase: domain.EventBase{Depth: 0, Turn: 2}, Text: "done"},
	}
	got := prog.events()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %#v; want %#v", got, want)
	}
}

// TestTeaSinkFlushesPendingBeforeEveryOtherVariant proves the ordering rule the folds depend
// on: whatever the non-token event is, the tokens emitted before it reach the Update loop
// ahead of it — a reset must discard tokens that already arrived, and a usage event must
// find the generation the first token started.
func TestTeaSinkFlushesPendingBeforeEveryOtherVariant(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		event domain.Event
	}{
		{"stream reset", domain.StreamResetEvent{}},
		{"message", domain.MessageEvent{Text: "committed"}},
		{"tool call", domain.ToolCallEvent{}},
		{"usage", domain.UsageEvent{}},
		{"error", domain.ErrorEvent{Source: "tool", Err: "boom"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sink, prog := newBufferingSink(t)

			sink.Emit(domain.TokenEvent{Text: "buff"})
			sink.Emit(domain.TokenEvent{Text: "ered"})
			sink.Emit(tc.event)

			want := []domain.Event{domain.TokenEvent{Text: "buffered"}, tc.event}
			got := prog.events()
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("events = %#v; want %#v", got, want)
			}
		})
	}
}

// TestTeaSinkDeliversATrailingTokenOnTheWindow proves the sink never sits on text: a token
// with no follow-up event to force it out is delivered by the window timer alone. Without
// that timer the last chunk of a reply would hang until the next event.
func TestTeaSinkDeliversATrailingTokenOnTheWindow(t *testing.T) {
	t.Parallel()
	sink, prog := newTestSink(t)

	sink.Emit(domain.TokenEvent{Text: "trailing"})

	got := waitForEvents(t, prog, 1)
	want := []domain.Event{domain.TokenEvent{Text: "trailing"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %#v; want %#v", got, want)
	}
}

// TestTeaSinkLosesNoTokenText proves the lossless property over a stream that crosses several
// windows: the concatenation of every delivered token equals the concatenation of every
// emitted one, in order. Coalescing merges — it never drops and never reorders.
func TestTeaSinkLosesNoTokenText(t *testing.T) {
	t.Parallel()
	sink, prog := newTestSink(t)

	var emitted strings.Builder
	chunks := []string{"The ", "quick ", "brown ", "fox ", "jumps ", "over ", "the ", "lazy ", "dog"}
	for i, chunk := range chunks {
		emitted.WriteString(chunk)
		sink.Emit(domain.TokenEvent{Text: chunk})
		if i%3 == 2 {
			time.Sleep(2 * time.Millisecond) // let a window close mid-stream
		}
	}
	sink.Emit(domain.MessageEvent{Text: "done"}) // the boundary that flushes whatever is left

	var delivered strings.Builder
	var messages int
	for _, e := range prog.events() {
		switch e := e.(type) {
		case domain.TokenEvent:
			delivered.WriteString(e.Text)
		case domain.MessageEvent:
			messages++
		}
	}
	if delivered.String() != emitted.String() {
		t.Errorf("delivered %q; want %q", delivered.String(), emitted.String())
	}
	if messages != 1 {
		t.Errorf("captured %d MessageEvents; want 1", messages)
	}
}

// TestTeaSinkFlushDeliversWithoutWaitingForTheWindow proves the boundary flush the worker calls:
// buffered tokens are delivered on demand, merged, with the window still wide open — and the sink
// is left holding nothing, so a second flush sends nothing at all.
func TestTeaSinkFlushDeliversWithoutWaitingForTheWindow(t *testing.T) {
	t.Parallel()
	sink, prog := newBufferingSink(t)

	sink.Emit(domain.TokenEvent{Text: "un"})
	sink.Emit(domain.TokenEvent{Text: "flushed"})
	if got := prog.events(); len(got) != 0 {
		t.Fatalf("captured %d events before the flush; want 0 (the window is still open): %#v", len(got), got)
	}

	sink.flush()
	sink.flush() // a second flush has nothing left to deliver

	want := []domain.Event{domain.TokenEvent{Text: "unflushed"}}
	if got := prog.events(); !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %#v; want %#v", got, want)
	}
	if stillBuffering(sink) {
		t.Error("sink still buffering after a flush")
	}
}

// TestTeaSinkUnboundIsNoOp proves an Emit before the program is bound neither panics nor
// blocks. This cannot happen in production (Emit fires only from a worker launched after
// Bind), but the sink must stay safe to drive in isolation.
func TestTeaSinkUnboundIsNoOp(t *testing.T) {
	t.Parallel()
	sink := &teaSink{prog: &programRef{}} // never bound
	sink.Emit(domain.TokenEvent{Text: "x"})
}

// TestSinkForwardsSeamClosedWithoutTheLiveValue pins the one field the bridge strips: a
// SeamClosedEvent's Value is a live reference into the engine's working value, valid only while
// Emit runs (domain.SeamClosedEvent), and the Update goroutine reads its Msg after that. The copy
// that crosses keeps Seam and Fired and carries a nil Value — and the event the caller handed over
// is untouched, because a Reaction Runner wrapping the sink matches on that copy after forwarding.
func TestSinkForwardsSeamClosedWithoutTheLiveValue(t *testing.T) {
	t.Parallel()
	sink, prog := newTestSink(t)

	payload := &domain.Request{}
	ev := domain.SeamClosedEvent{
		EventBase: domain.EventBase{Turn: 3},
		Seam:      domain.MomentPreRequest,
		Fired:     []string{"r1"},
		Value:     payload,
	}
	sink.Emit(ev)

	got := prog.events()
	if len(got) != 1 {
		t.Fatalf("captured %d events; want 1: %#v", len(got), got)
	}
	seam, ok := got[0].(domain.SeamClosedEvent)
	if !ok {
		t.Fatalf("forwarded %T; want domain.SeamClosedEvent", got[0])
	}
	if seam.Value != nil {
		t.Errorf("the live Value crossed to the Update goroutine: %#v", seam.Value)
	}
	if seam.Seam != ev.Seam || seam.Turn != ev.Turn || !reflect.DeepEqual(seam.Fired, ev.Fired) {
		t.Errorf("the forwarded copy lost a fact: got %#v; want Seam/Turn/Fired of %#v", seam, ev)
	}
	if ev.Value != payload {
		t.Errorf("the caller's own event was rewritten: Value = %#v", ev.Value)
	}
}

// TestTeaSinkCoalescesReasoningDeltas proves reasoning deltas merge the way tokens do: a burst of
// ReasoningEvents under one EventBase reaches the program as ONE ReasoningEvent carrying the text
// joined in order — one /thinking repaint per window instead of one per provider delta.
func TestTeaSinkCoalescesReasoningDeltas(t *testing.T) {
	t.Parallel()
	sink, prog := newBufferingSink(t)
	base := domain.EventBase{Depth: 0, Turn: 3}

	for _, chunk := range []string{"Let", " me", " think", " about", " this."} {
		sink.Emit(domain.ReasoningEvent{EventBase: base, Text: chunk})
	}
	sink.flush()

	want := []domain.Event{domain.ReasoningEvent{EventBase: base, Text: "Let me think about this."}}
	if got := prog.events(); !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %#v; want %#v", got, want)
	}
}

// TestTeaSinkNeverMergesReasoningAcrossKinds proves the kind is part of the merge key. One Turn's
// reasoning and visible tokens share an EventBase exactly, so a buffer keyed on the EventBase alone
// would fold thinking into the reply; here every change of kind — and the interleaved tool call —
// flushes first, so the program sees the runs in emission order, each merged only within itself.
func TestTeaSinkNeverMergesReasoningAcrossKinds(t *testing.T) {
	t.Parallel()
	sink, prog := newBufferingSink(t)
	base := domain.EventBase{Depth: 0, Turn: 1}

	for _, e := range []domain.Event{
		domain.ReasoningEvent{EventBase: base, Text: "hm"},
		domain.ReasoningEvent{EventBase: base, Text: "m"},
		domain.TokenEvent{EventBase: base, Text: "So"},
		domain.TokenEvent{EventBase: base, Text: " yes"},
		domain.ReasoningEvent{EventBase: base, Text: "wait"},
		domain.ToolCallEvent{EventBase: base},
		domain.ReasoningEvent{EventBase: base, Text: "after"},
		domain.ReasoningEvent{EventBase: base, Text: " tool"},
	} {
		sink.Emit(e)
	}
	sink.flush()

	want := []domain.Event{
		domain.ReasoningEvent{EventBase: base, Text: "hmm"},
		domain.TokenEvent{EventBase: base, Text: "So yes"},
		domain.ReasoningEvent{EventBase: base, Text: "wait"},
		domain.ToolCallEvent{EventBase: base},
		domain.ReasoningEvent{EventBase: base, Text: "after tool"},
	}
	if got := prog.events(); !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %#v; want %#v", got, want)
	}
}

// TestTeaSinkNeverMergesReasoningAcrossRuns proves two concurrent siblings' thinking stays apart:
// they share a Depth and a Turn but not a spawning call id, and the call id is what keeps one
// delegate's reasoning out of the other's record (ADR 0039).
func TestTeaSinkNeverMergesReasoningAcrossRuns(t *testing.T) {
	t.Parallel()
	sink, prog := newBufferingSink(t)
	a := domain.EventBase{Depth: 1, Turn: 1, CallID: "call-a"}
	b := domain.EventBase{Depth: 1, Turn: 1, CallID: "call-b"}

	for _, e := range []domain.Event{
		domain.ReasoningEvent{EventBase: a, Text: "a1"},
		domain.ReasoningEvent{EventBase: a, Text: "a2"},
		domain.ReasoningEvent{EventBase: b, Text: "b1"},
		domain.ReasoningEvent{EventBase: a, Text: "a3"},
	} {
		sink.Emit(e)
	}
	sink.flush()

	want := []domain.Event{
		domain.ReasoningEvent{EventBase: a, Text: "a1a2"},
		domain.ReasoningEvent{EventBase: b, Text: "b1"},
		domain.ReasoningEvent{EventBase: a, Text: "a3"},
	}
	if got := prog.events(); !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %#v; want %#v", got, want)
	}
}

// thinkingTextOf feeds every ReasoningEvent in events to a fresh board, as the Model's fold does,
// and returns the one run's in-flight record text.
func thinkingTextOf(t *testing.T, events []domain.Event) string {
	t.Helper()
	var board thinkingBoard
	for _, e := range events {
		r, ok := e.(domain.ReasoningEvent)
		if !ok {
			t.Fatalf("non-reasoning event %#v", e)
		}
		board.append(r.Text, runOf(r.EventBase), r.Turn)
	}
	if len(board.live) != 1 {
		t.Fatalf("board holds %d live records; want 1", len(board.live))
	}
	return board.live[0].text
}

// TestTeaSinkReasoningMergeKeepsTheThinkingRecord proves the merge is invisible to the /thinking
// board wherever every chunk carries whole escapes and whole runes: the record built from the one
// merged event is byte-identical to the record the unmerged deltas build.
func TestTeaSinkReasoningMergeKeepsTheThinkingRecord(t *testing.T) {
	t.Parallel()
	base := domain.EventBase{Depth: 0, Turn: 2}
	var deltas []domain.Event
	for _, chunk := range []string{"Plan: ", "read \x1b[1mfile\x1b[0m", ", then ", "héllo — ", "日本語", " done.\n"} {
		deltas = append(deltas, domain.ReasoningEvent{EventBase: base, Text: chunk})
	}

	sink, prog := newBufferingSink(t)
	for _, e := range deltas {
		sink.Emit(e)
	}
	sink.flush()
	merged := prog.events()
	if len(merged) != 1 {
		t.Fatalf("sink delivered %d events; want 1 merged: %#v", len(merged), merged)
	}

	if got, want := thinkingTextOf(t, merged), thinkingTextOf(t, deltas); got != want {
		t.Fatalf("merged record = %q; unmerged record = %q", got, want)
	}
}

// TestTeaSinkReasoningMergeStripsASplitEscapeWhole pins the one place the merge changes what the
// board holds: an escape sequence the provider split across two deltas. Unmerged, each half is
// stripped on its own and the CSI's tail survives as literal text; merged, the seam sees the whole
// sequence and strips all of it — the same edge the token path has always had.
func TestTeaSinkReasoningMergeStripsASplitEscapeWhole(t *testing.T) {
	t.Parallel()
	base := domain.EventBase{Depth: 0, Turn: 1}
	sink, prog := newBufferingSink(t)
	sink.Emit(domain.ReasoningEvent{EventBase: base, Text: "red \x1b[3"})
	sink.Emit(domain.ReasoningEvent{EventBase: base, Text: "1mtext"})
	sink.flush()

	if got, want := thinkingTextOf(t, prog.events()), "red text"; got != want {
		t.Fatalf("merged record = %q; want %q", got, want)
	}
}

// BenchmarkTeaSinkReasoningBurst measures one window's worth of reasoning deltas through the sink:
// the whole burst lands as a single Msg, however many deltas the provider split it into.
func BenchmarkTeaSinkReasoningBurst(b *testing.B) {
	base := domain.EventBase{Depth: 0, Turn: 1}
	b.ReportAllocs()
	for b.Loop() {
		prog := newStubProgram()
		ref := &programRef{}
		ref.bind(prog)
		sink := &teaSink{prog: ref, window: time.Hour}
		for range 64 {
			sink.Emit(domain.ReasoningEvent{EventBase: base, Text: "a reasoning delta "})
		}
		sink.flush()
	}
}
