package domain

import (
	"slices"
	"testing"
)

// TestPeekDeferredLeavesTheQueue: a peek reads the pending corrections in FIFO order without
// draining them, and the copy it returns is the caller's own — editing it leaves the queue whole —
// so the drain that builds the next request still takes every one.
func TestPeekDeferredLeavesTheQueue(t *testing.T) {
	t.Parallel()

	conv := NewConversation(nil)
	if got := conv.PeekDeferred(); got != nil {
		t.Fatalf("PeekDeferred on an empty queue = %q, want nil", got)
	}
	conv.Defer("first correction")
	conv.Defer("second correction")
	want := []string{"first correction", "second correction"}

	peeked := conv.PeekDeferred()
	if !slices.Equal(peeked, want) {
		t.Fatalf("PeekDeferred = %q, want %q", peeked, want)
	}
	peeked[0] = "edited by the caller"

	if got := conv.DeferredLen(); got != len(want) {
		t.Errorf("DeferredLen after a peek = %d, want %d: the peek drained the queue", got, len(want))
	}
	taken, ok := conv.TakeDeferred()
	if !ok || !slices.Equal(taken, want) {
		t.Errorf("TakeDeferred after a peek = %q (ok %v), want %q: the peek's copy reached the queue", taken, ok, want)
	}
}
