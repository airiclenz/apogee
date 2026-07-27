package agent

// Agent.Interject — the human's remark committed INTO a running Exchange at the
// between-Steps boundary (plan item 2). These tests drive the real Turn state machine
// through the package's scripted-responder fakes and assert the four facts that make an
// interjection honest: it lands as a MARKED user message after the tool results, it is
// refused when no Exchange is open, its @file references resolve at DELIVERY time, and its
// fate follows the Exchange it joined (kept through a cancelled Turn and a snapshot
// round-trip, dropped with an aborted Exchange).

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// interjectAgentAtBoundary drives a scripted Agent to the between-Steps boundary of an open
// Exchange: Turn 0 asks for a tool, the tool runs, and the Step returns StatusTurnComplete
// with the Exchange still open — exactly where the worker drains its interjection mailbox.
// The returned responder has captured every request so far and is scripted to finish the
// Exchange on the next Step.
func interjectAgentAtBoundary(t *testing.T, cfg domain.Config) (*Agent, *captureAllResponder) {
	t.Helper()
	responder := &captureAllResponder{scripts: [][]provider.Delta{
		toolCallScript("c1", "lookup", `{"q":"meaning"}`),
		contentScript("all done"),
	}}
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "look it up"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step 0: %v", err)
	}
	if res.Status != domain.StatusTurnComplete {
		t.Fatalf("Turn 0 status = %q, want %q (the Exchange must stay open)", res.Status, domain.StatusTurnComplete)
	}
	return a, responder
}

// interjectConfig is the standard fixture: one read-only tool the scripted first Turn calls.
func interjectConfig(sink domain.EventSink) domain.Config {
	return configWithTools(sink, fakeTool{name: "lookup", readOnly: true, result: "the answer is 42"})
}

// TestInterjectAppendsMarkedUserMessage is the core fact: an interjection at the
// between-Steps boundary commits a RoleUser message carrying the Interjected marker, and the
// NEXT request carries it on the wire after the tool results, in order.
func TestInterjectAppendsMarkedUserMessage(t *testing.T) {
	a, responder := interjectAgentAtBoundary(t, interjectConfig(&recordingSink{}))

	// user, assistant(tool call), tool result — the tail an interjection lands after.
	if got := a.conv.Len(); got != 3 {
		t.Fatalf("conversation has %d messages at the boundary, want 3", got)
	}
	if err := a.Interject(domain.UserInput{Text: "also check the tests"}); err != nil {
		t.Fatalf("Interject: %v", err)
	}

	if got := a.conv.Len(); got != 4 {
		t.Fatalf("conversation has %d messages after Interject, want 4", got)
	}
	msg := a.conv.At(3)
	if msg.Role != domain.RoleUser {
		t.Errorf("interjected message role = %q, want %q", msg.Role, domain.RoleUser)
	}
	if msg.Content != "also check the tests" {
		t.Errorf("interjected message content = %q, want %q", msg.Content, "also check the tests")
	}
	if !msg.Interjected {
		t.Error("interjected message is not marked Interjected")
	}
	// The Exchange opening did not move: the derived Exchange still opens at the original ask.
	if idx := domain.CurrentExchange(&a.conv).UserIndex(); idx != 0 {
		t.Errorf("Exchange opening moved to index %d, want 0 (the interjection must not open one)", idx)
	}

	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step 1: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("Turn 1 status = %q, want %q", res.Status, domain.StatusExchangeComplete)
	}

	if len(responder.got) != 2 {
		t.Fatalf("responder saw %d requests, want 2", len(responder.got))
	}
	msgs := responder.got[1].Messages
	if len(msgs) < 3 {
		t.Fatalf("second request carries %d messages, want at least 3", len(msgs))
	}
	tail := msgs[len(msgs)-3:]
	if tail[0].Role != string(domain.RoleAssistant) || len(tail[0].ToolCalls) == 0 {
		t.Errorf("request tail[-3] = %+v, want the assistant message carrying tool calls", tail[0])
	}
	if tail[1].Role != string(domain.RoleTool) {
		t.Errorf("request tail[-2] role = %q, want %q", tail[1].Role, domain.RoleTool)
	}
	if tail[2].Role != string(domain.RoleUser) || tail[2].Content != "also check the tests" {
		t.Errorf("request tail[-1] = %+v, want the interjection as the last user message", tail[2])
	}
}

// TestInterjectRefusedWhenIdle proves the mirror of ErrInputPending: with no Exchange in
// flight there is nothing to interject into, so the call is refused loudly rather than
// silently promoted to a Submit — and history is left untouched.
func TestInterjectRefusedWhenIdle(t *testing.T) {
	a, err := newAgent(interjectConfig(&recordingSink{}), echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	if err := a.Interject(domain.UserInput{Text: "too early"}); !errors.Is(err, domain.ErrNoOpenExchange) {
		t.Errorf("Interject on a fresh Agent err = %v, want ErrNoOpenExchange", err)
	}
	if got := a.conv.Len(); got != 0 {
		t.Errorf("refused Interject appended %d messages, want 0", got)
	}

	// A Submit queues input but does NOT open the Exchange (the next Step does), so the
	// refusal still holds — the queued message is the one that opens it.
	if err := a.Submit(domain.UserInput{Text: "the real ask"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if err := a.Interject(domain.UserInput{Text: "still too early"}); !errors.Is(err, domain.ErrNoOpenExchange) {
		t.Errorf("Interject after Submit err = %v, want ErrNoOpenExchange", err)
	}
	if got := a.conv.Len(); got != 0 {
		t.Errorf("refused Interject appended %d messages, want 0", got)
	}
}

// TestInterjectRefusesEmptyInput pins the second refusal: an input with no text, no @file
// references, and no attached skills would commit a blank user message wedged after the
// tool results — noise on the wire and invisible in the transcript.
func TestInterjectRefusesEmptyInput(t *testing.T) {
	a, _ := interjectAgentAtBoundary(t, interjectConfig(&recordingSink{}))

	before := a.conv.Len()
	if err := a.Interject(domain.UserInput{}); !errors.Is(err, errEmptyInterjection) {
		t.Errorf("Interject with empty input err = %v, want errEmptyInterjection", err)
	}
	if got := a.conv.Len(); got != before {
		t.Errorf("refused Interject appended %d messages, want 0", got-before)
	}
}

// TestInterjectResolvesFileRefs proves references resolve at DELIVERY, not at typing time:
// the fixture is written only AFTER the Exchange opened, and its content still lands in the
// interjected message.
func TestInterjectResolvesFileRefs(t *testing.T) {
	dir := t.TempDir()
	cfg := interjectConfig(&recordingSink{})
	cfg.WorkspaceDir = dir

	a, _ := interjectAgentAtBoundary(t, cfg)

	// Written mid-Exchange: a Submit-time resolution could not have seen this.
	writeWorkspaceFile(t, dir, "notes.txt", "FRESH CONTENT 7")

	if err := a.Interject(domain.UserInput{Text: "and this file", FileRefs: []string{"notes.txt"}}); err != nil {
		t.Fatalf("Interject: %v", err)
	}
	got := a.conv.At(a.conv.Len() - 1).Content
	if !strings.Contains(got, "FRESH CONTENT 7") {
		t.Errorf("resolved file content missing from the interjection:\n%s", got)
	}
	if !strings.Contains(got, "and this file") {
		t.Errorf("interjection text missing from the message:\n%s", got)
	}
	if !strings.HasSuffix(got, "and this file") {
		t.Errorf("ref block must precede the text (loop order); got:\n%s", got)
	}
}

// TestInterjectSurvivesCancelledTurn pins the rollback fate: the cancel boundary is armed
// when the NEXT Turn builds its request, so an interjection delivered before it precedes
// t.rollback and is kept — a stopped Turn never eats the human's remark.
func TestInterjectSurvivesCancelledTurn(t *testing.T) {
	responder := &blockAtResponder{
		scripts: [][]provider.Delta{toolCallScript("c1", "lookup", "{}")},
		blockAt: 1,
		started: make(chan struct{}),
	}
	a, err := newAgent(interjectConfig(&recordingSink{}), responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "look it up"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if res, err := a.Step(context.Background()); err != nil || res.Status != domain.StatusTurnComplete {
		t.Fatalf("Step 0 = %+v, %v; want StatusTurnComplete", res, err)
	}

	if err := a.Interject(domain.UserInput{Text: "wait — also the tests"}); err != nil {
		t.Fatalf("Interject: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-responder.started
		cancel()
	}()
	res, err := a.Step(ctx)
	if err != nil {
		t.Fatalf("Step 1 returned a loop error on cancel: %v", err)
	}
	if res.Status != domain.StatusCancelled {
		t.Fatalf("Step 1 status = %q, want %q", res.Status, domain.StatusCancelled)
	}

	last := a.conv.At(a.conv.Len() - 1)
	if !last.Interjected || last.Content != "wait — also the tests" {
		t.Errorf("the cancelled Turn's rollback dropped the interjection; tail = %+v", last)
	}
}

// TestAbortExchangeDropsInterjections pins the other fate: scrapping the Exchange scraps
// everything committed into it, the interjection included. The transcript keeps the visual
// record; the model's history does not.
func TestAbortExchangeDropsInterjections(t *testing.T) {
	a, _ := interjectAgentAtBoundary(t, interjectConfig(&recordingSink{}))

	if err := a.Interject(domain.UserInput{Text: "one more thing"}); err != nil {
		t.Fatalf("Interject: %v", err)
	}
	a.AbortExchange()

	if got := a.conv.Len(); got != 0 {
		t.Errorf("AbortExchange left %d messages, want 0 (the whole Exchange is scrapped)", got)
	}
	if a.InExchange() {
		t.Error("AbortExchange left the Exchange open")
	}
}

// TestInterjectPersistsAcrossSnapshotRestore proves the marker is durable: it rides the
// conversation's own marshal into a Session and comes back marked, so a session saved
// mid-task and restored later still derives the same Exchange opening.
func TestInterjectPersistsAcrossSnapshotRestore(t *testing.T) {
	a, _ := interjectAgentAtBoundary(t, interjectConfig(&recordingSink{}))

	if err := a.Interject(domain.UserInput{Text: "carry me across"}); err != nil {
		t.Fatalf("Interject: %v", err)
	}
	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	b, err := newAgent(interjectConfig(&recordingSink{}), echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent (restore target): %v", err)
	}
	if err := b.RestoreSession(snap); err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}

	if got := b.conv.Len(); got != 4 {
		t.Fatalf("restored conversation has %d messages, want 4", got)
	}
	restored := b.conv.At(3)
	if !restored.Interjected {
		t.Error("the restored message lost its Interjected marker")
	}
	if restored.Content != "carry me across" {
		t.Errorf("restored content = %q, want %q", restored.Content, "carry me across")
	}
	if idx := domain.CurrentExchange(&b.conv).UserIndex(); idx != 0 {
		t.Errorf("restored Exchange opening = %d, want 0", idx)
	}
	if !b.InExchange() {
		t.Error("the restored Agent does not report the open Exchange")
	}
}
