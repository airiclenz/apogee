package agent

// The step-budget notice (stepnotice.go): an engine note, structural for every delegate. The loop
// test drives a capped child through Run, where the note has to land under the engine's own fence
// on the tool result that closes the Turn reaching three quarters of the cap and on no other, and
// book no firing; the one-result tests (adviseOneCall) pin the edges one result at a time — a
// second result of the same Turn, depth 0, an unbounded cap, Bypass, a rollback — and the fold
// test proves a fold that swallowed the note has it told again. The token-budget notice, its twin
// for the token cap, is pinned by the same suite over the child's cumulative prompt tokens
// (TestTokenNotice*), at the foot of this file.

import (
	"strings"
	"testing"

	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// stepNoticeConfig is a config with a read-only tool for a scripted child to spend its Turns on;
// the notice itself has no switch to set.
func stepNoticeConfig(sink domain.EventSink) domain.Config {
	return configWithTools(sink, fakeTool{name: "read_thing", readOnly: true, result: "package main"})
}

// stepNoticeChild builds a depth-1 Agent on cfg with the step cap given — what newChildAgent seeds
// on a delegate — holding one assistant tool call so adviseOneCall can commit results against it.
func stepNoticeChild(t *testing.T, cfg domain.Config, stepCap int) *Agent {
	t.Helper()

	a, err := newAgent(cfg, echoResponder(t, "unused"))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.depth = 1
	a.stepCap = stepCap
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "read_file"}}})
	return a
}

// stepNoticeRendered is the exact fence the notice appends at used of cap.
func stepNoticeRendered(line string) string {
	return domain.RenderEngineNote(stepNoticeTopic, line)
}

// assertStepNoted asserts msg carries the step notice's note and nothing else past body: the
// content is body then the exact fence, and the ledger's one row is the engine note on the topic
// at the offset the fence begins, naming no Reaction.
func assertStepNoted(t *testing.T, msg domain.Message, body, line string) {
	t.Helper()
	assertEngineNoted(t, msg, body, stepNoticeTopic, line)
}

// assertEngineNoted is assertStepNoted for any one engine-note topic.
func assertEngineNoted(t *testing.T, msg domain.Message, body, topic, line string) {
	t.Helper()

	if want := body + domain.RenderEngineNote(topic, line); msg.Content != want {
		t.Errorf("noted tool message =\n%q\nwant the body then the engine fence:\n%q", msg.Content, want)
	}
	want := domain.AdviceSpan{Origin: domain.OriginEngine, Offset: len(body), Topic: topic}
	if len(msg.Advice) != 1 || msg.Advice[0] != want {
		t.Errorf("ledger = %+v, want the one engine-note row %+v", msg.Advice, want)
	}
}

// assertBare asserts msg carries no fence of any kind: its content is body and its ledger empty.
func assertBare(t *testing.T, i int, msg domain.Message, body string) {
	t.Helper()

	if len(msg.Advice) != 0 || msg.Content != body {
		t.Errorf("tool message %d = %q with ledger %+v, want it bare", i, msg.Content, msg.Advice)
	}
}

// assertNoStepFiring asserts the notice booked nothing: no ReactionFiredEvent names it, because a
// structural note is not a Reaction and has no firing to book.
func assertNoStepFiring(t *testing.T, sink *recordingSink) {
	t.Helper()

	for _, fe := range firedAdvice(sink) {
		if strings.Contains(fe.Reaction, "step") || strings.HasPrefix(fe.Detail, "step ") {
			t.Errorf("the stream booked a firing for the step notice, want none: %+v", fe)
		}
	}
}

const (
	stepNoticeLineThreeOfFour = "steps: 3 of 4 used — 1 left before the wrap-up Turn; write your output now"
	stepNoticeLineFourOfFour  = "steps: 4 of 4 used — 0 left before the wrap-up Turn; write your output now"
)

// The whole contract through the loop: a child capped at 4 hears the notice exactly once, on the
// tool result that closes Turn 3 — ceil(0.75 × 4) — as the engine fence with the exact text and a
// ledger row on the topic, and nothing is booked. The results of Turns 1, 2 and 4 land bare — the
// fourth is past the threshold too, but the note it would carry is still in the conversation.
func TestStepNoticeFiresOnceOnTheThirdTurnOfACapOfFour(t *testing.T) {
	sink := &recordingSink{}
	// Four working Turns, then the engine fold's reply and the wrap-up's — the two requests a
	// bound spends past the cap (finishAtStepCap).
	scripts := append(cappedChildTurns(4), contentScript(childFoldSummary), contentScript(childClosingReport))
	responder := &requestLogResponder{scripts: scripts}
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
	assertNoStepFiring(t, sink)
	msgs := toolMessages(a)
	if len(msgs) != 4 {
		t.Fatalf("conversation holds %d tool messages, want 4 — the cap's Turns", len(msgs))
	}
	for i, msg := range msgs {
		if i == 2 {
			continue
		}
		assertBare(t, i, msg, "package main")
	}
	assertStepNoted(t, msgs[2], "package main", stepNoticeLineThreeOfFour)
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

// adviceFixture is the body of the closing read_file result in the fixture test: lines copied from
// internal/domain/advice.go, a file that itself spells both fence headers — what a delegate read
// in the 2026-09-19 session when it took the advice fence on its result for part of the file.
const adviceFixture = `// An advise Reaction returns text the model sees. For a tool-shaped Moment the text lands as
// a fenced trailer on the closing tool result, so the request prefix is untouched and a local
// server's prefix cache survives the Turn. The engine rides the same seam for its own
// structural text — an ENGINE NOTE (RenderEngineNote, Message.WithEngineNote), fenced under its
// own header on the closing tool result of a request that needs it (a capped delegate's wrap-up
// directive, Request.NoteOnTail) and recorded on the same ledger, so one strip and one staleness
// guard cover both.
const (
	AdviceFencePrefix      = "[advice — reaction "
	AdviceFenceClosePrefix = "[end advice — "
)
const (
	EngineNoteFencePrefix      = "[engine — "
	EngineNoteFenceClosePrefix = "[end engine — "
)`

// The note is the tail of the message, after the body, under the engine's fence and never the
// advice fence — on a result whose body spells both headers itself, so the fixture is the file
// the session's delegate misread. The body is unchanged and the ledger row is the note's.
func TestStepNoticeIsTheEngineFenceOnTheTailOfAFileThatSpellsBothHeaders(t *testing.T) {
	sink := &recordingSink{}
	a := stepNoticeChild(t, stepNoticeConfig(sink), 4)
	a.turns.exchangeTurns = 2 // Run has counted two Turns: the one under way is the third

	msg := adviseOneCall(t, a, adviceFixture)

	assertStepNoted(t, msg, adviceFixture, stepNoticeLineThreeOfFour)
	if tail := msg.Content[len(adviceFixture):]; strings.Contains(tail, domain.AdviceFencePrefix) {
		t.Errorf("the note's tail %q carries the advice fence; want the engine fence alone", tail)
	}
	if !strings.HasPrefix(msg.Content[len(adviceFixture):], "\n\n"+domain.EngineNoteFencePrefix+stepNoticeTopic+"]\n") {
		t.Errorf("the note opens %q, want the `[engine — step budget]` header", msg.Content[len(adviceFixture):])
	}
	assertNoStepFiring(t, sink)
}

// A Turn with several tool calls commits once per call, and the notice rides the FIRST result of
// the threshold Turn alone: the second lands bare.
func TestStepNoticeRidesOneResultOfAManyCallTurn(t *testing.T) {
	sink := &recordingSink{}
	a := stepNoticeChild(t, stepNoticeConfig(sink), 4)
	a.turns.exchangeTurns = 2

	first := adviseOneCall(t, a, "one")
	second := adviseOneCall(t, a, "two")

	assertStepNoted(t, first, "one", stepNoticeLineThreeOfFour)
	assertBare(t, 1, second, "two")
}

// A cancelled Turn's rollback re-arms the notice ONLY when the dropped result is the one the note
// rode: the re-attempt of the threshold Turn is told again (and the re-arm is idempotent), while a
// cancelled Turn PAST the threshold keeps the noted result, so its re-attempt carries no second
// copy.
func TestStepNoticeReArmsAfterARollback(t *testing.T) {
	t.Run("the threshold Turn itself", func(t *testing.T) {
		sink := &recordingSink{}
		a := stepNoticeChild(t, stepNoticeConfig(sink), 4)
		a.turns.exchangeTurns = 2
		adviseOneCall(t, a, "one")
		if a.stepNoticeAt != 1 || !a.stepNoticeLive {
			t.Fatalf("the notice fired and latched (%d, %v), want Turn 1 live", a.stepNoticeAt, a.stepNoticeLive)
		}

		a.rearmNotices() // the rollback: the index still names the cancelled Turn
		a.rearmNotices()

		if a.stepNoticeAt != 0 || a.stepNoticeLive {
			t.Fatalf("latch = (%d, %v) after the re-arm, want cleared", a.stepNoticeAt, a.stepNoticeLive)
		}
		assertStepNoted(t, adviseOneCall(t, a, "one again"), "one again", stepNoticeLineThreeOfFour)
	})
	t.Run("a later Turn past the threshold", func(t *testing.T) {
		sink := &recordingSink{}
		a := stepNoticeChild(t, stepNoticeConfig(sink), 4)
		a.turns.exchangeTurns = 2
		adviseOneCall(t, a, "three")
		a.turns.index, a.turns.exchangeTurns = 1, 3 // Turn 4 under way; the noted result stays
		assertBare(t, 1, adviseOneCall(t, a, "four"), "four")

		a.rearmNotices() // Turn 4 cancelled: its own messages drop, the noted third stays

		if a.stepNoticeAt != 1 || !a.stepNoticeLive {
			t.Fatalf("latch = (%d, %v) after a later Turn's rollback, want Turn 1 still live", a.stepNoticeAt, a.stepNoticeLive)
		}
		assertBare(t, 2, adviseOneCall(t, a, "four again"), "four again")
	})
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

			assertBare(t, 0, msg, "body")
			assertNoStepFiring(t, sink)
		})
	}
}

// Structural means on under Bypass: the note is the engine's, not a Reaction's, so the switch that
// silences every model-shaping class leaves it exactly where it lands with Bypass off — and the
// ladder never held it in the first place.
func TestStepNoticeFiresUnderBypassAndIsNoBuiltin(t *testing.T) {
	sink := &recordingSink{}
	cfg := stepNoticeConfig(sink)
	cfg.Bypass = true
	a := stepNoticeChild(t, cfg, 4)
	a.turns.exchangeTurns = 2

	for _, id := range builtinIDs(a) {
		if strings.Contains(id, "step") {
			t.Errorf("builtins = %v, want no step-notice rung on the ladder", builtinIDs(a))
		}
	}
	msg := adviseOneCall(t, a, "body")

	assertStepNoted(t, msg, "body", stepNoticeLineThreeOfFour)
	assertNoStepFiring(t, sink)
}

// A fold between the notice Turn and the cap swallows the note with the history it sat in, and the
// next tool result is told again: a child capped at 4 hears it on Turn 3, the request for Turn 4
// overflows and the emergency fold replaces the conversation, and Turn 4's result — the first
// past the threshold with no note live — carries it once more, at 4 of 4. Without a fold the run
// carries one copy (the loop test above); with one, still never two at a time.
func TestStepNoticeIsToldAgainAfterAFoldSwallowedIt(t *testing.T) {
	sink := &recordingSink{}
	turns := cappedChildTurns(4)
	scripts := [][]provider.Delta{
		turns[0], turns[1], turns[2],
		{{Kind: provider.DeltaContextOverflow, Err: "apogee: context window exceeded"}}, // Turn 4's first request
		contentScript("FOLDED"), // the overflow fold's summary call
		turns[3],                // Turn 4 re-sent over the folded conversation
		contentScript(childFoldSummary),
		contentScript(childClosingReport),
	}
	responder := &requestLogResponder{scripts: scripts}
	cfg := stepNoticeConfig(sink)
	cfg.Context.CompactionEnabled = true
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.depth = 1
	a.stepCap = 4

	res := runExchange(t, a, "trawl the repo")

	if !res.StepCapped {
		t.Fatalf("the child ended %+v, want it capped", res)
	}
	if responder.calls != len(scripts) {
		t.Fatalf("the upstream took %d requests, want %d — three Turns, the overflow, its fold, the re-sent Turn, the engine fold and the wrap-up", responder.calls, len(scripts))
	}
	overflowed := responder.requests[3].Messages
	if tail := overflowed[len(overflowed)-1].Content; !strings.HasSuffix(tail, stepNoticeRendered(stepNoticeLineThreeOfFour)) {
		t.Errorf("the overflowed request's tail = %q, want Turn 3's result carrying the note", tail)
	}
	for _, msg := range responder.requests[5].Messages {
		if strings.Contains(msg.Content, domain.EngineNoteFencePrefix+stepNoticeTopic) {
			t.Errorf("the re-sent request still carries the note in %q; the fold should have swallowed it", msg.Content)
		}
	}
	msgs := toolMessages(a)
	if len(msgs) != 1 {
		t.Fatalf("conversation holds %d tool messages after the fold, want the one post-fold result", len(msgs))
	}
	assertStepNoted(t, msgs[0], "package main", stepNoticeLineFourOfFour)
	assertNoStepFiring(t, sink)
}

// stepNoticePruneConfig is pruneConfig with the step-notice tool: a discovered window, Pruning
// armed and the fold silenced, for a capped child whose old results the prune will stub.
func stepNoticePruneConfig(sink domain.EventSink) domain.Config {
	cfg := pruneConfig(sink)
	cfg.Tools = stepNoticeConfig(sink).Tools
	return cfg
}

// A prune that stubs the noted result swallows the note as a fold does, and the next tool result
// is told again. The noted body here is a few bytes — SHORTER than the stub that replaces it — on
// purpose: dropStaleAdvice clears a ledger only when the new content no longer reaches the note's
// offset, so over a short body the stub keeps the ledger row, and only the header check behind
// Conversation.HasEngineNote tells the engine the note is gone. The control case prunes an old
// result while the noted one stays inside the kept window: the latch stands and the next result
// carries no second copy.
func TestStepNoticeIsToldAgainAfterAPruneStubbedIt(t *testing.T) {
	t.Run("the noted result is stubbed", func(t *testing.T) {
		sink := &recordingSink{}
		a := stepNoticeChild(t, stepNoticePruneConfig(sink), 4)
		a.turns.exchangeTurns = 2
		noted := adviseOneCall(t, a, "one") // the noted result: index 1, the oldest tool result
		assertStepNoted(t, noted, "one", stepNoticeLineThreeOfFour)
		seedToolTurns(a, apogeectx.PruneKeepTurns, 4000) // ~16k chars, past the ~9.4k-char trigger; the noted Turn is the one outside the window
		if !a.stepNoticeLive {
			t.Fatal("the notice did not latch on the noted result")
		}

		a.autoPrune(1)

		results := toolResultContents(a)
		if !strings.HasPrefix(results[0], "[pruned:") {
			t.Fatalf("the noted result = %q after the prune, want it stubbed", results[0])
		}
		if len(results[0]) <= len("one") {
			t.Fatalf("stub %q is not longer than the noted body; the test needs the ledger row to survive dropStaleAdvice", results[0])
		}
		if len(a.conv.At(1).Advice) == 0 {
			t.Fatal("the stub dropped the ledger row; the test needs the row to survive so the header check alone re-arms")
		}
		if a.stepNoticeLive {
			t.Fatal("stepNoticeLive still true after the prune stubbed the noted result, want the latch cleared")
		}
		assertStepNoted(t, adviseOneCall(t, a, "two"), "two", stepNoticeLineThreeOfFour)
		assertNoStepFiring(t, sink)
	})
	t.Run("the noted result stays inside the kept window", func(t *testing.T) {
		sink := &recordingSink{}
		a := stepNoticeChild(t, stepNoticePruneConfig(sink), 4)
		seedToolTurns(a, apogeectx.PruneKeepTurns+1, 4000) // the oldest Turn is the one outside the window
		a.turns.exchangeTurns = 2
		assertStepNoted(t, adviseOneCall(t, a, "one"), "one", stepNoticeLineThreeOfFour)

		a.autoPrune(1)

		results := toolResultContents(a)
		if !strings.HasPrefix(results[0], "[pruned:") {
			t.Fatalf("the oldest result = %q after the prune, want it stubbed", results[0])
		}
		if last := results[len(results)-1]; !strings.HasSuffix(last, stepNoticeRendered(stepNoticeLineThreeOfFour)) {
			t.Fatalf("the noted result = %q after the prune, want it kept whole", last)
		}
		if !a.stepNoticeLive {
			t.Fatal("stepNoticeLive cleared by a prune that kept the noted result, want the latch standing")
		}
		assertBare(t, 1, adviseOneCall(t, a, "two"), "two")
		assertNoStepFiring(t, sink)
	})
}

// ---------------------------------------------------------------------------
// The token-budget notice: stepBudgetNotice's twin over the child's cumulative prompt tokens
// (Agent.usage) against its token cap (`delegate-max-tokens`). The 2026-09-18 session's evidence:
// a child at 10.7M of a 20M budget under a 1.3M working window heard nothing, because the fill
// ladder measures one request against the window and the step notice counts Turns.

// tokenNoticeChild is stepNoticeChild for the token cap: a depth-1 Agent whose usage tally already
// reads spent prompt tokens — what the server's reports across its earlier Turns left it at — with
// no step cap, so nothing but the token notice can fire.
func tokenNoticeChild(t *testing.T, cfg domain.Config, tokenCap, spent int) *Agent {
	t.Helper()

	a := stepNoticeChild(t, cfg, 0)
	a.tokenCap = tokenCap
	a.usage.prompt = spent
	return a
}

// tokenNoticeRendered is the exact fence the token notice appends.
func tokenNoticeRendered(line string) string {
	return domain.RenderEngineNote(tokenNoticeTopic, line)
}

// assertTokenNoted is assertStepNoted for the token notice's topic.
func assertTokenNoted(t *testing.T, msg domain.Message, body, line string) {
	t.Helper()
	assertEngineNoted(t, msg, body, tokenNoticeTopic, line)
}

// assertNoTokenFiring asserts the token notice booked nothing: a structural note has no firing.
func assertNoTokenFiring(t *testing.T, sink *recordingSink) {
	t.Helper()

	for _, fe := range firedAdvice(sink) {
		if strings.Contains(fe.Reaction, "token") || strings.HasPrefix(fe.Detail, "token") {
			t.Errorf("the stream booked a firing for the token notice, want none: %+v", fe)
		}
	}
}

const (
	tokenCapTwentyMillion          = 20_000_000
	tokenNoticeLineFifteenPointTwo = "tokens: 15.2M of 20.0M spent — 4.8M left before the wrap-up Turn; write your output now"
	tokenNoticeLineSixteen         = "tokens: 16.0M of 20.0M spent — 4.0M left before the wrap-up Turn; write your output now"
	tokenNoticeLineTwentySix       = "tokens: 26.0M of 20.0M spent — 0 left before the wrap-up Turn; write your output now"
)

// The whole contract through the loop: a child budgeted 20M whose Turns report 10M, 6M and 10M
// prompt tokens hears the notice exactly once, on the tool result closing Turn 2 — the first
// Turn whose cumulative spend (16M) reaches ceil(0.75 × 20M) = 15M — as the engine fence with the
// M-tier figures and a ledger row on the topic, and nothing is booked. Turn 1's result lands bare
// (10M is under the threshold) and Turn 3's too: past the threshold, but the note is still live.
// Turn 3 takes the spend to 26M, so the token bound ends the run through the wrap-up.
func TestTokenNoticeFiresOnceAtThreeQuartersOfTheCap(t *testing.T) {
	sink := &recordingSink{}
	scripts := append(spentChildTurns(10_000_000, 6_000_000, 10_000_000), contentScript(childFoldSummary), contentScript(childClosingReport))
	responder := &requestLogResponder{scripts: scripts}
	a, err := newAgent(stepNoticeConfig(sink), responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.depth = 1
	a.tokenCap = tokenCapTwentyMillion

	res := runExchange(t, a, "trawl the repo")

	if !res.StepCapped {
		t.Fatalf("the child ended %+v, want it capped — the notice is measured against a budget the run meets", res)
	}
	if a.capHit != boundTokens {
		t.Fatalf("the bound that ended the child = %v, want the token bound", a.capHit)
	}
	assertNoTokenFiring(t, sink)
	msgs := toolMessages(a)
	if len(msgs) != 3 {
		t.Fatalf("conversation holds %d tool messages, want 3 — the Turns the budget allowed", len(msgs))
	}
	assertBare(t, 0, msgs[0], "package main")
	assertTokenNoted(t, msgs[1], "package main", tokenNoticeLineSixteen)
	assertBare(t, 2, msgs[2], "package main")
}

// The threshold is ceil(0.75 × cap) in tokens: the default 20M budget warns at 15M.
func TestTokenNoticeThresholdIsThreeQuartersRoundedUp(t *testing.T) {
	cases := map[int]int{1: 1, 2: 2, 4: 3, 1000: 750, 1001: 751, tokenCapTwentyMillion: 15_000_000}
	for tokenCap, want := range cases {
		if got := tokenNoticeThreshold(tokenCap); got != want {
			t.Errorf("tokenNoticeThreshold(%d) = %d, want %d", tokenCap, got, want)
		}
	}
}

// A Turn with several tool calls commits once per call, and the token notice rides the FIRST
// result of the threshold Turn alone: the second lands bare.
func TestTokenNoticeRidesOneResultOfAManyCallTurn(t *testing.T) {
	sink := &recordingSink{}
	a := tokenNoticeChild(t, stepNoticeConfig(sink), tokenCapTwentyMillion, 15_200_000)

	first := adviseOneCall(t, a, "one")
	second := adviseOneCall(t, a, "two")

	assertTokenNoted(t, first, "one", tokenNoticeLineFifteenPointTwo)
	assertBare(t, 1, second, "two")
	assertNoTokenFiring(t, sink)
}

// A cancelled Turn's rollback re-arms the token notice only when the dropped result is the one
// the note rode, exactly as it does the step notice: the threshold Turn's re-attempt is told again.
func TestTokenNoticeReArmsAfterARollback(t *testing.T) {
	sink := &recordingSink{}
	a := tokenNoticeChild(t, stepNoticeConfig(sink), tokenCapTwentyMillion, 15_200_000)
	adviseOneCall(t, a, "one")
	if a.tokenNoticeAt != 1 || !a.tokenNoticeLive {
		t.Fatalf("the notice fired and latched (%d, %v), want Turn 1 live", a.tokenNoticeAt, a.tokenNoticeLive)
	}

	a.rearmNotices() // the rollback: the index still names the cancelled Turn

	if a.tokenNoticeAt != 0 || a.tokenNoticeLive {
		t.Fatalf("latch = (%d, %v) after the re-arm, want cleared", a.tokenNoticeAt, a.tokenNoticeLive)
	}
	assertTokenNoted(t, adviseOneCall(t, a, "one again"), "one again", tokenNoticeLineFifteenPointTwo)
}

// Silent everywhere it has nothing to say: at depth 0 (no cap), on an unbounded budget (cap 0),
// and on a spend short of the threshold — 14.9M of 20M, where the 2026-09-18 child at 10.7M sat.
func TestTokenNoticeIsSilentAtDepthZeroUnboundedAndUnderTheThreshold(t *testing.T) {
	cases := []struct {
		name     string
		depth    int
		tokenCap int
		spent    int
	}{
		{name: "depth 0", depth: 0, tokenCap: tokenCapTwentyMillion, spent: 15_200_000},
		{name: "unbounded budget", depth: 1, tokenCap: 0, spent: 15_200_000},
		{name: "under the threshold", depth: 1, tokenCap: tokenCapTwentyMillion, spent: 14_900_000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			a := tokenNoticeChild(t, stepNoticeConfig(sink), tc.tokenCap, tc.spent)
			a.depth = tc.depth

			msg := adviseOneCall(t, a, "body")

			assertBare(t, 0, msg, "body")
			assertNoTokenFiring(t, sink)
		})
	}
}

// Structural means on under Bypass, and never a rung on the Reaction ladder.
func TestTokenNoticeFiresUnderBypassAndIsNoBuiltin(t *testing.T) {
	sink := &recordingSink{}
	cfg := stepNoticeConfig(sink)
	cfg.Bypass = true
	a := tokenNoticeChild(t, cfg, tokenCapTwentyMillion, 15_200_000)

	for _, id := range builtinIDs(a) {
		if strings.Contains(id, "token") {
			t.Errorf("builtins = %v, want no token-notice rung on the ladder", builtinIDs(a))
		}
	}
	msg := adviseOneCall(t, a, "body")

	assertTokenNoted(t, msg, "body", tokenNoticeLineFifteenPointTwo)
	assertNoTokenFiring(t, sink)
}

// A fold between the notice Turn and the bound swallows the note with the history it sat in, and
// the next tool result is told again: the child hears it on Turn 2 at 16M, Turn 3's request
// overflows and the emergency fold replaces the conversation, and Turn 3's result — past the
// threshold with no note live — carries it once more, at 26M of 20M with 0 left. Without the
// re-arm in foldFor the post-fold result would land bare.
func TestTokenNoticeIsToldAgainAfterAFoldSwallowedIt(t *testing.T) {
	sink := &recordingSink{}
	turns := spentChildTurns(10_000_000, 6_000_000, 10_000_000)
	scripts := [][]provider.Delta{
		turns[0], turns[1],
		{{Kind: provider.DeltaContextOverflow, Err: "apogee: context window exceeded"}}, // Turn 3's first request
		contentScript("FOLDED"), // the overflow fold's summary call
		turns[2],                // Turn 3 re-sent over the folded conversation
		contentScript(childFoldSummary),
		contentScript(childClosingReport),
	}
	responder := &requestLogResponder{scripts: scripts}
	cfg := stepNoticeConfig(sink)
	cfg.Context.CompactionEnabled = true
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.depth = 1
	a.tokenCap = tokenCapTwentyMillion

	res := runExchange(t, a, "trawl the repo")

	if !res.StepCapped {
		t.Fatalf("the child ended %+v, want it capped", res)
	}
	if responder.calls != len(scripts) {
		t.Fatalf("the upstream took %d requests, want %d — two Turns, the overflow, its fold, the re-sent Turn, the engine fold and the wrap-up", responder.calls, len(scripts))
	}
	overflowed := responder.requests[2].Messages
	if tail := overflowed[len(overflowed)-1].Content; !strings.HasSuffix(tail, tokenNoticeRendered(tokenNoticeLineSixteen)) {
		t.Errorf("the overflowed request's tail = %q, want Turn 2's result carrying the note", tail)
	}
	for _, msg := range responder.requests[4].Messages {
		if strings.Contains(msg.Content, domain.EngineNoteFencePrefix+tokenNoticeTopic) {
			t.Errorf("the re-sent request still carries the note in %q; the fold should have swallowed it", msg.Content)
		}
	}
	msgs := toolMessages(a)
	if len(msgs) != 1 {
		t.Fatalf("conversation holds %d tool messages after the fold, want the one post-fold result", len(msgs))
	}
	assertTokenNoted(t, msgs[0], "package main", tokenNoticeLineTwentySix)
	assertNoTokenFiring(t, sink)
}

// A prune that stubs the noted result swallows the token note as a fold does, and the next tool
// result is told again — over a short noted body, where only Conversation.HasEngineNote's header
// check can tell the engine the note is gone (see the step twin above). The control case keeps
// the noted result inside the window: the latch stands and no second copy lands.
func TestTokenNoticeIsToldAgainAfterAPruneStubbedIt(t *testing.T) {
	t.Run("the noted result is stubbed", func(t *testing.T) {
		sink := &recordingSink{}
		a := tokenNoticeChild(t, stepNoticePruneConfig(sink), tokenCapTwentyMillion, 15_200_000)
		assertTokenNoted(t, adviseOneCall(t, a, "one"), "one", tokenNoticeLineFifteenPointTwo)
		seedToolTurns(a, apogeectx.PruneKeepTurns, 4000) // the noted Turn is the one outside the kept window

		a.autoPrune(1)

		results := toolResultContents(a)
		if !strings.HasPrefix(results[0], "[pruned:") {
			t.Fatalf("the noted result = %q after the prune, want it stubbed", results[0])
		}
		if len(a.conv.At(1).Advice) == 0 {
			t.Fatal("the stub dropped the ledger row; the test needs the row to survive so the header check alone re-arms")
		}
		if a.tokenNoticeLive {
			t.Fatal("tokenNoticeLive still true after the prune stubbed the noted result, want the latch cleared")
		}
		assertTokenNoted(t, adviseOneCall(t, a, "two"), "two", tokenNoticeLineFifteenPointTwo)
		assertNoTokenFiring(t, sink)
	})
	t.Run("the noted result stays inside the kept window", func(t *testing.T) {
		sink := &recordingSink{}
		a := tokenNoticeChild(t, stepNoticePruneConfig(sink), tokenCapTwentyMillion, 15_200_000)
		seedToolTurns(a, apogeectx.PruneKeepTurns+1, 4000) // the oldest Turn is the one outside the window
		assertTokenNoted(t, adviseOneCall(t, a, "one"), "one", tokenNoticeLineFifteenPointTwo)

		a.autoPrune(1)

		results := toolResultContents(a)
		if !strings.HasPrefix(results[0], "[pruned:") {
			t.Fatalf("the oldest result = %q after the prune, want it stubbed", results[0])
		}
		if last := results[len(results)-1]; !strings.HasSuffix(last, tokenNoticeRendered(tokenNoticeLineFifteenPointTwo)) {
			t.Fatalf("the noted result = %q after the prune, want it kept whole", last)
		}
		if !a.tokenNoticeLive {
			t.Fatal("tokenNoticeLive cleared by a prune that kept the noted result, want the latch standing")
		}
		assertBare(t, 1, adviseOneCall(t, a, "two"), "two")
		assertNoTokenFiring(t, sink)
	})
}
