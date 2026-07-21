package agent

// The overflow seam: a context-window rejection is classified apart from a generic Upstream
// fault, because only the former says something the loop can act on (the PROMPT did not fit, so a
// shorter history is a real remedy). These tests pin the two halves of that split — the outcome
// respondAndReview reports and, crucially, the ErrorEvent it does NOT emit — plus the observable
// behaviour a caller sees, which must stay exactly what a plain fault produces until recovery is
// wired in.

import (
	"context"
	"iter"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// faultResponder answers every stream with one terminal fault Delta of the given kind — the fake
// for both halves of the split (DeltaContextOverflow vs DeltaError) with everything else held
// equal, so a difference in the assertion is a difference in classification and nothing else.
type faultResponder struct {
	kind provider.DeltaKind
	msg  string
}

func (r faultResponder) Stream(context.Context, provider.Request) iter.Seq[provider.Delta] {
	return func(yield func(provider.Delta) bool) {
		yield(provider.Delta{Kind: r.kind, Err: r.msg})
	}
}

// overflowFaultMsg is the sanitized message the provider builds for a llama.cpp 400 whose body
// matches an overflow marker (statusDelta, internal/provider/stream.go) — the real shape of the
// text the loop must carry through the seam unchanged.
const overflowFaultMsg = "apogee: context window exceeded: " +
	`{"error":{"message":"request (57546 tokens) exceeds the available context size (32768 tokens)"}}`

// errorEvents returns every ErrorEvent among events, in order.
func errorEvents(events []domain.Event) []domain.ErrorEvent {
	var out []domain.ErrorEvent
	for _, e := range events {
		if ee, ok := e.(domain.ErrorEvent); ok {
			out = append(out, ee)
		}
	}
	return out
}

// TestRespondAndReviewSplitsOverflowFromPlainFault proves the seam: an overflowed request ends the
// respond phase as turnOverflowed and stays SILENT, carrying its message out to the caller (which
// owns the give-up event, so a recovered Turn can be quiet), while a generic fault keeps today's
// behaviour verbatim — turnFailed, one ErrorEvent from source "loop", nothing carried.
func TestRespondAndReviewSplitsOverflowFromPlainFault(t *testing.T) {
	tests := []struct {
		name        string
		kind        provider.DeltaKind
		msg         string
		wantOutcome turnOutcome
		wantCarried string
		wantEvents  int
	}{
		{
			name:        "overflow is its own outcome and surfaces nothing",
			kind:        provider.DeltaContextOverflow,
			msg:         overflowFaultMsg,
			wantOutcome: turnOverflowed,
			wantCarried: overflowFaultMsg,
			wantEvents:  0,
		},
		{
			name:        "a plain fault still fails loudly",
			kind:        provider.DeltaError,
			msg:         "apogee: upstream HTTP 500: boom",
			wantOutcome: turnFailed,
			wantCarried: "",
			wantEvents:  1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			a, err := newAgent(baseConfig(sink), faultResponder{kind: tc.kind, msg: tc.msg})
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			req, _ := a.buildRequest(0)

			resp, outcome, carried := a.respondAndReview(context.Background(), 0, req)

			if outcome != tc.wantOutcome {
				t.Errorf("outcome = %v, want %v", outcome, tc.wantOutcome)
			}
			if resp != nil {
				t.Errorf("resp = %+v, want nil on a terminal fault", resp)
			}
			if carried != tc.wantCarried {
				t.Errorf("carried message = %q, want %q", carried, tc.wantCarried)
			}
			errs := errorEvents(sink.events)
			if len(errs) != tc.wantEvents {
				t.Fatalf("ErrorEvents = %d (%v), want %d", len(errs), errs, tc.wantEvents)
			}
			if tc.wantEvents == 1 {
				if errs[0].Source != "loop" || errs[0].Err != tc.msg {
					t.Errorf("ErrorEvent = {Source:%q Err:%q}, want {Source:%q Err:%q}",
						errs[0].Source, errs[0].Err, "loop", tc.msg)
				}
			}
		})
	}
}

// TestStepOverflowStillAbandonsTheTurnUnchanged pins the observable contract the seam must not
// move: where recovery cannot run — baseConfig leaves `auto-compact` off, the decision-4 opt-out —
// an overflowed request degrades the Turn exactly as before: one ErrorEvent from source "loop"
// carrying the provider's message verbatim, a clean Exchange-complete boundary, and no assistant
// message committed. Recovery is quiet on SUCCESS (overflowrecovery_test.go); it may never change
// what the GIVE-UP looks like, and this is the anchor for that.
func TestStepOverflowStillAbandonsTheTurnUnchanged(t *testing.T) {
	sink := &recordingSink{}
	a, err := newAgent(baseConfig(sink), faultResponder{kind: provider.DeltaContextOverflow, msg: overflowFaultMsg})
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

	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("status = %q, want %q — an overflow ends the Exchange at a clean boundary",
			res.Status, domain.StatusExchangeComplete)
	}
	errs := errorEvents(sink.events)
	if len(errs) != 1 {
		t.Fatalf("ErrorEvents = %d (%v), want exactly 1", len(errs), errs)
	}
	if errs[0].Source != "loop" {
		t.Errorf("ErrorEvent.Source = %q, want %q", errs[0].Source, "loop")
	}
	if errs[0].Err != overflowFaultMsg {
		t.Errorf("ErrorEvent.Err = %q, want the provider's message verbatim %q", errs[0].Err, overflowFaultMsg)
	}
	if hasEvent[domain.MessageEvent](sink.events) {
		t.Error("a MessageEvent was emitted for a Turn that produced no assistant message")
	}
	if got := a.conv.Len(); got != 1 {
		msgs := a.conv.Messages()
		var roles []string
		for _, m := range msgs {
			roles = append(roles, string(m.Role))
		}
		t.Errorf("conv.Len() = %d (roles %s), want 1 — only the user message survives an abandoned Turn",
			got, strings.Join(roles, ","))
	}
}
