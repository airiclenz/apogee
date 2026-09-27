package agent

// P1.2 acceptance (the convergence): a fake Responder + a fake Tool drive a multi-Turn
// Exchange under the full Turn/Step state machine — stream → parse → post-response hooks →
// tool dispatch through Approval → post-tool-result → quiescent boundary. These tests
// assert: a multi-Turn tool Exchange completes; Approval is consulted in Ask-Before and
// bypassed in Plan; cancellation mid-tool yields StatusCancelled + a resumable snapshot; a
// panicking tool yields an ErrorEvent and the loop survives; and the Outcome{Defer}
// feed-forward is Exchange-scoped — expired at the Exchange boundary, never crossing into the
// next Exchange (item 7 / F6).

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/stubllm"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// toolCallScript is a Delta stream that emits one native tool call then a tool_calls finish —
// the script shape the surviving hand-written fakes (requestLogResponder, routedResponder, …)
// still play; toolCallTurn is its stubllm twin for a scripted upstream.
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

// contentScript is a Delta stream that emits one content chunk then a stop finish; contentTurn
// is its stubllm twin.
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
	responder := scriptedResponder(t,
		toolCallTurn("c1", "lookup", `{"q":"meaning"}`),
		contentTurn("all done"),
	)

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
	responder := scriptedResponder(t,
		toolCallTurn("c1", "lookup", "{}"),
		contentTurn("finished"),
	)

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
			responder := scriptedResponder(t,
				toolCallTurn("c1", "write_it", "{}"),
				contentTurn("done"),
			)

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
	// The allow-for-session cache keys on the call as the model wrote it, so the second call must
	// be the SAME call — which is also what the tool-loop breaker Floor guard answers. The guard is
	// off for this test: it is about the approval cache, and the guard has its own repeat proof in
	// floorguards_test.go.
	cfg.Floor.DisableToolLoopBreaker = true
	responder := scriptedResponder(t,
		toolCallTurn("c1", "write_it", "{}"),
		toolCallTurn("c2", "write_it", "{}"),
		contentTurn("done"),
	)

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

// TestDispatch_ForcedApprovalNeverCachesAllowForSession is the WRITE-direction mirror of the
// cache test above: a Tier-2 forced gate answered "allow for session" must not write the
// allow-for-session cache, so a later ORDINARY gate on the same tool still prompts. The human
// pressing "allow for session" on a `sudo …` speed-bump authorises that one dangerous call —
// not the removal of Ask-Before's only approval boundary for the rest of the Session
// (confinement-execution-contract: "a forced gate … is never pre-allowable").
func TestDispatch_ForcedApprovalNeverCachesAllowForSession(t *testing.T) {
	sink := &recordingSink{}
	ran := 0
	cfg := configWithTools(sink, fakeTool{name: "terminal", readOnly: false, ran: &ran, result: "ok"})
	cfg.Mode = domain.ModeAskBefore
	approver := &seamApprover{decision: domain.ApprovalAllowForSession}
	cfg.Approver = approver
	responder := scriptedResponder(t,
		toolCallTurn("c1", "terminal", `{"command":"sudo apt-get install jq"}`), // Tier-2 → forced gate
		toolCallTurn("c2", "terminal", `{"command":"ls"}`),                      // ordinary gate
		contentTurn("done"),
	)

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "install then list"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	keys := approver.keysSeen()
	if len(keys) != 2 {
		t.Fatalf("approver consulted %d times, want 2 (a forced allow-for-session must not pre-allow the next ordinary gate)", len(keys))
	}
	// The mechanism behind that count: dispatch hands a forced gate an EMPTY CacheKey, the seam's
	// "this answer can never be remembered" signal, and the ordinary gate its real key — the tool
	// name plus the digest of its arguments (gateCacheKey).
	if keys[0] != "" {
		t.Errorf("the forced gate travelled with cache key %q, want no key at all", keys[0])
	}
	if !strings.HasPrefix(keys[1], "terminal") {
		t.Errorf("the ordinary gate's cache key = %q, want a key naming the tool", keys[1])
	}
	if ran != 2 {
		t.Errorf("tool ran %d times, want 2 (both calls were allowed)", ran)
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
	responder := scriptedResponder(t,
		toolCallTurn("c1", "read_it", "{}"),
		contentTurn("done"),
	)

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

// TestStep_CancelMidTool cancels while the second of three tool calls executes and proves the Turn
// is SETTLED, not rolled back (ADR 0088 D1): the reply stays and every call it issued has a result —
// the finished call its real one, the running call the while-it-ran text, the call never reached the
// not-run text — each reaching the Drivers as a ToolResultEvent; the kept Turn advances the counter,
// and its snapshot resumes into the next Turn, which reads the results.
func TestStep_CancelMidTool(t *testing.T) {
	sink := &recordingSink{}
	started := make(chan struct{})
	cfg := configWithTools(sink,
		fakeTool{name: "lookup", readOnly: true, result: "42"},
		blockingTool{name: "block", started: started},
	)
	responder := scriptedResponder(t, stubllm.Turn{ToolCalls: []stubllm.ToolCall{
		{ID: "c1", Name: "lookup", Arguments: `{"n":1}`},
		{ID: "c2", Name: "block", Arguments: "{}"},
		{ID: "c3", Name: "lookup", Arguments: `{"n":3}`},
	}})
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
	if got := a.conv.Len(); got != 5 {
		t.Fatalf("after cancel the conversation has %d messages, want 5 (user, reply, three results)", got)
	}
	if reply := a.conv.At(1); reply.Role != domain.RoleAssistant || len(reply.ToolCalls) != 3 {
		t.Errorf("message 1 = %+v, want the assistant reply with its three calls", reply)
	}
	want := []struct {
		callID, content string
		outcome         domain.ToolOutcome
	}{
		{"c1", "42", domain.ToolOutcomeOf(false)},
		{"c2", "cancelled by the user while it ran", domain.ToolOutcomeOf(true)},
		{"c3", "not run: cancelled by the user", domain.ToolOutcomeOf(true)},
	}
	for i, w := range want {
		m := a.conv.At(2 + i)
		if m.Role != domain.RoleTool || m.ToolCallID != w.callID || m.Content != w.content || m.ToolOutcome != w.outcome {
			t.Errorf("result %d = {role %q, call %q, content %q, outcome %v}, want {tool, %q, %q, %v}",
				i, m.Role, m.ToolCallID, m.Content, m.ToolOutcome, w.callID, w.content, w.outcome)
		}
	}
	var resulted []string
	for _, e := range sink.events {
		if re, ok := e.(domain.ToolResultEvent); ok {
			resulted = append(resulted, re.Result.CallID)
		}
	}
	if strings.Join(resulted, ",") != "c1,c2,c3" {
		t.Errorf("ToolResultEvents for %v, want one per call in order [c1 c2 c3]", resulted)
	}
	if a.turns.index != 1 {
		t.Errorf("Turn index = %d, want 1 (a kept Turn advances past itself)", a.turns.index)
	}

	// The snapshot resumes; the cancelled Exchange is still open, so a Submit is rejected and the
	// host carries on by re-Stepping into the next Turn, which reads the settled results.
	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot after cancel: %v", err)
	}
	cfg2 := configWithTools(&recordingSink{}, fakeTool{name: "lookup", readOnly: true, result: "42"}, fakeTool{name: "block", readOnly: true, result: "ok"})
	upstream := echoResponder(t, "recovered")
	b, err := resumeAgent(cfg2, snap, upstream)
	if err != nil {
		t.Fatalf("resumeAgent: %v", err)
	}
	if err := b.Submit(domain.UserInput{Text: "intrude"}); err == nil {
		t.Error("Submit after a mid-Exchange cancel was accepted; the open Exchange must reject it")
	}
	res2, err := b.Run(context.Background())
	if err != nil {
		t.Fatalf("Run (resumed): %v", err)
	}
	if res2.Status != domain.StatusExchangeComplete {
		t.Errorf("resumed status = %q, want %q", res2.Status, domain.StatusExchangeComplete)
	}
	var carried int
	for _, m := range upstream.last().Messages {
		if m.Role == "tool" {
			carried++
		}
	}
	if carried != 3 {
		t.Errorf("the resumed request carried %d tool results, want the 3 the cancel settled", carried)
	}
}

// TestStep_CancelWhileStreamingDropsTheTurn pins the other half of the settle rule: a Turn
// cancelled before its reply finished streaming issued nothing that ran, so it is rolled back
// whole — no assistant message, the counter held for the re-attempt (ADR 0088 D1).
func TestStep_CancelWhileStreamingDropsTheTurn(t *testing.T) {
	responder := &blockAtResponder{blockAt: 0, started: make(chan struct{})}
	a, err := newAgent(configWithTools(&recordingSink{}), responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "think hard"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-responder.started
		cancel()
	}()

	res, err := a.Step(ctx)

	if err != nil || res.Status != domain.StatusCancelled {
		t.Fatalf("Step = %+v, %v; want StatusCancelled", res, err)
	}
	if got := a.conv.Len(); got != 1 {
		t.Errorf("after the cancel the conversation has %d messages, want 1 (the user input alone)", got)
	}
	if a.turns.index != 0 {
		t.Errorf("Turn index = %d, want 0 (a dropped Turn is re-attempted, not advanced past)", a.turns.index)
	}
	if !a.InExchange() {
		t.Error("the cancel closed the Exchange; it stays open for the host's close or re-attempt")
	}
}

// ctxApprover parks inside Approve until the Turn is cancelled, then answers with the cancel — a
// human who never answered the prompt before pressing stop.
type ctxApprover struct{ entered chan struct{} }

func (c ctxApprover) Approve(ctx context.Context, _ domain.ApprovalRequest) (domain.ApprovalDecision, error) {
	close(c.entered)
	<-ctx.Done()
	return domain.ApprovalDeny, ctx.Err()
}

// TestStep_CancelAtTheApprovalGateSettlesNotRun proves a call the cancel reached at its Approval
// gate never ran and says so: its result is the not-run text, never the while-it-ran one, and the
// Turn is settled with the tool untouched.
func TestStep_CancelAtTheApprovalGateSettlesNotRun(t *testing.T) {
	ran := 0
	cfg := configWithTools(&recordingSink{}, fakeTool{name: "write_it", readOnly: false, ran: &ran, result: "wrote"})
	cfg.Mode = domain.ModeAskBefore
	approver := ctxApprover{entered: make(chan struct{})}
	cfg.Approver = approver
	a, err := newAgent(cfg, scriptedResponder(t, toolCallTurn("c1", "write_it", "{}")))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "edit it"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-approver.entered
		cancel()
	}()

	res, err := a.Step(ctx)

	if err != nil || res.Status != domain.StatusCancelled {
		t.Fatalf("Step = %+v, %v; want StatusCancelled", res, err)
	}
	if ran != 0 {
		t.Errorf("the gated tool ran %d times, want 0", ran)
	}
	if got := a.conv.Len(); got != 3 {
		t.Fatalf("conversation has %d messages, want 3 (user, reply, result)", got)
	}
	if last := a.conv.At(2); last.ToolCallID != "c1" || last.Content != "not run: cancelled by the user" {
		t.Errorf("result = {call %q, content %q}, want {c1, %q}", last.ToolCallID, last.Content, "not run: cancelled by the user")
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
	responder := scriptedResponder(t,
		toolCallTurn("c1", "block", "{}"),
	)

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
	a.upstream = scriptedResponder(t, contentTurn("hello"))
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
	a, err := newAgent(cfg, scriptedResponder(t, contentTurn("hi")))
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

// settledExchangeMarker is the fence a settled Exchange's cut opens with on its last tool result.
const settledExchangeMarker = domain.EngineNoteFencePrefix + "cancelled]"

// cancelledMidTurnAgent runs Turn 0 (a completed tool Turn) and cancels Turn 1 while its reply
// streams — the shape SettleExchange exists for: a finished Turn in history, the in-flight one
// rolled back, the Exchange still open.
func cancelledMidTurnAgent(t *testing.T) *Agent {
	t.Helper()
	responder := &blockAtResponder{
		scripts: [][]provider.Delta{toolCallScript("c1", "lookup", "{}")},
		blockAt: 1,
		started: make(chan struct{}),
	}
	cfg := configWithTools(&recordingSink{}, fakeTool{name: "lookup", readOnly: true, result: "42"})
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "look it up"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if res, err := a.Step(context.Background()); err != nil || res.Status != domain.StatusTurnComplete {
		t.Fatalf("Step 0 = %+v, %v; want StatusTurnComplete", res, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-responder.started
		cancel()
	}()
	res, err := a.Step(ctx)
	if err != nil {
		t.Fatalf("Step 1 returned a loop error on cancel: %v", err)
	}
	if res.Status != domain.StatusCancelled {
		t.Fatalf("Step 1 status = %q, want %q", res.Status, domain.StatusCancelled)
	}
	return a
}

// TestSettleExchange_KeepsFinishedTurnsAndNotesTheCut is the defect apogee-2un Stage A closes:
// a stop after a finished Turn keeps that Turn — the opening user message, the tool call and its
// result stay — and marks the cut as an `[engine — cancelled]` note on the last tool result, so
// the next request the model reads says the results stand and the reply was not given; the
// Exchange closes and the next Submit is accepted.
func TestSettleExchange_KeepsFinishedTurnsAndNotesTheCut(t *testing.T) {
	a := cancelledMidTurnAgent(t)

	dropped := a.SettleExchange()

	if dropped {
		t.Fatal("SettleExchange reported the Exchange dropped; a finished Turn must be kept")
	}
	if got := a.conv.Len(); got != 3 {
		t.Fatalf("after settle the conversation has %d messages, want 3 (user, tool call, tool result)", got)
	}
	last := a.conv.At(2)
	if last.Role != domain.RoleTool {
		t.Fatalf("last message role = %q, want %q", last.Role, domain.RoleTool)
	}
	if !strings.Contains(last.Content, settledExchangeMarker) || !strings.Contains(last.Content, cancelledNoteLine) {
		t.Errorf("last tool result carries no cancelled note; content = %q", last.Content)
	}
	if !strings.HasPrefix(last.Content, "42") {
		t.Errorf("the tool's own output must precede the note; content = %q", last.Content)
	}
	if a.InExchange() {
		t.Error("SettleExchange left the Exchange open")
	}

	// The next Exchange reads the kept Turn and the cut on the wire.
	upstream := echoResponder(t, "carrying on")
	a.upstream = upstream
	if err := a.Submit(domain.UserInput{Text: "next"}); err != nil {
		t.Fatalf("Submit after settle: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run after settle: %v", err)
	}
	var noted bool
	for _, m := range upstream.last().Messages {
		if m.Role == "tool" && strings.Contains(m.Content, settledExchangeMarker) {
			noted = true
		}
	}
	if !noted {
		t.Errorf("the next request carried no noted tool result; messages = %+v", upstream.last().Messages)
	}

	// A Turn the cancel reached on a tool call is kept as well (ADR 0088 D1): its finished leaf
	// keeps its real result, unmarked, and the cut rides the Turn's last result — the cancelled
	// call's own.
	t.Run("a Turn cancelled on a tool call keeps its finished leaf", func(t *testing.T) {
		started := make(chan struct{})
		cfg := configWithTools(&recordingSink{},
			fakeTool{name: "lookup", readOnly: true, result: "42"},
			blockingTool{name: "block", started: started},
		)
		responder := scriptedResponder(t,
			toolCallTurn("c1", "lookup", `{"n":1}`),
			twoToolCallScript(toolReq{"c2", "lookup", `{"n":2}`}, toolReq{"c3", "block", "{}"}),
		)
		b, err := newAgent(cfg, responder)
		if err != nil {
			t.Fatalf("newAgent: %v", err)
		}
		if err := b.Submit(domain.UserInput{Text: "look it up"}); err != nil {
			t.Fatalf("Submit: %v", err)
		}
		if res, err := b.Step(context.Background()); err != nil || res.Status != domain.StatusTurnComplete {
			t.Fatalf("Step 0 = %+v, %v; want StatusTurnComplete", res, err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			<-started
			cancel()
		}()
		if res, err := b.Step(ctx); err != nil || res.Status != domain.StatusCancelled {
			t.Fatalf("Step 1 = %+v, %v; want StatusCancelled", res, err)
		}

		dropped := b.SettleExchange()

		if dropped {
			t.Fatal("SettleExchange reported the Exchange dropped; the finished Turns must be kept")
		}
		if got := b.conv.Len(); got != 6 {
			t.Fatalf("after settle the conversation has %d messages, want 6 (user, two replies, three results)", got)
		}
		if leaf := b.conv.At(4); leaf.ToolCallID != "c2" || leaf.Content != "42" {
			t.Errorf("the settled Turn's finished leaf = {call %q, content %q}, want {c2, 42} unmarked", leaf.ToolCallID, leaf.Content)
		}
		last := b.conv.At(5)
		if last.ToolCallID != "c3" || !strings.HasPrefix(last.Content, cancelledWhileRunningContent) || !strings.Contains(last.Content, settledExchangeMarker) {
			t.Errorf("last result = {call %q, content %q}, want c3's while-it-ran text carrying the cancelled note", last.ToolCallID, last.Content)
		}
	})
}

// TestSettleExchange_LoneUserMessageFallsBackToAbort pins the fall-through: a Turn 0 cancelled
// while its reply streamed is dropped (ADR 0088 D1), so no tool result is left to carry the cut and
// the settle scraps the lone opening user message exactly as AbortExchange does — and says so.
func TestSettleExchange_LoneUserMessageFallsBackToAbort(t *testing.T) {
	responder := &blockAtResponder{blockAt: 0, started: make(chan struct{})}
	a, err := newAgent(configWithTools(&recordingSink{}), responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "think hard"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-responder.started
		cancel()
	}()
	if res, err := a.Step(ctx); err != nil || res.Status != domain.StatusCancelled {
		t.Fatalf("Step = %+v, %v; want StatusCancelled", res, err)
	}

	dropped := a.SettleExchange()

	if !dropped {
		t.Error("SettleExchange reported the Exchange kept; a lone user message must fall back to abort")
	}
	if got := a.conv.Len(); got != 0 {
		t.Errorf("after settle the conversation has %d messages, want 0", got)
	}
	if a.InExchange() {
		t.Error("SettleExchange left the Exchange open")
	}
	if err := a.Submit(domain.UserInput{Text: "start over"}); err != nil {
		t.Errorf("Submit after settle: %v, want accepted", err)
	}
}

// TestSettleExchange_TurnZeroCancelledMidToolKeepsThePrompt proves a stop during Turn 0's tool call
// no longer scraps the prompt: the settled Turn is a kept Turn, so the opening user message, the
// reply and the cancelled call's result stay, the cut rides that result, and the Exchange closes.
func TestSettleExchange_TurnZeroCancelledMidToolKeepsThePrompt(t *testing.T) {
	started := make(chan struct{})
	cfg := configWithTools(&recordingSink{}, blockingTool{name: "block", started: started})
	a, err := newAgent(cfg, scriptedResponder(t, toolCallTurn("c1", "block", "{}")))
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
	if res, err := a.Step(ctx); err != nil || res.Status != domain.StatusCancelled {
		t.Fatalf("Step = %+v, %v; want StatusCancelled", res, err)
	}

	dropped := a.SettleExchange()

	if dropped {
		t.Fatal("SettleExchange scrapped the Exchange; a Turn settled on its tool call is a kept Turn")
	}
	if got := a.conv.Len(); got != 3 {
		t.Fatalf("after settle the conversation has %d messages, want 3 (user, reply, result)", got)
	}
	if first := a.conv.At(0); first.Role != domain.RoleUser || first.Content != "run the slow tool" {
		t.Errorf("message 0 = %+v, want the opening prompt", first)
	}
	last := a.conv.At(2)
	if !strings.HasPrefix(last.Content, "cancelled by the user while it ran") || !strings.Contains(last.Content, settledExchangeMarker) {
		t.Errorf("last result = %q, want the while-it-ran text carrying the cancelled note", last.Content)
	}
	if a.InExchange() {
		t.Error("SettleExchange left the Exchange open")
	}
}

// TestSettleExchange_NoExchangeIsNoop proves SettleExchange leaves a quiescent Agent with no
// open Exchange untouched — no note lands on a finished Exchange's tool result.
func TestSettleExchange_NoExchangeIsNoop(t *testing.T) {
	cfg := configWithTools(&recordingSink{}, fakeTool{name: "lookup", readOnly: true, result: "ok"})
	a, err := newAgent(cfg, scriptedResponder(t, toolCallTurn("c1", "lookup", "{}"), contentTurn("done")))
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

	dropped := a.SettleExchange()

	if dropped {
		t.Error("SettleExchange reported a drop with no open Exchange")
	}
	if got := a.conv.Len(); got != before {
		t.Errorf("SettleExchange changed the conversation with no open Exchange (had %d, now %d)", before, got)
	}
	if a.conv.HasEngineNote(cancelledNoteTopic) {
		t.Error("SettleExchange noted a tool result with no open Exchange")
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
	responder := scriptedResponder(t,
		toolCallTurn("c1", "boom", "{}"),
		contentTurn("recovered and finished"),
	)

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
	capt := echoResponder(t, "done")
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
	if !containsContent(capt.last().Messages, "use these") {
		t.Errorf("the Text was not consumed despite the unresolved FileRefs: %+v", capt.last().Messages)
	}
}

// ---------------------------------------------------------------------------
// Post-response Reactions: intercept + Defer feed-forward
// ---------------------------------------------------------------------------

// interceptReaction rewrites the assistant text in place — the intercept the moved Revision books.
func interceptReaction(replacement string) domain.Reaction {
	return postResponseReaction("intercept_once", func(_ context.Context, resp *domain.Response) (domain.Outcome, error) {
		resp.SetText(replacement)
		return domain.Outcome{}, nil
	})
}

// TestStep_PostResponseIntercept proves an intercepting Reaction's SetText reaches the
// emitted MessageEvent and the committed conversation.
func TestStep_PostResponseIntercept(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Reactions = []domain.Reaction{interceptReaction("intercepted")}

	a, err := newAgent(cfg, echoResponder(t, "original"))
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

// retryOnceReaction asks the loop to re-call the Upstream exactly once, then lets the response
// stand — the post-response retry path.
func retryOnceReaction(done *bool) domain.Reaction {
	return postResponseReaction("retry_once", func(context.Context, *domain.Response) (domain.Outcome, error) {
		if *done {
			return domain.Outcome{Edited: true}, nil
		}
		*done = true
		return domain.Outcome{Retry: true}, nil
	})
}

// TestStep_RetryEmitsStreamReset proves an Outcome{Retry} re-streams the Turn and emits a
// StreamResetEvent first, so a streaming observer discards the superseded tokens; the
// committed final message is the retried response, not the draft.
func TestStep_RetryEmitsStreamReset(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	done := false
	cfg.Reactions = []domain.Reaction{retryOnceReaction(&done)}
	responder := scriptedResponder(t,
		contentTurn("draft"),
		contentTurn("final"),
	)

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

// deferOnceReaction defers a correction on the first response only, then no-ops — the loop half
// of the Defer feed-forward path.
func deferOnceReaction(done *bool, inject string) domain.Reaction {
	return postResponseReaction("defer_once", func(context.Context, *domain.Response) (domain.Outcome, error) {
		if *done {
			return domain.Outcome{Edited: true}, nil
		}
		*done = true
		return domain.Outcome{Defer: inject}, nil
	})
}

// TestStep_DeferredCorrectionExpiresAtExchangeEnd proves the Exchange-scoped lifetime of a Deferred
// Response Action (item 7 / F6): a post-response Defer made on a no-tool FINAL answer — the
// Turn that ends the Exchange — is cleared at the Exchange boundary rather than carried into the next
// Exchange. So neither the snapshot taken after the Exchange nor the resumed next-Exchange request
// carries the correction. This reverses the pre-F6 cross-Exchange delivery (a stale directive leaking
// past an Exchange was the reviewed High defect); within-Exchange defer delivery across a
// snapshot/resume is still proven by TestDeferredAction_CancelDuringDelegationRestoresSingleDirective.
func TestStep_DeferredCorrectionExpiresAtExchangeEnd(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	done := false
	cfg.Reactions = []domain.Reaction{deferOnceReaction(&done, "remember the constraint")}

	a, err := newAgent(cfg, echoResponder(t, "first answer"))
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
	// The Exchange ended on a no-tool final answer, so the deferred correction expired with it (F6):
	// the snapshot state does not carry it.
	if strings.Contains(string(snap.State), "remember the constraint") {
		t.Error("the deferred correction survived the Exchange boundary in the snapshot; F6 should have cleared it at Exchange end")
	}

	// Resume into a fresh Agent with a capturing responder; the next Exchange's request must NOT carry
	// the expired correction — a directive never crosses an Exchange boundary.
	sink2 := &recordingSink{}
	cfg2 := baseConfig(sink2)
	cfg2.Reactions = cfg.Reactions
	capt := echoResponder(t, "second answer")
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

	if containsContent(capt.last().Messages, "remember the constraint") {
		t.Errorf("the resumed next-Exchange request carried an expired deferred correction: %+v", capt.last().Messages)
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

func containsContent(msgs []stubllm.Message, want string) bool {
	for _, m := range msgs {
		if strings.Contains(m.Content, want) {
			return true
		}
	}
	return false
}
