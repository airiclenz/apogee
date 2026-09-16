package agent

// The /compact failure/cancel spine (post-v1.0.0 remediation item 3). The happy path lives in
// minilang_test.go (TestCompactSummarizesAndReplacesHistoryKeepingPrefix); this file exercises
// the fault side — precisely where the truthfulness fixes (plan item 2a/2b) live and where
// /compact runs most (on-demand compaction fires when the upstream is likeliest to fault, at
// high context fill). Every fault must leave the conversation untouched so a failed /compact
// never corrupts history, and compaction must stay out of the transcript (no TokenEvent). Its
// token accounting is NOT silent: a summary call the server accounts for rides one
// Maintenance-flagged UsageEvent, pinned in usagetally_test.go.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/stubllm"
)

// overflowResponder is the upstream for the "prompt too long" path: it answers every request with
// the 400 a server sends when the request itself exceeds the context window — unconditionally,
// regardless of prompt size — which the provider classes DeltaContextOverflow. It stands for the
// unbudgetable case: a server that rejects even a minimal prompt, where no transcript budget can
// help (item 6's window-derived one or the unknown-window default), so the fault must still
// surface cleanly and leave the conversation untouched.
func overflowResponder(t testing.TB) *scriptedUpstream {
	t.Helper()
	return scriptedResponder(t, stubllm.Turn{Repeat: true, HTTP: &stubllm.HTTPReply{
		Status:      http.StatusBadRequest,
		Body:        overflowBody,
		ContentType: "application/json",
	}})
}

// overflowBody is the llama.cpp 400 body for a prompt past the window, carrying the marker the
// provider's classifier reads (isContextOverflow).
const overflowBody = `{"error":{"message":"request (57546 tokens) exceeds the available context size (32768 tokens)"}}`

// windowResponder models a real server's context limit: it overflows (the 400 a server sends when
// the prompt itself exceeds the window) exactly when the request's estimated prompt tokens exceed
// window, and otherwise echoes reply. It records the last request so a test can assert what the
// budgeted summary call actually carried. It uses the same 4-chars-per-token estimate the Agent's
// uncalibrated budget does (context.DefaultCharsPerToken — only the regular turn path calibrates
// the estimator; compaction accounts its usage but never calibrates it, so in these fold-only
// tests the ratio never leaves the default), so the responder and the reducer agree on when a
// prompt fits.
type windowResponder struct {
	window int
	reply  string
	last   provider.Request
}

func (r *windowResponder) Stream(_ context.Context, req provider.Request) iter.Seq[provider.Delta] {
	r.last = req
	chars := 0
	for _, m := range req.Messages {
		chars += len(m.Content)
	}
	if chars/4 > r.window {
		return func(yield func(provider.Delta) bool) {
			yield(provider.Delta{Kind: provider.DeltaContextOverflow, Err: "apogee: context window exceeded"})
		}
	}
	return streamReply(r.reply)
}

// seedFoldable appends a text-only conversation with enough messages past the protected prefix
// (first user message) that Compact does real work rather than skipping: 4 messages, tail 3 ≥
// minCompactTail. The agent starts empty, so appending directly is the conversation state a
// couple of exchanges would have produced.
func seedFoldable(a *Agent) {
	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: "task one"})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, Content: "on it"})
	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: "task two"})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, Content: "done"})
}

// TestCompactUnbudgetableOverflowErrorsAndLeavesConvUntouched pins the residual fault: when no
// transcript budget can help — a server that overflows unconditionally, even under the
// unknown-window default baseConfig now renders through — the summary call still overflows, so
// Compact surfaces the error, reports skipped=false (a fault is not a skip), and leaves the
// conversation untouched. This is the "budget can't save it" backstop; the survivable high-fill
// case is TestCompactSurvivesHighFillViaTranscriptBudget below.
func TestCompactUnbudgetableOverflowErrorsAndLeavesConvUntouched(t *testing.T) {
	a, err := newAgent(baseConfig(&recordingSink{}), overflowResponder(t))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedFoldable(a)
	before := a.conv.Len()

	skipped, err := a.Compact(context.Background())
	if err == nil {
		t.Fatal("Compact err = nil, want the overflow surfaced as an error")
	}
	if skipped {
		t.Error("skipped = true on an overflow fault; a fault is not a skip")
	}
	if !strings.Contains(err.Error(), "context window exceeded") {
		t.Errorf("Compact err = %v, want the overflow message surfaced", err)
	}
	if a.conv.Len() != before {
		t.Errorf("conv mutated despite an overflow fault: Len = %d, want %d", a.conv.Len(), before)
	}
}

// seedLargeConv appends a conversation whose full rendered transcript far exceeds the test's
// context window, so an unbudgeted summary request would overflow: one protected-prefix message
// plus 60 turns of ~700 chars each (~42k chars ≈ 10.5k tokens against an 8k window).
func seedLargeConv(a *Agent) {
	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: "the OVERARCHING-GOAL to keep in the prefix"})
	for i := 0; i < 60; i++ {
		role := domain.RoleAssistant
		if i%2 == 0 {
			role = domain.RoleUser
		}
		a.conv.Append(domain.Message{Role: role, Content: fmt.Sprintf("turn %d: %s", i, strings.Repeat("detail ", 100))})
	}
}

// convContentChars sums the raw content length across the whole conversation — the size the
// summary request would carry if the transcript were rendered unbudgeted.
func convContentChars(a *Agent) int {
	total := 0
	for i := 0; i < a.conv.Len(); i++ {
		total += len(a.conv.At(i).Content)
	}
	return total
}

// TestCompactSurvivesHighFillViaTranscriptBudget is the item-6 flip: at a fill where the full
// transcript would overflow the summary call, Compact now succeeds because the reducer budgets
// the rendered transcript to the discovered window. windowResponder overflows iff the prompt
// exceeds the window, so a successful fold *is* proof the request was budgeted under it — and the
// request it received is both within the window and smaller than the raw conversation.
func TestCompactSurvivesHighFillViaTranscriptBudget(t *testing.T) {
	const window = 8192
	up := &windowResponder{window: window, reply: "FOLDED-SUMMARY"}
	cfg := baseConfig(&recordingSink{})
	cfg.Context.MaxContextTokens = window
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedLargeConv(a)
	rawChars := convContentChars(a)
	if rawChars/4 <= window {
		t.Fatalf("test setup: conversation (%d chars) does not exceed the window; it must overflow unbudgeted", rawChars)
	}
	before := a.conv.Len()

	skipped, err := a.Compact(context.Background())
	if err != nil {
		t.Fatalf("Compact errored despite the transcript budget (item 6 regression): %v", err)
	}
	if skipped {
		t.Fatal("skipped = true; want a real fold of a large conversation")
	}

	// Folded to the clean prefix → summary shape.
	if a.conv.Len() >= before {
		t.Errorf("conv not folded: Len = %d, want < %d", a.conv.Len(), before)
	}
	if got := a.conv.At(a.conv.Len() - 1); got.Role != domain.RoleAssistant || !strings.Contains(got.Content, "FOLDED-SUMMARY") {
		t.Errorf("last message is not the summary: %+v", got)
	}

	// The request the server actually saw fit under the window (else it would have overflowed) and
	// was reduced from the raw conversation — the budget did real elision work.
	reqChars := 0
	for _, m := range up.last.Messages {
		reqChars += len(m.Content)
	}
	if reqChars/4 > window {
		t.Errorf("budgeted summary prompt still exceeds the window: %d tokens > %d", reqChars/4, window)
	}
	if reqChars >= rawChars {
		t.Errorf("summary prompt was not reduced by the budget: %d chars >= raw %d", reqChars, rawChars)
	}
}

// TestCompactTranscriptCharsIsAlwaysBounded pins the summary call's char budget across the three
// window regimes it has to serve. The unknown-window row is the one the audit's wedge turned on: a
// zero budget means "render the whole conversation" to the reducer, so it must never be returned —
// the conservative default stands in until a window is known. A known window's arithmetic is
// unchanged, floor included.
func TestCompactTranscriptCharsIsAlwaysBounded(t *testing.T) {
	tests := []struct {
		name       string
		window     int
		wantTokens int
	}{
		{
			name:       "an unknown window falls back to the conservative default",
			window:     0,
			wantTokens: compactUnknownWindowTranscriptTokens,
		},
		{
			name:       "a known window keeps the window-derived budget",
			window:     32768,
			wantTokens: 32768 - compactMaxTokens - compactPromptOverheadTokens,
		},
		{
			name:       "a window smaller than the reserves floors instead of going negative",
			window:     2048,
			wantTokens: compactMinTranscriptTokens,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseConfig(&recordingSink{})
			cfg.Context.MaxContextTokens = tc.window
			a, err := newAgent(cfg, echoResponder(t, "unused"))
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}

			got := a.compactTranscriptChars()

			if want := int(float64(tc.wantTokens) * a.budget().CharsPerToken); got != want {
				t.Errorf("compactTranscriptChars() = %d, want %d (%d tokens × %.1f chars/token)",
					got, want, tc.wantTokens, a.budget().CharsPerToken)
			}
			if got <= 0 {
				t.Error("compactTranscriptChars() = 0, which renders the WHOLE conversation into the summary call")
			}
		})
	}
}

// TestCompactCancelMidSummaryLeavesConvUntouched drives the cancel-mid-summary path: the
// blocking responder surfaces the cancellation as a terminal DeltaError, but ctx.Err() wins
// over that masqueraded stream error (as in respondAndReview), so Compact returns
// context.Canceled — the exact signal startCompact classifies as a cancel — and the
// conversation is untouched.
func TestCompactCancelMidSummaryLeavesConvUntouched(t *testing.T) {
	responder := blockingResponder{started: make(chan struct{})}
	a, err := newAgent(baseConfig(&recordingSink{}), responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedFoldable(a)
	before := a.conv.Len()

	ctx, cancel := context.WithCancel(context.Background())
	type outcome struct {
		skipped bool
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		skipped, err := a.Compact(ctx)
		done <- outcome{skipped, err}
	}()

	<-responder.started // the summary call is in flight; cancel deterministically (no sleep)
	cancel()
	got := <-done

	if !errors.Is(got.err, context.Canceled) {
		t.Fatalf("Compact err = %v, want context.Canceled (ctx wins over the masqueraded DeltaError)", got.err)
	}
	if got.skipped {
		t.Error("skipped = true on a cancel; a cancel is not a skip")
	}
	if a.conv.Len() != before {
		t.Errorf("conv mutated on a cancelled compaction: Len = %d, want %d", a.conv.Len(), before)
	}
}

// TestCompactEmitsNoTokenEventAndNoUsageWithoutServerReport pins the transcript half of the
// contract: compaction is a maintenance call, not a Turn, so it must not stream into the
// transcript (TokenEvent). Its accounting event is conditional on the server reporting usage the
// same way a Turn's is — echoResponder reports none, so this fold accounts for nothing and emits
// no UsageEvent either. The accounted case (a flagged Maintenance event) is pinned by
// TestCompactionUsageRidesFlaggedMaintenanceEvent. A real exchange first (which does emit events)
// proves the sink is wired; the events it produced are dropped so only compaction's emissions are
// asserted on.
func TestCompactEmitsNoTokenEventAndNoUsageWithoutServerReport(t *testing.T) {
	sink := &recordingSink{}
	a, err := newAgent(baseConfig(sink), echoResponder(t, "reply"))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	// Two exchanges → a foldable [user, assistant, user, assistant] conversation. These DO emit
	// Token/Usage events; we discard them and assert only on what compaction emits next.
	for _, text := range []string{"task one", "task two"} {
		if err := a.Submit(domain.UserInput{Text: text}); err != nil {
			t.Fatalf("Submit: %v", err)
		}
		if _, err := a.Step(context.Background()); err != nil {
			t.Fatalf("Step: %v", err)
		}
	}
	sink.events = nil // only compaction's emissions matter from here

	if skipped, err := a.Compact(context.Background()); err != nil {
		t.Fatalf("Compact: %v", err)
	} else if skipped {
		t.Fatal("Compact skipped a foldable conversation; want a real (silent) fold")
	}

	if hasEvent[domain.TokenEvent](sink.events) {
		t.Error("compaction emitted a TokenEvent; it must not stream into the transcript")
	}
	if hasEvent[domain.UsageEvent](sink.events) {
		t.Error("compaction emitted a UsageEvent though the server reported no usage; there was nothing to account for")
	}
}

// seedToolCallConv appends the shape /compact exists to fold: assistant tool calls paired with
// their RoleTool results (the strict-template pairing a naive truncation would orphan),
// interleaved with prose. 8 messages, protected prefix 1 (the first user message).
func seedToolCallConv(a *Agent) {
	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: "implement feature X"})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{
		{ID: "c1", Tool: "read_file", Arguments: json.RawMessage(`{"path":"main.go"}`)},
	}})
	a.conv.Append(domain.Message{Role: domain.RoleTool, ToolCallID: "c1", Content: "package main"})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, Content: "read it; here is the plan"})
	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: "now add tests"})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{
		{ID: "c2", Tool: "write_file", Arguments: json.RawMessage(`{"path":"main_test.go"}`)},
	}})
	a.conv.Append(domain.Message{Role: domain.RoleTool, ToolCallID: "c2", Content: "wrote 1 file"})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, Content: "done"})
}

// TestCompactFoldsToolCallTurnsWithoutDanglingResults folds a conversation full of tool-call
// turns — the real /compact workload — and proves the result is a clean prefix →
// assistant-summary shape with NO surviving RoleTool message (a dangling tool result would break
// strict role alternation on the next user message). The summarizer still saw the tool work
// (calls rendered inline), and the folded Agent stays snapshot-safe: Snapshot → Resume → Submit
// → Step runs to completion.
func TestCompactFoldsToolCallTurnsWithoutDanglingResults(t *testing.T) {
	up := echoResponder(t, "FOLDED-SUMMARY")
	a, err := newAgent(baseConfig(&recordingSink{}), up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedToolCallConv(a)

	skipped, err := a.Compact(context.Background())
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if skipped {
		t.Fatal("Compact skipped a tool-heavy conversation; want a fold")
	}

	// Clean prefix → assistant-summary shape: exactly the first user message plus one summary,
	// and no tool result orphaned by the fold.
	if a.conv.Len() != 2 {
		t.Fatalf("conv.Len() = %d after fold, want 2 (prefix + summary)", a.conv.Len())
	}
	for i := 0; i < a.conv.Len(); i++ {
		if got := a.conv.At(i); got.Role == domain.RoleTool {
			t.Errorf("message %d is a dangling tool result after the fold: %+v", i, got)
		}
	}
	if got := a.conv.At(0); got.Role != domain.RoleUser || got.Content != "implement feature X" {
		t.Errorf("protected prefix not preserved: %+v", got)
	}
	if sum := a.conv.At(1); sum.Role != domain.RoleAssistant || !strings.Contains(sum.Content, "FOLDED-SUMMARY") {
		t.Errorf("summary message wrong: %+v", sum)
	}

	// The summarizer saw the tool work, not just prose (renderTranscript inlines the calls).
	body := up.last().Messages[len(up.last().Messages)-1].Content
	for _, want := range []string{"read_file", "write_file"} {
		if !strings.Contains(body, want) {
			t.Errorf("summary request missing tool %q:\n%s", want, body)
		}
	}

	// The folded Agent snapshots and resumes cleanly, and the resumed Agent completes a Turn —
	// proving the fold left no state that trips resume or the next exchange.
	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	b, err := resumeAgent(baseConfig(&recordingSink{}), snap, echoResponder(t, "resumed reply"))
	if err != nil {
		t.Fatalf("resumeAgent: %v", err)
	}
	if err := b.Submit(domain.UserInput{Text: "continue"}); err != nil {
		t.Fatalf("Submit (resumed): %v", err)
	}
	res, err := b.Step(context.Background())
	if err != nil {
		t.Fatalf("Step (resumed): %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("resumed Step status = %q, want %q", res.Status, domain.StatusExchangeComplete)
	}
}

// TestCompactSummaryRequestOmitsSystemPrompt: the summariser is a separate request path — it
// builds its own message list around the summariser's dedicated system prompt rather than
// going through buildRequest — so the user's configured system prompt (ADR 0023) must never
// reach it. Pinned here because the two prompts would otherwise compete for the summary.
func TestCompactSummaryRequestOmitsSystemPrompt(t *testing.T) {
	const marker = "MARKER-SYSTEM-PROMPT-COMPACT"

	cfg := baseConfig(&recordingSink{})
	cfg.SystemPrompt = "Remember " + marker + " while working in {{workspace}}."

	up := echoResponder(t, "FOLDED-SUMMARY")
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedFoldable(a)

	skipped, err := a.Compact(context.Background())
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if skipped {
		t.Fatal("Compact skipped a foldable conversation; want a fold so a summary request was made")
	}

	got := up.last()
	if len(got.Messages) == 0 || got.Messages[0].Role != string(domain.RoleSystem) {
		t.Fatalf("summary request messages = %+v, want the summariser's own system message first", got.Messages)
	}
	if !strings.HasPrefix(got.Messages[0].Content, "You are compacting a conversation") {
		t.Errorf("summary request system message = %q, want the summariser's own instruction", got.Messages[0].Content)
	}
	for i, m := range got.Messages {
		if strings.Contains(m.Content, marker) {
			t.Errorf("summary request message %d carries the configured system prompt: %q", i, m.Content)
		}
	}
}

// ---------------------------------------------------------------------------

// summaryEffortResponder records the SUMMARIZER's request beside the last MAIN-turn one, so a
// test can assert what the fold asked for on the wire and what the very next Turn asked for. It
// tells the two apart by the summary system prompt, as scriptedCompactResponder does —
// internal/context's summaryInstruction is unexported, and the leading substring is the stable
// half of it.
type summaryEffortResponder struct {
	summary      string
	reply        string
	summaryReq   provider.Request
	summaryCalls int
	last         provider.Request
}

func (r *summaryEffortResponder) Stream(_ context.Context, req provider.Request) iter.Seq[provider.Delta] {
	if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "compacting a conversation") {
		r.summaryCalls++
		r.summaryReq = req
		return streamReply(r.summary)
	}
	r.last = req
	return streamReply(r.reply)
}

// foldOnce folds a freshly seeded conversation and fails the test unless a summary call actually
// went out — a skipped fold makes every assertion below it read a stale request.
func foldOnce(t *testing.T, a *Agent, up *summaryEffortResponder, want int) {
	t.Helper()
	seedFoldable(a)
	skipped, err := a.Compact(context.Background())
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if skipped {
		t.Fatal("Compact skipped a foldable conversation; want a fold so a summary request was made")
	}
	if up.summaryCalls != want {
		t.Fatalf("summarizer calls = %d, want %d", up.summaryCalls, want)
	}
}

// TestCompactSummarizerAsksForNoReasoning pins the 2026-08-29 empty-summary fix on the dialect the
// incident server speaks (llama.cpp's chat_template_kwargs): the summary call asks for no
// reasoning pass whatever the session's effort resolves to, so a thinking model cannot spend the
// whole 4096-token cap thinking and come back with nothing. The session override is the sharp
// case — it outranks the profile everywhere else (ADR 0050), and it must still not reach this
// maintenance call, while the very next real Turn carries it untouched.
func TestCompactSummarizerAsksForNoReasoning(t *testing.T) {
	t.Parallel()

	cfg := baseConfig(&recordingSink{})
	cfg.EffortDialect = domain.EffortDialectKwargs
	cfg.Profile.Thinking.Effort = domain.EffortMedium
	up := &summaryEffortResponder{summary: "FOLDED", reply: "done"}
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	foldOnce(t, a, up, 1)
	if got := up.summaryReq.ThinkingEffort; got != provider.EffortOff {
		t.Errorf("summary request effort = %q, want %q — the profile's level must not reach the summarizer",
			got, provider.EffortOff)
	}
	if got := up.summaryReq.EffortDialect; got != provider.EffortDialectKwargs {
		t.Errorf("summary request dialect = %q, want the server's %q left alone", got, provider.EffortDialectKwargs)
	}

	// The session override is intent about the conversation, not about a maintenance call.
	a.SetEffortOverride(domain.EffortHigh)
	foldOnce(t, a, up, 2)
	if got := up.summaryReq.ThinkingEffort; got != provider.EffortOff {
		t.Errorf("summary request effort under a session override = %q, want %q", got, provider.EffortOff)
	}

	// ...and the next real Turn still carries it, so the override was suppressed for the summary
	// call alone rather than dropped.
	if err := a.Submit(domain.UserInput{Text: "carry on"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if got := up.last.ThinkingEffort; got != provider.EffortHigh {
		t.Errorf("main-turn effort = %q, want the session override %q", got, provider.EffortHigh)
	}
}

// TestCompactSummarizerKeepsTheResolvedEffortOnAnUndialledServer is the anchor half: on a server
// that named no effort dialect, apogee asks for nothing it did not ask for before this override
// existed (ADR 0050 — a caller that asks for nothing changes nothing on the wire), so the summary
// request carries resolvedEffort byte for byte: nothing when nothing is configured, the session
// override when one is set.
func TestCompactSummarizerKeepsTheResolvedEffortOnAnUndialledServer(t *testing.T) {
	t.Parallel()

	up := &summaryEffortResponder{summary: "FOLDED", reply: "done"}
	a, err := newAgent(baseConfig(&recordingSink{}), up) // baseConfig names no dialect
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	foldOnce(t, a, up, 1)
	if got := up.summaryReq.ThinkingEffort; got != provider.Effort("") {
		t.Errorf("summary request effort = %q, want none — nothing is configured and no dialect was named", got)
	}
	if got := up.summaryReq.EffortDialect; got != provider.EffortDialectNone {
		t.Errorf("summary request dialect = %q, want the zero anchor", got)
	}

	a.SetEffortOverride(domain.EffortHigh)
	foldOnce(t, a, up, 2)
	if got := up.summaryReq.ThinkingEffort; got != provider.EffortHigh {
		t.Errorf("summary request effort = %q, want the session's resolved %q untouched on an undialled server",
			got, provider.EffortHigh)
	}
}

// TestChildSummarizerFollowsTheParentsReboundDialect is the delegate half of the incident: the
// dialect is discovered and committed by Rebind onto the Agent, NOT onto the Config a child is
// built from, so a child spawned after a rebind must take its parent's LIVE dialect — otherwise
// exactly the delegate that looped for nine hours would keep speaking the shape of the server the
// session was on at startup and the EffortOff override would never fire for it.
func TestChildSummarizerFollowsTheParentsReboundDialect(t *testing.T) {
	t.Parallel()

	up := &summaryEffortResponder{summary: "FOLDED", reply: "done"}
	parent, err := newAgent(baseConfig(&recordingSink{}), up) // the startup Config names no dialect
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := parent.Rebind(RebindSpec{
		Model:            "another-model",
		MaxContextTokens: 8192,
		EffortDialect:    provider.EffortDialectKwargs,
	}); err != nil {
		t.Fatalf("Rebind: %v", err)
	}

	child, err := parent.newChildAgent("call_sub", "the delegated task", "")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}
	seedFoldable(child)
	if _, err := child.Compact(context.Background()); err != nil {
		t.Fatalf("child Compact: %v", err)
	}
	if up.summaryCalls != 1 {
		t.Fatalf("summarizer calls = %d, want exactly the child's one fold", up.summaryCalls)
	}
	if got := up.summaryReq.EffortDialect; got != provider.EffortDialectKwargs {
		t.Fatalf("child summary request dialect = %q, want the parent's rebound %q", got, provider.EffortDialectKwargs)
	}
	if got := up.summaryReq.ThinkingEffort; got != provider.EffortOff {
		t.Errorf("child summary request effort = %q, want %q", got, provider.EffortOff)
	}
}

// cappedSummaryTurn scripts one summary reply by its three observable parts: the reasoning channel
// the server splits out (reasoning_content), the visible content, and the finish reason it ends
// on. It is the turn for the 2026-08-29 incident — a thinking model that spends the whole
// compactMaxTokens cap reasoning and ends on "length" with nothing visible — and, with content and
// no separate channel, for the inline-<think> shape a delimited profile emits.
func cappedSummaryTurn(thinking, content, finish string) stubllm.Turn {
	return stubllm.Turn{Reasoning: thinking, Text: content, FinishReason: finish}
}

// cappedSummaryResponder is cappedSummaryTurn as a hand-written fake. Its one remaining user is
// maxTokRecordingResponder, which reads the summariser request's Sampling — a field the stubllm
// request log does not yet carry.
type cappedSummaryResponder struct {
	thinking string
	content  string
	finish   string
}

func (r cappedSummaryResponder) Stream(context.Context, provider.Request) iter.Seq[provider.Delta] {
	return func(yield func(provider.Delta) bool) {
		if r.thinking != "" && !yield(provider.Delta{Kind: provider.DeltaThinking, Thinking: r.thinking}) {
			return
		}
		if r.content != "" && !yield(provider.Delta{Kind: provider.DeltaContent, Content: r.content}) {
			return
		}
		yield(provider.Delta{Kind: provider.DeltaDone, FinishReason: r.finish})
	}
}

// TestCompactBlankSummaryFaultsOnTheCapOnlyWhenItWasCut pins what a blank summary SAYS. A reply
// that ran into compactMaxTokens is the 2026-08-29 incident's shape — the model answered, at
// length, and spent the entire cap on a reasoning pass the summarizer asked it not to make — so the
// fault names the cap and, when the reply carried reasoning, roughly what it burned under it; those
// are the two numbers an operator acts on. Every OTHER blank reply keeps the reducer's
// errEmptySummary verbatim, which describes a different failure (a model that produced nothing at
// all). All three leave the conversation untouched — context.Compact's guarantee.
func TestCompactBlankSummaryFaultsOnTheCapOnlyWhenItWasCut(t *testing.T) {
	t.Parallel()

	const reasoning = "The user asked for a fold; I should restate the task, the files touched, and the open question."

	cases := []struct {
		name      string
		up        stubllm.Turn
		capped    bool
		wantSpend bool
	}{
		{
			name:      "reasoning-only reply cut at the cap",
			up:        cappedSummaryTurn(reasoning, "", "length"),
			capped:    true,
			wantSpend: true,
		},
		{
			name:   "reply cut at the cap with no reasoning channel",
			up:     cappedSummaryTurn("", "", "length"),
			capped: true,
		},
		{
			name: "blank reply the server called finished",
			up:   cappedSummaryTurn("", "", "stop"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			a, err := newAgent(baseConfig(&recordingSink{}), scriptedResponder(t, tc.up))
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			seedFoldable(a)
			before := a.conv.Len()

			skipped, err := a.Compact(context.Background())

			if err == nil {
				t.Fatal("Compact err = nil, want a blank summary surfaced as a fault")
			}
			if skipped {
				t.Error("skipped = true on a blank summary; a fault is not a skip")
			}
			want := "apogee: compaction produced an empty summary"
			if tc.capped {
				spend := ""
				if tc.wantSpend {
					spend = fmt.Sprintf(", after roughly %d tokens of reasoning", a.tokens.EstimateTokens(len(reasoning)))
				}
				want = fmt.Sprintf("compaction summary hit its output cap (4096 tokens) with no visible text to "+
					"show for it%s — the cap went on a reasoning pass this server was never asked to skip",
					spend)
			}
			if err.Error() != want {
				t.Errorf("Compact err = %q, want %q", err, want)
			}
			if a.conv.Len() != before {
				t.Errorf("conv mutated despite a fault: Len = %d, want %d", a.conv.Len(), before)
			}
		})
	}
}

// TestCompactCappedSummaryFaultNamesOnlyWhatTheRequestAsked pins the halves of the capped-summary
// fault. The engine cannot inspect the server it just called, so the text stops at what it knows:
// when the summary request itself carried the off rung it says the ask went out and the server
// reasoned regardless; when it carried none — the three dialects compactCompleter leaves alone —
// it says the cap went on a pass nobody asked to skip, rather than accusing a template of ignoring
// a key that was never sent. The split is keyed on the REQUEST, not the dialect: the last case is a
// session on no dialect at all whose profile pins `effort: off`, which DID ask (applyEffort emits
// the kwargs hint for it) and must read as the asked half.
func TestCompactCappedSummaryFaultNamesOnlyWhatTheRequestAsked(t *testing.T) {
	t.Parallel()

	const reasoning = "Restate the task, the files touched, and the open question before summarising."
	const head = "compaction summary hit its output cap (4096 tokens) with no visible text to show for it"
	const asked = " — the summarizer asked for no reasoning and this server reasoned anyway"
	const notAsked = " — the cap went on a reasoning pass this server was never asked to skip"

	cases := []struct {
		name     string
		dialect  domain.EffortDialect
		effort   domain.ThinkingEffort
		wantTail string
	}{
		{name: "kwargs carries the off override", dialect: domain.EffortDialectKwargs, wantTail: asked},
		{name: "reasoning carries the off override", dialect: domain.EffortDialectReasoning, wantTail: asked},
		{name: "openai floors rather than switches off", dialect: domain.EffortDialectOpenAI, wantTail: notAsked},
		{name: "no dialect asks for nothing", wantTail: notAsked},
		{name: "the dialect is off outright", dialect: domain.EffortDialectOff, wantTail: notAsked},
		{
			name:     "no dialect but the profile pins effort off",
			effort:   domain.EffortOff,
			wantTail: asked,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := baseConfig(&recordingSink{})
			cfg.EffortDialect = tc.dialect
			cfg.Profile.Thinking.Effort = tc.effort
			a, err := newAgent(cfg, scriptedResponder(t, cappedSummaryTurn(reasoning, "", "length")))
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			seedFoldable(a)

			_, err = a.Compact(context.Background())

			if err == nil {
				t.Fatal("Compact err = nil, want the capped-summary fault")
			}
			spend := fmt.Sprintf(", after roughly %d tokens of reasoning", a.tokens.EstimateTokens(len(reasoning)))
			want := head + spend + tc.wantTail
			if err.Error() != want {
				t.Errorf("Compact err = %q, want %q", err, want)
			}
		})
	}
}

// maxTokRecordingResponder is cappedSummaryResponder with one addition: it records the MaxTokens
// the summariser request actually carried, so a test can hold the fault text against the number the
// server was sent rather than against the constant the engine happens to set today.
type maxTokRecordingResponder struct {
	cappedSummaryResponder
	sent *int
}

func (r maxTokRecordingResponder) Stream(ctx context.Context, req provider.Request) iter.Seq[provider.Delta] {
	if req.Sampling.MaxTokens != nil {
		*r.sent = *req.Sampling.MaxTokens
	}
	return r.cappedSummaryResponder.Stream(ctx, req)
}

// TestCompactCappedSummaryFaultNamesTheAppliedCap pins that the capped-summary fault names the cap
// the request was SENT with — the MaxTokens set on the summariser request — not the bare constant.
// Today the two agree (compactMaxTokens), and the test pins that too; the point is that the number
// the reader sees is read back from the request, so the fault cannot drift from what the server was
// actually asked for should the applied cap ever differ from the constant.
func TestCompactCappedSummaryFaultNamesTheAppliedCap(t *testing.T) {
	t.Parallel()

	sent := 0
	up := maxTokRecordingResponder{
		cappedSummaryResponder: cappedSummaryResponder{thinking: "plan the summary at length", finish: "length"},
		sent:                   &sent,
	}
	a, err := newAgent(baseConfig(&recordingSink{}), up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedFoldable(a)

	_, err = a.Compact(context.Background())

	if err == nil {
		t.Fatal("Compact err = nil, want the capped-summary fault")
	}
	if sent == 0 {
		t.Fatal("the summariser request carried no MaxTokens; want the cap on the request")
	}
	if sent != compactMaxTokens {
		t.Errorf("summariser request MaxTokens = %d, want compactMaxTokens (%d)", sent, compactMaxTokens)
	}
	wantHead := fmt.Sprintf("compaction summary hit its output cap (%d tokens)", sent)
	if !strings.HasPrefix(err.Error(), wantHead) {
		t.Errorf("Compact err = %q, want it to open with %q — the cap the request carried", err, wantHead)
	}
}

// TestCompactStripsInlineThinkingFromTheSummary: on a delimited-thinking profile the summarizer's
// reply carries its reasoning inline, and the fold runs it through the same stripper a Turn's reply
// goes through — otherwise the <think> span is written into the summary message and the folded
// conversation carries the model's scratchpad forward as if it were history.
func TestCompactStripsInlineThinkingFromTheSummary(t *testing.T) {
	t.Parallel()

	cfg := baseConfig(&recordingSink{})
	cfg.Profile = domain.ModelProfile{
		Thinking: domain.ThinkingProfile{Style: domain.ThinkingDelimited, Start: "<think>", End: "</think>"},
	}
	up := scriptedResponder(t, cappedSummaryTurn("", "<think>plan</think>Summary text", "stop"))
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedFoldable(a)

	skipped, err := a.Compact(context.Background())

	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if skipped {
		t.Fatal("Compact skipped a foldable conversation; want the fold so a summary was written")
	}
	msgs := a.conv.Messages()
	folded := msgs[len(msgs)-1].Content
	if !strings.Contains(folded, "Summary text") {
		t.Errorf("summary message = %q, want the visible summary kept", folded)
	}
	if strings.Contains(folded, "<think>") || strings.Contains(folded, "plan") {
		t.Errorf("summary message = %q, want the inline thinking stripped out", folded)
	}
}

// TestCompactKeepsASummaryCutAtTheCapAndMarksIt: a summary the server cut off at compactMaxTokens
// still said something, and that something is worth more than the fault it would otherwise raise —
// discarding it burns the fold's tokens for nothing and leaves the conversation as over-budget as
// before. So it folds normally, with the truncation marker appended INSIDE the summary message
// (after context.Compact's own prefix), because a cut summary loses precisely its tail — the recent
// state and the next step — and a model reading it unmarked resumes from the wrong place. A summary
// the server called finished carries no marker.
func TestCompactKeepsASummaryCutAtTheCapAndMarksIt(t *testing.T) {
	t.Parallel()

	if strings.HasSuffix(summaryTruncatedMarker, "\n") {
		t.Errorf("summaryTruncatedMarker = %q, want mustPrompt's single trailing newline stripped", summaryTruncatedMarker)
	}

	cases := []struct {
		name       string
		finish     string
		wantMarker bool
	}{
		{name: "cut at the output cap", finish: "length", wantMarker: true},
		{name: "the server called it finished", finish: "stop"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			up := scriptedResponder(t, cappedSummaryTurn("", "partial summary", tc.finish))
			a, err := newAgent(baseConfig(&recordingSink{}), up)
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			seedFoldable(a)

			skipped, err := a.Compact(context.Background())

			if err != nil {
				t.Fatalf("Compact: %v", err)
			}
			if skipped {
				t.Fatal("Compact skipped a foldable conversation; want the fold so a summary was written")
			}
			msgs := a.conv.Messages()
			if len(msgs) != 2 {
				t.Fatalf("conv = %d messages after the fold, want 2 (the protected prefix, then the summary)", len(msgs))
			}
			sum := msgs[1]
			if sum.Role != domain.RoleAssistant {
				t.Errorf("summary role = %q, want assistant", sum.Role)
			}
			if !strings.HasPrefix(sum.Content, "Summary of the conversation so far:") {
				t.Errorf("summary message = %q, want context.Compact's summary prefix", sum.Content)
			}
			if !strings.Contains(sum.Content, "partial summary") {
				t.Errorf("summary message = %q, want the visible summary kept", sum.Content)
			}
			gotMarker := strings.HasSuffix(sum.Content, "\n\n"+summaryTruncatedMarker)
			if gotMarker != tc.wantMarker {
				t.Errorf("summary ends with the truncation marker = %v, want %v; message = %q", gotMarker, tc.wantMarker, sum.Content)
			}
			if !tc.wantMarker && strings.Contains(sum.Content, "cut off") {
				t.Errorf("summary message = %q, want no truncation marker on a finished reply", sum.Content)
			}
		})
	}
}

// flakySummaryResponder faults the FIRST summary call with a transient in-band error and answers
// every later one with the canned summary — the aggregator that swapped its routed provider out
// partway through a fold. It counts the summary calls so a test can see the re-stream happen.
type flakySummaryResponder struct {
	summary      string
	summaryCalls int
	faults       []provider.Delta // the script for the first summary call
}

func (r *flakySummaryResponder) Stream(_ context.Context, req provider.Request) iter.Seq[provider.Delta] {
	if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "compacting a conversation") {
		r.summaryCalls++
		if r.summaryCalls == 1 {
			return scriptedDeltas(r.faults).Stream(context.Background(), req)
		}
		return streamReply(r.summary)
	}
	return streamReply("done")
}

// TestCompactRestreamsOnceOnATransientSummaryFault pins the fix for the fold's dropped
// Delta.Retryable: a transient in-band fault during the summary call is re-streamed once, so the
// conversation folds on the second summary and the upstream saw two summary requests — a momentary
// 502 no longer fails the fold (and, on the automatic trigger, no longer latches compactFailed for
// the Exchange). Nothing streamed into the transcript, so no StreamResetEvent rides the recovery.
// A NON-transient fault keeps failing the fold on its first appearance, and a second transient
// fault surfaces as every fault always did — the re-stream is spent once.
func TestCompactRestreamsOnceOnATransientSummaryFault(t *testing.T) {
	shortRestreamHoldoff(t)

	sink := &recordingSink{}
	up := &flakySummaryResponder{summary: "FOLDED", faults: retryableErrorScript(transientFaultMsg)}
	a, err := newAgent(baseConfig(sink), up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedFoldable(a)
	before := a.conv.Len()

	skipped, err := a.Compact(context.Background())

	if err != nil {
		t.Fatalf("Compact: %v, want the fold to recover from one transient fault", err)
	}
	if skipped {
		t.Fatal("Compact skipped a foldable conversation; want the fold so a summary was written")
	}
	if up.summaryCalls != 2 {
		t.Errorf("summary calls = %d, want 2 (the faulted stream and its one re-stream)", up.summaryCalls)
	}
	if a.conv.Len() >= before {
		t.Errorf("conv Len = %d, want fewer than %d: the second summary must fold", a.conv.Len(), before)
	}
	if n := countEvents[domain.StreamResetEvent](sink.events); n != 0 {
		t.Errorf("StreamResetEvents = %d, want 0: nothing streamed, so nothing to discard", n)
	}
}

// TestCompactDoesNotRestreamAPlainSummaryFault is the re-stream's boundary: the same first-call
// fault without the transient verdict fails the fold at once, and the upstream saw ONE summary
// request.
func TestCompactDoesNotRestreamAPlainSummaryFault(t *testing.T) {
	shortRestreamHoldoff(t)

	up := &flakySummaryResponder{summary: "FOLDED", faults: []provider.Delta{{Kind: provider.DeltaError, Err: "boom"}}}
	a, err := newAgent(baseConfig(&recordingSink{}), up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedFoldable(a)
	before := a.conv.Len()

	_, err = a.Compact(context.Background())

	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Compact err = %v, want the plain fault surfaced", err)
	}
	if up.summaryCalls != 1 {
		t.Errorf("summary calls = %d, want 1: a plain fault is never re-streamed", up.summaryCalls)
	}
	if a.conv.Len() != before {
		t.Errorf("conv mutated on a faulted compaction: Len = %d, want %d", a.conv.Len(), before)
	}
}

// TestFoldTable drives foldFor per trigger and pins each row of the latch table — the gate column
// (what closes it, and whether a closed gate refuses or declines silently) and the latch columns
// (stand-down, event, saturation, bridge) — so the three wrappers cannot drift back into three
// prose-governed copies.
func TestFoldTable(t *testing.T) {
	type env struct {
		a    *Agent
		sink *recordingSink
		up   *compactSpyResponder
	}
	// over seeds a history far past the 8k window's allocation, so the estimate row's gate opens.
	over := func(t *testing.T, cfg func(*domain.Config)) env {
		t.Helper()
		sink := &recordingSink{}
		up := &compactSpyResponder{reply: "FOLDED-SUMMARY"}
		c := autoCompactConfig(sink)
		if cfg != nil {
			cfg(&c)
		}
		a, err := newAgent(c, up)
		if err != nil {
			t.Fatalf("newAgent: %v", err)
		}
		seedLargeConv(a)
		return env{a, sink, up}
	}
	// midExchange places the agent inside an open Exchange at a quiescent Turn boundary, as a
	// child agent (midExchangeCompaction) sits at every Turn.
	midExchange := func(a *Agent) {
		a.midExchangeCompaction = true
		a.turns.restore(turnSnapshot{inExchange: true, exchangeStart: 1})
	}
	autoOff := func(c *domain.Config) { c.Context.CompactionEnabled = false }

	t.Run("gate column", func(t *testing.T) {
		gates := []struct {
			name    string
			kind    foldKind
			arrange func(env)
			refusal error // the error a closed gate hands the caller; nil = declines silently
		}{
			{"on demand refuses mid-Exchange", foldOnDemand, func(e env) { midExchange(e.a) }, domain.ErrInputPending},
			{"estimate declines while another fold runs", foldEstimate, func(e env) { e.a.compacting = true }, nil},
			{"estimate declines under auto-compact: false", foldEstimate, func(e env) { e.a.SetCompactionEnabled(false) }, nil},
			{"estimate declines on the stand-down latch", foldEstimate, func(e env) { e.a.turns.foldFaulted() }, nil},
			{"overflow declines while another fold runs", foldOverflow, func(e env) { e.a.compacting = true }, nil},
			{"overflow declines under auto-compact: false", foldOverflow, func(e env) { e.a.SetCompactionEnabled(false) }, nil},
		}
		for _, g := range gates {
			t.Run(g.name, func(t *testing.T) {
				e := over(t, nil)
				g.arrange(e)
				r := e.a.foldFor(context.Background(), 0, g.kind)
				if r.end != foldEndDeclined || r.skipped {
					t.Fatalf("result = %+v, want a declined, unskipped fold", r)
				}
				if !errors.Is(r.err, g.refusal) || (g.refusal == nil && r.err != nil) {
					t.Errorf("err = %v, want %v", r.err, g.refusal)
				}
				if e.up.summaryCalls != 0 {
					t.Errorf("summarizer calls = %d, want 0 (the gate precedes the wire)", e.up.summaryCalls)
				}
				if n := countCompactionErrors(e.sink.events); n != 0 {
					t.Errorf("compaction ErrorEvents = %d, want 0 (a closed gate is silent)", n)
				}
			})
		}
		t.Run("on demand ignores auto-compact: false and both latches", func(t *testing.T) {
			e := over(t, autoOff)
			e.a.turns.foldFaulted()
			e.a.turns.foldSaturated()
			if r := e.a.foldFor(context.Background(), 0, foldOnDemand); r.end != foldEndFolded || r.err != nil {
				t.Fatalf("result = %+v, want a fold that ran", r)
			}
			if e.up.summaryCalls != 1 {
				t.Errorf("summarizer calls = %d, want 1", e.up.summaryCalls)
			}
		})
		t.Run("overflow ignores both latches", func(t *testing.T) {
			e := over(t, nil)
			e.a.turns.foldFaulted()
			e.a.turns.foldSaturated()
			if r := e.a.foldFor(context.Background(), 0, foldOverflow); r.end != foldEndFolded {
				t.Fatalf("result = %+v, want a fold that ran", r)
			}
		})
	})

	t.Run("fault row", func(t *testing.T) {
		faults := []struct {
			name       string
			kind       foldKind
			inExchange bool
			latched    bool // compactFailed after the fault
			events     int  // compaction ErrorEvents
			standsDown bool // the event carries foldStandDownSuffix
		}{
			{"on demand: no latch, no event, the caller gets the error", foldOnDemand, false, false, 0, false},
			{"estimate at an opening: latch + one event", foldEstimate, false, true, 1, false},
			{"estimate mid-Exchange: latch + one event that says so", foldEstimate, true, true, 1, true},
			{"overflow: one event, no latch", foldOverflow, true, false, 1, false},
		}
		for _, f := range faults {
			t.Run(f.name, func(t *testing.T) {
				sink := &recordingSink{}
				a, err := newAgent(autoCompactConfig(sink), overflowResponder(t)) // every summary call faults
				if err != nil {
					t.Fatalf("newAgent: %v", err)
				}
				seedLargeConv(a)
				if f.inExchange {
					midExchange(a)
				}
				before := a.conv.Len()
				r := a.foldFor(context.Background(), 0, f.kind)
				if r.end != foldEndFaulted || r.err == nil || r.skipped {
					t.Fatalf("result = %+v, want faulted with the error carried", r)
				}
				if a.conv.Len() != before {
					t.Errorf("conv.Len() = %d, want %d (a faulted fold leaves history untouched)", a.conv.Len(), before)
				}
				if a.turns.compactFailed != f.latched {
					t.Errorf("compactFailed = %v, want %v", a.turns.compactFailed, f.latched)
				}
				texts := compactionErrorTexts(sink.events)
				if len(texts) != f.events {
					t.Fatalf("compaction ErrorEvents = %v, want %d", texts, f.events)
				}
				if f.events == 1 && strings.HasSuffix(texts[0], foldStandDownSuffix) != f.standsDown {
					t.Errorf("event %q: stand-down suffix present = %v, want %v", texts[0], !f.standsDown, f.standsDown)
				}
			})
		}
	})

	t.Run("bridge column", func(t *testing.T) {
		bridges := []struct {
			name       string
			kind       foldKind
			inExchange bool
			bridged    bool
		}{
			{"on demand: no bridge", foldOnDemand, false, false},
			{"estimate at an opening: no bridge", foldEstimate, false, false},
			{"estimate mid-Exchange: bridge", foldEstimate, true, true},
			{"overflow at an opening: bridge", foldOverflow, false, true},
			{"overflow mid-Exchange: bridge", foldOverflow, true, true},
		}
		for _, b := range bridges {
			t.Run(b.name, func(t *testing.T) {
				e := over(t, nil)
				if b.inExchange {
					midExchange(e.a)
				}
				if r := e.a.foldFor(context.Background(), 0, b.kind); r.end != foldEndFolded || r.err != nil || r.skipped {
					t.Fatalf("result = %+v, want a fold that ran", r)
				}
				last := e.a.conv.Messages()[e.a.conv.Len()-1]
				if got := last.Role == domain.RoleUser && last.Content == overflowBridge; got != b.bridged {
					t.Errorf("ends on the bridge = %v, want %v (roles %s)", got, b.bridged, convRoles(e.a))
				}
				if b.bridged && b.inExchange && e.a.turns.exchangeStart != e.a.conv.Len()-1 {
					t.Errorf("exchangeStart = %d, want %d (re-anchored at the bridge)", e.a.turns.exchangeStart, e.a.conv.Len()-1)
				}
				if n := countCompactionErrors(e.sink.events); n != 0 {
					t.Errorf("compaction ErrorEvents = %d, want 0 (a fold that ran is quiet)", n)
				}
			})
		}
	})

	t.Run("saturation column", func(t *testing.T) {
		// A protected prefix (the first user message) larger than the whole allocation: the fold
		// runs and the history is STILL over it.
		sat := func(a *Agent) {
			a.conv.Append(domain.Message{Role: domain.RoleUser, Content: strings.Repeat("goal ", 8000)})
			a.conv.Append(domain.Message{Role: domain.RoleAssistant, Content: "on it"})
			a.conv.Append(domain.Message{Role: domain.RoleUser, Content: "more"})
			a.conv.Append(domain.Message{Role: domain.RoleAssistant, Content: "done"})
		}
		for _, s := range []struct {
			name      string
			kind      foldKind
			saturates bool
		}{
			{"estimate latches with one event", foldEstimate, true},
			{"on demand never saturates", foldOnDemand, false},
			{"overflow never saturates", foldOverflow, false},
		} {
			t.Run(s.name, func(t *testing.T) {
				sink := &recordingSink{}
				a, err := newAgent(autoCompactConfig(sink), &compactSpyResponder{reply: "FOLDED-SUMMARY"})
				if err != nil {
					t.Fatalf("newAgent: %v", err)
				}
				sat(a)
				if r := a.foldFor(context.Background(), 0, s.kind); r.end != foldEndFolded {
					t.Fatalf("result = %+v, want a fold that ran", r)
				}
				if a.turns.compactSat != s.saturates {
					t.Errorf("compactSat = %v, want %v", a.turns.compactSat, s.saturates)
				}
				want := 0
				if s.saturates {
					want = 1
				}
				if n := countCompactionErrors(sink.events); n != want {
					t.Errorf("compaction ErrorEvents = %d, want %d", n, want)
				}
			})
		}
	})

	t.Run("cancel and skip end silently on every row", func(t *testing.T) {
		for _, kind := range []foldKind{foldOnDemand, foldEstimate, foldOverflow} {
			t.Run(fmt.Sprintf("kind %d cancelled", kind), func(t *testing.T) {
				e := over(t, nil)
				before := e.a.conv.Len()
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				r := e.a.foldFor(ctx, 0, kind)
				if r.end != foldEndCancelled || !errors.Is(r.err, context.Canceled) || r.skipped {
					t.Fatalf("result = %+v, want cancelled carrying ctx.Err()", r)
				}
				if e.a.conv.Len() != before || e.a.turns.compactFailed || countCompactionErrors(e.sink.events) != 0 {
					t.Errorf("a cancel must leave the conversation, the latch and the event stream untouched: len %d→%d, latched %v, events %d",
						before, e.a.conv.Len(), e.a.turns.compactFailed, countCompactionErrors(e.sink.events))
				}
			})
			t.Run(fmt.Sprintf("kind %d skipped", kind), func(t *testing.T) {
				sink := &recordingSink{}
				up := &compactSpyResponder{reply: "UNREACHED"}
				a, err := newAgent(autoCompactConfig(sink), up)
				if err != nil {
					t.Fatalf("newAgent: %v", err)
				}
				a.conv.Append(domain.Message{Role: domain.RoleUser, Content: strings.Repeat("x", 40000)}) // over budget, nothing past the prefix
				r := a.foldFor(context.Background(), 0, kind)
				if r.end != foldEndDeclined || !r.skipped || r.err != nil {
					t.Fatalf("result = %+v, want declined + skipped", r)
				}
				if up.summaryCalls != 0 || a.turns.compactSat || countCompactionErrors(sink.events) != 0 {
					t.Errorf("a skip proves nothing: calls %d, saturated %v, events %d", up.summaryCalls, a.turns.compactSat, countCompactionErrors(sink.events))
				}
			})
		}
	})
}
