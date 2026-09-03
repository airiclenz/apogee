package agent

// Second-review fixes for the automatic Compaction trigger (phase-4-second-review-fixes item 2,
// design record S2), plus the failed-fold stand-down (plan 2026-08-30 - 00 item 3). Four properties
// beyond the four baseline autocompact tests: (a) the trigger
// is Exchange-boundary-only — a mid-Exchange over-budget Turn defers the fold to the next Exchange
// opening — and its twin, that an Agent which compacts mid-Exchange (midExchangeCompaction, what
// every child agent carries) folds on that same Turn instead; (b) a fold that cannot bring the
// history under its allocation (an oversized protected prefix) saturates — exactly one ErrorEvent,
// then it stands down until the estimate drops under the allocation, then re-arms; (c)
// exchangeStart is repaired after a mid-Exchange history rewrite — and after a mid-Exchange fold —
// so AbortExchange still rolls back exactly to the Exchange boundary (no orphaned tool results, no
// over-drop into the protected prefix); (d) a fold that FAULTS stands the trigger down for the rest
// of the Exchange — one ErrorEvent naming the stand-down, then silence — while openExchange re-arms
// the main agent and the emergency fold and /compact keep their own shot.

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
	requests     []provider.Request // every MAIN-turn request, in order — what the model actually saw
}

func (r *scriptedCompactResponder) Stream(_ context.Context, req provider.Request) iter.Seq[provider.Delta] {
	if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "compacting a conversation") {
		r.summaryCalls++
		return streamReply(r.summaryReply)
	}
	r.requests = append(r.requests, req)
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
	// Only a MID-Exchange fold owes the conversation an overflow bridge. This one ran at an
	// Exchange opening, where the human's own "again" follows the summary as its own turn, so a
	// bridge here would put an invented user message in front of theirs.
	for i := 0; i < a.conv.Len(); i++ {
		if a.conv.At(i).Content == overflowBridge {
			t.Errorf("the Exchange-boundary fold appended an overflow bridge at message %d (roles: %s); only a mid-Exchange fold appends one",
				i, convRoles(a))
		}
	}
	if last := up.requests[len(up.requests)-1].Messages; last[len(last)-1].Content != "again" {
		t.Errorf("the post-fold request ends %q, want the user's own %q", last[len(last)-1].Content, "again")
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
	a.turns.inExchange = true
	a.turns.exchangeStart = 6 // where PENDING QUESTION was appended — the un-repaired opening value

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

// TestAutoCompactFoldsMidExchangeOnAnAgentThatCompactsMidExchange is the twin of the guard above:
// the SAME over-budget continuation Turn the main agent defers is folded on the spot by an Agent
// carrying midExchangeCompaction — what newChildAgent sets on every delegate, whose whole life is
// one Exchange and for whom the deferred-to opening never comes. It pins the fold firing exactly
// once at the quiescent Turn boundary, the saturation latch staying clear (the fold DID bring the
// history back under its allocation), and the cached Exchange boundary being repaired to the folded
// conversation — without that repair the stale boundary sits BELOW the protected prefix and
// AbortExchange eats the first user message along with the summary.
func TestAutoCompactFoldsMidExchangeOnAnAgentThatCompactsMidExchange(t *testing.T) {
	sink := &recordingSink{}
	up := &scriptedCompactResponder{
		summaryReply: "FOLDED",
		scripts: [][]provider.Delta{
			toolCallScript("c1", "probe", "{}"), // Turn 0 (opening): ask for the oversized tool
			toolCallScript("c2", "peek", "{}"),  // Turn 1: over budget at its top → fold, then keep the Exchange open
		},
	}
	cfg := autoCompactConfig(sink)
	toolReg := domain.NewToolRegistry()
	// The oversized result (~25k chars ≈ 6.2k tokens) exceeds the ~3.9k-token History allocation for
	// the 8k window, so committing it mid-Exchange puts the NEXT Turn over budget; the small one
	// keeps the Exchange open afterwards without pushing it back over.
	if err := toolReg.Register(fakeTool{name: "probe", readOnly: true, result: strings.Repeat("x", 25000)}); err != nil {
		t.Fatalf("Register(probe): %v", err)
	}
	if err := toolReg.Register(fakeTool{name: "peek", readOnly: true, result: "ok"}); err != nil {
		t.Fatalf("Register(peek): %v", err)
	}
	cfg.Tools = toolReg
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.midExchangeCompaction = true // what newChildAgent stamps on a delegate

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
		t.Fatalf("setup: history is not over budget after the large tool result; the fold would be untested")
	}

	res1, err := a.Step(context.Background()) // continuation Turn: over budget AND inExchange → it folds
	if err != nil {
		t.Fatalf("Step 1: %v", err)
	}
	if res1.Status != domain.StatusTurnComplete {
		t.Fatalf("Turn 1 status = %q, want %q (the fold does not end the Exchange)", res1.Status, domain.StatusTurnComplete)
	}
	if up.summaryCalls != 1 {
		t.Fatalf("mid-Exchange fold fired %d times, want exactly 1", up.summaryCalls)
	}
	if a.historyExceedsAllocation() {
		t.Error("the fold ran but did not bring the history under its allocation; the setup drifted")
	}
	if a.compactSat {
		t.Error("a fold that DID bring the history under its allocation saturated the trigger")
	}
	if n := countCompactionErrors(sink.events); n != 0 {
		t.Errorf("a successful fold emitted %d compaction ErrorEvents, want 0", n)
	}

	// The repair: the Exchange is still open, so aborting it must roll back to the folded
	// conversation — protected prefix + summary — not through it.
	a.AbortExchange()
	prefix := a.conv.PrefixEnd()
	if a.conv.Len() != prefix+1 {
		t.Fatalf("after abort conv.Len() = %d, want %d (protected prefix + the folded summary); the cached exchangeStart was not repaired after the mid-Exchange fold",
			a.conv.Len(), prefix+1)
	}
	if last := a.conv.At(a.conv.Len() - 1); !strings.Contains(last.Content, "FOLDED") {
		t.Errorf("last message after abort = %+v, want the folded summary", last)
	}
}

// compactionErrorTexts returns the Err text of every ErrorEvent attributed to the "compaction"
// source, in order — so a test can assert what the fold actually told the human, not just how many
// times it told them.
func compactionErrorTexts(events []domain.Event) []string {
	var out []string
	for _, e := range events {
		if ee, ok := e.(domain.ErrorEvent); ok && ee.Source == "compaction" {
			out = append(out, ee.Err)
		}
	}
	return out
}

// TestAutoCompactFailedFoldStandsDownForTheRestOfTheExchange drives the 2026-08-29 retry runaway:
// a CHILD agent (midExchangeCompaction) whose history sits over budget re-ran the identical failing
// summary call at every Turn boundary — one ~40-minute call per Turn for ~9 h. A fold that FAULTS
// leaves the conversation untouched (Compact's guarantee), so the next boundary would hand the same
// call the same history: the fault must latch the estimate-driven trigger off for the rest of the
// Exchange. Four over-budget Turns must therefore produce exactly ONE summary call and ONE
// ErrorEvent, and that event must say the trigger stood down.
//
// The four Turns keep the existing 25k-char result + "ok" reply shape deliberately: that keeps every
// request under requestExceedsWindow's doubled uncalibrated room (loop.go), so the latch-exempt
// emergencyFold never fires and adds a summary call of its own — summaryCalls == 1 is the
// estimate-driven trigger's own count.
func TestAutoCompactFailedFoldStandsDownForTheRestOfTheExchange(t *testing.T) {
	sink := &recordingSink{}
	up := &scriptedCompactResponder{
		summaryReply: "", // an empty summary is a fault: context.Compact rejects it (errEmptySummary)
		scripts: [][]provider.Delta{
			toolCallScript("c1", "probe", "{}"),     // Turn 0 (opening): the oversized result puts the history over budget
			toolCallScript("c2", "peek", "{}"),      // Turn 1: over budget at its top → the fold RUNS and faults
			toolCallScript("c3", "peek", `{"n":3}`), // Turn 2: still over budget → the latch must stand the trigger down
			toolCallScript("c4", "peek", `{"n":4}`), // Turn 3: ditto — a second silent boundary
			//                                          (the arguments differ per Turn so the tool-loop
			//                                          breaker guard does not read them as a repeat)
		},
	}
	cfg := autoCompactConfig(sink)
	toolReg := domain.NewToolRegistry()
	// The oversized result (~25k chars ≈ 6.2k tokens) exceeds the ~3.9k-token History allocation for
	// the 8k window; the small one keeps the Exchange open afterwards without shrinking it back.
	if err := toolReg.Register(fakeTool{name: "probe", readOnly: true, result: strings.Repeat("x", 25000)}); err != nil {
		t.Fatalf("Register(probe): %v", err)
	}
	if err := toolReg.Register(fakeTool{name: "peek", readOnly: true, result: "ok"}); err != nil {
		t.Fatalf("Register(peek): %v", err)
	}
	cfg.Tools = toolReg
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.midExchangeCompaction = true // what newChildAgent stamps on a delegate

	if err := a.Submit(domain.UserInput{Text: "start"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	for turn := 0; turn < 4; turn++ {
		res, err := a.Step(context.Background())
		if err != nil {
			t.Fatalf("Step %d: %v", turn, err)
		}
		if res.Status != domain.StatusTurnComplete {
			t.Fatalf("Turn %d status = %q, want %q (a tool Turn keeps the Exchange open)", turn, res.Status, domain.StatusTurnComplete)
		}
	}

	if !a.historyExceedsAllocation() {
		t.Fatal("setup: the history is not over budget after the failed fold; the stand-down would be untested")
	}
	if up.summaryCalls != 1 {
		t.Fatalf("summarizer calls = %d, want 1 — a faulted fold was retried at a later Turn boundary", up.summaryCalls)
	}
	errs := compactionErrorTexts(sink.events)
	if len(errs) != 1 {
		t.Fatalf("compaction ErrorEvents = %d (%q), want exactly 1 — one event, then silence", len(errs), errs)
	}
	if !strings.HasSuffix(errs[0], foldStandDownSuffix) {
		t.Errorf("compaction ErrorEvent = %q, want it to end with %q so the human learns why the failure is not repeated", errs[0], foldStandDownSuffix)
	}
	if !strings.Contains(errs[0], "empty summary") {
		t.Errorf("compaction ErrorEvent = %q, want the underlying fault still named", errs[0])
	}
}

// TestAutoCompactFailedFoldReArmsAtTheNextExchangeOpening is the main agent's half: its fold runs at
// an Exchange OPENING, and openExchange clears the latch immediately after, so the next Exchange
// folds afresh rather than inheriting a stand-down from the last one. Because no stand-down actually
// happens there, neither error carries the suffix — telling the human that automatic folding stood
// down for an Exchange that is already over would be a lie.
func TestAutoCompactFailedFoldReArmsAtTheNextExchangeOpening(t *testing.T) {
	sink := &recordingSink{}
	up := &scriptedCompactResponder{
		summaryReply: "", // every fold faults
		scripts: [][]provider.Delta{
			contentScript("one"), // Exchange 1: one Turn, after the fold at its opening faulted
			contentScript("two"), // Exchange 2: the trigger must have re-armed
		},
	}
	a, err := newAgent(autoCompactConfig(sink), up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	// Oversized protected prefix (~25k chars ≈ 6.2k tokens > the ~3.9k-token History allocation) plus
	// a foldable tail: over budget at every opening, and a faulted fold leaves it that way.
	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: strings.Repeat("g", 25000)})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, Content: "a1"})
	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: "u1"})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, Content: "a2"})

	runExchange(t, a, "q1")
	if up.summaryCalls != 1 {
		t.Fatalf("the first opening did not fold once: summarizer calls = %d, want 1", up.summaryCalls)
	}
	if a.compactFailed {
		t.Error("the stand-down latch survived openExchange; the main agent must re-arm at every opening")
	}

	runExchange(t, a, "q2")
	if up.summaryCalls != 2 {
		t.Fatalf("the trigger did not re-arm at the next Exchange opening: summarizer calls = %d, want 2", up.summaryCalls)
	}
	errs := compactionErrorTexts(sink.events)
	if len(errs) != 2 {
		t.Fatalf("compaction ErrorEvents = %d (%q), want 2 — one per faulted fold", len(errs), errs)
	}
	for i, e := range errs {
		if strings.Contains(e, foldStandDownSuffix) {
			t.Errorf("compaction ErrorEvent %d = %q, want no stand-down suffix: the fold ran at an Exchange opening, and openExchange re-arms it", i, e)
		}
	}
}

// TestFailedFoldStandDownDoesNotBlockTheEmergencyFold pins the latch's exemption: the overflow-driven
// emergency fold is the Turn's ONLY remedy for a request the server just rejected, so it keeps its
// single shot even in an Exchange whose estimate-driven trigger already stood down. A child folds at
// the Turn boundary and faults (latch set), the same Turn's request then comes back
// DeltaContextOverflow, and the emergency fold's own summary call must still be made.
func TestFailedFoldStandDownDoesNotBlockTheEmergencyFold(t *testing.T) {
	sink := &recordingSink{}
	up := &scriptedCompactResponder{
		summaryReply: "", // both folds fault, so the emergency fold's shot is visible in the call count alone
		scripts: [][]provider.Delta{
			toolCallScript("c1", "probe", "{}"),                                             // Turn 0 (opening): the oversized result puts the history over budget
			{{Kind: provider.DeltaContextOverflow, Err: "apogee: context window exceeded"}}, // Turn 1: fold faults, then the request is rejected
		},
	}
	cfg := autoCompactConfig(sink)
	toolReg := domain.NewToolRegistry()
	if err := toolReg.Register(fakeTool{name: "probe", readOnly: true, result: strings.Repeat("x", 25000)}); err != nil {
		t.Fatalf("Register(probe): %v", err)
	}
	cfg.Tools = toolReg
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.midExchangeCompaction = true

	if err := a.Submit(domain.UserInput{Text: "start"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step 0: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step 1: %v", err)
	}

	if !a.compactFailed {
		t.Fatal("setup: the Turn-boundary fold did not fault, so the latch is not set and the exemption is untested")
	}
	if up.summaryCalls != 2 {
		t.Fatalf("summarizer calls = %d, want 2 — the stand-down latch swallowed the emergency fold's one shot", up.summaryCalls)
	}
}

// TestCompactOnDemandIgnoresTheStandDownLatch pins the other exemption: /compact is the human asking
// for this fold now, so a stand-down left by a failed automatic fold must not silently refuse them.
func TestCompactOnDemandIgnoresTheStandDownLatch(t *testing.T) {
	up := &compactSpyResponder{reply: "SUMMARY"}
	a, err := newAgent(autoCompactConfig(&recordingSink{}), up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedFoldable(a)
	a.compactFailed = true // what a faulted automatic fold left behind earlier in this Exchange

	skipped, err := a.Compact(context.Background())
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if skipped {
		t.Fatal("Compact skipped; the seeded tail is foldable, so the assertion below would be vacuous")
	}
	if up.summaryCalls != 1 {
		t.Fatalf("summarizer calls = %d, want 1 — the on-demand fold must ignore the stand-down latch", up.summaryCalls)
	}
}
