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
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// overflowResponder is the fake for the "prompt too long" path: it answers every stream with a
// single terminal DeltaContextOverflow, the 400 the server sends when the request itself exceeds
// the context window — unconditionally, regardless of prompt size. It stands for the unbudgetable
// case: a server that rejects even a minimal prompt, where no transcript budget can help (item 6's
// window-derived one or the unknown-window default), so the fault must still surface cleanly and
// leave the conversation untouched.
type overflowResponder struct{}

func (overflowResponder) Stream(context.Context, provider.Request) iter.Seq[provider.Delta] {
	return func(yield func(provider.Delta) bool) {
		yield(provider.Delta{Kind: provider.DeltaContextOverflow, Err: "apogee: context window exceeded"})
	}
}

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
	a, err := newAgent(baseConfig(&recordingSink{}), overflowResponder{})
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
			a, err := newAgent(cfg, echoResponder{reply: "unused"})
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
	a, err := newAgent(baseConfig(sink), echoResponder{reply: "reply"})
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
	up := &recordingResponder{reply: "FOLDED-SUMMARY"}
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
	body := up.last.Messages[len(up.last.Messages)-1].Content
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
	b, err := resumeAgent(baseConfig(&recordingSink{}), snap, echoResponder{reply: "resumed reply"})
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

	up := &recordingResponder{reply: "FOLDED-SUMMARY"}
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

	got := up.last
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

// cappedSummaryResponder scripts one summary stream by its three observable parts: the reasoning
// channel the server splits out (reasoning_content), the visible content, and the finish reason it
// ends on. It is the fake for the 2026-08-29 incident — a thinking model that spends the whole
// compactMaxTokens cap reasoning and ends on "length" with nothing visible — and, with content and
// no separate channel, for the inline-<think> shape a delimited profile emits.
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
		up        cappedSummaryResponder
		capped    bool
		wantSpend bool
	}{
		{
			name:      "reasoning-only reply cut at the cap",
			up:        cappedSummaryResponder{thinking: reasoning, finish: "length"},
			capped:    true,
			wantSpend: true,
		},
		{
			name:   "reply cut at the cap with no reasoning channel",
			up:     cappedSummaryResponder{finish: "length"},
			capped: true,
		},
		{
			name: "blank reply the server called finished",
			up:   cappedSummaryResponder{finish: "stop"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			a, err := newAgent(baseConfig(&recordingSink{}), tc.up)
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
					"show for it%s — the summarizer asked for no reasoning; this server's template did not honour that",
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
	up := cappedSummaryResponder{content: "<think>plan</think>Summary text", finish: "stop"}
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

			up := cappedSummaryResponder{content: "partial summary", finish: tc.finish}
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
