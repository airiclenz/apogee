package tui

import (
	"reflect"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestTeaSinkEmitsEventsInOrder proves the C2 contract: every Event becomes exactly one
// eventMsg, delivered in Emit order, nothing dropped (the lossless default).
func TestTeaSinkEmitsEventsInOrder(t *testing.T) {
	t.Parallel()
	prog := newStubProgram()
	ref := &programRef{}
	ref.bind(prog)
	sink := &teaSink{prog: ref}

	want := []domain.Event{
		domain.TokenEvent{Text: "he"},
		domain.TokenEvent{Text: "llo"},
		domain.StreamResetEvent{},
		domain.TokenEvent{Text: "hi"},
		domain.ErrorEvent{Source: "tool", Err: "boom"},
		domain.MessageEvent{Text: "hi there"},
	}
	for _, e := range want {
		sink.Emit(e)
	}

	got := prog.events()
	if len(got) != len(want) {
		t.Fatalf("captured %d events; want %d", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("event[%d] = %#v; want %#v", i, got[i], want[i])
		}
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
