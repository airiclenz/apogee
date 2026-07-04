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
