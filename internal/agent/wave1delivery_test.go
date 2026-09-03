package agent

// Loop-level delivery tests for the Wave-1 Mechanisms riding the retry-in-place seam (R1,
// phase-4-review-fixes item 2). The Mechanisms are built through the production catalogue
// (mechanisms.Build — the same seam the config surface drives) and registered on a real
// MechanismRegistry, so these tests prove the registry-built dispatch path end-to-end through
// scripted responders: a validate correction rides the retried request and the fixed second
// response dispatches; a validate fail short-circuits the syntax/autofix cascade; the enforcer's
// retry carries the superseded narration plus the correction; an empty reply retries with the
// sim's completion-check nudge; an always-empty responder terminates at the loop cap; and both
// off-ramps still fire at dispatch under Bypass AND through a tripped Turn Budget.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/provider"
)

// wave1Nudge is the sim's first-attempt completion-check nudge (empty_recovery.go @pin),
// duplicated verbatim here so the loop-level test proves the exact wording rides the wire.
const wave1Nudge = "Your response was empty. Review the original task — there are likely remaining steps or files you haven't addressed yet. Use a tool call to continue with the next unfinished part. Do not summarize or stop until every part of the task is complete."

// wave1Registry builds a MechanismRegistry carrying the production-catalogue Mechanisms named by
// ids, so the tests exercise registry-built dispatch, not descriptor-only fakes.
func wave1Registry(t *testing.T, ids ...domain.MechanismID) *domain.MechanismRegistry {
	t.Helper()
	reg := domain.NewMechanismRegistry()
	for _, id := range ids {
		m, err := mechanisms.Build(id, mechanisms.Deps{})
		if err != nil {
			t.Fatalf("Build(%q): %v", id, err)
		}
		mustAddMech(t, reg, m)
	}
	return reg
}

// schemaTool is fakeTool with an injectable JSON schema, so validate has required parameters to
// enforce from the menu the model was shown.
type schemaTool struct {
	fakeTool
	schema string
}

func (t schemaTool) Schema() json.RawMessage { return json.RawMessage(t.schema) }

// emptyScript is a stream that finishes with no content and no tool calls — the empty reply.
func emptyScript() []provider.Delta {
	return []provider.Delta{{Kind: provider.DeltaDone, FinishReason: "stop"}}
}

// runExchange submits text and drives the Exchange to completion on an existing Agent.
func runExchange(t *testing.T, a *Agent, text string) domain.StepResult {
	t.Helper()
	if err := a.Submit(domain.UserInput{Text: text}); err != nil {
		t.Fatalf("Submit(%q): %v", text, err)
	}
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run(%q): %v", text, err)
	}
	return res
}

// wireUserIndexContaining returns the index of the first user wire message containing substr, or -1.
func wireUserIndexContaining(msgs []provider.Message, substr string) int {
	for i, m := range msgs {
		if m.Role == "user" && strings.Contains(m.Content, substr) {
			return i
		}
	}
	return -1
}

// firesBeforeStreamReset returns the MechanismFiredEvents emitted before the first
// StreamResetEvent — the fires of a retried Turn's failing pass.
func firesBeforeStreamReset(events []domain.Event) []domain.MechanismFiredEvent {
	var out []domain.MechanismFiredEvent
	for _, e := range events {
		if _, ok := e.(domain.StreamResetEvent); ok {
			break
		}
		if fe, ok := e.(domain.MechanismFiredEvent); ok {
			out = append(out, fe)
		}
	}
	return out
}

// hasFire reports whether a MechanismFiredEvent for id with action was emitted.
func hasFire(events []domain.Event, id domain.MechanismID, action string) bool {
	for _, fe := range mechanismFires(events) {
		if fe.Mechanism == id && fe.Action == action {
			return true
		}
	}
	return false
}

// fireCountFor counts the MechanismFiredEvents attributed to id.
func fireCountFor(events []domain.Event, id domain.MechanismID) int {
	n := 0
	for _, fe := range mechanismFires(events) {
		if fe.Mechanism == id {
			n++
		}
	}
	return n
}

// dispatchedCalls collects the tool calls the loop actually dispatched (ToolCallEvents).
func dispatchedCalls(events []domain.Event) []domain.ToolCall {
	var out []domain.ToolCall
	for _, e := range events {
		if te, ok := e.(domain.ToolCallEvent); ok {
			out = append(out, te.Call)
		}
	}
	return out
}

// TestWave1_RepairGuardShortCircuitsTheCascade: with syntax and autofix registered, a response the
// tool-call repair guard rejects (and whose broken-Go content would also trip syntax) retries
// immediately — the guard runs AHEAD of the whole hook cascade (ADR 0071), so the failing pass
// fires no catalogued Mechanism at all.
func TestWave1_RepairGuardShortCircuitsTheCascade(t *testing.T) {
	sink := &recordingSink{}
	writeTool := schemaTool{
		fakeTool: fakeTool{name: "write_file", result: "ok"},
		schema:   `{"type":"object","required":["path","content","mode"]}`,
	}
	cfg := configWithTools(sink, writeTool)
	cfg.Mechanisms = wave1Registry(t, "autofix", "syntax") // shuffled; Ordered sorts
	responder := &scriptedResponder{scripts: [][]provider.Delta{
		// Missing required "mode" (the repair guard rejects) AND broken Go content (syntax would fail).
		toolCallScript("c1", "write_file", `{"path":"main.go","content":"package main\nfunc main() {"}`),
		contentScript("stopping here"),
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "write the file")

	if !hasGuardFire(sink.events, guardToolCallRepair, guardActionRetry) {
		t.Fatal("the repair guard did not retry (did the retry happen at all?)")
	}
	for _, fe := range firesBeforeStreamReset(sink.events) {
		if fe.Mechanism == "syntax" || fe.Mechanism == "autofix" {
			t.Errorf("%q fired in the failing pass (action %q); the guard retry must short-circuit the cascade", fe.Mechanism, fe.Action)
		}
	}
}
