package agent

// The empty-reply guard: an Upstream reply carrying nothing the user can act on — no visible text
// and no tool calls — is a failure wearing a success's clothes (an aggregator's in-band error on an
// HTTP 200, a stream that ended before its first token). It must fail the Turn loudly rather than
// commit a blank assistant message that leaves the Turn looking answered. These tests pin the two
// halves of that contract: what the guard faults, and what it must let past — a post-response hook
// that retried and recovered real content keeps first claim, so the off-ramp still owns the Turn.

import (
	"context"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// thinkingOnlyScript is a stream that reasons and then stops without emitting one visible token —
// the shape a reasoning model produces when it thinks itself into silence.
func thinkingOnlyScript() []provider.Delta {
	return []provider.Delta{
		{Kind: provider.DeltaThinking, Thinking: "the user asked for a summary; I should start by..."},
		{Kind: provider.DeltaDone, FinishReason: "stop"},
	}
}

// TestEmptyReplyFailsTheTurn is the regression test for the observed silent turn (owner session
// 20260806T092047Z): an Upstream that answered with nothing committed `{"role":"assistant",
// "content":""}` and said nothing about it. Every row now ends the Turn the way a stream fault
// does — one ErrorEvent from source "loop" naming the finish reason, a faulted boundary, and only
// the user message left in history.
func TestEmptyReplyFailsTheTurn(t *testing.T) {
	tests := []struct {
		name    string
		script  []provider.Delta
		wantErr string
	}{
		{
			name:    "a bare done commits nothing",
			script:  emptyScript(),
			wantErr: "upstream returned an empty reply (finish: stop)",
		},
		{
			name:    "a thinking-only reply is a non-answer",
			script:  thinkingOnlyScript(),
			wantErr: "upstream returned an empty reply (finish: stop)",
		},
		{
			name:    "the finish reason rides along for diagnosis",
			script:  []provider.Delta{{Kind: provider.DeltaDone, FinishReason: "length"}},
			wantErr: "upstream returned an empty reply (finish: length)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			responder := &captureAllResponder{scripts: [][]provider.Delta{tc.script}}
			a, err := newAgent(baseConfig(sink), responder)
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			if err := a.Submit(domain.UserInput{Text: "summarize the repository"}); err != nil {
				t.Fatalf("Submit: %v", err)
			}

			res, err := a.Step(context.Background())
			if err != nil {
				t.Fatalf("Step: %v", err)
			}

			if res.Status != domain.StatusExchangeComplete || !res.Faulted {
				t.Errorf("StepResult = {Status:%q Faulted:%v}, want {Status:%q Faulted:true}",
					res.Status, res.Faulted, domain.StatusExchangeComplete)
			}
			errs := errorEvents(sink.events)
			if len(errs) != 1 {
				t.Fatalf("ErrorEvents = %d (%v), want exactly 1", len(errs), errs)
			}
			if errs[0].Source != "loop" || errs[0].Err != tc.wantErr {
				t.Errorf("ErrorEvent = {Source:%q Err:%q}, want {Source:%q Err:%q}",
					errs[0].Source, errs[0].Err, "loop", tc.wantErr)
			}
			if hasEvent[domain.MessageEvent](sink.events) {
				t.Error("a MessageEvent was emitted for a Turn that produced no reply")
			}
			if got := a.conv.Len(); got != 1 {
				t.Errorf("conv.Len() = %d, want 1 — only the user message survives a faulted Turn", got)
			}
			// The guard is terminal, not a retry: the Upstream was called exactly once.
			if got := len(responder.got); got != 1 {
				t.Errorf("provider was called %d times, want 1 (the guard re-requests nothing)", got)
			}
		})
	}
}

// TestEmptyReplyGuardYieldsToRecoveredRetry pins the guard's placement, which is the whole reason
// it sits after the post-response hook loop: a hook that answered the empty reply with an
// ActionRetry — what `empty_response_recovery` does — gets its second attempt, and content that
// arrives on it commits normally with no fault surfaced. The guard judges what the hook loop
// RESOLVED to, never the attempt the hooks are still working on.
func TestEmptyReplyGuardYieldsToRecoveredRetry(t *testing.T) {
	sink := &recordingSink{}
	calls := 0
	cfg := retryHookConfig(t, sink, scriptedRetryHook{injects: []string{"say something"}, calls: &calls})
	responder := &captureAllResponder{scripts: [][]provider.Delta{
		emptyScript(),
		contentScript("recovered answer"),
	}}

	a := driveExchange(t, cfg, responder, "please implement the parser")

	if errs := errorEvents(sink.events); len(errs) != 0 {
		t.Errorf("ErrorEvents = %v, want none — the retry recovered content before the guard ran", errs)
	}
	if me, ok := lastMessageEvent(sink.events); !ok || me.Text != "recovered answer" {
		t.Errorf("final MessageEvent = %+v (ok=%v), want %q", me, ok, "recovered answer")
	}
	if got := a.conv.Len(); got != 2 {
		t.Errorf("conv.Len() = %d, want 2 (user + the recovered assistant reply)", got)
	}
}
