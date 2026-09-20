package agent

// The overflow seam: a context-window rejection is classified apart from a generic Upstream
// fault, because only the former says something the loop can act on (the PROMPT did not fit, so a
// shorter history is a real remedy). These tests pin the two halves of that split — the outcome
// respondAndReview reports and, crucially, the ErrorEvent it does NOT emit — plus the observable
// behaviour a caller sees, which must stay exactly what a plain fault produces until recovery is
// wired in.
//
// The transient seam (at the foot of this file) is the second fault the respond phase can act on:
// an in-band error whose CLASS the provider marked retryable, which the Turn re-streams up to its per-Turn budget.

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/stubllm"
)

// faultResponder answers every request with one raw HTTP reply — the upstream for both halves of
// the split with everything else held equal, so a difference in the assertion is a difference in
// the provider's classification and nothing else: an overflow is a 400 carrying the window marker
// (overflowBody, classed DeltaContextOverflow), a plain fault the 500 a server sends for anything
// else (classed DeltaError).
func faultResponder(t testing.TB, status int, body string) *scriptedUpstream {
	t.Helper()
	return scriptedResponder(t, stubllm.Turn{Repeat: true, HTTP: &stubllm.HTTPReply{Status: status, Body: body}})
}

// overflowFaultMsg is the sanitized message the provider builds for a llama.cpp 400 whose body
// matches an overflow marker (statusDelta, internal/provider/stream.go) — the real shape of the
// text the loop must carry through the seam unchanged.
const overflowFaultMsg = "apogee: context window exceeded: " + overflowBody

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
		status      int
		body        string
		msg         string // the text the provider renders the reply as
		wantOutcome turnOutcome
		wantCarried string
		wantEvents  int
	}{
		{
			name:        "overflow is its own outcome and surfaces nothing",
			status:      http.StatusBadRequest,
			body:        overflowBody,
			msg:         overflowFaultMsg,
			wantOutcome: turnOverflowed,
			wantCarried: overflowFaultMsg,
			wantEvents:  0,
		},
		{
			name:        "a plain fault still fails loudly",
			status:      http.StatusInternalServerError,
			body:        "boom",
			msg:         "apogee: upstream HTTP 500: boom",
			wantOutcome: turnFailed,
			wantCarried: "",
			wantEvents:  1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			a, err := newAgent(baseConfig(sink), faultResponder(t, tc.status, tc.body))
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			req, _ := a.buildRequest(0)
			run := &turnRun{turn: 0, req: req}

			resp, outcome, carried := a.respondAndReview(context.Background(), run)

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
// LEADING with the provider's message, a clean Exchange-complete boundary, and no assistant
// message committed. Recovery is quiet on SUCCESS (overflowrecovery_test.go); it may never change
// what the GIVE-UP looks like, and this is the anchor for that.
//
// The one amendment (2026-08-02): baseConfig knows no window, and a session with no window wedges
// — every later message overflows identically — so the give-up appends the remedy that ends it.
// The provider's message still leads unchanged; the suffix and its window-gating are pinned by
// TestOverflowGiveUpNamesTheWindowRemedy below.
func TestStepOverflowStillAbandonsTheTurnUnchanged(t *testing.T) {
	sink := &recordingSink{}
	a, err := newAgent(baseConfig(sink), faultResponder(t, http.StatusBadRequest, overflowBody))
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
	if !strings.HasPrefix(errs[0].Err, overflowFaultMsg) {
		t.Errorf("ErrorEvent.Err = %q, want it to lead with the provider's message %q", errs[0].Err, overflowFaultMsg)
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

// TestOverflowGiveUpNamesTheWindowRemedy pins the give-up message's one window-dependent half: with
// NO window known every growth bound is inert, so the session repeats this failure on every message
// until /clear — the event therefore names the config key that ends it. With a window known the
// user has nothing to set, so the provider's message surfaces byte-identically, exactly as ADR 0018
// decision 2 requires. Both rows drive the full Step give-up path, not the helper alone.
func TestOverflowGiveUpNamesTheWindowRemedy(t *testing.T) {
	tests := []struct {
		name       string
		window     int
		wantRemedy bool
	}{
		{name: "an unknown window names `context-window:`", window: 0, wantRemedy: true},
		{name: "a known window keeps the provider's message verbatim", window: 8192, wantRemedy: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			cfg := baseConfig(sink)
			cfg.Context.MaxContextTokens = tc.window // 0 ⇒ neither discovery nor `context-window:` reported one
			a, err := newAgent(cfg, faultResponder(t, http.StatusBadRequest, overflowBody))
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			if err := a.Submit(domain.UserInput{Text: "summarize the repository"}); err != nil {
				t.Fatalf("Submit: %v", err)
			}

			if _, err := a.Step(context.Background()); err != nil {
				t.Fatalf("Step: %v", err)
			}

			errs := errorEvents(sink.events)
			if len(errs) != 1 || errs[0].Source != "loop" {
				t.Fatalf("ErrorEvents = %v, want exactly one from source %q", errs, "loop")
			}
			if !strings.HasPrefix(errs[0].Err, overflowFaultMsg) {
				t.Errorf("ErrorEvent.Err = %q, want it to lead with the provider's message %q", errs[0].Err, overflowFaultMsg)
			}
			if got := strings.Contains(errs[0].Err, "`context-window:`"); got != tc.wantRemedy {
				t.Errorf("ErrorEvent names `context-window:` = %v, want %v; message: %q", got, tc.wantRemedy, errs[0].Err)
			}
			if !tc.wantRemedy && errs[0].Err != overflowFaultMsg {
				t.Errorf("ErrorEvent.Err = %q, want the provider's message verbatim %q with a window known",
					errs[0].Err, overflowFaultMsg)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The transient seam: a re-stream budget per Turn
// ---------------------------------------------------------------------------

// transientFaultMsg is the sanitized text the provider builds for the in-band 502 an aggregator
// wrapped in an HTTP 200 mid-stream — the observed shape (session 20260813T100440Z-104eaf7a) that
// used to kill the whole exchange with no retry.
const transientFaultMsg = `apogee: upstream in-band error 502: ` +
	`{"error":{"code":502,"message":"Provider returned error","metadata":{"raw":"upstream timed out"}},` +
	`"error_type":"provider_unavailable"}`

// retryableErrorScript is a Delta stream that faults with a TRANSIENT in-band error — the
// classification the provider attaches (Delta.Retryable) when the fault's class is one it would
// have retried at the HTTP layer. It is what the surviving hand-written fakes play;
// retryableErrorTurn is its stubllm twin, and errorScript the non-retryable turn.
func retryableErrorScript(msg string) []provider.Delta {
	return []provider.Delta{{Kind: provider.DeltaError, Err: msg, Retryable: true}}
}

// retryableErrorTurn is a turn that ends its stream with an in-band 502 carrying msg — the
// aggregator fault the provider classes TRANSIENT (Delta.Retryable), rendered "apogee: upstream
// in-band error 502: {…msg…}", so an assertion on the fault's text looks for msg within it.
func retryableErrorTurn(msg string) stubllm.Turn {
	return stubllm.Turn{Error: &stubllm.InBandError{Code: http.StatusBadGateway, Message: msg}}
}

// shortRestreamHoldoff shrinks the loop's re-stream hold-off for the duration of one test, so a
// test driving the recovery does not sit through the production second, and restores it after.
// Safe because the tests that call it are serial: none of them calls t.Parallel.
func shortRestreamHoldoff(t *testing.T) {
	t.Helper()
	previous := restreamHoldoff
	restreamHoldoff = time.Millisecond
	t.Cleanup(func() { restreamHoldoff = previous })
}

// countEvents reports how many of events are of type T.
func countEvents[T domain.Event](events []domain.Event) int {
	n := 0
	for _, e := range events {
		if _, ok := e.(T); ok {
			n++
		}
	}
	return n
}

// lastFaultMsg returns the message the last faulting turn in scripts carries — the text the
// give-up path must surface, which for a re-streamed Turn is the SECOND attempt's fault, not the
// first's. The provider renders the turn's message inside its own wording (the in-band envelope,
// the HTTP status line), so the assertion is that the surfaced text carries it.
func lastFaultMsg(scripts []stubllm.Turn) string {
	msg := ""
	for _, turn := range scripts {
		switch {
		case turn.Error != nil:
			msg = turn.Error.Message
		case turn.HTTP != nil:
			msg = turn.HTTP.Body
		}
	}
	return msg
}

// TestRespondAndReviewReStreamsATransientFaultUpToTheBudget pins the recovery and its edges. A
// fault the provider classed transient re-sends the SAME request, up to the Turn's re-stream
// budget (three), and an attempt that lands completes the Turn SILENTLY — one StreamResetEvent
// per re-stream, so a streaming Driver discards the partial reply, and no ErrorEvent, because
// nothing reached the user that they must act on (the overflow-recovery precedent). The edges: a
// fourth transient fault has spent the budget and gives up naming that last fault, and a fault
// that was never transient never re-streams at all. Under shortRestreamHoldoff the wall clock says
// nothing about the ladder — TestRestreamHoldoffLadder pins the rungs on the pure function.
func TestRespondAndReviewReStreamsATransientFaultUpToTheBudget(t *testing.T) {
	shortRestreamHoldoff(t)

	tests := []struct {
		name        string
		scripts     []stubllm.Turn
		wantOutcome turnOutcome
		wantText    string
		wantCalls   int
		wantResets  int
		wantErrors  int
	}{
		{
			name:        "a transient fault re-streams and the recovered Turn stays quiet",
			scripts:     []stubllm.Turn{retryableErrorTurn(transientFaultMsg), contentTurn("recovered")},
			wantOutcome: turnOK,
			wantText:    "recovered",
			wantCalls:   2,
			wantResets:  1,
			wantErrors:  0,
		},
		{
			name: "three transient faults are ridden out and the fourth attempt lands",
			scripts: []stubllm.Turn{
				retryableErrorTurn("first blip"),
				retryableErrorTurn("second blip"),
				retryableErrorTurn("third blip"),
				contentTurn("recovered"),
			},
			wantOutcome: turnOK,
			wantText:    "recovered",
			wantCalls:   4,
			wantResets:  3,
			wantErrors:  0,
		},
		{
			name: "a fourth transient fault has spent the budget and gives up",
			scripts: []stubllm.Turn{
				retryableErrorTurn("first blip"),
				retryableErrorTurn("second blip"),
				retryableErrorTurn("third blip"),
				retryableErrorTurn("fourth blip"),
			},
			wantOutcome: turnFailed,
			wantCalls:   4,
			wantResets:  3,
			wantErrors:  1,
		},
		{
			name:        "a fault that is not transient fails on the spot",
			scripts:     []stubllm.Turn{errorScript("bad request"), contentTurn("unreached")},
			wantOutcome: turnFailed,
			wantCalls:   1,
			wantResets:  0,
			wantErrors:  1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			responder := scriptedResponder(t, tc.scripts...)
			a, err := newAgent(baseConfig(sink), responder)
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			req, _ := a.buildRequest(0)
			run := &turnRun{turn: 0, req: req}

			resp, outcome, carried := a.respondAndReview(context.Background(), run)

			if outcome != tc.wantOutcome {
				t.Errorf("outcome = %v, want %v", outcome, tc.wantOutcome)
			}
			if carried != "" {
				t.Errorf("carried message = %q, want empty — only an overflow carries its fault out", carried)
			}
			if tc.wantText == "" {
				if resp != nil {
					t.Errorf("resp = %+v, want nil on a terminal fault", resp)
				}
			} else if resp == nil || resp.Text() != tc.wantText {
				t.Errorf("resp = %+v, want the re-streamed reply %q", resp, tc.wantText)
			}
			if responder.calls() != tc.wantCalls {
				t.Errorf("Upstream calls = %d, want %d — the Turn re-streams at most %d times", responder.calls(), tc.wantCalls, defaultRestreamBudget)
			}
			if got := countEvents[domain.StreamResetEvent](sink.events); got != tc.wantResets {
				t.Errorf("StreamResetEvents = %d, want %d", got, tc.wantResets)
			}
			errs := errorEvents(sink.events)
			if len(errs) != tc.wantErrors {
				t.Fatalf("ErrorEvents = %d (%v), want %d", len(errs), errs, tc.wantErrors)
			}
			if tc.wantErrors == 1 && (errs[0].Source != "loop" || !strings.Contains(errs[0].Err, lastFaultMsg(tc.scripts))) {
				t.Errorf("ErrorEvent = {Source:%q Err:%q}, want {Source:%q Err:…%q…} — the last attempt's fault",
					errs[0].Source, errs[0].Err, "loop", lastFaultMsg(tc.scripts))
			}
			if run.restreamsSpent != tc.wantResets {
				t.Errorf("restreamsSpent = %d, want %d — one spend per StreamResetEvent", run.restreamsSpent, tc.wantResets)
			}
		})
	}
}

// TestReStreamBudgetIsPerTurn proves the budget is scoped to the Turn rather than the session: a
// later Turn that hits its own blip re-streams again instead of inheriting a spent counter.
// Session-scoping it would leave every Turn after the first few recovered stutters with no
// recovery at all.
func TestReStreamBudgetIsPerTurn(t *testing.T) {
	shortRestreamHoldoff(t)

	sink := &recordingSink{}
	responder := scriptedResponder(t,
		retryableErrorTurn(transientFaultMsg), contentTurn("first"), // Turn 0: a blip, then the answer
		retryableErrorTurn(transientFaultMsg), contentTurn("second"), // Turn 1: its own blip, its own recovery
	)
	a, err := newAgent(baseConfig(sink), responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	for _, text := range []string{"first question", "second question"} {
		if err := a.Submit(domain.UserInput{Text: text}); err != nil {
			t.Fatalf("Submit(%q): %v", text, err)
		}
		res, err := a.Step(context.Background())
		if err != nil {
			t.Fatalf("Step(%q): %v", text, err)
		}
		if res.Status != domain.StatusExchangeComplete || res.Faulted {
			t.Fatalf("Step(%q) result = %+v, want a clean exchange-complete", text, res)
		}
	}

	if got := countEvents[domain.StreamResetEvent](sink.events); got != 2 {
		t.Errorf("StreamResetEvents = %d, want 2 — each Turn spends its own budget", got)
	}
	if errs := errorEvents(sink.events); len(errs) != 0 {
		t.Errorf("ErrorEvents = %v, want none — both Turns recovered", errs)
	}
	if me, ok := lastMessageEvent(sink.events); !ok || me.Text != "second" {
		t.Errorf("final MessageEvent = %+v (ok=%v), want %q", me, ok, "second")
	}
}
