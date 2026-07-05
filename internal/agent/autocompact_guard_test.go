package agent

// Second-review fixes for the automatic Compaction trigger (phase-4-second-review-fixes item 2,
// design record S2). Three properties beyond the four baseline autocompact tests: (a) the trigger
// is Exchange-boundary-only — a mid-Exchange over-budget Turn defers the fold to the next Exchange
// opening; (b) a fold that cannot bring the history under its allocation (an oversized protected
// prefix) saturates — exactly one ErrorEvent, then it stands down until the estimate drops under the
// allocation, then re-arms; (c) exchangeStart is repaired after a mid-Exchange history rewrite so
// AbortExchange still rolls back exactly to the Exchange boundary (no orphaned tool results).

import (
	"context"
	"iter"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/provider"
)

// scriptedCompactResponder plays a scripted stream for each MAIN-turn call while intercepting the
// summarizer call (identified by the summary system prompt) to count auto-folds and return a canned
// summary — so a test can drive a real multi-Turn Exchange (tool calls and all) and still assert
// exactly when an auto-fold fired, without a summarizer call consuming a main-turn script slot.
type scriptedCompactResponder struct {
	scripts      [][]provider.Delta
	summaryReply string
	summaryCalls int
	calls        int
}

func (r *scriptedCompactResponder) Stream(_ context.Context, req provider.Request) iter.Seq[provider.Delta] {
	if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "compacting a conversation") {
		r.summaryCalls++
		return streamReply(r.summaryReply)
	}
	i := r.calls
	r.calls++
	return func(yield func(provider.Delta) bool) {
		if i >= len(r.scripts) {
			yield(provider.Delta{Kind: provider.DeltaError, Err: "scriptedCompactResponder: out of scripts"})
			return
		}
		for _, d := range r.scripts[i] {
			if !yield(d) {
				return
			}
		}
	}
}

// countCompactionErrors counts the ErrorEvents attributed to the "compaction" source — the
// saturation notice's fingerprint.
func countCompactionErrors(events []domain.Event) int {
	n := 0
	for _, e := range events {
		if ee, ok := e.(domain.ErrorEvent); ok && ee.Source == "compaction" {
			n++
		}
	}
	return n
}

// TestAutoCompactSkipsMidExchangeThenFoldsAtNextOpening drives an Exchange whose history crosses the
// Budget threshold mid-flight (a large tool result), so the continuation Turn is over budget while
// inExchange: the fold must NOT fire there (tool_result_cap is the mid-Exchange relief valve, S2), and
// must instead fire at the next Exchange opening where the same over-budget history is folded.
func TestAutoCompactSkipsMidExchangeThenFoldsAtNextOpening(t *testing.T) {
	sink := &recordingSink{}
	up := &scriptedCompactResponder{
		summaryReply: "FOLDED",
		scripts: [][]provider.Delta{
			toolCallScript("c1", "probe", "{}"), // Turn 0 (opening): ask for the tool
			contentScript("continued"),          // Turn 1 (continuation): finish the Exchange
			contentScript("next answer"),        // Turn 2 (the next Exchange, after the deferred fold)
		},
	}
	cfg := autoCompactConfig(sink)
	toolReg := domain.NewToolRegistry()
	// The tool result alone (~25k chars ≈ 6.2k tokens) exceeds the ~3.9k-token History allocation for
	// the 8k window, so committing it mid-Exchange pushes the history over budget.
	if err := toolReg.Register(fakeTool{name: "probe", readOnly: true, result: strings.Repeat("x", 25000)}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	cfg.Tools = toolReg
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	if err := a.Submit(domain.UserInput{Text: "start"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res0, err := a.Step(context.Background()) // opening Turn: under budget at the top → no fold
	if err != nil {
		t.Fatalf("Step 0: %v", err)
	}
	if res0.Status != domain.StatusTurnComplete {
		t.Fatalf("Turn 0 status = %q, want %q (a tool Turn)", res0.Status, domain.StatusTurnComplete)
	}
	if up.summaryCalls != 0 {
		t.Fatalf("a fold fired on the opening Turn before the history was over budget: %d", up.summaryCalls)
	}
	if !a.historyExceedsAllocation() {
		t.Fatalf("setup: history is not over budget after the large tool result; the guard would be untested")
	}

	res1, err := a.Step(context.Background()) // continuation Turn: over budget AND inExchange → guard defers
	if err != nil {
		t.Fatalf("Step 1: %v", err)
	}
	if res1.Status != domain.StatusExchangeComplete {
		t.Fatalf("Turn 1 status = %q, want %q", res1.Status, domain.StatusExchangeComplete)
	}
	if up.summaryCalls != 0 {
		t.Fatalf("auto-compaction folded mid-Exchange (%d summarizer calls); the inExchange guard must defer it", up.summaryCalls)
	}

	// The next Exchange opening: inExchange is false at the top of step(), so the deferred fold fires.
	if err := a.Submit(domain.UserInput{Text: "again"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step 2: %v", err)
	}
	if up.summaryCalls != 1 {
		t.Fatalf("deferred fold did not fire at the next Exchange opening: summarizer calls = %d, want 1", up.summaryCalls)
	}
}

// TestAutoCompactSaturatesWhenPrefixExceedsAllocation drives the thrash-guard: a first user message
// (the protected prefix) that alone exceeds the History allocation means every fold keeps an
// over-budget prefix, so the fold cannot help. The trigger must fold exactly once, emit exactly one
// compaction ErrorEvent, then stand down (no re-fold, no further ErrorEvent) even as the history
// grows; and the saturation must clear once the estimate drops back under the allocation (a larger
// window), re-arming so a later overflow folds again.
func TestAutoCompactSaturatesWhenPrefixExceedsAllocation(t *testing.T) {
	sink := &recordingSink{}
	up := &compactSpyResponder{reply: "SUMMARY"}
	a, err := newAgent(autoCompactConfig(sink), up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	// Oversized protected prefix (~25k chars ≈ 6.2k tokens > the ~3.9k-token History allocation) plus
	// a foldable tail: every fold keeps the prefix and stays over budget.
	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: strings.Repeat("g", 25000)})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, Content: "a1"})
	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: "u1"})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, Content: "a2"})

	// Exchange 1: one fold attempt, but the oversized prefix keeps it over budget → saturate.
	runExchange(t, a, "q1")
	if up.summaryCalls != 1 {
		t.Fatalf("first over-budget opening did not fold once: summarizer calls = %d, want 1", up.summaryCalls)
	}
	if n := countCompactionErrors(sink.events); n != 1 {
		t.Fatalf("saturating fold emitted %d compaction ErrorEvents, want exactly 1", n)
	}

	// Exchange 2: still over budget, but saturated → no further fold, no further ErrorEvent.
	runExchange(t, a, "q2")
	if up.summaryCalls != 1 {
		t.Fatalf("saturated trigger re-folded on growth: summarizer calls = %d, want 1", up.summaryCalls)
	}
	if n := countCompactionErrors(sink.events); n != 1 {
		t.Fatalf("saturated trigger emitted another ErrorEvent: %d, want 1", n)
	}

	// A larger window drops the estimate under the allocation → saturation clears. Exchange 3 does not
	// fold (now in budget), and the latch is rearmed for a future overflow.
	a.cfg.Context.MaxContextTokens = 1 << 20
	runExchange(t, a, "q3")
	if up.summaryCalls != 1 {
		t.Fatalf("in-budget Exchange folded: summarizer calls = %d, want 1", up.summaryCalls)
	}

	// Shrinking the window back over budget re-arms the trigger → it folds again and re-saturates,
	// proving the latch cleared rather than sticking off permanently.
	a.cfg.Context.MaxContextTokens = 8192
	runExchange(t, a, "q4")
	if up.summaryCalls != 2 {
		t.Fatalf("saturation did not clear: summarizer calls = %d, want 2 after the window shrank back over budget", up.summaryCalls)
	}
	if n := countCompactionErrors(sink.events); n != 2 {
		t.Fatalf("re-saturating fold did not emit a fresh ErrorEvent: %d compaction ErrorEvents, want 2", n)
	}
}

// TestAutoCompactSkippedFoldDoesNotSaturate drives F1 (phase-4-third-review-fixes item 1): an
// over-budget history with only one message past the protected prefix makes Compact SKIP (nothing
// worth folding), and a skip must NOT latch the saturation trigger — folding nothing proves nothing.
// Proof: no ErrorEvent, no summarizer call, and the latch stays clear; then, once a foldable
// multi-message tail has accumulated at a later opening, the fold RUNS (summarizer called) — which it
// could not if the earlier skip had wrongly saturated (a latched trigger stands down entirely).
func TestAutoCompactSkippedFoldDoesNotSaturate(t *testing.T) {
	sink := &recordingSink{}
	up := &compactSpyResponder{reply: "SUMMARY"}
	a, err := newAgent(autoCompactConfig(sink), up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	// Protected prefix (the first user message) + one oversized assistant answer: the ~25k-char answer
	// (~6.2k tokens) pushes the ~2-message history well past the ~3.9k-token allocation, yet only ONE
	// message sits past the prefix, so Compact skips (minCompactTail = 2).
	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: "the overarching goal"})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, Content: strings.Repeat("x", 25000)})
	if !a.historyExceedsAllocation() {
		t.Fatal("setup: history is not over budget; the skip-vs-latch distinction would be untested")
	}

	// Exchange 1: over budget at the opening, but the fold skips (too short a tail) → no latch, no
	// ErrorEvent, no summarizer call. The just-submitted user message and its reply then accumulate a
	// foldable tail for the next opening.
	runExchange(t, a, "q1")
	if up.summaryCalls != 0 {
		t.Fatalf("a skipped fold still called the summarizer: %d, want 0", up.summaryCalls)
	}
	if n := countCompactionErrors(sink.events); n != 0 {
		t.Fatalf("a skipped fold emitted %d compaction ErrorEvents, want 0 (nothing folded ⇒ nothing proved)", n)
	}
	if a.compactSat {
		t.Fatal("a skipped fold latched the saturation trigger; the skip must not saturate")
	}

	// Exchange 2: the history now has a foldable multi-message tail (prefix + prior answer + q1 + its
	// reply), so the fold RUNS. If the earlier skip had saturated, shouldAutoCompact would stand the
	// trigger down and this fold would never fire — so the summarizer call proves the latch stayed clear.
	runExchange(t, a, "q2")
	if up.summaryCalls != 1 {
		t.Fatalf("the fold did not run once a foldable tail existed: summarizer calls = %d, want 1", up.summaryCalls)
	}
	if a.historyExceedsAllocation() {
		t.Error("the fold ran but did not bring the history under its allocation; the setup drifted")
	}
}

// TestExchangeStartRepairedAfterMidExchangeTruncation drives the exchangeStart repair (S2c): a
// mid-Exchange truncate_history fold drops the middle of the conversation — including this Exchange's
// initiating user message — so the stale exchangeStart would leave AbortExchange dropping the wrong
// range (orphaning this Exchange's tool results). With the repair, exchangeStart re-anchors just past
// the gap note and AbortExchange rolls the conversation back to exactly prefix + gap note.
func TestExchangeStartRepairedAfterMidExchangeTruncation(t *testing.T) {
	sink := &recordingSink{}
	reg := domain.NewMechanismRegistry()
	m, err := mechanisms.Build(domain.MechanismID("truncate_history"), mechanisms.Deps{})
	if err != nil {
		t.Fatalf("Build(truncate_history): %v", err)
	}
	mustAddMech(t, reg, m)

	toolReg := domain.NewToolRegistry()
	if err := toolReg.Register(fakeTool{name: "probe", readOnly: true, result: "ok"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	cfg := baseConfig(sink)
	cfg.Mechanisms = reg
	cfg.Tools = toolReg
	// One main model call this Turn (the tool call keeps the Exchange open); compaction is off, so no
	// summarizer call — a single script suffices.
	a, err := newAgent(cfg, &scriptedResponder{scripts: [][]provider.Delta{toolCallScript("c9", "probe", "{}")}})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	// A mid-Exchange continuation boundary: index 0 is the protected prefix; indices 1–5 are prior
	// history; index 6 opens THIS Exchange (exchangeStart = 6), followed by four tool Turns. Seven
	// assistant boundaries mean truncate_history (keepLastTurns = 4) will cut back to the 4th-from-last
	// boundary — dropping the prior history AND this Exchange's initiating user message.
	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: "OVERARCHING GOAL"})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, Content: "prior 1"})
	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: "prior u1"})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, Content: "prior 2"})
	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: "prior u2"})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, Content: "prior 3"})
	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: "PENDING QUESTION"})
	for i, id := range []string{"t1", "t2", "t3", "t4"} {
		a.conv.Append(domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: id, Tool: "probe"}}})
		a.conv.Append(domain.Message{Role: domain.RoleTool, ToolCallID: id, Content: "result " + string(rune('a'+i))})
	}
	a.inExchange = true
	a.exchangeStart = 6 // where PENDING QUESTION was appended — the un-repaired opening value

	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if res.Status != domain.StatusTurnComplete {
		t.Fatalf("Turn status = %q, want %q (a tool Turn keeps the Exchange open)", res.Status, domain.StatusTurnComplete)
	}
	// truncate_history fired mid-Exchange (it dropped the middle and inserted the gap note).
	if !hasEvent[domain.MechanismFiredEvent](sink.events) {
		t.Fatal("truncate_history did not fire; the repair path is untested")
	}

	a.AbortExchange()

	if a.conv.Len() != 2 {
		t.Fatalf("after abort conv.Len() = %d, want 2 (prefix + gap note); a stale exchangeStart over-/under-dropped", a.conv.Len())
	}
	if got := a.conv.At(0); got.Content != "OVERARCHING GOAL" {
		t.Errorf("protected prefix not preserved: %+v", got)
	}
	gap := a.conv.At(1)
	if gap.Role != domain.RoleUser || !strings.Contains(gap.Content, "omitted to keep the context window") {
		t.Errorf("conversation does not end at the gap note: %+v", gap)
	}
	for i := 0; i < a.conv.Len(); i++ {
		if a.conv.At(i).Role == domain.RoleTool {
			t.Errorf("message %d is an orphaned tool result after abort: %+v", i, a.conv.At(i))
		}
	}
}
