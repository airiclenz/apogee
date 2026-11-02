package agent

import (
	"context"
	"iter"
	"sync"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// faultScript is a Responder that plays one scripted Delta sequence per request, in order, and
// repeats the last one once the script runs out. before, when set, runs at the start of each
// request's stream with that request's index — the hook a test uses to move the sharedUpstream
// while a request is in flight.
type faultScript struct {
	mu     sync.Mutex
	turns  [][]provider.Delta
	n      int
	before func(call int)
}

func (s *faultScript) Stream(context.Context, provider.Request) iter.Seq[provider.Delta] {
	s.mu.Lock()
	call := s.n
	s.n++
	deltas := s.turns[min(call, len(s.turns)-1)]
	s.mu.Unlock()
	return func(yield func(provider.Delta) bool) {
		if s.before != nil {
			s.before(call)
		}
		for _, d := range deltas {
			if !yield(d) {
				return
			}
		}
	}
}

// calls reports how many requests the script has been sent.
func (s *faultScript) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n
}

// faultDeltas is a request that ends on a terminal DeltaError of class fault, never Retryable, so
// it reaches the resolver without spending the Turn's re-stream budget first.
func faultDeltas(fault provider.UpstreamFault) []provider.Delta {
	return []provider.Delta{{Kind: provider.DeltaError, Err: "apogee: upstream fault", Fault: fault}}
}

// replyDeltas is a request answered with text and a clean stop.
func replyDeltas(text string) []provider.Delta {
	return []provider.Delta{
		{Kind: provider.DeltaContent, Content: text},
		{Kind: provider.DeltaDone, FinishReason: "stop"},
	}
}

// stubResolver is a FallbackResolver that records every report and answers with answer, or
// FallbackNone when answer is nil.
type stubResolver struct {
	mu      sync.Mutex
	reports []FallbackReport
	answer  func(ctx context.Context, report FallbackReport) FallbackAnswer
}

func (r *stubResolver) Resolve(ctx context.Context, report FallbackReport) FallbackAnswer {
	r.mu.Lock()
	r.reports = append(r.reports, report)
	r.mu.Unlock()
	if r.answer == nil {
		return FallbackAnswer{}
	}
	return r.answer(ctx, report)
}

// seen returns a copy of the reports the resolver has been handed.
func (r *stubResolver) seen() []FallbackReport {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]FallbackReport(nil), r.reports...)
}

// resolverAnswering is a stubResolver that gives every report the same verdict.
func resolverAnswering(verdict FallbackVerdict) *stubResolver {
	return &stubResolver{answer: func(context.Context, FallbackReport) FallbackAnswer {
		return FallbackAnswer{Verdict: verdict, First: true}
	}}
}

// newFallbackAgent builds an Agent over responder holding resolver, with a millisecond re-stream
// hold-off so a test that reaches the re-stream path does not sit through the production second.
func newFallbackAgent(t *testing.T, sink domain.EventSink, responder provider.Responder, resolver FallbackResolver) *Agent {
	t.Helper()
	a, err := newAgent(baseConfig(sink), responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	shortRestreamHoldoff(t, a)
	a.fallback = resolver
	return a
}

// respondOnce runs one respond phase of Turn 0 on a fresh request.
func respondOnce(t *testing.T, a *Agent) (*domain.Response, turnOutcome, *turnRun) {
	t.Helper()
	req, _ := a.buildRequest(0)
	run := &turnRun{turn: 0, req: req}
	resp, outcome, _ := a.respondAndReview(context.Background(), run)
	return resp, outcome, run
}

// TestFallbackConsultedOnlyForEligibleFaults pins the trigger: the resolver is asked about a fault
// that is the upstream's own (a failed connection, the idle cut, a cut stream, 429, 5xx, 529, an
// in-band 5xx or overload) and never about one a different server would share (400, 401, 413) or
// one apogee raised itself — and with a "none" answer every fault is abandoned exactly as it is
// with no resolver: one ErrorEvent, turnFailed.
func TestFallbackConsultedOnlyForEligibleFaults(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		fault    provider.UpstreamFault
		eligible bool
	}{
		{"transport", provider.UpstreamFault{Class: provider.FaultTransport}, true},
		{"timeout", provider.UpstreamFault{Class: provider.FaultTimeout}, true},
		{"cut", provider.UpstreamFault{Class: provider.FaultCut}, true},
		{"status 429", provider.UpstreamFault{Class: provider.FaultStatus, Code: 429}, true},
		{"status 503", provider.UpstreamFault{Class: provider.FaultStatus, Code: 503}, true},
		{"status 529", provider.UpstreamFault{Class: provider.FaultStatus, Code: 529}, true},
		{"in-band 502", provider.UpstreamFault{Class: provider.FaultInBand, Code: 502}, true},
		{"in-band overloaded", provider.UpstreamFault{Class: provider.FaultInBand, ErrorType: "overloaded_error"}, true},
		{"status 400", provider.UpstreamFault{Class: provider.FaultStatus, Code: 400}, false},
		{"status 401", provider.UpstreamFault{Class: provider.FaultStatus, Code: 401}, false},
		{"status 413", provider.UpstreamFault{Class: provider.FaultStatus, Code: 413}, false},
		{"local", provider.UpstreamFault{Class: provider.FaultLocal}, false},
		{"unclassified", provider.UpstreamFault{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sink := &recordingSink{}
			responder := &faultScript{turns: [][]provider.Delta{faultDeltas(tc.fault)}}
			resolver := resolverAnswering(FallbackNone)
			a := newFallbackAgent(t, sink, responder, resolver)

			_, outcome, _ := respondOnce(t, a)

			if outcome != turnFailed {
				t.Fatalf("outcome = %v, want turnFailed — a none answer abandons as today", outcome)
			}
			reports := resolver.seen()
			if tc.eligible && len(reports) != 1 {
				t.Fatalf("resolver reports = %d, want 1 for an eligible fault", len(reports))
			}
			if !tc.eligible && len(reports) != 0 {
				t.Fatalf("resolver reports = %v, want none for a fault a fallback would share", reports)
			}
			if tc.eligible && reports[0].Fault != tc.fault {
				t.Errorf("reported fault = %+v, want %+v", reports[0].Fault, tc.fault)
			}
			if got := len(errorEvents(sink.events)); got != 1 {
				t.Errorf("ErrorEvents = %d, want 1", got)
			}
			if responder.calls() != 1 {
				t.Errorf("Upstream calls = %d, want 1 — nothing is resent", responder.calls())
			}
		})
	}
}

// TestFallbackRecoveredResendsOnTheSameUpstream pins the "recovered" answer: the partial reply is
// superseded (one StreamResetEvent), the same request goes out again on the same Upstream with a
// fresh re-stream budget, and its answer is the Turn's — silently, no ErrorEvent.
func TestFallbackRecoveredResendsOnTheSameUpstream(t *testing.T) {
	t.Parallel()
	sink := &recordingSink{}
	responder := &faultScript{turns: [][]provider.Delta{
		faultDeltas(provider.UpstreamFault{Class: provider.FaultStatus, Code: 503}),
		replyDeltas("back again"),
	}}
	resolver := resolverAnswering(FallbackRecovered)
	a := newFallbackAgent(t, sink, responder, resolver)
	req, _ := a.buildRequest(0)
	run := &turnRun{turn: 0, req: req, restreamsSpent: a.restreamBudget()}

	resp, outcome, _ := a.respondAndReview(context.Background(), run)

	if outcome != turnOK || resp == nil || resp.Text() != "back again" {
		t.Fatalf("outcome = %v, resp = %+v; want turnOK with the resent reply", outcome, resp)
	}
	if responder.calls() != 2 {
		t.Errorf("Upstream calls = %d, want 2 — one fault, one resend", responder.calls())
	}
	if got := countEvents[domain.StreamResetEvent](sink.events); got != 1 {
		t.Errorf("StreamResetEvents = %d, want 1 before the resend", got)
	}
	if errs := errorEvents(sink.events); len(errs) != 0 {
		t.Errorf("ErrorEvents = %v, want none — a recovered resend is silent", errs)
	}
	if run.restreamsSpent != 0 {
		t.Errorf("restreamsSpent = %d, want 0 — the resend gets a fresh re-stream budget", run.restreamsSpent)
	}
	if !run.recoveredSpent {
		t.Error("recoveredSpent unset — the Turn's one recovered resend must be latched")
	}
	if reports := resolver.seen(); len(reports) != 1 || reports[0].RecoveredSpent || reports[0].Model != testModel {
		t.Errorf("reports = %+v, want one unflagged report naming %q", reports, testModel)
	}
}

// TestFallbackNoneAbandonsTheExchangeAsToday drives a whole Step: a "none" answer leaves the
// Exchange abandoned with the fault surfaced, exactly as an Agent with no resolver leaves it.
func TestFallbackNoneAbandonsTheExchangeAsToday(t *testing.T) {
	t.Parallel()
	for _, withResolver := range []bool{true, false} {
		sink := &recordingSink{}
		responder := &faultScript{turns: [][]provider.Delta{
			faultDeltas(provider.UpstreamFault{Class: provider.FaultTransport}),
		}}
		var resolver FallbackResolver
		if withResolver {
			resolver = resolverAnswering(FallbackNone)
		}
		a := newFallbackAgent(t, sink, responder, resolver)
		if err := a.Submit(domain.UserInput{Text: "ask the model"}); err != nil {
			t.Fatalf("Submit: %v", err)
		}

		res, err := a.Step(context.Background())

		if err != nil {
			t.Fatalf("Step: %v", err)
		}
		if !res.Faulted {
			t.Errorf("resolver=%v: Faulted unset, want the Exchange abandoned", withResolver)
		}
		if got := len(errorEvents(sink.events)); got != 1 {
			t.Errorf("resolver=%v: ErrorEvents = %d, want 1", withResolver, got)
		}
		if responder.calls() != 1 {
			t.Errorf("resolver=%v: Upstream calls = %d, want 1", withResolver, responder.calls())
		}
	}
}

// TestFallbackCancelDuringResolverWaitIsACancel pins that a cancel landing while the resolver waits
// out its grace window is the user's cancel, not the fault: the Turn rolls back and resumes, with
// no ErrorEvent, whatever the resolver hands back.
func TestFallbackCancelDuringResolverWaitIsACancel(t *testing.T) {
	t.Parallel()
	sink := &recordingSink{}
	responder := &faultScript{turns: [][]provider.Delta{
		faultDeltas(provider.UpstreamFault{Class: provider.FaultStatus, Code: 502}),
		replyDeltas("resumed"),
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resolver := &stubResolver{answer: func(waitCtx context.Context, _ FallbackReport) FallbackAnswer {
		cancel() // the user's Esc, landing inside the wait
		<-waitCtx.Done()
		return FallbackAnswer{Verdict: FallbackRecovered, First: true}
	}}
	a := newFallbackAgent(t, sink, responder, resolver)
	if err := a.Submit(domain.UserInput{Text: "ask the model"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	res, err := a.Step(ctx)

	if err != nil {
		t.Fatalf("Step returned a loop error on cancel: %v", err)
	}
	if res.Status != domain.StatusCancelled || res.Faulted {
		t.Fatalf("Step result = %+v, want a cancelled, unfaulted Turn", res)
	}
	if errs := errorEvents(sink.events); len(errs) != 0 {
		t.Errorf("ErrorEvents = %v, want none — the user's cancel is not a fault", errs)
	}
	if responder.calls() != 1 {
		t.Errorf("Upstream calls = %d, want 1 — nothing is resent into a cancelled context", responder.calls())
	}

	res2, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step (resumed): %v", err)
	}
	if res2.Status != domain.StatusExchangeComplete || res2.Faulted {
		t.Fatalf("resumed result = %+v, want a clean exchange-complete", res2)
	}
}

// TestFallbackFaultOnARetiredUpstreamResendsWithoutTheResolver pins the generation guard: a request
// that faults on an Upstream the sharedUpstream was replaced past while it streamed is resent on
// the current Upstream, and the resolver — which already answered for the retired server — is not
// asked.
func TestFallbackFaultOnARetiredUpstreamResendsWithoutTheResolver(t *testing.T) {
	t.Parallel()
	sink := &recordingSink{}
	current := &faultScript{turns: [][]provider.Delta{replyDeltas("from the current upstream")}}
	retired := &faultScript{turns: [][]provider.Delta{
		faultDeltas(provider.UpstreamFault{Class: provider.FaultCut}),
	}}
	resolver := resolverAnswering(FallbackNone)
	a := newFallbackAgent(t, sink, retired, resolver)
	retired.before = func(int) { a.upstream.replace(current) } // the seat moves while the request streams

	resp, outcome, _ := respondOnce(t, a)

	if outcome != turnOK || resp == nil || resp.Text() != "from the current upstream" {
		t.Fatalf("outcome = %v, resp = %+v; want the current upstream's reply", outcome, resp)
	}
	if reports := resolver.seen(); len(reports) != 0 {
		t.Errorf("resolver reports = %+v, want none — a retired upstream's fault is not asked about", reports)
	}
	if retired.calls() != 1 || current.calls() != 1 {
		t.Errorf("calls retired=%d current=%d, want 1 each", retired.calls(), current.calls())
	}
	if got := countEvents[domain.StreamResetEvent](sink.events); got != 1 {
		t.Errorf("StreamResetEvents = %d, want 1 before the resend", got)
	}
	if errs := errorEvents(sink.events); len(errs) != 0 {
		t.Errorf("ErrorEvents = %v, want none", errs)
	}
}

// TestFallbackFlappingPrimaryCannotLoopTheTurn pins the bound on recovery: a primary that recovers
// and fails again earns the Turn one recovered resend; the second fault reaches the resolver flagged
// RecoveredSpent, and a second "recovered" answer is read as none, so the Turn is abandoned rather
// than looping on a server that keeps coming back.
func TestFallbackFlappingPrimaryCannotLoopTheTurn(t *testing.T) {
	t.Parallel()
	sink := &recordingSink{}
	responder := &faultScript{turns: [][]provider.Delta{
		faultDeltas(provider.UpstreamFault{Class: provider.FaultStatus, Code: 503}),
	}}
	resolver := resolverAnswering(FallbackRecovered)
	a := newFallbackAgent(t, sink, responder, resolver)

	_, outcome, run := respondOnce(t, a)

	if outcome != turnFailed {
		t.Fatalf("outcome = %v, want turnFailed — the second fault after a recovered resend abandons", outcome)
	}
	if responder.calls() != 2 {
		t.Errorf("Upstream calls = %d, want 2 — one recovered resend, no more", responder.calls())
	}
	reports := resolver.seen()
	if len(reports) != 2 || reports[0].RecoveredSpent || !reports[1].RecoveredSpent {
		t.Fatalf("reports = %+v, want two, only the second flagged RecoveredSpent", reports)
	}
	if !run.recoveredSpent {
		t.Error("recoveredSpent unset after the recovered resend")
	}
	if got := len(errorEvents(sink.events)); got != 1 {
		t.Errorf("ErrorEvents = %d, want 1", got)
	}
}

// TestFallbackRestreamBudgetSpentBeforeTheResolver pins the order: a transient fault the re-stream
// budget covers spends that budget on the same upstream first, and only the fault that finds it
// spent reaches the resolver.
func TestFallbackRestreamBudgetSpentBeforeTheResolver(t *testing.T) {
	t.Parallel()
	sink := &recordingSink{}
	cut := []provider.Delta{{
		Kind: provider.DeltaError, Err: "apogee: read stream: unexpected EOF", Retryable: true,
		Fault: provider.UpstreamFault{Class: provider.FaultCut},
	}}
	responder := &faultScript{turns: [][]provider.Delta{cut}}
	resolver := resolverAnswering(FallbackNone)
	a := newFallbackAgent(t, sink, responder, resolver)

	_, outcome, _ := respondOnce(t, a)

	if outcome != turnFailed {
		t.Fatalf("outcome = %v, want turnFailed", outcome)
	}
	if want := a.restreamBudget() + 1; responder.calls() != want {
		t.Errorf("Upstream calls = %d, want %d — the re-stream budget is spent first", responder.calls(), want)
	}
	if got := len(resolver.seen()); got != 1 {
		t.Errorf("resolver reports = %d, want 1 — asked once, after the budget", got)
	}
}

// TestFallbackResolverSeededThroughNew pins the Option: WithFallbackResolver seats the resolver on
// the Agent New builds, so the respond phase consults it.
func TestFallbackResolverSeededThroughNew(t *testing.T) {
	t.Parallel()
	responder := &faultScript{turns: [][]provider.Delta{
		faultDeltas(provider.UpstreamFault{Class: provider.FaultTimeout}),
	}}
	resolver := resolverAnswering(FallbackNone)
	a, err := New(baseConfig(&recordingSink{}), WithDialer(dialerTo(responder).dial), WithFallbackResolver(resolver))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()
	if err := a.Submit(domain.UserInput{Text: "ask the model"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if got := len(resolver.seen()); got != 1 {
		t.Errorf("resolver reports = %d, want 1", got)
	}
}
