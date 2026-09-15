package agent

// The step-budget notice (ADR 0077 addendum, stepnotice.go): the engine's second advise Reaction.
// The loop test drives a capped child through Run, where the notice has to land as the advise
// trailer on the tool result that closes the Turn reaching three quarters of the cap and on no
// other; the cascade tests (adviseOneCall) pin the edges one result at a time — a second result of
// the same Turn, depth 0, an unbounded cap, the switch off and Bypass.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// stepNoticeConfig is a config with the notice switched on and a read-only tool for a scripted
// child to spend its Turns on.
func stepNoticeConfig(sink domain.EventSink) domain.Config {
	cfg := configWithTools(sink, fakeTool{name: "read_thing", readOnly: true, result: "package main"})
	cfg.StepBudgetNotice = true
	return cfg
}

// stepNoticeChild builds a depth-1 Agent on cfg with the step cap given — what newChildAgent seeds
// on a delegate — holding one assistant tool call so adviseOneCall can commit results against it.
func stepNoticeChild(t *testing.T, cfg domain.Config, stepCap int) *Agent {
	t.Helper()

	a, err := newAgent(cfg, echoResponder{reply: "unused"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.depth = 1
	a.stepCap = stepCap
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "read_file"}}})
	return a
}

// stepNoticeFirings returns the ReactionFiredEvents the notice booked, in order.
func stepNoticeFirings(sink *recordingSink) []domain.ReactionFiredEvent {
	var out []domain.ReactionFiredEvent
	for _, fe := range firedAdvice(sink) {
		if fe.Reaction == stepBudgetNoticeID {
			out = append(out, fe)
		}
	}
	return out
}

// The whole contract through the loop: a child capped at 4 hears the notice exactly once, on the
// tool result that closes Turn 3 — ceil(0.75 × 4) — as the fenced advise trailer with the exact
// text, and the firing books the step and the cap. The results of Turns 1, 2 and 4 land bare.
func TestStepNoticeFiresOnceOnTheThirdTurnOfACapOfFour(t *testing.T) {
	sink := &recordingSink{}
	responder := &requestLogResponder{scripts: cappedChildTurns(10)}
	a, err := newAgent(stepNoticeConfig(sink), responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.depth = 1
	a.stepCap = 4

	res := runExchange(t, a, "trawl the repo")

	if !res.StepCapped {
		t.Fatalf("the child ended %+v, want it capped — the notice is measured against a cap the run meets", res)
	}
	fired := stepNoticeFirings(sink)
	if len(fired) != 1 {
		t.Fatalf("notice fired %d times, want once: %+v", len(fired), fired)
	}
	if fired[0].Action != actionNotice || fired[0].Detail != "step 3 of 4" {
		t.Errorf("firing = action %q detail %q, want %q / %q", fired[0].Action, fired[0].Detail, actionNotice, "step 3 of 4")
	}

	msgs := toolMessages(a)
	if len(msgs) != 4 {
		t.Fatalf("conversation holds %d tool messages, want 4 — the cap's Turns", len(msgs))
	}
	for i, msg := range msgs {
		if i == 2 {
			continue
		}
		if len(msg.Advice) != 0 || strings.Contains(msg.Content, "steps:") {
			t.Errorf("tool message %d = %q with spans %+v, want it bare", i, msg.Content, msg.Advice)
		}
	}
	third := msgs[2]
	if len(third.Advice) != 1 {
		t.Fatalf("the third tool message carries %d advice spans, want the notice's one: %+v", len(third.Advice), third.Advice)
	}
	span := third.Advice[0]
	if span.Reaction != stepBudgetNoticeID || span.Origin != domain.OriginEngine || span.Moment != domain.MomentPostToolResult || span.Turn != 2 {
		t.Errorf("span = %+v, want %q of engine origin at post-tool-result on Turn 2", span, stepBudgetNoticeID)
	}
	const wantLine = "steps: 3 of 4 used — 1 left before the wrap-up Turn; write your output now"
	if !strings.HasSuffix(third.Content, domain.RenderAdvice(span, wantLine)) {
		t.Errorf("third tool message = %q, want it to end with the shipped fence around %q", third.Content, wantLine)
	}
}

// The threshold is ceil(0.75 × cap): 3 of 4, 60 of 80, and a cap too small for a fraction still
// names a Turn the child reaches.
func TestStepNoticeThresholdIsThreeQuartersRoundedUp(t *testing.T) {
	cases := map[int]int{1: 1, 2: 2, 3: 3, 4: 3, 5: 4, 8: 6, 10: 8, 80: 60}
	for stepCap, want := range cases {
		if got := stepNoticeThreshold(stepCap); got != want {
			t.Errorf("stepNoticeThreshold(%d) = %d, want %d", stepCap, got, want)
		}
	}
}

// A Turn with several tool calls reaches post-tool-result once per call, and the notice rides the
// FIRST result of the threshold Turn alone: the second lands bare.
func TestStepNoticeRidesOneResultOfAManyCallTurn(t *testing.T) {
	sink := &recordingSink{}
	a := stepNoticeChild(t, stepNoticeConfig(sink), 4)
	a.turns.exchangeTurns = 2 // Run has counted two Turns: the one under way is the third

	first := adviseOneCall(t, a, "one")
	second := adviseOneCall(t, a, "two")

	if fired := stepNoticeFirings(sink); len(fired) != 1 {
		t.Fatalf("notice fired %d times over one Turn's two results, want once: %+v", len(fired), fired)
	}
	if len(first.Advice) != 1 || first.Advice[0].Reaction != stepBudgetNoticeID {
		t.Errorf("first result's spans = %+v, want the notice's one", first.Advice)
	}
	if len(second.Advice) != 0 || second.Content != "two" {
		t.Errorf("second result = %q with spans %+v, want it bare", second.Content, second.Advice)
	}
}

// A cancelled Turn's rollback drops the result the notice rode on and re-arms it, so the
// re-attempt of that Turn is told again — and the re-arm is idempotent.
func TestStepNoticeReArmsAfterARollback(t *testing.T) {
	sink := &recordingSink{}
	a := stepNoticeChild(t, stepNoticeConfig(sink), 4)
	a.turns.exchangeTurns = 2
	adviseOneCall(t, a, "one")
	if a.stepNoticeAt == 0 {
		t.Fatal("the notice fired and latched nothing")
	}

	a.rearmStepNotice()
	a.rearmStepNotice()

	if a.stepNoticeAt != 0 {
		t.Fatalf("stepNoticeAt = %d after the re-arm, want 0", a.stepNoticeAt)
	}
	adviseOneCall(t, a, "one again")
	if fired := stepNoticeFirings(sink); len(fired) != 2 {
		t.Errorf("notice fired %d times across a rollback, want twice — once per attempt: %+v", len(fired), fired)
	}
}

// Silent everywhere it has nothing to say: at depth 0 (no cap, the human's loop), on an unbounded
// delegation (cap 0), and on a Turn short of the threshold.
func TestStepNoticeIsSilentAtDepthZeroUnboundedAndUnderTheThreshold(t *testing.T) {
	cases := []struct {
		name    string
		depth   int
		stepCap int
		counted int
	}{
		{name: "depth 0", depth: 0, stepCap: 4, counted: 2},
		{name: "unbounded cap", depth: 1, stepCap: 0, counted: 2},
		{name: "under the threshold", depth: 1, stepCap: 4, counted: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			a := stepNoticeChild(t, stepNoticeConfig(sink), tc.stepCap)
			a.depth = tc.depth
			a.turns.exchangeTurns = tc.counted

			msg := adviseOneCall(t, a, "body")

			if fired := stepNoticeFirings(sink); len(fired) != 0 || len(msg.Advice) != 0 || msg.Content != "body" {
				t.Errorf("firings %+v, spans %+v, content %q — want the bare result", fired, msg.Advice, msg.Content)
			}
		})
	}
}

// Off is absent, not silent: with the switch off the notice is no builtin at all, and under Bypass
// the armed notice is skipped with the rest of its class — either way the threshold result lands
// bare, with no firing.
func TestStepNoticeIsAbsentWhenOffAndSkippedUnderBypass(t *testing.T) {
	cases := []struct {
		name   string
		notice bool
		bypass bool
	}{
		{name: "switch off", notice: false},
		{name: "Bypass on", notice: true, bypass: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			cfg := stepNoticeConfig(sink)
			cfg.StepBudgetNotice = tc.notice
			cfg.Bypass = tc.bypass
			a := stepNoticeChild(t, cfg, 4)
			a.turns.exchangeTurns = 2

			if armed := builtinIDs(a); tc.notice != contains(armed, stepBudgetNoticeID) {
				t.Errorf("builtins = %v, want the notice armed = %v", armed, tc.notice)
			}
			msg := adviseOneCall(t, a, "body")

			if fired := stepNoticeFirings(sink); len(fired) != 0 || len(msg.Advice) != 0 || msg.Content != "body" {
				t.Errorf("firings %+v, spans %+v, content %q — want the bare result", fired, msg.Advice, msg.Content)
			}
		})
	}
}

// The two notices are independent switches of one ladder: each is armed by its own, in order —
// the fill notice ahead of the step notice — and a swap moving only the step switch rebuilds the
// ladder exactly as one moving only the fill switch does.
func TestStepNoticeSwitchArmsAndRebuildsIndependently(t *testing.T) {
	sink := &recordingSink{}
	a := stepNoticeChild(t, stepNoticeConfig(sink), 4)

	ids := builtinIDs(a)
	if contains(ids, contextFillNoticeID) || ids[len(ids)-1] != stepBudgetNoticeID {
		t.Errorf("ladder = %v, want the guards and the step notice last, no fill notice", ids)
	}

	a.SetReactions(domain.Generation{ContextFillNotice: true, StepBudgetNotice: true})
	ids = builtinIDs(a)
	if n := len(ids); n != len(guardIDs)+2 || ids[n-2] != contextFillNoticeID || ids[n-1] != stepBudgetNoticeID {
		t.Errorf("ladder = %v, want the seven guards, the fill notice and then the step notice", ids)
	}

	a.SetReactions(domain.Generation{ContextFillNotice: true})
	if got := a.Generation(); got.StepBudgetNotice || !got.ContextFillNotice {
		t.Errorf("Generation() = %+v, want the step switch cleared and the fill switch kept", got)
	}
	if ids := builtinIDs(a); contains(ids, stepBudgetNoticeID) || !contains(ids, contextFillNoticeID) {
		t.Errorf("ladder = %v after a step-only swap, want the fill notice kept and the step notice gone", ids)
	}
}

// A child inherits the parent's LIVE step-notice switch at spawn, as it inherits the fill notice's
// (ADR 0077 D1): on when the parent's generation says on, off when it says off.
func TestStepNoticeSwitchReachesAChildAtSpawn(t *testing.T) {
	for _, on := range []bool{true, false} {
		t.Run(fmt.Sprintf("switch %v", on), func(t *testing.T) {
			sink := &recordingSink{}
			parent, err := newAgent(subAgentConfig(sink, domain.ModeAskBefore), &scriptedResponder{scripts: [][]provider.Delta{contentScript("unused")}})
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			gen := parent.Generation()
			gen.StepBudgetNotice = on
			parent.SetReactions(gen)

			child, err := parent.newChildAgent("c1", "count the files", "")
			if err != nil {
				t.Fatalf("newChildAgent: %v", err)
			}
			if got := child.Generation().StepBudgetNotice; got != on {
				t.Errorf("child Generation().StepBudgetNotice = %v, want the parent's live %v", got, on)
			}
			if got := contains(builtinIDs(child), stepBudgetNoticeID); got != on {
				t.Errorf("child's ladder arms the notice = %v, want %v", got, on)
			}
		})
	}
}
