package agent

// Item 2 acceptance (the in-TUI resume primitive): RestoreSession swaps a snapshot into the
// LIVE Agent at a quiescent boundary — no rebuild — and InExchange reports whether the restored
// Session was interrupted mid-task. RestoreSession refuses mid-Exchange (ErrInputPending) and
// leaves the live conversation untouched on a corrupt or future-version snapshot, exactly as
// ClearContext and Resume do on their halves of the surface.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// containsUser reports whether the conversation holds a user-role message whose content contains
// want — enough to prove a message survived (or was dropped by) a restore without pinning the
// exact framing the loop wraps user input in.
func containsUser(conv *domain.Conversation, want string) bool {
	for i := 0; i < conv.Len(); i++ {
		if m := conv.At(i); m.Role == domain.RoleUser && strings.Contains(m.Content, want) {
			return true
		}
	}
	return false
}

// TestRestoreSession_RoundTripsAtIdle proves the live-restore primitive: snapshot a conversation,
// mutate the same Agent past it, RestoreSession the snapshot back in, and the conversation is the
// snapshot's again — then a fresh Exchange still drives to completion, proving the Agent is live
// (no rebuild).
func TestRestoreSession_RoundTripsAtIdle(t *testing.T) {
	a, err := newAgent(baseConfig(&recordingSink{}), echoResponder{reply: "first"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "one"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantLen := a.conv.Len() // user "one" + assistant "first"

	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// Mutate the LIVE conversation: a second Exchange grows it past the snapshot.
	if err := a.Submit(domain.UserInput{Text: "two"}); err != nil {
		t.Fatalf("Submit (mutate): %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run (mutate): %v", err)
	}
	if a.conv.Len() == wantLen {
		t.Fatalf("mutation did not change the conversation; the test cannot prove a restore")
	}

	// Restore the earlier snapshot into the SAME Agent — no rebuild.
	if err := a.RestoreSession(snap); err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}
	if got := a.conv.Len(); got != wantLen {
		t.Errorf("restored conversation has %d messages, want %d (the snapshot's)", got, wantLen)
	}
	if containsUser(&a.conv, "two") {
		t.Error("restored conversation still holds the mutating message \"two\"")
	}
	if !containsUser(&a.conv, "one") {
		t.Error("restored conversation lost the snapshot's message \"one\"")
	}
	if a.InExchange() {
		t.Error("InExchange is true after restoring an idle snapshot")
	}

	// Liveness: the restored Agent still drives a fresh Exchange to completion.
	if err := a.Submit(domain.UserInput{Text: "three"}); err != nil {
		t.Fatalf("Submit after restore: %v", err)
	}
	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step after restore: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("post-restore Step status = %q, want exchange-complete", res.Status)
	}
	if got := a.conv.Len(); got != wantLen+2 {
		t.Errorf("post-restore conversation has %d messages, want %d", got, wantLen+2)
	}
}

// TestRestoreSession_RefusesMidExchange proves RestoreSession is boundary-only: an Agent with an
// open Exchange refuses it with ErrInputPending and its conversation is untouched, so a
// half-streamed Turn is never orphaned (the ClearContext contract, on the restore half).
func TestRestoreSession_RefusesMidExchange(t *testing.T) {
	// A valid snapshot from a fresh (idle, empty) Agent — the payload the refused restore must
	// NOT apply.
	fresh, err := newAgent(baseConfig(&recordingSink{}), echoResponder{reply: "x"})
	if err != nil {
		t.Fatalf("newAgent (fresh): %v", err)
	}
	snap, err := fresh.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot (fresh): %v", err)
	}

	// Drive one tool-call Turn so the Exchange stays open (StatusTurnComplete, inExchange true).
	cfg := configWithTools(&recordingSink{}, fakeTool{name: "lookup", readOnly: true, result: "42"})
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
	res0, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step 0: %v", err)
	}
	if res0.Status != domain.StatusTurnComplete {
		t.Fatalf("Turn 0 status = %q, want turn-complete (Exchange open)", res0.Status)
	}
	before := a.conv.Len()

	if err := a.RestoreSession(snap); !errors.Is(err, domain.ErrInputPending) {
		t.Errorf("RestoreSession mid-Exchange err = %v, want ErrInputPending", err)
	}
	if got := a.conv.Len(); got != before {
		t.Errorf("conversation changed on a refused restore: %d, want %d (untouched)", got, before)
	}
	if !a.InExchange() {
		t.Error("the open Exchange was closed by a refused restore")
	}
}

// TestRestoreSession_RejectsFutureVersionUntouched proves a snapshot newer than this build
// understands is refused (ErrSessionVersion) and the live conversation is left intact.
func TestRestoreSession_RejectsFutureVersionUntouched(t *testing.T) {
	a := idleAgentWithHistory(t)
	before := a.conv.Len()

	future := domain.Session{Version: domain.SessionVersion + 1}
	if err := a.RestoreSession(future); !errors.Is(err, domain.ErrSessionVersion) {
		t.Errorf("RestoreSession of a future-version snapshot err = %v, want ErrSessionVersion", err)
	}
	if got := a.conv.Len(); got != before {
		t.Errorf("conversation changed on a future-version restore: %d, want %d (untouched)", got, before)
	}
}

// TestRestoreSession_RejectsCorruptPayloadUntouched proves a malformed State payload returns a
// decode error and the live conversation is left intact — the atomic-swap guarantee.
func TestRestoreSession_RejectsCorruptPayloadUntouched(t *testing.T) {
	a := idleAgentWithHistory(t)
	before := a.conv.Len()

	corrupt := domain.Session{Version: domain.SessionVersion, State: json.RawMessage(`{"conversation":`)}
	if err := a.RestoreSession(corrupt); err == nil {
		t.Error("RestoreSession of a corrupt payload returned nil, want a decode error")
	}
	if got := a.conv.Len(); got != before {
		t.Errorf("conversation changed on a corrupt restore: %d, want %d (untouched)", got, before)
	}
}

// TestInExchange_TrueForRestoredMidExchange_FalseAfterAbort proves InExchange round-trips the
// open-Exchange flag: a mid-Exchange snapshot restored into an idle Agent reports InExchange
// true, and AbortExchange clears it.
func TestInExchange_TrueForRestoredMidExchange_FalseAfterAbort(t *testing.T) {
	// Snapshot mid-Exchange (one tool-call Turn done, Exchange still open).
	cfg := configWithTools(&recordingSink{}, fakeTool{name: "lookup", readOnly: true, result: "42"})
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
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step 0: %v", err)
	}
	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// Restore into a fresh, idle Agent and confirm the flag round-tripped.
	b, err := newAgent(baseConfig(&recordingSink{}), echoResponder{reply: "x"})
	if err != nil {
		t.Fatalf("newAgent (b): %v", err)
	}
	if b.InExchange() {
		t.Fatal("a fresh Agent reports InExchange true")
	}
	if err := b.RestoreSession(snap); err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}
	if !b.InExchange() {
		t.Error("InExchange is false after restoring a mid-Exchange snapshot")
	}

	b.AbortExchange()
	if b.InExchange() {
		t.Error("InExchange is true after AbortExchange")
	}
}

// idleAgentWithHistory builds an Agent, drives one Exchange to completion, and returns it at a
// quiescent boundary with a non-empty conversation — the fixture the untouched-on-error tests
// mutate against.
func idleAgentWithHistory(t *testing.T) *Agent {
	t.Helper()
	a, err := newAgent(baseConfig(&recordingSink{}), echoResponder{reply: "reply"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if a.conv.Len() == 0 {
		t.Fatal("fixture Agent has an empty conversation")
	}
	return a
}
