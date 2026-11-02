package agent

import (
	"context"
	"iter"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// faultScript is a Responder that plays one scripted Delta sequence per request, in order, and
// repeats the last one once the script runs out. before, when set, runs at the start of each
// request's stream with that request's index — the hook a test uses to move the sharedUpstream
// while a request is in flight.
type faultScript struct {
	mu       sync.Mutex
	turns    [][]provider.Delta
	n        int
	before   func(call int)
	requests []provider.Request
}

func (s *faultScript) Stream(_ context.Context, req provider.Request) iter.Seq[provider.Delta] {
	s.mu.Lock()
	call := s.n
	s.n++
	s.requests = append(s.requests, req)
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

// sent returns a copy of every request the script has been sent, in order.
func (s *faultScript) sent() []provider.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]provider.Request(nil), s.requests...)
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
	t.Cleanup(func() { _ = a.Close() })
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

// The seat rig: a primary server that answers 503 to every request, a "spare" fallback that answers
// with text, a "grunt" Sub-agent server, and a parent Agent on the primary holding a resolver that
// moves the seat to the spare.
const (
	seatPrimaryEndpoint = "http://primary.local:1111"
	seatSpareEndpoint   = "http://spare.local:2222"
	seatGruntEndpoint   = "http://grunt.local:3333"
	spareModel          = "spare-model"
	sparePrompt         = "You are on the SPARE server."
	spareWindow         = 8192
)

// spareAnswer is the FallbackSwitch answer that moves a seat to the spare: its dial facts and the
// bindings for the model its beat observed.
func spareAnswer() FallbackAnswer {
	return FallbackAnswer{
		Verdict: FallbackSwitch,
		Upstream: UpstreamSpec{
			Endpoint: seatSpareEndpoint, APIKey: "spare-key", ServerName: "spare",
			Vision: true, MaxContextTokens: spareWindow,
		},
		Rebind: RebindSpec{
			Model: spareModel, SystemPrompt: sparePrompt, MaxContextTokens: spareWindow,
			Profile: domain.ModelProfile{Thinking: domain.ThinkingProfile{Effort: domain.EffortLow}},
		},
		Server: "spare",
		Waited: 3 * time.Second,
		First:  true,
	}
}

type seatRig struct {
	sink                  *recordingSink
	primary, spare, grunt *faultScript
	dialer                *fakeDialer
	resolver              *stubResolver
	parent                *Agent
}

// newSeatRig builds the rig; edit, when set, adjusts the parent's Config first.
func newSeatRig(t *testing.T, edit func(*domain.Config)) *seatRig {
	t.Helper()
	r := &seatRig{
		sink:    &recordingSink{},
		primary: &faultScript{turns: [][]provider.Delta{faultDeltas(provider.UpstreamFault{Class: provider.FaultStatus, Code: 503})}},
		spare:   &faultScript{turns: [][]provider.Delta{replyDeltas("from the spare")}},
		grunt:   &faultScript{turns: [][]provider.Delta{replyDeltas("from the grunt")}},
		resolver: &stubResolver{answer: func(context.Context, FallbackReport) FallbackAnswer {
			return spareAnswer()
		}},
	}
	r.dialer = dialerAnswering(func(endpoint string) provider.Responder {
		switch endpoint {
		case seatSpareEndpoint:
			return r.spare
		case seatGruntEndpoint:
			return r.grunt
		default:
			return r.primary
		}
	})
	cfg := baseConfig(r.sink)
	cfg.Endpoint = seatPrimaryEndpoint
	cfg.ServerName = "box"
	cfg.Context.MaxContextTokens = 1_000_000
	if edit != nil {
		edit(&cfg)
	}
	parent, err := newAgent(cfg, r.primary)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	shortRestreamHoldoff(t, parent)
	parent.fallback = r.resolver
	parent.dial = r.dialer.dial
	r.parent = parent
	return r
}

// child spawns an unrouted child of the rig's parent.
func (r *seatRig) child(t *testing.T, name string) *Agent {
	t.Helper()
	child, err := r.parent.newChildAgent("call_"+name, "the delegated task "+name, name)
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}
	return child
}

// switchedEvents returns every UpstreamSwitchedEvent the rig's sink recorded.
func (r *seatRig) switchedEvents() []domain.UpstreamSwitchedEvent {
	var out []domain.UpstreamSwitchedEvent
	for _, e := range r.sink.events {
		if ev, ok := e.(domain.UpstreamSwitchedEvent); ok {
			out = append(out, ev)
		}
	}
	return out
}

// lastSpareModel is the model the spare's latest request named.
func (r *seatRig) lastSpareModel(t *testing.T) string {
	t.Helper()
	sent := r.spare.sent()
	if len(sent) == 0 {
		t.Fatal("the spare was sent no request")
	}
	return sent[len(sent)-1].Model
}

// fallbackDials counts the dials of the spare's endpoint.
func (r *seatRig) fallbackDials() []dialRecord {
	var out []dialRecord
	for _, d := range r.dialer.dialled() {
		if d.endpoint == seatSpareEndpoint {
			out = append(out, d)
		}
	}
	return out
}

// countingPreRequest is an armed pre-request Reaction that counts its firings.
func countingPreRequest(n *int) domain.Reaction {
	return domain.Reaction{
		ID:     "count-pre-request",
		Origin: domain.OriginEngine,
		Class:  domain.ClassObserve,
		On:     []domain.Moment{domain.MomentPreRequest},
		Handler: domain.PreRequestFunc(func(context.Context, *domain.Request) (domain.Outcome, error) {
			*n++
			return domain.Outcome{}, nil
		}),
	}
}

// TestFallbackSwitchRehomesTheTurnOntoTheFallback pins the mid-Turn rehome: a FallbackSwitch answer
// discards the failed reply, dials the fallback once, binds its model, prompt, window and profile,
// re-arms the request — the pre-request Moment fires again and the deferred correction rides the
// resend — and the Turn completes on the fallback, announced by one UpstreamSwitchedEvent.
func TestFallbackSwitchRehomesTheTurnOntoTheFallback(t *testing.T) {
	t.Parallel()
	preRequests := 0
	r := newSeatRig(t, nil)
	r.parent.cfg.Reactions = nil
	r.parent.installGeneration(domain.Generation{Sync: []domain.Reaction{countingPreRequest(&preRequests)}})
	r.parent.conv.Defer("DEFERRED-CORRECTION")

	res := stepOnce(t, r.parent, "ask the model")

	if res.Status != domain.StatusExchangeComplete || res.Faulted {
		t.Fatalf("result = %+v, want a completed, unfaulted Exchange", res)
	}
	if r.primary.calls() != 1 || r.spare.calls() != 1 {
		t.Fatalf("calls primary=%d spare=%d, want 1 each", r.primary.calls(), r.spare.calls())
	}
	req := r.spare.sent()[0]
	if req.Model != spareModel {
		t.Errorf("resent request model = %q, want %q", req.Model, spareModel)
	}
	if len(req.Messages) == 0 || !strings.Contains(req.Messages[0].Content, sparePrompt) {
		t.Errorf("resent request does not lead with the fallback's system prompt: %+v", req.Messages)
	}
	if !requestCarries(req, "DEFERRED-CORRECTION") {
		t.Error("the deferred correction did not survive into the resent request")
	}
	if preRequests != 2 {
		t.Errorf("pre-request Moment fired %d times, want 2 — once per request", preRequests)
	}
	if got := r.parent.cfg; got.ServerName != "spare" || got.Model != spareModel || got.Context.MaxContextTokens != spareWindow {
		t.Errorf("bound server=%q model=%q window=%d, want the spare's", got.ServerName, got.Model, got.Context.MaxContextTokens)
	}
	if _, profile := r.parent.ThinkingEffort(); profile != domain.EffortLow {
		t.Errorf("profile effort = %q, want the RebindSpec profile's %q", profile, domain.EffortLow)
	}
	if !r.parent.Vision() {
		t.Error("Vision() = false, want the spare's vision opt-in")
	}
	dials := r.fallbackDials()
	if len(dials) != 1 || dials[0].model != spareModel || dials[0].apiKey != "spare-key" {
		t.Errorf("fallback dials = %+v, want one, with the spare's model and key", dials)
	}
	events := r.switchedEvents()
	if len(events) != 1 {
		t.Fatalf("UpstreamSwitchedEvents = %d, want 1", len(events))
	}
	want := domain.UpstreamSwitchedEvent{
		EventBase:  events[0].EventBase,
		FromServer: "box", FromModel: testModel, ToServer: "spare", ToModel: spareModel,
		Fault: "status 503", Waited: 3 * time.Second, Seat: domain.UpstreamSeatSession,
	}
	if events[0] != want || events[0].Depth != 0 {
		t.Errorf("event = %+v, want %+v at depth 0", events[0], want)
	}
	if countEvents[domain.StreamResetEvent](r.sink.events) == 0 {
		t.Error("no StreamResetEvent: the failed reply's partial was never discarded")
	}
	if errs := errorEvents(r.sink.events); len(errs) != 0 {
		t.Errorf("ErrorEvents = %v, want none", errs)
	}
	if reports := r.resolver.seen(); len(reports) != 1 || reports[0].Server != "box" || reports[0].Model != testModel {
		t.Errorf("resolver reports = %+v, want one about box/%s", reports, testModel)
	}
}

// requestCarries reports whether any message of req contains text.
func requestCarries(req provider.Request, text string) bool {
	for _, m := range req.Messages {
		if strings.Contains(m.Content, text) {
			return true
		}
	}
	return false
}

// TestFallbackSwitchItCannotApplyAbandonsTheTurn pins validate-before-move: a switch answer naming
// no model moves nothing — no dial, no event — and the Turn is abandoned as with no resolver.
func TestFallbackSwitchItCannotApplyAbandonsTheTurn(t *testing.T) {
	t.Parallel()
	r := newSeatRig(t, nil)
	r.resolver.answer = func(context.Context, FallbackReport) FallbackAnswer {
		answer := spareAnswer()
		answer.Rebind.Model = ""
		return answer
	}

	_, outcome, _ := respondOnce(t, r.parent)

	if outcome != turnFailed {
		t.Fatalf("outcome = %v, want turnFailed", outcome)
	}
	if dials := r.fallbackDials(); len(dials) != 0 {
		t.Errorf("fallback dials = %+v, want none", dials)
	}
	if n := len(r.switchedEvents()); n != 0 {
		t.Errorf("UpstreamSwitchedEvents = %d, want none", n)
	}
	if got := len(errorEvents(r.sink.events)); got != 1 {
		t.Errorf("ErrorEvents = %d, want 1", got)
	}
}

// TestFallbackSwitchByAnUnroutedChildMovesTheSessionSeat pins the shared seat: an unrouted child
// that hits the fault moves the SESSION's Upstream (Seat "session", whatever the emitter's depth),
// and the parent's next Step binds the fallback before its boundary fold — the fold is judged
// against the fallback's window, the request names the fallback's model, and the primary hears
// nothing more.
func TestFallbackSwitchByAnUnroutedChildMovesTheSessionSeat(t *testing.T) {
	t.Parallel()
	r := newSeatRig(t, func(cfg *domain.Config) { cfg.Context.CompactionEnabled = true })
	child := r.child(t, "trigger")
	if child.fallback != r.resolver {
		t.Fatal("the unrouted child did not inherit the session's resolver")
	}

	stepOnce(t, child, "delegated work")

	events := r.switchedEvents()
	if len(events) != 1 || events[0].Seat != domain.UpstreamSeatSession || events[0].Depth != 1 {
		t.Fatalf("events = %+v, want one session-seat switch emitted at depth 1", events)
	}
	if got := r.lastSpareModel(t); got != spareModel {
		t.Errorf("child resend model = %q, want %q", got, spareModel)
	}

	// Primary window 1,000,000: this history fits it. The spare's 8192 does not.
	seedLargeConv(r.parent)
	seeded := r.parent.conv.Len()
	sentBefore := r.spare.calls()

	stepOnce(t, r.parent, "the parent's next question")

	if r.parent.conv.Len() > seeded {
		t.Errorf("conv.Len() = %d after the Step, want a fold below the seeded %d — the boundary fold was judged against the primary's window", r.parent.conv.Len(), seeded)
	}
	if r.spare.calls() != sentBefore+2 {
		t.Errorf("spare calls = %d, want %d — the fold's summary and the Turn's request", r.spare.calls(), sentBefore+2)
	}
	if got := r.lastSpareModel(t); got != spareModel {
		t.Errorf("parent request model = %q, want %q", got, spareModel)
	}
	if r.parent.cfg.Context.MaxContextTokens != spareWindow {
		t.Errorf("parent window = %d, want the spare's %d", r.parent.cfg.Context.MaxContextTokens, spareWindow)
	}
	if r.primary.calls() != 1 {
		t.Errorf("primary calls = %d, want only the child's faulted one", r.primary.calls())
	}
	if n := len(r.switchedEvents()); n != 1 {
		t.Errorf("UpstreamSwitchedEvents = %d, want still 1", n)
	}
}

// backgroundChild spawns an unrouted child off a background workflow's host of the rig's parent —
// the launch-time snapshot a background workflow runs off (backgroundHost) — and returns both.
func (r *seatRig) backgroundChild(t *testing.T, name string) (host, child *Agent) {
	t.Helper()
	host = r.parent.backgroundHost()
	child, err := host.newChildAgent("call_"+name, "the background item "+name, name)
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}
	return host, child
}

// switchAway runs the human's `/server` on the rig's parent, which retires the holder the parent
// spoke over (SwitchUpstream) — the one a background workflow's host copied.
func (r *seatRig) switchAway(t *testing.T) {
	t.Helper()
	if err := r.parent.SwitchUpstream(UpstreamSpec{Endpoint: "http://elsewhere.local:4444"}); err != nil {
		t.Fatalf("SwitchUpstream: %v", err)
	}
}

// TestFallbackBackgroundChildConsultsTheSessionResolver pins a background workflow on the session's
// live holder: its host carries the session seat's resolver, so a child that hits the fault moves
// the SESSION's seat — one session-seat switch event — and the parent follows at its next Step.
func TestFallbackBackgroundChildConsultsTheSessionResolver(t *testing.T) {
	t.Parallel()
	r := newSeatRig(t, nil)
	host, child := r.backgroundChild(t, "item")
	if host.fallback != r.resolver || child.fallback != r.resolver {
		t.Fatal("the background host or its child did not carry the session's resolver")
	}

	res := stepOnce(t, child, "background item work")

	if res.Faulted {
		t.Fatalf("child result = %+v, want the item finished on the fallback", res)
	}
	if n := len(r.resolver.seen()); n != 1 {
		t.Errorf("resolver reports = %d, want 1", n)
	}
	events := r.switchedEvents()
	if len(events) != 1 || events[0].Seat != domain.UpstreamSeatSession || events[0].ToServer != "spare" {
		t.Fatalf("events = %+v, want one session-seat switch onto the spare", events)
	}
	if got := r.lastSpareModel(t); got != spareModel {
		t.Errorf("child resend model = %q, want %q", got, spareModel)
	}

	stepOnce(t, r.parent, "the parent's next question")

	if r.parent.cfg.ServerName != "spare" || r.parent.cfg.Model != spareModel {
		t.Errorf("parent bound server=%q model=%q, want the spare's", r.parent.cfg.ServerName, r.parent.cfg.Model)
	}
	if got := r.lastSpareModel(t); got != spareModel {
		t.Errorf("parent request model = %q, want %q", got, spareModel)
	}
	if r.primary.calls() != 1 {
		t.Errorf("primary calls = %d, want only the child's faulted one", r.primary.calls())
	}
}

// TestFallbackBackgroundChildOnARetiredHolderAbandons pins the exception: once `/server` has moved
// the session off the holder a background workflow copied, a fault there is abandoned as before
// Server fallback (ADR 0089) — the resolver is not asked and no seat moves.
func TestFallbackBackgroundChildOnARetiredHolderAbandons(t *testing.T) {
	t.Parallel()
	r := newSeatRig(t, nil)
	_, child := r.backgroundChild(t, "item")
	r.switchAway(t)

	res := stepOnce(t, child, "background item work")

	if !res.Faulted {
		t.Errorf("child result = %+v, want the item abandoned", res)
	}
	if reports := r.resolver.seen(); len(reports) != 0 {
		t.Errorf("resolver reports = %+v, want none on a retired holder", reports)
	}
	if n := len(r.switchedEvents()); n != 0 {
		t.Errorf("UpstreamSwitchedEvents = %d, want none", n)
	}
	if dials := r.fallbackDials(); len(dials) != 0 {
		t.Errorf("fallback dials = %+v, want none", dials)
	}
	if got := len(errorEvents(r.sink.events)); got != 1 {
		t.Errorf("ErrorEvents = %d, want 1", got)
	}
}

// TestFallbackBackgroundChildRetiredMidResolveAbandons pins the race the retired flag closes: a
// background child whose resolver is still waiting when `/server` retires its holder abandons on
// the switch answer that follows — no move, no session-seat switch announced after the human's.
func TestFallbackBackgroundChildRetiredMidResolveAbandons(t *testing.T) {
	t.Parallel()
	r := newSeatRig(t, nil)
	_, child := r.backgroundChild(t, "item")
	var switchErr error
	r.resolver.answer = func(context.Context, FallbackReport) FallbackAnswer {
		switched := make(chan struct{})
		go func() { // the human's `/server`, landing while the resolver waits
			defer close(switched)
			switchErr = r.parent.SwitchUpstream(UpstreamSpec{Endpoint: "http://elsewhere.local:4444"})
		}()
		<-switched
		return spareAnswer()
	}

	res := stepOnce(t, child, "background item work")

	if switchErr != nil {
		t.Fatalf("SwitchUpstream: %v", switchErr)
	}

	if !res.Faulted {
		t.Errorf("child result = %+v, want the item abandoned", res)
	}
	if n := len(r.resolver.seen()); n != 1 {
		t.Errorf("resolver reports = %d, want the one in flight when the holder was retired", n)
	}
	if n := len(r.switchedEvents()); n != 0 {
		t.Errorf("UpstreamSwitchedEvents = %d, want none", n)
	}
	if dials := r.fallbackDials(); len(dials) != 0 {
		t.Errorf("fallback dials = %+v, want none", dials)
	}
	if r.spare.calls() != 0 {
		t.Errorf("spare calls = %d, want none", r.spare.calls())
	}
	if got := len(errorEvents(r.sink.events)); got != 1 {
		t.Errorf("ErrorEvents = %d, want 1", got)
	}
}

// TestFallbackSwitchFollowedByEveryAgentOnTheSeat pins the followers: a child spawned before the
// move and one spawned after it — from a parent that has not stepped since, so it carries the
// parent's APPLIED generation rather than the holder's — both stream from the fallback with its
// model, and a child spawned once the parent has followed starts already bound.
func TestFallbackSwitchFollowedByEveryAgentOnTheSeat(t *testing.T) {
	t.Parallel()
	r := newSeatRig(t, nil)
	early := r.child(t, "early")
	stepOnce(t, r.child(t, "trigger"), "move the seat")

	late := r.child(t, "late")
	if late.appliedGen != r.parent.appliedGen || late.appliedGen != 0 {
		t.Fatalf("late child appliedGen = %d, parent's = %d; want the parent's applied 0", late.appliedGen, r.parent.appliedGen)
	}
	for name, child := range map[string]*Agent{"early": early, "late": late} {
		stepOnce(t, child, "work on the seat")
		if got := r.lastSpareModel(t); got != spareModel {
			t.Errorf("%s child request model = %q, want %q", name, got, spareModel)
		}
		if child.cfg.ServerName != "spare" {
			t.Errorf("%s child server = %q, want spare", name, child.cfg.ServerName)
		}
	}

	stepOnce(t, r.parent, "the parent follows")
	if r.parent.appliedGen != 1 {
		t.Fatalf("parent appliedGen = %d after its Step, want 1", r.parent.appliedGen)
	}
	bound := r.child(t, "bound")
	if bound.appliedGen != 1 || bound.cfg.Model != spareModel {
		t.Errorf("child spawned after the follow: appliedGen=%d model=%q, want 1 and %q", bound.appliedGen, bound.cfg.Model, spareModel)
	}
	if r.primary.calls() != 1 {
		t.Errorf("primary calls = %d, want only the trigger's", r.primary.calls())
	}
	if n := len(r.switchedEvents()); n != 1 {
		t.Errorf("UpstreamSwitchedEvents = %d, want 1", n)
	}
}

// TestFallbackSwitchAnnouncedOnceForManyAgentsOnOneSeat pins one announcement per move: N agents on
// one sharedUpstream fault on the same generation at once, the seat is dialled and moved once, one
// UpstreamSwitchedEvent is emitted, and every agent finishes on the fallback.
func TestFallbackSwitchAnnouncedOnceForManyAgentsOnOneSeat(t *testing.T) {
	t.Parallel()
	const n = 3
	r := newSeatRig(t, nil)
	var arrived sync.WaitGroup
	arrived.Add(n)
	release := make(chan struct{})
	r.primary.before = func(int) {
		arrived.Done()
		<-release
	}
	children := make([]*Agent, n)
	for i := range children {
		children[i] = r.child(t, "sibling")
		if err := children[i].Submit(domain.UserInput{Text: "work"}); err != nil {
			t.Fatalf("Submit: %v", err)
		}
	}
	go func() {
		arrived.Wait()
		close(release)
	}()

	var wg sync.WaitGroup
	results := make([]domain.StepResult, n)
	for i, child := range children {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := child.Step(context.Background())
			if err != nil {
				t.Errorf("Step: %v", err)
			}
			results[i] = res
		}()
	}
	wg.Wait()

	for i, res := range results {
		if res.Faulted {
			t.Errorf("child %d faulted: %+v", i, res)
		}
	}
	if got := len(r.switchedEvents()); got != 1 {
		t.Errorf("UpstreamSwitchedEvents = %d, want 1", got)
	}
	if dials := r.fallbackDials(); len(dials) != 1 {
		t.Errorf("fallback dials = %d, want 1", len(dials))
	}
	if r.primary.calls() != n || r.spare.calls() != n {
		t.Errorf("calls primary=%d spare=%d, want %d each", r.primary.calls(), r.spare.calls(), n)
	}
}

// TestFallbackSwitchSiblingSentOnThePrimaryResendsOnTheFallback pins the generation guard across a
// move: a sibling whose request went out on the primary before another agent moved the seat, and
// which faults after, resends on the fallback with the fallback's binding — without asking the
// resolver, which already answered for the primary.
func TestFallbackSwitchSiblingSentOnThePrimaryResendsOnTheFallback(t *testing.T) {
	t.Parallel()
	r := newSeatRig(t, nil)
	sibling := r.child(t, "sibling")
	mover := r.child(t, "mover")
	r.primary.before = func(call int) {
		if call == 0 {
			stepOnce(t, mover, "move the seat while the sibling streams")
		}
	}

	stepOnce(t, sibling, "sent on the primary")

	if got := len(r.resolver.seen()); got != 1 {
		t.Errorf("resolver reports = %d, want 1 — the mover's; the sibling's fault asks nothing", got)
	}
	if got := r.lastSpareModel(t); got != spareModel {
		t.Errorf("sibling resend model = %q, want %q", got, spareModel)
	}
	if sibling.cfg.ServerName != "spare" || sibling.appliedGen != 1 {
		t.Errorf("sibling server=%q appliedGen=%d, want spare and 1", sibling.cfg.ServerName, sibling.appliedGen)
	}
	if r.primary.calls() != 2 || r.spare.calls() != 2 {
		t.Errorf("calls primary=%d spare=%d, want 2 each", r.primary.calls(), r.spare.calls())
	}
	if n := len(r.switchedEvents()); n != 1 {
		t.Errorf("UpstreamSwitchedEvents = %d, want 1", n)
	}
	if errs := errorEvents(r.sink.events); len(errs) != 0 {
		t.Errorf("ErrorEvents = %v, want none", errs)
	}
}

// TestFallbackSwitchAccessorReadsRaceTheRehomeCleanly pins the rehome's guard: the accessors a
// Driver calls from its own goroutine while a Step runs read the binding the rehome writes
// mid-Turn, and -race finds nothing.
func TestFallbackSwitchAccessorReadsRaceTheRehomeCleanly(t *testing.T) {
	t.Parallel()
	r := newSeatRig(t, nil)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = r.parent.Vision()
			_ = r.parent.VisionRefusal()
			_, _ = r.parent.ThinkingEffort()
			r.parent.SetAllowRules(nil)
		}
	}()

	stepOnce(t, r.parent, "ask the model")
	close(stop)
	<-done

	if !r.parent.Vision() {
		t.Error("Vision() = false after the rehome, want the spare's opt-in")
	}
}

// TestFallbackSwitchLeavesARoutedChildOnItsOwnSeat pins the seat boundary: a routed child speaks
// over a holder of its own on the Sub-agents seat, holds none of the session's resolver, and stays
// on its routed server when the session's seat moves.
func TestFallbackSwitchLeavesARoutedChildOnItsOwnSeat(t *testing.T) {
	t.Parallel()
	r := newSeatRig(t, nil)
	r.parent.SetDelegationTarget(&DelegationTarget{Endpoint: seatGruntEndpoint, Model: "grunt-model"})
	routed := r.child(t, "routed")
	if routed.upstream == r.parent.upstream || routed.upstream.seatOf() != domain.UpstreamSeatSubAgents {
		t.Fatalf("routed child seat = %q on the parent's holder = %v; want a Sub-agents holder of its own",
			routed.upstream.seatOf(), routed.upstream == r.parent.upstream)
	}
	if routed.fallback != nil {
		t.Error("the routed child holds the session's resolver")
	}

	stepOnce(t, r.parent, "move the session seat")
	stepOnce(t, routed, "routed work")

	if r.grunt.calls() != 1 || r.grunt.sent()[0].Model != "grunt-model" {
		t.Errorf("grunt calls = %d, want 1 on grunt-model", r.grunt.calls())
	}
	if routed.cfg.Model != "grunt-model" || routed.appliedGen != 0 {
		t.Errorf("routed child model=%q appliedGen=%d, want grunt-model and 0", routed.cfg.Model, routed.appliedGen)
	}
	if r.spare.calls() != 1 {
		t.Errorf("spare calls = %d, want only the parent's", r.spare.calls())
	}
}

// firstOnceResolver is a seat resolver that answers every report with the spare and marks only the
// first report First, the way the Driver's single-flight resolver does.
func firstOnceResolver() *stubResolver {
	var mu sync.Mutex
	answered := false
	return &stubResolver{answer: func(context.Context, FallbackReport) FallbackAnswer {
		mu.Lock()
		defer mu.Unlock()
		answer := spareAnswer()
		answer.First = !answered
		answered = true
		return answer
	}}
}

// TestFallbackRoutedChildConsultsItsTargetsResolver pins the routed seat's own fallover: a routed
// child holds the resolver its Delegation target carries — never the session's — reports the
// Sub-agent server it dials, continues on the fallback, and the one event names the Sub-agents seat.
func TestFallbackRoutedChildConsultsItsTargetsResolver(t *testing.T) {
	t.Parallel()
	r := newSeatRig(t, nil)
	r.grunt.turns = [][]provider.Delta{faultDeltas(provider.UpstreamFault{Class: provider.FaultStatus, Code: 503})}
	seatResolver := firstOnceResolver()
	r.parent.SetDelegationTarget(&DelegationTarget{
		Endpoint: seatGruntEndpoint, Model: "grunt-model", ServerName: "grunt", Fallback: seatResolver,
	})
	routed := r.child(t, "routed")
	if routed.fallback != seatResolver {
		t.Fatal("the routed child does not hold its target's resolver")
	}

	res := stepOnce(t, routed, "routed work")

	if res.Faulted {
		t.Fatalf("routed child faulted: %+v", res)
	}
	if got := r.spare.calls(); got != 1 {
		t.Errorf("spare calls = %d, want 1", got)
	}
	if got := len(r.resolver.seen()); got != 0 {
		t.Errorf("the session resolver was consulted %d times, want 0", got)
	}
	reports := seatResolver.seen()
	if len(reports) != 1 || reports[0].Server != "grunt" || reports[0].Model != "grunt-model" {
		t.Errorf("seat resolver reports = %+v, want one on grunt/grunt-model", reports)
	}
	events := r.switchedEvents()
	if len(events) != 1 || events[0].Seat != domain.UpstreamSeatSubAgents ||
		events[0].FromServer != "grunt" || events[0].ToServer != "spare" {
		t.Fatalf("switched events = %+v, want one sub-agents grunt → spare", events)
	}
	if r.parent.upstream.responder() != r.primary {
		t.Error("the routed fallover moved the session's seat")
	}
}

// TestFallbackRoutedSiblingsAnnounceOnce pins one announcement per Sub-agents seat: N routed
// siblings, each on a holder of its own, fault on one dead Sub-agent server at once; each moves its
// own holder onto the fallback and finishes there, and only the First caller announces the move.
func TestFallbackRoutedSiblingsAnnounceOnce(t *testing.T) {
	t.Parallel()
	const n = 3
	r := newSeatRig(t, nil)
	r.grunt.turns = [][]provider.Delta{faultDeltas(provider.UpstreamFault{Class: provider.FaultStatus, Code: 503})}
	r.parent.SetDelegationTarget(&DelegationTarget{
		Endpoint: seatGruntEndpoint, Model: "grunt-model", ServerName: "grunt", Fallback: firstOnceResolver(),
	})
	children := make([]*Agent, n)
	for i := range children {
		children[i] = r.child(t, "routed")
		if err := children[i].Submit(domain.UserInput{Text: "work"}); err != nil {
			t.Fatalf("Submit: %v", err)
		}
	}

	var wg sync.WaitGroup
	results := make([]domain.StepResult, n)
	for i, child := range children {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := child.Step(context.Background())
			if err != nil {
				t.Errorf("Step: %v", err)
			}
			results[i] = res
		}()
	}
	wg.Wait()

	for i, res := range results {
		if res.Faulted {
			t.Errorf("routed child %d faulted: %+v", i, res)
		}
	}
	if got := len(r.switchedEvents()); got != 1 {
		t.Errorf("UpstreamSwitchedEvents = %d, want 1", got)
	}
	if got := r.spare.calls(); got != n {
		t.Errorf("spare calls = %d, want %d", got, n)
	}
}

// rosterToolNames are the tools a Driver-built set holds in the roster tests, and droppedTool the
// one the fallback's profile roster disables.
var rosterToolNames = []string{"alpha", "beta", "gamma"}

const droppedTool = "gamma"

// rosterRig is a seat rig whose parent runs a Driver-built tool set (Config.Tools) with the host's
// builder for it (Config.ToolsForRoster), and whose fallback answer binds a profile disabling
// droppedTool. built records every roster the builder was asked for.
func rosterRig(t *testing.T) (r *seatRig, built *[]domain.ToolRosterDelta) {
	t.Helper()
	var mu sync.Mutex
	var rosters []domain.ToolRosterDelta
	r = newSeatRig(t, func(cfg *domain.Config) {
		cfg.Tools = toolRegistryWithout(t, nil)
		cfg.ToolsForRoster = func(roster domain.ToolRosterDelta) *domain.ToolRegistry {
			mu.Lock()
			rosters = append(rosters, roster)
			mu.Unlock()
			return toolRegistryWithout(t, roster.Disabled)
		}
	})
	if r.parent.ownsToolSet {
		t.Fatal("the parent owns its tool set; want the Driver's injected set")
	}
	r.resolver.answer = func(context.Context, FallbackReport) FallbackAnswer {
		answer := spareAnswer()
		answer.Rebind.Profile.Tools = domain.ToolRosterDelta{Disabled: []string{droppedTool}}
		return answer
	}
	return r, &rosters
}

// toolRegistryWithout is a registry of rosterToolNames minus the names in disabled.
func toolRegistryWithout(t *testing.T, disabled []string) *domain.ToolRegistry {
	t.Helper()
	reg := domain.NewToolRegistry()
	for _, name := range rosterToolNames {
		if slices.Contains(disabled, name) {
			continue
		}
		if err := reg.Register(fakeTool{name: name}); err != nil {
			t.Fatalf("Register(%q): %v", name, err)
		}
	}
	return reg
}

// TestFallbackRehomeAppliesTheRosterToADriverToolSet pins the host-builder exception (ADR 0057
// bounds, amended 2026-11-02): an Agent whose tool set the Driver built offers the fallback
// profile's roster from the first request after the rehome, rebuilt through the Driver's builder
// with the fallback's roster axis, as an engine-composed set already does.
func TestFallbackRehomeAppliesTheRosterToADriverToolSet(t *testing.T) {
	t.Parallel()
	r, built := rosterRig(t)

	stepOnce(t, r.parent, "ask the model")

	if got := requestToolNames(r.primary.sent()[0]); !slices.Contains(got, droppedTool) {
		t.Fatalf("primary request tools = %v, want the primary roster with %q", got, droppedTool)
	}
	if r.spare.calls() != 1 {
		t.Fatalf("spare calls = %d, want 1", r.spare.calls())
	}
	if got, want := requestToolNames(r.spare.sent()[0]), []string{"alpha", "beta"}; !slices.Equal(got, want) {
		t.Errorf("resent request tools = %v, want the fallback roster %v", got, want)
	}
	if len(*built) != 1 || !slices.Equal((*built)[0].Disabled, []string{droppedTool}) {
		t.Errorf("builder rosters = %+v, want one, the fallback profile's", *built)
	}
}

// TestFallbackRehomeKeepsANarrowedChildsSubset pins the regression guard: a child spawned before
// the rehome with a narrowed roster keeps that subset after the rehome its own fault drives — it
// never inherits the parent's builder, so it is neither widened to the parent's roster nor
// re-narrowed by the fallback profile — and the parent's builder is not consulted on its behalf.
func TestFallbackRehomeKeepsANarrowedChildsSubset(t *testing.T) {
	t.Parallel()
	r, built := rosterRig(t)
	child := r.child(t, "narrowed")
	child.tools = child.tools.Subset("alpha", droppedTool)

	stepOnce(t, child, "delegated work")

	if r.spare.calls() != 1 {
		t.Fatalf("spare calls = %d, want the child's resend", r.spare.calls())
	}
	if got, want := requestToolNames(r.spare.sent()[0]), []string{"alpha", droppedTool}; !slices.Equal(got, want) {
		t.Errorf("child resend tools = %v, want its spawn-time subset %v", got, want)
	}
	if len(*built) != 0 {
		t.Errorf("builder rosters = %+v, want none — the child carries no builder", *built)
	}
}
