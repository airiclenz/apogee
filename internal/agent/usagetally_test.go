package agent

import (
	"context"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/tools"
)

// ----------------------------------------------------------------------------
// Cumulative usage accounting (agent.go usageTally → domain.UsageEvent)
// ----------------------------------------------------------------------------

// usageScript is a stream that emits one content chunk then a terminal Done carrying the
// server's token accounting — the scripted counterpart of usageResponder, so a multi-Turn
// script can vary the usage per call.
func usageScript(text string, u provider.Usage) []provider.Delta {
	return []provider.Delta{
		{Kind: provider.DeltaContent, Content: text},
		{Kind: provider.DeltaDone, FinishReason: "stop", Usage: &u},
	}
}

// usageToolCallScript is toolCallScript with the same terminal usage report attached, so a
// Turn that ends in a tool call still accounts for the tokens it spent.
func usageToolCallScript(id, name, args string, u provider.Usage) []provider.Delta {
	return []provider.Delta{
		{Kind: provider.DeltaToolCall, ToolCall: &provider.ToolCall{
			ID:       id,
			Type:     "function",
			Function: provider.FunctionCall{Name: name, Arguments: args},
		}},
		{Kind: provider.DeltaDone, FinishReason: "tool_calls", Usage: &u},
	}
}

// usageEvents collects every UsageEvent from a recorded stream, in emission order.
func usageEvents(events []domain.Event) []domain.UsageEvent {
	var out []domain.UsageEvent
	for _, e := range events {
		if ue, ok := e.(domain.UsageEvent); ok {
			out = append(out, ue)
		}
	}
	return out
}

// TestUsageEventsCarryCumulativeTotals pins the accounting an observer reads instead of summing
// the stream: two completions emit two UsageEvents whose cumulative fields grow — call count 1
// then 2, each sum the running total of both calls — while the fill fields keep reporting the
// call's own counts. Neither is a maintenance event.
func TestUsageEventsCarryCumulativeTotals(t *testing.T) {
	sink := &recordingSink{}
	responder := &scriptedResponder{scripts: [][]provider.Delta{
		usageScript("first", provider.Usage{PromptTokens: 12, CompletionTokens: 7, TotalTokens: 19}),
		usageScript("second", provider.Usage{PromptTokens: 30, CompletionTokens: 5, TotalTokens: 35}),
	}}
	a, err := newAgent(baseConfig(sink), responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	for _, text := range []string{"hi", "again"} {
		if err := a.Submit(domain.UserInput{Text: text}); err != nil {
			t.Fatalf("Submit(%q): %v", text, err)
		}
		if _, err := a.Step(context.Background()); err != nil {
			t.Fatalf("Step(%q): %v", text, err)
		}
	}

	got := usageEvents(sink.events)
	if len(got) != 2 {
		t.Fatalf("emitted %d UsageEvents, want 2 (one per completion)", len(got))
	}
	want := []domain.UsageEvent{
		{
			PromptTokens: 12, CompletionTokens: 7, TotalTokens: 19,
			CumulativePromptTokens: 12, CumulativeCompletionTokens: 7, CumulativeTotalTokens: 19,
			CumulativeCalls: 1,
		},
		{
			PromptTokens: 30, CompletionTokens: 5, TotalTokens: 35,
			CumulativePromptTokens: 42, CumulativeCompletionTokens: 12, CumulativeTotalTokens: 54,
			CumulativeCalls: 2,
		},
	}
	for i, w := range want {
		g := got[i]
		if g.PromptTokens != w.PromptTokens || g.CompletionTokens != w.CompletionTokens || g.TotalTokens != w.TotalTokens {
			t.Errorf("event %d fill fields = {%d %d %d}, want {%d %d %d} (the call's own counts)",
				i, g.PromptTokens, g.CompletionTokens, g.TotalTokens, w.PromptTokens, w.CompletionTokens, w.TotalTokens)
		}
		if g.CumulativeCalls != w.CumulativeCalls {
			t.Errorf("event %d CumulativeCalls = %d, want %d", i, g.CumulativeCalls, w.CumulativeCalls)
		}
		if g.CumulativePromptTokens != w.CumulativePromptTokens ||
			g.CumulativeCompletionTokens != w.CumulativeCompletionTokens ||
			g.CumulativeTotalTokens != w.CumulativeTotalTokens {
			t.Errorf("event %d cumulative = {%d %d %d}, want {%d %d %d} (running totals over both calls)",
				i, g.CumulativePromptTokens, g.CumulativeCompletionTokens, g.CumulativeTotalTokens,
				w.CumulativePromptTokens, w.CumulativeCompletionTokens, w.CumulativeTotalTokens)
		}
		if g.Maintenance {
			t.Errorf("event %d is flagged Maintenance; a Turn's completion is not maintenance accounting", i)
		}
	}
}

// TestSubAgentUsageIsChildLocal proves the tally is PER-AGENT: a sub-agent starts from zero, so
// its Depth-1 event counts only its own call, and the parent's next event continues the parent's
// own totals untouched by the delegation. That is what lets an observer group by Depth/CallID and
// sum the agents itself without double-counting.
func TestSubAgentUsageIsChildLocal(t *testing.T) {
	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore)

	responder := &scriptedResponder{scripts: [][]provider.Delta{
		usageToolCallScript("c1", tools.SubAgentToolName, subAgentArgs("summarise the repo"),
			provider.Usage{PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110}),
		usageScript("child reply", provider.Usage{PromptTokens: 40, CompletionTokens: 4, TotalTokens: 44}),
		usageScript("parent done", provider.Usage{PromptTokens: 200, CompletionTokens: 20, TotalTokens: 220}),
	}}
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "please research"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var parent, child []domain.UsageEvent
	for _, ue := range usageEvents(sink.events) {
		if ue.Depth == 0 {
			parent = append(parent, ue)
			continue
		}
		child = append(child, ue)
	}
	if len(child) != 1 {
		t.Fatalf("sub-agent emitted %d UsageEvents, want 1", len(child))
	}
	if len(parent) != 2 {
		t.Fatalf("parent emitted %d UsageEvents, want 2", len(parent))
	}

	if c := child[0]; c.CumulativeCalls != 1 || c.CumulativePromptTokens != 40 ||
		c.CumulativeCompletionTokens != 4 || c.CumulativeTotalTokens != 44 {
		t.Errorf("child cumulative = {calls %d, %d %d %d}, want {calls 1, 40 4 44} — its own call only",
			c.CumulativeCalls, c.CumulativePromptTokens, c.CumulativeCompletionTokens, c.CumulativeTotalTokens)
	}
	if c := child[0]; c.CallID != "c1" {
		t.Errorf("child event CallID = %q, want the spawning call %q (the grouping key)", c.CallID, "c1")
	}
	if p := parent[1]; p.CumulativeCalls != 2 || p.CumulativePromptTokens != 300 ||
		p.CumulativeCompletionTokens != 30 || p.CumulativeTotalTokens != 330 {
		t.Errorf("parent cumulative after the delegation = {calls %d, %d %d %d}, want {calls 2, 300 30 330} — the child's tokens must not fold in",
			p.CumulativeCalls, p.CumulativePromptTokens, p.CumulativeCompletionTokens, p.CumulativeTotalTokens)
	}
}
