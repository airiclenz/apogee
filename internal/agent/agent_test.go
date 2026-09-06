package agent

// The engine doors on the Agent itself. Today that is the Thinking-effort override
// (SetEffortOverride / ThinkingEffort): what the door does to the session, what it deliberately
// does NOT do to a delegated child, and how it composes with a model switch — plus the effort WIRE
// DIALECT the same switch carries in from the server (ADR 0060).
//
// It also covers the Agent's own Turn-boundary reporting: TurnEvent, emitted once per StepResult
// at the exported returns (ADR 0073).

import (
	"context"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// TestEffortOverrideLayersOverTheProfile is the door's own contract: the two layers are reported
// separately so a Driver can SHOW the layering (bare /effort), the override moves freely, and the
// zero value clears it rather than meaning a fifth level.
func TestEffortOverrideLayersOverTheProfile(t *testing.T) {
	t.Parallel()

	cfg := baseConfig(&recordingSink{})
	cfg.Profile.Thinking.Effort = domain.EffortLow
	a, err := newAgent(cfg, &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	override, profile := a.ThinkingEffort()
	if override != "" || profile != domain.EffortLow {
		t.Fatalf("fresh session reports override %q / profile %q, want \"\" / %q", override, profile, domain.EffortLow)
	}
	if got := a.resolvedEffort(); got != domain.EffortLow {
		t.Errorf("resolved effort with no override = %q, want the profile's %q", got, domain.EffortLow)
	}

	a.SetEffortOverride(domain.EffortHigh)
	override, profile = a.ThinkingEffort()
	if override != domain.EffortHigh || profile != domain.EffortLow {
		t.Errorf("under the override: override %q / profile %q, want %q / %q", override, profile, domain.EffortHigh, domain.EffortLow)
	}
	if got := a.resolvedEffort(); got != domain.EffortHigh {
		t.Errorf("resolved effort under the override = %q, want %q", got, domain.EffortHigh)
	}

	a.SetEffortOverride("")
	if override, _ = a.ThinkingEffort(); override != "" {
		t.Errorf("override after clearing = %q, want the empty absence", override)
	}
	if got := a.resolvedEffort(); got != domain.EffortLow {
		t.Errorf("resolved effort after clearing = %q, want the profile's %q back", got, domain.EffortLow)
	}
}

// TestChildDoesNotInheritTheEffortOverride pins the override as PRIMARY-loop state (ADR 0050): a
// delegated child resolves effort from its own profile, so a user asking the session to think hard
// about the conversation does not silently re-price every sub-agent it spawns. It holds by
// construction — the override lives on the Agent, not on the Config a child is built from — and
// this is what makes a future move of it onto the Config fail loudly.
func TestChildDoesNotInheritTheEffortOverride(t *testing.T) {
	t.Parallel()

	cfg := configWithTools(&recordingSink{}, fakeTool{name: "w"})
	cfg.Profile.Thinking.Effort = domain.EffortLow
	parent, err := newAgent(cfg, &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	parent.SetEffortOverride(domain.EffortHigh)

	child, err := parent.newChildAgent("call_sub", "the delegated task", "")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}

	if override, _ := child.ThinkingEffort(); override != "" {
		t.Errorf("child override = %q, want none — the session override is the parent's alone", override)
	}
	if got := child.resolvedEffort(); got != domain.EffortLow {
		t.Errorf("child resolved effort = %q, want its own profile's %q", got, domain.EffortLow)
	}
	if got := parent.resolvedEffort(); got != domain.EffortHigh {
		t.Errorf("parent resolved effort = %q, want its override %q left standing", got, domain.EffortHigh)
	}
}

// TestEffortOverrideSurvivesARebind is the model-switch regression: the profile half re-resolves
// with the new model (the effort rides the profile through the existing door, no code of its own),
// while the SESSION override — an instruction about this conversation, not about a model — stays
// exactly where the user put it.
func TestEffortOverrideSurvivesARebind(t *testing.T) {
	t.Parallel()

	cfg := baseConfig(&recordingSink{})
	cfg.Profile.Thinking.Effort = domain.EffortLow
	a, err := newAgent(cfg, &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.SetEffortOverride(domain.EffortHigh)

	if err := a.Rebind(RebindSpec{
		Model:            "another-model",
		SystemPrompt:     "you are bound to the new model",
		MaxContextTokens: 8192,
		Profile:          domain.ModelProfile{Thinking: domain.ThinkingProfile{Effort: domain.EffortMedium}},
	}); err != nil {
		t.Fatalf("Rebind: %v", err)
	}

	override, profile := a.ThinkingEffort()
	if override != domain.EffortHigh {
		t.Errorf("override after the rebind = %q, want the session's %q intact", override, domain.EffortHigh)
	}
	if profile != domain.EffortMedium {
		t.Errorf("profile effort after the rebind = %q, want the new model's %q", profile, domain.EffortMedium)
	}
	if got := a.resolvedEffort(); got != domain.EffortHigh {
		t.Errorf("resolved effort after the rebind = %q, want the override %q still on top", got, domain.EffortHigh)
	}

	// Clearing now falls back to the NEW model's profile, not the departed one's.
	a.SetEffortOverride("")
	if got := a.resolvedEffort(); got != domain.EffortMedium {
		t.Errorf("resolved effort after clearing = %q, want the new profile's %q", got, domain.EffortMedium)
	}
}

// TestNewSeedsTheEffortDialectFromTheConfig is the construction half of the same server fact: an
// engine built with a dialect on its Config sends that dialect on its FIRST request, with nobody
// having rebound anything. That is what makes a Driver that never rebinds — an unattended Firing, a
// bench arm, any embedder over run.Once — reach the same wire a session reaches (ADR 0031 parity;
// the 2026-08-25 audit's C-03). It reads the body the fake provider was actually handed, not the
// projection, so nothing between the seed and the Upstream can quietly drop it.
func TestNewSeedsTheEffortDialectFromTheConfig(t *testing.T) {
	t.Parallel()

	cfg := baseConfig(&recordingSink{})
	cfg.EffortDialect = domain.EffortDialectReasoning
	cfg.Profile.Thinking.Effort = domain.EffortMedium

	up := &recordingResponder{reply: "done"}
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	if got := up.last.EffortDialect; got != provider.EffortDialectReasoning {
		t.Errorf("first request's dialect = %q, want the Config's %q — an unrebound Driver must still speak the server's shape",
			got, provider.EffortDialectReasoning)
	}
	if got := up.last.ThinkingEffort; got != provider.EffortMedium {
		t.Errorf("first request's effort = %q, want the profile's %q", got, provider.EffortMedium)
	}

	// And the seed is TOTAL: a dialect no build understands degrades to the zero — the historical
	// chat_template_kwargs shape — rather than putting an unknown word on the wire mid-Turn.
	bogus := baseConfig(&recordingSink{})
	bogus.EffortDialect = domain.EffortDialect("no-such-dialect")
	strange := &recordingResponder{reply: "done"}
	b, err := newAgent(bogus, strange)
	if err != nil {
		t.Fatalf("newAgent with an unknown dialect: %v", err)
	}
	if err := b.Submit(domain.UserInput{Text: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := b.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if got := strange.last.EffortDialect; got != provider.EffortDialectNone {
		t.Errorf("first request's dialect for an unknown seed = %q, want the zero anchor", got)
	}
}

// TestRebindCarriesTheEffortDialectOntoTheRequest is the server half of the effort seam: the
// dialect is not a per-model binding but a fact about the server the session is on, and it reaches
// the engine through the one channel a server fact has — the RebindSpec (ADR 0060). What the next
// request carries is what the last spec stated.
func TestRebindCarriesTheEffortDialectOntoTheRequest(t *testing.T) {
	t.Parallel()

	a, err := newAgent(baseConfig(&recordingSink{}), &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	// The anchor first: a session nobody has dialled asks for no dialect at all, so the request is
	// byte-identical to the pre-dialect loop (ADR 0031).
	if got := a.toProviderRequest(effortTestRequest()).EffortDialect; got != provider.EffortDialectNone {
		t.Fatalf("dialect before any rebind = %q, want the zero anchor", got)
	}

	if err := a.Rebind(RebindSpec{
		Model:            "another-model",
		MaxContextTokens: 8192,
		EffortDialect:    provider.EffortDialectReasoning,
	}); err != nil {
		t.Fatalf("Rebind: %v", err)
	}
	if got := a.toProviderRequest(effortTestRequest()).EffortDialect; got != provider.EffortDialectReasoning {
		t.Errorf("dialect after the rebind = %q, want the spec's %q", got, provider.EffortDialectReasoning)
	}

	// And a move onto a server that advertises no dial states the zero rather than staying silent:
	// keeping the departed server's shape would express this session's effort in a dialect the
	// server it is now on does not read.
	if err := a.Rebind(RebindSpec{Model: "third-model", MaxContextTokens: 8192}); err != nil {
		t.Fatalf("Rebind onto an undialled server: %v", err)
	}
	if got := a.toProviderRequest(effortTestRequest()).EffortDialect; got != provider.EffortDialectNone {
		t.Errorf("dialect after rebinding onto an undialled server = %q, want the zero", got)
	}
}

// ---------------------------------------------------------------------------
// Turn boundaries on the stream (TurnEvent)
// ---------------------------------------------------------------------------

// turnEvents collects the TurnEvents a sink recorded, in emission order.
func turnEvents(events []domain.Event) []domain.TurnEvent {
	out := make([]domain.TurnEvent, 0, len(events))
	for _, e := range events {
		if te, ok := e.(domain.TurnEvent); ok {
			out = append(out, te)
		}
	}
	return out
}

// TestTurnEventReportsEveryBoundaryToStepAndRunHostsAlike is the variant's whole contract at the
// top level: one event per boundary, in Turn order, carrying the same Status the StepResult did —
// and the SAME sequence whether the host drives Step itself (the bench, headless) or hands the
// Exchange to Run. The emit site is the exported return precisely so the two agree.
func TestTurnEventReportsEveryBoundaryToStepAndRunHostsAlike(t *testing.T) {
	t.Parallel()

	drivers := []struct {
		name  string
		drive func(t *testing.T, a *Agent)
	}{
		{"Step-driven", func(t *testing.T, a *Agent) {
			t.Helper()
			for {
				res, err := a.Step(context.Background())
				if err != nil {
					t.Fatalf("Step: %v", err)
				}
				if res.Status != domain.StatusTurnComplete {
					return
				}
			}
		}},
		{"Run-driven", func(t *testing.T, a *Agent) {
			t.Helper()
			if _, err := a.Run(context.Background()); err != nil {
				t.Fatalf("Run: %v", err)
			}
		}},
	}

	want := []domain.TurnEvent{
		{EventBase: domain.EventBase{Turn: 0}, Status: domain.StatusTurnComplete},
		{EventBase: domain.EventBase{Turn: 1}, Status: domain.StatusExchangeComplete},
	}

	for _, driver := range drivers {
		t.Run(driver.name, func(t *testing.T) {
			t.Parallel()

			sink := &recordingSink{}
			cfg := configWithTools(sink, fakeTool{name: "lookup", readOnly: true, result: "the answer is 42"})
			a, err := newAgent(cfg, &scriptedResponder{scripts: [][]provider.Delta{
				toolCallScript("c1", "lookup", `{"q":"meaning"}`),
				contentScript("all done"),
			}})
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			if err := a.Submit(domain.UserInput{Text: "look it up"}); err != nil {
				t.Fatalf("Submit: %v", err)
			}

			driver.drive(t, a)

			got := turnEvents(sink.events)
			if len(got) != len(want) {
				t.Fatalf("emitted %d TurnEvents, want %d: %+v", len(got), len(want), got)
			}
			for i, w := range want {
				if got[i] != w {
					t.Errorf("TurnEvent %d = %+v, want %+v", i, got[i], w)
				}
			}
		})
	}
}

// TestTurnEventMarksTheStepCappedChildBoundary pins the reason the emit site is the exported
// return rather than step(): StepCapped is decided ABOVE step(), and the capped exit has two
// shapes — the wrap-up Turn's own boundary, and the turns.end fallback row built when that
// wrap-up produced no trustworthy boundary of its own. Both must reach an observer as one capped
// TurnEvent on the CHILD's stream, at its depth and under the delegating call's id, and never as
// a failure.
func TestTurnEventMarksTheStepCappedChildBoundary(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		wrapUp []provider.Delta
	}{
		{"the wrap-up Turn completes", contentScript("here is what I found")},
		{"the wrap-up Turn faults", errorScript("upstream exploded on the wrap-up")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sink := &recordingSink{}
			reader := fakeTool{name: "read_thing", readOnly: true, result: "package main"}
			cfg := subAgentConfig(sink, domain.ModeAskBefore, reader)
			cfg.Delegation.MaxSteps = 3

			scripts := [][]provider.Delta{subAgentCallScript("c1", "trawl the repo")}
			scripts = append(scripts, cappedChildTurns(3)...)
			scripts = append(scripts, tc.wrapUp, contentScript("parent done"))

			a, err := newAgent(cfg, &scriptedResponder{scripts: scripts})
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			if err := a.Submit(domain.UserInput{Text: "please research"}); err != nil {
				t.Fatalf("Submit: %v", err)
			}
			if _, err := a.Run(context.Background()); err != nil {
				t.Fatalf("Run: %v", err)
			}

			capped := make([]domain.TurnEvent, 0, 1)
			for _, te := range turnEvents(sink.events) {
				if te.StepCapped {
					capped = append(capped, te)
				}
			}
			if len(capped) != 1 {
				t.Fatalf("%d capped TurnEvents, want exactly 1: %+v", len(capped), capped)
			}
			got := capped[0]
			if got.Depth != 1 || got.CallID != "c1" {
				t.Errorf("capped TurnEvent base = %+v, want the child's identity (Depth 1, CallID %q)", got.EventBase, "c1")
			}
			if got.Status != domain.StatusExchangeComplete {
				t.Errorf("capped TurnEvent status = %q, want %q — the cap closes the Exchange", got.Status, domain.StatusExchangeComplete)
			}
			if got.Faulted {
				t.Error("capped TurnEvent is Faulted; the delegate step cap is a bound, never a failure")
			}
		})
	}
}

// TestTurnEventReportsACancelledTurn covers the third disposition: a Turn abandoned by a cancelled
// ctx still reaches its quiescent boundary, so it is still one Turn boundary and still one event —
// carrying StatusCancelled, which is what tells an observer the Turn produced nothing.
func TestTurnEventReportsACancelledTurn(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	responder := blockingResponder{started: make(chan struct{})}
	a, err := newAgent(baseConfig(sink), responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "slow"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-responder.started
		cancel()
	}()
	if _, err := a.Step(ctx); err != nil {
		t.Fatalf("Step: %v", err)
	}

	got := turnEvents(sink.events)
	if len(got) != 1 {
		t.Fatalf("emitted %d TurnEvents, want 1: %+v", len(got), got)
	}
	if got[0].Status != domain.StatusCancelled {
		t.Errorf("TurnEvent status = %q, want %q", got[0].Status, domain.StatusCancelled)
	}
}
