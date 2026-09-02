package agent

import (
	"context"
	"strings"
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

// idLessCall is a native tool_calls entry the server sent without an id — the `tool_calls:[{}]`
// family the probe's battery already refuses to count as evidence (C-18). Dispatching one sends
// its result back as a tool message whose omitempty tool_call_id drops off the wire, leaving the
// server holding a result it cannot match to any call it issued.
func idLessCall(name, args string) provider.Delta {
	return provider.Delta{Kind: provider.DeltaToolCall, ToolCall: &provider.ToolCall{
		Type:     "function",
		Function: provider.FunctionCall{Name: name, Arguments: args},
	}}
}

// firstToolCallEvent returns the first dispatched call the loop announced.
func firstToolCallEvent(events []domain.Event) (domain.ToolCallEvent, bool) {
	for _, e := range events {
		if tce, ok := e.(domain.ToolCallEvent); ok {
			return tce, true
		}
	}
	return domain.ToolCallEvent{}, false
}

// TestIDLessNativeCallIsDroppedWhileItsSiblingDispatches closes the gap the probe had opened over
// the loop it speaks for: the battery scored `tool_calls:[{}]` as no evidence at all, while the
// loop dispatched the very same entry. The malformed sibling is now dropped on the shared
// predicate and REPORTED once from source "processing", and the well-formed call runs untouched.
func TestIDLessNativeCallIsDroppedWhileItsSiblingDispatches(t *testing.T) {
	sink := &recordingSink{}
	ran := 0
	cfg := configWithTools(sink, fakeTool{name: "lookup", readOnly: true, ran: &ran, result: "the answer is 42"})
	responder := &scriptedResponder{scripts: [][]provider.Delta{
		{
			{Kind: provider.DeltaToolCall, ToolCall: &provider.ToolCall{
				ID: "c1", Type: "function",
				Function: provider.FunctionCall{Name: "lookup", Arguments: `{"q":"meaning"}`},
			}},
			idLessCall("lookup", `{"q":"unusable"}`),
			{Kind: provider.DeltaDone, FinishReason: "tool_calls"},
		},
		contentScript("all done"),
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "look it up"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}

	if res.Status != domain.StatusTurnComplete || res.Faulted {
		t.Errorf("StepResult = {Status:%q Faulted:%v}, want {Status:%q Faulted:false} — the usable call still runs",
			res.Status, res.Faulted, domain.StatusTurnComplete)
	}
	if ran != 1 {
		t.Errorf("tool ran %d times, want 1 — the id-less entry must not reach the executor", ran)
	}
	if got := countEvents[domain.ToolCallEvent](sink.events); got != 1 {
		t.Errorf("ToolCallEvents = %d, want 1", got)
	}
	if tce, ok := firstToolCallEvent(sink.events); !ok || tce.Call.ID != "c1" {
		t.Errorf("dispatched call = %+v (ok=%v), want the well-formed c1", tce.Call, ok)
	}

	errs := errorEvents(sink.events)
	if len(errs) != 1 {
		t.Fatalf("ErrorEvents = %d (%v), want exactly 1 — one signal per reply, not one per entry", len(errs), errs)
	}
	if errs[0].Source != "processing" {
		t.Errorf("ErrorEvent source = %q, want %q — the server sent the unusable shape, not the model",
			errs[0].Source, "processing")
	}
	if !strings.Contains(errs[0].Err, "dropped 1 of 2") {
		t.Errorf("ErrorEvent = %q, want it to name how many entries were dropped", errs[0].Err)
	}
}

// TestAReplyWhoseOnlyNativeCallLacksAnIDFaults pins the other half of the control flow: with
// nothing left after the filter the reply reads exactly like one that carried no calls at all —
// the text parser gets its turn (a no-op on a native profile) and the empty-reply guard faults the
// Turn. The alternative, dispatching it, is what the drop exists to prevent.
//
// The off-ramp floor (ADR 0070) sits between the two now: a Config with no EnableMechanisms arms
// empty_response_recovery, which retries the empty reply in place up to maxPostResponseRetries
// times before the loop proceeds with the last one. So the same reply is scripted four times — the
// first call and its three retries — and the Turn ends on the same fault, with the same wording,
// after the recovery has had its turns.
func TestAReplyWhoseOnlyNativeCallLacksAnIDFaults(t *testing.T) {
	sink := &recordingSink{}
	ran := 0
	cfg := configWithTools(sink, fakeTool{name: "lookup", readOnly: true, ran: &ran, result: "never reached"})
	idLessReply := []provider.Delta{
		idLessCall("lookup", "{}"),
		{Kind: provider.DeltaDone, FinishReason: "tool_calls"},
	}
	attempts := 1 + maxPostResponseRetries
	scripts := make([][]provider.Delta, 0, attempts)
	for range attempts {
		scripts = append(scripts, idLessReply)
	}
	responder := &scriptedResponder{scripts: scripts}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "look it up"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}

	if res.Status != domain.StatusExchangeComplete || !res.Faulted {
		t.Errorf("StepResult = {Status:%q Faulted:%v}, want {Status:%q Faulted:true}",
			res.Status, res.Faulted, domain.StatusExchangeComplete)
	}
	if ran != 0 {
		t.Errorf("tool ran %d times, want 0 — no call survived the filter", ran)
	}
	if got := countEvents[domain.ToolCallEvent](sink.events); got != 0 {
		t.Errorf("ToolCallEvents = %d, want 0", got)
	}

	errs := errorEvents(sink.events)
	if len(errs) != attempts+1 {
		t.Fatalf("ErrorEvents = %d (%v), want %d — one drop per attempt, then the Turn's fault",
			len(errs), errs, attempts+1)
	}
	for i, e := range errs[:attempts] {
		if e.Source != "processing" || !strings.Contains(e.Err, "dropped 1 of 1") {
			t.Errorf("ErrorEvent %d = {Source:%q Err:%q}, want the processing drop signal", i, e.Source, e.Err)
		}
	}
	want := "upstream returned an empty reply (finish: tool_calls)"
	if last := errs[attempts]; last.Source != "loop" || last.Err != want {
		t.Errorf("last ErrorEvent = {Source:%q Err:%q}, want {Source:%q Err:%q}",
			last.Source, last.Err, "loop", want)
	}
	// Nothing was committed: the user message alone survives a faulted Turn.
	if got := a.conv.Len(); got != 1 {
		t.Errorf("conversation len = %d, want 1", got)
	}
}
