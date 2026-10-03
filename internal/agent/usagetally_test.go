package agent

import (
	"context"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
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

// pricedAt is a stated server price with the given per-1M rates — the shape priceOfEntry hands the
// engine for an entry with a `price:` block.
func pricedAt(input, output, cachedInput float64) domain.ServerPrice {
	return domain.ServerPrice{
		Rate:     domain.Price{Input: input, Output: output, CachedInput: cachedInput},
		IsStated: true,
	}
}

// stepText submits text and advances one Turn, failing the test on either error.
func stepText(t *testing.T, a *Agent, text string) {
	t.Helper()
	if err := a.Submit(domain.UserInput{Text: text}); err != nil {
		t.Fatalf("Submit(%q): %v", text, err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step(%q): %v", text, err)
	}
}

// TestUsageTallyPricesEachCallAtTheBoundServer pins ADR 0093 decisions 3 and 4 at the one place a
// call is priced: a call to a priced server carries its own amount — the uncached prompt at the
// input rate, the cached share at the cached-input rate, the completion at the output rate — and
// adds it to Cumulative as a priced call; a call to an unpriced server carries nothing and is
// counted as unpriced, never as a free priced call.
func TestUsageTallyPricesEachCallAtTheBoundServer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		price      domain.ServerPrice
		wantCost   int64
		wantPriced bool
		wantUsage  domain.Usage
	}{
		{
			name:  "priced server",
			price: pricedAt(2, 10, 0.5),
			// 600 uncached × 2 + 400 cached × 0.5 + 100 completion × 10 millionths.
			wantCost:   2400,
			wantPriced: true,
			wantUsage: domain.Usage{
				Calls: 1, PromptTokens: 1000, CachedPromptTokens: 400, CompletionTokens: 100,
				TotalTokens: 1100, CostMicros: 2400, PricedCalls: 1,
			},
		},
		{
			name:  "unpriced server",
			price: domain.ServerPrice{},
			wantUsage: domain.Usage{
				Calls: 1, PromptTokens: 1000, CachedPromptTokens: 400, CompletionTokens: 100,
				TotalTokens: 1100, UnpricedCalls: 1,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sink := &recordingSink{}
			cfg := baseConfig(sink)
			cfg.Price = tc.price
			responder := scriptedResponder(t,
				usageScript("reply", stubllm.Usage{Prompt: 1000, Completion: 100, Cached: 400}))
			a, err := newAgent(cfg, responder)
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}

			stepText(t, a, "hi")

			got := usageEvents(sink.events)
			if len(got) != 1 {
				t.Fatalf("emitted %d UsageEvents, want 1", len(got))
			}
			if got[0].CostMicros != tc.wantCost || got[0].Priced != tc.wantPriced {
				t.Errorf("call = {cost %d, priced %v}, want {cost %d, priced %v}",
					got[0].CostMicros, got[0].Priced, tc.wantCost, tc.wantPriced)
			}
			if got[0].Cumulative != tc.wantUsage {
				t.Errorf("Cumulative = %+v, want %+v", got[0].Cumulative, tc.wantUsage)
			}
		})
	}
}

// TestUsageTallySwitchMidSessionSumsTwoPrices is ADR 0093's "a session is not on one server": a
// call before a `/server` switch is priced at the first server, a call after it at the arrived-at
// server's price the switch carried, and Cumulative is the plain sum of the two — the earlier call
// is never repriced. A rebind that restates the price (a `price:` edit dropping the block) leaves
// the next call unpriced, and a rebind silent about the price keeps the one in force.
func TestUsageTallySwitchMidSessionSumsTwoPrices(t *testing.T) {
	t.Parallel()

	const (
		firstEndpoint  = "http://first.local:1111"
		secondEndpoint = "http://second.local:2222"
	)
	first := scriptedResponder(t, usageScript("on the first", stubllm.Usage{Prompt: 100, Completion: 10}))
	second := scriptedResponder(t,
		usageScript("on the second", stubllm.Usage{Prompt: 100, Completion: 10}),
		usageScript("still on the second", stubllm.Usage{Prompt: 100, Completion: 10}),
		usageScript("price dropped", stubllm.Usage{Prompt: 100, Completion: 10}),
	)
	dialer := dialerAnswering(func(endpoint string) provider.Responder {
		if endpoint == secondEndpoint {
			return second
		}
		return first
	})
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Endpoint = firstEndpoint
	cfg.Price = pricedAt(1, 2, 1)
	a, err := New(cfg, WithDialer(dialer.dial))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	stepText(t, a, "one")
	if err := a.SwitchUpstream(UpstreamSpec{Endpoint: secondEndpoint, Price: pricedAt(3, 4, 3)}); err != nil {
		t.Fatalf("SwitchUpstream: %v", err)
	}
	if err := a.Rebind(RebindSpec{Model: testModel}); err != nil {
		t.Fatalf("Rebind (silent about the price): %v", err)
	}
	stepText(t, a, "two")
	stepText(t, a, "three")
	unpriced := domain.ServerPrice{}
	if err := a.Rebind(RebindSpec{Model: testModel, Price: &unpriced}); err != nil {
		t.Fatalf("Rebind (price dropped): %v", err)
	}
	stepText(t, a, "four")

	got := usageEvents(sink.events)
	if len(got) != 4 {
		t.Fatalf("emitted %d UsageEvents, want 4", len(got))
	}
	// 100 × 1 + 10 × 2 = 120 on the first server; 100 × 3 + 10 × 4 = 340 on the second.
	wantCosts := []int64{120, 340, 340, 0}
	for i, want := range wantCosts {
		if got[i].CostMicros != want || got[i].Priced != (want != 0) {
			t.Errorf("call %d = {cost %d, priced %v}, want {cost %d, priced %v}",
				i, got[i].CostMicros, got[i].Priced, want, want != 0)
		}
	}
	last := got[3].Cumulative
	if last.CostMicros != 800 || last.PricedCalls != 3 || last.UnpricedCalls != 1 {
		t.Errorf("final Cumulative = {cost %d, priced %d, unpriced %d}, want {800, 3, 1} — one sum over both prices",
			last.CostMicros, last.PricedCalls, last.UnpricedCalls)
	}
}

// TestUsageTallyRoutedDelegationPricesAtTheTarget is decision 3 for a routed delegation: a child
// routed to the Sub-agent server prices its calls at THAT server's price — and a target with no
// `price:` leaves the child unpriced rather than borrowing the parent's rate — while an unrouted
// child calls the parent's server and so carries the parent's price.
func TestUsageTallyRoutedDelegationPricesAtTheTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		target     *DelegationTarget
		wantCost   int64
		wantPriced bool
	}{
		// reading's call: 10 prompt, 5 completion.
		{name: "routed to a priced target", target: routedTargetPriced(pricedAt(3, 4, 3)), wantCost: 50, wantPriced: true},
		{name: "routed to an unpriced target", target: routedTargetPriced(domain.ServerPrice{})},
		{name: "unrouted", target: nil, wantCost: 30, wantPriced: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			parent := routingParent(t)
			parent.cfg.Price = pricedAt(1, 4, 1)
			parent.SetDelegationTarget(tc.target)

			child := reading(spawn(t, parent))

			if child.CostMicros != tc.wantCost || child.Priced != tc.wantPriced {
				t.Errorf("child call = {cost %d, priced %v}, want {cost %d, priced %v}",
					child.CostMicros, child.Priced, tc.wantCost, tc.wantPriced)
			}
		})
	}
}

// routedTargetPriced is routedTarget with the Sub-agent server's price set.
func routedTargetPriced(price domain.ServerPrice) *DelegationTarget {
	target := routedTarget()
	target.Price = price
	return target
}

// TestUsageCompactionCallIsPriced pins decision 3's maintenance half: the Compaction fold is a real
// call to the server the agent is bound to, so its Maintenance reading is priced there through the
// same tally — no second path — and its amount joins the Turns' in Cumulative.
func TestUsageCompactionCallIsPriced(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Price = pricedAt(1, 2, 1)
	responder := scriptedResponder(t,
		usageScript("first", stubllm.Usage{Prompt: 10, Completion: 1}),
		usageScript("second", stubllm.Usage{Prompt: 20, Completion: 2}),
		usageScript("FOLDED-SUMMARY", stubllm.Usage{Prompt: 500, Completion: 60}),
	)
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	stepText(t, a, "task one")
	stepText(t, a, "task two")

	if skipped, err := a.Compact(context.Background()); err != nil {
		t.Fatalf("Compact: %v", err)
	} else if skipped {
		t.Fatal("Compact skipped a foldable conversation; want a real fold that spends tokens")
	}

	got := usageEvents(sink.events)
	if len(got) != 3 || !got[2].Maintenance {
		t.Fatalf("emitted %d UsageEvents (last maintenance: %v), want 3 ending in the fold", len(got), len(got) == 3 && got[2].Maintenance)
	}
	// 500 × 1 + 60 × 2 = 620; the Turns before it cost 12 and 24.
	if fold := got[2]; fold.CostMicros != 620 || !fold.Priced {
		t.Errorf("fold = {cost %d, priced %v}, want {620, true}", fold.CostMicros, fold.Priced)
	}
	if c := got[2].Cumulative; c.CostMicros != 656 || c.PricedCalls != 3 || c.UnpricedCalls != 0 {
		t.Errorf("Cumulative after the fold = {cost %d, priced %d, unpriced %d}, want {656, 3, 0}",
			c.CostMicros, c.PricedCalls, c.UnpricedCalls)
	}
}
