package agent

// P1.2 acceptance (the convergence): a fake Responder + a fake Tool drive a multi-Turn
// Exchange under the full Turn/Step state machine — stream → parse → post-response hooks →
// tool dispatch through Approval → post-tool-result → quiescent boundary. These tests
// assert: a multi-Turn tool Exchange completes; Approval is consulted in Ask-Before and
// bypassed in Plan; cancellation mid-tool yields StatusCancelled + a resumable snapshot; a
// panicking tool yields an ErrorEvent and the loop survives; and the ActionDefer
// feed-forward survives a snapshot and injects on the next request.

import (
	"context"
	"encoding/json"
	"iter"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// scriptedResponder yields a pre-scripted stream per call — the multi-Turn driver: call N
// returns scripts[N], so a test scripts "ask for a tool" then "finish".
type scriptedResponder struct {
	scripts [][]provider.Delta
	calls   int
}

func (r *scriptedResponder) Stream(_ context.Context, _ provider.Request) iter.Seq[provider.Delta] {
	i := r.calls
	r.calls++
	return func(yield func(provider.Delta) bool) {
		if i >= len(r.scripts) {
			yield(provider.Delta{Kind: provider.DeltaError, Err: "scriptedResponder: out of scripts"})
			return
		}
		for _, d := range r.scripts[i] {
			if !yield(d) {
				return
			}
		}
	}
}

// toolCallScript is a stream that emits one native tool call then a tool_calls finish.
func toolCallScript(id, name, args string) []provider.Delta {
	return []provider.Delta{
		{Kind: provider.DeltaToolCall, ToolCall: &provider.ToolCall{
			ID:       id,
			Type:     "function",
			Function: provider.FunctionCall{Name: name, Arguments: args},
		}},
		{Kind: provider.DeltaDone, FinishReason: "tool_calls"},
	}
}

// contentScript is a stream that emits one content chunk then a stop finish.
func contentScript(text string) []provider.Delta {
	return []provider.Delta{
		{Kind: provider.DeltaContent, Content: text},
		{Kind: provider.DeltaDone, FinishReason: "stop"},
	}
}

// fakeTool is a configurable Tool: it records that it ran and returns a canned result, or
// defers to an execute override. It declares its read-only status so the Plan / Ask-Before
// gates can be exercised.
type fakeTool struct {
	name     string
	readOnly bool
	ran      *int
	result   string
	execute  func(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error)
}

func (t fakeTool) Name() string            { return t.name }
func (t fakeTool) Description() string     { return t.name + " tool" }
func (t fakeTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t fakeTool) ReadOnly() bool          { return t.readOnly }

func (t fakeTool) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if t.execute != nil {
		return t.execute(ctx, call)
	}
	if t.ran != nil {
		*t.ran++
	}
	return domain.ToolResult{CallID: call.ID, Content: t.result}, nil
}

// fakeApprover records how often it was consulted and returns a scripted verdict.
type fakeApprover struct {
	decision domain.ApprovalDecision
	err      error
	calls    int
}

func (a *fakeApprover) Approve(_ context.Context, _ domain.ApprovalRequest) (domain.ApprovalDecision, error) {
	a.calls++
	return a.decision, a.err
}

func configWithTools(sink domain.EventSink, tools ...domain.Tool) domain.Config {
	cfg := baseConfig(sink)
	reg := domain.NewToolRegistry()
	for _, t := range tools {
		_ = reg.Register(t)
	}
	cfg.Tools = reg
	return cfg
}

func lastMessageEvent(events []domain.Event) (domain.MessageEvent, bool) {
	out, ok := domain.MessageEvent{}, false
	for _, e := range events {
		if me, isMsg := e.(domain.MessageEvent); isMsg {
			out, ok = me, true
		}
	}
	return out, ok
}

// ---------------------------------------------------------------------------
// Multi-Turn Exchange
// ---------------------------------------------------------------------------

// TestStep_MultiTurnToolExchange drives the core convergence: Turn 0 the model asks for a
// tool (StatusTurnComplete, the tool runs), Turn 1 the model finishes (StatusExchangeComplete).
func TestStep_MultiTurnToolExchange(t *testing.T) {
	sink := &recordingSink{}
	ran := 0
	cfg := configWithTools(sink, fakeTool{name: "lookup", readOnly: true, ran: &ran, result: "the answer is 42"})
	responder := &scriptedResponder{scripts: [][]provider.Delta{
		toolCallScript("c1", "lookup", `{"q":"meaning"}`),
		contentScript("all done"),
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "look it up"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	res0, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step 0: %v", err)
	}
	if res0.Status != domain.StatusTurnComplete {
		t.Errorf("Turn 0 status = %q, want %q", res0.Status, domain.StatusTurnComplete)
	}
	if ran != 1 {
		t.Errorf("tool ran %d times after Turn 0, want 1", ran)
	}
	if !hasEvent[domain.ToolCallEvent](sink.events) {
		t.Error("no ToolCallEvent emitted")
	}
	if !hasEvent[domain.ToolResultEvent](sink.events) {
		t.Error("no ToolResultEvent emitted")
	}

	res1, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step 1: %v", err)
	}
	if res1.Status != domain.StatusExchangeComplete {
		t.Errorf("Turn 1 status = %q, want %q", res1.Status, domain.StatusExchangeComplete)
	}
	if me, ok := lastMessageEvent(sink.events); !ok || me.Text != "all done" {
		t.Errorf("final MessageEvent = %+v (ok=%v), want Text=%q", me, ok, "all done")
	}

	// user → assistant(tool call) → tool result → assistant(final) = 4 messages.
	if got := a.conv.Len(); got != 4 {
		t.Errorf("conversation has %d messages, want 4", got)
	}
}

// TestRun_DrivesExchangeToCompletion proves Run steps through the tool Turn and the final
// Turn in one call, returning StatusExchangeComplete.
func TestRun_DrivesExchangeToCompletion(t *testing.T) {
	sink := &recordingSink{}
	ran := 0
	cfg := configWithTools(sink, fakeTool{name: "lookup", readOnly: true, ran: &ran, result: "ok"})
	responder := &scriptedResponder{scripts: [][]provider.Delta{
		toolCallScript("c1", "lookup", "{}"),
		contentScript("finished"),
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "go"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("Run status = %q, want %q", res.Status, domain.StatusExchangeComplete)
	}
	if ran != 1 {
		t.Errorf("tool ran %d times, want 1", ran)
	}
	if me, ok := lastMessageEvent(sink.events); !ok || me.Text != "finished" {
		t.Errorf("final MessageEvent = %+v (ok=%v), want Text=%q", me, ok, "finished")
	}
}

// ---------------------------------------------------------------------------
// Approval
// ---------------------------------------------------------------------------

// TestDispatch_ApprovalAskBefore consults the Approver for a write tool in Ask-Before mode,
// runs it on Allow and refuses it on Deny.
func TestDispatch_ApprovalAskBefore(t *testing.T) {
	tests := []struct {
		name     string
		decision domain.ApprovalDecision
		wantRan  int
		wantErr  bool // the tool result should be an error result
	}{
		{"allow", domain.ApprovalAllow, 1, false},
		{"deny", domain.ApprovalDeny, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			ran := 0
			cfg := configWithTools(sink, fakeTool{name: "write_it", readOnly: false, ran: &ran, result: "wrote"})
			cfg.Mode = domain.ModeAskBefore
			approver := &fakeApprover{decision: tc.decision}
			cfg.Approver = approver
			responder := &scriptedResponder{scripts: [][]provider.Delta{
				toolCallScript("c1", "write_it", "{}"),
				contentScript("done"),
			}}

			a, err := newAgent(cfg, responder)
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			if err := a.Submit(domain.UserInput{Text: "edit the file"}); err != nil {
				t.Fatalf("Submit: %v", err)
			}
			if _, err := a.Run(context.Background()); err != nil {
				t.Fatalf("Run: %v", err)
			}

			if approver.calls != 1 {
				t.Errorf("approver consulted %d times, want 1", approver.calls)
			}
			if ran != tc.wantRan {
				t.Errorf("tool ran %d times, want %d", ran, tc.wantRan)
			}
			if !hasEvent[domain.ApprovalEvent](sink.events) {
				t.Error("no ApprovalEvent emitted")
			}
			if got := toolResultIsError(sink.events); got != tc.wantErr {
				t.Errorf("tool-result IsError = %v, want %v", got, tc.wantErr)
			}
		})
	}
}

// TestDispatch_ApprovalAllowForSession remembers an allow-for-session verdict, so a second
// call to the same tool runs without re-consulting the Approver.
func TestDispatch_ApprovalAllowForSession(t *testing.T) {
	sink := &recordingSink{}
	ran := 0
	cfg := configWithTools(sink, fakeTool{name: "write_it", readOnly: false, ran: &ran, result: "wrote"})
	cfg.Mode = domain.ModeAskBefore
	approver := &fakeApprover{decision: domain.ApprovalAllowForSession}
	cfg.Approver = approver
	responder := &scriptedResponder{scripts: [][]provider.Delta{
		toolCallScript("c1", "write_it", "{}"),
		toolCallScript("c2", "write_it", "{}"),
		contentScript("done"),
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "edit twice"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if approver.calls != 1 {
		t.Errorf("approver consulted %d times, want 1 (allow-for-session caches)", approver.calls)
	}
	if ran != 2 {
		t.Errorf("tool ran %d times, want 2", ran)
	}
}

// TestDispatch_PlanBypassesApproval runs a read-only tool in Plan mode without consulting
// the Approver, and filters write tools out of the menu the model is shown.
func TestDispatch_PlanBypassesApproval(t *testing.T) {
	sink := &recordingSink{}
	ran := 0
	cfg := configWithTools(sink,
		fakeTool{name: "read_it", readOnly: true, ran: &ran, result: "contents"},
		fakeTool{name: "write_it", readOnly: false},
	)
	cfg.Mode = domain.ModePlan
	approver := &fakeApprover{decision: domain.ApprovalAllow}
	cfg.Approver = approver
	responder := &scriptedResponder{scripts: [][]provider.Delta{
		toolCallScript("c1", "read_it", "{}"),
		contentScript("done"),
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "investigate"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if approver.calls != 0 {
		t.Errorf("approver consulted %d times in Plan mode, want 0", approver.calls)
	}
	if ran != 1 {
		t.Errorf("read-only tool ran %d times, want 1", ran)
	}
	if hasEvent[domain.ApprovalEvent](sink.events) {
		t.Error("ApprovalEvent emitted in Plan mode")
	}

	// The Plan menu shows only the read-only tool.
	menu := a.toolMenu()
	if len(menu) != 1 || menu[0].Name != "read_it" {
		t.Errorf("Plan menu = %+v, want only read_it", menu)
	}
}

// ---------------------------------------------------------------------------
// Cancellation mid-tool & tool-panic survival
// ---------------------------------------------------------------------------

// blockingTool blocks until ctx is cancelled — the cancel-mid-tool driver. started is
// closed once Execute is in flight so the test cancels deterministically.
type blockingTool struct {
	name    string
	started chan struct{}
}

func (t blockingTool) Name() string            { return t.name }
func (t blockingTool) Description() string     { return "blocks until cancelled" }
func (t blockingTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t blockingTool) ReadOnly() bool          { return true }

func (t blockingTool) Execute(ctx context.Context, _ domain.ToolCall) (domain.ToolResult, error) {
	close(t.started)
	<-ctx.Done()
	return domain.ToolResult{}, ctx.Err()
}

// TestStep_CancelMidTool cancels while a tool executes and proves the Step returns
// StatusCancelled with the Turn rolled back to a serializable boundary that resumes.
func TestStep_CancelMidTool(t *testing.T) {
	sink := &recordingSink{}
	started := make(chan struct{})
	cfg := configWithTools(sink, blockingTool{name: "block", started: started})
	responder := &scriptedResponder{scripts: [][]provider.Delta{
		toolCallScript("c1", "block", "{}"),
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "run the slow tool"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()
	res, err := a.Step(ctx)
	if err != nil {
		t.Fatalf("Step returned a loop error on cancel: %v", err)
	}
	if res.Status != domain.StatusCancelled {
		t.Fatalf("Step status = %q, want %q", res.Status, domain.StatusCancelled)
	}

	// The Turn rolled back: only the user message remains (no assistant tool-call message,
	// no partial tool result) — a clean, serializable boundary.
	if got := a.conv.Len(); got != 1 {
		t.Errorf("after cancel the conversation has %d messages, want 1 (the user input)", got)
	}

	// The snapshot resumes and completes against a working responder.
	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot after cancel: %v", err)
	}
	sink2 := &recordingSink{}
	cfg2 := configWithTools(sink2, fakeTool{name: "block", readOnly: true, result: "ok"})
	b, err := resumeAgent(cfg2, snap, &scriptedResponder{scripts: [][]provider.Delta{contentScript("recovered")}})
	if err != nil {
		t.Fatalf("resumeAgent: %v", err)
	}

	// The cancelled Exchange is still open (inExchange survived the cancel/resume), so a
	// Submit is rejected — interleaving a fresh user message into the open Exchange would
	// produce a malformed conversation. The host continues by re-Stepping, not re-Submitting.
	if err := b.Submit(domain.UserInput{Text: "intrude"}); err == nil {
		t.Error("Submit after a mid-Exchange cancel was accepted; the open Exchange must reject it")
	}

	// Re-Stepping re-attempts the Turn from the rolled-back boundary and completes it.
	res2, err := b.Run(context.Background())
	if err != nil {
		t.Fatalf("Run (resumed): %v", err)
	}
	if res2.Status != domain.StatusExchangeComplete {
		t.Errorf("resumed status = %q, want %q", res2.Status, domain.StatusExchangeComplete)
	}
}

// TestAbortExchange_AfterCancelUnwedges proves the interactive recovery path: after a cancel
// leaves the Exchange open (so Submit/ClearContext are refused), AbortExchange rolls the
// conversation back to the pre-Exchange boundary and clears the open-Exchange flag, so the
// next ClearContext and Submit are accepted again — the fix for the post-Esc TUI wedge where
// a cancelled session could neither clear nor send.
func TestAbortExchange_AfterCancelUnwedges(t *testing.T) {
	sink := &recordingSink{}
	started := make(chan struct{})
	cfg := configWithTools(sink, blockingTool{name: "block", started: started})
	responder := &scriptedResponder{scripts: [][]provider.Delta{
		toolCallScript("c1", "block", "{}"),
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "run the slow tool"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()
	res, err := a.Step(ctx)
	if err != nil {
		t.Fatalf("Step returned a loop error on cancel: %v", err)
	}
	if res.Status != domain.StatusCancelled {
		t.Fatalf("Step status = %q, want %q", res.Status, domain.StatusCancelled)
	}

	// The Exchange is still open: a Submit and a ClearContext are both refused (the wedge).
	if err := a.Submit(domain.UserInput{Text: "new message"}); err == nil {
		t.Fatal("Submit before AbortExchange was accepted; the open Exchange must reject it")
	}
	if err := a.ClearContext(); err == nil {
		t.Fatal("ClearContext before AbortExchange was accepted; the open Exchange must reject it")
	}

	// Discard the cancelled Exchange. The un-answered user message is rolled back to the
	// pre-Exchange boundary, leaving a clean, empty conversation.
	a.AbortExchange()
	if got := a.conv.Len(); got != 0 {
		t.Fatalf("after AbortExchange the conversation has %d messages, want 0", got)
	}

	// ClearContext is accepted again (no longer ErrInputPending).
	if err := a.ClearContext(); err != nil {
		t.Fatalf("ClearContext after AbortExchange: %v", err)
	}

	// A fresh message runs to completion against a working responder — a clean user→assistant
	// Exchange with no interleaved/orphaned message from the scrapped one.
	a.upstream = &scriptedResponder{scripts: [][]provider.Delta{contentScript("hello")}}
	if err := a.Submit(domain.UserInput{Text: "start over"}); err != nil {
		t.Fatalf("Submit after AbortExchange: %v", err)
	}
	done, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run after AbortExchange: %v", err)
	}
	if done.Status != domain.StatusExchangeComplete {
		t.Errorf("status = %q, want %q", done.Status, domain.StatusExchangeComplete)
	}
	if got := a.conv.Len(); got != 2 {
		t.Errorf("conversation has %d messages, want 2 (user + assistant)", got)
	}
}

// TestAbortExchange_NoExchangeIsNoop proves AbortExchange leaves a quiescent Agent with no
// open Exchange untouched — it never drops committed history out from under the next Submit.
func TestAbortExchange_NoExchangeIsNoop(t *testing.T) {
	sink := &recordingSink{}
	cfg := configWithTools(sink, fakeTool{name: "noop", readOnly: true, result: "ok"})
	a, err := newAgent(cfg, &scriptedResponder{scripts: [][]provider.Delta{contentScript("hi")}})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "hello"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	before := a.conv.Len()

	a.AbortExchange() // no Exchange open — must be a no-op
	if got := a.conv.Len(); got != before {
		t.Errorf("AbortExchange dropped %d messages with no open Exchange (had %d, now %d)", before-got, before, got)
	}
}

// panickingTool panics in Execute — the input for the recover-at-extension-boundary guarantee.
type panickingTool struct{ name string }

func (t panickingTool) Name() string            { return t.name }
func (t panickingTool) Description() string     { return "panics" }
func (t panickingTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t panickingTool) ReadOnly() bool          { return true }

func (t panickingTool) Execute(context.Context, domain.ToolCall) (domain.ToolResult, error) {
	panic("tool boom")
}

// TestDispatch_ToolPanicSurvives proves a panicking tool becomes an ErrorEvent + an error
// tool-result, and the loop continues to a clean final response (the host is never unwound).
func TestDispatch_ToolPanicSurvives(t *testing.T) {
	sink := &recordingSink{}
	cfg := configWithTools(sink, panickingTool{name: "boom"})
	responder := &scriptedResponder{scripts: [][]provider.Delta{
		toolCallScript("c1", "boom", "{}"),
		contentScript("recovered and finished"),
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "call the bad tool"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned a loop error on tool panic: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("status = %q, want %q", res.Status, domain.StatusExchangeComplete)
	}
	if !hasEvent[domain.ErrorEvent](sink.events) {
		t.Error("no ErrorEvent emitted for the panicking tool")
	}
	if !toolResultIsError(sink.events) {
		t.Error("the panicking tool did not yield an error tool-result")
	}
	if me, ok := lastMessageEvent(sink.events); !ok || me.Text != "recovered and finished" {
		t.Errorf("final MessageEvent = %+v (ok=%v), want the loop to have survived", me, ok)
	}
}

// TestStep_FileRefsAreSurfacedNotSilentlyDropped proves that UserInput.FileRefs the loop does
// not yet resolve are reported via a loop ErrorEvent (so a host is not misled into thinking
// they took effect), while the Text is still consumed and the Exchange completes normally.
func TestStep_FileRefsAreSurfacedNotSilentlyDropped(t *testing.T) {
	sink := &recordingSink{}
	capt := &capturingResponder{reply: "done"}
	a, err := newAgent(baseConfig(sink), capt)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "use these", FileRefs: []string{"a.go", "b.go"}}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !hasEvent[domain.ErrorEvent](sink.events) {
		t.Error("FileRefs were dropped without surfacing a loop ErrorEvent")
	}
	if !containsContent(capt.got.Messages, "use these") {
		t.Errorf("the Text was not consumed despite the unresolved FileRefs: %+v", capt.got.Messages)
	}
}

// ---------------------------------------------------------------------------
// Post-response hooks: intercept + ActionDefer feed-forward
// ---------------------------------------------------------------------------

// interceptHook rewrites the assistant text in place (ActionIntercept).
type interceptHook struct{ replacement string }

func (h interceptHook) PostResponse(_ context.Context, resp *domain.Response) (domain.PostResponseDecision, error) {
	resp.SetText(h.replacement)
	return domain.PostResponseDecision{Action: domain.ActionIntercept}, nil
}

// TestStep_PostResponseIntercept proves an ActionIntercept hook's SetText reaches the
// emitted MessageEvent and the committed conversation.
func TestStep_PostResponseIntercept(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Mechanisms = domain.NewMechanismRegistry()
	if err := cfg.Mechanisms.AddExperimental(domain.HookPostResponse, interceptHook{replacement: "intercepted"}); err != nil {
		t.Fatalf("AddExperimental: %v", err)
	}

	a, err := newAgent(cfg, echoResponder{reply: "original"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if me, ok := lastMessageEvent(sink.events); !ok || me.Text != "intercepted" {
		t.Errorf("MessageEvent = %+v (ok=%v), want the intercepted text", me, ok)
	}
}

// retryOnceHook asks the loop to re-call the Upstream exactly once (ActionRetry), then lets
// the response stand — the post-response retry path.
type retryOnceHook struct{ done *bool }

func (h retryOnceHook) PostResponse(_ context.Context, _ *domain.Response) (domain.PostResponseDecision, error) {
	if *h.done {
		return domain.PostResponseDecision{Action: domain.ActionIntercept}, nil
	}
	*h.done = true
	return domain.PostResponseDecision{Action: domain.ActionRetry}, nil
}

// TestStep_RetryEmitsStreamReset proves an ActionRetry re-streams the Turn and emits a
// StreamResetEvent first, so a streaming observer discards the superseded tokens; the
// committed final message is the retried response, not the draft.
func TestStep_RetryEmitsStreamReset(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	done := false
	cfg.Mechanisms = domain.NewMechanismRegistry()
	if err := cfg.Mechanisms.AddExperimental(domain.HookPostResponse, retryOnceHook{done: &done}); err != nil {
		t.Fatalf("AddExperimental: %v", err)
	}
	responder := &scriptedResponder{scripts: [][]provider.Delta{
		contentScript("draft"),
		contentScript("final"),
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "go"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !hasEvent[domain.StreamResetEvent](sink.events) {
		t.Error("no StreamResetEvent emitted before the retry re-streamed")
	}
	if me, ok := lastMessageEvent(sink.events); !ok || me.Text != "final" {
		t.Errorf("final MessageEvent = %+v (ok=%v), want the retried text %q", me, ok, "final")
	}
}

// deferOnceHook defers a correction on the first response only, then no-ops — the loop half
// of the ActionDefer feed-forward path.
type deferOnceHook struct {
	done   *bool
	inject string
}

func (h deferOnceHook) PostResponse(_ context.Context, _ *domain.Response) (domain.PostResponseDecision, error) {
	if *h.done {
		return domain.PostResponseDecision{Action: domain.ActionIntercept}, nil
	}
	*h.done = true
	return domain.PostResponseDecision{Action: domain.ActionDefer, Inject: h.inject}, nil
}

// TestStep_DeferredCorrectionSurvivesSnapshot proves a post-response ActionDefer survives a
// snapshot/resume boundary and is injected, role-safe, into the next Exchange's request —
// the loop-level delivery of the P1.5 acceptance clause proven there only at the primitive level.
func TestStep_DeferredCorrectionSurvivesSnapshot(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	done := false
	cfg.Mechanisms = domain.NewMechanismRegistry()
	if err := cfg.Mechanisms.AddExperimental(domain.HookPostResponse, deferOnceHook{done: &done, inject: "remember the constraint"}); err != nil {
		t.Fatalf("AddExperimental: %v", err)
	}

	a, err := newAgent(cfg, echoResponder{reply: "first answer"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "task one"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run (exchange 1): %v", err)
	}

	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// Resume into a fresh Agent with a capturing responder; the next request must carry the
	// deferred correction, injected before the new user input.
	sink2 := &recordingSink{}
	cfg2 := baseConfig(sink2)
	cfg2.Mechanisms = cfg.Mechanisms
	capt := &capturingResponder{reply: "second answer"}
	b, err := resumeAgent(cfg2, snap, capt)
	if err != nil {
		t.Fatalf("resumeAgent: %v", err)
	}
	if err := b.Submit(domain.UserInput{Text: "task two"}); err != nil {
		t.Fatalf("Submit (exchange 2): %v", err)
	}
	if _, err := b.Run(context.Background()); err != nil {
		t.Fatalf("Run (exchange 2): %v", err)
	}

	if !containsContent(capt.got.Messages, "remember the constraint") {
		t.Errorf("the resumed request did not carry the deferred correction: %+v", capt.got.Messages)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func toolResultIsError(events []domain.Event) bool {
	for _, e := range events {
		if tr, ok := e.(domain.ToolResultEvent); ok && tr.Result.IsError {
			return true
		}
	}
	return false
}

func containsContent(msgs []provider.Message, want string) bool {
	for _, m := range msgs {
		if strings.Contains(m.Content, want) {
			return true
		}
	}
	return false
}
