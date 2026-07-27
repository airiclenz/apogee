package tui

import (
	"sync"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The interjection mailbox (ADR 0025)
// ----------------------------------------------------------------------------

// queuedInterjection is one message the human typed while the model was working, staged for
// delivery into the running Exchange. It carries three things because three different consumers
// need different halves of it:
//
//   - id names the row across the two copies of the queue — the Model's display slice and the
//     mailbox the worker drains — so the delivery fold can remove exactly the rows that landed
//     without depending on order or on pointer identity. Ids are minted by the staging side
//     (the Update goroutine) and are unique within a session, never reused.
//   - raw is the pre-parse editor text, kept verbatim so popping the newest row back into the
//     editor restores what the human actually typed (@refs and all), not a reconstruction of it.
//   - input is the parsed form the engine consumes; its @file references resolve at DELIVERY,
//     inside Agent.Interject, so the model reads a file as it stands then rather than as it
//     stood when the remark was typed.
type queuedInterjection struct {
	id    int
	raw   string
	input domain.UserInput
}

// interjectBox is the per-Exchange mailbox: the Update goroutine pushes staged rows into it and
// the worker goroutine drains them between Steps, which is the ONE place the two goroutines
// touch the same state. Everywhere else the split is clean — the Model owns the display rows,
// the worker owns the engine — so this mutex is the whole of the concurrency in the interjection
// path (the engine needs none: Agent.Interject is called at the between-Steps boundary, where the
// worker owns the conversation outright).
//
// It is held BY POINTER on the Model. The Model is value-copied on every Update (ADR 0011), and a
// sync.Mutex copied by value would hand each copy its own lock — silently unsynchronising the two
// goroutines rather than failing loudly (doc.go's no-copy invariant).
//
// A nil box is the "no worker to deliver to" state and is deliberately usable: push is a no-op and
// drainAll yields nothing, so staging a row while /compact runs (a worker that drives no Exchange
// and so takes no box) stages the display row without wedging it into a mailbox nobody empties.
// Such a row reaches the model through the terminal flush instead.
type interjectBox struct {
	mu    sync.Mutex
	items []queuedInterjection
}

// newInterjectBox returns an empty mailbox for one Exchange. A box is never reused across
// Exchanges: the worker that drains it is the only reader, and it dies with that worker.
func newInterjectBox() *interjectBox {
	return &interjectBox{}
}

// push stages it for delivery at the next between-Steps boundary. Called from the Update
// goroutine. A nil box drops the row silently — see the type doc: the display copy on the Model
// is what makes that safe, not luck.
func (b *interjectBox) push(it queuedInterjection) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.items = append(b.items, it)
}

// drainAll removes and returns every staged row, oldest first — FIFO, because the human wrote
// them in that order and the model should read them in it. Called from the worker goroutine.
// It returns nil when there is nothing staged (or the box is nil), which is the caller's cue to
// skip the delivery notification entirely.
//
// The drain is unconditional: rows leave the mailbox even if a delivery later in the batch fails.
// That is deliberate — the Model's display copy is the queue of record, and a row that did not
// land simply stays on it (reported by the delivery Msg naming only what DID land) and reaches the
// model through the terminal flush. A row is never silently lost, and never delivered twice.
func (b *interjectBox) drainAll() []queuedInterjection {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.items) == 0 {
		return nil
	}
	out := b.items
	b.items = nil
	return out
}
