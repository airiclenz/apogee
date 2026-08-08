package domain

// White-box tests for the shared prompt slot (ADR 0039 decision 12): the ONE surface an Approval
// and an ask_user question both queue on, and the context seam that designates it for a whole
// Agent tree.

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestPromptSlot_AdmitsOneAtATime pins the whole point of the type: a second caller waits until the
// first releases, and then gets in — whatever KIND of prompt either of them was carrying, since the
// slot has no notion of kind at all.
func TestPromptSlot_AdmitsOneAtATime(t *testing.T) {
	slot := NewPromptSlot()
	if err := slot.Acquire(context.Background()); err != nil {
		t.Fatalf("the first Acquire returned %v, want the empty slot", err)
	}

	admitted := make(chan error, 1)
	go func() { admitted <- slot.Acquire(context.Background()) }()

	select {
	case err := <-admitted:
		t.Fatalf("a second caller was admitted (err %v) while the first held the slot", err)
	case <-time.After(20 * time.Millisecond):
	}

	slot.Release()
	select {
	case err := <-admitted:
		if err != nil {
			t.Errorf("the waiting caller returned %v, want admission once the slot was free", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the waiting caller was never admitted after Release")
	}
	slot.Release()
}

// TestPromptSlot_QueuedCallerAnswersItsOwnCancellation pins why the wait is a channel and not a
// mutex: a sibling queued behind the visible prompt must be able to give up when the human cancels
// the Turn, rather than parking until the prompt ahead of it resolves and only THEN reaching a
// Driver whose Turn has already rolled back.
func TestPromptSlot_QueuedCallerAnswersItsOwnCancellation(t *testing.T) {
	slot := NewPromptSlot()
	if err := slot.Acquire(context.Background()); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer slot.Release()

	ctx, cancel := context.WithCancel(context.Background())
	queued := make(chan error, 1)
	go func() { queued <- slot.Acquire(ctx) }()

	time.Sleep(20 * time.Millisecond) // long enough for the second caller to be genuinely waiting
	cancel()

	select {
	case err := <-queued:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("a cancelled queued caller returned %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a queued caller never answered its own cancellation — the wait is not ctx-aware")
	}
}

// TestWithPromptSlot_KeepsTheOutermostSlot pins the tree rule: a nested Agent runs under a context
// derived from its parent's, so designating its own slot must be a no-op — otherwise every depth
// would open a private, uncontended prompt surface and siblings would stop queueing against each
// other.
func TestWithPromptSlot_KeepsTheOutermostSlot(t *testing.T) {
	if got := PromptSlotFromContext(context.Background()); got != nil {
		t.Errorf("a bare context designated %#v, want no slot", got)
	}

	outer, inner := NewPromptSlot(), NewPromptSlot()
	ctx := WithPromptSlot(context.Background(), outer)
	if got := PromptSlotFromContext(WithPromptSlot(ctx, inner)); got != outer {
		t.Error("a nested designation replaced its parent's slot; the tree would stop queueing as one")
	}
	if got := PromptSlotFromContext(WithPromptSlot(context.Background(), nil)); got != nil {
		t.Errorf("designating a nil slot installed %#v, want nothing", got)
	}
}

// TestPromptSlotFor_ContextWinsOverTheSeamsOwn pins the resolution the two queueing seams share:
// inside a running Agent they take the designated slot — which is what puts an approval and a
// question in ONE queue across two packages — and outside one they fall back to their own, which is
// then the only prompt surface there is.
func TestPromptSlotFor_ContextWinsOverTheSeamsOwn(t *testing.T) {
	own, designated := NewPromptSlot(), NewPromptSlot()

	if got := PromptSlotFor(context.Background(), own); got != own {
		t.Error("with no slot designated, a seam did not fall back to its own")
	}
	ctx := WithPromptSlot(context.Background(), designated)
	if got := PromptSlotFor(ctx, own); got != designated {
		t.Error("a seam kept its own slot under a designated one; the two kinds would not share a queue")
	}
}
