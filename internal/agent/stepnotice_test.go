package agent

// The step-budget notice (stepnotice.go): an engine note, structural for every delegate. The loop
// test drives a capped child through Run, where the note has to land under the engine's own fence
// on the tool result that closes the Turn reaching three quarters of the cap and on no other, and
// book no firing; the one-result tests (adviseOneCall) pin the edges one result at a time — a
// second result of the same Turn, depth 0, an unbounded cap, Bypass, a rollback — and the fold
// test proves a fold that swallowed the note has it told again.

import (
	"strings"
	"testing"

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

// assertStepNoted asserts msg carries the notice's note and nothing else past body: the content
// is body then the exact fence, and the ledger's one row is the engine note on the topic at the
// offset the fence begins, naming no Reaction.
func assertStepNoted(t *testing.T, msg domain.Message, body, line string) {
	t.Helper()

	if want := body + stepNoticeRendered(line); msg.Content != want {
		t.Errorf("noted tool message =\n%q\nwant the body then the engine fence:\n%q", msg.Content, want)
	}
	want := domain.AdviceSpan{Origin: domain.OriginEngine, Offset: len(body), Topic: stepNoticeTopic}
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
		if fe.Reaction == stepBudgetNoticeID || strings.HasPrefix(fe.Detail, "step ") {
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

	if armed := builtinIDs(a); contains(armed, stepBudgetNoticeID) {
		t.Errorf("builtins = %v, want no step-notice rung on the ladder", armed)
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
