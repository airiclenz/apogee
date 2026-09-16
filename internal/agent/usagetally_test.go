package agent

import (
	"context"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tools"
)

// ----------------------------------------------------------------------------
// Cumulative usage accounting (agent.go usageTally → domain.UsageEvent)
// ----------------------------------------------------------------------------

// usageScript is a turn that replies with text and reports the server's token accounting on its
// terminal chunk, so a multi-Turn script can vary the usage per call.
func usageScript(text string, u stubllm.Usage) stubllm.Turn {
	return stubllm.Turn{Text: text, Usage: &u}
}

// servedModelID is the id a served-model script advertises, and so the id every reply names as
// the model that answered — deliberately not the id baseConfig binds, so a reading that stamped
// the bound model where the served one belongs would show.
const servedModelID = "served-by-x"

// servedResponder is scriptedResponder for a script whose replies name servedModelID as the
// model that answered.
func servedResponder(t testing.TB, turns ...stubllm.Turn) *scriptedUpstream {
	t.Helper()
	return scriptResponder(t, stubllm.Script{Model: servedModelID, Turns: turns})
}

// usageToolCallScript is toolCallTurn with the same terminal usage report attached, so a
// Turn that ends in a tool call still accounts for the tokens it spent.
func usageToolCallScript(id, name, args string, u stubllm.Usage) stubllm.Turn {
	turn := toolCallTurn(id, name, args)
	turn.Usage = &u
	return turn
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
	responder := scriptedResponder(t,
		usageScript("first", stubllm.Usage{Prompt: 12, Completion: 7, Cached: 4}),
		usageScript("second", stubllm.Usage{Prompt: 30, Completion: 5, Cached: 11}),
	)
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
			PromptTokens: 12, CompletionTokens: 7, TotalTokens: 19, CachedPromptTokens: 4,
			Cumulative: domain.Usage{
				Calls: 1, PromptTokens: 12, CachedPromptTokens: 4, CompletionTokens: 7, TotalTokens: 19,
			},
		},
		{
			PromptTokens: 30, CompletionTokens: 5, TotalTokens: 35, CachedPromptTokens: 11,
			Cumulative: domain.Usage{
				Calls: 2, PromptTokens: 42, CachedPromptTokens: 15, CompletionTokens: 12, TotalTokens: 54,
			},
		},
	}
	for i, w := range want {
		g := got[i]
		if g.PromptTokens != w.PromptTokens || g.CompletionTokens != w.CompletionTokens || g.TotalTokens != w.TotalTokens {
			t.Errorf("event %d fill fields = {%d %d %d}, want {%d %d %d} (the call's own counts)",
				i, g.PromptTokens, g.CompletionTokens, g.TotalTokens, w.PromptTokens, w.CompletionTokens, w.TotalTokens)
		}
		if g.Cumulative.Calls != w.Cumulative.Calls {
			t.Errorf("event %d Cumulative.Calls = %d, want %d", i, g.Cumulative.Calls, w.Cumulative.Calls)
		}
		// The cached share is a subset of the prompt count, folded on the same terms: the call's
		// own reading in the fill field, the running sum in the cumulative one.
		if g.CachedPromptTokens != w.CachedPromptTokens ||
			g.Cumulative.CachedPromptTokens != w.Cumulative.CachedPromptTokens {
			t.Errorf("event %d cached = {%d, cumulative %d}, want {%d, cumulative %d}",
				i, g.CachedPromptTokens, g.Cumulative.CachedPromptTokens,
				w.CachedPromptTokens, w.Cumulative.CachedPromptTokens)
		}
		if g.Cumulative.PromptTokens != w.Cumulative.PromptTokens ||
			g.Cumulative.CompletionTokens != w.Cumulative.CompletionTokens ||
			g.Cumulative.TotalTokens != w.Cumulative.TotalTokens {
			t.Errorf("event %d cumulative = {%d %d %d}, want {%d %d %d} (running totals over both calls)",
				i, g.Cumulative.PromptTokens, g.Cumulative.CompletionTokens, g.Cumulative.TotalTokens,
				w.Cumulative.PromptTokens, w.Cumulative.CompletionTokens, w.Cumulative.TotalTokens)
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

	responder := scriptedResponder(t,
		usageToolCallScript("c1", tools.SubAgentToolName, subAgentArgs("summarise the repo"),
			stubllm.Usage{Prompt: 100, Completion: 10, Cached: 50}),
		usageScript("child reply", stubllm.Usage{Prompt: 40, Completion: 4, Cached: 9}),
		usageScript("parent done", stubllm.Usage{Prompt: 200, Completion: 20, Cached: 90}),
	)
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

	if c := child[0].Cumulative; c.Calls != 1 || c.PromptTokens != 40 ||
		c.CompletionTokens != 4 || c.TotalTokens != 44 {
		t.Errorf("child cumulative = {calls %d, %d %d %d}, want {calls 1, 40 4 44} — its own call only",
			c.Calls, c.PromptTokens, c.CompletionTokens, c.TotalTokens)
	}
	// The cached share is per-agent on the same terms as the counters it qualifies: the child
	// starts at zero rather than inheriting the 50 the parent's spawning call had cached.
	if c := child[0]; c.Cumulative.CachedPromptTokens != 9 {
		t.Errorf("child cumulative cached = %d, want 9 — its own call only", c.Cumulative.CachedPromptTokens)
	}
	if c := child[0]; c.CallID != "c1" {
		t.Errorf("child event CallID = %q, want the spawning call %q (the grouping key)", c.CallID, "c1")
	}
	if p := parent[1].Cumulative; p.Calls != 2 || p.PromptTokens != 300 ||
		p.CompletionTokens != 30 || p.TotalTokens != 330 {
		t.Errorf("parent cumulative after the delegation = {calls %d, %d %d %d}, want {calls 2, 300 30 330} — the child's tokens must not fold in",
			p.Calls, p.PromptTokens, p.CompletionTokens, p.TotalTokens)
	}
	if p := parent[1]; p.Cumulative.CachedPromptTokens != 140 {
		t.Errorf("parent cumulative cached after the delegation = %d, want 140 — the child's 9 must not fold in",
			p.Cumulative.CachedPromptTokens)
	}
}

// TestCompactionUsageRidesFlaggedMaintenanceEvent pins the compaction half of the accounting: the
// summary call is real spend, so it folds into the same Agent's tally and surfaces as exactly ONE
// UsageEvent flagged Maintenance — the flag being what lets a gauge/tokens-per-sec reader skip a
// fill figure that describes the summarizer's request rather than the conversation, while a totals
// reader keeps it. The Turn after the fold continues from those totals unflagged, which is what
// makes /usage right immediately after /compact.
func TestCompactionUsageRidesFlaggedMaintenanceEvent(t *testing.T) {
	sink := &recordingSink{}
	responder := servedResponder(t,
		usageScript("first", stubllm.Usage{Prompt: 12, Completion: 7}),
		usageScript("second", stubllm.Usage{Prompt: 30, Completion: 5}),
		usageScript("FOLDED-SUMMARY", stubllm.Usage{Prompt: 500, Completion: 60}),
		usageScript("after the fold", stubllm.Usage{Prompt: 8, Completion: 3}),
	)
	a, err := newAgent(baseConfig(sink), responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	// Two exchanges → a foldable [user, assistant, user, assistant] conversation, then the fold,
	// then one more exchange to prove the totals carry across it.
	step := func(text string) {
		t.Helper()
		if err := a.Submit(domain.UserInput{Text: text}); err != nil {
			t.Fatalf("Submit(%q): %v", text, err)
		}
		if _, err := a.Step(context.Background()); err != nil {
			t.Fatalf("Step(%q): %v", text, err)
		}
	}
	step("task one")
	step("task two")
	if skipped, err := a.Compact(context.Background()); err != nil {
		t.Fatalf("Compact: %v", err)
	} else if skipped {
		t.Fatal("Compact skipped a foldable conversation; want a real fold that spends tokens")
	}
	step("task three")

	got := usageEvents(sink.events)
	if len(got) != 4 {
		t.Fatalf("emitted %d UsageEvents, want 4 (three Turns + the compaction call)", len(got))
	}
	var maintenance []domain.UsageEvent
	for _, ue := range got {
		if ue.Maintenance {
			maintenance = append(maintenance, ue)
		}
	}
	if len(maintenance) != 1 {
		t.Fatalf("%d Maintenance UsageEvents, want exactly 1 (the compaction call)", len(maintenance))
	}

	fold := maintenance[0]
	if fold.PromptTokens != 500 || fold.CompletionTokens != 60 || fold.TotalTokens != 560 {
		t.Errorf("compaction fill fields = {%d %d %d}, want {500 60 560} (the summary call's own counts)",
			fold.PromptTokens, fold.CompletionTokens, fold.TotalTokens)
	}
	if c := fold.Cumulative; c.Calls != 3 || c.PromptTokens != 542 ||
		c.CompletionTokens != 72 || c.TotalTokens != 614 {
		t.Errorf("compaction cumulative = {calls %d, %d %d %d}, want {calls 3, 542 72 614} — the two Turns plus the fold",
			c.Calls, c.PromptTokens, c.CompletionTokens, c.TotalTokens)
	}
	if got[2] != fold {
		t.Error("the Maintenance event is not the third emission; the fold must account at the point it ran")
	}
	// The summariser's reply names the model that answered it exactly as a Turn's does: the served
	// id is read off the same terminal Done the usage is, so a fold on an aliased or routed server
	// records who summarised, not just what it cost.
	if fold.ServedModel != servedModelID {
		t.Errorf("compaction ServedModel = %q, want %q — the id the summary reply carried", fold.ServedModel, servedModelID)
	}

	after := got[3]
	if after.Maintenance {
		t.Error("the Turn after the fold is flagged Maintenance; only the compaction call is")
	}
	if c := after.Cumulative; c.Calls != 4 || c.PromptTokens != 550 ||
		c.CompletionTokens != 75 || c.TotalTokens != 625 {
		t.Errorf("post-fold cumulative = {calls %d, %d %d %d}, want {calls 4, 550 75 625} — continuing from the fold's totals",
			c.Calls, c.PromptTokens, c.CompletionTokens, c.TotalTokens)
	}
}
