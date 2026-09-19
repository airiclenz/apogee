package agent

// P1.6 acceptance (concrete Session schema + versioning): a snapshot restores the loop's
// full quiescent-boundary state — turnIndex, the in-Exchange flag, pending input, and the
// model's preserved reasoning channel — not just the message list, so Resume continues an
// Exchange rather than restarting it. A future-version snapshot is rejected.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tasklist"
)

// TestSnapshot_RestoresTurnIndex closes the documented P0.6 gap: a snapshot taken mid-
// Exchange (after a tool Turn) restores turnIndex and the in-Exchange flag, so Resume
// CONTINUES the Exchange at the next Turn instead of re-zeroing the counter and waiting on a
// fresh Submit.
func TestSnapshot_RestoresTurnIndex(t *testing.T) {
	sink := &recordingSink{}
	ran := 0
	cfg := configWithTools(sink, fakeTool{name: "lookup", readOnly: true, ran: &ran, result: "42"})
	responder := scriptedResponder(t,
		toolCallTurn("c1", "lookup", "{}"),
		contentTurn("done"),
	)
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "look it up"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// Turn 0: the model asks for a tool; the Exchange stays open and turnIndex advances.
	res0, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step 0: %v", err)
	}
	if res0.Status != domain.StatusTurnComplete || res0.TurnIndex != 0 {
		t.Fatalf("Turn 0 = (%q, %d), want (turn-complete, 0)", res0.Status, res0.TurnIndex)
	}

	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// Resume into a fresh Agent whose responder continues from the finish reply.
	sink2 := &recordingSink{}
	cfg2 := configWithTools(sink2, fakeTool{name: "lookup", readOnly: true, result: "42"})
	resumed := scriptedResponder(t, contentTurn("done"))
	b, err := resumeAgent(cfg2, snap, resumed)
	if err != nil {
		t.Fatalf("resumeAgent: %v", err)
	}

	// A Submit must be rejected — the Exchange is still open (inExchange survived).
	if err := b.Submit(domain.UserInput{Text: "intrude"}); err == nil {
		t.Error("Submit mid-Exchange after resume was accepted; inExchange was not restored")
	}

	// The next Step continues the Exchange at Turn 1, not a re-zeroed Turn 0.
	res1, err := b.Step(context.Background())
	if err != nil {
		t.Fatalf("Step (resumed): %v", err)
	}
	if res1.TurnIndex != 1 {
		t.Errorf("resumed Step TurnIndex = %d, want 1 (continued, not re-zeroed)", res1.TurnIndex)
	}
	if res1.Status != domain.StatusExchangeComplete {
		t.Errorf("resumed Step status = %q, want exchange-complete", res1.Status)
	}
}

// TestSnapshot_RestoresPendingInput proves a Submit→Snapshot→Resume sequence (no Step in
// between) does not silently drop the queued input: the resumed Agent's first Step consumes
// it and sends it upstream.
func TestSnapshot_RestoresPendingInput(t *testing.T) {
	a, err := newAgent(baseConfig(&recordingSink{}), echoResponder(t, "ack"))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "queued task"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// Snapshot BEFORE stepping — the input is pending, not yet in the conversation.
	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	capt := echoResponder(t, "ack")
	b, err := resumeAgent(baseConfig(&recordingSink{}), snap, capt)
	if err != nil {
		t.Fatalf("resumeAgent: %v", err)
	}
	// A Submit must be rejected — the restored input is still queued.
	if err := b.Submit(domain.UserInput{Text: "second"}); err == nil {
		t.Error("Submit was accepted while a restored input was pending")
	}
	if _, err := b.Step(context.Background()); err != nil {
		t.Fatalf("Step (resumed): %v", err)
	}
	if !containsContent(capt.last().Messages, "queued task") {
		t.Errorf("resumed Step did not consume the pending input: %+v", capt.last().Messages)
	}
}

// TestSnapshot_PreservesReasoningContent proves the model's reasoning channel is recorded on
// the committed assistant message as reasoning_content Extra and survives snapshot/resume.
func TestSnapshot_PreservesReasoningContent(t *testing.T) {
	responder := scriptedResponder(t, stubllm.Turn{Reasoning: "let me think", Text: "the answer"})
	a, err := newAgent(baseConfig(&recordingSink{}), responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "q"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	assertReasoning := func(t *testing.T, conv *domain.Conversation) {
		t.Helper()
		last := conv.At(conv.Len() - 1) // the committed assistant message
		if v, ok := last.Extra("reasoning_content"); !ok || string(v) != `"let me think"` {
			t.Errorf("assistant reasoning_content = %q ok=%v, want \"let me think\"", v, ok)
		}
	}
	assertReasoning(t, &a.conv)

	// It survives the snapshot/resume round-trip.
	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	b, err := resumeAgent(baseConfig(&recordingSink{}), snap, echoResponder(t, "x"))
	if err != nil {
		t.Fatalf("resumeAgent: %v", err)
	}
	assertReasoning(t, &b.conv)
}

// TestSnapshot_RoundTripsExchangeBoundaryForAbort pins the ADR 0017 §2 fallback: the snapshot
// keeps writing exchangeStart — the cached rollback boundary is load-bearing, because a
// mid-Exchange history rewrite (a lab `HistoryRewriter`) can drop the open Exchange's opening user message, so the
// boundary cannot be re-derived on resume — and a resumed Agent's AbortExchange rolls back to
// exactly the boundary the snapshotting Agent cached, then accepts a fresh Submit.
func TestSnapshot_RoundTripsExchangeBoundaryForAbort(t *testing.T) {
	cfg := configWithTools(&recordingSink{}, fakeTool{name: "lookup", readOnly: true, result: "42"})
	responder := scriptedResponder(t, toolCallTurn("c1", "lookup", "{}"))
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	// Prior history, so the Exchange boundary sits past index 0 and an over-drop is detectable.
	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: "prior question"})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, Content: "prior answer"})

	if err := a.Submit(domain.UserInput{Text: "look it up"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Step(context.Background()) // a tool Turn: the Exchange stays open
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if res.Status != domain.StatusTurnComplete {
		t.Fatalf("Turn status = %q, want %q (a tool Turn keeps the Exchange open)", res.Status, domain.StatusTurnComplete)
	}
	boundary := a.exchangeBoundary() // where "look it up" was appended
	if boundary != 2 {
		t.Fatalf("exchangeBoundary() = %d, want 2 (just past the prior history)", boundary)
	}

	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	// The schema still carries the boundary — the deepening plan's 4(b) stop-writing change did
	// NOT proceed, so its absence here would be a regression, not a cleanup.
	var st struct {
		ExchangeStart *int `json:"exchangeStart"`
	}
	if err := json.Unmarshal(snap.State, &st); err != nil {
		t.Fatalf("Unmarshal snapshot state: %v", err)
	}
	if st.ExchangeStart == nil || *st.ExchangeStart != boundary {
		t.Fatalf("snapshot exchangeStart = %v, want %d (the boundary must round-trip)", st.ExchangeStart, boundary)
	}

	cfg2 := configWithTools(&recordingSink{}, fakeTool{name: "lookup", readOnly: true, result: "42"})
	b, err := resumeAgent(cfg2, snap, echoResponder(t, "unused"))
	if err != nil {
		t.Fatalf("resumeAgent: %v", err)
	}
	b.AbortExchange()
	if b.conv.Len() != boundary {
		t.Fatalf("after abort conv.Len() = %d, want %d (rolled back to the cached boundary)", b.conv.Len(), boundary)
	}
	if got := b.conv.At(boundary - 1); got.Content != "prior answer" {
		t.Errorf("message before the boundary = %+v, want the prior history intact", got)
	}
	if err := b.Submit(domain.UserInput{Text: "next"}); err != nil {
		t.Errorf("Submit after abort: %v, want accepted (the aborted Exchange closed)", err)
	}
}

// TestAgentState_EncodesStableKeyNames pins the serialized key names of the v1 session-state
// schema so a field relocation (e.g. onto turnLifecycle) cannot silently rename the JSON the
// version gate (domain.SessionVersion) assumes stable. It marshals a fully-populated agentState
// — every omitempty field non-zero — and asserts each documented key is present under its name.
func TestAgentState_EncodesStableKeyNames(t *testing.T) {
	raw, err := json.Marshal(agentState{
		Conversation:  domain.NewConversation(nil),
		TurnIndex:     3,
		InExchange:    true,
		ExchangeStart: 2,
		PendingInput:  &domain.UserInput{Text: "queued"},
		Tasks:         []tasklist.Item{{Text: "the model's own checklist"}},
	})
	if err != nil {
		t.Fatalf("marshal agentState: %v", err)
	}
	var keyed map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keyed); err != nil {
		t.Fatalf("unmarshal encoded state to keys: %v", err)
	}
	for _, key := range []string{"conversation", "turnIndex", "inExchange", "exchangeStart", "pendingInput", "tasks"} {
		if _, ok := keyed[key]; !ok {
			t.Errorf("encoded session state missing key %q (the schema is version-gated and must stay byte-compatible)", key)
		}
	}
}

// TestResume_RejectsFutureVersion proves the engine refuses a snapshot newer than this build
// understands before touching its state.
func TestResume_RejectsFutureVersion(t *testing.T) {
	future := domain.Session{Version: domain.SessionVersion + 1}
	if _, err := resumeAgent(baseConfig(&recordingSink{}), future, echoResponder(t, "")); !errors.Is(err, domain.ErrSessionVersion) {
		t.Errorf("resume of a future-version snapshot err = %v, want ErrSessionVersion", err)
	}
}

// ---------------------------------------------------------------------------
// The task list is session state (ADR 0072)
// ---------------------------------------------------------------------------

// newSnapshotAgent builds a plain top-level Agent for the snapshot tests below — no scripted
// Turns, because what they assert is what a snapshot carries rather than how it was produced.
func newSnapshotAgent(t *testing.T) *Agent {
	t.Helper()

	a, err := newAgent(baseConfig(&recordingSink{}), scriptedResponder(t))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	return a
}

// TestSnapshot_RoundTripsTheTaskList is the reason the list is session state at all: a resumed
// session still knows what is left. The done flags ride with it, so a resumed model sees which
// rows it had already ticked off rather than a list of everything it ever planned.
func TestSnapshot_RoundTripsTheTaskList(t *testing.T) {
	a := newSnapshotAgent(t)
	want := []tasklist.Item{
		{Text: "read the plan", Done: true},
		{Text: "write the code"},
		{Text: "run the tests"},
	}
	if err := a.tasks.Replace(want); err != nil {
		t.Fatalf("seed the list: %v", err)
	}

	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	b, err := resumeAgent(baseConfig(&recordingSink{}), snap, scriptedResponder(t))
	if err != nil {
		t.Fatalf("resumeAgent: %v", err)
	}

	got := b.tasks.Items()
	if len(got) != len(want) {
		t.Fatalf("the resumed task list = %v, want %v", got, want)
	}
	for i, item := range want {
		if got[i] != item {
			t.Fatalf("the resumed task list = %v, want %v", got, want)
		}
	}
}

// TestSnapshot_OmitsTheTaskListKeyWhenEmpty pins the omitempty half of the additive-both-ways
// claim that lets domain.SessionVersion stay 1: an engine whose model never wrote a list writes a
// payload byte-identical to the one this schema had before the field existed.
func TestSnapshot_OmitsTheTaskListKeyWhenEmpty(t *testing.T) {
	snap, err := newSnapshotAgent(t).Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	var keyed map[string]json.RawMessage
	if err := json.Unmarshal(snap.State, &keyed); err != nil {
		t.Fatalf("unmarshal the snapshot payload to keys: %v", err)
	}
	if raw, ok := keyed["tasks"]; ok {
		t.Errorf("a snapshot of an empty list carries a %q key (%s), want it omitted", "tasks", raw)
	}
}

// TestRestore_WithoutATaskListEmptiesTheHeldOne is the other direction of the same additivity, and
// the regression guard RestoreSession needs: a snapshot from before the field existed — or one
// taken with an empty list — restores an EMPTY list rather than leaving the outgoing session's
// checklist standing under a conversation that knows nothing about it. restoreState's
// unconditional Replace is that clear; there is no reset beside the console close.
func TestRestore_WithoutATaskListEmptiesTheHeldOne(t *testing.T) {
	a := newSnapshotAgent(t)
	if err := a.tasks.Replace([]tasklist.Item{{Text: "the outgoing session's work"}}); err != nil {
		t.Fatalf("seed the list: %v", err)
	}

	if err := a.RestoreSession(domain.Session{
		Version: domain.SessionVersion,
		State:   json.RawMessage(`{"conversation":{"messages":[]},"turnIndex":0}`),
	}); err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}

	if items := a.tasks.Items(); len(items) != 0 {
		t.Errorf("the task list after restoring a snapshot with no tasks = %v, want empty", items)
	}
}

// TestRestore_RejectsAnOverCapTaskList pins that the caps are enforced at the restore seam too: a
// hand-edited or corrupt snapshot carrying more tasks than the list allows is a decode ERROR, not
// a list silently truncated behind the model's back — and the refusal leaves the live list exactly
// as it was, which is what keeps restoreSnapshot's no-partial-swap promise.
func TestRestore_RejectsAnOverCapTaskList(t *testing.T) {
	a := newSnapshotAgent(t)
	if err := a.tasks.Replace([]tasklist.Item{{Text: "the list that must survive the refusal"}}); err != nil {
		t.Fatalf("seed the list: %v", err)
	}

	oversized := make([]tasklist.Item, tasklist.MaxItems+1)
	for i := range oversized {
		oversized[i] = tasklist.Item{Text: fmt.Sprintf("task %d", i)}
	}
	state, err := json.Marshal(agentState{Conversation: domain.NewConversation(nil), Tasks: oversized})
	if err != nil {
		t.Fatalf("marshal the oversized payload: %v", err)
	}

	if err := a.RestoreSession(domain.Session{Version: domain.SessionVersion, State: state}); err == nil {
		t.Fatal("RestoreSession of an over-cap task list = nil, want a decode error")
	}
	assertTaskTexts(t, a.tasks, "the list that must survive the refusal")
}

// TestRestore_AnEmptyPayloadClearsTheLiveSession pins the seam restoreState's empty-payload branch
// exists for. apogee's own Snapshot never writes an empty payload — encodeState always emits a
// conversation — but a truncated, hand-edited or foreign session file can, and RestoreSession
// reads it against a LIVE Agent that still holds the OUTGOING session. Returning early there would
// leave that session's conversation, Turn counters, pending input and task list standing
// underneath the incoming session's file, with no error for the host to see and the /sessions flow
// already redirecting saves into it. An empty payload MEANS a never-stepped Agent, so that is what
// gets restored — the same place the fresh-Agent Resume path lands.
func TestRestore_AnEmptyPayloadClearsTheLiveSession(t *testing.T) {
	a := newSnapshotAgent(t)
	if err := a.tasks.Replace([]tasklist.Item{{Text: "the outgoing session's work"}}); err != nil {
		t.Fatalf("seed the list: %v", err)
	}
	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: "the outgoing session's history"})
	a.turns.restore(turnSnapshot{index: 7, pendingInput: &domain.UserInput{Text: "the outgoing session's queued input"}})

	if err := a.RestoreSession(domain.Session{Version: domain.SessionVersion}); err != nil {
		t.Fatalf("RestoreSession of an empty payload: %v", err)
	}

	if items := a.tasks.Items(); len(items) != 0 {
		t.Errorf("the task list after restoring an empty payload = %v, want empty", items)
	}
	if msgs := a.conv.Messages(); len(msgs) != 0 {
		t.Errorf("the conversation after restoring an empty payload = %d messages, want 0", len(msgs))
	}
	if a.turns.index != 0 {
		t.Errorf("the Turn index after restoring an empty payload = %d, want 0", a.turns.index)
	}
	if a.turns.pendingInput != nil {
		t.Errorf("the pending input after restoring an empty payload = %v, want nil", a.turns.pendingInput)
	}
}

// TestRestoreSessionClearsCompactionLatches: the two automatic-fold latches judged the OUTGOING
// conversation — a fold that faulted against it, a fold that could not bring it under the
// allocation — so a session restored over a stood-down Agent must not stay stood down. Before
// turnLifecycle.restore owned the reset, RestoreSession left both latched, so the first over-budget
// boundary of the incoming session never folded. The context-fill ladder re-arms with them.
func TestRestoreSessionClearsCompactionLatches(t *testing.T) {
	a := newSnapshotAgent(t)
	a.turns.foldFaulted()
	a.turns.foldSaturated()
	a.turns.noteFill(50)

	if err := a.RestoreSession(domain.Session{Version: domain.SessionVersion}); err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}

	if a.turns.compactFailed {
		t.Error("compactFailed still latched after RestoreSession — the incoming session inherits a stand-down it never earned")
	}
	if a.turns.compactSat {
		t.Error("compactSat still latched after RestoreSession — the incoming session inherits a saturation it never measured")
	}
	if a.turns.fillRung != 0 {
		t.Errorf("fillRung = %d after RestoreSession, want 0 — the ladder climbed the conversation just swapped out", a.turns.fillRung)
	}
}

// ---------------------------------------------------------------------------
// CutSession — the fork primitive cuts a snapshot at an earlier Exchange
// ---------------------------------------------------------------------------

// cutFixtureSession encodes a hand-built loop state as a current-version Session, so the cut tests
// assert what CutSession does to a payload rather than how an Agent produced it.
func cutFixtureSession(t *testing.T, st agentState) domain.Session {
	t.Helper()

	state, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("encode the fixture state: %v", err)
	}
	return domain.Session{Version: domain.SessionVersion, State: state}
}

// decodeCutState reads a cut Session back into the loop state it carries.
func decodeCutState(t *testing.T, snap domain.Session) agentState {
	t.Helper()

	var st agentState
	if err := json.Unmarshal(snap.State, &st); err != nil {
		t.Fatalf("decode the cut state: %v", err)
	}
	if st.Conversation == nil {
		t.Fatal("the cut state carries no conversation")
	}
	return st
}

// threeExchanges is a history of three Exchanges: the first a plain reply, the second a tool call
// with an interjection landing mid-Exchange, the third a plain reply.
func threeExchanges() []domain.Message {
	return []domain.Message{
		{Role: domain.RoleUser, Content: "first"},
		{Role: domain.RoleAssistant, Content: "first reply"},
		{Role: domain.RoleUser, Content: "second"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "lookup", Arguments: json.RawMessage(`{}`)}}},
		{Role: domain.RoleTool, ToolCallID: "c1", Content: "42"},
		{Role: domain.RoleUser, Content: "also check the date", Interjected: true},
		{Role: domain.RoleAssistant, Content: "second reply"},
		{Role: domain.RoleUser, Content: "third"},
		{Role: domain.RoleAssistant, Content: "third reply"},
	}
}

// contents lists the Content of every message in conv, the shape the cut assertions compare.
func contents(conv *domain.Conversation) []string {
	out := make([]string, 0, conv.Len())
	conv.Range(func(_ int, m domain.Message) bool {
		out = append(out, m.Content)
		return true
	})
	return out
}

// TestCutSessionDropsTheLastExchanges is the cut itself: counted from the end, drop 1 ends the
// history at the second Exchange's final assistant message — the interjection inside it opened
// nothing and stays — and drop 2 ends it at the first's; either way the result is an idle boundary
// with an empty task list.
func TestCutSessionDropsTheLastExchanges(t *testing.T) {
	t.Parallel()

	snap := cutFixtureSession(t, agentState{
		Conversation: domain.NewConversation(threeExchanges()),
		TurnIndex:    5,
		Tasks:        []tasklist.Item{{Text: "finish the third"}},
	})
	cases := []struct {
		drop int
		want []string
	}{
		{drop: 1, want: []string{"first", "first reply", "second", "", "42", "also check the date", "second reply"}},
		{drop: 2, want: []string{"first", "first reply"}},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("drop %d", tc.drop), func(t *testing.T) {
			cut, err := CutSession(snap, tc.drop)
			if err != nil {
				t.Fatalf("CutSession(%d): %v", tc.drop, err)
			}

			st := decodeCutState(t, cut)
			if got := contents(st.Conversation); fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Errorf("cut history = %q, want %q", got, tc.want)
			}
			if st.InExchange || st.ExchangeStart != 0 || st.PendingInput != nil {
				t.Errorf("cut state = (inExchange %v, exchangeStart %d, pendingInput %v), want an idle boundary",
					st.InExchange, st.ExchangeStart, st.PendingInput)
			}
			if len(st.Tasks) != 0 {
				t.Errorf("cut task list = %v, want it cleared", st.Tasks)
			}
			if st.TurnIndex != 5 {
				t.Errorf("cut turnIndex = %d, want the parent's 5 carried over", st.TurnIndex)
			}
		})
	}
}

// TestCutSessionCountsTheBridgeAsAnOpening pins the from-the-end rule on a folded history: after a
// fold the history is [first user, summary, bridge, …] where the bridge is a RoleUser message with
// no transcript entry, and counted from the end it is an opening like any other — so drop 1 removes
// only the last post-bridge Exchange and the bridge's own Exchange survives.
func TestCutSessionCountsTheBridgeAsAnOpening(t *testing.T) {
	t.Parallel()

	snap := cutFixtureSession(t, agentState{Conversation: domain.NewConversation([]domain.Message{
		{Role: domain.RoleUser, Content: "first"},
		{Role: domain.RoleAssistant, Content: "summary of the folded stretch"},
		{Role: domain.RoleUser, Content: overflowBridge},
		{Role: domain.RoleAssistant, Content: "continuing after the fold"},
		{Role: domain.RoleUser, Content: "after the bridge"},
		{Role: domain.RoleAssistant, Content: "last reply"},
	})})

	cut, err := CutSession(snap, 1)
	if err != nil {
		t.Fatalf("CutSession(1): %v", err)
	}

	want := []string{"first", "summary of the folded stretch", overflowBridge, "continuing after the fold"}
	if got := contents(decodeCutState(t, cut).Conversation); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("cut history = %q, want %q", got, want)
	}
}

// TestCutSessionZeroIsAClone pins the drop-0 contract a fork at the newest prompt relies on: the
// message history is untouched, yet the normalisation still applies — tasks cleared, no pending
// input, no open Exchange, no deferred correction — exactly as at any earlier prompt.
func TestCutSessionZeroIsAClone(t *testing.T) {
	t.Parallel()

	conv := domain.NewConversation(threeExchanges())
	conv.Defer("a stale correction")
	snap := cutFixtureSession(t, agentState{
		Conversation:  conv,
		TurnIndex:     3,
		InExchange:    true,
		ExchangeStart: 7,
		PendingInput:  &domain.UserInput{Text: "queued"},
		Tasks:         []tasklist.Item{{Text: "still open"}},
	})

	cut, err := CutSession(snap, 0)
	if err != nil {
		t.Fatalf("CutSession(0): %v", err)
	}

	st := decodeCutState(t, cut)
	if got, want := contents(st.Conversation), contents(domain.NewConversation(threeExchanges())); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("cut history = %q, want the parent's %q", got, want)
	}
	if st.InExchange || st.ExchangeStart != 0 || st.PendingInput != nil || len(st.Tasks) != 0 {
		t.Errorf("cut state = (inExchange %v, exchangeStart %d, pendingInput %v, tasks %v), want an idle boundary with no tasks",
			st.InExchange, st.ExchangeStart, st.PendingInput, st.Tasks)
	}
	if st.Conversation.DeferredLen() != 0 {
		t.Errorf("cut state keeps %d deferred corrections, want none", st.Conversation.DeferredLen())
	}
}

// TestCutSessionRejectsOutOfRange: a negative count and one that would drop every opening are
// refused with an error naming both counts, and the caller's snapshot is untouched.
func TestCutSessionRejectsOutOfRange(t *testing.T) {
	t.Parallel()

	snap := cutFixtureSession(t, agentState{Conversation: domain.NewConversation(threeExchanges())})
	for _, drop := range []int{-1, 3, 4} {
		_, err := CutSession(snap, drop)
		if err == nil {
			t.Errorf("CutSession(%d) accepted, want a refusal", drop)
			continue
		}
		if want := fmt.Sprintf("drop %d of 3 exchanges", drop); !strings.Contains(err.Error(), want) {
			t.Errorf("CutSession(%d) err = %q, want it to name %q", drop, err, want)
		}
	}
}

// TestCutSessionRejectsAForeignVersion: a snapshot newer than this build understands is refused
// exactly as Resume refuses it, before its payload is read.
func TestCutSessionRejectsAForeignVersion(t *testing.T) {
	t.Parallel()

	future := domain.Session{Version: domain.SessionVersion + 1, State: json.RawMessage(`{"conversation":{"messages":[]}}`)}
	if _, err := CutSession(future, 0); !errors.Is(err, domain.ErrSessionVersion) {
		t.Errorf("CutSession(future) err = %v, want ErrSessionVersion", err)
	}
}

// TestCutSessionResumes closes the loop: the cut Session is a well-formed snapshot, so Resume
// rebuilds an idle Agent over the kept prefix that accepts a fresh Submit.
func TestCutSessionResumes(t *testing.T) {
	snap := cutFixtureSession(t, agentState{
		Conversation: domain.NewConversation(threeExchanges()),
		InExchange:   true,
		PendingInput: &domain.UserInput{Text: "queued"},
	})
	cut, err := CutSession(snap, 1)
	if err != nil {
		t.Fatalf("CutSession(1): %v", err)
	}

	a, err := resumeAgent(baseConfig(&recordingSink{}), cut, scriptedResponder(t))
	if err != nil {
		t.Fatalf("resumeAgent(cut): %v", err)
	}

	if a.conv.Len() != 7 || a.InExchange() {
		t.Errorf("resumed Agent = (%d messages, inExchange %v), want the 7-message prefix at idle", a.conv.Len(), a.InExchange())
	}
	if err := a.Submit(domain.UserInput{Text: "a fresh prompt on the fork"}); err != nil {
		t.Errorf("Submit on the resumed cut: %v", err)
	}
}

// TestSnapshot_NeverCarriesRetainedDelegates pins ADR 0022 D8 for the capped delegations a parent
// retains (plan 2026-09-18 - 00, item 8): they live in memory for one Exchange and the session
// payload is byte-identical with and without them.
func TestSnapshot_NeverCarriesRetainedDelegates(t *testing.T) {
	sink := &recordingSink{}
	a, err := newAgent(baseConfig(sink), scriptedResponder(t))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	before, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	a.retained.retain(retainedDelegate{task: "trawl the repo", name: "Repo Survey", fold: "the fold", spawnCallID: "c1"})
	after, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot with a retained delegation: %v", err)
	}

	if string(before.State) != string(after.State) {
		t.Errorf("session state changed once a delegation was retained:\nbefore = %s\nafter  = %s", before.State, after.State)
	}
}
