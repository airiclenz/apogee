package agent

// White-box capstone harness (P0.6e, re-homed to internal/agent by P1.0). It lives in
// package agent so it can inject a deterministic fake Responder through the unexported
// newAgent/resumeAgent seam — the provider seam stays internal (Decision C), so there
// is no public way to supply a fake, and the full Turn cannot be driven black-box. The
// public-API validation paths that need no fake live in the black-box apogee_test
// package (../../apogee_test.go).

import (
	"context"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// recordingSink captures every emitted Event for assertion. It is written only by the
// goroutine driving Step, so it is race-safe under the single-goroutine Agent contract.
type recordingSink struct {
	events []domain.Event
}

func (s *recordingSink) Emit(e domain.Event) { s.events = append(s.events, e) }

// streamReply is a fake stream that yields one content chunk then a terminal Done — the
// streaming stand-in for a canned assistant reply.
func streamReply(content string) iter.Seq[provider.Delta] {
	return func(yield func(provider.Delta) bool) {
		if content != "" && !yield(provider.Delta{Kind: provider.DeltaContent, Content: content}) {
			return
		}
		yield(provider.Delta{Kind: provider.DeltaDone, FinishReason: "stop"})
	}
}

// echoResponder is the canned-reply fake: it answers every request with a fixed
// assistant message, the stand-in for the real HTTP provider on the streaming seam.
type echoResponder struct {
	reply string
}

func (r echoResponder) Stream(context.Context, provider.Request) iter.Seq[provider.Delta] {
	return streamReply(r.reply)
}

// blockingResponder blocks until ctx is cancelled, then surfaces the cancellation as a
// terminal stream error — the fake that drives the cancel-mid-stream path. started is
// closed once the stream is in flight so the test can cancel deterministically (no sleep).
type blockingResponder struct {
	started chan struct{}
}

func (r blockingResponder) Stream(ctx context.Context, _ provider.Request) iter.Seq[provider.Delta] {
	return func(yield func(provider.Delta) bool) {
		close(r.started)
		<-ctx.Done()
		yield(provider.Delta{Kind: provider.DeltaError, Err: ctx.Err().Error()})
	}
}

// firingHook is a no-op experimental pre-request hook that records that it fired.
type firingHook struct {
	fired *bool
}

func (h firingHook) PreRequest(context.Context, *domain.Request) error {
	*h.fired = true
	return nil
}

// panickingHook is an experimental hook that panics — the input for the
// recover-at-extension-boundary guarantee.
type panickingHook struct{}

func (panickingHook) PreRequest(context.Context, *domain.Request) error { panic("hook boom") }

// ---------------------------------------------------------------------------

func baseConfig(sink domain.EventSink) domain.Config {
	return domain.Config{
		Endpoint: "http://localhost:0",
		Model:    "test-model",
		Events:   sink,
	}
}

func firstMessageEvent(t *testing.T, events []domain.Event) (domain.MessageEvent, bool) {
	t.Helper()
	for _, e := range events {
		if me, ok := e.(domain.MessageEvent); ok {
			return me, true
		}
	}
	return domain.MessageEvent{}, false
}

func hasEvent[T domain.Event](events []domain.Event) bool {
	for _, e := range events {
		if _, ok := e.(T); ok {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------

// TestHarness_FullCapstonePath drives the end-to-end seam the plan names: construct →
// Submit → Step (observe the experimental hook fire + the assistant message at the
// quiescent boundary) → Snapshot → Resume → Submit → Step, proving the resumed Agent
// continues the restored conversation.
func TestHarness_FullCapstonePath(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	fired := false
	cfg.Mechanisms = domain.NewMechanismRegistry()
	if err := cfg.Mechanisms.AddExperimental(domain.HookPreRequest, firingHook{fired: &fired}); err != nil {
		t.Fatalf("AddExperimental: %v", err)
	}

	a, err := newAgent(cfg, echoResponder{reply: "hello from model"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	if err := a.Submit(domain.UserInput{Text: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}

	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("Step status = %q, want %q", res.Status, domain.StatusExchangeComplete)
	}
	if res.TurnIndex != 0 {
		t.Errorf("Step TurnIndex = %d, want 0", res.TurnIndex)
	}
	if !fired {
		t.Error("experimental pre-request hook did not fire")
	}
	if !hasEvent[domain.MechanismFiredEvent](sink.events) {
		t.Error("no MechanismFiredEvent emitted for the experimental hook")
	}
	if me, ok := firstMessageEvent(t, sink.events); !ok || me.Text != "hello from model" {
		t.Errorf("MessageEvent = %+v (ok=%v), want Text=%q", me, ok, "hello from model")
	}

	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snap.Version != domain.SessionVersion {
		t.Errorf("Snapshot Version = %d, want %d", snap.Version, domain.SessionVersion)
	}

	// Resume into a fresh Agent (fresh sink) and continue the restored conversation.
	sink2 := &recordingSink{}
	cfg2 := baseConfig(sink2)
	cfg2.Mechanisms = cfg.Mechanisms
	b, err := resumeAgent(cfg2, snap, echoResponder{reply: "second reply"})
	if err != nil {
		t.Fatalf("resumeAgent: %v", err)
	}
	if err := b.Submit(domain.UserInput{Text: "again"}); err != nil {
		t.Fatalf("Submit (resumed): %v", err)
	}
	if _, err := b.Step(context.Background()); err != nil {
		t.Fatalf("Step (resumed): %v", err)
	}

	// user "hi", assistant "hello from model" (restored) + user "again", assistant
	// "second reply" (this Turn) = 4 messages — proving Resume restored the history.
	if got := b.conv.Len(); got != 4 {
		t.Errorf("resumed conversation has %d messages, want 4", got)
	}
	if me, ok := firstMessageEvent(t, sink2.events); !ok || me.Text != "second reply" {
		t.Errorf("resumed MessageEvent = %+v (ok=%v), want Text=%q", me, ok, "second reply")
	}
}

// TestHarness_RealProviderWirePath exercises the P1.1 wire path hermetically: the public
// New binds the real OpenAI-compatible provider client at cfg.Endpoint (no longer the
// Placeholder), and a Step drives a full non-streaming round-trip against an httptest
// Upstream, surfacing the server's reply as a MessageEvent at the quiescent boundary.
func TestHarness_RealProviderWirePath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The loop streams (the §6 #6 path): answer with SSE, not a whole JSON body.
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"from the wire\"},\"finish_reason\":null}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Endpoint = srv.URL // the real client dials this Upstream

	a, err := New(cfg) // public constructor — binds provider.NewClient (P1.1), not a fake
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}

	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("Step status = %q, want %q", res.Status, domain.StatusExchangeComplete)
	}
	if me, ok := firstMessageEvent(t, sink.events); !ok || me.Text != "from the wire" {
		t.Errorf("MessageEvent = %+v (ok=%v), want Text=%q", me, ok, "from the wire")
	}
}

// TestHarness_SubmitMidExchange rejects a second Submit before the first is consumed.
func TestHarness_SubmitMidExchange(t *testing.T) {
	a, err := newAgent(baseConfig(&recordingSink{}), echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "first"}); err != nil {
		t.Fatalf("first Submit: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "second"}); err != domain.ErrInputPending {
		t.Errorf("second Submit err = %v, want ErrInputPending", err)
	}
}

// TestHarness_CancellationIsResumable cancels mid-respond and proves the Step returns
// StatusCancelled with serializable state that resumes and continues (ADR 0007).
func TestHarness_CancellationIsResumable(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	responder := blockingResponder{started: make(chan struct{})}
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "slow"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-responder.started
		cancel()
	}()
	res, err := a.Step(ctx)
	if err != nil {
		t.Fatalf("Step returned a loop error on cancel: %v", err)
	}
	if res.Status != domain.StatusCancelled {
		t.Fatalf("Step status = %q, want %q", res.Status, domain.StatusCancelled)
	}

	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot after cancel: %v", err)
	}

	// The snapshot is valid: resume against a working responder and complete the Turn.
	sink2 := &recordingSink{}
	b, err := resumeAgent(baseConfig(sink2), snap, echoResponder{reply: "recovered"})
	if err != nil {
		t.Fatalf("resumeAgent after cancel: %v", err)
	}
	// The Exchange is still open across the cancel (inExchange survived), so a Submit is
	// rejected — the host continues the cancelled Turn by re-Stepping, not re-Submitting.
	if err := b.Submit(domain.UserInput{Text: "intrude"}); err == nil {
		t.Error("Submit after a cancel was accepted; the open Exchange must reject it")
	}
	res2, err := b.Step(context.Background())
	if err != nil {
		t.Fatalf("Step (resumed): %v", err)
	}
	if res2.Status != domain.StatusExchangeComplete {
		t.Errorf("resumed Step status = %q, want %q", res2.Status, domain.StatusExchangeComplete)
	}
}

// TestHarness_PanicRecovery proves a panicking hook becomes an ErrorEvent at a clean
// boundary and the loop survives a second Step (the host is never unwound).
func TestHarness_PanicRecovery(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Mechanisms = domain.NewMechanismRegistry()
	if err := cfg.Mechanisms.AddExperimental(domain.HookPreRequest, panickingHook{}); err != nil {
		t.Fatalf("AddExperimental: %v", err)
	}
	a, err := newAgent(cfg, echoResponder{reply: "unreached"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	if err := a.Submit(domain.UserInput{Text: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step returned a loop error on hook panic: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("Step status = %q, want %q", res.Status, domain.StatusExchangeComplete)
	}
	if !hasEvent[domain.ErrorEvent](sink.events) {
		t.Error("no ErrorEvent emitted for the panicking hook")
	}
	if _, ok := firstMessageEvent(t, sink.events); ok {
		t.Error("a MessageEvent was emitted despite the pre-request hook panicking")
	}

	// The loop survived: a second Submit/Step recovers again and still returns cleanly.
	if err := a.Submit(domain.UserInput{Text: "again"}); err != nil {
		t.Fatalf("Submit after recovery: %v", err)
	}
	res2, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("second Step after recovery returned a loop error: %v", err)
	}
	if res2.Status != domain.StatusExchangeComplete {
		t.Errorf("second Step status = %q, want %q", res2.Status, domain.StatusExchangeComplete)
	}
}
