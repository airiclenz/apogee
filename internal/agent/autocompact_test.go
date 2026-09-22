package agent

// The automatic, budget-driven Compaction trigger (Phase-4 item 9). autoCompact folds the
// conversation at a quiescent boundary — before a Turn's request is built — when the history has
// outgrown its Budget allocation, using the same generative Compact the /compact command drives. It
// is structural (on by default, on under Bypass) with a file-only `auto-compact: false` opt-out
// (Config.Context.CompactionEnabled), and the on-demand /compact is unaffected by that gate.

import (
	"context"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/stubllm"
)

// summaryInstructionMark is the leading substring of internal/context's unexported
// summaryInstruction — the stable half of it — by which the summarizer's call is told from a
// Turn's request: it opens the summarizer's own system message and appears in no other.
const summaryInstructionMark = "compacting a conversation"

// isSummaryLogged reports whether a logged request is the compaction summarizer's call rather
// than a Turn's request, the stubllm half of isSummaryRequest.
func isSummaryLogged(r stubllm.Request) bool {
	return len(r.Messages) > 0 && strings.Contains(r.Messages[0].Content, summaryInstructionMark)
}

// summaryTurn is the turn that answers every summarizer call with text, selected by the
// summarizer's system message so it never spends an ordered turn — a test can drive a real
// multi-Turn Exchange (tool calls and all) and still see exactly when a fold fired.
func summaryTurn(text string) stubllm.Turn {
	return stubllm.Turn{When: &stubllm.Match{System: summaryInstructionMark}, Text: text, Repeat: true}
}

// compactSpyResponder echoes reply to every request, summarizer calls included, so a test can
// count auto-folds through summaryCalls and read what the main request carried afterward
// through last.
func compactSpyResponder(t testing.TB, reply string) *scriptedUpstream {
	t.Helper()
	return echoResponder(t, reply)
}

// summaryCalls is how many of the upstream's requests were the summarizer's.
func (u *scriptedUpstream) summaryCalls() int {
	n := 0
	for _, r := range u.server.Requests() {
		if isSummaryLogged(r) {
			n++
		}
	}
	return n
}

// mains is every MAIN-turn request, in order — the log with the summarizer's calls left out,
// what the model actually saw.
func (u *scriptedUpstream) mains() []stubllm.Request {
	var out []stubllm.Request
	for _, r := range u.server.Requests() {
		if !isSummaryLogged(r) {
			out = append(out, r)
		}
	}
	return out
}

// lastSummary is the most recent summarizer request, or the zero Request when none was made.
func (u *scriptedUpstream) lastSummary() stubllm.Request {
	requests := u.server.Requests()
	for i := len(requests) - 1; i >= 0; i-- {
		if isSummaryLogged(requests[i]) {
			return requests[i]
		}
	}
	return stubllm.Request{}
}

// autoCompactConfig is baseConfig with a discovered window and the automatic trigger enabled — the
// on-by-default CLI posture, set explicitly here because a bare domain.Config zero-values it off.
func autoCompactConfig(sink domain.EventSink) domain.Config {
	cfg := baseConfig(sink)
	cfg.Context.MaxContextTokens = 8192
	cfg.Context.CompactionEnabled = true
	return cfg
}

// TestAutoCompactFoldsWhenHistoryOverBudget drives a Turn whose pre-existing history is far past its
// Budget allocation: the loop folds it (one summarizer call) before building the request, so the
// model sees a handful of messages, and the just-submitted user message survives the fold as its own
// turn rather than being folded into the summary.
func TestAutoCompactFoldsWhenHistoryOverBudget(t *testing.T) {
	sink := &recordingSink{}
	up := compactSpyResponder(t, "FOLDED-SUMMARY")
	a, err := newAgent(autoCompactConfig(sink), up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedLargeConv(a) // ~42k chars, well over the ~3.6k-token History allocation for an 8k window
	seeded := a.conv.Len()

	if err := a.Submit(domain.UserInput{Text: "the fresh question"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	if up.summaryCalls() != 1 {
		t.Fatalf("summarizer calls = %d, want exactly 1 auto-fold", up.summaryCalls())
	}
	if n := len(up.last().Messages); n > 5 {
		t.Errorf("main request carried %d messages (seeded %d); auto-compaction did not fold", n, seeded)
	}
	last := up.last().Messages[len(up.last().Messages)-1]
	if last.Role != string(domain.RoleUser) || !strings.Contains(last.Content, "the fresh question") {
		t.Errorf("fresh user message not preserved as its own turn: %+v", last)
	}
	if hasEvent[domain.ErrorEvent](sink.events) {
		t.Error("a successful auto-fold emitted an ErrorEvent")
	}
}

// TestAutoCompactNotBelowThreshold pins that a small (foldable but in-budget) history is NOT folded:
// the trigger fires at the threshold, not before, so no summarizer call runs and the model sees the
// original messages.
func TestAutoCompactNotBelowThreshold(t *testing.T) {
	up := compactSpyResponder(t, "reply")
	a, err := newAgent(autoCompactConfig(&recordingSink{}), up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedFoldable(a) // 4 short messages — tiny against the budget

	if err := a.Submit(domain.UserInput{Text: "next"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	if up.summaryCalls() != 0 {
		t.Fatalf("summarizer calls = %d, want 0 below the threshold", up.summaryCalls())
	}
	if up.last().Messages[0].Content != "task one" {
		t.Errorf("history was altered below the threshold: first message = %q", up.last().Messages[0].Content)
	}
}

// TestAutoCompactOptOutRespected pins the `auto-compact: false` opt-out: with CompactionEnabled off,
// an over-budget history is sent whole — no auto-fold — even though the window is known.
func TestAutoCompactOptOutRespected(t *testing.T) {
	up := compactSpyResponder(t, "reply")
	cfg := autoCompactConfig(&recordingSink{})
	cfg.Context.CompactionEnabled = false
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedLargeConv(a)

	if err := a.Submit(domain.UserInput{Text: "next"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	if up.summaryCalls() != 0 {
		t.Fatalf("summarizer calls = %d with auto-compact off; want 0", up.summaryCalls())
	}
	if n := len(up.last().Messages); n < 50 {
		t.Errorf("history was folded despite the opt-out: request carried only %d messages", n)
	}
}

// TestOnDemandCompactIgnoresAutoGate pins that the /compact command folds regardless of the
// `auto-compact` opt-out: CompactionEnabled is the AUTOMATIC trigger's gate, not a switch on the
// reducer itself.
func TestOnDemandCompactIgnoresAutoGate(t *testing.T) {
	cfg := autoCompactConfig(&recordingSink{})
	cfg.Context.CompactionEnabled = false // auto-compaction off …
	a, err := newAgent(cfg, echoResponder(t, "ON-DEMAND-SUMMARY"))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedFoldable(a)
	before := a.conv.Len()

	skipped, err := a.Compact(context.Background()) // … but /compact still folds on request
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if skipped {
		t.Fatal("on-demand Compact skipped a foldable conversation despite auto-compact being off")
	}
	if a.conv.Len() >= before {
		t.Errorf("on-demand Compact did not fold: Len = %d, want < %d", a.conv.Len(), before)
	}
}

// TestAutoCompactRunsOnceThenStable pins the non-reentrant / once-per-boundary property: an
// over-budget history folds on the first Turn, and the resulting small history does NOT re-fold on
// the next Turn — one summarizer call across both.
func TestAutoCompactRunsOnceThenStable(t *testing.T) {
	up := compactSpyResponder(t, "FOLDED")
	a, err := newAgent(autoCompactConfig(&recordingSink{}), up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedLargeConv(a)

	for _, text := range []string{"first", "second"} {
		if err := a.Submit(domain.UserInput{Text: text}); err != nil {
			t.Fatalf("Submit(%q): %v", text, err)
		}
		if _, err := a.Step(context.Background()); err != nil {
			t.Fatalf("Step(%q): %v", text, err)
		}
	}

	if up.summaryCalls() != 1 {
		t.Errorf("summarizer calls = %d across two Turns, want exactly 1 (folded once, then stable)", up.summaryCalls())
	}
}
