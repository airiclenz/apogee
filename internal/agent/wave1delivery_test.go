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

// TestWave1_ValidateRetryCarriesCorrectionThenDispatchesFixed: a bad tool call (missing required
// parameter) makes validate retry in place — the retried request carries the superseded assistant
// call and the correction — and the scripted second (fixed) response is the one dispatched.
func TestWave1_ValidateRetryCarriesCorrectionThenDispatchesFixed(t *testing.T) {
	sink := &recordingSink{}
	ran := 0
	lookup := schemaTool{
		fakeTool: fakeTool{name: "lookup", readOnly: true, ran: &ran, result: "42"},
		schema:   `{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}`,
	}
	cfg := configWithTools(sink, lookup)
	cfg.Mechanisms = wave1Registry(t, "validate")
	responder := &captureAllResponder{scripts: [][]provider.Delta{
		toolCallScript("c1", "lookup", `{}`),        // missing required "q" — validate retries
		toolCallScript("c2", "lookup", `{"q":"x"}`), // the corrected call — dispatches
		contentScript("done"),
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "look it up")

	if len(responder.got) != 3 {
		t.Fatalf("provider was called %d times, want 3 (draft, retry, final)", len(responder.got))
	}
	second := responder.got[1].Messages
	ai := wireMessageIndex(second, "assistant", "")
	if ai < 0 {
		t.Fatalf("retried request carries no superseded assistant message: %+v", second)
	}
	if tc := second[ai].ToolCalls; len(tc) != 1 || tc[0].ID != "c1" {
		t.Errorf("superseded assistant tool calls = %+v, want the draft's c1 call", tc)
	}
	ci := wireUserIndexContaining(second, "Your previous tool call had errors")
	if ci != ai+1 {
		t.Errorf("correction at index %d, want %d (immediately after the superseded assistant)", ci, ai+1)
	}
	if wireUserIndexContaining(second, `missing required parameter "q"`) < 0 {
		t.Errorf("retried request correction does not name the missing parameter: %+v", second)
	}

	// The fixed call — not the superseded one — is what dispatched.
	calls := dispatchedCalls(sink.events)
	if len(calls) != 1 || calls[0].ID != "c2" || string(calls[0].Arguments) != `{"q":"x"}` {
		t.Errorf("dispatched calls = %+v, want only the corrected c2 call", calls)
	}
	if ran != 1 {
		t.Errorf("tool ran %d times, want 1", ran)
	}
	if me, ok := lastMessageEvent(sink.events); !ok || me.Text != "done" {
		t.Errorf("final MessageEvent = %+v (ok=%v), want %q", me, ok, "done")
	}
	// Request-scoped: the corrective exchange never committed to history.
	// user, assistant (c2 call), tool result, assistant final = 4 messages.
	if got := a.conv.Len(); got != 4 {
		t.Errorf("committed history has %d messages, want 4", got)
	}
}

// TestWave1_ValidateFailShortCircuitsCascade: with the full validate → syntax → autofix cascade
// registered, a response that fails validate (and whose broken-Go content would also trip syntax)
// retries immediately — the failing pass fires validate only, never reaching syntax or autofix.
func TestWave1_ValidateFailShortCircuitsCascade(t *testing.T) {
	sink := &recordingSink{}
	writeTool := schemaTool{
		fakeTool: fakeTool{name: "write_file", result: "ok"},
		schema:   `{"type":"object","required":["path","content","mode"]}`,
	}
	cfg := configWithTools(sink, writeTool)
	cfg.Mechanisms = wave1Registry(t, "autofix", "validate", "syntax") // shuffled; Ordered sorts
	responder := &scriptedResponder{scripts: [][]provider.Delta{
		// Missing required "mode" (validate fails) AND broken Go content (syntax would fail).
		toolCallScript("c1", "write_file", `{"path":"main.go","content":"package main\nfunc main() {"}`),
		contentScript("stopping here"),
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "write the file")

	failing := firesBeforeStreamReset(sink.events)
	if len(failing) == 0 {
		t.Fatal("no MechanismFiredEvent in the failing pass (did the retry happen at all?)")
	}
	sawValidateRetry := false
	for _, fe := range failing {
		switch fe.Mechanism {
		case "validate":
			if fe.Action == string(domain.ActionRetry) {
				sawValidateRetry = true
			}
		case "syntax", "autofix":
			t.Errorf("%q fired in the failing pass (action %q); the validate retry must short-circuit the cascade", fe.Mechanism, fe.Action)
		}
	}
	if !sawValidateRetry {
		t.Errorf("validate did not fire with %q in the failing pass: %+v", domain.ActionRetry, failing)
	}
}

// TestWave1_EnforcerRetryCarriesNarrationAndCorrection: two prose replies to action requests,
// then a third narration — the enforcer retries in place, and the retried request carries the
// superseded narration followed by the "use a tool" correction (the sim's retryForToolUse shape).
func TestWave1_EnforcerRetryCarriesNarrationAndCorrection(t *testing.T) {
	sink := &recordingSink{}
	ran := 0
	cfg := configWithTools(sink,
		fakeTool{name: "read_file", readOnly: true, ran: &ran, result: "contents"},
		fakeTool{name: "write_file", result: "ok"},
	)
	cfg.Mechanisms = wave1Registry(t, "tool_use_enforcer")
	responder := &captureAllResponder{scripts: [][]provider.Delta{
		contentScript("I'll implement feature X."),
		contentScript("Here is my plan."),
		contentScript("I would edit main.go to add the parser."), // narration #3 — the enforcer retries
		toolCallScript("c1", "read_file", `{"path":"main.go"}`),  // the corrected, acting response
		contentScript("done"),
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "please implement feature X")
	runExchange(t, a, "continue")
	runExchange(t, a, "please implement feature X now")

	if len(responder.got) != 5 {
		t.Fatalf("provider was called %d times, want 5", len(responder.got))
	}
	retried := responder.got[3].Messages
	ai := wireMessageIndex(retried, "assistant", "I would edit main.go to add the parser.")
	if ai < 0 {
		t.Fatalf("retried request carries no superseded narration: %+v", retried)
	}
	ci := wireUserIndexContaining(retried, "You MUST use one of the available tools")
	if ci != ai+1 {
		t.Errorf("correction at index %d, want %d (immediately after the superseded narration)", ci, ai+1)
	}
	if wireUserIndexContaining(retried, "Respond with a tool call, not a text description.") < 0 {
		t.Errorf("retried request lacks the sim's tool-use directive: %+v", retried)
	}
	if !hasFire(sink.events, "tool_use_enforcer", string(domain.ActionRetry)) {
		t.Error("no MechanismFiredEvent for tool_use_enforcer with the retry action")
	}
	if ran != 1 {
		t.Errorf("read_file ran %d times, want 1 (the corrected response acted)", ran)
	}
}

// TestWave1_EmptyResponseRetryCarriesNudge: an empty reply mid-task retries in place and the
// retried request carries the sim's completion-check nudge verbatim as a role-safe user message
// (and no superseded assistant message — the empty draft carried nothing).
func TestWave1_EmptyResponseRetryCarriesNudge(t *testing.T) {
	sink := &recordingSink{}
	cfg := configWithTools(sink, fakeTool{name: "read_file", readOnly: true, result: "contents"})
	cfg.Mechanisms = wave1Registry(t, "empty_response_recovery")
	responder := &captureAllResponder{scripts: [][]provider.Delta{
		emptyScript(),
		contentScript("recovered"),
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "please implement the parser")

	if len(responder.got) != 2 {
		t.Fatalf("provider was called %d times, want 2", len(responder.got))
	}
	second := responder.got[1].Messages
	if n := wireRoleCount(second, "assistant"); n != 0 {
		t.Errorf("retried request carries %d assistant messages, want 0 (empty superseded reply)", n)
	}
	if wireMessageIndex(second, "user", wave1Nudge) < 0 {
		t.Errorf("retried request does not carry the completion-check nudge verbatim: %+v", second)
	}
	if me, ok := lastMessageEvent(sink.events); !ok || me.Text != "recovered" {
		t.Errorf("final MessageEvent = %+v (ok=%v), want %q", me, ok, "recovered")
	}
}

// TestWave1_AlwaysEmptyTerminatesAtCap: a responder that never produces anything terminates at
// the loop's maxPostResponseRetries and the last empty response passes through as the Turn's
// final message — the off-ramp cannot spin the loop.
func TestWave1_AlwaysEmptyTerminatesAtCap(t *testing.T) {
	sink := &recordingSink{}
	cfg := configWithTools(sink, fakeTool{name: "read_file", readOnly: true, result: "contents"})
	cfg.Mechanisms = wave1Registry(t, "empty_response_recovery")
	responder := &captureAllResponder{scripts: [][]provider.Delta{
		emptyScript(), emptyScript(), emptyScript(), emptyScript(),
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	res := runExchange(t, a, "please implement the parser")

	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("status = %q, want %q (the empty final passes through)", res.Status, domain.StatusExchangeComplete)
	}
	if len(responder.got) != maxPostResponseRetries+1 {
		t.Errorf("provider was called %d times, want %d (the retry cap)",
			len(responder.got), maxPostResponseRetries+1)
	}
	if me, ok := lastMessageEvent(sink.events); !ok || me.Text != "" {
		t.Errorf("final MessageEvent = %+v (ok=%v), want the passed-through empty reply", me, ok)
	}
	if got := a.conv.Len(); got != 2 {
		t.Errorf("committed history has %d messages, want 2 (user + empty final assistant)", got)
	}
}

// TestWave1_OffRampsFireUnderBypassAndTrippedBudget: both off-ramps — registry-built, dispatched
// through the real gates — still fire with Config.Bypass on AND the global Turn Budget tripped,
// while a co-registered non-off-ramp (validate) is withdrawn at dispatch. This is the dispatch-
// level guarantee, not the descriptor-only one.
func TestWave1_OffRampsFireUnderBypassAndTrippedBudget(t *testing.T) {
	t.Run("empty_response_recovery", func(t *testing.T) {
		sink := &recordingSink{}
		cfg := configWithTools(sink, fakeTool{name: "read_file", readOnly: true, result: "contents"})
		cfg.Bypass = true
		cfg.Mechanisms = wave1Registry(t, "empty_response_recovery", "validate")
		responder := &captureAllResponder{scripts: [][]provider.Delta{
			emptyScript(),
			contentScript("recovered"),
		}}

		a, err := newAgent(cfg, responder)
		if err != nil {
			t.Fatalf("newAgent: %v", err)
		}
		a.tracker.budgetTripped = true
		a.tracker.harmfulStreak = turnBudgetLimit
		runExchange(t, a, "please implement the parser")

		if len(responder.got) != 2 {
			t.Fatalf("provider was called %d times, want 2 (the off-ramp must retry through the gates)", len(responder.got))
		}
		if wireMessageIndex(responder.got[1].Messages, "user", wave1Nudge) < 0 {
			t.Errorf("retried request does not carry the nudge: %+v", responder.got[1].Messages)
		}
		if !hasFire(sink.events, "empty_response_recovery", string(domain.ActionRetry)) {
			t.Error("no MechanismFiredEvent for empty_response_recovery with the retry action")
		}
		if n := fireCountFor(sink.events, "validate"); n != 0 {
			t.Errorf("validate fired %d times; a non-off-ramp must be withdrawn under Bypass + a tripped Turn Budget", n)
		}
	})

	t.Run("tool_use_enforcer", func(t *testing.T) {
		sink := &recordingSink{}
		cfg := configWithTools(sink,
			fakeTool{name: "read_file", readOnly: true, result: "contents"},
			fakeTool{name: "write_file", result: "ok"},
		)
		cfg.Bypass = true
		cfg.Mechanisms = wave1Registry(t, "tool_use_enforcer", "validate")
		responder := &captureAllResponder{scripts: [][]provider.Delta{
			contentScript("I'll implement feature X."),
			contentScript("Here is my plan."),
			contentScript("I would edit main.go to add the parser."),
			toolCallScript("c1", "read_file", `{"path":"main.go"}`),
			contentScript("done"),
		}}

		a, err := newAgent(cfg, responder)
		if err != nil {
			t.Fatalf("newAgent: %v", err)
		}
		a.tracker.budgetTripped = true
		a.tracker.harmfulStreak = turnBudgetLimit
		runExchange(t, a, "please implement feature X")
		runExchange(t, a, "continue")
		runExchange(t, a, "please implement feature X now")

		if len(responder.got) != 5 {
			t.Fatalf("provider was called %d times, want 5 (the off-ramp must retry through the gates)", len(responder.got))
		}
		retried := responder.got[3].Messages
		if wireMessageIndex(retried, "assistant", "I would edit main.go to add the parser.") < 0 {
			t.Errorf("retried request carries no superseded narration: %+v", retried)
		}
		if wireUserIndexContaining(retried, "You MUST use one of the available tools") < 0 {
			t.Errorf("retried request carries no correction: %+v", retried)
		}
		if !hasFire(sink.events, "tool_use_enforcer", string(domain.ActionRetry)) {
			t.Error("no MechanismFiredEvent for tool_use_enforcer with the retry action")
		}
		if n := fireCountFor(sink.events, "validate"); n != 0 {
			t.Errorf("validate fired %d times; a non-off-ramp must be withdrawn under Bypass + a tripped Turn Budget", n)
		}
	})
}
