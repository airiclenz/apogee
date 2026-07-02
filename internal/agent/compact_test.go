package agent

// The /compact failure/cancel spine (post-v1.0.0 remediation item 3). The happy path lives in
// minilang_test.go (TestCompactSummarizesAndReplacesHistoryKeepingPrefix); this file exercises
// the fault side — precisely where the truthfulness fixes (plan item 2a/2b) live and where
// /compact runs most (on-demand compaction fires when the upstream is likeliest to fault, at
// high context fill). Every fault must leave the conversation untouched so a failed /compact
// never corrupts history, and compaction must be SILENT (no TokenEvent/UsageEvent).

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// overflowResponder is the fake for the "prompt too long" path: it answers every stream with a
// single terminal DeltaContextOverflow, the 400 the server sends when the request itself
// exceeds the context window. This is the deterministic failure item 6 will later make
// survivable (a budgeted-tail fallback); today it must surface as a clean error.
type overflowResponder struct{}

func (overflowResponder) Stream(context.Context, provider.Request) iter.Seq[provider.Delta] {
	return func(yield func(provider.Delta) bool) {
		yield(provider.Delta{Kind: provider.DeltaContextOverflow, Err: "apogee: context window exceeded"})
	}
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

// TestCompactContextOverflowErrorsAndLeavesConvUntouched pins the overflow fault: the summary
// call itself overflows, so Compact surfaces the error, reports skipped=false (a fault is not a
// skip), and leaves the conversation untouched. Item 6 will flip this from "errors cleanly" to
// "succeeds via fallback"; until then this is the deterministic-failure guard.
func TestCompactContextOverflowErrorsAndLeavesConvUntouched(t *testing.T) {
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

// TestCompactEmitsNoTokenOrUsageEvents pins the silence contract: compaction is a maintenance
// call, not a Turn, so it must not stream into the transcript (TokenEvent) or move the live
// context gauge (UsageEvent). A real exchange first (which does emit events) proves the sink is
// wired; the events it produced are dropped so only compaction's emissions are asserted on.
func TestCompactEmitsNoTokenOrUsageEvents(t *testing.T) {
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
		t.Error("compaction emitted a UsageEvent; it must not move the live context gauge")
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
