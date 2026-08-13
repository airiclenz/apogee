package agent

import (
	"context"
	"testing"

	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// TestBudgetIsHonestBeforeCalibration pins the uncalibrated view: a fresh Agent that has seen no
// server usage reports the default chars→token ratio and a zero Used, but its window allocation is
// already populated from the configured window (the allocation needs no usage).
func TestBudgetIsHonestBeforeCalibration(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.Context.MaxContextTokens = 8192
	a, err := newAgent(cfg, echoResponder{reply: "unused"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	b := a.budget()
	if b.Used != 0 {
		t.Errorf("Used = %d, want 0 before the first UsageEvent", b.Used)
	}
	if b.CharsPerToken != apogeectx.DefaultCharsPerToken {
		t.Errorf("CharsPerToken = %v, want the default %v uncalibrated", b.CharsPerToken, apogeectx.DefaultCharsPerToken)
	}
	if b.ContextLimit != 8192 {
		t.Errorf("ContextLimit = %d, want the configured window 8192", b.ContextLimit)
	}
	if want := apogeectx.Allocate(8192, 0); b.ResponseReserve != want.ResponseReserve ||
		b.SystemPrompt != want.SystemPrompt || b.FileContext != want.FileContext || b.History != want.History {
		t.Errorf("allocation = {reserve %d sys %d file %d hist %d}, want context.Allocate %+v",
			b.ResponseReserve, b.SystemPrompt, b.FileContext, b.History, want)
	}
}

// TestBudgetViewReflectsCalibratedUsage drives one real Turn whose stream carries server usage and
// proves the Budget view is calibrated: Used snaps to the reported prompt tokens and CharsPerToken
// folds toward the char/token sample, staying inside the sane band (plan item 8 acceptance:
// "Budget() view reflects it").
func TestBudgetViewReflectsCalibratedUsage(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.Context.MaxContextTokens = 8192
	a, err := newAgent(cfg, usageResponder{
		content: "hello",
		usage:   provider.Usage{PromptTokens: 12, CompletionTokens: 7, TotalTokens: 19},
	})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	if err := a.Submit(domain.UserInput{Text: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	b := a.budget()
	if b.Used != 12 {
		t.Errorf("Used = %d, want 12 (snapped to the reported prompt tokens)", b.Used)
	}
	if b.CharsPerToken == apogeectx.DefaultCharsPerToken {
		t.Errorf("CharsPerToken still the default %v; calibration did not fold in the usage report", b.CharsPerToken)
	}
	if b.CharsPerToken < 2.0 || b.CharsPerToken > 8.0 {
		t.Errorf("CharsPerToken = %v, want it kept inside the sane band [2, 8]", b.CharsPerToken)
	}
}

// TestBudgetDoesNotReshapeRequests pins "no behaviour change to requests themselves": even with a
// tiny window (an over-full budget), the loop sends and commits the conversation unaltered — the
// allocation is advisory until the item-9 reducers consume it, so nothing here truncates or injects.
func TestBudgetDoesNotReshapeRequests(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.Context.MaxContextTokens = 16 // absurdly small on purpose
	a, err := newAgent(cfg, usageResponder{
		content: "the assistant reply",
		usage:   provider.Usage{PromptTokens: 9, CompletionTokens: 4, TotalTokens: 13},
	})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	if err := a.Submit(domain.UserInput{Text: "the user question"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Fatalf("Step status = %q, want the Exchange to complete unaffected by the budget", res.Status)
	}
	if got := a.conv.At(0).Content; got != "the user question" {
		t.Errorf("user message = %q, want it unaltered by the budget", got)
	}
	if got := a.conv.At(1).Content; got != "the assistant reply" {
		t.Errorf("assistant message = %q, want it unaltered by the budget", got)
	}
}

// TestMaxOutputTokensDerivesFromTheReplyBudget pins the cap the engine states on the wire (ADR
// 0046) across the four inputs that decide it: an ordinary window derives the Budget's own
// ResponseReserve, a large one is clamped to the ceiling, an UNKNOWN one takes the floor rather
// than going uncapped (Allocation's contract: unknown is never "unbounded"), and a per-server pin
// beats the derivation outright at exactly the value written.
func TestMaxOutputTokensDerivesFromTheReplyBudget(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		window int
		pin    int
		want   int
	}{
		// The 2026-08-12 incident's server: 20% of 98,304 is the room the Budget already reserved,
		// so the runaway would have ended at ~53 minutes instead of running to the context wall.
		{name: "the reserve of an ordinary window", window: 98304, want: 19660},
		{name: "a big window is clamped to the ceiling", window: 200000, want: maxOutputTokenCap},
		{name: "an unknown window takes the floor", window: 0, want: minOutputTokenCap},
		{name: "the pin beats the derivation", window: 98304, pin: 500, want: 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := baseConfig(&recordingSink{})
			cfg.Context.MaxContextTokens = tc.window
			cfg.Context.MaxOutputTokens = tc.pin
			a, err := newAgent(cfg, echoResponder{reply: "unused"})
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			if got := a.maxOutputTokens(); got != tc.want {
				t.Errorf("maxOutputTokens() = %d, want %d (window %d, pin %d)", got, tc.want, tc.window, tc.pin)
			}
		})
	}
}

// TestTurnRequestCarriesTheOutputCap is the regression guard for the defect itself: before ADR 0046
// every agent Turn went out with no max_tokens at all, so a thinking model generated until the
// server's context wall. The provider request a Turn builds must now carry the derived cap.
func TestTurnRequestCarriesTheOutputCap(t *testing.T) {
	t.Parallel()

	cfg := baseConfig(&recordingSink{})
	cfg.Context.MaxContextTokens = 98304
	resp := &capturingResponder{reply: "ok"}
	driveOneStep(t, cfg, resp)

	got := resp.got.Sampling.MaxTokens
	if got == nil {
		t.Fatalf("provider request carried a nil MaxTokens — the reply is unbounded again")
	}
	if *got != 19660 {
		t.Errorf("MaxTokens = %d, want the derived cap 19660", *got)
	}
	if resp.got.Sampling.Temperature != nil {
		t.Errorf("Temperature = %v, want it left nil (the server's own default)", *resp.got.Sampling.Temperature)
	}
}

// cappingHook is a pre-request hook that states its own reply ceiling, standing in for any
// Mechanism that would.
type cappingHook struct{ tokens int }

func (h cappingHook) PreRequest(_ context.Context, req *domain.Request) error {
	req.SetSampling(domain.SamplingParams{MaxTokens: &h.tokens})
	return nil
}

// TestPreRequestHookBeatsTheOutputCap pins the ordering the loop's stamp depends on: the engine's
// derived cap is the LOOP's value, set before the hooks run, so a hook that sets MaxTokens still
// wins — SamplingParams's standing contract, true of this field for the first time (ADR 0046).
func TestPreRequestHookBeatsTheOutputCap(t *testing.T) {
	t.Parallel()

	cfg := baseConfig(&recordingSink{})
	cfg.Context.MaxContextTokens = 98304
	cfg.Mechanisms = domain.NewMechanismRegistry()
	if err := cfg.Mechanisms.AddExperimental(domain.HookPreRequest, cappingHook{tokens: 77}); err != nil {
		t.Fatalf("AddExperimental: %v", err)
	}
	resp := &capturingResponder{reply: "ok"}
	driveOneStep(t, cfg, resp)

	got := resp.got.Sampling.MaxTokens
	if got == nil || *got != 77 {
		t.Fatalf("MaxTokens = %v, want the hook's 77 rather than the loop's derived cap", got)
	}
}
