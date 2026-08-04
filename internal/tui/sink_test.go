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
// the buffer explicitly. It is what the Step-boundary flush is proved with.
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
	sink, prog := newTestSink(t)

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
	sink, prog := newTestSink(t)

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
			sink, prog := newTestSink(t)

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
