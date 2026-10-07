package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tools"
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
	responder := scriptedResponder(t,
		retryableErrorTurn(transientFaultMsg), // the blip that arms the re-stream
		contentTurn("resumed"),                // reached only by the re-attempt after the cancel
	)
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
	if responder.calls() != 1 {
		t.Errorf("Upstream calls = %d, want 1 — the hold-off must not re-stream into a dead context", responder.calls())
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

// eofFaultMsg is the text the provider builds for a body cut before its terminator — the
// `unexpected EOF` a server or proxy dropping the connection mid-reply leaves the chunked reader
// with — which arrives at the loop marked Retryable exactly like the in-band 502.
const eofFaultMsg = "apogee: read stream: unexpected EOF"

// TestRespondAndReviewReStreamsAMidStreamEOF pins that a mid-stream EOF rides the same per-Turn
// re-stream budget as a transient in-band error: the Turn re-sends the request, the second reply
// commits, and the recovered Turn stays quiet — one StreamResetEvent, no ErrorEvent. It used to
// fail the Turn on the spot, because the read fault carried no Retryable verdict.
func TestRespondAndReviewReStreamsAMidStreamEOF(t *testing.T) {
	sink := &recordingSink{}
	responder := scriptedResponder(t,
		retryableErrorTurn(eofFaultMsg), // the connection drops mid-reply
		contentTurn("after the cut"),    // the re-stream's answer
	)
	a, err := newAgent(baseConfig(sink), responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	shortRestreamHoldoff(t, a)
	req, _ := a.buildRequest(0)
	run := &turnRun{turn: 0, req: req}

	resp, outcome, carried := a.respondAndReview(context.Background(), run)

	if outcome != turnOK {
		t.Fatalf("outcome = %v, want turnOK — the EOF is re-streamed, not surfaced", outcome)
	}
	if carried != "" {
		t.Errorf("carried message = %q, want empty", carried)
	}
	if resp == nil || resp.Text() != "after the cut" {
		t.Errorf("resp = %+v, want the re-streamed reply %q committed", resp, "after the cut")
	}
	if responder.calls() != 2 {
		t.Errorf("Upstream calls = %d, want 2 — one cut stream, one re-stream", responder.calls())
	}
	if run.restreamsSpent != 1 {
		t.Errorf("restreamsSpent = %d, want 1 — the EOF spends one of the Turn's re-streams", run.restreamsSpent)
	}
	if got := countEvents[domain.StreamResetEvent](sink.events); got != 1 {
		t.Errorf("StreamResetEvents = %d, want 1 — the tokens streamed before the cut are superseded", got)
	}
	if errs := errorEvents(sink.events); len(errs) != 0 {
		t.Errorf("ErrorEvents = %v, want none — a recovered re-stream is silent", errs)
	}
}

// TestRestreamHoldoffLadder pins the hold-off ladder on the pure function, where the wall clock
// cannot blur it: the first re-stream keeps the base wait every re-stream used to get, and each
// later one waits twice the one before — 1×, 2×, 4× of defaultRestreamHoldoff for the three
// re-streams the default budget allows, on an Agent whose restreamHoldoff field is unset.
func TestRestreamHoldoffLadder(t *testing.T) {
	tests := []struct {
		rung int
		want time.Duration
	}{
		{rung: 0, want: defaultRestreamHoldoff},
		{rung: 1, want: 2 * defaultRestreamHoldoff},
		{rung: 2, want: 4 * defaultRestreamHoldoff},
	}

	a := &Agent{}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("re-stream %d", tc.rung), func(t *testing.T) {
			if got := a.restreamHoldoffFor(tc.rung); got != tc.want {
				t.Errorf("restreamHoldoffFor(%d) = %v, want %v", tc.rung, got, tc.want)
			}
		})
	}
}

// TestReStreamBudgetZeroNeverReStreams pins the budget's floor: a Turn with no re-stream left
// fails on the first transient fault exactly as a plain fault fails — one Upstream call, no
// StreamResetEvent, the fault surfaced as the Turn's ErrorEvent. Until the `re-stream-budget:`
// key reaches Config the only zero the loop can meet is a counter already at the budget, so the
// test walks that counter to it; the same branch decides both.
func TestReStreamBudgetZeroNeverReStreams(t *testing.T) {
	const blip = "provider swapped out"
	sink := &recordingSink{}
	responder := scriptedResponder(t,
		retryableErrorTurn(blip), // transient, but there is no budget to spend on it
		contentTurn("unreached"),
	)
	a, err := newAgent(baseConfig(sink), responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	shortRestreamHoldoff(t, a)
	req, _ := a.buildRequest(0)
	run := &turnRun{turn: 0, req: req, restreamsSpent: a.restreamBudget()}

	resp, outcome, _ := a.respondAndReview(context.Background(), run)

	if outcome != turnFailed || resp != nil {
		t.Fatalf("outcome = %v, resp = %+v; want turnFailed with no response — nothing left to re-stream with", outcome, resp)
	}
	if responder.calls() != 1 {
		t.Errorf("Upstream calls = %d, want 1 — a spent budget never re-streams", responder.calls())
	}
	if got := countEvents[domain.StreamResetEvent](sink.events); got != 0 {
		t.Errorf("StreamResetEvents = %d, want 0 — no re-stream was announced", got)
	}
	errs := errorEvents(sink.events)
	if len(errs) != 1 || !strings.Contains(errs[0].Err, blip) {
		t.Errorf("ErrorEvents = %v, want exactly the transient fault surfaced", errs)
	}
}

// TestReStreamBudgetNilConfigDefaultsToThree pins the pointer contract of Config.RestreamBudget:
// the nil an embedder's zero Config and baseConfig carry is the engine's default of three — three
// transient faults are ridden out and the fourth attempt lands — while a pointer to 0 is a VALUE,
// "never re-stream": the first transient fault fails the Turn on the spot. The two cases share one
// script so the only difference between them is the budget the Config carries.
func TestReStreamBudgetNilConfigDefaultsToThree(t *testing.T) {
	zero := 0
	tests := []struct {
		name        string
		budget      *int
		wantOutcome turnOutcome
		wantCalls   int
		wantResets  int
	}{
		{name: "a nil budget rides out three faults", budget: nil, wantOutcome: turnOK, wantCalls: 4, wantResets: 3},
		{name: "a pointer to 0 never re-streams", budget: &zero, wantOutcome: turnFailed, wantCalls: 1, wantResets: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			responder := scriptedResponder(t,
				retryableErrorTurn("first blip"),
				retryableErrorTurn("second blip"),
				retryableErrorTurn("third blip"),
				contentTurn("recovered"),
			)
			cfg := baseConfig(sink)
			cfg.RestreamBudget = tc.budget
			a, err := newAgent(cfg, responder)
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			shortRestreamHoldoff(t, a)
			req, _ := a.buildRequest(0)
			run := &turnRun{turn: 0, req: req}

			_, outcome, _ := a.respondAndReview(context.Background(), run)

			if outcome != tc.wantOutcome {
				t.Fatalf("outcome = %v, want %v", outcome, tc.wantOutcome)
			}
			if responder.calls() != tc.wantCalls {
				t.Errorf("Upstream calls = %d, want %d", responder.calls(), tc.wantCalls)
			}
			if got := countEvents[domain.StreamResetEvent](sink.events); got != tc.wantResets {
				t.Errorf("StreamResetEvents = %d, want %d — one per re-stream", got, tc.wantResets)
			}
		})
	}
}

// TestReStreamBudgetComesFromConfig pins that the budget the Turn spends is the Config's number and
// not the engine's constant: a Config carrying 1 re-streams once and the second transient fault
// fails the Turn — two requests, one StreamResetEvent, the second fault surfaced.
func TestReStreamBudgetComesFromConfig(t *testing.T) {
	one := 1
	sink := &recordingSink{}
	responder := scriptedResponder(t,
		retryableErrorTurn("first blip"),  // spends the one re-stream
		retryableErrorTurn("second blip"), // nothing left to spend
		contentTurn("unreached"),
	)
	cfg := baseConfig(sink)
	cfg.RestreamBudget = &one
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	shortRestreamHoldoff(t, a)
	req, _ := a.buildRequest(0)
	run := &turnRun{turn: 0, req: req}

	resp, outcome, _ := a.respondAndReview(context.Background(), run)

	if outcome != turnFailed || resp != nil {
		t.Fatalf("outcome = %v, resp = %+v; want turnFailed with no response — the budget of 1 is spent", outcome, resp)
	}
	if responder.calls() != 2 {
		t.Errorf("Upstream calls = %d, want 2 — one fault, one re-stream, then the Turn fails", responder.calls())
	}
	if got := countEvents[domain.StreamResetEvent](sink.events); got != 1 {
		t.Errorf("StreamResetEvents = %d, want 1 — the one re-stream announced", got)
	}
	errs := errorEvents(sink.events)
	if len(errs) != 1 || !strings.Contains(errs[0].Err, "second blip") {
		t.Errorf("ErrorEvents = %v, want exactly the second fault surfaced", errs)
	}
}

// TestRestreamHoldOffThatElapsesStillReStreams is the untouched half of the same branch: when the
// wait ends because it EXPIRED rather than because the ctx died, the Turn re-streams exactly as it
// always did. Without this the fix above could pass by never re-streaming at all.
func TestRestreamHoldOffThatElapsesStillReStreams(t *testing.T) {
	sink := &recordingSink{}
	responder := scriptedResponder(t,
		retryableErrorTurn(transientFaultMsg),
		contentTurn("recovered"),
	)
	a, err := newAgent(baseConfig(sink), responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	shortRestreamHoldoff(t, a)
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
	if responder.calls() != 2 {
		t.Errorf("Upstream calls = %d, want 2 — an elapsed hold-off re-streams the same request", responder.calls())
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
	// A Delta script rather than a stubllm Turn: the stub numbers an id-less call by position, so
	// the very shape under test — a call with NO id — cannot reach the loop from the wire.
	responder := scriptedDeltas{
		{Kind: provider.DeltaToolCall, ToolCall: &provider.ToolCall{
			ID: "c1", Type: "function",
			Function: provider.FunctionCall{Name: "lookup", Arguments: `{"q":"meaning"}`},
		}},
		idLessCall("lookup", `{"q":"unusable"}`),
		{Kind: provider.DeltaDone, FinishReason: "tool_calls"},
	}

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
// The empty-response recovery Floor guard sits between the two now: it is on for every model
// (ADR 0071) and retries the empty reply in place up to maxPostResponseRetries times before the loop
// proceeds with the last one. So the same reply is scripted four times — the first call and its
// three retries — and the Turn ends on the same fault, with the same wording, after the recovery has
// had its turns.
func TestAReplyWhoseOnlyNativeCallLacksAnIDFaults(t *testing.T) {
	sink := &recordingSink{}
	ran := 0
	cfg := configWithTools(sink, fakeTool{name: "lookup", readOnly: true, ran: &ran, result: "never reached"})
	// A Delta script rather than a stubllm Turn: the stub numbers an id-less call by position, so
	// the shape under test — a call with NO id — cannot reach the loop from the wire. scriptedDeltas
	// plays it on every request, which covers the first call and its three retries alike.
	responder := scriptedDeltas{
		idLessCall("lookup", "{}"),
		{Kind: provider.DeltaDone, FinishReason: "tool_calls"},
	}
	attempts := 1 + maxPostResponseRetries

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

// pngBytes is a PNG signature followed by filler: enough for the magic-byte sniff (imageMediaType),
// which is all the engine reads of an image before it rides the message.
const pngBytes = "\x89PNG\r\n\x1a\nfiller"

// imageAgent builds a scripted Agent on a server named "box" whose workspace is dir, with the
// server's vision opt-in as given, plus the sink that captured its events.
func imageAgent(t *testing.T, dir string, vision bool) (*Agent, *recordingSink) {
	t.Helper()
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.WorkspaceDir = dir
	cfg.ServerName = "box"
	cfg.Vision = vision
	a, err := newAgent(cfg, echoResponder(t, "ok"))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	return a, sink
}

// submitAndStep submits in and drives the opening Turn, returning the committed user message.
func submitAndStep(t *testing.T, a *Agent, in domain.UserInput) domain.Message {
	t.Helper()
	if err := a.Submit(in); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}
	return a.conv.At(0)
}

// TestFileRefImage_BecomesAnImagePart: on a vision server an @ref whose bytes are an image rides the
// message as an image part named by the ref — the media type read from the magic bytes, so a JPEG
// saved without an extension and a WebP both arrive typed — and adds nothing to the text.
func TestFileRefImage_BecomesAnImagePart(t *testing.T) {
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "shot.png", pngBytes)
	writeWorkspaceFile(t, dir, "capture", "\xff\xd8\xff\xe0jpeg")
	writeWorkspaceFile(t, dir, "art.webp", "RIFF\x10\x00\x00\x00WEBPVP8 ")
	writeWorkspaceFile(t, dir, "anim.gif", "GIF89a....")

	a, sink := imageAgent(t, dir, true)
	got := submitAndStep(t, a, domain.UserInput{Text: "look", FileRefs: []string{"shot.png", "capture", "art.webp", "anim.gif"}})

	if got.Content != "look" {
		t.Errorf("content = %q, want just the text (an image ref adds no block)", got.Content)
	}
	want := []struct{ name, mediaType string }{
		{"shot.png", "image/png"}, {"capture", "image/jpeg"}, {"art.webp", "image/webp"}, {"anim.gif", "image/gif"},
	}
	if len(got.Images) != len(want) {
		t.Fatalf("message carries %d images, want %d: %+v", len(got.Images), len(want), got.Images)
	}
	for i, w := range want {
		if got.Images[i].Name != w.name || got.Images[i].MediaType != w.mediaType {
			t.Errorf("image %d = %s (%s), want %s (%s)", i, got.Images[i].Name, got.Images[i].MediaType, w.name, w.mediaType)
		}
	}
	if string(got.Images[0].Data) != pngBytes {
		t.Errorf("image bytes = %q, want the file as read", got.Images[0].Data)
	}
	if hasEvent[domain.ErrorEvent](sink.events) {
		t.Error("an image ref on a vision server emitted an ErrorEvent")
	}
}

// TestFileRefImage_TextNamedPNGStaysText: the sniff reads bytes, not names — a text file called
// notes.png is injected as text and carries no image part.
func TestFileRefImage_TextNamedPNGStaysText(t *testing.T) {
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "notes.png", "PLAIN TEXT MARKER")

	a, _ := imageAgent(t, dir, true)
	got := submitAndStep(t, a, domain.UserInput{Text: "read it", FileRefs: []string{"notes.png"}})

	if !strings.Contains(got.Content, "Referenced file `notes.png`:\n") || !strings.Contains(got.Content, "PLAIN TEXT MARKER") {
		t.Errorf("a text file named .png was not injected as text:\n%s", got.Content)
	}
	if len(got.Images) != 0 {
		t.Errorf("a text file named .png became %d image parts", len(got.Images))
	}
}

// TestFileRefImage_NonVisionServerIgnoresIt: on a server without `vision: true` an image @ref is not
// a refusal of the message (composeUserMessage) — it is skipped through refIgnored with the vision
// refusal as its reason, and the Turn goes ahead with the text.
func TestFileRefImage_NonVisionServerIgnoresIt(t *testing.T) {
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "shot.png", pngBytes)

	a, sink := imageAgent(t, dir, false)
	got := submitAndStep(t, a, domain.UserInput{Text: "look", FileRefs: []string{"shot.png"}})

	const want = `@shot.png could not be resolved and was ignored: ` +
		`server "box" does not accept images: set vision: true on its servers: entry`
	if !errorEventContaining(sink.events, want) {
		t.Errorf("no ErrorEvent reading %q; events: %+v", want, sink.events)
	}
	if got.Content != "look" || len(got.Images) != 0 {
		t.Errorf("message = %q with %d images, want just the text", got.Content, len(got.Images))
	}
}

// TestFileRefImage_OverTheCapsIsIgnored: an image @ref over the per-image cap, or one that would push
// the message's images past the per-message cap (counting the attached UserInput.Images), is skipped
// with a reason naming the file and the cap.
func TestFileRefImage_OverTheCapsIsIgnored(t *testing.T) {
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "huge.png", pngBytes+strings.Repeat("x", domain.MaxImageBytes))
	writeWorkspaceFile(t, dir, "half.png", pngBytes+strings.Repeat("x", domain.MaxMessageImageBytes/2))

	a, sink := imageAgent(t, dir, true)
	attached := domain.Image{Name: "pasted.png", MediaType: "image/png",
		Data: []byte(pngBytes + strings.Repeat("y", domain.MaxMessageImageBytes/2))}
	got := submitAndStep(t, a, domain.UserInput{Text: "look", FileRefs: []string{"huge.png", "half.png"},
		Images: []domain.Image{attached}})

	perImage := fmt.Sprintf(`@huge.png could not be resolved and was ignored: image "huge.png" is %d bytes, over the %d-byte cap on one image`,
		len(pngBytes)+domain.MaxImageBytes, domain.MaxImageBytes)
	if !errorEventContaining(sink.events, perImage) {
		t.Errorf("no ErrorEvent reading %q", perImage)
	}
	perMessage := fmt.Sprintf(`@half.png could not be resolved and was ignored: image "half.png" brings this message's images to %d bytes, over the %d-byte cap on one message`,
		2*len(pngBytes)+domain.MaxMessageImageBytes, domain.MaxMessageImageBytes)
	if !errorEventContaining(sink.events, perMessage) {
		t.Errorf("no ErrorEvent reading %q", perMessage)
	}
	if len(got.Images) != 1 || got.Images[0].Name != "pasted.png" {
		t.Errorf("message images = %+v, want only the attached pasted.png", got.Images)
	}
}

// TestSubmitImage_RidesTheMessage: images attached to a UserInput on a vision server land on the
// committed message, ahead of any @ref image.
func TestSubmitImage_RidesTheMessage(t *testing.T) {
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "shot.png", pngBytes)

	a, _ := imageAgent(t, dir, true)
	attached := domain.Image{Name: "pasted.png", MediaType: "image/png", Data: []byte(pngBytes)}
	got := submitAndStep(t, a, domain.UserInput{Text: "look", FileRefs: []string{"shot.png"}, Images: []domain.Image{attached}})

	if len(got.Images) != 2 || got.Images[0].Name != "pasted.png" || got.Images[1].Name != "shot.png" {
		t.Errorf("message images = %+v, want pasted.png then shot.png", got.Images)
	}
}

// TestSubmitImage_RefusedBeforeAnyRequest: Submit refuses attached images the bound server cannot
// take — any image on a server without `vision: true`, or one over a cap — and queues nothing.
func TestSubmitImage_RefusedBeforeAnyRequest(t *testing.T) {
	tests := []struct {
		name    string
		vision  bool
		images  []domain.Image
		wantErr string
	}{
		{
			name:    "non-vision server",
			images:  []domain.Image{{Name: "pasted.png", MediaType: "image/png", Data: []byte(pngBytes)}},
			wantErr: `server "box" does not accept images: set vision: true on its servers: entry`,
		},
		{
			name:   "over the per-image cap",
			vision: true,
			images: []domain.Image{{Name: "big.png", MediaType: "image/png", Data: make([]byte, domain.MaxImageBytes+1)}},
			wantErr: fmt.Sprintf(`image "big.png" is %d bytes, over the %d-byte cap on one image`,
				domain.MaxImageBytes+1, domain.MaxImageBytes),
		},
		{
			name:   "over the per-message cap",
			vision: true,
			images: []domain.Image{
				{Name: "a.png", MediaType: "image/png", Data: make([]byte, domain.MaxMessageImageBytes/2+1)},
				{Name: "b.png", MediaType: "image/png", Data: make([]byte, domain.MaxMessageImageBytes/2+1)},
			},
			wantErr: fmt.Sprintf(`image "b.png" brings this message's images to %d bytes, over the %d-byte cap on one message`,
				domain.MaxMessageImageBytes+2, domain.MaxMessageImageBytes),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := imageAgent(t, t.TempDir(), tc.vision)

			err := a.Submit(domain.UserInput{Text: "look", Images: tc.images})

			if err == nil || err.Error() != tc.wantErr {
				t.Fatalf("Submit error = %v, want %q", err, tc.wantErr)
			}
			if a.turns.pendingInput != nil {
				t.Error("a refused Submit left input pending")
			}
		})
	}
}

// TestImageVisionRefusal_NamesNoBlankServer: on a server without `vision: true` both refusal
// channels — Submit's refusal of attached images and an image @ref's ignored-reason — name the
// server when it has a name, and say "this server" rather than a blank `server ""` when it has none.
// On the one-run `--endpoint` entry (Config.ServerEphemeral) there is no entry to edit, so both
// channels advise adding one instead.
func TestImageVisionRefusal_NamesNoBlankServer(t *testing.T) {
	tests := []struct {
		name       string
		serverName string
		ephemeral  bool
		want       string
	}{
		{
			name:       "named server",
			serverName: "box",
			want:       `server "box" does not accept images: set vision: true on its servers: entry`,
		},
		{
			name:       "unnamed server",
			serverName: "",
			want:       "this server does not accept images: set vision: true on its servers: entry",
		},
		{
			name:       "endpoint server",
			serverName: "rented.invalid",
			ephemeral:  true,
			want: `server "rented.invalid" does not accept images: an --endpoint server cannot turn vision on` +
				` — add a servers: entry for it with vision: true and start with --server <name>`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name+"/submit", func(t *testing.T) {
			a, _ := imageAgent(t, t.TempDir(), false)
			a.cfg.ServerName = tc.serverName
			a.cfg.ServerEphemeral = tc.ephemeral

			err := a.Submit(domain.UserInput{Text: "look",
				Images: []domain.Image{{Name: "pasted.png", MediaType: "image/png", Data: []byte(pngBytes)}}})

			if err == nil || err.Error() != tc.want {
				t.Fatalf("Submit error = %v, want %q", err, tc.want)
			}
		})
		t.Run(tc.name+"/ref", func(t *testing.T) {
			dir := t.TempDir()
			writeWorkspaceFile(t, dir, "shot.png", pngBytes)
			a, sink := imageAgent(t, dir, false)
			a.cfg.ServerName = tc.serverName
			a.cfg.ServerEphemeral = tc.ephemeral

			submitAndStep(t, a, domain.UserInput{Text: "look", FileRefs: []string{"shot.png"}})

			want := "@shot.png could not be resolved and was ignored: " + tc.want
			if !errorEventContaining(sink.events, want) {
				t.Errorf("no ErrorEvent reading %q; events: %+v", want, sink.events)
			}
		})
	}
}

// TestInterjectImage_RefusedOnANonVisionServer: Interject gives Submit's refusal for attached images
// on a server without `vision: true`, and the conversation is untouched; on a vision server an
// image-only interjection is not empty and lands.
func TestInterjectImage_RefusedOnANonVisionServer(t *testing.T) {
	image := domain.Image{Name: "pasted.png", MediaType: "image/png", Data: []byte(pngBytes)}

	cfg := interjectConfig(&recordingSink{})
	cfg.ServerName = "box"
	a, _ := interjectAgentAtBoundary(t, cfg)
	before := a.conv.Len()
	err := a.Interject(context.Background(), domain.UserInput{Text: "see this", Images: []domain.Image{image}})
	const want = `server "box" does not accept images: set vision: true on its servers: entry`
	if err == nil || err.Error() != want {
		t.Fatalf("Interject error = %v, want %q", err, want)
	}
	if a.conv.Len() != before {
		t.Errorf("a refused interjection changed the conversation: %d messages, want %d", a.conv.Len(), before)
	}

	cfg = interjectConfig(&recordingSink{})
	cfg.Vision = true
	a, _ = interjectAgentAtBoundary(t, cfg)
	if err := a.Interject(context.Background(), domain.UserInput{Images: []domain.Image{image}}); err != nil {
		t.Fatalf("image-only Interject on a vision server: %v", err)
	}
	if got := a.conv.At(a.conv.Len() - 1); len(got.Images) != 1 || !got.Interjected {
		t.Errorf("interjected message = %+v, want one image, marked interjected", got)
	}
}

// TestImageVisionRidesTheServerBinding: the vision opt-in moves with the server — a `/server` switch
// to an entry without it turns it off and a model rebind leaves it where the switch put it, and a
// routed delegation takes the target's while an unrouted one inherits the parent's.
func TestImageVisionRidesTheServerBinding(t *testing.T) {
	const second = "http://second.local:2222"
	dialer := dialerAnswering(func(string) provider.Responder { return echoResponder(t, "ok") })
	cfg := baseConfig(&recordingSink{})
	cfg.Vision = true
	a, err := New(cfg, WithDialer(dialer.dial))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.SwitchUpstream(UpstreamSpec{Endpoint: second}); err != nil {
		t.Fatalf("SwitchUpstream: %v", err)
	}
	if a.cfg.Vision {
		t.Error("a switch to an entry without vision kept the retired server's opt-in")
	}
	if err := a.SwitchUpstream(UpstreamSpec{Endpoint: second, Vision: true}); err != nil {
		t.Fatalf("SwitchUpstream: %v", err)
	}
	if err := a.Rebind(RebindSpec{Model: testModel}); err != nil {
		t.Fatalf("Rebind: %v", err)
	}
	if !a.cfg.Vision {
		t.Error("a model rebind reset the server's vision opt-in")
	}

	parent := routingParent(t)
	if child := spawn(t, parent); child.cfg.Vision {
		t.Error("an unrouted child of a non-vision parent has vision")
	}
	target := routedTarget()
	target.Vision = true
	parent.SetDelegationTarget(target)
	if child := spawn(t, parent); !child.cfg.Vision {
		t.Error("a child routed to a vision target did not take the target's opt-in")
	}
}

// TestAssembleResponseDecodesSchemaTypedArgs: a recovered markdown-fenced call carries every
// value as a verbatim string, and assembleResponse decodes those whose property in the tool
// menu's schema does not admit a string — integer, boolean, array, and an MCP-style
// anyOf integer/null — while a string-typed param keeps its text even when it reads as JSON. The
// property is found through the folded key, so START_LINE reaches start_line.
func TestAssembleResponseDecodesSchemaTypedArgs(t *testing.T) {
	root := t.TempDir()
	menuTools := []domain.Tool{
		tools.NewReadFile(root, domain.ReadMounts{}),
		tools.NewListDir(root, domain.ReadMounts{}),
		tools.NewAskUser(nil),
		tools.NewWriteFile(root),
	}
	menu := make([]domain.ToolDef, 0, len(menuTools)+1)
	for _, tool := range menuTools {
		menu = append(menu, domain.ToolDef{Name: tool.Name(), Schema: tool.Schema()})
	}
	menu = append(menu, domain.ToolDef{
		Name:   "fetch",
		Schema: []byte(`{"type":"object","properties":{"limit":{"anyOf":[{"type":"integer"},{"type":"null"}]}}}`),
	})
	view := domain.NewRequest(testModel, nil, menu, domain.Budget{}, 0).View()
	cfg := baseConfig(&recordingSink{})
	cfg.Profile = domain.ModelProfile{ToolCallFormat: domain.FormatMarkdownFenced}
	a := newProfileAgent(t, cfg, echoResponder(t, ""))
	fenced := func(tool string, args ...string) string {
		var b strings.Builder
		b.WriteString("```tool\nTOOL_NAME\n" + tool + "\n")
		for i := 0; i+1 < len(args); i += 2 {
			b.WriteString("BEGIN_ARG\n" + args[i] + "\nEND_ARG\n" + args[i+1] + "\n")
		}
		b.WriteString("```")
		return b.String()
	}
	packageJSON := "{\n  \"name\": \"demo\",\n  \"private\": true\n}"
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "integer params decode to numbers",
			content: fenced("read_file", "path", "main.go", "start_line", "42", "max_lines", "10"),
			want:    `{"max_lines":10,"path":"main.go","start_line":42}`,
		},
		{
			name:    "boolean param decodes",
			content: fenced("list_dir", "path", ".", "recursive", "true"),
			want:    `{"path":".","recursive":true}`,
		},
		{
			name:    "array param decodes",
			content: fenced("ask_user", "question", "Which one?", "choices", `["red", "blue"]`),
			want:    `{"choices":["red", "blue"],"question":"Which one?"}`,
		},
		{
			name:    "string param holding 123 stays a string",
			content: fenced("read_file", "path", "123"),
			want:    `{"path":"123"}`,
		},
		{
			name:    "string content holding package.json stays a string",
			content: fenced("write_file", "path", "package.json", "content", packageJSON),
			want:    `{"content":` + mustQuote(t, packageJSON) + `,"path":"package.json"}`,
		},
		{
			name:    "anyOf integer/null param decodes to a number",
			content: fenced("fetch", "limit", "5"),
			want:    `{"limit":5}`,
		},
		{
			name:    "folded key spelling decodes",
			content: fenced("read_file", "path", "main.go", "START_LINE", "42"),
			want:    `{"START_LINE":42,"path":"main.go"}`,
		},
		{
			name:    "a value that does not decode stays the string",
			content: fenced("read_file", "path", "main.go", "start_line", "forty"),
			want:    `{"path":"main.go","start_line":"forty"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := a.assembleResponse(0, view, completion{content: tc.content, finish: domain.FinishStop}, nil)

			calls := resp.ToolCalls()
			if len(calls) != 1 {
				t.Fatalf("ToolCalls = %d, want 1", len(calls))
			}
			if got := string(calls[0].Arguments); got != tc.want {
				t.Errorf("Arguments = %s, want %s", got, tc.want)
			}
		})
	}
}

// mustQuote JSON-encodes s as a string literal.
func mustQuote(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal %q: %v", s, err)
	}
	return string(b)
}

// malformedArgumentsWant is the answer a Malformed call gets for raw argument text that is cut
// short — the JSON error encoding/json reports for it, then raw as sent.
func malformedArgumentsWant(parseError, raw string) string {
	return fmt.Sprintf("arguments were not valid JSON (%s); you sent: %s", parseError, raw)
}

// runMalformedExchange drives cfg against turns to the end of the Exchange, with the tool-call
// repair Floor guard off so a Malformed call reaches dispatch, and returns the Agent and its
// upstream for the assertions.
func runMalformedExchange(t *testing.T, cfg domain.Config, turns ...stubllm.Turn) (*Agent, *scriptedUpstream) {
	t.Helper()
	cfg.Floor.DisableToolCallRepair = true
	upstream := scriptedResponder(t, turns...)
	a, err := newAgent(cfg, upstream)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "read them"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return a, upstream
}

// resultFor is the depth-0 tool result committed for callID, failing the test when there is none.
func resultFor(t *testing.T, events []domain.Event, callID string) domain.ToolResult {
	t.Helper()
	for _, result := range subAgentResults(events) {
		if result.CallID == callID {
			return result
		}
	}
	t.Fatalf("no tool result for %s", callID)
	return domain.ToolResult{}
}

// TestLoopMalformedNativeCallAnsweredSiblingsRun is the audit High's regression: one reply carries
// a well-formed call and a call whose arguments are cut short. The well-formed sibling runs; the
// bad one is answered with the parse error and its raw text, committed with arguments {} — and
// sent back upstream that way — surfaced as a call and audited with the malformed decision, with
// no ErrorEvent and no pre-tool-exec firing for it.
func TestLoopMalformedNativeCallAnsweredSiblingsRun(t *testing.T) {
	sink := &recordingSink{}
	ran := 0
	var seen []string
	var mu sync.Mutex
	cfg := configWithTools(sink, fakeTool{name: "read", readOnly: true, ran: &ran, result: "contents of a"})
	cfg.Reactions = []domain.Reaction{preToolExecWatcher(&seen, &mu)}

	a, upstream := runMalformedExchange(t, cfg,
		stubllm.Turn{ToolCalls: []stubllm.ToolCall{
			{ID: "c1", Name: "read", Arguments: `{"path":"a"}`},
			{ID: "c2", Name: "read", Arguments: `{"path":`},
		}},
		contentTurn("done"),
	)

	if ran != 1 {
		t.Errorf("tool ran %d times, want 1 — the valid sibling runs, the malformed call never does", ran)
	}
	if good := resultFor(t, sink.events, "c1"); good.IsError || good.Content != "contents of a" {
		t.Errorf("sibling result = %+v, want the tool's own answer", good)
	}
	bad := resultFor(t, sink.events, "c2")
	if want := malformedArgumentsWant("unexpected end of JSON input", `{"path":`); !bad.IsError || bad.Content != want {
		t.Errorf("malformed result = %+v, want the error %q", bad, want)
	}
	assertCommittedArguments(t, a, "c2", "{}")
	assertWireArguments(t, upstream, "c2", "{}")
	if eventIndex(sink.events, "c2") < 0 {
		t.Error("no ToolCallEvent for the malformed call, want it surfaced like every call")
	}
	assertMalformedAudit(t, sink.events, "c2")
	if errs := errorEvents(sink.events); len(errs) != 0 {
		t.Errorf("ErrorEvents = %v, want none — the error tool-result row is the surface", errs)
	}
	mu.Lock()
	fired := slices.Clone(seen)
	mu.Unlock()
	if !slices.Equal(fired, []string{"c1"}) {
		t.Errorf("pre-tool-exec fired for %v, want only the well-formed c1", fired)
	}
}

// TestLoopMalformedNativeCallLongRawIsCut pins the quote's bound: a 300-rune raw argument text is
// quoted back as its first 200 runes followed by "…".
func TestLoopMalformedNativeCallLongRawIsCut(t *testing.T) {
	sink := &recordingSink{}
	cfg := configWithTools(sink, fakeTool{name: "read", readOnly: true, result: "unused"})
	raw := `{"path":"` + strings.Repeat("é", 291)

	runMalformedExchange(t, cfg, toolCallTurn("c1", "read", raw), contentTurn("done"))

	want := malformedArgumentsWant("unexpected end of JSON input", string([]rune(raw)[:200])+"…")
	if got := resultFor(t, sink.events, "c1"); got.Content != want {
		t.Errorf("malformed result = %q, want %q", got.Content, want)
	}
}

// TestLoopMalformedNativeCallToUnknownToolGetsUnknownToolAnswer pins the order of the dispatch
// facts: the registry miss is answered first, so an unknown tool with broken arguments gets the
// unknown-tool answer and no malformed audit record.
func TestLoopMalformedNativeCallToUnknownToolGetsUnknownToolAnswer(t *testing.T) {
	sink := &recordingSink{}
	cfg := configWithTools(sink, fakeTool{name: "read", readOnly: true, result: "unused"})

	a, _ := runMalformedExchange(t, cfg, toolCallTurn("c1", "raed", `{"path":`), contentTurn("done"))

	want := a.unknownToolResult(domain.ToolCall{ID: "c1", Tool: "raed"}).Content
	if got := resultFor(t, sink.events, "c1"); got.Content != want {
		t.Errorf("result = %q, want the unknown-tool answer %q", got.Content, want)
	}
	if audits := auditEvents(sink.events); len(audits) != 0 {
		t.Errorf("AuditEvents = %+v, want none for an unknown tool", audits)
	}
}

// TestLoopMalformedNativeCallToModeWithdrawnToolGetsTheMalformedAnswer pins that the mode does not
// pre-empt the parse error: a write tool Plan mode withdraws is still in the registry, so its
// broken call is answered with the malformed error, not a mode refusal.
func TestLoopMalformedNativeCallToModeWithdrawnToolGetsTheMalformedAnswer(t *testing.T) {
	sink := &recordingSink{}
	ran := 0
	cfg := configWithTools(sink, fakeTool{name: "write", ran: &ran, result: "unused"})
	cfg.Mode = domain.ModePlan

	runMalformedExchange(t, cfg, toolCallTurn("c1", "write", `[1]`), contentTurn("done"))

	if want := malformedArgumentsWant("not a JSON object", `[1]`); resultFor(t, sink.events, "c1").Content != want {
		t.Errorf("result = %q, want %q", resultFor(t, sink.events, "c1").Content, want)
	}
	if ran != 0 {
		t.Errorf("tool ran %d times, want 0", ran)
	}
}

// TestLoopMalformedNativeCallSubAgentIsALeaf pins that a malformed sub_agent call is no
// delegation: under a ceiling of two (two rounds × width 1), a reply of [bad, good, good]
// sub_agent calls answers the bad one with the malformed error, mints it no run id, and runs both
// good delegations — the bad one took no ceiling index.
func TestLoopMalformedNativeCallSubAgentIsALeaf(t *testing.T) {
	sink := &recordingSink{}
	call := func(id, args string) provider.Delta {
		return provider.Delta{Kind: provider.DeltaToolCall, ToolCall: &provider.ToolCall{
			ID: id, Type: "function", Function: provider.FunctionCall{Name: tools.SubAgentToolName, Arguments: args},
		}}
	}
	reply := []provider.Delta{
		call("c0", `{"task":`),
		call("c1", subAgentArgs("task one")),
		call("c2", subAgentArgs("task two")),
		{Kind: provider.DeltaDone, FinishReason: "tool_calls"},
	}
	up := newRoutedResponder().
		route("delegate two things", nil, reply).
		route("task one", nil, contentScript("child one done")).
		route("task two", nil, contentScript("child two done")).
		route("delegate two things", nil, contentScript("parent done"))
	cfg := subAgentConfig(sink, domain.ModeAskBefore)
	cfg.ParallelAgents = 1
	cfg.Delegation.FanOutRounds = 2
	cfg.Floor.DisableToolCallRepair = true
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "delegate two things"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if want := malformedArgumentsWant("unexpected end of JSON input", `{"task":`); resultFor(t, sink.events, "c0").Content != want {
		t.Errorf("c0 result = %q, want %q", resultFor(t, sink.events, "c0").Content, want)
	}
	for id, want := range map[string]string{"c1": "child one done", "c2": "child two done"} {
		if got := resultFor(t, sink.events, id); got.IsError || !strings.Contains(got.Content, want) {
			t.Errorf("%s result = %+v, want the child's report %q — the malformed call took a ceiling index", id, got, want)
		}
	}
	for _, e := range sink.events {
		if ce, ok := e.(domain.ToolCallEvent); ok && ce.Call.ID == "c0" && ce.SpawnRunID != "" {
			t.Errorf("malformed sub_agent call minted run id %q, want none", ce.SpawnRunID)
		}
	}
}

// assertCommittedArguments checks the committed assistant message carries callID with args.
func assertCommittedArguments(t *testing.T, a *Agent, callID, args string) {
	t.Helper()
	for i := 0; i < a.conv.Len(); i++ {
		for _, call := range a.conv.At(i).ToolCalls {
			if call.ID == callID {
				if string(call.Arguments) != args {
					t.Errorf("committed %s arguments = %s, want %s", callID, call.Arguments, args)
				}
				return
			}
		}
	}
	t.Errorf("no committed assistant message carries %s", callID)
}

// assertWireArguments checks the next request upstream echoed callID back with args.
func assertWireArguments(t *testing.T, upstream *scriptedUpstream, callID, args string) {
	t.Helper()
	for _, msg := range upstream.last().Messages {
		for _, call := range msg.ToolCalls {
			if call.ID == callID {
				if call.Arguments != args {
					t.Errorf("wire %s arguments = %s, want %s", callID, call.Arguments, args)
				}
				return
			}
		}
	}
	t.Errorf("the next request carried no call %s", callID)
}

// assertMalformedAudit checks callID was audited exactly once, with the malformed decision.
func assertMalformedAudit(t *testing.T, events []domain.Event, callID string) {
	t.Helper()
	var decisions []string
	for _, audit := range auditEvents(events) {
		if audit.CallID == callID {
			decisions = append(decisions, audit.Decision)
		}
	}
	if !slices.Equal(decisions, []string{string(security.AuditMalformedArguments)}) {
		t.Errorf("%s audit decisions = %v, want exactly [%s]", callID, decisions, security.AuditMalformedArguments)
	}
}
