package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
)

// ----------------------------------------------------------------------------
// The parked call — the rendezvous body both human gates share
// ----------------------------------------------------------------------------
//
// THE THREE CROSS-GOROUTINE IDIOMS, NAMED. Every place in this package where two goroutines
// meet is one of exactly three shapes, and naming them is what keeps a fourth from being
// invented by accident:
//
//   - RENDEZVOUS — the caller sends and then BLOCKS on a reply. Both human gates are this
//     shape: [uiApprover.Approve] parks a Step on an approval decision, [uiAsker.Ask] parks
//     one on a typed answer. The body below is the whole idiom; the gates differ only in
//     what they carry and in the verdict an abandoned request settles on.
//   - MAILBOX — the two goroutines share one mutex-guarded queue, each owning an end of it.
//     [interjectBox] is the only one: the Update goroutine pushes staged rows, the worker
//     drains them between Steps (ADR 0025). Nobody blocks; the mutex is the whole of it.
//   - FIRE-AND-FORGET — the caller sends and returns, expecting nothing back. [uiPresenter]
//     hands a finished presentation to the loop this way (a presentation asks the human
//     nothing), as do [Bridge.NotifySchedule] and [Bridge.NotifyRouting], the composition
//     root's own ways in from a scheduler goroutine and from the routing heartbeat.
//
// All three ride the same late-bound [programRef], and all three share its one hard rule:
// [programRef.send] blocks until the Update loop takes the message, so calling any of them
// FROM Update deadlocks the program against itself. Each is therefore some other goroutine's
// idiom — the worker's, the scheduler's, the root's.
//
// What the goroutine on the far side may then do to the ENGINE is a separate question, and
// ADR 0011 answers it with three legality classes: idle-only calls proved safe by the state
// machine, the SetMode class behind a documented mutex, and between-Steps calls made safe by
// the boundary itself (the driving goroutine IS the engine's single driver there). A
// rendezvous is none of those — it touches the UI, not the engine — which is exactly why it
// needs a shape of its own rather than a rule about locks.

// parkCall hands one request to the single-threaded Update loop and blocks the calling
// goroutine until the human answers or ctx is cancelled — the whole rendezvous, once, for
// both gates that need it.
//
// envelope wraps the freshly made reply channel in the Msg the loop recognises; the channel
// is buffered (cap 1) so the loop never blocks sending its reply, and so a reply that arrives
// AFTER a cancel is absorbed by the buffer rather than parking the UI — no goroutine leak.
//
// abandoned is the value returned alongside ctx.Err() when the human never answers (a user
// stop cancelled the in-flight Exchange). Each gate states its own: the two disagree, so the
// difference is a parameter rather than a zero value, and the call site says why it chose
// what it chose. The error is always ctx.Err() — the engine rolls the Turn back to a
// quiescent boundary with StatusCancelled (ADR 0007), so it is the cancellation, never the
// abandoned value, that ends the Turn.
//
// It never hangs past ctx, which is what makes both gates fail-safe under a non-interactive
// shutdown. It lives host-side, on the delegates, and never on the value-typed [Model]
// (ADR 0011).
func parkCall[Reply any](
	ctx context.Context,
	prog *programRef,
	envelope func(reply chan Reply) tea.Msg,
	abandoned Reply,
) (Reply, error) {
	reply := make(chan Reply, 1) // buffered: the UI never blocks replying
	prog.send(envelope(reply))
	select {
	case answer := <-reply:
		return answer, nil
	case <-ctx.Done():
		return abandoned, ctx.Err()
	}
}
