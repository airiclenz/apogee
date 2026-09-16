package agent

// White-box capstone harness (P0.6e, re-homed to internal/agent by P1.0). It lives in
// package agent so it can inject a deterministic upstream through the unexported
// newAgent/resumeAgent seam — the provider seam stays internal (Decision C), so there
// is no public way to supply one, and the full Turn cannot be driven black-box. The
// public-API validation paths that need no fake live in the black-box apogee_test
// package (../../apogee_test.go).
//
// The upstream a test scripts here is a stubllm Script played IN PROCESS: the real provider
// client decodes what the stub's handler writes, over a pipe rather than a socket, so an engine
// test and a driver test exercise one decoder against one wire shape (ADR 0062). The pure Delta
// fakes that used to stand in for the client are gone; what remains as a hand-written Responder
// is the handful of behaviours no Script expresses (a stream that blocks until cancelled, a
// window-sized overflow, a Close counter).

import (
	"context"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/stubllm"
)

// testModel is the model id every scripted upstream advertises and every request names — the
// same id baseConfig binds, so the wire the stub logs is the wire a configured session sends.
const testModel = "test-model"

// scriptedUpstream is a stubllm Script played in process: the real provider client the engine
// streams from, over the stub's pipe transport, and the Server whose request log a test reads
// back where a fake used to capture requests itself. It IS the provider client — Stream, Close
// and SetModel are the client's own — so the engine cannot tell it from a configured one.
type scriptedUpstream struct {
	*provider.Client
	server *stubllm.Server
}

// scriptResponder plays script through the real provider client over stubllm's in-process
// transport: parity with the HTTP path by construction, one decoder. Retries are off, so an
// unanticipated request's 500 — or a scripted `http: 500` turn — faults on the spot instead
// of being retried three times with backoff. A script that names no model plays as testModel.
func scriptResponder(t testing.TB, script stubllm.Script) *scriptedUpstream {
	t.Helper()
	if script.Model == "" {
		script.Model = testModel
	}
	server := stubllm.InProcess(t, script)
	client := provider.NewClient("http://stubllm", script.Model,
		provider.WithHTTPClient(&http.Client{Transport: server.Transport()}),
		provider.WithMaxRetries(0),
	)
	return &scriptedUpstream{Client: client, server: server}
}

// scriptedResponder plays turns in order — request N is answered by turn N — the multi-Turn
// driver a test scripts "ask for a tool" then "finish" with. A request past the last turn is
// the stub's 500 naming it, which the client surfaces as a terminal fault.
func scriptedResponder(t testing.TB, turns ...stubllm.Turn) *scriptedUpstream {
	t.Helper()
	if len(turns) == 0 {
		turns = []stubllm.Turn{{HTTP: &stubllm.HTTPReply{Status: http.StatusInternalServerError, Body: "scriptedResponder: out of scripts"}, Repeat: true}}
	}
	return scriptResponder(t, stubllm.Script{Turns: turns})
}

// echoResponder answers every request with one fixed assistant message — the canned-reply
// upstream, and a repeating turn so the count of requests is never the test's concern.
func echoResponder(t testing.TB, reply string) *scriptedUpstream {
	t.Helper()
	return scriptedResponder(t, stubllm.Turn{Text: reply, Repeat: true})
}

// requests is every request the upstream received, in order — what a test asserts the loop
// actually sent, message by message.
func (u *scriptedUpstream) requests() []stubllm.Request { return u.server.Requests() }

// calls is how many requests the upstream received.
func (u *scriptedUpstream) calls() int { return len(u.server.Requests()) }

// last is the most recent request the upstream received, or the zero Request when none has.
func (u *scriptedUpstream) last() stubllm.Request {
	requests := u.server.Requests()
	if len(requests) == 0 {
		return stubllm.Request{}
	}
	return requests[len(requests)-1]
}

// contentTurn is a turn that replies with text and ends on stop.
func contentTurn(text string) stubllm.Turn {
	return stubllm.Turn{Text: text}
}

// toolCallTurn is a turn that emits one native tool call and ends on tool_calls.
func toolCallTurn(id, name, args string) stubllm.Turn {
	return stubllm.Turn{ToolCalls: []stubllm.ToolCall{{ID: id, Name: name, Arguments: args}}}
}

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

// recordingResponder is the canned-reply fake that also captures the last request it was handed,
// as the provider sees it — the fields a stubllm request log does not yet carry (the effort
// dialect, the thinking effort) are what its remaining users assert. A pointer receiver so last
// survives across calls.
type recordingResponder struct {
	reply string
	last  provider.Request
}

func (r *recordingResponder) Stream(_ context.Context, req provider.Request) iter.Seq[provider.Delta] {
	r.last = req
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

// closingResponder is echoResponder plus the io.Closer seam the real provider client satisfies:
// it counts teardowns so the client-lifecycle tests can prove WHO closed an Upstream and how
// often (Agent.Close, SwitchUpstream retiring a replaced client, and the child that must close
// nothing because it only borrowed its parent's). A pointer receiver so closes survives the call.
type closingResponder struct {
	closes int
}

func (r *closingResponder) Stream(context.Context, provider.Request) iter.Seq[provider.Delta] {
	return streamReply("")
}

func (r *closingResponder) Close() error {
	r.closes++
	return nil
}

// firingReaction is an armed pre-request Reaction that records that it fired and reports the
// firing through its Outcome without touching the request — the bench's plain instrument, booked
// on every invocation because it SAYS it acted (fired means acted, so a Reaction that touches
// nothing and says nothing is never booked).
func firingReaction(id string, fired *bool) domain.Reaction {
	return domain.Reaction{
		ID:     id,
		Origin: domain.OriginEngine,
		Class:  domain.ClassObserve,
		On:     []domain.Moment{domain.MomentPreRequest},
		Handler: domain.PreRequestFunc(func(context.Context, *domain.Request) (domain.Outcome, error) {
			*fired = true
			return domain.Outcome{Edited: true}, nil
		}),
	}
}

// panickingReaction is an armed Reaction that panics — the input for the
// recover-at-extension-boundary guarantee.
func panickingReaction(id string) domain.Reaction {
	return domain.Reaction{
		ID:     id,
		Origin: domain.OriginEngine,
		Class:  domain.ClassObserve,
		On:     []domain.Moment{domain.MomentPreRequest},
		Handler: domain.PreRequestFunc(func(context.Context, *domain.Request) (domain.Outcome, error) {
			panic("reaction boom")
		}),
	}
}

// ---------------------------------------------------------------------------

func baseConfig(sink domain.EventSink) domain.Config {
	return domain.Config{
		Endpoint: "http://localhost:0",
		Model:    testModel,
		Events:   sink,
	}
}

// stepOnce submits text and advances the loop exactly ONE Turn, returning that Turn's boundary
// StepResult — the single-Turn counterpart of runExchange, for a test that needs to inspect the
// loop between Turns rather than at the end of an Exchange.
func stepOnce(t *testing.T, a *Agent, text string) domain.StepResult {
	t.Helper()
	if err := a.Submit(domain.UserInput{Text: text}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	return res
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
// Submit → Step (observe the armed Reaction fire + the assistant message at the
// quiescent boundary) → Snapshot → Resume → Submit → Step, proving the resumed Agent
// continues the restored conversation.
func TestHarness_FullCapstonePath(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	fired := false
	cfg.Reactions = []domain.Reaction{firingReaction("capstone_probe", &fired)}

	a, err := newAgent(cfg, echoResponder(t, "hello from model"))
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
		t.Error("the armed pre-request Reaction did not fire")
	}
	if !hasEvent[domain.ReactionFiredEvent](sink.events) {
		t.Error("no ReactionFiredEvent emitted for the armed Reaction")
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
	cfg2.Reactions = cfg.Reactions
	b, err := resumeAgent(cfg2, snap, echoResponder(t, "second reply"))
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
	a, err := newAgent(baseConfig(&recordingSink{}), echoResponder(t, "ok"))
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
	b, err := resumeAgent(baseConfig(sink2), snap, echoResponder(t, "recovered"))
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

// TestHarness_PanicRecovery proves a panicking Reaction becomes an ErrorEvent at a clean
// boundary and the loop survives a second Step (the host is never unwound). The Turn itself is
// untouched: a recovered Reaction degrades to one that did nothing, so the request still goes
// out (ADR 0076 stage-1 header call).
func TestHarness_PanicRecovery(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Reactions = []domain.Reaction{panickingReaction("panic_probe")}
	a, err := newAgent(cfg, echoResponder(t, "answered anyway"))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	if err := a.Submit(domain.UserInput{Text: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step returned a loop error on a Reaction panic: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("Step status = %q, want %q", res.Status, domain.StatusExchangeComplete)
	}
	if !hasEvent[domain.ErrorEvent](sink.events) {
		t.Error("no ErrorEvent emitted for the panicking Reaction")
	}
	if me, ok := firstMessageEvent(t, sink.events); !ok || me.Text != "answered anyway" {
		t.Errorf("MessageEvent = %+v (ok=%v), want the Turn to carry on past the recovered panic", me, ok)
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
