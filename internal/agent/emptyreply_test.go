package agent

// The empty-reply guard: an Upstream reply carrying nothing the user can act on — no visible text
// and no tool calls — is a failure wearing a success's clothes (an aggregator's in-band error on an
// HTTP 200, a stream that ended before its first token). It must fail the Turn loudly rather than
// commit a blank assistant message that leaves the Turn looking answered. These tests pin the two
// halves of that contract: what the guard faults, and what it must let past — a post-response hook
// that retried and recovered real content keeps first claim, so the off-ramp still owns the Turn.
// A third set pins what the guard SAYS: a reply cut off at the engine's own output cap (ADR 0046)
// is told apart from an upstream that answered with nothing, because calling a 20k-token reasoning
// run "empty" names neither the cap nor what was burned reaching it — at every depth, so a child's
// empty capped reply reports its reasoning spend too. A fourth pins the one place the guard judges
// MORE than emptiness: on a delegate, a capped reply that carries visible text but no tool call
// faults, because a truncated answer reaches a parent MODEL that cannot see the cut.

import (
	"context"
	"fmt"
	"strings"
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
			script:  []provider.Delta{{Kind: provider.DeltaDone, FinishReason: "content_filter"}},
			wantErr: "upstream returned an empty reply (finish: content_filter)",
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
// it sits after the Floor guards and the post-response hook loop: whoever answered the empty reply
// with a retry — the empty-response recovery guard, or a hook — gets its second attempt, and content
// that arrives on it commits normally with no fault surfaced. The guard judges what the seam
// RESOLVED to, never the attempt still being worked on.
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

// cutOffScript is a stream that reasons and is then cut off mid-thought — the shape the 2026-08-12
// incident produces now that the engine states a ceiling: reasoning, not one visible token, and a
// finish reason of "length".
func cutOffScript() []provider.Delta {
	return []provider.Delta{
		{Kind: provider.DeltaThinking, Thinking: "let me enumerate every file in the repository before I answer..."},
		{Kind: provider.DeltaDone, FinishReason: "length"},
	}
}

// TestCutOffReplyNamesTheOutputCap pins the honest failure for the incident this branch exists for
// (ADR 0046): a reply that reasoned itself into the engine's OWN ceiling is not "an empty reply"
// — the model answered at length and apogee stopped it — so the fault names the cap, the remedy
// key, and what the reasoning cost, while failing the Turn exactly as every other empty reply does.
// The depth arm pins that this reading is not the main agent's alone: a CHILD that reasons itself
// silent is the run whose spend a parent most needs named, and the delegate wording (which reports
// no number) must not claim it just because the reply came from depth 1.
func TestCutOffReplyNamesTheOutputCap(t *testing.T) {
	tests := []struct {
		name          string
		depth         int
		script        []provider.Delta
		wantReasoning bool
	}{
		{
			name:          "a reply cut off mid-reasoning reports what it spent",
			script:        cutOffScript(),
			wantReasoning: true,
		},
		{
			name:          "a cut-off reply with no reasoning at all still names the cap",
			script:        []provider.Delta{{Kind: provider.DeltaDone, FinishReason: "length"}},
			wantReasoning: false,
		},
		{
			name:          "a delegate cut off mid-reasoning reports what it spent too",
			depth:         1,
			script:        cutOffScript(),
			wantReasoning: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			cfg := baseConfig(sink)
			cfg.Context.MaxContextTokens = 98304 // the incident's window: a 19,660-token derived cap
			responder := &captureAllResponder{scripts: [][]provider.Delta{tc.script}}
			a, err := newAgent(cfg, responder)
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			a.depth = tc.depth
			if err := a.Submit(domain.UserInput{Text: "audit the repository"}); err != nil {
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
			if errs[0].Source != "loop" {
				t.Errorf("ErrorEvent.Source = %q, want %q", errs[0].Source, "loop")
			}
			got := errs[0].Err
			if strings.Contains(got, "empty reply") {
				t.Errorf("ErrorEvent.Err = %q, want the cut-off message, not the empty-reply one", got)
			}
			if strings.Contains(got, "a truncated answer is not a result") {
				t.Errorf("ErrorEvent.Err = %q, want the cut-off message, not the delegate one — "+
					"there is no truncated answer here and the delegate wording reports no spend", got)
			}
			if !strings.Contains(got, "19660") {
				t.Errorf("ErrorEvent.Err = %q, want it to name the cap the engine sent (19660)", got)
			}
			if !strings.Contains(got, "max-output-tokens") {
				t.Errorf("ErrorEvent.Err = %q, want it to name the key that raises the cap", got)
			}
			if spent := strings.Contains(got, "tokens of reasoning"); spent != tc.wantReasoning {
				t.Errorf("ErrorEvent.Err = %q, reasoning spend reported = %v, want %v",
					got, spent, tc.wantReasoning)
			}
			if hasEvent[domain.MessageEvent](sink.events) {
				t.Error("a MessageEvent was emitted for a Turn that produced no reply")
			}
			if got := a.conv.Len(); got != 1 {
				t.Errorf("conv.Len() = %d, want 1 — only the user message survives a faulted Turn", got)
			}
			// Naming the cap changes the message, not the control flow: still no retry.
			if got := len(responder.got); got != 1 {
				t.Errorf("provider was called %d times, want 1 (the branch re-requests nothing)", got)
			}
		})
	}
}

// cappedWithTextScript is a stream that answers at length and is cut off mid-sentence: visible
// text present, no tool call, finish reason "length". It is the shape the 2026-08-25 delegate
// incident produced — an answer that LOOKS complete to whoever reads only the tokens.
func cappedWithTextScript() []provider.Delta {
	return []provider.Delta{
		{Kind: provider.DeltaContent, Content: "the audit found 14 issues; the first is that the parser"},
		{Kind: provider.DeltaDone, FinishReason: "length"},
	}
}

// TestCappedReplyWithTextFaultsOnlyOnADelegate pins the depth split the delegate rule introduces.
// At depth 0 a truncated reply commits exactly as it always did — the human reading it can see the
// cut and ask for the rest. At depth > 0 it faults instead, because the reader is the parent MODEL:
// it receives one tool result with no cut to see, and on 2026-08-25 accepted 223K characters of
// half-finished audit as a delegation's finding.
func TestCappedReplyWithTextFaultsOnlyOnADelegate(t *testing.T) {
	tests := []struct {
		name      string
		depth     int
		wantFault bool
	}{
		{name: "a delegate's truncated answer is not a result", depth: 1, wantFault: true},
		{name: "the main agent's truncated answer still commits", depth: 0, wantFault: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			cfg := baseConfig(sink)
			cfg.Context.MaxContextTokens = 98304 // the incident's window: a 19,660-token derived cap
			responder := &captureAllResponder{scripts: [][]provider.Delta{cappedWithTextScript()}}
			a, err := newAgent(cfg, responder)
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			a.depth = tc.depth
			if err := a.Submit(domain.UserInput{Text: "audit the repository"}); err != nil {
				t.Fatalf("Submit: %v", err)
			}

			res, err := a.Step(context.Background())
			if err != nil {
				t.Fatalf("Step: %v", err)
			}

			if res.Status != domain.StatusExchangeComplete || res.Faulted != tc.wantFault {
				t.Errorf("StepResult = {Status:%q Faulted:%v}, want {Status:%q Faulted:%v}",
					res.Status, res.Faulted, domain.StatusExchangeComplete, tc.wantFault)
			}
			errs := errorEvents(sink.events)
			if !tc.wantFault {
				if len(errs) != 0 {
					t.Fatalf("ErrorEvents = %v, want none — depth 0 is untouched by the delegate rule", errs)
				}
				if got := a.conv.Len(); got != 2 {
					t.Errorf("conv.Len() = %d, want 2 (the user message + the committed reply)", got)
				}
				return
			}
			if len(errs) != 1 {
				t.Fatalf("ErrorEvents = %d (%v), want exactly 1", len(errs), errs)
			}
			want := fmt.Sprintf(cappedDelegateReplyErrFmt, 19660)
			if errs[0].Source != "loop" || errs[0].Err != want {
				t.Errorf("ErrorEvent = {Source:%q Err:%q}, want {Source:%q Err:%q}",
					errs[0].Source, errs[0].Err, "loop", want)
			}
			if got := a.conv.Len(); got != 1 {
				t.Errorf("conv.Len() = %d, want 1 — the truncated text is discarded, never committed", got)
			}
			// The rule changes what is JUDGED, not what a fault does: still no retry.
			if got := len(responder.got); got != 1 {
				t.Errorf("provider was called %d times, want 1 (the rule re-requests nothing)", got)
			}
		})
	}
}
