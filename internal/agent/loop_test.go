package agent

import (
	"context"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// cancelOnResetSink cancels the Step's context the instant the loop announces a re-stream. The
// StreamResetEvent is emitted immediately before the hold-off wait, and Emit runs on the loop's
// own goroutine, so the cancel is already latched when holdOffRestream selects — landing the
// cancel INSIDE the hold-off deterministically, with no sleep and no shortened timer.
type cancelOnResetSink struct {
	recordingSink
	cancel context.CancelFunc
}

func (s *cancelOnResetSink) Emit(e domain.Event) {
	s.recordingSink.Emit(e)
	if _, ok := e.(domain.StreamResetEvent); ok {
		s.cancel()
	}
}

// TestCancelInsideRestreamHoldOffStaysResumable pins the uniform cancel semantics of the
// transient-fault re-stream: a cancel that arrives while the Turn waits out the hold-off is a
// cancel, not the second fault. It used to fall through to the give-up path, degrading a Turn the
// user had merely interrupted into an ABANDONED one — a lost, un-resumable Exchange plus an
// ErrorEvent blaming the upstream for the user's own Esc. The Turn must instead roll back to its
// pre-request boundary and resume, exactly as a cancel mid-stream does.
func TestCancelInsideRestreamHoldOffStaysResumable(t *testing.T) {
	sink := &cancelOnResetSink{}
	responder := &scriptedResponder{scripts: [][]provider.Delta{
		retryableErrorScript(transientFaultMsg), // the blip that arms the re-stream
		contentScript("resumed"),                // reached only by the re-attempt after the cancel
	}}
	a, err := newAgent(baseConfig(sink), responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "ask the model"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	// A pending correction the Turn's request drains, so the rollback's re-queue is observable.
	a.conv.Defer("re-read the file before editing")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sink.cancel = cancel

	res, err := a.Step(ctx)
	if err != nil {
		t.Fatalf("Step returned a loop error on cancel: %v", err)
	}

	if res.Status != domain.StatusCancelled {
		t.Fatalf("Step status = %q, want %q — a cancel inside the hold-off is a cancel, not a give-up",
			res.Status, domain.StatusCancelled)
	}
	if res.Faulted {
		t.Error("Faulted set; a cancel is a re-attemptable rollback, not a fault")
	}
	if errs := errorEvents(sink.events); len(errs) != 0 {
		t.Errorf("ErrorEvents = %v, want none — the user's own cancel is not a fault to surface", errs)
	}
	if got := countEvents[domain.StreamResetEvent](sink.events); got != 1 {
		t.Errorf("StreamResetEvents = %d, want 1 — the reset is consistent with the rolled-back Turn", got)
	}
	if responder.calls != 1 {
		t.Errorf("Upstream calls = %d, want 1 — the hold-off must not re-stream into a dead context", responder.calls)
	}
	// Rolled back to a serializable boundary: the committed user message survives, this Turn's
	// work does not, and the drained correction is back on the queue for the re-attempt (F6).
	if got := a.conv.Len(); got != 1 {
		t.Errorf("conversation len = %d, want 1 (the user input alone)", got)
	}
	if got := a.conv.DeferredLen(); got != 1 {
		t.Errorf("deferred queue len = %d, want 1 — the drained correction is restored for the re-attempt", got)
	}
	// The Exchange stays open, so the host continues by re-Stepping rather than re-Submitting.
	if err := a.Submit(domain.UserInput{Text: "intrude"}); err == nil {
		t.Error("Submit after the cancel was accepted; the open Exchange must reject it")
	}

	// The proof of resumability: the very same agent re-attempts the Turn and completes it.
	res2, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step (resumed): %v", err)
	}
	if res2.Status != domain.StatusExchangeComplete || res2.Faulted {
		t.Fatalf("resumed result = %+v, want a clean exchange-complete", res2)
	}
	if me, ok := lastMessageEvent(sink.events); !ok || me.Text != "resumed" {
		t.Errorf("final MessageEvent = %+v (ok=%v), want %q", me, ok, "resumed")
	}
}

// TestRestreamHoldOffThatElapsesStillReStreams is the untouched half of the same branch: when the
// wait ends because it EXPIRED rather than because the ctx died, the Turn re-streams exactly as it
// always did. Without this the fix above could pass by never re-streaming at all.
func TestRestreamHoldOffThatElapsesStillReStreams(t *testing.T) {
	shortRestreamHoldoff(t)

	sink := &recordingSink{}
	responder := &scriptedResponder{scripts: [][]provider.Delta{
		retryableErrorScript(transientFaultMsg),
		contentScript("recovered"),
	}}
	a, err := newAgent(baseConfig(sink), responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "ask the model"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete || res.Faulted {
		t.Fatalf("Step result = %+v, want a clean exchange-complete", res)
	}
	if responder.calls != 2 {
		t.Errorf("Upstream calls = %d, want 2 — an elapsed hold-off re-streams the same request", responder.calls)
	}
	if errs := errorEvents(sink.events); len(errs) != 0 {
		t.Errorf("ErrorEvents = %v, want none — a recovered re-stream stays silent", errs)
	}
}
