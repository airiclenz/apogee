package agent

// Item-2 acceptance (ADR 0014): the two hook-visible seams guided decomposition needs but
// hooks could not reach — LoopView.Depth() and Response.AppendToolCall. These drive the seams
// through the REAL loop with a scripted upstream (no mechanism internals mocked): a stub
// post-response hook synthesizes a sub_agent delegation the model never emitted, and the loop
// must treat it exactly like a model-emitted call — commit it on the assistant message and
// dispatch it through the full per-call Resolution to a real nested child (Depth 1). A second
// case proves an in-place mutation and a returned ActionDefer both take effect.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/tools"
)

// synthesizeCallHook is an experimental post-response hook standing in for guided
// decomposition's intercept: at the top level (Depth 0) it appends a sub_agent delegation the
// model never emitted, exactly once, and — when deferInject is set — also returns ActionDefer
// so the mutate-AND-defer composition is exercised. Gating on Depth() == 0 keeps a nested
// child (which shares the parent's Mechanisms) from re-appending, exercising the Depth() seam
// in the same test.
type synthesizeCallHook struct {
	task        string
	callID      string
	deferInject string
	appended    *bool
}

func (h synthesizeCallHook) PostResponse(_ context.Context, resp *domain.Response) (domain.PostResponseDecision, error) {
	if resp.View().Depth() != 0 || *h.appended {
		return domain.PostResponseDecision{}, nil
	}
	resp.AppendToolCall(domain.ToolCall{
		ID:        h.callID,
		Tool:      tools.SubAgentToolName,
		Arguments: json.RawMessage(subAgentArgs(h.task)),
	})
	*h.appended = true
	if h.deferInject != "" {
		return domain.PostResponseDecision{Action: domain.ActionDefer, Inject: h.deferInject}, nil
	}
	return domain.PostResponseDecision{}, nil
}

// TestSynthesizedToolCall_DispatchesLikeModelEmitted proves a post-response hook can add a
// sub_agent call to a response the model returned with none, and the loop then commits it on
// the assistant message and dispatches it through the full per-call Resolution — driving a
// real nested child whose events nest at Depth 1 (ADR 0013 recursion point untouched).
func TestSynthesizedToolCall_DispatchesLikeModelEmitted(t *testing.T) {
	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore)
	appended := false
	cfg.Mechanisms = domain.NewMechanismRegistry()
	if err := cfg.Mechanisms.AddExperimental(domain.HookPostResponse, synthesizeCallHook{
		task:     "summarise the repo",
		callID:   "text_call_0",
		appended: &appended,
	}); err != nil {
		t.Fatalf("AddExperimental: %v", err)
	}

	// The model itself emits NO tool call (plain text); the hook synthesizes the sub_agent
	// delegation. The child (Depth 1) then replies with its final message.
	responder := &scriptedResponder{scripts: [][]provider.Delta{
		contentScript("here is my plan"), // parent Turn: text only, no native tool call
		contentScript("child summary"),   // the synthesized sub-agent's only Turn
	}}
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "research the repo"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}

	// The parent Turn now carries a (synthesized) tool call, so it completes as a tool Turn
	// rather than ending the Exchange — proof the loop saw the appended call.
	if res.Status != domain.StatusTurnComplete {
		t.Errorf("Step status = %q, want %q (the synthesized call must make it a tool Turn)", res.Status, domain.StatusTurnComplete)
	}
	if !appended {
		t.Fatal("the synthesis hook never fired at Depth 0")
	}

	// The committed assistant message must carry the synthesized sub_agent call.
	assistant, ok := lastAssistantWithCalls(a.conv.Messages())
	if !ok {
		t.Fatal("no committed assistant message carried the synthesized tool call")
	}
	if len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].Tool != tools.SubAgentToolName {
		t.Fatalf("committed assistant tool calls = %+v, want one sub_agent call", assistant.ToolCalls)
	}

	// A full per-call Resolution ran: the sub_agent call hit the recursion point, drove a real
	// nested child whose events nested at Depth 1, and surfaced the child's report back as the
	// tool result.
	if !hasMessageAtDepth(sink.events, 1, "child summary") {
		t.Error("the synthesized sub_agent call did not dispatch a real nested child at Depth 1")
	}
	sub, ok := lastSubAgentResult(sink.events)
	if !ok || sub.IsError || !strings.Contains(sub.Content, "child summary") {
		t.Errorf("sub_agent tool result = %+v (ok=%v), want the child's final message", sub, ok)
	}
}

// TestSynthesizedToolCall_MutateAndDeferBothLand pins the decision-composition semantic the
// intercept relies on: an in-place response mutation (AppendToolCall) combined with a returned
// ActionDefer must BOTH take effect — the appended call dispatches, and the deferred directive
// is queued for the next request (hookrun applies the mutation, then routes the defer).
func TestSynthesizedToolCall_MutateAndDeferBothLand(t *testing.T) {
	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore)
	appended := false
	const remaining = "REMAINING: delegate the next subtask via sub_agent"
	cfg.Mechanisms = domain.NewMechanismRegistry()
	if err := cfg.Mechanisms.AddExperimental(domain.HookPostResponse, synthesizeCallHook{
		task:        "first subtask",
		callID:      "text_call_0",
		deferInject: remaining,
		appended:    &appended,
	}); err != nil {
		t.Fatalf("AddExperimental: %v", err)
	}

	responder := &scriptedResponder{scripts: [][]provider.Delta{
		contentScript("plan text"), // parent Turn: text only; the hook appends + defers
		contentScript("child one"), // the synthesized child
	}}
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "go"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	// The mutation landed: the synthesized sub_agent call was committed and dispatched to a
	// real child (Depth 1), whose report surfaced as the tool result.
	if sub, ok := lastSubAgentResult(sink.events); !ok || sub.IsError || !strings.Contains(sub.Content, "child one") {
		t.Errorf("the in-place mutation did not take effect: sub_agent result = %+v (ok=%v)", sub, ok)
	}

	// The ActionDefer also landed: the remaining-items directive is queued for the next request
	// (it rides conversation state, drained by the next buildRequest).
	injects, ok := a.conv.TakeDeferred()
	if !ok || len(injects) != 1 || injects[0] != remaining {
		t.Fatalf("the deferred directive did not land alongside the mutation: %v (ok=%v)", injects, ok)
	}
}

// lastAssistantWithCalls returns the last assistant message carrying tool calls.
func lastAssistantWithCalls(msgs []domain.Message) (domain.Message, bool) {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == domain.RoleAssistant && len(msgs[i].ToolCalls) > 0 {
			return msgs[i], true
		}
	}
	return domain.Message{}, false
}

// hasMessageAtDepth reports whether a MessageEvent at the given Depth carries text containing sub.
func hasMessageAtDepth(events []domain.Event, depth int, sub string) bool {
	for _, e := range events {
		if me, ok := e.(domain.MessageEvent); ok && me.Depth == depth && strings.Contains(me.Text, sub) {
			return true
		}
	}
	return false
}
