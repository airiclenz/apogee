package agent

// P1.6 acceptance (concrete Session schema + versioning): a snapshot restores the loop's
// full quiescent-boundary state — turnIndex, the in-Exchange flag, pending input, and the
// model's preserved reasoning channel — not just the message list, so Resume continues an
// Exchange rather than restarting it. A future-version snapshot is rejected.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// TestSnapshot_RestoresTurnIndex closes the documented P0.6 gap: a snapshot taken mid-
// Exchange (after a tool Turn) restores turnIndex and the in-Exchange flag, so Resume
// CONTINUES the Exchange at the next Turn instead of re-zeroing the counter and waiting on a
// fresh Submit.
func TestSnapshot_RestoresTurnIndex(t *testing.T) {
	sink := &recordingSink{}
	ran := 0
	cfg := configWithTools(sink, fakeTool{name: "lookup", readOnly: true, ran: &ran, result: "42"})
	responder := &scriptedResponder{scripts: [][]provider.Delta{
		toolCallScript("c1", "lookup", "{}"),
		contentScript("done"),
	}}
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
	resumed := &scriptedResponder{scripts: [][]provider.Delta{contentScript("done")}}
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
	a, err := newAgent(baseConfig(&recordingSink{}), echoResponder{reply: "ack"})
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

	capt := &capturingResponder{reply: "ack"}
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
	if !containsContent(capt.got.Messages, "queued task") {
		t.Errorf("resumed Step did not consume the pending input: %+v", capt.got.Messages)
	}
}

// TestSnapshot_PreservesReasoningContent proves the model's reasoning channel is recorded on
// the committed assistant message as reasoning_content Extra and survives snapshot/resume.
func TestSnapshot_PreservesReasoningContent(t *testing.T) {
	responder := &scriptedResponder{scripts: [][]provider.Delta{{
		{Kind: provider.DeltaThinking, Thinking: "let me think"},
		{Kind: provider.DeltaContent, Content: "the answer"},
		{Kind: provider.DeltaDone, FinishReason: "stop"},
	}}}
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
	b, err := resumeAgent(baseConfig(&recordingSink{}), snap, echoResponder{reply: "x"})
	if err != nil {
		t.Fatalf("resumeAgent: %v", err)
	}
	assertReasoning(t, &b.conv)
}

// TestSnapshot_RoundTripsExchangeBoundaryForAbort pins the ADR 0017 §2 fallback: the snapshot
// keeps writing exchangeStart — the cached rollback boundary is load-bearing, because a
// mid-Exchange truncate_history fold can drop the open Exchange's opening user message, so the
// boundary cannot be re-derived on resume — and a resumed Agent's AbortExchange rolls back to
// exactly the boundary the snapshotting Agent cached, then accepts a fresh Submit.
func TestSnapshot_RoundTripsExchangeBoundaryForAbort(t *testing.T) {
	cfg := configWithTools(&recordingSink{}, fakeTool{name: "lookup", readOnly: true, result: "42"})
	responder := &scriptedResponder{scripts: [][]provider.Delta{toolCallScript("c1", "lookup", "{}")}}
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
	b, err := resumeAgent(cfg2, snap, echoResponder{reply: "unused"})
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

// TestResume_RejectsFutureVersion proves the engine refuses a snapshot newer than this build
// understands before touching its state.
func TestResume_RejectsFutureVersion(t *testing.T) {
	future := domain.Session{Version: domain.SessionVersion + 1}
	if _, err := resumeAgent(baseConfig(&recordingSink{}), future, echoResponder{}); !errors.Is(err, domain.ErrSessionVersion) {
		t.Errorf("resume of a future-version snapshot err = %v, want ErrSessionVersion", err)
	}
}
