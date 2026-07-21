package agent

// Overflow recovery, end to end through step(): a request the model's context window rejects
// folds the history once (emergencyFold) and re-sends the SAME Turn, so a Turn that used to die
// mid-task now completes. These tests pin the guarantee and its three edges — the give-up shape
// stays byte-identical to today's, the `auto-compact: false` opt-out keeps today's behaviour with
// no extra Upstream call, and a cancel anywhere in the recovery lands on a resumable boundary.

import (
	"context"
	"encoding/json"
	"iter"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// isSummaryRequest reports whether req is the compaction summarizer's call rather than a Turn's
// request, identified by the summary system prompt (as compactSpyResponder does) — the seam a
// scripted fake needs to answer the two request kinds differently within one Turn.
func isSummaryRequest(req provider.Request) bool {
	return len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "compacting a conversation")
}

// recoveryResponder scripts the Upstream per request kind: a summarizer call always yields
// summary, while each MAIN request consumes the next entry of overflows — true ⇒ one terminal
// DeltaContextOverflow (the 400 a server sends when the prompt exceeds the window), false or past
// the end ⇒ reply. Every main request is recorded in order, so a test can assert what the retried
// request actually carried.
type recoveryResponder struct {
	reply     string
	replyCall *provider.ToolCall // when set, a non-overflowing main request ASKS FOR THIS TOOL instead of answering
	summary   string
	overflows []bool
	mains     []provider.Request
	summaries int
}

func (r *recoveryResponder) Stream(_ context.Context, req provider.Request) iter.Seq[provider.Delta] {
	if isSummaryRequest(req) {
		r.summaries++
		return streamReply(r.summary)
	}
	i := len(r.mains)
	r.mains = append(r.mains, req)
	if i < len(r.overflows) && r.overflows[i] {
		return func(yield func(provider.Delta) bool) {
			yield(provider.Delta{Kind: provider.DeltaContextOverflow, Err: overflowFaultMsg})
		}
	}
	if r.replyCall != nil {
		return func(yield func(provider.Delta) bool) {
			if !yield(provider.Delta{Kind: provider.DeltaToolCall, ToolCall: r.replyCall}) {
				return
			}
			yield(provider.Delta{Kind: provider.DeltaDone, FinishReason: "tool_calls"})
		}
	}
	return streamReply(r.reply)
}

// foldBlockingResponder overflows every main request and BLOCKS the summary call until ctx is
// cancelled — the fake for a cancel delivered while the emergency fold is in flight. started is
// closed once the summary call is running so the test can cancel deterministically (no sleep).
type foldBlockingResponder struct {
	started chan struct{}
}

func (r foldBlockingResponder) Stream(ctx context.Context, req provider.Request) iter.Seq[provider.Delta] {
	if isSummaryRequest(req) {
		return func(yield func(provider.Delta) bool) {
			close(r.started)
			<-ctx.Done()
			yield(provider.Delta{Kind: provider.DeltaError, Err: ctx.Err().Error()})
		}
	}
	return func(yield func(provider.Delta) bool) {
		yield(provider.Delta{Kind: provider.DeltaContextOverflow, Err: overflowFaultMsg})
	}
}

// assertRequestTemplateLegal fails when the wire request would be rejected by a strict chat
// template — a tool result or an assistant tool call whose partner the fold replaced, or two
// consecutive messages in the same role once past the leading system messages.
func assertRequestTemplateLegal(t *testing.T, req provider.Request) {
	t.Helper()
	prev := ""
	for i, m := range req.Messages {
		if m.Role == string(domain.RoleTool) {
			t.Errorf("request message %d is a dangling tool result: %+v", i, m)
		}
		if len(m.ToolCalls) > 0 {
			t.Errorf("request message %d carries an unanswered tool call: %+v", i, m)
		}
		if m.Role != string(domain.RoleSystem) && m.Role == prev {
			t.Errorf("request messages %d and %d are both %q; a strict template requires alternation",
				i-1, i, m.Role)
		}
		prev = m.Role
	}
}

// convRoles renders the conversation's roles for a readable failure message.
func convRoles(a *Agent) string {
	roles := make([]string, 0, a.conv.Len())
	for i := 0; i < a.conv.Len(); i++ {
		roles = append(roles, string(a.conv.At(i).Role))
	}
	return strings.Join(roles, ",")
}

// TestOverflowRecoveryFoldsAndRetriesToCompletion is the guarantee: a Turn whose request the
// server rejects for exceeding the window folds its history once and re-sends, completing the
// Exchange. The recovery is QUIET — the model answers, so nothing about the overflow reaches the
// host — and the surviving history is the folded shape plus the reply.
func TestOverflowRecoveryFoldsAndRetriesToCompletion(t *testing.T) {
	sink := &recordingSink{}
	up := &recoveryResponder{reply: "recovered reply", summary: "EMERGENCY-SUMMARY", overflows: []bool{true}}
	a, err := newAgent(autoCompactConfig(sink), up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedToolCallConv(a) // 8 messages past a 1-message protected prefix — small, so no AUTO fold fires

	if err := a.Submit(domain.UserInput{Text: "keep going"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}

	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("status = %q, want %q — the recovered Turn ran to a final answer", res.Status, domain.StatusExchangeComplete)
	}
	if up.summaries != 1 {
		t.Errorf("summarizer calls = %d, want exactly 1 (the one emergency fold this Turn may spend)", up.summaries)
	}
	if len(up.mains) != 2 {
		t.Fatalf("main requests = %d, want 2 (the overflowed one and the retry)", len(up.mains))
	}
	if errs := errorEvents(sink.events); len(errs) != 0 {
		t.Errorf("recovery surfaced %d ErrorEvent(s) %v; a successful recovery is quiet", len(errs), errs)
	}
	if me, ok := firstMessageEvent(t, sink.events); !ok || me.Text != "recovered reply" {
		t.Errorf("MessageEvent = %+v (ok=%v), want the retried reply %q", me, ok, "recovered reply")
	}

	// The folded shape, plus the reply the retry produced: prefix | summary | bridge | assistant.
	if a.conv.Len() != 4 {
		t.Fatalf("conv.Len() = %d (roles %s), want 4 (prefix + summary + bridge + reply)", a.conv.Len(), convRoles(a))
	}
	if got := a.conv.At(0); got.Role != domain.RoleUser || got.Content != "implement feature X" {
		t.Errorf("protected prefix not preserved: %+v", got)
	}
	if got := a.conv.At(1); got.Role != domain.RoleAssistant || !strings.Contains(got.Content, "EMERGENCY-SUMMARY") {
		t.Errorf("message 1 is not the assistant summary: %+v", got)
	}
	if got := a.conv.At(2); got.Role != domain.RoleUser || got.Content != overflowBridge {
		t.Errorf("message 2 is not the user bridge: %+v", got)
	}
	if got := a.conv.At(3); got.Role != domain.RoleAssistant || got.Content != "recovered reply" {
		t.Errorf("message 3 is not the retried reply: %+v", got)
	}

	// The retried request is the folded conversation — smaller than the one that overflowed, ending
	// at the bridge so the model is told to continue rather than to keep writing the summary.
	retry := up.mains[1]
	if len(retry.Messages) >= len(up.mains[0].Messages) {
		t.Errorf("retry carried %d messages, want fewer than the overflowed request's %d",
			len(retry.Messages), len(up.mains[0].Messages))
	}
	if last := retry.Messages[len(retry.Messages)-1]; last.Role != string(domain.RoleUser) || last.Content != overflowBridge {
		t.Errorf("retry does not end at the user bridge: %+v", last)
	}
	assertRequestTemplateLegal(t, retry)
}

// TestOverflowRecoveryRetriedToolCallContinuesTheExchange proves the recovered Turn rejoins the
// normal path rather than a special one: when the retried request comes back asking for a tool,
// the Turn dispatches it and ends at StatusTurnComplete — the status the REPLY dictates, not one
// the recovery imposes — leaving the Exchange open for the next Turn.
func TestOverflowRecoveryRetriedToolCallContinuesTheExchange(t *testing.T) {
	sink := &recordingSink{}
	ran := 0
	cfg := configWithTools(sink, fakeTool{name: "lookup", readOnly: true, ran: &ran, result: "the answer is 42"})
	cfg.Context.MaxContextTokens = 8192
	cfg.Context.CompactionEnabled = true
	up := &recoveryResponder{
		replyCall: &provider.ToolCall{
			ID:       "c9",
			Type:     "function",
			Function: provider.FunctionCall{Name: "lookup", Arguments: `{"q":"meaning"}`},
		},
		summary:   "EMERGENCY-SUMMARY",
		overflows: []bool{true},
	}
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedToolCallConv(a)

	if err := a.Submit(domain.UserInput{Text: "keep going"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}

	if res.Status != domain.StatusTurnComplete {
		t.Errorf("status = %q, want %q — the retried reply asked for a tool, so the Exchange stays open",
			res.Status, domain.StatusTurnComplete)
	}
	if ran != 1 {
		t.Errorf("tool ran %d times, want 1 — the recovered Turn dispatches like any other", ran)
	}
	if errs := errorEvents(sink.events); len(errs) != 0 {
		t.Errorf("recovery surfaced %d ErrorEvent(s) %v; a successful recovery is quiet", len(errs), errs)
	}
	// prefix | summary | bridge | assistant(tool call) | tool result.
	if a.conv.Len() != 5 {
		t.Fatalf("conv.Len() = %d (roles %s), want 5", a.conv.Len(), convRoles(a))
	}
	if got := a.conv.At(4); got.Role != domain.RoleTool || got.ToolCallID != "c9" {
		t.Errorf("message 4 is not the tool result for the retried call: %+v", got)
	}
}

// TestOverflowRecoveryGivesUpAfterASecondOverflow pins the bound: recovery gets ONE fold per Turn,
// so an overflow on the retried request gives up exactly as an unrecoverable overflow always did —
// one ErrorEvent from source "loop" carrying the provider's message verbatim, and a clean
// Exchange-complete boundary with no assistant message. The fold ran (it just did not help), so it
// is not repeated.
func TestOverflowRecoveryGivesUpAfterASecondOverflow(t *testing.T) {
	sink := &recordingSink{}
	up := &recoveryResponder{reply: "UNREACHED", summary: "EMERGENCY-SUMMARY", overflows: []bool{true, true}}
	a, err := newAgent(autoCompactConfig(sink), up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedToolCallConv(a)

	if err := a.Submit(domain.UserInput{Text: "keep going"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}

	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("status = %q, want %q — a spent recovery ends the Exchange at a clean boundary",
			res.Status, domain.StatusExchangeComplete)
	}
	errs := errorEvents(sink.events)
	if len(errs) != 1 {
		t.Fatalf("ErrorEvents = %d (%v), want exactly 1 — the give-up is indistinguishable from a plain fault", len(errs), errs)
	}
	if errs[0].Source != "loop" || errs[0].Err != overflowFaultMsg {
		t.Errorf("ErrorEvent = {Source:%q Err:%q}, want {Source:%q Err:%q}",
			errs[0].Source, errs[0].Err, "loop", overflowFaultMsg)
	}
	if hasEvent[domain.MessageEvent](sink.events) {
		t.Error("a MessageEvent was emitted for a Turn that produced no assistant message")
	}
	if up.summaries != 1 {
		t.Errorf("summarizer calls = %d, want exactly 1 — the Turn folds once, never twice", up.summaries)
	}
	if len(up.mains) != 2 {
		t.Errorf("main requests = %d, want 2 (the original and the single retry)", len(up.mains))
	}
}

// TestOverflowRecoveryRespectsCompactionOptOut pins decision 4 at the loop level: with
// `auto-compact: false` the emergency fold declines before any Upstream call, so an overflow
// degrades the Turn exactly as it did before recovery existed — one ErrorEvent, one abandoned
// Exchange, and crucially NO retry request on the wire.
func TestOverflowRecoveryRespectsCompactionOptOut(t *testing.T) {
	sink := &recordingSink{}
	up := &recoveryResponder{reply: "UNREACHED", summary: "UNREACHED", overflows: []bool{true}}
	cfg := autoCompactConfig(sink)
	cfg.Context.CompactionEnabled = false
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedToolCallConv(a)
	seeded := a.conv.Len()

	if err := a.Submit(domain.UserInput{Text: "keep going"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}

	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("status = %q, want %q", res.Status, domain.StatusExchangeComplete)
	}
	errs := errorEvents(sink.events)
	if len(errs) != 1 || errs[0].Source != "loop" || errs[0].Err != overflowFaultMsg {
		t.Fatalf("ErrorEvents = %v, want exactly one {Source:%q Err:%q}", errs, "loop", overflowFaultMsg)
	}
	if up.summaries != 0 {
		t.Errorf("summarizer calls = %d with auto-compact off, want 0", up.summaries)
	}
	if len(up.mains) != 1 {
		t.Errorf("main requests = %d with auto-compact off, want 1 (no retry)", len(up.mains))
	}
	if a.conv.Len() != seeded+1 {
		t.Errorf("conv.Len() = %d, want %d — the opted-out Turn folds nothing", a.conv.Len(), seeded+1)
	}
}

// seedOpenToolTurn appends the mid-Exchange shape recovery exists for: an Exchange in flight whose
// history ends in a tool result, so the next request is a tool CONTINUATION — exactly where
// autoCompact stands down (S2) and where a naive fold would orphan a tool result.
func seedOpenToolTurn(a *Agent) {
	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: "implement feature X"})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{
		{ID: "c1", Tool: "read_file", Arguments: json.RawMessage(`{"path":"main.go"}`)},
	}})
	a.conv.Append(domain.Message{Role: domain.RoleTool, ToolCallID: "c1", Content: "package main"})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, Content: "read it; now the tests"})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{
		{ID: "c2", Tool: "read_file", Arguments: json.RawMessage(`{"path":"main_test.go"}`)},
	}})
	a.conv.Append(domain.Message{Role: domain.RoleTool, ToolCallID: "c2", Content: strings.Repeat("huge file body ", 200)})
	a.inExchange = true
	a.exchangeStart = 1
}

// TestOverflowRecoveryOnToolContinuationIsTemplateLegal drives the real failure mode: a whole-file
// read blows the window MID-Exchange, on a tool-continuation Turn. Recovery must work there (the
// Exchange cannot wait for a boundary that will never come) and the retried request must be one a
// strict chat template accepts — no orphaned tool result, no unanswered tool call, no two
// consecutive same-role messages.
func TestOverflowRecoveryOnToolContinuationIsTemplateLegal(t *testing.T) {
	sink := &recordingSink{}
	up := &recoveryResponder{reply: "continuing from the summary", summary: "EMERGENCY-SUMMARY", overflows: []bool{true}}
	a, err := newAgent(autoCompactConfig(sink), up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedOpenToolTurn(a) // no Submit: the Exchange is already open, so this Step is a continuation

	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}

	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("status = %q, want %q", res.Status, domain.StatusExchangeComplete)
	}
	if errs := errorEvents(sink.events); len(errs) != 0 {
		t.Errorf("recovery surfaced %d ErrorEvent(s) %v; a successful mid-Exchange recovery is quiet", len(errs), errs)
	}
	if len(up.mains) != 2 {
		t.Fatalf("main requests = %d, want 2 (the overflowed continuation and the retry)", len(up.mains))
	}

	// The setup really was the pathological shape: the request that overflowed carried tool results.
	var toolResults int
	for _, m := range up.mains[0].Messages {
		if m.Role == string(domain.RoleTool) {
			toolResults++
		}
	}
	if toolResults == 0 {
		t.Fatal("test setup: the overflowed request carried no tool results, so it is not a tool continuation")
	}

	assertRequestTemplateLegal(t, up.mains[1])
	if last := up.mains[1].Messages[len(up.mains[1].Messages)-1]; last.Content != overflowBridge {
		t.Errorf("retried continuation does not end at the user bridge: %+v", last)
	}
	if a.conv.Len() != 4 {
		t.Errorf("conv.Len() = %d (roles %s), want 4 (prefix + summary + bridge + reply)", a.conv.Len(), convRoles(a))
	}
	// The mid-Exchange fold re-anchored the Exchange boundary, so the Exchange the recovery carried
	// to completion left a conversation a later Exchange can be aborted out of cleanly.
	if a.exchangeStart != 2 {
		t.Errorf("exchangeStart = %d after the mid-Exchange fold, want 2 (the bridge's index)", a.exchangeStart)
	}
}

// TestOverflowRecoveryCancelDuringFoldIsResumable pins the cancel path: a cancel delivered while
// the summary call is in flight lands on StatusCancelled — not on the overflow's give-up event —
// with the conversation uncorrupted, and the snapshot taken there resumes and completes the Turn.
func TestOverflowRecoveryCancelDuringFoldIsResumable(t *testing.T) {
	sink := &recordingSink{}
	up := foldBlockingResponder{started: make(chan struct{})}
	a, err := newAgent(autoCompactConfig(sink), up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedToolCallConv(a)
	if err := a.Submit(domain.UserInput{Text: "keep going"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	want := a.conv.Len() + 1 // the seeded history plus the user message this Turn committed

	ctx, cancel := context.WithCancel(context.Background())
	type outcome struct {
		res domain.StepResult
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := a.Step(ctx)
		done <- outcome{res, err}
	}()

	<-up.started // the emergency fold's summary call is in flight; cancel deterministically
	cancel()
	got := <-done

	if got.err != nil {
		t.Fatalf("Step returned a loop error on a cancelled fold: %v", got.err)
	}
	if got.res.Status != domain.StatusCancelled {
		t.Fatalf("status = %q, want %q — a cancel wins over the overflow give-up", got.res.Status, domain.StatusCancelled)
	}
	if errs := errorEvents(sink.events); len(errs) != 0 {
		t.Errorf("a cancelled recovery surfaced %d ErrorEvent(s) %v; the cancel is the outcome, not a fault", len(errs), errs)
	}
	if a.conv.Len() != want {
		t.Errorf("conv.Len() = %d (roles %s), want %d — a cancelled fold leaves history untouched",
			a.conv.Len(), convRoles(a), want)
	}

	// The boundary is serializable: resume against a working Upstream and re-attempt the Turn.
	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot after cancel: %v", err)
	}
	b, err := resumeAgent(autoCompactConfig(&recordingSink{}), snap, echoResponder{reply: "resumed reply"})
	if err != nil {
		t.Fatalf("resumeAgent: %v", err)
	}
	res, err := b.Step(context.Background())
	if err != nil {
		t.Fatalf("Step (resumed): %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("resumed Step status = %q, want %q", res.Status, domain.StatusExchangeComplete)
	}
}
